// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package closure contains the Matter ClosureControl cluster server
// (0x0104) for the Positioning + Ventilation feature profile. It is
// instantiated by the rich-model garage type and mounted onto its Matter
// endpoint projection.
//
// ClosureControl is what Matter offers for a closure whose travel has
// named stops rather than a continuous position. WindowCovering, which
// the garage projection used before, has only a lift axis: a garage
// drive's ventilation position had to be expressed as a percentage
// somewhere between open and closed, which no controller can label and
// no user can find.
//
// The cluster's identity is the generated definition (cluster/spec/
// closurecontrol, ADR 0013): ids, revision, the feature check, the
// attribute, command and event lists, the event priorities, the write
// statuses and the TargetPositionEnum values the selection allows. The
// FeatureMap is derived from what the server serves ([DerivedFeatureMap]),
// not taken on trust: a host FeatureMap naming a feature whose elements
// the server does not serve is refused at construction. MoveTo decodes
// through the definition; the MoveTo and Stop rules, the state and the
// events are the server's.
//
// The package also holds the ClosureDimension server (0x0105,
// [DimensionServer]) of a ClosurePanel: one movable part of a closure,
// with a continuous position, a latch, or both.
package closure

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	closuredef "github.com/SukramJ/go-fabric/cluster/spec/closurecontrol"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// ClusterRevision is the ClosureControl cluster revision of the generated
// definition. Pinned against matter.js HEAD by
// TestParityMatterJS_ClosureControlClusterRevision.
const ClusterRevision = closuredef.Revision

// PositioningVentilationFeatureMap is the feature set a garage drive with
// a ventilation stop advertises: Positioning plus Ventilation.
//
// Ventilation carries conformance "[PS]" (matter.js
// closure-control.element.ts:31), so it is never advertised alone.
const PositioningVentilationFeatureMap = wire.ClosureControlFeaturePositioning |
	wire.ClosureControlFeatureVentilation

// servedFeatures are the features whose elements this server serves on
// any mover.
//
// Positioning is fixed: OverallCurrentState / OverallTargetState carry a
// Position and MoveTo moves to one. Ventilation and Pedestrian add only a
// TargetPositionEnum / CurrentPositionEnum value each, ManuallyOperable
// only EngageStateChanged (served by [ControlServer.SetEngaged]),
// Protection only MainState Protected, and Instantaneous withdraws Stop,
// MovementCompleted and CountdownTime — so a host may select them for a
// drive that has them (closure-control.element.ts, conformance of every
// element). The capability features ([capabilityFeatures]) are served on
// a mover that implements their interface.
const servedFeatures = wire.ClosureControlFeaturePositioning |
	wire.ClosureControlFeatureVentilation |
	wire.ClosureControlFeaturePedestrian |
	wire.ClosureControlFeatureProtection |
	wire.ClosureControlFeatureManuallyOperable |
	wire.ClosureControlFeatureInstantaneous

// capabilityFeatures are the features served on a mover that implements
// the optional interface each needs: MotionLatching (LatchControlModes,
// the Latch fields, MoveTo's Latch) a [closuredef.Latcher], Speed (the
// Speed fields, MoveTo's Speed) a [closuredef.SpeedMover], Calibration
// (Calibrate, MainState Calibrating) a [closuredef.Calibrator]. An Ahead
// of matter.js row (notes/parity/by_design.md
// BD-chip-ClosureControl-RulesInServer; [AheadFeatures]).
var capabilityFeatures = []struct {
	bit   uint32
	name  string
	iface string
	has   func(Mover) bool
}{
	{wire.ClosureControlFeatureMotionLatching, "MotionLatching", "closurecontrol.Latcher", func(m Mover) bool { _, ok := m.(closuredef.Latcher); return ok }},
	{wire.ClosureControlFeatureSpeed, "Speed", "closurecontrol.SpeedMover", func(m Mover) bool { _, ok := m.(closuredef.SpeedMover); return ok }},
	{wire.ClosureControlFeatureCalibration, "Calibration", "closurecontrol.Calibrator", func(m Mover) bool { _, ok := m.(closuredef.Calibrator); return ok }},
}

// servable is the feature set the server serves on m: [servedFeatures]
// and each capability feature whose interface m implements.
func servable(m Mover) uint32 {
	fm := servedFeatures
	for _, c := range capabilityFeatures {
		if m != nil && c.has(m) {
			fm |= c.bit
		}
	}
	return fm
}

// ErrFeatureMap reports a Config.FeatureMap the server does not serve.
var ErrFeatureMap = errors.New("closure: FeatureMap names features this server does not serve")

// Mover is the drive (closurecontroldef.Mover): MoveTo applies a target
// position the server checked, Stop halts motion.
//
// Either returns an error to refuse; the server maps that onto a Failure
// status and leaves its target state untouched, so a controller that
// could not reach the device does not see a target the device never
// accepted.
//
// The mover owns the southbound urgency of the writes it performs: the
// cluster contract carries no priority, so a host whose command queue
// ranks by urgency names the value it wants there, where the device
// vocabulary is already in scope.
type Mover = closuredef.Mover

// Config carries the handlers and initial state for a
// [ControlServer].
type Config struct {
	// FeatureMap is the advertised feature set. Zero selects
	// [PositioningVentilationFeatureMap]. Any other value must be the one
	// [DerivedFeatureMap] derives from it — Positioning plus a choice of
	// Ventilation, Pedestrian, Protection, ManuallyOperable and
	// Instantaneous, and MotionLatching, Speed and Calibration where the
	// Mover implements their interface — or [New] refuses it. The
	// conformance between them (Speed "[PS & !IS]") is the definition's.
	FeatureMap uint32
	// Mover carries out MoveTo and Stop, and through its optional
	// interfaces the latch, the speed and Calibrate. Nil makes the
	// commands report Failure rather than accepting a command that
	// reaches nothing.
	Mover Mover
}

// ControlServer implements
// [github.com/SukramJ/go-fabric/contract.ClusterServer]
// for the Matter ClosureControl cluster (0x0104).
//
// The server holds the state a controller reads and forwards commands to
// the handlers in [Config]; the mapping between a Matter position and the
// device's own vocabulary lives in the rich-model projection, not here.
type ControlServer struct {
	// AttributeChanges tells the bridge which attributes moved, so a
	// change the drive reports between commands (an arrival, an error)
	// reaches subscribers for exactly those attributes.
	cluster.AttributeChanges
	spec.Hooks

	// mu guards currentPosition, currentLatch, currentSpeed, errorList,
	// mainState, secureState, targetPosition, targetLatch and targetSpeed.
	mu sync.RWMutex

	inst  *spec.Instance
	mover Mover

	// latchModes is LatchControlModes (LT, fixed), read from the
	// mover's [closuredef.Latcher] at construction.
	latchModes closuredef.LatchControlModesBitmap

	mainState wire.ClosureMainState
	errorList wire.ClosureErrorList

	// currentPosition and targetPosition are nil until observed, which is
	// the null the spec asks for on a quality-X field. A drive that has
	// not reported yet must not read as FullyClosed.
	currentPosition *wire.ClosureCurrentPosition
	targetPosition  *wire.ClosureTargetPosition
	secureState     *bool

	// currentLatch and targetLatch are the Latch fields (LT): nil is
	// null. currentSpeed and targetSpeed are the Speed fields (SP), which
	// carry no null: Auto until the drive or a MoveTo says otherwise.
	// Each is replaced by a fresh pointer, never written through.
	currentLatch, targetLatch *bool
	currentSpeed, targetSpeed *closuredef.ThreeLevelAutoEnum

	// stateReports and currentReports count the drive's reports —
	// SetMainState and the OverallCurrentState setters. MoveTo and
	// Calibrate compare them across the mover call: a report made while
	// the mover had the command, from inside it or from another goroutine,
	// is newer than the outcome the command writes after it returned, and
	// is kept (review II, R2-P1-clu-01).
	stateReports, currentReports uint64

	// emitter and endpoint address the events (closurecontrol_events.go);
	// the bridge wires both at reassembly.
	emitter  contract.EventEmitter
	endpoint uint16
}

// DerivedFeatureMap is the FeatureMap the server advertises for cfg: what
// it serves. Zero selects [PositioningVentilationFeatureMap]; otherwise
// Positioning is always served and the rest is taken from cfg.FeatureMap
// as far as the server serves it on cfg.Mover (see servedFeatures and
// capabilityFeatures). It differs from a non-zero cfg.FeatureMap exactly
// when that one names a feature the server does not serve — or whose
// interface the Mover lacks — or leaves out Positioning.
func DerivedFeatureMap(cfg Config) uint32 {
	if cfg.FeatureMap == 0 {
		return PositioningVentilationFeatureMap
	}
	return wire.ClosureControlFeaturePositioning | cfg.FeatureMap&servable(cfg.Mover)
}

// New constructs a [ControlServer] from cfg. A cfg.FeatureMap other than
// zero and the one [DerivedFeatureMap] derives is refused with
// [ErrFeatureMap], naming each capability feature whose interface the
// Mover lacks: advertising it would list attributes, commands or events
// the server does not serve. A derived selection the definition's
// conformance refuses (Speed with Instantaneous) is refused with the
// definition's error.
func New(cfg Config) (*ControlServer, error) {
	fm := DerivedFeatureMap(cfg)
	if cfg.FeatureMap != 0 && cfg.FeatureMap != fm {
		var msg strings.Builder
		fmt.Fprintf(&msg, "0x%X, the server serves 0x%X", cfg.FeatureMap, fm)
		for _, c := range capabilityFeatures {
			if cfg.FeatureMap&c.bit != 0 && fm&c.bit == 0 {
				fmt.Fprintf(&msg, "; %s needs a Mover that implements %s", c.name, c.iface)
			}
		}
		return nil, fmt.Errorf("%w: %s", ErrFeatureMap, msg.String())
	}
	// Positioning holds "O.a+" and "[PS]"; Speed "[PS & !IS]" is the one
	// conformance a served selection can break, and spec.New refuses it.
	inst, err := spec.New(closuredef.Definition, spec.Options{Features: fm})
	if err != nil {
		return nil, fmt.Errorf("closure: %w", err)
	}
	s := &ControlServer{
		inst:  inst,
		mover: cfg.Mover,
		// SetupRequired, not Stopped: the drive has reported nothing yet,
		// and Stopped is a claim about a device we have not heard from.
		// Mirrors matter.js closure-control.element.ts:113 MainStateEnum.
		mainState: wire.ClosureMainStateSetupRequired,
		errorList: wire.ClosureErrorList{},
	}
	if l, ok := cfg.Mover.(closuredef.Latcher); ok && s.has(wire.ClosureControlFeatureMotionLatching) {
		s.latchModes = l.LatchControlModes()
	}
	if s.has(wire.ClosureControlFeatureSpeed) {
		cur, tgt := closuredef.ThreeLevelAutoAuto, closuredef.ThreeLevelAutoAuto
		s.currentSpeed, s.targetSpeed = &cur, &tgt
	}
	s.Init(inst)
	return s, nil
}

// has reports whether the advertised FeatureMap carries feature.
func (s *ControlServer) has(feature uint32) bool { return s.inst.FeatureMap()&feature != 0 }

// NewControlServer constructs a [ControlServer] from cfg, as [New] does; it
// panics on a cfg.FeatureMap [New] refuses — a wiring error, not a runtime
// condition. A host that builds its FeatureMap at run time calls [New].
func NewControlServer(cfg Config) *ControlServer {
	s, err := New(cfg)
	if err != nil {
		panic(err)
	}
	return s
}

// MatterClusterID returns the ClosureControl cluster ID (0x0104).
func (s *ControlServer) MatterClusterID() uint32 { return closuredef.ClusterID }

// MatterRead resolves the mandatory ClosureControl attributes and the two
// globals.
//
// CountdownTime (conformance "[PS & !IS]") is optional and not served;
// LatchControlModes (conformance "LT") is answered with MotionLatching.
func (s *ControlServer) MatterRead(attrID uint32) (value any, ok bool) {
	// FeatureMap and ClusterRevision: every cluster server answers the two
	// universal globals itself; nothing upstream fills them in. Without
	// FeatureMap a controller cannot see the Ventilation feature, which is
	// the whole reason this cluster is here.
	if v, ok := s.inst.ReadGlobal(attrID); ok {
		return v, true
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	switch attrID {
	case wire.ClosureControlAttrMainState:
		return uint8(s.mainState), true
	case wire.ClosureControlAttrCurrentErrorList:
		// A copy: the caller must not be able to mutate cluster state
		// through a value it read.
		out := make(wire.ClosureErrorList, len(s.errorList))
		copy(out, s.errorList)
		return out, true
	case wire.ClosureControlAttrOverallCurrentState:
		return s.currentStateLocked(), true
	case wire.ClosureControlAttrOverallTargetState:
		return s.targetStateLocked(), true
	case wire.ClosureControlAttrLatchControlModes:
		if !s.has(wire.ClosureControlFeatureMotionLatching) {
			return nil, false
		}
		return uint8(s.latchModes), true
	default:
		return nil, false
	}
}

// MatterWrite refuses every write, as the definition answers it: every
// ClosureControl attribute carries access "R V" (closure-control.element.ts),
// so a served one is UNSUPPORTED_WRITE (Core §8.7.3.2; matter.js
// AttributeWriteResponse — a plain error read as FAILURE, which TC-ACE-2.2's
// write-access checker refuses) and any other UNSUPPORTED_ATTRIBUTE. State
// changes travel through MoveTo and Stop.
func (s *ControlServer) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	v, err := s.inst.ValidateWrite(attrID, value, nil)
	if err != nil {
		return err
	}
	return s.CheckWrite(ctx, attrID, v)
}

// closureUnsupportedCommandErr is a typed [im.StatusCodeError] for a
// command the advertised feature set does not include.
type closureUnsupportedCommandErr struct{ msg string }

func (e closureUnsupportedCommandErr) Error() string { return e.msg }
func (closureUnsupportedCommandErr) MatterStatusCode() im.StatusCode {
	return im.StatusUnsupportedCommand
}

// closureInvalidCommandErr is a typed [im.StatusCodeError] for a MoveTo
// that carries none of its fields.
type closureInvalidCommandErr struct{ msg string }

func (e closureInvalidCommandErr) Error() string                 { return e.msg }
func (closureInvalidCommandErr) MatterStatusCode() im.StatusCode { return im.StatusInvalidCommand }
func (closureInvalidCommandErr) Unwrap() error                   { return wire.ErrClosureControlMalformed }

// closureConstraintErr is a typed [im.StatusCodeError] for a MoveTo
// carrying a position outside the advertised feature set.
type closureConstraintErr struct{ msg string }

func (e closureConstraintErr) Error() string                 { return e.msg }
func (closureConstraintErr) MatterStatusCode() im.StatusCode { return im.StatusConstraintError }

// Compile-time assertions.
var (
	_ im.StatusCodeError = closureUnsupportedCommandErr{}
	_ im.StatusCodeError = closureConstraintErr{}
)

// MatterInvoke handles the ClosureControl commands: MoveTo (conformance
// M), Stop (conformance "!IS") and Calibrate (conformance "CL").
//
// Stop with Instantaneous and Calibrate without Calibration report
// UnsupportedCommand rather than succeeding silently, as connectedhomeip's
// HandleStop and HandleCalibrate do (ClosureControlCluster.cpp:535, :644
// at 6170af84).
func (s *ControlServer) MatterInvoke(
	ctx context.Context, cmdID uint32, fields any,
) (response any, err error) {
	if resp, handled, err := s.Invoke(ctx, cmdID, fields); handled {
		return resp, err
	}
	switch cmdID {
	case closuredef.CmdMoveTo:
		return nil, s.invokeMoveTo(ctx, fields)
	case closuredef.CmdStop:
		if s.has(wire.ClosureControlFeatureInstantaneous) {
			return nil, closureUnsupportedCommandErr{"closure: Stop is withdrawn by the Instantaneous feature"}
		}
		return nil, s.invokeStop(ctx)
	case closuredef.CmdCalibrate:
		return nil, s.invokeCalibrate(ctx)
	default:
		return nil, im.UnsupportedCommandf("closure: unknown command 0x%02X", cmdID)
	}
}

// invokeMoveTo applies a MoveTo request, as the specification text
// matter.js carries describes it (closure-control.resource.ts, MoveTo,
// cluster§5.4.8.2): a request with none of the three "O.a+" fields is
// INVALID_COMMAND; a field of a feature the server does not advertise is
// ignored rather than refused; with Positioning, an absent Position falls
// back to OverallTargetState.Position when the request carries no served
// Latch or Speed and the closure is not latched, and with that null too the
// closure does not change its position; a Position outside the advertised set is CONSTRAINT_ERROR.
// matter.js has no ClosureControl logic
// (notes/parity/by_design.md BD-chip-ClosureControl-RulesInServer):
// connectedhomeip's HandleMoveTo is the authority for the state rules — a
// closure whose MainState is not Moving, WaitingForMotion or Stopped
// answers INVALID_IN_STATE. chip's other gate, a null
// OverallCurrentState (ClosureControlCluster.cpp:560-562 at 6170af84),
// cannot fire here: this server reads OverallCurrentState as a struct
// whose fields are null until observed, never as null.
//
// With MotionLatching and Speed the request's Latch and Speed follow
// HandleMoveTo too (ClosureControlCluster.cpp:579-597, :607-620 at
// 6170af84): a Latch the LatchControlModes do not allow remotely is
// INVALID_IN_STATE, an unknown Speed CONSTRAINT_ERROR, and a Position
// change of a latched closure without Latch false INVALID_IN_STATE. The
// mover hears the request in the order a drive carries it out — unlatch,
// speed, motion, latch.
func (s *ControlServer) invokeMoveTo(ctx context.Context, fields any) error {
	req, err := moveToRequest(fields)
	if errors.Is(err, wire.ErrClosureControlMalformed) {
		return closureInvalidCommandErr{"closure: MoveTo carries none of Position, Latch and Speed"}
	}
	if err != nil {
		return err
	}
	s.mu.RLock()
	fallback := s.targetPosition
	state := s.mainState
	latched := s.currentLatch != nil && *s.currentLatch
	stateReports, currentReports := s.stateReports, s.currentReports
	s.mu.RUnlock()
	// Latch and Speed of a feature the server does not advertise are
	// ignored (cluster§5.4.8.2; chip :579, :592).
	latch, speed := req.Latch, req.Speed
	if !s.has(wire.ClosureControlFeatureMotionLatching) {
		latch = nil
	}
	if !s.has(wire.ClosureControlFeatureSpeed) {
		speed = nil
	}
	target := (*wire.ClosureTargetPosition)(req.Position)
	if target == nil && latch == nil && speed == nil && !latched {
		// The fallback to the outstanding target moves the closure, which
		// a latched closure may do only with Latch false in the request
		// (chip :607-620); a request that carries none falls back to
		// nothing while latched (review II, R2-P1-clu-06).
		target = fallback
	}
	if target != nil {
		if err := s.checkPositionSupported(*target); err != nil {
			return err
		}
	}
	// Mirrors connectedhomeip
	// src/app/clusters/closure-control-server/ClosureControlCluster.cpp:579-590
	// at 6170af84: a Latch true needs RemoteLatching, a Latch false
	// RemoteUnlatching, or the request is INVALID_IN_STATE.
	if latch != nil {
		need := closuredef.LatchControlModesRemoteUnlatching
		if *latch {
			need = closuredef.LatchControlModesRemoteLatching
		}
		if s.latchModes&need == 0 {
			return spec.Errorf(im.StatusInvalidInState, "closure: MoveTo Latch %t is not allowed remotely (LatchControlModes 0x%X)", *latch, s.latchModes)
		}
	}
	// chip :592-597: a Speed outside ThreeLevelAutoEnum is CONSTRAINT_ERROR.
	if speed != nil && !slices.ContainsFunc(closuredef.ThreeLevelAutoEnumDef.Values, func(v spec.EnumValue) bool { return v.Value == uint64(*speed) }) {
		return closureConstraintErr{fmt.Sprintf("closure: MoveTo speed %d is not a ThreeLevelAutoEnum value", *speed)}
	}
	// Mirrors connectedhomeip
	// src/app/clusters/closure-control-server/ClosureControlCluster.cpp:599-605
	// at 6170af84: in any MainState other than Moving, WaitingForMotion or
	// Stopped MoveTo is INVALID_IN_STATE, after the field checks and
	// before the delegate — so an Error, SetupRequired, Protected,
	// Disengaged or Calibrating state the host set is not overwritten
	// with Moving. One the host reports while the mover has the command
	// is kept by the report counters below.
	switch state {
	case wire.ClosureMainStateMoving, wire.ClosureMainStateWaitingForMotion, wire.ClosureMainStateStopped:
	default:
		return spec.Errorf(im.StatusInvalidInState, "closure: MoveTo in MainState %d", state)
	}
	// Mirrors connectedhomeip ClosureControlCluster.cpp:607-620 at
	// 6170af84: a Position change of a latched closure needs Latch false
	// in the same request.
	if latched && req.Position != nil && s.has(wire.ClosureControlFeatureMotionLatching) && (latch == nil || *latch) {
		return spec.Errorf(im.StatusInvalidInState, "closure: MoveTo Position while latched needs Latch false")
	}
	if target == nil && latch == nil && speed == nil {
		return nil
	}
	if s.mover == nil {
		return errors.New("closure: MoveTo has no mover")
	}
	if err := s.drive(ctx, target, latch, speed); err != nil {
		// Deliberately no state change: recording a target the device
		// refused would leave the controller reading a move that never
		// happened.
		return fmt.Errorf("closure: MoveTo: %w", err)
	}
	s.recordMove(target, latch, speed, stateReports, currentReports)
	return nil
}

// recordMove writes the outcome of a MoveTo the mover accepted. It is
// written after the mover returned, as chip writes it after the delegate
// (ClosureControlCluster.cpp:623-637 at 6170af84). chip's handler holds
// the stack lock, so no report interleaves with it; here the drive may
// report while the mover has the command, and that report is newer than
// this write: a MainState it reported stays (stateReports moved), and a
// target the position or latch it reported already fulfils is not left
// outstanding (currentReports moved; the arrival rule of setCurrent).
func (s *ControlServer) recordMove(target *wire.ClosureTargetPosition, latch *bool, speed *closuredef.ThreeLevelAutoEnum, stateReports, currentReports uint64) {
	s.mu.Lock()
	if target != nil {
		t := *target
		s.targetPosition = &t
	}
	if latch != nil {
		s.targetLatch = copyOf(latch)
	}
	if speed != nil {
		s.targetSpeed = copyOf(speed)
	}
	if s.currentReports != currentReports {
		s.clearFulfilledTargetsLocked(true, true)
	}
	attrs := []uint32{wire.ClosureControlAttrOverallTargetState}
	if s.stateReports == stateReports {
		s.mainState = wire.ClosureMainStateMoving
		attrs = []uint32{wire.ClosureControlAttrMainState, wire.ClosureControlAttrOverallTargetState}
	}
	s.mu.Unlock()
	s.NotifyFrom(contract.OriginCommand, attrs...)
}

// drive hands a checked MoveTo to the mover in the order a drive carries
// it out: an unlatch before the motion, the speed, the motion, a latch
// after it (connectedhomeip's closure-app orders a panel's actions so,
// examples/closure-app/linux/ClosureManager.cpp HandlePanelUnlatchAction,
// HandlePanelSetTargetAction at 6170af84). The capability interfaces are
// present: the advertised features required them at construction.
func (s *ControlServer) drive(ctx context.Context, target *wire.ClosureTargetPosition, latch *bool, speed *closuredef.ThreeLevelAutoEnum) error {
	latcher, _ := s.mover.(closuredef.Latcher)
	if latch != nil && !*latch {
		if err := latcher.Latch(ctx, false); err != nil {
			return err
		}
	}
	if speeder, ok := s.mover.(closuredef.SpeedMover); ok && speed != nil {
		if err := speeder.SetSpeed(ctx, *speed); err != nil {
			return err
		}
	}
	if target != nil {
		if err := s.mover.MoveTo(ctx, closuredef.TargetPositionEnum(*target)); err != nil {
			return err
		}
	}
	if latch != nil && *latch {
		return latcher.Latch(ctx, true)
	}
	return nil
}

// invokeCalibrate starts a calibration, as connectedhomeip's
// HandleCalibrate does (ClosureControlCluster.cpp:642-665 at 6170af84):
// without Calibration UnsupportedCommand; already Calibrating SUCCESS
// without another call; in any MainState other than Stopped and
// SetupRequired INVALID_IN_STATE; otherwise the mover's
// [closuredef.Calibrator] starts it and, on its success, MainState is
// Calibrating — the drive reports Stopped when it is done. matter.js has
// no Calibrate logic (ClosureControlServer.ts:17, the empty subclass).
func (s *ControlServer) invokeCalibrate(ctx context.Context) error {
	if !s.has(wire.ClosureControlFeatureCalibration) {
		return closureUnsupportedCommandErr{
			"closure: Calibrate requires the Calibration feature, which this server does not advertise",
		}
	}
	s.mu.RLock()
	state, stateReports := s.mainState, s.stateReports
	s.mu.RUnlock()
	switch state {
	case wire.ClosureMainStateCalibrating:
		return nil
	case wire.ClosureMainStateStopped, wire.ClosureMainStateSetupRequired:
	default:
		return spec.Errorf(im.StatusInvalidInState, "closure: Calibrate in MainState %d", state)
	}
	calibrator, ok := s.mover.(closuredef.Calibrator)
	if !ok {
		return errors.New("closure: Calibrate has no calibrator")
	}
	if err := calibrator.Calibrate(ctx); err != nil {
		return fmt.Errorf("closure: Calibrate: %w", err)
	}
	// chip sets Calibrating after the delegate (:658-661) under its stack
	// lock; a MainState the drive reported while the calibrator had the
	// command (Stopped from a calibration already done, Error) is newer
	// and stays.
	s.mu.Lock()
	written := s.stateReports == stateReports
	if written {
		s.mainState = wire.ClosureMainStateCalibrating
	}
	s.mu.Unlock()
	if written {
		s.NotifyFrom(contract.OriginCommand, wire.ClosureControlAttrMainState)
	}
	return nil
}

// copyOf is a copy of *p, nil for nil: a value read out of the server, or
// handed into it, does not alias its state.
func copyOf[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// invokeStop halts motion (cluster§5.4.8.1): only a closure that is
// Moving, WaitingForMotion or Calibrating stops and becomes Stopped; in
// any other state the command changes nothing. SUCCESS either way. A
// mover that is also a [closuredef.StopObserver] hears of the ignored
// Stop (StopIgnored), and its error answers FAILURE; the state stays as
// it is. matter.js has no Stop logic to mirror here: a case-insensitive
// grep for "stop" in packages/node/src/behaviors/closure-control/
// ClosureControlServer.ts at the pin (862ad75f) finds nothing.
func (s *ControlServer) invokeStop(ctx context.Context) error {
	s.mu.RLock()
	state := s.mainState
	s.mu.RUnlock()
	switch state {
	case wire.ClosureMainStateMoving, wire.ClosureMainStateWaitingForMotion, wire.ClosureMainStateCalibrating:
	default:
		if o, ok := s.mover.(closuredef.StopObserver); ok {
			if err := o.StopIgnored(ctx); err != nil {
				return fmt.Errorf("closure: Stop: %w", err)
			}
		}
		return nil
	}
	if s.mover == nil {
		return errors.New("closure: Stop has no mover")
	}
	if err := s.mover.Stop(ctx); err != nil {
		return fmt.Errorf("closure: Stop: %w", err)
	}
	s.mu.Lock()
	// MainState only: OverallTargetState keeps the target, as
	// connectedhomeip's HandleStop touches MainState alone
	// (ClosureControlCluster.cpp:531-551 at 6170af84; W3-club1-05).
	s.mainState = wire.ClosureMainStateStopped
	s.mu.Unlock()
	s.NotifyFrom(contract.OriginCommand, wire.ClosureControlAttrMainState)
	return nil
}

// checkPositionSupported rejects a target the advertised feature set does
// not carry, by the TargetPositionEnum conformance of the definition:
// MoveToPedestrianPosition is PD, MoveToVentilationPosition VT, the other
// three M (closure-control.element.ts, TargetPositionEnum). Accepting one
// whose feature is not advertised would move the drive somewhere the
// controller was never told about.
func (s *ControlServer) checkPositionSupported(p wire.ClosureTargetPosition) error {
	if !slices.ContainsFunc(closuredef.TargetPositionEnumDef.Values, func(v spec.EnumValue) bool { return v.Value == uint64(p) }) {
		return closureConstraintErr{fmt.Sprintf("closure: MoveTo position %d is not a TargetPositionEnum value", p)}
	}
	if !s.inst.EnumSupported(closuredef.TargetPositionEnumDef, uint64(p)) {
		return closureConstraintErr{
			fmt.Sprintf("closure: MoveTo position %d needs a feature this server does not advertise", p),
		}
	}
	return nil
}

// moveToRequest normalises the command payload the bridge hands over. The
// bridge decodes MoveTo through the generated definition, so a real
// invocation arrives as [closuredef.MoveToRequest] (a field of the wrong
// TLV type already answered INVALID_COMMAND there, as matter.js's request
// schema answers it), by value or by pointer — the primary form; a host
// that decodes the command itself may pass the tag-keyed map of the
// bridge's generic salvage path (bridge/fields_reader.go
// decodeGenericTagMap).
func moveToRequest(fields any) (closuredef.MoveToRequest, error) {
	if req, ok := spec.RequestOf[closuredef.MoveToRequest](fields); ok {
		if req.Position == nil && req.Latch == nil && req.Speed == nil {
			return closuredef.MoveToRequest{}, wire.ErrClosureControlMalformed
		}
		return req, nil
	}
	switch v := fields.(type) {
	case map[uint8]any:
		return moveToRequestFromTagMap(v)
	case *closuredef.MoveToRequest: // nil: RequestOf takes a non-nil one
		return closuredef.MoveToRequest{}, wire.ErrClosureControlMalformed
	}
	return closuredef.MoveToRequest{}, fmt.Errorf(
		"closure: MoveTo expected closurecontrol.MoveToRequest or map[uint8]any, got %T", fields,
	)
}

// MoveTo field tags (closure-control.element.ts:79-81).
const (
	moveToFieldPosition uint8 = 0
	moveToFieldLatch    uint8 = 1
	moveToFieldSpeed    uint8 = 2
)

// moveToRequestFromTagMap reads the three "O.a+" MoveTo fields out of the
// generic tag map. Unsigned TLV integers surface as uint64 and an explicit
// null keeps its tag with a nil value (decodeGenericTagMap), which counts
// as absent here — the same reading the generated decoder gives a
// null element. A map carrying none of the three fields is malformed for
// the same reason the TLV decoder says so: at least one is mandatory.
func moveToRequestFromTagMap(m map[uint8]any) (closuredef.MoveToRequest, error) {
	var req closuredef.MoveToRequest
	if raw, present := m[moveToFieldPosition]; present && raw != nil {
		// TargetPositionEnum is enum8: a wider value is a malformed field,
		// not a large position.
		n, ok := raw.(uint64)
		if !ok || n > math.MaxUint8 {
			return closuredef.MoveToRequest{}, wire.ErrClosureControlMalformed
		}
		pos := closuredef.TargetPositionEnum(n)
		req.Position = &pos
	}
	if raw, present := m[moveToFieldLatch]; present && raw != nil {
		latch, ok := raw.(bool)
		if !ok {
			return closuredef.MoveToRequest{}, wire.ErrClosureControlMalformed
		}
		req.Latch = &latch
	}
	if raw, present := m[moveToFieldSpeed]; present && raw != nil {
		// ThreeLevelAutoEnum is enum8; same reasoning as Position.
		n, ok := raw.(uint64)
		if !ok || n > math.MaxUint8 {
			return closuredef.MoveToRequest{}, wire.ErrClosureControlMalformed
		}
		speed := closuredef.ThreeLevelAutoEnum(n)
		req.Speed = &speed
	}
	if req.Position == nil && req.Latch == nil && req.Speed == nil {
		return closuredef.MoveToRequest{}, wire.ErrClosureControlMalformed
	}
	return req, nil
}

// MatterReportable lists the served attributes (none is fixed). The
// dispatcher reports CurrentErrorList like the others now: it moves with
// [ControlServer.SetErrorList] and [ControlServer.ReportError].
func (s *ControlServer) MatterReportable() []uint32 { return s.inst.MatterReportable() }

// MatterAttributes implements [contract.ClusterAttributeLister]: the
// attributes the derived feature set makes mandatory. The dispatcher adds
// the globals, FeatureMap and ClusterRevision among them, to AttributeList
// (Core §7.13; matter.js lists them as every cluster's), and answers a
// write to one with UNSUPPORTED_WRITE before it reaches the server.
func (s *ControlServer) MatterAttributes() []uint32 { return s.inst.MatterAttributes() }

// FeatureMap reports the advertised (derived) feature set.
func (s *ControlServer) FeatureMap() uint32 { return s.inst.FeatureMap() }

// SetCurrentPosition records a position the device reported.
//
// pos nil means the drive is somewhere between its named stops — mid
// travel, most often — which the spec expresses as a null Position rather
// than a nearest-stop guess.
//
// A change of SecureState — true exactly at FullyClosed, and latched
// where the closure advertises MotionLatching — emits SecureStateChanged.
func (s *ControlServer) SetCurrentPosition(pos *wire.ClosureCurrentPosition) {
	s.setCurrent(currentReport{position: pos, setPosition: true})
}

// SetCurrentLatch records the latch the device reported (MotionLatching):
// nil is unknown, the null of the quality-X field. Reaching the latch a
// MoveTo asked for clears OverallTargetState's Latch, as an arrival
// clears its Position; SecureState follows ([ControlServer.SetCurrentPosition]).
// Without MotionLatching it changes nothing.
func (s *ControlServer) SetCurrentLatch(latch *bool) {
	if !s.has(wire.ClosureControlFeatureMotionLatching) {
		return
	}
	s.setCurrent(currentReport{latch: latch, setLatch: true})
}

// SetCurrentSpeed records the speed the device reported (Speed). Without
// Speed, or with a value outside the enum, it changes nothing (chip
// refuses an unknown speed, ClosureControlCluster.cpp:357-359 at 6170af84).
func (s *ControlServer) SetCurrentSpeed(speed closuredef.ThreeLevelAutoEnum) {
	if !s.has(wire.ClosureControlFeatureSpeed) || speed > closuredef.ThreeLevelAutoHigh {
		return
	}
	s.setCurrent(currentReport{speed: &speed})
}

// currentReport is one report of OverallCurrentState's fields; a field
// whose set flag (or pointer, for speed) is unset stays as it is.
type currentReport struct {
	position              *wire.ClosureCurrentPosition
	setPosition, setLatch bool
	latch                 *bool
	speed                 *closuredef.ThreeLevelAutoEnum
}

// setCurrent applies r, derives SecureState and clears what an arrival
// fulfils in OverallTargetState.
func (s *ControlServer) setCurrent(r currentReport) {
	var events []pendingEvent
	defer func() { s.emit(events) }()
	defer s.NotifyFrom(contract.OriginHostSet, wire.ClosureControlAttrOverallCurrentState, wire.ClosureControlAttrOverallTargetState)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.currentReports++
	prevSecure := s.secureState
	if r.setPosition {
		s.currentPosition = r.position
	}
	if r.setLatch {
		s.currentLatch = copyOf(r.latch)
	}
	if r.speed != nil {
		s.currentSpeed = copyOf(r.speed)
	}
	s.secureState = s.secureLocked()
	if s.secureState != nil && (prevSecure == nil || *prevSecure != *s.secureState) {
		events = append(events, pendingEvent{
			closuredef.EventSecureStateChanged,
			SecureStateChangedEvent{SecureValue: *s.secureState},
			s.inst.EventPriority(closuredef.EventSecureStateChanged),
		})
	}
	// A null Position means the drive's position is unknown, which the
	// spec says plainly (matter.js closure-control.resource.ts:429-439:
	// "If the closure doesn't know accurately its current state the value
	// null shall be used"). Whether it is MOVING is a different question
	// and a different attribute, answered by the model from the drive's
	// own motion signal and delivered through [ControlServer.SetMainState]
	// — reading it off the position made a stuck or unreferenced door
	// report as perpetually in motion.
	//
	// The drive arrived at the target, so nothing is outstanding; a
	// position on the way (PartiallyOpened while moving to FullyOpen) keeps
	// it (W3-club1-05). The latch alike.
	s.clearFulfilledTargetsLocked(r.setPosition, r.setLatch)
}

// clearFulfilledTargetsLocked clears the position target the current
// position has reached (position) and the latch target the current latch
// equals (latch). Caller holds s.mu.
func (s *ControlServer) clearFulfilledTargetsLocked(position, latch bool) {
	if p := s.currentPosition; position && p != nil && s.targetPosition != nil && reachedTarget(*p, *s.targetPosition) {
		s.targetPosition = nil
	}
	if l := s.currentLatch; latch && l != nil && s.targetLatch != nil && *l == *s.targetLatch {
		s.targetLatch = nil
	}
}

// currentStateLocked is OverallCurrentState as MatterRead answers it: a
// fresh struct per read whose fields copy the state, so a reader cannot
// reach the server's state through it. The bridge compares two readings
// by value, not by the struct's address. Caller holds s.mu.
func (s *ControlServer) currentStateLocked() *wire.ClosureOverallCurrentState {
	return &wire.ClosureOverallCurrentState{
		Position: copyOf(s.currentPosition), SecureState: copyOf(s.secureState),
		Latch: copyOf(s.currentLatch), HasLatch: s.has(wire.ClosureControlFeatureMotionLatching),
		Speed: copyOf(s.currentSpeed),
	}
}

// targetStateLocked is OverallTargetState as MatterRead answers it, built
// like [ControlServer.currentStateLocked]. Caller holds s.mu.
func (s *ControlServer) targetStateLocked() *wire.ClosureOverallTargetState {
	return &wire.ClosureOverallTargetState{
		Position: copyOf(s.targetPosition), Latch: copyOf(s.targetLatch),
		HasLatch: s.has(wire.ClosureControlFeatureMotionLatching), Speed: copyOf(s.targetSpeed),
	}
}

// secureLocked derives SecureState, "the closure is in a position that
// secures the opening" (matter.js closure-control.element.ts:134-137):
// unknown while the position is, false away from FullyClosed, and at
// FullyClosed true — where the closure advertises MotionLatching, only
// latched, unknown with an unknown latch. connectedhomeip refuses a true
// SecureState otherwise (ClosureControlCluster.cpp:364-386 at 6170af84).
func (s *ControlServer) secureLocked() *bool {
	if s.currentPosition == nil {
		return nil
	}
	secure := *s.currentPosition == wire.ClosureCurrentPositionFullyClosed
	if secure && s.has(wire.ClosureControlFeatureMotionLatching) {
		if s.currentLatch == nil {
			return nil
		}
		secure = *s.currentLatch
	}
	return &secure
}

// reachedTarget reports whether a CurrentPositionEnum is the position a
// TargetPositionEnum moves to (closure-control.element.ts CurrentPositionEnum
// and TargetPositionEnum at 862ad75f).
func reachedTarget(pos wire.ClosureCurrentPosition, target wire.ClosureTargetPosition) bool {
	switch target {
	case wire.ClosureTargetPositionMoveToFullyClosed:
		return pos == wire.ClosureCurrentPositionFullyClosed
	case wire.ClosureTargetPositionMoveToFullyOpen:
		return pos == wire.ClosureCurrentPositionFullyOpened
	case wire.ClosureTargetPositionMoveToPedestrianPosition:
		return pos == wire.ClosureCurrentPositionOpenedForPedestrian
	case wire.ClosureTargetPositionMoveToVentilationPosition:
		return pos == wire.ClosureCurrentPositionOpenedForVentilation
	case wire.ClosureTargetPositionMoveToSignaturePosition:
		return pos == wire.ClosureCurrentPositionOpenedAtSignature
	}
	return false
}

// SetMainState overrides the operational state. A motion that ends —
// Moving to Stopped — emits MovementCompleted (conformance !IS).
func (s *ControlServer) SetMainState(state wire.ClosureMainState) {
	s.mu.Lock()
	prev := s.mainState
	s.mainState = state
	s.stateReports++
	s.mu.Unlock()
	if prev != state {
		s.NotifyFrom(contract.OriginHostSet, wire.ClosureControlAttrMainState)
	}
	if prev == wire.ClosureMainStateMoving && state == wire.ClosureMainStateStopped && s.inst.Emits(closuredef.EventMovementCompleted) {
		s.emit([]pendingEvent{{closuredef.EventMovementCompleted, MovementCompletedEvent{}, s.inst.EventPriority(closuredef.EventMovementCompleted)}})
	}
}

// SetErrorList replaces CurrentErrorList, truncating to the spec's
// "max 10[all]" constraint.
func (s *ControlServer) SetErrorList(list wire.ClosureErrorList) {
	if len(list) > wire.ClosureErrorListMax {
		list = list[:wire.ClosureErrorListMax]
	}
	s.mu.Lock()
	s.errorList = make(wire.ClosureErrorList, len(list))
	copy(s.errorList, list)
	s.mu.Unlock()
	s.NotifyFrom(contract.OriginHostSet, wire.ClosureControlAttrCurrentErrorList)
}

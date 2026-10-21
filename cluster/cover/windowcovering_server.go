// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package cover contains the Matter WindowCovering cluster server (0x0102)
// for the position-aware lift and tilt feature profiles.
//
// The server holds its own state. Without a host port it answers every
// command without forwarding it anywhere; with [Config.MoveStep] it
// simulates the lift's travel the way matter.js's CHIP test node does —
// the reference daemon (examples/reference-bridge) mounts it that way, to
// put the surface in front of chip-tool. A host that drives a device sets
// [Config.Mover]: each command's target reaches the host, and the device's
// position and movement come back through
// [WindowCoveringServer.SetCurrentPosition] and
// [WindowCoveringServer.SetOperationalStatus].
//
// The cluster's identity is the generated definition (cluster/spec/
// windowcovering, ADR 0013): ids, revision, the attribute and command
// lists, the write statuses and checks and the privileges. The FeatureMap
// names the axes the server serves — Lift with PositionAwareLift
// ([ServedFeatureMap], the default), Tilt with PositionAwareTilt
// ([ServedTiltFeatureMap]), or both ([ServedLiftTiltFeatureMap]) — and is
// not taken on trust: any other host FeatureMap is refused at
// construction. GoToLiftPercentage and GoToTiltPercentage decode through
// the definition; the position arithmetic and the travel are the server's.
package cover

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	wcdef "github.com/SukramJ/go-fabric/cluster/spec/windowcovering"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/im"
)

// ClusterRevision is the WindowCovering cluster revision of the generated
// definition (window-covering-cluster.element.ts).
const ClusterRevision = wcdef.Revision

// ServedFeatureMap is the default FeatureMap the server serves: Lift and
// PositionAwareLift (0x05). Its attributes are exactly those the
// selection makes mandatory — TargetPositionLiftPercent100ths and
// CurrentPositionLiftPercent100ths are "LF & PA_LF" — plus the optional
// CurrentPositionLiftPercentage ("[LF & PA_LF]") and SafetyStatus ("O").
// Lift without PositionAwareLift would disallow the two position
// attributes the server serves, so an axis is always position-aware here.
const ServedFeatureMap = uint32(wcdef.FeatureLift | wcdef.FeaturePositionAwareLift)

// ServedTiltFeatureMap is the tilt-only FeatureMap: Tilt and
// PositionAwareTilt (0x12), with TargetPositionTiltPercent100ths and
// CurrentPositionTiltPercent100ths ("TL & PA_TL"), the optional
// CurrentPositionTiltPercentage and GoToTiltPercentage.
const ServedTiltFeatureMap = uint32(wcdef.FeatureTilt | wcdef.FeaturePositionAwareTilt)

// ServedLiftTiltFeatureMap serves both axes (0x17): a lift+tilt blind
// (Type TiltBlindLift, 8).
const ServedLiftTiltFeatureMap = ServedFeatureMap | ServedTiltFeatureMap

// ErrFeatureMap reports a Config.FeatureMap other than [ServedFeatureMap],
// [ServedTiltFeatureMap] or [ServedLiftTiltFeatureMap].
var ErrFeatureMap = errors.New("cover: FeatureMap names features this server does not serve")

// ErrMoverWithMoveStep reports a [Config] that sets both a Mover and a
// MoveStep: the device moves, the server does not simulate it.
var ErrMoverWithMoveStep = errors.New("cover: Config.Mover and Config.MoveStep exclude each other")

// ErrEnumValue reports a Config.Type or Config.EndProductType the
// FeatureMap's conformance disallows: a shutter (Shutter, SwingingShutter:
// "LF & !TL | !LF & TL") that lifts and tilts, a roller shade ("LF & !TL")
// on a covering that tilts too.
var ErrEnumValue = errors.New("cover: Type or EndProductType not allowed under the FeatureMap")

// Mover is the optional host port: the device that moves. It is the Go
// form of a matter.js host overriding handleMovement / handleStopMovement
// (packages/node/src/behaviors/window-covering/WindowCoveringServer.ts:338,
// :487 at matter.js 0ceb202d; "do not invoke super.handleMovement()",
// :328): the server validates the command, infers the target (UpOrOpen
// open, DownOrClose closed — on every served axis, :521-535, :545-559 —
// GoToLiftPercentage / GoToTiltPercentage the percentage on its axis,
// :576-592, :598-614), stores the target attributes and calls the Mover —
// one call per command, both axes in it where matter.js triggers two
// movements — instead of moving the covering itself.
//
// An error refuses the command and restores the previous target: an
// error that carries an IM status ([im.StatusCodeError]) answers with it,
// any other answers FAILURE — the rule thermo.Sink follows too.
// The Mover runs inside the command: the command's answer waits for it,
// so it returns once the device accepted the target, not once it arrived
// (matter.js runs handleMovement as a worker and answers first,
// :378-392). The device's position and movement come back through
// [WindowCoveringServer.SetCurrentPosition] and
// [WindowCoveringServer.SetOperationalStatus], which may be called from
// inside the Mover. Debouncing is the host's.
type Mover interface {
	// GoTo moves to the targets, in percent100ths (0 open, 10000 closed).
	// lift is the lift target and tilt the tilt target; nil leaves that
	// axis untouched. An axis the server does not serve is always nil;
	// UpOrOpen and DownOrClose carry every served axis, GoToLiftPercentage
	// only lift, GoToTiltPercentage only tilt.
	GoTo(ctx context.Context, lift, tilt *uint16) error
	// Stop stops every movement (StopMotion, matter.js handleStopMovement
	// :487-504). The server has set each served target to its current
	// position.
	Stop(ctx context.Context) error
}

// servedOptional returns the optional attributes the server serves under
// features: the deprecated percentage of each served axis and SafetyStatus.
func servedOptional(features uint32) []uint32 {
	out := []uint32{wcdef.AttrSafetyStatus}
	if features&uint32(wcdef.FeatureLift) != 0 {
		out = append(out, wcdef.AttrCurrentPositionLiftPercentage)
	}
	if features&uint32(wcdef.FeatureTilt) != 0 {
		out = append(out, wcdef.AttrCurrentPositionTiltPercentage)
	}
	return out
}

// goToPercentageFieldValue is the LiftPercent100thsValue /
// TiltPercent100thsValue context tag (window-covering-cluster.element.ts:95,
// :99, id 0x0).
const goToPercentageFieldValue uint8 = 0

// Config carries the initial state and type-specific attributes for a
// [WindowCoveringServer].
type Config struct {
	// Type is the Matter WindowCovering Type attribute value (enum8).
	Type uint8
	// EndProductType is the Matter WindowCovering EndProductType
	// attribute value (enum8).
	EndProductType uint8
	// FeatureMap is the Matter FeatureMap: [ServedFeatureMap] (zero
	// selects it), [ServedTiltFeatureMap] or [ServedLiftTiltFeatureMap];
	// any other value is refused by [New].
	FeatureMap uint32
	// InitialPositionPercent100ths is the starting value for
	// CurrentPositionLiftPercent100ths and
	// TargetPositionLiftPercent100ths (0 = fully open, 10000 = fully
	// closed, per Matter §5.3.6.11). Ignored without the Lift feature.
	InitialPositionPercent100ths uint16
	// InitialTiltPositionPercent100ths is the starting value for
	// CurrentPositionTiltPercent100ths and
	// TargetPositionTiltPercent100ths. Ignored without the Tilt feature.
	InitialTiltPositionPercent100ths uint16
	// MoveStep makes a movement travel: when non-zero a command moves the
	// axis in six steps of MoveStep, reporting OperationalStatus Opening
	// or Closing until the target is reached, as matter.js's CHIP test
	// node does (support/chip-testing/src/cluster/
	// TestWindowCoveringServer.ts, six 950 ms steps). Zero moves at once,
	// as matter.js's WindowCoveringServer does without a handleMovement of
	// its own. [New] refuses a MoveStep together with a Mover.
	MoveStep time.Duration
	// Mover is the optional host port; nil keeps the server
	// self-contained.
	Mover Mover
	// Clock paces a travelling movement's steps (MoveStep); nil is
	// [clock.System].
	Clock clock.Clock
	// Logger receives the server's diagnostics; nil is slog.Default().
	Logger *slog.Logger
}

// moveSteps is the number of steps a travelling movement takes
// (TestWindowCoveringServer.ts #handleMovementTick: counter >= 6).
const moveSteps = 6

// WindowCoveringServer implements
// [github.com/SukramJ/go-fabric/contract.ClusterServer]
// for the Matter WindowCovering cluster (0x0102), position-aware lift
// and/or tilt feature profile. Without a [Mover] the state is
// self-contained: commands update internal fields and return Success, so
// that commissioning tools and controller apps receive a well-formed
// cluster without a device behind it. With a Mover the commands reach the
// device.
type WindowCoveringServer struct {
	// log is the server's logger ([Config.Logger]; slog.Default() when nil).
	log *slog.Logger
	// op serialises the commands while a Mover runs, so a command
	// restoring its target never races another; mu guards the values and
	// is not held while the Mover runs.
	op sync.Mutex
	mu sync.RWMutex

	mover Mover

	inst           *spec.Instance
	features       uint32
	wcType         uint8
	endProductType uint8
	wcMode         uint8 // Mode attribute (0x0017), RW VM, constraint max 15.

	// lift and tilt are the served axes; an unserved axis is nil.
	lift, tilt *axis

	operationalStatus uint8

	moveStep time.Duration
	// clock paces a travelling movement's steps.
	clock clock.Clock
	// changes reports what a travelling movement changes between
	// requests (contract.AttributeChangeNotifier).
	changes cluster.AttributeChanges
}

// axis is one position-aware axis of the covering: its positions, its
// attribute ids and its running travel.
type axis struct {
	current, target uint16
	// move is the running movement; nil when the axis stands still.
	move *movement

	attrCurrent, attrPercentage, attrTarget uint32
	// shift is the axis's position in OperationalStatus: lift bits 2-3,
	// tilt bits 4-5 (window-covering-cluster.element.ts:130-131).
	shift uint8
}

// movement is one travelling axis movement.
type movement struct {
	timer     clock.Timer
	increment int
	target    uint16
	counter   int
}

// OperationalStatus MovementStatus values (window-covering-cluster
// element: Stopped 0, Opening 1, Closing 2) and the bitmap layout: global
// in bits 0-1, lift in bits 2-3, tilt in bits 4-5.
const (
	movementStopped uint8 = 0
	movementOpening uint8 = 1
	movementClosing uint8 = 2

	liftShift uint8 = 2
	tiltShift uint8 = 4
)

// globalStatus is the OperationalStatus bitmap for the given lift and
// tilt movement: global tracks lift while it moves, otherwise tilt
// (matter.js #handleOperationalStatusChanging,
// WindowCoveringServer.ts:203-214 at 0ceb202d).
func globalStatus(lift, tilt uint8) uint8 {
	global := tilt
	if lift != movementStopped {
		global = lift
	}
	return global | lift<<liftShift | tilt<<tiltShift
}

// axisStatus is the movement of the axis in the stored OperationalStatus.
func (s *WindowCoveringServer) axisStatusLocked(a *axis) uint8 {
	if a == nil {
		return movementStopped
	}
	return (s.operationalStatus >> a.shift) & 0b11
}

// setAxisStatusLocked stores st for the axis and recomputes global.
// Caller holds s.mu.
func (s *WindowCoveringServer) setAxisStatusLocked(a *axis, st uint8) {
	lift, tilt := s.axisStatusLocked(s.lift), s.axisStatusLocked(s.tilt)
	switch a.shift {
	case liftShift:
		lift = st
	case tiltShift:
		tilt = st
	}
	s.operationalStatus = globalStatus(lift, tilt)
}

// OnMatterAttributesChanged implements [contract.AttributeChangeNotifier].
func (s *WindowCoveringServer) OnMatterAttributesChanged(cb func(attrIDs []uint32)) (unsubscribe func()) {
	return s.changes.OnMatterAttributesChanged(cb)
}

// New constructs a [WindowCoveringServer] from cfg. A cfg.FeatureMap other
// than zero, [ServedFeatureMap], [ServedTiltFeatureMap] and
// [ServedLiftTiltFeatureMap] is refused with [ErrFeatureMap]: advertising
// it would list attributes or commands the server does not serve. A Mover
// together with a non-zero MoveStep is refused with [ErrMoverWithMoveStep],
// a Type or EndProductType the FeatureMap disallows with [ErrEnumValue].
func New(cfg Config) (*WindowCoveringServer, error) {
	features := cfg.FeatureMap
	if features == 0 {
		features = ServedFeatureMap
	}
	switch features {
	case ServedFeatureMap, ServedTiltFeatureMap, ServedLiftTiltFeatureMap:
	default:
		return nil, fmt.Errorf("%w: 0x%X, the server serves 0x%X, 0x%X or 0x%X", ErrFeatureMap, cfg.FeatureMap, ServedFeatureMap, ServedTiltFeatureMap, ServedLiftTiltFeatureMap)
	}
	if cfg.Mover != nil && cfg.MoveStep != 0 {
		return nil, fmt.Errorf("%w: MoveStep %s", ErrMoverWithMoveStep, cfg.MoveStep)
	}
	// Each served selection satisfies "O.a+" and "[LF]" / "[TL]", and its
	// optional attributes are allowed under it, so spec.New cannot refuse it.
	inst, _ := spec.New(wcdef.Definition, spec.Options{Features: features, Attributes: servedOptional(features)})
	// The initial state conforms to the schema or the server is not built:
	// a TypeEnum or EndProductTypeEnum member whose conformance the features
	// disallow is refused (window-covering-cluster.element.ts:150-192).
	// Mirrors matter.js packages/node/src/behavior/internal/ServerBehaviorBacking.ts:60-63
	// (invokeInitializer validates the behavior's state) and
	// packages/node/src/behavior/state/validation/conformance-compiler.ts:735-747
	// (disallowEnumValue throws EnumValueConformanceError).
	for _, v := range []struct {
		attr  uint32
		value uint8
	}{{wcdef.AttrType, cfg.Type}, {wcdef.AttrEndProductType, cfg.EndProductType}} {
		if _, err := inst.CheckValue(v.attr, uint64(v.value), nil); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrEnumValue, err)
		}
	}
	s := &WindowCoveringServer{
		log:            cmp.Or(cfg.Logger, slog.Default()),
		inst:           inst,
		features:       features,
		wcType:         cfg.Type,
		endProductType: cfg.EndProductType,
		moveStep:       cfg.MoveStep,
		clock:          clock.Or(cfg.Clock),
		mover:          cfg.Mover,
	}
	if features&uint32(wcdef.FeatureLift) != 0 {
		s.lift = &axis{
			current: cfg.InitialPositionPercent100ths, target: cfg.InitialPositionPercent100ths,
			attrCurrent: wcdef.AttrCurrentPositionLiftPercent100ths, attrPercentage: wcdef.AttrCurrentPositionLiftPercentage,
			attrTarget: wcdef.AttrTargetPositionLiftPercent100ths, shift: liftShift,
		}
	}
	if features&uint32(wcdef.FeatureTilt) != 0 {
		s.tilt = &axis{
			current: cfg.InitialTiltPositionPercent100ths, target: cfg.InitialTiltPositionPercent100ths,
			attrCurrent: wcdef.AttrCurrentPositionTiltPercent100ths, attrPercentage: wcdef.AttrCurrentPositionTiltPercentage,
			attrTarget: wcdef.AttrTargetPositionTiltPercent100ths, shift: tiltShift,
		}
	}
	return s, nil
}

// NewWindowCoveringServer constructs a [WindowCoveringServer] from cfg, as
// [New] does; it panics on a cfg.FeatureMap [New] refuses — a wiring
// error, not a runtime condition.
func NewWindowCoveringServer(cfg Config) *WindowCoveringServer {
	s, err := New(cfg)
	if err != nil {
		panic(err)
	}
	return s
}

// MatterClusterID returns the WindowCovering cluster ID (0x0102).
func (s *WindowCoveringServer) MatterClusterID() uint32 { return wcdef.ClusterID }

// MatterRead resolves the served WindowCovering attributes and the two
// globals. The dispatcher synthesises only the list-valued globals; a
// server that leaves FeatureMap and ClusterRevision to it answers
// UnsupportedAttribute on every wildcard read.
func (s *WindowCoveringServer) MatterRead(attrID uint32) (any, bool) {
	if v, ok := s.inst.ReadGlobal(attrID); ok {
		return v, true
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	switch attrID {
	case wire.WindowCoveringAttrType:
		return s.wcType, true
	case wire.WindowCoveringAttrConfigStatus:
		return s.configStatusLocked(), true
	case wire.WindowCoveringAttrOperationalStatus:
		return s.operationalStatus, true
	case wire.WindowCoveringAttrEndProductType:
		return s.endProductType, true
	case wire.WindowCoveringAttrMode:
		return s.wcMode, true
	case wire.WindowCoveringAttrSafetyStatus:
		return uint16(0), true
	}
	for _, a := range []*axis{s.lift, s.tilt} {
		if a == nil {
			continue
		}
		switch attrID {
		case a.attrPercentage:
			// Deprecated uint8 field; the scaled-down value for Matter 1.0
			// controllers (matter.js #syncLiftCurrentPositions /
			// #syncTiltCurrentPositions, WindowCoveringServer.ts:239-240,
			// :257-258 at 0ceb202d).
			return uint8((a.current / 100) & 0xFF), true // value bounded 0-100 by design
		case a.attrTarget:
			return a.target, true
		case a.attrCurrent:
			return a.current, true
		}
	}
	return nil, false
}

// ConfigStatusBitmap bit positions, from the datatype's per-field
// constraint in matter.js
// packages/model/src/standard/elements/window-covering-cluster.element.ts:109-116.
// The bits are read from the element file, not counted from the field
// order: OnlineReserved (bit 1, conformance D) and LiftMovementReversed
// (bit 2) sit between the two this server sets.
//
// matter.js WindowCoveringServer.ts:121-132 initialize() sets Operational
// and, for a position-aware lift or tilt, LiftPositionAware /
// TiltPositionAware; Operational and LiftMovementReversed follow the Mode
// attribute (#handleModeChanging, :185-190).
const (
	configStatusOperational          uint8 = 1 << 0 // constraint "0", element :110
	configStatusLiftMovementReversed uint8 = 1 << 2 // constraint "2", element :112
	configStatusLiftPositionAware    uint8 = 1 << 3 // constraint "3", element :113
	configStatusTiltPositionAware    uint8 = 1 << 4 // constraint "4", element :114
)

// configStatusLocked derives ConfigStatus from the served axes and Mode:
// Operational unless MaintenanceMode is set, LiftMovementReversed with a
// lift that has MotorDirectionReversed set. Mirrors matter.js
// WindowCoveringServer.ts:121-132 (initialize) and :185-190
// (#handleModeChanging: `operational = !maintenanceMode || (calibrationMode
// && !supportsCalibration)` — a calibration request is refused here, so
// only the first term remains). Caller holds s.mu.
func (s *WindowCoveringServer) configStatusLocked() uint8 {
	var st uint8
	if s.wcMode&uint8(wcdef.ModeMaintenanceMode) == 0 {
		st |= configStatusOperational
	}
	if s.lift != nil {
		st |= configStatusLiftPositionAware
		if s.wcMode&uint8(wcdef.ModeMotorDirectionReversed) != 0 {
			st |= configStatusLiftMovementReversed
		}
	}
	if s.tilt != nil {
		st |= configStatusTiltPositionAware
	}
	return st
}

// windowCoveringConstraintErr is a typed [im.StatusCodeError] for
// writes that violate the "max 10000" constraint on percent100ths attributes.
// Mirrors matter.js window-covering-cluster.element.ts:72,76 constraint.
type windowCoveringConstraintErr struct{ msg string }

func (e windowCoveringConstraintErr) Error() string                 { return e.msg }
func (windowCoveringConstraintErr) MatterStatusCode() im.StatusCode { return im.StatusConstraintError }

// Compile-time assertion.
var _ im.StatusCodeError = windowCoveringConstraintErr{}

// MatterWrite accepts Mode (0x0017), the one writable attribute ("RW VM");
// all others are controlled via commands. The definition checks the write
// (matter.js AttributeWriteResponse + ValueValidator): a served read-only
// attribute is UNSUPPORTED_WRITE, an unserved one UNSUPPORTED_ATTRIBUTE,
// a Mode outside "max 15" — the four ModeBitmap bits — CONSTRAINT_ERROR.
//
// A Mode that passes is synced as matter.js #handleModeChanging does
// (WindowCoveringServer.ts:162-200 at 0ceb202d): MaintenanceMode takes
// priority over CalibrationMode, which is cleared beside it; a
// CalibrationMode left set is CONSTRAINT_ERROR, as matter.js answers
// without supportsCalibration (its default, :73, :620) — the server has no
// calibration procedure; and ConfigStatus follows ([configStatusLocked]),
// reported when it moved. In MaintenanceMode the movement commands answer
// BUSY ([WindowCoveringServer.MatterInvoke]).
func (s *WindowCoveringServer) MatterWrite(_ context.Context, attrID uint32, value any) error {
	v, err := s.inst.ValidateWrite(attrID, value, nil)
	if err != nil {
		if attrID == wcdef.AttrMode {
			return windowCoveringConstraintErr{fmt.Sprintf("cover: Mode %v exceeds constraint max 15: %v", value, err)}
		}
		return err
	}
	n, _ := v.(uint64) // a map8 ≤ 15 per ValidateWrite
	mode := uint8(n)   //nolint:gosec // ≤ 15 per ValidateWrite
	maintenance, calibration := uint8(wcdef.ModeMaintenanceMode), uint8(wcdef.ModeCalibrationMode)
	if mode&maintenance != 0 {
		mode &^= calibration
	}
	if mode&calibration != 0 {
		return windowCoveringConstraintErr{"cover: Mode CalibrationMode: calibration not supported"}
	}
	s.mu.Lock()
	before := s.configStatusLocked()
	s.wcMode = mode
	moved := s.configStatusLocked() != before
	s.mu.Unlock()
	if moved {
		s.changes.Notify(wire.WindowCoveringAttrConfigStatus)
	}
	return nil
}

// maintenanceErr answers a movement command in MaintenanceMode.
type maintenanceErr struct{}

func (maintenanceErr) Error() string                   { return "cover: device is in maintenance mode" }
func (maintenanceErr) MatterStatusCode() im.StatusCode { return im.StatusBusy }

// MinWritePrivilege implements [contract.ClusterAttributeWritePrivilege]:
// Mode is "RW VM".
func (s *WindowCoveringServer) MinWritePrivilege(attrID uint32) uint8 {
	return s.inst.MinWritePrivilege(attrID)
}

// command is a validated movement command: the target of each served
// axis it moves (nil = axis untouched), or a stop.
type command struct {
	lift, tilt *uint16
	stop       bool
}

// parseCommand validates cmdID and fields and infers the targets:
// UpOrOpen 0 and DownOrClose 10000 on every served axis
// (WindowCoveringServer.ts:521-535, :545-559 at matter.js 0ceb202d),
// GoToLiftPercentage / GoToTiltPercentage the value on its axis
// (:576-584, :598-606), StopMotion a stop (:566-570). A GoTo command of
// an unserved axis is not in AcceptedCommandList and is refused here too.
func (s *WindowCoveringServer) parseCommand(cmdID uint32, fields any) (command, error) {
	both := func(v uint16) command {
		var c command
		if s.lift != nil {
			c.lift = &v
		}
		if s.tilt != nil {
			t := v
			c.tilt = &t
		}
		return c
	}
	switch cmdID {
	case wcdef.CmdUpOrOpen:
		return both(0), nil
	case wire.WindowCoveringCmdDownOrClose:
		return both(10000), nil
	case wire.WindowCoveringCmdStopMotion:
		return command{stop: true}, nil
	case wire.WindowCoveringCmdGoToLiftPercentage:
		if s.lift == nil {
			return command{}, spec.Errorf(im.StatusUnsupportedCommand, "cover: GoToLiftPercentage without the Lift feature")
		}
		pct, err := extractPercent100ths(fields, "GoToLiftPercentage")
		if err != nil {
			return command{}, err
		}
		return command{lift: &pct}, nil
	case wire.WindowCoveringCmdGoToTiltPercentage:
		if s.tilt == nil {
			return command{}, spec.Errorf(im.StatusUnsupportedCommand, "cover: GoToTiltPercentage without the Tilt feature")
		}
		pct, err := extractPercent100ths(fields, "GoToTiltPercentage")
		if err != nil {
			return command{}, err
		}
		return command{tilt: &pct}, nil
	default:
		return command{}, fmt.Errorf("cover: unknown command 0x%02X", cmdID)
	}
}

// MatterInvoke handles the WindowCovering commands: UpOrOpen, DownOrClose,
// StopMotion, GoToLiftPercentage (Lift) and GoToTiltPercentage (Tilt).
// Without a [Mover] they accept the request and update internal state;
// they return Success without forwarding anywhere. With one, see
// [WindowCoveringServer.invokeMover].
func (s *WindowCoveringServer) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	c, err := s.parseCommand(cmdID, fields)
	if err != nil {
		return nil, err
	}
	// Every movement command, StopMotion included, answers BUSY in
	// MaintenanceMode. Mirrors matter.js WindowCoveringServer.ts:289-292
	// (#assertMotionLockStatus, called at :519, :543, :567, :577, :599).
	s.mu.RLock()
	inMaintenance := s.wcMode&uint8(wcdef.ModeMaintenanceMode) != 0
	s.mu.RUnlock()
	if inMaintenance {
		return nil, maintenanceErr{}
	}
	if s.mover != nil {
		return nil, s.invokeMover(ctx, cmdID, c)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if c.stop {
		// matter.js handleStopMovement (:487-504): each axis stops where
		// it is, and the target follows the position.
		for _, a := range []*axis{s.lift, s.tilt} {
			if a == nil {
				continue
			}
			s.stopLocked(a)
			a.target = a.current
			s.setAxisStatusLocked(a, movementStopped)
		}
		return nil, nil
	}
	if c.lift != nil {
		s.moveToLocked(s.lift, *c.lift)
	}
	if c.tilt != nil {
		s.moveToLocked(s.tilt, *c.tilt)
	}
	return nil, nil
}

// invokeMover is MatterInvoke with a [Mover]: the inferred targets are
// stored (StopMotion: each served axis's current position, :491-496) and
// the Mover called — GoTo with the targets, Stop for StopMotion. The
// server neither moves the covering nor changes OperationalStatus: both
// are the device's, reported through the inbound setters. A Mover error
// answers with its IM status, FAILURE without one, and restores the
// previous target of each axis the command moved — unless
// [WindowCoveringServer.SetTargetPosition] stored a newer one meanwhile,
// which then stands (the host's report wins the overlap, by_design
// BD-Matter-HostPort-OutsideTheSetterLock).
func (s *WindowCoveringServer) invokeMover(ctx context.Context, cmdID uint32, c command) error {
	s.op.Lock()
	defer s.op.Unlock()
	s.mu.Lock()
	if c.stop {
		if s.lift != nil {
			v := s.lift.current
			c.lift = &v
		}
		if s.tilt != nil {
			v := s.tilt.current
			c.tilt = &v
		}
	}
	var prevLift, prevTilt uint16
	if c.lift != nil {
		prevLift, s.lift.target = s.lift.target, *c.lift
	}
	if c.tilt != nil {
		prevTilt, s.tilt.target = s.tilt.target, *c.tilt
	}
	s.mu.Unlock()

	var err error
	if c.stop {
		err = s.mover.Stop(ctx)
	} else {
		err = s.mover.GoTo(ctx, c.lift, c.tilt)
	}
	if err == nil {
		return nil
	}
	s.mu.Lock()
	if c.lift != nil && s.lift.target == *c.lift {
		s.lift.target = prevLift
	}
	if c.tilt != nil && s.tilt.target == *c.tilt {
		s.tilt.target = prevTilt
	}
	s.mu.Unlock()
	var sc im.StatusCodeError
	if errors.As(err, &sc) {
		return err
	}
	return spec.Errorf(im.StatusFailure, "cover: command 0x%02X refused: %v", cmdID, err)
}

// SetCurrentPosition records the positions the device reports: lift is
// CurrentPositionLiftPercent100ths (and, scaled,
// CurrentPositionLiftPercentage), tilt CurrentPositionTiltPercent100ths
// (and CurrentPositionTiltPercentage); nil leaves an axis unchanged, and
// a value for an axis the server does not serve is ignored. A value above
// 10000 (the attribute's "max 10000") is clamped to it and logged. It is
// stored without the [Mover] and reported through the change notifier; it
// does not move the targets or OperationalStatus. Without a Mover it does
// not stop a simulated travel either.
func (s *WindowCoveringServer) SetCurrentPosition(lift, tilt *uint16) {
	s.setPositions(lift, tilt, "matter.windowcovering.inbound_position_clamped", func(a *axis, v uint16) (bool, []uint32) {
		changed := a.current != v
		a.current = v
		return changed, []uint32{a.attrCurrent, a.attrPercentage}
	})
}

// SetTargetPosition records a target the host infers from the device's
// own movement — a covering a wall button or a programme started, which
// matter.js has no command for: there every movement starts with a target
// write (WindowCoveringServer.ts:524, :548, :580, :602 at 0ceb202d) and a
// stop snaps the target to the position (:491-496). lift is
// TargetPositionLiftPercent100ths, tilt TargetPositionTiltPercent100ths;
// nil leaves an axis unchanged, an unserved axis is ignored, a value
// above 10000 is clamped and logged. It is stored without the [Mover] and
// reported through the change notifier; OperationalStatus stays the
// host's ([WindowCoveringServer.SetOperationalStatus]). A target that
// lands while a command's Mover runs is stored and reported at once; if
// that Mover then fails, the command does not restore its own target
// over it.
func (s *WindowCoveringServer) SetTargetPosition(lift, tilt *uint16) {
	s.setPositions(lift, tilt, "matter.windowcovering.inbound_target_clamped", func(a *axis, v uint16) (bool, []uint32) {
		changed := a.target != v
		a.target = v
		return changed, []uint32{a.attrTarget}
	})
}

// setPositions applies store to each served axis a value is given for,
// clamping above 10000, and reports what changed.
func (s *WindowCoveringServer) setPositions(lift, tilt *uint16, clampMsg string, store func(a *axis, v uint16) (bool, []uint32)) {
	var report []uint32
	s.mu.Lock()
	for _, in := range []struct {
		a *axis
		v *uint16
	}{{s.lift, lift}, {s.tilt, tilt}} {
		if in.a == nil || in.v == nil {
			continue
		}
		v := *in.v
		if v > 10000 {
			s.logger().Warn(clampMsg, slog.Int("value", int(v)), slog.Int("stored", 10000))
			v = 10000
		}
		if changed, attrs := store(in.a, v); changed {
			report = append(report, attrs...)
		}
	}
	s.mu.Unlock()
	if len(report) > 0 {
		s.changes.Notify(report...)
	}
}

// SetOperationalStatus records the OperationalStatus bitmap the device
// reports (global in bits 0-1, lift in bits 2-3, tilt in bits 4-5 —
// window-covering-cluster.element.ts:128-131 at matter.js 0ceb202d),
// stored without the [Mover] and reported through the change notifier.
// Bits above the attribute's "max 63" (:55-56) are dropped and logged.
func (s *WindowCoveringServer) SetOperationalStatus(status uint8) {
	if status > 63 {
		s.logger().Warn("matter.windowcovering.inbound_operational_status_masked",
			slog.Int("value", int(status)), slog.Int("stored", int(status&63)))
		status &= 63
	}
	s.mu.Lock()
	changed := s.operationalStatus != status
	s.operationalStatus = status
	s.mu.Unlock()
	if changed {
		s.changes.Notify(wire.WindowCoveringAttrOperationalStatus)
	}
}

// moveToLocked starts a movement of the axis to target. Caller holds
// s.mu. Without a MoveStep the axis arrives at once; with one it travels,
// as matter.js TestWindowCoveringServer.handleMovement does: the status
// says which way (#computeOperationalState, WindowCoveringServer.ts:273-283),
// and the position moves in six steps.
func (s *WindowCoveringServer) moveToLocked(a *axis, target uint16) {
	s.stopLocked(a)
	a.target = target
	cur := a.current
	if s.moveStep == 0 || cur == target {
		a.current = target
		s.setAxisStatusLocked(a, movementStopped)
		return
	}
	dir := movementOpening
	if target > cur {
		dir = movementClosing
	}
	s.setAxisStatusLocked(a, dir)
	diff := float64(int(target) - int(cur))
	inc := int(math.Floor(diff / moveSteps))
	if target < cur {
		inc = int(math.Ceil(diff / moveSteps))
	}
	m := &movement{increment: inc, target: target}
	m.timer = s.clock.AfterFunc(s.moveStep, func() { s.tick(a, m) })
	a.move = m
}

// stopLocked ends a running movement of the axis. Caller holds s.mu.
func (s *WindowCoveringServer) stopLocked(a *axis) {
	if a.move != nil {
		a.move.timer.Stop()
		a.move = nil
	}
}

// tick is one step of a travelling movement
// (TestWindowCoveringServer.ts #handleMovementTick).
func (s *WindowCoveringServer) tick(a *axis, m *movement) {
	s.mu.Lock()
	if a.move != m {
		s.mu.Unlock()
		return // stopped or replaced between the fire and the lock
	}
	changed := []uint32{a.attrCurrent, a.attrPercentage}
	m.counter++
	if m.counter >= moveSteps {
		a.current = m.target
		s.setAxisStatusLocked(a, movementStopped)
		a.move = nil
		changed = append(changed, wire.WindowCoveringAttrOperationalStatus)
	} else {
		a.current = uint16(int(a.current) + m.increment) //nolint:gosec // six increments of a 0-10000 span stay inside it
		m.timer.Reset(s.moveStep)
	}
	s.mu.Unlock()
	s.changes.Notify(changed...)
}

// MatterAcceptedCommands implements [contract.ClusterCommandLister]:
// UpOrOpen, DownOrClose and StopMotion (conformance M), GoToLiftPercentage
// ("LF & PA_LF, [LF]") and GoToTiltPercentage ("TL & PA_TL, [TL]") for
// the served axes. Without the lister the
// dispatcher synthesised an empty AcceptedCommandList — a covering that,
// read by the book, accepts no command at all. Found by the chip-tool
// data-model sweep.
func (s *WindowCoveringServer) MatterAcceptedCommands() []uint32 {
	return s.inst.MatterAcceptedCommands()
}

// MatterGeneratedCommands implements [contract.ClusterCommandLister]: every
// command answers with a status only.
func (s *WindowCoveringServer) MatterGeneratedCommands() []uint32 {
	return s.inst.MatterGeneratedCommands()
}

// MinInvokePrivilege implements [contract.ClusterCommandInvokePrivilege].
func (s *WindowCoveringServer) MinInvokePrivilege(cmdID uint32) uint8 {
	return s.inst.MinInvokePrivilege(cmdID)
}

// MatterReportable lists the served attributes that move: all but the
// fixed Type and EndProductType.
func (s *WindowCoveringServer) MatterReportable() []uint32 { return s.inst.MatterReportable() }

// MatterAttributes implements [contract.ClusterAttributeLister]: the
// attributes the served features make mandatory and the optional ones
// served.
func (s *WindowCoveringServer) MatterAttributes() []uint32 { return s.inst.MatterAttributes() }

// extractPercent100ths pulls the LiftPercent100thsValue /
// TiltPercent100thsValue (field 0, window-covering-cluster.element.ts:95,
// :99) out of the GoToLiftPercentage / GoToTiltPercentage command fields.
// The bridge decodes the command through the generated definition, so a
// real invocation arrives as [wcdef.GoToLiftPercentageRequest] or
// [wcdef.GoToTiltPercentageRequest] (a missing field already answered
// INVALID_COMMAND there, a value above 10000 CONSTRAINT_ERROR, as
// matter.js's request schema answers them); a host that decodes the
// command itself may pass the wire request or the tag-keyed map of the
// bridge's generic salvage path (bridge/fields_reader.go
// decodeGenericTagMap, the value a uint64), and the bare uint16 and the
// "percent"-keyed map stay accepted for direct callers. Values > 10000
// are rejected with ConstraintError per
// window-covering-cluster.element.ts:72, :76 constraint "max 10000".
func extractPercent100ths(fields any, cmd string) (uint16, error) {
	var pct uint16
	switch v := fields.(type) {
	case uint16:
		pct = v
	case wcdef.GoToLiftPercentageRequest:
		pct = v.LiftPercent100thsValue
	case *wcdef.GoToLiftPercentageRequest:
		if v == nil {
			return 0, fmt.Errorf("cover: %s carried no fields", cmd)
		}
		pct = v.LiftPercent100thsValue
	case wire.GoToLiftPercentageRequest:
		pct = v.LiftPercent100thsValue
	case *wire.GoToLiftPercentageRequest:
		if v == nil {
			return 0, fmt.Errorf("cover: %s carried no fields", cmd)
		}
		pct = v.LiftPercent100thsValue
	case wcdef.GoToTiltPercentageRequest:
		pct = v.TiltPercent100thsValue
	case *wcdef.GoToTiltPercentageRequest:
		if v == nil {
			return 0, fmt.Errorf("cover: %s carried no fields", cmd)
		}
		pct = v.TiltPercent100thsValue
	case wire.GoToTiltPercentageRequest:
		pct = v.TiltPercent100thsValue
	case *wire.GoToTiltPercentageRequest:
		if v == nil {
			return 0, fmt.Errorf("cover: %s carried no fields", cmd)
		}
		pct = v.TiltPercent100thsValue
	case map[uint8]any:
		raw, ok := v[goToPercentageFieldValue]
		if !ok {
			return 0, fmt.Errorf("cover: %s missing Percent100thsValue (field 0)", cmd)
		}
		n, isUint := raw.(uint64)
		if !isUint || n > math.MaxUint16 {
			return 0, fmt.Errorf("cover: %s Percent100thsValue %v (%T) is not a uint16", cmd, raw, raw)
		}
		pct = uint16(n)
	case map[string]any:
		raw, ok := v["percent"]
		if !ok {
			return 0, fmt.Errorf("cover: %s missing percent field", cmd)
		}
		var castOK bool
		pct, castOK = raw.(uint16)
		if !castOK {
			return 0, fmt.Errorf("cover: %s percent expected uint16, got %T", cmd, raw)
		}
	default:
		return 0, fmt.Errorf("cover: %s expected map[uint8]any, a GoTo…PercentageRequest, uint16 or map[string]any, got %T", cmd, fields)
	}
	// Constraint max 10000 per matter.js window-covering-cluster.element.ts:72, :76.
	if pct > 10000 {
		return 0, windowCoveringConstraintErr{fmt.Sprintf("cover: %s value %d exceeds constraint max 10000", cmd, pct)}
	}
	return pct, nil
}

// logger is the WindowCoveringServer's logger; a value built without its constructor
// logs through slog.Default().
func (s *WindowCoveringServer) logger() *slog.Logger {
	if s.log != nil {
		return s.log
	}
	return slog.Default()
}

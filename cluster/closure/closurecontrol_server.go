// SPDX-License-Identifier: MIT
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
package closure

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
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

// servedFeatures are the features whose elements this server serves.
//
// Positioning is fixed: OverallCurrentState / OverallTargetState carry a
// Position and MoveTo moves to one. Ventilation and Pedestrian add only a
// TargetPositionEnum / CurrentPositionEnum value each, ManuallyOperable
// only EngageStateChanged (served by [ControlServer.SetEngaged]),
// Protection only MainState Protected — so a host may select them for a
// drive that has them (closure-control.element.ts, conformance of every
// element). The others are not served: MotionLatching needs
// LatchControlModes and the Latch fields, Speed the Speed fields,
// Calibration the Calibrate command, and Instantaneous withdraws Stop and
// MovementCompleted, which this server serves.
const servedFeatures = wire.ClosureControlFeaturePositioning |
	wire.ClosureControlFeatureVentilation |
	wire.ClosureControlFeaturePedestrian |
	wire.ClosureControlFeatureProtection |
	wire.ClosureControlFeatureManuallyOperable

// ErrFeatureMap reports a Config.FeatureMap the server does not serve.
var ErrFeatureMap = errors.New("closurecontrol: FeatureMap names features this server does not serve")

// MoveHandler applies a target position to the underlying device.
//
// It returns an error to refuse the move; the server maps that onto a
// Failure status and leaves its target state untouched, so a controller
// that could not reach the device does not see a target the device never
// accepted.
//
// The handler owns the southbound urgency of the write it performs: the
// cluster contract carries no priority, so a host whose command queue
// ranks by urgency names the value it wants at the handler, where the
// device vocabulary is already in scope.
type MoveHandler func(ctx context.Context, target wire.ClosureTargetPosition) error

// StopHandler halts motion. Like [MoveHandler] it owns the southbound
// urgency of its own write.
type StopHandler func(ctx context.Context) error

// Config carries the handlers and initial state for a
// [ControlServer].
type Config struct {
	// FeatureMap is the advertised feature set. Zero selects
	// [PositioningVentilationFeatureMap]. Any other value must be the one
	// [DerivedFeatureMap] derives from it — Positioning plus a choice of
	// Ventilation, Pedestrian, Protection and ManuallyOperable — or
	// [New] refuses it.
	FeatureMap uint32
	// Move applies a MoveTo. Nil makes MoveTo report Failure rather than
	// accepting a command that reaches nothing.
	Move MoveHandler
	// Stop halts motion. Nil makes Stop report Failure.
	Stop StopHandler
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

	mu sync.RWMutex

	inst *spec.Instance
	move MoveHandler
	stop StopHandler

	mainState wire.ClosureMainState
	errorList wire.ClosureErrorList

	// currentPosition and targetPosition are nil until observed, which is
	// the null the spec asks for on a quality-X field. A drive that has
	// not reported yet must not read as FullyClosed.
	currentPosition *wire.ClosureCurrentPosition
	targetPosition  *wire.ClosureTargetPosition
	secureState     *bool

	// emitter and endpoint address the events (closurecontrol_events.go);
	// the bridge wires both at reassembly.
	emitter  contract.EventEmitter
	endpoint uint16
}

// DerivedFeatureMap is the FeatureMap the server advertises for cfg: what
// it serves. Zero selects [PositioningVentilationFeatureMap]; otherwise
// Positioning is always served and Ventilation, Pedestrian, Protection and
// ManuallyOperable are taken from cfg.FeatureMap (see servedFeatures). It
// differs from a non-zero cfg.FeatureMap exactly when that one names a
// feature the server does not serve, or leaves out Positioning.
func DerivedFeatureMap(cfg Config) uint32 {
	if cfg.FeatureMap == 0 {
		return PositioningVentilationFeatureMap
	}
	return wire.ClosureControlFeaturePositioning | cfg.FeatureMap&servedFeatures
}

// New constructs a [ControlServer] from cfg. A cfg.FeatureMap other than
// zero and the one [DerivedFeatureMap] derives is refused with
// [ErrFeatureMap]: advertising it would list attributes, commands or events
// the server does not serve.
func New(cfg Config) (*ControlServer, error) {
	fm := DerivedFeatureMap(cfg)
	if cfg.FeatureMap != 0 && cfg.FeatureMap != fm {
		return nil, fmt.Errorf("%w: 0x%X, the server serves 0x%X", ErrFeatureMap, cfg.FeatureMap, fm)
	}
	// The derived selection satisfies every feature conformance
	// (Positioning, so "O.a+" and "[PS]" hold), and no optional element is
	// declared, so spec.New cannot refuse it.
	inst, _ := spec.New(closuredef.Definition, spec.Options{Features: fm})
	return &ControlServer{
		inst: inst,
		move: cfg.Move,
		stop: cfg.Stop,
		// SetupRequired, not Stopped: the drive has reported nothing yet,
		// and Stopped is a claim about a device we have not heard from.
		// Mirrors matter.js closure-control.element.ts:113 MainStateEnum.
		mainState: wire.ClosureMainStateSetupRequired,
		errorList: wire.ClosureErrorList{},
	}, nil
}

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
// CountdownTime (conformance "[PS & !IS]") and LatchControlModes
// (conformance "LT") are absent: the first is optional and not served, the
// second belongs to a feature the server does not serve.
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
		return &wire.ClosureOverallCurrentState{
			Position:    s.currentPosition,
			SecureState: s.secureState,
		}, true
	case wire.ClosureControlAttrOverallTargetState:
		return &wire.ClosureOverallTargetState{Position: s.targetPosition}, true
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
func (s *ControlServer) MatterWrite(_ context.Context, attrID uint32, value any) error {
	_, err := s.inst.ValidateWrite(attrID, value, nil)
	return err
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

// MatterInvoke handles the ClosureControl commands this feature set
// carries: MoveTo (conformance M) and Stop (conformance "!IS", mandatory
// because the server does not advertise Instantaneous).
//
// Calibrate is conformance "CL" and this server does not advertise
// Calibration, so it reports UnsupportedCommand rather than succeeding
// silently.
func (s *ControlServer) MatterInvoke(
	ctx context.Context, cmdID uint32, fields any,
) (response any, err error) {
	switch cmdID {
	case closuredef.CmdMoveTo:
		return nil, s.invokeMoveTo(ctx, fields)
	case closuredef.CmdStop:
		return nil, s.invokeStop(ctx)
	case closuredef.CmdCalibrate:
		return nil, closureUnsupportedCommandErr{
			"closurecontrol: Calibrate requires the Calibration feature, which this server does not advertise",
		}
	default:
		return nil, fmt.Errorf("closurecontrol: unknown command 0x%02X", cmdID)
	}
}

// invokeMoveTo applies a MoveTo request, as the specification text
// matter.js carries describes it (closure-control.resource.ts, MoveTo,
// cluster§5.4.8.2): a request with none of the three "O.a+" fields is
// INVALID_COMMAND; a field of a feature the server does not advertise is
// ignored rather than refused; with Positioning, an absent Position falls
// back to OverallTargetState.Position, and with that null too the closure
// does not change its position; a Position outside the advertised set is
// CONSTRAINT_ERROR.
func (s *ControlServer) invokeMoveTo(ctx context.Context, fields any) error {
	req, err := moveToRequest(fields)
	if errors.Is(err, wire.ErrClosureControlMalformed) {
		return closureInvalidCommandErr{"closurecontrol: MoveTo carries none of Position, Latch and Speed"}
	}
	if err != nil {
		return err
	}
	s.mu.RLock()
	fallback := s.targetPosition
	s.mu.RUnlock()
	// Position belongs to Positioning, which the server always serves;
	// Latch and Speed to features it does not (LT, SP): both ignored.
	target := req.Position
	if target == nil {
		target = fallback
	}
	if target == nil {
		return nil
	}
	if err := s.checkPositionSupported(*target); err != nil {
		return err
	}
	if s.move == nil {
		return errors.New("closurecontrol: MoveTo has no handler")
	}
	if err := s.move(ctx, *target); err != nil {
		// Deliberately no state change: recording a target the device
		// refused would leave the controller reading a move that never
		// happened.
		return fmt.Errorf("closurecontrol: MoveTo: %w", err)
	}
	s.mu.Lock()
	t := *target
	s.targetPosition = &t
	s.mainState = wire.ClosureMainStateMoving
	s.mu.Unlock()
	s.Notify(wire.ClosureControlAttrMainState, wire.ClosureControlAttrOverallTargetState)
	return nil
}

// invokeStop halts motion (cluster§5.4.8.1): only a closure that is
// Moving, WaitingForMotion or Calibrating stops and becomes Stopped; in
// any other state the command changes nothing. SUCCESS either way.
func (s *ControlServer) invokeStop(ctx context.Context) error {
	s.mu.RLock()
	state := s.mainState
	s.mu.RUnlock()
	switch state {
	case wire.ClosureMainStateMoving, wire.ClosureMainStateWaitingForMotion, wire.ClosureMainStateCalibrating:
	default:
		return nil
	}
	if s.stop == nil {
		return errors.New("closurecontrol: Stop has no handler")
	}
	if err := s.stop(ctx); err != nil {
		return fmt.Errorf("closurecontrol: Stop: %w", err)
	}
	s.mu.Lock()
	// The target is dropped, not kept: after a stop the drive is heading
	// nowhere, and a retained target claims otherwise.
	s.targetPosition = nil
	s.mainState = wire.ClosureMainStateStopped
	s.mu.Unlock()
	s.Notify(wire.ClosureControlAttrMainState, wire.ClosureControlAttrOverallTargetState)
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
		return closureConstraintErr{fmt.Sprintf("closurecontrol: MoveTo position %d is not a TargetPositionEnum value", p)}
	}
	if !s.inst.EnumSupported(closuredef.TargetPositionEnumDef, uint64(p)) {
		return closureConstraintErr{
			fmt.Sprintf("closurecontrol: MoveTo position %d needs a feature this server does not advertise", p),
		}
	}
	return nil
}

// moveToRequest normalises the command payload the bridge hands over. The
// bridge decodes MoveTo through the generated definition, so a real
// invocation arrives as [closuredef.MoveToRequest] (a field of the wrong
// TLV type already answered INVALID_COMMAND there, as matter.js's request
// schema answers it); a host that decodes the command itself may pass
// [wire.MoveToRequest], the raw TLV or the tag-keyed map of the bridge's
// generic salvage path (bridge/fields_reader.go decodeGenericTagMap).
func moveToRequest(fields any) (wire.MoveToRequest, error) {
	switch v := fields.(type) {
	case closuredef.MoveToRequest:
		return moveToRequest(wire.MoveToRequest{
			Position: (*wire.ClosureTargetPosition)(v.Position),
			Latch:    v.Latch,
			Speed:    (*uint8)(v.Speed),
		})
	case *closuredef.MoveToRequest:
		if v == nil {
			return wire.MoveToRequest{}, wire.ErrClosureControlMalformed
		}
		return moveToRequest(*v)
	case wire.MoveToRequest:
		if v.Position == nil && v.Latch == nil && v.Speed == nil {
			return wire.MoveToRequest{}, wire.ErrClosureControlMalformed
		}
		return v, nil
	case *wire.MoveToRequest:
		if v == nil {
			return wire.MoveToRequest{}, wire.ErrClosureControlMalformed
		}
		return moveToRequest(*v)
	case []byte:
		return wire.DecodeClosureMoveTo(v)
	case map[uint8]any:
		return moveToRequestFromTagMap(v)
	default:
		return wire.MoveToRequest{}, fmt.Errorf(
			"closurecontrol: MoveTo expected wire.MoveToRequest, map[uint8]any or []byte, got %T", fields,
		)
	}
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
// as absent here — the same reading [wire.DecodeClosureMoveTo] gives a
// null element. A map carrying none of the three fields is malformed for
// the same reason the TLV decoder says so: at least one is mandatory.
func moveToRequestFromTagMap(m map[uint8]any) (wire.MoveToRequest, error) {
	var req wire.MoveToRequest
	if raw, present := m[moveToFieldPosition]; present && raw != nil {
		// TargetPositionEnum is enum8: a wider value is a malformed field,
		// not a large position.
		n, ok := raw.(uint64)
		if !ok || n > math.MaxUint8 {
			return wire.MoveToRequest{}, wire.ErrClosureControlMalformed
		}
		pos := wire.ClosureTargetPosition(n)
		req.Position = &pos
	}
	if raw, present := m[moveToFieldLatch]; present && raw != nil {
		latch, ok := raw.(bool)
		if !ok {
			return wire.MoveToRequest{}, wire.ErrClosureControlMalformed
		}
		req.Latch = &latch
	}
	if raw, present := m[moveToFieldSpeed]; present && raw != nil {
		// ThreeLevelAutoEnum is enum8; same reasoning as Position.
		n, ok := raw.(uint64)
		if !ok || n > math.MaxUint8 {
			return wire.MoveToRequest{}, wire.ErrClosureControlMalformed
		}
		speed := uint8(n)
		req.Speed = &speed
	}
	if req.Position == nil && req.Latch == nil && req.Speed == nil {
		return wire.MoveToRequest{}, wire.ErrClosureControlMalformed
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
// A change of SecureState — true exactly at FullyClosed — emits
// SecureStateChanged.
func (s *ControlServer) SetCurrentPosition(pos *wire.ClosureCurrentPosition) {
	var events []pendingEvent
	defer func() { s.emit(events) }()
	defer s.Notify(wire.ClosureControlAttrOverallCurrentState, wire.ClosureControlAttrOverallTargetState)
	s.mu.Lock()
	defer s.mu.Unlock()
	prevSecure := s.secureState
	defer func() {
		if s.secureState != nil && (prevSecure == nil || *prevSecure != *s.secureState) {
			events = append(events, pendingEvent{
				closuredef.EventSecureStateChanged,
				SecureStateChangedEvent{SecureValue: *s.secureState},
				s.inst.EventPriority(closuredef.EventSecureStateChanged),
			})
		}
	}()
	s.currentPosition = pos
	// SecureState is "the closure is in a position that secures the
	// opening" — true only when fully closed (matter.js
	// closure-control.element.ts:134-137).
	secure := pos != nil && *pos == wire.ClosureCurrentPositionFullyClosed
	if pos == nil {
		s.secureState = nil
	} else {
		s.secureState = &secure
	}
	if pos == nil {
		// A null Position means the drive's position is unknown, which the
		// spec says plainly (matter.js closure-control.resource.ts:429-439:
		// "If the closure doesn't know accurately its current state the value
		// null shall be used"). Whether it is MOVING is a different question
		// and a different attribute, answered by the model from the drive's
		// own motion signal and delivered through [ControlServer.SetMainState]
		// — reading it off the position made a stuck or unreferenced door
		// report as perpetually in motion.
		return
	}
	// The drive arrived, so nothing is outstanding.
	s.targetPosition = nil
}

// SetMainState overrides the operational state. A motion that ends —
// Moving to Stopped — emits MovementCompleted (conformance !IS).
func (s *ControlServer) SetMainState(state wire.ClosureMainState) {
	s.mu.Lock()
	prev := s.mainState
	s.mainState = state
	s.mu.Unlock()
	if prev != state {
		s.Notify(wire.ClosureControlAttrMainState)
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
	s.Notify(wire.ClosureControlAttrCurrentErrorList)
}

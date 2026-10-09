// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package cover contains the Matter WindowCovering cluster server (0x0102)
// for the position-aware lift feature profile.
//
// This package is a conformance reference: the server holds its own state
// and answers every command with Success without forwarding it anywhere,
// so it does not drive a device. With [Config.MoveStep] it simulates the
// lift's travel the way matter.js's CHIP test node does. It exists to pin the cluster's wire shape
// and attribute surface against matter.js HEAD; only the reference daemon
// (examples/reference-bridge) mounts it, to put that surface in front of
// chip-tool. A host that needs live control mounts its own
// [contract.ClusterServer] on the endpoint instead.
//
// The cluster's identity is the generated definition (cluster/spec/
// windowcovering, ADR 0013): ids, revision, the attribute and command
// lists, the write statuses and checks and the privileges. The FeatureMap
// is derived from what the server serves — Lift and PositionAwareLift
// ([ServedFeatureMap]) — not taken on trust: a host FeatureMap other than
// that one is refused at construction. GoToLiftPercentage decodes through
// the definition; the lift arithmetic and the travel are the server's.
package cover

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	wcdef "github.com/SukramJ/go-fabric/cluster/spec/windowcovering"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/im"
)

// ClusterRevision is the WindowCovering cluster revision of the generated
// definition (window-covering-cluster.element.ts).
const ClusterRevision = wcdef.Revision

// ServedFeatureMap is the one FeatureMap the server serves: Lift and
// PositionAwareLift (0x05). Its attributes are exactly those the
// selection makes mandatory — TargetPositionLiftPercent100ths and
// CurrentPositionLiftPercent100ths are "LF & PA_LF" — plus the optional
// CurrentPositionLiftPercentage ("[LF & PA_LF]") and SafetyStatus ("O").
// Tilt would add tilt attributes the server has no state for, and Lift
// without PositionAwareLift would disallow the two position attributes it
// serves.
const ServedFeatureMap = uint32(wcdef.FeatureLift | wcdef.FeaturePositionAwareLift)

// ErrFeatureMap reports a Config.FeatureMap other than [ServedFeatureMap].
var ErrFeatureMap = errors.New("windowcovering: FeatureMap names features this server does not serve")

// servedOptional are the optional attributes the server serves.
var servedOptional = []uint32{wcdef.AttrCurrentPositionLiftPercentage, wcdef.AttrSafetyStatus}

// goToLiftPercentageFieldValue is the LiftPercent100thsValue context tag
// (window-covering-cluster.element.ts:95, id 0x0).
const goToLiftPercentageFieldValue uint8 = 0

// Config carries the initial state and type-specific attributes for a
// [WindowCoveringServer].
type Config struct {
	// Type is the Matter WindowCovering Type attribute value (enum8).
	Type uint8
	// EndProductType is the Matter WindowCovering EndProductType
	// attribute value (enum8).
	EndProductType uint8
	// FeatureMap is the Matter FeatureMap. The server derives it from
	// what it serves, [ServedFeatureMap]; zero selects that, and any other
	// value but it is refused by [New].
	FeatureMap uint32
	// InitialPositionPercent100ths is the starting value for
	// CurrentPositionLiftPercent100ths and
	// TargetPositionLiftPercent100ths (0 = fully open, 10000 = fully
	// closed, per Matter §5.3.6.11).
	InitialPositionPercent100ths uint16
	// MoveStep makes a movement travel: when non-zero a command moves the
	// lift in six steps of MoveStep, reporting OperationalStatus Opening
	// or Closing until the target is reached, as matter.js's CHIP test
	// node does (support/chip-testing/src/cluster/
	// TestWindowCoveringServer.ts, six 950 ms steps). Zero moves at once,
	// as matter.js's WindowCoveringServer does without a handleMovement of
	// its own.
	MoveStep time.Duration
}

// moveSteps is the number of steps a travelling movement takes
// (TestWindowCoveringServer.ts #handleMovementTick: counter >= 6).
const moveSteps = 6

// WindowCoveringServer implements
// [github.com/SukramJ/go-fabric/contract.ClusterServer]
// for the Matter WindowCovering cluster (0x0102), position-aware lift
// feature profile. State is self-contained; commands update internal
// fields and return Success so that commissioning tools and controller
// apps receive a well-formed cluster without requiring a live CCU
// backend.
type WindowCoveringServer struct {
	mu sync.RWMutex

	inst           *spec.Instance
	wcType         uint8
	endProductType uint8
	wcMode         uint8 // Mode attribute (0x0017), RW VM, constraint max 15.

	currentPositionPercent100ths uint16
	targetPositionPercent100ths  uint16
	operationalStatus            uint8

	moveStep time.Duration
	// move is the running movement; nil when the lift stands still.
	move *movement
	// changes reports what a travelling movement changes between
	// requests (contract.AttributeChangeNotifier).
	changes cluster.AttributeChanges
}

// movement is one travelling lift movement.
type movement struct {
	timer     *time.Timer
	increment int
	target    uint16
	counter   int
}

// OperationalStatus MovementStatus values (window-covering-cluster
// element: Stopped 0, Opening 1, Closing 2) and the bitmap layout: global
// in bits 0-1, lift in bits 2-3.
const (
	movementStopped uint8 = 0
	movementOpening uint8 = 1
	movementClosing uint8 = 2
)

// liftStatus is OperationalStatus with global and lift set to st
// (matter.js #updateGlobalStatus: global follows lift when tilt stands).
func liftStatus(st uint8) uint8 { return st | st<<2 }

// OnMatterAttributesChanged implements [contract.AttributeChangeNotifier].
func (s *WindowCoveringServer) OnMatterAttributesChanged(cb func(attrIDs []uint32)) (unsubscribe func()) {
	return s.changes.OnMatterAttributesChanged(cb)
}

// New constructs a [WindowCoveringServer] from cfg. A cfg.FeatureMap other
// than zero and [ServedFeatureMap] is refused with [ErrFeatureMap]:
// advertising it would list attributes or commands the server does not
// serve.
func New(cfg Config) (*WindowCoveringServer, error) {
	if cfg.FeatureMap != 0 && cfg.FeatureMap != ServedFeatureMap {
		return nil, fmt.Errorf("%w: 0x%X, the server serves 0x%X", ErrFeatureMap, cfg.FeatureMap, ServedFeatureMap)
	}
	// LF | PA_LF satisfies "O.a+" and "[LF]", and both optional attributes
	// are allowed under it, so spec.New cannot refuse it.
	inst, _ := spec.New(wcdef.Definition, spec.Options{Features: ServedFeatureMap, Attributes: servedOptional})
	return &WindowCoveringServer{
		inst:                         inst,
		wcType:                       cfg.Type,
		endProductType:               cfg.EndProductType,
		currentPositionPercent100ths: cfg.InitialPositionPercent100ths,
		targetPositionPercent100ths:  cfg.InitialPositionPercent100ths,
		operationalStatus:            0,
		moveStep:                     cfg.MoveStep,
	}, nil
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
		return configStatusOperational | configStatusLiftPositionAware, true
	case wire.WindowCoveringAttrCurrentPositionLiftPercentage:
		// Deprecated uint8 field (0x0008); return scaled-down value for
		// backward compatibility with Matter 1.0 controllers.
		return uint8((s.currentPositionPercent100ths / 100) & 0xFF), true // value bounded 0-100 by design
	case wire.WindowCoveringAttrOperationalStatus:
		return s.operationalStatus, true
	case wire.WindowCoveringAttrTargetPositionLiftPercent100ths:
		return s.targetPositionPercent100ths, true
	case wire.WindowCoveringAttrEndProductType:
		return s.endProductType, true
	case wire.WindowCoveringAttrCurrentPositionLiftPercent100ths:
		return s.currentPositionPercent100ths, true
	case wire.WindowCoveringAttrMode:
		return s.wcMode, true
	case wire.WindowCoveringAttrSafetyStatus:
		return uint16(0), true
	default:
		return nil, false
	}
}

// ConfigStatusBitmap bit positions, from the datatype's per-field
// constraint in matter.js
// packages/model/src/standard/elements/window-covering-cluster.element.ts:109-116.
// The bits are read from the element file, not counted from the field
// order: OnlineReserved (bit 1, conformance D) and LiftMovementReversed
// (bit 2) sit between the two this server sets.
//
// matter.js WindowCoveringServer.ts:121-125 initialize() sets Operational
// and, for a position-aware lift, LiftPositionAware; LiftMovementReversed
// is only ever derived from Mode.MotorDirectionReversed
// (WindowCoveringServer.ts:189), which this server never sets.
const (
	configStatusOperational       uint8 = 1 << 0 // constraint "0", element :110
	configStatusLiftPositionAware uint8 = 1 << 3 // constraint "3", element :113
)

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
func (s *WindowCoveringServer) MatterWrite(_ context.Context, attrID uint32, value any) error {
	v, err := s.inst.ValidateWrite(attrID, value, nil)
	if err != nil {
		if attrID == wcdef.AttrMode {
			return windowCoveringConstraintErr{fmt.Sprintf("windowcovering: Mode %v exceeds constraint max 15: %v", value, err)}
		}
		return err
	}
	n, _ := v.(uint64) // a map8 ≤ 15 per ValidateWrite
	s.mu.Lock()
	s.wcMode = uint8(n) //nolint:gosec // ≤ 15 per ValidateWrite
	s.mu.Unlock()
	return nil
}

// MinWritePrivilege implements [contract.ClusterAttributeWritePrivilege]:
// Mode is "RW VM".
func (s *WindowCoveringServer) MinWritePrivilege(attrID uint32) uint8 {
	return s.inst.MinWritePrivilege(attrID)
}

// MatterInvoke handles the mandatory WindowCovering commands. All four
// commands accept the request and update internal state; they return
// Success without forwarding anywhere — see the package doc on why this
// server does not drive a device.
func (s *WindowCoveringServer) MatterInvoke(_ context.Context, cmdID uint32, fields any) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch cmdID {
	case wcdef.CmdUpOrOpen:
		s.moveToLocked(0)
		return nil, nil
	case wire.WindowCoveringCmdDownOrClose:
		s.moveToLocked(10000)
		return nil, nil
	case wire.WindowCoveringCmdStopMotion:
		// matter.js handleStopMovement: the lift stops where it is, and
		// the target follows the position.
		s.stopLocked()
		s.targetPositionPercent100ths = s.currentPositionPercent100ths
		s.operationalStatus = liftStatus(movementStopped)
		return nil, nil
	case wire.WindowCoveringCmdGoToLiftPercentage:
		pct, err := extractPercent100ths(fields)
		if err != nil {
			return nil, err
		}
		s.moveToLocked(pct)
		return nil, nil
	default:
		return nil, fmt.Errorf("windowcovering: unknown command 0x%02X", cmdID)
	}
}

// moveToLocked starts a movement to target. Caller holds s.mu. Without a
// MoveStep the lift arrives at once; with one it travels, as matter.js
// TestWindowCoveringServer.handleMovement does: the status says which way
// (#computeOperationalState), and the position moves in six steps.
func (s *WindowCoveringServer) moveToLocked(target uint16) {
	s.stopLocked()
	s.targetPositionPercent100ths = target
	cur := s.currentPositionPercent100ths
	if s.moveStep == 0 || cur == target {
		s.currentPositionPercent100ths = target
		s.operationalStatus = liftStatus(movementStopped)
		return
	}
	dir := movementOpening
	if target > cur {
		dir = movementClosing
	}
	s.operationalStatus = liftStatus(dir)
	diff := float64(int(target) - int(cur))
	inc := int(math.Floor(diff / moveSteps))
	if target < cur {
		inc = int(math.Ceil(diff / moveSteps))
	}
	m := &movement{increment: inc, target: target}
	m.timer = time.AfterFunc(s.moveStep, func() { s.tick(m) })
	s.move = m
}

// stopLocked ends a running movement. Caller holds s.mu.
func (s *WindowCoveringServer) stopLocked() {
	if s.move != nil {
		s.move.timer.Stop()
		s.move = nil
	}
}

// tick is one step of a travelling movement
// (TestWindowCoveringServer.ts #handleMovementTick).
func (s *WindowCoveringServer) tick(m *movement) {
	s.mu.Lock()
	if s.move != m {
		s.mu.Unlock()
		return // stopped or replaced between the fire and the lock
	}
	changed := []uint32{wire.WindowCoveringAttrCurrentPositionLiftPercent100ths, wire.WindowCoveringAttrCurrentPositionLiftPercentage}
	m.counter++
	if m.counter >= moveSteps {
		s.currentPositionPercent100ths = m.target
		s.operationalStatus = liftStatus(movementStopped)
		s.move = nil
		changed = append(changed, wire.WindowCoveringAttrOperationalStatus)
	} else {
		s.currentPositionPercent100ths = uint16(int(s.currentPositionPercent100ths) + m.increment) //nolint:gosec // six increments of a 0-10000 span stay inside it
		m.timer.Reset(s.moveStep)
	}
	s.mu.Unlock()
	s.changes.Notify(changed...)
}

// MatterAcceptedCommands implements [contract.ClusterCommandLister]:
// UpOrOpen, DownOrClose and StopMotion (conformance M), and
// GoToLiftPercentage ("LF & PA_LF, [LF]"). Without the lister the
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
// attributes LF | PA_LF makes mandatory and the two optional ones served.
func (s *WindowCoveringServer) MatterAttributes() []uint32 { return s.inst.MatterAttributes() }

// extractPercent100ths pulls the LiftPercent100thsValue (field 0,
// window-covering-cluster.element.ts:95) out of the GoToLiftPercentage
// command fields. The bridge decodes the command through the generated
// definition, so a real invocation arrives as
// [wcdef.GoToLiftPercentageRequest] (a missing field already answered
// INVALID_COMMAND there, a value above 10000 CONSTRAINT_ERROR, as
// matter.js's request schema answers them); a host that decodes the
// command itself may pass [wire.GoToLiftPercentageRequest] or the tag-keyed
// map of the bridge's generic salvage path (bridge/fields_reader.go
// decodeGenericTagMap, the value a uint64), and the bare uint16 and the
// "percent"-keyed map stay accepted for direct callers. Values > 10000
// are rejected with ConstraintError per
// window-covering-cluster.element.ts:72 constraint "max 10000".
func extractPercent100ths(fields any) (uint16, error) {
	var pct uint16
	switch v := fields.(type) {
	case uint16:
		pct = v
	case wcdef.GoToLiftPercentageRequest:
		pct = v.LiftPercent100thsValue
	case *wcdef.GoToLiftPercentageRequest:
		if v == nil {
			return 0, errors.New("windowcovering: GoToLiftPercentage carried no fields")
		}
		pct = v.LiftPercent100thsValue
	case wire.GoToLiftPercentageRequest:
		pct = v.LiftPercent100thsValue
	case *wire.GoToLiftPercentageRequest:
		if v == nil {
			return 0, errors.New("windowcovering: GoToLiftPercentage carried no fields")
		}
		pct = v.LiftPercent100thsValue
	case map[uint8]any:
		raw, ok := v[goToLiftPercentageFieldValue]
		if !ok {
			return 0, errors.New("windowcovering: GoToLiftPercentage missing LiftPercent100thsValue (field 0)")
		}
		n, isUint := raw.(uint64)
		if !isUint || n > math.MaxUint16 {
			return 0, fmt.Errorf("windowcovering: GoToLiftPercentage LiftPercent100thsValue %v (%T) is not a uint16", raw, raw)
		}
		pct = uint16(n)
	case map[string]any:
		raw, ok := v["percent"]
		if !ok {
			return 0, errors.New("windowcovering: GoToLiftPercentage missing percent field")
		}
		var castOK bool
		pct, castOK = raw.(uint16)
		if !castOK {
			return 0, fmt.Errorf("windowcovering: GoToLiftPercentage percent expected uint16, got %T", raw)
		}
	default:
		return 0, fmt.Errorf("windowcovering: GoToLiftPercentage expected map[uint8]any, wire.GoToLiftPercentageRequest, uint16 or map[string]any, got %T", fields)
	}
	// Constraint max 10000 per matter.js window-covering-cluster.element.ts:72.
	if pct > 10000 {
		return 0, windowCoveringConstraintErr{fmt.Sprintf("windowcovering: liftPercent100thsValue %d exceeds constraint max 10000", pct)}
	}
	return pct, nil
}

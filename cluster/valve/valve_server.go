// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package valve contains the Matter ValveConfigurationAndControl cluster
// server (0x0081): a valve that opens and closes, with the Level (LVL)
// and TimeSync (TS) features where the host selects them.
//
// The server owns no valve state. Every attribute it answers is read
// from a [StateSource] the host implements, and both commands are
// forwarded to that same port; a server that flipped an internal field
// and reported Success would tell a controller the valve moved without
// anything having reached the device. The one value it derives is
// AutoCloseTime (TS): the node's Matter time at an Open plus the opening's
// duration, as connectedhomeip sets it.
//
// Level is served over the StateSource's optional [LevelSource] (and
// [DefaultOpenLevelSource] for the optional DefaultOpenLevel): the server
// resolves an Open's level and checks it against LevelStep, as
// connectedhomeip's cluster does. matter.js's server is the empty
// subclass (ValveConfigurationAndControlServer.ts:14 at 862ad75f), so
// both features are an "Ahead of matter.js" row
// (notes/parity/by_design.md BD-chip-Valve-RulesInServer; [Ahead]).
//
// The cluster's identity is the generated definition
// (cluster/spec/valveconfigurationandcontrol, ADR 0013): ids, revision,
// the attribute and command lists the selection yields, the write checks
// and the decoding of the Open request. What stays here is the host port,
// the OpenDuration presence rule and the Level and TimeSync rules.
package valve

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	valvedef "github.com/SukramJ/go-fabric/cluster/spec/valveconfigurationandcontrol"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// ClusterID is the ValveConfigurationAndControl cluster id.
const ClusterID = valvedef.ClusterID

// Attribute ids of the five conformance-M attributes. AutoCloseTime (0x2)
// is "TS"-gated (valve-configuration-and-control.element.ts:35),
// CurrentLevel (0x6), TargetLevel (0x7), DefaultOpenLevel (0x8) and
// LevelStep (0xa) are LVL-gated (:39-40, :41-44, :46-49); the server
// serves them with the features (valvedef.Attr…). ValveFault (0x9) is
// optional (:45) and not served.
const (
	// AttrOpenDuration is the duration the valve stays open for the
	// current opening, in seconds; null means "until closed by the user
	// or some other automation" (element :27-30, resource :37-43).
	AttrOpenDuration = valvedef.AttrOpenDuration
	// AttrDefaultOpenDuration is the duration applied when an Open
	// command carries no OpenDuration field (element :31-34,
	// resource :46-52). It is the cluster's only writable attribute
	// here: access "RW VO" (:32).
	AttrDefaultOpenDuration = valvedef.AttrDefaultOpenDuration
	// AttrRemainingDuration is the time left until the valve closes;
	// null while OpenDuration is null or the valve is closed
	// (element :36, resource :77-105).
	AttrRemainingDuration = valvedef.AttrRemainingDuration
	// AttrCurrentState is the valve's current [State]; null means the
	// state is not known (element :37, resource :109-113).
	AttrCurrentState = valvedef.AttrCurrentState
	// AttrTargetState is the state the valve is moving towards; null
	// means no target is set because the change is done or failed
	// (element :38, resource :116-120).
	AttrTargetState = valvedef.AttrTargetState
)

// Command ids. Both carry conformance "M" and response "status"
// (element :60, :64), so neither produces a generated command.
const (
	// CmdOpen sets the valve to its open position (element :60).
	CmdOpen = valvedef.CmdOpen
	// CmdClose sets the valve to its closed position (element :64).
	CmdClose = valvedef.CmdClose
)

// Open command field tags of the tag map an in-process caller hands over
// (element :61-62).
const (
	openFieldOpenDuration uint8 = 0
	openFieldTargetLevel  uint8 = 1
)

// FeatureMap bits. TS carries conformance "desc" and constraint "0"; LVL
// carries "O" and constraint "1" (element :24-25).
const (
	// FeatureTimeSync is the TS bit — the valve expresses AutoCloseTime
	// against synchronized UTC time (resource :21-26), the node's Matter
	// time ([Server.AttachMatterTime]).
	FeatureTimeSync = uint32(valvedef.FeatureTimeSync)
	// FeatureLevel is the LVL bit — the valve can be driven to a
	// percentage of its range (resource :29-32); it needs a [LevelSource].
	FeatureLevel = uint32(valvedef.FeatureLevel)
)

// featureMapNone is the FeatureMap of a valve that only opens and closes:
// TS is conformance "desc" and LVL is optional, so zero is conformant.
const featureMapNone uint32 = 0

// instance is the definition bound to [featureMapNone] with no optional
// element declared: the five mandatory attributes, Open and Close — what
// [Server.MatterRead] answers the globals from.
var instance, _ = spec.New(valvedef.Definition, spec.Options{Features: featureMapNone})

// maxLevel is the full level, 100 %: the level of an Open without
// TargetLevel and DefaultOpenLevel, and a level that complies with any
// LevelStep (connectedhomeip kMaxLevelValuePercent).
const maxLevel uint8 = 100

// ErrSelection reports a Config whose selection the server cannot serve on
// its StateSource: a feature or optional attribute whose interface the
// source lacks, TS without the node's time, or a selection the definition
// refuses.
var ErrSelection = errors.New("valve: selection not served")

// State is the ValveStateEnum (enum8) a controller reads from
// CurrentState and TargetState.
type State = valvedef.ValveStateEnum

// ValveStateEnum values (element :76-81).
const (
	// StateClosed — valve is in closed position (resource :250).
	StateClosed = valvedef.ValveStateClosed
	// StateOpen — valve is in open position (resource :251).
	StateOpen = valvedef.ValveStateOpen
	// StateTransitioning — valve is moving between positions
	// (resource :252-255).
	StateTransitioning = valvedef.ValveStateTransitioning
)

// OpenRequest is the Open command's payload in the generated type
// (element :59-63).
//
// Presence and null are distinct and both are carried: a nil
// OpenDuration (the field absent) means "use DefaultOpenDuration" while a
// present null (OpenDuration.Null) means "stay open until something
// closes me" (resource :46-52 vs :213-218). Collapsing the two would
// silently turn an indefinite opening into the default one. TargetLevel
// needs the Level feature this server does not advertise, so the source
// sees one only with the Level feature, resolved by the server.
type OpenRequest = valvedef.OpenRequest

// StateSource is the host port this server reads and drives. The host
// implements it over whatever actually owns the valve; nothing in this
// package caches or invents a value.
//
// Every reader returns (value, known). known=false becomes a TLV null,
// which is what quality X asks for on all five attributes
// (element :27-38) — the spec's own way of saying the value is not
// known, as opposed to guessing Closed for a device that has not
// reported.
//
// The command methods own the southbound urgency of the writes they
// perform: the cluster contract carries no priority, so a host whose
// command queue ranks by urgency names the value it wants inside its
// own implementation, where the device vocabulary is in scope.
//
// It is the generated port (valveconfigurationandcontroldef.StateSource).
type StateSource = valvedef.StateSource

// LevelSource is the StateSource's optional interface for the Level
// feature (valveconfigurationandcontroldef.LevelSource).
type LevelSource = valvedef.LevelSource

// DefaultOpenLevelSource is the StateSource's optional interface for the
// DefaultOpenLevel attribute
// (valveconfigurationandcontroldef.DefaultOpenLevelSource).
type DefaultOpenLevelSource = valvedef.DefaultOpenLevelSource

// Config carries the construction parameters for [Server].
type Config struct {
	// Source is the host port. A nil Source makes every attribute read
	// null and every command fail — the server never pretends to have
	// moved a valve it cannot reach.
	Source StateSource
	// DataVersion is an optional tracker owned by the host. When
	// non-nil the server bumps and reports the caller's counter, so the
	// version survives server reconstruction; when nil an embedded
	// tracker is used.
	DataVersion *cluster.DataVersionTracker
	// Events are the optional events the server declares
	// (ValveStateChanged, ValveFault), which the host emits through the
	// typed server's Emit.
	Events []uint32
	// Features is the FeatureMap: zero, [FeatureLevel] (the Source
	// implements [LevelSource]) and [FeatureTimeSync] (AutoCloseTime on the
	// node's Matter time).
	Features uint32
	// Attributes are the optional attributes the server serves beyond the
	// selection's mandatory ones: DefaultOpenLevel (the Source implements
	// [DefaultOpenLevelSource]) and LevelStep, both "[LVL]".
	Attributes []uint32
	// LevelStep is the LevelStep attribute (fixed, 1 to 50; the
	// definition's default 1 when zero) where Attributes carries it: a
	// TargetLevel or DefaultOpenLevel must then be 100 or a multiple of it.
	LevelStep uint8
}

// Server implements [contract.ClusterServer] for
// ValveConfigurationAndControl (0x0081).
type Server struct {
	embedded cluster.DataVersionTracker // used when Config.DataVersion is nil
	ext      *cluster.DataVersionTracker
	src      StateSource
	inst     *spec.Instance // the definition with the selection and Config.Events
	hooks    spec.Hooks

	level     LevelSource            // nil without LVL
	defaults  DefaultOpenLevelSource // nil without DefaultOpenLevel
	levelStep uint8                  // zero without LevelStep
	timeSync  bool                   // TS
	// matterTime is the Matter time TS reads ([Server.AttachMatterTime]);
	// nil without one.
	matterTime atomic.Pointer[clock.MatterTime]

	// cmd serialises what writes AutoCloseTime: Open, Close and
	// MatterTimeChanged, each held across its reads of the time and of the
	// source, so a moved time neither overwrites a fresher Open or Close
	// nor is overwritten by an Open that read the time before it moved.
	cmd sync.Mutex
	// mu guards autoClose, the AutoCloseTime the last Open set (TS),
	// epoch-us; nil is null.
	mu        sync.Mutex
	autoClose *uint64
}

// Compile-time assertions.
var (
	_ contract.ClusterServer          = (*Server)(nil)
	_ contract.ClusterDataVersion     = (*Server)(nil)
	_ contract.ClusterAttributeLister = (*Server)(nil)
	_ contract.ClusterCommandLister   = (*Server)(nil)
)

// New constructs a [Server] over the host port in cfg. A selection the
// server cannot serve is refused with [ErrSelection], naming the missing
// interface: LVL without a [LevelSource], DefaultOpenLevel without a
// [DefaultOpenLevelSource], a LevelStep outside 1 to 50, or a selection
// the definition refuses. TS reads the node's Matter time, which the node
// hands the server as it joins the topology ([Server.AttachMatterTime]).
func New(cfg Config) (*Server, error) {
	s := &Server{src: cfg.Source, ext: cfg.DataVersion}
	if cfg.Features&^(FeatureLevel|FeatureTimeSync) != 0 {
		return nil, fmt.Errorf("%w: FeatureMap 0x%X names an undefined feature", ErrSelection, cfg.Features)
	}
	if cfg.Features&FeatureLevel != 0 {
		l, ok := cfg.Source.(LevelSource)
		if !ok {
			return nil, fmt.Errorf("%w: Level needs a StateSource that implements valveconfigurationandcontrol.LevelSource", ErrSelection)
		}
		s.level = l
	}
	s.timeSync = cfg.Features&FeatureTimeSync != 0
	if slices.Contains(cfg.Attributes, valvedef.AttrDefaultOpenLevel) {
		d, ok := cfg.Source.(DefaultOpenLevelSource)
		if !ok {
			return nil, fmt.Errorf("%w: DefaultOpenLevel needs a StateSource that implements valveconfigurationandcontrol.DefaultOpenLevelSource", ErrSelection)
		}
		s.defaults = d
	}
	if slices.Contains(cfg.Attributes, valvedef.AttrLevelStep) {
		s.levelStep = cfg.LevelStep
		if s.levelStep == 0 {
			s.levelStep = 1 // the definition's default (element :46-49)
		}
		if s.levelStep > 50 {
			return nil, fmt.Errorf("%w: LevelStep %d is outside 1 to 50", ErrSelection, s.levelStep)
		}
	}
	inst, err := spec.New(valvedef.Definition, spec.Options{Features: cfg.Features, Attributes: cfg.Attributes, Events: cfg.Events})
	if err != nil {
		if len(cfg.Events) == 0 || cfg.Features != 0 || len(cfg.Attributes) != 0 {
			return nil, fmt.Errorf("%w: %w", ErrSelection, err)
		}
		// An event id outside the definition leaves the EventList empty,
		// as without Events.
		inst = instance
	}
	s.inst = inst
	s.hooks.Init(s.inst)
	return s, nil
}

// NewServer constructs a [Server] from cfg, as [New] does; it panics on a
// selection [New] refuses — a wiring error, not a runtime condition. A
// host that builds its selection at run time calls [New].
func NewServer(cfg Config) *Server {
	s, err := New(cfg)
	if err != nil {
		panic(err)
	}
	return s
}

// MatterEvents implements [contract.ClusterEventLister]: the optional
// events Config.Events declares.
func (s *Server) MatterEvents() []uint32 { return s.inst.MatterEvents() }

// tracker returns the active DataVersion counter — the host's when it
// supplied one, the embedded one otherwise.
func (s *Server) tracker() *cluster.DataVersionTracker {
	if s.ext != nil {
		return s.ext
	}
	return &s.embedded
}

// Revision returns the cluster revision of the generated definition. Reading
// it rather than restating it is the point: a regeneration moves this value
// and a hand-written copy would not follow.
func Revision() uint16 { return valvedef.Revision }

// MatterClusterID returns 0x0081.
func (*Server) MatterClusterID() uint32 { return ClusterID }

// MatterDataVersion implements [contract.ClusterDataVersion].
func (s *Server) MatterDataVersion() uint32 { return s.tracker().Current() }

// MatterRead resolves the served attributes plus the two universal
// globals every cluster server answers itself.
func (s *Server) MatterRead(attrID uint32) (value any, ok bool) {
	if v, ok := s.inst.ReadGlobal(attrID); ok {
		return v, true
	}
	if !s.inst.Serves(attrID) {
		return nil, false
	}
	switch attrID {
	case valvedef.AttrAutoCloseTime:
		return s.readAutoClose()
	case valvedef.AttrCurrentLevel:
		return nullablePercent(s.level.CurrentLevel())
	case valvedef.AttrTargetLevel:
		return nullablePercent(s.level.TargetLevel())
	case valvedef.AttrDefaultOpenLevel:
		return s.defaults.DefaultOpenLevel(), true
	case valvedef.AttrLevelStep:
		return s.levelStep, true
	}
	if s.src == nil {
		return nil, true
	}
	return s.readFromSource(attrID)
}

// readAutoClose answers AutoCloseTime: the value the last Open set, while
// the valve still counts down (RemainingDuration known and not zero); null
// otherwise — after a Close, a self-close or an Open without a duration or
// a synchronized time (connectedhomeip ValveConfigurationAndControlCluster.cpp
// CloseValve :243-247 and SetAutoCloseTime :391-413 at 6170af84).
func (s *Server) readAutoClose() (any, bool) {
	s.mu.Lock()
	at := s.autoClose
	s.mu.Unlock()
	if at == nil || s.src == nil {
		return nil, true
	}
	if left, known := s.src.RemainingDuration(); !known || left == 0 {
		return nil, true
	}
	return *at, true
}

// nullablePercent mirrors [nullableSeconds] for the level attributes.
func nullablePercent(percent uint8, known bool) (any, bool) {
	if !known {
		return nil, true
	}
	return percent, true
}

// readFromSource projects one host-reported value onto the wire types
// the encoder expects: elapsed-s as uint32 (4-byte TLV uint) and
// ValveStateEnum as uint8. Returning the named [State] type instead
// would fall through the encoder's type switch.
func (s *Server) readFromSource(attrID uint32) (any, bool) {
	switch attrID {
	case AttrOpenDuration:
		return nullableSeconds(s.src.OpenDuration())
	case AttrDefaultOpenDuration:
		return nullableSeconds(s.src.DefaultOpenDuration())
	case AttrRemainingDuration:
		return nullableSeconds(s.src.RemainingDuration())
	case AttrCurrentState:
		return nullableState(s.src.CurrentState())
	case AttrTargetState:
		return nullableState(s.src.TargetState())
	default:
		return nil, false
	}
}

// nullableSeconds maps a host reading onto (value, true) or the TLV
// null the quality-X attributes carry when nothing has been observed.
func nullableSeconds(seconds uint32, known bool) (any, bool) {
	if !known {
		return nil, true
	}
	return seconds, true
}

// nullableState mirrors [nullableSeconds] for the two enum attributes.
func nullableState(state State, known bool) (any, bool) {
	if !known {
		return nil, true
	}
	return uint8(state), true
}

// MatterWrite applies the writable attributes: DefaultOpenDuration and,
// where served, DefaultOpenLevel.
//
// Both carry access "RW VO"; the others are "R V" and are rejected as
// UNSUPPORTED_WRITE rather than silently dropped, an id the cluster does
// not serve as UNSUPPORTED_ATTRIBUTE. The definition checks the value: a
// null DefaultOpenDuration clears the default (resource :50-52), any other
// value must satisfy constraint "min 1", so zero is a CONSTRAINT_ERROR
// rather than a duration of no seconds; DefaultOpenLevel is 1 to 100.
func (s *Server) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	v, err := s.inst.ValidateWrite(attrID, value, nil)
	if err != nil {
		return err
	}
	if err := s.hooks.CheckWrite(ctx, attrID, v); err != nil {
		return err
	}
	if attrID == valvedef.AttrDefaultOpenLevel {
		return s.writeDefaultOpenLevel(ctx, v)
	}
	var seconds *uint32
	if n, ok := v.(uint64); ok {
		sec := uint32(n) //nolint:gosec // a nullable uint32 per ValidateWrite
		seconds = &sec
	}
	if s.src == nil {
		return errNoSource("DefaultOpenDuration write")
	}
	// An unchanged value is a successful no-op: no host call, no
	// DataVersion. Mirrors connectedhomeip
	// src/app/clusters/valve-configuration-and-control-server/ValveConfigurationAndControlCluster.cpp:165-166
	// at 6170af84 (kWriteSuccessNoOp; W3-club2-05).
	if cur, known := s.src.DefaultOpenDuration(); (seconds == nil && !known) || (seconds != nil && known && cur == *seconds) {
		return nil
	}
	if err := s.src.SetDefaultOpenDuration(ctx, seconds); err != nil {
		return fmt.Errorf("valve: DefaultOpenDuration write: %w", err)
	}
	s.tracker().Bump()
	return nil
}

// writeDefaultOpenLevel applies a checked DefaultOpenLevel: a level that
// is not 100 or a multiple of LevelStep is CONSTRAINT_ERROR, an unchanged
// one a successful no-op. Mirrors connectedhomeip
// src/app/clusters/valve-configuration-and-control-server/ValveConfigurationAndControlCluster.cpp:175-192
// at 6170af84.
func (s *Server) writeDefaultOpenLevel(ctx context.Context, v any) error {
	n, _ := v.(uint64)
	level := uint8(n) //nolint:gosec // percent, 1 to 100 per ValidateWrite
	if !s.compliesWithLevelStep(level) {
		return constraintError{fmt.Sprintf("valve: DefaultOpenLevel %d is not 100 or a multiple of LevelStep %d", level, s.levelStep)}
	}
	if s.defaults.DefaultOpenLevel() == level {
		return nil
	}
	if err := s.defaults.SetDefaultOpenLevel(ctx, level); err != nil {
		return fmt.Errorf("valve: DefaultOpenLevel write: %w", err)
	}
	s.tracker().Bump()
	return nil
}

// compliesWithLevelStep reports whether level is 100 or a multiple of
// LevelStep; any level complies without LevelStep (connectedhomeip
// ValueCompliesWithLevelStep, ValveConfigurationAndControlCluster.cpp:483-490
// at 6170af84).
func (s *Server) compliesWithLevelStep(level uint8) bool {
	return s.levelStep == 0 || level == maxLevel || level%s.levelStep == 0
}

// MatterInvoke dispatches the two conformance-M commands. Both reach
// the host port; neither reports Success on its own.
//
// A host error surfaces as the generic Failure status. The cluster's own
// StatusCodeEnum carries FailureDueToFault (0x2, element :83-85), but
// nothing in the port says a refusal came from a valve fault, and
// claiming one would put a diagnosis on the wire that was never measured.
func (s *Server) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	if resp, handled, err := s.hooks.Invoke(ctx, cmdID, fields); handled {
		return resp, err
	}
	switch cmdID {
	case CmdOpen:
		req, err := openRequestFrom(fields)
		if err != nil {
			return nil, err
		}
		if err := s.resolveLevel(&req); err != nil {
			return nil, err
		}
		if s.src == nil {
			return nil, errNoSource("Open")
		}
		s.cmd.Lock()
		defer s.cmd.Unlock()
		at := s.autoCloseFor(req)
		if err := s.src.Open(ctx, req); err != nil {
			return nil, fmt.Errorf("valve: Open: %w", err)
		}
		s.setAutoClose(at)
		s.tracker().Bump()
		return nil, nil
	case CmdClose:
		if s.src == nil {
			return nil, errNoSource("Close")
		}
		s.cmd.Lock()
		defer s.cmd.Unlock()
		if err := s.src.Close(ctx); err != nil {
			return nil, fmt.Errorf("valve: Close: %w", err)
		}
		s.setAutoClose(nil)
		s.tracker().Bump()
		return nil, nil
	default:
		return nil, im.UnsupportedCommandf("valve: unknown command 0x%02X", cmdID)
	}
}

// resolveLevel applies the Level rules to an Open: without LVL a
// TargetLevel is refused ([errTargetLevel]); with it a TargetLevel that is
// not 100 or a multiple of LevelStep is CONSTRAINT_ERROR, and an absent
// one becomes DefaultOpenLevel where served, else 100 — the level the
// StateSource opens to. Mirrors connectedhomeip
// src/app/clusters/valve-configuration-and-control-server/ValveConfigurationAndControlCluster.cpp:312-341
// at 6170af84.
func (s *Server) resolveLevel(req *OpenRequest) error {
	if s.level == nil {
		if req.TargetLevel != nil {
			return errTargetLevel
		}
		return nil
	}
	if req.TargetLevel != nil {
		if !s.compliesWithLevelStep(*req.TargetLevel) {
			return constraintError{fmt.Sprintf("valve: Open TargetLevel %d is not 100 or a multiple of LevelStep %d", *req.TargetLevel, s.levelStep)}
		}
		return nil
	}
	level := maxLevel
	if s.defaults != nil {
		level = s.defaults.DefaultOpenLevel()
	}
	req.TargetLevel = &level
	return nil
}

// autoCloseFor is the AutoCloseTime an Open of req sets (TS): the node's
// Matter time plus the opening's duration — the request's OpenDuration,
// else DefaultOpenDuration — in epoch-us; nil without TS, for an opening
// without a duration, and while the node's time is not synchronized.
// Mirrors connectedhomeip
// src/app/clusters/valve-configuration-and-control-server/ValveConfigurationAndControlCluster.cpp:391-413
// at 6170af84 (SetAutoCloseTime), with the epoch-s the node's time carries.
func (s *Server) autoCloseFor(req OpenRequest) *uint64 {
	if !s.timeSync {
		return nil
	}
	var seconds uint32
	switch {
	case req.OpenDuration != nil && req.OpenDuration.Null:
		return nil
	case req.OpenDuration != nil:
		seconds = req.OpenDuration.Value
	default:
		d, known := s.src.DefaultOpenDuration()
		if !known {
			return nil
		}
		seconds = d
	}
	matterTime := s.matterTime.Load()
	if matterTime == nil {
		return nil
	}
	now, ok := (*matterTime)()
	if !ok {
		return nil
	}
	at := (uint64(now) + uint64(seconds)) * 1_000_000
	return &at
}

// AttachMatterTime hands the server the Matter time an Open's
// AutoCloseTime is set on with TS — the server's only input of it. A node
// calls it as the server joins its topology, with the root
// TimeSynchronization's UTC in epoch seconds
// (core.TimeSynchronization.MatterEpochSeconds, false while the node has
// no time): connectedhomeip's valve asks the node's TimeSynchronization
// whether the time is valid and reads the system clock SetUTCTime sets
// (ValveConfigurationAndControlCluster.cpp:391-400, CodegenIntegration.cpp:
// 46-58, TimeSynchronizationCluster.cpp:856-872 at 6170af84). A TS server
// outside a node has no Matter time until its host attaches one: every
// Open leaves AutoCloseTime null, where chip's Startup refuses TS without
// a time tracker (ValveConfigurationAndControlCluster.cpp:46-50). nil
// leaves the time unchanged.
func (s *Server) AttachMatterTime(matterTime clock.MatterTime) {
	if matterTime != nil {
		s.matterTime.Store(&matterTime)
	}
}

// MatterTimeChanged sets AutoCloseTime anew on the moved Matter time: the
// time now plus RemainingDuration, while the valve counts down
// (RemainingDuration known and not zero) and the time is synchronized; an
// opening that began without a time gets its AutoCloseTime here. A node
// calls it whenever its UTC moves; the host of a server outside a node
// calls it when its time does. Mirrors connectedhomeip's UpdateAutoCloseTime
// (ValveConfigurationAndControlCluster.cpp:415-423 at 6170af84), which the
// reference application's UTCTimeAvailabilityChanged calls
// (examples/all-clusters-app/linux/ValveControlDelegate.cpp:70-73), with
// the epoch-s the Matter time carries.
func (s *Server) MatterTimeChanged() {
	if !s.timeSync || s.src == nil {
		return
	}
	s.cmd.Lock()
	defer s.cmd.Unlock()
	matterTime := s.matterTime.Load()
	if matterTime == nil {
		return
	}
	now, ok := (*matterTime)()
	if !ok {
		return
	}
	left, known := s.src.RemainingDuration()
	if !known || left == 0 {
		return
	}
	at := (uint64(now) + uint64(left)) * 1_000_000
	s.setAutoClose(&at)
	s.tracker().Bump()
}

// RequiresMatterTime reports whether the server cannot be served without
// the node's Matter time: with TS, as connectedhomeip's Startup refuses TS
// without a time tracker (ValveConfigurationAndControlCluster.cpp:46-50
// at 6170af84) — a node without TimeSynchronization refuses such a valve.
func (s *Server) RequiresMatterTime() bool { return s.timeSync }

// setAutoClose records the AutoCloseTime of the last Open (nil: null).
func (s *Server) setAutoClose(at *uint64) {
	s.mu.Lock()
	s.autoClose = at
	s.mu.Unlock()
}

// openRequestFrom normalises the payload the bridge hands over. The
// bridge decodes the request through the generated definition into a
// [OpenRequest], with matter.js's field checks (OpenDuration "min
// 1", TargetLevel "1 to 100"); an in-process caller may hand over the
// tag-keyed map, and a parameterless Open arrives as nil.
func openRequestFrom(fields any) (OpenRequest, error) {
	switch v := fields.(type) {
	case nil:
		return OpenRequest{}, nil
	case OpenRequest:
		return openRequestFromGenerated(v)
	case *OpenRequest:
		if v == nil {
			return OpenRequest{}, nil
		}
		return openRequestFromGenerated(*v)
	case map[uint8]any:
		return openRequestFromTagMap(v)
	default:
		return OpenRequest{}, fmt.Errorf(
			"valve: Open expected valve.OpenRequest or map[uint8]any, got %T", fields,
		)
	}
}

// openRequestFromGenerated takes a decoded request as it is: an absent
// OpenDuration stays apart from a null one, and TargetLevel is
// [Server.resolveLevel]'s.
func openRequestFromGenerated(r OpenRequest) (OpenRequest, error) { return r, nil }

// errTargetLevel refuses an Open that carries TargetLevel on a valve
// without the Level feature: the field is conformance "[LVL]" (element
// :62). Ignoring it would open the valve to a level the controller never
// got told was impossible.
var errTargetLevel = constraintError{
	"valve: Open carries TargetLevel, which needs the Level feature this server does not advertise",
}

// openRequestFromTagMap reads the Open command's two fields out of the
// generic tag map. Unsigned TLV integers surface as uint64 there
// (bridge/fields_reader.go decodeGenericTagMap), and an explicit null
// keeps its tag with a nil value — which is how presence stays
// distinguishable from absence.
func openRequestFromTagMap(m map[uint8]any) (OpenRequest, error) {
	var req OpenRequest
	if raw, present := m[openFieldTargetLevel]; present {
		// percent, constraint "1 to 100" (element :62).
		level, ok := asUint32(raw)
		if !ok || level < 1 || level > uint32(maxLevel) {
			return req, constraintError{fmt.Sprintf("valve: Open TargetLevel violates constraint 1 to 100: %v", raw)}
		}
		l := uint8(level)
		req.TargetLevel = &l
	}
	raw, present := m[openFieldOpenDuration]
	if !present {
		return req, nil
	}
	if raw == nil {
		req.OpenDuration = &spec.Nullable[uint32]{Null: true}
		return req, nil
	}
	seconds, ok := asUint32(raw)
	if !ok {
		return req, constraintError{fmt.Sprintf("valve: Open OpenDuration expected a number, got %T", raw)}
	}
	if seconds < 1 {
		// constraint "min 1" (element :61).
		return req, constraintError{"valve: Open OpenDuration violates constraint min 1"}
	}
	req.OpenDuration = &spec.Nullable[uint32]{Value: seconds}
	return req, nil
}

// asUint32 narrows a decoded wire value to the uint32 an elapsed-s field
// carries. The bridge surfaces unsigned TLV integers as uint64 and
// signed ones as int64 (bridge/attribute_value_reader.go
// primitiveAttributeValue), so a strict type assertion on uint32 would
// reject every value that actually arrives. Out-of-range and negative
// values are rejected rather than wrapped.
func asUint32(v any) (uint32, bool) {
	const maxUint32 = int64(^uint32(0))
	var n int64
	switch x := v.(type) {
	case uint8:
		n = int64(x)
	case uint16:
		n = int64(x)
	case uint32:
		n = int64(x)
	case uint64:
		if x > uint64(maxUint32) {
			return 0, false
		}
		n = int64(x) //nolint:gosec // range-checked against uint32 max on the line above
	case int:
		n = int64(x)
	case int8:
		n = int64(x)
	case int16:
		n = int64(x)
	case int32:
		n = int64(x)
	case int64:
		n = x
	default:
		return 0, false
	}
	if n < 0 || n > maxUint32 {
		return 0, false
	}
	return uint32(n), true //nolint:gosec // range-checked against 0 and uint32 max above
}

// MatterReportable lists the attributes that move while the valve runs:
// the durations and states, and with the features AutoCloseTime and the
// two levels. DefaultOpenDuration and DefaultOpenLevel are absent: they
// change only through a write, which the dispatcher already reflects in
// the endpoint's DataVersion; LevelStep is fixed.
func (s *Server) MatterReportable() []uint32 {
	out := []uint32{AttrOpenDuration}
	if s.timeSync {
		out = append(out, valvedef.AttrAutoCloseTime)
	}
	out = append(out, AttrRemainingDuration, AttrCurrentState, AttrTargetState)
	if s.level != nil {
		out = append(out, valvedef.AttrCurrentLevel, valvedef.AttrTargetLevel)
	}
	return out
}

// MatterAttributes implements [contract.ClusterAttributeLister], in id
// order and without the universal globals — the dispatcher merges those.
func (s *Server) MatterAttributes() []uint32 { return s.inst.MatterAttributes() }

// MatterAcceptedCommands implements [contract.ClusterCommandLister].
func (s *Server) MatterAcceptedCommands() []uint32 { return s.inst.MatterAcceptedCommands() }

// MatterGeneratedCommands implements [contract.ClusterCommandLister].
// Open and Close both declare response "status", so the server emits no
// command payloads.
func (s *Server) MatterGeneratedCommands() []uint32 { return s.inst.MatterGeneratedCommands() }

// FeatureMap reports the advertised feature set.
func (s *Server) FeatureMap() uint32 { return s.inst.FeatureMap() }

// errNoSource reports that the server has no host port to reach.
func errNoSource(what string) error {
	return fmt.Errorf("valve: %s has no host port", what)
}

// constraintError maps onto the Matter ConstraintError status.
type constraintError struct{ msg string }

func (e constraintError) Error() string                 { return e.msg }
func (constraintError) MatterStatusCode() im.StatusCode { return im.StatusConstraintError }

// Compile-time assertion for the typed status carrier.
var _ im.StatusCodeError = constraintError{}

// OnMatterValueChanged implements [contract.ChangeNotifier] over the
// StateSource's: a change the host reports there moves the server's data
// version, then cb runs — the bridge hears a pull server's change through
// it. A StateSource that notifies nothing never calls cb.
func (s *Server) OnMatterValueChanged(cb func()) (unsubscribe func()) {
	n, ok := s.src.(contract.ChangeNotifier)
	if !ok || n == nil {
		return func() {}
	}
	return n.OnMatterValueChanged(func() {
		s.tracker().Bump()
		if cb != nil {
			cb()
		}
	})
}

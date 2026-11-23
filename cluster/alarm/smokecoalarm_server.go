// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package alarm contains the Matter SmokeCoAlarm cluster server (0x005C),
// the one mandatory application cluster of the SmokeCoAlarm device type
// (0x0076).
//
// The server owns no alarm state. The host reports its device as one
// [State] snapshot through a [StateSource]; the server projects it onto
// the cluster's attributes, derives ExpressedState from it, gates the
// SelfTestRequest command, and turns transitions between two snapshots
// into the events the cluster specification says "shall be generated".
//
// What matter.js does, and what this package adds. matter.js
// SmokeCoAlarmServer
// (packages/node/src/behaviors/smoke-co-alarm/SmokeCoAlarmServer.ts:19-43)
// only seeds defaults in initialize(): ExpressedState, SmokeState,
// CoState and BatteryAlert Normal, TestInProgress and HardwareFaultAlert
// false, EndOfServiceAlert Normal. Everything else — choosing the
// expressed condition, refusing a self-test while alarming, emitting the
// events — is left to the application. A bridge host is that
// application, but the rules are cluster rules, not device knowledge, so
// they live here and are recorded as an addition in
// notes/parity/by_design.md. Their source is the specification text
// matter.js itself carries in
// packages/model/src/standard/resources/smoke-co-alarm-cluster.resource.ts.
//
// The server instance is long-lived: events are emitted from the
// instance the bridge wired an emitter into at reassembly, so a host
// returns the same *Server from every MatterClusterServers call and
// calls [Server.Refresh] after each change to its device state.
//
// The cluster's identity is the generated definition (cluster/spec/
// smokecoalarm, ADR 0013): ids, revision, the enums, the feature
// selection check, the attribute, command and event lists the selection
// and the declared optionals yield, the event priorities, the write check
// and the write privileges. ExpressedState derivation, the SelfTestRequest
// gate and the event diffing stay here.
package alarm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"

	"thingfabric.dev/go-fabric/cluster"
	"thingfabric.dev/go-fabric/cluster/spec"
	sdef "thingfabric.dev/go-fabric/cluster/spec/smokecoalarm"
	clusterwire "thingfabric.dev/go-fabric/cluster/wire"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
)

// ClusterID is the SmokeCoAlarm cluster id.
const ClusterID = sdef.ClusterID

// DeviceTypeSmokeCoAlarm is the device type the cluster is mandatory on
// (matter.js smoke-co-alarm-device.element.ts).
const DeviceTypeSmokeCoAlarm uint16 = 0x0076

// Attribute ids (smoke-co-alarm-cluster.element.ts:27-46).
const (
	AttrExpressedState         = sdef.AttrExpressedState         // M
	AttrSmokeState             = sdef.AttrSmokeState             // SMOKE
	AttrCOState                = sdef.AttrCoState                // CO
	AttrBatteryAlert           = sdef.AttrBatteryAlert           // M
	AttrDeviceMuted            = sdef.AttrDeviceMuted            // O
	AttrTestInProgress         = sdef.AttrTestInProgress         // M
	AttrHardwareFaultAlert     = sdef.AttrHardwareFaultAlert     // M
	AttrEndOfServiceAlert      = sdef.AttrEndOfServiceAlert      // M
	AttrInterconnectSmokeAlarm = sdef.AttrInterconnectSmokeAlarm // O
	AttrInterconnectCOAlarm    = sdef.AttrInterconnectCoAlarm    // O
	AttrContaminationState     = sdef.AttrContaminationState     // [SMOKE]
	AttrSmokeSensitivityLevel  = sdef.AttrSmokeSensitivityLevel  // [SMOKE], RW VM
	AttrExpiryDate             = sdef.AttrExpiryDate             // O
	AttrUnmounted              = sdef.AttrUnmounted              // [Rev >= v2]
)

// Event ids (smoke-co-alarm-cluster.element.ts:47-72).
const (
	EventSmokeAlarm             = sdef.EventSmokeAlarm             // SMOKE, critical
	EventCOAlarm                = sdef.EventCoAlarm                // CO, critical
	EventLowBattery             = sdef.EventLowBattery             // M, info
	EventHardwareFault          = sdef.EventHardwareFault          // M, info
	EventEndOfService           = sdef.EventEndOfService           // M, info
	EventSelfTestComplete       = sdef.EventSelfTestComplete       // M, info
	EventAlarmMuted             = sdef.EventAlarmMuted             // O, info
	EventMuteEnded              = sdef.EventMuteEnded              // O, info
	EventInterconnectSmokeAlarm = sdef.EventInterconnectSmokeAlarm // [SMOKE], critical
	EventInterconnectCOAlarm    = sdef.EventInterconnectCoAlarm    // [CO], critical
	EventAllClear               = sdef.EventAllClear               // M, info
)

// CmdSelfTestRequest is the cluster's one command: conformance O,
// response "status" (smoke-co-alarm-cluster.element.ts:73).
const CmdSelfTestRequest = sdef.CmdSelfTestRequest

// Feature is a SmokeCoAlarm FeatureMap bit.
type Feature = sdef.Feature

// FeatureMap bits (smoke-co-alarm-cluster.element.ts:24-25). Both carry
// conformance "O.a+": at least one of the two is required.
const (
	FeatureSmokeAlarm = sdef.FeatureSmokeAlarm // SMOKE
	FeatureCOAlarm    = sdef.FeatureCoAlarm    // CO
)

// AlarmState is the AlarmStateEnum (element :75-80).
//
//nolint:revive // AlarmState mirrors the Matter AlarmStateEnum name verbatim.
type AlarmState = sdef.AlarmStateEnum

// AlarmStateEnum values.
const (
	AlarmNormal   = sdef.AlarmStateNormal
	AlarmWarning  = sdef.AlarmStateWarning
	AlarmCritical = sdef.AlarmStateCritical
)

// alarming reports whether the state is Warning or Critical — the two
// values every "changes to either Warning or Critical state" event rule
// is written against (resource :124, :136, :142).
func alarming(a AlarmState) bool { return a == AlarmWarning || a == AlarmCritical }

// Sensitivity is the SensitivityEnum (element :82-87).
type Sensitivity = sdef.SensitivityEnum

// SensitivityEnum values. Standard is the mandatory one.
const (
	SensitivityHigh     = sdef.SensitivityHigh
	SensitivityStandard = sdef.SensitivityStandard
	SensitivityLow      = sdef.SensitivityLow
)

// ExpressedState is the ExpressedStateEnum (element :89-101).
type ExpressedState = sdef.ExpressedStateEnum

// ExpressedStateEnum values.
const (
	ExpressedNormal            = sdef.ExpressedStateNormal
	ExpressedSmokeAlarm        = sdef.ExpressedStateSmokeAlarm // SMOKE
	ExpressedCOAlarm           = sdef.ExpressedStateCoAlarm    // CO
	ExpressedBatteryAlert      = sdef.ExpressedStateBatteryAlert
	ExpressedTesting           = sdef.ExpressedStateTesting
	ExpressedHardwareFault     = sdef.ExpressedStateHardwareFault
	ExpressedEndOfService      = sdef.ExpressedStateEndOfService
	ExpressedInterconnectSmoke = sdef.ExpressedStateInterconnectSmoke // O
	ExpressedInterconnectCO    = sdef.ExpressedStateInterconnectCo    // O
	ExpressedInoperative       = sdef.ExpressedStateInoperative       // [Rev >= v2]
)

// MuteState is the MuteStateEnum (element :103-107).
type MuteState = sdef.MuteStateEnum

// MuteStateEnum values.
const (
	NotMuted = sdef.MuteStateNotMuted
	Muted    = sdef.MuteStateMuted
)

// EndOfService is the EndOfServiceEnum (element :108-112).
type EndOfService = sdef.EndOfServiceEnum

// EndOfServiceEnum values.
const (
	EndOfServiceNormal  = sdef.EndOfServiceNormal
	EndOfServiceExpired = sdef.EndOfServiceExpired
)

// ContaminationState is the ContaminationStateEnum (element :114-120).
type ContaminationState = sdef.ContaminationStateEnum

// ContaminationStateEnum values.
const (
	ContaminationNormal   = sdef.ContaminationStateNormal
	ContaminationLow      = sdef.ContaminationStateLow
	ContaminationWarning  = sdef.ContaminationStateWarning
	ContaminationCritical = sdef.ContaminationStateCritical
)

// Optional names the optional attributes a host declares it serves.
// Each one is a promise: the attribute joins AttributeList and reads
// from the [State] the source reports.
type Optional uint32

// Optional attributes. The event rows each one brings with it follow the
// element: AlarmMuted / MuteEnded are "O" and ride with DeviceMuted, the
// two Interconnect events are "[SMOKE]" / "[CO]" and ride with their
// attribute while that feature is advertised.
const (
	// OptionalDeviceMuted serves DeviceMuted (0x4) and emits AlarmMuted
	// and MuteEnded.
	OptionalDeviceMuted Optional = 1 << iota
	// OptionalInterconnectSmokeAlarm serves InterconnectSmokeAlarm (0x8)
	// and, with SMOKE, emits the InterconnectSmokeAlarm event.
	OptionalInterconnectSmokeAlarm
	// OptionalInterconnectCOAlarm serves InterconnectCoAlarm (0x9) and,
	// with CO, emits the InterconnectCoAlarm event.
	OptionalInterconnectCOAlarm
	// OptionalContaminationState serves ContaminationState (0xA);
	// conformance "[SMOKE]", so it needs FeatureSmokeAlarm.
	OptionalContaminationState
	// OptionalSmokeSensitivityLevel serves the writable
	// SmokeSensitivityLevel (0xB); conformance "[SMOKE]", so it needs
	// FeatureSmokeAlarm and a source implementing [SensitivitySetter].
	OptionalSmokeSensitivityLevel
	// OptionalExpiryDate serves ExpiryDate (0xC).
	OptionalExpiryDate
	// OptionalUnmounted serves Unmounted (0xD); conformance
	// "[Rev >= v2]", optional at the revision matter.js HEAD ships.
	OptionalUnmounted
)

// optionalAttributes are the attributes each Optional bit declares.
var optionalAttributes = []struct {
	opt  Optional
	attr uint32
}{
	{OptionalDeviceMuted, AttrDeviceMuted},
	{OptionalInterconnectSmokeAlarm, AttrInterconnectSmokeAlarm},
	{OptionalInterconnectCOAlarm, AttrInterconnectCOAlarm},
	{OptionalContaminationState, AttrContaminationState},
	{OptionalSmokeSensitivityLevel, AttrSmokeSensitivityLevel},
	{OptionalExpiryDate, AttrExpiryDate},
	{OptionalUnmounted, AttrUnmounted},
}

// OptionalFor is the Optional set that serves attrIDs: each optional
// attribute among them sets its bit, every other id is ignored.
func OptionalFor(attrIDs []uint32) Optional {
	var o Optional
	for _, a := range optionalAttributes {
		if slices.Contains(attrIDs, a.attr) {
			o |= a.opt
		}
	}
	return o
}

// snapshot is one observation of the host's alarm, as the server reads
// the source's generated State ([snapshotOf]). Its zero value is the
// state matter.js SmokeCoAlarmBaseServer.initialize seeds
// (SmokeCoAlarmServer.ts:20-41) — everything Normal, nothing in progress
// or faulted — except SmokeSensitivityLevel, whose zero is High; a host
// that serves that attribute reports its real level.
type snapshot struct {
	SmokeState             AlarmState
	COState                AlarmState
	BatteryAlert           AlarmState
	DeviceMuted            MuteState
	TestInProgress         bool
	HardwareFaultAlert     bool
	EndOfServiceAlert      EndOfService
	InterconnectSmokeAlarm AlarmState
	InterconnectCOAlarm    AlarmState
	ContaminationState     ContaminationState
	SmokeSensitivityLevel  Sensitivity
	// ExpiryDate is epoch-s (seconds since 2000-01-01 UTC).
	ExpiryDate uint32
	Unmounted  bool
	// Inoperative reports that the sensor currently cannot detect smoke
	// or CO. The specification permits it only when BatteryAlert is
	// Critical, HardwareFaultAlert is true or Unmounted is true
	// (resource :315-326); the host is the one that knows whether the
	// device actually stopped detecting, so it says so here, and
	// ExpressedState then reads Inoperative.
	Inoperative bool
}

// StateSource is the host port. SmokeCOState is read on every attribute
// read and on every [Server.Refresh]; it must be cheap and must not
// block on the device.
//
// It is the generated port (smokecoalarmdef.StateSource): the source
// reports the generated State, and an ExpressedState of Inoperative in it
// is the host's "the sensor cannot detect" ([snapshot] Inoperative); any
// other ExpressedState is the server's to derive. A source that is a
// [contract.ChangeNotifier] has the server refresh — emit the events of
// the change — before each of its notifications reaches the bridge.
type StateSource = sdef.StateSource

// SelfTester is the optional capability that enables SelfTestRequest.
// A source without it leaves the command out of AcceptedCommandList, as
// matter.js does for an optional command it was given no implementation
// for. SelfTest starts the test and returns; the host then reports
// TestInProgress true until the test finishes, and the transition back
// to false is what emits SelfTestComplete.
type SelfTester = sdef.SelfTester

// SensitivitySetter is the capability [OptionalSmokeSensitivityLevel]
// requires: a write that reaches the device. A controller's write reaches
// it only when the written level differs from the one SmokeCOState reads
// at that moment; such a write counts as a change — its DataVersion
// advances and it is reported — whether or not SmokeCOState shows it when
// the setter returns, so a host may apply it later.
type SensitivitySetter = sdef.SensitivitySetter

// DefaultExpressedStatePriority is the order ExpressedState is derived
// in when the host gives none: the condition earliest in the list that
// holds is the one expressed. The specification leaves the order to the
// manufacturer (resource :27-30); this default is the order connectedhomeip's
// smoke-co-alarm example application passes to
// SmokeCoAlarmServer::SetExpressedStateByPriority — Inoperative first, then
// these eight in this order (examples/smoke-co-alarm-app/silabs/src/
// SmokeCoAlarmManager.cpp:32-36, telink/src/SmokeCoAlarmManager.cpp:31-35,
// at the harness pin 6170af8461b10b1766044122ac83332c6d00ab20). Inoperative is not in
// it: when the host reports [State.Inoperative] it is expressed ahead of
// everything, because an alarm that cannot detect has nothing else to
// say truthfully.
var DefaultExpressedStatePriority = []ExpressedState{
	ExpressedSmokeAlarm,
	ExpressedInterconnectSmoke,
	ExpressedCOAlarm,
	ExpressedInterconnectCO,
	ExpressedHardwareFault,
	ExpressedTesting,
	ExpressedEndOfService,
	ExpressedBatteryAlert,
}

// Config carries the construction parameters for [Server].
type Config struct {
	// Source is the host port; nil reads as the matter.js initial state.
	Source StateSource
	// Features is the FeatureMap. At least one of SMOKE and CO is
	// required (conformance "O.a+").
	Features Feature
	// Optional lists the optional attributes served.
	Optional Optional
	// ExpressedStatePriority overrides [DefaultExpressedStatePriority].
	// Every entry must be a condition the server can evaluate.
	ExpressedStatePriority []ExpressedState
	// DataVersion is an optional host-owned tracker, as on every server
	// here; nil uses an embedded one.
	//
	// Deprecated: a host-owned tracker adds nothing — the wire carries the
	// endpoint's DataVersion for the cluster, or the server's own where it
	// serves one (decision 165, F-db021da0-46); leave it nil. Removable in
	// v0.25.0.
	DataVersion *cluster.DataVersionTracker
}

// Server implements [contract.ClusterServer] for SmokeCoAlarm.
type Server struct {
	embedded cluster.DataVersionTracker
	ext      *cluster.DataVersionTracker

	src      StateSource
	features Feature
	optional Optional
	priority []ExpressedState
	inst     *spec.Instance
	// held routes MatterInvoke through [spec.InvokeHeld] — the request
	// admitted and the response checked with the server's attributes, a
	// host handler ([Server.Handle]) ahead of the server's own logic
	// (decision 154); events and veto are the server's.
	held   spec.SelfHeld
	events spec.Events
	veto   spec.WriteVeto

	// refresh serialises Refresh: held across reading the source, swapping
	// the baseline and handing the events over, so two overlapping calls
	// cannot swap in the older state last (R2-P6-conc-1503). A one-slot
	// channel, so that a waiter is durably blocked for a testing/synctest
	// bubble.
	refresh chan struct{}

	// mu guards emitter, endpoint, last and lastExpr.
	mu       sync.Mutex
	emitter  contract.EventEmitter
	endpoint uint16
	// last is the snapshot the previous Refresh saw; events are the
	// differences between it and the next one.
	last     snapshot
	lastExpr ExpressedState

	// admitted holds, per enum attribute, the last defined value the host
	// reported, served in place of an undefined one (R2-C2-wire-1553);
	// before any, the first value of the enum the instance admits.
	admitted map[uint32]*atomic.Uint32
}

// enumAttrs are the enum attributes [Server.MatterRead] holds to their
// enum on the way out.
var enumAttrs = []uint32{
	AttrSmokeState, AttrCOState, AttrBatteryAlert, AttrDeviceMuted, AttrEndOfServiceAlert,
	AttrInterconnectSmokeAlarm, AttrInterconnectCOAlarm, AttrContaminationState, AttrSmokeSensitivityLevel,
}

// Compile-time assertions.
var (
	_ contract.ClusterServer                  = (*Server)(nil)
	_ contract.ClusterDataVersion             = (*Server)(nil)
	_ contract.ClusterAttributeLister         = (*Server)(nil)
	_ contract.ClusterCommandLister           = (*Server)(nil)
	_ contract.ClusterEventLister             = (*Server)(nil)
	_ contract.EventReceiver                  = (*Server)(nil)
	_ contract.ClusterAttributeWritePrivilege = (*Server)(nil)
)

// Configuration errors.
var (
	// ErrNoAlarmFeature: neither SMOKE nor CO — the "O.a+" choice is
	// unsatisfied.
	ErrNoAlarmFeature = errors.New("alarm: SmokeCoAlarm needs at least one of SMOKE and CO")
	// ErrOptionalNeedsSmoke: ContaminationState or SmokeSensitivityLevel
	// without SMOKE; both are "[SMOKE]".
	ErrOptionalNeedsSmoke = errors.New("alarm: ContaminationState and SmokeSensitivityLevel need the SMOKE feature")
	// ErrSensitivityNotWritable: SmokeSensitivityLevel served by a source
	// that cannot apply a write to it.
	ErrSensitivityNotWritable = errors.New("alarm: SmokeSensitivityLevel needs a source implementing SensitivitySetter")
	// ErrPriorityEntry: a priority entry the server cannot evaluate for
	// this configuration.
	ErrPriorityEntry = errors.New("alarm: ExpressedStatePriority names a condition this configuration cannot express")
)

// NewServer validates cfg against the cluster's conformance and returns
// the server.
func NewServer(cfg Config) (*Server, error) {
	if err := spec.CheckFeatures(sdef.Definition, uint32(cfg.Features)); err != nil {
		if errors.Is(err, spec.ErrUnknownFeature) {
			return nil, fmt.Errorf("alarm: %w", err)
		}
		return nil, fmt.Errorf("%w: %w", ErrNoAlarmFeature, err)
	}
	if cfg.Optional&(OptionalContaminationState|OptionalSmokeSensitivityLevel) != 0 && cfg.Features&FeatureSmokeAlarm == 0 {
		return nil, ErrOptionalNeedsSmoke
	}
	if cfg.Optional&OptionalSmokeSensitivityLevel != 0 {
		if _, ok := cfg.Source.(SensitivitySetter); !ok {
			return nil, ErrSensitivityNotWritable
		}
	}
	s := &Server{
		refresh:  make(chan struct{}, 1),
		src:      cfg.Source,
		ext:      cfg.DataVersion,
		features: cfg.Features,
		optional: cfg.Optional,
		priority: DefaultExpressedStatePriority,
		lastExpr: ExpressedNormal,
	}
	srv, err := newHeld(sdef.Definition, instanceOptions(cfg), s.MatterRead, s.invoke)
	if err != nil {
		return nil, fmt.Errorf("alarm: %w", err)
	}
	s.inst, s.held = srv.Instance, spec.NewSelfHeld(srv)
	s.admitted = make(map[uint32]*atomic.Uint32, len(enumAttrs))
	for _, id := range enumAttrs {
		s.admitted[id] = new(atomic.Uint32)
		for v := range uint64(256) {
			if _, err := s.inst.CheckValue(id, v, nil); err == nil {
				s.admitted[id].Store(uint32(v))
				break
			}
		}
	}
	if cfg.ExpressedStatePriority != nil {
		for _, e := range cfg.ExpressedStatePriority {
			if !s.canExpress(e) || e == ExpressedNormal || e == ExpressedInoperative {
				return nil, fmt.Errorf("%w: %d", ErrPriorityEntry, e)
			}
		}
		s.priority = slices.Clone(cfg.ExpressedStatePriority)
	}
	return s, nil
}

// SetAttributes implements spec.AttributeSetter: every attribute is the
// StateSource's — the host changes the alarm there and notifies through
// the source's contract.ChangeNotifier ([spec.ErrNotHeld]).
func (*Server) SetAttributes(values map[uint32]any) error {
	for id := range values {
		return fmt.Errorf("%w: alarm: attribute 0x%04X is the StateSource's", spec.ErrNotHeld, id)
	}
	return nil
}

// Handle implements spec.CommandRegistrar ([spec.SelfHeld.Handle]).
func (s *Server) Handle(cmdID uint32, fn spec.CommandHandler) error { return s.held.Handle(cmdID, fn) }

// Emit implements spec.EventEmitting: an event of the EventList the host
// raises itself ([spec.Events.Emit]); the state's events are the
// server's ([Server.Refresh]).
func (s *Server) Emit(endpoint uint16, eventID uint32, data any) error {
	return s.events.Emit(s.inst, endpoint, eventID, data)
}

// SetBeforeWrite implements spec.BeforeWriteSetter: the veto of a
// controller's SmokeSensitivityLevel write, before the source takes it.
func (s *Server) SetBeforeWrite(fn func(ctx context.Context, attrID uint32, value any) error) {
	s.veto.Set(fn)
}

// The typed server's optional interfaces.
var (
	_ spec.AttributeSetter    = (*Server)(nil)
	_ spec.CommandRegistrar   = (*Server)(nil)
	_ spec.EventEmitting      = (*Server)(nil)
	_ spec.BeforeWriteSetter  = (*Server)(nil)
	_ contract.ChangeNotifier = (*Server)(nil)
)

// instanceOptions declares the optional elements cfg serves: the optional
// attributes, SelfTestRequest when the source can run one, AlarmMuted and
// MuteEnded with DeviceMuted, and each Interconnect event with its
// attribute while its feature is advertised ("[SMOKE]" / "[CO]").
func instanceOptions(cfg Config) spec.Options {
	opts := spec.Options{Features: uint32(cfg.Features)}
	for _, o := range optionalAttributes {
		if cfg.Optional&o.opt != 0 {
			opts.Attributes = append(opts.Attributes, o.attr)
		}
	}
	if _, ok := cfg.Source.(SelfTester); ok {
		opts.Commands = []uint32{CmdSelfTestRequest}
	}
	if cfg.Optional&OptionalDeviceMuted != 0 {
		opts.Events = append(opts.Events, EventAlarmMuted, EventMuteEnded)
	}
	if cfg.Features&FeatureSmokeAlarm != 0 && cfg.Optional&OptionalInterconnectSmokeAlarm != 0 {
		opts.Events = append(opts.Events, EventInterconnectSmokeAlarm)
	}
	if cfg.Features&FeatureCOAlarm != 0 && cfg.Optional&OptionalInterconnectCOAlarm != 0 {
		opts.Events = append(opts.Events, EventInterconnectCOAlarm)
	}
	return opts
}

// Revision returns the cluster revision of the generated definition.
func Revision() uint16 { return sdef.Revision }

func (s *Server) tracker() *cluster.DataVersionTracker {
	if s.ext != nil {
		return s.ext
	}
	return &s.embedded
}

func (s *Server) has(o Optional) bool { return s.optional&o != 0 }

func (s *Server) hasFeature(f Feature) bool { return s.features&f != 0 }

// canExpress reports whether ExpressedState may take e under the
// configured features and optional attributes: SmokeAlarm needs SMOKE,
// CoAlarm needs CO, the two Interconnect values need their attribute
// (element :89-101).
func (s *Server) canExpress(e ExpressedState) bool {
	switch e {
	case ExpressedSmokeAlarm:
		return s.hasFeature(FeatureSmokeAlarm)
	case ExpressedCOAlarm:
		return s.hasFeature(FeatureCOAlarm)
	case ExpressedInterconnectSmoke:
		return s.has(OptionalInterconnectSmokeAlarm)
	case ExpressedInterconnectCO:
		return s.has(OptionalInterconnectCOAlarm)
	case ExpressedNormal, ExpressedBatteryAlert, ExpressedTesting, ExpressedHardwareFault,
		ExpressedEndOfService, ExpressedInoperative:
		return true
	default:
		return false
	}
}

// state reads the host snapshot, or the matter.js initial state without
// a source.
func (s *Server) state() snapshot {
	if s.src == nil {
		return snapshot{SmokeSensitivityLevel: SensitivityStandard}
	}
	return snapshotOf(s.src.SmokeCOState())
}

// snapshotOf is the server's reading of the source's generated State.
func snapshotOf(st sdef.State) snapshot {
	return snapshot{
		SmokeState: st.SmokeState, COState: st.CoState, BatteryAlert: st.BatteryAlert, DeviceMuted: st.DeviceMuted,
		TestInProgress: st.TestInProgress, HardwareFaultAlert: st.HardwareFaultAlert, EndOfServiceAlert: st.EndOfServiceAlert,
		InterconnectSmokeAlarm: st.InterconnectSmokeAlarm, InterconnectCOAlarm: st.InterconnectCoAlarm,
		ContaminationState: st.ContaminationState, SmokeSensitivityLevel: st.SmokeSensitivityLevel,
		ExpiryDate: st.ExpiryDate, Unmounted: st.Unmounted, Inoperative: st.ExpressedState == ExpressedInoperative,
	}
}

// OnMatterValueChanged implements [contract.ChangeNotifier] over the
// source's: the server refreshes — emits the events of the change — and
// then cb runs. A source that notifies nothing never calls cb.
func (s *Server) OnMatterValueChanged(cb func()) (unsubscribe func()) {
	n, ok := s.src.(contract.ChangeNotifier)
	if !ok || cb == nil {
		return func() {}
	}
	return n.OnMatterValueChanged(func() {
		s.Refresh()
		cb()
	})
}

// holds reports whether the condition behind e is present in st —
// the "attribute corresponding to the value shall NOT be Normal" rule
// (resource :30-34) read from the other side.
func holds(e ExpressedState, st snapshot) bool {
	switch e {
	case ExpressedSmokeAlarm:
		return alarming(st.SmokeState)
	case ExpressedCOAlarm:
		return alarming(st.COState)
	case ExpressedBatteryAlert:
		return alarming(st.BatteryAlert)
	case ExpressedTesting:
		return st.TestInProgress
	case ExpressedHardwareFault:
		return st.HardwareFaultAlert
	case ExpressedEndOfService:
		return st.EndOfServiceAlert == EndOfServiceExpired
	case ExpressedInterconnectSmoke:
		return alarming(st.InterconnectSmokeAlarm)
	case ExpressedInterconnectCO:
		return alarming(st.InterconnectCOAlarm)
	default:
		return false
	}
}

// ExpressedStateOf derives ExpressedState from a snapshot: Inoperative
// when the host reports it, otherwise the first condition of the
// priority order that holds, otherwise Normal. Only conditions the
// configuration can express are considered, so a value the
// ExpressedStateEnum conformance forbids never reaches the wire.
func (s *Server) ExpressedStateOf(st sdef.State) ExpressedState {
	return s.expressedOf(snapshotOf(st))
}

// expressedOf is [Server.ExpressedStateOf] of the server's reading.
func (s *Server) expressedOf(st snapshot) ExpressedState {
	if st.Inoperative {
		return ExpressedInoperative
	}
	for _, e := range s.priority {
		if s.canExpress(e) && holds(e, st) {
			return e
		}
	}
	return ExpressedNormal
}

// MatterClusterID returns 0x005C.
func (*Server) MatterClusterID() uint32 { return ClusterID }

// MatterDataVersion implements [contract.ClusterDataVersion].
func (s *Server) MatterDataVersion() uint32 { return s.tracker().Current() }

// MatterRead resolves an attribute. Every attribute is non-nullable, so
// a value is always returned; an attribute this configuration does not
// serve reads as unsupported. A host enum value outside its enum is
// logged and the last defined one served instead (R2-C2-wire-1553).
func (s *Server) MatterRead(attrID uint32) (any, bool) {
	if v, ok := s.inst.ReadGlobal(attrID); ok {
		return v, true
	}
	if !s.inst.Serves(attrID) {
		return nil, false
	}
	st := s.state()
	switch attrID {
	case AttrExpressedState:
		return uint8(s.expressedOf(st)), true
	case AttrSmokeState:
		return s.admit(attrID, uint8(st.SmokeState)), true
	case AttrCOState:
		return s.admit(attrID, uint8(st.COState)), true
	case AttrBatteryAlert:
		return s.admit(attrID, uint8(st.BatteryAlert)), true
	case AttrDeviceMuted:
		return s.admit(attrID, uint8(st.DeviceMuted)), true
	case AttrTestInProgress:
		return st.TestInProgress, true
	case AttrHardwareFaultAlert:
		return st.HardwareFaultAlert, true
	case AttrEndOfServiceAlert:
		return s.admit(attrID, uint8(st.EndOfServiceAlert)), true
	case AttrInterconnectSmokeAlarm:
		return s.admit(attrID, uint8(st.InterconnectSmokeAlarm)), true
	case AttrInterconnectCOAlarm:
		return s.admit(attrID, uint8(st.InterconnectCOAlarm)), true
	case AttrContaminationState:
		return s.admit(attrID, uint8(st.ContaminationState)), true
	case AttrSmokeSensitivityLevel:
		return s.admit(attrID, uint8(st.SmokeSensitivityLevel)), true
	case AttrExpiryDate:
		return st.ExpiryDate, true
	default: // AttrUnmounted, the last one MatterAttributes can list
		return st.Unmounted, true
	}
}

// logger is the server's logger: the process default (Config carries
// none).
func (*Server) logger() *slog.Logger { return slog.Default() }

// admit is the host's enum value v of attrID when the instance admits it,
// remembered as the last admitted one; the last admitted one otherwise,
// with a warning (R2-C2-wire-1553).
func (s *Server) admit(attrID uint32, v uint8) uint8 {
	last := s.admitted[attrID]
	if _, err := s.inst.CheckValue(attrID, uint64(v), nil); err == nil {
		last.Store(uint32(v))
		return v
	}
	served := uint8(last.Load()) //nolint:gosec // a stored uint8
	s.logger().Warn("matter.smokecoalarm.inbound_value_not_admitted",
		slog.Int("attribute", int(attrID)), slog.Int("value", int(v)), slog.Int("served", int(served)))
	return served
}

// MatterWrite applies the one writable attribute, SmokeSensitivityLevel
// (access "RW VM"). The definition checks the write — served, writable, a
// SensitivityEnum member, as matter.js validates an enum write before any
// behavior runs and answers ConstraintError otherwise.
func (s *Server) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	_, err := s.MatterWriteChanged(ctx, attrID, value)
	return err
}

// MatterWriteChanged implements [contract.ChangeReportingWriter]: the
// write of [Server.MatterWrite], changed when it reached the host's
// setter. The written level, checked, is compared with the one the host's
// state holds before the host is called: the held level reaches no setter
// and answers unchanged; another is applied and answers changed,
// advancing the server's own version once, whatever the host's state
// reads when the setter returns. A host's setter is therefore called only
// for a level that differs from what its state read. matter.js notes a
// change, and runs a reactor, only for a value that differs from the held
// one (packages/node/src/behavior/state/managed/Datasource.ts:1476-1492 at
// db021da0, #noteIfChanged); its SmokeCoAlarmServer adds no behaviour to
// a write (packages/node/src/behaviors/smoke-co-alarm/
// SmokeCoAlarmServer.ts:18-42).
func (s *Server) MatterWriteChanged(ctx context.Context, attrID uint32, value any) (bool, error) {
	v, err := s.inst.ValidateWrite(attrID, value, nil)
	if err != nil {
		return false, err
	}
	n, _ := v.(uint64)      // an enum8 per ValidateWrite
	level := Sensitivity(n) //nolint:gosec // an enum8 per ValidateWrite
	if s.state().SmokeSensitivityLevel == level {
		return false, nil
	}
	// The host's veto hears a change only ([spec.WriteVeto.Check]).
	if err := s.veto.Check(ctx, attrID, spec.Stored(sdef.Definition.Attribute(attrID).Type, v)); err != nil {
		return false, err
	}
	setter, _ := s.src.(SensitivitySetter) // guaranteed by NewServer
	if err := setter.SetSmokeSensitivityLevel(ctx, level); err != nil {
		return false, fmt.Errorf("alarm: SmokeSensitivityLevel write: %w", err)
	}
	s.tracker().Bump()
	return true, nil
}

// MinWritePrivilege implements [contract.ClusterAttributeWritePrivilege]:
// SmokeSensitivityLevel is written with Manage ("RW VM").
func (s *Server) MinWritePrivilege(attrID uint32) uint8 { return s.inst.MinWritePrivilege(attrID) }

// selfTestBusy lists the ExpressedState values under which a self-test
// is refused with BUSY (resource :201-204).
var selfTestBusy = []ExpressedState{
	ExpressedSmokeAlarm, ExpressedCOAlarm, ExpressedTesting,
	ExpressedInterconnectSmoke, ExpressedInterconnectCO,
}

// MatterInvoke dispatches SelfTestRequest.
//
// The specification's rule (resource :201-204): while ExpressedState is
// SmokeAlarm, CoAlarm, Testing, InterconnectSmoke or InterconnectCO the
// device "shall NOT execute the self-test, and shall return status code
// BUSY". Otherwise the request reaches the host; on success the server
// refreshes, so the TestInProgress / Testing the host now reports is
// diffed from here on.
func (s *Server) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	if _, ok := s.src.(SelfTester); cmdID != CmdSelfTestRequest || !ok {
		return nil, im.UnsupportedCommandf("alarm: unknown command 0x%02X", cmdID)
	}
	return spec.InvokeHeld(ctx, &s.held, cmdID, fields)
}

// invoke is the server's own SelfTestRequest, which [spec.InvokeHeld] runs
// when the host registered no handler.
func (s *Server) invoke(ctx context.Context, _ uint32, _ any) (any, error) {
	tester, _ := s.src.(SelfTester) // MatterInvoke admitted only a SelfTester's
	if slices.Contains(selfTestBusy, s.expressedOf(s.state())) {
		return nil, statusError{im.StatusBusy, "alarm: SelfTestRequest while alarming or testing"}
	}
	if err := tester.SelfTest(ctx); err != nil {
		return nil, fmt.Errorf("alarm: SelfTestRequest: %w", err)
	}
	s.tracker().Bump()
	s.Refresh()
	return nil, nil
}

// MatterReportable lists every served attribute except the fixed
// ExpiryDate (quality F).
func (s *Server) MatterReportable() []uint32 { return s.inst.MatterReportable() }

// MatterAttributes implements [contract.ClusterAttributeLister]: the
// mandatory attributes, the feature-gated ones for the advertised
// features, and the declared optional ones, in id order.
func (s *Server) MatterAttributes() []uint32 { return s.inst.MatterAttributes() }

// MatterAcceptedCommands implements [contract.ClusterCommandLister]:
// SelfTestRequest when the source can run one.
func (s *Server) MatterAcceptedCommands() []uint32 { return s.inst.MatterAcceptedCommands() }

// MatterGeneratedCommands implements [contract.ClusterCommandLister];
// SelfTestRequest answers with a status.
func (s *Server) MatterGeneratedCommands() []uint32 { return s.inst.MatterGeneratedCommands() }

// MatterEvents implements [contract.ClusterEventLister] — the events
// whose conformance this configuration satisfies, in id order.
func (s *Server) MatterEvents() []uint32 { return s.inst.MatterEvents() }

// SetMatterEventEmitter implements [contract.EventReceiver].
func (s *Server) SetMatterEventEmitter(emitter contract.EventEmitter) {
	s.mu.Lock()
	s.emitter = emitter
	s.mu.Unlock()
	s.events.SetEmitter(emitter)
}

// SetEndpoint stamps the endpoint the events are addressed to; the
// bridge calls it at reassembly.
func (s *Server) SetEndpoint(endpoint uint16) {
	s.mu.Lock()
	s.endpoint = endpoint
	s.mu.Unlock()
	s.events.SetEndpoint(endpoint) // EmitEvent, the typed Record<Event>
}

// EmitEvent emits eventID on the endpoint the server is mounted on
// ([spec.Events.EmitEvent]) — the call the typed Record<Event> makes.
func (s *Server) EmitEvent(eventID uint32, data any) error {
	return s.events.EmitEvent(s.inst, eventID, data)
}

// AlarmSeverityEvent is the payload of the SmokeAlarm, CoAlarm,
// LowBattery, InterconnectSmokeAlarm and InterconnectCoAlarm events: one
// field, AlarmSeverityLevel (tag 0, AlarmStateEnum), carrying the new
// value of the attribute that triggered it (element :48-71,
// resource :127, :135, :146, :179, :189).
//
// It is the generated SmokeAlarm payload: the five events share one
// shape (field 0 AlarmSeverityLevel, AlarmStateEnum) and the generated
// SmokeAlarmEvent, CoAlarmEvent, LowBatteryEvent,
// InterconnectSmokeAlarmEvent and InterconnectCoAlarmEvent codecs are
// identical, so the bridge encodes it through the generated codec
// (spec.Encodable).
//
//nolint:revive // named after the event field it carries, AlarmSeverityLevel.
type AlarmSeverityEvent = sdef.SmokeAlarmEvent

// pendingEvent is one event a Refresh decided to emit.
type pendingEvent struct {
	id       uint32
	data     any
	priority contract.EventPriority
}

// Refresh reads the host's state and emits the events its change since
// the previous Refresh calls for. The baseline before the first Refresh
// is the matter.js initial state, so an alarm that is already sounding
// when the bridge starts is reported once. Overlapping calls run one
// after the other, each from its own read of the source, so the baseline
// never falls back to an older state (R2-P6-conc-1503); the source's
// SmokeCOState must not call Refresh itself.
//
// Each rule is the specification's, as matter.js carries it in
// smoke-co-alarm-cluster.resource.ts: SmokeAlarm / CoAlarm / LowBattery /
// the Interconnect events "when [the attribute] changes to either Warning
// or Critical" (:124, :136, :142, :175, :185); HardwareFault when
// HardwareFaultAlert becomes true (:152); EndOfService when
// EndOfServiceAlert is set to Expired (:157); SelfTestComplete when
// TestInProgress changes to false (:161); AlarmMuted / MuteEnded on the
// DeviceMuted flips (:166, :171); AllClear when ExpressedState returns to
// Normal (:195). Priorities are the element's, read from the definition.
//
// Without an emitter the baseline still advances: a change nobody could
// be told about is not replayed later as if it had just happened.
func (s *Server) Refresh() {
	s.refresh <- struct{}{}
	defer func() { <-s.refresh }()
	st := s.state()
	expr := s.expressedOf(st)

	s.mu.Lock()
	prev, prevExpr := s.last, s.lastExpr
	s.last, s.lastExpr = st, expr
	emitter, endpoint := s.emitter, s.endpoint
	s.mu.Unlock()

	events := s.transitions(prev, st, prevExpr, expr)
	if emitter == nil {
		return
	}
	for _, ev := range events {
		emitter.MatterEmitEvent(endpoint, ClusterID, ev.id, ev.data, ev.priority)
	}
}

// transitions lists the events between two snapshots, in event-id order
// with AllClear last.
func (s *Server) transitions(prev, cur snapshot, prevExpr, curExpr ExpressedState) []pendingEvent {
	var out []pendingEvent
	severity := func(id uint32, was, now AlarmState) {
		if now != was && alarming(now) {
			out = append(out, pendingEvent{id, AlarmSeverityEvent{AlarmSeverityLevel: now}, s.inst.EventPriority(id)})
		}
	}
	fieldless := func(id uint32, fire bool) {
		if fire {
			out = append(out, pendingEvent{id, clusterwire.FieldlessEvent{}, s.inst.EventPriority(id)})
		}
	}
	if s.hasFeature(FeatureSmokeAlarm) {
		severity(EventSmokeAlarm, prev.SmokeState, cur.SmokeState)
	}
	if s.hasFeature(FeatureCOAlarm) {
		severity(EventCOAlarm, prev.COState, cur.COState)
	}
	severity(EventLowBattery, prev.BatteryAlert, cur.BatteryAlert)
	fieldless(EventHardwareFault, !prev.HardwareFaultAlert && cur.HardwareFaultAlert)
	fieldless(EventEndOfService, prev.EndOfServiceAlert != EndOfServiceExpired && cur.EndOfServiceAlert == EndOfServiceExpired)
	fieldless(EventSelfTestComplete, prev.TestInProgress && !cur.TestInProgress)
	if s.has(OptionalDeviceMuted) {
		fieldless(EventAlarmMuted, prev.DeviceMuted != Muted && cur.DeviceMuted == Muted)
		fieldless(EventMuteEnded, prev.DeviceMuted == Muted && cur.DeviceMuted == NotMuted)
	}
	if s.hasFeature(FeatureSmokeAlarm) && s.has(OptionalInterconnectSmokeAlarm) {
		severity(EventInterconnectSmokeAlarm, prev.InterconnectSmokeAlarm, cur.InterconnectSmokeAlarm)
	}
	if s.hasFeature(FeatureCOAlarm) && s.has(OptionalInterconnectCOAlarm) {
		severity(EventInterconnectCOAlarm, prev.InterconnectCOAlarm, cur.InterconnectCOAlarm)
	}
	fieldless(EventAllClear, prevExpr != ExpressedNormal && curExpr == ExpressedNormal)
	return out
}

// statusError carries an exact IM status to the dispatcher.
type statusError struct {
	status im.StatusCode
	msg    string
}

func (e statusError) Error() string                   { return e.msg }
func (e statusError) MatterStatusCode() im.StatusCode { return e.status }

var _ im.StatusCodeError = statusError{}

// The host methods the typed server's Host returns (brief CK-2).
var _ sdef.Host = (*Server)(nil)

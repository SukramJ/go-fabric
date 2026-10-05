// SPDX-License-Identifier: MIT
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
package alarm

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/SukramJ/go-fabric/cluster"
	clusterwire "github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/schema"
)

// ClusterID is the SmokeCoAlarm cluster id
// (matter.js smoke-co-alarm-cluster.element.ts:20).
const ClusterID uint32 = 0x005C

// DeviceTypeSmokeCoAlarm is the device type the cluster is mandatory on
// (matter.js smoke-co-alarm-device.element.ts).
const DeviceTypeSmokeCoAlarm uint16 = 0x0076

// Attribute ids (smoke-co-alarm-cluster.element.ts:27-46).
const (
	AttrExpressedState         uint32 = 0x0000 // M
	AttrSmokeState             uint32 = 0x0001 // SMOKE
	AttrCOState                uint32 = 0x0002 // CO
	AttrBatteryAlert           uint32 = 0x0003 // M
	AttrDeviceMuted            uint32 = 0x0004 // O
	AttrTestInProgress         uint32 = 0x0005 // M
	AttrHardwareFaultAlert     uint32 = 0x0006 // M
	AttrEndOfServiceAlert      uint32 = 0x0007 // M
	AttrInterconnectSmokeAlarm uint32 = 0x0008 // O
	AttrInterconnectCOAlarm    uint32 = 0x0009 // O
	AttrContaminationState     uint32 = 0x000A // [SMOKE]
	AttrSmokeSensitivityLevel  uint32 = 0x000B // [SMOKE], RW VM
	AttrExpiryDate             uint32 = 0x000C // O
	AttrUnmounted              uint32 = 0x000D // [Rev >= v2]
)

// Event ids (smoke-co-alarm-cluster.element.ts:47-72).
const (
	EventSmokeAlarm             uint32 = 0x00 // SMOKE, critical
	EventCOAlarm                uint32 = 0x01 // CO, critical
	EventLowBattery             uint32 = 0x02 // M, info
	EventHardwareFault          uint32 = 0x03 // M, info
	EventEndOfService           uint32 = 0x04 // M, info
	EventSelfTestComplete       uint32 = 0x05 // M, info
	EventAlarmMuted             uint32 = 0x06 // O, info
	EventMuteEnded              uint32 = 0x07 // O, info
	EventInterconnectSmokeAlarm uint32 = 0x08 // [SMOKE], critical
	EventInterconnectCOAlarm    uint32 = 0x09 // [CO], critical
	EventAllClear               uint32 = 0x0A // M, info
)

// CmdSelfTestRequest is the cluster's one command: conformance O,
// response "status" (smoke-co-alarm-cluster.element.ts:73).
const CmdSelfTestRequest uint32 = 0x00

// Feature is a SmokeCoAlarm FeatureMap bit.
type Feature uint32

// FeatureMap bits (smoke-co-alarm-cluster.element.ts:24-25). Both carry
// conformance "O.a+": at least one of the two is required.
const (
	FeatureSmokeAlarm Feature = 1 << 0 // SMOKE
	FeatureCOAlarm    Feature = 1 << 1 // CO
)

// AlarmState is the AlarmStateEnum (element :75-80).
//
//nolint:revive // AlarmState mirrors the Matter AlarmStateEnum name verbatim.
type AlarmState uint8

// AlarmStateEnum values.
const (
	AlarmNormal   AlarmState = 0
	AlarmWarning  AlarmState = 1
	AlarmCritical AlarmState = 2
)

// alarming reports whether the state is Warning or Critical — the two
// values every "changes to either Warning or Critical state" event rule
// is written against (resource :124, :136, :142).
func (a AlarmState) alarming() bool { return a == AlarmWarning || a == AlarmCritical }

// Sensitivity is the SensitivityEnum (element :82-87).
type Sensitivity uint8

// SensitivityEnum values. Standard is the mandatory one.
const (
	SensitivityHigh     Sensitivity = 0
	SensitivityStandard Sensitivity = 1
	SensitivityLow      Sensitivity = 2
)

// ExpressedState is the ExpressedStateEnum (element :89-101).
type ExpressedState uint8

// ExpressedStateEnum values.
const (
	ExpressedNormal            ExpressedState = 0
	ExpressedSmokeAlarm        ExpressedState = 1 // SMOKE
	ExpressedCOAlarm           ExpressedState = 2 // CO
	ExpressedBatteryAlert      ExpressedState = 3
	ExpressedTesting           ExpressedState = 4
	ExpressedHardwareFault     ExpressedState = 5
	ExpressedEndOfService      ExpressedState = 6
	ExpressedInterconnectSmoke ExpressedState = 7 // O
	ExpressedInterconnectCO    ExpressedState = 8 // O
	ExpressedInoperative       ExpressedState = 9 // [Rev >= v2]
)

// MuteState is the MuteStateEnum (element :103-107).
type MuteState uint8

// MuteStateEnum values.
const (
	NotMuted MuteState = 0
	Muted    MuteState = 1
)

// EndOfService is the EndOfServiceEnum (element :108-112).
type EndOfService uint8

// EndOfServiceEnum values.
const (
	EndOfServiceNormal  EndOfService = 0
	EndOfServiceExpired EndOfService = 1
)

// ContaminationState is the ContaminationStateEnum (element :114-120).
type ContaminationState uint8

// ContaminationStateEnum values.
const (
	ContaminationNormal   ContaminationState = 0
	ContaminationLow      ContaminationState = 1
	ContaminationWarning  ContaminationState = 2
	ContaminationCritical ContaminationState = 3
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

// State is one observation of the host's alarm. Its zero value is the
// state matter.js SmokeCoAlarmBaseServer.initialize seeds
// (SmokeCoAlarmServer.ts:20-41) — everything Normal, nothing in progress
// or faulted — except SmokeSensitivityLevel, whose zero is High; a host
// that serves that attribute reports its real level.
type State struct {
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
type StateSource interface {
	SmokeCOState() State
}

// SelfTester is the optional capability that enables SelfTestRequest.
// A source without it leaves the command out of AcceptedCommandList, as
// matter.js does for an optional command it was given no implementation
// for. SelfTest starts the test and returns; the host then reports
// TestInProgress true until the test finishes, and the transition back
// to false is what emits SelfTestComplete.
type SelfTester interface {
	SelfTest(ctx context.Context) error
}

// SensitivitySetter is the capability [OptionalSmokeSensitivityLevel]
// requires: a write that reaches the device.
type SensitivitySetter interface {
	SetSmokeSensitivityLevel(ctx context.Context, level Sensitivity) error
}

// DefaultExpressedStatePriority is the order ExpressedState is derived
// in when the host gives none: the condition earliest in the list that
// holds is the one expressed. The specification leaves the order to the
// manufacturer (resource :27-30); this default is the order connectedhomeip's
// smoke-co-alarm example application passes to
// SmokeCoAlarmServer::SetExpressedStateByPriority. Inoperative is not in
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

	mu       sync.Mutex
	emitter  contract.EventEmitter
	endpoint uint16
	// last is the snapshot the previous Refresh saw; events are the
	// differences between it and the next one.
	last     State
	lastExpr ExpressedState
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
	if cfg.Features&(FeatureSmokeAlarm|FeatureCOAlarm) == 0 {
		return nil, ErrNoAlarmFeature
	}
	if cfg.Features&^(FeatureSmokeAlarm|FeatureCOAlarm) != 0 {
		return nil, fmt.Errorf("alarm: unknown feature bits 0x%X", uint32(cfg.Features&^(FeatureSmokeAlarm|FeatureCOAlarm)))
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
		src:      cfg.Source,
		ext:      cfg.DataVersion,
		features: cfg.Features,
		optional: cfg.Optional,
		priority: DefaultExpressedStatePriority,
		lastExpr: ExpressedNormal,
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

// Revision returns the cluster revision from the generated schema.
func Revision() uint16 { return schema.ClusterRevisions[ClusterID] }

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
func (s *Server) state() State {
	if s.src == nil {
		return State{SmokeSensitivityLevel: SensitivityStandard}
	}
	return s.src.SmokeCOState()
}

// holds reports whether the condition behind e is present in st —
// the "attribute corresponding to the value shall NOT be Normal" rule
// (resource :30-34) read from the other side.
func holds(e ExpressedState, st State) bool {
	switch e {
	case ExpressedSmokeAlarm:
		return st.SmokeState.alarming()
	case ExpressedCOAlarm:
		return st.COState.alarming()
	case ExpressedBatteryAlert:
		return st.BatteryAlert.alarming()
	case ExpressedTesting:
		return st.TestInProgress
	case ExpressedHardwareFault:
		return st.HardwareFaultAlert
	case ExpressedEndOfService:
		return st.EndOfServiceAlert == EndOfServiceExpired
	case ExpressedInterconnectSmoke:
		return st.InterconnectSmokeAlarm.alarming()
	case ExpressedInterconnectCO:
		return st.InterconnectCOAlarm.alarming()
	default:
		return false
	}
}

// ExpressedStateOf derives ExpressedState from a snapshot: Inoperative
// when the host reports it, otherwise the first condition of the
// priority order that holds, otherwise Normal. Only conditions the
// configuration can express are considered, so a value the
// ExpressedStateEnum conformance forbids never reaches the wire.
func (s *Server) ExpressedStateOf(st State) ExpressedState {
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
// serve reads as unsupported.
func (s *Server) MatterRead(attrID uint32) (any, bool) {
	switch attrID {
	case cluster.AttrGlobalFeatureMap:
		return uint32(s.features), true
	case cluster.AttrGlobalClusterRevision:
		return Revision(), true
	}
	if !slices.Contains(s.MatterAttributes(), attrID) {
		return nil, false
	}
	st := s.state()
	switch attrID {
	case AttrExpressedState:
		return uint8(s.ExpressedStateOf(st)), true
	case AttrSmokeState:
		return uint8(st.SmokeState), true
	case AttrCOState:
		return uint8(st.COState), true
	case AttrBatteryAlert:
		return uint8(st.BatteryAlert), true
	case AttrDeviceMuted:
		return uint8(st.DeviceMuted), true
	case AttrTestInProgress:
		return st.TestInProgress, true
	case AttrHardwareFaultAlert:
		return st.HardwareFaultAlert, true
	case AttrEndOfServiceAlert:
		return uint8(st.EndOfServiceAlert), true
	case AttrInterconnectSmokeAlarm:
		return uint8(st.InterconnectSmokeAlarm), true
	case AttrInterconnectCOAlarm:
		return uint8(st.InterconnectCOAlarm), true
	case AttrContaminationState:
		return uint8(st.ContaminationState), true
	case AttrSmokeSensitivityLevel:
		return uint8(st.SmokeSensitivityLevel), true
	case AttrExpiryDate:
		return st.ExpiryDate, true
	default: // AttrUnmounted, the last one MatterAttributes can list
		return st.Unmounted, true
	}
}

// MatterWrite applies the one writable attribute, SmokeSensitivityLevel
// (access "RW VM", element :44). Its value must be a SensitivityEnum
// member — matter.js validates an enum write against the enum's values
// before any behavior runs and answers ConstraintError otherwise.
func (s *Server) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	if !slices.Contains(s.MatterAttributes(), attrID) {
		return statusError{im.StatusUnsupportedAttribute, fmt.Sprintf("alarm: attribute 0x%04X is not served", attrID)}
	}
	if attrID != AttrSmokeSensitivityLevel {
		return statusError{im.StatusUnsupportedWrite, fmt.Sprintf("alarm: attribute 0x%04X is read-only", attrID)}
	}
	n, ok := cluster.AsUintMax(value, uint64(SensitivityLow))
	if !ok {
		return statusError{im.StatusConstraintError, fmt.Sprintf("alarm: SmokeSensitivityLevel %v is not a SensitivityEnum value", value)}
	}
	setter, _ := s.src.(SensitivitySetter) // guaranteed by NewServer
	level := Sensitivity(n)                //nolint:gosec // range-checked against SensitivityLow by AsUintMax above
	if err := setter.SetSmokeSensitivityLevel(ctx, level); err != nil {
		return fmt.Errorf("alarm: SmokeSensitivityLevel write: %w", err)
	}
	s.tracker().Bump()
	return nil
}

// MinWritePrivilege implements [contract.ClusterAttributeWritePrivilege]:
// SmokeSensitivityLevel is written with Manage ("RW VM").
func (*Server) MinWritePrivilege(attrID uint32) uint8 {
	if attrID == AttrSmokeSensitivityLevel {
		return 4 // Manage
	}
	return 3 // Operate
}

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
func (s *Server) MatterInvoke(ctx context.Context, cmdID uint32, _ any) (any, error) {
	tester, ok := s.src.(SelfTester)
	if cmdID != CmdSelfTestRequest || !ok {
		return nil, im.UnsupportedCommandf("alarm: unknown command 0x%02X", cmdID)
	}
	if slices.Contains(selfTestBusy, s.ExpressedStateOf(s.state())) {
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
func (s *Server) MatterReportable() []uint32 {
	attrs := s.MatterAttributes()
	return slices.DeleteFunc(attrs, func(id uint32) bool { return id == AttrExpiryDate })
}

// MatterAttributes implements [contract.ClusterAttributeLister]: the
// mandatory attributes, the feature-gated ones for the advertised
// features, and the declared optional ones, in id order.
func (s *Server) MatterAttributes() []uint32 {
	out := []uint32{AttrExpressedState}
	if s.hasFeature(FeatureSmokeAlarm) {
		out = append(out, AttrSmokeState)
	}
	if s.hasFeature(FeatureCOAlarm) {
		out = append(out, AttrCOState)
	}
	out = append(out, AttrBatteryAlert)
	if s.has(OptionalDeviceMuted) {
		out = append(out, AttrDeviceMuted)
	}
	out = append(out, AttrTestInProgress, AttrHardwareFaultAlert, AttrEndOfServiceAlert)
	for _, o := range []struct {
		opt  Optional
		attr uint32
	}{
		{OptionalInterconnectSmokeAlarm, AttrInterconnectSmokeAlarm},
		{OptionalInterconnectCOAlarm, AttrInterconnectCOAlarm},
		{OptionalContaminationState, AttrContaminationState},
		{OptionalSmokeSensitivityLevel, AttrSmokeSensitivityLevel},
		{OptionalExpiryDate, AttrExpiryDate},
		{OptionalUnmounted, AttrUnmounted},
	} {
		if s.has(o.opt) {
			out = append(out, o.attr)
		}
	}
	return out
}

// MatterAcceptedCommands implements [contract.ClusterCommandLister]:
// SelfTestRequest when the source can run one.
func (s *Server) MatterAcceptedCommands() []uint32 {
	if _, ok := s.src.(SelfTester); ok {
		return []uint32{CmdSelfTestRequest}
	}
	return []uint32{}
}

// MatterGeneratedCommands implements [contract.ClusterCommandLister];
// SelfTestRequest answers with a status.
func (*Server) MatterGeneratedCommands() []uint32 { return []uint32{} }

// MatterEvents implements [contract.ClusterEventLister] — the events
// whose conformance this configuration satisfies, in id order.
func (s *Server) MatterEvents() []uint32 {
	var out []uint32
	if s.hasFeature(FeatureSmokeAlarm) {
		out = append(out, EventSmokeAlarm)
	}
	if s.hasFeature(FeatureCOAlarm) {
		out = append(out, EventCOAlarm)
	}
	out = append(out, EventLowBattery, EventHardwareFault, EventEndOfService, EventSelfTestComplete)
	if s.has(OptionalDeviceMuted) {
		out = append(out, EventAlarmMuted, EventMuteEnded)
	}
	if s.hasFeature(FeatureSmokeAlarm) && s.has(OptionalInterconnectSmokeAlarm) {
		out = append(out, EventInterconnectSmokeAlarm)
	}
	if s.hasFeature(FeatureCOAlarm) && s.has(OptionalInterconnectCOAlarm) {
		out = append(out, EventInterconnectCOAlarm)
	}
	return append(out, EventAllClear)
}

// SetMatterEventEmitter implements [contract.EventReceiver].
func (s *Server) SetMatterEventEmitter(emitter contract.EventEmitter) {
	s.mu.Lock()
	s.emitter = emitter
	s.mu.Unlock()
}

// SetEndpoint stamps the endpoint the events are addressed to; the
// bridge calls it at reassembly.
func (s *Server) SetEndpoint(endpoint uint16) {
	s.mu.Lock()
	s.endpoint = endpoint
	s.mu.Unlock()
}

// AlarmSeverityEvent is the payload of the SmokeAlarm, CoAlarm,
// LowBattery, InterconnectSmokeAlarm and InterconnectCoAlarm events: one
// field, AlarmSeverityLevel (tag 0, AlarmStateEnum), carrying the new
// value of the attribute that triggered it (element :48-71,
// resource :127, :135, :146, :179, :189).
//
//nolint:revive // named after the event field it carries, AlarmSeverityLevel.
type AlarmSeverityEvent struct {
	AlarmSeverityLevel AlarmState
}

// pendingEvent is one event a Refresh decided to emit.
type pendingEvent struct {
	id       uint32
	data     any
	priority contract.EventPriority
}

// Refresh reads the host's state and emits the events its change since
// the previous Refresh calls for. The baseline before the first Refresh
// is the matter.js initial state, so an alarm that is already sounding
// when the bridge starts is reported once.
//
// Each rule is the specification's, as matter.js carries it in
// smoke-co-alarm-cluster.resource.ts: SmokeAlarm / CoAlarm / LowBattery /
// the Interconnect events "when [the attribute] changes to either Warning
// or Critical" (:124, :136, :142, :175, :185); HardwareFault when
// HardwareFaultAlert becomes true (:152); EndOfService when
// EndOfServiceAlert is set to Expired (:157); SelfTestComplete when
// TestInProgress changes to false (:161); AlarmMuted / MuteEnded on the
// DeviceMuted flips (:166, :171); AllClear when ExpressedState returns to
// Normal (:195). Priorities are the element's.
//
// Without an emitter the baseline still advances: a change nobody could
// be told about is not replayed later as if it had just happened.
func (s *Server) Refresh() {
	st := s.state()
	expr := s.ExpressedStateOf(st)

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
func (s *Server) transitions(prev, cur State, prevExpr, curExpr ExpressedState) []pendingEvent {
	var out []pendingEvent
	severity := func(id uint32, was, now AlarmState, priority contract.EventPriority) {
		if now != was && now.alarming() {
			out = append(out, pendingEvent{id, AlarmSeverityEvent{AlarmSeverityLevel: now}, priority})
		}
	}
	fieldless := func(id uint32, fire bool) {
		if fire {
			out = append(out, pendingEvent{id, clusterwire.FieldlessEvent{}, contract.EventPriorityInfo})
		}
	}
	if s.hasFeature(FeatureSmokeAlarm) {
		severity(EventSmokeAlarm, prev.SmokeState, cur.SmokeState, contract.EventPriorityCritical)
	}
	if s.hasFeature(FeatureCOAlarm) {
		severity(EventCOAlarm, prev.COState, cur.COState, contract.EventPriorityCritical)
	}
	severity(EventLowBattery, prev.BatteryAlert, cur.BatteryAlert, contract.EventPriorityInfo)
	fieldless(EventHardwareFault, !prev.HardwareFaultAlert && cur.HardwareFaultAlert)
	fieldless(EventEndOfService, prev.EndOfServiceAlert != EndOfServiceExpired && cur.EndOfServiceAlert == EndOfServiceExpired)
	fieldless(EventSelfTestComplete, prev.TestInProgress && !cur.TestInProgress)
	if s.has(OptionalDeviceMuted) {
		fieldless(EventAlarmMuted, prev.DeviceMuted != Muted && cur.DeviceMuted == Muted)
		fieldless(EventMuteEnded, prev.DeviceMuted == Muted && cur.DeviceMuted == NotMuted)
	}
	if s.hasFeature(FeatureSmokeAlarm) && s.has(OptionalInterconnectSmokeAlarm) {
		severity(EventInterconnectSmokeAlarm, prev.InterconnectSmokeAlarm, cur.InterconnectSmokeAlarm, contract.EventPriorityCritical)
	}
	if s.hasFeature(FeatureCOAlarm) && s.has(OptionalInterconnectCOAlarm) {
		severity(EventInterconnectCOAlarm, prev.InterconnectCOAlarm, cur.InterconnectCOAlarm, contract.EventPriorityCritical)
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

// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package fan contains the Matter FanControl cluster server (0x0202), the
// mandatory application cluster of the Fan (0x002B), AirPurifier (0x002D)
// and ExtractorHood (0x007A) device types.
//
// The server owns no fan state. The host reports its fan as one [State]
// snapshot and applies changes through [StateSource.ApplyFanSettings];
// the server validates every write against the cluster's constraints and
// resolves the coupling between FanMode, PercentSetting and SpeedSetting
// before the host sees it, so a host receives one consistent [Settings]
// per write instead of having to re-derive the rules.
//
// What matter.js does, and what this package adds. matter.js
// FanControlServer
// (packages/node/src/behaviors/fan-control/FanControlServer.ts:13-19)
// only defaults FanMode to Off in initialize(); the generated behavior
// validates types, enum members and the "max 100" / "max speedMax"
// constraints, and everything else is the application's. The coupling
// rules are specification text — matter.js carries it in
// packages/model/src/standard/resources/fan-control.resource.ts — and
// they are the same for every fan, so they live here; notes/parity/by_design.md
// records the addition and its sources. The Percent Rules (§4.4.6.3.1)
// and Speed Rules (§4.4.6.6.1) the resource refers to are not in the
// matter.js tree; the two formulas used for them are the ones
// connectedhomeip's fan-control-server applies, and are flagged as such
// in notes/parity/matter_behaviour_findings.md until they are checked
// against a connectedhomeip checkout.
package fan

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/SukramJ/go-fabric/cluster"
	clusterwire "github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/schema"
)

// ClusterID is the FanControl cluster id.
const ClusterID = clusterwire.FanControlClusterID

// Device types that mandate FanControl (schema/devicetypes.go, from
// matter.js fan.element.ts, air-purifier.element.ts,
// extractor-hood.element.ts).
const (
	DeviceTypeFan           uint16 = 0x002B
	DeviceTypeAirPurifier   uint16 = 0x002D
	DeviceTypeExtractorHood uint16 = 0x007A
)

// Attribute ids (fan-control.element.ts:32-67).
const (
	AttrFanMode          uint32 = 0x0000 // M, RW VO
	AttrFanModeSequence  uint32 = 0x0001 // M, fixed
	AttrPercentSetting   uint32 = 0x0002 // M, RW VO, nullable, max 100
	AttrPercentCurrent   uint32 = 0x0003 // M, max 100
	AttrSpeedMax         uint32 = 0x0004 // SPD, fixed, 1 to 100
	AttrSpeedSetting     uint32 = 0x0005 // SPD, RW VO, nullable, max speedMax
	AttrSpeedCurrent     uint32 = 0x0006 // SPD, max speedMax
	AttrRockSupport      uint32 = 0x0007 // RCK, fixed, min 1
	AttrRockSetting      uint32 = 0x0008 // RCK, RW VO
	AttrWindSupport      uint32 = 0x0009 // WND, fixed, min 1
	AttrWindSetting      uint32 = 0x000A // WND, RW VO
	AttrAirflowDirection uint32 = 0x000B // DIR, RW VO
)

// CmdStep is the Step command (STEP feature).
const CmdStep = clusterwire.FanControlCmdStep

// Feature is a FanControl FeatureMap bit (fan-control.element.ts:22-30).
type Feature uint32

// FeatureMap bits; all six are conformance "O".
const (
	FeatureMultiSpeed       Feature = 1 << 0 // SPD
	FeatureAuto             Feature = 1 << 1 // AUT
	FeatureRocking          Feature = 1 << 2 // RCK
	FeatureWind             Feature = 1 << 3 // WND
	FeatureStep             Feature = 1 << 4 // STEP
	FeatureAirflowDirection Feature = 1 << 5 // DIR
)

const allFeatures = FeatureMultiSpeed | FeatureAuto | FeatureRocking | FeatureWind | FeatureStep | FeatureAirflowDirection

// FanMode is the FanModeEnum (fan-control.element.ts:99-108).
//
//nolint:revive // FanMode mirrors the Matter FanModeEnum and the FanMode attribute verbatim.
type FanMode uint8

// FanModeEnum values. On and Smart are deprecated (conformance "D").
const (
	FanModeOff    FanMode = 0
	FanModeLow    FanMode = 1
	FanModeMedium FanMode = 2
	FanModeHigh   FanMode = 3
	FanModeOn     FanMode = 4
	FanModeAuto   FanMode = 5
	FanModeSmart  FanMode = 6
)

// Sequence is the FanModeSequenceEnum (fan-control.element.ts:110-118).
type Sequence uint8

// FanModeSequenceEnum values. The three without Auto carry "[!AUT].a",
// the three with it "[AUT].b".
const (
	SequenceOffLowMedHigh     Sequence = 0
	SequenceOffLowHigh        Sequence = 1
	SequenceOffLowMedHighAuto Sequence = 2
	SequenceOffLowHighAuto    Sequence = 3
	SequenceOffHighAuto       Sequence = 4
	SequenceOffHigh           Sequence = 5
)

// RockBitmap is the RockBitmap (fan-control.element.ts:76-81).
type RockBitmap uint8

// RockBitmap bits.
const (
	RockLeftRight RockBitmap = 1 << 0
	RockUpDown    RockBitmap = 1 << 1
	RockRound     RockBitmap = 1 << 2
	rockAll                  = RockLeftRight | RockUpDown | RockRound
)

// WindBitmap is the WindBitmap (fan-control.element.ts:83-87).
type WindBitmap uint8

// WindBitmap bits.
const (
	WindSleep   WindBitmap = 1 << 0
	WindNatural WindBitmap = 1 << 1
	windAll                = WindSleep | WindNatural
)

// AirflowDirection is the AirflowDirectionEnum
// (fan-control.element.ts:93-97).
type AirflowDirection uint8

// AirflowDirectionEnum values.
const (
	AirflowForward AirflowDirection = 0
	AirflowReverse AirflowDirection = 1
)

// StepDirectionEnum values (fan-control.element.ts:88-92).
const (
	StepIncrease uint8 = 0
	StepDecrease uint8 = 1
)

// State is one observation of the host's fan.
type State struct {
	FanMode FanMode
	// PercentSetting is nil for null — the value FanMode Auto requires
	// (resource :82-84).
	PercentSetting *uint8
	PercentCurrent uint8
	// SpeedSetting is nil for null; read only with MultiSpeed.
	SpeedSetting     *uint8
	SpeedCurrent     uint8
	RockSetting      RockBitmap
	WindSetting      WindBitmap
	AirflowDirection AirflowDirection
}

// Setting is a resolved PercentSetting or SpeedSetting: a value, or the
// attribute's null.
type Setting struct {
	Null  bool
	Value uint8
}

// Settings is what one FanMode, PercentSetting or SpeedSetting write — or
// one Step — resolves to under the coupling rules. A nil field is one
// the rules do not determine: the device's own mapping decides it, and
// the host reports the outcome in its next [State].
//
//   - FanMode Off   → PercentSetting 0, SpeedSetting 0.
//   - FanMode Auto  → PercentSetting null, SpeedSetting null
//     (resource :82-84, :120-122).
//   - FanMode Low / Medium / High → both nil: which percentage a mode
//     means is the manufacturer's.
//   - PercentSetting p → SpeedSetting ceil(SpeedMax × p / 100); FanMode
//     Off when p is 0, nil otherwise.
//   - SpeedSetting s → PercentSetting floor(s × 100 / SpeedMax); FanMode
//     Off when s is 0, nil otherwise.
type Settings struct {
	FanMode        *FanMode
	PercentSetting *Setting
	// SpeedSetting is always nil without MultiSpeed.
	SpeedSetting *Setting
}

// StateSource is the host port. FanState is read on every attribute read
// and must not block on the device; ApplyFanSettings carries every
// speed-oriented change to it.
//
// A host that cannot honour a change in the fan's current state returns
// [ErrInvalidInState] (or an error wrapping it): the specification's
// answer for "the fan is not in a state where this attribute can be
// changed to the requested value" (resource :53-55, :87-88, :126-128).
type StateSource interface {
	FanState() State
	ApplyFanSettings(ctx context.Context, s Settings) error
}

// RockSetter applies a RockSetting write; required with Rocking.
type RockSetter interface {
	SetRockSetting(ctx context.Context, rock RockBitmap) error
}

// WindSetter applies a WindSetting write; required with Wind.
type WindSetter interface {
	SetWindSetting(ctx context.Context, wind WindBitmap) error
}

// AirflowDirectionSetter applies an AirflowDirection write; required with
// AirflowDirection.
type AirflowDirectionSetter interface {
	SetAirflowDirection(ctx context.Context, dir AirflowDirection) error
}

// Stepper is the optional capability that takes the Step command over
// entirely. How a step maps onto the speed-oriented attributes "is
// implementation specific" (resource :214-215); a host with its own
// idea implements Stepper, and one without gets the server's default
// (see [Server.MatterInvoke]).
type Stepper interface {
	Step(ctx context.Context, req clusterwire.FanStepRequest) error
}

// Config carries the construction parameters for [Server].
type Config struct {
	Source   StateSource
	Features Feature
	// Sequence is FanModeSequence (fixed). It must be one of the three
	// sequences with Auto when FeatureAuto is set and one of the three
	// without it otherwise.
	Sequence Sequence
	// SpeedMax is required with MultiSpeed, 1 to 100.
	SpeedMax uint8
	// RockSupport is required with Rocking: at least one bit.
	RockSupport RockBitmap
	// WindSupport is required with Wind: at least one bit.
	WindSupport WindBitmap
	// DeviceType, when set, is checked against the device type's own
	// restrictions on the cluster: ExtractorHood excludes RCK, WND and
	// DIR (extractor-hood.element.ts).
	DeviceType uint16
	// DataVersion is an optional host-owned tracker.
	DataVersion *cluster.DataVersionTracker
}

// Configuration errors.
var (
	ErrNoSource          = errors.New("fan: FanControl needs a StateSource")
	ErrUnknownFeature    = errors.New("fan: unknown FeatureMap bits")
	ErrSequence          = errors.New("fan: FanModeSequence does not match the Auto feature")
	ErrSpeedMax          = errors.New("fan: MultiSpeed needs SpeedMax 1 to 100")
	ErrRockSupport       = errors.New("fan: Rocking needs a RockSupport with at least one known bit and a RockSetter")
	ErrWindSupport       = errors.New("fan: Wind needs a WindSupport with at least one known bit and a WindSetter")
	ErrAirflowDirection  = errors.New("fan: AirflowDirection needs an AirflowDirectionSetter")
	ErrDeviceTypeFeature = errors.New("fan: the device type excludes a configured feature")
)

// ErrInvalidInState is what a host returns when the fan cannot take a
// change in its current state; it reaches the controller as
// INVALID_IN_STATE.
var ErrInvalidInState error = statusError{im.StatusInvalidInState, "fan: not in a state where the change can be applied"}

// Server implements [contract.ClusterServer] for FanControl.
type Server struct {
	embedded cluster.DataVersionTracker
	ext      *cluster.DataVersionTracker
	src      StateSource
	cfg      Config
}

// Compile-time assertions.
var (
	_ contract.ClusterServer          = (*Server)(nil)
	_ contract.ClusterDataVersion     = (*Server)(nil)
	_ contract.ClusterAttributeLister = (*Server)(nil)
	_ contract.ClusterCommandLister   = (*Server)(nil)
)

// NewServer validates cfg against the cluster's conformance and
// constraints and returns the server.
func NewServer(cfg Config) (*Server, error) {
	if cfg.Source == nil {
		return nil, ErrNoSource
	}
	if cfg.Features&^allFeatures != 0 {
		return nil, fmt.Errorf("%w: 0x%X", ErrUnknownFeature, uint32(cfg.Features&^allFeatures))
	}
	withAuto := []Sequence{SequenceOffLowMedHighAuto, SequenceOffLowHighAuto, SequenceOffHighAuto}
	withoutAuto := []Sequence{SequenceOffLowMedHigh, SequenceOffLowHigh, SequenceOffHigh}
	if (cfg.Features&FeatureAuto != 0 && !slices.Contains(withAuto, cfg.Sequence)) ||
		(cfg.Features&FeatureAuto == 0 && !slices.Contains(withoutAuto, cfg.Sequence)) {
		return nil, fmt.Errorf("%w: sequence %d", ErrSequence, cfg.Sequence)
	}
	if cfg.Features&FeatureMultiSpeed != 0 && (cfg.SpeedMax < 1 || cfg.SpeedMax > 100) {
		return nil, ErrSpeedMax
	}
	if cfg.Features&FeatureRocking != 0 {
		if _, ok := cfg.Source.(RockSetter); !ok || cfg.RockSupport == 0 || cfg.RockSupport&^rockAll != 0 {
			return nil, ErrRockSupport
		}
	}
	if cfg.Features&FeatureWind != 0 {
		if _, ok := cfg.Source.(WindSetter); !ok || cfg.WindSupport == 0 || cfg.WindSupport&^windAll != 0 {
			return nil, ErrWindSupport
		}
	}
	if cfg.Features&FeatureAirflowDirection != 0 {
		if _, ok := cfg.Source.(AirflowDirectionSetter); !ok {
			return nil, ErrAirflowDirection
		}
	}
	if cfg.DeviceType == DeviceTypeExtractorHood && cfg.Features&(FeatureRocking|FeatureWind|FeatureAirflowDirection) != 0 {
		return nil, ErrDeviceTypeFeature
	}
	return &Server{src: cfg.Source, ext: cfg.DataVersion, cfg: cfg}, nil
}

// Revision returns the cluster revision from the generated schema.
func Revision() uint16 { return schema.ClusterRevisions[ClusterID] }

func (s *Server) tracker() *cluster.DataVersionTracker {
	if s.ext != nil {
		return s.ext
	}
	return &s.embedded
}

func (s *Server) has(f Feature) bool { return s.cfg.Features&f != 0 }

// MatterClusterID returns 0x0202.
func (*Server) MatterClusterID() uint32 { return ClusterID }

// MatterDataVersion implements [contract.ClusterDataVersion].
func (s *Server) MatterDataVersion() uint32 { return s.tracker().Current() }

// MatterAttributes implements [contract.ClusterAttributeLister]: the four
// mandatory attributes and those of each advertised feature, in id order.
func (s *Server) MatterAttributes() []uint32 {
	out := []uint32{AttrFanMode, AttrFanModeSequence, AttrPercentSetting, AttrPercentCurrent}
	if s.has(FeatureMultiSpeed) {
		out = append(out, AttrSpeedMax, AttrSpeedSetting, AttrSpeedCurrent)
	}
	if s.has(FeatureRocking) {
		out = append(out, AttrRockSupport, AttrRockSetting)
	}
	if s.has(FeatureWind) {
		out = append(out, AttrWindSupport, AttrWindSetting)
	}
	if s.has(FeatureAirflowDirection) {
		out = append(out, AttrAirflowDirection)
	}
	return out
}

// MatterReportable lists the served attributes that move; the fixed ones
// (FanModeSequence, SpeedMax, RockSupport, WindSupport — quality F) are
// left out.
func (s *Server) MatterReportable() []uint32 {
	fixed := []uint32{AttrFanModeSequence, AttrSpeedMax, AttrRockSupport, AttrWindSupport}
	return slices.DeleteFunc(s.MatterAttributes(), func(id uint32) bool { return slices.Contains(fixed, id) })
}

// MatterAcceptedCommands implements [contract.ClusterCommandLister].
func (s *Server) MatterAcceptedCommands() []uint32 {
	if s.has(FeatureStep) {
		return []uint32{CmdStep}
	}
	return []uint32{}
}

// MatterGeneratedCommands implements [contract.ClusterCommandLister]; Step
// answers with a status.
func (*Server) MatterGeneratedCommands() []uint32 { return []uint32{} }

// MatterRead resolves an attribute. Host values are clamped to the
// attribute constraints on the way out, so a device reporting 120 % or a
// speed above SpeedMax cannot put a constraint violation on the wire.
func (s *Server) MatterRead(attrID uint32) (any, bool) {
	switch attrID {
	case cluster.AttrGlobalFeatureMap:
		return uint32(s.cfg.Features), true
	case cluster.AttrGlobalClusterRevision:
		return Revision(), true
	}
	if !slices.Contains(s.MatterAttributes(), attrID) {
		return nil, false
	}
	st := s.src.FanState()
	switch attrID {
	case AttrFanMode:
		return uint8(st.FanMode), true
	case AttrFanModeSequence:
		return uint8(s.cfg.Sequence), true
	case AttrPercentSetting:
		return nullableCapped(st.PercentSetting, 100)
	case AttrPercentCurrent:
		return min(st.PercentCurrent, 100), true
	case AttrSpeedMax:
		return s.cfg.SpeedMax, true
	case AttrSpeedSetting:
		return nullableCapped(st.SpeedSetting, s.cfg.SpeedMax)
	case AttrSpeedCurrent:
		return min(st.SpeedCurrent, s.cfg.SpeedMax), true
	case AttrRockSupport:
		return uint8(s.cfg.RockSupport), true
	case AttrRockSetting:
		return uint8(st.RockSetting & s.cfg.RockSupport), true
	case AttrWindSupport:
		return uint8(s.cfg.WindSupport), true
	case AttrWindSetting:
		return uint8(st.WindSetting & s.cfg.WindSupport), true
	default: // AttrAirflowDirection, the last one MatterAttributes can list
		return uint8(st.AirflowDirection), true
	}
}

func nullableCapped(v *uint8, ceiling uint8) (any, bool) {
	if v == nil {
		return nil, true
	}
	return min(*v, ceiling), true
}

// MatterWrite applies a write to one of the six writable attributes
// (access "RW VO", fan-control.element.ts:32-67).
func (s *Server) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	if !slices.Contains(s.MatterAttributes(), attrID) {
		return statusError{im.StatusUnsupportedAttribute, fmt.Sprintf("fan: attribute 0x%04X is not served", attrID)}
	}
	var err error
	switch attrID {
	case AttrFanMode:
		err = s.writeFanMode(ctx, value)
	case AttrPercentSetting:
		err = s.writePercentSetting(ctx, value)
	case AttrSpeedSetting:
		err = s.writeSpeedSetting(ctx, value)
	case AttrRockSetting:
		err = s.writeRockSetting(ctx, value)
	case AttrWindSetting:
		err = s.writeWindSetting(ctx, value)
	case AttrAirflowDirection:
		err = s.writeAirflowDirection(ctx, value)
	default:
		return statusError{im.StatusUnsupportedWrite, fmt.Sprintf("fan: attribute 0x%04X is read-only", attrID)}
	}
	if err != nil {
		return err
	}
	s.tracker().Bump()
	return nil
}

// supports reports whether FanMode m is one the FanModeSequence offers:
// Off and High always, Low "if and only if the FanModeSequence attribute
// value is less than 4", Medium "if and only if [it] is 0 or 2"
// (resource :48-51), Auto with the three Auto sequences.
func (s *Server) supports(m FanMode) bool {
	switch m {
	case FanModeOff, FanModeHigh:
		return true
	case FanModeLow:
		return s.cfg.Sequence < SequenceOffHighAuto
	case FanModeMedium:
		return s.cfg.Sequence == SequenceOffLowMedHigh || s.cfg.Sequence == SequenceOffLowMedHighAuto
	case FanModeAuto:
		return s.has(FeatureAuto)
	default:
		return false
	}
}

// resolveFanMode validates a written FanMode and maps the two deprecated
// values the way connectedhomeip's fan-control-server does: On becomes
// High, Smart becomes Auto where the sequence offers Auto and High
// otherwise. A value outside the enum, or one the sequence does not
// offer, is CONSTRAINT_ERROR (resource :62-63).
func (s *Server) resolveFanMode(value any) (FanMode, error) {
	n, ok := cluster.AsUintMax(value, uint64(FanModeSmart))
	if !ok {
		return 0, statusError{im.StatusConstraintError, fmt.Sprintf("fan: FanMode %v is not a FanModeEnum value", value)}
	}
	m := FanMode(n) //nolint:gosec // range-checked against FanModeSmart by AsUintMax above
	switch m {
	case FanModeOn:
		m = FanModeHigh
	case FanModeSmart:
		m = FanModeHigh
		if s.supports(FanModeAuto) {
			m = FanModeAuto
		}
	case FanModeOff, FanModeLow, FanModeMedium, FanModeHigh, FanModeAuto:
		// Current values pass through to the sequence check.
	}
	if !s.supports(m) {
		return 0, statusError{im.StatusConstraintError, fmt.Sprintf("fan: FanMode %d is not offered by FanModeSequence %d", m, s.cfg.Sequence)}
	}
	return m, nil
}

// fanModeSettings is what a FanMode resolves to.
func (s *Server) fanModeSettings(m FanMode) Settings {
	out := Settings{FanMode: &m}
	switch m {
	case FanModeOff:
		out.PercentSetting = &Setting{Value: 0}
		if s.has(FeatureMultiSpeed) {
			out.SpeedSetting = &Setting{Value: 0}
		}
	case FanModeAuto:
		out.PercentSetting = &Setting{Null: true}
		if s.has(FeatureMultiSpeed) {
			out.SpeedSetting = &Setting{Null: true}
		}
	case FanModeLow, FanModeMedium, FanModeHigh, FanModeOn, FanModeSmart:
		// The percentage a speed mode means is the manufacturer's: both
		// settings stay nil for the device's own mapping.
	}
	return out
}

// percentSettings is what a PercentSetting p resolves to: with
// MultiSpeed, SpeedSetting = ceil(SpeedMax × p / 100); FanMode Off at 0.
func (s *Server) percentSettings(p uint8) Settings {
	out := Settings{PercentSetting: &Setting{Value: p}}
	if p == 0 {
		off := FanModeOff
		out.FanMode = &off
	}
	if s.has(FeatureMultiSpeed) {
		speed := uint8((uint16(s.cfg.SpeedMax)*uint16(p) + 99) / 100) //nolint:gosec // p ≤ 100 and SpeedMax ≤ 100: at most SpeedMax
		out.SpeedSetting = &Setting{Value: speed}
	}
	return out
}

// speedSettings is what a SpeedSetting v resolves to: PercentSetting =
// floor(v × 100 / SpeedMax); FanMode Off at 0.
func (s *Server) speedSettings(v uint8) Settings {
	percent := uint8(uint16(v) * 100 / uint16(s.cfg.SpeedMax)) //nolint:gosec // v ≤ SpeedMax: at most 100
	out := Settings{PercentSetting: &Setting{Value: percent}, SpeedSetting: &Setting{Value: v}}
	if v == 0 {
		off := FanModeOff
		out.FanMode = &off
	}
	return out
}

func (s *Server) apply(ctx context.Context, what string, set Settings) error {
	if err := s.src.ApplyFanSettings(ctx, set); err != nil {
		return fmt.Errorf("fan: %s: %w", what, err)
	}
	return nil
}

func (s *Server) writeFanMode(ctx context.Context, value any) error {
	m, err := s.resolveFanMode(value)
	if err != nil {
		return err
	}
	return s.apply(ctx, "FanMode write", s.fanModeSettings(m))
}

// writePercentSetting: a null write leaves the attribute unchanged
// ("If a client writes null to this attribute, the attribute value shall
// NOT change", resource :86-87) and succeeds; a value above 100 violates
// "max 100" (element :39).
func (s *Server) writePercentSetting(ctx context.Context, value any) error {
	if value == nil {
		return nil
	}
	n, ok := cluster.AsUintMax(value, 100)
	if !ok {
		return statusError{im.StatusConstraintError, fmt.Sprintf("fan: PercentSetting %v violates max 100", value)}
	}
	return s.apply(ctx, "PercentSetting write", s.percentSettings(uint8(n))) //nolint:gosec // ≤ 100 per AsUintMax
}

// writeSpeedSetting mirrors writePercentSetting against "max speedMax"
// (element :48).
func (s *Server) writeSpeedSetting(ctx context.Context, value any) error {
	if value == nil {
		return nil
	}
	n, ok := cluster.AsUintMax(value, uint64(s.cfg.SpeedMax))
	if !ok {
		return statusError{im.StatusConstraintError, fmt.Sprintf("fan: SpeedSetting %v violates max speedMax %d", value, s.cfg.SpeedMax)}
	}
	return s.apply(ctx, "SpeedSetting write", s.speedSettings(uint8(n))) //nolint:gosec // ≤ SpeedMax per AsUintMax
}

// writeRockSetting: "Each bit shall only be set to 1, if the
// corresponding bit in the RockSupport attribute is set to 1, otherwise a
// status code of CONSTRAINT_ERROR shall be returned" (resource :162-164).
// Picking the lowest bit of a supported-but-impossible combination
// (:166-168) needs to know which combinations the device runs, so the
// host does that.
func (s *Server) writeRockSetting(ctx context.Context, value any) error {
	n, ok := cluster.AsUintMax(value, 0xFF)
	if !ok || RockBitmap(n)&^s.cfg.RockSupport != 0 { //nolint:gosec // ≤ 0xFF per AsUintMax
		return statusError{im.StatusConstraintError, fmt.Sprintf("fan: RockSetting %v outside RockSupport 0x%02X", value, s.cfg.RockSupport)}
	}
	setter, _ := s.src.(RockSetter)                                   // guaranteed by NewServer
	if err := setter.SetRockSetting(ctx, RockBitmap(n)); err != nil { //nolint:gosec // ≤ 0xFF per AsUintMax
		return fmt.Errorf("fan: RockSetting write: %w", err)
	}
	return nil
}

// writeWindSetting is writeRockSetting for WindSetting (resource :186-188).
func (s *Server) writeWindSetting(ctx context.Context, value any) error {
	n, ok := cluster.AsUintMax(value, 0xFF)
	if !ok || WindBitmap(n)&^s.cfg.WindSupport != 0 { //nolint:gosec // ≤ 0xFF per AsUintMax
		return statusError{im.StatusConstraintError, fmt.Sprintf("fan: WindSetting %v outside WindSupport 0x%02X", value, s.cfg.WindSupport)}
	}
	setter, _ := s.src.(WindSetter)                                   // guaranteed by NewServer
	if err := setter.SetWindSetting(ctx, WindBitmap(n)); err != nil { //nolint:gosec // ≤ 0xFF per AsUintMax
		return fmt.Errorf("fan: WindSetting write: %w", err)
	}
	return nil
}

// writeAirflowDirection: "shall be set to one of the values in the
// AirflowDirectionEnum table" (resource :202-204).
func (s *Server) writeAirflowDirection(ctx context.Context, value any) error {
	n, ok := cluster.AsUintMax(value, uint64(AirflowReverse))
	if !ok {
		return statusError{im.StatusConstraintError, fmt.Sprintf("fan: AirflowDirection %v is not an AirflowDirectionEnum value", value)}
	}
	setter, _ := s.src.(AirflowDirectionSetter)                                  // guaranteed by NewServer
	if err := setter.SetAirflowDirection(ctx, AirflowDirection(n)); err != nil { //nolint:gosec // ≤ 1 per AsUintMax
		return fmt.Errorf("fan: AirflowDirection write: %w", err)
	}
	return nil
}

// MatterInvoke dispatches Step.
//
// A [Stepper] host decides what a step means. Without one the server
// steps itself, along the axis the fan exposes: with MultiSpeed one unit
// of SpeedSetting (0 or 1 … SpeedMax), resolved as a SpeedSetting write;
// without it one rung of the FanModeSequence's speed modes (Off, Low,
// Medium, High as offered), resolved as a FanMode write — the reading
// the specification's own example gives (resource :219-224). Off is a
// rung only with LowestOff; Wrap carries a step past either end round
// to the other, and without it the step stops at the end.
func (s *Server) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	if cmdID != CmdStep || !s.has(FeatureStep) {
		return nil, im.UnsupportedCommandf("fan: unknown command 0x%02X", cmdID)
	}
	req, err := stepRequestFrom(fields)
	if err != nil {
		return nil, err
	}
	if stepper, ok := s.src.(Stepper); ok {
		if err := stepper.Step(ctx, req); err != nil {
			return nil, fmt.Errorf("fan: Step: %w", err)
		}
		s.tracker().Bump()
		return nil, nil
	}
	st := s.src.FanState()
	var set Settings
	if s.has(FeatureMultiSpeed) {
		current := st.SpeedCurrent
		if st.SpeedSetting != nil {
			current = *st.SpeedSetting
		}
		set = s.speedSettings(stepValue(current, req, s.cfg.SpeedMax))
	} else {
		set = s.fanModeSettings(s.stepMode(st.FanMode, req))
	}
	if err := s.apply(ctx, "Step", set); err != nil {
		return nil, err
	}
	s.tracker().Bump()
	return nil, nil
}

// stepValue moves current one unit along [lowest, ceiling].
func stepValue(current uint8, req clusterwire.FanStepRequest, ceiling uint8) uint8 {
	lowest := uint8(1)
	if req.LowestOff {
		lowest = 0
	}
	current = min(max(current, lowest), ceiling)
	if req.Direction == StepIncrease {
		if current < ceiling {
			return current + 1
		}
		if req.Wrap {
			return lowest
		}
		return ceiling
	}
	if current > lowest {
		return current - 1
	}
	if req.Wrap {
		return ceiling
	}
	return lowest
}

// stepMode moves one rung along the sequence's speed modes. Off below the
// ladder and Auto above it are positions, not rungs, unless LowestOff
// makes Off one.
func (s *Server) stepMode(current FanMode, req clusterwire.FanStepRequest) FanMode {
	var rungs []FanMode
	for _, m := range []FanMode{FanModeOff, FanModeLow, FanModeMedium, FanModeHigh} {
		if (m != FanModeOff || req.LowestOff) && s.supports(m) {
			rungs = append(rungs, m)
		}
	}
	last := len(rungs) - 1
	pos := slices.Index(rungs, current)
	if pos < 0 {
		// Off (not a rung) sits below the first rung; Auto and the
		// deprecated values above the last.
		pos = -1
		if current != FanModeOff {
			pos = len(rungs)
		}
	}
	if req.Direction == StepIncrease {
		switch {
		case pos < last:
			return rungs[pos+1]
		case req.Wrap:
			return rungs[0]
		case pos == last:
			return rungs[last]
		default:
			return current
		}
	}
	switch {
	case pos > last:
		return rungs[last]
	case pos > 0:
		return rungs[pos-1]
	case req.Wrap:
		return rungs[last]
	case pos == 0:
		return rungs[0]
	default:
		return current
	}
}

// stepRequestFrom normalises the Step payload: the bridge's typed
// decoder hands over a [clusterwire.FanStepRequest]; a generic tag map
// (a host decoding the command itself) is read with the element defaults
// for the two optional fields. Direction is mandatory, and must be a
// StepDirectionEnum value.
func stepRequestFrom(fields any) (clusterwire.FanStepRequest, error) {
	req := clusterwire.FanStepRequest{LowestOff: true}
	switch v := fields.(type) {
	case clusterwire.FanStepRequest:
		req = v
	case map[uint8]any:
		raw, present := v[clusterwire.FanStepFieldDirection]
		if !present {
			return req, statusError{im.StatusInvalidCommand, "fan: Step without Direction"}
		}
		n, ok := cluster.AsUintMax(raw, 0xFF)
		if !ok {
			return req, statusError{im.StatusInvalidCommand, fmt.Sprintf("fan: Step Direction %v is not an enum", raw)}
		}
		req.Direction = uint8(n) //nolint:gosec // ≤ 0xFF per AsUintMax
		if w, ok := v[clusterwire.FanStepFieldWrap].(bool); ok {
			req.Wrap = w
		}
		if l, ok := v[clusterwire.FanStepFieldLowestOff].(bool); ok {
			req.LowestOff = l
		}
	default:
		return req, statusError{im.StatusInvalidCommand, fmt.Sprintf("fan: Step payload %T", fields)}
	}
	if req.Direction > StepDecrease {
		return req, statusError{im.StatusConstraintError, fmt.Sprintf("fan: Step Direction %d is not a StepDirectionEnum value", req.Direction)}
	}
	return req, nil
}

// statusError carries an exact IM status to the dispatcher.
type statusError struct {
	status im.StatusCode
	msg    string
}

func (e statusError) Error() string                   { return e.msg }
func (e statusError) MatterStatusCode() im.StatusCode { return e.status }

var _ im.StatusCodeError = statusError{}

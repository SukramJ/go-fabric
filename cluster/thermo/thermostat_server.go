// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package thermo provides the Matter Thermostat cluster server (0x0201).
// The server supports HEAT-only, COOL-only, and HEAT+COOL (with optional
// AUTO) configurations, enforcing conformance rules on feature-gated
// attributes per Matter Application Cluster Specification §4.3, and the
// selectable Occupancy (the unoccupied setpoints, occupancy from the host)
// and Presets (presets, the active preset and the atomic writes a
// controller changes them with, persisted through a [PresetStore])
// features, as matter.js's ThermostatServer serves them, and on request
// the optional TemperatureSetpointHold (with TemperatureSetpointHoldDuration)
// and ThermostatRunningState. Ahead of matter.js, which does not implement
// it, the server serves MatterScheduleConfiguration (the device's week
// programs, the active one, SetActiveScheduleRequest and the atomic write
// of Schedules) after connectedhomeip's ThermostatSchedules, behind a
// [SchedulePort] (schedules.go).
//
// The server holds its own state. Without a host port it answers every
// write and command without forwarding it anywhere — the reference daemon
// (examples/reference-bridge) mounts it that way, to put the surface in
// front of chip-tool. A host that drives a device sets
// [ThermostatConfig.Sink]: SystemMode and occupied-setpoint changes a
// controller asks for reach the host before they are stored, and the
// host refuses one by returning an error; so does a TemperatureSetpointHold
// write when the server serves the hold. The device's own changes come
// back through [ThermostatServer.SetSystemMode],
// [ThermostatServer.SetOccupiedHeatingSetpoint],
// [ThermostatServer.SetOccupiedCoolingSetpoint],
// [ThermostatServer.SetLocalTemperature],
// [ThermostatServer.SetTemperatureSetpointHold] and
// [ThermostatServer.SetThermostatRunningState], which do not call the Sink.
//
// The cluster's identity is the generated definition (cluster/spec/
// thermostat, ADR 0013): ids, revision, the feature check, the attribute
// and command lists, the write statuses and type checks, the
// ControlSequenceOfOperation / SystemMode enum conformance and the
// privileges. SetpointRaiseLower decodes through the definition. The
// setpoint, limit and deadband rules (setpoints.go), the SystemMode /
// ControlSequenceOfOperation coupling and SetpointRaiseLower's arithmetic
// are the server's.
package thermo

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"sync"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	tdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// Thermostat cluster ID and revision of the generated definition
// (thermostat-cluster.element.ts).
const (
	ThermostatClusterID       = tdef.ClusterID
	ThermostatClusterRevision = tdef.Revision
)

// Feature bits (thermostat-cluster.element.ts).
const (
	ThermostatFeatureHEAT = uint32(tdef.FeatureHeating)                     // bit 0 — Heating
	ThermostatFeatureCOOL = uint32(tdef.FeatureCooling)                     // bit 1 — Cooling
	ThermostatFeatureAUTO = uint32(tdef.FeatureAutoMode)                    // bit 5 — AutoMode (requires HEAT+COOL)
	ThermostatFeatureLTNE = uint32(tdef.FeatureLocalTemperatureNotExposed)  // bit 6 — LocalTemperatureNotExposed
	ThermostatFeatureOCC  = uint32(tdef.FeatureOccupancy)                   // bit 2 — Occupancy
	ThermostatFeaturePRES = uint32(tdef.FeaturePresets)                     // bit 8 — Presets
	ThermostatFeatureMSCH = uint32(tdef.FeatureMatterScheduleConfiguration) // bit 7 — MatterScheduleConfiguration
)

// servedFeatures are the features whose elements the server serves.
// MatterScheduleConfiguration is served ahead of matter.js ("not
// implemented yet", ThermostatServer.ts:82 and :126-128 at 862ad75f),
// after connectedhomeip (schedules.go), and so are the Events feature's
// events, which matter.js does not emit (events.go). ScheduleConfiguration
// and Setback (deprecated, refused, :120-125) and ThermostatSuggestions are
// not.
const servedFeatures = ThermostatFeatureHEAT | ThermostatFeatureCOOL | ThermostatFeatureAUTO | ThermostatFeatureLTNE |
	ThermostatFeatureOCC | ThermostatFeaturePRES | ThermostatFeatureMSCH | ThermostatFeatureTEVT

// ErrFeatures reports a ThermostatConfig.Features the server refuses.
var ErrFeatures = errors.New("thermo: feature selection not served")

// ErrSetpoints reports a ThermostatConfig whose setpoint limits and
// initial setpoints violate the setpoint rules in a way [New] cannot
// repair — an absolute minimum above its maximum, or an AUTO deadband the
// absolute limits leave no room for.
var ErrSetpoints = errors.New("thermo: setpoint configuration cannot be fixed")

// Thermostat attribute IDs.
const (
	thermoAttrLocalTemperature            = tdef.AttrLocalTemperature
	thermoAttrAbsMinHeatSetpointLimit     = tdef.AttrAbsMinHeatSetpointLimit
	thermoAttrAbsMaxHeatSetpointLimit     = tdef.AttrAbsMaxHeatSetpointLimit
	thermoAttrAbsMinCoolSetpointLimit     = tdef.AttrAbsMinCoolSetpointLimit
	thermoAttrAbsMaxCoolSetpointLimit     = tdef.AttrAbsMaxCoolSetpointLimit
	thermoAttrLocalTemperatureCalibration = tdef.AttrLocalTemperatureCalibration
	thermoAttrOccupiedCoolingSetpoint     = tdef.AttrOccupiedCoolingSetpoint
	thermoAttrOccupiedHeatingSetpoint     = tdef.AttrOccupiedHeatingSetpoint
	thermoAttrMinHeatSetpointLimit        = tdef.AttrMinHeatSetpointLimit
	thermoAttrMaxHeatSetpointLimit        = tdef.AttrMaxHeatSetpointLimit
	thermoAttrMinCoolSetpointLimit        = tdef.AttrMinCoolSetpointLimit
	thermoAttrMaxCoolSetpointLimit        = tdef.AttrMaxCoolSetpointLimit
	thermoAttrMinSetpointDeadBand         = tdef.AttrMinSetpointDeadBand
	thermoAttrControlSequenceOfOperation  = tdef.AttrControlSequenceOfOperation
	thermoAttrSystemMode                  = tdef.AttrSystemMode
	thermoAttrOccupancy                   = tdef.AttrOccupancy
	thermoAttrUnoccupiedHeatingSetpoint   = tdef.AttrUnoccupiedHeatingSetpoint
	thermoAttrUnoccupiedCoolingSetpoint   = tdef.AttrUnoccupiedCoolingSetpoint
	thermoAttrTemperatureSetpointHold     = tdef.AttrTemperatureSetpointHold
	thermoAttrTemperatureSetpointHoldDur  = tdef.AttrTemperatureSetpointHoldDuration
	thermoAttrThermostatRunningState      = tdef.AttrThermostatRunningState
)

// defaultMinSetpointDeadBand is MinSetpointDeadBand's default, 2.0 °C in
// its 0.1 °C unit (thermostat-cluster.element.ts: default celsius 2;
// matter.js ThermostatBehaviorTest "correctly specifies
// Thermostat-specific value in defaults").
const defaultMinSetpointDeadBand = 20

// ThermostatConfig holds the static configuration for a Thermostat
// cluster server instance. All temperature values are in units of
// 0.01°C (Matter signed int16), e.g. 2000 = 20.00°C.
type ThermostatConfig struct {
	// Features selects the active feature set. Use the
	// ThermostatFeature* constants: HEAT, COOL or both (matter.js's
	// "AUTO, O.a+" — at least one), optionally AUTO (cleared without
	// HEAT+COOL) and LTNE. [New] refuses any other selection.
	Features uint32
	// AbsMinHeatSetpointLimit is the absolute minimum heating setpoint
	// (conformance [HEAT]). Default: 700 (7.00°C).
	AbsMinHeatSetpointLimit int16
	// AbsMaxHeatSetpointLimit is the absolute maximum heating setpoint
	// (conformance [HEAT]). Default: 3000 (30.00°C).
	AbsMaxHeatSetpointLimit int16
	// AbsMinCoolSetpointLimit is the absolute minimum cooling setpoint
	// (conformance [COOL]). Default: 1600 (16.00°C).
	AbsMinCoolSetpointLimit int16
	// AbsMaxCoolSetpointLimit is the absolute maximum cooling setpoint
	// (conformance [COOL]). Default: 3200 (32.00°C).
	AbsMaxCoolSetpointLimit int16
	// InitialHeatingSetpoint is the initial OccupiedHeatingSetpoint
	// (conformance HEAT). Default: 2000 (20.00°C).
	InitialHeatingSetpoint int16
	// InitialCoolingSetpoint is the initial OccupiedCoolingSetpoint
	// (conformance COOL). Default: 2600 (26.00°C).
	InitialCoolingSetpoint int16
	// MinHeatSetpointLimit, MaxHeatSetpointLimit, MinCoolSetpointLimit and
	// MaxCoolSetpointLimit are the initial user setpoint limits; nil takes
	// the absolute limit (matter.js #userLimit: an unset user limit is its
	// absolute bound, ThermostatServer.ts:927-931). [New] reconciles them
	// with the rest.
	MinHeatSetpointLimit, MaxHeatSetpointLimit *int16
	MinCoolSetpointLimit, MaxCoolSetpointLimit *int16
	// MinSetpointDeadBand is the AUTO deadband in 0.1 °C (0 to 127); nil is
	// the default, 20 (2.0 °C). A value above 127 is reset to 20 and a
	// negative one refused, as matter.js's initialize does
	// (ThermostatServer.ts:154-160).
	MinSetpointDeadBand *int8
	// InitialUnoccupiedHeatingSetpoint and InitialUnoccupiedCoolingSetpoint
	// are the unoccupied setpoints (conformance HEAT & OCC, COOL & OCC); nil
	// is the attribute default, 2000 and 2600 (thermostat-cluster.element.ts
	// default celsius 20 and 26).
	InitialUnoccupiedHeatingSetpoint, InitialUnoccupiedCoolingSetpoint *int16
	// Occupied is the initial occupancy with OCC — matter.js's
	// externallyMeasuredOccupancy (ThermostatServer.ts:560-570); nil is
	// occupied, with the warning matter.js logs. The host reports changes
	// through [ThermostatServer.SetOccupied].
	Occupied *bool
	// NumberOfPresets, PresetTypes, Presets and ActivePresetHandle configure
	// the Presets feature (PRES): the number of presets the thermostat
	// holds, the preset types it supports, the presets it ships with and
	// the active preset's handle (nil for none). The configured presets are
	// the stored ones on the first start — a preset without a handle gets
	// one, a null BuiltIn is false — and a [PresetStore] that holds presets
	// replaces them (ThermostatServer.ts:134-152).
	NumberOfPresets    uint8
	PresetTypes        []PresetType
	Presets            []Preset
	ActivePresetHandle []byte
	// PresetStore persists the presets and the active handle; nil keeps
	// them in memory.
	PresetStore PresetStore
	// TemperatureSetpointHold serves the optional TemperatureSetpointHold
	// attribute (TemperatureSetpointHoldEnum: 0 SetpointHoldOff — follow the
	// schedule, 1 SetpointHoldOn — hold the setpoint) starting at the value
	// given; nil does not serve it. With it the server also serves
	// TemperatureSetpointHoldDuration, null, as matter.js initialises it
	// (ThermostatServer.ts:365-369 at 862ad75f) — stored as written, with no
	// hold timer. SetpointHoldExpiryTimestamp is not served (matter.js
	// leaves it unset, :372-379). A controller's write of the hold reaches
	// the [Sink]; the device's own change comes back through
	// [ThermostatServer.SetTemperatureSetpointHold].
	TemperatureSetpointHold *uint8
	// ThermostatRunningState serves the optional, read-only
	// ThermostatRunningState attribute (RelayStateBitmap: Heat, Cool, Fan
	// and their stages) starting at the bitmap given; nil does not serve
	// it. The device reports changes through
	// [ThermostatServer.SetThermostatRunningState].
	ThermostatRunningState *uint16
	// ScheduleTypes, NumberOfSchedules, NumberOfScheduleTransitions and
	// NumberOfScheduleTransitionPerDay (nil for no per-day limit) configure
	// MatterScheduleConfiguration (MSCH): the schedule types the device
	// supports, the number of schedules and of transitions across them it
	// holds, and the transitions a day may carry. Schedules are the
	// device's own week programs — each with a handle, a null BuiltIn taken
	// as built in — and ActiveScheduleHandle the one it follows (nil for
	// none; one that names no schedule is cleared with a warning). The
	// device's later changes come back through [ThermostatServer.SetSchedules]
	// and [ThermostatServer.SetActiveScheduleHandle].
	ScheduleTypes                    []ScheduleType
	NumberOfSchedules                uint8
	NumberOfScheduleTransitions      uint8
	NumberOfScheduleTransitionPerDay *uint8
	Schedules                        []Schedule
	ActiveScheduleHandle             []byte
	// SchedulePort switches the device's active schedule and stores the
	// schedules a controller wrote, with MSCH; nil keeps them in memory.
	SchedulePort SchedulePort
	// Clock times the atomic writes; nil is the system clock.
	Clock clock.Clock
	// Sink is the optional host port; nil keeps the server self-contained.
	Sink Sink
	// Logger receives the server's diagnostics; nil is slog.Default().
	Logger *slog.Logger
}

// Sink is the optional host port: told about every SystemMode and
// setpoint change a controller asks for — a write of SystemMode,
// OccupiedHeatingSetpoint or OccupiedCoolingSetpoint (with OCC also the
// unoccupied pair), and the setpoints a SetpointRaiseLower or a
// SetActivePresetRequest moves — after the server validated it and before
// anything is stored. An error refuses the change: the write or command
// answers with the error's status when it carries one (an
// [im.StatusCodeError], passed through as is), FAILURE otherwise, and
// nothing is stored. It is the Go form of a matter.js host refusing by
// throwing in occupied*Setpoint$Changing / systemMode$Changing: the
// validation runs before the state changes
// (packages/node/src/behaviors/thermostat/ThermostatServer.ts:594-601,
// :614 at matter.js 0ceb202d). For comparison, onoff's Sink answers every
// error, a status error included, with FAILURE.
//
// With [ThermostatConfig.TemperatureSetpointHold] a write of
// TemperatureSetpointHold reaches the Sink the same way, as
// SetTemperatureSetpointHold. matter.js stores the hold only (its
// reaction is commented out, ThermostatServer.ts:381 at 862ad75f) and
// connectedhomeip gives it no behaviour either
// (src/app/clusters/thermostat-server/ThermostatClusterBase.cpp:357-358 at
// 6170af84, an optional attribute only); the hold reaching the host is
// this module's port, so a device that follows a week program learns that
// a controller asked it to hold or to resume.
//
// The setpoint limits (Min/MaxHeat/CoolSetpointLimit) are configuration,
// not device commands: their writes do not call the Sink, and neither
// do LocalTemperatureCalibration and TemperatureSetpointHoldDuration.
// The host's own changes ([ThermostatServer.SetSystemMode],
// [ThermostatServer.SetOccupiedHeatingSetpoint],
// [ThermostatServer.SetOccupiedCoolingSetpoint],
// [ThermostatServer.SetTemperatureSetpointHold]) do not call it.
//
// The Sink runs with no lock held that the inbound setters or a read
// take: it may read the server (MatterRead) and may confirm the change
// through the inbound setters before it returns — on its own goroutine or
// from any other — without waiting for the write or command that called
// it. Writes and SetpointRaiseLower stay serialised behind it (the next
// one waits until it returned); the host never takes that queue. After
// the Sink consented, the server stores on the state as it is then: the
// value the Sink consented to, over whatever an inbound setter stored for
// that attribute meanwhile, and — for a write — the companions the
// reconcile computes on that state. When those, or the written setpoint
// itself, differ from what the Sink was told (an inbound setter moved a
// setpoint while the Sink ran), the recomputed values are stored and the
// server logs matter.thermostat.sink_companions_moved. The cover server's Mover
// follows the same rule (cluster/cover windowcovering_server.go
// invokeMover).
type Sink interface {
	// SetSystemMode is a SystemMode write the definition admitted.
	SetSystemMode(ctx context.Context, mode uint8) error
	// SetSetpoint is a change of OccupiedHeatingSetpoint or
	// OccupiedCoolingSetpoint — with OCC also UnoccupiedHeatingSetpoint or
	// UnoccupiedCoolingSetpoint — (attrID) to value, with companions: the
	// other attributes that move with it, computed but not yet stored.
	// For a write, value is the written setpoint after the reconcile
	// (matter.js #reconcileSetpoints) — the value the server will store,
	// should the reconcile ever pin it elsewhere than the controller
	// wrote — and the companions are what the reconcile moves besides it. A SetpointRaiseLower makes one call: in Both
	// mode on a HEAT+COOL thermostat the cooling setpoint is the primary
	// and the heating setpoint its companion (matter.js assigns cooling
	// first, ThermostatServer.ts:219-220); when only one of them moves it
	// travels alone, and a single-mode command moves one with no
	// companions.
	//
	// A value equal to the stored one is no change: the write or command
	// answers SUCCESS without calling the Sink (matter.js Datasource.ts
	// skips an equal value on commit, :760-762, and fires $Changing only
	// for fields that changed, :1042-1043).
	SetSetpoint(ctx context.Context, attrID uint32, value int16, companions []SetpointChange) error
	// SetTemperatureSetpointHold is a TemperatureSetpointHold write the
	// definition admitted (0 SetpointHoldOff, 1 SetpointHoldOn), on a
	// server that serves the hold. A value equal to the stored one is no
	// change and does not call it.
	SetTemperatureSetpointHold(ctx context.Context, hold uint8) error
}

// SetpointChange is one attribute a setpoint change would move besides
// the one asked for: its id and the new value — the generated
// thermostat package's type, so a Sink declared there
// (thermostatdef.Declare) is this Sink.
type SetpointChange = tdef.SetpointChange

// DefaultThermostatConfig returns a heating+cooling thermostat config with
// sensible European defaults.
func DefaultThermostatConfig() ThermostatConfig {
	return ThermostatConfig{
		Features:                ThermostatFeatureHEAT | ThermostatFeatureCOOL | ThermostatFeatureAUTO,
		AbsMinHeatSetpointLimit: 700,
		AbsMaxHeatSetpointLimit: 3000,
		AbsMinCoolSetpointLimit: 1600,
		AbsMaxCoolSetpointLimit: 3200,
		InitialHeatingSetpoint:  2000,
		InitialCoolingSetpoint:  2600,
	}
}

// ThermostatServer is the Matter Thermostat cluster server (0x0201).
// It enforces feature-conformance rules: AUTO is only set when both HEAT
// and COOL are active; COOL-only attributes are suppressed without COOL;
// HEAT-only attributes are suppressed without HEAT; LocalTemperatureCalibration
// is only served when LTNE is not set.
type ThermostatServer struct {
	// log is the server's logger ([ThermostatConfig.Logger]; slog.Default() when nil).
	log *slog.Logger
	// op serialises the controller's changes (writes, commands): the
	// queue, held across the Sink, never taken by the inbound setters. mu
	// guards the values; the setters take only mu, and it is not held
	// while the Sink runs (issue #63).
	op sync.Mutex
	// mu guards the values that change after construction: localTemp
	// through systemMode. It is taken inside op and not held while the Sink
	// runs.
	mu       sync.RWMutex
	features uint32
	inst     *spec.Instance
	sink     Sink

	localTemp      *int16 // nullable per Matter §4.3.6.1
	localTempCalib int16

	// HEAT-gated state
	absMinHeat int16
	absMaxHeat int16
	occupHeat  int16
	minHeat    int16
	maxHeat    int16

	// COOL-gated state
	absMinCool int16
	absMaxCool int16
	occupCool  int16
	minCool    int16
	maxCool    int16

	// AUTO-gated state
	minSetpointDeadBand int8 // 0.1°C units; default 20 = 2.0°C

	// OCC-gated state
	unoccHeat int16
	unoccCool int16
	occupied  bool

	// PRES-gated state: matter.js persistedPresets, presetTypes,
	// numberOfPresets and activePresetHandle.
	presets            []Preset
	presetTypes        []PresetType
	numberOfPresets    uint8
	activePresetHandle []byte
	presetStore        PresetStore

	systemMode uint8 // SystemModeEnum value

	// Optional state, served on request
	// ([ThermostatConfig.TemperatureSetpointHold],
	// [ThermostatConfig.ThermostatRunningState]): the hold, its duration
	// (nil is null) and the relay state.
	hold         uint8
	holdDuration *uint16
	runningState uint16
	// runningMode is ThermostatRunningMode, served with TEVT and AUTO
	// (ThermostatRunningModeEnum; [ThermostatServer.SetThermostatRunningMode]).
	runningMode uint8
	// endpoint is where a host change's events go ([ThermostatServer.SetEndpoint]).
	endpoint endpointStamp
	// pendingSetpointEvents are the SetpointChange events commitSetpoints
	// queued under mu (events.go).
	pendingSetpointEvents []tdef.SetpointChangeEvent

	// MSCH-gated state (schedules.go): the configuration, the stored
	// schedules, the active handle and the port.
	scheduleTypes               []ScheduleType
	numberOfSchedules           uint8
	numberOfScheduleTransitions uint8
	transitionsPerDay           *uint8
	schedules                   []Schedule
	activeScheduleHandle        []byte
	schedulePort                SchedulePort

	clk    clock.Clock
	atomic atomicWrites

	// changes reports the setpoints a write's reconcile moved.
	changes cluster.AttributeChanges

	// host, events and veto are the typed server's command handlers,
	// event path and write veto (thermostat_typed.go).
	host   spec.HostHandlers
	events spec.Events
	veto   spec.WriteVeto
}

// New constructs the cluster. AUTO is silently cleared from the feature
// set when HEAT+COOL are not both present. A selection with neither HEAT
// nor COOL — which matter.js's FeatureSelectionErrors refuses, HEAT and
// COOL being "AUTO, O.a+" (thermostat-cluster.element.ts) — or naming a
// feature the server does not serve (Setback, the schedule, event and
// suggestion features) is refused with [ErrFeatures]; a negative
// MinSetpointDeadBand with [ErrSetpoints], a preset configuration the
// write rules refuse with [ErrPresets], a schedule configuration with
// [ErrSchedules].
//
// Limits and initial setpoints that violate the setpoint rules are
// repaired, preferring the heating side (the cooling side without HEAT):
// the other side's limits and setpoint move first, the preferred side is
// pulled back only where the other would leave its absolute limits. New
// logs matter.thermostat.initial_setpoints_adjusted with the violation and
// every adjustment, and refuses with [ErrSetpoints] only when a violation
// remains. Mirrors matter.js 0ceb202d packages/node/src/behaviors/
// thermostat/ThermostatServer.ts:1015-1042 (#reconcileInitialSetpoints,
// run from #setupValidations at :582-583, after the deadband is captured
// at :154-164); here the deadband is the fixed default, set before the
// reconcile.
func New(cfg ThermostatConfig) (*ThermostatServer, error) {
	features := cfg.Features
	// AUTO requires HEAT+COOL — clear it if either is missing.
	if features&ThermostatFeatureAUTO != 0 {
		if features&ThermostatFeatureHEAT == 0 || features&ThermostatFeatureCOOL == 0 {
			features &^= ThermostatFeatureAUTO
		}
	}
	if features&^servedFeatures != 0 {
		return nil, fmt.Errorf("%w: 0x%X names features the server does not serve", ErrFeatures, features&^servedFeatures)
	}
	inst, err := spec.New(tdef.Definition, spec.Options{Features: features, Attributes: servedOptionalFor(cfg, features), Events: servedEvents(features)})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFeatures, err)
	}

	s := &ThermostatServer{
		log:                 cmp.Or(cfg.Logger, slog.Default()),
		features:            features,
		inst:                inst,
		sink:                cfg.Sink,
		localTempCalib:      0,
		minSetpointDeadBand: defaultMinSetpointDeadBand,
		occupied:            true,
		clk:                 clock.Or(cfg.Clock),
	}
	// Mirrors matter.js ThermostatServer.ts:154-160 at 862ad75f: a deadband
	// above 127 is reset to the default, one below 0 refused.
	if d := cfg.MinSetpointDeadBand; d != nil {
		if *d < 0 {
			return nil, fmt.Errorf("%w: minSetpointDeadBand is out of valid range 0..127", ErrSetpoints)
		}
		s.minSetpointDeadBand = *d
	}
	or := func(v *int16, def int16) int16 {
		if v != nil {
			return *v
		}
		return def
	}

	if features&ThermostatFeatureHEAT != 0 {
		s.absMinHeat = cfg.AbsMinHeatSetpointLimit
		s.absMaxHeat = cfg.AbsMaxHeatSetpointLimit
		s.occupHeat = cfg.InitialHeatingSetpoint
		s.minHeat = or(cfg.MinHeatSetpointLimit, cfg.AbsMinHeatSetpointLimit)
		s.maxHeat = or(cfg.MaxHeatSetpointLimit, cfg.AbsMaxHeatSetpointLimit)
		if features&ThermostatFeatureOCC != 0 {
			s.unoccHeat = or(cfg.InitialUnoccupiedHeatingSetpoint, 2000)
		}
	}
	if features&ThermostatFeatureCOOL != 0 {
		s.absMinCool = cfg.AbsMinCoolSetpointLimit
		s.absMaxCool = cfg.AbsMaxCoolSetpointLimit
		s.occupCool = cfg.InitialCoolingSetpoint
		s.minCool = or(cfg.MinCoolSetpointLimit, cfg.AbsMinCoolSetpointLimit)
		s.maxCool = or(cfg.MaxCoolSetpointLimit, cfg.AbsMaxCoolSetpointLimit)
		if features&ThermostatFeatureOCC != 0 {
			s.unoccCool = or(cfg.InitialUnoccupiedCoolingSetpoint, 2600)
		}
	}
	s.initOccupancy(cfg.Occupied)
	if err := s.initOptional(cfg); err != nil {
		return nil, err
	}

	st := s.setpointState()
	before := st
	violation, adjusted, err := st.reconcileInitial()
	if err != nil {
		return nil, err
	}
	if violation != "" {
		s.commitSetpoints(st)
		s.logger().Warn("matter.thermostat.initial_setpoints_adjusted",
			slog.String("violation", violation),
			slog.String("adjusted", adjustedText(adjusted)))
	}
	if features&ThermostatFeaturePRES != 0 {
		if err := s.seedPresets(cfg); err != nil {
			return nil, err
		}
		// The setpoints initialize repaired change in its transaction, so
		// their $Changing reactors run at its commit: a direct change of the
		// setpoints in effect clears the active preset
		// (ThermostatServer.ts:682-706, :744-768 at 862ad75f; matter.js
		// ThermostatBehaviorTest "clears the active preset when it adjusts a
		// setpoint").
		unocc := !s.occupied
		if s.activePresetHandle != nil &&
			(before.setpoint(true, unocc) != st.setpoint(true, unocc) || before.setpoint(false, unocc) != st.setpoint(false, unocc)) {
			s.activePresetHandle = nil
			if err := s.savePresets(context.Background(), s.presets, nil); err != nil {
				return nil, fmt.Errorf("%w: %w", ErrPresets, err)
			}
		}
	}

	if features&ThermostatFeatureMSCH != 0 {
		if err := s.seedSchedules(cfg); err != nil {
			return nil, err
		}
	}

	s.systemMode = defaultSystemMode(features)
	// The initial reconcile is no change a controller hears of.
	s.pendingSetpointEvents = nil
	return s, nil
}

// defaultSystemMode is SystemMode's default: Auto with the AUTO feature,
// Heat with HEAT, Cool when COOL-only.
func defaultSystemMode(features uint32) uint8 {
	switch {
	case features&ThermostatFeatureAUTO != 0:
		return uint8(tdef.SystemModeAuto)
	case features&ThermostatFeatureHEAT != 0:
		return uint8(tdef.SystemModeHeat)
	default: // COOL-only; New refused a selection with neither
		return uint8(tdef.SystemModeCool)
	}
}

// servedOptionalFor is [servedOptional] with the optional attributes the
// configuration asks for: TemperatureSetpointHold with its duration, and
// ThermostatRunningState.
func servedOptionalFor(cfg ThermostatConfig, features uint32) []uint32 {
	optional := servedOptional(features)
	if cfg.TemperatureSetpointHold != nil {
		optional = append(optional, thermoAttrTemperatureSetpointHold, thermoAttrTemperatureSetpointHoldDur)
	}
	if cfg.ThermostatRunningState != nil {
		optional = append(optional, thermoAttrThermostatRunningState)
	}
	return optional
}

// initOccupancy is matter.js #setupOccupancyIntegration
// (ThermostatServer.ts:559-570 at 862ad75f) for a thermostat without an
// OccupancySensing endpoint: externallyMeasuredOccupancy, else occupied.
func (s *ThermostatServer) initOccupancy(occupied *bool) {
	if s.features&ThermostatFeatureOCC == 0 {
		return
	}
	if occupied == nil {
		s.logger().Warn("matter.thermostat.occupancy_unset",
			slog.String("reason", "no local OccupancySensing cluster available and externallyMeasuredOccupancy state not set"))
		return
	}
	s.occupied = *occupied
}

// initOptional takes the initial TemperatureSetpointHold and
// ThermostatRunningState, each checked against its type
// (TemperatureSetpointHoldEnum, RelayStateBitmap). The hold's duration
// starts null: matter.js's #setupThermostatLogic initialises
// temperatureSetpointHoldDuration to null when temperatureSetpointHold is
// present (ThermostatServer.ts:365-369 at 862ad75f), which makes it a
// served attribute (packages/node/src/behavior/cluster/ValidatedElements.ts:252:
// an attribute is implemented when its state is not undefined).
func (s *ThermostatServer) initOptional(cfg ThermostatConfig) error {
	if h := cfg.TemperatureSetpointHold; h != nil {
		if _, err := s.inst.CheckSet(thermoAttrTemperatureSetpointHold, *h, s.MatterRead); err != nil {
			return fmt.Errorf("thermo: TemperatureSetpointHold %d: %w", *h, err)
		}
		s.hold = *h
	}
	if r := cfg.ThermostatRunningState; r != nil {
		if _, err := s.inst.CheckSet(thermoAttrThermostatRunningState, *r, s.MatterRead); err != nil {
			return fmt.Errorf("thermo: ThermostatRunningState 0x%04X: %w", *r, err)
		}
		s.runningState = *r
	}
	return nil
}

// NewThermostatServer constructs the cluster as [New] does; it panics on a
// selection or setpoint configuration [New] refuses — a wiring error, not
// a runtime condition.
func NewThermostatServer(cfg ThermostatConfig) *ThermostatServer {
	s, err := New(cfg)
	if err != nil {
		panic(err)
	}
	return s
}

// servedOptional lists the optional attributes the server serves for a
// selection: the absolute and user setpoint limits of each of HEAT and
// COOL ("[HEAT]" / "[COOL]"), LocalTemperatureCalibration without LTNE
// ("[!LTNE]").
func servedOptional(features uint32) []uint32 {
	var out []uint32
	if features&ThermostatFeatureHEAT != 0 {
		out = append(out, thermoAttrAbsMinHeatSetpointLimit, thermoAttrAbsMaxHeatSetpointLimit,
			thermoAttrMinHeatSetpointLimit, thermoAttrMaxHeatSetpointLimit)
	}
	if features&ThermostatFeatureCOOL != 0 {
		out = append(out, thermoAttrAbsMinCoolSetpointLimit, thermoAttrAbsMaxCoolSetpointLimit,
			thermoAttrMinCoolSetpointLimit, thermoAttrMaxCoolSetpointLimit)
	}
	if features&ThermostatFeatureLTNE == 0 {
		out = append(out, thermoAttrLocalTemperatureCalibration)
	}
	return out
}

// MatterClusterID returns 0x0201.
func (s *ThermostatServer) MatterClusterID() uint32 { return ThermostatClusterID }

// SetLocalTemperature updates the LocalTemperature attribute (quality X —
// nullable). Pass nil to set null (sensor unavailable). The value is
// stored as given — no range check, no log — and not announced through
// the change notifier: LocalTemperature is in [ThermostatServer.MatterReportable].
// With TEVT a changed value emits LocalTemperatureChange (events.go).
func (s *ThermostatServer) SetLocalTemperature(t *int16) {
	s.mu.Lock()
	before := s.eventStateLocked()
	s.localTemp = t
	after := s.eventStateLocked()
	s.mu.Unlock()
	s.emitChanges(context.Background(), before, after)
}

// SetSystemMode records a SystemMode the device took on itself: stored and
// reported through the change notifier, without the Sink. A mode the
// definition does not admit for the selection (Auto without AUTO, Heat or
// EmergencyHeat without HEAT, Cool or Precooling without COOL —
// thermostat-cluster.element.ts SystemModeEnum) cannot be served: it is
// logged and the stored mode kept.
func (s *ThermostatServer) SetSystemMode(mode uint8) {
	if _, err := s.inst.ValidateWrite(thermoAttrSystemMode, mode, nil); err != nil {
		s.logger().Warn("matter.thermostat.inbound_system_mode_ignored",
			slog.Int("system_mode", int(mode)), slog.String("reason", err.Error()))
		return
	}
	s.mu.Lock()
	before := s.eventStateLocked()
	changed := s.systemMode != mode
	s.systemMode = mode
	running := changed && s.followSystemModeLocked(mode)
	after := s.eventStateLocked()
	s.mu.Unlock()
	if changed {
		ids := []uint32{thermoAttrSystemMode}
		if running {
			ids = append(ids, tdef.AttrThermostatRunningMode)
		}
		s.changes.NotifyFrom(contract.OriginHostSet, ids...)
		s.emitChanges(context.Background(), before, after)
	}
}

// SetTemperatureSetpointHold records a TemperatureSetpointHold the device
// took on itself — a manual change on the device holds the setpoint, the
// next program step resumes the schedule: stored and reported through the
// change notifier, without the Sink. A server that does not serve the
// hold ([ThermostatConfig.TemperatureSetpointHold] nil), or a value
// outside TemperatureSetpointHoldEnum, is logged and changes nothing.
func (s *ThermostatServer) SetTemperatureSetpointHold(hold uint8) {
	if _, err := s.inst.CheckSet(thermoAttrTemperatureSetpointHold, hold, s.MatterRead); err != nil {
		s.logger().Warn("matter.thermostat.inbound_setpoint_hold_ignored",
			slog.String("attribute", "temperature_setpoint_hold"), slog.Int("value", int(hold)), slog.String("reason", err.Error()))
		return
	}
	s.mu.Lock()
	changed := s.hold != hold
	s.hold = hold
	s.mu.Unlock()
	if changed {
		s.changes.NotifyFrom(contract.OriginHostSet, thermoAttrTemperatureSetpointHold)
	}
}

// SetThermostatRunningState records the relays the device runs right now
// (RelayStateBitmap: bit 0 Heat, 1 Cool, 2 Fan, 3-6 the second and third
// stages): stored and reported through the change notifier. The
// attribute is read-only to a controller. A server that does not serve it
// ([ThermostatConfig.ThermostatRunningState] nil), or a bitmap with a bit
// RelayStateBitmap does not define, is logged and changes nothing.
func (s *ThermostatServer) SetThermostatRunningState(state uint16) {
	if _, err := s.inst.CheckSet(thermoAttrThermostatRunningState, state, s.MatterRead); err != nil {
		s.logger().Warn("matter.thermostat.inbound_running_state_ignored",
			slog.String("attribute", "thermostat_running_state"), slog.Int("value", int(state)), slog.String("reason", err.Error()))
		return
	}
	s.mu.Lock()
	before := s.eventStateLocked()
	changed := s.runningState != state
	s.runningState = state
	after := s.eventStateLocked()
	s.mu.Unlock()
	if changed {
		s.changes.NotifyFrom(contract.OriginHostSet, thermoAttrThermostatRunningState)
		s.emitChanges(context.Background(), before, after)
	}
}

// SetOccupiedHeatingSetpoint records an OccupiedHeatingSetpoint the device
// took on itself: stored and reported through the change notifier,
// without the Sink. It runs the reconcile a write runs (matter.js
// #reconcileSetpoints on occupiedHeatingSetpoint$Changing), so the
// cooling setpoint may move with it and is reported too. It is not
// refused for its limits: a value outside the effective heating limits is
// clamped to them and logged. A server without HEAT, or a value the
// reconcile cannot fit, is logged and changes nothing.
func (s *ThermostatServer) SetOccupiedHeatingSetpoint(v int16) { s.setInboundSetpoint(true, v) }

// SetOccupiedCoolingSetpoint is [ThermostatServer.SetOccupiedHeatingSetpoint]
// for OccupiedCoolingSetpoint (COOL).
func (s *ThermostatServer) SetOccupiedCoolingSetpoint(v int16) { s.setInboundSetpoint(false, v) }

func (s *ThermostatServer) setInboundSetpoint(heat bool, v int16) {
	key, feature, name := spOccupiedCooling, ThermostatFeatureCOOL, "occupied_cooling_setpoint"
	if heat {
		key, feature, name = spOccupiedHeating, ThermostatFeatureHEAT, "occupied_heating_setpoint"
	}
	if s.features&feature == 0 {
		s.logger().Warn("matter.thermostat.inbound_setpoint_ignored",
			slog.String("attribute", name), slog.Int("value", int(v)), slog.String("reason", "feature not served"))
		return
	}
	s.mu.Lock()
	before := s.eventStateLocked()
	st := s.setpointState()
	lo, hi := st.effectiveLimits(heat)
	n := min(max(int(v), lo), hi)
	if heat {
		st.occupHeat = n
	} else {
		st.occupCool = n
	}
	err := st.reconcile(key)
	var changed []uint32
	if err == nil {
		changed = s.commitSetpoints(st)
	}
	after := s.eventStateLocked()
	s.mu.Unlock()
	s.emitChanges(context.Background(), before, after)
	if err != nil {
		s.logger().Warn("matter.thermostat.inbound_setpoint_ignored",
			slog.String("attribute", name), slog.Int("value", int(v)), slog.String("reason", err.Error()))
		return
	}
	if n != int(v) {
		s.logger().Warn("matter.thermostat.inbound_setpoint_clamped",
			slog.String("attribute", name), slog.Int("value", int(v)), slog.Int("stored", n),
			slog.Int("min", lo), slog.Int("max", hi))
	}
	s.changes.NotifyFrom(contract.OriginHostSet, changed...)
	// A local change of an occupied setpoint is a direct one: it ends the
	// active preset (ThermostatServer.ts:744-768 at 862ad75f).
	if len(changed) > 0 {
		s.clearActivePresetOnDirectWrite(context.Background(), false)
	}
}

// sinkError maps a Sink refusal onto the answer: an error naming its
// status by type ([spec.CarriesStatus] — a host's contract.StatusError or
// an im.StatusCodeError) passes through as is, any other answers FAILURE.
func sinkError(what string, err error) error {
	if spec.CarriesStatus(err) {
		return err
	}
	return spec.Errorf(im.StatusFailure, "thermo: %s refused: %v", what, err)
}

// MatterRead implements [contract.ClusterServer]. An attribute the
// selection does not serve returns (nil, false) — the IM dispatcher
// handles the UnsupportedAttribute response.
func (s *ThermostatServer) MatterRead(attrID uint32) (any, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.readLocked(attrID)
}

// readLocked is [ThermostatServer.MatterRead] for a caller holding s.mu.
func (s *ThermostatServer) readLocked(attrID uint32) (any, bool) {
	if v, ok := s.inst.ReadGlobal(attrID); ok {
		return v, true
	}
	if !s.inst.Serves(attrID) {
		return nil, false
	}
	if v, ok := s.readOptionalLocked(attrID); ok {
		return v, true
	}
	switch attrID {
	case thermoAttrLocalTemperature:
		if s.localTemp == nil {
			return nil, true // nullable quality X: null value, attribute present
		}
		return *s.localTemp, true
	case thermoAttrAbsMinHeatSetpointLimit:
		return s.absMinHeat, true
	case thermoAttrAbsMaxHeatSetpointLimit:
		return s.absMaxHeat, true
	case thermoAttrAbsMinCoolSetpointLimit:
		return s.absMinCool, true
	case thermoAttrAbsMaxCoolSetpointLimit:
		return s.absMaxCool, true
	case thermoAttrLocalTemperatureCalibration:
		return s.localTempCalib, true
	case thermoAttrOccupiedCoolingSetpoint:
		return s.occupCool, true
	case thermoAttrOccupiedHeatingSetpoint:
		return s.occupHeat, true
	case thermoAttrMinHeatSetpointLimit:
		return s.minHeat, true
	case thermoAttrMaxHeatSetpointLimit:
		return s.maxHeat, true
	case thermoAttrMinCoolSetpointLimit:
		return s.minCool, true
	case thermoAttrMaxCoolSetpointLimit:
		return s.maxCool, true
	case thermoAttrMinSetpointDeadBand:
		return s.minSetpointDeadBand, true
	case thermoAttrOccupancy:
		// OccupancyBitmap: bit 0 Occupied.
		if s.occupied {
			return uint8(1), true
		}
		return uint8(0), true
	case thermoAttrUnoccupiedHeatingSetpoint:
		return s.unoccHeat, true
	case thermoAttrUnoccupiedCoolingSetpoint:
		return s.unoccCool, true
	case tdef.AttrPresetTypes:
		return spec.List[PresetType](slices.Clone(s.presetTypes)), true
	case tdef.AttrNumberOfPresets:
		return s.numberOfPresets, true
	case tdef.AttrActivePresetHandle:
		if s.activePresetHandle == nil {
			return nil, true
		}
		return bytes.Clone(s.activePresetHandle), true
	case tdef.AttrPresets:
		return spec.List[Preset](clonePresets(s.presets)), true
	case tdef.AttrThermostatRunningMode:
		return s.runningMode, true
	case thermoAttrControlSequenceOfOperation:
		// Mandatory (M conformance). Derived from the supported modes:
		// 4=CoolingAndHeating, 2=HeatingOnly, 0=CoolingOnly. The value
		// follows the device's immutable HEAT/COOL capability; a write is
		// accepted and ignored, as the specification and matter.js have it
		// (see MatterWrite). Being immutable it is (correctly) not in
		// MatterReportable.
		return controlSequenceOfOperation(s.features), true
	default: // thermoAttrSystemMode, the last served attribute
		return s.systemMode, true
	}
}

// readOptionalLocked reads the hold, its duration, the running state and
// the MSCH attributes; false for any other attribute. The caller holds
// s.mu and checked that the attribute is served.
func (s *ThermostatServer) readOptionalLocked(attrID uint32) (any, bool) {
	switch attrID {
	case thermoAttrTemperatureSetpointHold:
		return s.hold, true
	case thermoAttrTemperatureSetpointHoldDur:
		if s.holdDuration == nil {
			return nil, true
		}
		return *s.holdDuration, true
	case thermoAttrThermostatRunningState:
		return s.runningState, true
	case tdef.AttrScheduleTypes:
		return spec.List[ScheduleType](slices.Clone(s.scheduleTypes)), true
	case tdef.AttrNumberOfSchedules:
		return s.numberOfSchedules, true
	case tdef.AttrNumberOfScheduleTransitions:
		return s.numberOfScheduleTransitions, true
	case tdef.AttrNumberOfScheduleTransitionPerDay:
		if s.transitionsPerDay == nil {
			return nil, true
		}
		return *s.transitionsPerDay, true
	case tdef.AttrActiveScheduleHandle:
		if s.activeScheduleHandle == nil {
			return nil, true
		}
		return bytes.Clone(s.activeScheduleHandle), true
	case tdef.AttrSchedules:
		return spec.List[Schedule](cloneSchedules(s.schedules)), true
	}
	return nil, false
}

// MatterReadFiltered implements [contract.FabricScopedReader] for the two
// attributes whose value depends on the reader: Presets and Schedules,
// which the peer holding an atomic write of them reads as it staged them —
// matter.js's Presets getter (ThermostatServer.ts:1680-1699 at 862ad75f),
// chip's Schedules read (ThermostatClusterSchedules.cpp:517-533 at
// 645b267a). Every other attribute falls through to
// [ThermostatServer.MatterRead].
func (s *ThermostatServer) MatterReadFiltered(ctx context.Context, attrID uint32) (any, bool) {
	if (attrID != tdef.AttrPresets && attrID != tdef.AttrSchedules) || !s.inst.Serves(attrID) {
		return nil, false
	}
	if v, ok := s.pendingAtomicValue(ctx, attrID); ok {
		switch staged := v.(type) {
		case []Preset:
			return spec.List[Preset](clonePresets(staged)), true
		case []Schedule:
			return spec.List[Schedule](cloneSchedules(staged)), true
		}
	}
	return s.MatterRead(attrID)
}

// MatterWrite handles writable attributes (setpoints, setpoint limits,
// LocalTemperatureCalibration, SystemMode, and the two optionally writable
// ones it ignores). The definition checks every write first (matter.js
// AttributeWriteResponse + ValueValidator): an attribute the selection does
// not serve is UNSUPPORTED_ATTRIBUTE, a read-only one UNSUPPORTED_WRITE, a
// value outside its type, its constraint or its enum's conformant values
// CONSTRAINT_ERROR. The rules beyond it are the server's. With a
// [Sink], a SystemMode or occupied-setpoint write reaches it after all of
// that and before anything is stored ([Sink] has the answer to a refusal).
func (s *ThermostatServer) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	v, err := s.inst.ValidateWrite(attrID, value, nil)
	if err != nil {
		return err
	}
	if err := s.veto.Check(ctx, attrID, storedWrite(attrID, v)); err != nil {
		return err
	}
	switch attrID {
	case tdef.AttrPresets:
		return s.writePresets(ctx, v)
	case tdef.AttrSchedules:
		return s.writeSchedules(ctx, v)
	}
	s.op.Lock()
	defer s.op.Unlock()
	switch attrID {
	case thermoAttrOccupiedHeatingSetpoint, thermoAttrOccupiedCoolingSetpoint,
		thermoAttrUnoccupiedHeatingSetpoint, thermoAttrUnoccupiedCoolingSetpoint,
		thermoAttrMinHeatSetpointLimit, thermoAttrMaxHeatSetpointLimit,
		thermoAttrMinCoolSetpointLimit, thermoAttrMaxCoolSetpointLimit:
		n, _ := v.(int64) // an int16 per ValidateWrite
		changed, err := s.writeSetpoint(ctx, attrID, int(n))
		if err != nil {
			return err
		}
		// The written attribute and what the reconcile moved besides it
		// reach the listeners from the write, as matter.js emits each
		// <setpoint>$Changed on the commit of the write's transaction
		// (Datasource.ts:1473-1476). The subscriber's report of the
		// written attribute is the write path's as well; a subscription
		// reports it once.
		slices.Sort(changed)
		s.changes.NotifyFrom(contract.OriginControllerWrite, changed...)
		return nil
	case thermoAttrControlSequenceOfOperation:
		// "For backwards compatibility, this attribute is optionally
		// writeable. However, any writes to this attribute SHALL be
		// silently ignored" — matter.js ThermostatServer.ts
		// #ensureControlSequenceOfOperationNotWritable accepts the write
		// and reverts it (TC-TSTAT-2.2 step 12). The definition checked the
		// enum.
		return nil
	case thermoAttrMinSetpointDeadBand:
		// Likewise optionally writable and silently ignored (matter.js
		// #ensureMinSetpointDeadBandNotWritable); the definition checked
		// "0 to 12.7°C" (0 to 127).
		return nil
	case thermoAttrLocalTemperatureCalibration:
		// "RW VM", plain state in matter.js.
		n, _ := v.(int64) // an int8 per ValidateWrite
		calib := int16(n) //nolint:gosec // an int8 per ValidateWrite
		s.mu.Lock()
		changed := s.localTempCalib != calib
		s.localTempCalib = calib
		s.mu.Unlock()
		if changed {
			s.changes.NotifyFrom(contract.OriginControllerWrite, thermoAttrLocalTemperatureCalibration)
		}
		return nil
	case thermoAttrTemperatureSetpointHold:
		n, _ := v.(uint64)                // an enum8 per ValidateWrite
		return s.writeHold(ctx, uint8(n)) //nolint:gosec // an enum8 per ValidateWrite
	case thermoAttrTemperatureSetpointHoldDur:
		// "RW VM", plain state in matter.js, with no hold timer (its
		// reaction is commented out, ThermostatServer.ts:381-386 at
		// 862ad75f); the definition checked "max 1440" and null.
		var d *uint16
		if n, ok := v.(uint64); ok {
			m := uint16(n) //nolint:gosec // a uint16 per ValidateWrite
			d = &m
		}
		s.mu.Lock()
		changed := !equalDuration(s.holdDuration, d)
		s.holdDuration = d
		s.mu.Unlock()
		if changed {
			s.changes.NotifyFrom(contract.OriginControllerWrite, thermoAttrTemperatureSetpointHoldDur)
		}
		return nil
	default: // thermoAttrSystemMode, the last writable attribute
		n, _ := v.(uint64)                      // an enum8 per ValidateWrite, its conformance checked
		return s.writeSystemMode(ctx, uint8(n)) //nolint:gosec // an enum8 per ValidateWrite
	}
}

// writePresets is a controller's write of Presets: outside the local
// actor and a command, the Presets setter hands it to the atomic write
// that holds the attribute (matter.js ThermostatServer.ts:1702-1716 at
// 862ad75f, AtomicWriteHandler.writeAttribute), which validates it against
// the stored presets — presets$AtomicChanging, #validatePresetWriteRequest
// (:1289-1291) — and stages it. A list arrives element by element (the
// REPLACE-ALL, then one append each): every step is validated and staged,
// and the dispatcher appends to what the peer staged
// ([ThermostatServer.MatterReadFiltered]).
func (s *ThermostatServer) writePresets(ctx context.Context, v any) error {
	presets, err := presetsValue(v)
	if err != nil {
		return err
	}
	presets = clonePresets(presets)
	return s.writeAtomicAttribute(ctx, tdef.AttrPresets, presets, func(any) error {
		s.mu.RLock()
		old, rules := s.presets, s.presetRules()
		s.mu.RUnlock()
		return rules.validatePresetWrite(presets, old, false, map[string]bool{})
	})
}

// writeSystemMode applies a SystemMode the definition admitted for the
// selection: Auto needs AUTO, Cool and Precooling "[COOL]", Heat and
// EmergencyHeat "[HEAT]" (thermostat-cluster.element.ts SystemModeEnum).
// That covers matter.js ThermostatServer.ts #assertSystemModeChanging too:
// CoolingOnly forbids Heat and EmergencyHeat, HeatingOnly Cool and
// Precooling, and controlSequenceOfOperation derives CoolingOnly exactly
// without HEAT and HeatingOnly exactly without COOL — the reheat sequences
// and CoolingAndHeating (which forbids nothing) aside, which it never
// derives or which forbid nothing the enum allows.
//
// With a [Sink] the mode goes to the host first (matter.js
// systemMode$Changing, ThermostatServer.ts:614 at 0ceb202d, runs before
// the state changes); a refusal stores nothing. The Sink runs without
// s.mu; once it consented the mode is stored over whatever
// [ThermostatServer.SetSystemMode] stored meanwhile. The caller holds
// s.op.
func (s *ThermostatServer) writeSystemMode(ctx context.Context, v uint8) error {
	// Mirrors matter.js packages/node/src/behavior/state/managed/Datasource.ts:760-762
	// (isDeepEqual skips an unchanged value on commit) and :1042-1043
	// ($Changing only for changed fields) at 0ceb202d: an unchanged mode
	// is no change, and the Sink is not called.
	s.mu.RLock()
	same := s.systemMode == v
	s.mu.RUnlock()
	if same {
		return nil
	}
	if s.sink != nil {
		if err := s.sink.SetSystemMode(ctx, v); err != nil {
			return sinkError(fmt.Sprintf("SystemMode %d", v), err)
		}
	}
	s.mu.Lock()
	before := s.eventStateLocked()
	s.systemMode = v
	running := s.followSystemModeLocked(v)
	after := s.eventStateLocked()
	s.mu.Unlock()
	s.changes.NotifyFrom(contract.OriginControllerWrite, thermoAttrSystemMode)
	if running {
		// matter.js sets it as a local actor (asLocalActor, :1196).
		s.changes.NotifyFrom(contract.OriginHostSet, tdef.AttrThermostatRunningMode)
	}
	s.emitChanges(ctx, before, after)
	return nil
}

// writeHold applies a TemperatureSetpointHold the definition admitted. With
// a [Sink] the hold goes to the host first; a refusal stores nothing and
// answers as [Sink] describes. An unchanged hold is no change and does not
// call the Sink (Datasource.ts:760-762 at 0ceb202d, as for SystemMode).
// matter.js stores the value only (ThermostatServer.ts:381 at 862ad75f);
// the Sink call is this module's. The caller holds s.op.
func (s *ThermostatServer) writeHold(ctx context.Context, v uint8) error {
	s.mu.RLock()
	same := s.hold == v
	s.mu.RUnlock()
	if same {
		return nil
	}
	if s.sink != nil {
		if err := s.sink.SetTemperatureSetpointHold(ctx, v); err != nil {
			return sinkError(fmt.Sprintf("TemperatureSetpointHold %d", v), err)
		}
	}
	s.mu.Lock()
	s.hold = v
	s.mu.Unlock()
	s.changes.NotifyFrom(contract.OriginControllerWrite, thermoAttrTemperatureSetpointHold)
	return nil
}

// equalDuration compares two nullable TemperatureSetpointHoldDuration values.
func equalDuration(a, b *uint16) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// writeSetpoint writes an occupied setpoint or a user setpoint limit and
// reconciles the rest, as matter.js ThermostatServer does on
// occupied*Setpoint$Changing (#assertSetpointWithinLimits, then
// #reconcileSetpoints) and on the limits' $Changing
// (#assertLimitWithinAbs, then #reconcileSetpoints). It returns the
// attributes the write changed: the written one and what the reconcile
// moved besides it.
//
// An occupied setpoint goes to the [Sink] after the assert and the
// reconcile, with what the reconcile would move, and before anything is
// stored (matter.js occupied*Setpoint$Changing, ThermostatServer.ts:594-601
// at 0ceb202d: the assert and the reconcile run before the state
// changes). A limit does not: limits are configuration. The Sink runs
// without s.mu; afterwards the value goes onto the state as it is then
// and the reconcile runs on that ([Sink] describes the rule). The caller
// holds s.op.
func (s *ThermostatServer) writeSetpoint(ctx context.Context, attrID uint32, v int) ([]uint32, error) {
	t := map[uint32]struct {
		heat   bool
		limit  bool
		key    string
		unoccu bool
	}{
		thermoAttrOccupiedHeatingSetpoint:   {true, false, spOccupiedHeating, false},
		thermoAttrOccupiedCoolingSetpoint:   {false, false, spOccupiedCooling, false},
		thermoAttrUnoccupiedHeatingSetpoint: {true, false, spUnoccHeating, true},
		thermoAttrUnoccupiedCoolingSetpoint: {false, false, spUnoccCooling, true},
		thermoAttrMinHeatSetpointLimit:      {true, true, spMinHeatLimit, false},
		thermoAttrMaxHeatSetpointLimit:      {true, true, spMaxHeatLimit, false},
		thermoAttrMinCoolSetpointLimit:      {false, true, spMinCoolLimit, false},
		thermoAttrMaxCoolSetpointLimit:      {false, true, spMaxCoolLimit, false},
	}[attrID]
	s.mu.Lock()
	// Served only with its feature, per ValidateWrite.
	cur := s.setpointState()
	// Mirrors matter.js packages/node/src/behavior/state/managed/Datasource.ts:760-762
	// and :1042-1043 at 0ceb202d: an occupied setpoint equal to the stored
	// one is no change — no $Changing, so neither the assert nor the Sink.
	if !t.limit && v == cur.value(attrID) {
		s.mu.Unlock()
		return nil, nil
	}
	st := cur
	if t.limit {
		if err := st.assertLimitWithinAbs(t.heat, v); err != nil {
			s.mu.Unlock()
			return nil, err
		}
	} else if err := st.assertSetpointWithinLimits(t.heat, t.unoccu, v); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	st.assign(attrID, v)
	if err := st.reconcile(t.key); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	if t.limit || s.sink == nil {
		// No port call: computed and stored under one hold of s.mu, so an
		// inbound setter cannot land in between.
		changed := s.commitSetpoints(st)
		s.mu.Unlock()
		s.flushSetpointEvents(ctx)
		if !t.limit {
			s.clearActivePresetOnDirectWrite(ctx, t.unoccu)
		}
		return changed, nil
	}
	// The Sink hears the value the server will store: the written
	// setpoint after the reconcile, not the value the controller wrote.
	// Within the limits the assert admitted the reconcile never moves it
	// (moving it needs v + deadband above MaxCool or v - deadband below
	// MinHeat, which the deadband between the user limits rules out), so
	// in practice the two are equal; the rule is the one the Sink states.
	primary := st.value(attrID)
	saw := withoutChange(setpointDiff(cur, st), attrID)
	s.mu.Unlock()

	if err := s.sink.SetSetpoint(ctx, attrID, int16(primary), saw); err != nil { //nolint:gosec // reconciled values stay within the int16 absolute limits
		return nil, sinkError(fmt.Sprintf("setpoint 0x%04X = %d", attrID, primary), err)
	}

	s.mu.Lock()
	// The state as it is now: an inbound setter may have moved a setpoint
	// while the Sink ran. The consented value goes onto it and the
	// reconcile computes the companions afresh; the limits cannot have
	// moved (only writes change them, and writes wait in s.op).
	// The re-run starts from the value the Sink consented to; should it
	// move that value too, the recomputed one is stored and logged with
	// the companions — the host's own state is the truth.
	now := s.setpointState()
	next := now
	next.assign(attrID, primary)
	if err := next.reconcile(t.key); err != nil {
		// The written value passed the same limits before the Sink ran, so
		// the reconcile fits it on any setpoint the setters can store;
		// should it not, the state the Sink consented to is stored.
		s.logger().Warn("matter.thermostat.sink_reconcile_failed",
			slog.Uint64("attribute", uint64(attrID)), slog.Int("value", primary), slog.String("reason", err.Error()))
		next = st
	}
	if moved := companionsMoved(attrID, st, next, saw, withoutChange(setpointDiff(now, next), attrID)); len(moved) > 0 {
		attrs := make([]any, 0, len(moved)+2)
		attrs = append(attrs, slog.Uint64("attribute", uint64(attrID)), slog.Int("value", primary))
		for _, c := range moved {
			attrs = append(attrs, slog.Int(fmt.Sprintf("0x%04X", c.AttrID), int(c.Value)))
		}
		s.logger().Warn("matter.thermostat.sink_companions_moved", attrs...)
	}
	changed := s.commitSetpoints(next)
	s.mu.Unlock()
	s.flushSetpointEvents(ctx)
	s.clearActivePresetOnDirectWrite(ctx, t.unoccu)
	return changed, nil
}

// withoutChange drops attrID's entry from changes.
func withoutChange(changes []SetpointChange, attrID uint32) []SetpointChange {
	var out []SetpointChange
	for _, c := range changes {
		if c.AttrID != attrID {
			out = append(out, c)
		}
	}
	return out
}

// companionsMoved lists, with the value that will be stored, the written
// setpoint (attrID) and every companion — one the Sink was told about
// (saw) or one the reconcile moves on the re-read state (recomputed) —
// whose stored value differs from the one the Sink's call assumed (st). A
// host that echoed a companion makes no difference; one that stored
// another value does.
func companionsMoved(attrID uint32, st, next setpointState, saw, recomputed []SetpointChange) []SetpointChange {
	named := map[uint32]bool{attrID: true}
	for _, c := range saw {
		named[c.AttrID] = true
	}
	for _, c := range recomputed {
		named[c.AttrID] = true
	}
	var out []SetpointChange
	for _, c := range setpointDiff(st, next) {
		if named[c.AttrID] {
			out = append(out, c)
		}
	}
	return out
}

// OnAttributeChange implements [contract.AttributeChangeNotifier]:
// the setpoints a write's reconcile moved besides the written attribute.
func (s *ThermostatServer) OnAttributeChange(cb func(change contract.Change)) (unsubscribe func()) {
	return s.changes.OnAttributeChange(cb)
}

// MatterInvoke handles SetpointRaiseLower and, with PRES,
// SetActivePresetRequest and AtomicRequest. Without PRES (and without
// MatterScheduleConfiguration, which the server does not serve) AtomicRequest
// is unsupported (matter.js ThermostatServer.ts:130-132 at 862ad75f).
func (s *ThermostatServer) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	if resp, handled, err := s.host.Invoke(ctx, cmdID, fields); handled {
		return resp, err
	}
	switch {
	case cmdID == tdef.CmdSetpointRaiseLower:
		return nil, s.handleSetpointRaiseLower(ctx, fields)
	case cmdID == tdef.CmdSetActivePresetRequest && s.inst.Accepts(cmdID):
		return nil, s.handleSetActivePresetRequest(ctx, fields)
	case cmdID == tdef.CmdSetActiveScheduleRequest && s.inst.Accepts(cmdID):
		return nil, s.handleSetActiveScheduleRequest(ctx, fields)
	case cmdID == tdef.CmdAtomicRequest && s.inst.Accepts(cmdID):
		s.op.Lock()
		defer s.op.Unlock()
		return s.handleAtomicRequest(ctx, fields)
	default:
		return nil, im.UnsupportedCommandf("thermo: command 0x%02X not supported", cmdID)
	}
}

// abs16 returns the absolute value of an int16. Used by the coordinated
// Both-mode setpoint clamp to pick the more limiting overshoot.
func abs16(x int16) int16 {
	if x < 0 {
		return -x
	}
	return x
}

// handleSetpointRaiseLower implements the SetpointRaiseLower command per
// matter.js ThermostatServer.ts:setpointRaiseLower (lines 186-274).
// mode=Heat without HEAT feature → InvalidCommand; mode=Cool without COOL
// feature → InvalidCommand; otherwise apply amount*10 delta, clamped to
// limits; Heat or Cool on an AUTO thermostat moves the other setpoint to
// keep the deadband, InvalidCommand when it cannot (:229-269).
//
// Mode values are SetpointRaiseLowerModeEnum
// (thermostat-cluster.element.ts:510-514): Heat 0x0, Cool 0x1, Both 0x2 —
// the [wire.ThermostatSetpointModeHeat] family, never bare literals.
//
// The new setpoints are computed first; with a [Sink] what moves goes to
// it in one call before anything is stored (cooling primary, heating its
// companion, for Both on a HEAT+COOL thermostat — matter.js's assignment
// order, ThermostatServer.ts:219-220 at 0ceb202d); in matter.js those
// assignments pass occupied*Setpoint$Changing (:594-601), where a host
// refuses. A refusal answers the command and stores neither setpoint.
func (s *ThermostatServer) handleSetpointRaiseLower(ctx context.Context, fields any) error { //nolint:funlen // single-purpose setpoint command handler with many mode/feature branches
	req, err := setpointRaiseLowerRequest(fields)
	if err != nil {
		return err
	}
	mode, amount := uint8(req.Mode), req.Amount

	// matter.js ThermostatServer.ts:189-198: reject Heat/Cool modes when feature absent.
	switch mode {
	case uint8(tdef.SetpointRaiseLowerModeHeat):
		if s.features&ThermostatFeatureHEAT == 0 {
			return thermoInvalidCommandErr{"thermo: SetpointRaiseLower mode=Heat requires HEAT feature"}
		}
	case wire.ThermostatSetpointModeCool:
		if s.features&ThermostatFeatureCOOL == 0 {
			return thermoInvalidCommandErr{"thermo: SetpointRaiseLower mode=Cool requires COOL feature"}
		}
	}

	s.op.Lock()
	defer s.op.Unlock()
	s.mu.RLock()
	// The setpoints in effect: the unoccupied pair while the thermostat is
	// unoccupied (matter.js heatingSetpoint / coolingSetpoint,
	// ThermostatServer.ts:327-361 at 862ad75f).
	unoccupied := !s.occupiedLocked()
	occupHeat, occupCool := s.occupHeat, s.occupCool
	if unoccupied {
		occupHeat, occupCool = s.unoccHeat, s.unoccCool
	}
	minHeat, maxHeat, minCool, maxCool := s.minHeat, s.maxHeat, s.minCool, s.maxCool
	deadband := int(s.minSetpointDeadBand) * 10 // matter.js setpointDeadBand, 0.01 °C
	s.mu.RUnlock()
	auto := s.features&ThermostatFeatureAUTO != 0
	prevHeat, prevCool := occupHeat, occupCool

	// amount is in 0.1°C steps in the command; setpoints are in 0.01°C units.
	// matter.js ThermostatServer.ts:200: amount *= 10.
	delta := int16(amount) * 10

	clampHeat := func(v int16) int16 {
		if v < minHeat {
			return minHeat
		}
		if v > maxHeat {
			return maxHeat
		}
		return v
	}
	clampCool := func(v int16) int16 {
		if v < minCool {
			return minCool
		}
		if v > maxCool {
			return maxCool
		}
		return v
	}

	switch mode {
	case wire.ThermostatSetpointModeBoth:
		heat := s.features&ThermostatFeatureHEAT != 0
		cool := s.features&ThermostatFeatureCOOL != 0
		switch {
		case heat && cool:
			// Coordinated clamp that preserves the deadband: compute each
			// setpoint's overshoot past its limit, then subtract the more
			// limiting overshoot from BOTH setpoints so their spacing is
			// kept. Clamping each independently would skew the deadband.
			// matter.js ThermostatServer.ts:203-220.
			desiredCool := occupCool + delta
			coolLimit := desiredCool - clampCool(desiredCool)
			desiredHeat := occupHeat + delta
			heatLimit := desiredHeat - clampHeat(desiredHeat)
			if coolLimit != 0 || heatLimit != 0 {
				if abs16(coolLimit) <= abs16(heatLimit) {
					desiredHeat -= heatLimit
					desiredCool -= heatLimit
				} else {
					desiredHeat -= coolLimit
					desiredCool -= coolLimit
				}
			}
			occupCool = desiredCool
			occupHeat = desiredHeat
		case cool:
			occupCool = clampCool(occupCool + delta)
		default: // heating-only (matter.js falls through to the heating setpoint)
			occupHeat = clampHeat(occupHeat + delta)
		}
	case wire.ThermostatSetpointModeHeat:
		occupHeat = clampHeat(occupHeat + delta)
		// On an AUTO thermostat the cooling setpoint follows to keep the
		// deadband, or the command is refused. Mirrors matter.js
		// ThermostatServer.ts:251-269 (0ceb202d). Under the AUTO limit
		// invariants (maxCool - maxHeat ≥ deadband, setpoints.go violation)
		// the companion always fits; the refusal is matter.js's guard.
		if auto && int(occupCool)-int(occupHeat) < deadband {
			companion := int(occupHeat) + deadband
			if companion < int(minCool) || companion > int(maxCool) {
				return thermoInvalidCommandErr{"thermo: SetpointRaiseLower could not adjust the cooling setpoint to maintain the deadband"}
			}
			occupCool = int16(companion) //nolint:gosec // within [minCool, maxCool], checked above
		}
	case wire.ThermostatSetpointModeCool:
		occupCool = clampCool(occupCool + delta)
		// The heating setpoint follows the same way. Mirrors matter.js
		// ThermostatServer.ts:229-249 (0ceb202d).
		if auto && int(occupCool)-int(occupHeat) < deadband {
			companion := int(occupCool) - deadband
			if companion < int(minHeat) || companion > int(maxHeat) {
				return thermoInvalidCommandErr{"thermo: SetpointRaiseLower could not adjust the heating setpoint to maintain the deadband"}
			}
			occupHeat = int16(companion) //nolint:gosec // within [minHeat, maxHeat], checked above
		}
	default:
		return thermoInvalidCommandErr{fmt.Sprintf("thermo: SetpointRaiseLower unsupported mode %d", mode)}
	}
	return s.applyRaiseLower(ctx, unoccupied, prevHeat, prevCool, occupHeat, occupCool)
}

// applyRaiseLower hands what SetpointRaiseLower moves to the Sink in one
// call — the cooling setpoint as the primary and the heating setpoint as
// its companion when both move, matter.js's order (ThermostatServer.ts:219-220,
// :222, :224, :247, :269 at 0ceb202d), the one that moves alone otherwise —
// and stores once the call succeeded. A setpoint that does not move is no
// change (Datasource.ts:760-762, :1042-1043): when neither moves, the Sink is
// not called. prevHeat / prevCool are the setpoints the command read.
//
// The Sink runs without s.mu. Once it consented, what the command moved is
// stored as the Sink saw it, over whatever an inbound setter stored for it
// meanwhile; a setpoint the command did not move keeps what is stored
// then. The command runs no reconcile, before the Sink or after it. The
// caller holds s.op.
func (s *ThermostatServer) applyRaiseLower(ctx context.Context, unoccupied bool, prevHeat, prevCool, heat, cool int16) error {
	moves := make([]SetpointChange, 0, 2)
	if cool != prevCool {
		moves = append(moves, SetpointChange{AttrID: setpointAttr(false, unoccupied), Value: cool})
	}
	if heat != prevHeat {
		moves = append(moves, SetpointChange{AttrID: setpointAttr(true, unoccupied), Value: heat})
	}
	if len(moves) == 0 {
		return nil
	}
	if s.sink != nil {
		primary, companions := moves[0], moves[1:]
		if len(companions) == 0 {
			companions = nil
		}
		if err := s.sink.SetSetpoint(ctx, primary.AttrID, primary.Value, companions); err != nil {
			return sinkError(fmt.Sprintf("SetpointRaiseLower setpoint 0x%04X = %d", primary.AttrID, primary.Value), err)
		}
	}
	s.mu.Lock()
	changed := make([]uint32, 0, len(moves))
	for _, m := range moves {
		dst := &s.unoccHeat
		switch m.AttrID {
		case thermoAttrOccupiedCoolingSetpoint:
			dst = &s.occupCool
		case thermoAttrOccupiedHeatingSetpoint:
			dst = &s.occupHeat
		case thermoAttrUnoccupiedCoolingSetpoint:
			dst = &s.unoccCool
		}
		if *dst != m.Value {
			s.queueSetpointEventLocked(m.AttrID, *dst, m.Value)
		}
		*dst = m.Value
		changed = append(changed, m.AttrID)
	}
	s.mu.Unlock()
	s.flushSetpointEvents(ctx)
	// What the command moved reaches the listeners from the command, as
	// applyCommandSetpoints reports it (Datasource.ts:1473-1476).
	slices.Sort(changed)
	s.changes.NotifyFrom(contract.OriginCommand, changed...)
	return nil
}

// applyCommandSetpoints stores the setpoints a command moved (next against
// cur): what moves goes to the [Sink] in one call first — the heating
// setpoint as the primary, matter.js's assignment order for
// setActivePresetRequest (ThermostatServer.ts:309-314 at 862ad75f) — and a
// refusal stores nothing. The caller holds s.op.
func (s *ThermostatServer) applyCommandSetpoints(ctx context.Context, cur, next setpointState, what string) error {
	moves := setpointDiff(cur, next)
	if len(moves) == 0 {
		return nil
	}
	slices.SortStableFunc(moves, func(a, b SetpointChange) int {
		return setpointOrder(a.AttrID) - setpointOrder(b.AttrID)
	})
	if s.sink != nil {
		primary, companions := moves[0], moves[1:]
		if len(companions) == 0 {
			companions = nil
		}
		if err := s.sink.SetSetpoint(ctx, primary.AttrID, primary.Value, companions); err != nil {
			return sinkError(fmt.Sprintf("%s setpoint 0x%04X = %d", what, primary.AttrID, primary.Value), err)
		}
	}
	s.mu.Lock()
	changed := s.commitSetpoints(next)
	s.mu.Unlock()
	s.flushSetpointEvents(ctx)
	s.changes.NotifyFrom(contract.OriginCommand, changed...)
	return nil
}

// setpointOrder puts the setpoints a command moves first, heating before
// cooling, then the limits.
func setpointOrder(attrID uint32) int {
	switch attrID {
	case thermoAttrOccupiedHeatingSetpoint, thermoAttrUnoccupiedHeatingSetpoint:
		return 0
	case thermoAttrOccupiedCoolingSetpoint, thermoAttrUnoccupiedCoolingSetpoint:
		return 1
	}
	return 2
}

// occupiedLocked is matter.js `occupied` (ThermostatServer.ts:327-329 at
// 862ad75f): the occupancy with OCC, occupied otherwise. The caller holds
// s.mu.
func (s *ThermostatServer) occupiedLocked() bool {
	return s.features&ThermostatFeatureOCC == 0 || s.occupied
}

// SetOccupied records the occupancy the host measured — matter.js's
// externallyMeasuredOccupancy (ThermostatServer.ts:568, :573-575 at
// 862ad75f): Occupancy follows and is announced, and the setpoints in
// effect for SetpointRaiseLower and SetActivePresetRequest become the
// occupied or the unoccupied pair. Without OCC it is logged and ignored.
func (s *ThermostatServer) SetOccupied(occupied bool) {
	if s.features&ThermostatFeatureOCC == 0 {
		s.logger().Warn("matter.thermostat.inbound_occupancy_ignored", slog.String("reason", "feature not served"))
		return
	}
	s.mu.Lock()
	changed := s.occupied != occupied
	s.occupied = occupied
	s.mu.Unlock()
	if changed {
		s.changes.NotifyFrom(contract.OriginHostSet, thermoAttrOccupancy)
	}
}

// SetAbsoluteSetpointLimit changes an absolute setpoint limit
// (AbsMin/AbsMax Heat/Cool SetpointLimit) as the device's own change: the
// user limits must stay within it, or the change is refused with
// CONSTRAINT_ERROR — matter.js #assertUserSetpointLimits
// (ThermostatServer.ts:830-853 at 862ad75f), which the absolute limits'
// change reactors run. The attributes are read-only to a controller.
func (s *ThermostatServer) SetAbsoluteSetpointLimit(attrID uint32, v int16) error {
	heat := attrID == thermoAttrAbsMinHeatSetpointLimit || attrID == thermoAttrAbsMaxHeatSetpointLimit
	if !heat && attrID != thermoAttrAbsMinCoolSetpointLimit && attrID != thermoAttrAbsMaxCoolSetpointLimit {
		return fmt.Errorf("thermo: 0x%04X is not an absolute setpoint limit", attrID)
	}
	if !s.inst.Serves(attrID) {
		return spec.Errorf(im.StatusUnsupportedAttribute, "thermo: attribute 0x%04X is not served", attrID)
	}
	s.op.Lock()
	defer s.op.Unlock()
	s.mu.Lock()
	st := s.setpointState()
	switch attrID {
	case thermoAttrAbsMinHeatSetpointLimit:
		st.absMinHeat = int(v)
	case thermoAttrAbsMaxHeatSetpointLimit:
		st.absMaxHeat = int(v)
	case thermoAttrAbsMinCoolSetpointLimit:
		st.absMinCool = int(v)
	default:
		st.absMaxCool = int(v)
	}
	if err := st.assertUserSetpointLimits(heat); err != nil {
		s.mu.Unlock()
		return err
	}
	changed := false
	for _, f := range []struct {
		dst *int16
		v   int
	}{{&s.absMinHeat, st.absMinHeat}, {&s.absMaxHeat, st.absMaxHeat}, {&s.absMinCool, st.absMinCool}, {&s.absMaxCool, st.absMaxCool}} {
		if int(*f.dst) != f.v {
			*f.dst = int16(f.v) //nolint:gosec // an int16 given
			changed = true
		}
	}
	s.mu.Unlock()
	if changed {
		s.changes.NotifyFrom(contract.OriginHostSet, attrID)
	}
	return nil
}

// SetpointRaiseLower field tags (thermostat-cluster.element.ts:322-323).
const (
	setpointRaiseLowerFieldMode   uint8 = 0
	setpointRaiseLowerFieldAmount uint8 = 1
)

// setpointRaiseLowerRequest normalises the command payload the bridge
// hands over. The bridge decodes the command through the generated
// definition, so a real invocation arrives as
// [tdef.SetpointRaiseLowerRequest] (a missing field already answered
// INVALID_COMMAND there, as matter.js's request schema answers it), by
// value or by pointer — the primary form; a host that decodes the command
// itself may pass the tag-keyed map of the bridge's generic salvage path
// (bridge/fields_reader.go decodeGenericTagMap: unsigned as uint64, signed
// as int64). The "mode" / "amount"
// string-keyed map stays accepted for direct callers.
//
// Both fields are conformance M (element :322-323), so a payload without
// Mode is refused rather than acknowledged as a no-op.
func setpointRaiseLowerRequest(fields any) (tdef.SetpointRaiseLowerRequest, error) {
	if req, ok := spec.RequestOf[tdef.SetpointRaiseLowerRequest](fields); ok {
		return req, nil
	}
	switch v := fields.(type) {
	case *tdef.SetpointRaiseLowerRequest: // nil: RequestOf takes a non-nil one
		return tdef.SetpointRaiseLowerRequest{}, thermoInvalidCommandErr{"thermo: SetpointRaiseLower carried no fields"}
	case map[uint8]any:
		rawMode, ok := v[setpointRaiseLowerFieldMode]
		if !ok {
			return tdef.SetpointRaiseLowerRequest{}, thermoInvalidCommandErr{"thermo: SetpointRaiseLower is missing the mandatory Mode field"}
		}
		mode, ok := cluster.AsUint8(rawMode)
		if !ok {
			return tdef.SetpointRaiseLowerRequest{}, thermoInvalidCommandErr{fmt.Sprintf("thermo: SetpointRaiseLower Mode is %T, want an enum8", rawMode)}
		}
		rawAmount, ok := v[setpointRaiseLowerFieldAmount]
		if !ok {
			return tdef.SetpointRaiseLowerRequest{}, thermoInvalidCommandErr{"thermo: SetpointRaiseLower is missing the mandatory Amount field"}
		}
		amount, ok := cluster.AsInt16(rawAmount)
		if !ok || amount < math.MinInt8 || amount > math.MaxInt8 {
			return tdef.SetpointRaiseLowerRequest{}, thermoInvalidCommandErr{fmt.Sprintf("thermo: SetpointRaiseLower Amount %v (%T) is not an int8", rawAmount, rawAmount)}
		}
		return tdef.SetpointRaiseLowerRequest{Mode: tdef.SetpointRaiseLowerModeEnum(mode), Amount: int8(amount)}, nil
	case map[string]any:
		var req tdef.SetpointRaiseLowerRequest
		rawMode, has := v["mode"]
		if !has {
			return tdef.SetpointRaiseLowerRequest{}, thermoInvalidCommandErr{"thermo: SetpointRaiseLower is missing the mandatory Mode field"}
		}
		mode, ok := cluster.AsUint8(rawMode)
		if !ok {
			return tdef.SetpointRaiseLowerRequest{}, thermoInvalidCommandErr{fmt.Sprintf("thermo: SetpointRaiseLower mode is %T, want an enum8", rawMode)}
		}
		req.Mode = tdef.SetpointRaiseLowerModeEnum(mode)
		if rawAmount, has := v["amount"]; has {
			amount, ok := cluster.AsInt16(rawAmount)
			if !ok || amount < math.MinInt8 || amount > math.MaxInt8 {
				return tdef.SetpointRaiseLowerRequest{}, thermoInvalidCommandErr{fmt.Sprintf("thermo: SetpointRaiseLower amount %v (%T) is not an int8", rawAmount, rawAmount)}
			}
			req.Amount = int8(amount)
		}
		return req, nil
	default:
		return tdef.SetpointRaiseLowerRequest{}, thermoInvalidCommandErr{
			fmt.Sprintf("thermo: SetpointRaiseLower expected thermostat.SetpointRaiseLowerRequest or map[uint8]any, got %T", fields),
		}
	}
}

// controlSequenceOfOperation derives the ControlSequenceOfOperationEnum
// from the advertised feature set.
func controlSequenceOfOperation(features uint32) uint8 {
	hasHeat := features&ThermostatFeatureHEAT != 0
	hasCool := features&ThermostatFeatureCOOL != 0
	switch {
	case hasHeat && hasCool:
		return uint8(tdef.ControlSequenceOfOperationCoolingAndHeating)
	case hasCool:
		return uint8(tdef.ControlSequenceOfOperationCoolingOnly)
	default:
		return uint8(tdef.ControlSequenceOfOperationHeatingOnly)
	}
}

// MatterAcceptedCommands implements [contract.ClusterCommandLister]:
// SetpointRaiseLower, the only mandatory command, with PRES
// SetActivePresetRequest, with MSCH SetActiveScheduleRequest, and with
// either AtomicRequest. Without the lister the dispatcher synthesised an empty
// AcceptedCommandList, and a controller that reads the list before
// invoking — Apple Home and Google Home both do — saw a thermostat that
// accepts nothing. Found by the chip-tool data-model sweep.
func (s *ThermostatServer) MatterAcceptedCommands() []uint32 { return s.inst.MatterAcceptedCommands() }

// MatterGeneratedCommands implements [contract.ClusterCommandLister]:
// AtomicResponse with PRES; SetpointRaiseLower and SetActivePresetRequest
// answer with a status only.
func (s *ThermostatServer) MatterGeneratedCommands() []uint32 {
	return s.inst.MatterGeneratedCommands()
}

// MinWritePrivilege implements [contract.ClusterAttributeWritePrivilege]:
// the occupied setpoints are "RW VO", the rest "RW VM".
func (s *ThermostatServer) MinWritePrivilege(attrID uint32) uint8 {
	return s.inst.MinWritePrivilege(attrID)
}

// MinInvokePrivilege implements [contract.ClusterCommandInvokePrivilege].
func (s *ThermostatServer) MinInvokePrivilege(cmdID uint32) uint8 {
	return s.inst.MinInvokePrivilege(cmdID)
}

// MatterReportable returns attributes that emit reports on change outside
// a write: the room temperature and SystemMode.
func (s *ThermostatServer) MatterReportable() []uint32 {
	return []uint32{
		thermoAttrLocalTemperature,
		thermoAttrSystemMode,
	}
}

// MatterAttributes returns the attributes the selection makes mandatory
// and the optional ones the server serves for it (servedOptional) —
// chip-tool and Apple Home validate AttributeList conformance.
func (s *ThermostatServer) MatterAttributes() []uint32 { return s.inst.MatterAttributes() }

// MatterEvents implements [contract.ClusterEventLister]: the TEVT events
// the selection emits (servedEvents), none without TEVT.
func (s *ThermostatServer) MatterEvents() []uint32 { return s.inst.MatterEvents() }

// thermoConstraintErr is a typed [im.StatusCodeError] returned when a
// setpoint or SystemMode write violates limit constraints.
// Mirrors matter.js ThermostatServer.ts:#assertSetpointWithinLimits and
// #assertSystemModeChanging which both throw ConstraintErrorError.
type thermoConstraintErr struct{ msg string }

func (e thermoConstraintErr) Error() string                 { return e.msg }
func (thermoConstraintErr) MatterStatusCode() im.StatusCode { return im.StatusConstraintError }

// thermoInvalidCommandErr is a typed [im.StatusCodeError] returned when a
// SetpointRaiseLower request is invalid.
// Mirrors matter.js ThermostatServer.ts:setpointRaiseLower which throws
// InvalidCommandError for unsupported modes.
type thermoInvalidCommandErr struct{ msg string }

func (e thermoInvalidCommandErr) Error() string                 { return e.msg }
func (thermoInvalidCommandErr) MatterStatusCode() im.StatusCode { return im.StatusInvalidCommand }

// Compile-time assertions.
var (
	_ contract.ClusterServer          = (*ThermostatServer)(nil)
	_ contract.ClusterAttributeLister = (*ThermostatServer)(nil)
	_ contract.FabricScopedReader     = (*ThermostatServer)(nil)
	_ im.StatusCodeError              = thermoConstraintErr{}
	_ im.StatusCodeError              = thermoInvalidCommandErr{}
)

// logger is the ThermostatServer's logger; a value built without its constructor
// logs through slog.Default().
func (s *ThermostatServer) logger() *slog.Logger {
	if s.log != nil {
		return s.log
	}
	return slog.Default()
}

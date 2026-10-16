// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package thermo provides the Matter Thermostat cluster server (0x0201).
// The server supports HEAT-only, COOL-only, and HEAT+COOL (with optional
// AUTO) configurations, enforcing conformance rules on feature-gated
// attributes per Matter Application Cluster Specification §4.3.
//
// The server holds its own state. Without a host port it answers every
// write and command without forwarding it anywhere — the reference daemon
// (examples/reference-bridge) mounts it that way, to put the surface in
// front of chip-tool. A host that drives a device sets
// [ThermostatConfig.Sink]: SystemMode and occupied-setpoint changes a
// controller asks for reach the host before they are stored, and the
// host refuses one by returning an error. The device's own changes come
// back through [ThermostatServer.SetSystemMode],
// [ThermostatServer.SetOccupiedHeatingSetpoint],
// [ThermostatServer.SetOccupiedCoolingSetpoint] and
// [ThermostatServer.SetLocalTemperature], which do not call the Sink.
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
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sync"

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
	ThermostatFeatureHEAT = uint32(tdef.FeatureHeating)                    // bit 0 — Heating
	ThermostatFeatureCOOL = uint32(tdef.FeatureCooling)                    // bit 1 — Cooling
	ThermostatFeatureAUTO = uint32(tdef.FeatureAutoMode)                   // bit 5 — AutoMode (requires HEAT+COOL)
	ThermostatFeatureLTNE = uint32(tdef.FeatureLocalTemperatureNotExposed) // bit 6 — LocalTemperatureNotExposed
)

// servedFeatures are the features whose elements the server serves.
// Occupancy (the Unoccupied setpoints), MatterScheduleConfiguration and
// Presets (their attributes and commands) and the rest are not.
const servedFeatures = ThermostatFeatureHEAT | ThermostatFeatureCOOL | ThermostatFeatureAUTO | ThermostatFeatureLTNE

// ErrFeatures reports a ThermostatConfig.Features the server refuses.
var ErrFeatures = errors.New("thermostat: feature selection not served")

// ErrSetpoints reports a ThermostatConfig whose setpoint limits and
// initial setpoints violate the setpoint rules in a way [New] cannot
// repair — an absolute minimum above its maximum, or an AUTO deadband the
// absolute limits leave no room for.
var ErrSetpoints = errors.New("thermostat: setpoint configuration cannot be fixed")

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
)

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
	// Sink is the optional host port; nil keeps the server self-contained.
	Sink Sink
}

// Sink is the optional host port: told about every SystemMode and
// occupied-setpoint change a controller asks for — a write of SystemMode,
// OccupiedHeatingSetpoint or OccupiedCoolingSetpoint, and the setpoints a
// SetpointRaiseLower moves — after the server validated it and before
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
// The setpoint limits (Min/MaxHeat/CoolSetpointLimit) are configuration,
// not device commands: their writes do not call the Sink, and neither
// does LocalTemperatureCalibration. The host's own changes
// ([ThermostatServer.SetSystemMode], [ThermostatServer.SetOccupiedHeatingSetpoint],
// [ThermostatServer.SetOccupiedCoolingSetpoint]) do not call it.
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
	// OccupiedCoolingSetpoint (attrID) to value, with companions: the
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
}

// SetpointChange is one attribute a setpoint change would move besides
// the one asked for: its id and the new value.
type SetpointChange struct {
	AttrID uint32
	Value  int16
}

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
	// op serialises the controller's changes (writes, commands): the
	// queue, held across the Sink, never taken by the inbound setters. mu
	// guards the values; the setters take only mu, and it is not held
	// while the Sink runs (issue #63).
	op       sync.Mutex
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

	systemMode uint8 // SystemModeEnum value

	// changes reports the setpoints a write's reconcile moved.
	changes cluster.AttributeChanges
}

// New constructs the cluster. AUTO is silently cleared from the feature
// set when HEAT+COOL are not both present. A selection with neither HEAT
// nor COOL — which matter.js's FeatureSelectionErrors refuses, HEAT and
// COOL being "AUTO, O.a+" (thermostat-cluster.element.ts) — or naming a
// feature the server does not serve (Occupancy, Setback, the schedule,
// preset, event and suggestion features) is refused with [ErrFeatures].
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
	inst, err := spec.New(tdef.Definition, spec.Options{Features: features, Attributes: servedOptional(features)})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFeatures, err)
	}

	s := &ThermostatServer{
		features:            features,
		inst:                inst,
		sink:                cfg.Sink,
		localTempCalib:      0,
		minSetpointDeadBand: 20, // 2.0°C default per matter.js packages/model/src/standard/elements/thermostat-cluster.element.ts MinSetpointDeadBand default
	}

	if features&ThermostatFeatureHEAT != 0 {
		s.absMinHeat = cfg.AbsMinHeatSetpointLimit
		s.absMaxHeat = cfg.AbsMaxHeatSetpointLimit
		s.occupHeat = cfg.InitialHeatingSetpoint
		s.minHeat = cfg.AbsMinHeatSetpointLimit
		s.maxHeat = cfg.AbsMaxHeatSetpointLimit
	}
	if features&ThermostatFeatureCOOL != 0 {
		s.absMinCool = cfg.AbsMinCoolSetpointLimit
		s.absMaxCool = cfg.AbsMaxCoolSetpointLimit
		s.occupCool = cfg.InitialCoolingSetpoint
		s.minCool = cfg.AbsMinCoolSetpointLimit
		s.maxCool = cfg.AbsMaxCoolSetpointLimit
	}

	st := s.setpointState()
	violation, adjusted, err := st.reconcileInitial()
	if err != nil {
		return nil, err
	}
	if violation != "" {
		s.commitSetpoints(st)
		slog.Default().Warn("matter.thermostat.initial_setpoints_adjusted",
			slog.String("violation", violation),
			slog.String("adjusted", adjustedText(adjusted)))
	}

	// SystemMode default: Auto with the AUTO feature, Heat with HEAT,
	// Cool when COOL-only.
	switch {
	case features&ThermostatFeatureAUTO != 0:
		s.systemMode = uint8(tdef.SystemModeAuto)
	case features&ThermostatFeatureHEAT != 0:
		s.systemMode = uint8(tdef.SystemModeHeat)
	default: // COOL-only; New refused a selection with neither
		s.systemMode = uint8(tdef.SystemModeCool)
	}
	return s, nil
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
func (s *ThermostatServer) SetLocalTemperature(t *int16) {
	s.mu.Lock()
	s.localTemp = t
	s.mu.Unlock()
}

// SetSystemMode records a SystemMode the device took on itself: stored and
// reported through the change notifier, without the Sink. A mode the
// definition does not admit for the selection (Auto without AUTO, Heat or
// EmergencyHeat without HEAT, Cool or Precooling without COOL —
// thermostat-cluster.element.ts SystemModeEnum) cannot be served: it is
// logged and the stored mode kept.
func (s *ThermostatServer) SetSystemMode(mode uint8) {
	if _, err := s.inst.ValidateWrite(thermoAttrSystemMode, mode, nil); err != nil {
		slog.Default().Warn("matter.thermostat.inbound_system_mode_ignored",
			slog.Int("system_mode", int(mode)), slog.String("reason", err.Error()))
		return
	}
	s.mu.Lock()
	changed := s.systemMode != mode
	s.systemMode = mode
	s.mu.Unlock()
	if changed {
		s.changes.Notify(thermoAttrSystemMode)
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
		slog.Default().Warn("matter.thermostat.inbound_setpoint_ignored",
			slog.String("attribute", name), slog.Int("value", int(v)), slog.String("reason", "feature not served"))
		return
	}
	s.mu.Lock()
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
	s.mu.Unlock()
	if err != nil {
		slog.Default().Warn("matter.thermostat.inbound_setpoint_ignored",
			slog.String("attribute", name), slog.Int("value", int(v)), slog.String("reason", err.Error()))
		return
	}
	if n != int(v) {
		slog.Default().Warn("matter.thermostat.inbound_setpoint_clamped",
			slog.String("attribute", name), slog.Int("value", int(v)), slog.Int("stored", n),
			slog.Int("min", lo), slog.Int("max", hi))
	}
	s.changes.Notify(changed...)
}

// sinkError maps a Sink refusal onto the answer: an error carrying an IM
// status passes through as is, any other answers FAILURE.
func sinkError(what string, err error) error {
	var sc im.StatusCodeError
	if errors.As(err, &sc) {
		return err
	}
	return spec.Errorf(im.StatusFailure, "thermostat: %s refused: %v", what, err)
}

// MatterRead implements [contract.ClusterServer]. An attribute the
// selection does not serve returns (nil, false) — the IM dispatcher
// handles the UnsupportedAttribute response.
func (s *ThermostatServer) MatterRead(attrID uint32) (any, bool) {
	if v, ok := s.inst.ReadGlobal(attrID); ok {
		return v, true
	}
	if !s.inst.Serves(attrID) {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

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
	s.op.Lock()
	defer s.op.Unlock()
	switch attrID {
	case thermoAttrOccupiedHeatingSetpoint, thermoAttrOccupiedCoolingSetpoint,
		thermoAttrMinHeatSetpointLimit, thermoAttrMaxHeatSetpointLimit,
		thermoAttrMinCoolSetpointLimit, thermoAttrMaxCoolSetpointLimit:
		n, _ := v.(int64) // an int16 per ValidateWrite
		coupled, err := s.writeSetpoint(ctx, attrID, int(n))
		if err != nil {
			return err
		}
		// The written attribute is reported by the write path; what the
		// reconcile moved besides it is reported here.
		s.changes.Notify(coupled...)
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
		s.mu.Lock()
		s.localTempCalib = int16(n) //nolint:gosec // an int8 per ValidateWrite
		s.mu.Unlock()
		return nil
	default: // thermoAttrSystemMode, the last writable attribute
		n, _ := v.(uint64)                      // an enum8 per ValidateWrite, its conformance checked
		return s.writeSystemMode(ctx, uint8(n)) //nolint:gosec // an enum8 per ValidateWrite
	}
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
	s.systemMode = v
	s.mu.Unlock()
	return nil
}

// writeSetpoint writes an occupied setpoint or a user setpoint limit and
// reconciles the rest, as matter.js ThermostatServer does on
// occupied*Setpoint$Changing (#assertSetpointWithinLimits, then
// #reconcileSetpoints) and on the limits' $Changing
// (#assertLimitWithinAbs, then #reconcileSetpoints). It returns the
// attributes the reconcile moved besides the written one.
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
		heat  bool
		limit bool
		key   string
	}{
		thermoAttrOccupiedHeatingSetpoint: {true, false, spOccupiedHeating},
		thermoAttrOccupiedCoolingSetpoint: {false, false, spOccupiedCooling},
		thermoAttrMinHeatSetpointLimit:    {true, true, spMinHeatLimit},
		thermoAttrMaxHeatSetpointLimit:    {true, true, spMaxHeatLimit},
		thermoAttrMinCoolSetpointLimit:    {false, true, spMinCoolLimit},
		thermoAttrMaxCoolSetpointLimit:    {false, true, spMaxCoolLimit},
	}[attrID]
	s.mu.Lock()
	// Served only with its feature, per ValidateWrite.
	cur := s.setpointState()
	// Mirrors matter.js packages/node/src/behavior/state/managed/Datasource.ts:760-762
	// and :1042-1043 at 0ceb202d: an occupied setpoint equal to the stored
	// one is no change — no $Changing, so neither the assert nor the Sink.
	if (attrID == thermoAttrOccupiedHeatingSetpoint && v == cur.occupHeat) ||
		(attrID == thermoAttrOccupiedCoolingSetpoint && v == cur.occupCool) {
		s.mu.Unlock()
		return nil, nil
	}
	st := cur
	if t.limit {
		if err := st.assertLimitWithinAbs(t.heat, v); err != nil {
			s.mu.Unlock()
			return nil, err
		}
	} else if err := st.assertSetpointWithinLimits(t.heat, v); err != nil {
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
		coupled := withoutAttr(s.commitSetpoints(st), attrID)
		s.mu.Unlock()
		return coupled, nil
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
	defer s.mu.Unlock()
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
		slog.Default().Warn("matter.thermostat.sink_reconcile_failed",
			slog.Uint64("attribute", uint64(attrID)), slog.Int("value", primary), slog.String("reason", err.Error()))
		next = st
	}
	if moved := companionsMoved(attrID, st, next, saw, withoutChange(setpointDiff(now, next), attrID)); len(moved) > 0 {
		attrs := make([]any, 0, len(moved)+2)
		attrs = append(attrs, slog.Uint64("attribute", uint64(attrID)), slog.Int("value", primary))
		for _, c := range moved {
			attrs = append(attrs, slog.Int(fmt.Sprintf("0x%04X", c.AttrID), int(c.Value)))
		}
		slog.Default().Warn("matter.thermostat.sink_companions_moved", attrs...)
	}
	return withoutAttr(s.commitSetpoints(next), attrID), nil
}

// withoutAttr drops attrID from ids.
func withoutAttr(ids []uint32, attrID uint32) []uint32 {
	var out []uint32
	for _, id := range ids {
		if id != attrID {
			out = append(out, id)
		}
	}
	return out
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

// OnMatterAttributesChanged implements [contract.AttributeChangeNotifier]:
// the setpoints a write's reconcile moved besides the written attribute.
func (s *ThermostatServer) OnMatterAttributesChanged(cb func(attrIDs []uint32)) (unsubscribe func()) {
	return s.changes.OnMatterAttributesChanged(cb)
}

// MatterInvoke handles SetpointRaiseLower and weekly-schedule commands.
func (s *ThermostatServer) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	switch cmdID {
	case tdef.CmdSetpointRaiseLower:
		return nil, s.handleSetpointRaiseLower(ctx, fields)
	default:
		return nil, im.UnsupportedCommandf("thermostat: command 0x%02X not supported", cmdID)
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
	mode, amount := req.Mode, req.Amount

	// matter.js ThermostatServer.ts:189-198: reject Heat/Cool modes when feature absent.
	switch mode {
	case uint8(tdef.SetpointRaiseLowerModeHeat):
		if s.features&ThermostatFeatureHEAT == 0 {
			return thermoInvalidCommandErr{"thermostat: SetpointRaiseLower mode=Heat requires HEAT feature"}
		}
	case wire.ThermostatSetpointModeCool:
		if s.features&ThermostatFeatureCOOL == 0 {
			return thermoInvalidCommandErr{"thermostat: SetpointRaiseLower mode=Cool requires COOL feature"}
		}
	}

	s.op.Lock()
	defer s.op.Unlock()
	s.mu.RLock()
	occupHeat, occupCool := s.occupHeat, s.occupCool
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
				return thermoInvalidCommandErr{"thermostat: SetpointRaiseLower could not adjust the cooling setpoint to maintain the deadband"}
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
				return thermoInvalidCommandErr{"thermostat: SetpointRaiseLower could not adjust the heating setpoint to maintain the deadband"}
			}
			occupHeat = int16(companion) //nolint:gosec // within [minHeat, maxHeat], checked above
		}
	default:
		return thermoInvalidCommandErr{fmt.Sprintf("thermostat: SetpointRaiseLower unsupported mode %d", mode)}
	}
	return s.applyRaiseLower(ctx, prevHeat, prevCool, occupHeat, occupCool)
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
func (s *ThermostatServer) applyRaiseLower(ctx context.Context, prevHeat, prevCool, heat, cool int16) error {
	moves := make([]SetpointChange, 0, 2)
	if cool != prevCool {
		moves = append(moves, SetpointChange{AttrID: thermoAttrOccupiedCoolingSetpoint, Value: cool})
	}
	if heat != prevHeat {
		moves = append(moves, SetpointChange{AttrID: thermoAttrOccupiedHeatingSetpoint, Value: heat})
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
	for _, m := range moves {
		if m.AttrID == thermoAttrOccupiedCoolingSetpoint {
			s.occupCool = m.Value
		} else {
			s.occupHeat = m.Value
		}
	}
	s.mu.Unlock()
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
// INVALID_COMMAND there, as matter.js's request schema answers it); a host
// that decodes the command itself may pass [wire.SetpointRaiseLowerRequest],
// the raw TLV or the tag-keyed map of the bridge's generic salvage path
// (bridge/fields_reader.go decodeGenericTagMap: unsigned as uint64, signed
// as int64). The "mode" /
// "amount" string-keyed map stays accepted for direct callers.
//
// Both fields are conformance M (element :322-323), so a payload without
// Mode is refused rather than acknowledged as a no-op.
func setpointRaiseLowerRequest(fields any) (wire.SetpointRaiseLowerRequest, error) {
	switch v := fields.(type) {
	case tdef.SetpointRaiseLowerRequest:
		return wire.SetpointRaiseLowerRequest{Mode: uint8(v.Mode), Amount: v.Amount}, nil
	case *tdef.SetpointRaiseLowerRequest:
		if v == nil {
			return wire.SetpointRaiseLowerRequest{}, thermoInvalidCommandErr{"thermostat: SetpointRaiseLower carried no fields"}
		}
		return setpointRaiseLowerRequest(*v)
	case wire.SetpointRaiseLowerRequest:
		return v, nil
	case *wire.SetpointRaiseLowerRequest:
		if v == nil {
			return wire.SetpointRaiseLowerRequest{}, thermoInvalidCommandErr{"thermostat: SetpointRaiseLower carried no fields"}
		}
		return *v, nil
	case []byte:
		req, err := wire.DecodeSetpointRaiseLower(v)
		if err != nil {
			return wire.SetpointRaiseLowerRequest{}, thermoInvalidCommandErr{fmt.Sprintf("thermostat: SetpointRaiseLower: %v", err)}
		}
		return req, nil
	case map[uint8]any:
		rawMode, ok := v[setpointRaiseLowerFieldMode]
		if !ok {
			return wire.SetpointRaiseLowerRequest{}, thermoInvalidCommandErr{"thermostat: SetpointRaiseLower is missing the mandatory Mode field"}
		}
		mode, ok := cluster.AsUint8(rawMode)
		if !ok {
			return wire.SetpointRaiseLowerRequest{}, thermoInvalidCommandErr{fmt.Sprintf("thermostat: SetpointRaiseLower Mode is %T, want an enum8", rawMode)}
		}
		rawAmount, ok := v[setpointRaiseLowerFieldAmount]
		if !ok {
			return wire.SetpointRaiseLowerRequest{}, thermoInvalidCommandErr{"thermostat: SetpointRaiseLower is missing the mandatory Amount field"}
		}
		amount, ok := cluster.AsInt16(rawAmount)
		if !ok || amount < math.MinInt8 || amount > math.MaxInt8 {
			return wire.SetpointRaiseLowerRequest{}, thermoInvalidCommandErr{fmt.Sprintf("thermostat: SetpointRaiseLower Amount %v (%T) is not an int8", rawAmount, rawAmount)}
		}
		return wire.SetpointRaiseLowerRequest{Mode: mode, Amount: int8(amount)}, nil
	case map[string]any:
		var req wire.SetpointRaiseLowerRequest
		rawMode, has := v["mode"]
		if !has {
			return wire.SetpointRaiseLowerRequest{}, thermoInvalidCommandErr{"thermostat: SetpointRaiseLower is missing the mandatory Mode field"}
		}
		mode, ok := cluster.AsUint8(rawMode)
		if !ok {
			return wire.SetpointRaiseLowerRequest{}, thermoInvalidCommandErr{fmt.Sprintf("thermostat: SetpointRaiseLower mode is %T, want an enum8", rawMode)}
		}
		req.Mode = mode
		if rawAmount, has := v["amount"]; has {
			amount, ok := cluster.AsInt16(rawAmount)
			if !ok || amount < math.MinInt8 || amount > math.MaxInt8 {
				return wire.SetpointRaiseLowerRequest{}, thermoInvalidCommandErr{fmt.Sprintf("thermostat: SetpointRaiseLower amount %v (%T) is not an int8", rawAmount, rawAmount)}
			}
			req.Amount = int8(amount)
		}
		return req, nil
	default:
		return wire.SetpointRaiseLowerRequest{}, thermoInvalidCommandErr{
			fmt.Sprintf("thermostat: SetpointRaiseLower expected wire.SetpointRaiseLowerRequest, map[uint8]any or []byte, got %T", fields),
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
// SetpointRaiseLower, the only mandatory command; the schedule, preset and
// atomic commands carry MSCH / PRES conformance, features the server never
// serves. Without the lister the dispatcher synthesised an empty
// AcceptedCommandList, and a controller that reads the list before
// invoking — Apple Home and Google Home both do — saw a thermostat that
// accepts nothing. Found by the chip-tool data-model sweep.
func (s *ThermostatServer) MatterAcceptedCommands() []uint32 { return s.inst.MatterAcceptedCommands() }

// MatterGeneratedCommands implements [contract.ClusterCommandLister].
// SetpointRaiseLower answers with a status only.
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
	_ im.StatusCodeError              = thermoConstraintErr{}
	_ im.StatusCodeError              = thermoInvalidCommandErr{}
)

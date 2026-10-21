// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package pump contains the Matter PumpConfigurationAndControl cluster
// server (0x0200), the cluster the Pump device type (0x0303) mandates
// next to OnOff.
//
// The server owns no pump state. The pump's fixed limits arrive in
// [Config]; its live state is read from a [StateSource] on every read;
// the four writable attributes reach the host through the setters it
// implements, after the server has checked each value against the
// cluster's enum, feature and range rules. The alarm events are the
// host's to raise through [Server.Emit], because only the device knows
// when its supply voltage dropped.
//
// matter.js PumpConfigurationAndControlServer
// (packages/node/src/behaviors/pump-configuration-and-control/PumpConfigurationAndControlServer.ts)
// adds nothing to the generated behavior, and neither does this server:
// it is built on the generated definition
// (cluster/spec/pumpconfigurationandcontrol, ADR 0013), which decides the
// attribute list from the features and the declared optionals ("<feature>,
// [AUTO]" limit pairs included), the feature selection ("O.a+" over the
// five control features), the event priorities, the write privileges
// ("RW VM") and every write check — feature-gated enum values, the
// nullable ranges. The OperationMode / ControlMode rule that an unsupported
// mode is CONSTRAINT_ERROR is the specification text matter.js carries in
// pump-configuration-and-control.resource.ts, and the definition's enum
// check answers it so. What remains here is the host port.
package pump

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	pumpdef "github.com/SukramJ/go-fabric/cluster/spec/pumpconfigurationandcontrol"
	clusterwire "github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// ClusterID is the PumpConfigurationAndControl cluster id.
const ClusterID = pumpdef.ClusterID

// DeviceTypePump is the Pump device type (pump.element.ts), which
// mandates this cluster and OnOff.
const DeviceTypePump uint16 = 0x0303

// Attribute ids. AlarmMask (0x22) is deprecated ("D") and not served.
const (
	AttrMaxPressure            = pumpdef.AttrMaxPressure
	AttrMaxSpeed               = pumpdef.AttrMaxSpeed
	AttrMaxFlow                = pumpdef.AttrMaxFlow
	AttrMinConstPressure       = pumpdef.AttrMinConstPressure
	AttrMaxConstPressure       = pumpdef.AttrMaxConstPressure
	AttrMinCompPressure        = pumpdef.AttrMinCompPressure
	AttrMaxCompPressure        = pumpdef.AttrMaxCompPressure
	AttrMinConstSpeed          = pumpdef.AttrMinConstSpeed
	AttrMaxConstSpeed          = pumpdef.AttrMaxConstSpeed
	AttrMinConstFlow           = pumpdef.AttrMinConstFlow
	AttrMaxConstFlow           = pumpdef.AttrMaxConstFlow
	AttrMinConstTemp           = pumpdef.AttrMinConstTemp
	AttrMaxConstTemp           = pumpdef.AttrMaxConstTemp
	AttrPumpStatus             = pumpdef.AttrPumpStatus
	AttrEffectiveOperationMode = pumpdef.AttrEffectiveOperationMode
	AttrEffectiveControlMode   = pumpdef.AttrEffectiveControlMode
	AttrCapacity               = pumpdef.AttrCapacity
	AttrSpeed                  = pumpdef.AttrSpeed
	AttrLifetimeRunningHours   = pumpdef.AttrLifetimeRunningHours
	AttrPower                  = pumpdef.AttrPower
	AttrLifetimeEnergyConsumed = pumpdef.AttrLifetimeEnergyConsumed
	AttrOperationMode          = pumpdef.AttrOperationMode
	AttrControlMode            = pumpdef.AttrControlMode
)

// Feature is a PumpConfigurationAndControl FeatureMap bit. The first five
// carry "O.a+": at least one of them is required.
type Feature = pumpdef.Feature

// FeatureMap bits.
const (
	FeatureConstantPressure    = pumpdef.FeatureConstantPressure    // PRSCONST
	FeatureCompensatedPressure = pumpdef.FeatureCompensatedPressure // PRSCOMP
	FeatureConstantFlow        = pumpdef.FeatureConstantFlow        // FLW
	FeatureConstantSpeed       = pumpdef.FeatureConstantSpeed       // SPD
	FeatureConstantTemperature = pumpdef.FeatureConstantTemperature // TEMP
	FeatureAutomatic           = pumpdef.FeatureAutomatic           // AUTO
	FeatureLocalOperation      = pumpdef.FeatureLocalOperation      // LOCAL
)

// OperationMode is the OperationModeEnum.
type OperationMode = pumpdef.OperationModeEnum

// OperationModeEnum values; Minimum and Maximum need SPD, Local LOCAL.
const (
	OperationNormal  = pumpdef.OperationModeNormal
	OperationMinimum = pumpdef.OperationModeMinimum
	OperationMaximum = pumpdef.OperationModeMaximum
	OperationLocal   = pumpdef.OperationModeLocal
)

// ControlMode is the ControlModeEnum.
type ControlMode = pumpdef.ControlModeEnum

// ControlModeEnum values, each gated on its feature.
const (
	ControlConstantSpeed        = pumpdef.ControlModeConstantSpeed        // SPD
	ControlConstantPressure     = pumpdef.ControlModeConstantPressure     // PRSCONST
	ControlProportionalPressure = pumpdef.ControlModeProportionalPressure // PRSCOMP
	ControlConstantFlow         = pumpdef.ControlModeConstantFlow         // FLW
	ControlConstantTemperature  = pumpdef.ControlModeConstantTemperature  // TEMP
	ControlAutomatic            = pumpdef.ControlModeAutomatic            // AUTO
)

// Status is the PumpStatusBitmap (map16).
type Status = pumpdef.PumpStatusBitmap

// PumpStatusBitmap bits.
const (
	StatusDeviceFault       = pumpdef.PumpStatusDeviceFault
	StatusSupplyFault       = pumpdef.PumpStatusSupplyFault
	StatusSpeedLow          = pumpdef.PumpStatusSpeedLow
	StatusSpeedHigh         = pumpdef.PumpStatusSpeedHigh
	StatusLocalOverride     = pumpdef.PumpStatusLocalOverride
	StatusRunning           = pumpdef.PumpStatusRunning
	StatusRemotePressure    = pumpdef.PumpStatusRemotePressure
	StatusRemoteFlow        = pumpdef.PumpStatusRemoteFlow
	StatusRemoteTemperature = pumpdef.PumpStatusRemoteTemperature
)

// Event ids. Every event is optional and fieldless; DryRunning,
// PumpMotorFatalFailure, PumpBlocked and ElectronicFatalFailure are
// critical, the others info.
const (
	EventSupplyVoltageLow          = pumpdef.EventSupplyVoltageLow
	EventSupplyVoltageHigh         = pumpdef.EventSupplyVoltageHigh
	EventPowerMissingPhase         = pumpdef.EventPowerMissingPhase
	EventSystemPressureLow         = pumpdef.EventSystemPressureLow
	EventSystemPressureHigh        = pumpdef.EventSystemPressureHigh
	EventDryRunning                = pumpdef.EventDryRunning
	EventMotorTemperatureHigh      = pumpdef.EventMotorTemperatureHigh
	EventPumpMotorFatalFailure     = pumpdef.EventPumpMotorFatalFailure
	EventElectronicTemperatureHigh = pumpdef.EventElectronicTemperatureHigh
	EventPumpBlocked               = pumpdef.EventPumpBlocked
	EventSensorFailure             = pumpdef.EventSensorFailure
	EventElectronicNonFatalFailure = pumpdef.EventElectronicNonFatalFailure
	EventElectronicFatalFailure    = pumpdef.EventElectronicFatalFailure
	EventGeneralFault              = pumpdef.EventGeneralFault
	EventLeakage                   = pumpdef.EventLeakage
	EventAirDetection              = pumpdef.EventAirDetection
	EventTurbineOperation          = pumpdef.EventTurbineOperation
)

// Optional names the optional attributes a host declares it serves.
type Optional uint32

// Optional attributes (all conformance "O").
const (
	OptionalPumpStatus Optional = 1 << iota
	OptionalSpeed
	// OptionalLifetimeRunningHours needs a [RunningHoursSetter] source.
	OptionalLifetimeRunningHours
	OptionalPower
	// OptionalLifetimeEnergyConsumed needs an [EnergyConsumedSetter] source.
	OptionalLifetimeEnergyConsumed
	// OptionalControlMode needs a [ControlModeSetter] source.
	OptionalControlMode
	// OptionalAutomaticLimits serves, with AUTO, the Min/Max limit pairs
	// of every control feature that is not advertised — their
	// conformance is "<feature>, [AUTO]".
	OptionalAutomaticLimits
)

// optionalAttributes are the attributes each Optional bit declares.
var optionalAttributes = []struct {
	opt   Optional
	attrs []uint32
}{
	{OptionalPumpStatus, []uint32{AttrPumpStatus}},
	{OptionalSpeed, []uint32{AttrSpeed}},
	{OptionalLifetimeRunningHours, []uint32{AttrLifetimeRunningHours}},
	{OptionalPower, []uint32{AttrPower}},
	{OptionalLifetimeEnergyConsumed, []uint32{AttrLifetimeEnergyConsumed}},
	{OptionalControlMode, []uint32{AttrControlMode}},
	{OptionalAutomaticLimits, []uint32{
		AttrMinConstPressure, AttrMaxConstPressure, AttrMinCompPressure, AttrMaxCompPressure, AttrMinConstSpeed,
		AttrMaxConstSpeed, AttrMinConstFlow, AttrMaxConstFlow, AttrMinConstTemp, AttrMaxConstTemp,
	}},
}

// Limits are the fixed (quality F), nullable limit attributes; nil is
// null, "if the value is invalid" (resource :35-133).
type Limits struct {
	MaxPressure      *int16
	MaxSpeed         *uint16
	MaxFlow          *uint16
	MinConstPressure *int16
	MaxConstPressure *int16
	MinCompPressure  *int16
	MaxCompPressure  *int16
	MinConstSpeed    *uint16
	MaxConstSpeed    *uint16
	MinConstFlow     *uint16
	MaxConstFlow     *uint16
	MinConstTemp     *int16
	MaxConstTemp     *int16
}

// State is one observation of the host's pump, in the generated type
// (pumpconfigurationandcontroldef.State). Pointer fields are the nullable
// attributes; nil is null. Capacity is in 0.005 % units of the effective
// maximum setpoint, Speed in RPM, LifetimeRunningHours a uint24 count of
// hours, Power a uint24 in watts, LifetimeEnergyConsumed in kWh. The
// server reads the operational fields; the limits and AlarmMask are the
// configuration's ([Limits]) and are not read from here.
type State = pumpdef.State

// StateSource is the host port: the live state, and the one mandatory
// writable attribute (pumpconfigurationandcontroldef.StateSource).
type StateSource = pumpdef.StateSource

// ControlModeSetter applies a ControlMode write.
type ControlModeSetter = pumpdef.ControlModeSetter

// RunningHoursSetter applies a LifetimeRunningHours write (reset after
// maintenance, resource :215-216); hours is nil for null.
type RunningHoursSetter = pumpdef.RunningHoursSetter

// EnergyConsumedSetter applies a LifetimeEnergyConsumed write; kWh is
// nil for null.
type EnergyConsumedSetter = pumpdef.EnergyConsumedSetter

// Config carries the construction parameters for [Server].
type Config struct {
	Source   StateSource
	Features Feature
	Optional Optional
	Limits   Limits
	// Events lists the optional events the host raises through
	// [Server.Emit]; they make up EventList.
	Events []uint32
	// DataVersion is an optional host-owned tracker.
	DataVersion *cluster.DataVersionTracker
}

// Configuration errors.
var (
	ErrNoSource        = errors.New("pump: PumpConfigurationAndControl needs a StateSource")
	ErrNoControlMode   = errors.New("pump: at least one of PRSCONST, PRSCOMP, FLW, SPD and TEMP is required")
	ErrUnknownFeature  = errors.New("pump: unknown FeatureMap bits")
	ErrSetterMissing   = errors.New("pump: a writable optional attribute needs its setter on the source")
	ErrAutoLimits      = errors.New("pump: OptionalAutomaticLimits needs the AUTO feature")
	ErrUnknownEvent    = errors.New("pump: not a PumpConfigurationAndControl event")
	ErrEventUndeclared = errors.New("pump: event not declared in Config.Events")
)

// Server implements [contract.ClusterServer] for
// PumpConfigurationAndControl.
type Server struct {
	embedded cluster.DataVersionTracker
	ext      *cluster.DataVersionTracker
	src      StateSource
	cfg      Config
	inst     *spec.Instance
	host     spec.HostHandlers
	veto     spec.WriteVeto

	// mu guards emitter and endpoint.
	mu       sync.Mutex
	emitter  contract.EventEmitter
	endpoint uint16
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
	_ spec.AttributeSetter                    = (*Server)(nil)
	_ spec.CommandRegistrar                   = (*Server)(nil)
	_ spec.EventEmitting                      = (*Server)(nil)
	_ spec.BeforeWriteSetter                  = (*Server)(nil)
)

// NewServer validates cfg against the cluster's conformance and returns
// the server.
func NewServer(cfg Config) (*Server, error) {
	if cfg.Source == nil {
		return nil, ErrNoSource
	}
	if err := spec.CheckFeatures(pumpdef.Definition, uint32(cfg.Features)); err != nil {
		if errors.Is(err, spec.ErrUnknownFeature) {
			return nil, fmt.Errorf("%w: %w", ErrUnknownFeature, err)
		}
		return nil, fmt.Errorf("%w: %w", ErrNoControlMode, err)
	}
	if cfg.Optional&OptionalAutomaticLimits != 0 && cfg.Features&FeatureAutomatic == 0 {
		return nil, ErrAutoLimits
	}
	for _, req := range []struct {
		opt Optional
		ok  bool
	}{
		{OptionalControlMode, implements[ControlModeSetter](cfg.Source)},
		{OptionalLifetimeRunningHours, implements[RunningHoursSetter](cfg.Source)},
		{OptionalLifetimeEnergyConsumed, implements[EnergyConsumedSetter](cfg.Source)},
	} {
		if cfg.Optional&req.opt != 0 && !req.ok {
			return nil, ErrSetterMissing
		}
	}
	opts := spec.Options{Features: uint32(cfg.Features), Events: cfg.Events}
	for _, o := range optionalAttributes {
		if cfg.Optional&o.opt != 0 {
			opts.Attributes = append(opts.Attributes, o.attrs...)
		}
	}
	inst, err := spec.New(pumpdef.Definition, opts)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnknownEvent, err)
	}
	return &Server{src: cfg.Source, ext: cfg.DataVersion, cfg: cfg, inst: inst}, nil
}

func implements[T any](v any) bool {
	_, ok := v.(T)
	return ok
}

// Revision returns the cluster revision of the generated definition.
func Revision() uint16 { return pumpdef.Revision }

func (s *Server) tracker() *cluster.DataVersionTracker {
	if s.ext != nil {
		return s.ext
	}
	return &s.embedded
}

// MatterClusterID returns 0x0200.
func (*Server) MatterClusterID() uint32 { return ClusterID }

// MatterDataVersion implements [contract.ClusterDataVersion].
func (s *Server) MatterDataVersion() uint32 { return s.tracker().Current() }

// MatterAttributes implements [contract.ClusterAttributeLister], in id
// order.
func (s *Server) MatterAttributes() []uint32 { return s.inst.MatterAttributes() }

// MatterReportable lists the served attributes that move: everything
// but the fixed limits (0x0000-0x000C).
func (s *Server) MatterReportable() []uint32 { return s.inst.MatterReportable() }

// MatterAcceptedCommands implements [contract.ClusterCommandLister]; the
// cluster has no commands.
func (s *Server) MatterAcceptedCommands() []uint32 { return s.inst.MatterAcceptedCommands() }

// MatterGeneratedCommands implements [contract.ClusterCommandLister].
func (s *Server) MatterGeneratedCommands() []uint32 { return s.inst.MatterGeneratedCommands() }

// MatterEvents implements [contract.ClusterEventLister]: the declared
// events, in id order.
func (s *Server) MatterEvents() []uint32 { return s.inst.MatterEvents() }

// MatterInvoke rejects every command: the cluster defines none.
func (*Server) MatterInvoke(_ context.Context, cmdID uint32, _ any) (any, error) {
	return nil, im.UnsupportedCommandf("pump: unknown command 0x%02X", cmdID)
}

// SetAttributes implements [spec.AttributeSetter]. The values are checked
// against the attributes' types and constraints first, all or nothing;
// none is then the server's to set: the moving attributes (PumpStatus,
// the effective modes, Capacity, Speed, the lifetime counters, Power,
// OperationMode, ControlMode) are the [StateSource]'s — the host reports
// a change through it — and the limits are fixed by [Config.Limits].
// Each refuses with [spec.ErrNotHeld]; an empty map changes nothing.
func (s *Server) SetAttributes(values map[uint32]any) error {
	ids := slices.Sorted(maps.Keys(values))
	for _, id := range ids {
		if _, err := s.inst.CheckSet(id, values[id], nil); err != nil {
			return err
		}
	}
	if len(ids) > 0 {
		return fmt.Errorf("%w: pump: attribute 0x%04X is the StateSource's or the Config's", spec.ErrNotHeld, ids[0])
	}
	return nil
}

// Handle implements [spec.CommandRegistrar]. The cluster defines no
// command, so every handler is refused with [spec.ErrNotAccepted].
func (s *Server) Handle(cmdID uint32, fn spec.CommandHandler) error {
	return s.host.Register(s.inst, cmdID, fn)
}

// SetBeforeWrite implements [spec.BeforeWriteSetter]: fn vetoes a
// controller's write of OperationMode, ControlMode or a lifetime counter
// after the definition's checks and before it reaches the setter, with the
// value in its stored form (a uint8 enum, a uint32 counter, nil for null).
func (s *Server) SetBeforeWrite(fn func(ctx context.Context, attrID uint32, value any) error) {
	s.veto.Set(fn)
}

// MatterRead resolves an attribute.
func (s *Server) MatterRead(attrID uint32) (any, bool) {
	if v, ok := s.inst.ReadGlobal(attrID); ok {
		return v, true
	}
	if !s.inst.Serves(attrID) {
		return nil, false
	}
	if v, ok := s.readLimit(attrID); ok {
		return v, true
	}
	st := s.src.PumpState()
	switch attrID {
	case AttrPumpStatus:
		// A bitmap carries only its defined bits.
		return uint16(st.PumpStatus) & uint16(pumpdef.PumpStatusBitmapDef.Defined()), true //nolint:gosec // a map16's bits
	case AttrEffectiveOperationMode:
		return uint8(st.EffectiveOperationMode), true
	case AttrEffectiveControlMode:
		return uint8(st.EffectiveControlMode), true
	case AttrCapacity:
		return nullableInt16(st.Capacity), true
	case AttrSpeed:
		return nullableUint16(st.Speed), true
	case AttrLifetimeRunningHours:
		return nullableUint32(st.LifetimeRunningHours, maxUint24), true
	case AttrPower:
		return nullableUint32(st.Power, maxUint24), true
	case AttrLifetimeEnergyConsumed:
		return nullableUint32(st.LifetimeEnergyConsumed, maxUint32), true
	case AttrOperationMode:
		return uint8(st.OperationMode), true
	default: // AttrControlMode, the last one MatterAttributes can list
		return uint8(st.ControlMode), true
	}
}

// readLimit answers the fixed limit attributes from Config.Limits.
func (s *Server) readLimit(attrID uint32) (any, bool) {
	l := s.cfg.Limits
	signed := map[uint32]*int16{
		AttrMaxPressure: l.MaxPressure, AttrMinConstPressure: l.MinConstPressure, AttrMaxConstPressure: l.MaxConstPressure,
		AttrMinCompPressure: l.MinCompPressure, AttrMaxCompPressure: l.MaxCompPressure,
		AttrMinConstTemp: l.MinConstTemp, AttrMaxConstTemp: l.MaxConstTemp,
	}
	if v, ok := signed[attrID]; ok {
		// A numeric lower bound ("min -27315" on the temperature limits).
		if b := pumpdef.Definition.Attribute(attrID).Constraint.Min; b.IsInt() && v != nil && int64(*v) < b.Int {
			floor := int16(b.Int) //nolint:gosec // an int16 attribute's own bound
			v = &floor
		}
		return nullableInt16(v), true
	}
	unsigned := map[uint32]*uint16{
		AttrMaxSpeed: l.MaxSpeed, AttrMaxFlow: l.MaxFlow, AttrMinConstSpeed: l.MinConstSpeed,
		AttrMaxConstSpeed: l.MaxConstSpeed, AttrMinConstFlow: l.MinConstFlow, AttrMaxConstFlow: l.MaxConstFlow,
	}
	if v, ok := unsigned[attrID]; ok {
		return nullableUint16(v), true
	}
	return nil, false
}

// Nullable widths: the null of a nullable int16 is -32768, of a uint16
// 0xFFFF, of a uint24 0xFFFFFF and of a uint32 0xFFFFFFFF, so a real
// value is held one step inside each.
const (
	maxUint24 uint32 = 0xFFFFFE
	maxUint32 uint32 = 0xFFFFFFFE
)

func nullableInt16(v *int16) any {
	if v == nil {
		return nil
	}
	return max(*v, -32767)
}

func nullableUint16(v *uint16) any {
	if v == nil {
		return nil
	}
	return min(*v, 0xFFFE)
}

func nullableUint32(v *uint32, ceiling uint32) any {
	if v == nil {
		return nil
	}
	return min(*v, ceiling)
}

// MatterWrite applies one of the four writable attributes (access
// "RW VM"). The definition checks the write — served, writable, a
// supported enum value or a value inside the nullable range — and the
// setter the host implements applies it.
func (s *Server) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	v, err := s.inst.ValidateWrite(attrID, value, nil)
	if err != nil {
		return err
	}
	if err := s.veto.Check(ctx, attrID, storedForm(attrID, v)); err != nil {
		return err
	}
	n, _ := v.(uint64) // an enum8 or a counter per ValidateWrite; nil for null
	switch attrID {
	case AttrOperationMode:
		err = s.src.SetOperationMode(ctx, OperationMode(n)) //nolint:gosec // an enum8 per ValidateWrite
	case AttrControlMode:
		setter, _ := s.src.(ControlModeSetter)           // guaranteed by NewServer
		err = setter.SetControlMode(ctx, ControlMode(n)) //nolint:gosec // an enum8 per ValidateWrite
	case AttrLifetimeRunningHours:
		setter, _ := s.src.(RunningHoursSetter) // guaranteed by NewServer
		err = setter.SetLifetimeRunningHours(ctx, counter(v))
	default: // AttrLifetimeEnergyConsumed, the last writable attribute
		setter, _ := s.src.(EnergyConsumedSetter) // guaranteed by NewServer
		err = setter.SetLifetimeEnergyConsumed(ctx, counter(v))
	}
	if err != nil {
		return fmt.Errorf("pump: attribute 0x%04X write: %w", attrID, err)
	}
	s.tracker().Bump()
	return nil
}

// storedForm is a value [spec.Instance.ValidateWrite] normalised, in the
// stored form a write veto receives: the two modes as their enum8 (a
// uint8), a lifetime counter as a uint32, nil for null.
func storedForm(attrID uint32, v any) any {
	if attrID == AttrOperationMode || attrID == AttrControlMode {
		n, _ := v.(uint64)
		return uint8(n) //nolint:gosec // an enum8 per ValidateWrite
	}
	if c := counter(v); c != nil {
		return *c
	}
	return nil
}

// counter is a validated nullable lifetime counter: nil for null.
func counter(v any) *uint32 {
	n, ok := v.(uint64)
	if !ok {
		return nil
	}
	c := uint32(n) //nolint:gosec // ≤ the uint32 null-less range per ValidateWrite
	return &c
}

// MinWritePrivilege implements [contract.ClusterAttributeWritePrivilege]:
// every writable attribute of the cluster is "RW VM", written with
// Manage.
func (s *Server) MinWritePrivilege(attrID uint32) uint8 { return s.inst.MinWritePrivilege(attrID) }

// SupportsOperationMode reports whether the configuration offers m:
// Normal always, Minimum and Maximum with SPD, Local with LOCAL.
func (s *Server) SupportsOperationMode(m OperationMode) bool {
	return s.inst.EnumSupported(pumpdef.OperationModeEnumDef, uint64(m))
}

// SupportsControlMode reports whether the configuration offers m, each
// value gated on its feature.
func (s *Server) SupportsControlMode(m ControlMode) bool {
	return s.inst.EnumSupported(pumpdef.ControlModeEnumDef, uint64(m))
}

// SetMatterEventEmitter implements [contract.EventReceiver].
func (s *Server) SetMatterEventEmitter(emitter contract.EventEmitter) {
	s.mu.Lock()
	s.emitter = emitter
	s.mu.Unlock()
}

// SetEndpoint stamps the endpoint the server is mounted on (the bridge
// calls it at assembly); [Server.Endpoint] reads it back.
func (s *Server) SetEndpoint(endpoint uint16) {
	s.mu.Lock()
	s.endpoint = endpoint
	s.mu.Unlock()
}

// Endpoint returns the endpoint the server is mounted on, as stamped by
// [Server.SetEndpoint]; 0 before the bridge assembled it.
func (s *Server) Endpoint() uint16 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.endpoint
}

// Emit implements [spec.EventEmitting]: it raises one of the declared
// alarm events on endpoint, at the priority matter.js declares for it.
// Every alarm event of the cluster is fieldless: data nil (or a
// [clusterwire.FieldlessEvent] or the generated event struct) sends the
// empty structure. Without an emitter wired the event is dropped; an
// event not in Config.Events is refused ([ErrEventUndeclared]), so
// EventList never under-reports what the server sends.
func (s *Server) Emit(endpoint uint16, eventID uint32, data any) error {
	if !s.inst.Emits(eventID) {
		return fmt.Errorf("%w: %w: 0x%02X", ErrEventUndeclared, spec.ErrNotEmitted, eventID)
	}
	s.mu.Lock()
	emitter := s.emitter
	s.mu.Unlock()
	if emitter != nil {
		emitter.MatterEmitEvent(endpoint, ClusterID, eventID, clusterwire.FieldlessEvent{}, s.inst.EventPriority(eventID))
	}
	return nil
}

// SPDX-License-Identifier: MIT
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
// adds nothing to the generated behavior, so this server mirrors what
// that behavior enforces from the model: feature-gated attributes and
// enum members (pump-configuration-and-control.element.ts), the nullable
// ranges, and the write access "RW VM". The OperationMode / ControlMode
// rule that an unsupported mode is CONSTRAINT_ERROR is the specification
// text matter.js carries in pump-configuration-and-control.resource.ts.
package pump

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

// ClusterID is the PumpConfigurationAndControl cluster id
// (pump-configuration-and-control.element.ts:19).
const ClusterID uint32 = 0x0200

// DeviceTypePump is the Pump device type (pump.element.ts), which
// mandates this cluster and OnOff.
const DeviceTypePump uint16 = 0x0303

// Attribute ids (pump-configuration-and-control.element.ts:33-111).
// AlarmMask (0x22) is deprecated ("D") and not served.
const (
	AttrMaxPressure            uint32 = 0x0000 // M, int16, X F
	AttrMaxSpeed               uint32 = 0x0001 // M, uint16, X F
	AttrMaxFlow                uint32 = 0x0002 // M, uint16, X F
	AttrMinConstPressure       uint32 = 0x0003 // PRSCONST, [AUTO]
	AttrMaxConstPressure       uint32 = 0x0004 // PRSCONST, [AUTO]
	AttrMinCompPressure        uint32 = 0x0005 // PRSCOMP, [AUTO]
	AttrMaxCompPressure        uint32 = 0x0006 // PRSCOMP, [AUTO]
	AttrMinConstSpeed          uint32 = 0x0007 // SPD, [AUTO]
	AttrMaxConstSpeed          uint32 = 0x0008 // SPD, [AUTO]
	AttrMinConstFlow           uint32 = 0x0009 // FLW, [AUTO]
	AttrMaxConstFlow           uint32 = 0x000A // FLW, [AUTO]
	AttrMinConstTemp           uint32 = 0x000B // TEMP, [AUTO]
	AttrMaxConstTemp           uint32 = 0x000C // TEMP, [AUTO]
	AttrPumpStatus             uint32 = 0x0010 // O
	AttrEffectiveOperationMode uint32 = 0x0011 // M
	AttrEffectiveControlMode   uint32 = 0x0012 // M
	AttrCapacity               uint32 = 0x0013 // M, int16, X
	AttrSpeed                  uint32 = 0x0014 // O, uint16, X
	AttrLifetimeRunningHours   uint32 = 0x0015 // O, uint24, RW VM, X N
	AttrPower                  uint32 = 0x0016 // O, uint24, X
	AttrLifetimeEnergyConsumed uint32 = 0x0017 // O, uint32, RW VM, X N
	AttrOperationMode          uint32 = 0x0020 // M, RW VM
	AttrControlMode            uint32 = 0x0021 // O, RW VM
)

// Feature is a PumpConfigurationAndControl FeatureMap bit
// (pump-configuration-and-control.element.ts:22-31). The first five
// carry "O.a+": at least one of them is required.
type Feature uint32

// FeatureMap bits.
const (
	FeatureConstantPressure    Feature = 1 << 0 // PRSCONST
	FeatureCompensatedPressure Feature = 1 << 1 // PRSCOMP
	FeatureConstantFlow        Feature = 1 << 2 // FLW
	FeatureConstantSpeed       Feature = 1 << 3 // SPD
	FeatureConstantTemperature Feature = 1 << 4 // TEMP
	FeatureAutomatic           Feature = 1 << 5 // AUTO
	FeatureLocalOperation      Feature = 1 << 6 // LOCAL

	controlFeatures = FeatureConstantPressure | FeatureCompensatedPressure | FeatureConstantFlow |
		FeatureConstantSpeed | FeatureConstantTemperature
	allFeatures = controlFeatures | FeatureAutomatic | FeatureLocalOperation
)

// OperationMode is the OperationModeEnum (element :143-149).
type OperationMode uint8

// OperationModeEnum values; Minimum and Maximum need SPD, Local LOCAL.
const (
	OperationNormal  OperationMode = 0
	OperationMinimum OperationMode = 1
	OperationMaximum OperationMode = 2
	OperationLocal   OperationMode = 3
)

// ControlMode is the ControlModeEnum (element :151-159).
type ControlMode uint8

// ControlModeEnum values, each gated on its feature.
const (
	ControlConstantSpeed        ControlMode = 0 // SPD
	ControlConstantPressure     ControlMode = 1 // PRSCONST
	ControlProportionalPressure ControlMode = 2 // PRSCOMP
	ControlConstantFlow         ControlMode = 3 // FLW
	ControlConstantTemperature  ControlMode = 5 // TEMP
	ControlAutomatic            ControlMode = 7 // AUTO
)

// Status is the PumpStatusBitmap (map16, element :130-141).
type Status uint16

// PumpStatusBitmap bits.
const (
	StatusDeviceFault       Status = 1 << 0
	StatusSupplyFault       Status = 1 << 1
	StatusSpeedLow          Status = 1 << 2
	StatusSpeedHigh         Status = 1 << 3
	StatusLocalOverride     Status = 1 << 4
	StatusRunning           Status = 1 << 5
	StatusRemotePressure    Status = 1 << 6
	StatusRemoteFlow        Status = 1 << 7
	StatusRemoteTemperature Status = 1 << 8
	statusAll                      = Status(1<<9 - 1)
)

// Event ids (element :112-128). Every event is optional and fieldless.
const (
	EventSupplyVoltageLow          uint32 = 0x00
	EventSupplyVoltageHigh         uint32 = 0x01
	EventPowerMissingPhase         uint32 = 0x02
	EventSystemPressureLow         uint32 = 0x03
	EventSystemPressureHigh        uint32 = 0x04
	EventDryRunning                uint32 = 0x05 // critical
	EventMotorTemperatureHigh      uint32 = 0x06
	EventPumpMotorFatalFailure     uint32 = 0x07 // critical
	EventElectronicTemperatureHigh uint32 = 0x08
	EventPumpBlocked               uint32 = 0x09 // critical
	EventSensorFailure             uint32 = 0x0A
	EventElectronicNonFatalFailure uint32 = 0x0B
	EventElectronicFatalFailure    uint32 = 0x0C // critical
	EventGeneralFault              uint32 = 0x0D
	EventLeakage                   uint32 = 0x0E
	EventAirDetection              uint32 = 0x0F
	EventTurbineOperation          uint32 = 0x10
)

// criticalEvents are the four events matter.js declares "critical";
// every other one is "info" (element :112-128).
var criticalEvents = []uint32{EventDryRunning, EventPumpMotorFatalFailure, EventPumpBlocked, EventElectronicFatalFailure}

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

// State is one observation of the host's pump. Pointer fields are the
// nullable attributes; nil is null.
type State struct {
	PumpStatus             Status
	EffectiveOperationMode OperationMode
	EffectiveControlMode   ControlMode
	// Capacity is in 0.005 % units of the effective maximum setpoint.
	Capacity *int16
	// Speed is in RPM.
	Speed *uint16
	// LifetimeRunningHours is a uint24 count of hours.
	LifetimeRunningHours *uint32
	// Power is a uint24 in watts.
	Power *uint32
	// LifetimeEnergyConsumed is in kWh.
	LifetimeEnergyConsumed *uint32
	OperationMode          OperationMode
	ControlMode            ControlMode
}

// StateSource is the host port: the live state, and the one mandatory
// writable attribute.
type StateSource interface {
	PumpState() State
	SetOperationMode(ctx context.Context, mode OperationMode) error
}

// ControlModeSetter applies a ControlMode write.
type ControlModeSetter interface {
	SetControlMode(ctx context.Context, mode ControlMode) error
}

// RunningHoursSetter applies a LifetimeRunningHours write (reset after
// maintenance, resource :215-216); hours is nil for null.
type RunningHoursSetter interface {
	SetLifetimeRunningHours(ctx context.Context, hours *uint32) error
}

// EnergyConsumedSetter applies a LifetimeEnergyConsumed write; kWh is
// nil for null.
type EnergyConsumedSetter interface {
	SetLifetimeEnergyConsumed(ctx context.Context, kWh *uint32) error
}

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
	events   []uint32

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
)

// NewServer validates cfg against the cluster's conformance and returns
// the server.
func NewServer(cfg Config) (*Server, error) {
	if cfg.Source == nil {
		return nil, ErrNoSource
	}
	if cfg.Features&^allFeatures != 0 {
		return nil, fmt.Errorf("%w: 0x%X", ErrUnknownFeature, uint32(cfg.Features&^allFeatures))
	}
	if cfg.Features&controlFeatures == 0 {
		return nil, ErrNoControlMode
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
	events := slices.Clone(cfg.Events)
	slices.Sort(events)
	events = slices.Compact(events)
	for _, e := range events {
		if e > EventTurbineOperation {
			return nil, fmt.Errorf("%w: 0x%02X", ErrUnknownEvent, e)
		}
	}
	return &Server{src: cfg.Source, ext: cfg.DataVersion, cfg: cfg, events: events}, nil
}

func implements[T any](v any) bool {
	_, ok := v.(T)
	return ok
}

// Revision returns the cluster revision from the generated schema.
func Revision() uint16 { return schema.ClusterRevisions[ClusterID] }

func (s *Server) tracker() *cluster.DataVersionTracker {
	if s.ext != nil {
		return s.ext
	}
	return &s.embedded
}

func (s *Server) has(f Feature) bool  { return s.cfg.Features&f != 0 }
func (s *Server) opt(o Optional) bool { return s.cfg.Optional&o != 0 }
func (s *Server) limitPair(f Feature) bool {
	return s.has(f) || (s.has(FeatureAutomatic) && s.opt(OptionalAutomaticLimits))
}

// MatterClusterID returns 0x0200.
func (*Server) MatterClusterID() uint32 { return ClusterID }

// MatterDataVersion implements [contract.ClusterDataVersion].
func (s *Server) MatterDataVersion() uint32 { return s.tracker().Current() }

// MatterAttributes implements [contract.ClusterAttributeLister], in id
// order.
func (s *Server) MatterAttributes() []uint32 {
	out := []uint32{AttrMaxPressure, AttrMaxSpeed, AttrMaxFlow}
	for _, p := range []struct {
		f        Feature
		min, max uint32
	}{
		{FeatureConstantPressure, AttrMinConstPressure, AttrMaxConstPressure},
		{FeatureCompensatedPressure, AttrMinCompPressure, AttrMaxCompPressure},
		{FeatureConstantSpeed, AttrMinConstSpeed, AttrMaxConstSpeed},
		{FeatureConstantFlow, AttrMinConstFlow, AttrMaxConstFlow},
		{FeatureConstantTemperature, AttrMinConstTemp, AttrMaxConstTemp},
	} {
		if s.limitPair(p.f) {
			out = append(out, p.min, p.max)
		}
	}
	if s.opt(OptionalPumpStatus) {
		out = append(out, AttrPumpStatus)
	}
	out = append(out, AttrEffectiveOperationMode, AttrEffectiveControlMode, AttrCapacity)
	for _, o := range []struct {
		opt  Optional
		attr uint32
	}{
		{OptionalSpeed, AttrSpeed},
		{OptionalLifetimeRunningHours, AttrLifetimeRunningHours},
		{OptionalPower, AttrPower},
		{OptionalLifetimeEnergyConsumed, AttrLifetimeEnergyConsumed},
	} {
		if s.opt(o.opt) {
			out = append(out, o.attr)
		}
	}
	out = append(out, AttrOperationMode)
	if s.opt(OptionalControlMode) {
		out = append(out, AttrControlMode)
	}
	return out
}

// MatterReportable lists the served attributes that move: everything
// but the fixed limits (0x0000-0x000C).
func (s *Server) MatterReportable() []uint32 {
	return slices.DeleteFunc(s.MatterAttributes(), func(id uint32) bool { return id <= AttrMaxConstTemp })
}

// MatterAcceptedCommands implements [contract.ClusterCommandLister]; the
// cluster has no commands.
func (*Server) MatterAcceptedCommands() []uint32 { return []uint32{} }

// MatterGeneratedCommands implements [contract.ClusterCommandLister].
func (*Server) MatterGeneratedCommands() []uint32 { return []uint32{} }

// MatterEvents implements [contract.ClusterEventLister]: the declared
// events, in id order.
func (s *Server) MatterEvents() []uint32 { return slices.Clone(s.events) }

// MatterInvoke rejects every command: the cluster defines none.
func (*Server) MatterInvoke(_ context.Context, cmdID uint32, _ any) (any, error) {
	return nil, im.UnsupportedCommandf("pump: unknown command 0x%02X", cmdID)
}

// MatterRead resolves an attribute.
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
	if v, ok := s.readLimit(attrID); ok {
		return v, true
	}
	st := s.src.PumpState()
	switch attrID {
	case AttrPumpStatus:
		return uint16(st.PumpStatus & statusAll), true
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
		if attrID == AttrMinConstTemp || attrID == AttrMaxConstTemp {
			// constraint "min -27315" (element :74, :78).
			if v != nil && *v < -27315 {
				floor := int16(-27315)
				v = &floor
			}
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
// "RW VM", element :95, :100, :104, :108).
func (s *Server) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	if !slices.Contains(s.MatterAttributes(), attrID) {
		return statusError{im.StatusUnsupportedAttribute, fmt.Sprintf("pump: attribute 0x%04X is not served", attrID)}
	}
	var err error
	switch attrID {
	case AttrOperationMode:
		err = s.writeOperationMode(ctx, value)
	case AttrControlMode:
		err = s.writeControlMode(ctx, value)
	case AttrLifetimeRunningHours:
		err = s.writeCounter(ctx, value, maxUint24, func(ctx context.Context, v *uint32) error {
			setter, _ := s.src.(RunningHoursSetter) // guaranteed by NewServer
			return setter.SetLifetimeRunningHours(ctx, v)
		})
	case AttrLifetimeEnergyConsumed:
		err = s.writeCounter(ctx, value, maxUint32, func(ctx context.Context, v *uint32) error {
			setter, _ := s.src.(EnergyConsumedSetter) // guaranteed by NewServer
			return setter.SetLifetimeEnergyConsumed(ctx, v)
		})
	default:
		return statusError{im.StatusUnsupportedWrite, fmt.Sprintf("pump: attribute 0x%04X is read-only", attrID)}
	}
	if err != nil {
		return err
	}
	s.tracker().Bump()
	return nil
}

// MinWritePrivilege implements [contract.ClusterAttributeWritePrivilege]:
// every writable attribute of the cluster is "RW VM", written with
// Manage.
func (*Server) MinWritePrivilege(attrID uint32) uint8 {
	switch attrID {
	case AttrOperationMode, AttrControlMode, AttrLifetimeRunningHours, AttrLifetimeEnergyConsumed:
		return 4 // Manage
	}
	return 3 // Operate
}

// SupportsOperationMode reports whether the configuration offers m:
// Normal always, Minimum and Maximum with SPD, Local with LOCAL
// (element :143-149).
func (s *Server) SupportsOperationMode(m OperationMode) bool {
	switch m {
	case OperationNormal:
		return true
	case OperationMinimum, OperationMaximum:
		return s.has(FeatureConstantSpeed)
	case OperationLocal:
		return s.has(FeatureLocalOperation)
	default:
		return false
	}
}

// SupportsControlMode reports whether the configuration offers m, each
// value gated on its feature (element :151-159).
func (s *Server) SupportsControlMode(m ControlMode) bool {
	switch m {
	case ControlConstantSpeed:
		return s.has(FeatureConstantSpeed)
	case ControlConstantPressure:
		return s.has(FeatureConstantPressure)
	case ControlProportionalPressure:
		return s.has(FeatureCompensatedPressure)
	case ControlConstantFlow:
		return s.has(FeatureConstantFlow)
	case ControlConstantTemperature:
		return s.has(FeatureConstantTemperature)
	case ControlAutomatic:
		return s.has(FeatureAutomatic)
	default:
		return false
	}
}

// writeOperationMode: "In the case a device does not support a specific
// operation mode, the write interaction to this attribute with an
// unsupported operation mode value shall be ignored and a response
// containing the status of CONSTRAINT_ERROR shall be returned"
// (resource :252-254).
func (s *Server) writeOperationMode(ctx context.Context, value any) error {
	n, ok := cluster.AsUintMax(value, 0xFF)
	if !ok || !s.SupportsOperationMode(OperationMode(n)) { //nolint:gosec // ≤ 0xFF per AsUintMax
		return statusError{im.StatusConstraintError, fmt.Sprintf("pump: OperationMode %v is not supported", value)}
	}
	if err := s.src.SetOperationMode(ctx, OperationMode(n)); err != nil { //nolint:gosec // ≤ 0xFF per AsUintMax
		return fmt.Errorf("pump: OperationMode write: %w", err)
	}
	return nil
}

// writeControlMode mirrors writeOperationMode (resource :264-266).
func (s *Server) writeControlMode(ctx context.Context, value any) error {
	n, ok := cluster.AsUintMax(value, 0xFF)
	if !ok || !s.SupportsControlMode(ControlMode(n)) { //nolint:gosec // ≤ 0xFF per AsUintMax
		return statusError{im.StatusConstraintError, fmt.Sprintf("pump: ControlMode %v is not supported", value)}
	}
	setter, _ := s.src.(ControlModeSetter)                             // guaranteed by NewServer
	if err := setter.SetControlMode(ctx, ControlMode(n)); err != nil { //nolint:gosec // ≤ 0xFF per AsUintMax
		return fmt.Errorf("pump: ControlMode write: %w", err)
	}
	return nil
}

// writeCounter validates a nullable lifetime counter against the
// largest non-null value of its width and forwards it.
func (*Server) writeCounter(ctx context.Context, value any, ceiling uint32, set func(context.Context, *uint32) error) error {
	var v *uint32
	if value != nil {
		n, ok := cluster.AsUintMax(value, uint64(ceiling))
		if !ok {
			return statusError{im.StatusConstraintError, fmt.Sprintf("pump: counter %v outside 0..%d", value, ceiling)}
		}
		c := uint32(n) //nolint:gosec // ≤ ceiling (a uint32) per AsUintMax
		v = &c
	}
	if err := set(ctx, v); err != nil {
		return fmt.Errorf("pump: lifetime counter write: %w", err)
	}
	return nil
}

// SetMatterEventEmitter implements [contract.EventReceiver].
func (s *Server) SetMatterEventEmitter(emitter contract.EventEmitter) {
	s.mu.Lock()
	s.emitter = emitter
	s.mu.Unlock()
}

// SetEndpoint stamps the endpoint the events are addressed to.
func (s *Server) SetEndpoint(endpoint uint16) {
	s.mu.Lock()
	s.endpoint = endpoint
	s.mu.Unlock()
}

// Emit raises one of the declared alarm events, at the priority matter.js
// declares for it. The payload is fieldless. Without an emitter wired
// the event is dropped; an event not in Config.Events is refused, so
// EventList never under-reports what the server sends.
func (s *Server) Emit(event uint32) error {
	if !slices.Contains(s.events, event) {
		return fmt.Errorf("%w: 0x%02X", ErrEventUndeclared, event)
	}
	priority := contract.EventPriorityInfo
	if slices.Contains(criticalEvents, event) {
		priority = contract.EventPriorityCritical
	}
	s.mu.Lock()
	emitter, endpoint := s.emitter, s.endpoint
	s.mu.Unlock()
	if emitter != nil {
		emitter.MatterEmitEvent(endpoint, ClusterID, event, clusterwire.FieldlessEvent{}, priority)
	}
	return nil
}

// statusError carries an exact IM status to the dispatcher.
type statusError struct {
	status im.StatusCode
	msg    string
}

func (e statusError) Error() string                   { return e.msg }
func (e statusError) MatterStatusCode() im.StatusCode { return e.status }

var _ im.StatusCodeError = statusError{}

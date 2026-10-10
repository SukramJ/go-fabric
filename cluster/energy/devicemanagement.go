// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package energy holds the servers of the Matter energy-management
// clusters. This file and evse.go add DeviceEnergyManagement (0x0098) and
// EnergyEvse (0x0099); both are built on a generated cluster definition
// (ADR 0013; cluster/spec/deviceenergymanagement, cluster/spec/energyevse)
// and the generated server ([spec.Server]). matter.js's
// DeviceEnergyManagementServer and EvseServer
// (packages/node/src/behaviors/device-energy-management/,
// packages/node/src/behaviors/energy-evse/) are empty subclasses of the
// generated behaviors; the rules here are connectedhomeip's, read at the
// harness pin 6170af8461b10b1766044122ac83332c6d00ab20, each cited where it
// is applied.
package energy

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	dem "github.com/SukramJ/go-fabric/cluster/spec/deviceenergymanagement"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// DeviceEnergyManagement (0x0098). Two chip sources carry its rules, cited
// below by file name and line:
//
//   - Cluster.cpp — src/app/clusters/device-energy-management-server/
//     DeviceEnergyManagementCluster.cpp: the command field checks, the
//     opt-out check and the ESAState preconditions, and the post-conditions
//     it verifies after the delegate ran.
//   - DelegateImpl.cpp — examples/energy-management/device-energy-management/
//     src/DeviceEnergyManagementDelegateImpl.cpp: the delegate every chip
//     energy app (EVSE, water heater, heat pump) shares — the ESAState
//     transitions, the PowerAdjustStart/End, Paused and Resumed events, the
//     adjustment and pause timers, the forecast bookkeeping and the opt-out
//     cancellation. Its manufacturer hooks (DEMManufacturerDelegate.h) are
//     the host port, [DemManager].
//
// PowerRangeAdjustment (the PRA feature, PowerRangeAdjustRequest) exists at
// the pin but not in the snapshot this module is generated from; it is not
// served.

// ClusterIDDeviceEnergyManagement is the DeviceEnergyManagement cluster id.
const ClusterIDDeviceEnergyManagement = dem.ClusterID

// The DeviceEnergyManagement datatypes, the generated ones.
type (
	// DemFeature is a DeviceEnergyManagement FeatureMap bit.
	DemFeature = dem.Feature
	// EsaType is the ESATypeEnum.
	EsaType = dem.ESATypeEnum
	// EsaState is the ESAStateEnum.
	EsaState = dem.ESAStateEnum
	// OptOutState is the OptOutStateEnum.
	OptOutState = dem.OptOutStateEnum
	// AdjustmentCause is the AdjustmentCauseEnum a request carries.
	AdjustmentCause = dem.AdjustmentCauseEnum
	// DemCause is the CauseEnum the PowerAdjustEnd and Resumed events carry.
	DemCause = dem.CauseEnum
	// PowerAdjustCapability is the PowerAdjustCapabilityStruct.
	PowerAdjustCapability = dem.PowerAdjustCapabilityStruct
	// PowerAdjustRange is the PowerAdjustStruct, one entry of
	// PowerAdjustCapability.
	PowerAdjustRange = dem.PowerAdjustStruct
	// PowerAdjustReason is the PowerAdjustReasonEnum.
	PowerAdjustReason = dem.PowerAdjustReasonEnum
	// Forecast is the ForecastStruct.
	Forecast = dem.ForecastStruct
	// ForecastSlot is the SlotStruct.
	ForecastSlot = dem.SlotStruct
	// SlotAdjustment is the SlotAdjustmentStruct of ModifyForecastRequest.
	SlotAdjustment = dem.SlotAdjustmentStruct
	// ForecastConstraint is the ConstraintsStruct of
	// RequestConstraintBasedForecast.
	ForecastConstraint = dem.ConstraintsStruct
	// ForecastUpdateReason is the ForecastUpdateReasonEnum.
	ForecastUpdateReason = dem.ForecastUpdateReasonEnum
)

// DeviceEnergyManagement features.
const (
	DemFeaturePowerAdjustment           = dem.FeaturePowerAdjustment           // PA
	DemFeaturePowerForecastReporting    = dem.FeaturePowerForecastReporting    // PFR
	DemFeatureStateForecastReporting    = dem.FeatureStateForecastReporting    // SFR
	DemFeatureStartTimeAdjustment       = dem.FeatureStartTimeAdjustment       // STA
	DemFeaturePausable                  = dem.FeaturePausable                  // PAU
	DemFeatureForecastAdjustment        = dem.FeatureForecastAdjustment        // FA
	DemFeatureConstraintBasedAdjustment = dem.FeatureConstraintBasedAdjustment // CON
)

// ESA states.
const (
	EsaStateOffline           = dem.ESAStateOffline
	EsaStateOnline            = dem.ESAStateOnline
	EsaStateFault             = dem.ESAStateFault
	EsaStatePowerAdjustActive = dem.ESAStatePowerAdjustActive
	EsaStatePaused            = dem.ESAStatePaused
)

// Opt-out states. LocalOptOut and GridOptOut are single bits, OptOut both
// (DelegateImpl.cpp:1304-1307 tests them with a bitwise and).
const (
	OptOutNone  = dem.OptOutStateNoOptOut
	OptOutLocal = dem.OptOutStateLocalOptOut
	OptOutGrid  = dem.OptOutStateGridOptOut
	OptOutAll   = dem.OptOutStateOptOut
)

// DemManager is the host port: chip's DEMManufacturerDelegate
// (examples/energy-management/device-energy-management/include/
// DEMManufacturerDelegate.h:32-93). Every hook but
// ApproxEnergyDuringSession has a do-nothing default there; embed
// [NopDemManager] for the same. A hook's error fails the command as the
// delegate fails it (FAILURE).
//
// The hooks are called with the server's state lock held, as chip calls
// them from within the delegate: they must not call the server.
type DemManager interface {
	// ApproxEnergyDuringSession is the PowerAdjustEnd event's EnergyUse,
	// in mWh (DelegateImpl.cpp:322-329).
	ApproxEnergyDuringSession() int64
	PowerAdjust(ctx context.Context, powerMw int64, durationS uint32, cause AdjustmentCause) error
	PowerAdjustCompleted() error
	CancelPowerAdjust(cause DemCause) error
	StartTimeAdjust(ctx context.Context, requestedStartTime uint32, cause AdjustmentCause) error
	Pause(ctx context.Context, durationS uint32, cause AdjustmentCause) error
	PauseCompleted() error
	CancelPause(cause DemCause) error
	CancelRequest(ctx context.Context) error
	ModifyForecast(ctx context.Context, forecastID uint32, adjustments []SlotAdjustment, cause AdjustmentCause) error
	RequestConstraintBasedForecast(ctx context.Context, constraints []ForecastConstraint, cause AdjustmentCause) error
}

// NopDemManager is DEMManufacturerDelegate's defaults: every hook
// succeeds and does nothing, and the session used no energy.
type NopDemManager struct{}

var _ DemManager = NopDemManager{}

// ApproxEnergyDuringSession implements [DemManager].
func (NopDemManager) ApproxEnergyDuringSession() int64 { return 0 }

// PowerAdjust implements [DemManager].
func (NopDemManager) PowerAdjust(context.Context, int64, uint32, AdjustmentCause) error {
	return nil
}

// PowerAdjustCompleted implements [DemManager].
func (NopDemManager) PowerAdjustCompleted() error { return nil }

// CancelPowerAdjust implements [DemManager].
func (NopDemManager) CancelPowerAdjust(DemCause) error { return nil }

// StartTimeAdjust implements [DemManager].
func (NopDemManager) StartTimeAdjust(context.Context, uint32, AdjustmentCause) error {
	return nil
}

// Pause implements [DemManager].
func (NopDemManager) Pause(context.Context, uint32, AdjustmentCause) error { return nil }

// PauseCompleted implements [DemManager].
func (NopDemManager) PauseCompleted() error { return nil }

// CancelPause implements [DemManager].
func (NopDemManager) CancelPause(DemCause) error { return nil }

// CancelRequest implements [DemManager].
func (NopDemManager) CancelRequest(context.Context) error { return nil }

// ModifyForecast implements [DemManager].
func (NopDemManager) ModifyForecast(context.Context, uint32, []SlotAdjustment, AdjustmentCause) error {
	return nil
}

// RequestConstraintBasedForecast implements [DemManager].
func (NopDemManager) RequestConstraintBasedForecast(context.Context, []ForecastConstraint, AdjustmentCause) error {
	return nil
}

// DeviceEnergyConfig carries the construction parameters.
type DeviceEnergyConfig struct {
	Features DemFeature
	// EsaType and EsaCanGenerate are fixed (quality "F").
	EsaType        EsaType
	EsaCanGenerate bool
	// EsaState is the state the server starts in; chip's delegate starts
	// Offline (DelegateImpl.cpp:39) and its EVSE app sets Online at init
	// (examples/evse-app/evse-common/src/EVSEManufacturerImpl.cpp:73).
	EsaState EsaState
	// AbsMinPower and AbsMaxPower are in mW.
	AbsMinPower int64
	AbsMaxPower int64
	// Manager carries out the requests; required.
	Manager DemManager
	// Now is the wall clock the epoch-s times are read from; nil is
	// time.Now.
	Now func() time.Time
	// DataVersion is an optional host-owned tracker.
	DataVersion *cluster.DataVersionTracker
}

// ErrNoDemManager is a DeviceEnergyConfig without a Manager.
var ErrNoDemManager = errors.New("energy: DeviceEnergyManagement needs an DemManager")

// DeviceEnergyManagementServer implements [contract.ClusterServer] for
// DeviceEnergyManagement. Reads, the data version, the change
// notifications and the event priorities are the generated server's; the
// lists, globals and privileges its embedded [spec.Instance]'s. What stays
// here is chip's cluster checks and its shared delegate's state machine.
type DeviceEnergyManagementServer struct {
	*spec.Instance

	srv     *spec.Server
	manager DemManager
	now     func() time.Time

	// mu serialises the delegate state, as chip's single-threaded stack
	// does; it is held across the manager hooks (see [DemManager]).
	mu                    sync.Mutex
	endpoint              uint16
	powerAdjustInProgress bool
	powerAdjustStartUtc   uint32
	powerAdjustTimer      *time.Timer
	pauseInProgress       bool
	pauseTimer            *time.Timer
}

// Compile-time assertions.
var (
	_ contract.ClusterServer                 = (*DeviceEnergyManagementServer)(nil)
	_ contract.ClusterDataVersion            = (*DeviceEnergyManagementServer)(nil)
	_ contract.ClusterAttributeLister        = (*DeviceEnergyManagementServer)(nil)
	_ contract.ClusterCommandLister          = (*DeviceEnergyManagementServer)(nil)
	_ contract.ClusterEventLister            = (*DeviceEnergyManagementServer)(nil)
	_ contract.ClusterCommandInvokePrivilege = (*DeviceEnergyManagementServer)(nil)
	_ contract.AttributeChangeNotifier       = (*DeviceEnergyManagementServer)(nil)
	_ contract.EventReceiver                 = (*DeviceEnergyManagementServer)(nil)
)

// NewDeviceEnergyManagement builds a DeviceEnergyManagement server.
// PowerAdjustmentCapability and Forecast start null and OptOutState
// NoOptOut, as chip's delegate starts (DelegateImpl.cpp:38-43; the two
// structs are default-constructed nullables).
func NewDeviceEnergyManagement(cfg DeviceEnergyConfig) (*DeviceEnergyManagementServer, error) {
	if cfg.Manager == nil {
		return nil, ErrNoDemManager
	}
	srv, err := spec.NewServer(dem.Definition, spec.Options{Features: uint32(cfg.Features)}, spec.ServerConfig{DataVersion: cfg.DataVersion})
	if err != nil {
		return nil, fmt.Errorf("energy: %w", err)
	}
	s := &DeviceEnergyManagementServer{Instance: srv.Instance, srv: srv, manager: cfg.Manager, now: cfg.Now}
	if s.now == nil {
		s.now = time.Now
	}
	values := map[uint32]any{
		dem.AttrEsaType:        cfg.EsaType,
		dem.AttrEsaCanGenerate: cfg.EsaCanGenerate,
		dem.AttrEsaState:       cfg.EsaState,
		dem.AttrAbsMinPower:    cfg.AbsMinPower,
		dem.AttrAbsMaxPower:    cfg.AbsMaxPower,
	}
	if s.Serves(dem.AttrPowerAdjustmentCapability) {
		values[dem.AttrPowerAdjustmentCapability] = nil
	}
	if s.Serves(dem.AttrForecast) {
		values[dem.AttrForecast] = nil
	}
	if s.Serves(dem.AttrOptOutState) {
		values[dem.AttrOptOutState] = OptOutNone
	}
	if err := srv.SetAttributes(values); err != nil {
		return nil, fmt.Errorf("energy: %w", err)
	}
	srv.Handle(dem.CmdPowerAdjustRequest, s.powerAdjustRequest)
	srv.Handle(dem.CmdCancelPowerAdjustRequest, s.cancelPowerAdjustRequest)
	srv.Handle(dem.CmdStartTimeAdjustRequest, s.startTimeAdjustRequest)
	srv.Handle(dem.CmdPauseRequest, s.pauseRequest)
	srv.Handle(dem.CmdResumeRequest, s.resumeRequest)
	srv.Handle(dem.CmdModifyForecastRequest, s.modifyForecastRequest)
	srv.Handle(dem.CmdRequestConstraintBasedForecast, s.requestConstraintBasedForecast)
	srv.Handle(dem.CmdCancelRequest, s.cancelRequest)
	return s, nil
}

// matterEpochOffset is the Unix time of the Matter epoch,
// 2000-01-01T00:00:00Z.
const matterEpochOffset = 946684800

// matterEpochS is t in Matter epoch seconds.
func matterEpochS(t time.Time) uint32 {
	return uint32(t.Unix() - matterEpochOffset) //nolint:gosec // a wall clock after 2000 and before 2136
}

func (s *DeviceEnergyManagementServer) nowEpoch() uint32 { return matterEpochS(s.now()) }

// --- reads --------------------------------------------------------------

// EsaState returns the ESAState.
func (s *DeviceEnergyManagementServer) EsaState() EsaState {
	v, _ := s.srv.Value(dem.AttrEsaState)
	n, _ := v.(uint8)
	return EsaState(n)
}

// AbsMinPower returns AbsMinPower in mW.
func (s *DeviceEnergyManagementServer) AbsMinPower() int64 {
	v, _ := s.srv.Value(dem.AttrAbsMinPower)
	n, _ := v.(int64)
	return n
}

// AbsMaxPower returns AbsMaxPower in mW.
func (s *DeviceEnergyManagementServer) AbsMaxPower() int64 {
	v, _ := s.srv.Value(dem.AttrAbsMaxPower)
	n, _ := v.(int64)
	return n
}

// OptOutState returns the OptOutState; NoOptOut when it is not served.
func (s *DeviceEnergyManagementServer) OptOutState() OptOutState {
	v, _ := s.srv.Value(dem.AttrOptOutState)
	n, _ := v.(uint8)
	return OptOutState(n)
}

// PowerAdjustmentCapability returns the capability; nil while null.
func (s *DeviceEnergyManagementServer) PowerAdjustmentCapability() *PowerAdjustCapability {
	v, _ := s.srv.Value(dem.AttrPowerAdjustmentCapability)
	if c, ok := v.(PowerAdjustCapability); ok {
		return &c
	}
	return nil
}

// Forecast returns the forecast; nil while null.
func (s *DeviceEnergyManagementServer) Forecast() *Forecast {
	v, _ := s.srv.Value(dem.AttrForecast)
	if f, ok := v.(Forecast); ok {
		return &f
	}
	return nil
}

// --- device-side setters ------------------------------------------------

// SetEsaState records the ESA's state (DelegateImpl.cpp:1190-1207).
func (s *DeviceEnergyManagementServer) SetEsaState(state EsaState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.setLocked(dem.AttrEsaState, state)
}

// SetAbsMinPower records AbsMinPower in mW (DelegateImpl.cpp:1209-1221).
func (s *DeviceEnergyManagementServer) SetAbsMinPower(mW int64) error {
	return s.set(dem.AttrAbsMinPower, mW)
}

// SetAbsMaxPower records AbsMaxPower in mW (DelegateImpl.cpp:1223-1235).
func (s *DeviceEnergyManagementServer) SetAbsMaxPower(mW int64) error {
	return s.set(dem.AttrAbsMaxPower, mW)
}

// SetPowerAdjustmentCapability records the capability; nil is null
// (DelegateImpl.cpp:1237-1248).
func (s *DeviceEnergyManagementServer) SetPowerAdjustmentCapability(c *PowerAdjustCapability) error {
	if c == nil {
		return s.set(dem.AttrPowerAdjustmentCapability, nil)
	}
	return s.set(dem.AttrPowerAdjustmentCapability, *c)
}

// SetForecast records the forecast; nil is null (DelegateImpl.cpp:
// 1262-1272).
func (s *DeviceEnergyManagementServer) SetForecast(f *Forecast) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.setForecastLocked(f)
}

// SetOptOutState records the user's opt-out and cancels what it opts out
// of, as chip's delegate does (DelegateImpl.cpp:1274-1377): a Local opt-out
// over a Grid one, or the other way round, is OptOut; a running power
// adjustment or pause whose cause the new state opts out of ends with
// cause UserOptOut; a forecast adjusted for an opted-out cause returns to
// InternalOptimization.
func (s *DeviceEnergyManagementServer) SetOptOutState(state OptOutState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.OptOutState()
	next := state
	if (old == OptOutGrid && state == OptOutLocal) || (old == OptOutLocal && state == OptOutGrid) { // :1283-1291
		next = OptOutAll
	}
	if err := s.setLocked(dem.AttrOptOutState, next); err != nil {
		return err
	}
	local, grid := next&OptOutLocal != 0, next&OptOutGrid != 0
	var errs []error
	if s.powerAdjustInProgress { // :1302-1311
		if c := s.PowerAdjustmentCapability(); c != nil &&
			((local && c.Cause == dem.PowerAdjustReasonLocalOptimizationAdjustment) ||
				(grid && c.Cause == dem.PowerAdjustReasonGridOptimizationAdjustment)) {
			errs = append(errs, s.cancelPowerAdjustLocked(dem.CauseUserOptOut))
		}
	}
	if s.pauseInProgress { // :1314-1324
		if f := s.Forecast(); f != nil &&
			((local && f.ForecastUpdateReason == dem.ForecastUpdateReasonLocalOptimization) ||
				(grid && f.ForecastUpdateReason == dem.ForecastUpdateReasonGridOptimization)) {
			errs = append(errs, s.cancelPauseLocked(dem.CauseUserOptOut))
		}
	}
	if f := s.Forecast(); f != nil { // :1339-1374
		if (f.ForecastUpdateReason == dem.ForecastUpdateReasonLocalOptimization && local) ||
			(f.ForecastUpdateReason == dem.ForecastUpdateReasonGridOptimization && grid) {
			f.ForecastUpdateReason = dem.ForecastUpdateReasonInternalOptimization
			errs = append(errs, s.setForecastLocked(f))
		}
	}
	return errors.Join(errs...)
}

func (s *DeviceEnergyManagementServer) set(attrID uint32, v any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.setLocked(attrID, v)
}

func (s *DeviceEnergyManagementServer) setLocked(attrID uint32, v any) error {
	if err := s.srv.Set(attrID, v); err != nil {
		return fmt.Errorf("energy: DeviceEnergyManagement: %w", err)
	}
	return nil
}

func (s *DeviceEnergyManagementServer) setForecastLocked(f *Forecast) error {
	if f == nil {
		return s.setLocked(dem.AttrForecast, nil)
	}
	return s.setLocked(dem.AttrForecast, *f)
}

// setCapabilityCauseLocked is SetPowerAdjustmentCapabilityPowerAdjustReason
// (DelegateImpl.cpp:1250-1260); chip dereferences the capability
// unchecked, a null one is left null here.
func (s *DeviceEnergyManagementServer) setCapabilityCauseLocked(reason PowerAdjustReason) error {
	c := s.PowerAdjustmentCapability()
	if c == nil {
		return nil
	}
	c.Cause = reason
	return s.setLocked(dem.AttrPowerAdjustmentCapability, *c)
}

func (s *DeviceEnergyManagementServer) emitLocked(eventID uint32, data any) error {
	if err := s.srv.Emit(s.endpoint, eventID, data); err != nil {
		return fmt.Errorf("energy: %w", err)
	}
	return nil
}

// --- commands -----------------------------------------------------------

// checkOptOut is CheckOptOutAllowsRequest (Cluster.cpp:275-322): a cause
// outside the enum is CONSTRAINT_ERROR (chip's INVALID_VALUE, the same
// code 0x87), as is a cause the user opted out of.
func (s *DeviceEnergyManagementServer) checkOptOut(cause AdjustmentCause) error {
	if cause > dem.AdjustmentCauseGridOptimization { // :280-284
		return spec.Errorf(im.StatusConstraintError, "energy: adjustment cause %d is invalid", cause)
	}
	switch s.OptOutState() {
	case OptOutNone: // :288-290
		return nil
	case OptOutLocal: // :292-301
		if cause == dem.AdjustmentCauseGridOptimization {
			return nil
		}
	case OptOutGrid: // :303-312
		if cause == dem.AdjustmentCauseLocalOptimization {
			return nil
		}
	case OptOutAll: // :314-316
	default: // :318-320
		return spec.Errorf(im.StatusConstraintError, "energy: invalid OptOutState %d", s.OptOutState())
	}
	return spec.Errorf(im.StatusConstraintError, "energy: the user opted out of cause %d", cause)
}

func demFailure(format string, args ...any) error {
	return spec.Errorf(im.StatusFailure, "energy: "+format, args...)
}

func demConstraint(format string, args ...any) error {
	return spec.Errorf(im.StatusConstraintError, "energy: "+format, args...)
}

func demInvalidInState(format string, args ...any) error {
	return spec.Errorf(im.StatusInvalidInState, "energy: "+format, args...)
}

// energyRequest unwraps a decoded command payload, given by value or by
// pointer; anything else is INVALID_COMMAND.
func energyRequest[T any](name string, fields any) (T, error) {
	switch r := fields.(type) {
	case *T:
		if r != nil {
			return *r, nil
		}
	case T:
		return r, nil
	}
	var zero T
	return zero, spec.Errorf(im.StatusInvalidCommand, "energy: %s fields %T", name, fields)
}

// withinRange is IsWithinRange (Cluster.cpp:66-82).
func withinRange(power int64, duration uint32, c PowerAdjustCapability) bool {
	if c.PowerAdjustCapability.Null {
		return false
	}
	for _, r := range c.PowerAdjustCapability.Value {
		if power >= r.MinPower && duration >= r.MinDuration && power <= r.MaxPower && duration <= r.MaxDuration {
			return true
		}
	}
	return false
}

// powerAdjustReason is AdjustmentCauseToPowerAdjustReason (Cluster.cpp:
// 51-62).
func powerAdjustReason(cause AdjustmentCause) (PowerAdjustReason, bool) {
	switch cause {
	case dem.AdjustmentCauseLocalOptimization:
		return dem.PowerAdjustReasonLocalOptimizationAdjustment, true
	case dem.AdjustmentCauseGridOptimization:
		return dem.PowerAdjustReasonGridOptimizationAdjustment, true
	}
	return 0, false
}

// forecastReason is the delegate's cause-to-reason switch
// (DelegateImpl.cpp:359-371, :688-699).
func forecastReason(cause AdjustmentCause) (ForecastUpdateReason, bool) {
	switch cause {
	case dem.AdjustmentCauseLocalOptimization:
		return dem.ForecastUpdateReasonLocalOptimization, true
	case dem.AdjustmentCauseGridOptimization:
		return dem.ForecastUpdateReasonGridOptimization, true
	}
	return 0, false
}

// powerAdjustRequest is HandlePowerAdjustRequest (Cluster.cpp:324-379)
// over the delegate's PowerAdjustRequest (DelegateImpl.cpp:100-181).
func (s *DeviceEnergyManagementServer) powerAdjustRequest(ctx context.Context, fields any) (any, error) {
	r, err := energyRequest[dem.PowerAdjustRequestRequest]("PowerAdjustRequest", fields)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkOptOut(r.Cause); err != nil { // :332
		return nil, err
	}
	c := s.PowerAdjustmentCapability()
	if c == nil { // :336-340
		return nil, demConstraint("PowerAdjustmentCapability is null")
	}
	if !withinRange(r.Power, r.Duration, *c) { // :342-347
		return nil, demConstraint("power %d mW for %d s is outside PowerAdjustmentCapability", r.Power, r.Duration)
	}
	if st := s.EsaState(); st != EsaStateOnline && st != EsaStatePowerAdjustActive { // :349-353
		return nil, demInvalidInState("ESAState %d", st)
	}
	if err := s.delegatePowerAdjustLocked(ctx, r); err != nil {
		return nil, err
	}
	// The cluster verifies the delegate's post-condition (:362-376).
	want, _ := powerAdjustReason(r.Cause)
	if c := s.PowerAdjustmentCapability(); c == nil || c.Cause != want {
		return nil, demConstraint("PowerAdjustmentCapability cause not updated")
	}
	return nil, nil
}

func (s *DeviceEnergyManagementServer) delegatePowerAdjustLocked(ctx context.Context, r dem.PowerAdjustRequestRequest) error {
	generateEvent := false
	if s.powerAdjustInProgress { // :106-109
		s.stopPowerAdjustTimerLocked()
	} else { // :110-122
		generateEvent = true
		s.powerAdjustStartUtc = s.nowEpoch()
	}
	if err := s.manager.PowerAdjust(ctx, r.Power, r.Duration, r.Cause); err != nil { // :125-132
		return demFailure("PowerAdjustRequest: %v", err)
	}
	if err := s.setLocked(dem.AttrEsaState, EsaStatePowerAdjustActive); err != nil { // :134
		return err
	}
	reason, ok := powerAdjustReason(r.Cause)
	if !ok { // :148-150
		s.powerAdjustFailureLocked()
		return demFailure("PowerAdjustRequest: cause %d", r.Cause)
	}
	if err := s.setCapabilityCauseLocked(reason); err != nil { // :138-146
		return err
	}
	s.powerAdjustInProgress = true                                                                   // :155
	s.powerAdjustTimer = time.AfterFunc(time.Duration(r.Duration)*time.Second, s.powerAdjustExpired) // :157
	if generateEvent {                                                                               // :166-178
		if err := s.emitLocked(dem.EventPowerAdjustStart, dem.PowerAdjustStartEvent{}); err != nil {
			s.powerAdjustFailureLocked()
			return demFailure("PowerAdjustStart: %v", err)
		}
	}
	return nil
}

// powerAdjustFailureLocked is HandlePowerAdjustRequestFailure
// (DelegateImpl.cpp:188-200).
func (s *DeviceEnergyManagementServer) powerAdjustFailureLocked() {
	s.stopPowerAdjustTimerLocked()
	_ = s.setLocked(dem.AttrEsaState, EsaStateOnline)
	s.powerAdjustInProgress = false
	_ = s.setCapabilityCauseLocked(dem.PowerAdjustReasonNoAdjustment)
}

func (s *DeviceEnergyManagementServer) stopPowerAdjustTimerLocked() {
	if s.powerAdjustTimer != nil {
		s.powerAdjustTimer.Stop()
		s.powerAdjustTimer = nil
	}
}

func (s *DeviceEnergyManagementServer) stopPauseTimerLocked() {
	if s.pauseTimer != nil {
		s.pauseTimer.Stop()
		s.pauseTimer = nil
	}
}

// powerAdjustExpired is HandlePowerAdjustTimerExpiry (DelegateImpl.cpp:
// 222-241).
func (s *DeviceEnergyManagementServer) powerAdjustExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.powerAdjustInProgress {
		return
	}
	s.powerAdjustTimer = nil
	s.powerAdjustInProgress = false
	_ = s.setLocked(dem.AttrEsaState, EsaStateOnline)
	_ = s.setCapabilityCauseLocked(dem.PowerAdjustReasonNoAdjustment)
	_ = s.powerAdjustEndLocked(dem.CauseNormalCompletion)
	_ = s.manager.PowerAdjustCompleted()
}

// powerAdjustEndLocked is GeneratePowerAdjustEndEvent (DelegateImpl.cpp:
// 304-339).
func (s *DeviceEnergyManagementServer) powerAdjustEndLocked(cause DemCause) error {
	return s.emitLocked(dem.EventPowerAdjustEnd, dem.PowerAdjustEndEvent{
		Cause:     cause,
		Duration:  s.nowEpoch() - s.powerAdjustStartUtc,
		EnergyUse: s.manager.ApproxEnergyDuringSession(),
	})
}

// cancelPowerAdjustLocked is CancelPowerAdjustRequestAndGenerateEvent
// (DelegateImpl.cpp:278-298).
func (s *DeviceEnergyManagementServer) cancelPowerAdjustLocked(cause DemCause) error {
	s.stopPowerAdjustTimerLocked()
	_ = s.setLocked(dem.AttrEsaState, EsaStateOnline)
	s.powerAdjustInProgress = false
	_ = s.setCapabilityCauseLocked(dem.PowerAdjustReasonNoAdjustment)
	_ = s.powerAdjustEndLocked(cause) // :287; its error is overwritten by the hook's (:294)
	return s.manager.CancelPowerAdjust(cause)
}

// cancelPowerAdjustRequest is HandleCancelPowerAdjustRequest (Cluster.cpp:
// 381-414) over CancelPowerAdjustRequest (DelegateImpl.cpp:255-266).
func (s *DeviceEnergyManagementServer) cancelPowerAdjustRequest(_ context.Context, _ any) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st := s.EsaState(); st != EsaStatePowerAdjustActive { // :390
		return nil, demInvalidInState("ESAState %d, not PowerAdjustActive", st)
	}
	if err := s.cancelPowerAdjustLocked(dem.CauseCancelled); err != nil {
		return nil, demFailure("CancelPowerAdjustRequest: %v", err)
	}
	if c := s.PowerAdjustmentCapability(); c == nil || c.Cause != dem.PowerAdjustReasonNoAdjustment { // :395-411
		return nil, demConstraint("PowerAdjustmentCapability cause not reset")
	}
	return nil, nil
}

// startTimeAdjustRequest is HandleStartTimeAdjustRequest (Cluster.cpp:
// 416-520) over StartTimeAdjustRequest (DelegateImpl.cpp:352-406).
func (s *DeviceEnergyManagementServer) startTimeAdjustRequest(ctx context.Context, fields any) (any, error) {
	r, err := energyRequest[dem.StartTimeAdjustRequestRequest]("StartTimeAdjustRequest", fields)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkOptOut(r.Cause); err != nil { // :424
		return nil, err
	}
	f := s.Forecast()
	if f == nil { // :426-431
		return nil, demFailure("Forecast is null")
	}
	if f.EarliestStartTime == nil || f.LatestEndTime == nil { // :436-440
		return nil, demFailure("EarliestStartTime / LatestEndTime absent")
	}
	earliest := f.EarliestStartTime.Value
	if f.EarliestStartTime.Null { // :446-456: null means now
		earliest = s.nowEpoch()
	}
	duration := f.EndTime - f.StartTime  // :462
	if r.RequestedStartTime < earliest { // :463-468
		return nil, demConstraint("RequestedStartTime %d before EarliestStartTime %d", r.RequestedStartTime, earliest)
	}
	if r.RequestedStartTime+duration > *f.LatestEndTime { // :470-475
		return nil, demConstraint("RequestedStartTime + duration %d after LatestEndTime %d", r.RequestedStartTime+duration, *f.LatestEndTime)
	}
	originalID := f.ForecastId // :478

	// The delegate (DelegateImpl.cpp:352-406).
	reason, ok := forecastReason(r.Cause)
	if !ok { // :367-370
		return nil, demFailure("StartTimeAdjustRequest: cause %d", r.Cause)
	}
	f.ForecastUpdateReason = reason
	f.ForecastId++ // :373
	savedStart, savedEnd := f.StartTime, f.EndTime
	f.StartTime, f.EndTime = r.RequestedStartTime, r.RequestedStartTime+duration            // :383-384
	if herr := s.manager.StartTimeAdjust(ctx, r.RequestedStartTime, r.Cause); herr != nil { // :386-400
		// chip restores the times and resets the reason to
		// InternalOptimization; the incremented ForecastID stays.
		f.ForecastUpdateReason = dem.ForecastUpdateReasonInternalOptimization
		f.StartTime, f.EndTime = savedStart, savedEnd
		_ = s.setForecastLocked(f)
		return nil, demFailure("StartTimeAdjustRequest: %v", herr)
	}
	if err := s.setForecastLocked(f); err != nil {
		return nil, err
	}
	// The cluster verifies the delegate's post-conditions (:485-517).
	switch u := s.Forecast(); {
	case u == nil:
		return nil, demFailure("Forecast is null after StartTimeAdjustRequest")
	case u.StartTime != r.RequestedStartTime, u.ForecastId <= originalID, u.EndTime != r.RequestedStartTime+duration:
		return nil, demConstraint("Forecast not updated by StartTimeAdjustRequest")
	}
	return nil, nil
}

// pauseRequest is HandlePauseRequest (Cluster.cpp:522-594) over
// PauseRequest (DelegateImpl.cpp:425-492).
func (s *DeviceEnergyManagementServer) pauseRequest(ctx context.Context, fields any) (any, error) {
	r, err := energyRequest[dem.PauseRequestRequest]("PauseRequest", fields)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkOptOut(r.Cause); err != nil { // :530
		return nil, err
	}
	if st := s.EsaState(); st != EsaStateOnline && st != EsaStatePowerAdjustActive && st != EsaStatePaused { // :532-535
		return nil, demConstraint("ESAState %d", st)
	}
	f := s.Forecast()
	if f == nil { // :537-542
		return nil, demFailure("Forecast is null")
	}
	if f.ActiveSlotNumber.Null { // :546-550
		return nil, demFailure("ActiveSlotNumber is null")
	}
	active := int(f.ActiveSlotNumber.Value)
	if active >= len(f.Slots) { // :552-558
		return nil, demFailure("ActiveSlotNumber %d of %d slots", active, len(f.Slots))
	}
	slot := f.Slots[active]
	switch {
	case slot.SlotIsPausable == nil, slot.MinPauseDuration == nil, slot.MaxPauseDuration == nil: // :562-578
		return nil, demFailure("slot %d lacks SlotIsPausable, MinPauseDuration or MaxPauseDuration", active)
	case !*slot.SlotIsPausable: // :580-584
		return nil, demFailure("slot %d is not pausable", active)
	case r.Duration < *slot.MinPauseDuration || r.Duration > *slot.MaxPauseDuration: // :586-591
		return nil, demConstraint("pause duration %d outside %d..%d", r.Duration, *slot.MinPauseDuration, *slot.MaxPauseDuration)
	}

	// The delegate (DelegateImpl.cpp:425-492).
	generateEvent := false
	if s.pauseInProgress { // :430-433
		s.stopPauseTimerLocked()
	} else { // :434-441
		generateEvent = true
		s.pauseInProgress = true
	}
	s.pauseTimer = time.AfterFunc(time.Duration(r.Duration)*time.Second, s.pauseExpired) // :443
	if herr := s.manager.Pause(ctx, r.Duration, r.Cause); herr != nil {                  // :451-460
		s.pauseFailureLocked()
		return nil, demFailure("PauseRequest: %v", herr)
	}
	if generateEvent { // :462-473
		if err := s.emitLocked(dem.EventPaused, dem.PausedEvent{}); err != nil {
			s.pauseFailureLocked()
			return nil, demFailure("Paused: %v", err)
		}
	}
	if err := s.setLocked(dem.AttrEsaState, EsaStatePaused); err != nil { // :475
		return nil, err
	}
	if reason, ok := forecastReason(r.Cause); ok { // :478-489
		f.ForecastUpdateReason = reason
		if err := s.setForecastLocked(f); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

// pauseFailureLocked is HandlePauseRequestFailure (DelegateImpl.cpp:
// 499-509). chip cancels the power-adjustment timer there (:501), not the
// pause timer it started (:443); this transcribes it as written.
func (s *DeviceEnergyManagementServer) pauseFailureLocked() {
	s.stopPowerAdjustTimerLocked()
	_ = s.setLocked(dem.AttrEsaState, EsaStateOnline)
	s.pauseInProgress = false
}

// pauseExpired is HandlePauseRequestTimerExpiry (DelegateImpl.cpp:531-546).
func (s *DeviceEnergyManagementServer) pauseExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.pauseInProgress {
		return
	}
	s.pauseTimer = nil
	s.pauseInProgress = false
	_ = s.setLocked(dem.AttrEsaState, EsaStateOnline)
	_ = s.emitLocked(dem.EventResumed, dem.ResumedEvent{Cause: dem.CauseNormalCompletion})
	_ = s.manager.PauseCompleted()
}

// cancelPauseLocked is CancelPauseRequestAndGenerateEvent
// (DelegateImpl.cpp:558-588): the hook's error takes precedence over the
// event's.
func (s *DeviceEnergyManagementServer) cancelPauseLocked(cause DemCause) error {
	s.pauseInProgress = false
	_ = s.setLocked(dem.AttrEsaState, EsaStateOnline)
	s.stopPauseTimerLocked()
	err := s.emitLocked(dem.EventResumed, dem.ResumedEvent{Cause: cause})
	if herr := s.manager.CancelPause(cause); herr != nil {
		return herr
	}
	return err
}

// resumeRequest is HandleResumeRequest (Cluster.cpp:596-616) over
// ResumeRequest (DelegateImpl.cpp:622-646).
func (s *DeviceEnergyManagementServer) resumeRequest(_ context.Context, _ any) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st := s.EsaState(); st != EsaStatePaused { // :604
		return nil, demInvalidInState("ESAState %d, not Paused", st)
	}
	if !s.pauseInProgress { // :624-626, :645
		return nil, demFailure("no pause in progress")
	}
	if f := s.Forecast(); f != nil { // :629-636
		f.ForecastUpdateReason = dem.ForecastUpdateReasonInternalOptimization
		if err := s.setForecastLocked(f); err != nil {
			return nil, err
		}
	}
	if err := s.cancelPauseLocked(dem.CauseCancelled); err != nil { // :638-642
		return nil, demFailure("ResumeRequest: %v", err)
	}
	// The cluster's post-conditions (:609-613).
	if s.EsaState() == EsaStatePaused {
		return nil, demInvalidInState("still Paused")
	}
	f := s.Forecast()
	if f == nil {
		return nil, demFailure("Forecast is null")
	}
	if f.ForecastUpdateReason != dem.ForecastUpdateReasonInternalOptimization {
		return nil, demInvalidInState("ForecastUpdateReason %d", f.ForecastUpdateReason)
	}
	return nil, nil
}

// modifyForecastRequest is HandleModifyForecastRequest (Cluster.cpp:
// 618-684) over ModifyForecastRequest (DelegateImpl.cpp:662-707).
func (s *DeviceEnergyManagementServer) modifyForecastRequest(ctx context.Context, fields any) (any, error) {
	r, err := energyRequest[dem.ModifyForecastRequestRequest]("ModifyForecastRequest", fields)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkOptOut(r.Cause); err != nil { // :626
		return nil, err
	}
	f := s.Forecast()
	if f == nil { // :628-633
		return nil, demFailure("Forecast is null")
	}
	pfr := s.HasFeature("PFR")
	for _, a := range r.SlotAdjustments {
		if int(a.SlotIndex) >= len(f.Slots) { // :642-646
			return nil, demFailure("bad slot index %d", a.SlotIndex)
		}
		if !f.ActiveSlotNumber.Null && uint16(a.SlotIndex) < f.ActiveSlotNumber.Value { // :649-653
			return nil, demConstraint("slot %d already ran", a.SlotIndex)
		}
		slot := f.Slots[a.SlotIndex]
		if pfr && (a.NominalPower == nil || slot.MinPowerAdjustment == nil || slot.MaxPowerAdjustment == nil ||
			*a.NominalPower < *slot.MinPowerAdjustment || *a.NominalPower > *slot.MaxPowerAdjustment) { // :658-667
			return nil, demConstraint("bad NominalPower for slot %d", a.SlotIndex)
		}
		if slot.MinDurationAdjustment == nil || slot.MaxDurationAdjustment == nil ||
			a.Duration < *slot.MinDurationAdjustment || a.Duration > *slot.MaxDurationAdjustment { // :669-675
			return nil, demConstraint("bad Duration for slot %d", a.SlotIndex)
		}
	}

	// The delegate (DelegateImpl.cpp:662-707).
	if f.ForecastId != r.ForecastId { // :672-675
		return nil, demFailure("ForecastID %d, current %d", r.ForecastId, f.ForecastId)
	}
	if herr := s.manager.ModifyForecast(ctx, r.ForecastId, r.SlotAdjustments, r.Cause); herr != nil { // :676-684
		return nil, demFailure("ModifyForecastRequest: %v", herr)
	}
	return nil, s.adjustForecastLocked(f, r.Cause)
}

// adjustForecastLocked records a successful adjustment: the reason the
// cause gives (other causes leave it) and the next ForecastID
// (DelegateImpl.cpp:686-704, :740-760).
func (s *DeviceEnergyManagementServer) adjustForecastLocked(f *Forecast, cause AdjustmentCause) error {
	if reason, ok := forecastReason(cause); ok {
		f.ForecastUpdateReason = reason
	}
	f.ForecastId++
	return s.setForecastLocked(f)
}

// requestConstraintBasedForecast is HandleRequestConstraintBasedForecast
// (Cluster.cpp:686-796) over RequestConstraintBasedForecast
// (DelegateImpl.cpp:721-763). As chip, only the first constraint's fields
// are checked (:706-764: `if (iterator.Next())`), then the order and
// overlap of all of them (:767-793).
func (s *DeviceEnergyManagementServer) requestConstraintBasedForecast(ctx context.Context, fields any) (any, error) {
	r, err := energyRequest[dem.RequestConstraintBasedForecastRequest]("RequestConstraintBasedForecast", fields)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkOptOut(r.Cause); err != nil { // :695
		return nil, err
	}
	now := s.nowEpoch() // :697-703
	if len(r.Constraints) > 0 {
		c := r.Constraints[0]
		if c.StartTime < now { // :713-716
			return nil, demConstraint("constraint StartTime %d before now %d", c.StartTime, now)
		}
		if s.HasFeature("PFR") { // :718-742
			if c.NominalPower == nil {
				return nil, spec.Errorf(im.StatusInvalidCommand, "energy: constraint without NominalPower")
			}
			if *c.NominalPower < s.AbsMinPower() || *c.NominalPower > s.AbsMaxPower() {
				return nil, demConstraint("constraint NominalPower %d outside AbsMinPower..AbsMaxPower", *c.NominalPower)
			}
			if c.MaximumEnergy == nil {
				return nil, spec.Errorf(im.StatusInvalidCommand, "energy: constraint without MaximumEnergy")
			}
		}
		if s.HasFeature("SFR") { // :744-757
			if c.LoadControl == nil {
				return nil, spec.Errorf(im.StatusInvalidCommand, "energy: constraint without LoadControl")
			}
			if *c.LoadControl < -100 || *c.LoadControl > 100 {
				return nil, demConstraint("constraint LoadControl %d", *c.LoadControl)
			}
		}
	}
	for i := 1; i < len(r.Constraints); i++ { // :767-793
		prev, c := r.Constraints[i-1], r.Constraints[i]
		if c.StartTime < prev.StartTime || prev.StartTime+prev.Duration >= c.StartTime {
			return nil, demConstraint("overlapping constraint times")
		}
	}

	// The delegate (DelegateImpl.cpp:721-763).
	f := s.Forecast()
	if f == nil { // :726-729
		return nil, demFailure("Forecast is null")
	}
	if herr := s.manager.RequestConstraintBasedForecast(ctx, r.Constraints, r.Cause); herr != nil { // :730-738
		return nil, demFailure("RequestConstraintBasedForecast: %v", herr)
	}
	return nil, s.adjustForecastLocked(f, r.Cause)
}

// cancelRequest is HandleCancelRequest (Cluster.cpp:798-826) over
// CancelRequest (DelegateImpl.cpp:776-798).
func (s *DeviceEnergyManagementServer) cancelRequest(ctx context.Context, _ any) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.Forecast()
	if f == nil { // :808-812
		return nil, demFailure("cancelling on a null forecast")
	}
	if f.ForecastUpdateReason == dem.ForecastUpdateReasonInternalOptimization { // :814-818
		return nil, demInvalidInState("ForecastUpdateReason is already InternalOptimization")
	}
	f.ForecastUpdateReason = dem.ForecastUpdateReasonInternalOptimization // :780
	if err := s.setForecastLocked(f); err != nil {
		return nil, err
	}
	if herr := s.manager.CancelRequest(ctx); herr != nil { // :788-795, Cluster.cpp:820
		return nil, demFailure("CancelRequest: %v", herr)
	}
	return nil, nil
}

// --- plumbing -----------------------------------------------------------

// SetEndpoint stamps the endpoint the events are addressed to; the bridge
// calls it at reassembly.
func (s *DeviceEnergyManagementServer) SetEndpoint(endpoint uint16) {
	s.mu.Lock()
	s.endpoint = endpoint
	s.mu.Unlock()
}

// Close stops the adjustment and pause timers (chip's destructor,
// DelegateImpl.cpp:45-53).
func (s *DeviceEnergyManagementServer) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopPowerAdjustTimerLocked()
	s.stopPauseTimerLocked()
}

// SetMatterEventEmitter implements [contract.EventReceiver].
func (s *DeviceEnergyManagementServer) SetMatterEventEmitter(emitter contract.EventEmitter) {
	s.srv.SetMatterEventEmitter(emitter)
}

// MatterDataVersion implements [contract.ClusterDataVersion].
func (s *DeviceEnergyManagementServer) MatterDataVersion() uint32 {
	return s.srv.MatterDataVersion()
}

// OnMatterAttributesChanged implements [contract.AttributeChangeNotifier].
func (s *DeviceEnergyManagementServer) OnMatterAttributesChanged(cb func(attrIDs []uint32)) (unsubscribe func()) {
	return s.srv.OnMatterAttributesChanged(cb)
}

// MatterRead resolves an attribute.
func (s *DeviceEnergyManagementServer) MatterRead(attrID uint32) (any, bool) {
	return s.srv.MatterRead(attrID)
}

// MatterWrite answers every write: the cluster has no writable attribute.
func (s *DeviceEnergyManagementServer) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	return s.srv.MatterWrite(ctx, attrID, value)
}

// MatterInvoke dispatches the eight requests.
func (s *DeviceEnergyManagementServer) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	return s.srv.MatterInvoke(ctx, cmdID, fields)
}

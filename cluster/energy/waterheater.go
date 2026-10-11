// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package energy

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	whm "github.com/SukramJ/go-fabric/cluster/spec/waterheatermanagement"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// ClusterIDWaterHeaterManagement is the WaterHeaterManagement cluster id.
const ClusterIDWaterHeaterManagement = whm.ClusterID

// The WaterHeaterManagement datatypes, the generated ones.
type (
	// WaterHeaterFeature is a WaterHeaterManagement FeatureMap bit.
	WaterHeaterFeature = whm.Feature
	// HeatSource is the WaterHeaterHeatSourceBitmap (HeaterTypes,
	// HeatDemand).
	HeatSource = whm.WaterHeaterHeatSourceBitmap
	// BoostState is the BoostStateEnum.
	BoostState = whm.BoostStateEnum
	// BoostInfo is the WaterHeaterBoostInfoStruct a Boost carries.
	BoostInfo = whm.WaterHeaterBoostInfoStruct
)

// WaterHeaterManagement features (EM, TP; both optional).
const (
	WaterHeaterFeatureEnergyManagement = whm.FeatureEnergyManagement
	WaterHeaterFeatureTankPercent      = whm.FeatureTankPercent
)

// Heat sources.
const (
	HeatSourceImmersionElement1 = whm.WaterHeaterHeatSourceImmersionElement1
	HeatSourceImmersionElement2 = whm.WaterHeaterHeatSourceImmersionElement2
	HeatSourceHeatPump          = whm.WaterHeaterHeatSourceHeatPump
	HeatSourceBoiler            = whm.WaterHeaterHeatSourceBoiler
	HeatSourceOther             = whm.WaterHeaterHeatSourceOther
)

// Boost states.
const (
	BoostStateInactive = whm.BoostStateInactive
	BoostStateActive   = whm.BoostStateActive
)

// Booster is the host port for the two commands. Boost starts heating
// towards the set point (or info.TemporarySetpoint) for info.Duration
// seconds; CancelBoost returns to the previous mode. An error refuses the
// command — one carrying an IM status ([im.StatusCodeError]) answers with
// it — and leaves BoostState as it was.
//
// Ending a boost on its own — its duration elapsed, or a one-shot boost
// reached its target — is the host's to report, with
// [WaterHeaterManagementServer.EndBoost]: chip leaves both to the device
// delegate (WaterHeaterManagementCluster.cpp:191, :201 call the delegate
// only), whose timer the app runs (examples/water-heater-app/
// water-heater-common/src/WaterHeaterDelegateImpl.cpp:181, :234-259,
// :439-455).
type Booster interface {
	Boost(ctx context.Context, info BoostInfo) error
	CancelBoost(ctx context.Context) error
}

// WaterHeaterState is the state the device reports.
type WaterHeaterState struct {
	// HeatDemand is the heat sources currently heating; a subset of
	// HeaterTypes.
	HeatDemand HeatSource
	// TankVolume is the tank's volume in litres (EM).
	TankVolume uint16
	// EstimatedHeatRequired is the energy to heat the tank to its target,
	// in mWh (EM).
	EstimatedHeatRequired int64
	// TankPercentage is the hot water in the tank, in percent (TP).
	TankPercentage uint8
}

// WaterHeaterConfig carries the construction parameters.
type WaterHeaterConfig struct {
	Features WaterHeaterFeature
	// HeaterTypes is the heat sources the water heater has (quality "F").
	HeaterTypes HeatSource
	// Initial is the state the server starts with.
	Initial WaterHeaterState
	// Booster carries out Boost and CancelBoost; required.
	Booster Booster
	// DataVersion is an optional host-owned tracker.
	DataVersion *cluster.DataVersionTracker
}

// Configuration and state errors.
var (
	ErrNoBooster    = errors.New("energy: WaterHeaterManagement needs a Booster")
	ErrInvalidValue = errors.New("energy: value outside the cluster's model")
)

// WaterHeaterManagementServer implements [contract.ClusterServer] for
// WaterHeaterManagement. Reads, the data version, the change
// notifications and the event priorities are the generated server's; the
// lists, globals and privileges its embedded [spec.Instance]'s. What stays
// here is chip's Boost field validation and the BoostState transitions
// with their events.
type WaterHeaterManagementServer struct {
	*spec.Instance

	srv     *spec.Server
	booster Booster

	// mu serialises the BoostState transitions and their events; it is
	// never held across a host call.
	mu       sync.Mutex
	endpoint uint16
}

// Compile-time assertions.
var (
	_ contract.ClusterServer                  = (*WaterHeaterManagementServer)(nil)
	_ contract.ClusterDataVersion             = (*WaterHeaterManagementServer)(nil)
	_ contract.ClusterAttributeLister         = (*WaterHeaterManagementServer)(nil)
	_ contract.ClusterCommandLister           = (*WaterHeaterManagementServer)(nil)
	_ contract.ClusterEventLister             = (*WaterHeaterManagementServer)(nil)
	_ contract.ClusterAttributeWritePrivilege = (*WaterHeaterManagementServer)(nil)
	_ contract.ClusterCommandInvokePrivilege  = (*WaterHeaterManagementServer)(nil)
	_ contract.AttributeChangeNotifier        = (*WaterHeaterManagementServer)(nil)
	_ contract.EventReceiver                  = (*WaterHeaterManagementServer)(nil)
)

// NewWaterHeaterManagement builds a WaterHeaterManagement server. BoostState
// starts Inactive.
func NewWaterHeaterManagement(cfg WaterHeaterConfig) (*WaterHeaterManagementServer, error) {
	if cfg.Booster == nil {
		return nil, ErrNoBooster
	}
	srv, err := spec.NewServer(whm.Definition, spec.Options{Features: uint32(cfg.Features)}, spec.ServerConfig{DataVersion: cfg.DataVersion})
	if err != nil {
		return nil, fmt.Errorf("energy: %w", err)
	}
	s := &WaterHeaterManagementServer{Instance: srv.Instance, srv: srv, booster: cfg.Booster}
	values := s.stateValues(cfg.Initial)
	values[whm.AttrHeaterTypes] = cfg.HeaterTypes
	values[whm.AttrBoostState] = BoostStateInactive
	if err := s.set(values); err != nil {
		return nil, err
	}
	srv.Handle(whm.CmdBoost, s.boost)
	srv.Handle(whm.CmdCancelBoost, s.cancelBoost)
	return s, nil
}

// stateValues maps the served attributes of st to their values.
func (s *WaterHeaterManagementServer) stateValues(st WaterHeaterState) map[uint32]any {
	out := map[uint32]any{whm.AttrHeatDemand: st.HeatDemand}
	if s.Serves(whm.AttrTankVolume) {
		out[whm.AttrTankVolume] = st.TankVolume
		out[whm.AttrEstimatedHeatRequired] = st.EstimatedHeatRequired
	}
	if s.Serves(whm.AttrTankPercentage) {
		out[whm.AttrTankPercentage] = st.TankPercentage
	}
	return out
}

// set checks values against the definition (bitmap bits, "min 0",
// "max 100") and stores them; HeatDemand must lie inside HeaterTypes.
func (s *WaterHeaterManagementServer) set(values map[uint32]any) error {
	if d, ok := values[whm.AttrHeatDemand]; ok {
		types := s.heaterTypes()
		if t, set := values[whm.AttrHeaterTypes]; set {
			types, _ = t.(HeatSource)
		}
		if demand, _ := d.(HeatSource); demand&^types != 0 {
			return fmt.Errorf("%w: HeatDemand 0x%02X outside HeaterTypes 0x%02X", ErrInvalidValue, demand, types)
		}
	}
	if err := s.srv.SetAttributes(values); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidValue, err)
	}
	return nil
}

func (s *WaterHeaterManagementServer) heaterTypes() HeatSource {
	v, _ := s.srv.Value(whm.AttrHeaterTypes)
	n, _ := v.(uint8)
	return HeatSource(n)
}

// State returns the reported state.
func (s *WaterHeaterManagementServer) State() WaterHeaterState {
	var st WaterHeaterState
	if v, ok := s.srv.Value(whm.AttrHeatDemand); ok {
		n, _ := v.(uint8)
		st.HeatDemand = HeatSource(n)
	}
	if v, ok := s.srv.Value(whm.AttrTankVolume); ok {
		st.TankVolume, _ = v.(uint16)
	}
	if v, ok := s.srv.Value(whm.AttrEstimatedHeatRequired); ok {
		st.EstimatedHeatRequired, _ = v.(int64)
	}
	if v, ok := s.srv.Value(whm.AttrTankPercentage); ok {
		st.TankPercentage, _ = v.(uint8)
	}
	return st
}

// SetHeatDemand records which heat sources are heating; a source outside
// HeaterTypes is refused.
func (s *WaterHeaterManagementServer) SetHeatDemand(demand HeatSource) error {
	return s.set(map[uint32]any{whm.AttrHeatDemand: demand})
}

// SetTankPercentage records the hot-water share of the tank (TP).
func (s *WaterHeaterManagementServer) SetTankPercentage(percent uint8) error {
	return s.set(map[uint32]any{whm.AttrTankPercentage: percent})
}

// SetTankVolume records the tank volume in litres (EM).
func (s *WaterHeaterManagementServer) SetTankVolume(litres uint16) error {
	return s.set(map[uint32]any{whm.AttrTankVolume: litres})
}

// SetEstimatedHeatRequired records the energy still needed, in mWh (EM).
func (s *WaterHeaterManagementServer) SetEstimatedHeatRequired(mWh int64) error {
	return s.set(map[uint32]any{whm.AttrEstimatedHeatRequired: mWh})
}

// BoostState returns the current BoostState.
func (s *WaterHeaterManagementServer) BoostState() BoostState {
	v, _ := s.srv.Value(whm.AttrBoostState)
	n, _ := v.(uint8)
	return BoostState(n)
}

// EndBoost records that the running boost ended on its own — its duration
// elapsed, or a one-shot boost reached its target: BoostState goes
// Inactive and BoostEnded is emitted, as chip's water-heater app does on
// both paths (WaterHeaterDelegateImpl.cpp:234-259 HandleBoostTimerExpiry,
// :439-455 the one-shot end in ChangeHeatingIfNecessary). Without a
// running boost it does nothing.
func (s *WaterHeaterManagementServer) EndBoost() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.BoostState() != BoostStateActive {
		return nil
	}
	return s.transitionLocked(BoostStateInactive, whm.EventBoostEnded, whm.BoostEndedEvent{})
}

// transitionLocked stores state and emits the event that goes with it.
func (s *WaterHeaterManagementServer) transitionLocked(state BoostState, eventID uint32, data any) error {
	if err := s.srv.Set(whm.AttrBoostState, state); err != nil {
		return fmt.Errorf("energy: BoostState: %w", err)
	}
	if err := s.srv.Emit(s.endpoint, eventID, data); err != nil {
		return fmt.Errorf("energy: %w", err)
	}
	return nil
}

// boost carries out Boost. The field rules are chip's
// (WaterHeaterManagementCluster.cpp:152-189, HandleBoost): with TP, a
// TargetPercentage or TargetReheat above 100 is INVALID_COMMAND, as is a
// TargetReheat without a TargetPercentage or together with OneShot;
// without TP, either field is INVALID_COMMAND. Then the host boosts; on
// success BoostState is Active and BoostStarted carries the request's
// BoostInfo — also when a boost was already running, which the new one
// replaces (WaterHeaterDelegateImpl.cpp:175-193, :212-213).
func (s *WaterHeaterManagementServer) boost(ctx context.Context, fields any) (any, error) {
	var info BoostInfo
	switch r := fields.(type) {
	case whm.BoostRequest:
		info = r.BoostInfo
	case *whm.BoostRequest:
		if r == nil {
			return nil, statusError{im.StatusInvalidCommand, "energy: Boost without fields"}
		}
		info = r.BoostInfo
	default:
		return nil, statusError{im.StatusInvalidCommand, fmt.Sprintf("energy: Boost fields %T", fields)}
	}
	if err := s.checkBoost(info); err != nil {
		return nil, err
	}
	if err := s.booster.Boost(ctx, info); err != nil {
		return nil, fmt.Errorf("energy: Boost: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return nil, s.transitionLocked(BoostStateActive, whm.EventBoostStarted, whm.BoostStartedEvent{BoostInfo: info})
}

// checkBoost applies chip's HandleBoost field rules
// (WaterHeaterManagementCluster.cpp:153-189).
func (s *WaterHeaterManagementServer) checkBoost(info BoostInfo) error {
	invalid := func(msg string) error { return statusError{im.StatusInvalidCommand, "energy: Boost: " + msg} }
	if !s.HasFeature("TP") {
		if info.TargetPercentage != nil || info.TargetReheat != nil {
			return invalid("TargetPercentage or TargetReheat without the TankPercent feature") // :185-189
		}
		return nil
	}
	switch {
	case info.TargetPercentage != nil && *info.TargetPercentage > 100: // :155-161
		return invalid(fmt.Sprintf("TargetPercentage %d > 100", *info.TargetPercentage))
	case info.TargetReheat == nil:
		return nil
	case *info.TargetReheat > 100: // :166-170
		return invalid(fmt.Sprintf("TargetReheat %d > 100", *info.TargetReheat))
	case info.TargetPercentage == nil: // :172-176
		return invalid("TargetReheat without TargetPercentage")
	case info.OneShot != nil: // :178-182
		return invalid("TargetReheat together with OneShot")
	}
	return nil
}

// cancelBoost carries out CancelBoost. Without a running boost it
// succeeds and does nothing — no host call, no event — as chip's
// water-heater app answers it (WaterHeaterDelegateImpl.cpp:270-296: only
// an Active boost is cancelled, SUCCESS either way) and as the
// certification case expects (TC_EWATERHTR_2_2.py:196-197, :470-473:
// "status SUCCESS(0x00) and no event sent"). With one, the host cancels
// it; on success BoostState is Inactive and BoostEnded is emitted
// (:272, :287).
func (s *WaterHeaterManagementServer) cancelBoost(ctx context.Context, _ any) (any, error) {
	if s.BoostState() != BoostStateActive {
		return nil, nil
	}
	if err := s.booster.CancelBoost(ctx); err != nil {
		return nil, fmt.Errorf("energy: CancelBoost: %w", err)
	}
	return nil, s.EndBoost()
}

// SetEndpoint stamps the endpoint the events are addressed to; the bridge
// calls it at reassembly.
func (s *WaterHeaterManagementServer) SetEndpoint(endpoint uint16) {
	s.mu.Lock()
	s.endpoint = endpoint
	s.mu.Unlock()
}

// SetMatterEventEmitter implements [contract.EventReceiver].
func (s *WaterHeaterManagementServer) SetMatterEventEmitter(emitter contract.EventEmitter) {
	s.srv.SetMatterEventEmitter(emitter)
}

// MatterDataVersion implements [contract.ClusterDataVersion].
func (s *WaterHeaterManagementServer) MatterDataVersion() uint32 { return s.srv.MatterDataVersion() }

// OnMatterAttributesChanged implements [contract.AttributeChangeNotifier].
func (s *WaterHeaterManagementServer) OnMatterAttributesChanged(cb func(attrIDs []uint32)) (unsubscribe func()) {
	return s.srv.OnMatterAttributesChanged(cb)
}

// MatterRead resolves an attribute.
func (s *WaterHeaterManagementServer) MatterRead(attrID uint32) (any, bool) {
	return s.srv.MatterRead(attrID)
}

// MatterWrite answers every write: the cluster has no writable attribute.
func (s *WaterHeaterManagementServer) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	return s.srv.MatterWrite(ctx, attrID, value)
}

// MatterInvoke dispatches Boost and CancelBoost.
func (s *WaterHeaterManagementServer) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	return s.srv.MatterInvoke(ctx, cmdID, fields)
}

// statusError carries an exact IM status to the dispatcher.
type statusError struct {
	status im.StatusCode
	msg    string
}

func (e statusError) Error() string { return e.msg }

// MatterStatusCode implements [im.StatusCodeError].
func (e statusError) MatterStatusCode() im.StatusCode { return e.status }

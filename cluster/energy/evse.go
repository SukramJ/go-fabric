// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package energy

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	evse "github.com/SukramJ/go-fabric/cluster/spec/energyevse"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// EnergyEvse (0x0099). The chip sources, cited below by file name and line:
//
//   - EvseCluster.cpp — src/app/clusters/energy-evse-server/
//     EnergyEvseCluster.cpp: the attribute setters' constraints, the
//     writes, the command field checks and ValidateTargets; Constants.h
//     next to it holds the limits.
//   - EvseDelegate.cpp — examples/evse-app/evse-common/src/
//     EnergyEvseDelegateImpl.cpp: the EVSE state machine — what the
//     commands and the hardware inputs (the delegate's Hw* calls) do to
//     State, SupplyState and FaultState, the current limits, the session,
//     the ChargingEnabledUntil timer and the six events. The certification
//     cases (TC_EEVSE_2_x) are written against it.
//   - TargetsStore.cpp — examples/evse-app/evse-common/src/
//     EnergyEvseTargetsStore.cpp: how SetTargets merges a schedule into
//     the stored ones.
//
// The device side drives the state machine through the setters below (the
// Hw* inputs); the host port [EvseHost] reads the energy meter and is told
// of the changes chip's EVSECallbacks report.

// ClusterIDEnergyEvse is the EnergyEvse cluster id.
const ClusterIDEnergyEvse = evse.ClusterID

// The EnergyEvse datatypes, the generated ones.
type (
	// EvseFeature is an EnergyEvse FeatureMap bit.
	EvseFeature = evse.Feature
	// EvseState is the StateEnum.
	EvseState = evse.StateEnum
	// SupplyState is the SupplyStateEnum.
	SupplyState = evse.SupplyStateEnum
	// EvseFault is the FaultStateEnum.
	EvseFault = evse.FaultStateEnum
	// ChargingTargetSchedule is the ChargingTargetScheduleStruct.
	ChargingTargetSchedule = evse.ChargingTargetScheduleStruct
	// ChargingTarget is the ChargingTargetStruct.
	ChargingTarget = evse.ChargingTargetStruct
	// TargetDays is the TargetDayOfWeekBitmap.
	TargetDays = evse.TargetDayOfWeekBitmap
)

// EnergyEvse features.
const (
	EvseFeatureChargingPreferences = evse.FeatureChargingPreferences // PREF
	EvseFeatureSoCReporting        = evse.FeatureSoCReporting        // SOC
	EvseFeaturePlugAndCharge       = evse.FeaturePlugAndCharge       // PNC
	EvseFeatureRfid                = evse.FeatureRfid                // RFID
	EvseFeatureV2X                 = evse.FeatureV2X                 // V2X
)

// EVSE states.
const (
	EvseNotPluggedIn         = evse.StateNotPluggedIn
	EvsePluggedInNoDemand    = evse.StatePluggedInNoDemand
	EvsePluggedInDemand      = evse.StatePluggedInDemand
	EvsePluggedInCharging    = evse.StatePluggedInCharging
	EvsePluggedInDischarging = evse.StatePluggedInDischarging
	EvseSessionEnding        = evse.StateSessionEnding
	EvseFaulted              = evse.StateFault
)

// Supply states.
const (
	SupplyDisabled            = evse.SupplyStateDisabled
	SupplyChargingEnabled     = evse.SupplyStateChargingEnabled
	SupplyDischargingEnabled  = evse.SupplyStateDischargingEnabled
	SupplyDisabledError       = evse.SupplyStateDisabledError
	SupplyDisabledDiagnostics = evse.SupplyStateDisabledDiagnostics
	SupplyEnabled             = evse.SupplyStateEnabled
)

// EvseFaultNone is FaultState NoError.
const EvseFaultNone = evse.FaultStateNoError

// The limits of Constants.h (src/app/clusters/energy-evse-server/
// Constants.h:24-32) and of the app (examples/evse-app/evse-common/include/
// EvseTargetsConfig.h:36-39).
const (
	evseMinimumChargeCurrentLimit int64  = 0      // kMinimumChargeCurrentLimit
	evseMinimumChargeCurrent      int64  = 6000   // kMinimumChargeCurrent, the MinimumChargeCurrent default
	evseMaxTargetsDays                   = 7      // kEvseTargetsMaxNumberOfDays
	evseMaxTargetsPerDay                 = 10     // kEvseTargetsMaxTargetsPerDay
	evseMaxMinutesPastMidnight    uint16 = 1439   // kMaxMinutesPastMidnight
	evseMaxTargetSoC              uint8  = 100    // kMaxTargetSoCPercent
	evseDayOfWeekMask                    = 0x7F   // kDayOfWeekBitmapMask, kAllTargetDaysMask
	evseMaxVehicleIDLen                  = 32     // kMaxVehicleIDBufSize
	evseMinimumMainsVoltage       int64  = 100000 // kMinimumMainsVoltage_mV
	evseDefaultMainsVoltage       int64  = 230000 // mNominalMainsVoltage (EnergyEvseDelegateImpl.h:313)
)

// EvseChange is one of the changes chip's EVSE delegate reports to the
// application (examples/evse-app/evse-common/include/EVSECallbacks.h,
// EVSECallbackType).
type EvseChange uint8

// The changes.
const (
	// EvseStateChanged: State or SupplyState changed (EvseDelegate.cpp:
	// 1309-1323).
	EvseStateChanged EvseChange = iota + 1
	// EvseChargeCurrentChanged: MaximumChargeCurrent changed (:1279-1292).
	EvseChargeCurrentChanged
	// EvseDischargeCurrentChanged: MaximumDischargeCurrent changed
	// (:1294-1307).
	EvseDischargeCurrentChanged
	// EvseChargingPreferencesChanged: SetTargets or ClearTargets succeeded
	// (:1325-1337).
	EvseChargingPreferencesChanged
)

// EvseHost is the host port: the energy meter the EnergyTransferStarted /
// Stopped events read (EvseDelegate.cpp:1339-1354, the
// EnergyMeterReadingRequested callback), and the change notifications.
//
// EvseEnergyMeter is called with the server's state lock held and must not
// call the server. EvseChanged is called after the operation that caused
// the change has finished and the lock is released — chip calls it
// synchronously from within; the host may call the server from it, as
// chip's app recomputes NextChargeStartTime there
// (examples/evse-app/evse-common/src/EVSEManufacturerImpl.cpp:485-525).
type EvseHost interface {
	// EvseEnergyMeter returns the cumulative imported (discharging false)
	// or exported energy in mWh.
	EvseEnergyMeter(discharging bool) int64
	EvseChanged(change EvseChange)
}

// EvseConfig carries the construction parameters.
type EvseConfig struct {
	Features EvseFeature
	// The optional attributes (EvseCluster.cpp:405-432,
	// OptionalAttributes) and the optional StartDiagnostics command.
	UserMaximumChargeCurrent bool
	RandomizationDelayWindow bool
	ApproximateEvEfficiency  bool // only with PREF
	StartDiagnostics         bool
	// Host reads the meter and is told of changes; nil reads 0 and tells
	// nobody, as chip's delegate without a registered callback
	// (EvseDelegate.cpp:1286-1289).
	Host EvseHost
	// Now is the wall clock the epoch-s times are read from; nil is
	// time.Now.
	Now func() time.Time
	// DataVersion is an optional host-owned tracker.
	DataVersion *cluster.DataVersionTracker
}

// evseSession is EvseSession (EvseDelegate.cpp:1747-1846).
type evseSession struct {
	startTime         uint32
	chargedAtStart    int64
	dischargedAtStart int64
}

// EvseServer implements [contract.ClusterServer] for EnergyEvse.
type EvseServer struct {
	*spec.Instance

	srv     *spec.Server
	version *cluster.DataVersionTracker
	own     cluster.DataVersionTracker
	host    EvseHost
	now     func() time.Time

	// mu serialises the state machine, as chip's single-threaded stack
	// does. pending holds the changes to report once it is released.
	mu       sync.Mutex
	pending  []EvseChange
	endpoint uint16

	// The delegate's members (EnergyEvseDelegateImpl.h:306-322).
	maxHwCharge        int64
	maxHwDischarge     int64
	cableLimit         int64
	chargeFromCommand  int64
	actualCharge       int64
	dischargeFromCmd   int64
	actualDischarge    int64
	nominalVoltage     int64
	userMaxCharge      int64
	hwState            EvseState
	stateBeforeFault   *EvseState
	supplyBeforeFault  *SupplyState
	session            evseSession
	importedAtTransfer int64
	exportedAtTransfer int64
	enabledTimer       *time.Timer
	targets            []ChargingTargetSchedule
	vehicleID          *string
}

// Compile-time assertions.
var (
	_ contract.ClusterServer                  = (*EvseServer)(nil)
	_ contract.ClusterDataVersion             = (*EvseServer)(nil)
	_ contract.ClusterAttributeLister         = (*EvseServer)(nil)
	_ contract.ClusterCommandLister           = (*EvseServer)(nil)
	_ contract.ClusterEventLister             = (*EvseServer)(nil)
	_ contract.ClusterAttributeWritePrivilege = (*EvseServer)(nil)
	_ contract.ClusterCommandInvokePrivilege  = (*EvseServer)(nil)
	_ contract.AttributeChangeNotifier        = (*EvseServer)(nil)
	_ contract.EventReceiver                  = (*EvseServer)(nil)
	_ spec.Sink                               = (*EvseServer)(nil)
)

// NewEnergyEvse builds an EnergyEvse server. The attributes start as
// chip's cluster starts them (EnergyEvseCluster.h:198-224): NotPluggedIn,
// Disabled, NoError, MinimumChargeCurrent 6000 mA, RandomizationDelayWindow
// 600 s, every other number 0 and every nullable null.
func NewEnergyEvse(cfg EvseConfig) (*EvseServer, error) {
	opts := spec.Options{Features: uint32(cfg.Features)}
	if cfg.UserMaximumChargeCurrent {
		opts.Attributes = append(opts.Attributes, evse.AttrUserMaximumChargeCurrent)
	}
	if cfg.RandomizationDelayWindow {
		opts.Attributes = append(opts.Attributes, evse.AttrRandomizationDelayWindow)
	}
	if cfg.ApproximateEvEfficiency {
		opts.Attributes = append(opts.Attributes, evse.AttrApproximateEvEfficiency)
	}
	if cfg.Features&EvseFeatureRfid != 0 { // Rfid is "[RFID]"; chip emits it (EvseDelegate.cpp:731-743)
		opts.Events = append(opts.Events, evse.EventRfid)
	}
	if cfg.StartDiagnostics {
		opts.Commands = append(opts.Commands, evse.CmdStartDiagnostics)
	}
	s := &EvseServer{host: cfg.Host, now: cfg.Now, version: cfg.DataVersion, nominalVoltage: evseDefaultMainsVoltage}
	if s.now == nil {
		s.now = time.Now
	}
	if s.version == nil {
		s.version = &s.own
	}
	srv, err := spec.NewServer(evse.Definition, opts, spec.ServerConfig{DataVersion: s.version, Sink: s})
	if err != nil {
		return nil, fmt.Errorf("energy: %w", err)
	}
	s.srv, s.Instance = srv, srv.Instance
	values := map[uint32]any{
		evse.AttrState:                EvseNotPluggedIn,
		evse.AttrSupplyState:          SupplyDisabled,
		evse.AttrFaultState:           EvseFaultNone,
		evse.AttrChargingEnabledUntil: nil,
		evse.AttrCircuitCapacity:      int64(0),
		evse.AttrMinimumChargeCurrent: evseMinimumChargeCurrent,
		evse.AttrMaximumChargeCurrent: int64(0),
		evse.AttrSessionId:            nil,
		evse.AttrSessionDuration:      nil,
		evse.AttrSessionEnergyCharged: nil,
	}
	for _, id := range []uint32{
		evse.AttrDischargingEnabledUntil, evse.AttrNextChargeStartTime, evse.AttrNextChargeTargetTime,
		evse.AttrNextChargeRequiredEnergy, evse.AttrNextChargeTargetSoC, evse.AttrApproximateEvEfficiency,
		evse.AttrStateOfCharge, evse.AttrBatteryCapacity, evse.AttrVehicleId, evse.AttrSessionEnergyDischarged,
	} {
		if s.Serves(id) {
			values[id] = nil
		}
	}
	if s.Serves(evse.AttrMaximumDischargeCurrent) {
		values[evse.AttrMaximumDischargeCurrent] = int64(0)
	}
	if s.Serves(evse.AttrUserMaximumChargeCurrent) {
		values[evse.AttrUserMaximumChargeCurrent] = int64(0)
	}
	if err := srv.SetAttributes(values); err != nil {
		return nil, fmt.Errorf("energy: %w", err)
	}
	srv.Handle(evse.CmdDisable, s.disable)
	srv.Handle(evse.CmdEnableCharging, s.enableCharging)
	srv.Handle(evse.CmdEnableDischarging, s.enableDischarging)
	srv.Handle(evse.CmdStartDiagnostics, s.startDiagnostics)
	srv.Handle(evse.CmdSetTargets, s.setTargets)
	srv.Handle(evse.CmdGetTargets, s.getTargets)
	srv.Handle(evse.CmdClearTargets, s.clearTargets)
	return s, nil
}

// lock takes the state lock; unlock releases it and then reports the
// changes the operation made.
func (s *EvseServer) lock() { s.mu.Lock() }

func (s *EvseServer) unlock() {
	pending := s.pending
	s.pending = nil
	host := s.host
	s.mu.Unlock()
	if host == nil {
		return
	}
	for _, c := range pending {
		host.EvseChanged(c)
	}
}

func (s *EvseServer) notifyLocked(c EvseChange) { s.pending = append(s.pending, c) }

func (s *EvseServer) nowEpoch() uint32 { return matterEpochS(s.now()) }

// --- attribute access -----------------------------------------------------

func (s *EvseServer) u8(attrID uint32) uint8 {
	v, _ := s.srv.Value(attrID)
	n, _ := v.(uint8)
	return n
}

func (s *EvseServer) i64(attrID uint32) int64 {
	v, _ := s.srv.Value(attrID)
	n, _ := v.(int64)
	return n
}

// nullU32 reads a nullable uint32 attribute; nil is null.
func (s *EvseServer) nullU32(attrID uint32) *uint32 {
	v, _ := s.srv.Value(attrID)
	if n, ok := v.(uint32); ok {
		return &n
	}
	return nil
}

func (s *EvseServer) nullI64(attrID uint32) *int64 {
	v, _ := s.srv.Value(attrID)
	if n, ok := v.(int64); ok {
		return &n
	}
	return nil
}

func (s *EvseServer) nullU8(attrID uint32) *uint8 {
	v, _ := s.srv.Value(attrID)
	if n, ok := v.(uint8); ok {
		return &n
	}
	return nil
}

// setLocked stores a served attribute; an unserved one — a V2X attribute
// on an EVSE without it — is skipped, as chip keeps the member but never
// serves it.
func (s *EvseServer) setLocked(attrID uint32, v any) error {
	if !s.Serves(attrID) {
		return nil
	}
	if err := s.srv.Set(attrID, v); err != nil {
		return fmt.Errorf("energy: EnergyEvse: %w", err)
	}
	return nil
}

// forceLocked stores v and reports the attribute even when it did not
// change: chip's session setters do not compare, "because the tests
// expect reports at session boundaries" (EvseCluster.cpp:261-289).
func (s *EvseServer) forceLocked(attrID uint32, v any) {
	if !s.Serves(attrID) {
		return
	}
	old, _ := s.srv.Value(attrID)
	_ = s.srv.Set(attrID, v)
	if now, _ := s.srv.Value(attrID); now == old {
		s.version.Bump()
		s.srv.Notify(attrID)
	}
}

func ptrValue[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}

// State returns State.
func (s *EvseServer) State() EvseState { return EvseState(s.u8(evse.AttrState)) }

// SupplyState returns SupplyState.
func (s *EvseServer) SupplyState() SupplyState { return SupplyState(s.u8(evse.AttrSupplyState)) }

// FaultState returns FaultState.
func (s *EvseServer) FaultState() EvseFault { return EvseFault(s.u8(evse.AttrFaultState)) }

// CircuitCapacity returns CircuitCapacity in mA.
func (s *EvseServer) CircuitCapacity() int64 { return s.i64(evse.AttrCircuitCapacity) }

// MaximumChargeCurrent returns MaximumChargeCurrent in mA.
func (s *EvseServer) MaximumChargeCurrent() int64 { return s.i64(evse.AttrMaximumChargeCurrent) }

// MaximumDischargeCurrent returns the computed discharge limit in mA.
func (s *EvseServer) MaximumDischargeCurrent() int64 {
	s.lock()
	defer s.unlock()
	return s.actualDischarge
}

// UserMaximumChargeCurrent returns the user's limit in mA.
func (s *EvseServer) UserMaximumChargeCurrent() int64 {
	s.lock()
	defer s.unlock()
	return s.userMaxCharge
}

// StateOfCharge returns StateOfCharge; nil while null.
func (s *EvseServer) StateOfCharge() *uint8 { return s.nullU8(evse.AttrStateOfCharge) }

// BatteryCapacity returns BatteryCapacity in mWh; nil while null.
func (s *EvseServer) BatteryCapacity() *int64 { return s.nullI64(evse.AttrBatteryCapacity) }

// SessionID returns SessionID; nil while null.
func (s *EvseServer) SessionID() *uint32 { return s.nullU32(evse.AttrSessionId) }

// NominalMainsVoltage returns the mains voltage in mV.
func (s *EvseServer) NominalMainsVoltage() int64 {
	s.lock()
	defer s.unlock()
	return s.nominalVoltage
}

// HardwareState returns the state the hardware last reported.
func (s *EvseServer) HardwareState() EvseState {
	s.lock()
	defer s.unlock()
	return s.hwState
}

// MaxHardwareChargeCurrent returns the hardware charge limit in mA.
func (s *EvseServer) MaxHardwareChargeCurrent() int64 {
	s.lock()
	defer s.unlock()
	return s.maxHwCharge
}

// MaxHardwareDischargeCurrent returns the hardware discharge limit in mA.
func (s *EvseServer) MaxHardwareDischargeCurrent() int64 {
	s.lock()
	defer s.unlock()
	return s.maxHwDischarge
}

// CableAssemblyLimit returns the cable's limit in mA.
func (s *EvseServer) CableAssemblyLimit() int64 {
	s.lock()
	defer s.unlock()
	return s.cableLimit
}

// IsPluggedIn is IsEvsePluggedIn (EvseDelegate.cpp:1733-1738).
func (s *EvseServer) IsPluggedIn() bool {
	switch s.State() {
	case EvsePluggedInCharging, EvsePluggedInDemand, EvsePluggedInDischarging, EvsePluggedInNoDemand:
		return true
	default:
		return false
	}
}

// Targets returns the stored charging target schedules.
func (s *EvseServer) Targets() []ChargingTargetSchedule {
	s.lock()
	defer s.unlock()
	return cloneSchedules(s.targets)
}

func cloneSchedules(in []ChargingTargetSchedule) []ChargingTargetSchedule {
	out := make([]ChargingTargetSchedule, len(in))
	for i, sch := range in {
		out[i] = ChargingTargetSchedule{DayOfWeekForSequence: sch.DayOfWeekForSequence, ChargingTargets: append([]ChargingTarget(nil), sch.ChargingTargets...)}
	}
	return out
}

// --- the attribute setters with their checks (EvseCluster.cpp:47-289) ----

func (s *EvseServer) setStateLocked(st EvseState) {
	if s.State() == st { // :49
		return
	}
	_ = s.setLocked(evse.AttrState, st)
	s.notifyLocked(EvseStateChanged) // :52 OnStateChanged → :1500-1504
}

func (s *EvseServer) setSupplyStateLocked(st SupplyState) {
	if s.SupplyState() == st { // :59
		return
	}
	_ = s.setLocked(evse.AttrSupplyState, st)
	s.notifyLocked(EvseStateChanged) // :62 OnSupplyStateChanged → :1506-1510
}

func (s *EvseServer) setMinimumChargeCurrentLocked(mA int64) {
	_ = s.setLocked(evse.AttrMinimumChargeCurrent, mA)
}

// --- device-side inputs: the delegate's Hw* calls ---------------------------

// SetMaxHardwareChargeCurrent is HwSetMaxHardwareChargeCurrentLimit
// (EvseDelegate.cpp:489-500): a negative limit is CONSTRAINT_ERROR.
func (s *EvseServer) SetMaxHardwareChargeCurrent(mA int64) error {
	s.lock()
	defer s.unlock()
	if mA < evseMinimumChargeCurrentLimit {
		return demConstraint("hardware charge limit %d mA", mA)
	}
	s.maxHwCharge = mA
	s.computeChargeLimitLocked()
	return nil
}

// SetMaxHardwareDischargeCurrent is HwSetMaxHardwareDischargeCurrentLimit
// (EvseDelegate.cpp:510-521).
func (s *EvseServer) SetMaxHardwareDischargeCurrent(mA int64) error {
	s.lock()
	defer s.unlock()
	if mA < evseMinimumChargeCurrentLimit {
		return demConstraint("hardware discharge limit %d mA", mA)
	}
	s.maxHwDischarge = mA
	s.computeDischargeLimitLocked()
	return nil
}

// SetNominalMainsVoltage is HwSetNominalMainsVoltage (EvseDelegate.cpp:
// 531-542): below 100 V is CONSTRAINT_ERROR.
func (s *EvseServer) SetNominalMainsVoltage(mV int64) error {
	s.lock()
	defer s.unlock()
	if mV < evseMinimumMainsVoltage {
		return demConstraint("mains voltage %d mV", mV)
	}
	s.nominalVoltage = mV
	return nil
}

// SetCircuitCapacity is HwSetCircuitCapacity (EvseDelegate.cpp:553-565).
func (s *EvseServer) SetCircuitCapacity(mA int64) error {
	s.lock()
	defer s.unlock()
	if mA < evseMinimumChargeCurrentLimit {
		return demConstraint("circuit capacity %d mA", mA)
	}
	_ = s.setLocked(evse.AttrCircuitCapacity, mA)
	s.computeChargeLimitLocked()
	return nil
}

// SetCableAssemblyLimit is HwSetCableAssemblyLimit (EvseDelegate.cpp:
// 579-590).
func (s *EvseServer) SetCableAssemblyLimit(mA int64) error {
	s.lock()
	defer s.unlock()
	if mA < evseMinimumChargeCurrentLimit {
		return demConstraint("cable limit %d mA", mA)
	}
	s.cableLimit = mA
	s.computeChargeLimitLocked()
	return nil
}

// SetUserMaximumChargeCurrent records the user's limit, as the cluster's
// setter and the delegate's change handler do (EvseCluster.cpp:135-143;
// EvseDelegate.cpp:1565-1571): a negative limit is CONSTRAINT_ERROR, and
// the charge limit is recomputed.
func (s *EvseServer) SetUserMaximumChargeCurrent(mA int64) error {
	s.lock()
	defer s.unlock()
	if mA < 0 {
		return demConstraint("user maximum charge current %d mA", mA)
	}
	if s.userMaxCharge == mA {
		return nil
	}
	s.userMaxCharge = mA
	_ = s.setLocked(evse.AttrUserMaximumChargeCurrent, mA)
	s.computeChargeLimitLocked()
	return nil
}

// SetStateOfCharge records the vehicle's state of charge; nil is null
// (EvseCluster.cpp:200-207).
func (s *EvseServer) SetStateOfCharge(percent *uint8) error {
	s.lock()
	defer s.unlock()
	return s.setLocked(evse.AttrStateOfCharge, ptrValue(percent))
}

// SetBatteryCapacity records the vehicle's battery capacity in mWh; nil is
// null (EvseCluster.cpp:209-216).
func (s *EvseServer) SetBatteryCapacity(mWh *int64) error {
	s.lock()
	defer s.unlock()
	return s.setLocked(evse.AttrBatteryCapacity, ptrValue(mWh))
}

// ChargeSchedule is the next charge the EVSE plans: NextChargeStartTime,
// NextChargeTargetTime (epoch-s), NextChargeRequiredEnergy (mWh) and
// NextChargeTargetSoC; nil is null.
type ChargeSchedule struct {
	StartTime      *uint32
	TargetTime     *uint32
	RequiredEnergy *int64
	TargetSoC      *uint8
}

// SetChargeSchedule records the next charge (EvseCluster.cpp:155-189).
func (s *EvseServer) SetChargeSchedule(c ChargeSchedule) error {
	s.lock()
	defer s.unlock()
	for _, v := range []struct {
		id  uint32
		val any
	}{
		{evse.AttrNextChargeStartTime, ptrValue(c.StartTime)},
		{evse.AttrNextChargeTargetTime, ptrValue(c.TargetTime)},
		{evse.AttrNextChargeRequiredEnergy, ptrValue(c.RequiredEnergy)},
		{evse.AttrNextChargeTargetSoC, ptrValue(c.TargetSoC)},
	} {
		if err := s.setLocked(v.id, v.val); err != nil {
			return err
		}
	}
	return nil
}

// SetHardwareState is HwSetState (EvseDelegate.cpp:605-685): the
// hardware's plug and demand state drives the state machine. Only
// NotPluggedIn, PluggedInNoDemand and PluggedInDemand are inputs; any
// other is FAILURE.
func (s *EvseServer) SetHardwareState(next EvseState) error {
	s.lock()
	defer s.unlock()
	old := s.hwState
	switch next {
	case EvseNotPluggedIn: // :609-628
		switch old {
		case EvseNotPluggedIn:
		case EvsePluggedInNoDemand, EvsePluggedInDemand:
			s.hwState = next
			s.evNotDetectedLocked()
		default:
			s.hwState = next
		}
	case EvsePluggedInNoDemand: // :630-652
		switch old {
		case EvseNotPluggedIn:
			s.hwState = next
			s.evPluggedInLocked()
		case EvsePluggedInNoDemand:
		case EvsePluggedInDemand:
			s.hwState = next
			s.evNoDemandLocked()
		default:
			s.hwState = next
		}
	case EvsePluggedInDemand: // :653-676
		switch old {
		case EvseNotPluggedIn:
			s.hwState = next
			s.evPluggedInLocked()
			s.evDemandLocked()
		case EvsePluggedInNoDemand:
			s.hwState = next
			s.evDemandLocked()
		case EvsePluggedInDemand:
		default:
			s.hwState = next
		}
	default: // :678-682
		return demFailure("hardware state %d is not an input", next)
	}
	return nil
}

// SetFault is HwSetFault (EvseDelegate.cpp:692-724): the same fault again
// is FAILURE; otherwise Fault is emitted with the state before it,
// FaultState changes, and the state machine enters or leaves Fault.
func (s *EvseServer) SetFault(fault EvseFault) error {
	s.lock()
	defer s.unlock()
	if s.FaultState() == fault { // :696-700
		return demFailure("FaultState is already %d", fault)
	}
	s.emitFaultLocked(fault)                    // :707
	_ = s.setLocked(evse.AttrFaultState, fault) // :710
	// The state machine's answer is not returned (:712-723).
	if fault == EvseFaultNone {
		_ = s.faultClearedLocked()
	} else {
		s.faultRaisedLocked()
	}
	return nil
}

// ReportRFID is HwSetRFID (EvseDelegate.cpp:731-743): the Rfid event.
func (s *EvseServer) ReportRFID(uid []byte) error {
	s.lock()
	defer s.unlock()
	return s.emitLocked(evse.EventRfid, evse.RfidEvent{Uid: uid})
}

// SetVehicleID is HwSetVehicleID (EvseDelegate.cpp:753-787): an unchanged
// id is accepted and changes nothing, one longer than 32 bytes is FAILURE,
// an empty one is null.
func (s *EvseServer) SetVehicleID(id string) error {
	s.lock()
	defer s.unlock()
	if (s.vehicleID != nil && *s.vehicleID == id) || (s.vehicleID == nil && id == "") { // :759-764
		return nil
	}
	if len(id) > evseMaxVehicleIDLen { // :766-770
		return demFailure("vehicle id of %d bytes", len(id))
	}
	if id == "" { // :773-778
		s.vehicleID = nil
		return s.setLocked(evse.AttrVehicleId, nil)
	}
	s.vehicleID = &id
	return s.setLocked(evse.AttrVehicleId, id)
}

// DiagnosticsComplete is HwDiagnosticsComplete (EvseDelegate.cpp:818-833):
// from DisabledDiagnostics back to Disabled; in any other SupplyState it
// is FAILURE.
func (s *EvseServer) DiagnosticsComplete() error {
	s.lock()
	defer s.unlock()
	if s.SupplyState() != SupplyDisabledDiagnostics {
		return demFailure("SupplyState %d, not DisabledDiagnostics", s.SupplyState())
	}
	s.setSupplyStateLocked(SupplyDisabled)
	return nil
}

// --- the state machine (EvseDelegate.cpp:845-1214) --------------------------

// evPluggedInLocked is HandleEVPluggedInEvent (:891-908).
func (s *EvseServer) evPluggedInLocked() {
	if s.State() != EvseNotPluggedIn {
		return
	}
	s.startSessionLocked()
	_ = s.emitLocked(evse.EventEvConnected, evse.EvConnectedEvent{SessionId: s.sessionIDLocked()})
	s.setStateLocked(s.hwState)
}

// evNotDetectedLocked is HandleEVNotDetectedEvent (:910-929).
func (s *EvseServer) evNotDetectedLocked() {
	if st := s.State(); st == EvsePluggedInCharging || st == EvsePluggedInDischarging {
		s.energyTransferStoppedLocked(evse.EnergyTransferStoppedReasonOther)
	}
	s.stopSessionLocked()
	ev := evse.EvNotDetectedEvent{SessionId: s.sessionIDLocked(), State: s.State()}
	if d := s.nullU32(evse.AttrSessionDuration); d != nil {
		ev.SessionDuration = *d
	}
	if e := s.nullI64(evse.AttrSessionEnergyCharged); e != nil {
		ev.SessionEnergyCharged = *e
	}
	// chip always sets SessionEnergyDischarged (:1389), from the session
	// value it keeps whether V2X is served or not.
	discharged := -s.session.dischargedAtStart
	ev.SessionEnergyDischarged = &discharged
	_ = s.emitLocked(evse.EventEvNotDetected, ev)
	s.setStateLocked(EvseNotPluggedIn)
}

// evNoDemandLocked is HandleEVNoDemandEvent (:931-947).
func (s *EvseServer) evNoDemandLocked() {
	if st := s.State(); st == EvsePluggedInCharging || st == EvsePluggedInDischarging {
		s.recalculateSessionDurationLocked()
		s.energyTransferStoppedLocked(evse.EnergyTransferStoppedReasonEvStopped)
	}
	s.setStateLocked(EvsePluggedInNoDemand)
}

// evDemandLocked is HandleEVDemandEvent (:948-992).
func (s *EvseServer) evDemandLocked() {
	switch s.SupplyState() {
	case SupplyChargingEnabled:
		s.computeChargeLimitLocked()
		s.setStateLocked(EvsePluggedInCharging)
		s.energyTransferStartedLocked()
	case SupplyDischargingEnabled:
		s.computeDischargeLimitLocked()
		s.setStateLocked(EvsePluggedInDischarging)
		s.energyTransferStartedLocked()
	case SupplyEnabled:
		s.computeChargeLimitLocked()
		s.computeDischargeLimitLocked()
		s.setStateLocked(EvsePluggedInCharging)
		s.energyTransferStartedLocked()
	case SupplyDisabled, SupplyDisabledError, SupplyDisabledDiagnostics:
		s.setStateLocked(EvsePluggedInDemand)
	}
}

// checkFaultOrDiagnosticLocked is CheckFaultOrDiagnostic (:994-1008).
func (s *EvseServer) checkFaultOrDiagnosticLocked() error {
	if s.FaultState() != EvseFaultNone {
		return demFailure("EVSE is faulted (%d)", s.FaultState())
	}
	if s.SupplyState() == SupplyDisabledDiagnostics {
		return demFailure("EVSE is in diagnostics")
	}
	return nil
}

// chargingEnabledLocked is HandleChargingEnabledEvent (:1010-1071).
func (s *EvseServer) chargingEnabledLocked() error {
	if err := s.checkFaultOrDiagnosticLocked(); err != nil {
		return err
	}
	switch s.SupplyState() {
	case SupplyDisabled:
		s.setSupplyStateLocked(SupplyChargingEnabled)
	case SupplyDischargingEnabled:
		s.setSupplyStateLocked(SupplyEnabled)
	default: // ChargingEnabled, Enabled, DisabledError, DisabledDiagnostics stay (:1028-1040)
	}
	if s.State() == EvsePluggedInDemand {
		s.computeChargeLimitLocked()
		s.setStateLocked(EvsePluggedInCharging)
		s.energyTransferStartedLocked()
	}
	s.scheduleEnabledCheckLocked()
	return nil
}

// dischargingEnabledLocked is HandleDischargingEnabledEvent (:1072-1128).
// chip computes the discharge limit twice for PluggedInDemand and
// PluggedInCharging (:1117-1118) and changes State no further.
func (s *EvseServer) dischargingEnabledLocked() error {
	if err := s.checkFaultOrDiagnosticLocked(); err != nil {
		return err
	}
	switch s.SupplyState() {
	case SupplyDisabled:
		s.setSupplyStateLocked(SupplyDischargingEnabled)
	case SupplyChargingEnabled:
		s.setSupplyStateLocked(SupplyEnabled)
	default: // DischargingEnabled, Enabled, DisabledError, DisabledDiagnostics stay (:1094-1102)
	}
	if st := s.State(); st == EvsePluggedInDemand || st == EvsePluggedInCharging {
		s.computeDischargeLimitLocked()
	}
	s.scheduleEnabledCheckLocked()
	return nil
}

// disabledLocked is HandleDisabledEvent (:1129-1160).
func (s *EvseServer) disabledLocked() error {
	if err := s.checkFaultOrDiagnosticLocked(); err != nil {
		return err
	}
	s.setSupplyStateLocked(SupplyDisabled)
	if st := s.State(); st == EvsePluggedInCharging || st == EvsePluggedInDischarging {
		s.energyTransferStoppedLocked(evse.EnergyTransferStoppedReasonEvseStopped)
		s.setStateLocked(s.hwState)
	}
	return nil
}

// faultRaisedLocked is HandleFaultRaised (:1169-1191).
func (s *EvseServer) faultRaisedLocked() {
	if s.stateBeforeFault == nil {
		st := s.State()
		s.stateBeforeFault = &st
	}
	if s.supplyBeforeFault == nil {
		sup := s.SupplyState()
		s.supplyBeforeFault = &sup
	}
	s.setStateLocked(EvseFaulted)
	s.setSupplyStateLocked(SupplyDisabledError)
}

// faultClearedLocked is HandleFaultCleared (:1192-1214).
func (s *EvseServer) faultClearedLocked() error {
	if s.stateBeforeFault == nil || s.supplyBeforeFault == nil {
		return demFailure("no state recorded before the fault")
	}
	s.setStateLocked(*s.stateBeforeFault)
	s.setSupplyStateLocked(*s.supplyBeforeFault)
	s.stateBeforeFault, s.supplyBeforeFault = nil, nil
	return nil
}

// computeChargeLimitLocked is ComputeMaxChargeCurrentLimit (:1227-1247):
// the least of the hardware, circuit, cable, command and user limits;
// MaximumChargeCurrent changes, and is reported, only when it moved.
func (s *EvseServer) computeChargeLimitLocked() {
	old := s.actualCharge
	s.actualCharge = min(s.maxHwCharge, s.CircuitCapacity(), s.cableLimit, s.chargeFromCommand, s.userMaxCharge)
	if old != s.actualCharge {
		_ = s.setLocked(evse.AttrMaximumChargeCurrent, s.actualCharge)
		s.notifyLocked(EvseChargeCurrentChanged)
	}
}

// computeDischargeLimitLocked is ComputeMaxDischargeCurrentLimit
// (:1258-1277): the user limit does not take part.
func (s *EvseServer) computeDischargeLimitLocked() {
	old := s.actualDischarge
	s.actualDischarge = min(s.maxHwDischarge, s.CircuitCapacity(), s.cableLimit, s.dischargeFromCmd)
	if old != s.actualDischarge {
		_ = s.setLocked(evse.AttrMaximumDischargeCurrent, s.actualDischarge)
		s.notifyLocked(EvseDischargeCurrentChanged)
	}
}

func (s *EvseServer) meterLocked(discharging bool) int64 {
	if s.host == nil {
		return 0
	}
	return s.host.EvseEnergyMeter(discharging)
}

func (s *EvseServer) sessionIDLocked() uint32 {
	if id := s.SessionID(); id != nil {
		return *id
	}
	return 0
}

// energyTransferStartedLocked is SendEnergyTransferStartedEvent
// (:1400-1439).
func (s *EvseServer) energyTransferStartedLocked() {
	ev := evse.EnergyTransferStartedEvent{SessionId: s.sessionIDLocked(), State: s.State(), MaximumCurrent: s.MaximumChargeCurrent()}
	s.importedAtTransfer = s.meterLocked(false)
	if s.HasFeature("V2X") {
		s.exportedAtTransfer = s.meterLocked(true)
		discharge := s.actualDischarge
		ev.MaximumDischargeCurrent = &discharge
	} else {
		s.exportedAtTransfer = 0
	}
	_ = s.emitLocked(evse.EventEnergyTransferStarted, ev)
}

// energyTransferStoppedLocked is SendEnergyTransferStoppedEvent
// (:1440-1474); EnergyDischarged is chip's start-minus-now (:1460).
func (s *EvseServer) energyTransferStoppedLocked(reason evse.EnergyTransferStoppedReasonEnum) {
	ev := evse.EnergyTransferStoppedEvent{
		SessionId: s.sessionIDLocked(), State: s.State(), Reason: reason,
		EnergyTransferred: s.meterLocked(false) - s.importedAtTransfer,
	}
	if s.HasFeature("V2X") {
		discharged := s.exportedAtTransfer - s.meterLocked(true)
		ev.EnergyDischarged = &discharged
	}
	_ = s.emitLocked(evse.EventEnergyTransferStopped, ev)
}

// emitFaultLocked is SendFaultEvent (:1476-1494): SessionID may be null,
// State is the one before the fault.
func (s *EvseServer) emitFaultLocked(fault EvseFault) {
	ev := evse.FaultEvent{SessionId: spec.NullOf[uint32](), State: s.State(), FaultStatePreviousState: s.FaultState(), FaultStateCurrentState: fault}
	if id := s.SessionID(); id != nil {
		ev.SessionId = spec.ValueOf(*id)
	}
	_ = s.emitLocked(evse.EventFault, ev)
}

func (s *EvseServer) emitLocked(eventID uint32, data any) error {
	if err := s.srv.Emit(s.endpoint, eventID, data); err != nil {
		return fmt.Errorf("energy: %w", err)
	}
	return nil
}

// startSessionLocked is EvseSession::StartSession (:1747-1783), which the
// delegate calls with meter values 0 (:900).
func (s *EvseServer) startSessionLocked() {
	s.session = evseSession{startTime: s.nowEpoch()}
	next := uint32(0)
	if id := s.SessionID(); id != nil {
		next = *id + 1
	}
	_ = s.setLocked(evse.AttrSessionId, next)
	s.forceLocked(evse.AttrSessionDuration, uint32(0))
	s.forceLocked(evse.AttrSessionEnergyCharged, int64(0))
	s.forceLocked(evse.AttrSessionEnergyDischarged, int64(0))
}

// stopSessionLocked is EvseSession::StopSession (:1792-1797), called with
// meter values 0 (:925).
func (s *EvseServer) stopSessionLocked() {
	s.recalculateSessionDurationLocked()
	s.forceLocked(evse.AttrSessionEnergyCharged, -s.session.chargedAtStart)
	s.forceLocked(evse.AttrSessionEnergyDischarged, -s.session.dischargedAtStart)
}

// recalculateSessionDurationLocked is RecalculateSessionDuration
// (:1806-1821).
func (s *EvseServer) recalculateSessionDurationLocked() {
	s.forceLocked(evse.AttrSessionDuration, s.nowEpoch()-s.session.startTime)
}

// --- the enabled-until timer (EvseDelegate.cpp:164-348) -------------------

// scheduleEnabledCheckLocked is ScheduleCheckOnEnabledTimeout
// (:265-335): a timer to the earliest enabled-until time of the supply
// state; one already passed disables at once.
func (s *EvseServer) scheduleEnabledCheckLocked() {
	var until *uint32
	switch s.SupplyState() {
	case SupplyChargingEnabled:
		until = s.nullU32(evse.AttrChargingEnabledUntil)
	case SupplyDischargingEnabled:
		until = s.nullU32(evse.AttrDischargingEnabledUntil)
	case SupplyEnabled:
		until = earliest(s.nullU32(evse.AttrChargingEnabledUntil), s.nullU32(evse.AttrDischargingEnabledUntil))
	default:
		return
	}
	if until == nil {
		return
	}
	now := s.nowEpoch()
	if *until > now { // :308-315
		s.stopEnabledTimerLocked()
		s.enabledTimer = time.AfterFunc(time.Duration(*until-now)*time.Second, s.enabledTimerExpired)
		return
	}
	switch s.SupplyState() { // :321-332
	case SupplyChargingEnabled, SupplyDischargingEnabled:
		_ = s.disableLocked()
	case SupplyEnabled:
		s.enabledStateExpirationLocked(now)
		s.scheduleEnabledCheckLocked()
	default:
	}
}

// earliest is GetEarliestTime (:164-172).
func earliest(a, b *uint32) *uint32 {
	switch {
	case a == nil:
		return b
	case b == nil:
		return a
	case *a < *b:
		return a
	}
	return b
}

func expired(t *uint32, now uint32) bool { return t != nil && *t <= now } // IsTimeExpired (:181-184)

// enabledStateExpirationLocked is HandleEnabledStateExpiration (:197-253).
func (s *EvseServer) enabledStateExpirationLocked(now uint32) {
	charging := expired(s.nullU32(evse.AttrChargingEnabledUntil), now)
	discharging := expired(s.nullU32(evse.AttrDischargingEnabledUntil), now)
	zero := uint32(0)
	if charging {
		_ = s.setLocked(evse.AttrChargingEnabledUntil, zero)
		s.setMinimumChargeCurrentLocked(0)
		s.chargeFromCommand = 0
		s.computeChargeLimitLocked()
		if !discharging {
			s.setSupplyStateLocked(SupplyDischargingEnabled)
		} else {
			_ = s.disableLocked()
		}
	}
	if discharging {
		_ = s.setLocked(evse.AttrDischargingEnabledUntil, zero)
		s.dischargeFromCmd = 0
		s.computeDischargeLimitLocked()
		if !charging {
			s.setSupplyStateLocked(SupplyChargingEnabled)
		} else {
			_ = s.disableLocked()
		}
	}
}

func (s *EvseServer) enabledTimerExpired() {
	s.lock()
	defer s.unlock()
	s.enabledTimer = nil
	s.scheduleEnabledCheckLocked()
}

func (s *EvseServer) stopEnabledTimerLocked() {
	if s.enabledTimer != nil {
		s.enabledTimer.Stop()
		s.enabledTimer = nil
	}
}

// --- commands ---------------------------------------------------------------

// disable is HandleDisable (EvseCluster.cpp:466-470) over Disable
// (EvseDelegate.cpp:41-63).
func (s *EvseServer) disable(_ context.Context, _ any) (any, error) {
	s.lock()
	defer s.unlock()
	return nil, s.disableLocked()
}

func (s *EvseServer) disableLocked() error {
	zero := uint32(0)
	_ = s.setLocked(evse.AttrChargingEnabledUntil, zero)    // :49
	_ = s.setLocked(evse.AttrDischargingEnabledUntil, zero) // :50
	s.setMinimumChargeCurrentLocked(0)                      // :53
	s.chargeFromCommand = 0                                 // :55
	s.computeChargeLimitLocked()
	s.dischargeFromCmd = 0 // :59
	s.computeDischargeLimitLocked()
	return s.disabledLocked() // :62
}

// enableCharging is HandleEnableCharging (EvseCluster.cpp:472-487) over
// EnableCharging (EvseDelegate.cpp:72-117).
func (s *EvseServer) enableCharging(_ context.Context, fields any) (any, error) {
	r, err := energyRequest[evse.EnableChargingRequest]("EnableCharging", fields)
	if err != nil {
		return nil, err
	}
	switch { // EvseCluster.cpp:482-484
	case r.MinimumChargeCurrent < evseMinimumChargeCurrentLimit, r.MaximumChargeCurrent < evseMinimumChargeCurrentLimit:
		return nil, demConstraint("negative charge current")
	case r.MinimumChargeCurrent > r.MaximumChargeCurrent:
		return nil, demConstraint("MinimumChargeCurrent %d above MaximumChargeCurrent %d", r.MinimumChargeCurrent, r.MaximumChargeCurrent)
	}
	s.lock()
	defer s.unlock()
	var until any // :107
	if !r.ChargingEnabledUntil.Null {
		until = r.ChargingEnabledUntil.Value
	}
	_ = s.setLocked(evse.AttrChargingEnabledUntil, until)
	s.chargeFromCommand = r.MaximumChargeCurrent            // :110
	s.setMinimumChargeCurrentLocked(r.MinimumChargeCurrent) // :111
	s.computeChargeLimitLocked()                            // :114
	return nil, s.chargingEnabledLocked()                   // :116
}

// enableDischarging is HandleEnableDischarging (EvseCluster.cpp:489-501)
// over EnableDischarging (EvseDelegate.cpp:125-157).
func (s *EvseServer) enableDischarging(_ context.Context, fields any) (any, error) {
	r, err := energyRequest[evse.EnableDischargingRequest]("EnableDischarging", fields)
	if err != nil {
		return nil, err
	}
	if r.MaximumDischargeCurrent < evseMinimumChargeCurrentLimit { // EvseCluster.cpp:498
		return nil, demConstraint("negative discharge current")
	}
	s.lock()
	defer s.unlock()
	var until any // :148
	if !r.DischargingEnabledUntil.Null {
		until = r.DischargingEnabledUntil.Value
	}
	_ = s.setLocked(evse.AttrDischargingEnabledUntil, until)
	s.dischargeFromCmd = r.MaximumDischargeCurrent // :151
	s.computeDischargeLimitLocked()
	return nil, s.dischargingEnabledLocked() // :156
}

// startDiagnostics is HandleStartDiagnostics (EvseCluster.cpp:503-507)
// over StartDiagnostics (EvseDelegate.cpp:356-373): only from Disabled.
func (s *EvseServer) startDiagnostics(_ context.Context, _ any) (any, error) {
	s.lock()
	defer s.unlock()
	if s.SupplyState() != SupplyDisabled {
		return nil, demFailure("SupplyState %d, not Disabled", s.SupplyState())
	}
	s.setSupplyStateLocked(SupplyDisabledDiagnostics)
	return nil, nil
}

// validateTargets is ValidateTargets (EvseCluster.cpp:509-572).
func (s *EvseServer) validateTargets(schedules []ChargingTargetSchedule) error {
	var days uint8
	soc := s.HasFeature("SOC")
	for _, sch := range schedules {
		mask := uint8(sch.DayOfWeekForSequence) & evseDayOfWeekMask // :518
		if mask == 0 {                                              // :521-522
			return demConstraint("DayOfWeekForSequence is empty")
		}
		if days&mask != 0 { // :524-525
			return demConstraint("DayOfWeekForSequence 0x%02X repeats a day", mask)
		}
		days |= mask
		for _, t := range sch.ChargingTargets {
			if t.TargetTimeMinutesPastMidnight > evseMaxMinutesPastMidnight { // :537-538
				return demConstraint("TargetTimeMinutesPastMidnight %d", t.TargetTimeMinutesPastMidnight)
			}
			if soc { // :540-547
				if t.TargetSoC == nil {
					return spec.Errorf(im.StatusInvalidCommand, "energy: SoCReporting without TargetSoC")
				}
				if *t.TargetSoC > evseMaxTargetSoC {
					return demConstraint("TargetSoC %d", *t.TargetSoC)
				}
			} else if t.TargetSoC != nil && *t.TargetSoC != evseMaxTargetSoC { // :548-553
				return demConstraint("TargetSoC must be 100 without SoCReporting")
			}
			if t.TargetSoC == nil && t.AddedEnergy == nil { // :555-556
				return demFailure("a target needs AddedEnergy or TargetSoC")
			}
			if t.AddedEnergy != nil && *t.AddedEnergy < 0 { // :558-560
				return demConstraint("AddedEnergy %d", *t.AddedEnergy)
			}
		}
		if len(sch.ChargingTargets) > evseMaxTargetsPerDay { // :564-565
			return spec.Errorf(im.StatusResourceExhausted, "energy: %d targets in a day", len(sch.ChargingTargets))
		}
	}
	return nil
}

// setTargets is HandleSetTargets (EvseCluster.cpp:574-586) over SetTargets
// (EvseDelegate.cpp:378-399) and the store's merge (TargetsStore.cpp:
// 245-389).
func (s *EvseServer) setTargets(_ context.Context, fields any) (any, error) {
	r, err := energyRequest[evse.SetTargetsRequest]("SetTargets", fields)
	if err != nil {
		return nil, err
	}
	if err := s.validateTargets(r.ChargingTargetSchedules); err != nil {
		return nil, err
	}
	s.lock()
	defer s.unlock()
	merged, err := mergeTargets(s.targets, r.ChargingTargetSchedules)
	if err != nil {
		return nil, err
	}
	s.targets = merged
	s.notifyLocked(EvseChargingPreferencesChanged) // :396
	return nil, nil
}

// mergeTargets is EvseTargetsDelegate::SetTargets (TargetsStore.cpp:
// 245-389). For each new schedule, every stored one whose days lie wholly
// inside the new days takes the new targets (and keeps its own days); any
// other loses the new days. A new schedule that replaced none is
// appended; an eighth is RESOURCE_EXHAUSTED (:345, CHIP_ERROR_NO_MEMORY →
// EvseDelegate.cpp:387-390). As in chip, a stored schedule whose days are
// all taken away stays, with no days.
func mergeTargets(current, updates []ChargingTargetSchedule) ([]ChargingTargetSchedule, error) {
	list := cloneSchedules(current)
	for _, n := range updates {
		newMask := uint8(n.DayOfWeekForSequence) & evseDayOfWeekMask // :271-272
		found := false
		next := make([]ChargingTargetSchedule, 0, len(list)+1)
		for _, c := range list {
			cur := uint8(c.DayOfWeekForSequence) & evseDayOfWeekMask // :285-286
			a, b := cur&newMask, cur&^newMask                        // :293-294
			if cur == a {                                            // :298-314
				next = append(next, ChargingTargetSchedule{DayOfWeekForSequence: TargetDays(a), ChargingTargets: append([]ChargingTarget(nil), n.ChargingTargets...)})
				found = true
				continue
			}
			next = append(next, ChargingTargetSchedule{DayOfWeekForSequence: TargetDays(b), ChargingTargets: c.ChargingTargets}) // :315-329
		}
		if !found { // :343-366
			if len(next) >= evseMaxTargetsDays {
				return nil, spec.Errorf(im.StatusResourceExhausted, "energy: more than %d target schedules", evseMaxTargetsDays)
			}
			next = append(next, ChargingTargetSchedule{DayOfWeekForSequence: n.DayOfWeekForSequence, ChargingTargets: append([]ChargingTarget(nil), n.ChargingTargets...)})
		}
		list = next
	}
	return list, nil
}

// getTargets is HandleGetTargets (EvseCluster.cpp:588-598).
func (s *EvseServer) getTargets(_ context.Context, _ any) (any, error) {
	s.lock()
	defer s.unlock()
	return evse.GetTargetsResponse{ChargingTargetSchedules: cloneSchedules(s.targets)}, nil
}

// clearTargets is HandleClearTargets (EvseCluster.cpp:600-604) over
// ClearTargets (EvseDelegate.cpp:429-454).
func (s *EvseServer) clearTargets(_ context.Context, _ any) (any, error) {
	s.lock()
	defer s.unlock()
	s.targets = nil
	s.notifyLocked(EvseChargingPreferencesChanged)
	return nil, nil
}

// MatterWriteAttribute implements [spec.Sink]: the cluster's writes
// (EvseCluster.cpp:351-377). An unchanged value is a success that does
// nothing (:359, :365, :371); UserMaximumChargeCurrent must not be
// negative (:138) and recomputes the charge limit (EvseDelegate.cpp:
// 1565-1571).
func (s *EvseServer) MatterWriteAttribute(_ context.Context, attrID uint32, value any) error {
	if attrID != evse.AttrUserMaximumChargeCurrent {
		return nil
	}
	mA, ok := value.(int64)
	if !ok {
		return spec.Errorf(im.StatusInvalidDataType, "energy: UserMaximumChargeCurrent %T", value)
	}
	s.lock()
	defer s.unlock()
	if mA < 0 {
		return demConstraint("UserMaximumChargeCurrent %d", mA)
	}
	if mA == s.userMaxCharge {
		return nil
	}
	s.userMaxCharge = mA
	s.computeChargeLimitLocked()
	return nil
}

// --- plumbing ---------------------------------------------------------------

// SetEndpoint stamps the endpoint the events are addressed to; the bridge
// calls it at reassembly.
func (s *EvseServer) SetEndpoint(endpoint uint16) {
	s.lock()
	s.endpoint = endpoint
	s.unlock()
}

// Close stops the enabled-until timer.
func (s *EvseServer) Close() {
	s.lock()
	defer s.unlock()
	s.stopEnabledTimerLocked()
}

// SetMatterEventEmitter implements [contract.EventReceiver].
func (s *EvseServer) SetMatterEventEmitter(emitter contract.EventEmitter) {
	s.srv.SetMatterEventEmitter(emitter)
}

// MatterDataVersion implements [contract.ClusterDataVersion].
func (s *EvseServer) MatterDataVersion() uint32 { return s.srv.MatterDataVersion() }

// OnMatterAttributesChanged implements [contract.AttributeChangeNotifier].
func (s *EvseServer) OnMatterAttributesChanged(cb func(attrIDs []uint32)) (unsubscribe func()) {
	return s.srv.OnMatterAttributesChanged(cb)
}

// MatterRead resolves an attribute.
func (s *EvseServer) MatterRead(attrID uint32) (any, bool) { return s.srv.MatterRead(attrID) }

// MatterWrite checks a write against the definition, then the cluster's
// rules ([EvseServer.MatterWriteAttribute]).
func (s *EvseServer) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	return s.srv.MatterWrite(ctx, attrID, value)
}

// MatterInvoke dispatches the commands.
func (s *EvseServer) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	return s.srv.MatterInvoke(ctx, cmdID, fields)
}

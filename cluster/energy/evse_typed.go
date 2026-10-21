// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package energy

import (
	"fmt"

	"github.com/SukramJ/go-fabric/cluster/spec"
	evse "github.com/SukramJ/go-fabric/cluster/spec/energyevse"
)

// The typed server's optional interfaces of the self-held EnergyEvse
// server: Handle, Emit and SetBeforeWrite come from the embedded
// [spec.SelfHeld]; SetAttributes keeps the state machine's rules.
var (
	_ spec.AttributeSetter   = (*EvseServer)(nil)
	_ spec.CommandRegistrar  = (*EvseServer)(nil)
	_ spec.EventEmitting     = (*EvseServer)(nil)
	_ spec.BeforeWriteSetter = (*EvseServer)(nil)
)

// evseDerived are the attributes the state machine computes — State,
// SupplyState and FaultState from [EvseServer.SetHardwareState],
// [EvseServer.SetFault] and the commands, the enable deadlines from the
// Enable / Disable commands, the charge limits from the hardware, circuit,
// cable and user limits, the session from the charging state.
var evseDerived = map[uint32]bool{
	evse.AttrState: true, evse.AttrSupplyState: true, evse.AttrFaultState: true,
	evse.AttrChargingEnabledUntil: true, evse.AttrDischargingEnabledUntil: true,
	evse.AttrMinimumChargeCurrent: true, evse.AttrMaximumChargeCurrent: true, evse.AttrMaximumDischargeCurrent: true,
	evse.AttrSessionId: true, evse.AttrSessionDuration: true, evse.AttrSessionEnergyCharged: true, evse.AttrSessionEnergyDischarged: true,
}

// SetAttributes implements [spec.AttributeSetter]: the host's own change,
// all or nothing — every value is checked against the definition and its
// setter's rule before any is applied — and the host port is not called.
// CircuitCapacity and UserMaximumChargeCurrent recompute the charge limit
// as [EvseServer.SetCircuitCapacity] and [EvseServer.SetUserMaximumChargeCurrent]
// do; VehicleId is [EvseServer.SetVehicleID]'s (an empty one is null);
// StateOfCharge, BatteryCapacity, the NextCharge attributes,
// RandomizationDelayWindow and ApproximateEvEfficiency are stored as they
// are. The attributes the state machine computes are refused
// ([spec.ErrNotHeld]): its inputs are [EvseServer.SetHardwareState],
// [EvseServer.SetFault] and the commands.
func (s *EvseServer) SetAttributes(values map[uint32]any) error {
	ids, checked, err := checkedSet(s.Instance, values)
	if err != nil {
		return err
	}
	for _, id := range ids {
		v := checked[id]
		switch {
		case evseDerived[id]:
			return fmt.Errorf("%w: energy: EnergyEvse attribute 0x%04X is the state machine's", spec.ErrNotHeld, id)
		case id == evse.AttrCircuitCapacity:
			if mA, _ := v.(int64); mA < evseMinimumChargeCurrentLimit {
				return demConstraint("circuit capacity %d mA", mA)
			}
		case id == evse.AttrUserMaximumChargeCurrent:
			if mA, _ := v.(int64); mA < 0 {
				return demConstraint("user maximum charge current %d mA", mA)
			}
		case id == evse.AttrVehicleId:
			if str, _ := v.(string); len(str) > evseMaxVehicleIDLen {
				return demFailure("vehicle id of %d bytes", len(str))
			}
		}
	}
	s.lock()
	defer s.unlock()
	recompute := false
	for _, id := range ids {
		v := checked[id]
		switch id {
		case evse.AttrCircuitCapacity:
			recompute = true
		case evse.AttrUserMaximumChargeCurrent:
			s.userMaxCharge, _ = v.(int64)
			recompute = true
		case evse.AttrVehicleId:
			if str, _ := v.(string); str != "" {
				s.vehicleID = &str
			} else {
				s.vehicleID, v = nil, nil
			}
		}
		if err := s.setLocked(id, v); err != nil {
			return err
		}
	}
	if recompute {
		s.computeChargeLimitLocked()
	}
	return nil
}

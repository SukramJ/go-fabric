// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	fandef "github.com/SukramJ/go-fabric/cluster/spec/fancontrol"
	pumpdef "github.com/SukramJ/go-fabric/cluster/spec/pumpconfigurationandcontrol"
	thermostatdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	valvedef "github.com/SukramJ/go-fabric/cluster/spec/valveconfigurationandcontrol"
)

// HoldsAttribute reports whether the value of clusterID's attrID is the
// state of a server the device built itself: the device mounts a server
// for the cluster, the host did not supply it (a declared HostServer),
// and no host port the declaration carries takes the attribute's writes —
// the Thermostat's Sink (SystemMode, the four setpoints,
// TemperatureSetpointHold), FanControl's StateSource (FanMode), the
// Pump's StateSource (OperationMode; ControlMode, LifetimeRunningHours and
// LifetimeEnergyConsumed when it takes them) and the Valve's StateSource
// (DefaultOpenDuration; DefaultOpenLevel when it takes it). A cluster
// served from a [Measurement] source is the host's. The node stores a
// controller's write of such an attribute when it is nonvolatile, and
// restores it before it serves the device.
func (d *Device) HoldsAttribute(clusterID, attrID uint32) bool {
	if d.hosted[clusterID] || d.Server(clusterID) == nil {
		return false
	}
	return !portTakes(clusterID, d.ports[clusterID], attrID)
}

// portTakes reports whether the host port of the cluster's declaration
// takes the writes of attrID, so its value is the host device's state.
func portTakes(clusterID uint32, ports any, attrID uint32) bool {
	switch clusterID {
	case thermostatdef.ClusterID:
		d, _ := ports.(thermostatdef.Declare)
		if d.Sink == nil {
			return false
		}
		switch attrID {
		case thermostatdef.AttrSystemMode, thermostatdef.AttrOccupiedHeatingSetpoint, thermostatdef.AttrOccupiedCoolingSetpoint,
			thermostatdef.AttrUnoccupiedHeatingSetpoint, thermostatdef.AttrUnoccupiedCoolingSetpoint, thermostatdef.AttrTemperatureSetpointHold:
			return true
		}
	case fandef.ClusterID:
		d, _ := ports.(fandef.Declare)
		return d.StateSource != nil && attrID == fandef.AttrFanMode
	case pumpdef.ClusterID:
		d, _ := ports.(pumpdef.Declare)
		if d.StateSource == nil {
			return false
		}
		switch attrID {
		case pumpdef.AttrOperationMode:
			return true
		case pumpdef.AttrControlMode:
			_, ok := d.StateSource.(pumpdef.ControlModeSetter)
			return ok
		case pumpdef.AttrLifetimeRunningHours:
			_, ok := d.StateSource.(pumpdef.RunningHoursSetter)
			return ok
		case pumpdef.AttrLifetimeEnergyConsumed:
			_, ok := d.StateSource.(pumpdef.EnergyConsumedSetter)
			return ok
		}
	case valvedef.ClusterID:
		d, _ := ports.(valvedef.Declare)
		if d.StateSource == nil {
			return false
		}
		switch attrID {
		case valvedef.AttrDefaultOpenDuration:
			return true
		case valvedef.AttrDefaultOpenLevel:
			_, ok := d.StateSource.(valvedef.DefaultOpenLevelSource)
			return ok
		}
	}
	return false
}

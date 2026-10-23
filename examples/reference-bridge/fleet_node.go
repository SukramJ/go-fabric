// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"log/slog"
	"sync"

	"github.com/SukramJ/go-fabric/cluster/spec"
	actionsdef "github.com/SukramJ/go-fabric/cluster/spec/actions"
	ecodef "github.com/SukramJ/go-fabric/cluster/spec/ecosysteminformation"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/node"
)

// The node-level clusters of the reference daemon: Actions on the
// Aggregator, PowerSourceConfiguration on the root, EcosystemInformation
// on one bridged endpoint. Their contents are this daemon's demo data; the
// endpoint ids they name are known only once the topology is assembled,
// so both lists are read through the live topology.

// The StableKeys the node clusters point at.
const (
	// evsePowerKey is the EV charger's PowerSource part (fleet_evse.go
	// parts), the node's one PowerSource endpoint that is a power source
	// of its own rather than a battery of a sensor.
	evsePowerKey = device.StringKey("demo:evse:power")
	// lampKey is the desk lamp, the endpoint the demo actions switch.
	lampKey = device.StringKey("demo:light:1")
)

// endpointByKey returns the live endpoint number of the bridged device or
// part with key; false before the first assembly or for a key the
// topology does not hold.
func endpointByKey(topology node.BridgeTopology, key device.StringKey) (uint16, bool) {
	return findByKey(topology.Devices, key)
}

func findByKey(devices []node.BridgedDevice, key device.StringKey) (uint16, bool) {
	for _, d := range devices {
		if d.Key == key {
			return d.Endpoint, true
		}
		if n, ok := findByKey(d.Parts, key); ok {
			return n, true
		}
	}
	return 0, false
}

// attachNodeProviders points PowerSourceConfiguration's Sources at the EV
// charger's PowerSource endpoint, the demo actions at the desk lamp's and
// the fleet's endpoint lookup at the live topology. Attached after Start,
// as the PartsList providers are.
func attachNodeProviders(topology func() node.BridgeTopology, pscfg *node.PowerSourceConfigurationServer, f *fleet) {
	resolve := func(key device.StringKey) (uint16, bool) { return endpointByKey(topology(), key) }
	f.endpointOf = resolve
	actions := f.actions
	if pscfg != nil {
		pscfg.SetSourcesProvider(func() []uint16 {
			if id, ok := resolve(evsePowerKey); ok {
				return []uint16{id}
			}
			return nil
		})
	}
	if actions != nil {
		actions.setLampEndpoint(func() (uint16, bool) { return resolve(lampKey) })
	}
}

// --- Actions on the Aggregator -------------------------------------------

// The demo's ids: one endpoint list, two actions. connectedhomeip's
// all-clusters app serves three actions that support no command at all
// (examples/all-clusters-app/all-clusters-common/include/
// bridged-actions-stub.h:36-40, ActionStructStorage(…, 0, kInactive)); these two
// support InstantAction, so a controller can run them.
const (
	demoLivingRoomList uint16 = 0xE001
	demoLightsOn       uint16 = 0x1001
	demoLightsOff      uint16 = 0x1002
)

// demoActions is the Aggregator's [node.ActionsHost]: two scenes
// over the living room, which holds the desk lamp. An instant action
// leaves its action Inactive, so it raises no StateChanged.
type demoActions struct {
	light *demoLight

	mu   sync.RWMutex
	lamp func() (uint16, bool)
}

var _ node.ActionsHost = (*demoActions)(nil)

func newDemoActions(light *demoLight) *demoActions { return &demoActions{light: light} }

func (a *demoActions) setLampEndpoint(fn func() (uint16, bool)) {
	a.mu.Lock()
	a.lamp = fn
	a.mu.Unlock()
}

// ActionList implements [node.ActionsHost].
func (*demoActions) ActionList() []actionsdef.ActionStruct {
	return []actionsdef.ActionStruct{
		{
			ActionId: demoLightsOn, Name: "Lights On", Type: actionsdef.ActionTypeScene,
			EndpointListId: demoLivingRoomList, SupportedCommands: actionsdef.CommandBitsInstantAction,
			State: actionsdef.ActionStateInactive,
		},
		{
			ActionId: demoLightsOff, Name: "Lights Off", Type: actionsdef.ActionTypeScene,
			EndpointListId: demoLivingRoomList, SupportedCommands: actionsdef.CommandBitsInstantAction,
			State: actionsdef.ActionStateInactive,
		},
	}
}

// EndpointLists implements [node.ActionsHost]: the living room, with
// the desk lamp once the topology numbers it.
func (a *demoActions) EndpointLists() []actionsdef.EndpointListStruct {
	endpoints := []uint16{}
	a.mu.RLock()
	lamp := a.lamp
	a.mu.RUnlock()
	if lamp != nil {
		if id, ok := lamp(); ok {
			endpoints = append(endpoints, id)
		}
	}
	return []actionsdef.EndpointListStruct{{
		EndpointListId: demoLivingRoomList, Name: "Living Room", Type: actionsdef.EndpointListTypeRoom, Endpoints: endpoints,
	}}
}

// HandleAction implements [node.ActionsHost]. The server has
// already refused an unknown action and a command the action does not
// support, so only InstantAction on one of the two reaches here.
func (a *demoActions) HandleAction(_ context.Context, req node.ActionRequest) error {
	on := req.ActionID == demoLightsOn
	if a.light != nil {
		a.light.setOn(on)
	}
	slog.Info("actions.applied", slog.Int("action", int(req.ActionID)), slog.Bool("on", on))
	return nil
}

// --- EcosystemInformation on a bridged endpoint ------------------------------

// demoEcosystemFabric is the fabric the demo directory belongs to: the
// first fabric index a commissioner is given. A controller on another
// fabric reads the directory empty (filtered) or redacted (unfiltered).
const demoEcosystemFabric uint8 = 1

// demoEcosystem is a static [node.EcosystemDirectory]: the air
// quality sensor as the ecosystem behind the bridge knows it, in one room.
type demoEcosystem struct{ name string }

var _ node.EcosystemDirectory = demoEcosystem{}

// EcosystemDevices implements [node.EcosystemDirectory].
func (e demoEcosystem) EcosystemDevices(context.Context) []ecodef.EcosystemDeviceStruct {
	name := e.name
	original := uint16(1)
	airQuality := uint32(contract.MeasurementClassDeviceType(contract.MeasurementCO2))
	return []ecodef.EcosystemDeviceStruct{{
		DeviceName:       &name,
		OriginalEndpoint: &original,
		DeviceTypes: []ecodef.DeviceTypeStruct{{
			DeviceType: airQuality,
			Revision:   deviceTypeRevision(airQuality),
		}},
		UniqueLocationIds: []string{"living-room"},
		FabricIndex:       demoEcosystemFabric,
	}}
}

// EcosystemLocations implements [node.EcosystemDirectory].
func (demoEcosystem) EcosystemLocations(context.Context) []ecodef.EcosystemLocationStruct {
	return []ecodef.EcosystemLocationStruct{{
		UniqueLocationId: "living-room",
		LocationDescriptor: ecodef.Locationdesc{
			LocationName: "Living Room",
			FloorNumber:  spec.Nullable[int16]{Value: 0},
			AreaType:     spec.Nullable[uint8]{Null: true},
		},
		FabricIndex: demoEcosystemFabric,
	}}
}

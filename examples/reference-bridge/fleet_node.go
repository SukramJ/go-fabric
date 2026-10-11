// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"log/slog"
	"sync"

	matterbridge "github.com/SukramJ/go-fabric/bridge"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec"
	actionsdef "github.com/SukramJ/go-fabric/cluster/spec/actions"
	ecodef "github.com/SukramJ/go-fabric/cluster/spec/ecosysteminformation"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/endpoint"
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
	evsePowerKey = endpoint.StringKey("demo:evse:power")
	// lampKey is the desk lamp, the endpoint the demo actions switch.
	lampKey = endpoint.StringKey("demo:light:1")
)

// endpointByKey returns the live endpoint id of the spec with key; false
// before the first assembly or for a key the topology does not hold.
func endpointByKey(br *matterbridge.Bridge, key endpoint.StringKey) (uint16, bool) {
	topology := br.Topology()
	if topology == nil {
		return 0, false
	}
	for _, ep := range topology.Endpoints {
		if ep != nil && ep.SourceKey == key {
			return ep.ID, true
		}
	}
	return 0, false
}

// attachNodeProviders points PowerSourceConfiguration's Sources at the EV
// charger's PowerSource endpoint, the demo actions at the desk lamp's and
// the fleet's endpoint lookup at the live topology. Attached after Start,
// as the PartsList providers are.
func attachNodeProviders(br *matterbridge.Bridge, pscfg *mattercore.PowerSourceConfiguration, f *fleet) {
	f.endpointOf = func(key endpoint.StringKey) (uint16, bool) { return endpointByKey(br, key) }
	actions := f.actions
	if pscfg != nil {
		pscfg.SetSourcesProvider(func() []uint16 {
			if id, ok := endpointByKey(br, evsePowerKey); ok {
				return []uint16{id}
			}
			return nil
		})
	}
	if actions != nil {
		actions.setLampEndpoint(func() (uint16, bool) { return endpointByKey(br, lampKey) })
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

// demoActions is the Aggregator's [mattercore.ActionsHost]: two scenes
// over the living room, which holds the desk lamp. An instant action
// leaves its action Inactive, so it raises no StateChanged.
type demoActions struct {
	light *demoLight

	mu   sync.RWMutex
	lamp func() (uint16, bool)
}

var _ mattercore.ActionsHost = (*demoActions)(nil)

func newDemoActions(light *demoLight) *demoActions { return &demoActions{light: light} }

func (a *demoActions) setLampEndpoint(fn func() (uint16, bool)) {
	a.mu.Lock()
	a.lamp = fn
	a.mu.Unlock()
}

// ActionList implements [mattercore.ActionsHost].
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

// EndpointLists implements [mattercore.ActionsHost]: the living room, with
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

// HandleAction implements [mattercore.ActionsHost]. The server has
// already refused an unknown action and a command the action does not
// support, so only InstantAction on one of the two reaches here.
func (a *demoActions) HandleAction(_ context.Context, req mattercore.ActionRequest) error {
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

// demoEcosystem is a static [mattercore.EcosystemDirectory]: the air
// quality sensor as the ecosystem behind the bridge knows it, in one room.
type demoEcosystem struct{ name string }

var _ mattercore.EcosystemDirectory = demoEcosystem{}

// EcosystemDevices implements [mattercore.EcosystemDirectory].
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

// EcosystemLocations implements [mattercore.EcosystemDirectory].
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

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/schema"
)

// controllerParity is what one matter.js controller device file states
// (packages/node/src/devices/<name>.ts at 0ceb202d): the type's identity,
// its `server.mandatory` / `server.optional` blocks and its
// `client.mandatory` / `client.optional` blocks, transcribed in order —
// packages/node/src/devices/on-off-light-switch.ts:75-95 is the shape of all eleven.
// CastingVideoClient (casting-video-client.ts at 862ad75f: the Binding
// server, the optional ContentAppObserver server, client behaviours only)
// has the same shape and is held here too.
type controllerParity struct {
	ty                              *Type
	id, revision                    uint16
	serverMandatory, serverOptional []string
	clientMandatory, clientOptional []string
}

var controllerTypes = []controllerParity{
	{
		&OnOffLightSwitchType, 0x0103, 4,
		[]string{"Identify", "Binding"},
		nil,
		[]string{"Identify", "OnOff"},
		[]string{"Groups", "ScenesManagement"},
	},
	{
		&DimmerSwitchType, 0x0104, 4,
		[]string{"Identify", "Binding"},
		nil,
		[]string{"Identify", "OnOff", "LevelControl"},
		[]string{"Groups", "ScenesManagement"},
	},
	{
		&ColorDimmerSwitchType, 0x0105, 4,
		[]string{"Identify", "Binding"},
		nil,
		[]string{"Identify", "OnOff", "LevelControl", "ColorControl"},
		[]string{"Groups", "ScenesManagement"},
	},
	{
		&ControlBridgeType, 0x0840, 4,
		[]string{"Identify", "Binding"},
		nil,
		[]string{"Identify", "Groups", "OnOff", "LevelControl", "ScenesManagement", "ColorControl"},
		[]string{"IlluminanceMeasurement", "OccupancySensing"},
	},
	{
		&OnOffSensorType, 0x0850, 4,
		[]string{"Identify", "Binding"},
		nil,
		[]string{"Identify", "OnOff"},
		[]string{"Groups", "LevelControl", "ScenesManagement", "ColorControl"},
	},
	{
		&DoorLockControllerType, 0x000B, 4,
		[]string{"Binding"},
		nil,
		[]string{"DoorLock"},
		[]string{"Groups", "ScenesManagement"},
	},
	{
		&WindowCoveringControllerType, 0x0203, 5,
		[]string{"Binding"},
		[]string{"Identify"},
		[]string{"WindowCovering"},
		[]string{"Identify", "Groups"},
	},
	{
		&ThermostatControllerType, 0x030A, 2,
		[]string{"Binding"},
		nil,
		[]string{"Thermostat"},
		[]string{"Identify", "Groups", "ScenesManagement"},
	},
	{
		&PumpControllerType, 0x0304, 5,
		[]string{"Identify", "Binding"},
		nil,
		[]string{"OnOff", "PumpConfigurationAndControl"},
		[]string{"Identify", "Groups", "LevelControl", "ScenesManagement", "TemperatureMeasurement", "PressureMeasurement", "FlowMeasurement"},
	},
	{
		&ClosureControllerType, 0x023E, 2,
		[]string{"Binding"},
		nil,
		[]string{"ClosureControl"},
		[]string{"Identify", "ClosureDimension"},
	},
	{
		&VideoRemoteControlType, 0x002A, 2,
		[]string{"Binding"},
		nil,
		[]string{"OnOff", "MediaPlayback", "KeypadInput"},
		[]string{"LevelControl", "WakeOnLan", "Channel", "TargetNavigator", "MediaInput", "LowPower", "ContentLauncher", "AudioOutput", "ApplicationLauncher", "AccountLogin", "ContentControl"},
	},
	{
		&CastingVideoClientType, 0x0029, 2,
		[]string{"Binding"},
		[]string{"ContentAppObserver"},
		[]string{"OnOff", "KeypadInput", "ContentLauncher", "ApplicationBasic"},
		[]string{"LevelControl", "Messages", "WakeOnLan", "Channel", "TargetNavigator", "MediaPlayback", "MediaInput", "LowPower", "AudioOutput", "ApplicationLauncher", "AccountLogin", "ContentControl"},
	},
}

func splitNames(reqs []ClusterRequirement) (mandatory, optional []string) {
	for _, r := range reqs {
		if r.Mandatory {
			mandatory = append(mandatory, r.Name)
		} else {
			optional = append(optional, r.Name)
		}
	}
	return mandatory, optional
}

// TestControllerDeviceTypesParity: each of the eleven controller device
// types and CastingVideoClient names its identity, its servers and its
// client set as the matter.js device file does, and its client set is the snapshot's clientCluster
// requirements (parity/schema.json, read through schema), mandatory where
// the conformance is "M" — what matter.js's RequirementGenerator emits as
// `client.mandatory`.
func TestControllerDeviceTypesParity(t *testing.T) {
	t.Parallel()
	for _, c := range controllerTypes {
		ty := c.ty
		t.Run(ty.Name, func(t *testing.T) {
			t.Parallel()
			if uint16(ty.ID) != c.id || ty.Revision != c.revision {
				t.Errorf("identity 0x%04X r%d, want 0x%04X r%d", ty.ID, ty.Revision, c.id, c.revision)
			}
			if got, ok := TypeByID(schema.DeviceTypeID(c.id)); !ok || got != ty {
				t.Error("not in the registry")
			}
			sm, so := splitNames(ty.Servers)
			if !slices.Equal(sm, c.serverMandatory) || !slices.Equal(so, c.serverOptional) {
				t.Errorf("servers %v / %v, want %v / %v", sm, so, c.serverMandatory, c.serverOptional)
			}
			cm, co := splitNames(ty.Clients)
			if !slices.Equal(cm, c.clientMandatory) || !slices.Equal(co, c.clientOptional) {
				t.Errorf("clients %v / %v, want %v / %v", cm, co, c.clientMandatory, c.clientOptional)
			}
			def, ok := schema.DeviceTypeDefinitionOf(schema.DeviceTypeID(c.id))
			if !ok {
				t.Fatal("not in the snapshot")
			}
			snapshot := map[uint32]bool{}
			for i := range def.Requirements {
				r := &def.Requirements[i]
				if r.Element != schema.RequirementClientCluster || r.Conformance.Text == "X" {
					continue
				}
				snapshot[r.Referent.ID] = r.Conformance.Text == "M"
			}
			if len(snapshot) != len(ty.Clients) {
				t.Errorf("%d client requirements, the snapshot states %d", len(ty.Clients), len(snapshot))
			}
			for _, r := range ty.Clients {
				m, ok := snapshot[r.ID]
				if !ok || m != r.Mandatory {
					t.Errorf("client %s (0x%04X): mandatory %v, the snapshot %v (stated %v)", r.Name, r.ID, r.Mandatory, m, ok)
				}
				if r.Definition != nil {
					t.Errorf("client %s carries a server definition", r.Name)
				}
			}
			// Binding is Base's "Simple & Client": every controller type
			// mandates a bindable application client.
			if b, ok := ty.Server(0x001E); !ok || !b.Mandatory || !b.BaseMandated {
				t.Errorf("Binding %+v", b)
			}
		})
	}
}

// TestControllerDeviceDeclaresClients: a constructed OnOffLightSwitch
// declares the type's mandatory clients and those WithClientClusters adds
// (matter.js MutableEndpoint.ts:53-61, :123-129), each once, and mounts
// the module's Binding server.
func TestControllerDeviceDeclaresClients(t *testing.T) {
	t.Parallel()
	d, err := NewOnOffLightSwitch(testOptions("switch"), WithClientClusters(0x0004, 0x0006))
	if err != nil {
		t.Fatal(err)
	}
	if got := d.Spec().ClientClusters; !slices.Equal(got, []uint32{0x0003, 0x0006, 0x0004}) {
		t.Fatalf("ClientClusters %v", got)
	}
	if d.Binding() == nil {
		t.Fatal("no Binding server")
	}
	plain, err := NewOnOffLightSwitch(Options{StableKey: testOptions("plain").StableKey})
	if err != nil {
		t.Fatal(err)
	}
	if got := plain.Spec().ClientClusters; !slices.Equal(got, []uint32{0x0003, 0x0006}) {
		t.Fatalf("ClientClusters %v", got)
	}
}

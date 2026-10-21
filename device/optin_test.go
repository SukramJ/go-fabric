// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"errors"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/endpoint"
)

const (
	groupsID = 0x0004
	scenesID = 0x0062
)

// TestOptionalGroupsAndScenes: the declaration opts a device into the Groups
// or ScenesManagement its type lists as optional (report P question 7);
// the assembler mounts the stack's server (endpoint.Spec.OptIn), the
// device mounts none itself, and the endpoint still conforms to its
// device type.
func TestOptionalGroupsAndScenes(t *testing.T) {
	t.Parallel()
	for name, ids := range map[string][]uint32{
		"AirPurifier":        {groupsID},
		"Pump":               {groupsID, scenesID},
		"RoomAirConditioner": {groupsID, scenesID},
		"SmokeCoAlarm":       {groupsID},
		"Thermostat":         {groupsID},
		"WindowCovering":     {groupsID},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ty := typeByName(name)
			with := testWith(t, name)
			for _, id := range ids {
				with = append(with, withOptional(id))
			}
			d, err := New(ty, testOptions(name), with...)
			if err != nil {
				t.Fatal(err)
			}
			if f, ok := fixtures[name]; ok && f.parts != nil {
				f.parts(t, d)
			}
			if got := d.Spec().OptIn; !slices.Equal(got, ids) {
				t.Errorf("Spec OptIn %v, want %v", got, ids)
			}
			for _, id := range ids {
				if d.Server(id) != nil {
					t.Errorf("the device mounts cluster 0x%04X itself", id)
				}
			}
			topo := assemble(t, d)
			var ep *endpoint.Endpoint
			for _, e := range topo.Endpoints {
				if e.DeviceType == ty.ID && !e.Part {
					ep = e
				}
			}
			if ep == nil {
				t.Fatal("device endpoint not assembled")
			}
			served := clusterIDs(endpoint.ClusterServers(ep))
			for _, id := range ids {
				if !slices.Contains(served, id) {
					t.Errorf("cluster 0x%04X not mounted: %v", id, served)
				}
			}
			tolerate := fixtures[name].tolerate
			for _, v := range violationsOf(topo) {
				if _, ok := tolerate[v.Key()]; !ok {
					t.Errorf("device type violation: %s", v)
				}
			}
		})
	}
}

// TestOptInRefusals: an opt-in is the only option for the assembler's
// clusters, and only where the type lists them.
func TestOptInRefusals(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		ty   *Type
		with []Option
	}{
		"Groups on a type that does not list it": {&TemperatureSensorType, []Option{withOptional(groupsID)}},
		"Identify":                               {&AirPurifierType, []Option{withOptional(0x0003)}},
		"a host Groups server":                   {&AirPurifierType, []Option{withOptional(groupsID), withServer(&hostServer{id: groupsID})}},
		"initial values for Groups":              {&AirPurifierType, []Option{withOptional(groupsID), withInitial(groupsID, map[uint32]any{0: uint8(0)})}},
	} {
		if _, err := New(c.ty, testOptions("r"), c.with...); !errors.Is(err, ErrConfiguration) {
			t.Errorf("%s: %v, want ErrConfiguration", name, err)
		}
	}
}

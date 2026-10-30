// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	thermostatdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/cluster/thermo"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/surface"
)

// thermostatAheadRows are the Thermostat rows of the "Ahead of matter.js"
// register and the feature each declares (thermo.Ahead).
var thermostatAheadRows = []struct {
	row     string
	feature thermostatdef.Feature
}{
	{"BD-chip-Thermostat-Schedules", thermostatdef.FeatureMatterScheduleConfiguration},
	{"BD-chip-Thermostat-Events", thermostatdef.FeatureEvents},
}

// aheadThermostat builds a heating Thermostat with features. With
// MatterScheduleConfiguration the declaration carries the one schedule
// type the server needs to start (thermo's schedule configuration rules).
func aheadThermostat(key string, features thermostatdef.Feature, p *surface.Policies) (*Thermostat, error) {
	decl := thermostatdef.Declare{Features: thermostatdef.FeatureHeating | features}
	if features&thermostatdef.FeatureMatterScheduleConfiguration != 0 {
		types := []thermostatdef.ScheduleTypeStruct{{SystemMode: thermostatdef.SystemModeHeat, NumberOfSchedules: 1}}
		one, transitions := uint8(1), uint8(4)
		decl.Initial = thermostatdef.Patch{ScheduleTypes: &types, NumberOfSchedules: &one, NumberOfScheduleTransitions: &transitions}
	}
	return NewThermostat(Options{StableKey: endpoint.StringKey(key), Name: key, Surface: p}, decl)
}

func TestAheadDeclarationsAreThermo(t *testing.T) {
	if !reflect.DeepEqual(aheadByCluster[thermostatdef.ClusterID], thermo.Ahead) {
		t.Fatal("the device layer's Thermostat declaration is not thermo.Ahead")
	}
	for _, r := range thermostatAheadRows {
		if got := thermo.Ahead.Row(surface.KindFeature, uint32(r.feature)); got != r.row {
			t.Errorf("thermo.Ahead names feature 0x%X as %q, want %q", uint32(r.feature), got, r.row)
		}
	}
	if !slices.Equal(AheadRows(), thermo.Ahead.IDs()) {
		t.Errorf("AheadRows() = %v, want %v", AheadRows(), thermo.Ahead.IDs())
	}
}

func TestAheadThermostatDisabled(t *testing.T) {
	for _, r := range thermostatAheadRows {
		_, err := aheadThermostat("refused", r.feature, nil)
		if !errors.Is(err, surface.ErrExtensionNotEnabled) || !errors.Is(err, surface.ErrRefused) {
			t.Errorf("%s: err = %v, want ErrExtensionNotEnabled", r.row, err)
			continue
		}
		if !strings.Contains(err.Error(), r.row) {
			t.Errorf("%s: error %q does not name the row", r.row, err)
		}
		// The other row enabled does not enable this one.
		other := surface.Policies{Extensions: surface.NewExtensions(otherRow(r.row))}
		if _, err := aheadThermostat("refused", r.feature, &other); !errors.Is(err, surface.ErrExtensionNotEnabled) {
			t.Errorf("%s: with only %s enabled, err = %v", r.row, otherRow(r.row), err)
		}
	}
}

func otherRow(row string) string {
	for _, r := range thermostatAheadRows {
		if r.row != row {
			return r.row
		}
	}
	return ""
}

// TestAheadThermostatNotSelected: a host that never selects an ahead
// feature builds under the zero-value policies, its decisions name no
// register row, and its surface is the one it has with every row enabled.
func TestAheadThermostatNotSelected(t *testing.T) {
	plain, err := aheadThermostat("plain", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	all := surface.Policies{Extensions: surface.NewExtensions(thermo.Ahead.IDs()...)}
	enabled, err := aheadThermostat("plain", 0, &all)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range plain.Surface().Decisions {
		if d.Reference.Ahead != "" {
			t.Errorf("decision %+v names a row", d)
		}
	}
	a, b := plain.Surface().Effective, enabled.Surface().Effective
	if diff := surface.Diff(a, b); len(diff) != 0 {
		t.Errorf("enabling rows the host does not select changes the surface: %v", diff)
	}
}

// TestAheadThermostatEnabled: with the row enabled the device builds, its
// FeatureMap carries the bit, the decisions tag the feature with the row,
// the reported cluster is what the wire serves, and the five lists differ
// from the device without the feature — the elements the row adds.
func TestAheadThermostatEnabled(t *testing.T) {
	plain, err := aheadThermostat("plain", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range thermostatAheadRows {
		p := surface.Policies{Extensions: surface.NewExtensions(r.row)}
		d, err := aheadThermostat("ahead", r.feature, &p)
		if err != nil {
			t.Fatalf("%s: %v", r.row, err)
		}
		rep := d.Surface()
		c := clusterOf(rep, thermostatdef.ClusterID)
		if c.FeatureMap&uint32(r.feature) == 0 {
			t.Errorf("%s: FeatureMap 0x%X lacks 0x%X", r.row, c.FeatureMap, uint32(r.feature))
		}
		tagged := false
		for _, dec := range rep.Decisions {
			if dec.Kind == surface.KindFeature && dec.Cluster == thermostatdef.ClusterID && dec.ID == uint32(r.feature) {
				tagged = dec.Reference.Ahead == r.row && dec.Verdict == surface.Keep
			}
		}
		if !tagged {
			t.Errorf("%s: no kept feature decision tagged with the row", r.row)
		}
		topo, err := assembleAll([]*Device{d.Device})
		if err != nil {
			t.Fatal(err)
		}
		ep := topo.Bridged()[0]
		if wire := wireRead(t, topo, ep.ID)[thermostatdef.ClusterID]; !reflect.DeepEqual(c, wire) {
			t.Errorf("%s:\nsurface %+v\n   wire %+v", r.row, c, wire)
		}
		base := clusterOf(plain.Surface(), thermostatdef.ClusterID)
		if reflect.DeepEqual(c.Attributes, base.Attributes) && reflect.DeepEqual(c.Accepted, base.Accepted) &&
			reflect.DeepEqual(c.Generated, base.Generated) && reflect.DeepEqual(c.Events, base.Events) {
			t.Errorf("%s: the feature adds no element to the lists", r.row)
		}
	}
}

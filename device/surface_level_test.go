// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"errors"
	"slices"
	"strings"
	"testing"

	thermostatdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/surface"
)

// TestLevelRemovesAnOptionalFeature: Thermostat Events (TEVT, bit 9) is in
// the 1.6.0 and 1.6.1 models only (parity/levels, measured). Enabled as
// an extension and selected, it is served at the pin and removed at
// 1.5.1, the decision naming the level; the candidates carry their levels.
func TestLevelRemovesAnOptionalFeature(t *testing.T) {
	tevt := uint32(thermostatdef.FeatureEvents)
	ext := surface.NewExtensions("BD-chip-Thermostat-Events")
	pin := surface.Policies{Extensions: ext}
	low := surface.Policies{Extensions: ext, Level: "1.5.1"}
	a, err := aheadThermostat("t", thermostatdef.FeatureEvents, &pin)
	if err != nil {
		t.Fatal(err)
	}
	b, err := aheadThermostat("t", thermostatdef.FeatureEvents, &low)
	if err != nil {
		t.Fatal(err)
	}
	ra, rb := a.Surface(), b.Surface()
	if fm := wireCluster(t, ra, thermostatdef.ClusterID).FeatureMap; fm&tevt == 0 {
		t.Fatalf("pin FeatureMap 0x%X lacks TEVT", fm)
	}
	cb := wireCluster(t, rb, thermostatdef.ClusterID)
	if cb.FeatureMap&tevt != 0 || len(cb.Events) != 0 {
		t.Fatalf("1.5.1 FeatureMap 0x%X, events %v", cb.FeatureMap, cb.Events)
	}
	var found bool
	for _, d := range rb.Decisions {
		if d.Kind == surface.KindFeature && d.ID == tevt && d.Policy == "level" {
			found = d.Verdict == surface.Remove && d.Reason == "absent at level 1.5.1" && d.Levels != 0
		}
	}
	if !found {
		t.Error("no level removal of TEVT")
	}
	if rb.Level != "1.5.1" || ra.Level != surface.PinLevel() {
		t.Errorf("levels %q, %q", ra.Level, rb.Level)
	}
	for _, d := range ra.Decisions {
		if d.Policy == "level" {
			t.Errorf("a level decision at the pin: %+v", d)
		}
	}
}

// wireCluster is the device's effective cluster id.
func wireCluster(t *testing.T, r surface.Report, id uint32) surface.Cluster {
	t.Helper()
	for _, c := range r.Effective.Endpoints[0].Clusters {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("cluster 0x%04X not served", id)
	return surface.Cluster{}
}

// TestLevelWithholdsAnUnknownType: the Closure device type (0x0230) is
// not in the 1.4.2 model (parity/levels, measured): New returns the
// device with a WithheldError, the typed constructor too.
func TestLevelWithholdsAnUnknownType(t *testing.T) {
	p := surface.Policies{Level: "1.4.2"}
	d, err := NewClosure(Options{StableKey: endpoint.StringKey("c"), Name: "c", Surface: &p})
	var w *surface.WithheldError
	if !errors.As(err, &w) || d == nil || !strings.Contains(err.Error(), "Closure") {
		t.Fatalf("NewClosure: %v, %v", d, err)
	}
	if !slices.ContainsFunc(w.Elements, func(e surface.Element) bool { return e.Kind == surface.KindDeviceType && e.ID == uint32(ClosureID) }) {
		t.Errorf("withheld %v", w.Elements)
	}
	if rep := d.Surface(); len(rep.Withheld) != len(w.Elements) || len(rep.Effective.Endpoints) != 0 || d.Server(0x0104) != nil {
		t.Errorf("withheld device %+v", rep)
	}
	if _, err := NewClosure(Options{StableKey: endpoint.StringKey("c"), Name: "c", Surface: &surface.Policies{Level: "1.5"}}); !errors.Is(err, surface.ErrUnknownLevel) {
		t.Errorf("unknown level: %v", err)
	}
}

// TestLevelServerInternal: a decision for every effective element the
// level does not know that no candidate decided.
func TestLevelServerInternal(t *testing.T) {
	p := surface.Policies{Level: "1.4"}
	d, err := aheadThermostat("t", 0, &p)
	if err != nil {
		t.Fatal(err)
	}
	rep := d.Surface()
	for _, x := range rep.Decisions {
		if x.Reason != surface.ReasonServerInternal {
			continue
		}
		if x.Policy != "level" || x.Levels.Has("1.4") || x.Endpoint != "t" {
			t.Errorf("server-internal %+v", x)
		}
	}
}

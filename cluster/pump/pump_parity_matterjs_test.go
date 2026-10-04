// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package pump_test

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/pump"
	"github.com/SukramJ/go-fabric/contract"
	matterparity "github.com/SukramJ/go-fabric/parity"
	"github.com/SukramJ/go-fabric/schema"
)

// The PumpConfigurationAndControl entry of parity/schema.json, the
// matter.js HEAD pin.
type (
	snapElement struct {
		ID          uint32 `json:"id"`
		Name        string `json:"name"`
		Type        string `json:"type"`
		Conformance string `json:"conformance"`
		Access      string `json:"access"`
		Constraint  string `json:"constraint"`
		Quality     string `json:"quality"`
		Priority    string `json:"priority"`
	}
	snapFeature struct {
		Name        string `json:"name"`
		Conformance string `json:"conformance"`
		Bit         uint32 `json:"bit"`
	}
	snapCluster struct {
		ID         uint32        `json:"id"`
		Revision   uint16        `json:"revision"`
		Attributes []snapElement `json:"attributes"`
		Commands   []snapElement `json:"commands"`
		Events     []snapElement `json:"events"`
		Features   []snapFeature `json:"features"`
	}
)

func pumpSnapshot(t *testing.T) snapCluster {
	t.Helper()
	var s struct {
		Clusters []snapCluster `json:"clusters"`
	}
	if err := json.Unmarshal(matterparity.SchemaJSON(), &s); err != nil {
		t.Fatalf("unmarshal schema snapshot: %v", err)
	}
	for _, c := range s.Clusters {
		if c.ID == pump.ClusterID {
			return c
		}
	}
	t.Fatalf("matter.js schema has no PumpConfigurationAndControl (0x%04X)", pump.ClusterID)
	return snapCluster{}
}

// conformance decides whether a matter.js conformance expression makes
// an element mandatory or permitted for the given feature names: "M",
// "O", "D" (deprecated — not served), a feature name, "[feature]".
// Comma-separated terms are alternatives tried in order, as matter.js
// Conformance evaluates "PRSCONST, [AUTO]".
func conformance(expr string, features map[string]bool) (required, allowed bool) {
	for term := range strings.SplitSeq(expr, ",") {
		term = strings.TrimSpace(term)
		optional := strings.HasPrefix(term, "[") && strings.HasSuffix(term, "]")
		if optional {
			term = strings.TrimSpace(term[1 : len(term)-1])
		}
		switch {
		case term == "M":
			return !optional, true
		case term == "O":
			return false, true
		case features[term]:
			return !optional, true
		}
	}
	return false, false
}

var featureByName = map[string]pump.Feature{
	"PRSCONST": pump.FeatureConstantPressure, "PRSCOMP": pump.FeatureCompensatedPressure,
	"FLW": pump.FeatureConstantFlow, "SPD": pump.FeatureConstantSpeed, "TEMP": pump.FeatureConstantTemperature,
	"AUTO": pump.FeatureAutomatic, "LOCAL": pump.FeatureLocalOperation,
}

func TestParityMatterJS_PumpRevisionIDsAndAccess(t *testing.T) {
	t.Parallel()
	js := pumpSnapshot(t)
	if pump.Revision() != js.Revision {
		t.Errorf("Revision = %d, want %d", pump.Revision(), js.Revision)
	}
	attrs := map[string]uint32{
		"MaxPressure": pump.AttrMaxPressure, "MaxSpeed": pump.AttrMaxSpeed, "MaxFlow": pump.AttrMaxFlow,
		"MinConstPressure": pump.AttrMinConstPressure, "MaxConstPressure": pump.AttrMaxConstPressure,
		"MinCompPressure": pump.AttrMinCompPressure, "MaxCompPressure": pump.AttrMaxCompPressure,
		"MinConstSpeed": pump.AttrMinConstSpeed, "MaxConstSpeed": pump.AttrMaxConstSpeed,
		"MinConstFlow": pump.AttrMinConstFlow, "MaxConstFlow": pump.AttrMaxConstFlow,
		"MinConstTemp": pump.AttrMinConstTemp, "MaxConstTemp": pump.AttrMaxConstTemp,
		"PumpStatus": pump.AttrPumpStatus, "EffectiveOperationMode": pump.AttrEffectiveOperationMode,
		"EffectiveControlMode": pump.AttrEffectiveControlMode, "Capacity": pump.AttrCapacity, "Speed": pump.AttrSpeed,
		"LifetimeRunningHours": pump.AttrLifetimeRunningHours, "Power": pump.AttrPower,
		"LifetimeEnergyConsumed": pump.AttrLifetimeEnergyConsumed, "OperationMode": pump.AttrOperationMode,
		"ControlMode": pump.AttrControlMode,
	}
	writable := []uint32{pump.AttrLifetimeRunningHours, pump.AttrLifetimeEnergyConsumed, pump.AttrOperationMode, pump.AttrControlMode}
	srv := newFull(t, &device{})
	for _, a := range js.Attributes {
		if a.ID >= 0xFFF0 || a.Name == "AlarmMask" {
			continue // globals; AlarmMask is deprecated and not served
		}
		if id, ok := attrs[a.Name]; !ok || id != a.ID {
			t.Errorf("attribute %s (0x%04X) has constant 0x%04X (known %v)", a.Name, a.ID, id, ok)
		}
		if strings.HasPrefix(a.Access, "RW") != slices.Contains(writable, a.ID) {
			t.Errorf("%s access %q disagrees with the server's writable set", a.Name, a.Access)
		}
		if strings.HasPrefix(a.Access, "RW") && (!strings.Contains(a.Access, "VM") || srv.MinWritePrivilege(a.ID) != 4) {
			t.Errorf("%s access %q: the server asks for Manage", a.Name, a.Access)
		}
	}
	if len(js.Commands) != 0 {
		t.Errorf("matter.js has %d commands; the server serves none", len(js.Commands))
	}
	for _, f := range js.Features {
		if want, ok := featureByName[f.Name]; !ok || uint32(want) != 1<<f.Bit {
			t.Errorf("feature %+v has no matching constant", f)
		}
	}
}

// TestParityMatterJS_PumpWireTypes checks every served attribute reads as
// the Go type the value writer encodes at the width matter.js declares.
func TestParityMatterJS_PumpWireTypes(t *testing.T) {
	t.Parallel()
	js := pumpSnapshot(t)
	d := &device{}
	d.st.Capacity, d.st.Speed, d.st.LifetimeRunningHours, d.st.Power, d.st.LifetimeEnergyConsumed = i16(1), u16(1), u32(1), u32(1), u32(1)
	srv, err := pump.NewServer(pump.Config{
		Source: d, Features: pump.FeatureConstantFlow | pump.FeatureAutomatic, Optional: everyOptional | pump.OptionalAutomaticLimits,
		Limits: pump.Limits{
			MaxPressure: i16(1), MaxSpeed: u16(1), MaxFlow: u16(1), MinConstPressure: i16(1), MaxConstPressure: i16(1),
			MinCompPressure: i16(1), MaxCompPressure: i16(1), MinConstSpeed: u16(1), MaxConstSpeed: u16(1),
			MinConstFlow: u16(1), MaxConstFlow: u16(1), MinConstTemp: i16(1), MaxConstTemp: i16(1),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	goType := map[string]string{
		"int16": "int16", "uint16": "uint16", "uint24": "uint32", "uint32": "uint32",
		"OperationModeEnum": "uint8", "ControlModeEnum": "uint8", "PumpStatusBitmap": "uint16",
	}
	for _, a := range js.Attributes {
		if !slices.Contains(srv.MatterAttributes(), a.ID) {
			continue
		}
		v, ok := srv.MatterRead(a.ID)
		if !ok {
			t.Errorf("%s not readable", a.Name)
			continue
		}
		if got := fmt.Sprintf("%T", v); got != goType[a.Type] {
			t.Errorf("%s (%s) reads as %s, want %s", a.Name, a.Type, got, goType[a.Type])
		}
	}
}

// TestParityMatterJS_PumpConformance builds feature combinations and
// checks the attribute list against each element's conformance,
// including the "<feature>, [AUTO]" limit pairs.
func TestParityMatterJS_PumpConformance(t *testing.T) {
	t.Parallel()
	js := pumpSnapshot(t)
	built := 0
	for f := range pump.Feature(1 << 7) {
		for _, opt := range []pump.Optional{0, everyOptional, everyOptional | pump.OptionalAutomaticLimits} {
			srv, err := pump.NewServer(pump.Config{Source: &device{}, Features: f, Optional: opt})
			if err != nil {
				continue
			}
			built++
			names := map[string]bool{}
			for n, bit := range featureByName {
				names[n] = f&bit != 0
			}
			for _, a := range js.Attributes {
				if a.ID >= 0xFFF0 {
					continue
				}
				required, allowed := conformance(a.Conformance, names)
				has := slices.Contains(srv.MatterAttributes(), a.ID)
				if required && !has {
					t.Errorf("features 0x%02X opt 0x%X: mandatory %s (%q) missing", f, opt, a.Name, a.Conformance)
				}
				if has && !allowed {
					t.Errorf("features 0x%02X opt 0x%X: %s (%q) served but not allowed", f, opt, a.Name, a.Conformance)
				}
			}
			if fm, _ := srv.MatterRead(cluster.AttrGlobalFeatureMap); fm != uint32(f) {
				t.Errorf("FeatureMap = %v, want 0x%02X", fm, f)
			}
		}
	}
	if built == 0 {
		t.Fatal("no configuration built")
	}
	// "O.a+": no control feature at all is refused.
	if _, err := pump.NewServer(pump.Config{Source: &device{}, Features: pump.FeatureAutomatic | pump.FeatureLocalOperation}); err == nil {
		t.Error("a pump without any control feature was accepted")
	}
}

// TestParityMatterJS_PumpEventsAndPriorities declares every event and
// checks EventList and each emitted priority against matter.js.
func TestParityMatterJS_PumpEventsAndPriorities(t *testing.T) {
	t.Parallel()
	js := pumpSnapshot(t)
	all := make([]uint32, 0, len(js.Events))
	want := map[uint32]contract.EventPriority{}
	for _, e := range js.Events {
		if e.Conformance != "O" {
			t.Errorf("event %s conformance %q; the server treats every event as optional", e.Name, e.Conformance)
		}
		all = append(all, e.ID)
		want[e.ID] = contract.EventPriorityInfo
		if e.Priority == "critical" {
			want[e.ID] = contract.EventPriorityCritical
		}
	}
	srv, err := pump.NewServer(pump.Config{Source: &device{}, Features: pump.FeatureConstantSpeed, Events: all})
	if err != nil {
		t.Fatal(err)
	}
	if got := srv.MatterEvents(); !slices.Equal(got, all) {
		t.Errorf("EventList = %v, want %v", got, all)
	}
	rec := &recorder{}
	srv.SetMatterEventEmitter(rec)
	for _, id := range all {
		if err := srv.Emit(id); err != nil {
			t.Fatalf("Emit(0x%02X): %v", id, err)
		}
	}
	for _, e := range rec.events {
		if e.priority != want[e.event] {
			t.Errorf("event 0x%02X priority %d, want %d", e.event, e.priority, want[e.event])
		}
	}
}

// TestParityMatterJS_PumpDeviceType pins Pump (0x0303): it mandates this
// cluster and OnOff.
func TestParityMatterJS_PumpDeviceType(t *testing.T) {
	t.Parallel()
	if name, _ := schema.DeviceTypeName(uint32(pump.DeviceTypePump)); name != "Pump" {
		t.Errorf("device type 0x%04X is %q", pump.DeviceTypePump, name)
	}
	for _, c := range []uint32{pump.ClusterID, 0x0006} {
		if !schema.DeviceTypeRequiresServerCluster(uint32(pump.DeviceTypePump), c) {
			t.Errorf("Pump does not mandate 0x%04X in the snapshot", c)
		}
	}
}

// TestParityMatterJS_PumpEnumValues pins the enum and bitmap values
// against pump-configuration-and-control.element.ts:130-159.
func TestParityMatterJS_PumpEnumValues(t *testing.T) {
	t.Parallel()
	got := []uint16{
		uint16(pump.OperationNormal), uint16(pump.OperationMinimum), uint16(pump.OperationMaximum), uint16(pump.OperationLocal),
		uint16(pump.ControlConstantSpeed), uint16(pump.ControlConstantPressure), uint16(pump.ControlProportionalPressure),
		uint16(pump.ControlConstantFlow), uint16(pump.ControlConstantTemperature), uint16(pump.ControlAutomatic),
		uint16(pump.StatusDeviceFault), uint16(pump.StatusSupplyFault), uint16(pump.StatusSpeedLow), uint16(pump.StatusSpeedHigh),
		uint16(pump.StatusLocalOverride), uint16(pump.StatusRunning), uint16(pump.StatusRemotePressure),
		uint16(pump.StatusRemoteFlow), uint16(pump.StatusRemoteTemperature),
	}
	want := []uint16{0, 1, 2, 3, 0, 1, 2, 3, 5, 7, 1, 2, 4, 8, 16, 32, 64, 128, 256}
	if !slices.Equal(got, want) {
		t.Errorf("enum values %v, want %v", got, want)
	}
}

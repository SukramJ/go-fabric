// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js
// packages/node/test/behavior/state/managed/values/ValuePatcherTest.ts at
// 862ad75f: the defaults storage holds for a struct field the host leaves
// out, decided per feature set (ValuePatcher.ts, SelectDefaultValue.ts
// StoredDefaultValue).
//
// matter.js patches a plain object over the defaults; the Go value is a
// struct, so a field the host can leave out is a pointer (nil) or a
// slice — the form the generator gives every field whose conformance is
// not a plain "M". The test's Widgets cluster (ValuePatcherTest.ts:17-72)
// is a synthetic definition here, its WidgetStruct a Go struct whose
// fields are all omissible, Flags a named bitmap type. The patch is
// spec.Instance.PatchDefaults over an instance of the feature selection
// (patchWidgets, :74-85); the Thermostat cases run against the committed
// Thermostat definition with Presets and the feature named (patchPreset,
// :91-101).
package spec_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	tstatdef "github.com/SukramJ/go-fabric/cluster/spec/thermostat"
)

// widgetFlags is WidgetStruct's map8 Flags (ValuePatcherTest.ts:51-58).
type widgetFlags uint8

// widget is WidgetStruct (ValuePatcherTest.ts:43-68).
type widget struct {
	Id       *uint16 //nolint:revive // the field name the definition states ("Id")
	Extra    *uint16
	Required *uint16
	Flags    *widgetFlags
	Tags     []uint16
}

// strayWidget is a WidgetStruct value with a key the schema does not
// declare.
type strayWidget struct {
	Id   *uint16 //nolint:revive // the field name the definition states ("Id")
	Nope *uint16
}

const featureEXT = 1 << 0

var widgetStruct = &spec.Struct{Name: "WidgetStruct", Fields: []spec.Field{
	{ID: 0, Name: "Id", Type: spec.Type{Kind: spec.KindUint, Bits: 16}, Conformance: op(spec.ConfMandatory)},
	{ID: 1, Name: "Extra", Type: spec.Type{Kind: spec.KindUint, Bits: 16}, Conformance: op(spec.ConfOptionalIf, name("EXT")), Default: float64(4)},
	{ID: 2, Name: "Required", Type: spec.Type{Kind: spec.KindUint, Bits: 16}, Conformance: name("EXT"), Default: float64(7)},
	{ID: 4, Name: "Flags", Type: spec.Type{Kind: spec.KindBitmap, Bits: 8, Bitmap: &spec.Bitmap{Name: "Flags", Bits: 8, Members: []spec.BitmapMember{{Name: "Marked", Bit: 0, Width: 1}}}}, Conformance: op(spec.ConfMandatory), Default: float64(1)},
	{ID: 3, Name: "Tags", Type: spec.Type{Kind: spec.KindList, Entry: &spec.Field{Name: "entry", Type: spec.Type{Kind: spec.KindUint, Bits: 16}}}, Conformance: op(spec.ConfMandatory), Default: spec.EmptyListDefault{}},
}}

var widgetsCluster = &spec.Cluster{
	ID: 0xfff1fc01, Name: "Widgets", Revision: 1,
	Features: []spec.Feature{{Name: "EXT", Title: "Extras", Bit: 0, Conformance: op(spec.ConfOptional)}},
	Attributes: []spec.Attribute{{
		ID: 0, Name: "Widgets", Conformance: op(spec.ConfMandatory),
		Type: spec.Type{Kind: spec.KindList, Entry: &spec.Field{Name: "entry", Type: spec.Type{Name: "WidgetStruct", Kind: spec.KindStruct, Struct: widgetStruct}}},
	}},
}

func patchWidgets(t *testing.T, features uint32, widgets ...any) []any {
	t.Helper()
	inst, err := spec.New(widgetsCluster, spec.Options{Features: features})
	if err != nil {
		t.Fatal(err)
	}
	out, err := inst.PatchDefaults(0, widgets)
	if err != nil {
		t.Fatal(err)
	}
	return out.([]any)
}

func patchWidget(t *testing.T, features uint32, w widget) widget {
	t.Helper()
	return patchWidgets(t, features, w)[0].(widget)
}

func patchPreset(t *testing.T, features tstatdef.Feature) tstatdef.PresetStruct {
	t.Helper()
	inst, err := spec.New(tstatdef.Definition, spec.Options{Features: uint32(tstatdef.FeaturePresets | features)})
	if err != nil {
		t.Fatal(err)
	}
	presets := []tstatdef.PresetStruct{{PresetHandle: spec.NullOf[[]byte](), PresetScenario: tstatdef.PresetScenarioOccupied}}
	out, err := inst.PatchDefaults(tstatdef.AttrPresets, presets)
	if err != nil {
		t.Fatal(err)
	}
	return out.([]tstatdef.PresetStruct)[0]
}

func TestValuePatcherParityMatterJS(t *testing.T) {
	one := new(uint16(1))

	t.Run("omits the fallback of a field the active features disallow", func(t *testing.T) {
		got := patchWidget(t, 0, widget{Id: one})
		if got.Extra != nil || got.Required != nil {
			t.Fatalf("Extra %v, Required %v under no features; want both absent", got.Extra, got.Required)
		}
		if got.Flags == nil || *got.Flags != 1 || got.Tags == nil || len(got.Tags) != 0 || *got.Id != 1 {
			t.Fatalf("got %+v, want {Id: 1, Tags: [], Flags: Marked}", got)
		}
	})

	t.Run("omits the fallback of a field the active features leave optional", func(t *testing.T) {
		if got := patchWidget(t, featureEXT, widget{Id: one}); got.Extra != nil {
			t.Fatalf("Extra %d under EXT ([EXT]); want absent", *got.Extra)
		}
	})

	t.Run("applies the fallback of a field the active features make mandatory", func(t *testing.T) {
		if got := patchWidget(t, featureEXT, widget{Id: one}); got.Required == nil || *got.Required != 7 {
			t.Fatalf("Required %v under EXT; want 7", got.Required)
		}
		// Where the value enters a server: Initial is patched before it is
		// checked and stored.
		srv, err := spec.NewServer(widgetsCluster, spec.Options{Features: featureEXT}, spec.ServerConfig{
			Initial: map[uint32]any{0: []widget{{Id: one}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		v, _ := srv.Value(0)
		if ws := v.([]widget); *ws[0].Required != 7 {
			t.Fatalf("the stored Initial carries Required %v; want 7", ws[0].Required)
		}
	})

	t.Run("gives each struct its own copy of a container default", func(t *testing.T) {
		in := []any{widget{Id: one}, widget{Id: new(uint16(2))}}
		got := patchWidgets(t, 0, in...)
		a, b := got[0].(widget), got[1].(widget)
		if a.Tags == nil || b.Tags == nil {
			t.Fatal("the Tags default is not applied")
		}
		a.Tags = append(a.Tags, 5)
		if len(b.Tags) != 0 {
			t.Fatal("the two structs share their Tags")
		}
		if in[0].(widget).Tags != nil {
			t.Fatal("the value handed over was changed")
		}
	})

	t.Run("applies a bitmap fallback decoded", func(t *testing.T) {
		// Decoded is the Go form of the bitmap: its generated type, the
		// Marked bit set.
		if got := patchWidget(t, 0, widget{Id: one}); got.Flags == nil || *got.Flags != widgetFlags(1) {
			t.Fatalf("Flags %v; want Marked (1)", got.Flags)
		}
	})

	t.Run("refuses a key the schema does not declare, including a property of Object.prototype", func(t *testing.T) {
		// A Go struct has no prototype, so toString and __proto__ have no Go
		// form; a field the definition does not declare is the key.
		inst, err := spec.New(widgetsCluster, spec.Options{})
		if err != nil {
			t.Fatal(err)
		}
		_, err = inst.PatchDefaults(0, []any{strayWidget{Id: one}})
		if err == nil || !strings.Contains(err.Error(), "Nope is not a property of WidgetStruct") {
			t.Fatalf("PatchDefaults = %v, want Nope is not a property of WidgetStruct", err)
		}
		// A Set refuses it as a value outside the attribute's type.
		srv, err := spec.NewServer(widgetsCluster, spec.Options{}, spec.ServerConfig{})
		if err != nil {
			t.Fatal(err)
		}
		if err := srv.Set(0, []strayWidget{{Id: one}}); !errors.Is(err, spec.ErrInvalidValue) {
			t.Fatalf("Set = %v, want ErrInvalidValue", err)
		}
	})

	t.Run("applies a fallback in the units the datatype encodes", func(t *testing.T) {
		// The schema states 26°C; state carries the wire units of the
		// temperature datatype.
		if got := patchPreset(t, tstatdef.FeatureCooling); got.CoolingSetpoint == nil || *got.CoolingSetpoint != 2600 {
			t.Fatalf("CoolingSetpoint %v; want 2600", got.CoolingSetpoint)
		}
	})

	t.Run("applies defaults per feature set rather than per schema", func(t *testing.T) {
		if got := patchPreset(t, tstatdef.FeatureCooling); got.CoolingSetpoint == nil || *got.CoolingSetpoint != 2600 {
			t.Fatalf("CoolingSetpoint %v under COOL; want 2600", got.CoolingSetpoint)
		}
		if got := patchPreset(t, tstatdef.FeatureHeating); got.CoolingSetpoint != nil {
			t.Fatalf("CoolingSetpoint %d under HEAT; want absent", *got.CoolingSetpoint)
		}
	})
}

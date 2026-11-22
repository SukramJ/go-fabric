// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	bscdef "thingfabric.dev/go-fabric/cluster/spec/booleanstateconfiguration"
	hepa "thingfabric.dev/go-fabric/cluster/spec/hepafiltermonitoring"
	locdef "thingfabric.dev/go-fabric/cluster/spec/localizationconfiguration"
	tfldef "thingfabric.dev/go-fabric/cluster/spec/timeformatlocalization"
	"thingfabric.dev/go-fabric/im"
)

// unmappedForm is a hand-written form whose declaration leaves a field of
// its definition struct out.
type unmappedForm struct{ A uint8 }

// TestVerifyFieldMapsNamesAMissingField: a MapFields declaration that
// leaves a definition field without a Go field fails VerifyFieldMaps,
// naming the field.
func TestVerifyFieldMapsNamesAMissingField(t *testing.T) {
	t.Parallel()
	s := &spec.Struct{Name: "Form", Fields: []spec.Field{
		{ID: 0, Name: "A", Type: spec.Type{Name: "uint8", Kind: spec.KindUint, Bits: 8}},
		{ID: 1, Name: "B", Type: spec.Type{Name: "uint8", Kind: spec.KindUint, Bits: 8}},
	}}
	spec.MapFields[unmappedForm](s, spec.FieldMap{"A": "A"})
	err := spec.VerifyFieldMaps()
	if err == nil || !strings.Contains(err.Error(), "Form.B") {
		t.Fatalf("VerifyFieldMaps = %v, want it to name Form.B", err)
	}
}

// TestCheckWrittenHoldsTheValueToTheDefinition: CheckWritten judges a
// controller's written value by the attribute's definition before any
// server runs — a bound naming a sibling attribute, an "in" constraint,
// an enum's defined values, a null on a nullable attribute — and answers a
// refusal with CONSTRAINT_ERROR; an attribute the definition does not
// define and a raw Node are not judged.
func TestCheckWrittenHoldsTheValueToTheDefinition(t *testing.T) {
	t.Parallel()
	peers := spec.Peers(func(attrID uint32) (any, bool) {
		if attrID == bscdef.AttrSupportedSensitivityLevels {
			return uint8(3), true
		}
		return nil, false
	})
	locales := spec.Peers(func(attrID uint32) (any, bool) {
		if attrID == locdef.AttrSupportedLocales {
			return []string{"en-US", "de-DE"}, true
		}
		return nil, false
	})
	for _, c := range []struct {
		name   string
		def    *spec.Cluster
		attrID uint32
		value  any
		peers  spec.Peers
		refuse bool
	}{
		{"a level below SupportedSensitivityLevels", bscdef.Definition, bscdef.AttrCurrentSensitivityLevel, uint64(2), peers, false},
		{"a level at SupportedSensitivityLevels", bscdef.Definition, bscdef.AttrCurrentSensitivityLevel, uint64(3), peers, true},
		{"a supported locale", locdef.Definition, locdef.AttrActiveLocale, "de-DE", locales, false},
		{"a locale outside SupportedLocales", locdef.Definition, locdef.AttrActiveLocale, "fr-FR", locales, true},
		{"a defined HourFormat", tfldef.Definition, tfldef.AttrHourFormat, uint64(tfldef.HourFormatUseActiveLocale), nil, false},
		{"an HourFormat the enum does not define", tfldef.Definition, tfldef.AttrHourFormat, uint64(9), nil, true},
		{"null on the nullable LastChangedTime", hepa.Definition, hepa.AttrLastChangedTime, nil, nil, false},
		{"an attribute the definition does not define", hepa.Definition, 0x7777, "anything", nil, false},
		{"a raw Node", tfldef.Definition, tfldef.AttrHourFormat, spec.Node{}, nil, false},
	} {
		err := spec.CheckWritten(c.def, 0, c.attrID, c.value, c.peers)
		switch {
		case c.refuse && spec.StatusOf(err) != im.StatusConstraintError:
			t.Errorf("%s: %v, want CONSTRAINT_ERROR", c.name, err)
		case !c.refuse && err != nil:
			t.Errorf("%s: %v, want no refusal", c.name, err)
		}
	}
}

// conformedRecord is a written value of the struct attribute "Record" of
// conformedCluster: the Go form a struct field conformance is judged in.
type conformedRecord struct {
	A *any
	B *any
}

// conformedCluster defines one writable struct attribute "Record" whose
// field A is mandatory and B optional.
func conformedCluster() *spec.Cluster {
	u8 := spec.Type{Name: "uint8", Kind: spec.KindUint, Bits: 8}
	rec := &spec.Struct{Name: "Rec", Fields: []spec.Field{
		{ID: 0, Name: "A", Type: u8, Conformance: spec.Conformance{Text: "M", Op: spec.ConfMandatory}},
		{ID: 1, Name: "B", Type: u8, Conformance: spec.Conformance{Text: "O", Op: spec.ConfOptional}},
	}}
	return &spec.Cluster{ID: 0xFFF1FC02, Name: "Conformed", Revision: 1, Attributes: []spec.Attribute{{
		ID: 0, Name: "Record", Type: spec.Type{Name: "Rec", Kind: spec.KindStruct, Struct: rec},
		Conformance: spec.Conformance{Text: "M", Op: spec.ConfMandatory},
		Access:      spec.Access{RW: "RW", Read: spec.PrivilegeView, Write: spec.PrivilegeOperate},
	}}}
}

// TestServerWriteHoldsAStructToItsFieldConformance: a written struct that
// leaves out a field its conformance makes mandatory is refused by
// Server.MatterWriteChanged with CONSTRAINT_ERROR (ErrConformance),
// unchanged, nothing stored and no version advanced — as MatterWrite
// refuses it; the struct with the field is stored and answers changed,
// and the same struct again answers unchanged.
func TestServerWriteHoldsAStructToItsFieldConformance(t *testing.T) {
	t.Parallel()
	srv, err := spec.NewServer(conformedCluster(), spec.Options{}, spec.ServerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	one := any(uint64(1))
	v0 := srv.MatterDataVersion()
	changed, err := srv.MatterWriteChanged(ctx, 0, conformedRecord{B: &one})
	if changed || !errors.Is(err, spec.ErrConformance) || spec.StatusOf(err) != im.StatusConstraintError {
		t.Errorf("a Record without A: changed %v, %v; want unchanged and CONSTRAINT_ERROR (ErrConformance)", changed, err)
	}
	if werr := srv.MatterWrite(ctx, 0, conformedRecord{B: &one}); !errors.Is(werr, spec.ErrConformance) {
		t.Errorf("MatterWrite of a Record without A: %v, want ErrConformance", werr)
	}
	if v, ok := srv.Value(0); ok || srv.MatterDataVersion() != v0 {
		t.Errorf("after the refused writes: stored %v (%v), version moved %v; want nothing stored", v, ok, srv.MatterDataVersion() != v0)
	}
	if changed, err := srv.MatterWriteChanged(ctx, 0, conformedRecord{A: &one}); !changed || err != nil {
		t.Errorf("a Record with A: changed %v, %v; want changed", changed, err)
	}
	if v, ok := srv.Value(0); !ok || !reflect.DeepEqual(v, conformedRecord{A: &one}) {
		t.Errorf("stored %#v, want the written Record", v)
	}
	again := any(uint64(1))
	if changed, err := srv.MatterWriteChanged(ctx, 0, conformedRecord{A: &again}); changed || err != nil {
		t.Errorf("the same Record again: changed %v, %v; want unchanged", changed, err)
	}
}

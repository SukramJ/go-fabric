// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"reflect"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/descriptor"
	"github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/cluster/spec/powersource"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	"github.com/SukramJ/go-fabric/tlv"
)

// encoded is the node of what put writes under an anonymous tag.
func encoded(t *testing.T, put func(e *tlv.Encoder, tag tlv.Tag)) spec.Node {
	t.Helper()
	e := tlv.NewEncoder()
	put(e, tlv.AnonymousTag())
	b, err := e.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return spectest.Decode(t, b)
}

func attrType(t *testing.T, def *spec.Cluster, id uint32) spec.Type {
	t.Helper()
	a := def.Attribute(id)
	if a == nil {
		t.Fatalf("%s has no attribute 0x%04X", def.Name, id)
	}
	return a.Type
}

// TestDecodeValueRoundTrip decodes, by each attribute's Type, what the
// generated encoders of OnOff, Descriptor and PowerSource write.
func TestDecodeValueRoundTrip(t *testing.T) {
	t.Parallel()
	dtl := spec.List[descriptor.DeviceTypeStruct]{{DeviceType: 0x0016, Revision: 3}, {DeviceType: 0x010A, Revision: 2}}
	cases := []struct {
		name string
		typ  spec.Type
		node spec.Node
		want any
	}{
		{"OnOff.OnOff", attrType(t, onoff.Definition, onoff.AttrOnOff), encoded(t, func(e *tlv.Encoder, tag tlv.Tag) { spec.PutBool(e, tag, true) }), true},
		{"OnOff.OnTime", attrType(t, onoff.Definition, onoff.AttrOnTime), encoded(t, func(e *tlv.Encoder, tag tlv.Tag) { spec.PutUint(e, tag, uint16(300)) }), uint64(300)},
		{"OnOff.StartUpOnOff null", attrType(t, onoff.Definition, onoff.AttrStartUpOnOff), encoded(t, func(e *tlv.Encoder, tag tlv.Tag) {
			spec.PutNullable(spec.PutUint[onoff.StartUpOnOffEnum])(e, tag, spec.NullOf[onoff.StartUpOnOffEnum]())
		}), nil},
		{"OnOff.StartUpOnOff", attrType(t, onoff.Definition, onoff.AttrStartUpOnOff), encoded(t, func(e *tlv.Encoder, tag tlv.Tag) {
			spec.PutNullable(spec.PutUint[onoff.StartUpOnOffEnum])(e, tag, spec.ValueOf(onoff.StartUpOnOffToggle))
		}), uint64(onoff.StartUpOnOffToggle)},
		{"Descriptor.DeviceTypeList", attrType(t, descriptor.Definition, descriptor.AttrDeviceTypeList), spectest.Decode(t, spectest.Encode(t, dtl)), []any{
			map[uint32]any{0: uint64(0x0016), 1: uint64(3)}, map[uint32]any{0: uint64(0x010A), 1: uint64(2)},
		}},
		{"Descriptor.ServerList", attrType(t, descriptor.Definition, descriptor.AttrServerList), encoded(t, func(e *tlv.Encoder, tag tlv.Tag) {
			spec.PutList(spec.PutUint[uint32])(e, tag, []uint32{6, 0x1D})
		}), []any{uint64(6), uint64(0x1D)}},
		{"PowerSource.Status", attrType(t, powersource.Definition, powersource.AttrStatus), encoded(t, func(e *tlv.Encoder, tag tlv.Tag) {
			spec.PutUint(e, tag, powersource.PowerSourceStatusActive)
		}), uint64(1)},
		{"PowerSource.Description", attrType(t, powersource.Definition, powersource.AttrDescription), encoded(t, func(e *tlv.Encoder, tag tlv.Tag) { spec.PutString(e, tag, "mains") }), "mains"},
		{"PowerSource.BatVoltage", attrType(t, powersource.Definition, powersource.AttrBatVoltage), encoded(t, func(e *tlv.Encoder, tag tlv.Tag) {
			spec.PutNullable(spec.PutUint[uint32])(e, tag, spec.ValueOf(uint32(3300)))
		}), uint64(3300)},
		{"PowerSource.ActiveWiredFaults", attrType(t, powersource.Definition, powersource.AttrActiveWiredFaults), encoded(t, func(e *tlv.Encoder, tag tlv.Tag) {
			spec.PutList(spec.PutUint[powersource.WiredFaultEnum])(e, tag, []powersource.WiredFaultEnum{1, 2})
		}), []any{uint64(1), uint64(2)}},
	}
	for _, tc := range cases {
		got, err := spec.DecodeValue(tc.typ, tc.node)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: %#v, want %#v", tc.name, got, tc.want)
		}
	}
}

// TestDecodeValueKinds: the kinds the three clusters do not cover, a
// struct member the type does not define, and the shape errors.
func TestDecodeValueKinds(t *testing.T) {
	t.Parallel()
	i16 := encoded(t, func(e *tlv.Encoder, tag tlv.Tag) { e.PutInt(tag, -5) })
	if v, err := spec.DecodeValue(spec.Type{Kind: spec.KindInt, Bits: 16}, i16); err != nil || v != int64(-5) {
		t.Fatalf("int: %v %v", v, err)
	}
	if v, err := spec.DecodeValue(spec.Type{Kind: spec.KindInt}, i16); err != nil || v != int64(-5) {
		t.Fatalf("int of no width: %v %v", v, err)
	}
	f32 := encoded(t, func(e *tlv.Encoder, tag tlv.Tag) { e.PutFloat32(tag, 1.5) })
	if v, err := spec.DecodeValue(spec.Type{Kind: spec.KindFloat32}, f32); err != nil || v != float32(1.5) {
		t.Fatalf("float32: %v %v", v, err)
	}
	f64 := encoded(t, func(e *tlv.Encoder, tag tlv.Tag) { e.PutFloat64(tag, 2.25) })
	if v, err := spec.DecodeValue(spec.Type{Kind: spec.KindFloat64}, f64); err != nil || v != 2.25 {
		t.Fatalf("float64: %v %v", v, err)
	}
	oct := encoded(t, func(e *tlv.Encoder, tag tlv.Tag) { e.PutOctets(tag, []byte{1, 2}) })
	if v, err := spec.DecodeValue(spec.Type{Kind: spec.KindBytes}, oct); err != nil || !reflect.DeepEqual(v, []byte{1, 2}) {
		t.Fatalf("bytes: %v %v", v, err)
	}
	if v, err := spec.DecodeValue(spec.Type{Kind: spec.KindAny}, oct); err != nil || !reflect.DeepEqual(v, oct) {
		t.Fatalf("any: %v %v", v, err)
	}
	st := encoded(t, func(e *tlv.Encoder, tag tlv.Tag) {
		e.StartStruct(tag)
		e.PutUint(tlv.ContextTag(0), 7)
		e.PutUint(tlv.ContextTag(9), 1)
		e.PutBool(tlv.CommonTag(1), true)
		_ = e.EndContainer()
	})
	if v, err := spec.DecodeValue(spec.Type{Kind: spec.KindStruct}, st); err != nil || !reflect.DeepEqual(v, st) {
		t.Fatalf("struct without its fields: %v %v", v, err)
	}
	fields := &spec.Struct{Name: "S", Fields: []spec.Field{{ID: 0, Name: "a", Type: spec.Type{Kind: spec.KindUint, Bits: 8}}}}
	if v, err := spec.DecodeValue(spec.Type{Kind: spec.KindStruct, Struct: fields}, st); err != nil || !reflect.DeepEqual(v, map[uint32]any{0: uint64(7)}) {
		t.Fatalf("struct: %v %v", v, err)
	}
	if v, err := spec.DecodeValue(spec.Type{Kind: spec.KindList}, encoded(t, func(e *tlv.Encoder, tag tlv.Tag) {
		spec.PutList(spec.PutUint[uint8])(e, tag, []uint8{1})
	})); err != nil || !reflect.DeepEqual(v, []any{encoded(t, func(e *tlv.Encoder, tag tlv.Tag) { e.PutUint(tag, 1) })}) {
		t.Fatalf("list without an entry type: %v %v", v, err)
	}
	for name, tc := range map[string]struct {
		typ  spec.Type
		node spec.Node
	}{
		"bool as uint":       {spec.Type{Kind: spec.KindUint, Bits: 8}, encoded(t, func(e *tlv.Encoder, tag tlv.Tag) { e.PutBool(tag, true) })},
		"uint past its bits": {spec.Type{Kind: spec.KindUint, Bits: 8}, encoded(t, func(e *tlv.Encoder, tag tlv.Tag) { e.PutUint(tag, 300) })},
		"uint as bool":       {spec.Type{Kind: spec.KindBool}, encoded(t, func(e *tlv.Encoder, tag tlv.Tag) { e.PutUint(tag, 1) })},
		"uint as string":     {spec.Type{Kind: spec.KindString}, encoded(t, func(e *tlv.Encoder, tag tlv.Tag) { e.PutUint(tag, 1) })},
		"uint as list":       {spec.Type{Kind: spec.KindList}, encoded(t, func(e *tlv.Encoder, tag tlv.Tag) { e.PutUint(tag, 1) })},
		"uint as struct":     {spec.Type{Kind: spec.KindStruct, Struct: fields}, encoded(t, func(e *tlv.Encoder, tag tlv.Tag) { e.PutUint(tag, 1) })},
		"a bad entry": {spec.Type{Kind: spec.KindList, Entry: &spec.Field{Type: spec.Type{Kind: spec.KindBool}}}, encoded(t, func(e *tlv.Encoder, tag tlv.Tag) {
			spec.PutList(spec.PutUint[uint8])(e, tag, []uint8{1})
		})},
		"a bad field": {spec.Type{Kind: spec.KindStruct, Struct: &spec.Struct{Fields: []spec.Field{{ID: 0, Name: "a", Type: spec.Type{Kind: spec.KindBool}}}}}, st},
	} {
		if _, err := spec.DecodeValue(tc.typ, tc.node); err == nil {
			t.Errorf("%s decoded", name)
		}
	}
}

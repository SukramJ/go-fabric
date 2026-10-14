// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	bindingdef "github.com/SukramJ/go-fabric/cluster/spec/binding"
	"github.com/SukramJ/go-fabric/schema"
	"github.com/SukramJ/go-fabric/tlv"

	// Every generated definition, so spec.Lookup knows each one.
	_ "github.com/SukramJ/go-fabric/controller/client"
)

// synth builds a JSON value of t. variant picks between forms: 0 uses the
// largest value and names, 1 numbers and null where nullable.
func synth(t spec.Type, nullable bool, variant, depth int) any {
	if nullable && variant == 1 {
		return nil
	}
	bits := t.Bits
	if bits == 0 {
		bits = 64
	}
	switch t.Kind {
	case spec.KindBool:
		return variant == 0
	case spec.KindUint:
		if variant == 0 {
			return json.Number(strconv.FormatUint(^uint64(0)>>(64-bits), 10))
		}
		return "0x1"
	case spec.KindEnum:
		if t.Enum != nil && len(t.Enum.Values) > 0 {
			if variant == 0 {
				return t.Enum.Values[len(t.Enum.Values)-1].Name
			}
			return json.Number(strconv.FormatUint(t.Enum.Values[0].Value, 10))
		}
		return json.Number("0")
	case spec.KindBitmap:
		if t.Bitmap != nil && len(t.Bitmap.Members) > 0 && variant == 0 {
			m := map[string]any{}
			for mi := range t.Bitmap.Members {
				mem := &t.Bitmap.Members[mi]
				if mem.Width == 1 {
					m[mem.Name] = true
				} else {
					m[mem.Name] = json.Number("1")
				}
			}
			return m
		}
		return json.Number("1")
	case spec.KindInt:
		if variant == 0 {
			return json.Number(strconv.FormatInt(-1<<(bits-1), 10))
		}
		return json.Number(strconv.FormatInt(1<<(bits-1)-1, 10))
	case spec.KindFloat32:
		return json.Number("1.5")
	case spec.KindFloat64:
		return json.Number("-2.25e10")
	case spec.KindString:
		return "go-fabric ü"
	case spec.KindBytes:
		if variant == 0 {
			return "hex:00ff10"
		}
		return "base64:AP8Q"
	case spec.KindList:
		if t.Entry == nil || depth > 4 {
			return []any{}
		}
		e := synth(t.Entry.Type, t.Entry.Quality.Nullable, variant, depth+1)
		return []any{e, e}
	case spec.KindStruct:
		m := map[string]any{}
		if t.Struct == nil {
			return m
		}
		for i := range t.Struct.Fields {
			f := &t.Struct.Fields[i]
			if variant == 1 && !f.Mandatory() && i%2 == 1 {
				continue
			}
			key := f.Name
			if variant == 1 {
				key = strconv.FormatUint(uint64(f.ID), 10)
			}
			m[key] = synth(f.Type, f.Quality.Nullable, variant, depth+1)
		}
		return m
	case spec.KindAny:
	}
	return map[string]any{"1": json.Number("7"), "2": []any{"hex:01", "s:5", "f:1.5", "d:2.5", "u:0x10", nil, true, "text"}}
}

// writeNode writes n as TLV, so the test can read it back.
func writeNode(enc *tlv.Encoder, tag tlv.Tag, n spec.Node) {
	el := n.El
	switch {
	case el.IsContainer:
		switch el.Type {
		case tlv.TypeArray:
			enc.StartArray(tag)
		case tlv.TypeList:
			enc.StartList(tag)
		default:
			enc.StartStruct(tag)
		}
		for _, c := range n.Children {
			writeNode(enc, c.El.Tag, c)
		}
		_ = enc.EndContainer()
	case el.IsNull:
		enc.PutNull(tag)
	case el.Type <= tlv.TypeSignedInt8:
		enc.PutInt(tag, el.Int)
	case el.Type <= tlv.TypeUnsignedInt8:
		enc.PutUint(tag, el.Uint)
	case el.Type == tlv.TypeBoolFalse || el.Type == tlv.TypeBoolTrue:
		enc.PutBool(tag, el.Bool)
	case el.Type == tlv.TypeFloat4:
		enc.PutFloat32(tag, float32(el.Float))
	case el.Type == tlv.TypeFloat8:
		enc.PutFloat64(tag, el.Float)
	case el.Type >= tlv.TypeUTF8Str1 && el.Type <= tlv.TypeUTF8Str8:
		enc.PutUTF8(tag, el.String)
	default:
		enc.PutOctets(tag, el.Octets)
	}
}

// wire is n on the wire and read back, as a peer would receive it.
func wire(t *testing.T, n spec.Node) spec.Node {
	t.Helper()
	enc := tlv.NewEncoder()
	writeNode(enc, tlv.AnonymousTag(), n)
	b, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	back, err := spec.ReadElement(tlv.NewDecoder(b))
	if err != nil {
		t.Fatal(err)
	}
	return back
}

func roundTrip(t *testing.T, what string, f *spec.Field, v any) {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
	n, err := spec.EncodeJSON(f, raw)
	if err != nil {
		t.Fatalf("%s: EncodeJSON(%s): %v", what, raw, err)
	}
	received := wire(t, n)
	if f != nil {
		if _, err := spec.DecodeValue(f.Type, received); err != nil {
			t.Fatalf("%s: DecodeValue of %s: %v", what, raw, err)
		}
	}
	out, err := spec.DecodeJSON(f, received)
	if err != nil {
		t.Fatalf("%s: DecodeJSON: %v", what, err)
	}
	again, err := spec.EncodeJSON(f, out)
	if err != nil {
		t.Fatalf("%s: EncodeJSON of the decoded %s: %v", what, out, err)
	}
	if !reflect.DeepEqual(wire(t, again), received) {
		t.Fatalf("%s: %s → %s does not encode to the same value", what, raw, out)
	}
}

// TestJSONRoundTripEveryDefinition: a value of every attribute and of every
// command and event payload of every committed definition survives
// JSON → node → wire → JSON → node, in both variants of synth.
func TestJSONRoundTripEveryDefinition(t *testing.T) {
	t.Parallel()
	ids := make([]uint32, 0, len(schema.ClusterNames))
	for id := range schema.ClusterNames {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	clusters, values := 0, 0
	for _, id := range ids {
		def := spec.Lookup(id)
		if def == nil {
			continue
		}
		clusters++
		for variant := range 2 {
			for i := range def.Attributes {
				a := &def.Attributes[i]
				f := spec.AttributeField(a)
				roundTrip(t, fmt.Sprintf("%s.%s/%d", def.Name, a.Name, variant), f, synth(a.Type, a.Quality.Nullable, variant, 0))
				values++
			}
			for i := range def.Commands {
				c := &def.Commands[i]
				f := spec.CommandField(c)
				roundTrip(t, fmt.Sprintf("%s.%s/%d", def.Name, c.Name, variant), f, synth(f.Type, false, variant, 0))
				values++
			}
			for i := range def.Events {
				e := &def.Events[i]
				f := spec.EventField(e)
				roundTrip(t, fmt.Sprintf("%s.%s/%d", def.Name, e.Name, variant), f, synth(f.Type, false, variant, 0))
				values++
			}
		}
	}
	if clusters < 50 {
		t.Fatalf("only %d definitions registered", clusters)
	}
	t.Logf("%d definitions, %d values", clusters, values)
	// An untyped value: CustomArgument.h's forms.
	roundTrip(t, "any", nil, synth(spec.Type{}, false, 0, 0))
}

// TestJSONChipToolFixtures: literal arguments of the CHIP YAML suites at
// 6170af84 encode as their types say.
func TestJSONChipToolFixtures(t *testing.T) {
	t.Parallel()
	binding := spec.AttributeField(bindingdef.Definition.Attribute(bindingdef.AttrBinding))
	// src/app/tests/suites/TestBinding.yaml:65-70 (keys as the YAML spells them).
	n, err := spec.EncodeJSON(binding, json.RawMessage(`[
		{"FabricIndex": 0, "Group": 1},
		{"FabricIndex": 0, "Node": 1, "Endpoint": 1, "Cluster": 6},
		{"FabricIndex": 0, "Node": 2, "Endpoint": 1}]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(n.Children) != 3 || n.Children[1].Children[1].El.Uint != 1 || n.Children[1].Children[2].El.Tag.Number != 4 {
		t.Fatalf("binding %+v", n)
	}
	out, err := spec.DecodeJSON(binding, wire(t, n))
	if err != nil {
		t.Fatal(err)
	}
	if want := `[{"group":1,"fabricIndex":0},{"node":1,"endpoint":1,"cluster":6,"fabricIndex":0},{"node":2,"endpoint":1,"fabricIndex":0}]`; string(out) != want {
		t.Fatalf("decoded %s, want %s", out, want)
	}
	// A write leaves the fabric index to the server.
	if _, err := spec.EncodeJSON(binding, json.RawMessage(`[{"node":"0x10","endpoint":1}]`)); err != nil {
		t.Fatalf("without FabricIndex: %v", err)
	}
	// src/app/tests/suites/TestCluster.yaml:734 — an octet string.
	n, err = spec.EncodeJSON(&spec.Field{Type: spec.Type{Kind: spec.KindBytes}}, json.RawMessage(`"hex:000102030405"`))
	if err != nil || string(n.El.Octets) != "\x00\x01\x02\x03\x04\x05" {
		t.Fatalf("octets %v %x", err, n.El.Octets)
	}
}

func TestJSONRejects(t *testing.T) {
	t.Parallel()
	binding := spec.AttributeField(bindingdef.Definition.Attribute(bindingdef.AttrBinding))
	u8 := &spec.Field{Type: spec.Type{Kind: spec.KindUint, Bits: 8}}
	enum := &spec.Field{Type: spec.Type{Kind: spec.KindEnum, Bits: 8, Enum: &spec.Enum{Values: []spec.EnumValue{{Value: 1, Name: "On"}}}}}
	bitmap := &spec.Field{Type: spec.Type{Kind: spec.KindBitmap, Bits: 8, Bitmap: &spec.Bitmap{Name: "B", Members: []spec.BitmapMember{{Name: "A", Bit: 0, Width: 1}, {Name: "W", Bit: 2, Width: 2}}}}}
	for _, tc := range []struct {
		f    *spec.Field
		raw  string
		want string
	}{
		{u8, `256`, "not a uint8"},
		{u8, `-1`, "not a uint8"},
		{u8, `null`, "not nullable"},
		{u8, `true`, "unsigned number"},
		{u8, `1 2`, "trailing"},
		{u8, `{`, "unexpected EOF"},
		{enum, `"Off"`, "not a uint8"},
		{bitmap, `{"X": true}`, "not a member"},
		{bitmap, `{"W": 4}`, "not a uint2"},
		{&spec.Field{Type: spec.Type{Kind: spec.KindInt, Bits: 8}}, `128`, "not an int8"},
		{&spec.Field{Type: spec.Type{Kind: spec.KindInt, Bits: 8}}, `"1"`, "signed number"},
		{&spec.Field{Type: spec.Type{Kind: spec.KindFloat32}}, `"x"`, "want a number"},
		{&spec.Field{Type: spec.Type{Kind: spec.KindBool}}, `1`, "boolean"},
		{&spec.Field{Type: spec.Type{Kind: spec.KindString}}, `1`, "want a string"},
		{&spec.Field{Type: spec.Type{Kind: spec.KindBytes}}, `"hex:0"`, "odd length"},
		{&spec.Field{Type: spec.Type{Kind: spec.KindBytes}}, `1`, "octet string"},
		{binding, `{}`, "want an array"},
		{binding, `[1]`, "want an object"},
		{binding, `[{"node": 1, "bogus": 2}]`, `Unexpected field name: "bogus"`},
		{nil, `{"x": 1}`, "not a context tag"},
		{nil, `"s:x"`, "not signed"},
		{nil, `"u:x"`, "not unsigned"},
		{nil, `"f:x"`, "not a float"},
		{nil, `"d:x"`, "not a double"},
		{nil, `"base64:!"`, "illegal base64"},
	} {
		_, err := spec.EncodeJSON(tc.f, json.RawMessage(tc.raw))
		if err == nil || !errors.Is(err, spec.ErrJSON) || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err %v, want %q", tc.raw, err, tc.want)
		}
	}
	// A mandatory struct field: chip-tool's EnsureMemberExist message.
	st := &spec.Field{Type: spec.Type{Kind: spec.KindStruct, Struct: &spec.Struct{Fields: []spec.Field{{ID: 0, Name: "GroupKeySetID", Type: spec.Type{Kind: spec.KindUint, Bits: 16}, Conformance: spec.Conformance{Op: spec.ConfMandatory}}}}}}
	if _, err := spec.EncodeJSON(st, json.RawMessage(`{}`)); err == nil || !strings.Contains(err.Error(), `GroupKeySetID is required. Should be provided as {"groupKeySetID": value}`) {
		t.Errorf("missing member: %v", err)
	}
	// Decoding refuses a node of the wrong shape.
	for _, k := range []spec.Kind{spec.KindBool, spec.KindUint, spec.KindInt, spec.KindFloat32, spec.KindString, spec.KindBytes, spec.KindList, spec.KindStruct} {
		node := spec.Node{El: tlv.Element{Type: tlv.TypeUTF8Str1}}
		if k == spec.KindString {
			node = spec.Node{El: tlv.Element{Type: tlv.TypeBoolTrue}}
		}
		if _, err := spec.DecodeJSON(&spec.Field{Type: spec.Type{Kind: k}}, node); err == nil {
			t.Errorf("kind %d decoded a mismatched node", k)
		}
	}
}

func TestJSONForms(t *testing.T) {
	t.Parallel()
	enum := &spec.Field{Type: spec.Type{Kind: spec.KindEnum, Bits: 8, Enum: &spec.Enum{Values: []spec.EnumValue{{Value: 1, Name: "On"}}}}}
	bitmap := &spec.Field{Type: spec.Type{Kind: spec.KindBitmap, Bits: 8, Bitmap: &spec.Bitmap{Members: []spec.BitmapMember{{Name: "A", Bit: 0, Width: 1}, {Name: "W", Bit: 2, Width: 2}}}}}
	bytesF := &spec.Field{Type: spec.Type{Kind: spec.KindBytes}}
	for _, tc := range []struct {
		f    *spec.Field
		raw  string
		want string
	}{
		{enum, `"on"`, `1`},
		{enum, `"0x01"`, `1`},
		{bitmap, `{"a": true, "W": 3}`, `13`},
		{bitmap, `{"a": false}`, `0`},
		{bytesF, `"str:AB"`, `"hex:4142"`},
		{bytesF, `"base64:QUI="`, `"hex:4142"`},
		{bytesF, `"4142"`, `"hex:4142"`},
		{&spec.Field{Type: spec.Type{Kind: spec.KindUint, Bits: 64}}, `18446744073709551615`, `18446744073709551615`},
		{&spec.Field{Type: spec.Type{Kind: spec.KindInt, Bits: 64}}, `-9223372036854775808`, `-9223372036854775808`},
		{&spec.Field{Type: spec.Type{Kind: spec.KindFloat32}}, `0.1`, `0.1`},
		{&spec.Field{Type: spec.Type{Kind: spec.KindString}}, `"a\"b"`, `"a\"b"`},
		{&spec.Field{Type: spec.Type{Kind: spec.KindStruct}}, `{"3": "x"}`, `{"3":"x"}`},
		{nil, `-5`, `-5`},
		{nil, `1.25`, `"d:1.25"`},
		{nil, `"s:7"`, `"s:7"`},
		{nil, `"base64:AQ=="`, `"hex:01"`},
		{nil, `[null, false]`, `[null,false]`},
	} {
		n, err := spec.EncodeJSON(tc.f, json.RawMessage(tc.raw))
		if err != nil {
			t.Errorf("%s: %v", tc.raw, err)
			continue
		}
		out, err := spec.DecodeJSON(tc.f, n)
		if err != nil || string(out) != tc.want {
			t.Errorf("%s: decoded %s (%v), want %s", tc.raw, out, err, tc.want)
		}
	}
	// A struct member the definition does not name is kept under its tag.
	st := &spec.Field{Type: spec.Type{Kind: spec.KindStruct, Struct: &spec.Struct{Fields: []spec.Field{{ID: 1, Name: "PIRDelay", Type: spec.Type{Kind: spec.KindUint}}}}}}
	n := spec.Node{El: tlv.Element{Type: tlv.TypeStructure, IsContainer: true}, Children: []spec.Node{
		{El: tlv.Element{Tag: tlv.ContextTag(1), Type: tlv.TypeUnsignedInt1, Uint: 2}},
		{El: tlv.Element{Tag: tlv.ContextTag(9), Type: tlv.TypeFloat8, Float: 1}},
	}}
	if out, err := spec.DecodeJSON(st, n); err != nil || string(out) != `{"pirDelay":2,"9":"d:1"}` {
		t.Errorf("struct %s %v", out, err)
	}
	// NaN has no JSON number.
	nan := spec.Node{El: tlv.Element{Type: tlv.TypeFloat8, Float: float64FromBits()}}
	if out, _ := spec.DecodeJSON(&spec.Field{Type: spec.Type{Kind: spec.KindFloat64}}, nan); string(out) != `"NaN"` {
		t.Errorf("NaN %s", out)
	}
}

func float64FromBits() float64 {
	var zero float64
	return zero / zero
}

func TestJSONEdges(t *testing.T) {
	t.Parallel()
	st := &spec.Field{Type: spec.Type{Kind: spec.KindStruct, Struct: &spec.Struct{Fields: []spec.Field{
		{ID: 0, Name: "ID", Type: spec.Type{Kind: spec.KindUint}},
		{ID: 1, Name: "Inner", Type: spec.Type{Kind: spec.KindStruct}},
	}}}}
	n, err := spec.EncodeJSON(st, json.RawMessage(`{"id": 1, "inner": {"0": "u:2"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if out, err := spec.DecodeJSON(st, n); err != nil || string(out) != `{"id":1,"inner":{"0":2}}` {
		t.Errorf("decoded %s %v", out, err)
	}
	if _, err := spec.EncodeJSON(st, json.RawMessage(`{"id": 1, "0": 2}`)); err == nil {
		t.Error("a field given twice was accepted")
	}
	list := spec.Node{El: tlv.Element{Type: tlv.TypeList, IsContainer: true}, Children: []spec.Node{{El: tlv.Element{Type: tlv.TypeEndContainer}}}}
	if _, err := spec.DecodeJSON(nil, list); err == nil {
		t.Error("an end-of-container element rendered")
	}
	if _, err := spec.DecodeJSON(&spec.Field{Type: spec.Type{Kind: spec.KindStruct, Struct: st.Type.Struct}}, spec.Node{El: tlv.Element{Type: tlv.TypeStructure, IsContainer: true}, Children: []spec.Node{{El: tlv.Element{Tag: tlv.ContextTag(0), Type: tlv.TypeBoolTrue}}}}); err == nil {
		t.Error("a mistyped member rendered")
	}
}

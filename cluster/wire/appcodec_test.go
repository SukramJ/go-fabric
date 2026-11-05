// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package wire_test

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/groups"
	"github.com/SukramJ/go-fabric/cluster/spec/modeselect"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// encodeTop runs put against a fresh encoder and returns its bytes.
func encodeTop(t *testing.T, put func(e *tlv.Encoder)) []byte {
	t.Helper()
	e := tlv.NewEncoder()
	put(e)
	b, err := e.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	return b
}

// truncated returns the bytes put writes without their last n
// EndContainer markers: a stream whose containers never close.
func truncated(t *testing.T, n int, put func(e *tlv.Encoder)) []byte {
	t.Helper()
	b := encodeTop(t, put)
	return b[:len(b)-n]
}

// openFirst returns a decoder over b with its first element already read.
func openFirst(t *testing.T, b []byte) (*tlv.Decoder, tlv.Element) {
	t.Helper()
	dec := tlv.NewDecoder(b)
	el, err := dec.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	return dec, el
}

// writeRead writes v with AttributeValueWriter and reads it back with
// AttributeValueReader at path.
func writeRead(t *testing.T, path im.ConcreteAttributePath, v im.AttributeValue) im.AttributeValue {
	t.Helper()
	b := encodeTop(t, func(e *tlv.Encoder) { wire.AttributeValueWriter(e, tlv.AnonymousTag(), v) })
	dec, el := openFirst(t, b)
	got, err := wire.AttributeValueReader(path, el, dec)
	if err != nil {
		t.Fatalf("AttributeValueReader: %v", err)
	}
	return got
}

func modeSelectAttr(attr uint32) im.ConcreteAttributePath {
	return im.ConcreteAttributePath{Endpoint: 1, Cluster: modeselect.ClusterID, Attribute: attr}
}

func TestAppCodecScalarAttributeRoundTrip(t *testing.T) {
	got := writeRead(t, modeSelectAttr(modeselect.AttrCurrentMode), im.AttributeValue{Value: uint8(3)})
	if got.IsNull || fmt.Sprint(got.Value) != "3" {
		t.Fatalf("CurrentMode = %+v, want 3", got)
	}
	got = writeRead(t, modeSelectAttr(modeselect.AttrDescription), im.AttributeValue{Value: "coffee"})
	if fmt.Sprint(got.Value) != "coffee" {
		t.Fatalf("Description = %+v, want coffee", got)
	}
}

func TestAppCodecNullAttributeRoundTrip(t *testing.T) {
	for name, v := range map[string]im.AttributeValue{
		"IsNull":     {IsNull: true},
		"nil value":  {},
		"unknown":    {Value: struct{ X int }{1}},
		"float list": {Value: []float64{1.5}},
	} {
		got := writeRead(t, modeSelectAttr(modeselect.AttrStartUpMode), v)
		if !got.IsNull || got.Value != nil {
			t.Errorf("%s: read back %+v, want null", name, got)
		}
	}
}

func TestAppCodecStructListAttributeRoundTrip(t *testing.T) {
	want := []modeselect.ModeOptionStruct{
		{Label: "Espresso", Mode: 1, SemanticTags: []modeselect.SemanticTagStruct{{MfgCode: 0xFFF1, Value: 2}}},
		{Label: "Lungo", Mode: 2, SemanticTags: []modeselect.SemanticTagStruct{{Value: 3}}},
	}
	got := writeRead(t, modeSelectAttr(modeselect.AttrSupportedModes), im.AttributeValue{Value: want})
	list, ok := got.Value.(spec.List[modeselect.ModeOptionStruct])
	if !ok {
		t.Fatalf("SupportedModes decoded as %T, want spec.List[ModeOptionStruct]", got.Value)
	}
	if !reflect.DeepEqual([]modeselect.ModeOptionStruct(list), want) {
		t.Fatalf("SupportedModes = %+v, want %+v", list, want)
	}
}

func TestAppCodecScalarListAttributeRoundTrip(t *testing.T) {
	path := modeSelectAttr(spec.AttrAttributeList)
	got := writeRead(t, path, im.AttributeValue{Value: []uint32{0, 1, 0xFFFD}})
	if fmt.Sprint(got.Value) != "[0 1 65533]" {
		t.Fatalf("AttributeList = %v, want [0 1 65533]", got.Value)
	}
}

func TestAppCodecUntypedAttributeKeepsNode(t *testing.T) {
	path := im.ConcreteAttributePath{Cluster: 0xFFF1FC00, Attribute: 7}
	got := writeRead(t, path, im.AttributeValue{Value: groups.AddGroupRequest{GroupId: 5, GroupName: "x"}})
	n, ok := got.Value.(spec.Node)
	if !ok {
		t.Fatalf("untyped value is %T, want spec.Node", got.Value)
	}
	if !n.El.IsContainer || len(n.Children) != 2 {
		t.Fatalf("untyped node = %+v, want a two-field structure", n)
	}
}

func TestAppCodecAttributeReaderErrors(t *testing.T) {
	// A scalar where the attribute's definition wants a list of structs.
	b := encodeTop(t, func(e *tlv.Encoder) { e.PutUint(tlv.AnonymousTag(), 1) })
	dec, el := openFirst(t, b)
	if _, err := wire.AttributeValueReader(modeSelectAttr(modeselect.AttrSupportedModes), el, dec); err == nil {
		t.Fatal("scalar SupportedModes: want an error")
	}
	// A container that never closes.
	b = truncated(t, 1, func(e *tlv.Encoder) {
		e.StartArray(tlv.AnonymousTag())
		e.PutUint(tlv.AnonymousTag(), 1)
		_ = e.EndContainer()
	})
	dec, el = openFirst(t, b)
	if _, err := wire.AttributeValueReader(modeSelectAttr(modeselect.AttrSupportedModes), el, dec); err == nil {
		t.Fatal("truncated container: want an error")
	}
}

func TestAppCodecWriterEncodesEveryScalar(t *testing.T) {
	values := []any{
		true, uint8(8), uint16(16), uint32(32), uint64(64),
		int8(-8), int16(-16), int32(-32), int64(-64),
		float32(1.5), float64(2.5), "s",
		tlv.BoundedString{Value: "bounded", MaxBytes: 32},
		[]byte{1, 2},
	}
	b := encodeTop(t, func(e *tlv.Encoder) {
		e.StartStruct(tlv.AnonymousTag())
		for i, v := range values {
			wire.AttributeValueWriter(e, tlv.ContextTag(uint8(i)), im.AttributeValue{Value: v})
		}
		wire.AttributeValueWriter(e, tlv.ContextTag(uint8(len(values))), im.AttributeValue{IsNull: true})
		_ = e.EndContainer()
	})
	dec, _ := openFirst(t, b)
	got, err := wire.DecodeGenericTagMap(dec)
	if err != nil {
		t.Fatalf("DecodeGenericTagMap: %v", err)
	}
	want := map[uint8]any{
		0: true, 1: uint64(8), 2: uint64(16), 3: uint64(32), 4: uint64(64),
		5: int64(-8), 6: int64(-16), 7: int64(-32), 8: int64(-64),
		9: float64(1.5), 10: float64(2.5), 11: "s", 12: "bounded",
		13: []byte{1, 2}, 14: nil,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fields = %#v\nwant %#v", got, want)
	}
}

type labels []string

func TestAppCodecWriterEncodesLists(t *testing.T) {
	cases := []struct {
		name string
		v    any
		want func(e *tlv.Encoder)
	}{
		{"strings", []string{"a", "b"}, func(e *tlv.Encoder) {
			e.StartArray(tlv.AnonymousTag())
			e.PutUTF8(tlv.AnonymousTag(), "a")
			e.PutUTF8(tlv.AnonymousTag(), "b")
			_ = e.EndContainer()
		}},
		{"named strings", labels{"c"}, func(e *tlv.Encoder) {
			e.StartArray(tlv.AnonymousTag())
			e.PutUTF8(tlv.AnonymousTag(), "c")
			_ = e.EndContainer()
		}},
		{"ints", []int16{-1, 2}, func(e *tlv.Encoder) {
			e.StartArray(tlv.AnonymousTag())
			e.PutInt(tlv.AnonymousTag(), -1)
			e.PutInt(tlv.AnonymousTag(), 2)
			_ = e.EndContainer()
		}},
		{"bools", []bool{true, false}, func(e *tlv.Encoder) {
			e.StartArray(tlv.AnonymousTag())
			e.PutBool(tlv.AnonymousTag(), true)
			e.PutBool(tlv.AnonymousTag(), false)
			_ = e.EndContainer()
		}},
		{"operational states", []wire.OperationalStateStruct{{OperationalStateID: 1}}, func(e *tlv.Encoder) {
			wire.OperationalStateList{{OperationalStateID: 1}}.EncodeTLV(e, tlv.AnonymousTag())
		}},
		{"mode options", []wire.ModeOptionStruct{{Label: "Eco", Mode: 4}}, func(e *tlv.Encoder) {
			wire.ModeOptionList{{Label: "Eco", Mode: 4}}.EncodeTLV(e, tlv.AnonymousTag())
		}},
		{"encodables with nil", []spec.Encodable{nil, groups.AddGroupResponse{GroupId: 9}}, func(e *tlv.Encoder) {
			e.StartArray(tlv.AnonymousTag())
			e.PutNull(tlv.AnonymousTag())
			groups.AddGroupResponse{GroupId: 9}.EncodeTLV(e, tlv.AnonymousTag())
			_ = e.EndContainer()
		}},
		{"pointers with nil", []*groups.AddGroupResponse{nil}, func(e *tlv.Encoder) {
			e.StartArray(tlv.AnonymousTag())
			e.PutNull(tlv.AnonymousTag())
			_ = e.EndContainer()
		}},
	}
	for _, tc := range cases {
		got := encodeTop(t, func(e *tlv.Encoder) {
			if !wire.EncodeApplicationValue(e, tlv.AnonymousTag(), tc.v) {
				t.Errorf("%s: EncodeApplicationValue reported false", tc.name)
			}
		})
		if want := encodeTop(t, tc.want); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: bytes % X, want % X", tc.name, got, want)
		}
	}
	for _, v := range []any{3, []float32{1}, struct{}{}} {
		e := tlv.NewEncoder()
		if wire.EncodeApplicationValue(e, tlv.AnonymousTag(), v) {
			t.Errorf("EncodeApplicationValue(%T) reported true", v)
		}
	}
}

func TestAppCodecCommandFieldsWriter(t *testing.T) {
	resp := groups.AddGroupResponse{Status: 0, GroupId: 7}
	got := encodeTop(t, func(e *tlv.Encoder) { wire.CommandFieldsWriter(e, tlv.AnonymousTag(), resp) })
	if want := encodeTop(t, func(e *tlv.Encoder) { resp.EncodeTLV(e, tlv.AnonymousTag()) }); !reflect.DeepEqual(got, want) {
		t.Fatalf("response bytes % X, want % X", got, want)
	}
	got = encodeTop(t, func(e *tlv.Encoder) { wire.CommandFieldsWriter(e, tlv.AnonymousTag(), 42) })
	if want := []byte{0x15, 0x18}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unknown response bytes % X, want an empty structure % X", got, want)
	}
	if wire.EncodeApplicationResponse(tlv.NewEncoder(), tlv.AnonymousTag(), "no") {
		t.Fatal("EncodeApplicationResponse(string) reported true")
	}
}

func TestAppCodecCommandFieldsReader(t *testing.T) {
	addGroup := im.ConcreteCommandPath{Endpoint: 1, Cluster: groups.ClusterID, Command: groups.CmdAddGroup}
	b := encodeTop(t, func(e *tlv.Encoder) {
		groups.AddGroupRequest{GroupId: 0x0102, GroupName: "Kitchen"}.EncodeTLV(e, tlv.AnonymousTag())
	})
	dec, el := openFirst(t, b)
	fields, err := wire.CommandFieldsReader(addGroup, dec, el)
	if err != nil {
		t.Fatalf("AddGroup: %v", err)
	}
	if fmt.Sprintf("%+v", fields) != fmt.Sprintf("%+v", groups.AddGroupRequest{GroupId: 0x0102, GroupName: "Kitchen"}) {
		t.Fatalf("AddGroup fields = %+v", fields)
	}

	// A group id of 0 is below the request's constraint.
	b = encodeTop(t, func(e *tlv.Encoder) { groups.AddGroupRequest{GroupName: "x"}.EncodeTLV(e, tlv.AnonymousTag()) })
	dec, el = openFirst(t, b)
	if _, err := wire.CommandFieldsReader(addGroup, dec, el); err == nil {
		t.Fatal("AddGroup with group id 0: want an error")
	}

	// A command no definition decodes takes the generic map.
	b = buildPayload(t, func(e *tlv.Encoder) { e.PutUint(tlv.ContextTag(3), 33) })
	dec, el = openFirst(t, b)
	fields, err = wire.CommandFieldsReader(im.ConcreteCommandPath{Cluster: 0xFFF1FC01, Command: 1}, dec, el)
	if err != nil {
		t.Fatalf("generic: %v", err)
	}
	if !reflect.DeepEqual(fields, map[uint8]any{3: uint64(33)}) {
		t.Fatalf("generic fields = %#v", fields)
	}
}

func TestAppCodecResponseCommandOf(t *testing.T) {
	req := im.ConcreteCommandPath{Endpoint: 1, Cluster: groups.ClusterID, Command: groups.CmdAddGroup}
	ent := im.InvokeResponseEntry{Path: req, Response: groups.AddGroupResponse{GroupId: 1}, HasResponse: true}
	wire.ResponseCommandOf(&ent)
	if ent.Path.Command != groups.CmdAddGroupResponse {
		t.Fatalf("command = 0x%02X, want AddGroupResponse 0x%02X", ent.Path.Command, groups.CmdAddGroupResponse)
	}
	for name, ent := range map[string]im.InvokeResponseEntry{
		"status":        {Path: req, IsStatus: true, Response: groups.AddGroupResponse{}},
		"no response":   {Path: req},
		"not a payload": {Path: req, Response: groups.AddGroupRequest{}},
	} {
		wire.ResponseCommandOf(&ent)
		if ent.Path.Command != groups.CmdAddGroup {
			t.Errorf("%s: command moved to 0x%02X", name, ent.Path.Command)
		}
	}
}

func TestAppCodecDecodeGenericTagMap(t *testing.T) {
	b := buildPayload(t, func(e *tlv.Encoder) {
		e.PutUint(tlv.ContextTag(0), 1)
		e.StartStruct(tlv.ContextTag(1))
		e.StartArray(tlv.ContextTag(0))
		e.PutUint(tlv.AnonymousTag(), 2)
		_ = e.EndContainer()
		_ = e.EndContainer()
		e.PutUTF8(tlv.ContextTag(2), "two")
		e.PutUint(tlv.CommonTag(5), 5)
	})
	dec, _ := openFirst(t, b)
	got, err := wire.DecodeGenericTagMap(dec)
	if err != nil {
		t.Fatalf("DecodeGenericTagMap: %v", err)
	}
	if want := map[uint8]any{0: uint64(1), 2: "two"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("fields = %#v, want %#v", got, want)
	}

	dec, _ = openFirst(t, buildPayload(t, func(*tlv.Encoder) {}))
	if got, err := wire.DecodeGenericTagMap(dec); err != nil || got != nil {
		t.Fatalf("empty structure = %#v, %v; want nil, nil", got, err)
	}
}

func TestAppCodecDecodeGenericTagMapErrors(t *testing.T) {
	// The structure ends without its EndContainer.
	b := truncated(t, 1, func(e *tlv.Encoder) {
		e.StartStruct(tlv.AnonymousTag())
		e.PutUint(tlv.ContextTag(0), 1)
		_ = e.EndContainer()
	})
	dec, _ := openFirst(t, b)
	if _, err := wire.DecodeGenericTagMap(dec); err == nil {
		t.Fatal("truncated structure: want an error")
	}
	// A nested container ends without its EndContainer.
	b = truncated(t, 2, func(e *tlv.Encoder) {
		e.StartStruct(tlv.AnonymousTag())
		e.StartList(tlv.ContextTag(0))
		e.PutUint(tlv.AnonymousTag(), 1)
		_ = e.EndContainer()
		_ = e.EndContainer()
	})
	dec, _ = openFirst(t, b)
	if _, err := wire.DecodeGenericTagMap(dec); err == nil {
		t.Fatal("truncated nested container: want an error")
	}
}

func TestAppCodecDrainContainer(t *testing.T) {
	b := encodeTop(t, func(e *tlv.Encoder) {
		e.StartStruct(tlv.AnonymousTag())
		e.StartArray(tlv.ContextTag(0))
		e.StartStruct(tlv.AnonymousTag())
		_ = e.EndContainer()
		_ = e.EndContainer()
		_ = e.EndContainer()
		e.PutUint(tlv.AnonymousTag(), 99)
	})
	dec, _ := openFirst(t, b)
	if err := wire.DrainContainer(dec); err != nil {
		t.Fatalf("DrainContainer: %v", err)
	}
	el, err := dec.Next()
	if err != nil || el.Uint != 99 {
		t.Fatalf("after drain: %+v, %v; want the trailing 99", el, err)
	}
	if err := wire.DrainContainer(dec); err == nil {
		t.Fatal("drain past the end: want an error")
	}
}

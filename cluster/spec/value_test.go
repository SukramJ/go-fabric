// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/tlv"
)

func valueTLV(t *testing.T, v spec.Encodable) []byte {
	t.Helper()
	enc := tlv.NewEncoder()
	v.EncodeTLV(enc, tlv.AnonymousTag())
	b, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func directTLV(t *testing.T, put func(*tlv.Encoder)) []byte {
	t.Helper()
	enc := tlv.NewEncoder()
	put(enc)
	b, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestValueEncodes: each constructor writes what the encoder writes for
// its Go type, and the null (and the zero Value) TLV null.
func TestValueEncodes(t *testing.T) {
	t.Parallel()
	a := tlv.AnonymousTag()
	type enum8 uint8
	cases := []struct {
		name string
		v    spec.Encodable
		want func(*tlv.Encoder)
	}{
		{"Uint8", spec.Uint8(20), func(e *tlv.Encoder) { e.PutUint(a, 20) }},
		{"Uint16", spec.Uint16(300), func(e *tlv.Encoder) { e.PutUint(a, 300) }},
		{"Uint32", spec.Uint32(70000), func(e *tlv.Encoder) { e.PutUint(a, 70000) }},
		{"Uint64", spec.Uint64(1 << 40), func(e *tlv.Encoder) { e.PutUint(a, 1<<40) }},
		{"Uint enum", spec.Uint(enum8(2)), func(e *tlv.Encoder) { e.PutUint(a, 2) }},
		{"Int8", spec.Int8(-1), func(e *tlv.Encoder) { e.PutInt(a, -1) }},
		{"Int16", spec.Int16(2250), func(e *tlv.Encoder) { e.PutInt(a, 2250) }},
		{"Int32", spec.Int32(-70000), func(e *tlv.Encoder) { e.PutInt(a, -70000) }},
		{"Int64", spec.Int64(-1 << 40), func(e *tlv.Encoder) { e.PutInt(a, -1<<40) }},
		{"Bool", spec.Bool(true), func(e *tlv.Encoder) { e.PutBool(a, true) }},
		{"Float32", spec.Float32(1.5), func(e *tlv.Encoder) { e.PutFloat32(a, 1.5) }},
		{"Float64", spec.Float64(2.5), func(e *tlv.Encoder) { e.PutFloat64(a, 2.5) }},
		{"String", spec.String("x"), func(e *tlv.Encoder) { e.PutUTF8(a, "x") }},
		{"Bytes", spec.Bytes([]byte{1, 2}), func(e *tlv.Encoder) { e.PutOctets(a, []byte{1, 2}) }},
		{"Null", spec.Null[uint16](), func(e *tlv.Encoder) { e.PutNull(a) }},
		{"zero", spec.Value[uint16]{}, func(e *tlv.Encoder) { e.PutNull(a) }},
	}
	for _, c := range cases {
		if got, want := valueTLV(t, c.v), directTLV(t, c.want); !bytes.Equal(got, want) {
			t.Errorf("%s: % X, want % X", c.name, got, want)
		}
	}
	if v, ok := spec.Uint16(5).Get(); !ok || v != 5 || spec.Uint16(5).IsNull() {
		t.Errorf("Uint16(5).Get() = %v, %v", v, ok)
	}
	if _, ok := spec.Null[uint16]().Get(); ok || !spec.Null[uint16]().IsNull() {
		t.Error("Null is a value")
	}
}

// TestDecodeAttribute: globals by their own type with or without a
// definition, null as nil, an unknown attribute as its node with
// ErrUntyped, a scalar at its widest Go type.
func TestDecodeAttribute(t *testing.T) {
	t.Parallel()
	def := &spec.Cluster{ID: 6, Attributes: []spec.Attribute{
		{ID: 0, Name: "OnOff", Type: spec.Type{Kind: spec.KindBool}},
		{ID: 1, Name: "Level", Type: spec.Type{Kind: spec.KindUint, Bits: 8}},
	}}
	node := func(b []byte) spec.Node {
		n, err := spec.ReadElement(tlv.NewDecoder(b))
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if v, err := spec.DecodeAttribute(nil, spec.AttrClusterRevision, node(valueTLV(t, spec.Uint16(4)))); err != nil || v != uint64(4) {
		t.Errorf("ClusterRevision without a definition: %v, %v", v, err)
	}
	if v, err := spec.DecodeAttribute(nil, spec.AttrAttributeList, node(valueTLV(t, spec.List[spec.Value[uint32]]{spec.Uint32(0)}))); err != nil || len(v.([]any)) != 1 {
		t.Errorf("AttributeList: %v, %v", v, err)
	}
	if v, err := spec.DecodeAttribute(def, 1, node(valueTLV(t, spec.Uint8(20)))); err != nil || v != uint64(20) {
		t.Errorf("Level: %v, %v", v, err)
	}
	if v, err := spec.DecodeAttribute(def, 1, node(valueTLV(t, spec.Null[uint8]()))); err != nil || v != nil {
		t.Errorf("null: %v, %v", v, err)
	}
	if _, err := spec.DecodeAttribute(def, 7, node(valueTLV(t, spec.Uint8(1)))); !errors.Is(err, spec.ErrUntyped) {
		t.Errorf("unknown attribute: %v", err)
	}
	if _, err := spec.DecodeAttribute(nil, 0, node(valueTLV(t, spec.Bool(true)))); !errors.Is(err, spec.ErrUntyped) {
		t.Errorf("no definition: %v", err)
	}
}

// probeState is a State of the probe definition.
type probeState struct {
	OnOff bool
	Level *uint8
}

func (st *probeState) FromAttributeValues(read func(uint32) (any, bool)) {
	if v, ok := read(0); ok {
		spec.Assign(&st.OnOff, v)
	}
	if v, ok := read(1); ok {
		spec.Assign(&st.Level, v)
	}
}

// TestStateFrom: present values decoded and converted, the rest absent;
// nil values all absent; an undecodable value absent.
func TestStateFrom(t *testing.T) {
	t.Parallel()
	def := &spec.Cluster{ID: 8, Attributes: []spec.Attribute{
		{ID: 0, Name: "OnOff", Type: spec.Type{Kind: spec.KindBool}},
		{ID: 1, Name: "Level", Type: spec.Type{Kind: spec.KindUint, Bits: 8}},
	}}
	node := func(v spec.Encodable) spec.Node {
		n, err := spec.ReadElement(tlv.NewDecoder(valueTLV(t, v)))
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	st, absent := spec.StateFrom[probeState](def, spec.AttributeValues{0: node(spec.Bool(true))})
	if !st.OnOff || st.Level != nil || len(absent) != 1 || absent[0] != 1 {
		t.Errorf("partial: %+v, absent %v", st, absent)
	}
	st, absent = spec.StateFrom[probeState](def, spec.AttributeValues{1: node(spec.Uint8(42)), 0: node(spec.Uint8(1))})
	if st.Level == nil || *st.Level != 42 || len(absent) != 1 || absent[0] != 0 {
		t.Errorf("a value of the wrong type is absent: %+v, absent %v", st, absent)
	}
	if _, absent := spec.StateFrom[probeState](def, nil); len(absent) != 2 {
		t.Errorf("nil values: absent %v", absent)
	}
}

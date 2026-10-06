// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/cluster/spec/rvcrunmode"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// node encodes one value with put and reads it back as a node.
func node(t *testing.T, put func(*tlv.Encoder)) spec.Node {
	t.Helper()
	enc := tlv.NewEncoder()
	put(enc)
	b, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	n, err := spec.ReadElement(tlv.NewDecoder(b))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

var anon = tlv.AnonymousTag()

func wantStatus(t *testing.T, what string, err error, status im.StatusCode) {
	t.Helper()
	var fe *spec.FieldError
	if !errors.As(err, &fe) || fe.MatterStatusCode() != status || !fe.FieldsContainerConsumed() || fe.Error() == "" {
		t.Errorf("%s: %v, want status 0x%02X", what, err, uint8(status))
	}
}

func TestScalarDecoders(t *testing.T) {
	t.Parallel()
	u := node(t, func(e *tlv.Encoder) { e.PutUint(anon, 300) })
	i := node(t, func(e *tlv.Encoder) { e.PutInt(anon, -5) })
	b := node(t, func(e *tlv.Encoder) { e.PutBool(anon, true) })
	f4 := node(t, func(e *tlv.Encoder) { e.PutFloat32(anon, 1.5) })
	f8 := node(t, func(e *tlv.Encoder) { e.PutFloat64(anon, 1e300) })
	s := node(t, func(e *tlv.Encoder) { e.PutUTF8(anon, "héllo") })
	o := node(t, func(e *tlv.Encoder) { e.PutOctets(anon, []byte{1, 2, 3}) })
	null := node(t, func(e *tlv.Encoder) { e.PutNull(anon) })

	if v, err := spec.DecodeUint[uint16](16)(u); err != nil || v != 300 {
		t.Errorf("uint16 %v %v", v, err)
	}
	wantStatus(t, "uint8 overflow", second(spec.DecodeUint[uint8](8)(u)), im.StatusConstraintError)
	wantStatus(t, "uint bound", second(spec.DecodeUintIn[uint16](16, 0, 100)(u)), im.StatusConstraintError)
	wantStatus(t, "uint of a signed", second(spec.DecodeUint[uint8](8)(i)), im.StatusInvalidCommand)
	if v, err := spec.DecodeInt[int8](8)(i); err != nil || v != -5 {
		t.Errorf("int8 %v %v", v, err)
	}
	wantStatus(t, "int bound", second(spec.DecodeIntIn[int16](16, 0, 10)(i)), im.StatusConstraintError)
	wantStatus(t, "int of a bool", second(spec.DecodeInt[int8](8)(b)), im.StatusInvalidCommand)
	if v, err := spec.DecodeBool(b); err != nil || !v {
		t.Errorf("bool %v %v", v, err)
	}
	wantStatus(t, "bool of a uint", second(spec.DecodeBool(u)), im.StatusInvalidCommand)
	if v, err := spec.DecodeFloat32(f4); err != nil || v != 1.5 {
		t.Errorf("float32 %v %v", v, err)
	}
	wantStatus(t, "float32 overflow", second(spec.DecodeFloat32(f8)), im.StatusConstraintError)
	wantStatus(t, "float of a string", second(spec.DecodeFloat32(s)), im.StatusInvalidCommand)
	if v, err := spec.DecodeFloat64(f8); err != nil || v != 1e300 {
		t.Errorf("float64 %v %v", v, err)
	}
	if v, err := spec.DecodeString(s); err != nil || v != "héllo" {
		t.Errorf("string %v %v", v, err)
	}
	wantStatus(t, "string length", second(spec.DecodeStringIn(0, 4)(s)), im.StatusConstraintError)
	wantStatus(t, "string of octets", second(spec.DecodeString(o)), im.StatusInvalidCommand)
	invalidUTF8 := spec.Node{El: tlv.Element{Type: tlv.TypeUTF8Str1, String: "\xff"}}
	wantStatus(t, "invalid UTF-8", second(spec.DecodeString(invalidUTF8)), im.StatusInvalidCommand)
	if v, err := spec.DecodeBytes(o); err != nil || !reflect.DeepEqual(v, []byte{1, 2, 3}) {
		t.Errorf("bytes %v %v", v, err)
	}
	wantStatus(t, "bytes length", second(spec.DecodeBytesIn(4, 8)(o)), im.StatusConstraintError)
	wantStatus(t, "bytes of a string", second(spec.DecodeBytes(s)), im.StatusInvalidCommand)

	nb := spec.DecodeNullable(spec.DecodeBool)
	if v, err := nb(null); err != nil || !v.Null {
		t.Errorf("null %v %v", v, err)
	}
	if v, err := nb(b); err != nil || v != spec.ValueOf(true) {
		t.Errorf("non-null %v %v", v, err)
	}
	wantStatus(t, "nullable of a wrong type", second(nb(u)), im.StatusInvalidCommand)
	ob := spec.DecodeOptional(spec.DecodeBool)
	if v, err := ob(b); err != nil || v == nil || !*v {
		t.Errorf("optional %v %v", v, err)
	}
	wantStatus(t, "optional of a wrong type", second(ob(u)), im.StatusInvalidCommand)
	if spec.NullOf[int]() != (spec.Nullable[int]{Null: true}) {
		t.Error("NullOf")
	}
}

func second[T any](_ T, err error) error { return err }

func TestListAndStructDecoders(t *testing.T) {
	t.Parallel()
	list := node(t, func(e *tlv.Encoder) {
		e.StartArray(anon)
		e.PutUint(anon, 1)
		e.PutUint(anon, 2)
		_ = e.EndContainer()
	})
	if v, err := spec.DecodeList(spec.DecodeUint[uint8](8))(list); err != nil || !reflect.DeepEqual(v, []uint8{1, 2}) {
		t.Errorf("list %v %v", v, err)
	}
	wantStatus(t, "list length", second(spec.DecodeListIn(spec.DecodeUint[uint8](8), 3, 8)(list)), im.StatusConstraintError)
	wantStatus(t, "list entry", second(spec.DecodeList(spec.DecodeBool)(list)), im.StatusInvalidCommand)
	scalar := node(t, func(e *tlv.Encoder) { e.PutUint(anon, 1) })
	wantStatus(t, "list of a scalar", second(spec.DecodeList(spec.DecodeBool)(scalar)), im.StatusInvalidCommand)

	mandatory := node(t, func(e *tlv.Encoder) {
		e.StartStruct(anon)
		e.PutUint(tlv.ContextTag(0), 1)
		_ = e.EndContainer()
	})
	wantStatus(t, "missing mandatory", second(spec.DecodeStruct[rvcrunmode.ModeTagStruct](mandatory)), im.StatusInvalidCommand)
	wantStatus(t, "struct of a scalar", second(spec.DecodeStruct[rvcrunmode.ModeTagStruct](scalar)), im.StatusInvalidCommand)
	nested := node(t, func(e *tlv.Encoder) {
		e.StartStruct(anon)
		e.PutUTF8(tlv.ContextTag(0), "m")
		e.PutUint(tlv.ContextTag(1), 1)
		e.StartArray(tlv.ContextTag(2))
		e.StartStruct(anon)
		e.PutBool(tlv.ContextTag(1), true) // Value is no bool
		_ = e.EndContainer()
		_ = e.EndContainer()
		e.PutUint(tlv.CommonTag(9), 1)  // not a context tag: skipped
		e.PutUint(tlv.ContextTag(9), 1) // not a field: skipped
		_ = e.EndContainer()
	})
	wantStatus(t, "nested field", second(spec.DecodeStruct[rvcrunmode.ModeOptionStruct](nested)), im.StatusInvalidCommand)
}

func TestReadContainer(t *testing.T) {
	t.Parallel()
	if _, err := spec.ReadElement(tlv.NewDecoder(nil)); err == nil {
		t.Error("empty stream")
	}
	if _, err := spec.ReadElement(tlv.NewDecoder([]byte{0x18})); !errors.Is(err, spec.ErrUnbalanced) {
		t.Errorf("stray EndContainer: %v", err)
	}
	if _, err := spec.ReadElement(tlv.NewDecoder([]byte{0x15, 0x24, 0x00, 0x01})); !errors.Is(err, spec.ErrUnbalanced) {
		t.Errorf("unclosed struct: %v", err)
	}
	if _, err := spec.ReadElement(tlv.NewDecoder([]byte{0x15, 0x36, 0x00, 0x18})); !errors.Is(err, spec.ErrUnbalanced) {
		t.Errorf("unclosed nested array: %v", err)
	}
}

func TestEncoders(t *testing.T) {
	t.Parallel()
	enc := tlv.NewEncoder()
	enc.StartStruct(anon)
	spec.PutUint(enc, tlv.ContextTag(0), uint16(7))
	spec.PutInt(enc, tlv.ContextTag(1), int8(-1))
	spec.PutBool(enc, tlv.ContextTag(2), true)
	spec.PutFloat32(enc, tlv.ContextTag(3), 1)
	spec.PutFloat64(enc, tlv.ContextTag(4), 2)
	spec.PutString(enc, tlv.ContextTag(5), "s")
	spec.PutBytes(enc, tlv.ContextTag(6), []byte{9})
	spec.PutNullable(spec.PutBool)(enc, tlv.ContextTag(7), spec.NullOf[bool]())
	spec.PutNullable(spec.PutBool)(enc, tlv.ContextTag(8), spec.ValueOf(false))
	spec.PutOptional(enc, tlv.ContextTag(9), nil, spec.PutBool)
	spec.PutList(spec.PutUint[uint8])(enc, tlv.ContextTag(10), []uint8{1})
	spec.PutStruct(enc, tlv.ContextTag(11), rvcrunmode.ModeTagStruct{Value: 1})
	spec.List[rvcrunmode.ModeTagStruct]{{Value: 2}}.EncodeTLV(enc, tlv.ContextTag(12))
	_ = enc.EndContainer()
	b, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	n, err := spec.ReadElement(tlv.NewDecoder(b))
	if err != nil {
		t.Fatal(err)
	}
	if len(n.Children) != 12 { // the absent optional is left out
		t.Errorf("%d members: % X", len(n.Children), b)
	}
	if n.Children[7].El.Type != tlv.TypeNull {
		t.Error("null")
	}
}

func TestDecodeRequest(t *testing.T) {
	t.Parallel()
	payload := func(build func(*tlv.Encoder)) (*tlv.Decoder, tlv.Element) {
		enc := tlv.NewEncoder()
		enc.StartStruct(anon)
		build(enc)
		_ = enc.EndContainer()
		b, _ := enc.Bytes()
		dec := tlv.NewDecoder(b)
		open, err := dec.Next()
		if err != nil {
			t.Fatal(err)
		}
		return dec, open
	}
	dec, open := payload(func(e *tlv.Encoder) { e.PutUint(tlv.ContextTag(0), 3) })
	fields, ok, err := spec.DecodeRequest(rvcrunmode.ClusterID, rvcrunmode.CmdChangeToMode, dec, open)
	if !ok || err != nil || fields != (rvcrunmode.ChangeToModeRequest{NewMode: 3}) {
		t.Errorf("ChangeToMode: %v %v %v", fields, ok, err)
	}
	if dec.Remaining() != 0 {
		t.Error("the container was not consumed")
	}
	dec, open = payload(func(e *tlv.Encoder) { e.PutUint(tlv.ContextTag(0), 300) })
	if _, ok, err := spec.DecodeRequest(rvcrunmode.ClusterID, rvcrunmode.CmdChangeToMode, dec, open); !ok {
		t.Error("not decoded")
	} else {
		wantStatus(t, "NewMode overflow", err, im.StatusConstraintError)
	}
	for _, c := range []struct{ cluster, cmd uint32 }{{0xFFFFFF, 0}, {rvcrunmode.ClusterID, rvcrunmode.CmdChangeToModeResponse}, {rvcrunmode.ClusterID, 9}} {
		dec, open = payload(func(*tlv.Encoder) {})
		if _, ok, err := spec.DecodeRequest(c.cluster, c.cmd, dec, open); ok || err != nil {
			t.Errorf("0x%04X/0x%02X decoded: %v", c.cluster, c.cmd, err)
		}
	}
	broken := tlv.NewDecoder([]byte{0x15, 0x24, 0x00})
	open, _ = broken.Next()
	if _, ok, err := spec.DecodeRequest(rvcrunmode.ClusterID, rvcrunmode.CmdChangeToMode, broken, open); !ok || err == nil {
		t.Errorf("truncated payload: %v %v", ok, err)
	}
	spec.Register(rvcrunmode.Definition)
	if spec.Lookup(rvcrunmode.ClusterID) != rvcrunmode.Definition {
		t.Error("re-register")
	}
}

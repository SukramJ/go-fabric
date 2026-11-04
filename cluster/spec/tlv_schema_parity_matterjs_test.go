// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	eemdef "github.com/SukramJ/go-fabric/cluster/spec/electricalenergymeasurement"
	gkmdef "github.com/SukramJ/go-fabric/cluster/spec/groupkeymanagement"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/tlv"
)

// The typed half of matter.js packages/types/test/tlv at 862ad75f.
// matter.js's TLV schemas (TlvNumber.bound, TlvString.bound, TlvArray,
// TlvNullable, TlvAny, TlvObject, TlvOfModel) are one layer that encodes,
// decodes and validates; the Go module splits it: tlv/ is the raw codec,
// cluster/spec the typed decoders (DecodeUintIn, DecodeStringIn,
// DecodeBytesIn, DecodeListIn), the value checks of a definition
// (Instance.CheckValue) and the model-driven codec (EncodeJSON /
// DecodeJSON, TlvOfModel's counterpart). The cases below hold that layer
// to the matter.js cases with a Go subject.

// wireNode reads one element of the hex wire bytes.
func wireNode(t *testing.T, wire string) spec.Node {
	t.Helper()
	raw, err := hex.DecodeString(wire)
	if err != nil {
		t.Fatal(err)
	}
	n, err := spec.ReadElement(tlv.NewDecoder(raw))
	if err != nil {
		t.Fatalf("ReadElement(%s): %v", wire, err)
	}
	return n
}

// sameJSON reports whether two JSON texts hold the same value.
func sameJSON(t *testing.T, a, b []byte) bool {
	t.Helper()
	parse := func(raw []byte) any {
		var v any
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		if err := dec.Decode(&v); err != nil {
			t.Fatalf("parse %s: %v", raw, err)
		}
		return v
	}
	return reflect.DeepEqual(parse(a), parse(b))
}

// TestTlvAnyGenericDecodingParityMatterJS mirrors
// packages/types/test/tlv/TlvAnyTest.ts:78-147 at 862ad75f (describe
// "generic decoding"): TlvAny.decodeAnyTlvStream turns an element stream
// without a schema into a value — a structure into a record keyed by tag
// number, an array or list into an array whatever its members' tags. The
// Go form is DecodeJSON without a definition (spec.DecodeJSON(nil, …)).
func TestTlvAnyGenericDecodingParityMatterJS(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, wire, want string }{
		{"decodes a boolean", "09", `true`},
		{"decodes a null", "14", `null`},
		{"decodes an array of integers", "1604010402" + "18", `[1,2]`},
		{"decodes a list of strings", "170c01610c016218", `["a","b"]`},
		{"decodes a list of strings with partial tags", "170c01612c02016218", `["a","b"]`},
		{"decodes a structure", "152c0101612c02016218", `{"1":"a","2":"b"}`},
		{"decodes and array of structures", "16152c0101612c02016218152c0301632c040164" + "1818", `[{"1":"a","2":"b"},{"3":"c","4":"d"}]`},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := spec.DecodeJSON(nil, wireNode(t, c.wire))
			if err != nil {
				t.Fatal(err)
			}
			if !sameJSON(t, got, []byte(c.want)) {
				t.Errorf("DecodeJSON = %s, want %s", got, c.want)
			}
		})
	}
}

// TestTlvBoundsParityMatterJS mirrors the bounded schemas of
// packages/types/test/tlv at 862ad75f: TlvNumberTest.ts:112-126
// (TlvUInt32.bound({ min: 5, max: 10 }): 6 passes, 1 and 12 are refused),
// TlvStringTest.ts:85-99 and :125-139 (a string and a byte string bound
// to 4..6: 5 passes, 1 and 8 are refused) and TlvArrayTest.ts:119-150
// (TlvArray(TlvString) bound to 2..4: 2 entries pass, 1 and 5 are
// refused, a number among the strings is refused naming its index).
func TestTlvBoundsParityMatterJS(t *testing.T) {
	t.Parallel()
	refused := func(t *testing.T, err error, want bool) {
		t.Helper()
		if (err != nil) != want {
			t.Fatalf("err = %v, want refused %v", err, want)
		}
	}
	t.Run("number", func(t *testing.T) {
		dec := spec.DecodeUintIn[uint32](32, 5, 10)
		for _, c := range []struct {
			name string
			wire string
			out  bool
		}{
			{"validates a value between min and max", "0406", false},
			{"throws an error if the value is too low", "0401", true},
			{"throws an error if the value is too high", "040c", true},
		} {
			_, err := dec(wireNode(t, c.wire))
			refused(t, err, c.out)
		}
	})
	t.Run("string", func(t *testing.T) {
		dec := spec.DecodeStringIn(4, 6)
		for in, out := range map[string]bool{"abcde": false, "a": true, "abcdefgh": true} {
			_, err := dec(wireNode(t, "0c"+hex.EncodeToString([]byte{byte(len(in))})+hex.EncodeToString([]byte(in))))
			refused(t, err, out)
		}
	})
	t.Run("byte string", func(t *testing.T) {
		dec := spec.DecodeBytesIn(4, 6)
		for in, out := range map[string]bool{"0001020304": false, "00": true, "0001020304050607": true} {
			_, err := dec(wireNode(t, "10"+hex.EncodeToString([]byte{byte(len(in) / 2)})+in))
			refused(t, err, out)
		}
	})
	t.Run("array", func(t *testing.T) {
		dec := spec.DecodeListIn(spec.DecodeString, 2, 4)
		str := func(s string) string { return "0c01" + hex.EncodeToString([]byte(s)) }
		for _, c := range []struct {
			name, wire string
			out        bool
		}{
			{"validates an array with an acceptable length", "16" + str("a") + str("b") + "18", false},
			{"throws an error if the array is too short", "16" + str("a") + "18", true},
			{"throws an error if the array is too long", "16" + str("a") + str("b") + str("c") + str("d") + str("e") + "18", true},
		} {
			_, err := dec(wireNode(t, c.wire))
			refused(t, err, c.out)
		}
		_, err := dec(wireNode(t, "16"+str("a")+"040718"))
		if err == nil || !strings.Contains(err.Error(), "entry 1") {
			t.Errorf("a number among strings: err = %v, want a refusal naming entry 1", err)
		}
	})
}

// TestTlvNumberRelaxParityMatterJS mirrors
// packages/types/test/tlv/TlvNumberTest.ts:231-273 at 862ad75f (describe
// "relaxNumberTypeChecks"). matter.js decodes strictly by default and
// relaxes the signedness check only for incoming data reports
// (InputChunk.ts:42-48); the Go server decoders (DecodeUint, DecodeInt) are
// the strict default and spec.DecodeValue, the controller's decode of a
// reported value, is the relaxed one. Relaxed, an integer of the other
// signedness is taken when its value fits — still range-checked — and a
// negative value for an unsigned type, an out-of-range one for a signed
// type and a float for an integer stay refused.
func TestTlvNumberRelaxParityMatterJS(t *testing.T) {
	t.Parallel()
	u8 := spec.Type{Kind: spec.KindUint, Bits: 8}
	i8 := spec.Type{Kind: spec.KindInt, Bits: 8}
	enum32 := spec.Type{Kind: spec.KindEnum, Bits: 32}
	t.Run("accepts a signed-encoded positive value for an unsigned schema when relaxed", func(t *testing.T) {
		if v, err := spec.DecodeValue(u8, wireNode(t, "0005")); err != nil || v != uint64(5) {
			t.Errorf("DecodeValue = %v, %v; want 5", v, err)
		}
	})
	t.Run("rejects a signed-encoded value for an unsigned schema by default", func(t *testing.T) {
		if _, err := spec.DecodeUint[uint8](8)(wireNode(t, "0005")); err == nil {
			t.Error("the strict decoder accepted a signed-encoded value")
		}
	})
	t.Run("rejects a signed-encoded negative value for an unsigned schema even when relaxed", func(t *testing.T) {
		if _, err := spec.DecodeValue(u8, wireNode(t, "00ff")); err == nil {
			t.Error("accepted -1 for uint8")
		}
	})
	t.Run("accepts an unsigned-encoded in-range value for a signed schema when relaxed", func(t *testing.T) {
		if v, err := spec.DecodeValue(i8, wireNode(t, "0405")); err != nil || v != int64(5) {
			t.Errorf("DecodeValue = %v, %v; want 5", v, err)
		}
	})
	t.Run("rejects an unsigned-encoded out-of-range value for a signed schema even when relaxed", func(t *testing.T) {
		if _, err := spec.DecodeValue(i8, wireNode(t, "04c8")); err == nil {
			t.Error("accepted 200 for int8")
		}
	})
	t.Run("rejects a float-encoded value for an integer schema even when relaxed", func(t *testing.T) {
		if _, err := spec.DecodeValue(u8, wireNode(t, "0a0892cc45")); err == nil {
			t.Error("accepted a float for uint8")
		}
	})
	t.Run("accepts a signed-encoded enum value when relaxed", func(t *testing.T) {
		if v, err := spec.DecodeValue(enum32, wireNode(t, "0005")); err != nil || v != uint64(5) {
			t.Errorf("DecodeValue = %v, %v; want 5", v, err)
		}
	})
	t.Run("threads relax options through chunked array element decode", func(t *testing.T) {
		list := wireNode(t, "16000518")
		if _, err := spec.DecodeList(spec.DecodeUint[uint8](8))(list); err == nil {
			t.Error("the strict list decoder accepted a signed-encoded entry")
		}
		v, err := spec.DecodeValue(spec.Type{Kind: spec.KindList, Entry: &spec.Field{Type: u8}}, list)
		if err != nil || !reflect.DeepEqual(v, []any{uint64(5)}) {
			t.Errorf("DecodeValue(list) = %v, %v; want [5]", v, err)
		}
	})
}

// nullableCluster carries the nullable values of
// packages/types/test/tlv/TlvNullableTest.ts:101-153 and
// TlvOfModelTest.ts:256-266 at 862ad75f.
var nullableCluster = func() *spec.Cluster {
	x := spec.Quality{Nullable: true}
	map8 := &spec.Bitmap{Name: "Map8", Bits: 8, Members: []spec.BitmapMember{{Name: "Lsb", Bit: 0, Width: 1}, {Name: "Msb", Bit: 7, Width: 1}}}
	map16 := &spec.Bitmap{Name: "Map16", Bits: 16, Members: []spec.BitmapMember{{Name: "Lsb", Bit: 0, Width: 1}, {Name: "Msb", Bit: 15, Width: 1}}}
	return &spec.Cluster{
		ID: 0xFFF1_FC33, Name: "Nullable", Revision: 1,
		Attributes: []spec.Attribute{
			rw(0, "U64", spec.Type{Kind: spec.KindUint, Bits: 64}, x, spec.Constraint{}),
			rw(1, "I64", spec.Type{Kind: spec.KindInt, Bits: 64}, x, spec.Constraint{}),
			rw(2, "U64Min5", spec.Type{Kind: spec.KindUint, Bits: 64}, x, spec.Constraint{Text: "min 5", Min: &spec.Bound{Int: 5}}),
			rw(3, "I64Bounded", spec.Type{Kind: spec.KindInt, Bits: 64}, x, spec.Constraint{Text: "-10000 to 10000", Min: &spec.Bound{Int: -10000}, Max: &spec.Bound{Int: 10000}}),
			rw(4, "Map8", spec.Type{Kind: spec.KindBitmap, Bits: 8, Bitmap: map8}, x, spec.Constraint{}),
			rw(5, "Map16", spec.Type{Kind: spec.KindBitmap, Bits: 16, Bitmap: map16}, x, spec.Constraint{}),
			rw(6, "Big", spec.Type{Kind: spec.KindUint, Bits: 64}, spec.Quality{}, spec.Constraint{}),
		},
	}
}()

// TestTlvNullableParityMatterJS mirrors
// packages/types/test/tlv/TlvNullableTest.ts:101-153 at 862ad75f: a
// nullable integer reserves the sentinel of its type whatever its bound
// (the maximum of uint64, the minimum of int64), a bounded one admits the
// extremes it states, and a nullable bitmap reserves its most-significant
// bit (§ 7.19.1.2) — map8 bit 7, map16 bit 15. TlvOfModelTest.ts:256-266
// ("keeps the bound of a nullable type that states one") is the bounded
// int64 row.
func TestTlvNullableParityMatterJS(t *testing.T) {
	t.Parallel()
	inst := mustInstance(t, nullableCluster, 0)
	for _, c := range []struct {
		name  string
		attr  uint32
		value any
		ok    bool
	}{
		{"refuses the maximum of an unbounded unsigned type", 0, uint64(math.MaxUint64), false},
		{"admits one below the maximum of an unbounded unsigned type", 0, uint64(math.MaxUint64 - 1), true},
		{"refuses the minimum of an unbounded signed type", 1, int64(math.MinInt64), false},
		{"admits one above the minimum of an unbounded signed type", 1, int64(math.MinInt64 + 1), true},
		{"reserves the sentinel of a type bounded away from it", 2, uint64(math.MaxUint64), false},
		{"admits the lower extreme a constrained type states", 3, int64(-10000), true},
		{"admits the upper extreme a constrained type states", 3, int64(10000), true},
		{"refuses past the extreme a constrained type states", 3, int64(10001), false},
		{"rejects map8 with the reserved MSB set", 4, uint8(0x80), false},
		{"accepts map8 with only lower bits set", 4, uint8(0x01), true},
		{"rejects map16 with the reserved MSB set", 5, uint16(0x8000), false},
		{"accepts map16 with only lower bits set", 5, uint16(0x0001), true},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := inst.CheckValue(c.attr, c.value, nil)
			if (err == nil) != c.ok {
				t.Fatalf("CheckValue(%d, %v) = %v, want ok %v", c.attr, c.value, err, c.ok)
			}
		})
	}
	// TlvOfModelTest.ts:173-175 ("admits the widest value the bound
	// states"): a uint64 admits 2^64 - 2 against its 0..2^64-2 bound.
	if _, err := inst.CheckValue(6, uint64(math.MaxUint64-1), nil); err != nil {
		t.Errorf("CheckValue(Big, 2^64-2) = %v, want admitted", err)
	}
}

// TestTlvObjectParityMatterJS mirrors
// packages/types/test/tlv/TlvObjectTest.ts:117-122 at 862ad75f ("ignores
// unknown fields": a schema without field 1 decodes a structure carrying
// it) and :145-185 ("encode/decodes with fabricIndex", "encode/decodes
// with ignoring fabricIndex for write interaction": a fabric-scoped
// struct round-trips its FabricIndex, and one written without it encodes
// as the struct without the field).
func TestTlvObjectParityMatterJS(t *testing.T) {
	t.Parallel()
	t.Run("ignores unknown fields", func(t *testing.T) {
		var optional string
		err := spec.DecodeFields(wireNode(t, "152401012c02047465737418"), "Object",
			spec.Bind(2, "optionalField", false, &optional, spec.DecodeString))
		if err != nil || optional != "test" {
			t.Fatalf("DecodeFields = %v, optionalField %q; want nil, \"test\"", err, optional)
		}
	})
	withFabric := &spec.Field{Name: "Object", Type: spec.Type{Kind: spec.KindStruct, Struct: &spec.Struct{Name: "Object", Fields: []spec.Field{
		{ID: 1, Name: "MandatoryField", Type: spec.Type{Kind: spec.KindUint, Bits: 8}, Conformance: op(spec.ConfMandatory)},
		{ID: 2, Name: "OptionalField", Type: spec.Type{Kind: spec.KindString}, Conformance: op(spec.ConfOptional)},
		{ID: 254, Name: "FabricIndex", Type: spec.Type{Kind: spec.KindUint, Bits: 8}, Conformance: op(spec.ConfMandatory)},
	}}}}
	wire := func(n spec.Node) string {
		enc := tlv.NewEncoder()
		n.EncodeTLV(enc, tlv.AnonymousTag())
		raw, err := enc.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		return hex.EncodeToString(raw)
	}
	t.Run("encode/decodes with fabricIndex", func(t *testing.T) {
		in := `{"mandatoryField":1,"optionalField":"test","fabricIndex":1}`
		n, err := spec.EncodeJSON(withFabric, json.RawMessage(in))
		if err != nil {
			t.Fatal(err)
		}
		if got := wire(n); got == "152401012c02047465737418" {
			t.Errorf("encoded %s: the FabricIndex field is missing", got)
		}
		out, err := spec.DecodeJSON(withFabric, n)
		if err != nil || !sameJSON(t, out, []byte(in)) {
			t.Errorf("DecodeJSON = %s, %v; want %s", out, err, in)
		}
	})
	t.Run("encode/decodes with ignoring fabricIndex for write interaction", func(t *testing.T) {
		n, err := spec.EncodeJSON(withFabric, json.RawMessage(`{"mandatoryField":1,"optionalField":"test"}`))
		if err != nil {
			t.Fatal(err)
		}
		if got, want := wire(n), "152401012c02047465737418"; got != want {
			t.Errorf("encoded %s, want %s (the struct without FabricIndex)", got, want)
		}
	})
}

// TestTlvOfModelParityMatterJS mirrors
// packages/types/test/tlv/TlvOfModelTest.ts:36-129 at 862ad75f: TlvOfModel
// derives a codec from the model, the Go form is EncodeJSON / DecodeJSON
// over a generated definition. A struct with nullable epoch fields
// (GroupKeySetStruct, alone and in the KeySetWrite command), a struct of
// feature-conditional fields (CumulativeEnergyResetStruct), a nullable enum
// attribute (StartUpOnOff), the nullable struct attribute
// CumulativeEnergyReset and the non-nullable conditional GlobalSceneControl
// round-trip; an attribute typed "any" has a codec too. The epoch values
// are the wire integers: the Go module keeps epoch-s and epoch-us as
// counted from the Matter epoch and converts nothing.
func TestTlvOfModelParityMatterJS(t *testing.T) {
	t.Parallel()
	roundTrip := func(t *testing.T, f *spec.Field, in string) {
		t.Helper()
		n, err := spec.EncodeJSON(f, json.RawMessage(in))
		if err != nil {
			t.Fatalf("EncodeJSON(%s): %v", in, err)
		}
		out, err := spec.DecodeJSON(f, n)
		if err != nil {
			t.Fatalf("DecodeJSON: %v", err)
		}
		if !sameJSON(t, out, []byte(in)) {
			t.Errorf("round trip = %s, want %s", out, in)
		}
	}
	keySet := `{"groupKeySetId":1,"groupKeySecurityPolicy":0,"epochKey0":"hex:00000000000000000000000000000000",` +
		`"epochStartTime0":1,"epochKey1":null,"epochStartTime1":null,"epochKey2":null,"epochStartTime2":null}`
	reset := `{"importedResetTimestamp":1000,"exportedResetTimestamp":2000}`
	attr := func(def *spec.Cluster, id uint32) *spec.Field { return spec.AttributeField(def.Attribute(id)) }
	t.Run("preserves epoch-us fields in GroupKeySetStruct", func(t *testing.T) {
		roundTrip(t, &spec.Field{Name: "GroupKeySet", Type: spec.Type{Kind: spec.KindStruct, Struct: gkmdef.GroupKeySetStructDef}}, keySet)
	})
	t.Run("round-trips CumulativeEnergyResetStruct", func(t *testing.T) {
		roundTrip(t, attr(eemdef.Definition, eemdef.AttrCumulativeEnergyReset), reset)
	})
	t.Run("scalar (enum) attribute round-trips null", func(t *testing.T) {
		roundTrip(t, attr(onoffdef.Definition, onoffdef.AttrStartUpOnOff), `null`)
	})
	t.Run("scalar (enum) attribute round-trips non-null values", func(t *testing.T) {
		roundTrip(t, attr(onoffdef.Definition, onoffdef.AttrStartUpOnOff), `0`)
		roundTrip(t, attr(onoffdef.Definition, onoffdef.AttrStartUpOnOff), `2`)
	})
	t.Run("struct attribute round-trips null", func(t *testing.T) {
		roundTrip(t, attr(eemdef.Definition, eemdef.AttrCumulativeEnergyReset), `null`)
	})
	t.Run("struct attribute round-trips non-null value", func(t *testing.T) {
		roundTrip(t, attr(eemdef.Definition, eemdef.AttrCumulativeEnergyReset), reset)
	})
	t.Run("non-nullable conditional attribute round-trips false and true", func(t *testing.T) {
		f := attr(onoffdef.Definition, onoffdef.AttrGlobalSceneControl)
		roundTrip(t, f, `false`)
		roundTrip(t, f, `true`)
		if _, err := spec.EncodeJSON(f, json.RawMessage(`null`)); err == nil {
			t.Error("GlobalSceneControl encoded null; it is not nullable")
		}
	})
	t.Run("returns TlvAny for attribute typed as any", func(t *testing.T) {
		roundTrip(t, &spec.Field{Name: "unknown_1"}, `{"1":"a"}`)
	})
	t.Run("preserves epoch-us fields in KeySetWrite", func(t *testing.T) {
		roundTrip(t, spec.CommandField(gkmdef.Definition.CommandByName("KeySetWrite")), `{"groupKeySet":`+keySet+`}`)
	})
}

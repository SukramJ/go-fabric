// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec

import (
	"context"
	"reflect"
	"testing"
)

// The branches of the conformance compiler, the response check and the
// default patcher that the ported matter.js tables do not reach: static
// operands on either side, element names, malformed nodes, values of other
// shapes.

var ctDef = &Cluster{
	ID: 0xFFF1FC0B, Name: "CT", Revision: 2,
	Features: []Feature{{Name: "F", Title: "Foo", Bit: 0, Conformance: Conformance{Op: ConfOptional}}},
	Commands: []Command{
		{ID: 1, Name: "Go", Direction: Request, Response: "status"},
		{ID: 2, Name: "Ask", Direction: Request, Response: "Answer"},
		{ID: 3, Name: "Answer", Direction: Response},
	},
	Events:     []Event{{ID: 0, Name: "Happened"}},
	Attributes: []Attribute{{ID: 0, Name: "Peer", Type: Type{Kind: KindUint, Bits: 8}, Conformance: Conformance{Op: ConfMandatory}}},
}

func ctInstance(t *testing.T, features uint32) *Instance {
	t.Helper()
	inst, err := New(ctDef, Options{Features: features})
	if err != nil {
		t.Fatal(err)
	}
	return inst
}

func cName(n string) Conformance { return Conformance{Op: ConfName, Name: n} }

func cOp(op ConformanceOp, args ...Conformance) Conformance { return Conformance{Op: op, Args: args} }

type ctRecord struct {
	Value *any
	Opts  *ctOpts
}

type ctOpts struct{ Enable *any }

// evalStatic compiles c over the record struct and evaluates it.
func evalStatic(t *testing.T, features uint32, c Conformance, rec ctRecord, scope nameScope) confNode {
	t.Helper()
	s := &Struct{Name: "R", Fields: []Field{{ID: 0, Name: "Value"}, {ID: 1, Name: "Opts"}}}
	cc := &confCompiler{inst: ctInstance(t, features), fields: s}
	loc := &confLocation{siblings: reflect.ValueOf(rec), fields: s, choices: map[string]*confChoice{}, scope: scope}
	return evaluateNode(cc.compile(c), confValue{}, loc)
}

func some(v any) *any { return &v }

func TestCompilerBranches(t *testing.T) {
	t.Parallel()
	set := ctRecord{Value: some(uint8(1))}
	empty := ctRecord{}
	for _, c := range []struct {
		name     string
		features uint32
		conf     Conformance
		rec      ctRecord
		want     confCode
	}{
		{"dynamic & static true", 1, cOp(ConfAnd, cName("Value"), cName("F")), set, codeConformant},
		{"dynamic & static false", 0, cOp(ConfAnd, cName("Value"), cName("F")), set, codeNonconformant},
		{"static true | dynamic", 1, cOp(ConfOr, cName("F"), cName("Value")), empty, codeConformant},
		{"static false | dynamic", 0, cOp(ConfOr, cName("F"), cName("Value")), set, codeConformant},
		{"dynamic | static true", 1, cOp(ConfOr, cName("Value"), cName("F")), empty, codeConformant},
		{"dynamic | static false", 0, cOp(ConfOr, cName("Value"), cName("F")), empty, codeNonconformant},
		{"static true ^ dynamic", 1, cOp(ConfXor, cName("F"), cName("Value")), set, codeNonconformant},
		{"static false ^ dynamic", 0, cOp(ConfXor, cName("F"), cName("Value")), set, codeConformant},
		{"dynamic ^ static true", 1, cOp(ConfXor, cName("Value"), cName("F")), empty, codeConformant},
		{"dynamic ^ static false", 0, cOp(ConfXor, cName("Value"), cName("F")), empty, codeNonconformant},
		{"static value & static value", 0, cOp(ConfAnd, Conformance{Op: ConfValue, Value: "1"}, Conformance{Op: ConfValue, Value: "x"}), empty, codeConformant},
		{"static false & static", 0, cOp(ConfAnd, cName("F"), Conformance{Op: ConfValue, Value: "1"}), empty, codeNonconformant},
		{"command name", 0, cName("Go"), empty, codeConformant},
		{"event name", 0, cName("Happened"), empty, codeConformant},
		{"revision met", 0, Conformance{Op: ConfRevision, Rev: 2}, empty, codeConformant},
		{"!F", 0, cOp(ConfNot, cName("F")), empty, codeConformant},
		{"!F enabled", 1, cOp(ConfNot, cName("F")), empty, codeNonconformant},
		{"unknown op", 0, Conformance{Op: "?"}, empty, codeOptional},
		{"group with trailing D", 0, cOp(ConfOtherwise, cName("F"), cName("Value"), Conformance{Op: ConfDeprecated}), set, codeValue},
		{"group all nonconformant", 0, cOp(ConfOtherwise, cName("F"), cName("F")), empty, codeNonconformant},
		{"group runtime then optional", 0, cOp(ConfOtherwise, cOp(ConfAnd, cName("Value"), cName("Value")), Conformance{Op: ConfOptional}), empty, codeOptional},
		{"dot rhs not a name", 0, cOp(ConfDot, cName("Opts"), Conformance{Op: ConfValue, Value: "1"}), empty, codeValue},
		{"dot on static value", 0, cOp(ConfDot, Conformance{Op: ConfValue, Value: "1"}, cName("Enable")), empty, codeValue},
		{"dot on null value", 0, cOp(ConfDot, Conformance{Op: ConfValue, Value: "null"}, cName("Enable")), empty, codeValue},
		{"dot on a conformant node", 0, cOp(ConfDot, Conformance{Op: ConfMandatory}, cName("Enable")), empty, codeValue},
		{"dot on a sibling struct", 0, cOp(ConfDot, cName("Opts"), cName("Enable")), ctRecord{Opts: &ctOpts{Enable: some(true)}}, codeValue},
		{"dot on an absent sibling", 0, cOp(ConfDot, cName("Opts"), cName("Enable")), empty, codeValue},
		{"compare a non-value", 0, cOp(ConfEq, Conformance{Op: ConfMandatory}, Conformance{Op: ConfValue, Value: "1"}), empty, codeNonconformant},
		{"compare strings ==", 0, cOp(ConfEq, Conformance{Op: ConfValue, Value: `"a"`}, Conformance{Op: ConfValue, Value: `"a"`}), empty, codeConformant},
		{"compare strings !=", 0, cOp(ConfNe, Conformance{Op: ConfValue, Value: `"a"`}, Conformance{Op: ConfValue, Value: `"b"`}), empty, codeConformant},
		{"compare strings >", 0, cOp(ConfGt, Conformance{Op: ConfValue, Value: `"a"`}, Conformance{Op: ConfValue, Value: `"b"`}), empty, codeNonconformant},
		{"compare numbers <=", 0, cOp(ConfLte, Conformance{Op: ConfValue, Value: "1"}, Conformance{Op: ConfValue, Value: "2.5"}), empty, codeConformant},
		{"compare numbers <", 0, cOp(ConfLt, Conformance{Op: ConfValue, Value: "1"}, Conformance{Op: ConfValue, Value: "0x02"}), empty, codeConformant},
		{"compare int sibling", 0, cOp(ConfGte, cName("Value"), Conformance{Op: ConfValue, Value: "-1"}), ctRecord{Value: some(int16(-1))}, codeConformant},
		{"compare float sibling", 0, cOp(ConfEq, cName("Value"), Conformance{Op: ConfValue, Value: "0.5"}), ctRecord{Value: some(float32(0.5))}, codeConformant},
		{"choice without parameter", 0, Conformance{Op: ConfChoice, Args: []Conformance{cName("Value")}}, set, codeValue},
		{"choice gated off", 0, Conformance{Op: ConfChoice, Choice: &Choice{Name: "a", Num: 1}, Args: []Conformance{cName("F")}}, empty, codeNonconformant},
		{"choice dynamic gate off", 0, Conformance{Op: ConfChoice, Choice: &Choice{Name: "a", Num: 1}, Args: []Conformance{cOp(ConfAnd, cName("Value"), cName("Value"))}}, empty, codeNonconformant},
		{"optional-if static", 0, cOp(ConfOptionalIf, cName("F")), empty, codeNonconformant},
		{"attribute without peers", 0, cName("Peer"), empty, codeValue},
		{"outer without resolver", 0, cName("Elsewhere"), empty, codeValue},
	} {
		if got := evalStatic(t, c.features, c.conf, c.rec, nameScope{}).code; got != c.want {
			t.Errorf("%s: code %d, want %d", c.name, got, c.want)
		}
	}
	// A name the cluster's attribute answers through peers, and one the
	// outer resolver answers.
	scope := nameScope{peers: func(uint32) (any, bool) { return uint8(3), true }, outer: func(string) (any, bool) { return 1, true }}
	if n := evalStatic(t, 0, cName("Peer"), ctRecord{}, scope); !n.value.defined {
		t.Error("the peer attribute is not resolved")
	}
	if n := evalStatic(t, 0, cName("Elsewhere"), ctRecord{}, scope); !n.value.defined {
		t.Error("the outer name is not resolved")
	}
	if n := asNonvalue(confNode{code: codeValue, value: confValue{v: 1, defined: true}}); n.code != codeConformant {
		t.Errorf("asNonvalue(defined) = %d", n.code)
	}
	if n := asNonvalue(confNode{code: codeValue}); n.code != codeNonconformant {
		t.Errorf("asNonvalue(undefined) = %d", n.code)
	}
	if lit := literalOf("word"); lit.v != "word" {
		t.Errorf("literal %v", lit.v)
	}
	if _, ok := numberOf("x"); ok {
		t.Error("a string is a number")
	}
}

func TestFieldValueShapes(t *testing.T) {
	t.Parallel()
	if v := fieldValueOf(reflect.Value{}); v.defined {
		t.Error("an invalid field is defined")
	}
	if v := fieldValueOf(reflect.ValueOf(NullOf[uint8]())); !v.defined || v.v != nil {
		t.Errorf("a null Nullable = %+v", v)
	}
	if v := fieldValueOf(reflect.ValueOf(ValueOf(uint8(4)))); v.v != uint8(4) {
		t.Errorf("a Nullable value = %+v", v)
	}
	var nilOpts *ctOpts
	if f := fieldOfStruct(reflect.ValueOf(nilOpts), "Enable"); f.IsValid() {
		t.Error("a nil pointer has a field")
	}
	if f := fieldOfStruct(reflect.ValueOf(3), "Enable"); f.IsValid() {
		t.Error("an int has a field")
	}
	if f := fieldOfStruct(reflect.ValueOf(ctOpts{}), "Missing"); f.IsValid() {
		t.Error("a missing field was found")
	}
}

type ctEnumRec struct{ Mode *any }

func TestEnumAndValidatorBranches(t *testing.T) {
	t.Parallel()
	e := &Enum{Name: "E", Bits: 8, Values: []EnumValue{
		{Value: 0, Name: "Zero"},
		{Value: 1, Name: "Chosen", Conformance: Conformance{Op: ConfChoice, Choice: &Choice{Name: "c", Num: 1}, Args: []Conformance{cName("F")}}},
		{Value: 2, Name: "ByPeer", Conformance: cName("Peer")},
	}, Ranges: []EnumRange{{Name: "Low", Min: 10, Max: 20, Conformance: cName("F")}}}
	field := Field{ID: 0, Name: "Mode", Type: Type{Kind: KindEnum, Bits: 8, Enum: e}, Quality: Quality{Nullable: true}, Conformance: Conformance{Op: ConfMandatory}}
	s := &Struct{Name: "R", Fields: []Field{field}}
	inst := ctInstance(t, 0)
	check := func(rec ctEnumRec, scope nameScope) error {
		scope.conformance, scope.enums = true, true
		return inst.checkFieldConformance("R", s, reflect.ValueOf(rec), scope)
	}
	for _, c := range []struct {
		name string
		v    any
		ok   bool
	}{
		{"defined", uint8(0), true},
		{"choice member under no feature", uint8(1), false},
		{"peer-gated member without peers", uint8(2), false},
		{"range member gated off", uint8(15), false},
		{"null", nil, true},
		{"not a number", "x", true},
		{"negative", int8(-1), true},
	} {
		if err := check(ctEnumRec{Mode: some(c.v)}, nameScope{}); (err == nil) != c.ok {
			t.Errorf("%s: %v, want ok %v", c.name, err, c.ok)
		}
	}
	if err := check(ctEnumRec{Mode: some(uint8(2))}, nameScope{peers: func(uint32) (any, bool) { return uint8(1), true }}); err != nil {
		t.Errorf("peer-gated member with the peer: %v", err)
	}
	// An enum with no members enforces nothing.
	bare := Field{ID: 0, Name: "Mode", Type: Type{Kind: KindEnum, Bits: 8, Enum: &Enum{Name: "B"}}}
	if err := inst.checkFieldConformance("R", &Struct{Name: "R", Fields: []Field{bare}}, reflect.ValueOf(ctEnumRec{Mode: some(uint8(9))}), nameScope{conformance: true, enums: true}); err != nil {
		t.Errorf("a bare enum: %v", err)
	}
	// Static value conformances, a deprecated untyped field and an or-less
	// choice group.
	fields := []Field{
		{ID: 0, Name: "Mode", Conformance: Conformance{Op: ConfDeprecated}},
		{ID: 1, Name: "Value", Type: Type{Kind: KindUint, Bits: 8}, Conformance: Conformance{Op: ConfValue, Value: "1"}},
		{ID: 2, Name: "Other", Type: Type{Kind: KindUint, Bits: 8}, Conformance: Conformance{Op: ConfValue, Value: "1"}, Quality: Quality{Nullable: true}},
		{ID: 3, Name: "Gone", Type: Type{Kind: KindUint, Bits: 8}, Conformance: cName("Missing")},
		{ID: 4, Name: "Pick", Type: Type{Kind: KindUint, Bits: 8}, Conformance: Conformance{Op: ConfChoice, Choice: &Choice{Name: "x", Num: 2, OrLess: true}, Args: []Conformance{{Op: ConfOptional}}}},
	}
	type rec struct{ Mode, Value, Other, Gone, Pick *any }
	if err := inst.checkFieldConformance("R", &Struct{Name: "R", Fields: fields}, reflect.ValueOf(rec{Value: some(1)}), nameScope{conformance: true}); err != nil {
		t.Errorf("static values: %v", err)
	}
	if err := inst.checkFieldConformance("R", &Struct{Name: "R", Fields: fields}, reflect.ValueOf(rec{}), nameScope{conformance: true}); err == nil {
		t.Error("a static value conformance does not require its field")
	}
}

type ctAnswer struct{ Yes *any }

func TestResponseCheckShapes(t *testing.T) {
	t.Parallel()
	def := &Cluster{ID: 0xFFF1FC0C, Name: "Q", Revision: 1, Commands: []Command{
		{ID: 1, Name: "Go", Direction: Request, Response: "status"},
		{ID: 2, Name: "Ask", Direction: Request, Response: "Answer"},
		{ID: 3, Name: "Answer", Direction: Response, Fields: []Field{{ID: 0, Name: "Yes", Conformance: Conformance{Op: ConfMandatory}, Type: Type{Kind: KindBool}}}},
	}}
	inst, err := New(def, Options{Commands: []uint32{1, 2}})
	if err != nil {
		t.Fatal(err)
	}
	if err := inst.checkResponse(9, nil, ctAnswer{}, nil); err != nil {
		t.Errorf("an unknown command: %v", err)
	}
	if err := inst.checkResponse(1, nil, ctAnswer{}, nil); err != nil {
		t.Errorf("a status-only command: %v", err)
	}
	if err := inst.checkResponse(2, nil, ctAnswer{}, nil); err == nil {
		t.Error("an answer without Yes was accepted")
	}
	if err := inst.checkResponse(2, nil, ctAnswer{Yes: some(true)}, nil); err != nil {
		t.Errorf("an answer: %v", err)
	}
	if err := inst.CheckConformance(9, 1, nil); err != nil {
		t.Error(err)
	}
}

type ctPatchStruct struct {
	A *uint8
	B *uint8
	C *[]uint8
	D *uint8
	E *uint8
	F *uint8
	L []ctPatchInner
	N *ctPatchInner
}

type ctPatchInner struct{ X *uint8 }

func TestPatchBranches(t *testing.T) {
	t.Parallel()
	inner := &Struct{Name: "I", Fields: []Field{{ID: 0, Name: "X", Type: Type{Kind: KindUint, Bits: 8}, Conformance: Conformance{Op: ConfMandatory}, Default: float64(3)}}}
	s := &Struct{Name: "P", Fields: []Field{
		{ID: 0, Name: "A", Type: Type{Kind: KindUint, Bits: 8}, Conformance: Conformance{Op: ConfOptional}, Default: float64(1)},
		{ID: 1, Name: "B", Type: Type{Kind: KindUint, Bits: 8}, Conformance: Conformance{Op: ConfMandatory}},
		{ID: 2, Name: "C", Type: Type{Kind: KindUint, Bits: 8}, Conformance: Conformance{Op: ConfMandatory}, Default: EmptyListDefault{}},
		{ID: 3, Name: "D", Type: Type{Kind: KindUint, Bits: 8}, Conformance: Conformance{Op: ConfMandatory}, Default: NullDefault{}},
		{ID: 4, Name: "E", Type: Type{Kind: KindUint, Bits: 8}, Conformance: Conformance{Op: ConfMandatory}, Default: "text"},
		{ID: 5, Name: "F", Type: Type{Kind: KindUint, Bits: 8}, Conformance: Conformance{Op: ConfMandatory}, Default: float64(-1)},
		{ID: 6, Name: "L", Type: Type{Kind: KindList, Entry: &Field{Type: Type{Kind: KindStruct, Struct: inner}}}, Conformance: Conformance{Op: ConfMandatory}},
		{ID: 7, Name: "N", Type: Type{Kind: KindStruct, Struct: inner}, Conformance: Conformance{Op: ConfMandatory}},
		{ID: 8, Name: "Absent", Type: Type{Kind: KindUint, Bits: 8}, Conformance: Conformance{Op: ConfMandatory}, Default: float64(1)},
	}}
	def := &Cluster{ID: 0xFFF1FC0D, Name: "P", Revision: 1, Attributes: []Attribute{
		{ID: 0, Name: "V", Type: Type{Kind: KindStruct, Struct: s}, Conformance: Conformance{Op: ConfMandatory}},
		{ID: 1, Name: "L", Type: Type{Kind: KindList, Entry: &Field{Type: Type{Kind: KindStruct, Struct: inner}}}, Conformance: Conformance{Op: ConfMandatory}},
		{ID: 2, Name: "S", Type: Type{Kind: KindStruct}, Conformance: Conformance{Op: ConfMandatory}},
	}}
	inst, err := New(def, Options{})
	if err != nil {
		t.Fatal(err)
	}
	in := ctPatchStruct{L: []ctPatchInner{{}}, N: &ctPatchInner{}}
	out, err := inst.PatchDefaults(0, in)
	if err != nil {
		t.Fatal(err)
	}
	got := out.(ctPatchStruct)
	if got.A != nil || got.B != nil || got.C == nil || len(*got.C) != 0 || got.D != nil || got.E != nil || got.F != nil || *got.L[0].X != 3 || *got.N.X != 3 {
		t.Errorf("patched %+v", got)
	}
	if in.L[0].X != nil || in.N.X != nil {
		t.Error("the value handed over was changed")
	}
	for _, v := range []any{(*ctPatchStruct)(nil), 7, []ctPatchInner(nil), struct{}{}} {
		if _, err := inst.PatchDefaults(0, v); err != nil {
			t.Errorf("%T: %v", v, err)
		}
	}
	if _, err := inst.PatchDefaults(1, "not a list"); err != nil {
		t.Error(err)
	}
	if _, err := inst.PatchDefaults(2, struct{}{}); err != nil {
		t.Error(err)
	}
	var iface any = ctPatchStruct{}
	if _, err := inst.PatchDefaults(0, &iface); err != nil {
		t.Error(err)
	}
	// A refusal inside a list entry and a nested struct propagates.
	type stray struct{ Y *uint8 }
	if _, err := inst.PatchDefaults(1, []stray{{}}); err == nil {
		t.Error("a stray entry field was accepted")
	}
	type outer struct{ N *stray }
	if _, _, err := inst.patchStruct(featuresOnly{Context(def, 0)}, "V", &Struct{Name: "O", Fields: []Field{{Name: "N", Type: Type{Kind: KindStruct, Struct: inner}}}}, reflect.ValueOf(outer{N: &stray{}})); err == nil {
		t.Error("a stray nested field was accepted")
	}
	srv, err := NewServer(def, Options{}, ServerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Set(1, []stray{{}}); err == nil {
		t.Error("Set accepted a stray entry field")
	}
	_ = context.Background
}

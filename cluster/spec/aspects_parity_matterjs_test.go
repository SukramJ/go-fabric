// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
)

// Parity with matter.js packages/model/test/aspects at 862ad75f — the
// runtime half of the model aspects. matter.js parses an aspect's text
// (Access, Conformance, Constraint, Quality) into its model; the module
// receives the parsed form in parity/schema.json and evaluates it in
// cluster/spec.

// TestConformanceApplicabilityParityMatterJS mirrors
// packages/model/test/aspects/ConformanceTest.ts:701-805 at 862ad75f
// (describe "applicability", features AA and BB defined) and :807-827
// (describe "provisionality", isMandatory).
func TestConformanceApplicabilityParityMatterJS(t *testing.T) {
	t.Parallel()
	const (
		none = spec.ApplicabilityNone
		opt  = spec.ApplicabilityOptional
		cond = spec.ApplicabilityConditional
		mand = spec.ApplicabilityMandatory
	)
	ctx := func(supported ...string) features {
		f := features{"AA": false, "BB": false}
		for _, s := range supported {
			f[s] = true
		}
		return f
	}
	m, o, p, x, z, d := op(spec.ConfMandatory), op(spec.ConfOptional), op(spec.ConfProvisional), op(spec.ConfDisallowed), op(spec.ConfObsolete), op(spec.ConfDeprecated)
	aa, bb := name("AA"), name("BB")
	or := func(cs ...spec.Conformance) spec.Conformance { return op(spec.ConfOtherwise, cs...) }
	eq := op(spec.ConfEq, name("SomeField"), name("SomeValue"))
	for _, c := range []struct {
		it   string
		c    spec.Conformance
		f    features
		want spec.Applicability
	}{
		{"resolves plain flags: M", m, ctx(), mand},
		{"resolves plain flags: O", o, ctx(), opt},
		{"resolves plain flags: X", x, ctx(), none},
		{"resolves plain flags: Z", z, ctx(), none},
		{"resolves plain flags: D", d, ctx(), none},
		{"resolves features: AA supported", aa, ctx("AA"), mand},
		{"resolves features: AA unsupported", aa, ctx(), none},
		{"resolves features: AA, O supported", or(aa, o), ctx("AA"), mand},
		{"resolves features: AA, O unsupported", or(aa, o), ctx(), opt},
		{"makes a standalone provisional element optional", p, ctx(), opt},
		{"caps an intended mandatory conformance at optional", or(p, m), ctx(), opt},
		{"caps an intended feature conformance at optional: P, AA", or(p, aa), ctx("AA"), opt},
		{"caps an intended feature conformance at optional: P, AA & BB", or(p, op(spec.ConfAnd, aa, bb)), ctx("AA", "BB"), opt},
		{"excludes an intended feature conformance whose feature is unsupported: P, AA", or(p, aa), ctx(), none},
		{"excludes an intended feature conformance whose feature is unsupported: P, AA & BB", or(p, op(spec.ConfAnd, aa, bb)), ctx("AA"), none},
		{"leaves an intended optional conformance optional: P, O", or(p, o), ctx(), opt},
		{"leaves an intended optional conformance optional: P, [AA & BB]", or(p, op(spec.ConfOptionalIf, op(spec.ConfAnd, aa, bb))), ctx("AA", "BB"), opt},
		{"leaves an intended conditional conformance conditional", or(p, op(spec.ConfDesc)), ctx(), cond},
		{"does not cap a mandatory term preceding the provisional term", or(aa, p), ctx("AA"), mand},
		{"leaves a term that compares a value conditional: ==", eq, ctx(), cond},
		{"leaves a term that compares a value conditional: Rev", spec.Conformance{Op: spec.ConfRevision, Rev: 3}, ctx(), cond},
		{"leaves a term that compares a value conditional: == & !AA", op(spec.ConfAnd, eq, op(spec.ConfNot, aa)), ctx(), cond},
		{"leaves a term that compares a value conditional: == & AA", op(spec.ConfAnd, eq, aa), ctx("AA"), cond},
		{"leaves a term that compares a value conditional: !(==)", op(spec.ConfNot, eq), ctx(), cond},
	} {
		if got := c.c.Applicability(c.f); got != c.want {
			t.Errorf("%s: applicability %d, want %d", c.it, got, c.want)
		}
	}
	for _, c := range []struct {
		it   string
		c    spec.Conformance
		f    features
		want spec.Applicability
	}{
		{"makes a deprecated element optional", d, ctx(), opt},
		{"makes an obsolete element optional", z, ctx(), opt},
		{"decides a list ending in deprecated by the terms before it: [AA], D", or(op(spec.ConfOptionalIf, aa), d), ctx(), none},
		{"decides a list ending in deprecated by the terms before it: [AA], D supported", or(op(spec.ConfOptionalIf, aa), d), ctx("AA"), opt},
		{"decides a list ending in deprecated by the terms before it: AA, D", or(aa, d), ctx(), none},
		{"decides a list ending in deprecated by the terms before it: AA, D supported", or(aa, d), ctx("AA"), mand},
		{"decides a list ending in deprecated by the terms before it: SomeField, D", or(name("SomeField"), d), ctx(), cond},
		{"makes a deprecated term that does not end the list optional", or(d, aa), ctx(), opt},
		{"leaves a disallowed element excluded: X", x, ctx(), none},
		{"leaves a disallowed element excluded: AA, X", or(aa, x), ctx(), none},
	} {
		if got := c.c.ApplicabilityDeprecatedOptional(c.f); got != c.want {
			t.Errorf("with deprecated elements optional, %s: applicability %d, want %d", c.it, got, c.want)
		}
	}
	// provisionality: isMandatory stops at whichever of M and P comes first.
	for _, c := range []struct {
		c    spec.Conformance
		want bool
	}{
		{or(p, m), false},
		{or(d, p, m), false},
		{or(m, p), true},
	} {
		if got := c.c.IsMandatory(); got != c.want {
			t.Errorf("does not make an element optional where a mandatory term precedes the provisional one: IsMandatory = %t, want %t", got, c.want)
		}
	}
}

// TestAccessParityMatterJS mirrors
// packages/model/test/aspects/AccessTest.ts:63-84 at 862ad75f (describe
// "readable" and "writable"): R makes an element readable and W
// writable, whatever fabric and privilege flags stand beside them.
func TestAccessParityMatterJS(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		it       string
		a        spec.Access
		readable bool
		writable bool
	}{
		{"set by R", spec.Access{RW: "R"}, true, false},
		{"set by R (R F A)", spec.Access{RW: "R", Fabric: "F", Read: spec.PrivilegeAdminister}, true, false},
		{"set by W", spec.Access{RW: "W"}, false, true},
		{"set by W (W F A)", spec.Access{RW: "W", Fabric: "F", Write: spec.PrivilegeAdminister}, false, true},
	} {
		if got := c.a.Readable(); got != c.readable {
			t.Errorf("readable %s: %t, want %t", c.it, got, c.readable)
		}
		if got := c.a.Writable(); got != c.writable {
			t.Errorf("writable %s: %t, want %t", c.it, got, c.writable)
		}
	}
}

// TestConstraintValuesParityMatterJS mirrors the value tests of
// packages/model/test/aspects/ConstraintTest.ts:23-87 at 862ad75f
// ("accepts"/"rejects" over "0", "4", "-4", "min 4", "max 4" and
// "4 to 44"), judged by a definition's CheckValue. The bigint value tests
// beyond 2^53 are a findings-register row (float64 bounds).
func TestConstraintValuesParityMatterJS(t *testing.T) {
	t.Parallel()
	b := func(n int64) *spec.Bound { return &spec.Bound{Int: n} }
	i64 := spec.Type{Kind: spec.KindInt, Bits: 64}
	def := &spec.Cluster{ID: 0xFFF1_FC34, Name: "Constraint", Revision: 1, Attributes: []spec.Attribute{
		rw(0, "Zero", i64, spec.Quality{}, spec.Constraint{Text: "0", Value: b(0)}),
		rw(1, "Four", i64, spec.Quality{}, spec.Constraint{Text: "4", Value: b(4)}),
		rw(2, "MinusFour", i64, spec.Quality{}, spec.Constraint{Text: "-4", Value: b(-4)}),
		rw(3, "Min", i64, spec.Quality{}, spec.Constraint{Text: "min 4", Min: b(4)}),
		rw(4, "Max", i64, spec.Quality{}, spec.Constraint{Text: "max 4", Max: b(4)}),
		rw(5, "Range", i64, spec.Quality{}, spec.Constraint{Text: "4 to 44", Min: b(4), Max: b(44)}),
	}}
	inst := mustInstance(t, def, 0)
	for _, c := range []struct {
		attr  uint32
		value int64
		ok    bool
	}{
		{0, 0, true},
		{0, 1, false},
		{1, 4, true},
		{1, 3, false},
		{2, -4, true},
		{2, 4, false},
		{3, 5, true},
		{3, 4, true},
		{3, 3, false},
		{4, 3, true},
		{4, 4, true},
		{4, 5, false},
		{5, 3, false},
		{5, 4, true},
		{5, 24, true},
		{5, 44, true},
		{5, 45, false},
	} {
		_, err := inst.CheckValue(c.attr, c.value, nil)
		if (err == nil) != c.ok {
			t.Errorf("%q against %d: %v, want ok %t", def.Attributes[c.attr].Constraint.Text, c.value, err, c.ok)
		}
	}
}

// TestRequirementApplicabilityParityMatterJS mirrors
// packages/model/test/logic/RequirementApplicabilityTest.ts:10-46 at
// 862ad75f: a requirement's conformance judged by the names that hold
// (Sit) among the names known (Sit, Lit, Cooler); a name not known is
// left conditional.
func TestRequirementApplicabilityParityMatterJS(t *testing.T) {
	t.Parallel()
	known := func(trueNames ...string) features {
		f := features{"Sit": false, "Lit": false, "Cooler": false}
		for _, n := range trueNames {
			f[n] = true
		}
		return f
	}
	or := func(cs ...spec.Conformance) spec.Conformance { return op(spec.ConfOr, cs...) }
	for _, c := range []struct {
		it   string
		c    spec.Conformance
		f    features
		want spec.Applicability
	}{
		{"makes a mandatory requirement mandatory", op(spec.ConfMandatory), known(), spec.ApplicabilityMandatory},
		{"makes a disallowed requirement inapplicable", op(spec.ConfDisallowed), known(), spec.ApplicabilityNone},
		{"applies a satisfied name", or(name("Sit"), name("Lit")), known("Sit"), spec.ApplicabilityMandatory},
		{"does not apply an unsatisfied name", or(name("Sit"), name("Lit")), known(), spec.ApplicabilityNone},
		{"leaves a name it cannot judge conditional", name("Ethernet"), features{"Sit": false}, spec.ApplicabilityConditional},
		{"leaves desc conditional", op(spec.ConfDesc), known(), spec.ApplicabilityConditional},
		{"keeps an optional fallback optional", op(spec.ConfOtherwise, name("Cooler"), op(spec.ConfOptional)), known(), spec.ApplicabilityOptional},
	} {
		if got := c.c.Applicability(c.f); got != c.want {
			t.Errorf("%s: applicability %d, want %d", c.it, got, c.want)
		}
	}
}

// TestEffectiveAccessParityMatterJS mirrors
// packages/model/test/logic/EffectiveAccessTest.ts:33-53 at 862ad75f on
// the access matter.js computes: the extractor writes each element's
// effective access into parity/schema.json and script/clustergen into the
// definition, and cluster/spec reads it. An attribute stating only "A" is
// "R A", read-only; a command stating only "O" keeps O to invoke; an
// attribute stating nothing is "RW"; a field stating only fabric access
// inherits the write of its "RW VM" attribute.
func TestEffectiveAccessParityMatterJS(t *testing.T) {
	t.Parallel()
	administered := spec.Access{RW: "R", Read: spec.PrivilegeAdminister}
	if administered.Writable() || administered.ReadPrivilege() != spec.PrivilegeAdminister {
		t.Error("does not grant write to an attribute that states only a privilege")
	}
	if (spec.Access{Write: spec.PrivilegeOperate}).WritePrivilege() != spec.PrivilegeOperate {
		t.Error("retains the invoke privilege of a command that states only a privilege")
	}
	if !(spec.Access{RW: "RW"}).Writable() {
		t.Error("grants write to an attribute that states no access at all")
	}
	if !(spec.Access{RW: "RW", Fabric: "S", Read: spec.PrivilegeView, Write: spec.PrivilegeManage}).Writable() {
		t.Error("inherits read and write into a nested value that states only fabric access")
	}
}

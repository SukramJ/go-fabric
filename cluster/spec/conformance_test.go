// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
)

// features is a FeatureContext over named features.
type features map[string]bool // defined name → supported

func (f features) Defined(name string) bool   { _, ok := f[name]; return ok }
func (f features) Supported(name string) bool { return f[name] }

func name(n string) spec.Conformance { return spec.Conformance{Op: spec.ConfName, Name: n} }

func op(o spec.ConformanceOp, args ...spec.Conformance) spec.Conformance {
	return spec.Conformance{Op: o, Args: args}
}

// TestApplicability mirrors matter.js's computeApplicability
// (packages/model/src/aspects/Conformance.ts) case by case.
func TestApplicability(t *testing.T) {
	t.Parallel()
	ctx := features{"A": true, "B": false}
	const (
		none = spec.ApplicabilityNone
		opt  = spec.ApplicabilityOptional
		cond = spec.ApplicabilityConditional
		mand = spec.ApplicabilityMandatory
	)
	cases := []struct {
		name string
		c    spec.Conformance
		want spec.Applicability
	}{
		{"empty", spec.Conformance{}, opt},
		{"M", op(spec.ConfMandatory), mand},
		{"O", op(spec.ConfOptional), opt},
		{"P", op(spec.ConfProvisional), opt},
		{"X", op(spec.ConfDisallowed), none},
		{"D", op(spec.ConfDeprecated), none},
		{"Z", op(spec.ConfObsolete), none},
		{"desc", op(spec.ConfDesc), cond},
		{"Rev >= v2", spec.Conformance{Op: spec.ConfRevision, Rev: 2}, cond},
		{"supported feature", name("A"), mand},
		{"unsupported feature", name("B"), none},
		{"element name", name("Other"), cond},
		{"[A]", op(spec.ConfOptionalIf, name("A")), opt},
		{"[B]", op(spec.ConfOptionalIf, name("B")), none},
		{"[Other]", op(spec.ConfOptionalIf, name("Other")), cond},
		{"!A", op(spec.ConfNot, name("A")), none},
		{"!B", op(spec.ConfNot, name("B")), mand},
		{"!Other", op(spec.ConfNot, name("Other")), cond},
		{"A & B", op(spec.ConfAnd, name("A"), name("B")), none},
		{"A & Other", op(spec.ConfAnd, name("A"), name("Other")), cond},
		{"A & A", op(spec.ConfAnd, name("A"), name("A")), mand},
		{"A | B", op(spec.ConfOr, name("A"), name("B")), mand},
		{"B | B", op(spec.ConfOr, name("B"), name("B")), none},
		{"B | Other", op(spec.ConfOr, name("B"), name("Other")), cond},
		{"A ^ B", op(spec.ConfXor, name("A"), name("B")), cond},
		{"Status == Success", op(spec.ConfEq, name("Status"), name("Success")), cond},
		{"O.a+", spec.Conformance{Op: spec.ConfChoice, Choice: &spec.Choice{Name: "a", Num: 1, OrMore: true}, Args: []spec.Conformance{op(spec.ConfOptional)}}, opt},
		{"choice without expression", spec.Conformance{Op: spec.ConfChoice, Choice: &spec.Choice{Name: "a", Num: 1}}, opt},
		{"A, [B]", op(spec.ConfOtherwise, name("A"), op(spec.ConfOptionalIf, name("B"))), mand},
		{"B, [A]", op(spec.ConfOtherwise, name("B"), op(spec.ConfOptionalIf, name("A"))), opt},
		{"B, Other", op(spec.ConfOtherwise, name("B"), name("Other")), cond},
		{"Other, O", op(spec.ConfOtherwise, name("Other"), op(spec.ConfOptional)), cond},
		{"B, X", op(spec.ConfOtherwise, name("B"), op(spec.ConfDisallowed)), none},
		{"P, M", op(spec.ConfOtherwise, op(spec.ConfProvisional), op(spec.ConfMandatory)), opt},
		{"A, D", op(spec.ConfOtherwise, op(spec.ConfOptional), op(spec.ConfDeprecated)), opt},
		{"binary without operands", op(spec.ConfAnd), cond},
	}
	for _, tc := range cases {
		if got := tc.c.Applicability(ctx); got != tc.want {
			t.Errorf("%s: applicability %d, want %d", tc.name, got, tc.want)
		}
	}
}

// TestIsMandatory mirrors Conformance.isMandatory.
func TestIsMandatory(t *testing.T) {
	t.Parallel()
	cases := []struct {
		c    spec.Conformance
		want bool
	}{
		{op(spec.ConfMandatory), true},
		{op(spec.ConfOptional), false},
		{op(spec.ConfOtherwise, op(spec.ConfOptionalIf, name("Status")), op(spec.ConfMandatory)), true},
		{op(spec.ConfOtherwise, op(spec.ConfProvisional), op(spec.ConfMandatory)), false},
		{op(spec.ConfOtherwise, name("A")), false},
	}
	for i, tc := range cases {
		if got := tc.c.IsMandatory(); got != tc.want {
			t.Errorf("case %d: IsMandatory = %t", i, got)
		}
	}
	if !op(spec.ConfDisallowed).IsDisallowed() || op(spec.ConfMandatory).IsDisallowed() {
		t.Error("IsDisallowed")
	}
	f := spec.Field{Conformance: op(spec.ConfMandatory)}
	if !f.Mandatory() {
		t.Error("Field.Mandatory")
	}
}

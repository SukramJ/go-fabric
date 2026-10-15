// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
)

// TestRevisionConditionResolvesAgainstTheDefinition: "Rev >= vN" is
// decided against the definition's revision, as matter.js's runtime
// conformance compiler decides it (packages/node/src/behavior/state/
// validation/conformance-compiler.ts:252-256): an element "Rev >= v7" or
// "Rev >= v7, O" is served by default on a v7 definition and refused on a
// v6 one; "[Rev >= v7]" is optional on v7 and not applicable on v6. A
// feature selection is not judged on the revision
// (IllegalFeatureCombinations.ts:441-443, 464-468).
func TestRevisionConditionResolvesAgainstTheDefinition(t *testing.T) {
	t.Parallel()
	rev7 := spec.Conformance{Op: spec.ConfRevision, Rev: 7}
	cases := []struct {
		name     string
		c        spec.Conformance
		revision uint16
		want     spec.Applicability
	}{
		{"Rev >= v7 on v7", rev7, 7, spec.ApplicabilityMandatory},
		{"Rev >= v7 on v8", rev7, 8, spec.ApplicabilityMandatory},
		{"Rev >= v7 on v6", rev7, 6, spec.ApplicabilityNone},
		{"Rev >= v7, O on v7", op(spec.ConfOtherwise, rev7, op(spec.ConfOptional)), 7, spec.ApplicabilityMandatory},
		{"Rev >= v7, O on v6", op(spec.ConfOtherwise, rev7, op(spec.ConfOptional)), 6, spec.ApplicabilityOptional},
		{"[Rev >= v7] on v7", op(spec.ConfOptionalIf, rev7), 7, spec.ApplicabilityOptional},
		{"[Rev >= v7] on v6", op(spec.ConfOptionalIf, rev7), 6, spec.ApplicabilityNone},
		{"Rev >= v1 on unset revision", spec.Conformance{Op: spec.ConfRevision, Rev: 1}, 0, spec.ApplicabilityMandatory},
	}
	for _, tc := range cases {
		def := &spec.Cluster{Name: "Synthetic", Revision: tc.revision}
		if got := tc.c.Applicability(spec.Context(def, 0)); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
		// Without a revision the condition stays conditional
		// (Conformance.ts computeApplicability).
		if got := tc.c.Applicability(features{}); tc.c.Op == spec.ConfRevision && got != spec.ApplicabilityConditional {
			t.Errorf("%s without a revision: %v", tc.name, got)
		}
	}

	attr := func(c spec.Conformance) spec.Attribute {
		return spec.Attribute{ID: 2, Name: "MinLevel", Type: spec.Type{Name: "uint8", Kind: spec.KindUint, Bits: 8}, Conformance: c}
	}
	for _, tc := range []struct {
		name     string
		c        spec.Conformance
		revision uint16
		declared bool
		served   bool
		refused  bool
	}{
		{"Rev >= v7, O on v7", op(spec.ConfOtherwise, rev7, op(spec.ConfOptional)), 7, false, true, false},
		{"Rev >= v7, O on v6", op(spec.ConfOtherwise, rev7, op(spec.ConfOptional)), 6, false, false, false},
		{"Rev >= v7, O on v6 declared", op(spec.ConfOtherwise, rev7, op(spec.ConfOptional)), 6, true, true, false},
		{"Rev >= v7 on v6 declared", rev7, 6, true, false, true},
	} {
		def := &spec.Cluster{ID: 0xFFF1, Name: "Synthetic", Revision: tc.revision, Attributes: []spec.Attribute{attr(tc.c)}}
		var opts spec.Options
		if tc.declared {
			opts.Attributes = []uint32{2}
		}
		inst, err := spec.New(def, opts)
		if (err != nil) != tc.refused {
			t.Errorf("%s: err %v", tc.name, err)
			continue
		}
		if err == nil && inst.Serves(2) != tc.served {
			t.Errorf("%s: served %v, want %v", tc.name, inst.Serves(2), tc.served)
		}
	}

	// A feature whose conformance is "[Rev >= v7]" stays selectable — and
	// unselected — below the revision.
	def := &spec.Cluster{Name: "Synthetic", Revision: 6, Features: []spec.Feature{
		{Name: "FT", Title: "Feature", Bit: 0, Conformance: op(spec.ConfOptionalIf, rev7)},
	}}
	for _, sel := range []uint32{0, 1} {
		if err := spec.CheckFeatures(def, sel); err != nil {
			t.Errorf("feature [Rev >= v7] on v6, selection %d: %v", sel, err)
		}
	}
}

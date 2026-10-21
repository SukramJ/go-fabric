// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec

import "testing"

// coverageFeatures is a FeatureContext over named features.
type coverageFeatures map[string]bool // defined name → supported

func (f coverageFeatures) Defined(name string) bool   { _, ok := f[name]; return ok }
func (f coverageFeatures) Supported(name string) bool { return f[name] }

// TestFeatureApplicabilityOfAnOtherwiseList walks each entry kind of an
// "otherwise" list: the first entry that applies governs the feature.
func TestFeatureApplicabilityOfAnOtherwiseList(t *testing.T) {
	t.Parallel()

	ctx := coverageFeatures{"A": true, "B": false}
	named := func(n string) Conformance { return Conformance{Op: ConfName, Name: n} }
	otherwise := func(args ...Conformance) Conformance { return Conformance{Op: ConfOtherwise, Args: args} }
	cases := []struct {
		name string
		c    Conformance
		want Applicability
	}{
		{"not an otherwise list", Conformance{Op: ConfMandatory}, ApplicabilityMandatory},
		{"a feature that holds", otherwise(named("A")), ApplicabilityMandatory},
		{"a feature that does not hold, then mandatory", otherwise(named("B"), Conformance{Op: ConfMandatory}), ApplicabilityMandatory},
		{"provisional makes a mandatory entry optional", otherwise(Conformance{Op: ConfProvisional}, Conformance{Op: ConfMandatory}), ApplicabilityOptional},
		{"provisional alone governs nothing", otherwise(Conformance{Op: ConfProvisional}), ApplicabilityNone},
		{"a trailing deprecation is skipped", otherwise(Conformance{Op: ConfDeprecated}), ApplicabilityNone},
		{"a deprecation before another entry decides", otherwise(Conformance{Op: ConfDeprecated}, Conformance{Op: ConfMandatory}), ApplicabilityNone},
		{"a revision condition is not decidable", otherwise(Conformance{Op: ConfRevision}), ApplicabilityConditional},
		{"a described condition is not decidable", otherwise(Conformance{Op: ConfDesc}), ApplicabilityConditional},
		{"a choice is its member", otherwise(Conformance{Op: ConfChoice, Args: []Conformance{{Op: ConfOptional}}}), ApplicabilityOptional},
		{"a bracketed condition that holds", otherwise(Conformance{Op: ConfOptionalIf, Args: []Conformance{named("A")}}), ApplicabilityOptional},
		{"a bracketed condition that does not, then optional", otherwise(Conformance{Op: ConfOptionalIf, Args: []Conformance{named("B")}}, Conformance{Op: ConfOptional}), ApplicabilityOptional},
		{"disallowed", otherwise(Conformance{Op: ConfDisallowed}), ApplicabilityNone},
		{"obsolete", otherwise(Conformance{Op: ConfObsolete}), ApplicabilityNone},
		{"empty", otherwise(Conformance{Op: ConfEmpty}), ApplicabilityOptional},
	}
	for _, c := range cases {
		if got := featureApplicability(c.c, ctx); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

// TestInPeerReadsTheSiblingSet: an "in" constraint admits a value that
// belongs to the sibling attribute's value — a list, or a single value —
// and admits nothing when the sibling is unknown, unresolved or null, or
// the value is not comparable.
func TestInPeerReadsTheSiblingSet(t *testing.T) {
	t.Parallel()

	inst, err := New(testCluster(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	peers := func(set any, ok bool) Peers {
		return func(uint32) (any, bool) { return set, ok }
	}
	for _, c := range []struct {
		name  string
		in    string
		v     any
		peers Peers
		want  bool
	}{
		{"no peers", "Sibling", uint8(1), nil, false},
		{"an unknown sibling", "Nothing", uint8(1), peers([]uint8{1}, true), false},
		{"an unresolved sibling", "Sibling", uint8(1), peers(nil, false), false},
		{"a null sibling", "Sibling", uint8(1), peers(nil, true), false},
		{"a value that is not comparable", "Sibling", []uint8{1}, peers([]uint8{1}, true), false},
		{"a single value that matches", "Sibling", uint8(1), peers(uint8(1), true), true},
		{"a single value that differs", "Sibling", uint8(2), peers(uint8(1), true), false},
		{"a member of a list", "Sibling", uint8(2), peers([]uint8{1, 2}, true), true},
		{"not a member of a list", "Sibling", uint8(3), peers([]uint8{1, 2}, true), false},
	} {
		if got := inst.inPeer(c.in, c.v, c.peers); got != c.want {
			t.Errorf("%s: inPeer = %v, want %v", c.name, got, c.want)
		}
	}
}

// testCluster is a cluster with one mandatory attribute, "Sibling".
func testCluster() *Cluster {
	return &Cluster{
		ID: 0xFFF1, Name: "CoverageTest", Revision: 1,
		Attributes: []Attribute{{
			ID: 0, Name: "Sibling", Type: Type{Name: "uint8", Kind: KindUint, Bits: 8},
			Conformance: Conformance{Text: "M", Op: ConfMandatory},
			Access:      Access{RW: "R"},
		}},
	}
}

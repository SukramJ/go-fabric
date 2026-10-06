// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/schema"
)

// A device-type requirement's conformance, as the schema's generated
// tables carry it, converts into the tree this package evaluates, and
// evaluates as matter.js's Conformance.applicabilityFor does.
func TestConformanceFromSchema(t *testing.T) {
	t.Parallel()
	name := func(n string) schema.Conformance { return schema.Conformance{Op: "name", Name: n} }
	// "TimeSyncWithClientCond, [TlsCertificatesCond | TlsClientCond].a+, O"
	in := schema.Conformance{Text: "TimeSyncWithClientCond, [TlsCertificatesCond | TlsClientCond].a+, O", Op: "otherwise", Args: []schema.Conformance{
		name("TimeSyncWithClientCond"),
		{Op: "choice", Choice: &schema.ConformanceChoice{Name: "a", Num: 1, OrMore: true}, Args: []schema.Conformance{
			{Op: "optionalIf", Args: []schema.Conformance{{Op: "|", Args: []schema.Conformance{name("TlsCertificatesCond"), name("TlsClientCond")}}}},
		}},
		{Op: "O"},
	}}
	c := spec.ConformanceFromSchema(in)
	if c.Text != in.Text || c.Op != spec.ConfOtherwise || len(c.Args) != 3 || c.Args[1].Choice == nil || !c.Args[1].Choice.OrMore {
		t.Fatalf("converted = %+v", c)
	}
	known := set("TimeSyncWithClientCond", "TlsCertificatesCond", "TlsClientCond")
	if a := c.Applicability(ctx{defined: known, supported: set("TimeSyncWithClientCond")}); a != spec.ApplicabilityMandatory {
		t.Errorf("with the client condition = %v, want mandatory", a)
	}
	if a := c.Applicability(ctx{defined: known, supported: set()}); a != spec.ApplicabilityOptional {
		t.Errorf("without = %v, want optional", a)
	}
	if got := c.Names(); !slices.Equal(got, []string{"TimeSyncWithClientCond", "TlsCertificatesCond", "TlsClientCond"}) {
		t.Errorf("names = %v", got)
	}

	// Qualified names join their segments; other operands are walked.
	dot := spec.ConformanceFromSchema(schema.Conformance{Op: "!", Args: []schema.Conformance{
		{Op: "&", Args: []schema.Conformance{
			{Op: ".", Args: []schema.Conformance{name("RootNode"), name("GroupcastListenerCond")}},
			{Op: "==", Args: []schema.Conformance{name("Mode"), {Op: "value", Value: "1"}}},
		}},
	}})
	if got := dot.Names(); !slices.Equal(got, []string{"RootNode.GroupcastListenerCond", "Mode"}) {
		t.Errorf("names = %v", got)
	}
	// A member access over something other than names is walked.
	member := spec.ConformanceFromSchema(schema.Conformance{Op: ".", Args: []schema.Conformance{
		{Op: "revision", Rev: 4}, name("Field"),
	}})
	if got := member.Names(); !slices.Equal(got, []string{"Field"}) {
		t.Errorf("member names = %v", got)
	}
	right := spec.ConformanceFromSchema(schema.Conformance{Op: ".", Args: []schema.Conformance{
		name("A"), {Op: "value", Value: "2"},
	}})
	if got := right.Names(); !slices.Equal(got, []string{"A"}) {
		t.Errorf("right-hand names = %v", got)
	}
	if got := spec.ConformanceFromSchema(schema.Conformance{Op: "M"}).Names(); got != nil {
		t.Errorf("a flag names %v", got)
	}
}

type ctx struct{ defined, supported map[string]bool }

func (c ctx) Defined(n string) bool   { return c.defined[n] }
func (c ctx) Supported(n string) bool { return c.supported[n] }

func set(names ...string) map[string]bool {
	out := map[string]bool{}
	for _, n := range names {
		out[n] = true
	}
	return out
}

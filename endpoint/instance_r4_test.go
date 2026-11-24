// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"
	"strings"
	"testing"
)

// Issue #589, decisions 592, 594, 595: a new Spec.Instance behind a key the
// topology keeps gives the endpoint new DataVersion trackers and keeps the
// key's controller-written state; the same, a nil or an uncomparable
// Instance keeps the trackers.

const instanceCluster = 0x0404 // FlowMeasurement

func assembleInstance(t *testing.T, a *Assembler, instance any, extra ...Spec) *Endpoint {
	t.Helper()
	s := flowSpec(StringKey("a"), "Flow")
	s.Instance = instance
	topo, err := a.Assemble(context.Background(), []Snapshot{{Scope: "s", Endpoints: append([]Spec{s}, extra...)}})
	if err != nil {
		t.Fatal(err)
	}
	for _, ep := range topo.Endpoints {
		if ep.SourceKey != nil && ep.SourceKey.String() == "a" {
			return ep
		}
	}
	t.Fatal("key a not assembled")
	return nil
}

func TestNewInstanceRenewsTheTrackers(t *testing.T) {
	t.Parallel()
	type inst struct{ n int }
	one, two := &inst{1}, &inst{2}
	for _, tc := range []struct {
		name        string
		first, next any
		renewed     bool
	}{
		{"same instance", one, one, false},
		{"new instance", one, two, true},
		{"no instance", nil, nil, false},
		{"instance set later", nil, one, false},
		{"instance dropped", one, nil, false},
		{"uncomparable", []int{1}, []int{2}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := c3Assembler(t)
			old := assembleInstance(t, a, tc.first)
			v := old.ClusterDataVersion(instanceCluster)
			if err := old.SetNodeLabel("written"); err != nil {
				t.Fatal(err)
			}
			old.IncreaseConfigurationVersion()
			next := assembleInstance(t, a, tc.next)
			if got := !next.SameInstance(old); got != tc.renewed {
				t.Errorf("renewed %v, want %v", got, tc.renewed)
			}
			if moved := next.ClusterDataVersion(instanceCluster) != v; moved != tc.renewed {
				t.Errorf("DataVersion moved %v, want %v", moved, tc.renewed)
			}
			// The key's state stays either way (decision 594).
			if got := next.NodeLabel(); got != "written" {
				t.Errorf("NodeLabel %q, want the written one", got)
			}
			if got := next.ConfigurationVersion(); got != 2 {
				t.Errorf("ConfigurationVersion %d, want 2", got)
			}
			if old.identifyServer() != next.identifyServer() {
				t.Error("the new instance runs another Identify")
			}
			// The outgoing endpoint keeps serving its own versions.
			if got := old.ClusterDataVersion(instanceCluster); got != v {
				t.Errorf("the outgoing endpoint's version moved to %d", got)
			}
		})
	}
}

// TestFailedAssemblyInstallsNoNewInstance: an assembly that fails after
// binding a new instance's state leaves the registry as it was, so the
// next one judges against the instance served.
func TestFailedAssemblyInstallsNoNewInstance(t *testing.T) {
	t.Parallel()
	type inst struct{ n int }
	one, two := &inst{1}, &inst{2}
	a := c3Assembler(t)
	old := assembleInstance(t, a, one)
	v := old.ClusterDataVersion(instanceCluster)
	bad := flowSpec(StringKey("b"), "Bad")
	bad.VendorName = strings.Repeat("x", 40)
	s := flowSpec(StringKey("a"), "Flow")
	s.Instance = two
	if _, err := a.Assemble(context.Background(), []Snapshot{{Scope: "s", Endpoints: []Spec{s, bad}}}); err == nil {
		t.Fatal("the assembly with an over-long VendorName succeeded; the test needs it to fail")
	}
	again := assembleInstance(t, a, one)
	if !again.SameInstance(old) || again.ClusterDataVersion(instanceCluster) != v {
		t.Error("a failed assembly installed the new instance's state")
	}
}

// TestPartTakesItsParentsInstance: a part without an Instance of its own
// is renewed with its parent.
func TestPartTakesItsParentsInstance(t *testing.T) {
	t.Parallel()
	type inst struct{ n int }
	a := c3Assembler(t)
	build := func(instance any) *Endpoint {
		s := flowSpec(StringKey("a"), "Flow")
		s.Instance = instance
		s.Parts = []Spec{flowSpec(StringKey("a-part"), "Part")}
		topo, err := a.Assemble(context.Background(), []Snapshot{{Scope: "s", Endpoints: []Spec{s}}})
		if err != nil {
			t.Fatal(err)
		}
		for _, ep := range topo.Endpoints {
			if ep.SourceKey != nil && ep.SourceKey.String() == "a-part" {
				return ep
			}
		}
		t.Fatal("no part")
		return nil
	}
	old := build(&inst{1})
	if next := build(&inst{2}); next.SameInstance(old) {
		t.Error("the part kept its state under its parent's new instance")
	}
}

// TestSameInstanceOfBareEndpoints: endpoints the assembler did not bind to
// a key are never a new instance.
func TestSameInstanceOfBareEndpoints(t *testing.T) {
	t.Parallel()
	a, b := &Endpoint{ID: 2}, &Endpoint{ID: 2}
	a.ClusterDataVersion(instanceCluster)
	b.ClusterDataVersion(instanceCluster)
	if !a.SameInstance(b) || !a.SameInstance(nil) {
		t.Error("bare endpoints count as new instances")
	}
}

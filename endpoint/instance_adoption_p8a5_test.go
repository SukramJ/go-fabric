// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package endpoint

import (
	"context"
	"strings"
	"testing"
)

// TestFailedAssemblyAdoptsNoInstance (issue #605, R5-fsm-5108): an
// endpoint served without an Instance adopts the first one set quietly —
// but only once the assembly that set it succeeded. A failed assembly with
// instance 1 leaves the key without one, so instance 2 is adopted quietly
// too, not judged a new instance against the never-served 1.
func TestFailedAssemblyAdoptsNoInstance(t *testing.T) {
	t.Parallel()
	type inst struct{ n int }
	one, two := &inst{1}, &inst{2}
	a := c3Assembler(t)
	old := assembleInstance(t, a, nil)
	v := old.ClusterDataVersion(instanceCluster)
	bad := flowSpec(StringKey("b"), "Bad")
	bad.VendorName = strings.Repeat("x", 40)
	s := flowSpec(StringKey("a"), "Flow")
	s.Instance = one
	if _, err := a.Assemble(context.Background(), []Snapshot{{Scope: "s", Endpoints: []Spec{s, bad}}}); err == nil {
		t.Fatal("the assembly with an over-long VendorName succeeded; the test needs it to fail")
	}
	next := assembleInstance(t, a, two)
	if !next.SameInstance(old) || next.ClusterDataVersion(instanceCluster) != v {
		t.Error("a failed assembly adopted its instance: the next one was judged a new instance")
	}
}

// TestFailedFirstAssemblyLeavesNoState (issue #605, R5-fsm-5108): the
// first use of a key is written to the registry only once the assembly
// succeeded, so a failed first assembly with instance 1 leaves nothing
// for the next one, with instance 2, to be judged against.
func TestFailedFirstAssemblyLeavesNoState(t *testing.T) {
	t.Parallel()
	type inst struct{ n int }
	one, two := &inst{1}, &inst{2}
	a := c3Assembler(t)
	bad := flowSpec(StringKey("b"), "Bad")
	bad.VendorName = strings.Repeat("x", 40)
	s := flowSpec(StringKey("a"), "Flow")
	s.Instance = one
	if _, err := a.Assemble(context.Background(), []Snapshot{{Scope: "s", Endpoints: []Spec{s, bad}}}); err == nil {
		t.Fatal("the assembly with an over-long VendorName succeeded; the test needs it to fail")
	}
	a.states.mu.Lock()
	_, kept := a.states.states[StringKey("a")]
	a.states.mu.Unlock()
	if kept {
		t.Error("a failed first assembly wrote the key's state to the registry")
	}
	_ = assembleInstance(t, a, two)
}

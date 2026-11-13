// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/im"
)

// TestBoundNodeTypesAreComputed holds the generator's list of bound
// expression nodes (boundOperators, boundFunctions, a reference, a dotted
// reference, a number) against the runtime that computes them
// (cluster/spec/bound_expr.go): for each node type the generator accepts,
// a "max" bound stated with it refuses the value above what it computes.
// A node type the runtime does not know has no number and admits every
// value — the degradation checkBoundExpr refuses at generation
// (R2-P0-par-B05) — so a type accepted here without the runtime's half
// fails this test.
func TestBoundNodeTypesAreComputed(t *testing.T) {
	t.Parallel()
	const ref = `{"type":"reference","name":"peer"}`
	cases := map[string]struct {
		expr string
		max  uint64
	}{
		"+":         {`{"type":"+","lhs":6,"rhs":2}`, 8},
		"-":         {`{"type":"-","lhs":6,"rhs":2}`, 4},
		"*":         {`{"type":"*","lhs":6,"rhs":2}`, 12},
		"/":         {`{"type":"/","lhs":6,"rhs":2}`, 3},
		"^":         {`{"type":"^","lhs":6,"rhs":2}`, 36},
		"maxOf":     {`{"type":"maxOf","args":[6,2]}`, 6},
		"minOf":     {`{"type":"minOf","args":[6,2]}`, 2},
		"number":    {`12.5`, 12},
		"reference": {`{"type":"+","lhs":` + ref + `,"rhs":1}`, 41},
		".":         {`{"type":"+","lhs":{"type":".","lhs":{"type":"reference","name":"limits"},"rhs":{"type":"reference","name":"max"}},"rhs":1}`, 31},
	}
	// Every node type of the generator's lists has a case.
	var listed []string
	for _, m := range []map[string]bool{boundOperators, boundFunctions} {
		for name := range m {
			listed = append(listed, name)
		}
	}
	sort.Strings(listed)
	for _, name := range listed {
		if _, ok := cases[name]; !ok {
			t.Errorf("node type %q is accepted by the generator and has no case here", name)
		}
	}

	type limits struct{ Max uint8 }
	peers := func(id uint32) (any, bool) {
		switch id {
		case 1:
			return uint8(40), true
		case 2:
			return limits{Max: 30}, true
		}
		return nil, false
	}
	mandatory := spec.Conformance{Text: "M", Op: spec.ConfMandatory}
	attr := func(id uint32, name string, typ spec.Type, c spec.Constraint) spec.Attribute {
		return spec.Attribute{
			ID: id, Name: name, Type: typ, Conformance: mandatory, Constraint: c,
			Access: spec.Access{RW: "RW", Read: spec.PrivilegeView, Write: spec.PrivilegeOperate},
		}
	}
	admits := func(t *testing.T, expr string, value uint64) bool {
		t.Helper()
		uint8Type := spec.Type{Kind: spec.KindUint, Bits: 8}
		inst, err := spec.New(&spec.Cluster{ID: 0xFFF1_FC03, Name: "Bound", Revision: 1, Attributes: []spec.Attribute{
			attr(0, "Test", uint8Type, spec.Constraint{Text: "max …", Max: &spec.Bound{Expr: expr}}),
			attr(1, "Peer", uint8Type, spec.Constraint{}),
			attr(2, "Limits", uint8Type, spec.Constraint{}),
		}}, spec.Options{})
		if err != nil {
			t.Fatal(err)
		}
		_, err = inst.ValidateWrite(0, value, peers)
		if err == nil {
			return true
		}
		var sce im.StatusCodeError
		if !errors.As(err, &sce) || sce.MatterStatusCode() != im.StatusConstraintError {
			t.Fatalf("%s with %d: %v, want success or CONSTRAINT_ERROR", expr, value, err)
		}
		return false
	}
	names := make([]string, 0, len(cases))
	for name := range cases {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		c := cases[name]
		if err := checkBoundExpr(json.RawMessage(c.expr)); err != nil {
			t.Errorf("%s: the generator refuses %s: %v", name, c.expr, err)
		}
		if !admits(t, c.expr, c.max) || admits(t, c.expr, c.max+1) {
			t.Errorf("%s: max %s does not hold at %d: the runtime computes another bound or none", name, c.expr, c.max)
		}
	}

	// What the guard is for: a node type the runtime does not know admits
	// every value, and the generator refuses it.
	const unknown = `{"type":"%","lhs":6,"rhs":2}`
	if !admits(t, unknown, 255) {
		t.Errorf("the runtime refuses a value under the unknown node %s; this test's premise moved", unknown)
	}
	if checkBoundExpr(json.RawMessage(unknown)) == nil {
		t.Errorf("the generator accepts the unknown node %s", unknown)
	}
}

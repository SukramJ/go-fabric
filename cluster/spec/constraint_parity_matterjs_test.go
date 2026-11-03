// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	cddef "github.com/SukramJ/go-fabric/cluster/spec/closuredimension"
	"github.com/SukramJ/go-fabric/im"
)

// Parity with matter.js
// packages/node/test/behavior/state/validation/constraintTest.ts at
// 862ad75f, the table groups the existing ports do not hold. matter.js
// validates a record whose "test" field carries the constraint and whose
// siblings it names; here "Test" is an attribute of a synthetic cluster,
// its siblings attributes the Peers resolve, and the bound is the form
// script/clustergen gives it — a literal, a reference, or matter.js's
// Constraint.Expression tree as JSON.

// constraintCluster is a cluster with the attribute Test of type t and
// constraint c beside the given sibling attributes, every one a nullable
// uint16 unless the sibling states its type.
func constraintCluster(t spec.Type, c spec.Constraint, siblings ...spec.Attribute) *spec.Cluster {
	def := &spec.Cluster{ID: 0xFFF1_FC02, Name: "Test", Revision: 1, Attributes: []spec.Attribute{
		rw(0, "Test", t, spec.Quality{}, c),
	}}
	return withSiblings(def, siblings...)
}

func withSiblings(def *spec.Cluster, siblings ...spec.Attribute) *spec.Cluster {
	for n, s := range siblings {
		s.ID = uint32(n + 1)
		if s.Type.Kind == 0 {
			s.Type = spec.Type{Kind: spec.KindUint, Bits: 16}
		}
		s.Conformance, s.Access, s.Quality.Nullable = op(spec.ConfMandatory), spec.Access{RW: "R"}, true
		def.Attributes = append(def.Attributes, s)
	}
	return def
}

// record is the sibling values a case's record holds, by attribute id; a
// missing id is a missing reference.
type record map[uint32]any

func (r record) peers(id uint32) (any, bool) { v, ok := r[id]; return v, ok }

var uint16Type = spec.Type{Kind: spec.KindUint, Bits: 16}

// ref is a reference node of matter.js's Constraint.Expression tree.
func ref(name string) string { return `{"type":"reference","name":"` + name + `"}` }

func TestConstraintParityMatterJS(t *testing.T) {
	t.Parallel()
	type check struct {
		name  string
		value any
		rec   record
		ok    bool
	}
	run := func(t *testing.T, def *spec.Cluster, constraint string, checks []check) {
		t.Helper()
		inst := mustInstance(t, def, 0)
		for _, c := range checks {
			_, err := inst.ValidateWrite(0, c.value, c.rec.peers)
			switch {
			case c.ok && err != nil:
				t.Errorf("%s: refused: %v", c.name, err)
			case !c.ok && statusOf(err) != im.StatusConstraintError:
				t.Errorf("%s: err = %v, want CONSTRAINT_ERROR", c.name, err)
			case !c.ok && !strings.Contains(err.Error(), constraint):
				t.Errorf("%s: %q does not name %q", c.name, err, constraint)
			}
		}
	}

	// :26-37: percent100ths "0.01% to 100.00%", which the generator writes
	// in the encoded unit (1 to 10000) as it does ClosureDimension's
	// Resolution.
	t.Run("percentage bound", func(t *testing.T) {
		resolution := cddef.Definition.Attribute(cddef.AttrResolution)
		if resolution == nil || resolution.Constraint.Min == nil || resolution.Constraint.Min.Int != 1 || resolution.Constraint.Max.Int != 10000 {
			t.Fatalf("generated percent bound %+v", resolution)
		}
		c := spec.Constraint{Text: "0.01% to 100.00%", Min: &spec.Bound{Int: 1}, Max: &spec.Bound{Int: 10000}}
		run(t, constraintCluster(spec.Type{Name: "percent100ths", Kind: spec.KindUint, Bits: 16}, c), c.Text, []check{
			{"accepts a value within the encoded range", uint64(5000), nil, true},
			{"rejects a value above the encoded range", uint64(10001), nil, false},
		})
	})

	// :38-56: the entry constraint of a list of percent100ths.
	t.Run("percentage bound of a list entry", func(t *testing.T) {
		entry := spec.Constraint{Text: "0% to 100%", Min: &spec.Bound{Int: 0}, Max: &spec.Bound{Int: 10000}}
		c := spec.Constraint{Text: "max 4[0% to 100%]", Max: &spec.Bound{Int: 4}, Entry: &entry}
		list := spec.Type{Kind: spec.KindList, Entry: &spec.Field{Name: "entry", Type: spec.Type{Name: "percent100ths", Kind: spec.KindUint, Bits: 16}}}
		run(t, constraintCluster(list, c), "Test[0]", []check{
			{"accepts an entry within the encoded range", []uint16{5000}, nil, true},
			{"rejects an entry above the encoded range", []uint16{10001}, nil, false},
		})
	})

	// :57-70: "min MinVal", MinVal nullable.
	t.Run("min with reference", func(t *testing.T) {
		c := spec.Constraint{Text: "min minVal", Min: &spec.Bound{Ref: "minVal"}}
		run(t, constraintCluster(uint16Type, c, spec.Attribute{Name: "MinVal"}), c.Text, []check{
			{"accepts if over", uint64(5), record{1: uint16(4)}, true},
			{"rejects if under", uint64(3), record{1: uint16(4)}, false},
			{"accepts if reference value is missing", uint64(3), record{}, true},
			{"accepts if reference value is null", uint64(3), record{1: nil}, true},
		})
	})

	// :71-84: "min (MinVal + 1)".
	t.Run("min with expression", func(t *testing.T) {
		c := spec.Constraint{Text: "min minVal + 1", Min: &spec.Bound{Expr: `{"type":"+","lhs":` + ref("minVal") + `,"rhs":1}`}}
		run(t, constraintCluster(uint16Type, c, spec.Attribute{Name: "MinVal"}), c.Text, []check{
			{"accepts if over", uint64(6), record{1: uint16(4)}, true},
			{"rejects if under", uint64(4), record{1: uint16(4)}, false},
			{"accepts if reference value is missing", uint64(3), record{}, true},
			{"accepts if reference value is null", uint64(3), record{1: nil}, true},
		})
	})

	// :99-114: "max MaxVal".
	t.Run("max with reference", func(t *testing.T) {
		c := spec.Constraint{Text: "max maxVal", Max: &spec.Bound{Ref: "maxVal"}}
		run(t, constraintCluster(uint16Type, c, spec.Attribute{Name: "MaxVal"}), c.Text, []check{
			{"rejects if over", uint64(5), record{1: uint16(4)}, false},
			{"accepts if under", uint64(3), record{1: uint16(4)}, true},
			{"accepts if reference value is missing", uint64(3), record{}, true},
			{"accepts if reference value is null", uint64(3), record{1: nil}, true},
		})
	})

	// :182-197: "0 to NumberOfPositions-1".
	t.Run("range with expression", func(t *testing.T) {
		c := spec.Constraint{Text: "0 to numberOfPositions - 1", Min: &spec.Bound{Int: 0}, Max: &spec.Bound{Expr: `{"type":"-","lhs":` + ref("numberOfPositions") + `,"rhs":1}`}}
		run(t, constraintCluster(uint16Type, c, spec.Attribute{Name: "NumberOfPositions"}), c.Text, []check{
			{"accepts if under", uint64(1), record{1: uint16(2)}, true},
			{"rejects if over", uint64(2), record{1: uint16(2)}, false},
		})
	})

	// :198-262: "Limits.HoldTimeMin to Limits.HoldTimeMax", the dotted
	// reference the generator keeps as a qualified Ref.
	t.Run("range with dot-qualified reference", func(t *testing.T) {
		type limits struct{ HoldTimeMin, HoldTimeMax *uint16 }
		u16 := func(v uint16) *uint16 { return &v }
		both := record{1: limits{HoldTimeMin: u16(10), HoldTimeMax: u16(100)}}
		c := spec.Constraint{Text: "limits.holdTimeMin to limits.holdTimeMax", Min: &spec.Bound{Ref: "limits.holdTimeMin"}, Max: &spec.Bound{Ref: "limits.holdTimeMax"}}
		def := constraintCluster(uint16Type, c, spec.Attribute{Name: "Limits", Type: spec.Type{Kind: spec.KindStruct}})
		run(t, def, c.Text, []check{
			{"accepts at the lower bound", uint64(10), both, true},
			{"accepts within the bounds", uint64(50), both, true},
			{"accepts at the upper bound", uint64(100), both, true},
			{"rejects below the lower bound", uint64(9), both, false},
			{"rejects zero", uint64(0), both, false},
			{"rejects above the upper bound", uint64(101), both, false},
			{"rejects the far end of the type range", uint64(65535), both, false},
			{"accepts if the referenced element is missing", uint64(65535), record{}, true},
			{"accepts if the referenced member is missing", uint64(65535), record{1: limits{HoldTimeMin: u16(10)}}, true},
		})
	})

	// :555-565: "max 8{1}", one codepoint at most.
	t.Run("string length with codepoints", func(t *testing.T) {
		c := spec.Constraint{Text: "max 8{1}", Max: &spec.Bound{Int: 8}, CpMax: 1}
		run(t, constraintCluster(spec.Type{Kind: spec.KindString}, c), c.Text, []check{
			{"accepts if under", "𩸽", nil, true},
			{"rejects if over", "𩸽定", nil, false},
		})
	})
}

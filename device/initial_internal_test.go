// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"strings"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
)

// TestRefusedAtInitKinds walks every branch of refusedAtInit's rule with
// hand-made elements: a nullable element and one with a resolved default
// are never refused, an enum, string or octet string without one always,
// a struct when a mandatory child stays undefined (a list child that
// admits [] and a nested struct built from its children's defaults do
// not), a number
// or list when its constraint leaves out 0.
func TestRefusedAtInitKinds(t *testing.T) {
	t.Parallel()
	mandatory := spec.Conformance{Op: spec.ConfMandatory}
	structOf := func(fields ...spec.Field) spec.Type {
		return spec.Type{Kind: spec.KindStruct, Struct: &spec.Struct{Fields: fields}}
	}
	min1 := spec.Constraint{Min: &spec.Bound{Int: 1}}
	for _, c := range []struct {
		name string
		t    spec.Type
		q    spec.Quality
		c    spec.Constraint
		def  any
		want bool
	}{
		{"nullable enum", spec.Type{Kind: spec.KindEnum}, spec.Quality{Nullable: true}, spec.Constraint{}, nil, false},
		{"enum with a default", spec.Type{Kind: spec.KindEnum}, spec.Quality{}, spec.Constraint{}, float64(1), false},
		{"enum with an unresolved default", spec.Type{Kind: spec.KindEnum}, spec.Quality{}, spec.Constraint{}, spec.UnresolvedDefault{Kind: "reference"}, true},
		{"enum", spec.Type{Kind: spec.KindEnum}, spec.Quality{}, spec.Constraint{}, nil, true},
		{"string", spec.Type{Kind: spec.KindString}, spec.Quality{}, spec.Constraint{}, nil, true},
		{"octet string", spec.Type{Kind: spec.KindBytes}, spec.Quality{}, spec.Constraint{}, nil, true},
		{"struct of no known shape", spec.Type{Kind: spec.KindStruct}, spec.Quality{}, spec.Constraint{}, nil, false},
		{"struct, mandatory child without a default", structOf(spec.Field{Name: "a", Conformance: mandatory}), spec.Quality{}, spec.Constraint{}, nil, true},
		{"struct, mandatory child with a default", structOf(spec.Field{Name: "a", Conformance: mandatory, Default: float64(0)}), spec.Quality{}, spec.Constraint{}, nil, false},
		{"struct, nullable mandatory child", structOf(spec.Field{Name: "a", Conformance: mandatory, Quality: spec.Quality{Nullable: true}}), spec.Quality{}, spec.Constraint{}, nil, false},
		{"struct, optional child", structOf(spec.Field{Name: "a", Conformance: spec.Conformance{Op: spec.ConfOptional}}), spec.Quality{}, spec.Constraint{}, nil, false},
		{"struct, list child that admits []", structOf(spec.Field{Name: "a", Conformance: mandatory, Type: spec.Type{Kind: spec.KindList}}), spec.Quality{}, spec.Constraint{}, nil, false},
		{"struct, list child of min 1", structOf(spec.Field{Name: "a", Conformance: mandatory, Type: spec.Type{Kind: spec.KindList}, Constraint: min1}), spec.Quality{}, spec.Constraint{}, nil, true},
		{"struct, nested struct built from defaults", structOf(spec.Field{Name: "a", Conformance: mandatory, Type: structOf(spec.Field{Name: "b", Conformance: mandatory, Default: float64(0)})}), spec.Quality{}, spec.Constraint{}, nil, false},
		{"struct, nested struct with an undefined child", structOf(spec.Field{Name: "a", Conformance: mandatory, Type: structOf(spec.Field{Name: "b", Conformance: mandatory, Type: spec.Type{Kind: spec.KindEnum}})}), spec.Quality{}, spec.Constraint{}, nil, true},
		{"struct, nested struct of no known shape", structOf(spec.Field{Name: "a", Conformance: mandatory, Type: spec.Type{Kind: spec.KindStruct}}), spec.Quality{}, spec.Constraint{}, nil, false},
		{"list, min 1", spec.Type{Kind: spec.KindList}, spec.Quality{}, min1, nil, true},
		{"list", spec.Type{Kind: spec.KindList}, spec.Quality{}, spec.Constraint{}, nil, false},
		{"uint, min 1", spec.Type{Kind: spec.KindUint}, spec.Quality{}, min1, nil, true},
		{"int", spec.Type{Kind: spec.KindInt}, spec.Quality{}, spec.Constraint{}, nil, false},
		{"float, min 1", spec.Type{Kind: spec.KindFloat64}, spec.Quality{}, min1, nil, true},
		{"bool", spec.Type{Kind: spec.KindBool}, spec.Quality{}, spec.Constraint{}, nil, false},
		{"bitmap", spec.Type{Kind: spec.KindBitmap}, spec.Quality{}, spec.Constraint{}, nil, false},
		{"any", spec.Type{Kind: spec.KindAny}, spec.Quality{}, spec.Constraint{}, nil, false},
		{"unknown kind", spec.Type{Kind: spec.Kind(200)}, spec.Quality{}, spec.Constraint{}, nil, false},
	} {
		if got := refusedAtInit(c.t, c.q, c.c, c.def); got != c.want {
			t.Errorf("%s: refusedAtInit = %t, want %t", c.name, got, c.want)
		}
	}
}

// TestExcludesZero: a literal value, minimum or maximum that leaves out 0
// excludes it, alternatives exclude it only when every one does, and a
// bound naming another element or an expression admits it.
func TestExcludesZero(t *testing.T) {
	t.Parallel()
	b := func(v int64) *spec.Bound { return &spec.Bound{Int: v} }
	for _, c := range []struct {
		name string
		c    spec.Constraint
		want bool
	}{
		{"none", spec.Constraint{}, false},
		{"value 0", spec.Constraint{Value: b(0)}, false},
		{"value 3", spec.Constraint{Value: b(3)}, true},
		{"min 0", spec.Constraint{Min: b(0)}, false},
		{"min 1", spec.Constraint{Min: b(1)}, true},
		{"max -1", spec.Constraint{Max: b(-1)}, true},
		{"max 5", spec.Constraint{Max: b(5)}, false},
		{"min by reference", spec.Constraint{Min: &spec.Bound{Ref: "MinLevel"}}, false},
		{"value by expression", spec.Constraint{Value: &spec.Bound{Expr: "{}"}}, false},
		{"parts all exclude", spec.Constraint{Parts: []spec.Constraint{{Value: b(1)}, {Min: b(3)}}}, true},
		{"one part admits", spec.Constraint{Parts: []spec.Constraint{{Value: b(0)}, {Min: b(3)}}}, false},
	} {
		if got := excludesZero(c.c); got != c.want {
			t.Errorf("%s: excludesZero = %t, want %t", c.name, got, c.want)
		}
	}
}

// TestInitialRequirementNames: a cluster without a definition is named by
// its id, and the declaration of a known one names its generated package
// — a Go keyword gets "cluster" appended, as script/clustergen does.
func TestInitialRequirementNames(t *testing.T) {
	t.Parallel()
	var none *Type
	if got, err := none.RequiredInitial(); got != nil || err != nil {
		t.Errorf("nil type: RequiredInitial = %v, %v; want nil, nil", got, err)
	}
	r := initialRequirement(nil, 0xFFF1FC00, 0x0005)
	if r.ClusterName != "cluster 0xFFF1FC00" || r.AttributeName != "0x0005" || r.Declare != "the cluster's declaration" {
		t.Errorf("undefined cluster: %+v", r)
	}
	if s := r.String(); !strings.Contains(s, "has no value; state it with the cluster's declaration") {
		t.Errorf("String() = %q", s)
	}
	for in, want := range map[string]string{
		"Fan Control":          "fancontroldef",
		"Switch":               "switchclusterdef",
		"OccupancySensing":     "occupancysensingdef",
		"Wi-Fi Network Diag 2": "wifinetworkdiag2def",
	} {
		if got := declarePackage(in); got != want {
			t.Errorf("declarePackage(%q) = %q, want %q", in, got, want)
		}
	}
}

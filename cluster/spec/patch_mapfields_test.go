// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
)

// mappedPatchProbe carries mappedPatchStruct under Go field names of its
// own, declared with spec.MapFields.
type mappedPatchProbe struct {
	Alpha uint8
	Beta  *uint8
}

var mappedPatchStruct = &spec.Struct{Name: "MappedProbeStruct", Fields: []spec.Field{
	{ID: 0, Name: "A", Type: spec.Type{Name: "uint8", Kind: spec.KindUint, Bits: 8}, Conformance: spec.Conformance{Text: "M", Op: spec.ConfMandatory}},
	{ID: 1, Name: "B", Type: spec.Type{Name: "uint8", Kind: spec.KindUint, Bits: 8}, Conformance: spec.Conformance{Text: "M", Op: spec.ConfMandatory}, Default: float64(7)},
}}

func init() {
	spec.MapFields[mappedPatchProbe](mappedPatchStruct, spec.FieldMap{"A": "Alpha", "B": "Beta"})
}

// TestSetReadsAMapFieldsTypeThroughItsMap holds the default patch of a
// host's Set to the same field map the checks read: a value of a
// spec.MapFields type is patched through its declared Go fields, its
// absent optional field given the definition's default. Before,
// patchStruct matched Go fields by the definition's names and refused the
// type with "Alpha is not a property" (review II, R2-P6-api-1314).
func TestSetReadsAMapFieldsTypeThroughItsMap(t *testing.T) {
	t.Parallel()
	rw := spec.Access{RW: "RW", Read: spec.PrivilegeView, Write: spec.PrivilegeOperate}
	def := &spec.Cluster{
		ID: 0xFFF1FC0B, Name: "MappedProbe", Revision: 1,
		Attributes: []spec.Attribute{{
			ID: 0, Name: "C", Type: spec.Type{Name: "MappedProbeStruct", Kind: spec.KindStruct, Struct: mappedPatchStruct},
			Conformance: spec.Conformance{Text: "M", Op: spec.ConfMandatory}, Access: rw,
		}},
	}
	srv, err := spec.NewServer(def, spec.Options{}, spec.ServerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Set(0, mappedPatchProbe{Alpha: 3}); err != nil {
		t.Fatalf("Set of a MapFields value: %v, want nil", err)
	}
	v, _ := srv.Value(0)
	got, ok := v.(mappedPatchProbe)
	if !ok || got.Alpha != 3 {
		t.Fatalf("stored %#v, want mappedPatchProbe{Alpha: 3, …}", v)
	}
	if got.Beta == nil || *got.Beta != 7 {
		t.Errorf("stored Beta %v, want the default 7 patched in", got.Beta)
	}
}

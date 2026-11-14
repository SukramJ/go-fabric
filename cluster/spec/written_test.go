// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
)

// unmappedForm is a hand-written form whose declaration leaves a field of
// its definition struct out.
type unmappedForm struct{ A uint8 }

// TestVerifyFieldMapsNamesAMissingField: a MapFields declaration that
// leaves a definition field without a Go field fails VerifyFieldMaps,
// naming the field.
func TestVerifyFieldMapsNamesAMissingField(t *testing.T) {
	t.Parallel()
	s := &spec.Struct{Name: "Form", Fields: []spec.Field{
		{ID: 0, Name: "A", Type: spec.Type{Name: "uint8", Kind: spec.KindUint, Bits: 8}},
		{ID: 1, Name: "B", Type: spec.Type{Name: "uint8", Kind: spec.KindUint, Bits: 8}},
	}}
	spec.MapFields[unmappedForm](s, spec.FieldMap{"A": "A"})
	err := spec.VerifyFieldMaps()
	if err == nil || !strings.Contains(err.Error(), "Form.B") {
		t.Fatalf("VerifyFieldMaps = %v, want it to name Form.B", err)
	}
}

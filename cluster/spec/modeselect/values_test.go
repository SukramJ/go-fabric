// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package modeselect_test

import (
	"testing"

	msdef "github.com/SukramJ/go-fabric/cluster/spec/modeselect"
)

// TestBoundsMatchTheGeneratedDefinition pins each hand-written bound to the
// generated definition's Constraint.Max, so a snapshot that moves a bound
// fails here rather than in a host that trims to the old value.
func TestBoundsMatchTheGeneratedDefinition(t *testing.T) {
	t.Parallel()
	a := msdef.Definition.AttributeByName("SupportedModes")
	if a == nil || a.Constraint.Max == nil {
		t.Fatal("SupportedModes has no max constraint")
	}
	if got := a.Constraint.Max.Int; got != msdef.SupportedModesMaxEntries {
		t.Errorf("SupportedModes max %d, SupportedModesMaxEntries %d", got, msdef.SupportedModesMaxEntries)
	}
	fieldMax := func(name string) int64 {
		for _, f := range msdef.ModeOptionStructDef.Fields {
			if f.Name == name && f.Constraint.Max != nil {
				return f.Constraint.Max.Int
			}
		}
		t.Fatalf("ModeOptionStruct.%s has no max constraint", name)
		return 0
	}
	if got := fieldMax("Label"); got != msdef.LabelMaxBytes {
		t.Errorf("ModeOptionStruct.Label max %d, LabelMaxBytes %d", got, msdef.LabelMaxBytes)
	}
	if got := fieldMax("SemanticTags"); got != msdef.SemanticTagsMaxEntries {
		t.Errorf("ModeOptionStruct.SemanticTags max %d, SemanticTagsMaxEntries %d", got, msdef.SemanticTagsMaxEntries)
	}
}

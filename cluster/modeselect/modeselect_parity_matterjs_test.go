// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package modeselect_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/modeselect"
	msdef "github.com/SukramJ/go-fabric/cluster/spec/modeselect"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
)

// TestServerMatchesTheGeneratedDefinition holds the server against
// matter.js mode-select-cluster.element.ts for the one feature selection
// it serves (none, DEPONOFF clear).
func TestServerMatchesTheGeneratedDefinition(t *testing.T) {
	t.Parallel()
	spectest.CheckServer(t, modeselect.NewServer(modeselect.Config{Source: coffeeSource()}), msdef.Definition, 0)
}

// TestBoundsMatchTheDefinition pins the encode-time bounds the bridge
// applies to the constraints of the element file.
func TestBoundsMatchTheDefinition(t *testing.T) {
	t.Parallel()
	maxOf := func(name string) int64 {
		a := msdef.Definition.AttributeByName(name)
		if a == nil || a.Constraint.Max == nil {
			t.Fatalf("%s has no max constraint", name)
		}
		return a.Constraint.Max.Int
	}
	if got := maxOf("Description"); got != modeselect.DescriptionMaxBytes {
		t.Errorf("Description max %d, DescriptionMaxBytes %d", got, modeselect.DescriptionMaxBytes)
	}
	if got := maxOf("SupportedModes"); got != modeselect.SupportedModesMaxEntries {
		t.Errorf("SupportedModes max %d, SupportedModesMaxEntries %d", got, modeselect.SupportedModesMaxEntries)
	}
}

// TestChangeToModeTakesTheGeneratedRequest pins the shape the bridge hands
// over once the definition is registered.
func TestChangeToModeTakesTheGeneratedRequest(t *testing.T) {
	t.Parallel()
	src := coffeeSource()
	srv := modeselect.NewServer(modeselect.Config{Source: src})
	if _, err := srv.MatterInvoke(context.Background(), modeselect.CmdChangeToMode, msdef.ChangeToModeRequest{NewMode: 1}); err != nil {
		t.Fatalf("ChangeToMode: %v", err)
	}
	if len(src.changed) != 1 || src.changed[0] != 1 {
		t.Errorf("host saw %v, want [1]", src.changed)
	}
}

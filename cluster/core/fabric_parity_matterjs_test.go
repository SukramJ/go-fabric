// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

// A port of matter.js packages/protocol/test/fabric/FabricTest.ts at
// 862ad75f: the fabric label's length constraint (Spec 1.6 §11.18.6.11, max
// 32, no minimum).
//
// matter.js calls Fabric.setLabel; the Go label is set by the
// UpdateFabricLabel command of the Operational Credentials server on the
// invoking session's fabric. A rejection is IM InvalidCommand there.

import (
	"context"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
	mstore "github.com/SukramJ/go-fabric/store"
)

// fabricTestSetLabel sets label on a stored fabric and returns the label
// stored afterwards and the command's error.
func fabricTestSetLabel(t *testing.T, label string) (string, error) {
	t.Helper()
	ctx := context.Background()
	st := newFakeStore()
	idx, err := st.AddFabric(ctx, mstore.FabricRecord{FabricID: 1, Label: "mock-fabric-1"})
	if err != nil {
		t.Fatal(err)
	}
	oc, err := core.NewOperationalCredentials(st, core.OpcredsConfig{SupportedFabrics: 5})
	if err != nil {
		t.Fatal(err)
	}
	resp, ierr := oc.MatterInvoke(im.WithFabricFilter(ctx, false, idx), 0x09, core.UpdateFabricLabelRequest{Label: label})
	if ierr == nil {
		if noc, ok := resp.(core.NOCResponse); !ok || noc.StatusCode != core.NOCStatusOK {
			t.Fatalf("UpdateFabricLabel(%q) = %#v, want OK", label, resp)
		}
	}
	rec, err := st.GetFabric(ctx, idx)
	if err != nil {
		t.Fatal(err)
	}
	return rec.Label, ierr
}

func TestFabricSetLabel(t *testing.T) {
	t.Parallel()

	t.Run("accepts an empty label", func(t *testing.T) {
		t.Parallel()
		if got, err := fabricTestSetLabel(t, ""); err != nil || got != "" {
			t.Fatalf("label = %q, %v; want \"\"", got, err)
		}
	})

	t.Run("accepts a label of the maximum length", func(t *testing.T) {
		t.Parallel()
		label := strings.Repeat("x", 32)
		if got, err := fabricTestSetLabel(t, label); err != nil || got != label {
			t.Fatalf("label = %q, %v; want %q", got, err, label)
		}
	})

	t.Run("rejects a label that exceeds the maximum length", func(t *testing.T) {
		t.Parallel()
		got, err := fabricTestSetLabel(t, strings.Repeat("x", 33))
		if err == nil || !strings.Contains(err.Error(), "32") {
			t.Fatalf("UpdateFabricLabel(33 x) error = %v, want the 32 limit", err)
		}
		if got != "mock-fabric-1" {
			t.Fatalf("label = %q after the rejection, want it unchanged", got)
		}
	})
}

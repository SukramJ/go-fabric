// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// labelChanges records what BasicInformation reports to
// OnAttributeChange.
func labelChanges(t *testing.T, notifier contract.AttributeChangeNotifier) *[]contract.Change {
	t.Helper()
	var got []contract.Change
	unsub := notifier.OnAttributeChange(func(c contract.Change) { got = append(got, c) })
	t.Cleanup(unsub)
	return &got
}

// TestBasicInfo_WriteNodeLabel (issue 414): the host's write has a
// controller's write semantics — set, DataVersion advanced, persisted
// through the hook and reported with OriginHostSet, as matter.js's
// datasource commits a host's set of state.basicInformation.nodeLabel.
func TestBasicInfo_WriteNodeLabel(t *testing.T) {
	t.Parallel()
	b := newValidBasicInfo(t)
	changes := labelChanges(t, b)
	var persisted []string
	b.SetOnPersistentWrite(func(nodeLabel, _ string) { persisted = append(persisted, nodeLabel) })
	before := b.MatterDataVersion()

	if err := b.WriteNodeLabel(context.Background(), "Wohnzimmer"); err != nil {
		t.Fatalf("WriteNodeLabel: %v", err)
	}
	if v, _ := b.MatterRead(0x0005); v.(tlv.BoundedString).Value != "Wohnzimmer" {
		t.Errorf("NodeLabel = %v, want Wohnzimmer", v)
	}
	if b.MatterDataVersion() == before {
		t.Error("DataVersion did not advance")
	}
	if !slices.Equal(persisted, []string{"Wohnzimmer"}) {
		t.Errorf("persisted %v, want [Wohnzimmer]", persisted)
	}
	if len(*changes) != 1 || !slices.Equal((*changes)[0].Attributes, []uint32{0x0005}) || (*changes)[0].Origin != contract.OriginHostSet {
		t.Errorf("changes %+v, want one NodeLabel change from the host", *changes)
	}
	// Empty is a valid NodeLabel ("max 32").
	if err := b.WriteNodeLabel(context.Background(), ""); err != nil {
		t.Errorf("WriteNodeLabel(\"\"): %v", err)
	}
}

// TestBasicInfo_HostWriteNodeLabelTooLong (issue 414): a label over 32 bytes
// is refused with the status a controller's write gets, CONSTRAINT_ERROR,
// and nothing moves.
func TestBasicInfo_HostWriteNodeLabelTooLong(t *testing.T) {
	t.Parallel()
	b := newValidBasicInfo(t)
	changes := labelChanges(t, b)
	var persisted int
	b.SetOnPersistentWrite(func(string, string) { persisted++ })
	before := b.MatterDataVersion()
	long := strings.Repeat("y", 33)

	hostErr := b.WriteNodeLabel(context.Background(), long)
	controllerErr := b.MatterWrite(contract.WithOrigin(context.Background(), contract.OriginControllerWrite), 0x0005, long)
	for name, err := range map[string]error{"host": hostErr, "controller": controllerErr} {
		sce, ok := errors.AsType[im.StatusCodeError](err)
		if !ok || sce.MatterStatusCode() != im.StatusConstraintError {
			t.Errorf("%s: %v, want CONSTRAINT_ERROR", name, err)
		}
	}
	if v, _ := b.MatterRead(0x0005); v.(tlv.BoundedString).Value != "go-fabric-bridge" {
		t.Errorf("NodeLabel = %v, want unchanged", v)
	}
	if b.MatterDataVersion() != before || persisted != 0 || len(*changes) != 0 {
		t.Errorf("a refused write moved: version %d→%d, persisted %d, changes %+v", before, b.MatterDataVersion(), persisted, *changes)
	}
}

// TestBasicInfo_MatterWriteReportsOrigin (issue 414): a controller's write
// of NodeLabel or Location is reported with the origin the dispatcher put
// in the context.
func TestBasicInfo_MatterWriteReportsOrigin(t *testing.T) {
	t.Parallel()
	b := newValidBasicInfo(t)
	changes := labelChanges(t, b)
	ctx := contract.WithOrigin(context.Background(), contract.OriginControllerWrite)
	if err := b.MatterWrite(ctx, 0x0005, "Küche"); err != nil {
		t.Fatal(err)
	}
	if err := b.MatterWrite(ctx, 0x0006, "DE"); err != nil {
		t.Fatal(err)
	}
	if len(*changes) != 2 {
		t.Fatalf("changes %+v, want two", *changes)
	}
	for i, attr := range []uint32{0x0005, 0x0006} {
		if c := (*changes)[i]; !slices.Equal(c.Attributes, []uint32{attr}) || c.Origin != contract.OriginControllerWrite {
			t.Errorf("change %d = %+v, want 0x%04X from a controller's write", i, c, attr)
		}
	}
}

// TestBasicInfo_SetNodeLabelRestoresSilently (issue 414): the boot-time
// restore neither advances the DataVersion nor reports — it stays distinct
// from the host's write.
func TestBasicInfo_SetNodeLabelRestoresSilently(t *testing.T) {
	t.Parallel()
	b := newValidBasicInfo(t)
	changes := labelChanges(t, b)
	before := b.MatterDataVersion()
	if err := b.SetNodeLabel("restored"); err != nil {
		t.Fatal(err)
	}
	if b.MatterDataVersion() != before || len(*changes) != 0 {
		t.Errorf("restore moved: version %d→%d, changes %+v", before, b.MatterDataVersion(), *changes)
	}
}

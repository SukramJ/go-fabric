// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
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

// TestBasicInfo_HostSetNodeLabel (issue 414): the host's write has a
// controller's write semantics — set, DataVersion advanced, persisted
// through the hook and reported with OriginHostSet, as matter.js's
// datasource commits a host's set of state.basicInformation.nodeLabel.
func TestBasicInfo_HostSetNodeLabel(t *testing.T) {
	t.Parallel()
	b := newValidBasicInfo(t)
	changes := labelChanges(t, b)
	var persisted []string
	b.SetOnPersistentWrite(func(nodeLabel, _ string) error { persisted = append(persisted, nodeLabel); return nil })
	before := b.MatterDataVersion()

	if err := b.SetNodeLabel(context.Background(), "Wohnzimmer"); err != nil {
		t.Fatalf("SetNodeLabel: %v", err)
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
	if err := b.SetNodeLabel(context.Background(), ""); err != nil {
		t.Errorf("SetNodeLabel(\"\"): %v", err)
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
	b.SetOnPersistentWrite(func(string, string) error { persisted++; return nil })
	before := b.MatterDataVersion()
	long := strings.Repeat("y", 33)

	hostErr := b.SetNodeLabel(context.Background(), long)
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

// TestBasicInfo_RestoreNodeLabelIsSilent (issue 414): the boot-time
// restore neither advances the DataVersion nor reports — it stays distinct
// from the host's write.
func TestBasicInfo_RestoreNodeLabelIsSilent(t *testing.T) {
	t.Parallel()
	b := newValidBasicInfo(t)
	changes := labelChanges(t, b)
	before := b.MatterDataVersion()
	if err := b.RestoreNodeLabel("restored"); err != nil {
		t.Fatal(err)
	}
	if b.MatterDataVersion() != before || len(*changes) != 0 {
		t.Errorf("restore moved: version %d→%d, changes %+v", before, b.MatterDataVersion(), *changes)
	}
}

// TestBasicInfo_UnchangedWriteCommitsNothing (issue 414): writing the value
// a label already has succeeds and commits nothing — no DataVersion
// advance, no persistence, no report — as matter.js's #noteIfChanged
// skips an unchanged property (Datasource.ts:1477-1495 at db021da0); the
// host's write and a controller's alike, NodeLabel and Location alike.
func TestBasicInfo_UnchangedWriteCommitsNothing(t *testing.T) {
	t.Parallel()
	b := newValidBasicInfo(t)
	changes := labelChanges(t, b)
	var persisted int
	b.SetOnPersistentWrite(func(string, string) error { persisted++; return nil })
	ctx := context.Background()
	controller := contract.WithOrigin(ctx, contract.OriginControllerWrite)

	for range 2 {
		if err := b.SetNodeLabel(ctx, "Wohnzimmer"); err != nil {
			t.Fatal(err)
		}
	}
	afterHost := b.MatterDataVersion()
	if err := b.MatterWrite(controller, 0x0005, "Wohnzimmer"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := b.MatterWrite(controller, 0x0006, "DE"); err != nil {
			t.Fatal(err)
		}
	}
	if persisted != 2 || len(*changes) != 2 {
		t.Errorf("persisted %d, changes %+v; want one each for NodeLabel and Location", persisted, *changes)
	}
	if b.MatterDataVersion() != afterHost+1 {
		t.Errorf("DataVersion %d after the unchanged writes, want %d (only Location's first write advances)", b.MatterDataVersion(), afterHost+1)
	}
}

// TestBasicInfo_RestoresUseTheDefinition (issue 414): the restores refuse
// what a controller's write is refused, with its status — NodeLabel over 32
// bytes, a Location that is not two characters — and keep the served value.
func TestBasicInfo_RestoresUseTheDefinition(t *testing.T) {
	t.Parallel()
	b := newValidBasicInfo(t)
	for name, err := range map[string]error{
		"label":          b.RestoreNodeLabel(strings.Repeat("y", 33)),
		"location":       b.RestoreLocation("DEU"),
		"empty location": b.RestoreLocation(""),
	} {
		sce, ok := errors.AsType[im.StatusCodeError](err)
		if !ok || sce.MatterStatusCode() != im.StatusConstraintError {
			t.Errorf("%s: %v, want CONSTRAINT_ERROR", name, err)
		}
	}
	if err := b.RestoreNodeLabel(""); err != nil {
		t.Errorf("RestoreNodeLabel(\"\"): %v", err)
	}
	if err := b.RestoreLocation("FR"); err != nil {
		t.Errorf("RestoreLocation(FR): %v", err)
	}
	if v, _ := b.MatterRead(0x0006); v.(tlv.BoundedString).Value != "FR" {
		t.Errorf("Location = %v, want FR", v)
	}
}

// TestBasicInfo_StoreFailureFailsTheWrite (issue 414, review L2): when the
// store refuses the value, the write fails and nothing served changes — no
// value, no DataVersion advance, no report — as matter.js rolls the
// transaction back when its store write fails (Datasource.ts:1227-1261,
// :1329-1340 at db021da0). The host gets the store's error; a controller's
// write gets an error with no IM status, which the dispatcher answers
// FAILURE.
func TestBasicInfo_StoreFailureFailsTheWrite(t *testing.T) {
	t.Parallel()
	b := newValidBasicInfo(t)
	changes := labelChanges(t, b)
	errStore := errors.New("disk full")
	b.SetOnPersistentWrite(func(string, string) error { return errStore })
	before := b.MatterDataVersion()

	hostErr := b.SetNodeLabel(context.Background(), "Wohnzimmer")
	if !errors.Is(hostErr, errStore) {
		t.Errorf("host: %v, want the store's error", hostErr)
	}
	controllerErr := b.MatterWrite(contract.WithOrigin(context.Background(), contract.OriginControllerWrite), 0x0006, "DE")
	if !errors.Is(controllerErr, errStore) {
		t.Errorf("controller: %v, want the store's error", controllerErr)
	}
	if _, ok := errors.AsType[im.StatusCodeError](controllerErr); ok {
		t.Errorf("controller: %v carries an IM status, want none (FAILURE)", controllerErr)
	}
	if v, _ := b.MatterRead(0x0005); v.(tlv.BoundedString).Value != "go-fabric-bridge" {
		t.Errorf("NodeLabel = %v, want unchanged", v)
	}
	if v, _ := b.MatterRead(0x0006); v.(tlv.BoundedString).Value != "XX" {
		t.Errorf("Location = %v, want unchanged", v)
	}
	if b.MatterDataVersion() != before || len(*changes) != 0 {
		t.Errorf("a failed store write moved: version %d→%d, changes %+v", before, b.MatterDataVersion(), *changes)
	}
}

// TestBasicInfo_RacingWritersKeepStoreAndServedInStep (issue 414, review
// L3): writers racing on NodeLabel leave the store holding the label the
// cluster serves — the compare, the store write and the set are one step.
func TestBasicInfo_RacingWritersKeepStoreAndServedInStep(t *testing.T) {
	t.Parallel()
	for range 50 {
		b := newValidBasicInfo(t)
		var mu sync.Mutex
		var stored string
		b.SetOnPersistentWrite(func(nodeLabel, _ string) error {
			mu.Lock()
			stored = nodeLabel
			mu.Unlock()
			return nil
		})
		var wg sync.WaitGroup
		for i := range 8 {
			wg.Go(func() {
				if err := b.SetNodeLabel(context.Background(), fmt.Sprintf("label-%d", i)); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		v, _ := b.MatterRead(0x0005)
		mu.Lock()
		got := stored
		mu.Unlock()
		if served := v.(tlv.BoundedString).Value; served != got {
			t.Fatalf("served %q, stored %q", served, got)
		}
	}
}

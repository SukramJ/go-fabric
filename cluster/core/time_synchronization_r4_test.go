// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"slices"
	"testing"

	"thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/cluster/spec"
	tsdef "thingfabric.dev/go-fabric/cluster/spec/timesynchronization"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
)

func setTrustedOn(t *testing.T, srv *core.TimeSynchronization, fabric uint8) error {
	t.Helper()
	ctx := im.WithFabricFilter(context.Background(), false, fabric)
	req := tsdef.SetTrustedTimeSourceRequest{TrustedTimeSource: spec.ValueOf(tsdef.FabricScopedTrustedTimeSourceStruct{NodeId: 0x55, Endpoint: 0})}
	_, err := srv.MatterInvoke(ctx, tsdef.CmdSetTrustedTimeSource, req)
	return err
}

// TestTimeSyncDropRefusedStoreStaysOwed: a fabric's drop whose store of
// the cleared TrustedTimeSource is refused keeps the fabric listed as held
// and answers the error; the reconcile's next drop stores the clear, so a
// restart does not load the removed fabric's source (review II,
// R4-store-4151).
func TestTimeSyncDropRefusedStoreStaysOwed(t *testing.T) {
	st := &tsSettings{m: map[string]string{}}
	cfg := core.TimeSynchronizationConfig{Features: tsdef.FeatureTimeSyncClient, Store: st}
	srv, err := core.NewTimeSynchronizationWith(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := setTrustedOn(t, srv, 2); err != nil {
		t.Fatal(err)
	}
	var reported [][]uint32
	srv.OnAttributeChange(func(c contract.Change) { reported = append(reported, c.Attributes) })
	st.mu.Lock()
	st.fail = true
	st.mu.Unlock()
	if err := srv.MatterDropFabric(context.Background(), 2); err == nil {
		t.Error("the drop whose store the store refused answered nil")
	}
	st.mu.Lock()
	st.fail = false
	st.mu.Unlock()
	if held := srv.MatterHeldFabrics(); !slices.Contains(held, 2) {
		t.Fatalf("MatterHeldFabrics = %v after a refused store of the drop, want 2 listed (owed)", held)
	}
	if len(reported) != 0 {
		t.Errorf("reported %v after the refused store, want nothing (R4-store-4152)", reported)
	}
	if err := srv.MatterDropFabric(context.Background(), 2); err != nil {
		t.Fatalf("second drop: %v", err)
	}
	if held := srv.MatterHeldFabrics(); len(held) != 0 {
		t.Errorf("MatterHeldFabrics = %v after the owed clear landed, want none", held)
	}
	if !slices.ContainsFunc(reported, func(a []uint32) bool { return slices.Contains(a, tsdef.AttrTrustedTimeSource) }) {
		t.Errorf("reported %v after the owed clear landed, want TrustedTimeSource", reported)
	}
	re, err := core.NewTimeSynchronizationWith(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := re.TrustedTimeSource(); ok {
		t.Errorf("a restart loads the removed fabric's source %+v", got)
	}
}

// TestTimeSyncOwedDropSettledByALaterStore: another fabric's source stored
// after the refused clear settles the owed drop, and the retry does not
// overwrite it.
func TestTimeSyncOwedDropSettledByALaterStore(t *testing.T) {
	st := &tsSettings{m: map[string]string{}}
	cfg := core.TimeSynchronizationConfig{Features: tsdef.FeatureTimeSyncClient, Store: st}
	srv, err := core.NewTimeSynchronizationWith(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := setTrustedOn(t, srv, 2); err != nil {
		t.Fatal(err)
	}
	st.mu.Lock()
	st.fail = true
	st.mu.Unlock()
	_ = srv.MatterDropFabric(context.Background(), 2)
	st.mu.Lock()
	st.fail = false
	st.mu.Unlock()
	if err := setTrustedOn(t, srv, 1); err != nil {
		t.Fatal(err)
	}
	if held := srv.MatterHeldFabrics(); !slices.Equal(held, []uint8{1}) {
		t.Errorf("MatterHeldFabrics = %v, want [1]", held)
	}
	_ = srv.MatterDropFabric(context.Background(), 2)
	re, _ := core.NewTimeSynchronizationWith(cfg)
	if got, ok := re.TrustedTimeSource(); !ok || got.FabricIndex != 1 {
		t.Errorf("after a restart the source is %+v (%v), want fabric 1's", got, ok)
	}
}

// TestSetTrustedTimeSourceRefusedStoreReportsNothing: SetTrustedTimeSource
// whose store is refused answers SUCCESS and serves the source, as chip,
// but reports no change of TrustedTimeSource (review II, R4-store-4152,
// decision 603).
func TestSetTrustedTimeSourceRefusedStoreReportsNothing(t *testing.T) {
	st := &tsSettings{m: map[string]string{}, fail: true}
	srv, err := core.NewTimeSynchronizationWith(core.TimeSynchronizationConfig{Features: tsdef.FeatureTimeSyncClient, Store: st})
	if err != nil {
		t.Fatal(err)
	}
	var reported [][]uint32
	srv.OnAttributeChange(func(c contract.Change) { reported = append(reported, c.Attributes) })
	if err := setTrustedOn(t, srv, 1); err != nil {
		t.Errorf("answer %v, want SUCCESS", err)
	}
	if _, ok := srv.TrustedTimeSource(); !ok {
		t.Error("the source is not served after the refused store (memory first, as chip)")
	}
	if len(reported) != 0 {
		t.Errorf("reported %v after the refused store, want nothing", reported)
	}
	st.mu.Lock()
	st.fail = false
	st.mu.Unlock()
	if err := setTrustedOn(t, srv, 1); err != nil {
		t.Fatal(err)
	}
	if len(reported) != 1 || !slices.Contains(reported[0], tsdef.AttrTrustedTimeSource) {
		t.Errorf("reported %v after the stored set, want TrustedTimeSource once", reported)
	}
}

// TestTimeSyncClearedSourceReloadsAsNone: a source cleared by a fabric's
// drop is stored as null, which a restart loads as no source, not as a
// zero source of fabric 0 (found with R4-store-4151).
func TestTimeSyncClearedSourceReloadsAsNone(t *testing.T) {
	st := &tsSettings{m: map[string]string{}}
	cfg := core.TimeSynchronizationConfig{Features: tsdef.FeatureTimeSyncClient, Store: st}
	srv, err := core.NewTimeSynchronizationWith(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := setTrustedOn(t, srv, 2); err != nil {
		t.Fatal(err)
	}
	if err := srv.MatterDropFabric(context.Background(), 2); err != nil {
		t.Fatal(err)
	}
	re, err := core.NewTimeSynchronizationWith(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := re.TrustedTimeSource(); ok {
		t.Errorf("a restart after the clear serves the source %+v, want none", got)
	}
	if held := re.MatterHeldFabrics(); len(held) != 0 {
		t.Errorf("a restart after the clear holds fabrics %v, want none", held)
	}
}

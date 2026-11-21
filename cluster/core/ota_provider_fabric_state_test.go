// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bdx"

	provdef "github.com/SukramJ/go-fabric/cluster/spec/otasoftwareupdateprovider"
)

// TestOTAProviderDropFabricForgetsItsUpdates (review II, R2-P6-bound-1206,
// owner decision 167): the provider is fabric-scoped state — it names the
// fabrics its manager holds update attempts of, and a removed fabric's
// attempts end; another fabric's stay.
func TestOTAProviderDropFabricForgetsItsUpdates(t *testing.T) {
	t.Parallel()
	f := newOTAFixture(t, nil)
	if r := f.query(t, 7, bdxQuery()); r.Status != provdef.StatusUpdateAvailable {
		t.Fatalf("query %+v", r)
	}
	other := OTAPeer{FabricIndex: otaTestFabric + 1, NodeID: 5}
	f.p.mgr.ApplyUpdate(context.Background(), other, []byte{9}, 9, 0)
	if got := f.p.MatterHeldFabrics(); !slices.Equal(got, []uint8{otaTestFabric, otaTestFabric + 1}) {
		t.Fatalf("held %v", got)
	}
	if err := f.p.MatterDropFabric(context.Background(), otaTestFabric); err != nil {
		t.Fatal(err)
	}
	if a := f.p.mgr.AttemptFor(other, []byte{9}); a == nil || f.attempts() != 1 {
		t.Errorf("after the drop: %d attempts, the other fabric's kept %v", f.attempts(), a != nil)
	}
	if got := f.p.MatterHeldFabrics(); !slices.Equal(got, []uint8{otaTestFabric + 1}) {
		t.Errorf("held after the drop %v", got)
	}
}

// TestOTAProviderDropFabricWithdrawsTheTransferScope (review II,
// R2-P6-bound-1206 round 4): a removed fabric's peer loses the BDX scope
// its query enabled, and a transfer of it that still runs is closed.
func TestOTAProviderDropFabricWithdrawsTheTransferScope(t *testing.T) {
	t.Parallel()
	f := newOTAFixture(t, nil)
	r := f.query(t, 7, bdxQuery())
	_, done := f.startDownload(t, 7, *r.ImageUri)
	if err := f.p.MatterDropFabric(context.Background(), otaTestFabric); err != nil {
		t.Fatal(err)
	}
	limit, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	select {
	case <-done:
	case <-limit.Done():
		t.Error("the removed fabric's transfer still runs")
		_ = f.bdx.DisablePeerForScope(bdx.Peer{FabricIndex: otaTestFabric, NodeID: otaTestPeerNode}, otaScope, true)
		<-done
	}
	if s := f.bdx.SessionFor(bdx.Peer{FabricIndex: otaTestFabric, NodeID: otaTestPeerNode}, otaScope); s != nil {
		t.Error("a session of the removed fabric's peer is still listed")
	}
}

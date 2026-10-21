// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"crypto/rand"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bridge"
	"github.com/SukramJ/go-fabric/client/interaction"
	admincommdef "github.com/SukramJ/go-fabric/cluster/spec/administratorcommissioning"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/store"
)

var errChainNotLoaded = errors.New("vendor attestation chain did not load")

// TestCommissioningBlockedUncommissioned (#133): a block set before
// Start keeps an uncommissioned node's own window closed and its CM
// record unpublished, refuses every other way to open a window, and,
// lifted, lets the own window open at once.
func TestCommissioningBlockedUncommissioned(t *testing.T) {
	ctx := context.Background()
	adv := mdns.NewNoop()
	cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db")))
	cfg.Advertiser = adv
	n := newNode(t, cfg)
	n.SetCommissioningBlocked(ctx, errChainNotLoaded)
	if err := n.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	if _, _, ok := commissionableRecord(adv); ok || n.CommissioningWindow().IsOpen() {
		t.Fatal("a blocked uncommissioned node opened its own window / advertised CM")
	}
	if err := n.OpenBasicWindow(ctx, randomCredentials(t, time.Minute)); !errors.Is(err, bridge.ErrCommissioningBlocked) || !errors.Is(err, errChainNotLoaded) {
		t.Errorf("OpenBasicWindow: %v, want ErrCommissioningBlocked wrapping the host's cause", err)
	}
	if err := n.CommissioningWindow().OpenOwnWindow(ctx, time.Minute); !errors.Is(err, bridge.ErrCommissioningBlocked) {
		t.Errorf("OpenOwnWindow: %v, want ErrCommissioningBlocked", err)
	}
	if err := n.CommissioningWindow().OpenWindow(ctx, wire.OpenWindowParams{CommissioningTimeoutSeconds: 180}); !errors.Is(err, bridge.ErrCommissioningBlocked) {
		t.Errorf("OpenWindow: %v, want ErrCommissioningBlocked", err)
	}

	n.SetCommissioningBlocked(ctx, nil)
	waitRecord(t, adv, "the own window after the block is lifted", func(_, mode uint64, ok bool) bool { return ok && mode == 1 })
}

// TestCommissioningBlockedRefusesAdministrator (#133): on a commissioned
// node a paired controller's AdministratorCommissioning
// OpenCommissioningWindow answers FAILURE and opens nothing, an open
// window is revoked when the block is set, and the controller keeps its
// session.
func TestCommissioningBlockedRefusesAdministrator(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	n := liveNode(t, "blocked", nil)
	ctl := newCCFabric(t, 0x0000_0133_0000_0001, 0x0000_0133_0000_0011, "blocked controller")
	ix, err := ctl.commission(ctx, udpAddrOf(t, n), 0x0000_0133_0000_0021, "blocked", otaLivePasscode, 0)
	if err != nil {
		t.Fatalf("commission: %v", err)
	}

	// An open window is revoked when the block is set.
	if err := n.OpenBasicWindow(ctx, randomCredentials(t, time.Minute)); err != nil {
		t.Fatalf("OpenBasicWindow before the block: %v", err)
	}
	n.SetCommissioningBlocked(ctx, errChainNotLoaded)
	if n.CommissioningWindow().IsOpen() {
		t.Fatal("the open window survived the block")
	}

	verifier := make([]byte, 97)
	salt := make([]byte, 32)
	_, _ = rand.Read(verifier)
	_, _ = rand.Read(salt)
	_, err = ix.Invoke(ctx, im.ConcreteCommandPath{Endpoint: 0, Cluster: admincommdef.ClusterID, Command: admincommdef.CmdOpenCommissioningWindow},
		admincommdef.OpenCommissioningWindowRequest{
			CommissioningTimeout: 180, PakePasscodeVerifier: verifier, Discriminator: 0x0133, Iterations: 1000, Salt: salt,
		}, interaction.InvokeOptions{Timed: true, TimedTimeout: 5 * time.Second})
	var se *interaction.StatusError
	if !errors.As(err, &se) || se.Status != im.StatusFailure || se.ClusterStatus != nil {
		t.Fatalf("OpenCommissioningWindow while blocked: %v, want FAILURE without a cluster status", err)
	}
	if n.CommissioningWindow().IsOpen() {
		t.Error("OpenCommissioningWindow opened a window while blocked")
	}

	// The paired controller still reads.
	if _, err := ix.Read(ctx, im.ConcreteAttributePath{Endpoint: 0, Cluster: 0x0028, Attribute: 0x0005, HasEndpoint: true, HasCluster: true, HasAttribute: true}); err != nil {
		t.Errorf("Read by the paired controller while blocked: %v", err)
	}

	n.SetCommissioningBlocked(ctx, nil)
	if err := n.OpenBasicWindow(ctx, randomCredentials(t, time.Minute)); err != nil {
		t.Errorf("OpenBasicWindow after the block is lifted: %v", err)
	}
}

// TestStartDoesNotOpenWindowWhenStoredFabricsFailToLoad: a node whose
// fabric table holds a fabric that cannot load its CASE identity is a
// broken commissioned node, not a new one — Start advertises no
// commissionable record (matter.js asks whether the fabric table is
// empty, CommissioningServer enterCommissionableMode).
func TestStartDoesNotOpenWindowWhenStoredFabricsFailToLoad(t *testing.T) {
	ctx := context.Background()
	db := openDB(t, filepath.Join(t.TempDir(), "node.db"))
	if err := store.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := store.New(db).AddFabric(ctx, store.FabricRecord{FabricID: 0x0133, NodeID: 0x0134, VendorID: 0xFFF1, Label: "broken", RootPublicKey: make([]byte, 65), CompressedID: [8]byte{1}}); err != nil {
		t.Fatalf("AddFabric: %v", err)
	}
	adv := mdns.NewNoop()
	cfg := minimalConfig(db)
	cfg.Advertiser = adv
	n := newNode(t, cfg)
	if err := n.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	if _, _, ok := commissionableRecord(adv); ok || n.CommissioningWindow().IsOpen() {
		t.Fatal("a node whose stored fabric failed to load opened its commissioning window")
	}
}

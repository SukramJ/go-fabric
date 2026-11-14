// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/bridge/bridgetest"
	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/store"
)

// TestRestartRollsBackAnUncommittedFabricWithItsRemoval pins review II
// R2-P2-comm-03: a node goes down after AddNOC and before
// CommissioningComplete committed the fabric — here CommissioningComplete
// fails because the store refuses its commit, and the node is never
// stopped, as a power cut leaves it. The node that comes back on the same
// database rolls the fabric back and runs its removal consequences before
// it serves: the host's OnFabricRemoved fires once, with the fabric's
// identity, as connectedhomeip's Server::Init schedules the fail-safe
// cleanup for the fabric FabricTable::Init deleted
// (src/app/server/Server.cpp:662-685, src/credentials/FabricTable.cpp:1129-1140
// at 6170af84).
func TestRestartRollsBackAnUncommittedFabricWithItsRemoval(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "node.db")
	db := openDB(t, path)
	n := newNode(t, minimalConfig(db))
	if err := n.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// The commit CommissioningComplete makes is refused — the fabric row's
	// commit, and for a release that kept the commit state in the settings
	// the clear of the pending-NOC marker — and so is every removal of a
	// fabric, so the rollback the failed commissioning runs in this run
	// leaves the fabric too: the state of a node that loses power before
	// either lands.
	stmts := []string{
		`CREATE TRIGGER refuse_commit BEFORE UPDATE OF pending ON matter_fabrics WHEN NEW.pending = 0 AND OLD.pending <> 0 BEGIN SELECT RAISE(ABORT, 'test: commit refused'); END`,
		`CREATE TRIGGER refuse_marker_clear BEFORE UPDATE ON matter_settings WHEN NEW.key LIKE 'operational_credentials.pending_noc%' AND NEW.value = '' BEGIN SELECT RAISE(ABORT, 'test: commit refused'); END`,
		`CREATE TRIGGER refuse_removal BEFORE DELETE ON matter_fabrics BEGIN SELECT RAISE(ABORT, 'test: removal refused'); END`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	c := bridgetest.Dial(t, n.Bridge().LocalAddr(), commissionerOptions())
	if _, err := c.Commission(context.Background()); err == nil {
		t.Fatal("Commission succeeded although the store refused CommissioningComplete's commit")
	}
	if fabrics, err := n.FabricInfos(context.Background()); err != nil || len(fabrics) != 1 {
		t.Fatalf("fabrics before the restart: %+v, %v; want the uncommitted one", fabrics, err)
	}
	// Power is back: the store works again.
	for _, trigger := range []string{"refuse_commit", "refuse_marker_clear", "refuse_removal"} {
		if _, err := db.Exec(`DROP TRIGGER ` + trigger); err != nil {
			t.Fatal(err)
		}
	}

	var (
		mu      sync.Mutex
		removed []node.FabricInfo
	)
	again := newNode(t, minimalConfig(openDB(t, path)))
	again.Observe(node.Observers{OnFabricRemoved: func(info node.FabricInfo) {
		mu.Lock()
		removed = append(removed, info)
		mu.Unlock()
	}})
	if err := again.Start(context.Background()); err != nil {
		t.Fatalf("Start after the restart: %v", err)
	}
	if fabrics, err := again.FabricInfos(context.Background()); err != nil || len(fabrics) != 0 {
		t.Fatalf("fabrics after the restart: %+v, %v; want none", fabrics, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(removed) != 1 || removed[0].FabricID != testFabricID || removed[0].NodeID != testNodeID {
		t.Fatalf("OnFabricRemoved after the restart: %+v; want once, for fabric 0x%X node 0x%X", removed, testFabricID, testNodeID)
	}
}

// TestRestartRollsBackAnUncommittedUpdateWithItsConsequences pins review
// II R2-P2-dr-noc-05: a node goes down after UpdateNOC and before
// CommissioningComplete committed it, with a resumption record made under
// the uncommitted identity. The node that comes back on the same database
// rolls the update back and runs what the run-time rollback of an update
// runs before it serves: the fabric's resumption records go, and the
// host's OnFabricUpdated fires once, with the fabric as it stands again.
func TestRestartRollsBackAnUncommittedUpdateWithItsConsequences(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "node.db")
	db := openDB(t, path)
	_, c, _ := commissioned(t, minimalConfig(db))
	idx := c.FabricIndex()
	// The state an UpdateNOC and a CASE session under its identity leave
	// in the store when the node loses power before CommissioningComplete.
	st := store.New(db)
	id, err := st.GetIdentity(ctx, idx)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateFabricCredentials(ctx, idx, store.FabricCredentials{NOC: id.NOC, ICAC: id.ICAC, PrivateKey: id.PrivateKey, NodeID: testNodeID + 1}); err != nil {
		t.Fatal(err)
	}
	const peer = 0x77
	if err := st.UpsertResumption(ctx, store.ResumptionRecord{FabricIndex: idx, PeerNodeID: peer, ResumptionID: bytes.Repeat([]byte{1}, 16), SharedSecret: bytes.Repeat([]byte{2}, 32)}); err != nil {
		t.Fatal(err)
	}

	var (
		mu      sync.Mutex
		updated []node.FabricInfo
	)
	again := newNode(t, minimalConfig(openDB(t, path)))
	again.Observe(node.Observers{OnFabricUpdated: func(info node.FabricInfo) {
		mu.Lock()
		updated = append(updated, info)
		mu.Unlock()
	}})
	if err := again.Start(ctx); err != nil {
		t.Fatalf("Start after the restart: %v", err)
	}
	if _, err := st.GetResumptionByPeer(ctx, idx, peer); !errors.Is(err, store.ErrResumptionNotFound) {
		t.Errorf("the resumption record made under the rolled-back update after the restart: %v; want ErrResumptionNotFound", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(updated) != 1 || updated[0].FabricIndex != idx || updated[0].NodeID != testNodeID {
		t.Fatalf("OnFabricUpdated after the restart: %+v; want once, for fabric %d node 0x%X", updated, idx, testNodeID)
	}
}

// TestWiFiNetworkListIsStoredBeforeTheFabricCommit pins review II
// R2-P2-store-01: a Wi-Fi node stores its network list before
// CommissioningComplete commits the fabric, and a store that refuses the
// list fails CommissioningComplete with nothing committed — a restart
// finds no fabric — instead of a fabric committed on a node whose network
// is gone after its next restart.
func TestWiFiNetworkListIsStoredBeforeTheFabricCommit(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "node.db")
	db := openDB(t, path)
	cfg := minimalConfig(db)
	cfg.Network.WiFi = &node.WiFi{Driver: joinLab{}}
	n := newNode(t, cfg)
	if err := n.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	for _, stmt := range []string{
		`CREATE TRIGGER refuse_networks_insert BEFORE INSERT ON matter_settings WHEN NEW.key = 'netcomm.wifi.networks' BEGIN SELECT RAISE(ABORT, 'test: disk full'); END`,
		`CREATE TRIGGER refuse_networks_update BEFORE UPDATE ON matter_settings WHEN NEW.key = 'netcomm.wifi.networks' BEGIN SELECT RAISE(ABORT, 'test: disk full'); END`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	c := bridgetest.Dial(t, n.Bridge().LocalAddr(), commissionerOptions())
	if _, err := c.Commission(context.Background()); err == nil {
		t.Fatal("Commission succeeded although the node could not store its network list")
	}
	again := newNode(t, minimalConfig(openDB(t, path)))
	if fabrics, err := again.FabricInfos(context.Background()); err != nil || len(fabrics) != 0 {
		t.Fatalf("fabrics after the restart: %+v, %v; want none — the commissioning answered FAILURE", fabrics, err)
	}
}

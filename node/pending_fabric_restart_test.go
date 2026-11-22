// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"thingfabric.dev/go-fabric/bridge/bridgetest"
	"thingfabric.dev/go-fabric/node"
	"thingfabric.dev/go-fabric/store"
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

// TestRestartBetweenNewAndStartKeepsTheUpdatesStoreConsequence pins review
// II R2-P2-rr-dev-09: the node built on a database with an uncommitted
// UpdateNOC rolls the update back in New; a process that ends before
// Start — the host's own start-up failed — has committed the row back, so
// the next process finds nothing to roll back. The store-side consequence
// of the rollback, the removal of the resumption records made under the
// uncommitted identity, has run in New already, in the rollback's own
// write (store.Fabrics.RollbackFabric): no later process could run it.
func TestRestartBetweenNewAndStartKeepsTheUpdatesStoreConsequence(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "node.db")
	db := openDB(t, path)
	_, c, _ := commissioned(t, minimalConfig(db))
	idx := c.FabricIndex()
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

	newNode(t, minimalConfig(openDB(t, path))) // never started: the process ends
	if _, err := st.GetResumptionByPeer(ctx, idx, peer); !errors.Is(err, store.ErrResumptionNotFound) {
		t.Fatalf("the resumption record made under the update New rolled back: %v; want ErrResumptionNotFound", err)
	}
	again := newNode(t, minimalConfig(openDB(t, path)))
	if err := again.Start(ctx); err != nil {
		t.Fatalf("Start of the next process: %v", err)
	}
	if _, err := st.GetResumptionByPeer(ctx, idx, peer); !errors.Is(err, store.ErrResumptionNotFound) {
		t.Fatalf("the resumption record made under the rolled-back update in the next process: %v; want ErrResumptionNotFound", err)
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

// seedPendingAdd stores, on the database at path, the row an AddNOC
// leaves that no CommissioningComplete committed: the state of a node that
// lost power inside the fail-safe window.
func seedPendingAdd(t *testing.T, path string) store.FabricRecord {
	t.Helper()
	ctx := context.Background()
	db := openDB(t, path)
	if err := store.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	key := append([]byte{0x04}, bytes.Repeat([]byte{0x11}, 64)...)
	rec := store.FabricRecord{
		FabricID: testFabricID, NodeID: testNodeID, RootPublicKey: key,
		VendorID: 0xFFF1, CompressedID: [8]byte{1, 2, 3, 4, 5, 6, 7, 8},
		Pending: store.FabricPendingAdd,
	}
	idx, err := store.New(db).AddFabric(ctx, rec)
	if err != nil {
		t.Fatal(err)
	}
	rec.FabricIndex = idx
	return rec
}

// removalRecorder records every OnFabricRemoved a node runs.
type removalRecorder struct {
	mu      sync.Mutex
	removed []node.FabricInfo
}

func (r *removalRecorder) observe(n *node.Node) {
	n.Observe(node.Observers{OnFabricRemoved: func(info node.FabricInfo) {
		r.mu.Lock()
		r.removed = append(r.removed, info)
		r.mu.Unlock()
	}})
}

func (r *removalRecorder) take() []node.FabricInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.removed
	r.removed = nil
	return out
}

// startObserved builds a node on cfg, records its removals and starts it.
func startObserved(t *testing.T, cfg node.Config) []node.FabricInfo {
	t.Helper()
	var rec removalRecorder
	n := newNode(t, cfg)
	rec.observe(n)
	if err := n.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := n.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	return rec.take()
}

// TestOwedFabricRemovalOutlivesAProcessThatNeverStarted pins review II
// R2-P5-port-18 (decision 109): the node built on a database with an
// uncommitted AddNOC rolls the fabric back in New, and the removal it owes
// is kept in the store with the rollback. A process that ends before
// Start — the host's own start-up failed — loses nothing: the next
// process's Start runs OnFabricRemoved once, with the fabric's identity,
// and acknowledges it, so a third start runs nothing.
func TestOwedFabricRemovalOutlivesAProcessThatNeverStarted(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "node.db")
	seeded := seedPendingAdd(t, path)

	newNode(t, minimalConfig(openDB(t, path))) // never started: the process ends

	removed := startObserved(t, minimalConfig(openDB(t, path)))
	if len(removed) != 1 || removed[0].FabricIndex != seeded.FabricIndex || removed[0].FabricID != testFabricID || removed[0].NodeID != testNodeID {
		t.Fatalf("OnFabricRemoved in the next process: %+v; want once, for fabric %d", removed, seeded.FabricIndex)
	}
	if owed, err := store.New(openDB(t, path)).OwedFabricEvents(context.Background()); err != nil || len(owed) != 0 {
		t.Fatalf("owed events after the delivering start: %+v, %v; want none", owed, err)
	}
	if again := startObserved(t, minimalConfig(openDB(t, path))); len(again) != 0 {
		t.Fatalf("OnFabricRemoved in a third process: %+v; want none", again)
	}
}

// ackRefusingStore is a store whose acknowledgement of an owed event
// fails: a node that ran the hook and lost power before the
// acknowledgement landed.
type ackRefusingStore struct{ store.Port }

func (ackRefusingStore) AckOwedFabricEvent(context.Context, uint64) error {
	return errors.New("test: power lost before the acknowledgement")
}

// TestOwedFabricRemovalReplaysAfterACrashBehindTheHook pins the at-least-
// once delivery of decision 109: a start runs OnFabricRemoved and goes
// down before its acknowledgement landed; the next start runs it again,
// and acknowledges it.
func TestOwedFabricRemovalReplaysAfterACrashBehindTheHook(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "node.db")
	seeded := seedPendingAdd(t, path)

	db := openDB(t, path)
	cfg := minimalConfig(db)
	cfg.Store.Credentials = ackRefusingStore{store.New(db)}
	if first := startObserved(t, cfg); len(first) != 1 || first[0].FabricIndex != seeded.FabricIndex {
		t.Fatalf("OnFabricRemoved in the first process: %+v; want once", first)
	}
	if again := startObserved(t, minimalConfig(openDB(t, path))); len(again) != 1 || again[0].FabricIndex != seeded.FabricIndex {
		t.Fatalf("OnFabricRemoved after the crash behind the hook: %+v; want it delivered again, once", again)
	}
	if third := startObserved(t, minimalConfig(openDB(t, path))); len(third) != 0 {
		t.Fatalf("OnFabricRemoved once acknowledged: %+v; want none", third)
	}
}

// lookupFailingStore is a store whose GetFabric of one fabric index fails
// with an error that is not store.ErrFabricNotFound — the store could not
// answer — and counts the acknowledgements of owed events.
type lookupFailingStore struct {
	store.Port
	index uint8
	acks  atomic.Int32
}

func (s *lookupFailingStore) GetFabric(ctx context.Context, fabricIndex uint8) (store.FabricRecord, error) {
	if fabricIndex == s.index {
		return store.FabricRecord{}, errors.New("test: the store could not answer")
	}
	return s.Port.GetFabric(ctx, fabricIndex)
}

func (s *lookupFailingStore) AckOwedFabricEvent(ctx context.Context, id uint64) error {
	s.acks.Add(1)
	return s.Port.AckOwedFabricEvent(ctx, id)
}

// TestOwedFabricRemovalWaitsWhenTheStoreCannotAnswer: an owed removal runs
// only when the store answers that the fabric is gone. A start whose
// lookup of the fabric fails otherwise runs nothing for it and leaves the
// event owed; the next start, whose store answers, runs OnFabricRemoved
// once and acknowledges it.
func TestOwedFabricRemovalWaitsWhenTheStoreCannotAnswer(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "node.db")
	seeded := seedPendingAdd(t, path)

	newNode(t, minimalConfig(openDB(t, path))) // rolls the fabric back, owes its removal

	db := openDB(t, path)
	cfg := minimalConfig(db)
	failing := &lookupFailingStore{Port: store.New(db), index: seeded.FabricIndex}
	cfg.Store.Credentials = failing
	if first := startObserved(t, cfg); len(first) != 0 {
		t.Fatalf("OnFabricRemoved while the store could not answer: %+v; want none", first)
	}
	if n := failing.acks.Load(); n != 0 {
		t.Fatalf("%d acknowledgements while the store could not answer; want none", n)
	}
	if owed, err := store.New(openDB(t, path)).OwedFabricEvents(context.Background()); err != nil || len(owed) != 1 {
		t.Fatalf("owed events after that start: %+v, %v; want the removal still owed", owed, err)
	}
	if next := startObserved(t, minimalConfig(openDB(t, path))); len(next) != 1 || next[0].FabricIndex != seeded.FabricIndex {
		t.Fatalf("OnFabricRemoved once the store answers: %+v; want once, for fabric %d", next, seeded.FabricIndex)
	}
}

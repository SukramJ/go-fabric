// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
	mstore "github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/store/memstore"

	_ "modernc.org/sqlite"
)

// restartStore is what the restart tests read and write: the cluster's
// store, its settings, and the tables a revert must empty.
type restartStore interface {
	core.StoreFacade
	GetSetting(ctx context.Context, key string) (string, bool, error)
	SetSetting(ctx context.Context, key, value string) error
	UpdateFabricVendorID(ctx context.Context, fabricIndex uint8, vendorID uint16) error
	ListACL(ctx context.Context, fabricIndex uint8) ([]mstore.ACLEntry, error)
	ListGroupKeySets(ctx context.Context, fabricIndex uint8) ([]mstore.GroupKeySet, error)
}

// restartStores are the two stores a node runs on: the in-memory one and
// the SQLite one (whose fabric removal cascades by foreign key).
var restartStores = map[string]func(t *testing.T) restartStore{
	"memstore": func(*testing.T) restartStore { return memstore.New() },
	"sqlite": func(t *testing.T) restartStore {
		t.Helper()
		db, err := mstore.OpenSQLite(filepath.Join(t.TempDir(), "matter.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close() })
		if err := mstore.Apply(context.Background(), db); err != nil {
			t.Fatal(err)
		}
		return mstore.New(db)
	},
}

var errCrashed = errors.New("crashed: the node is down")

// crashingStore is a store whose writes stop reaching the store after
// the crashAfter-th one: that write lands, every later write fails, as a
// node that loses power right after it. crashAfter < 0 never crashes; the
// writes are counted either way.
type crashingStore struct {
	restartStore
	mu         sync.Mutex
	crashAfter int
	writes     int
}

func (c *crashingStore) write(fn func() error) error {
	c.mu.Lock()
	if c.crashAfter >= 0 && c.writes >= c.crashAfter {
		c.mu.Unlock()
		return errCrashed
	}
	c.writes++
	c.mu.Unlock()
	return fn()
}

func (c *crashingStore) SetSetting(ctx context.Context, key, value string) error {
	return c.write(func() error { return c.restartStore.SetSetting(ctx, key, value) })
}

func (c *crashingStore) AddFabric(ctx context.Context, rec mstore.FabricRecord) (uint8, error) {
	var idx uint8
	err := c.write(func() error {
		var err error
		idx, err = c.restartStore.AddFabric(ctx, rec)
		return err
	})
	return idx, err
}

func (c *crashingStore) UpdateFabricNodeID(ctx context.Context, fabricIndex uint8, nodeID uint64) error {
	return c.write(func() error { return c.restartStore.UpdateFabricNodeID(ctx, fabricIndex, nodeID) })
}

func (c *crashingStore) UpdateFabricVendorID(ctx context.Context, fabricIndex uint8, vendorID uint16) error {
	return c.write(func() error { return c.restartStore.UpdateFabricVendorID(ctx, fabricIndex, vendorID) })
}

func (c *crashingStore) RemoveFabric(ctx context.Context, fabricIndex uint8) error {
	return c.write(func() error { return c.restartStore.RemoveFabric(ctx, fabricIndex) })
}

func (c *crashingStore) UpsertIdentity(ctx context.Context, rec mstore.IdentityRecord) error {
	return c.write(func() error { return c.restartStore.UpsertIdentity(ctx, rec) })
}

func (c *crashingStore) ReplaceACL(ctx context.Context, fabricIndex uint8, entries []mstore.ACLEntry) error {
	return c.write(func() error { return c.restartStore.ReplaceACL(ctx, fabricIndex, entries) })
}

func (c *crashingStore) UpsertGroupKeySet(ctx context.Context, rec mstore.GroupKeySet) error {
	return c.write(func() error { return c.restartStore.UpsertGroupKeySet(ctx, rec) })
}

func (c *crashingStore) UpdateFabricCredentials(ctx context.Context, fabricIndex uint8, cr mstore.FabricCredentials) error {
	return c.write(func() error { return c.restartStore.UpdateFabricCredentials(ctx, fabricIndex, cr) })
}

func (c *crashingStore) UpdateFabricVIDVerification(ctx context.Context, fabricIndex uint8, statement, vvsc []byte) error {
	return c.write(func() error { return c.restartStore.UpdateFabricVIDVerification(ctx, fabricIndex, statement, vvsc) })
}

func (c *crashingStore) CommitFabric(ctx context.Context, fabricIndex uint8) error {
	return c.write(func() error { return c.restartStore.CommitFabric(ctx, fabricIndex) })
}

func (c *crashingStore) RollbackFabric(ctx context.Context, fabricIndex uint8) (mstore.FabricPending, error) {
	was := mstore.FabricCommitted
	err := c.write(func() error {
		var err error
		was, err = c.restartStore.RollbackFabric(ctx, fabricIndex)
		return err
	})
	return was, err
}

func (c *crashingStore) SetSettings(ctx context.Context, values map[string]string) error {
	return c.write(func() error { return c.restartStore.SetSettings(ctx, values) })
}

func (c *crashingStore) RemoveGroupKeysByFabric(ctx context.Context, fabricIndex uint8) error {
	return c.write(func() error { return c.restartStore.RemoveGroupKeysByFabric(ctx, fabricIndex) })
}

// restartOpcreds is the node coming back up: a new server over what the
// crashed one left in the store.
func restartOpcreds(t *testing.T, st restartStore) *core.OperationalCredentials {
	t.Helper()
	oc, err := core.NewOperationalCredentials(st, core.OpcredsConfig{SupportedFabrics: 5})
	if err != nil {
		t.Fatalf("NewOperationalCredentials after the restart: %v", err)
	}
	return oc
}

// addNOCWrites counts the store writes a clean AddNOC makes.
func addNOCWrites(t *testing.T, open func(*testing.T) restartStore) int {
	t.Helper()
	cs := &crashingStore{restartStore: open(t), crashAfter: -1}
	oc, err := core.NewOperationalCredentials(cs, core.OpcredsConfig{SupportedFabrics: 5, FailSafe: armedFailSafe(0)})
	if err != nil {
		t.Fatal(err)
	}
	commissionTestFabric(context.Background(), t, oc)
	return cs.writes
}

// TestOpcreds_RestartBeforeCommissioningComplete_RevertsAddNOC pins
// R2-P1-core-01: a node that goes down anywhere between AddNOC's first
// store write and CommissioningComplete comes back without the fabric, its
// operational key, its IPK key set and its Administer ACL entry — what the
// fail-safe would have rolled back. Both references keep the new fabric
// pending until CommissioningComplete (matter.js FailsafeContext.ts:141-143
// at db021da0, completeCommission → persistFabrics; chip
// OperationalCredentialsCluster.cpp:507 at 6170af84,
// AddNewPendingFabricWithOperationalKeystore).
func TestOpcreds_RestartBeforeCommissioningComplete_RevertsAddNOC(t *testing.T) {
	t.Parallel()
	for name, open := range restartStores {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			testRestartRevertsAddNOC(t, open)
		})
	}
}

func testRestartRevertsAddNOC(t *testing.T, open func(*testing.T) restartStore) {
	t.Helper()
	total := addNOCWrites(t, open)
	// AddFabric (pending), the identity, the IPK key set, the ACL.
	if total < 4 {
		t.Fatalf("a clean AddNOC made %d store writes, want at least 4", total)
	}
	for crashAfter := 1; crashAfter <= total; crashAfter++ {
		t.Run("", func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			inner := open(t)
			cs := &crashingStore{restartStore: inner, crashAfter: crashAfter}
			oc, err := core.NewOperationalCredentials(cs, core.OpcredsConfig{SupportedFabrics: 5, FailSafe: armedFailSafe(0)})
			if err != nil {
				t.Fatal(err)
			}
			// The commissioner's sequence; the AddNOC answer is whatever the
			// crash left it (the node is down, nobody reads it).
			commissionTestFabricTolerant(ctx, t, oc)

			restartOpcreds(t, inner)
			fabrics, err := inner.ListFabrics(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if len(fabrics) != 0 {
				t.Fatalf("crash after write %d of %d: %d fabric(s) survived the restart without CommissioningComplete", crashAfter, total, len(fabrics))
			}
			for idx := uint8(1); idx <= 2; idx++ {
				if _, err := inner.GetIdentity(ctx, idx); !errors.Is(err, mstore.ErrIdentityNotFound) {
					t.Errorf("crash after write %d: the identity of fabric %d survived (err=%v)", crashAfter, idx, err)
				}
				if acl, _ := inner.ListACL(ctx, idx); len(acl) != 0 {
					t.Errorf("crash after write %d: %d ACL entries of fabric %d survived", crashAfter, len(acl), idx)
				}
				if sets, _ := inner.ListGroupKeySets(ctx, idx); len(sets) != 0 {
					t.Errorf("crash after write %d: %d group key sets of fabric %d survived", crashAfter, len(sets), idx)
				}
			}
			// A second restart finds nothing left to revert.
			restartOpcreds(t, inner)
		})
	}
}

// commissionTestFabricTolerant runs commissionTestFabric's sequence but
// accepts any AddNOC answer: a crash inside AddNOC fails it.
func commissionTestFabricTolerant(ctx context.Context, t *testing.T, oc *core.OperationalCredentials) {
	t.Helper()
	_, _ = oc.MatterInvoke(ctx, 0x06, prepareAddNOC(ctx, t, oc))
}

// prepareAddNOC runs AddTrustedRootCertificate and CSRRequest on oc and
// returns the AddNOC request for a NOC minted over the pending key.
func prepareAddNOC(ctx context.Context, t *testing.T, oc *core.OperationalCredentials) core.AddNOCRequest {
	t.Helper()
	rootPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rootRaw := buildCoreSignedCert(t, rootPriv, true, rootPriv)
	if _, err := oc.MatterInvoke(ctx, 0x0B, core.AddTrustedRootCertificateRequest{RootCACertificate: rootRaw}); err != nil {
		t.Fatalf("AddTrustedRootCertificate: %v", err)
	}
	pendingPub := issueCSRPendingPubKey(ctx, t, oc, false)
	return core.AddNOCRequest{
		NOCValue:         buildCoreSignedCertForPubKey(t, pendingPub, false, rootPriv, testDefaultFabricID, testDefaultNodeID),
		IPKValue:         make([]byte, 16),
		CaseAdminSubject: 0xABCD,
		AdminVendorID:    0x1234,
	}
}

// TestOpcreds_RestartAfterCommissioningComplete_KeepsFabric is the
// counterpart: a committed fabric survives the restart.
func TestOpcreds_RestartAfterCommissioningComplete_KeepsFabric(t *testing.T) {
	t.Parallel()
	for name, open := range restartStores {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			testRestartKeepsFabric(t, open(t))
		})
	}
}

func testRestartKeepsFabric(t *testing.T, st restartStore) {
	t.Helper()
	ctx := context.Background()
	oc, err := core.NewOperationalCredentials(st, core.OpcredsConfig{SupportedFabrics: 5, FailSafe: armedFailSafe(0)})
	if err != nil {
		t.Fatal(err)
	}
	_, _, idx := commissionTestFabric(ctx, t, oc)
	commitForTest(t, oc) // CommissioningComplete
	restartOpcreds(t, st)
	if _, err := st.GetFabric(ctx, idx); err != nil {
		t.Fatalf("a committed fabric did not survive the restart: %v", err)
	}
	if acl, _ := st.ListACL(ctx, idx); len(acl) != 1 {
		t.Fatalf("a committed fabric's ACL has %d entries after the restart, want 1", len(acl))
	}
}

// TestOpcreds_RestartBeforeCommissioningComplete_RevertsUpdateNOC pins the
// UpdateNOC half of R2-P1-core-01: a node that goes down between
// UpdateNOC's first store write and CommissioningComplete comes back with
// the operational identity and node id the update replaced.
func TestOpcreds_RestartBeforeCommissioningComplete_RevertsUpdateNOC(t *testing.T) {
	t.Parallel()
	for name, open := range restartStores {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			testRestartRevertsUpdateNOC(t, open)
		})
	}
}

func testRestartRevertsUpdateNOC(t *testing.T, open func(*testing.T) restartStore) {
	t.Helper()
	// Count the writes of a clean UpdateNOC that moves the node id.
	updateWrites := func(t *testing.T, crashAfter int) (inner restartStore, before mstore.IdentityRecord, beforeNode uint64, idx uint8, writes int) {
		t.Helper()
		ctx := context.Background()
		inner = open(t)
		cs := &crashingStore{restartStore: inner, crashAfter: -1}
		oc, err := core.NewOperationalCredentials(cs, core.OpcredsConfig{SupportedFabrics: 5, FailSafe: armedFailSafe(0)})
		if err != nil {
			t.Fatal(err)
		}
		_, rootPriv, fi := commissionTestFabric(ctx, t, oc)
		commitForTest(t, oc) // CommissioningComplete
		before, _ = inner.GetIdentity(ctx, fi)
		fab, _ := inner.GetFabric(ctx, fi)
		fabCtx := im.WithFabricFilter(ctx, true, fi)
		nocRaw := mintUpdateNOC(fabCtx, t, oc, rootPriv, testDefaultFabricID, testDefaultNodeID+7)
		cs.mu.Lock()
		start := cs.writes
		if crashAfter >= 0 {
			cs.crashAfter = start + crashAfter
		}
		cs.mu.Unlock()
		_, _ = oc.MatterInvoke(fabCtx, 0x07, core.UpdateNOCRequest{NOCValue: nocRaw})
		cs.mu.Lock()
		writes = cs.writes - start
		cs.mu.Unlock()
		return inner, before, fab.NodeID, fi, writes
	}
	_, _, _, _, total := updateWrites(t, -1)
	// One: UpdateFabricCredentials, which marks the row pending with
	// what it replaced (owner decision 36).
	if total < 1 {
		t.Fatalf("a clean UpdateNOC made %d store writes, want at least 1", total)
	}
	for crashAfter := 1; crashAfter <= total; crashAfter++ {
		t.Run("", func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			inner, before, beforeNode, idx, _ := updateWrites(t, crashAfter)
			restartOpcreds(t, inner)
			after, err := inner.GetIdentity(ctx, idx)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(after.NOC, before.NOC) || !bytes.Equal(after.PrivateKey, before.PrivateKey) {
				t.Errorf("crash after write %d of %d: the uncommitted UpdateNOC's identity survived the restart", crashAfter, total)
			}
			if fab, _ := inner.GetFabric(ctx, idx); fab.NodeID != beforeNode {
				t.Errorf("crash after write %d of %d: node id 0x%X after the restart, want 0x%X", crashAfter, total, fab.NodeID, beforeNode)
			}
		})
	}
}

// failingRemoveStore is a store whose fabric removal fails.
type failingRemoveStore struct{ restartStore }

func (failingRemoveStore) RemoveFabric(context.Context, uint8) error { return errCrashed }

// syncBuffer is a bytes.Buffer safe for a logger's concurrent writes.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestOpcreds_RemoveFabricStoreFailureAfterLeaving pins R2-P1-core-11: a
// RemoveFabric whose store removal fails after the fabric-leaving hook ran
// — the Leave event is out, the fabric's subscriptions are flushed, and
// the fabric stays — says so at error level, and the fabric is still served.
func TestOpcreds_RemoveFabricStoreFailureAfterLeaving(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var logs syncBuffer
	st := failingRemoveStore{memstore.New()}
	oc, err := core.NewOperationalCredentials(st, core.OpcredsConfig{
		SupportedFabrics: 5, FailSafe: armedFailSafe(0),
		Logger: slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo})),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, idx := commissionTestFabric(ctx, t, oc)
	commitForTest(t, oc)
	oc.ClearPendingState()
	left := 0
	oc.SetOnFabricLeaving(func(context.Context, uint8) { left++ })
	if err := oc.RemoveFabric(ctx, idx); err == nil {
		t.Fatal("RemoveFabric succeeded over a failing store")
	}
	if left != 1 {
		t.Fatalf("the leaving hook ran %d times, want 1", left)
	}
	if !strings.Contains(logs.String(), "level=ERROR msg=matter.opcreds.remove_fabric.store_failed") {
		t.Fatalf("the failed removal after the Leave was not logged at error level:\n%s", logs.String())
	}
	if _, err := st.GetFabric(ctx, idx); err != nil {
		t.Fatalf("the fabric is gone although its removal failed: %v", err)
	}
}

// legacyMarker is the JSON a release before v8 wrote under
// operational_credentials.pending_noc[_update] (operational_credentials_pending.go).
type legacyMarker struct {
	Op            string `json:"op"`
	FabricIndex   uint8  `json:"fabric_index,omitempty"`
	FabricID      uint64 `json:"fabric_id"`
	RootPublicKey []byte `json:"root_public_key"`
	NOC           []byte `json:"noc,omitempty"`
	ICAC          []byte `json:"icac,omitempty"`
	PrivateKey    []byte `json:"private_key,omitempty"`
	IPK           []byte `json:"ipk,omitempty"`
	NodeID        uint64 `json:"node_id,omitempty"`
	VendorID      uint16 `json:"vendor_id,omitempty"`
}

// TestOpcreds_LegacyMarkerRolledBackAtStart pins the one path left for a
// database a release before v8 wrote in the middle of a commissioning
// (owner decision 36): the upgrade marked every fabric row committed, and
// the old marker names what was not. A start rolls back exactly what that
// release's start rolled back — an AddNOC's fabric by its index (reported
// by RolledBackFabrics), an intent-only AddNOC marker's fabric only when it
// has no identity, an UpdateNOC's replaced credentials — and clears the
// key.
func TestOpcreds_LegacyMarkerRolledBackAtStart(t *testing.T) {
	t.Parallel()
	const addKey, updateKey = "operational_credentials.pending_noc", "operational_credentials.pending_noc_update"
	write := func(t *testing.T, st restartStore, key string, m legacyMarker) {
		t.Helper()
		b, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		if err := st.SetSetting(context.Background(), key, string(b)); err != nil {
			t.Fatal(err)
		}
	}
	commissioned := func(t *testing.T, st restartStore) (*core.OperationalCredentials, *ecdsa.PrivateKey, mstore.FabricRecord) {
		t.Helper()
		oc, err := core.NewOperationalCredentials(st, core.OpcredsConfig{SupportedFabrics: 5, FailSafe: armedFailSafe(0)})
		if err != nil {
			t.Fatal(err)
		}
		_, rootPriv, idx := commissionTestFabric(context.Background(), t, oc)
		commitForTest(t, oc)
		fab, err := st.GetFabric(context.Background(), idx)
		if err != nil {
			t.Fatal(err)
		}
		return oc, rootPriv, fab
	}
	for name, open := range restartStores {
		t.Run(name+"/add", func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			st := open(t)
			_, _, fab := commissioned(t, st)
			write(t, st, addKey, legacyMarker{Op: "add", FabricIndex: fab.FabricIndex, FabricID: fab.FabricID, RootPublicKey: fab.RootPublicKey})
			oc := restartOpcreds(t, st)
			if _, err := st.GetFabric(ctx, fab.FabricIndex); !errors.Is(err, mstore.ErrFabricNotFound) {
				t.Fatalf("the fabric the legacy AddNOC marker names survived the start: %v", err)
			}
			if rb := oc.RolledBackFabrics(); len(rb) != 1 || rb[0].FabricIndex != fab.FabricIndex {
				t.Fatalf("RolledBackFabrics = %+v, want fabric %d", rb, fab.FabricIndex)
			}
			if v, _, _ := st.GetSetting(ctx, addKey); v != "" {
				t.Fatalf("the start left the legacy marker %s", v)
			}
		})
		t.Run(name+"/intent with identity", func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			st := open(t)
			_, _, fab := commissioned(t, st)
			write(t, st, addKey, legacyMarker{Op: "add", FabricID: fab.FabricID, RootPublicKey: fab.RootPublicKey})
			oc := restartOpcreds(t, st)
			if _, err := st.GetFabric(ctx, fab.FabricIndex); err != nil {
				t.Fatalf("an intent-only marker removed a fabric with an identity: %v", err)
			}
			if rb := oc.RolledBackFabrics(); len(rb) != 0 {
				t.Fatalf("RolledBackFabrics = %+v, want none", rb)
			}
		})
		t.Run(name+"/update", func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			st := open(t)
			oc, rootPriv, fab := commissioned(t, st)
			before, err := st.GetIdentity(ctx, fab.FabricIndex)
			if err != nil {
				t.Fatal(err)
			}
			fabCtx := im.WithFabricFilter(ctx, true, fab.FabricIndex)
			nocRaw := mintUpdateNOC(fabCtx, t, oc, rootPriv, testDefaultFabricID, testDefaultNodeID+7)
			if resp, err := oc.MatterInvoke(fabCtx, 0x07, core.UpdateNOCRequest{NOCValue: nocRaw}); err != nil || resp.(core.NOCResponse).StatusCode != core.NOCStatusOK {
				t.Fatalf("UpdateNOC: %+v, %v", resp, err)
			}
			commitForTest(t, oc) // the upgrade marked the row committed
			write(t, st, updateKey, legacyMarker{
				Op: "update", FabricIndex: fab.FabricIndex, FabricID: fab.FabricID, RootPublicKey: fab.RootPublicKey,
				NOC: before.NOC, ICAC: before.ICAC, PrivateKey: before.PrivateKey, IPK: before.IPK,
				NodeID: fab.NodeID, VendorID: fab.VendorID,
			})
			restartOpcreds(t, st)
			after, _ := st.GetIdentity(ctx, fab.FabricIndex)
			got, _ := st.GetFabric(ctx, fab.FabricIndex)
			if !bytes.Equal(after.NOC, before.NOC) || got.NodeID != fab.NodeID {
				t.Fatalf("after the start: node 0x%X, NOC restored %v; want node 0x%X and the replaced NOC", got.NodeID, bytes.Equal(after.NOC, before.NOC), fab.NodeID)
			}
			if v, _, _ := st.GetSetting(ctx, updateKey); v != "" {
				t.Fatalf("the start left the legacy marker %s", v)
			}
		})
	}
}

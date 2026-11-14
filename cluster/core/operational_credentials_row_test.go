// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

// The fail-safe on the fabric row's own state (review II, owner decision
// 36): an AddNOC or UpdateNOC writes its fabric row as pending, the
// commit of CommissioningComplete and the rollback of the fail-safe's end
// or of a start move it, and the fail-safe context in memory names no
// more than which row it wrote. Each test is the scenario of the finding
// it names, over the node's wiring of the two clusters (node/root.go).

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/core"
	gencommdef "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	"github.com/SukramJ/go-fabric/im"
	mstore "github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/store/memstore"
)

// rowBacking is a store the row tests run on: both the in-memory store
// and the SQLite one implement it.
type rowBacking interface {
	core.StoreFacade
	UpdateFabricVendorID(ctx context.Context, fabricIndex uint8, vendorID uint16) error
	UpdateFabricVIDVerification(ctx context.Context, fabricIndex uint8, statement, vvsc []byte) error
	UpdateFabricCredentials(ctx context.Context, fabricIndex uint8, c mstore.FabricCredentials) error
	CommitFabric(ctx context.Context, fabricIndex uint8) error
	RollbackFabric(ctx context.Context, fabricIndex uint8) (mstore.FabricPending, error)
	GetSetting(ctx context.Context, key string) (string, bool, error)
	SetSetting(ctx context.Context, key, value string) error
	SetSettings(ctx context.Context, values map[string]string) error
	GetMetadataCounter(ctx context.Context, key string) (uint64, bool, error)
	SetMetadataCounter(ctx context.Context, key string, value uint64) error
	ListACL(ctx context.Context, fabricIndex uint8) ([]mstore.ACLEntry, error)
}

// rowBackings are the two stores, the SQLite one on a temp file.
var rowBackings = map[string]func(t *testing.T) rowBacking{
	"memstore": func(*testing.T) rowBacking { return memstore.New() },
	"sqlite": func(t *testing.T) rowBacking {
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

// rowStore wraps a backing store with a fault: fault, when set, is asked
// before every call by its operation's name and fails the call with its
// error. crashAfter >= 0 lets that many writes land and fails every later
// one, as a node that loses power.
type rowStore struct {
	rowBacking
	mu         sync.Mutex
	fault      func(op string) error
	crashAfter int
	writes     int
}

func newRowStore(b rowBacking) *rowStore { return &rowStore{rowBacking: b, crashAfter: -1} }

func (s *rowStore) setFault(f func(op string) error) {
	s.mu.Lock()
	s.fault = f
	s.mu.Unlock()
}

func (s *rowStore) crashAfterWrites(n int) {
	s.mu.Lock()
	s.crashAfter, s.writes = n, 0
	s.mu.Unlock()
}

func (s *rowStore) check(op string, write bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fault != nil {
		if err := s.fault(op); err != nil {
			return err
		}
	}
	if !write {
		return nil
	}
	if s.crashAfter >= 0 && s.writes >= s.crashAfter {
		return errCrashed
	}
	s.writes++
	return nil
}

func (s *rowStore) ListFabrics(ctx context.Context) ([]mstore.FabricRecord, error) {
	if err := s.check("ListFabrics", false); err != nil {
		return nil, err
	}
	return s.rowBacking.ListFabrics(ctx)
}

func (s *rowStore) GetSetting(ctx context.Context, key string) (value string, ok bool, err error) {
	if err := s.check("GetSetting", false); err != nil {
		return "", false, err
	}
	return s.rowBacking.GetSetting(ctx, key)
}

func (s *rowStore) SetSetting(ctx context.Context, key, value string) error {
	if err := s.check("SetSetting:"+key, true); err != nil {
		return err
	}
	return s.rowBacking.SetSetting(ctx, key, value)
}

func (s *rowStore) SetSettings(ctx context.Context, values map[string]string) error {
	if err := s.check("SetSettings", true); err != nil {
		return err
	}
	return s.rowBacking.SetSettings(ctx, values)
}

func (s *rowStore) AddFabric(ctx context.Context, rec mstore.FabricRecord) (uint8, error) {
	if err := s.check("AddFabric", true); err != nil {
		return 0, err
	}
	return s.rowBacking.AddFabric(ctx, rec)
}

func (s *rowStore) UpsertIdentity(ctx context.Context, rec mstore.IdentityRecord) error {
	if err := s.check("UpsertIdentity", true); err != nil {
		return err
	}
	return s.rowBacking.UpsertIdentity(ctx, rec)
}

func (s *rowStore) ReplaceACL(ctx context.Context, fabricIndex uint8, entries []mstore.ACLEntry) error {
	if err := s.check("ReplaceACL", true); err != nil {
		return err
	}
	return s.rowBacking.ReplaceACL(ctx, fabricIndex, entries)
}

func (s *rowStore) UpsertGroupKeySet(ctx context.Context, rec mstore.GroupKeySet) error {
	if err := s.check("UpsertGroupKeySet", true); err != nil {
		return err
	}
	return s.rowBacking.UpsertGroupKeySet(ctx, rec)
}

func (s *rowStore) RemoveGroupKeysByFabric(ctx context.Context, fabricIndex uint8) error {
	if err := s.check("RemoveGroupKeysByFabric", true); err != nil {
		return err
	}
	return s.rowBacking.RemoveGroupKeysByFabric(ctx, fabricIndex)
}

func (s *rowStore) RemoveFabric(ctx context.Context, fabricIndex uint8) error {
	if err := s.check("RemoveFabric", true); err != nil {
		return err
	}
	return s.rowBacking.RemoveFabric(ctx, fabricIndex)
}

func (s *rowStore) UpdateFabricNodeID(ctx context.Context, fabricIndex uint8, nodeID uint64) error {
	if err := s.check("UpdateFabricNodeID", true); err != nil {
		return err
	}
	return s.rowBacking.UpdateFabricNodeID(ctx, fabricIndex, nodeID)
}

func (s *rowStore) UpdateFabricVendorID(ctx context.Context, fabricIndex uint8, vendorID uint16) error {
	if err := s.check("UpdateFabricVendorID", true); err != nil {
		return err
	}
	return s.rowBacking.UpdateFabricVendorID(ctx, fabricIndex, vendorID)
}

func (s *rowStore) UpdateFabricVIDVerification(ctx context.Context, fabricIndex uint8, statement, vvsc []byte) error {
	if err := s.check("UpdateFabricVIDVerification", true); err != nil {
		return err
	}
	return s.rowBacking.UpdateFabricVIDVerification(ctx, fabricIndex, statement, vvsc)
}

func (s *rowStore) UpdateFabricCredentials(ctx context.Context, fabricIndex uint8, c mstore.FabricCredentials) error {
	if err := s.check("UpdateFabricCredentials", true); err != nil {
		return err
	}
	return s.rowBacking.UpdateFabricCredentials(ctx, fabricIndex, c)
}

func (s *rowStore) CommitFabric(ctx context.Context, fabricIndex uint8) error {
	if err := s.check("CommitFabric", true); err != nil {
		return err
	}
	return s.rowBacking.CommitFabric(ctx, fabricIndex)
}

func (s *rowStore) RollbackFabric(ctx context.Context, fabricIndex uint8) (mstore.FabricPending, error) {
	if err := s.check("RollbackFabric", true); err != nil {
		return mstore.FabricCommitted, err
	}
	return s.rowBacking.RollbackFabric(ctx, fabricIndex)
}

// failOn is a fault that fails the named operations.
func failOn(ops ...string) func(string) error {
	return func(op string) error {
		for _, o := range ops {
			if o == op {
				return errCrashed
			}
		}
		return nil
	}
}

// rowRig is the node's wiring of GeneralCommissioning and
// OperationalCredentials over one store (node/root.go), with the
// fabric-removed hook counted.
type rowRig struct {
	failSafeRig
	st      *rowStore
	removed atomic.Int32
}

func newRowRig(t *testing.T, st *rowStore, tc *core.TermsAndConditionsConfig) *rowRig {
	t.Helper()
	gc := newGencomm(t, core.GeneralCommissioningConfig{
		LocationCapability: core.RegulatoryIndoor,
		FailSafeMaxSeconds: 900,
		Settings:           st,
		TermsAndConditions: tc,
		Clock:              clocktest.New(time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)),
	})
	oc, err := core.NewOperationalCredentials(st, core.OpcredsConfig{
		SupportedFabrics:       5,
		FailSafe:               gc.FailSafeFabric,
		RearmFailSafeForFabric: gc.SetCurrentFabric,
	})
	if err != nil {
		t.Fatal(err)
	}
	r := &rowRig{failSafeRig: failSafeRig{gc: gc, oc: oc}, st: st}
	wireFailSafeRig(gc, oc)
	oc.SetOnFabricRemoved(func(context.Context, uint8) { r.removed.Add(1) })
	return r
}

// commission arms over PASE, adds a fabric and commits it.
func (r *rowRig) commission(ctx context.Context, t *testing.T) (idx uint8, rootPriv *ecdsa.PrivateKey) {
	t.Helper()
	if resp, err := r.gc.MatterInvoke(ctx, 0x00, core.ArmFailSafeRequest{ExpiryLengthSeconds: 60}); err != nil || resp.(core.ArmFailSafeResponse).ErrorCode != core.CommissioningErrorOK {
		t.Fatalf("ArmFailSafe: %+v, %v", resp, err)
	}
	_, rootPriv, idx = commissionTestFabric(ctx, t, r.oc)
	if !r.commissioningComplete(ctx, t, idx) {
		t.Fatal("CommissioningComplete failed")
	}
	return idx, rootPriv
}

// armOver arms a fresh fail-safe over CASE of fabric idx.
func (r *rowRig) armOver(ctx context.Context, t *testing.T, idx uint8, seconds uint16) {
	t.Helper()
	resp, err := r.gc.MatterInvoke(im.WithFabricFilter(ctx, true, idx), 0x00, core.ArmFailSafeRequest{ExpiryLengthSeconds: seconds})
	if err != nil || resp.(core.ArmFailSafeResponse).ErrorCode != core.CommissioningErrorOK {
		t.Fatalf("ArmFailSafe(%d) over fabric %d: %+v, %v", seconds, idx, resp, err)
	}
}

// updateNOC runs CSRRequest(IsForUpdateNOC) and UpdateNOC over CASE of
// fabric idx in the armed fail-safe, with a NOC for nodeID.
func (r *rowRig) updateNOC(ctx context.Context, t *testing.T, idx uint8, rootPriv *ecdsa.PrivateKey, nodeID uint64) core.NOCResponse {
	t.Helper()
	fabCtx := im.WithFabricFilter(ctx, true, idx)
	pub := issueCSRPendingPubKey(fabCtx, t, r.oc, true)
	noc := buildCoreSignedCertForPubKey(t, pub, false, rootPriv, testDefaultFabricID, nodeID)
	resp, err := r.oc.MatterInvoke(fabCtx, 0x07, core.UpdateNOCRequest{NOCValue: noc})
	if err != nil {
		t.Fatalf("UpdateNOC: %v", err)
	}
	return resp.(core.NOCResponse)
}

// restartRow is the node coming back up on what the store holds.
func restartRow(t *testing.T, st core.StoreFacade) *core.OperationalCredentials {
	t.Helper()
	oc, err := core.NewOperationalCredentials(st, core.OpcredsConfig{SupportedFabrics: 5})
	if err != nil {
		t.Fatalf("NewOperationalCredentials after the restart: %v", err)
	}
	return oc
}

// credentialsOf is a fabric's identity NOC and row node id.
func credentialsOf(ctx context.Context, t *testing.T, st core.StoreFacade, idx uint8) (noc []byte, nodeID uint64) {
	t.Helper()
	id, err := st.GetIdentity(ctx, idx)
	if err != nil {
		t.Fatalf("identity of fabric %d: %v", idx, err)
	}
	fab, err := st.GetFabric(ctx, idx)
	if err != nil {
		t.Fatalf("fabric %d: %v", idx, err)
	}
	return id.NOC, fab.NodeID
}

// TestRow_RemovingAnotherFabricKeepsThePendingUpdate is R2-P2-comm-01 =
// R2-P2-fabric-01 (the UpdateNOC shape): fabric 2 was added last in this
// run; fabric 1's administrator updates its NOC in a fail-safe; fabric 2
// is removed; the fail-safe is disarmed without CommissioningComplete.
// Fabric 1 is back on its old NOC and node id, and stays so across a
// restart.
func TestRow_RemovingAnotherFabricKeepsThePendingUpdate(t *testing.T) {
	t.Parallel()
	for name, open := range rowBackings {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			st := newRowStore(open(t))
			r := newRowRig(t, st, nil)
			first, firstRoot := r.commission(ctx, t)
			second, _ := r.commission(ctx, t)
			oldNOC, oldNode := credentialsOf(ctx, t, st, first)

			r.armOver(ctx, t, first, 60)
			if resp := r.updateNOC(ctx, t, first, firstRoot, oldNode+1); resp.StatusCode != core.NOCStatusOK {
				t.Fatalf("UpdateNOC: %+v", resp)
			}
			if err := r.oc.RemoveFabric(ctx, second); err != nil {
				t.Fatalf("RemoveFabric(%d): %v", second, err)
			}
			r.armOver(ctx, t, first, 0)

			if noc, node := credentialsOf(ctx, t, st, first); !bytes.Equal(noc, oldNOC) || node != oldNode {
				t.Fatalf("the disarmed fail-safe left fabric %d's update in force: node 0x%X, NOC changed %v; want node 0x%X and the old NOC", first, node, !bytes.Equal(noc, oldNOC), oldNode)
			}
			restartRow(t, st)
			if noc, node := credentialsOf(ctx, t, st, first); !bytes.Equal(noc, oldNOC) || node != oldNode {
				t.Fatalf("after a restart fabric %d holds node 0x%X (NOC changed %v); want its old credentials", first, node, !bytes.Equal(noc, oldNOC))
			}
		})
	}
}

// TestRow_RemovingAnotherFabricKeepsThePendingCSR is R2-P2-fabric-01 (the
// CSR shape): a commissioning at the CSR stage survives the removal of
// the fabric added last in this run, and its AddNOC succeeds.
func TestRow_RemovingAnotherFabricKeepsThePendingCSR(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newRowStore(memstore.New())
	r := newRowRig(t, st, nil)
	r.commission(ctx, t)
	last, _ := r.commission(ctx, t)

	if resp, err := r.gc.MatterInvoke(ctx, 0x00, core.ArmFailSafeRequest{ExpiryLengthSeconds: 60}); err != nil || resp.(core.ArmFailSafeResponse).ErrorCode != core.CommissioningErrorOK {
		t.Fatalf("ArmFailSafe: %+v, %v", resp, err)
	}
	req := prepareAddNOC(ctx, t, r.oc)
	if err := r.oc.RemoveFabric(ctx, last); err != nil {
		t.Fatalf("RemoveFabric(%d): %v", last, err)
	}
	resp, err := r.oc.MatterInvoke(ctx, 0x06, req)
	if err != nil {
		t.Fatalf("AddNOC: %v", err)
	}
	if nr := resp.(core.NOCResponse); nr.StatusCode != core.NOCStatusOK {
		t.Fatalf("AddNOC after an unrelated fabric's removal: %+v, want OK", nr)
	}
}

// TestRow_FailedUpdateNOCWritesNothing is R2-P2-comm-04: an UpdateNOC
// whose store write fails answers InvalidNOC and leaves nothing a
// CommissioningComplete in the same fail-safe could commit — the fabric
// keeps its NOC and node id, across a restart too.
func TestRow_FailedUpdateNOCWritesNothing(t *testing.T) {
	t.Parallel()
	for name, open := range rowBackings {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			st := newRowStore(open(t))
			r := newRowRig(t, st, nil)
			idx, rootPriv := r.commission(ctx, t)
			oldNOC, oldNode := credentialsOf(ctx, t, st, idx)

			r.armOver(ctx, t, idx, 60)
			st.setFault(failOn("UpdateFabricNodeID", "UpdateFabricCredentials"))
			if resp := r.updateNOC(ctx, t, idx, rootPriv, oldNode+1); resp.StatusCode == core.NOCStatusOK {
				t.Fatalf("UpdateNOC over a failing store answered OK")
			}
			st.setFault(nil)
			if !r.commissioningComplete(ctx, t, idx) {
				t.Fatal("CommissioningComplete failed")
			}
			if noc, node := credentialsOf(ctx, t, st, idx); !bytes.Equal(noc, oldNOC) || node != oldNode {
				t.Fatalf("the failed UpdateNOC was committed: node 0x%X, NOC changed %v; want node 0x%X and the old NOC", node, !bytes.Equal(noc, oldNOC), oldNode)
			}
			restartRow(t, st)
			if noc, node := credentialsOf(ctx, t, st, idx); !bytes.Equal(noc, oldNOC) || node != oldNode {
				t.Fatalf("after a restart: node 0x%X, NOC changed %v; want the old credentials", node, !bytes.Equal(noc, oldNOC))
			}
		})
	}
}

// TestRow_FailedAddNOCIsRolledBackByTheFailSafe is R2-P2-comm-05: an AddNOC
// fails after its fabric was inserted and its own rollback fails too; a
// second AddNOC in the same fail-safe succeeds. The fail-safe's end
// removes both fabrics, and a restart finds none.
func TestRow_FailedAddNOCIsRolledBackByTheFailSafe(t *testing.T) {
	t.Parallel()
	for name, open := range rowBackings {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			st := newRowStore(open(t))
			r := newRowRig(t, st, nil)
			if resp, err := r.gc.MatterInvoke(ctx, 0x00, core.ArmFailSafeRequest{ExpiryLengthSeconds: 60}); err != nil || resp.(core.ArmFailSafeResponse).ErrorCode != core.CommissioningErrorOK {
				t.Fatalf("ArmFailSafe: %+v, %v", resp, err)
			}
			rootPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := r.oc.MatterInvoke(ctx, 0x0B, core.AddTrustedRootCertificateRequest{RootCACertificate: buildCoreSignedCert(t, rootPriv, true, rootPriv)}); err != nil {
				t.Fatalf("AddTrustedRootCertificate: %v", err)
			}
			pub := issueCSRPendingPubKey(ctx, t, r.oc, false)
			first := core.AddNOCRequest{
				NOCValue: buildCoreSignedCertForPubKey(t, pub, false, rootPriv, testDefaultFabricID, testDefaultNodeID),
				IPKValue: make([]byte, 16), CaseAdminSubject: 0xABCD, AdminVendorID: 0x1234,
			}
			st.setFault(failOn("ReplaceACL", "RemoveFabric", "RollbackFabric"))
			if resp, err := r.oc.MatterInvoke(ctx, 0x06, first); err != nil || resp.(core.NOCResponse).StatusCode == core.NOCStatusOK {
				t.Fatalf("the first AddNOC over a failing ACL write: %+v, %v; want a failure", resp, err)
			}
			st.setFault(nil)
			// The second AddNOC: the same root and pending key, another
			// fabric id.
			second := first
			second.NOCValue = buildCoreSignedCertForPubKey(t, pub, false, rootPriv, testDefaultFabricID+1, testDefaultNodeID)
			resp, err := r.oc.MatterInvoke(ctx, 0x06, second)
			if err != nil || resp.(core.NOCResponse).StatusCode != core.NOCStatusOK {
				t.Fatalf("the second AddNOC: %+v, %v; want OK", resp, err)
			}
			// The fail-safe now belongs to the second fabric (§11.18.6.16).
			r.armOver(ctx, t, resp.(core.NOCResponse).FabricIndex, 0)
			if fabrics, _ := st.ListFabrics(ctx); len(fabrics) != 0 {
				t.Fatalf("%d fabric(s) survived the fail-safe's end: %+v", len(fabrics), fabrics)
			}
			restartRow(t, st)
			if fabrics, _ := st.ListFabrics(ctx); len(fabrics) != 0 {
				t.Fatalf("%d fabric(s) survived the restart", len(fabrics))
			}
		})
	}
}

// TestRow_UpdateNOCKeepsTheVIDDataOnAReadError is R2-P2-comm-06: a
// settings read that fails while UpdateNOC runs does not make the
// rollback erase the fabric's VID verification statement.
func TestRow_UpdateNOCKeepsTheVIDDataOnAReadError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newRowStore(memstore.New())
	r := newRowRig(t, st, nil)
	idx, rootPriv := r.commission(ctx, t)
	fabCtx := im.WithFabricFilter(ctx, true, idx)
	statement := bytes.Repeat([]byte{0x5A}, 85)
	if _, err := r.oc.MatterInvoke(fabCtx, 0x0C, core.SetVidVerificationStatementRequest{
		HasVidVerificationStatement: true, VidVerificationStatement: statement,
	}); err != nil {
		t.Fatalf("SetVIDVerificationStatement: %v", err)
	}
	_, oldNode := credentialsOf(ctx, t, st, idx)

	r.armOver(ctx, t, idx, 60)
	st.setFault(failOn("GetSetting"))
	_ = r.updateNOC(ctx, t, idx, rootPriv, oldNode+1) // answered OK or not: the disarm restores either way
	st.setFault(nil)
	r.armOver(ctx, t, idx, 0)
	v, _ := r.oc.MatterReadFiltered(fabCtx, 0x0001)
	fabrics, _ := v.([]core.FabricDescriptorStruct)
	if len(fabrics) != 1 || !bytes.Equal(fabrics[0].VidVerificationStatement, statement) {
		t.Fatalf("Fabrics after the rolled-back UpdateNOC = %+v; want the VID statement kept", fabrics)
	}
}

// TestRow_AddNOCFailsWhenTheFabricListCannotBeRead is R2-P2-comm-10: on a
// full table, an AddNOC whose fabric list read fails installs nothing.
func TestRow_AddNOCFailsWhenTheFabricListCannotBeRead(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newRowStore(memstore.New())
	oc, err := core.NewOperationalCredentials(st, core.OpcredsConfig{SupportedFabrics: 1, FailSafe: armedFailSafe(0)})
	if err != nil {
		t.Fatal(err)
	}
	commissionTestFabric(ctx, t, oc)
	commitForTest(t, oc)
	oc.ClearPendingState()
	req := prepareAddNOC(ctx, t, oc)
	st.setFault(failOn("ListFabrics"))
	resp, err := oc.MatterInvoke(ctx, 0x06, req)
	st.setFault(nil)
	if err == nil && resp.(core.NOCResponse).StatusCode == core.NOCStatusOK {
		t.Fatalf("AddNOC with an unreadable fabric list answered OK: %+v", resp)
	}
	if fabrics, _ := st.ListFabrics(ctx); len(fabrics) != 1 {
		t.Fatalf("%d fabrics in a table of one; want 1", len(fabrics))
	}
}

// TestRow_ExpiryReportsARemovalWhenItLands is R2-P2-fabric-07: a
// fail-safe's end whose rollback of the AddNOC fails reports nothing
// removed — the fabric is still there; the next end, whose rollback
// lands, reports it once.
func TestRow_ExpiryReportsARemovalWhenItLands(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newRowStore(memstore.New())
	r := newRowRig(t, st, nil)
	if resp, err := r.gc.MatterInvoke(ctx, 0x00, core.ArmFailSafeRequest{ExpiryLengthSeconds: 60}); err != nil || resp.(core.ArmFailSafeResponse).ErrorCode != core.CommissioningErrorOK {
		t.Fatalf("ArmFailSafe: %+v, %v", resp, err)
	}
	_, _, idx := commissionTestFabric(ctx, t, r.oc)
	st.setFault(failOn("RemoveFabric", "RollbackFabric"))
	r.armOver(ctx, t, idx, 0)
	st.setFault(nil)
	if fabrics, _ := st.ListFabrics(ctx); len(fabrics) != 1 {
		t.Fatalf("%d fabrics after the failed rollback, want the 1 it could not remove", len(fabrics))
	}
	if n := r.removed.Load(); n != 0 {
		t.Fatalf("the fabric-removed hook fired %d time(s) for a fabric still in the table", n)
	}
	if resp, err := r.gc.MatterInvoke(ctx, 0x00, core.ArmFailSafeRequest{ExpiryLengthSeconds: 60}); err != nil || resp.(core.ArmFailSafeResponse).ErrorCode != core.CommissioningErrorOK {
		t.Fatalf("ArmFailSafe: %+v, %v", resp, err)
	}
	if _, err := r.gc.MatterInvoke(ctx, 0x00, core.ArmFailSafeRequest{}); err != nil {
		t.Fatalf("disarm: %v", err)
	}
	if fabrics, _ := st.ListFabrics(ctx); len(fabrics) != 0 {
		t.Fatalf("%d fabrics after the second fail-safe's end, want 0", len(fabrics))
	}
	if n := r.removed.Load(); n != 1 {
		t.Fatalf("the fabric-removed hook fired %d time(s), want once, when the removal landed", n)
	}
}

// TestRow_FreshArmDoesNotArmForTheFabricItRollsBack is R2-P2-dr-noc-04:
// an AddNOC's fabric whose rollback failed stays pending and serving; an
// ArmFailSafe from that fabric's administrator rolls it back before the arm
// is recorded, and arms nothing for the fabric it removed — another
// administrator's ArmFailSafe is not answered BusyWithOtherAdmin.
func TestRow_FreshArmDoesNotArmForTheFabricItRollsBack(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newRowStore(memstore.New())
	r := newRowRig(t, st, nil)
	if resp, err := r.gc.MatterInvoke(ctx, 0x00, core.ArmFailSafeRequest{ExpiryLengthSeconds: 60}); err != nil || resp.(core.ArmFailSafeResponse).ErrorCode != core.CommissioningErrorOK {
		t.Fatalf("ArmFailSafe: %+v, %v", resp, err)
	}
	_, _, idx := commissionTestFabric(ctx, t, r.oc)
	st.setFault(failOn("RemoveFabric", "RollbackFabric"))
	r.armOver(ctx, t, idx, 0)
	st.setFault(nil)
	if fabrics, _ := st.ListFabrics(ctx); len(fabrics) != 1 {
		t.Fatalf("%d fabrics after the failed rollback, want the 1 it could not remove", len(fabrics))
	}
	resp, err := r.gc.MatterInvoke(im.WithFabricFilter(ctx, true, idx), 0x00, core.ArmFailSafeRequest{ExpiryLengthSeconds: 300})
	if err == nil && resp.(core.ArmFailSafeResponse).ErrorCode == core.CommissioningErrorOK {
		t.Errorf("ArmFailSafe over fabric %d answered OK although its rollback removed that fabric", idx)
	}
	if fabrics, _ := st.ListFabrics(ctx); len(fabrics) != 0 {
		t.Fatalf("%d fabrics after the fresh arm, want the pending one rolled back", len(fabrics))
	}
	if fabric, armed := r.gc.FailSafeFabric(); armed {
		t.Fatalf("fail-safe armed for fabric %d after its rollback removed fabric %d", fabric, idx)
	}
	if n := r.removed.Load(); n != 1 {
		t.Fatalf("the fabric-removed hook fired %d time(s), want once", n)
	}
	r.armOver(ctx, t, idx+1, 60) // another administrator arms
	r.armOver(ctx, t, idx+1, 0)
}

// TestRow_FreshArmRefusesTheFabricWhoseRowItCouldNotSettle is
// R2-P2-rr-dev-01: an AddNOC's fabric whose rollback failed at the
// fail-safe's end, and fails again at the fresh start, keeps its row
// pending — its administrator's ArmFailSafe arms nothing, and its
// CommissioningComplete does not answer OK, so no commissioner is told the
// fabric is committed that the next start removes.
func TestRow_FreshArmRefusesTheFabricWhoseRowItCouldNotSettle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newRowStore(memstore.New())
	r := newRowRig(t, st, nil)
	if resp, err := r.gc.MatterInvoke(ctx, 0x00, core.ArmFailSafeRequest{ExpiryLengthSeconds: 60}); err != nil || resp.(core.ArmFailSafeResponse).ErrorCode != core.CommissioningErrorOK {
		t.Fatalf("ArmFailSafe: %+v, %v", resp, err)
	}
	_, _, idx := commissionTestFabric(ctx, t, r.oc)
	st.setFault(failOn("RemoveFabric", "RollbackFabric"))
	r.armOver(ctx, t, idx, 0) // the fail-safe ends; its rollback fails
	resp, err := r.gc.MatterInvoke(im.WithFabricFilter(ctx, true, idx), 0x00, core.ArmFailSafeRequest{ExpiryLengthSeconds: 300})
	if err == nil && resp.(core.ArmFailSafeResponse).ErrorCode == core.CommissioningErrorOK {
		t.Errorf("ArmFailSafe over fabric %d answered OK although its row stays pending", idx)
	}
	if fabric, armed := r.gc.FailSafeFabric(); armed {
		t.Errorf("fail-safe armed for fabric %d over the pending row of fabric %d", fabric, idx)
	}
	if r.commissioningComplete(ctx, t, idx) {
		t.Errorf("CommissioningComplete over fabric %d answered OK while its row stays pending", idx)
	}
	st.setFault(nil)
	fabrics, _ := st.ListFabrics(ctx)
	if len(fabrics) != 1 || fabrics[0].Pending == mstore.FabricCommitted {
		t.Fatalf("fabrics = %+v, want fabric %d still pending", fabrics, idx)
	}
}

// TestRow_CommissioningCompleteCommitsTheRowOfItsFabric is
// R2-P2-rr-dev-01's other half: a fail-safe armed for a fabric whose row
// stays pending outside the arm's judgement (ArmFailSafeFor, the host's
// arm for a session to come, does not refuse it) is completed only with
// that row: CommissioningComplete commits the row it finds pending for its
// fabric, or answers FAILURE.
func TestRow_CommissioningCompleteCommitsTheRowOfItsFabric(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newRowStore(memstore.New())
	r := newRowRig(t, st, nil)
	if resp, err := r.gc.MatterInvoke(ctx, 0x00, core.ArmFailSafeRequest{ExpiryLengthSeconds: 60}); err != nil || resp.(core.ArmFailSafeResponse).ErrorCode != core.CommissioningErrorOK {
		t.Fatalf("ArmFailSafe: %+v, %v", resp, err)
	}
	_, _, idx := commissionTestFabric(ctx, t, r.oc)
	st.setFault(failOn("RemoveFabric", "RollbackFabric", "CommitFabric"))
	r.armOver(ctx, t, idx, 0) // the fail-safe ends; its rollback fails
	if err := r.gc.ArmFailSafeFor(ctx, 60, idx); err != nil {
		t.Fatalf("ArmFailSafeFor(%d): %v", idx, err)
	}
	if r.commissioningComplete(ctx, t, idx) {
		t.Fatalf("CommissioningComplete over fabric %d answered OK although the commit of its pending row failed", idx)
	}
	if fabrics, _ := st.ListFabrics(ctx); len(fabrics) != 1 || fabrics[0].Pending == mstore.FabricCommitted {
		t.Fatalf("fabrics = %+v after the failed CommissioningComplete, want fabric %d pending", fabrics, idx)
	}
	st.setFault(nil)
	if !r.commissioningComplete(ctx, t, idx) {
		t.Fatalf("CommissioningComplete over fabric %d failed over a healthy store", idx)
	}
	if fabrics, _ := st.ListFabrics(ctx); len(fabrics) != 1 || fabrics[0].Pending != mstore.FabricCommitted {
		t.Fatalf("fabrics = %+v after CommissioningComplete answered OK, want fabric %d committed", fabrics, idx)
	}
}

// TestRow_TermsAcceptanceIsNotCommittedWithoutTheFabric is R2-P2-comm-07:
// a CommissioningComplete whose fabric commit fails leaves the terms
// acceptance of the window uncommitted — in the store too — so the
// fail-safe's end reverts it.
func TestRow_TermsAcceptanceIsNotCommittedWithoutTheFabric(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newRowStore(memstore.New())
	r := newRowRig(t, st, &core.TermsAndConditionsConfig{RequiredVersion: 1, RequiredAcknowledgements: 1})
	if resp, err := r.gc.MatterInvoke(ctx, 0x00, core.ArmFailSafeRequest{ExpiryLengthSeconds: 60}); err != nil || resp.(core.ArmFailSafeResponse).ErrorCode != core.CommissioningErrorOK {
		t.Fatalf("ArmFailSafe: %+v, %v", resp, err)
	}
	if _, err := r.gc.MatterInvoke(ctx, gencommdef.CmdSetTcAcknowledgements, gencommdef.SetTcAcknowledgementsRequest{TcVersion: 1, TcUserResponse: 1}); err != nil {
		t.Fatalf("SetTCAcknowledgements: %v", err)
	}
	_, _, idx := commissionTestFabric(ctx, t, r.oc)
	st.setFault(func(op string) error {
		if op == "CommitFabric" || op == "SetSetting:operational_credentials.pending_noc" {
			return errCrashed
		}
		return nil
	})
	if r.commissioningComplete(ctx, t, idx) {
		t.Fatal("CommissioningComplete answered success over a failing fabric commit")
	}
	st.setFault(nil)
	if v, ok, _ := st.GetSetting(ctx, mstore.SettingTermsAcceptance); ok && v != "" {
		t.Fatalf("the terms acceptance %q is stored although CommissioningComplete committed nothing", v)
	}
}

// wireFailSafeRig is node/root.go's wiring of the two clusters' fail-safe
// hooks.
func wireFailSafeRig(gc *core.GeneralCommissioning, oc *core.OperationalCredentials) {
	gc.SetOnFailSafeStarting(oc.PrepareFreshFailSafe)
	gc.SetOnFailSafeExpired(oc.OnFailSafeExpiry)
	gc.SetFailSafeCommit(oc.CommitFailSafe)
}

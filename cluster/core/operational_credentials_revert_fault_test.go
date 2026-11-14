// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
	mstore "github.com/SukramJ/go-fabric/store"
)

// pendingNOCUpdateKey is the settings key of the UpdateNOC's pending
// marker (operational_credentials_pending.go).
const pendingNOCUpdateKey = "operational_credentials.pending_noc_update"

var errFault = errors.New("store fault")

// faultStore fails the failAt-th store call made while armed — every read
// and every write counts — and lets every other one through.
type faultStore struct {
	restartStore
	mu     sync.Mutex
	armed  bool
	failAt int
	calls  int
}

func (s *faultStore) arm(failAt int) {
	s.mu.Lock()
	s.armed, s.failAt, s.calls = true, failAt, 0
	s.mu.Unlock()
}

// disarm ends the fault and reports how many calls were made while armed.
func (s *faultStore) disarm() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.armed = false
	return s.calls
}

func (s *faultStore) call() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.armed {
		return nil
	}
	s.calls++
	if s.calls == s.failAt {
		return errFault
	}
	return nil
}

func (s *faultStore) GetFabric(ctx context.Context, fabricIndex uint8) (mstore.FabricRecord, error) {
	if err := s.call(); err != nil {
		return mstore.FabricRecord{}, err
	}
	return s.restartStore.GetFabric(ctx, fabricIndex)
}

func (s *faultStore) GetIdentity(ctx context.Context, fabricIndex uint8) (mstore.IdentityRecord, error) {
	if err := s.call(); err != nil {
		return mstore.IdentityRecord{}, err
	}
	return s.restartStore.GetIdentity(ctx, fabricIndex)
}

func (s *faultStore) GetSetting(ctx context.Context, key string) (value string, ok bool, err error) {
	if err := s.call(); err != nil {
		return "", false, err
	}
	return s.restartStore.GetSetting(ctx, key)
}

func (s *faultStore) SetSetting(ctx context.Context, key, value string) error {
	if err := s.call(); err != nil {
		return err
	}
	return s.restartStore.SetSetting(ctx, key, value)
}

func (s *faultStore) UpsertIdentity(ctx context.Context, rec mstore.IdentityRecord) error {
	if err := s.call(); err != nil {
		return err
	}
	return s.restartStore.UpsertIdentity(ctx, rec)
}

func (s *faultStore) UpdateFabricNodeID(ctx context.Context, fabricIndex uint8, nodeID uint64) error {
	if err := s.call(); err != nil {
		return err
	}
	return s.restartStore.UpdateFabricNodeID(ctx, fabricIndex, nodeID)
}

func (s *faultStore) UpdateFabricVendorID(ctx context.Context, fabricIndex uint8, vendorID uint16) error {
	if err := s.call(); err != nil {
		return err
	}
	return s.restartStore.UpdateFabricVendorID(ctx, fabricIndex, vendorID)
}

// fabricCredentials is what an UpdateNOC and a VID statement inside its
// fail-safe change, and the expiry must put back.
type fabricCredentials struct {
	noc, key        []byte
	nodeID          uint64
	vendorID        uint16
	statement, vvsc string
}

func readCredentials(ctx context.Context, t *testing.T, st restartStore, idx uint8) fabricCredentials {
	t.Helper()
	id, err := st.GetIdentity(ctx, idx)
	if err != nil {
		t.Fatal(err)
	}
	fab, err := st.GetFabric(ctx, idx)
	if err != nil {
		t.Fatal(err)
	}
	c := fabricCredentials{noc: id.NOC, key: id.PrivateKey, nodeID: fab.NodeID, vendorID: fab.VendorID}
	c.statement, _, _ = st.GetSetting(ctx, fmt.Sprintf("matter.fabric.%d.vid.statement", idx))
	c.vvsc, _, _ = st.GetSetting(ctx, fmt.Sprintf("matter.fabric.%d.vid.vvsc", idx))
	return c
}

func (c fabricCredentials) equal(o fabricCredentials) bool {
	return bytes.Equal(c.noc, o.noc) && bytes.Equal(c.key, o.key) && c.nodeID == o.nodeID &&
		c.vendorID == o.vendorID && c.statement == o.statement && c.vvsc == o.vvsc
}

// TestOpcreds_FailedUpdateRevertKeepsTheMarker pins R2-P1-dr-sec-02: the
// fail-safe's rollback of an UpdateNOC — with a VID statement and a new
// vendor id set inside the same fail-safe — meets a store fault at each
// of its reads and writes in turn. After every one, either the fabric's
// credentials are fully back or the update's marker still stands; and a
// second attempt — the next start, or the next fresh arm in the same run
// — completes the revert and only then clears the marker.
func TestOpcreds_FailedUpdateRevertKeepsTheMarker(t *testing.T) {
	t.Parallel()
	for name, open := range restartStores {
		for _, second := range []string{"restart", "arm"} {
			t.Run(name+"/"+second, func(t *testing.T) {
				t.Parallel()
				for failAt := 1; ; failAt++ {
					if failAt > 40 {
						t.Fatal("the revert made more than 40 store calls")
					}
					if !testFailedUpdateRevert(t, open(t), failAt, second) {
						// UpsertIdentity, two VID settings, GetFabric, the
						// vendor id, the node id: the fault reached each.
						if failAt-1 < 6 {
							t.Fatalf("the revert made %d store calls, want at least 6", failAt-1)
						}
						t.Logf("faults at each of %d store calls", failAt-1)
						return
					}
				}
			})
		}
	}
}

// testFailedUpdateRevert runs one fault; it reports whether the fault was
// reached (false: the revert made fewer calls than failAt).
func testFailedUpdateRevert(t *testing.T, inner restartStore, failAt int, second string) (reached bool) {
	t.Helper()
	ctx := context.Background()
	st := &faultStore{restartStore: inner}
	oc, err := core.NewOperationalCredentials(st, core.OpcredsConfig{SupportedFabrics: 5, FailSafe: armedFailSafe(0)})
	if err != nil {
		t.Fatal(err)
	}
	_, rootPriv, idx := commissionTestFabric(ctx, t, oc)
	oc.ClearPendingState() // CommissioningComplete
	before := readCredentials(ctx, t, inner, idx)
	fabCtx := im.WithFabricFilter(ctx, true, idx)
	nocRaw := mintUpdateNOC(fabCtx, t, oc, rootPriv, testDefaultFabricID, testDefaultNodeID+7)
	if resp, err := oc.MatterInvoke(fabCtx, 0x07, core.UpdateNOCRequest{NOCValue: nocRaw}); err != nil || resp.(core.NOCResponse).StatusCode != core.NOCStatusOK {
		t.Fatalf("UpdateNOC: %+v, %v", resp, err)
	}
	if _, err := oc.MatterInvoke(fabCtx, 0x0C, core.SetVidVerificationStatementRequest{
		HasVendorID: true, VendorID: 0xFFF2,
		HasVidVerificationStatement: true, VidVerificationStatement: bytes.Repeat([]byte{0x21}, 85),
	}); err != nil {
		t.Fatalf("SetVIDVerificationStatement: %v", err)
	}
	if moved := readCredentials(ctx, t, inner, idx); moved.nodeID == before.nodeID || moved.vendorID == before.vendorID || moved.statement == before.statement {
		t.Fatal("the update moved neither the node id, the vendor id nor the VID statement: the revert has nothing to restore")
	}

	st.arm(failAt)
	oc.OnFailSafeExpiry(ctx, idx)
	if calls := st.disarm(); calls < failAt {
		if got := readCredentials(ctx, t, inner, idx); !got.equal(before) {
			t.Fatalf("a revert without a fault left %+v, want %+v", got, before)
		}
		if v, _, _ := inner.GetSetting(ctx, pendingNOCUpdateKey); v != "" {
			t.Fatal("a revert without a fault left the marker")
		}
		return false
	}
	marker, _, _ := inner.GetSetting(ctx, pendingNOCUpdateKey)
	if got := readCredentials(ctx, t, inner, idx); !got.equal(before) && marker == "" {
		t.Fatalf("fault at call %d: the marker was cleared over a half-restored fabric: %+v, want %+v", failAt, got, before)
	}

	switch second {
	case "restart":
		restartOpcreds(t, inner)
	case "arm":
		oc.ClearPendingState() // the next fresh arm
	}
	if got := readCredentials(ctx, t, inner, idx); !got.equal(before) {
		t.Fatalf("fault at call %d, then the %s: %+v, want %+v", failAt, second, got, before)
	}
	if v, _, _ := inner.GetSetting(ctx, pendingNOCUpdateKey); v != "" {
		t.Fatalf("fault at call %d, then the %s: the marker stands over a restored fabric", failAt, second)
	}
	return true
}

// identityFailingStore is a store whose UpsertIdentity fails while fail
// is set.
type identityFailingStore struct {
	restartStore
	fail atomic.Bool
}

func (s *identityFailingStore) UpsertIdentity(ctx context.Context, rec mstore.IdentityRecord) error {
	if s.fail.Load() {
		return errFault
	}
	return s.restartStore.UpsertIdentity(ctx, rec)
}

// TestOpcreds_OwedUpdateRevertOutlivesTheNextCommissioning is the in-run
// half of R2-P1-dr-sec-02: an UpdateNOC revert the expiry could not land
// stays owed through the next fail-safe. Its retry at the fresh arm fails
// again; an UpdateNOC in that window is refused rather than write its
// marker over the owed one; CommissioningComplete clears every marker key
// and its hook writes the owed marker back; once the store recovers, the
// next arm lands the revert and only then clears the marker.
func TestOpcreds_OwedUpdateRevertOutlivesTheNextCommissioning(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	inner := restartStores["memstore"](t)
	st := &identityFailingStore{restartStore: inner}
	oc, err := core.NewOperationalCredentials(st, core.OpcredsConfig{SupportedFabrics: 5, FailSafe: armedFailSafe(0)})
	if err != nil {
		t.Fatal(err)
	}
	_, rootPriv, idx := commissionTestFabric(ctx, t, oc)
	oc.ClearPendingState() // CommissioningComplete
	before := readCredentials(ctx, t, inner, idx)
	fabCtx := im.WithFabricFilter(ctx, true, idx)
	nocRaw := mintUpdateNOC(fabCtx, t, oc, rootPriv, testDefaultFabricID, testDefaultNodeID+7)
	if resp, err := oc.MatterInvoke(fabCtx, 0x07, core.UpdateNOCRequest{NOCValue: nocRaw}); err != nil || resp.(core.NOCResponse).StatusCode != core.NOCStatusOK {
		t.Fatalf("UpdateNOC: %+v, %v", resp, err)
	}
	marker := func() string {
		v, _, _ := inner.GetSetting(ctx, pendingNOCUpdateKey)
		return v
	}

	st.fail.Store(true)
	oc.OnFailSafeExpiry(ctx, idx)
	if marker() == "" {
		t.Fatal("the failed revert cleared its marker")
	}
	oc.ClearPendingState() // the next fresh arm: the retry fails again
	if marker() == "" {
		t.Fatal("the fresh arm cleared the owed revert's marker")
	}
	nocRaw2 := mintUpdateNOC(fabCtx, t, oc, rootPriv, testDefaultFabricID, testDefaultNodeID+8)
	resp, err := oc.MatterInvoke(fabCtx, 0x07, core.UpdateNOCRequest{NOCValue: nocRaw2})
	if nr, _ := resp.(core.NOCResponse); err != nil || nr.StatusCode != core.NOCStatusInvalidNOC {
		t.Fatalf("UpdateNOC over an owed revert: %+v, %v; want InvalidNOC", resp, err)
	}
	// CommissioningComplete: GeneralCommissioning clears every marker key,
	// then its hook runs ClearPendingState (node/root.go).
	for _, key := range []string{pendingNOCKey, pendingNOCUpdateKey} {
		if err := inner.SetSetting(ctx, key, ""); err != nil {
			t.Fatal(err)
		}
	}
	oc.ClearPendingState()
	if marker() == "" {
		t.Fatal("CommissioningComplete left the owed revert without its marker")
	}

	st.fail.Store(false)
	oc.ClearPendingState() // the next arm
	if got := readCredentials(ctx, t, inner, idx); !got.equal(before) {
		t.Fatalf("after the store recovered and the next arm: %+v, want %+v", got, before)
	}
	if marker() != "" {
		t.Fatal("the marker stands over the restored fabric")
	}
}

// removeFailingStore is a store whose RemoveFabric fails while fail is
// set.
type removeFailingStore struct {
	restartStore
	fail atomic.Bool
}

func (s *removeFailingStore) RemoveFabric(ctx context.Context, fabricIndex uint8) error {
	if s.fail.Load() {
		return errFault
	}
	return s.restartStore.RemoveFabric(ctx, fabricIndex)
}

// TestOpcreds_OwedAddRevertKeepsItsMarker is the AddNOC side of
// R2-P1-dr-sec-02: a fail-safe expiry whose removal of the added fabric
// fails keeps the AddNOC's marker, and the next fresh arm retries the
// removal instead of clearing the marker over the uncommitted fabric.
func TestOpcreds_OwedAddRevertKeepsItsMarker(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	inner := restartStores["memstore"](t)
	st := &removeFailingStore{restartStore: inner}
	oc, err := core.NewOperationalCredentials(st, core.OpcredsConfig{SupportedFabrics: 5, FailSafe: armedFailSafe(0)})
	if err != nil {
		t.Fatal(err)
	}
	_, _, idx := commissionTestFabric(ctx, t, oc)
	marker := func() string {
		v, _, _ := inner.GetSetting(ctx, pendingNOCKey)
		return v
	}
	st.fail.Store(true)
	oc.OnFailSafeExpiry(ctx, idx)
	oc.ClearPendingState() // the next fresh arm: the removal fails again
	if _, err := inner.GetFabric(ctx, idx); err != nil {
		t.Fatalf("the failing removal removed the fabric: %v", err)
	}
	if marker() == "" {
		t.Fatal("the fresh arm cleared the marker over the uncommitted fabric")
	}
	st.fail.Store(false)
	oc.ClearPendingState()
	if _, err := inner.GetFabric(ctx, idx); err == nil {
		t.Fatal("the uncommitted fabric survived the retried removal")
	}
	if marker() != "" {
		t.Fatal("the marker stands after the removal landed")
	}
}

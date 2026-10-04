// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package groups

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/SukramJ/go-fabric/store"
)

var testCompressed = [8]byte{0x87, 0xE1, 0xB0, 0x04, 0xE2, 0x35, 0xA1, 0x30}

func epochKeyOf(seed byte) []byte { return bytes.Repeat([]byte{seed}, 16) }

// newTestManager returns a manager over a store holding fabric 1 with key
// set 0x10 (epoch key 0xA1…) mapped to group 0x0101.
func newTestManager(t *testing.T) (*Manager, *memStore) {
	t.Helper()
	st := newMemStore()
	st.addFabric(1, 0x0FAB, testCompressed)
	st.putKeySet(1, store.GroupKeySet{GroupKeySetID: 0, EpochKey0: epochKeyOf(0x99)}) // the IPK
	st.putKeySet(1, store.GroupKeySet{GroupKeySetID: 0x10, EpochKey0: epochKeyOf(0xA1), EpochStart0: 1})
	st.mapGroup(1, 0x0101, 0x10)
	m, err := NewManager(st, nil)
	if err != nil {
		t.Fatal(err)
	}
	return m, st
}

func opKeyOf(t *testing.T, seed byte) ([]byte, uint16) {
	t.Helper()
	op, err := OperationalKey(epochKeyOf(seed), testCompressed)
	if err != nil {
		t.Fatal(err)
	}
	sid, err := SessionID(op)
	if err != nil {
		t.Fatal(err)
	}
	return op, sid
}

func TestNewManagerRequiresStore(t *testing.T) {
	t.Parallel()
	if _, err := NewManager(nil, nil); err == nil {
		t.Fatal("NewManager(nil) succeeded")
	}
}

func TestOperationalKeyRejectsShortEpochKey(t *testing.T) {
	t.Parallel()
	if _, err := OperationalKey(make([]byte, 15), testCompressed); !errors.Is(err, ErrEpochKeyLength) {
		t.Fatalf("err = %v, want ErrEpochKeyLength", err)
	}
}

func TestMembershipFollowsAddAndRemove(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m, st := newTestManager(t)
	var changes, tableChanges atomic.Int32
	m.OnMembershipChanged(func() { changes.Add(1) })
	m.OnMembershipChanged(nil) // ignored
	m.OnGroupTableChanged(func(idx uint8) {
		if idx == 1 {
			tableChanges.Add(1)
		}
	})
	m.OnGroupTableChanged(nil) // ignored

	if err := m.AddEndpointForGroup(ctx, 1, 0x0101, 3, "Kitchen"); err != nil {
		t.Fatal(err)
	}
	if err := m.AddEndpointForGroup(ctx, 1, 0x0101, 5, "Kitchen 2"); err != nil {
		t.Fatal(err)
	}
	if err := m.AddEndpointForGroup(ctx, 1, 0x0101, 3, "Kitchen 3"); err != nil { // already a member
		t.Fatal(err)
	}
	table, err := m.GroupTable(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	// The name always follows the last AddGroup (addEndpointForGroup).
	if len(table) != 1 || table[0].GroupName != "Kitchen 3" || !slices.Equal(table[0].Endpoints, []uint16{3, 5}) {
		t.Fatalf("table = %+v", table)
	}
	if got := changes.Load(); got != 1 {
		t.Fatalf("membership notifications = %d, want 1 (only the new group)", got)
	}
	persisted, _ := st.ListGroupTable(ctx, 1)
	if len(persisted) != 1 || !slices.Equal(persisted[0].Endpoints, []uint16{3, 5}) {
		t.Fatalf("persisted = %+v", persisted)
	}
	ms, err := m.Memberships(ctx)
	if err != nil || len(ms) != 1 || !ms[0].Address.Equal(MulticastAddress(0x0FAB, 0x0101)) {
		t.Fatalf("memberships = %+v (%v)", ms, err)
	}

	// Removing from a group the endpoint is not in reports false.
	if ok, err := m.RemoveEndpoint(ctx, 1, 9, 0x0101, false); err != nil || ok {
		t.Fatalf("RemoveEndpoint of a non-member = %v, %v", ok, err)
	}
	if ok, err := m.RemoveEndpoint(ctx, 1, 3, 0x0101, false); err != nil || !ok {
		t.Fatalf("RemoveEndpoint = %v, %v", ok, err)
	}
	if ok, err := m.RemoveEndpoint(ctx, 1, 5, 0, true); err != nil || !ok {
		t.Fatalf("RemoveEndpoint(all) = %v, %v", ok, err)
	}
	if table, _ := m.GroupTable(ctx, 1); len(table) != 0 {
		t.Fatalf("the group outlived its last endpoint: %+v", table)
	}
	if got := changes.Load(); got != 2 {
		t.Fatalf("membership notifications = %d, want 2", got)
	}
	// Every table mutation is announced: three adds, two removals; the
	// removal of a non-member changes nothing.
	if got := tableChanges.Load(); got != 5 {
		t.Fatalf("group table notifications = %d, want 5", got)
	}
	if ms, _ := m.Memberships(ctx); len(ms) != 0 {
		t.Fatalf("memberships after removal = %+v", ms)
	}
}

func TestAddEndpointForGroupHonoursMaxGroupsPerFabric(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m, _ := newTestManager(t)
	m.SetMaxGroupsPerFabric(2)
	m.SetMaxGroupsPerFabric(0) // ignored
	if m.MaxGroupsPerFabric() != 2 {
		t.Fatalf("MaxGroupsPerFabric = %d", m.MaxGroupsPerFabric())
	}
	for _, gid := range []uint16{1, 2} {
		if err := m.AddEndpointForGroup(ctx, 1, gid, 3, ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.AddEndpointForGroup(ctx, 1, 3, 3, ""); !errors.Is(err, ErrResourceExhausted) {
		t.Fatalf("third group: %v, want ErrResourceExhausted", err)
	}
	// An existing group still takes another endpoint.
	if err := m.AddEndpointForGroup(ctx, 1, 2, 4, ""); err != nil {
		t.Fatalf("existing group at the cap: %v", err)
	}
}

func TestMembershipSurvivesARestart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m, st := newTestManager(t)
	if err := m.AddEndpointForGroup(ctx, 1, 0x0101, 3, "Kitchen"); err != nil {
		t.Fatal(err)
	}
	restarted, _ := NewManager(st, nil)
	var notified atomic.Bool
	restarted.OnMembershipChanged(func() { notified.Store(true) })
	if err := restarted.Load(ctx); err != nil {
		t.Fatal(err)
	}
	if !notified.Load() {
		t.Fatal("Load did not announce the restored memberships")
	}
	ms, _ := restarted.Memberships(ctx)
	if len(ms) != 1 || ms[0].GroupID != 0x0101 {
		t.Fatalf("restored memberships = %+v", ms)
	}
	if ok, _ := restarted.HasKeyMapping(ctx, 1, 0x0101); !ok {
		t.Fatal("restored key map lost group 0x0101")
	}
}

func TestStoreFailuresSurface(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m, st := newTestManager(t)
	if err := m.AddEndpointForGroup(ctx, 1, 0x0101, 3, ""); err != nil {
		t.Fatal(err)
	}
	st.failPut = errors.New("disk full")
	if err := m.AddEndpointForGroup(ctx, 1, 0x0202, 3, ""); err == nil {
		t.Fatal("a failed persist was reported as success")
	}
	if _, err := m.RemoveEndpoint(ctx, 1, 3, 0x0101, false); err == nil {
		t.Fatal("a failed remove was reported as success")
	}
	if err := m.AddEndpointForGroup(ctx, 9, 0x0202, 3, ""); !errors.Is(err, ErrNoFabric) {
		t.Fatalf("unknown fabric: %v, want ErrNoFabric", err)
	}
	if _, err := m.GroupTable(ctx, 9); !errors.Is(err, ErrNoFabric) {
		t.Fatalf("unknown fabric table: %v", err)
	}
	if _, err := m.HasKeyMapping(ctx, 9, 1); !errors.Is(err, ErrNoFabric) {
		t.Fatalf("unknown fabric key map: %v", err)
	}
	if _, err := m.RemoveEndpoint(ctx, 9, 3, 1, false); !errors.Is(err, ErrNoFabric) {
		t.Fatalf("unknown fabric remove: %v", err)
	}
	if err := m.Reload(ctx, 9); !errors.Is(err, ErrNoFabric) {
		t.Fatalf("unknown fabric reload: %v", err)
	}
}

func TestDecodeRoutesAMappedGroupMessage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m, _ := newTestManager(t)
	if err := m.AddEndpointForGroup(ctx, 1, 0x0101, 3, ""); err != nil {
		t.Fatal(err)
	}
	op, sid := opKeyOf(t, 0xA1)
	for _, privacy := range []bool{true, false} {
		counter := uint32(100)
		if !privacy {
			counter = 200
		}
		msg, err := m.Decode(ctx, sealGroup(t, op, sid, 0x0101, 0x1B669, counter, privacy, []byte("payload")))
		if err != nil {
			t.Fatalf("privacy=%v: %v", privacy, err)
		}
		if string(msg.Payload) != "payload" || msg.GroupID != 0x0101 || !msg.HasValidMapping ||
			!slices.Equal(msg.Endpoints, []uint16{3}) || msg.FabricIndex != 1 || msg.KeySetID != 0x10 || msg.Duplicate {
			t.Fatalf("privacy=%v: decoded %+v", privacy, msg)
		}
	}
}

func TestDecodeRejections(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m, st := newTestManager(t)
	op, sid := opKeyOf(t, 0xA1)

	// Not a group message.
	unicast := sealGroup(t, op, sid, 0x0101, 1, 1, false, []byte("x"))
	unicast[3] &^= 0x03
	if _, err := m.Decode(ctx, unicast); !errors.Is(err, ErrNotGroupMessage) {
		t.Fatalf("unicast: %v", err)
	}
	if _, err := m.Decode(ctx, []byte{1, 2}); !errors.Is(err, ErrMalformed) {
		t.Fatalf("short: %v", err)
	}
	// No candidate key for the session id.
	if _, err := m.Decode(ctx, sealGroup(t, op, sid^0xFFFF, 0x0101, 1, 1, true, []byte("x"))); !errors.Is(err, ErrNoKey) {
		t.Fatalf("unknown session id: %v", err)
	}
	// Right session id, wrong key: fails to authenticate.
	other, _ := opKeyOf(t, 0x55)
	if _, err := m.Decode(ctx, sealGroup(t, other, sid, 0x0101, 1, 1, true, []byte("x"))); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("wrong key: %v", err)
	}
	// Tampered ciphertext.
	good := sealGroup(t, op, sid, 0x0101, 1, 2, true, []byte("x"))
	good[len(good)-1] ^= 1
	if _, err := m.Decode(ctx, good); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("tampered: %v", err)
	}
	// No source node id: there is no nonce.
	noSource := sealGroup(t, op, sid, 0x0101, 1, 3, false, []byte("x"))
	noSource[0] &^= 0x04
	if _, err := m.Decode(ctx, noSource); !errors.Is(err, ErrMalformed) {
		t.Fatalf("no source: %v", err)
	}
	// Group id 0 authenticates but is no group.
	if _, err := m.Decode(ctx, sealGroup(t, op, sid, 0, 1, 4, true, []byte("x"))); !errors.Is(err, ErrMalformed) {
		t.Fatalf("group 0: %v", err)
	}
	// An unmapped key set does not participate in decryption.
	st.mapGroup(1, 0x0101, 0x77)
	if err := m.Reload(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Decode(ctx, sealGroup(t, op, sid, 0x0101, 1, 5, true, []byte("x"))); !errors.Is(err, ErrNoKey) {
		t.Fatalf("unmapped key set: %v", err)
	}
}

// TestDecodeReportsAnInvalidMapping pins matter.js Groups.subjectForGroup:
// two key sets with identical key material share a session id; when the
// group is mapped to a key set other than the one whose key decrypted, the
// message authenticates but carries no valid Group subject.
func TestDecodeReportsAnInvalidMapping(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m, st := newTestManager(t)
	st.putKeySet(1, store.GroupKeySet{GroupKeySetID: 0x20, EpochKey0: epochKeyOf(0xB2), EpochStart0: 1})
	st.mapGroup(1, 0x0202, 0x10) // the A1 key set is mapped, so it is a candidate
	st.mapGroup(1, 0x0101, 0x20) // but group 0x0101 belongs to B2
	if err := m.Reload(ctx, 1); err != nil {
		t.Fatal(err)
	}
	op, sid := opKeyOf(t, 0xA1)
	msg, err := m.Decode(ctx, sealGroup(t, op, sid, 0x0101, 1, 9, true, []byte("x")))
	if err != nil {
		t.Fatal(err)
	}
	if msg.HasValidMapping {
		t.Fatal("a message authenticated by another key set's key carries a valid Group subject")
	}
}

func TestReplayWindowPerSenderAndKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m, st := newTestManager(t)
	op, sid := opKeyOf(t, 0xA1)
	seal := func(source uint64, counter uint32) []byte {
		return sealGroup(t, op, sid, 0x0101, source, counter, true, []byte("x"))
	}
	dup := func(b []byte) bool {
		t.Helper()
		msg, err := m.Decode(ctx, b)
		if err != nil {
			t.Fatal(err)
		}
		return msg.Duplicate
	}
	if dup(seal(1, 1000)) || !dup(seal(1, 1000)) || !dup(seal(1, 999)) || dup(seal(1, 1001)) {
		t.Fatal("sender 1: trust-first anchoring or replay detection broken")
	}
	// Another sender has its own window.
	if dup(seal(2, 5)) {
		t.Fatal("a second sender's first message was taken for a replay")
	}
	// Rewriting the key set with new key material forgets the old key's
	// reception state; reusing the old epoch key afterwards
	// re-synchronises (FabricGroups.#forgetUnreferencedReceptionState).
	st.putKeySet(1, store.GroupKeySet{GroupKeySetID: 0x10, EpochKey0: epochKeyOf(0xC3), EpochStart0: 1})
	if err := m.Reload(ctx, 1); err != nil {
		t.Fatal(err)
	}
	st.putKeySet(1, store.GroupKeySet{GroupKeySetID: 0x10, EpochKey0: epochKeyOf(0xA1), EpochStart0: 1})
	if err := m.Reload(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if dup(seal(1, 10)) {
		t.Fatal("reception state of a dropped key survived")
	}
}

func TestForgetFabricDropsItsGroups(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m, st := newTestManager(t)
	if err := m.AddEndpointForGroup(ctx, 1, 0x0101, 3, ""); err != nil {
		t.Fatal(err)
	}
	var notified atomic.Int32
	m.OnMembershipChanged(func() { notified.Add(1) })
	// The store's cascade removes the rows; the manager is told.
	delete(st.fabrics, 1)
	m.ForgetFabric(1)
	m.ForgetFabric(1) // idempotent, no second notification
	if notified.Load() != 1 {
		t.Fatalf("notifications = %d, want 1", notified.Load())
	}
	op, sid := opKeyOf(t, 0xA1)
	if _, err := m.Decode(ctx, sealGroup(t, op, sid, 0x0101, 1, 1, true, []byte("x"))); !errors.Is(err, ErrNoKey) {
		t.Fatalf("a removed fabric's key still authenticates: %v", err)
	}
}

func TestKeySetWithoutUsableEpochKeyIsSkipped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newMemStore()
	st.addFabric(1, 1, testCompressed)
	st.putKeySet(1, store.GroupKeySet{GroupKeySetID: 5, EpochKey0: make([]byte, 7)})
	st.mapGroup(1, 1, 5)
	m, _ := NewManager(st, nil)
	if err := m.Load(ctx); err != nil {
		t.Fatalf("a bad stored key set must not fail the load: %v", err)
	}
	if _, err := deriveKeySet(store.GroupKeySet{GroupKeySetID: 6}, testCompressed); err == nil {
		t.Fatal("a key set without epoch keys derived")
	}
}

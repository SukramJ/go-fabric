// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"slices"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/im/subscription"
	"thingfabric.dev/go-fabric/store/memstore"
)

// persistedIDs reads the ids of st's rows, the least recently saved first.
func persistedIDs(t *testing.T, st *memstore.Store) []uint32 {
	t.Helper()
	rows, err := st.LoadServerSubscriptions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]uint32, 0, len(rows))
	for _, row := range rows {
		rec, err := subscription.UnmarshalPeerSubscription(row)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, rec.SubscriptionID)
	}
	return ids
}

// seedRow saves a former subscription's row the manager does not hold.
func seedRow(t *testing.T, st *memstore.Store, id uint32, fabric uint8, peer uint64) {
	t.Helper()
	payload, err := subscription.MarshalPeerSubscription(subscription.PeerSubscription{
		SubscriptionID: id, FabricIndex: fabric, PeerNodeID: peer,
		AttributeRequests: []im.ConcreteAttributePath{{}},
		MinIntervalFloor:  1, MaxIntervalCeiling: 60, MaxInterval: 60, SendInterval: 48 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveServerSubscription(context.Background(), id, fabric, peer, payload); err != nil {
		t.Fatal(err)
	}
}

// boundTestBridge is a started bridge with a manager and a memstore.
func boundTestBridge(t *testing.T) (*Bridge, *subscription.Manager, *memstore.Store) {
	t.Helper()
	b := newStartedBridge(t)
	m := subscription.NewManager(subscription.Config{}, nil, nil)
	b.AttachSubscriptionManager(m)
	st := memstore.New()
	b.AttachSubscriptionStore(st)
	return b, m, st
}

// liveSubscription admits a live subscription of peer on fabric 1, routes
// it as a CASE subscription and activates it, as a Subscribe does before
// it persists.
func liveSubscription(t *testing.T, b *Bridge, m *subscription.Manager, peer uint64, keep bool) uint32 {
	t.Helper()
	sub, err := m.Subscribe(subscription.SubscribeArgs{
		FabricIndex: 1, PeerNodeID: peer, SessionID: 0x10, MinIntervalFloor: 1, MaxIntervalCeiling: 60,
		KeepSubscriptions: keep, AttributePaths: []im.ConcreteAttributePath{{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	b.routing.subTargets.Store(sub.ID, subTarget{sessionID: 0x10, fabricIndex: 1, subjectNodeID: peer, fabricFiltered: true})
	b.activateSubscription(sub, time.Now())
	return sub.ID
}

// TestKeepSubscriptionsFalseForgetsThePeersFormerRows (review II,
// R2-P6-bound-0502, decision 134): a peer that subscribes with
// KeepSubscriptions=false gives up the rows of its subscriptions that are
// no longer live; another peer's rows and a live subscription's stay.
func TestKeepSubscriptionsFalseForgetsThePeersFormerRows(t *testing.T) {
	t.Parallel()
	b, m, st := boundTestBridge(t)
	const peer, other = uint64(0xA1), uint64(0xB2)
	seedRow(t, st, 0x101, 1, peer)  // closed with its session
	seedRow(t, st, 0x102, 1, other) // another peer's
	seedRow(t, st, 0x103, 2, peer)  // the same node id on another fabric
	kept := liveSubscription(t, b, m, peer, true)
	b.persistSubscription(context.Background(), kept)
	if got := persistedIDs(t, st); !slices.Contains(got, 0x101) {
		t.Fatalf("KeepSubscriptions=true dropped a former row: %v", got)
	}
	fresh := liveSubscription(t, b, m, peer, false)
	b.persistSubscription(context.Background(), fresh)
	got := persistedIDs(t, st)
	want := []uint32{0x102, 0x103, kept, fresh}
	if !slices.Equal(got, want) {
		t.Fatalf("rows after a KeepSubscriptions=false subscribe = %#x, want %#x", got, want)
	}
}

// TestPersistedRowsAreCappedPerFabric (review II, R2-P6-bound-0502,
// decision 134): beyond the quota a fabric's oldest rows whose
// subscription is not live go first; a live subscription's row and
// another fabric's rows stay.
func TestPersistedRowsAreCappedPerFabric(t *testing.T) {
	t.Parallel()
	b, m, st := boundTestBridge(t)
	live := liveSubscription(t, b, m, 0xA1, true)
	b.persistSubscription(context.Background(), live) // the oldest row, live
	for id := uint32(0x201); id <= 0x205; id++ {
		seedRow(t, st, id, 1, 0xA1)
	}
	seedRow(t, st, 0x301, 2, 0xA1)
	sub, err := m.Get(live)
	if err != nil {
		t.Fatal(err)
	}
	b.resumption.mu.Lock()
	b.boundPersistedSubscriptionsLocked(context.Background(), m, sub.PeerSubscription(true), true, 3)
	b.resumption.mu.Unlock()
	if got, want := persistedIDs(t, st), []uint32{live, 0x204, 0x205, 0x301}; !slices.Equal(got, want) {
		t.Fatalf("rows after the cap = %#x, want %#x", got, want)
	}
}

// TestPersistSubscriptionSkipsUnattributableTargets (review II,
// R2-P6-store-0806): a subscription that cannot be re-established as the
// peer's — a PASE session's, one without a fabric or with an index the
// bridge could not resolve, one without a subject — is not persisted; a
// CASE subscription is.
func TestPersistSubscriptionSkipsUnattributableTargets(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		target  subTarget
		persist bool
	}{
		{"CASE", subTarget{fabricIndex: 1, subjectNodeID: 0xA1}, true},
		{"PASE", subTarget{pase: true, fabricIndex: 1, subjectNodeID: 0xA1}, false},
		{"fabric 0", subTarget{fabricIndex: 0, subjectNodeID: 0xA1}, false},
		{"unresolvable fabric", subTarget{fabricIndex: endpoint.FabricIndexUnresolvable, subjectNodeID: 0xA1}, false},
		{"subject 0", subTarget{fabricIndex: 1, subjectNodeID: 0}, false},
	} {
		b, m, st := boundTestBridge(t)
		sub, err := m.Subscribe(subscription.SubscribeArgs{
			FabricIndex: 1, PeerNodeID: 0xA1, SessionID: 0x10, MinIntervalFloor: 1, MaxIntervalCeiling: 60,
			KeepSubscriptions: true, AttributePaths: []im.ConcreteAttributePath{{}},
		})
		if err != nil {
			t.Fatal(err)
		}
		b.routing.subTargets.Store(sub.ID, tc.target)
		b.activateSubscription(sub, time.Now())
		b.persistSubscription(context.Background(), sub.ID)
		if got := persistedIDs(t, st); (len(got) == 1) != tc.persist {
			t.Errorf("%s: store holds %#x, persisted want %v", tc.name, got, tc.persist)
		}
	}
}

// TestPersistedRowsAreCappedAtTheManagersQuota (review II,
// R2-P6-bound-0502, decision 175): a Subscribe persists its row and the
// fabric's rows are capped at the quota the manager is configured with
// ([subscription.Manager.MaxSubscriptionsPerFabric]) — end to end, not
// only the function that caps.
func TestPersistedRowsAreCappedAtTheManagersQuota(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	m := subscription.NewManager(subscription.Config{MaxSubscriptionsPerFabric: 3}, nil, nil)
	b.AttachSubscriptionManager(m)
	st := memstore.New()
	b.AttachSubscriptionStore(st)
	for id := uint32(0x201); id <= 0x205; id++ {
		seedRow(t, st, id, 1, 0xA1)
	}
	seedRow(t, st, 0x301, 2, 0xA1)
	live := liveSubscription(t, b, m, 0xA1, true)
	b.persistSubscription(context.Background(), live)
	if got, want := persistedIDs(t, st), []uint32{0x204, 0x205, 0x301, live}; !slices.Equal(got, want) {
		t.Fatalf("rows after the subscribe = %#x, want %#x", got, want)
	}
	if got := m.MaxSubscriptionsPerFabric(); got != 3 {
		t.Errorf("MaxSubscriptionsPerFabric = %d, want 3", got)
	}
	if got := subscription.NewManager(subscription.Config{}, nil, nil).MaxSubscriptionsPerFabric(); got != subscription.DefaultMaxSubscriptionsPerFabric {
		t.Errorf("default MaxSubscriptionsPerFabric = %d, want %d", got, subscription.DefaultMaxSubscriptionsPerFabric)
	}
}

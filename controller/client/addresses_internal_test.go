// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"net/netip"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
)

func device(id string, addrs ...string) discovery.Device {
	d := discovery.Device{InstanceID: id, D: 3840, CM: 1}
	for _, a := range addrs {
		ap := netip.MustParseAddrPort(a)
		d.Addresses = append(d.Addresses, discovery.Address{IP: ap.Addr(), Port: ap.Port()})
	}
	return d
}

// tick advances the clock by one expiration interval.
func (h *regHarness) tick() { h.clock.Advance(ExpirationInterval) }

// TestExpirationOf: Peers.ts:959-966.
func TestExpirationOf(t *testing.T) {
	t.Parallel()
	at := time.Unix(1000, 0)
	if _, ok := expirationOf(time.Time{}, time.Second); ok {
		t.Fatal("no discovery time expires")
	}
	if exp, _ := expirationOf(at, 0); !exp.Equal(at.Add(DefaultTTL)) {
		t.Fatalf("unknown TTL: %v", exp)
	}
	if exp, _ := expirationOf(at, 30*time.Second); !exp.Equal(at.Add(30 * time.Second)) {
		t.Fatalf("TTL: %v", exp)
	}
}

// TestCullUncommissioned: Peers.#cullExpiredNodesAndAddresses for
// commissionable nodes — one whose only address expired is deleted at the
// next tick; one with a fresh address survives until its own lifespan
// ends; the timer runs while the registry has nodes and stops when it
// empties (Peers.ts:615-634).
func TestCullUncommissioned(t *testing.T) {
	t.Parallel()
	h := newRegHarness(t)
	r := h.registry(t, nil)
	if h.clock.armed() != 0 {
		t.Fatal("the timer runs on an empty registry")
	}
	stale, err := r.ForDevice(t.Context(), device("STALE", "[fd00::1]:5540"))
	if err != nil || stale.ID() != "peer1" || stale.IsCommissioned() {
		t.Fatalf("ForDevice %v %v", stale, err)
	}
	if h.clock.armed() != 1 {
		t.Fatal("the timer is not armed")
	}
	stale.updateRecord(func(rec *Record) { rec.Addresses[0].TTL = time.Second })
	fresh, _ := r.ForDevice(t.Context(), device("FRESH", "[fd00::2]:5540"))
	// The node's own lifespan is two minutes; its address lives longer.
	fresh.updateRecord(func(rec *Record) { rec.TTL = 2 * time.Minute })
	h.tick()
	if r.Get("peer1") != nil || r.Get("peer2") == nil {
		t.Fatalf("after the first tick %v", ids(r.Nodes()))
	}
	if len(r.Get("peer2").Record().Addresses) != 1 {
		t.Fatal("the fresh address was dropped")
	}
	h.tick()
	if r.Get("peer2") != nil || r.Size() != 0 {
		t.Fatalf("after the node-level expiry %v", ids(r.Nodes()))
	}
	if h.clock.armed() != 0 {
		t.Fatal("the timer runs on an emptied registry")
	}
	if ids, _ := h.store.NodeIDs(); len(ids) != 0 {
		t.Fatalf("stored %v", ids)
	}
}

// TestCullCommissioned: a commissioned node is never deleted; with every
// address expired it keeps the freshest (Peers.ts:692-702); one with a
// single address is left alone (:669-672); a node with a fabric operation
// is skipped (:659-661).
func TestCullCommissioned(t *testing.T) {
	t.Parallel()
	h := newRegHarness(t)
	now := h.clock.Now()
	two := commissionedRecord(0x10)
	two.Addresses = []NetworkAddress{
		{IP: netip.MustParseAddr("fd00::1"), Port: 1, TTL: time.Second, DiscoveredAt: now},
		{IP: netip.MustParseAddr("fd00::2"), Port: 2, TTL: 2 * time.Second, DiscoveredAt: now},
		{IP: netip.MustParseAddr("fd00::3"), Port: 3, TTL: 2 * time.Second, DiscoveredAt: now},
	}
	h.seed(t, "peer1", two, settings{})
	one := commissionedRecord(0x11)
	one.Addresses = []NetworkAddress{{IP: netip.MustParseAddr("fd00::4"), Port: 4, TTL: time.Second, DiscoveredAt: now}}
	h.seed(t, "peer2", one, settings{})
	busy := commissionedRecord(0x12)
	busy.Addresses = append([]NetworkAddress(nil), two.Addresses...)
	h.seed(t, "peer3", busy, settings{})
	h.seed(t, "peer4", Record{DiscoveredAt: now}, settings{}) // never located: no addresses
	r := h.registry(t, nil)
	op, err := r.registerOperation(r.Get("peer3"), opCommission)
	if err != nil {
		t.Fatal(err)
	}
	h.tick()
	h.tick()
	if r.Size() != 4 {
		t.Fatalf("nodes %v", ids(r.Nodes()))
	}
	got := r.Get("peer1").Record().Addresses
	if len(got) != 2 || got[0].Port != 2 || got[1].Port != 3 {
		t.Fatalf("kept %+v", got)
	}
	if len(r.Get("peer2").Record().Addresses) != 1 || len(r.Get("peer3").Record().Addresses) != 3 {
		t.Fatal("a node the cull must leave was changed")
	}
	if r.Get("peer4").Record().Addresses != nil {
		t.Fatal("a node never located gained addresses")
	}
	p3 := r.Get("peer3")
	r.mu.Lock()
	delete(r.ops, p3)
	r.mu.Unlock()
	close(op.done)
	h.tick()
	if len(r.Get("peer3").Record().Addresses) != 2 {
		t.Fatal("the released node was not culled")
	}
	// Stop cancels the timer; Start arms it again; Close cancels it.
	_ = r.Stop(t.Context())
	if h.clock.armed() != 0 {
		t.Fatal("Stop left the timer")
	}
	_ = r.Start(t.Context())
	if h.clock.armed() != 1 {
		t.Fatal("Start did not arm the timer")
	}
	_ = r.Close(t.Context())
	if h.clock.armed() != 0 {
		t.Fatal("Close left the timer")
	}
	r.onExpirationIntervalElapsed() // a late tick after Close does nothing
}

// TestForDevice: Peers.forDescriptor — a second sighting of the same
// instance refreshes its node (RemoteDescriptor.toLongForm); a node under
// decommission is waited for (Peers.ts:360-368); a device without an
// instance id never matches (RemoteDescriptor.ts:42-47).
func TestForDevice(t *testing.T) {
	t.Parallel()
	h := newRegHarness(t)
	r := h.registry(t, nil)
	first, _ := r.ForDevice(t.Context(), device("CAFE", "[fd00::1]:5540"))
	h.clock.Advance(time.Second)
	again, err := r.ForDevice(t.Context(), device("CAFE", "[fd00::9]:5540"))
	if err != nil || again != first || r.Size() != 1 {
		t.Fatalf("a second sighting: %v %d", err, r.Size())
	}
	if rec := again.Record(); rec.Addresses[0].String() != "[fd00::9]:5540" || !rec.DiscoveredAt.Equal(h.clock.Now()) {
		t.Fatalf("not refreshed %+v", rec)
	}
	if anon, _ := r.ForDevice(t.Context(), device("", "[fd00::2]:5540")); anon == first {
		t.Fatal("an anonymous device matched")
	}
	op, err := r.registerOperation(first, opDecommission)
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan *Node, 1)
	go func() {
		n, _ := r.ForDevice(t.Context(), device("CAFE"))
		got <- n
	}()
	select {
	case <-got:
		t.Fatal("ForDevice did not wait for the decommission")
	case <-time.After(50 * time.Millisecond):
	}
	r.mu.Lock()
	delete(r.ops, first)
	r.mu.Unlock()
	close(op.done)
	if n := <-got; n != first {
		t.Fatal("the kept node was not returned")
	}
	// A commission does not hold ForDevice back.
	op, _ = r.registerOperation(first, opCommission)
	if n, _ := r.ForDevice(t.Context(), device("CAFE")); n != first {
		t.Fatal("a commission held ForDevice")
	}
	r.mu.Lock()
	delete(r.ops, first)
	r.mu.Unlock()
	close(op.done)
	_ = r.Close(t.Context())
	if _, err := r.ForDevice(t.Context(), device("NEW")); err == nil {
		t.Fatal("ForDevice after Close")
	}
}

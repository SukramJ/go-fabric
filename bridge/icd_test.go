// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/secure/channel"
	"thingfabric.dev/go-fabric/transport/mrp"
)

// icdLookup resolves session 7 to fabric 1 and subject 0x1234, session 8
// to fabric 2.
type icdLookup struct{}

func (icdLookup) Lookup(uint16) (*channel.Session, bool) { return nil, false }

func (icdLookup) FabricFor(id uint16) (uint8, bool) {
	switch id {
	case 7:
		return 1, true
	case 8:
		return 2, true
	}
	return 0, false
}

func (icdLookup) SubjectFor(id uint16) (nodeID uint64, cats []uint32, ok bool) {
	if id == 7 {
		return 0x1234, []uint32{0x0001_0001}, true
	}
	return 0x5678, nil, id == 8
}

// TestICDSeams holds the bridge's ICD seams on their own: the MRP pad, the
// activity hook, the advertisement source, the one-shot unsecured send and
// the lookups that answer nothing on a bridge without sessions.
func TestICDSeams(t *testing.T) {
	t.Parallel()
	t.Run("the fixed MRP backoff is stored", func(t *testing.T) {
		t.Parallel()
		b := newStartedBridge(t)
		if got := b.LocalFixedMRPBackoff(); got != 0 {
			t.Fatalf("default %v", got)
		}
		b.SetLocalFixedMRPBackoff(200 * time.Millisecond)
		if got := b.LocalFixedMRPBackoff(); got != 200*time.Millisecond {
			t.Fatalf("pad %v", got)
		}
	})
	t.Run("the inbound activity hook runs until it is removed", func(t *testing.T) {
		t.Parallel()
		b := newStartedBridge(t)
		b.noteInboundActivity() // no hook: nothing happens
		n := 0
		b.SetOnInboundActivity(func() { n++ })
		b.noteInboundActivity()
		b.SetOnInboundActivity(nil)
		b.noteInboundActivity()
		if n != 1 {
			t.Fatalf("%d calls", n)
		}
	})
	t.Run("the ICD advertisement comes from its source", func(t *testing.T) {
		t.Parallel()
		b := newStartedBridge(t)
		if _, ok := b.icdAdvertisement(); ok {
			t.Fatal("an advertisement without a source")
		}
		want := ICDAdvertisement{Mode: 1, ActiveThreshold: 5000, OmitIdleInterval: true}
		b.SetICDAdvertisement(func() ICDAdvertisement { return want })
		if got, ok := b.icdAdvertisement(); !ok || got != want {
			t.Fatalf("%+v %v", got, ok)
		}
		b.SetICDAdvertisement(nil)
		if _, ok := b.icdAdvertisement(); ok {
			t.Fatal("an advertisement after the source was removed")
		}
	})
	t.Run("an unsecured send needs a listener", func(t *testing.T) {
		t.Parallel()
		b := &Bridge{}
		if err := b.SendUnsecured(loopbackSrc(), mrp.SCOpcodeIcdCheckIn, []byte{1}); !errors.Is(err, ErrNoUnsecuredRoute) {
			t.Fatal(err)
		}
	})
	t.Run("an unsecured send goes out on the listener", func(t *testing.T) {
		t.Parallel()
		b := newStartedBridge(t)
		before := b.unsecuredCounter.Load()
		if err := b.SendUnsecured(loopbackSrc(), mrp.SCOpcodeIcdCheckIn, []byte{1, 2, 3}); err != nil {
			t.Fatal(err)
		}
		if b.unsecuredCounter.Load() == before {
			t.Fatal("the unsecured counter did not move")
		}
	})
	t.Run("a bridge without sessions or a resolver answers nothing", func(t *testing.T) {
		t.Parallel()
		b := newStartedBridge(t)
		if a := b.PeerSessionAddress(1, 0x1234); a != nil {
			t.Fatal(a)
		}
		if a, err := b.ResolveOperationalAddress(context.Background(), [8]byte{}, 0x1234); a != nil || err != nil {
			t.Fatal(a, err)
		}
		if s := b.ActiveSubscriptionSubjects(1); len(s) != 0 {
			t.Fatal(s)
		}
	})
	t.Run("the lookups answer from the sessions and subscriptions the bridge holds", func(t *testing.T) {
		t.Parallel()
		b := newStartedBridge(t)
		b.mu.Lock()
		b.sessions = icdLookup{}
		b.mu.Unlock()
		addr := &net.UDPAddr{IP: net.IPv6loopback, Port: 5540}
		b.sessionPeerAddrs.Store(uint16(7), addr)
		b.sessionPeerAddrs.Store(uint16(8), &net.UDPAddr{IP: net.IPv6loopback, Port: 5541})
		if got := b.PeerSessionAddress(1, 0x1234); got != addr {
			t.Fatalf("address %v", got)
		}
		if got := b.PeerSessionAddress(1, 0x9999); got != nil {
			t.Fatalf("address of another subject %v", got)
		}
		b.routing.subTargets.Store(uint32(1), subTarget{sessionID: 7, fabricIndex: 1, subjectNodeID: 0x1234, subjectCATs: []uint32{0x0001_0001}})
		b.routing.subTargets.Store(uint32(2), subTarget{sessionID: 7, fabricIndex: 1, subjectNodeID: 0x1234})
		b.routing.subTargets.Store(uint32(3), subTarget{sessionID: 8, fabricIndex: 2, subjectNodeID: 0x5678})
		b.routing.subTargets.Store(uint32(4), subTarget{sessionID: 9, fabricIndex: 1, subjectNodeID: 0x4444})
		b.routing.subTargets.Store(uint32(5), subTarget{pase: true})
		got := b.ActiveSubscriptionSubjects(1)
		if len(got) != 1 || got[0].NodeID != 0x1234 { // one per session, whichever subscription comes first
			t.Fatalf("subjects %+v", got)
		}
		b.AttachOperationalResolver(staticResolver{addr: addr})
		if a, err := b.ResolveOperationalAddress(context.Background(), [8]byte{}, 0x1234); a != addr || err != nil {
			t.Fatal(a, err)
		}
	})
}

// sessionTable resolves each session id to the fabric and subject it names,
// and nothing for an id it does not hold.
type sessionTable map[uint16]tableSession

type tableSession struct {
	fabric uint8
	node   uint64
}

func (sessionTable) Lookup(uint16) (*channel.Session, bool) { return nil, false }

func (s sessionTable) FabricFor(id uint16) (uint8, bool) {
	e, ok := s[id]
	return e.fabric, ok
}

func (s sessionTable) SubjectFor(id uint16) (nodeID uint64, cats []uint32, ok bool) {
	e, ok := s[id]
	return e.node, nil, ok
}

// TestPeerSessionAddressIsPerFabric holds the fabric filter of
// PeerSessionAddress (R2-P6-test-0814): one controller node id on two
// fabrics is two peers, so each fabric's Check-In address is its own
// session's; a fabric the bridge holds no session on answers nil; and a
// session whose address is not recorded hides no other session's.
func TestPeerSessionAddressIsPerFabric(t *testing.T) {
	t.Parallel()
	const node = 0x1122_3344
	a1 := &net.UDPAddr{IP: net.IPv6loopback, Port: 5541}
	a2 := &net.UDPAddr{IP: net.IPv6loopback, Port: 5542}
	// A sync.Map's Range order is fixed per map and random across maps:
	// fresh bridges, and many unrecorded sessions beside the one recorded,
	// make an order-dependent answer show.
	for range 8 {
		table := sessionTable{7: {1, node}, 8: {2, node}}
		b := &Bridge{}
		b.sessionPeerAddrs.Store(uint16(7), a1)
		b.sessionPeerAddrs.Store(uint16(8), a2)
		for id := uint16(20); id < 36; id++ {
			table[id] = tableSession{2, node}
			b.sessionPeerAddrs.Store(id, (*net.UDPAddr)(nil))
		}
		b.sessions = table
		if got := b.PeerSessionAddress(1, node); got != a1 {
			t.Fatalf("fabric 1: %v, want %v", got, a1)
		}
		if got := b.PeerSessionAddress(2, node); got != a2 {
			t.Fatalf("fabric 2: %v, want %v", got, a2)
		}
		if got := b.PeerSessionAddress(3, node); got != nil {
			t.Fatalf("fabric 3 (no session): %v", got)
		}
	}
}

// emptyResolver answers a resolve with no address and no error.
type emptyResolver struct{}

func (emptyResolver) ResolveOperational(context.Context, [8]byte, uint64) ([]*net.UDPAddr, error) {
	return nil, nil
}

// TestResolveOperationalAddressEmptyAnswer: a resolver that finds nothing
// and reports no error leaves the address nil (R2-P6-test-0815).
func TestResolveOperationalAddressEmptyAnswer(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	b.AttachOperationalResolver(emptyResolver{})
	if a, err := b.ResolveOperationalAddress(context.Background(), [8]byte{}, 1); a != nil || err != nil {
		t.Fatal(a, err)
	}
}

// TestActiveSubscriptionSubjectsSkipsPASEAndDuplicates: a subscription on a
// PASE session is no active subscriber for Check-In suppression even when
// its session resolves to the fabric, and two subscriptions of one session
// count once (R2-P6-test-0815).
func TestActiveSubscriptionSubjectsSkipsPASEAndDuplicates(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	b.mu.Lock()
	b.sessions = sessionTable{7: {1, 0x1234}, 10: {1, 0x9999}}
	b.mu.Unlock()
	b.routing.subTargets.Store(uint32(1), subTarget{sessionID: 7, fabricIndex: 1, subjectNodeID: 0x1234})
	b.routing.subTargets.Store(uint32(2), subTarget{sessionID: 7, fabricIndex: 1, subjectNodeID: 0x1234})
	b.routing.subTargets.Store(uint32(3), subTarget{sessionID: 10, fabricIndex: 1, subjectNodeID: 0x9999, pase: true})
	for range 8 {
		got := b.ActiveSubscriptionSubjects(1)
		if len(got) != 1 || got[0].NodeID != 0x1234 {
			t.Fatalf("subjects %+v, want the one CASE subject 0x1234", got)
		}
	}
}

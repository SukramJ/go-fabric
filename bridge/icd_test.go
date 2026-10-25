// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/transport/mrp"
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

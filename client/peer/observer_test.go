// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
)

// TestObserve: a peer's observers see its sessions come and go, in
// registration order, after the set's own hook (NetworkClient.ts:81-83,
// Peer.ts:258-260, PeerSet.ts:95-102); stop removes one.
func TestObserve(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var seen []string
	note := func(s string) {
		mu.Lock()
		seen = append(seen, s)
		mu.Unlock()
	}
	h := newHarness(t, func(c *Context) { c.Disconnected = func(*Peer) { note("set disconnected") } })
	s1 := h.secureSession(t, 0x10, udpAddr("[fd00::1]:5540"))
	p := h.set.Adopt(s1)
	stopA := p.Observe(Observer{
		SessionAdded:   func(*exchange.Session) { note("a added") },
		SessionDeleted: func(*exchange.Session) { note("a deleted") },
	})
	p.Observe(Observer{
		SessionAdded:   func(*exchange.Session) { note("b added") },
		SessionDeleted: func(*exchange.Session) { note("b deleted") },
	})
	p.Observe(Observer{})
	s2 := h.secureSession(t, 0x11, udpAddr("[fd00::2]:5540"))
	h.set.Adopt(s2)
	s2.Close()
	eventually(t, "the session to leave", func() bool { return len(p.Sessions()) == 1 })
	stopA()
	s1.Close()
	want := []string{"a added", "b added", "a deleted", "b deleted", "set disconnected", "b deleted"}
	// The session leaves the peer under its lock and the observers hear of
	// it after the lock: wait for the notification, not for the list.
	eventually(t, "the last session's observers", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(seen) >= len(want)
	})
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != len(want) {
		t.Fatalf("seen %v, want %v", seen, want)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("seen %v, want %v", seen, want)
		}
	}
}

// TestObserveEstablishmentUnresponsive: the observers learn of an
// unresponsive establishment after the set's hook (Peer.ts:197-204,
// PeerConnection.ts:815-826).
func TestObserveEstablishmentUnresponsive(t *testing.T) {
	t.Parallel()
	var set, observed atomic.Int32
	h := newHarness(t, func(c *Context) { c.EstablishmentUnresponsive = func(*Peer) { set.Add(1) } })
	p, _ := h.set.AddKnownPeer(Descriptor{Address: testAddr, OperationalAddress: udpAddr(addr1)})
	p.Observe(Observer{EstablishmentUnresponsive: func() {
		if set.Load() == 1 {
			observed.Add(1)
		}
	}})
	connectAsync(p, ConnectOptions{ConnectionTimeout: -1})
	h.sigma1(t, addr1)
	for range 40 {
		h.clock.Advance(10 * time.Second)
		if observed.Load() > 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	eventually(t, "the observer", func() bool { return observed.Load() == 1 })
}

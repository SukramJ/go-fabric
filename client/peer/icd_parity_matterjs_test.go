// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

// The peer's part of the port of matter.js
// packages/node/test/behavior/system/icd/IcdClientTest.ts at 862ad75f:
// "MRP idle interval" (:2657-2690) and "address monitor" (:2693-2762). matter.js
// drives them on a LIT-operating MockSite pair and reaches into the protocol
// Peer; the port drives the Go peer directly, its LIT operation and its
// awaited state the reports the ICD client installs (SetLongIdleTimeOperating,
// SetICDAwait).

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
)

func TestIcdClientPeerParity(t *testing.T) {
	t.Parallel()
	lit := func() bool { return true }

	t.Run("MRP idle interval", func(t *testing.T) {
		t.Parallel()
		t.Run("floors the idle interval for a LIT peer only when no source gives an SII", func(t *testing.T) {
			t.Parallel()
			h := newHarness(t, nil)
			// A SII the peer reported in a session is honoured as it is.
			p, _ := h.set.AddKnownPeer(Descriptor{Address: testAddr, ReportedSessionParameters: &sessionparams.Parameters{SessionIdleInterval: 400}})
			p.SetLongIdleTimeOperating(lit)
			if got := p.SessionParameters().SessionIdleInterval; got != 400 {
				t.Fatalf("reported: %d ms", got)
			}
			// Without any SII the floor applies.
			h2 := newHarness(t, nil)
			q, _ := h2.set.AddKnownPeer(Descriptor{Address: testAddr, ReportedSessionParameters: &sessionparams.Parameters{}})
			q.SetLongIdleTimeOperating(lit)
			if got := q.SessionParameters().SessionIdleInterval; time.Duration(got)*time.Millisecond != LITMinIdleInterval {
				t.Fatalf("no SII: %d ms", got)
			}
			// An advertised SII is honoured as it is.
			h3 := newHarness(t, nil)
			r, _ := h3.set.AddKnownPeer(Descriptor{Address: testAddr, DiscoveryData: &discovery.TXTData{SII: dur(300 * time.Millisecond)}})
			r.SetLongIdleTimeOperating(lit)
			if got := r.SessionParameters().SessionIdleInterval; got != 300 {
				t.Fatalf("advertised: %d ms", got)
			}
		})
		t.Run("does not floor the idle interval for a non-LIT peer", func(t *testing.T) {
			t.Parallel()
			h := newHarness(t, nil)
			p, _ := h.set.AddKnownPeer(Descriptor{Address: testAddr})
			p.SetLongIdleTimeOperating(func() bool { return false })
			if got := p.SessionParameters().SessionIdleInterval; got != 500 {
				t.Fatalf("idle interval %d ms", got)
			}
		})
	})

	t.Run("address monitor", func(t *testing.T) {
		t.Parallel()
		t.Run("adopts a new mDNS address for a sleeping LIT ICD without probing", func(t *testing.T) {
			t.Parallel()
			h, p, n := monitored(t)
			p.SetICDAwait(lit)
			h.clock.Advance(3 * time.Minute) // past the probe cooldown
			h.announceAddrs(t, "fd00::2")
			eventually(t, "the address", func() bool { return len(p.Service().Addresses()) == 1 })
			h.udp.drain()
			if r := waitVerify(t, verifyAsync(context.Background(), p, ReasonAddressChange)); !r.ok {
				t.Fatal("verification failed")
			}
			h.udp.none(t) // no probe
			if !p.HasSession() || n.s.Addr().String() != addr2 {
				t.Fatalf("session at %v", n.s.Addr())
			}
			eventually(t, "the operational address", func() bool { return p.Descriptor().OperationalAddress.String() == addr2 })
		})
		t.Run("leaves a sleeping LIT ICD untouched while mDNS still lists its address", func(t *testing.T) {
			t.Parallel()
			h, p, n := monitored(t)
			p.SetICDAwait(lit)
			h.clock.Advance(3 * time.Minute)
			h.announceAddrs(t, "fd00::1", "fd00::2")
			eventually(t, "the addresses", func() bool { return len(p.Service().Addresses()) == 2 })
			h.udp.drain()
			if r := waitVerify(t, verifyAsync(context.Background(), p, ReasonAddressChange)); !r.ok {
				t.Fatal("verification failed")
			}
			h.udp.none(t)
			if !p.HasSession() || n.s.Addr().String() != addr1 {
				t.Fatalf("session at %v", n.s.Addr())
			}
		})
	})
}

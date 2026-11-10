// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

import (
	"context"
	"testing"
	"time"
)

// TestMonitorSuspectMigratesToAnAlternate: a session-suspect run whose
// current address does not answer probes the other discovered addresses
// and migrates the session to the one that answers, instead of closing it
// (BD-Matter-SuspectProbeWalksAlternates; matter.js closes it,
// PeerAddressMonitor.ts:219-221).
func TestMonitorSuspectMigratesToAnAlternate(t *testing.T) {
	t.Parallel()
	h, p, n := verifyFixture(t, "fd00::1", "fd00::2")
	res := verifyAsync(context.Background(), p, ReasonSessionSuspect)
	var probed []string
	for answered := false; !answered; {
		if len(probed) > 400 {
			t.Fatalf("probed %v, the alternate never", probed)
		}
		select {
		case d := <-h.udp.ch:
			probed = append(probed, d.to.String())
			if d.to.String() == addr2 {
				n.answer(t, d)
				answered = true
			}
		default:
			h.clock.Advance(time.Second)
			h.clock.Yield()
			probed = append(probed, "")
		}
	}
	if r := waitVerify(t, res); !r.ok {
		t.Fatal("migration: want true")
	}
	if n.s.Addr().String() != addr2 || n.s.IsPeerLost() {
		t.Fatalf("session at %v lost %v, want %s and kept", n.s.Addr(), n.s.IsPeerLost(), addr2)
	}
	eventually(t, "the operational address", func() bool { return p.Descriptor().OperationalAddress.String() == addr2 })
	var dests []string
	for _, a := range probed {
		if a != "" {
			dests = append(dests, a)
		}
	}
	for _, a := range dests {
		if a != addr1 && a != addr2 {
			t.Fatalf("probed %s", a)
		}
	}
	if dests[0] != addr1 {
		t.Fatalf("first probe to %s, want the current address", dests[0])
	}
}

// TestMonitorSuspectJoinedWalksAlternates: an address change joining a
// suspect run shares its walk; nothing answers, both callers see false
// and the session is lost after every address was probed.
func TestMonitorSuspectJoinedWalksAlternates(t *testing.T) {
	t.Parallel()
	h, p, n := verifyFixture(t, "fd00::2")
	suspect := verifyAsync(context.Background(), p, ReasonSessionSuspect)
	first := h.udp.next(t)
	change := verifyAsync(context.Background(), p, ReasonAddressChange)
	rs, to := failRuns(t, h, suspect, change)
	if rs[0].ok || rs[1].ok {
		t.Fatalf("results %v %v, want false false", rs[0].ok, rs[1].ok)
	}
	seen := map[string]bool{first.to.String(): true}
	for _, a := range to {
		seen[a] = true
	}
	if !seen[addr1] || !seen[addr2] || len(seen) != 2 {
		t.Fatalf("probed %v, want the current address and the alternate", seen)
	}
	if !n.s.IsPeerLost() {
		t.Fatal("the session survived")
	}
}

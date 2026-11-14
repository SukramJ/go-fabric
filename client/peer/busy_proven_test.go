// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/miekg/dns"
)

// TestBusyAtAnotherAddressOfTheProvenHostHoldsOnlyItsAttempt: a Busy
// answered from an address of the node's proven host that no session
// came from — one more AAAA record of the host's name, which any host on
// the link can announce — holds only the attempt that received it; only
// the address a session proved holds the peer (review II,
// R2-P3-ctl-lan-07).
func TestBusyAtAnotherAddressOfTheProvenHostHoldsOnlyItsAttempt(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		const other = "[fd00::66]:5540"
		r := newRig507(t)
		busyAt := time.Duration(-1)
		r.respond = func(u unsecured, to string) {
			if to == other {
				if busyAt < 0 {
					busyAt = r.now()
				}
				r.status(u, to, generalStatusBusy, secureChannelStatusBusy, busyData(65535))
			}
		}
		p := r.peer(i507Host)
		r.nodeOnly()
		rr := aaaaRR(r.hostQ, "fd00::66", 120)
		rr.Header().Class = dns.ClassINET
		r.mdns.announce(r.t, rr)
		r.waitAddrs(2, r.hostQ)
		r.connect(p)
		if _, ok := r.advanceUntil(5*time.Minute, func() bool { return busyAt >= 0 }); !ok {
			t.Fatalf("%s was never attempted; order: %s; %s", other, r.order(), r.state(p))
		}
		if d := p.busyRemaining(); d > 0 {
			t.Fatalf("a Busy from %s, an address no session came from, holds every Sigma1 to the node (%s) for %v; order: %s",
				other, addr1, d, r.order())
		}
	})
}

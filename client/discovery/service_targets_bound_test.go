// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package discovery

import (
	"fmt"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// TestService_SRVTargetsAreBounded: a Service tracks at most
// maxServiceTargets SRV targets, whatever a sender answers its instance
// with; past it the target discovered longest ago goes, so the service's
// addresses — every one a peer's connection target — stay at most
// maxServiceTargets × maxCachedAddressesPerHost (review II,
// R2-C2-bound-2152).
func TestService_SRVTargetsAreBounded(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		perPacket int
	}{
		{name: "one packet", perPacket: 20},
		{name: "one target a packet", perPacket: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			n, _, clk := testNames(t)
			s := n.Service(opCFID, opNode)
			defer s.Close()
			const targets = 20
			host := func(i int) string { return fmt.Sprintf("h%02d.local", i) }
			for first := 0; first < targets; first += tc.perPacket {
				clk.Advance(time.Millisecond)
				var answers, extra []dns.RR
				for i := first; i < first+tc.perPacket; i++ {
					answers = append(answers, srvRR(opQ, host(i), 5540, 120, false))
					for a := range maxCachedAddressesPerHost {
						extra = append(extra, aaaaRR(host(i), fmt.Sprintf("fd00::%x:%x", i+1, a+1), 120, false))
					}
				}
				deliver(t, n, 0, answers, extra...)
			}
			var tracked []string
			onLoop(t, n, func() {
				for _, e := range s.services.values() {
					tracked = append(tracked, e.host)
				}
			})
			if len(tracked) > maxServiceTargets {
				t.Fatalf("service tracks %d SRV targets %v, want at most %d", len(tracked), tracked, maxServiceTargets)
			}
			if got, limit := len(s.Addresses()), maxServiceTargets*maxCachedAddressesPerHost; got > limit {
				t.Fatalf("service holds %d addresses, want at most %d", got, limit)
			}
			if tc.perPacket == 1 {
				newest, first := false, false
				for _, h := range tracked {
					newest = newest || h == host(targets-1)
					first = first || h == host(0)
				}
				if !newest || first {
					t.Fatalf("targets %v: want the newest %s kept and the first %s gone", tracked, host(targets-1), host(0))
				}
				for _, a := range s.Addresses() {
					if a.Host == host(0) {
						t.Fatalf("address %v of the dropped target %s stays", a, host(0))
					}
				}
			}
		})
	}
}

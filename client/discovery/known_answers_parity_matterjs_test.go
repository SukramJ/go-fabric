// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package discovery

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/mdns"
	"github.com/miekg/dns"
)

// TestKnownAnswersParityMatterJS mirrors
// packages/general/test/net/dns-sd/DnssdNamesTest.ts:1577-1637 at 862ad75f
// (describe "known answers"): a query's known answers carry their remaining
// TTL, a partial second of it rounded up, and a record past half its TTL is
// left out so the responder refreshes it.
func TestKnownAnswersParityMatterJS(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		it      string
		elapsed time.Duration
		want    uint32 // 0: left out
	}{
		{"carry their remaining TTL", 30 * time.Second, 90},
		{"round a partial second of remaining TTL up", 59*time.Second + 500*time.Millisecond, 61},
		{"leave out a record past half its TTL so the responder refreshes it", 61 * time.Second, 0},
	} {
		t.Run(c.it, func(t *testing.T) {
			n, conn, clk := testNames(t)
			deliver(t, n, 0, []dns.RR{srvRR(instQ, hostQ, 5540, 120, true)})
			clk.advance(n, c.elapsed)
			conn.reset()
			n.Solicitor().Solicit(Solicitation{Name: n.Get(instQ), RecordTypes: []mdns.DNSType{TypeSRV}})
			qs := conn.queries(t)
			if len(qs) != 1 {
				t.Fatalf("queries = %+v, want one", qs)
			}
			switch {
			case c.want == 0 && len(qs[0].Answers) != 0:
				t.Fatalf("known answers = %+v, want none", qs[0].Answers)
			case c.want != 0 && (len(qs[0].Answers) != 1 || qs[0].Answers[0].TTL != c.want):
				t.Fatalf("known answers = %+v, want the SRV with TTL %d", qs[0].Answers, c.want)
			}
		})
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns

import (
	"context"
	"testing"

	"github.com/miekg/dns"
)

// TestSubtypePTRTTLParity pins the TTL of every record the responder
// publishes: 120 s, matter.js DEFAULT_MDNS_TTL
// (packages/general/src/codec/DnsCodec.ts:20 at 862ad75f) — the PTR, SRV,
// TXT, A, AAAA and subtype PTR records alike, the TXT record included where
// chip keeps 4500 s (src/lib/dnssd/wire/records/Txt.h:30 at 6170af84;
// matter.js first, concept v12 §D.2) — in the announcement and in a query
// answer, and 0 in the goodbye. Review 2026-11 W2-wire-06, issue #362.
func TestSubtypePTRTTLParity(t *testing.T) {
	t.Parallel()
	const want = 120
	ctx := context.Background()
	r, w, _ := newWiredResponder(map[int][]string{1: {"192.168.1.10", "fe80::1"}})
	svc := commissionableFixture(port1)
	if err := r.Publish(ctx, svc); err != nil {
		t.Fatal(err)
	}
	announcement := w.take()
	expectSent(t, announcement, 1)
	deliver(t, r, queryMsg([]dns.Question{question("_L1234._sub._matterc._udp.local", dns.TypePTR, false)}), 1, dummySource)
	answer := w.take()
	expectSent(t, answer, 1)
	if err := r.Withdraw(ctx, svc.InstanceName, svc.ServiceType); err != nil {
		t.Fatal(err)
	}
	goodbye := w.take()
	expectSent(t, goodbye, 1)

	kinds := map[string]func(dns.RR) bool{
		"PTR": func(rr dns.RR) bool {
			return rr.Header().Rrtype == dns.TypePTR && rr.Header().Name == "_matterc._udp.local."
		},
		"subtype PTR": func(rr dns.RR) bool {
			return rr.Header().Rrtype == dns.TypePTR && rr.Header().Name == "_L1234._sub._matterc._udp.local."
		},
		"SRV":  func(rr dns.RR) bool { return rr.Header().Rrtype == dns.TypeSRV },
		"TXT":  func(rr dns.RR) bool { return rr.Header().Rrtype == dns.TypeTXT },
		"A":    func(rr dns.RR) bool { return rr.Header().Rrtype == dns.TypeA },
		"AAAA": func(rr dns.RR) bool { return rr.Header().Rrtype == dns.TypeAAAA },
	}
	check := func(t *testing.T, what string, rrs []dns.RR, ttl uint32, match func(dns.RR) bool) {
		t.Helper()
		found := false
		for _, rr := range rrs {
			if match(rr) {
				found = true
				if rr.Header().Ttl != ttl {
					t.Errorf("%s: %v has TTL %d, want %d", what, rr, rr.Header().Ttl, ttl)
				}
			}
		}
		if !found {
			t.Errorf("%s: no such record", what)
		}
	}
	for name, match := range kinds {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			check(t, "announcement", announcement[0].msg.Answer, want, match)
			if name == "subtype PTR" {
				check(t, "query answer", answer[0].msg.Answer, want, match)
			} else if name != "PTR" {
				check(t, "query additional", answer[0].msg.Extra, want, match)
			}
		})
	}
	t.Run("goodbye", func(t *testing.T) {
		t.Parallel()
		for name, match := range kinds {
			check(t, name, goodbye[0].msg.Answer, 0, match)
		}
	})
}

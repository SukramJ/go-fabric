// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns

import (
	"net"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// unicastTo counts the packets sent by unicast to ip.
func unicastTo(w *fakeWire, ip net.IP) int {
	n := 0
	for _, p := range w.take() {
		if p.dst != nil && p.dst.IP.Equal(ip) {
			n++
		}
	}
	return n
}

// TestUnicastAnswersToOneSourceAreBounded: QU queries from one source
// get at most one unicast answer per service within a second — the
// one-second rule of RFC 6762 §6 applied to the source a unicast answer
// goes to — so queries with a spoofed source are not each reflected to
// it; a second later the source is answered by unicast again (review II,
// R2-P3-lan-dev-06).
func TestUnicastAnswersToOneSourceAreBounded(t *testing.T) {
	t.Parallel()
	r, w, clk := newServerFixture()
	// One ordinary multicast answer primes the peers' caches.
	deliver(t, r, queryMsg([]dns.Question{question(dummyQName, dns.TypeANY, false)}), 1, dummySource)
	_ = w.take()

	q := queryMsg([]dns.Question{question(dummyQName, dns.TypeANY, true)})
	const queries = 100
	for range queries {
		deliver(t, r, q, 1, dummySource)
	}
	if n := unicastTo(w, dummySource.IP); n != 1 {
		t.Fatalf("%d unicast answers to one source within one instant for %d queries, want 1", n, queries)
	}
	// Another source is answered on its own budget.
	other := &net.UDPAddr{IP: net.ParseIP("1.2.3.5"), Port: 5353}
	deliver(t, r, q, 1, other)
	if n := unicastTo(w, other.IP); n != 1 {
		t.Fatalf("%d unicast answers to a second source, want 1", n)
	}
	clk.Advance(time.Second)
	_ = w.take()
	deliver(t, r, q, 1, dummySource)
	if n := unicastTo(w, dummySource.IP); n != 1 {
		t.Errorf("%d unicast answers to the first source a second later, want 1", n)
	}
}

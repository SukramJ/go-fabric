// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns

import (
	"net"
	"testing"

	"github.com/miekg/dns"
)

// TestResponder_AnswersOnlyOnLinkSources: a query is answered — by
// multicast, by unicast, or held as a truncated query — only when its
// source is on the link of the interface it arrived on: an IPv6
// link-local address, or an address inside a prefix of that interface
// (RFC 6762 §11; review II, R2-P1-net-02). An off-link source gets
// nothing, not even the unicast answer its QU bit asks for once the
// records were multicast on the link.
func TestResponder_AnswersOnlyOnLinkSources(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name   string
		src    *net.UDPAddr
		onLink bool
	}{
		{"IPv4 inside the interface's prefix", &net.UDPAddr{IP: net.ParseIP("192.168.1.99"), Port: 5353}, true},
		{"IPv6 inside the interface's prefix", &net.UDPAddr{IP: net.ParseIP("2001:db8::99"), Port: 5353}, true},
		{"IPv6 link-local", &net.UDPAddr{IP: net.ParseIP("fe80::99"), Port: 5353, Zone: "fake1"}, true},
		{"IPv4 off the link", &net.UDPAddr{IP: net.ParseIP("8.8.8.8"), Port: 40000}, false},
		{"IPv4 link-local the interface has no address of", &net.UDPAddr{IP: net.ParseIP("169.254.1.1"), Port: 5353}, false},
		{"IPv6 in another prefix", &net.UDPAddr{IP: net.ParseIP("2001:db8:1::1"), Port: 40000}, false},
		{"IPv6 ULA the interface has no prefix of", &net.UDPAddr{IP: net.ParseIP("fd00::9"), Port: 5353}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			r, w, _ := newWiredResponder(map[int][]string{1: {"192.168.1.10", "2001:db8::7", "fe80::1"}})
			w.setNets(1, "192.168.1.10/24", "2001:db8::7/64", "fe80::1/64")
			setRecords(r, "foo", fooPTR(), fooSRV(), fooTXT(), fooA())
			want := 0
			if c.onLink {
				want = 1
			}

			// A multicast query.
			deliver(t, r, anyQuery(), 1, c.src)
			if got := w.take(); len(got) != want {
				t.Fatalf("a multicast query from %v got %d answer packets, want %d", c.src, len(got), want)
			}
			// The records were multicast on the link (by an on-link
			// querier's answer), so a QU query is answered by unicast.
			deliver(t, r, anyQuery(), 1, &net.UDPAddr{IP: net.ParseIP("192.168.1.50"), Port: 5353})
			w.take()
			deliver(t, r, queryMsg([]dns.Question{question(dummyQName, dns.TypeANY, true)}), 1, c.src)
			got := w.take()
			if len(got) != want {
				t.Fatalf("a QU query from %v got %d answer packets, want %d", c.src, len(got), want)
			}
			if c.onLink && (got[0].dst == nil || !got[0].dst.IP.Equal(c.src.IP)) {
				t.Errorf("the QU query's answer went to %v, want unicast to %v", got[0].dst, c.src)
			}
			// A truncated query is held only from an on-link source.
			tc := anyQuery()
			tc.Id = 7
			tc.Truncated = true
			deliver(t, r, tc, 1, c.src)
			if entries, _, _ := heldTruncated(r); entries != want {
				t.Errorf("a truncated query from %v left %d held queries, want %d", c.src, entries, want)
			}
		})
	}
}

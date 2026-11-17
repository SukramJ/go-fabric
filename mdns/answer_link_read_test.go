// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns

import (
	"fmt"
	"net"
	"testing"

	"github.com/miekg/dns"
)

// TestQueriesDoNotReadTheInterfacePerPacket holds R2-P6-bound-0217: the
// interface a query arrived on is read once per record cache, not once
// per answered packet — the read is two syscalls under the responder's
// lock, on the path every query on the link reaches.
func TestQueriesDoNotReadTheInterfacePerPacket(t *testing.T) {
	t.Parallel()
	r, w, _ := newServerFixture()
	deliver(t, r, queryMsg([]dns.Question{question(dummyQName, dns.TypeANY, false)}), 1, dummySource)
	_ = w.take()
	w.mu.Lock()
	before := w.linkReads
	w.mu.Unlock()
	const queries = 50
	for i := range queries {
		src := &net.UDPAddr{IP: net.ParseIP(fmt.Sprintf("1.2.3.%d", 10+i)), Port: 5353}
		deliver(t, r, queryMsg([]dns.Question{question(dummyQName, dns.TypeANY, true)}), 1, src)
	}
	if sent := len(w.take()); sent < queries {
		t.Fatalf("%d packets for %d unicast queries, want one each", sent, queries)
	}
	w.mu.Lock()
	reads := w.linkReads - before
	w.mu.Unlock()
	if reads != 0 {
		t.Fatalf("%d interface reads for %d answered queries within the record cache's lifetime, want 0", reads, queries)
	}
}

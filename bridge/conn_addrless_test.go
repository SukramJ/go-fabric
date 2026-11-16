// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"net"
	"testing"
)

// addrlessConn is a reliable transport.Conn that names no peer address.
type addrlessConn struct{ sent int }

func (c *addrlessConn) Send([]byte) error { c.sent++; return nil }
func (*addrlessConn) Remote() net.Addr    { return nil }
func (*addrlessConn) Reliable() bool      { return true }
func (*addrlessConn) MaxMessageSize() int { return 1500 }

// TestRouteWithoutPeerAddress (review II, phase 5b): a connection whose
// Remote is nil is no TCP route — the size limit, the send, the Large
// Message test, the session's transport and the route's names read it
// without dereferencing the nil address. Before, connMessageLimit and
// sendRouted called Remote().Network() unguarded and panicked.
func TestRouteWithoutPeerAddress(t *testing.T) {
	t.Parallel()
	c := &addrlessConn{}
	r := &connRoute{conn: c}
	b := &Bridge{}
	if limit, ok := b.connMessageLimit(r, 1); !ok || limit != 1500 {
		t.Errorf("connMessageLimit = %d, %v; want the channel's 1500, true", limit, ok)
	}
	if err := b.sendRouted(nil, r, 1, make([]byte, 4000)); err != nil || c.sent != 1 {
		t.Errorf("sendRouted = %v (sent %d); want the message sent", err, c.sent)
	}
	if routeCarriesLargeMessages(r) {
		t.Error("an address-less route carries Large Messages")
	}
	if got := r.Network(); got != "" {
		t.Errorf("Network = %q, want empty", got)
	}
	if got := r.String(); got == "" {
		t.Error("String is empty")
	}
	s := &PeerSession{link: peerLink{addr: r}}
	if got := s.transport(); got != "" {
		t.Errorf("transport = %q, want empty", got)
	}
	var none *connRoute
	if got := (&PeerSession{link: peerLink{addr: none}}).transport(); got != "" {
		t.Errorf("transport of a typed nil route = %q, want empty", got)
	}
}

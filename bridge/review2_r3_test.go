// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"sync/atomic"
	"testing"
)

// endNotingConn is a fakeConn that records the bridge's notes of session
// traffic and of its sessions' end.
type endNotingConn struct {
	fakeConn
	traffic atomic.Int32
	ended   atomic.Int32
}

func (c *endNotingConn) NoteSessionTraffic() { c.traffic.Add(1) }
func (c *endNotingConn) NoteSessionsEnded()  { c.ended.Add(1) }

// TestLastSessionEndTellsTheConnection (R3-bound-3402, a regression of the
// R2-C2-bound-0201 fix): when the last session bound to a connection goes
// while a handshake keeps the connection open, the bridge tells the
// connection that it carries no session any more — so the replies it then
// sends to the peer's unsecured frames do not renew its place at the TCP
// listener's bound. A connection that still carries a session is not told.
func TestLastSessionEndTellsTheConnection(t *testing.T) {
	t.Parallel()
	b := newStartedBridgeWithSnapshotter(t, wbEmptySnapshotter)
	c := &endNotingConn{}
	route := b.routeFor(c)
	b.bindSessionRoute(0x0901, route)
	b.bindSessionRoute(0x0902, route)
	b.holdHandshake(route, onExchange(1), false) // a Sigma1 in flight
	b.unbindSessionRoute(0x0901)
	if n := c.ended.Load(); n != 0 {
		t.Fatalf("told %d times with a session still bound, want 0", n)
	}
	b.unbindSessionRoute(0x0902)
	if n := c.ended.Load(); n != 1 {
		t.Fatalf("told %d times after the last session went with a handshake in flight, want 1", n)
	}
}

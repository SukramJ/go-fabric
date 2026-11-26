// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Kill tests for the C4 survivors in conn.go: the route helpers, the
// session-route binding, the handshake holds and the connection close
// (phase 8 wave 2b, #602, decision 692). Each test calls the helper
// directly and asserts synchronously.

package bridge

import (
	"errors"
	"net"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/diagevent"
	"thingfabric.dev/go-fabric/transport/mrp"
)

// noterConnW2B2b is a recording connection that takes session-traffic
// notes and can be closed.
type noterConnW2B2b struct {
	fakeConn
	nmu           sync.Mutex
	notes, closes int
}

func (c *noterConnW2B2b) NoteSessionTraffic() {
	c.nmu.Lock()
	defer c.nmu.Unlock()
	c.notes++
}

func (c *noterConnW2B2b) Close() error {
	c.nmu.Lock()
	defer c.nmu.Unlock()
	c.closes++
	return nil
}

func (c *noterConnW2B2b) counts() (notes, closes int) {
	c.nmu.Lock()
	defer c.nmu.Unlock()
	return c.notes, c.closes
}

// sessionReporterW2B2b names the session a handshake established.
type sessionReporterW2B2b struct {
	sid uint16
	ok  bool
}

func (r sessionReporterW2B2b) establishedSession() (uint16, bool) { return r.sid, r.ok }

// failingRegistryW2B2b is a session registry whose Close always fails.
type failingRegistryW2B2b struct{ fakeSessionRegistry }

func (*failingRegistryW2B2b) Close(uint16) error { return errors.New("unknown session") }

// boundW2B2b reports whether sid has a recorded route.
func boundW2B2b(b *Bridge, sid uint16) bool {
	_, ok := b.sessionPeerAddrs.Load(sid)
	return ok
}

// TestSendViaNilUDPAddrHasNoRouteW2B2b: a typed nil UDP address is no
// route, with a listener too. Kills conn.go:191 (guard off).
func TestSendViaNilUDPAddrHasNoRouteW2B2b(t *testing.T) {
	t.Parallel()
	b := newStartedBridge(t)
	b.mu.RLock()
	listener := b.listener
	b.mu.RUnlock()
	if listener == nil {
		t.Fatal("started bridge has no listener")
	}
	if err := sendVia(listener, (*net.UDPAddr)(nil), []byte{0}); !errors.Is(err, errNoRoute) {
		t.Fatalf("sendVia(nil *UDPAddr) = %v, want errNoRoute", err)
	}
}

// TestNilConnRoutesAreSkippedW2B2b: a typed nil connection route, as the
// new or the former route of a session, is counted, released and noted
// nowhere; a live one takes the session-traffic note. Kills conn.go:247
// and :253 (&& → ||), :275 (&& → ||, != → ==).
func TestNilConnRoutesAreSkippedW2B2b(t *testing.T) {
	t.Parallel()
	b := unstartedBridgeW2B2b(t)
	b.bindSessionRoute(11, (*connRoute)(nil))
	b.bindSessionRoute(11, &net.UDPAddr{IP: net.IPv6loopback, Port: 5540})
	noteSessionTraffic((*connRoute)(nil))

	c := &noterConnW2B2b{}
	noteSessionTraffic(&connRoute{conn: c})
	if notes, _ := c.counts(); notes != 1 {
		t.Fatalf("the connection took %d session-traffic notes, want 1", notes)
	}
}

// TestRetransmittedFirstMessageKeepsTheHoldW2B2b: a second first message
// on an exchange already held leaves the hold as it was, its initiator
// included. Kills conn.go:366 (guard off).
func TestRetransmittedFirstMessageKeepsTheHoldW2B2b(t *testing.T) {
	t.Parallel()
	b := unstartedBridgeW2B2b(t)
	r := &connRoute{conn: &fakeConn{}}
	first := HandshakeKey{ExchangeID: 3, InitiatorNodeID: 0xA}
	b.holdHandshake(r, first, false)
	b.holdHandshake(r, HandshakeKey{ExchangeID: 3, InitiatorNodeID: 0xB}, false)
	b.releaseHandshake(r, first)
	b.routeBindMu.Lock()
	held := len(r.handshakes)
	b.routeBindMu.Unlock()
	if held != 0 {
		t.Fatalf("the first initiator's release left %d holds: the second first message took the hold over", held)
	}
}

// TestSettleHandshakeBindsOnlyAnEstablishedSessionW2B2b: a failure
// StatusReport over a connection, a success with no source and a reporter
// that names no session bind nothing. Kills conn.go:436 (guard off),
// :450 and :479 (&& → ||).
func TestSettleHandshakeBindsOnlyAnEstablishedSessionW2B2b(t *testing.T) {
	t.Parallel()
	b := unstartedBridgeW2B2b(t)
	success := mrp.EncodeStatusReport(mrp.SCStatusGeneralSuccess, uint32(mrp.SecureChannelProtocolID), 0, nil)
	failure := mrp.EncodeStatusReport(mrp.SCStatusGeneralFailure, uint32(mrp.SecureChannelProtocolID), 0, nil)
	key := HandshakeKey{ExchangeID: 4, InitiatorNodeID: 0xA}

	b.settleHandshake(&connRoute{conn: &fakeConn{}}, key, sessionReporterW2B2b{sid: 21, ok: true}, mrp.SCOpcodeStatusReport, failure, nil)
	if boundW2B2b(b, 21) {
		t.Error("a failure StatusReport bound the handshake's session to the connection")
	}
	b.settleHandshake(nil, key, sessionReporterW2B2b{sid: 22, ok: true}, mrp.SCOpcodeStatusReport, success, nil)
	if boundW2B2b(b, 22) {
		t.Error("a handshake with no source bound its session")
	}
	b.bindEstablishedSession(&net.UDPAddr{IP: net.IPv6loopback, Port: 5540}, sessionReporterW2B2b{sid: 23, ok: false})
	b.bindEstablishedSession(&net.UDPAddr{IP: net.IPv6loopback, Port: 5540}, sessionReporterW2B2b{sid: 0, ok: true})
	if boundW2B2b(b, 23) || boundW2B2b(b, 0) {
		t.Error("a reporter naming no session had a route bound")
	}
}

// TestCloseConnRouteClosesOnlyTCPW2B2b: a connection that is not TCP is
// left open when its last session goes. Kills conn.go:498 (guard off).
func TestCloseConnRouteClosesOnlyTCPW2B2b(t *testing.T) {
	t.Parallel()
	b := unstartedBridgeW2B2b(t)
	c := &noterConnW2B2b{}
	b.closeConnRoute(&connRoute{conn: c})
	if _, closes := c.counts(); closes != 0 {
		t.Fatalf("a non-TCP connection was closed %d times", closes)
	}
}

// TestSessionHoldsSubscriptionWithoutManagerW2B2b: with no subscription
// manager no session holds a subscription. Kills conn.go:535 (guard off).
func TestSessionHoldsSubscriptionWithoutManagerW2B2b(t *testing.T) {
	t.Parallel()
	if unstartedBridgeW2B2b(t).sessionHoldsSubscription(1) {
		t.Fatal("a session holds a subscription without a manager")
	}
}

// TestEvictSessionRegistryMissRecordsNothingW2B2b: a session the registry
// does not know is not reported closed in the diagnostic trace. Kills
// conn.go:744 (guard off (err)).
func TestEvictSessionRegistryMissRecordsNothingW2B2b(t *testing.T) {
	t.Parallel()
	b := unstartedBridgeW2B2b(t)
	ring := diagevent.NewRing(8)
	b.AttachDiagnosticEvents(ring)
	b.AttachSessionRegistry(&failingRegistryW2B2b{})
	b.evictSession(31, &connRoute{conn: &fakeConn{}})
	if got := ring.Snapshot(); len(got) != 0 {
		t.Fatalf("a registry miss recorded %d diagnostic events: %+v", len(got), got)
	}
}

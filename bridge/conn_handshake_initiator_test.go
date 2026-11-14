// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// TestEvictedCASEResponderReleasesOnlyItsOwnConnectionHold: a CASE
// handshake holds the TCP connection it runs on until it ends; when the
// per-exchange state of one initiator's handshake is evicted
// ([Bridge.ForgetSigma1Replied]), the hold of that handshake ends, and a
// handshake of another initiator on another connection that picked the
// same exchange id keeps its hold. Exchange ids are the initiator's choice
// and only unique per initiator ([HandshakeKey]).
func TestEvictedCASEResponderReleasesOnlyItsOwnConnectionHold(t *testing.T) {
	t.Parallel()
	b := newStartedBridgeWithSnapshotter(t, wbEmptySnapshotter)
	b.AttachCaseHandler(sigma2ThenSuccessHandler{})
	const exchange = uint16(0x0707)
	type peer struct {
		nodeID uint64
		route  *connRoute
	}
	peers := []peer{{nodeID: 0xA1}, {nodeID: 0xB2}}
	for i := range peers {
		route, ok := b.routeFor(&fakeConn{}).(*connRoute)
		if !ok {
			t.Fatalf("route %T", route)
		}
		peers[i].route = route
		hdr := &message.Header{MessageCounter: 10, HasSourceNodeID: true, SourceNodeID: peers[i].nodeID}
		proto := message.ProtocolHeader{
			Initiator: true, NeedsAck: true, Opcode: mrp.SCOpcodeSigma1, ExchangeID: exchange, ProtocolID: mrp.SecureChannelProtocolID,
		}
		if err := b.dispatchSecureChannel(route, hdr, proto, []byte{0x15, 0x18}); err != nil {
			t.Fatalf("Sigma1 of %#x: %v", peers[i].nodeID, err)
		}
	}
	held := func(r *connRoute) int {
		b.routeBindMu.Lock()
		defer b.routeBindMu.Unlock()
		return r.liveHandshakesLocked(time.Now())
	}
	for _, p := range peers {
		if n := held(p.route); n != 1 {
			t.Fatalf("connection of %#x holds %d handshakes after its Sigma1, want 1", p.nodeID, n)
		}
	}
	b.ForgetSigma1Replied(HandshakeKey{InitiatorNodeID: peers[0].nodeID, ExchangeID: exchange})
	if n := held(peers[0].route); n != 0 {
		t.Errorf("connection of the evicted handshake holds %d handshakes, want 0", n)
	}
	if n := held(peers[1].route); n != 1 {
		t.Errorf("connection of the other initiator's handshake holds %d handshakes, want 1: another initiator's eviction ended its hold", n)
	}
}

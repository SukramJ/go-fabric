// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/secure/sessionparams"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// paramsCaseHandler is a CASE handler that kept the initiator's Sigma1
// SessionParameters, as [CaseAdapter.PeerSessionParams] does.
type paramsCaseHandler struct{ params *sessionparams.Parameters }

func (h paramsCaseHandler) PeerSessionParams() *sessionparams.Parameters { return h.params }

// firstWaitRange is the window the first retransmission of a message with
// base interval base falls into: base × MARGIN × (1 + [0, JITTER]).
func firstWaitRange(base time.Duration) (lo, hi time.Duration) {
	lo = time.Duration(float64(base) * mrp.MRPBackoffMargin)
	hi = time.Duration(float64(lo) * (1 + mrp.MRPBackoffJitterFactor))
	return lo, hi
}

// The responder's Sigma2 retransmits at the initiator's stated active
// interval: matter.js creates the responder's unsecured session with the
// peer active (UnsecuredSession.ts:30 `setActiveTimestamp:
// !isInitiator`), adopts the Sigma1 initiatorSessionParams before Sigma2
// (CaseServer.ts:120-123) and retransmissionIntervalOf picks
// activeInterval for an active peer (MRP.ts:181). An initiator that
// states none still counts active, so the spec default active interval
// (300 ms) applies, not the idle one.
func TestSigma2FirstRetransmitFollowsTheInitiatorsActiveInterval(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		handler any
		base    time.Duration
	}{
		"stated": {
			handler: paramsCaseHandler{&sessionparams.Parameters{SessionIdleInterval: 5000, SessionActiveInterval: 100}},
			base:    100 * time.Millisecond,
		},
		"none stated":   {handler: paramsCaseHandler{}, base: mrp.SessionActiveIntervalDefault},
		"no capability": {handler: struct{}{}, base: mrp.SessionActiveIntervalDefault},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			b := newStartedBridge(t)
			b.AttachAckTracker(mrp.NewAckTracker(50 * time.Millisecond))
			peer, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
			if err != nil {
				t.Fatalf("ListenUDP: %v", err)
			}
			t.Cleanup(func() { _ = peer.Close() })
			hdr := &message.Header{MessageCounter: 11}
			proto := message.ProtocolHeader{
				Initiator:  true,
				NeedsAck:   true,
				Opcode:     mrp.SCOpcodeSigma1,
				ExchangeID: 0x4242,
				ProtocolID: mrp.SecureChannelProtocolID,
			}
			process := func([]byte) (uint8, []byte, error) { return mrp.SCOpcodeSigma2, []byte{0x15, 0x18}, nil }
			before := time.Now()
			if err := b.handleCase(peer.LocalAddr(), hdr, proto, nil, "sigma1", process, tc.handler); err != nil {
				t.Fatalf("handleCase: %v", err)
			}
			after := time.Now()
			b.outboundReliable.mu.Lock()
			var entry *outboundEntry
			for k, p := range b.outboundReliable.pending {
				if k.sessionID == 0 && p.exchangeID == proto.ExchangeID {
					entry = p
				}
			}
			b.outboundReliable.mu.Unlock()
			if entry == nil {
				t.Fatal("Sigma2 not tracked for retransmission")
			}
			lo, hi := firstWaitRange(tc.base)
			if w := entry.nextSendAt.Sub(before); w < lo {
				t.Fatalf("first retransmit after at most %v, want at least %v", w, lo)
			}
			if w := entry.nextSendAt.Sub(after); w > hi {
				t.Fatalf("first retransmit after at least %v, want at most %v", w, hi)
			}
		})
	}
}

// Once the initiator has been quiet for its active threshold, the next
// retransmission falls back to its idle interval (matter.js
// Session.ts:149-151 isPeerActive, re-evaluated per transmission by
// MRP.ts:181).
func TestHandshakeRetransmitFallsBackToIdleAfterTheActiveThreshold(t *testing.T) {
	t.Parallel()
	rx := time.Unix(1_000_000, 0)
	peerTiming := newHandshakePeerTiming(&sessionparams.Parameters{
		SessionIdleInterval: 2000, SessionActiveInterval: 100, SessionActiveThreshold: 1000,
	}, rx)
	tr := newOutboundReliableTracker(nil, nil)
	tr.trackUnsecured(7, 9, []byte{1}, &net.UDPAddr{IP: net.IPv6loopback, Port: 5540}, rx, peerTiming)
	p := tr.pending[outboundKey{0, 7}]
	lo, hi := firstWaitRange(100 * time.Millisecond)
	if w := p.nextSendAt.Sub(rx); w < lo || w > hi {
		t.Fatalf("first wait %v, want %v to %v", w, lo, hi)
	}
	late := rx.Add(1500 * time.Millisecond)
	tr.Tick(late, func(net.Addr, []byte) error { return nil })
	lo, hi = firstWaitRange(2000 * time.Millisecond)
	if w := p.nextSendAt.Sub(late); w < lo || w > hi {
		t.Fatalf("wait after the threshold %v, want the idle range %v to %v", w, lo, hi)
	}
}

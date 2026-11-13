// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// readStandaloneAck reads one datagram from the peer socket and returns
// the counter its standalone ack acknowledges.
func readStandaloneAck(t *testing.T, read func([]byte) (int, error)) (uint32, bool) {
	t.Helper()
	buf := make([]byte, 512)
	n, err := read(buf)
	if err != nil {
		return 0, false
	}
	_, hdrLen, err := message.UnmarshalHeader(buf[:n])
	if err != nil {
		t.Fatalf("UnmarshalHeader: %v", err)
	}
	proto, _, err := message.UnmarshalProtocolHeader(buf[hdrLen:n])
	if err != nil {
		t.Fatalf("UnmarshalProtocolHeader: %v", err)
	}
	if proto.Opcode != mrp.StandaloneAckOpcode || !proto.HasAck {
		t.Fatalf("datagram is opcode %#x, ack %v, want a standalone ack", proto.Opcode, proto.HasAck)
	}
	return proto.AckCounter, true
}

// TestDisplacedAckIsSentAfterItsSuccessorWasPiggybacked: two reliable
// messages arrive on one exchange before the first is acknowledged, so
// the first obligation is displaced and waits for the pump's next pass
// ([mrp.AckTracker.Owe] — an MRP ack names one counter). The reply that
// piggybacks the second counter must leave the exchange's reply route to
// the displaced one: its standalone ack still has to reach the peer. The
// pump passes once per ackPumpInterval, so a reply inside that interval
// is the ordinary case, not a race. Both ways the piggyback is taken are
// covered: after the send ([Bridge.dischargeOwedAck]) and before it
// ([Bridge.takeOwedAck]).
func TestDisplacedAckIsSentAfterItsSuccessorWasPiggybacked(t *testing.T) {
	t.Parallel()
	const (
		exchangeID uint16 = 0x4411
		first      uint32 = 10
		second     uint32 = 11
	)
	piggybacks := map[string]func(b *Bridge){
		"dischargeOwedAck": func(b *Bridge) { b.dischargeOwedAck(0, exchangeID, false) },
		"takeOwedAck": func(b *Bridge) {
			if counter, ok := b.takeOwedAck(0, exchangeID, false); !ok || counter != second {
				t.Errorf("takeOwedAck = (%d, %v), want the second counter %d", counter, ok, second)
			}
		},
	}
	for name, piggyback := range piggybacks {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			b := newStartedBridge(t)
			b.AttachAckTracker(mrp.NewAckTracker(time.Hour)) // attached after Start: no pump
			peerConn, peerAddr := openPeerSocket(t)
			defer peerConn.Close()

			proto := buildNeedsAckProto(exchangeID, first)
			proto.Initiator = true
			b.owedInboundAck(peerAddr, buildMsgHdr(first), proto)
			b.owedInboundAck(peerAddr, buildMsgHdr(second), proto)
			piggyback(b)

			if n := b.RunAckPumpOnce(time.Now()); n != 1 {
				t.Fatalf("RunAckPumpOnce = %d obligations, want the displaced one", n)
			}
			if err := peerConn.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
				t.Fatal(err)
			}
			got, ok := readStandaloneAck(t, func(buf []byte) (int, error) {
				n, _, err := peerConn.ReadFromUDP(buf)
				return n, err
			})
			if !ok {
				t.Fatalf("no standalone ack for the displaced counter %d reached the peer: its reply route was released with the piggyback", first)
			}
			if got != first {
				t.Fatalf("standalone ack acknowledges %d, want the displaced counter %d", got, first)
			}
			if n := routesOfExchange(b, 0, exchangeID); n != 0 {
				t.Errorf("reply routes of the exchange once nothing is owed = %d, want 0", n)
			}
		})
	}
}

// TestDisplacedAcksOfOnePassAreAllSent: three reliable messages on one
// exchange leave two displaced obligations and one pending; a pump pass
// that takes all three routes all three. Before, the first standalone ack
// of the pass released the route and the others were dropped.
func TestDisplacedAcksOfOnePassAreAllSent(t *testing.T) {
	t.Parallel()
	const exchangeID uint16 = 0x4412
	b := newStartedBridge(t)
	b.AttachAckTracker(mrp.NewAckTracker(0)) // due at once; attached after Start: no pump
	peerConn, peerAddr := openPeerSocket(t)
	defer peerConn.Close()

	proto := buildNeedsAckProto(exchangeID, 10)
	proto.Initiator = true
	for _, counter := range []uint32{10, 11, 12} {
		b.owedInboundAck(peerAddr, buildMsgHdr(counter), proto)
	}
	if n := b.RunAckPumpOnce(time.Now()); n != 3 {
		t.Fatalf("RunAckPumpOnce = %d obligations, want 3", n)
	}
	got := map[uint32]bool{}
	for range 3 {
		if err := peerConn.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
			t.Fatal(err)
		}
		counter, ok := readStandaloneAck(t, func(buf []byte) (int, error) {
			n, _, err := peerConn.ReadFromUDP(buf)
			return n, err
		})
		if !ok {
			break
		}
		got[counter] = true
	}
	if !got[10] || !got[11] || !got[12] {
		t.Fatalf("standalone acks reached the peer for %v, want counters 10, 11 and 12", got)
	}
	if n := routesOfExchange(b, 0, exchangeID); n != 0 {
		t.Errorf("reply routes of the exchange once nothing is owed = %d, want 0", n)
	}
}

// TestReplyRouteSurvivesAnArrivalRacingItsRelease: a reliable message
// that arrives while a reply takes the exchange's owed ack keeps its
// route — the arrival's route and obligation are recorded in one step
// against the release. Before, the release could fall between the two
// (check, then delete) and leave an obligation without a route.
func TestReplyRouteSurvivesAnArrivalRacingItsRelease(t *testing.T) {
	t.Parallel()
	const exchangeID uint16 = 0x4413
	b := newStartedBridge(t)
	tracker := mrp.NewAckTracker(time.Hour) // attached after Start: no pump
	b.AttachAckTracker(tracker)
	_, peerAddr := openPeerSocket(t)
	proto := buildNeedsAckProto(exchangeID, 0)
	proto.Initiator = true

	for i := range 20000 {
		counter := uint32(2*i + 1)
		b.owedInboundAck(peerAddr, buildMsgHdr(counter), proto)
		start := make(chan struct{})
		done := make(chan struct{}, 2)
		go func() {
			<-start
			b.takeOwedAck(0, exchangeID, false)
			done <- struct{}{}
		}()
		go func() {
			<-start
			b.owedInboundAck(peerAddr, buildMsgHdr(counter+1), proto)
			done <- struct{}{}
		}()
		close(start)
		<-done
		<-done
		if tracker.Owes(0, exchangeID, false) && routesOfExchange(b, 0, exchangeID) == 0 {
			t.Fatalf("iteration %d: an obligation is owed on the exchange and its reply route is gone", i)
		}
		// Leave nothing behind for the next round.
		tracker.Due(time.Now().Add(2 * time.Hour))
		b.routing.exchangeSrcs.Delete(mrp.ExchangeKey{SessionID: 0, ExchangeID: exchangeID, Initiator: false})
	}
}

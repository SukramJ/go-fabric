// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"crypto/rand"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/secure/operational"
	"github.com/SukramJ/go-fabric/secure/sigma"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

func peerLossKeys(t *testing.T) sigma.SessionKeys {
	t.Helper()
	var keys sigma.SessionKeys
	if _, err := rand.Read(keys.I2RKey[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := rand.Read(keys.R2IKey[:]); err != nil {
		t.Fatal(err)
	}
	return keys
}

// TestInitiatedExchangePeerLoss_ForceClosesPeerSessions verifies the
// peer-loss path of an exchange this node opened (the OTA requestor's
// QueryImage to a provider): when the outbound-reliable tracker gives up
// on the message, every session with that peer established before the
// exchange is force-closed without a CloseSession, a session the peer
// established afterwards survives, and the next connect no longer finds
// a session to reuse. Mirrors matter.js MessageExchange.ts:620 →
// ExchangeManager.ts:556-580 → SessionManager.ts:677-695. Observed in
// TC-SU-2.2 step 2: the provider restarted under the same node id, the
// requestor kept invoking on the dead session and never reached it.
func TestInitiatedExchangePeerLoss_ForceClosesPeerSessions(t *testing.T) {
	t.Parallel()
	dev := startResumeDevice(t, newMemSubscriptionStore(), func(br *Bridge, _ *operational.Manager) {
		br.AttachAckTracker(mrp.NewAckTracker(0))
	})
	var graceful atomic.Int32
	dev.sessions.SetGracefulCloseNotifier(func(uint16, *channel.Session) { graceful.Add(1) })

	// A peer that never answers: a bound socket nobody reads.
	silent, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = silent.Close() })
	peerAddr, ok := silent.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatalf("peer address %T", silent.LocalAddr())
	}

	const (
		initiatorSID = uint16(0x0A01)
		responderSID = uint16(0x0A02)
		laterSID     = uint16(0x0A03)
	)
	if _, err := dev.sessions.OpenFromSigmaAsInitiatorWithID(initiatorSID, resumeFabricIndex, resumeDeviceNode, resumeCtrlNode, 0x0B01, nil, peerLossKeys(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := dev.sessions.OpenFromSigmaWithID(responderSID, resumeFabricIndex, resumeDeviceNode, resumeCtrlNode, 0x0B02, nil, peerLossKeys(t)); err != nil {
		t.Fatal(err)
	}
	dev.br.sessionPeerAddrs.Store(initiatorSID, peerAddr)
	dev.br.sessionPeerAddrs.Store(responderSID, peerAddr)

	link, ok := dev.br.existingPeerSession(resumeFabricIndex, resumeCtrlNode)
	if !ok {
		t.Fatal("no session to reuse before the loss")
	}
	ex, err := dev.br.openInitiatedExchange(link)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ex.Close() }()
	// Established after the exchange was opened: not what went unanswered.
	time.Sleep(time.Millisecond)
	if _, err := dev.sessions.OpenFromSigmaWithID(laterSID, resumeFabricIndex, resumeDeviceNode, resumeCtrlNode, 0x0B03, nil, peerLossKeys(t)); err != nil {
		t.Fatal(err)
	}

	if err := ex.Send(context.Background(), im.InteractionModelProtocolID, im.OpcodeInvokeRequest, []byte{0x15, 0x18}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	dev.br.mu.RLock()
	outbound := dev.br.outboundReliable
	dev.br.mu.RUnlock()
	if outbound == nil {
		t.Fatal("no outbound-reliable tracker")
	}
	// Drive the retransmissions to exhaustion without waiting out the
	// backoff: each tick is an hour later than the previous one.
	now := time.Now()
	for i := 0; i <= mrp.MaxRetransmissions+1; i++ {
		dev.br.tickOutboundReliable(outbound, now.Add(time.Duration(i+1)*time.Hour))
	}

	for _, sid := range []uint16{initiatorSID, responderSID} {
		if _, err := dev.sessions.Get(sid); !errors.Is(err, operational.ErrSessionNotFound) {
			t.Errorf("session %#x after the loss: %v, want ErrSessionNotFound", sid, err)
		}
	}
	if _, err := dev.sessions.Get(laterSID); err != nil {
		t.Errorf("session established after the exchange must survive: %v", err)
	}
	if n := graceful.Load(); n != 0 {
		t.Errorf("CloseSession sent %d times to a lost peer, want 0", n)
	}
	if _, ok := dev.br.existingPeerSession(resumeFabricIndex, resumeCtrlNode); ok {
		t.Error("a session of the lost peer is still offered for reuse")
	}
}

// TestInitiatedPeerLoss_UnknownExchangeIsNoOp pins that the initiated-
// exchange path alone ignores a counter outside any exchange it opened; a
// subscription report reaches peer loss through reportPeerLoss instead.
func TestInitiatedPeerLoss_UnknownExchangeIsNoOp(t *testing.T) {
	t.Parallel()
	dev := startResumeDevice(t, newMemSubscriptionStore(), nil)
	if _, err := dev.sessions.OpenFromSigmaWithID(0x0C01, resumeFabricIndex, resumeDeviceNode, resumeCtrlNode, 0x0D01, nil, peerLossKeys(t)); err != nil {
		t.Fatal(err)
	}
	dev.br.handleInitiatedPeerLoss(0x0C01, 0x7777)
	if _, err := dev.sessions.Get(0x0C01); err != nil {
		t.Fatalf("session closed by a loss report of an unknown exchange: %v", err)
	}
}

// TestReportExhaustionPeerLoss_ClosesPeerSessionsAndSubscriptions: an
// ongoing report the peer never acknowledges is peer loss, as on any
// exchange this node opens (matter.js MessageExchange.ts:616-621 →
// ExchangeManager.ts:569-573 → SessionManager.ts:677-695): both sessions of
// the subscriber established before the report's exchange are force-closed
// without a CloseSession, the subscription on the other session falls with
// its session through the registry's close hook, and a session of the same
// peer established afterwards, and the session and subscription of another
// peer, survive.
func TestReportExhaustionPeerLoss_ClosesPeerSessionsAndSubscriptions(t *testing.T) {
	t.Parallel()
	dev := startResumeDevice(t, newMemSubscriptionStore(), func(br *Bridge, _ *operational.Manager) {
		br.AttachAckTracker(mrp.NewAckTracker(0))
	})
	var graceful atomic.Int32
	dev.sessions.SetGracefulCloseNotifier(func(uint16, *channel.Session) { graceful.Add(1) })

	silent, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = silent.Close() })
	peerAddr, ok := silent.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatalf("peer address %T", silent.LocalAddr())
	}

	const (
		reportSID = uint16(0x0E01) // carries the unanswered report
		siblingID = uint16(0x0E02) // same peer, established before
		otherSID  = uint16(0x0E03) // another peer
		laterSID  = uint16(0x0E04) // same peer, established after the report
		otherNode = uint64(0xD0D0D0D0)
	)
	for _, s := range []struct {
		sid  uint16
		peer uint64
	}{{reportSID, resumeCtrlNode}, {siblingID, resumeCtrlNode}, {otherSID, otherNode}} {
		if _, err := dev.sessions.OpenFromSigmaWithID(s.sid, resumeFabricIndex, resumeDeviceNode, s.peer, s.sid+0x100, nil, peerLossKeys(t)); err != nil {
			t.Fatal(err)
		}
		dev.br.sessionPeerAddrs.Store(s.sid, peerAddr)
	}
	path := im.ConcreteAttributePath{Endpoint: 0, Cluster: 0x001F, Attribute: 0x0000}
	subscribe := func(sid uint16, peer uint64) uint32 {
		t.Helper()
		sub, err := dev.subs.Subscribe(subscription.SubscribeArgs{
			FabricIndex: resumeFabricIndex, PeerNodeID: peer, SessionID: sid,
			MinIntervalFloor: 0, MaxIntervalCeiling: 3600, KeepSubscriptions: true,
			AttributePaths: []im.ConcreteAttributePath{path},
		})
		if err != nil {
			t.Fatal(err)
		}
		return sub.ID
	}
	reportSub := subscribe(reportSID, resumeCtrlNode)
	siblingSub := subscribe(siblingID, resumeCtrlNode)
	otherSub := subscribe(otherSID, otherNode)
	dev.br.routing.subTargets.Store(reportSub, subTarget{
		src: peerAddr, sessionID: reportSID, fabricIndex: resumeFabricIndex, subjectNodeID: resumeCtrlNode,
	})

	sub, err := dev.subs.Get(reportSub)
	if err != nil {
		t.Fatal(err)
	}
	dev.br.reportSubscription(context.Background(), sub, []im.ConcreteAttributePath{path})
	time.Sleep(time.Millisecond)
	if _, err := dev.sessions.OpenFromSigmaWithID(laterSID, resumeFabricIndex, resumeDeviceNode, resumeCtrlNode, laterSID+0x100, nil, peerLossKeys(t)); err != nil {
		t.Fatal(err)
	}

	dev.br.mu.RLock()
	outbound := dev.br.outboundReliable
	dev.br.mu.RUnlock()
	if outbound == nil || outbound.Pending() == 0 {
		t.Fatal("the report is not tracked for retransmission")
	}
	now := time.Now()
	for i := 0; i <= mrp.MaxRetransmissions+1; i++ {
		dev.br.tickOutboundReliable(outbound, now.Add(time.Duration(i+1)*time.Hour))
	}

	for _, sid := range []uint16{reportSID, siblingID} {
		if _, err := dev.sessions.Get(sid); !errors.Is(err, operational.ErrSessionNotFound) {
			t.Errorf("session %#x after the loss: %v, want ErrSessionNotFound", sid, err)
		}
	}
	for _, sid := range []uint16{laterSID, otherSID} {
		if _, err := dev.sessions.Get(sid); err != nil {
			t.Errorf("session %#x must survive: %v", sid, err)
		}
	}
	for _, id := range []uint32{reportSub, siblingSub} {
		if _, err := dev.subs.Get(id); err == nil {
			t.Errorf("subscription %d of the lost peer still open", id)
		}
	}
	if _, err := dev.subs.Get(otherSub); err != nil {
		t.Errorf("another peer's subscription closed: %v", err)
	}
	if n := graceful.Load(); n != 0 {
		t.Errorf("CloseSession sent %d times to a lost peer, want 0", n)
	}
}

// TestReportPeerLoss_OnlyFirstMessageOfExchange: a counter that is not the
// first message of its report's exchange (a later chunk, sent after the
// peer's StatusResponse) or no longer owned by a report is not peer loss
// (matter.js #reportsPeerLoss, MessageExchange.ts:630-633).
func TestReportPeerLoss_OnlyFirstMessageOfExchange(t *testing.T) {
	t.Parallel()
	b := newStartedBridgeWithSnapshotter(t, wbEmptySnapshotter)
	key := mrp.ExchangeKey{SessionID: 7, ExchangeID: 9, Initiator: true}
	created := time.Now()
	b.reportExchangeOrigins.Store(key, reportExchangeOrigin{createdAt: created, firstCounter: 100})
	b.reportCounterOwner.Store(reportCounterKey(7, 100), uint32(1))
	b.reportCounterOwner.Store(reportCounterKey(7, 101), uint32(1))
	if asOf, lost := b.reportPeerLoss(7, 9, 100); !lost || !asOf.Equal(created) {
		t.Fatalf("first message: lost=%v asOf=%v, want true %v", lost, asOf, created)
	}
	if _, lost := b.reportPeerLoss(7, 9, 101); lost {
		t.Fatal("a later chunk counted as peer loss")
	}
	if _, lost := b.reportPeerLoss(7, 9, 102); lost {
		t.Fatal("an unowned counter counted as peer loss")
	}
	b.forgetReportExchange(1) // nothing remembered for sub 1: origin stays
	b.rememberReportExchange(1, 7, 9)
	b.rememberReportExchange(1, 7, 10)
	if _, ok := b.reportExchangeOrigins.Load(key); ok {
		t.Fatal("origin of a replaced report exchange kept")
	}
}

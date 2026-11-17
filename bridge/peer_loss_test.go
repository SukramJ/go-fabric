// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/internal/sessionseam"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/secure/operational"
	"github.com/SukramJ/go-fabric/secure/sigma"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// stampClock is the system clock whose reading a test steps forward. The
// peer-loss sweep keeps the peer's sessions created at or after the lost
// exchange's creation (operational.Manager.ClosePeerLost compares the
// session table's CreatedAt with the bridge's createdAt), so a test
// orders the two stamps with step instead of a wall-clock sleep; the
// bridge and the session table read the same clock, its timers stay the
// system's.
type stampClock struct {
	clock.System
	off atomic.Int64
}

func (c *stampClock) Now() time.Time { return time.Now().Add(time.Duration(c.off.Load())) }

// step moves the reading forward by d: every stamp after it is later than
// every stamp before it.
func (c *stampClock) step(d time.Duration) { c.off.Add(int64(d)) }

// onStampClock puts br and sessions on stamp; a configure callback of
// startResumeDevice calls it before Start.
func onStampClock(br *Bridge, sessions *operational.Manager, stamp *stampClock) {
	br.clock = stamp
	sessions.SetClock(stamp)
}

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
// a session to reuse. Mirrors matter.js MessageExchange.ts:643-651 →
// ExchangeManager.ts:621-644 → SessionManager.ts:686-688 at db021da0. Observed in
// TC-SU-2.2 step 2: the provider restarted under the same node id, the
// requestor kept invoking on the dead session and never reached it.
func TestInitiatedExchangePeerLoss_ForceClosesPeerSessions(t *testing.T) {
	t.Parallel()
	stamp := &stampClock{}
	dev := startResumeDevice(t, newMemSubscriptionStore(), func(br *Bridge, sessions *operational.Manager) {
		onStampClock(br, sessions, stamp)
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
	if _, err := sessionseam.OpenAt(dev.sessions, initiatorSID, true, resumeFabricIndex, resumeDeviceNode, resumeCtrlNode, 0x0B01, nil, peerLossKeys(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := sessionseam.OpenAt(dev.sessions, responderSID, false, resumeFabricIndex, resumeDeviceNode, resumeCtrlNode, 0x0B02, nil, peerLossKeys(t)); err != nil {
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
	stamp.step(time.Millisecond)
	if _, err := sessionseam.OpenAt(dev.sessions, laterSID, false, resumeFabricIndex, resumeDeviceNode, resumeCtrlNode, 0x0B03, nil, peerLossKeys(t)); err != nil {
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
	// The exchange itself ends with the exhaustion: a BDX transfer's
	// pending Receive fails now, not after the 5-minute idle timeout
	// (matter.js MessageExchange.send throws, MessageExchange.ts:604-624).
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := ex.Receive(ctx); !errors.Is(err, mrp.ErrMaxRetransmissionsReached) || !errors.Is(err, ErrExchangeClosed) {
		t.Errorf("Receive after the exhaustion: %v, want ErrExchangeClosed wrapping ErrMaxRetransmissionsReached", err)
	}
	if err := ex.Send(context.Background(), im.InteractionModelProtocolID, im.OpcodeInvokeRequest, nil); !errors.Is(err, mrp.ErrMaxRetransmissionsReached) {
		t.Errorf("Send after the exhaustion: %v, want ErrMaxRetransmissionsReached", err)
	}
}

// TestInitiatedPeerLoss_UnknownExchangeIsNoOp pins that the initiated-
// exchange path ignores a counter outside any exchange it opened; a
// subscription report's exhaustion is no peer loss
// (TestUndeliveredReportIsOneFailedUpdateParity).
func TestInitiatedPeerLoss_UnknownExchangeIsNoOp(t *testing.T) {
	t.Parallel()
	dev := startResumeDevice(t, newMemSubscriptionStore(), nil)
	if _, err := sessionseam.OpenAt(dev.sessions, 0x0C01, false, resumeFabricIndex, resumeDeviceNode, resumeCtrlNode, 0x0D01, nil, peerLossKeys(t)); err != nil {
		t.Fatal(err)
	}
	dev.br.handleInitiatedPeerLoss(0x0C01, 0x7777)
	if _, err := dev.sessions.Get(0x0C01); err != nil {
		t.Fatalf("session closed by a loss report of an unknown exchange: %v", err)
	}
}

// threeChunkReport is an ongoing report for subID that splits into three
// UDP chunks.
func threeChunkReport(t *testing.T, subID uint32) im.ReportData {
	t.Helper()
	report := im.ReportData{HasSubscription: true, SubscriptionID: subID}
	for i := range 3 {
		report.Reports = append(report.Reports, im.AttributeReport{
			Path:  im.ConcreteAttributePath{Endpoint: 0, Cluster: 0x0028, Attribute: uint32(0x10 + i), HasEndpoint: true, HasCluster: true, HasAttribute: true},
			Value: im.AttributeValue{Value: bytes.Repeat([]byte{byte('a' + i)}, 700)},
		})
	}
	chunks, err := chunkReportData(report, udpChunkLimits.budget, udpChunkLimits.hardCap)
	if err != nil || len(chunks) != 3 {
		t.Fatalf("report splits into %d chunks (%v), want 3", len(chunks), err)
	}
	return report
}

// chunkedReportPeer is a device with an unanswering peer of two sessions
// (one carrying a subscription), a session of another peer, and the
// ongoing report of that subscription about to be sent.
type chunkedReportPeer struct {
	dev        *resumeDevice
	outbound   *outboundReliableTracker
	target     subTarget
	reportSub  uint32
	reportSID  uint16
	siblingSID uint16
	otherSID   uint16
	graceful   *atomic.Int32
	stamp      *stampClock
}

func newChunkedReportPeer(t *testing.T, base uint16) *chunkedReportPeer {
	t.Helper()
	stamp := &stampClock{}
	dev := startResumeDevice(t, newMemSubscriptionStore(), func(br *Bridge, sessions *operational.Manager) {
		onStampClock(br, sessions, stamp)
		br.AttachAckTracker(mrp.NewAckTracker(0))
		// Long enough for the test to drive the first chunk to exhaustion
		// while the send still waits for its StatusResponse.
		br.chunkStatusResponseTimeoutOverride = 2 * time.Second
	})
	p := &chunkedReportPeer{dev: dev, stamp: stamp, graceful: &atomic.Int32{}, reportSID: base, siblingSID: base + 1, otherSID: base + 2}
	dev.sessions.SetGracefulCloseNotifier(func(uint16, *channel.Session) { p.graceful.Add(1) })
	silent, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = silent.Close() })
	peerAddr, ok := silent.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatalf("peer address %T", silent.LocalAddr())
	}
	for _, s := range []struct {
		sid  uint16
		peer uint64
	}{{p.reportSID, resumeCtrlNode}, {p.siblingSID, resumeCtrlNode}, {p.otherSID, 0xD1D1D1D1}} {
		if _, err := sessionseam.OpenAt(dev.sessions, s.sid, false, resumeFabricIndex, resumeDeviceNode, s.peer, s.sid+0x100, nil, peerLossKeys(t)); err != nil {
			t.Fatal(err)
		}
		dev.br.sessionPeerAddrs.Store(s.sid, peerAddr)
	}
	sub, err := dev.subs.Subscribe(subscription.SubscribeArgs{
		FabricIndex: resumeFabricIndex, PeerNodeID: resumeCtrlNode, SessionID: p.reportSID,
		MinIntervalFloor: 0, MaxIntervalCeiling: 3600, KeepSubscriptions: true,
		AttributePaths: []im.ConcreteAttributePath{{Endpoint: 0, Cluster: 0x001F, Attribute: 0x0000}},
	})
	if err != nil {
		t.Fatal(err)
	}
	p.reportSub = sub.ID
	p.target = subTarget{src: peerAddr, sessionID: p.reportSID, fabricIndex: resumeFabricIndex, subjectNodeID: resumeCtrlNode}
	dev.br.routing.subTargets.Store(sub.ID, p.target)
	dev.br.mu.RLock()
	p.outbound = dev.br.outboundReliable
	dev.br.mu.RUnlock()
	if p.outbound == nil {
		t.Fatal("no outbound-reliable tracker")
	}
	return p
}

// send starts the report and returns where its result lands.
func (p *chunkedReportPeer) send(t *testing.T) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	report := threeChunkReport(t, p.reportSub)
	go func() {
		_, _, err := p.dev.br.sendInitiatedReport(p.target, report)
		done <- err
	}()
	return done
}

// awaitPending waits until n messages are tracked for retransmission.
func (p *chunkedReportPeer) awaitPending(t *testing.T, n int) {
	t.Helper()
	waitFor(t, fmt.Sprintf("%d messages tracked for retransmission", n), func() bool { return p.outbound.Pending() == n })
}

// exhaust drives every tracked message to its last retransmission.
func (p *chunkedReportPeer) exhaust() {
	now := time.Now()
	for i := 0; i <= mrp.MaxRetransmissions+1; i++ {
		p.dev.br.tickOutboundReliable(p.outbound, now.Add(time.Duration(i+1)*time.Hour))
	}
}

// firstChunk is the report's exchange and the counter of its first chunk,
// tracked for retransmission: the exchange from the armed wait for the
// peer's StatusResponse, the counter from the tracker.
func (p *chunkedReportPeer) firstChunk(t *testing.T) (key mrp.ExchangeKey, counter uint32) {
	t.Helper()
	waitFor(t, "the wait for the first chunk's StatusResponse armed", func() bool {
		found := false
		p.dev.br.routing.statusResponseWaits.Range(func(k, _ any) bool {
			key, _ = k.(mrp.ExchangeKey)
			found = key.SessionID == p.reportSID
			return !found
		})
		return found
	})
	p.outbound.mu.Lock()
	defer p.outbound.mu.Unlock()
	for k := range p.outbound.pending {
		if k.sessionID == p.reportSID {
			return key, k.counter
		}
	}
	t.Fatal("the first chunk is not tracked for retransmission")
	return key, 0
}

// TestChunkedReportFirstChunkUnacknowledgedIsOneFailedUpdate: the first
// chunk of a three-chunk ongoing report is never acknowledged while the
// send waits for its StatusResponse. The send fails with that wait, the
// reporter counts one failed update, and nothing else ends: report
// exchanges suppress peer loss (matter.js
// packages/node/src/node/server/InteractionServer.ts:96-97 at db021da0),
// so the subscription and every session of the peer stay, and no
// CloseSession is sent. The other two chunks are never sent.
func TestChunkedReportFirstChunkUnacknowledgedIsOneFailedUpdate(t *testing.T) {
	t.Parallel()
	p := newChunkedReportPeer(t, 0x1201)
	done := p.send(t)
	p.awaitPending(t, 1)
	p.firstChunk(t)

	p.exhaust()

	for _, sid := range []uint16{p.reportSID, p.siblingSID, p.otherSID} {
		if _, err := p.dev.sessions.Get(sid); err != nil {
			t.Errorf("session %#x closed by the first chunk's loss: %v", sid, err)
		}
	}
	if n := p.graceful.Load(); n != 0 {
		t.Errorf("CloseSession sent %d times, want 0", n)
	}
	err := <-done
	if !errors.Is(err, errChunkUnanswered) {
		t.Fatalf("send = %v, want the StatusResponse wait's timeout", err)
	}
	p.dev.br.noteSubscriptionSendError(p.reportSub, "report", err)
	if raw, _ := p.dev.br.subSendErrorCount.Load(p.reportSub); raw != 1 {
		t.Errorf("failed updates counted: %v, want 1", raw)
	}
	if _, err := p.dev.subs.Get(p.reportSub); err != nil {
		t.Errorf("subscription closed by one failed update: %v", err)
	}
	if n := p.outbound.Pending(); n != 0 {
		t.Errorf("%d later chunks sent after the first went unanswered", n)
	}
}

// TestChunkedReportLaterChunkUnacknowledged: the first chunk is
// acknowledged and answered, the second never acknowledged. Its
// exhaustion closes neither the subscription nor any session, and the
// send ends with the StatusResponse wait's timeout, which the reporter
// counts as one failed update (noteSubscriptionSendError).
func TestChunkedReportLaterChunkUnacknowledged(t *testing.T) {
	t.Parallel()
	p := newChunkedReportPeer(t, 0x1301)
	done := p.send(t)
	p.awaitPending(t, 1)
	key, first := p.firstChunk(t)
	p.outbound.Ack(p.reportSID, first)
	p.dev.br.releaseReportCounter(p.reportSID, first)
	p.dev.br.signalStatusResponseRX(key.SessionID, key.ExchangeID, true, im.StatusSuccess)
	p.awaitPending(t, 1) // the second chunk

	p.exhaust()

	for _, sid := range []uint16{p.reportSID, p.siblingSID, p.otherSID} {
		if _, err := p.dev.sessions.Get(sid); err != nil {
			t.Errorf("session %#x closed by a later chunk's loss: %v", sid, err)
		}
	}
	if _, err := p.dev.subs.Get(p.reportSub); err != nil {
		t.Errorf("subscription closed by a later chunk's loss: %v", err)
	}
	if err := <-done; !errors.Is(err, errChunkUnanswered) {
		t.Fatalf("send = %v, want the StatusResponse wait's timeout", err)
	}
}

// lostPeerLookup holds one session, sid, with peer node and fabric.
type lostPeerLookup struct {
	sid    uint16
	sess   *channel.Session
	fabric uint8
}

func (l lostPeerLookup) Lookup(id uint16) (*channel.Session, bool) { return l.sess, id == l.sid }

func (l lostPeerLookup) FabricFor(id uint16) (uint8, bool) { return l.fabric, id == l.sid }

// lostPeerRegistry records how a peer loss closes sessions.
type lostPeerRegistry struct {
	closed  []uint16
	perPeer [][2]uint64
}

func (r *lostPeerRegistry) Close(id uint16) error { r.closed = append(r.closed, id); return nil }

func (r *lostPeerRegistry) CloseAllGraceful(time.Time) int { return 0 }

func (r *lostPeerRegistry) ClosePeerLostSince(fabricIndex uint8, peerNodeID uint64, _ time.Duration) int {
	r.perPeer = append(r.perPeer, [2]uint64{uint64(fabricIndex), peerNodeID})
	return 1
}

// TestPeerLossWithoutAFabricClosesOnlyItsSession holds the choice of a
// peer loss's reach (R2-P6-fsm-0820): a session with a fabric and a peer
// node id closes every session of that peer; a session without either
// (PASE, or an unknown peer) closes itself alone — a zero fabric or node
// id names no peer.
func TestPeerLossWithoutAFabricClosesOnlyItsSession(t *testing.T) {
	t.Parallel()
	const sid = uint16(0x0A10)
	for _, c := range []struct {
		name    string
		fabric  uint8
		node    uint64
		perPeer bool
	}{
		{"commissioned peer", 1, 0x1234, true},
		{"no fabric", 0, 0x1234, false},
		{"no node id", 1, 0, false},
		{"neither", 0, 0, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			key := make([]byte, 16)
			sess, err := channel.New(channel.Config{EncryptKey: key, DecryptKey: key, PeerNodeID: c.node, InitialCounter: 1})
			if err != nil {
				t.Fatal(err)
			}
			reg := &lostPeerRegistry{}
			b := &Bridge{}
			b.sessions = lostPeerLookup{sid: sid, sess: sess, fabric: c.fabric}
			b.sessionRegistry = reg
			b.closeLostPeer(sid, 1, 0, "a test exchange")
			if c.perPeer {
				if len(reg.perPeer) != 1 || reg.perPeer[0] != [2]uint64{uint64(c.fabric), c.node} || len(reg.closed) != 0 {
					t.Fatalf("per peer %v, closed %v; want one per-peer close of (%d, %#x) only", reg.perPeer, reg.closed, c.fabric, c.node)
				}
				return
			}
			if len(reg.perPeer) != 0 || len(reg.closed) != 1 || reg.closed[0] != sid {
				t.Fatalf("per peer %v, closed %v; want session %#x closed alone", reg.perPeer, reg.closed, sid)
			}
		})
	}
}

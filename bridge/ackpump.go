// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"slices"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/contract"
	endpointpkg "github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// ackPumpInterval is how often the pump goroutine polls the
// AckTracker for due obligations. Roughly one quarter of
// [mrp.DefaultStandaloneAckDelay] so a max-delayed obligation fires
// no more than ~50 ms past its deadline. Lower bound on the deadline
// is the receive-loop's handler latency (sub-millisecond), so this
// is the operationally-relevant precision.
const ackPumpInterval = 50 * time.Millisecond

// pumpStopGrace is the bounded budget [Bridge.Stop] hands to the ACK
// pump goroutine to unwind once its context is cancelled. The pump
// blocks at most one [ackPumpInterval] inside its select; we add a
// generous safety margin for race-detector overhead. Independent of
// the caller-supplied ctx so a slow serve-loop teardown doesn't
// starve the pump's exit path.
const pumpStopGrace = 500 * time.Millisecond

// AttachAckTracker wires the [mrp.AckTracker] in two roles at once:
//
//  1. The Discharge half (peer ACKs of our outbound messages) goes
//     through [AckHandler] just like before — the adapter is
//     constructed and registered transparently.
//  2. The Owe + Pump half (synthesising StandaloneAck datagrams for
//     un-piggybacked obligations) gets direct access to the tracker.
//     A pump goroutine spawns from [Bridge.Start] when this method
//     has been called; without it the bridge falls back to noop ACK
//     handling (useful for tests that do not need ACK emission).
//
// Calling AttachAckTracker AFTER [Bridge.Start] is allowed, but the
// pump goroutine starts only at the next Start (or never if the
// bridge is already started — the daemon is expected to wire the
// tracker before Start). For tests that wire post-Start, schedule
// pump exercises via direct [Bridge.RunAckPumpOnce] calls instead.
func (b *Bridge) AttachAckTracker(t *mrp.AckTracker) {
	b.AttachAckHandler(NewMRPAckAdapter(t))
	b.mu.Lock()
	b.ackTracker = t
	if b.outboundReliable == nil {
		b.outboundReliable = newOutboundReliableTracker(b.outboundBaseInterval, b.clk())
		b.outboundReliable.wake = b.wakeAckPump
		b.outboundReliable.fixedBackoff.Store(b.icd.fixedBackoff.Load())
	}
	b.mu.Unlock()
}

// outboundBaseInterval resolves the peer-appropriate MRP base
// interval for an outbound retransmission on sessionID. Delegates to
// the session lookup's optional
// [SessionRetransmitIntervalResolver] capability; a lookup without
// the capability or an unknown session returns 0 and the tracker falls
// back to the spec idle default. A responder's handshake reply on session
// 0 does not come here: it carries the initiator's own timing
// ([outboundReliableTracker.trackUnsecured]); the bridge's own handshakes
// as initiator stay on the idle default, as matter.js assumes an idle
// peer for them (UnsecuredSession.ts:30).
func (b *Bridge) outboundBaseInterval(sessionID uint16, now time.Time) time.Duration {
	b.mu.RLock()
	sessions := b.sessions
	b.mu.RUnlock()
	if r, ok := sessions.(SessionRetransmitIntervalResolver); ok {
		if d, ok := r.RetransmitBaseInterval(sessionID, now); ok && d > 0 {
			return d
		}
	}
	return 0
}

// exchangeReplyTarget captures the per-exchange routing state the
// ack pump needs to ship a StandaloneAck back to a peer. Beyond the
// UDP address, we remember the peer's SourceNodeID so the synthesised
// reply can echo it as DestNodeID — chip-tool's commissioner sets
// HasSourceNodeID on inbound PASE traffic and rejects unsecured
// replies that don't echo the value (Matter §4.4.1.2). Same constraint
// as [Bridge.sendReply].
type exchangeReplyTarget struct {
	src                 net.Addr
	hasPeerSourceNodeID bool
	peerSourceNodeID    uint64
	// hasPeerDestNodeID / peerDestNodeID is the node id the peer
	// addressed us by on an unsecured message — the responder's
	// messages of a CASE the bridge initiated carry only a destination
	// (bridge/case_initiator.go). An ack to such a message names that
	// id as our source: an unsecured message carries exactly one of
	// the two node ids (matter.js MessageCodec.ts:234-239 at db021da0,
	// "The CHIP SDK drops these too").
	hasPeerDestNodeID bool
	peerDestNodeID    uint64
	// sessionID is the SessionID of the inbound message that owed the
	// ACK. 0 means "unsecured" (PASE pre-fabric); non-zero means the
	// synthesised StandaloneAck must encrypt under that session.
	sessionID uint16
	// owedCounter is the message counter of the latest obligation
	// recorded with this route.
	owedCounter uint32
	// displaced reports that an obligation of the exchange was displaced
	// by a newer one ([mrp.AckTracker.Owe]) and waits for the pump's next
	// pass, which routes its standalone ack by this entry: a piggyback of
	// the newer counter must not release the route. The pump clears it
	// when it takes the displaced obligation ([Bridge.emitDueAcks]).
	displaced bool
}

// owedInboundAck registers an obligation for an inbound Reliable
// message. Caller is the receive pipeline; src plus the peer's
// SourceNodeID (when set) are captured per exchange-id so the pump
// can route the synthesised StandaloneAck back to the right peer with
// a header chip-tool's commissioner accepts.
//
// No-op when the bridge has no tracker wired — the receive pipeline
// then degrades to "no MRP". Peers retransmit until their max-retry
// cap, which is fine for unit tests but burns bandwidth in
// production; daemons should always wire a tracker.
func (b *Bridge) owedInboundAck(src net.Addr, hdr *message.Header, proto message.ProtocolHeader) {
	// Over a reliable connection the R flag is ignored and no
	// acknowledgement is owed (matter.js MessageExchange.ts:448-452;
	// transport/mrp UsesMRP).
	if !routeUsesMRP(src) {
		return
	}
	b.mu.RLock()
	tracker := b.ackTracker
	b.mu.RUnlock()
	if tracker == nil {
		return
	}
	// The route and the obligation are recorded in one step, so a release
	// of the route sees both or neither (exchangeRouting.ackRouteMu).
	b.routing.ackRouteMu.Lock()
	if !isNilRoute(src) {
		// Keyed on (session, exchange, our role): exchange IDs are only
		// unique per session AND per side, so two controllers sharing an
		// exchange ID — or a peer-opened exchange colliding with a
		// bridge-opened one — must not overwrite each other's reply
		// route. Mirrors matter.js ExchangeManager.ts:340 (session-scoped
		// resolution) and :138 (role folded into the exchange key).
		key := mrp.ExchangeKey{SessionID: hdr.SessionID, ExchangeID: proto.ExchangeID, Initiator: !proto.Initiator}
		// An obligation still pending for another counter is displaced by
		// this one and keeps needing the route until the pump took it.
		displaced := false
		if prev, ok := b.replyRoute(key); ok {
			displaced = prev.displaced ||
				(prev.owedCounter != hdr.MessageCounter && tracker.Owes(hdr.SessionID, proto.ExchangeID, !proto.Initiator))
		}
		b.routing.exchangeSrcs.Store(key, exchangeReplyTarget{
			src:                 src,
			hasPeerSourceNodeID: hdr.HasSourceNodeID,
			peerSourceNodeID:    hdr.SourceNodeID,
			hasPeerDestNodeID:   hdr.DestSize == message.DestNodeID,
			peerDestNodeID:      hdr.DestNodeID,
			sessionID:           hdr.SessionID,
			owedCounter:         hdr.MessageCounter,
			displaced:           displaced,
		})
	}
	// [mrp.AckObligation.Initiator] records OUR role on the exchange,
	// which is the inverse of the inbound message's flag: a peer-opened
	// exchange (proto.Initiator=true) makes us the responder, and a
	// message arriving with Initiator=false is the peer responding on an
	// exchange we opened — the ongoing-subscription case, where the
	// controller's IM StatusResponse answers a report we initiated. The
	// pump stamps the flag verbatim, and a StandaloneAck that does not
	// invert the peer's flag is discarded as unsolicited (chip
	// src/messaging/ExchangeContext.cpp:384, matter.js
	// packages/protocol/src/protocol/ExchangeManager.ts:324), leaving
	// the peer to retransmit until its cap fires.
	tracker.Owe(hdr.MessageCounter, hdr.SessionID, proto.ExchangeID, !proto.Initiator, b.clk().Now())
	b.routing.ackRouteMu.Unlock()
	b.wakeAckPump()
}

// replyRoute returns the reply route recorded for key.
func (b *Bridge) replyRoute(key mrp.ExchangeKey) (exchangeReplyTarget, bool) {
	raw, ok := b.routing.exchangeSrcs.Load(key)
	if !ok {
		return exchangeReplyTarget{}, false
	}
	target, ok := raw.(exchangeReplyTarget)
	return target, ok
}

// releaseReplyRouteLocked drops the reply route of an exchange once
// nothing needs it: no obligation is pending on the exchange and none that
// a newer one displaced waits for the pump. The caller holds
// exchangeRouting.ackRouteMu, so an arriving obligation is either seen
// whole or stores its route afterwards.
func (b *Bridge) releaseReplyRouteLocked(tracker *mrp.AckTracker, key mrp.ExchangeKey) {
	if tracker.Owes(key.SessionID, key.ExchangeID, key.Initiator) {
		return
	}
	if target, ok := b.replyRoute(key); ok && target.displaced {
		return
	}
	b.routing.exchangeSrcs.Delete(key)
}

// refreshAckCounter rewrites requestHdr.MessageCounter to the latest
// pending inbound ack-counter for exchangeID, so a subsequent
// sendReplyReliable's `AckCounter: requestHdr.MessageCounter`
// piggybacks the most-recent peer-sent counter rather than the
// stale original-request counter. Atomically discharges the
// obligation so the ack pump does not also emit a StandaloneAck for
// the same counter — the chunk we are about to send carries it.
//
// Needed on every mid-stream chunk in a multi-chunk ReportData burst
// (Subscribe-Initial AND chunked Read-Response): after the peer
// IM:StatusResponse-acks chunk N, chunk N+1 must piggyback that
// StatusResponse counter, otherwise python-matter-server's
// ReliableMessaging.cpp drops the chunk with "Dropping message
// without piggyback ack when we are waiting for an ack" and the
// commissioning interview times out after 5 retries.
//
// Mirrors the SubscribeResponse fix in subscribe.go's send-response
// branch but generalises it for every chunk in the burst. No-op when
// the tracker is not wired (test paths) or when no obligation exists
// (e.g. before the peer has acked any chunk yet).
func (b *Bridge) refreshAckCounter(requestHdr *message.Header, exchangeID uint16, initiator bool) {
	if counter, ok := b.takeOwedAck(requestHdr.SessionID, exchangeID, initiator); ok {
		requestHdr.MessageCounter = counter
	}
}

// takeOwedAck returns the ack counter the bridge owes on the (session,
// exchange, role) triple and discharges the obligation, for a message
// that is about to piggyback it ([mrp.AckTracker.LookupAndDischarge]).
// The exchange's reply route goes with the obligation — it exists for
// the pump's standalone ack and nothing is owed any more — unless a
// newer obligation arrived in between, or an older one this one displaced
// still waits for the pump: both are routed by it
// ([Bridge.releaseReplyRouteLocked]). A later reliable message on the
// exchange stores the route again ([Bridge.owedInboundAck]). (0, false)
// when nothing is owed or no tracker is wired.
//
// Before, only [Bridge.dischargeOwedAck] and the pump released the
// route: every path that took the ack here left it behind until its
// session was evicted (review II, W1-conc-08).
func (b *Bridge) takeOwedAck(sessionID, exchangeID uint16, initiator bool) (uint32, bool) {
	b.mu.RLock()
	tracker := b.ackTracker
	b.mu.RUnlock()
	if tracker == nil {
		return 0, false
	}
	b.routing.ackRouteMu.Lock()
	defer b.routing.ackRouteMu.Unlock()
	counter, ok := tracker.LookupAndDischarge(sessionID, exchangeID, initiator)
	b.releaseReplyRouteLocked(tracker, mrp.ExchangeKey{SessionID: sessionID, ExchangeID: exchangeID, Initiator: initiator})
	return counter, ok
}

// dischargeOwedAck cancels an obligation because the bridge just
// piggybacked an ACK on an outbound reply for the same (session,
// exchange). Caller invokes this from the IM / SC handler paths
// immediately after [Bridge.sendReply] succeeds.
//
// Safe to call when no obligation exists for the pair — the tracker
// silently no-ops.
func (b *Bridge) dischargeOwedAck(sessionID, exchangeID uint16, initiator bool) {
	b.mu.RLock()
	tracker := b.ackTracker
	b.mu.RUnlock()
	if tracker == nil {
		return
	}
	b.routing.ackRouteMu.Lock()
	defer b.routing.ackRouteMu.Unlock()
	tracker.Discharge(sessionID, exchangeID, initiator)
	b.releaseReplyRouteLocked(tracker, mrp.ExchangeKey{SessionID: sessionID, ExchangeID: exchangeID, Initiator: initiator})
}

// expediteDuplicateAck rewrites the just-registered obligation for a
// duplicate to due-now so the caller's immediate pump pass emits the
// StandaloneAck without waiting out the piggyback grace window. No-op
// when no tracker is wired or no obligation exists.
func (b *Bridge) expediteDuplicateAck(sessionID, exchangeID uint16, initiator bool) {
	b.mu.RLock()
	tracker := b.ackTracker
	b.mu.RUnlock()
	if tracker == nil {
		return
	}
	tracker.ExpediteDue(sessionID, exchangeID, initiator)
}

// armStatusResponseWait registers a per-exchange rendezvous channel
// the caller can <-receive on to block until the peer's next
// IM:StatusResponse arrives on this exchange (Matter §8.6.2). Used by
// the Subscribe-Initial chunk-streaming loop in [bridge/subscribe.go]
// to synchronise with Apple's ReadClient between chunks — see
// [exchangeRouting.statusResponseWaits] for the full rationale. The caller
// MUST [Bridge.disarmStatusResponseWait] the same exchange on the
// exit path so a delayed StatusResponse cannot panic on a closed
// channel.
//
// Returns a freshly-allocated channel that receives the carried
// status and is then closed exactly once by
// [Bridge.signalStatusResponseRX] when the StatusResponse lands; a
// receive that yields the zero value on a closed channel means the
// wait was disarmed without an answer. Keyed on (session, exchange) so
// a StatusResponse from another controller sharing the exchange ID
// cannot release this waiter.
func (b *Bridge) armStatusResponseWait(sessionID, exchangeID uint16, initiator bool) <-chan im.StatusCode {
	ch := make(chan im.StatusCode, 1)
	key := mrp.ExchangeKey{SessionID: sessionID, ExchangeID: exchangeID, Initiator: initiator}
	if prev, loaded := b.routing.statusResponseWaits.Swap(key, ch); loaded {
		// Pathological: caller armed a second waiter on the same
		// exchange without disarming the first. Close the orphan
		// channel so a goroutine blocked on it unblocks (interpreting
		// the close as "moved on"); the new wait wins.
		if prevCh, ok := prev.(chan im.StatusCode); ok {
			safeClose(prevCh)
		}
	}
	return ch
}

// disarmStatusResponseWait removes any pending rendezvous channel
// for the (session, exchange) pair. Safe to call when no wait is
// registered — no-op. Always paired with
// [Bridge.armStatusResponseWait] on the caller's exit path (success
// or timeout) so a late StatusResponse arrival cannot panic on a
// closed channel that a goroutine has already abandoned.
func (b *Bridge) disarmStatusResponseWait(sessionID, exchangeID uint16, initiator bool) {
	key := mrp.ExchangeKey{SessionID: sessionID, ExchangeID: exchangeID, Initiator: initiator}
	if v, loaded := b.routing.statusResponseWaits.LoadAndDelete(key); loaded {
		if ch, ok := v.(chan im.StatusCode); ok {
			safeClose(ch)
		}
	}
}

// signalStatusResponseRX is the IM-dispatcher hook into the wait
// machinery: when handleIMOpcode sees an inbound IM:StatusResponse
// for the (session, exchange) pair, it calls this so any goroutine
// inside the chunk-streaming loop unblocks and learns the status the
// peer answered with. Safe to call when no wait is registered — no-op.
// waited reports whether a waiter took the status: a chunk loop that
// sends the exchange's next message, which piggybacks the ack.
func (b *Bridge) signalStatusResponseRX(sessionID, exchangeID uint16, initiator bool, status im.StatusCode) (waited bool) {
	key := mrp.ExchangeKey{SessionID: sessionID, ExchangeID: exchangeID, Initiator: initiator}
	if v, loaded := b.routing.statusResponseWaits.LoadAndDelete(key); loaded {
		if ch, ok := v.(chan im.StatusCode); ok {
			safeSendAndClose(ch, status)
			return true
		}
	}
	return false
}

// safeSendAndClose hands status to the waiter and closes the channel,
// recovering from "send on closed channel" / "close of closed channel"
// panics. Belt-and-suspenders for the rendezvous channel lifecycle
// when arm/disarm/signal races.
func safeSendAndClose(ch chan im.StatusCode, status im.StatusCode) {
	defer func() { _ = recover() }()
	select {
	case ch <- status:
	default:
	}
	close(ch)
}

// safeClose closes a channel and recovers from "close of closed
// channel" / "close of nil channel" panics.
func safeClose(ch chan im.StatusCode) {
	defer func() { _ = recover() }()
	close(ch)
}

// runAckPump is the goroutine that fires StandaloneAck datagrams for
// obligations that have not been piggybacked within the grace
// window AND retransmits reliable outbound messages whose
// MessageCounter has not been ACKed by the peer. Stops when ctx is
// cancelled.
func (b *Bridge) runAckPump(ctx context.Context) {
	b.mu.RLock()
	tracker := b.ackTracker
	outbound := b.outboundReliable
	b.mu.RUnlock()
	if tracker == nil {
		return
	}
	p := &ackPumpTicker{ticker: b.clk().NewTicker(ackPumpInterval)}
	b.ackPump.Store(p)
	defer func() {
		b.ackPump.CompareAndSwap(p, nil)
		p.mu.Lock()
		p.ticker.Stop()
		p.mu.Unlock()
	}()
	for {
		p.mu.Lock()
		if !p.idle && b.ackPumpIdle(tracker, outbound) {
			// Nothing to acknowledge, retransmit or sweep: the pump stops
			// ticking until work arrives, so an idle node does not wake
			// every ackPumpInterval and a fake clock moves to its next real
			// deadline rather than in ackPumpInterval steps (brief FP2 §2:
			// a test site waiting a subscription's max interval stepped
			// 50 ms at a time). matter.js arms a timer per exchange instead
			// of polling — the ack receipt timer and the retransmission
			// timer (packages/protocol/src/protocol/MessageExchange.ts:258,
			// :789 at db021da0) — so its idle exchange layer holds no timer.
			p.idle = true
			p.ticker.Stop()
		}
		p.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case now := <-p.ticker.C():
			b.ackPumpTick(tracker, outbound, now)
		}
	}
}

// ackPumpTicker is the running pump's ticker and whether it is idle, the
// ticker stopped. mu orders the pump's idle decision against a producer's
// wake.
type ackPumpTicker struct {
	mu     sync.Mutex
	idle   bool
	ticker clock.Ticker
}

// ackPumpIdle reports whether the pump has nothing to do: no
// acknowledgement owed, no reliable message awaiting its ack and no
// timed deadline left to sweep. The pump decides it under its ticker's
// mutex, and a producer records its work before it wakes the pump
// (wakeAckPump): a wake that races the decision waits for it and re-arms.
func (b *Bridge) ackPumpIdle(tracker *mrp.AckTracker, outbound *outboundReliableTracker) bool {
	return tracker.Pending() == 0 && (outbound == nil || outbound.Pending() == 0) && !b.routing.hasTimedDeadlines()
}

// wakeAckPump re-arms an idle ACK pump's ticker in the producer's own
// call, not on the pump goroutine: the ticker's first tick is then
// ackPumpInterval after the work was recorded whatever the scheduler
// does, on the system clock as on a fake one — a pump goroutine
// scheduled late would otherwise let a fake site clock pass an owed
// acknowledgement for a later deadline before the ticker existed. It
// keeps the pump's tick rather than arming a timer at the earliest due
// time: the pump's cadence while it has work stays what it was, and a
// timer per deadline would need a next-due query of the AckTracker, the
// retransmission tracker and the timed table.
func (b *Bridge) wakeAckPump() {
	p := b.ackPump.Load()
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.idle {
		p.idle = false
		p.ticker.Reset(ackPumpInterval)
	}
}

// ackPumpTick performs the work of one pump iteration and reports how many
// StandaloneAck datagrams it emitted. The trackers are parameters rather
// than fields read here so [Bridge.runAckPump] keeps resolving them once at
// pump start, while a caller that resolves them per call gets identical
// behaviour from the same code — the alternative, a second copy of this
// body for out-of-band callers, is exactly the copy that drifts.
func (b *Bridge) ackPumpTick(tracker *mrp.AckTracker, outbound *outboundReliableTracker, now time.Time) int {
	emitted := 0
	if tracker != nil {
		emitted = b.emitDueAcks(tracker, now)
	}
	if outbound != nil {
		b.tickOutboundReliable(outbound, now)
	}
	// A peer that abandons its timed interaction and then goes quiet never
	// reaches the registration-site sweep, so the pump owns the idle case.
	// Amortised to one Range per [timedSweepInterval], not one per tick.
	if n := b.routing.maybeSweepExpiredTimedDeadlines(now); n > 0 {
		b.logger.Debug("matter.im.timed.deadlines_reclaimed",
			slog.Int("entries", n))
	}
	return emitted
}

// tickOutboundReliable drives one Tick of the outbound-reliable
// retransmit tracker, sending due datagrams via the bridge's
// listener and logging abandoned entries. Listener races with Stop
// fall through silently — the next tick (if any) re-checks.
//
// When a counter hits [mrp.MaxRetransmissions] the tracker reports
// `mrp.ErrMaxRetransmissionsReached`; the bridge then closes the
// owning subscription so the peer-vanished route stops burning
// engine ticks, and treats the peer as lost when the message was the
// first of an exchange this node opened — an ongoing report
// ([Bridge.reportPeerLoss]) or an OTA / BDX exchange
// ([Bridge.handleInitiatedPeerLoss]).
func (b *Bridge) tickOutboundReliable(outbound *outboundReliableTracker, now time.Time) {
	b.mu.RLock()
	listener := b.listener
	b.mu.RUnlock()
	if listener == nil {
		return
	}
	results := outbound.Tick(now, func(dest net.Addr, datagram []byte) error {
		return sendVia(listener, dest, datagram)
	})
	var retransmitted []uint8 // the distinct fabrics of the re-sent sessions
	for _, r := range results {
		if r.Err == nil {
			// A retransmission sent.
			b.count(contract.MetricMRPRetransmits)
			if r.SessionID != 0 {
				fabricIndex := b.resolveSessionFabric(r.SessionID)
				if fabricIndex != 0 && fabricIndex != endpointpkg.FabricIndexUnresolvable && !slices.Contains(retransmitted, fabricIndex) {
					retransmitted = append(retransmitted, fabricIndex)
				}
			}
			continue
		}
		if errors.Is(r.Err, mrp.ErrMaxRetransmissionsReached) {
			// Read before the reap below forgets the report's exchange.
			sinceMono, lost := b.reportPeerLoss(r.SessionID, r.ExchangeID, r.Counter)
			b.closeSubscriptionByCounter(r.SessionID, r.Counter)
			if lost {
				b.closeLostPeer(r.SessionID, r.ExchangeID, sinceMono, "an ongoing subscription report")
			}
			b.handleInitiatedPeerLoss(r.SessionID, r.ExchangeID)
			continue
		}
		b.logger.Debug("matter.tx.reliable.retransmit",
			slog.Int("counter", int(r.Counter)),
			slog.Any("err", r.Err))
	}
	if len(retransmitted) > 0 {
		b.retransmissionBroadcast(retransmitted)
	}
}

// retransmissionBroadcast is matter.js's "retransmit" broadcast event: each
// retransmission on a secure session with a fabric triggers one broadcast
// of that fabric's operational advertisement, unless it is broadcasting
// already (packages/protocol/src/protocol/DeviceAdvertiser.ts:164-172 at
// 862ad75f, MdnsAdvertiser.ts:234-242 at 862ad75f). fabrics are the
// distinct fabrics of one tick's re-sent sessions; each fabric's announced
// service takes the event once, a fabric without one is skipped. An
// advertiser without the event (mdns.Noop, a custom Advertiser) is left
// alone.
func (b *Bridge) retransmissionBroadcast(fabrics []uint8) {
	b.mu.RLock()
	advertiser := b.advertiser
	b.mu.RUnlock()
	ev, ok := advertiser.(serviceRetransmitter)
	if !ok {
		return
	}
	for _, fabricIndex := range fabrics {
		if instance, ok := b.operationalInstanceFor(fabricIndex); ok {
			ev.ServiceRetransmission(instance, mdns.ServiceTypeOperational)
		}
	}
}

// RunAckPumpOnce is the test-side entry point — drains the tracker's
// Due(now) once and emits a StandaloneAck for each, returning the
// number emitted. Production code uses [Bridge.runAckPump] instead.
func (b *Bridge) RunAckPumpOnce(now time.Time) int {
	b.mu.RLock()
	tracker := b.ackTracker
	b.mu.RUnlock()
	if tracker == nil {
		return 0
	}
	return b.emitDueAcks(tracker, now)
}

// emitDueAcks is one pass of the pump over the tracker: a standalone ack
// for every obligation that is due, the displaced ones included. The
// obligations and the routes they go by are taken in one step
// (exchangeRouting.ackRouteMu): a route goes with the pass unless a newer
// obligation of its exchange is still pending — that one is emitted over
// it later — and the displaced mark goes with the obligations it stood
// for. Two displaced obligations of one exchange in the same pass are
// both routed; before, the first took the route with it.
func (b *Bridge) emitDueAcks(tracker *mrp.AckTracker, now time.Time) int {
	b.routing.ackRouteMu.Lock()
	due := tracker.Due(now)
	targets := make([]exchangeReplyTarget, len(due))
	routed := make([]bool, len(due))
	for i, obl := range due {
		key := mrp.ExchangeKey{SessionID: obl.SessionID, ExchangeID: obl.ExchangeID, Initiator: obl.Initiator}
		targets[i], routed[i] = b.replyRoute(key)
	}
	for i, obl := range due {
		if !routed[i] {
			continue
		}
		key := mrp.ExchangeKey{SessionID: obl.SessionID, ExchangeID: obl.ExchangeID, Initiator: obl.Initiator}
		if !tracker.Owes(obl.SessionID, obl.ExchangeID, obl.Initiator) {
			b.routing.exchangeSrcs.Delete(key)
			continue
		}
		if current, ok := b.replyRoute(key); ok && current.displaced {
			current.displaced = false
			b.routing.exchangeSrcs.Store(key, current)
		}
	}
	b.routing.ackRouteMu.Unlock()
	for i, obl := range due {
		if !routed[i] {
			b.logger.Debug("matter.tx.ack_pump.no_src",
				slog.Int("exchange_id", int(obl.ExchangeID)),
				slog.Int("session_id", int(obl.SessionID)))
			continue
		}
		b.emitStandaloneAck(obl, targets[i])
	}
	return len(due)
}

// emitStandaloneAck synthesises a StandaloneAck datagram for obl and
// ships it via the bridge's listener to target, the reply route
// [Bridge.owedInboundAck] recorded for the exchange. An obligation
// without a route — added out-of-band (tests), or its session evicted —
// is logged and dropped by the caller.
func (b *Bridge) emitStandaloneAck(obl mrp.AckObligation, target exchangeReplyTarget) {
	if isNilRoute(target.src) {
		return
	}

	b.mu.RLock()
	listener := b.listener
	sessions := b.sessions
	b.mu.RUnlock()
	if listener == nil {
		return
	}

	proto := message.ProtocolHeader{
		Initiator:  obl.Initiator,
		HasAck:     true,
		AckCounter: obl.AckCounter,
		Opcode:     mrp.StandaloneAckOpcode,
		ExchangeID: obl.ExchangeID,
		ProtocolID: mrp.SecureChannelProtocolID,
	}
	hdr := message.Header{}
	// Echo the peer's SourceNodeID as our DestNodeID so chip-tool's
	// commissioner accepts the unsecured StandaloneAck (same rule as
	// [Bridge.sendReply], Matter §4.4.1.2). For peers that don't set
	// SourceNodeID the ack names the destination it addressed as our
	// source (exchangeReplyTarget). Skip on encrypted sessions —
	// chip-tool's secure-receive validator does not expect it there.
	if target.sessionID == 0 && target.hasPeerSourceNodeID {
		hdr.DestSize = message.DestNodeID
		hdr.DestNodeID = target.peerSourceNodeID
	}
	if target.sessionID == 0 && !target.hasPeerSourceNodeID && target.hasPeerDestNodeID {
		// The peer addressed us by node id and named no source: the ack
		// goes out as that node, so it carries exactly one node id.
		hdr.HasSourceNodeID = true
		hdr.SourceNodeID = target.peerDestNodeID
	}

	var datagram []byte
	if target.sessionID == 0 {
		// PASE pre-fabric — counter from the bridge's unsecured pool,
		// no encryption.
		hdr.SessionID = 0
		hdr.MessageCounter = b.nextUnsecuredCounter()
		datagram = append(hdr.Marshal(), proto.Marshal()...)
	} else {
		if sessions == nil {
			b.logger.Debug("matter.tx.ack_pump.no_sessions",
				slog.Int("exchange_id", int(obl.ExchangeID)),
				slog.Int("session_id", int(target.sessionID)))
			return
		}
		sess, ok := sessions.Lookup(target.sessionID)
		if !ok {
			// Session torn down between OWE and emit (rare; usually
			// only on fabric removal). Drop the synthesised ACK — the
			// peer either reaches its retransmit cap or notices the
			// session vanish via its own keepalive.
			b.logger.Debug("matter.tx.ack_pump.session_gone",
				slog.Int("exchange_id", int(obl.ExchangeID)),
				slog.Int("session_id", int(target.sessionID)))
			return
		}
		// Stamp the peer's view of the SessionID so their inbound
		// dispatcher resolves the right session — same rule as
		// [Bridge.sendReply] / [Bridge.sendUnsolicitedIM].
		if peerID := sess.PeerSessionID(); peerID != 0 {
			hdr.SessionID = peerID
		} else {
			hdr.SessionID = target.sessionID
		}
		body := proto.Marshal()
		enc, err := b.encryptSecureOutbound(sess, target.sessionID, &hdr, body)
		if err != nil {
			b.logger.Warn("matter.tx.ack_pump.encrypt",
				slog.Int("exchange_id", int(obl.ExchangeID)),
				slog.Int("session_id", int(target.sessionID)),
				slog.Any("err", err))
			return
		}
		datagram = append(hdr.Marshal(), enc.Ciphertext...)
	}

	if err := sendVia(listener, target.src, datagram); err != nil {
		b.logger.Warn("matter.tx.ack_pump.send",
			slog.Int("exchange_id", int(obl.ExchangeID)),
			slog.String("src", target.src.String()),
			slog.Any("err", err))
		return
	}
	b.logger.Debug("matter.tx.ack_pump.emitted",
		slog.Int("exchange_id", int(obl.ExchangeID)),
		slog.Int("ack_counter", int(obl.AckCounter)),
		slog.Int("session_id", int(target.sessionID)))
}

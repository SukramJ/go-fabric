// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package exchange

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/internal/inflight"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// Message is one message received on an exchange.
type Message struct {
	// Counter is the message counter of the packet header.
	Counter uint32
	// ProtocolID and Opcode name the message.
	ProtocolID uint16
	Opcode     uint8
	// ExchangeID is the exchange the message belongs to.
	ExchangeID uint16
	// Initiator is the sender's I flag.
	Initiator bool
	// RequiresAck is the R flag; HasAck and AckCounter the piggybacked
	// acknowledgement.
	RequiresAck bool
	HasAck      bool
	AckCounter  uint32
	// Payload is the application payload.
	Payload []byte
}

func (m *Message) isStandaloneAck() bool {
	return m.ProtocolID == mrp.SecureChannelProtocolID && m.Opcode == mrp.StandaloneAckOpcode
}

func (m *Message) isStatusReport() bool {
	return m.ProtocolID == mrp.SecureChannelProtocolID && m.Opcode == mrp.SCOpcodeStatusReport
}

// Options configures [Manager.Initiate] — matter.js
// MessageExchange.Options (MessageExchange.ts:1239-1274).
type Options struct {
	// SuppressPeerLoss waives the peer-loss report for this exchange —
	// matter.js MessageExchange.Options.suppressPeerLoss
	// (MessageExchange.ts:1272-1273).
	SuppressPeerLoss bool

	// OnSend is called before every transmission of a message on the
	// exchange with its retransmission number: 0 for a first
	// transmission (a standalone ack included), n for the n-th
	// retransmission — matter.js MessageExchange.Options.onSend
	// (:1240-1243), called at :768 and :943. It runs on the sending
	// goroutine, for a first transmission with the exchange locked, so it
	// must not block for long or call back into the exchange.
	OnSend func(retransmission int)

	// AddressOverride sends the exchange's messages to this address instead
	// of the session's, leaving the session's address and its other
	// exchanges alone — matter.js MessageExchange.Options.addressOverride
	// (:1266-1270), stored at :297 and used for every transmission (:488,
	// :771-774, :953). Nil sends to the session's address.
	AddressOverride *net.UDPAddr

	// PeerAdditionalMrpDelay, when set, is the peer-medium MRP margin of
	// the exchange's sends, in place of the session's peer margins for the
	// exchange's protocol — matter.js MessageExchange.Config
	// peerAdditionalMrpDelay (MessageExchange.ts:1289-1295), which a
	// network profile supplies per interaction (PeerExchangeProvider.ts:89-91,
	// PeerConnection.ts:111-114). The margin applied is the larger of it
	// and the manager's [Config].LocalAdditionalMrpDelay (:1216-1222).
	PeerAdditionalMrpDelay *time.Duration

	// OnReceive is called for every message the exchange takes, a
	// duplicate and a standalone ack included — matter.js
	// MessageExchange.Options.onReceive (:1245-1248), called at :469
	// after the protocol check.
	OnReceive func()

	// OnClosing is called once, when the exchange starts to close — a
	// Close, an Abort or its final close, whichever comes first. A network
	// profile's slot is released there: matter.js releases it on the
	// exchange's closing or closed event, whichever fires first
	// (PeerExchangeProvider.ts:136-141). It must not block.
	OnClosing func()
}

// SendOptions configures [Exchange.Send] — matter.js ExchangeSendOptions
// (MessageExchange.ts:82-132).
type SendOptions struct {
	// ExpectAckOnly makes a reply that carries data in place of a
	// standalone ack an [ErrUnexpectedMessage].
	ExpectAckOnly bool
	// ExpectedProcessingTime is the peer's processing time for the final
	// wait after the last retransmission; 0 is
	// [DefaultExpectedProcessingTime].
	ExpectedProcessingTime time.Duration
	// SuppressAck sends without requesting an acknowledgement (a
	// CloseSession or Busy StatusReport).
	SuppressAck bool
	// DisableMRP sends once, without retransmission and without waiting
	// for the acknowledgement.
	DisableMRP bool
	// MaxRetransmissions caps the transmissions; 0 is [MaxTransmissions],
	// a negative value is unlimited.
	MaxRetransmissions int
	// MaxRetransmissionTime caps each backoff, never below the peer's idle
	// interval; 0 is no cap.
	MaxRetransmissionTime time.Duration
	// InitialRetransmissionTime is added to every backoff.
	InitialRetransmissionTime time.Duration
	// SuppressPeerLoss waives the peer-loss report for this send.
	SuppressPeerLoss bool
	// AckWaitAbort, once closed, ends only the waits for the
	// acknowledgement and for retransmissions: the message is still
	// transmitted once, even when it closed before, and Send then returns
	// nil; the message stays unacknowledged for [Exchange.Close]. ctx —
	// matter.js's abort — still stops the transmission. Mirrors matter.js
	// ExchangeSendOptions.ackWaitAbort (MessageExchange.ts:125-130).
	AckWaitAbort <-chan struct{}
}

// ReceiveOptions configures [Exchange.Next] — matter.js
// ExchangeReceiveOptions (MessageExchange.ts:134-148).
type ReceiveOptions struct {
	// Timeout overrides the computed wait; 0 computes it from
	// [MaxPeerResponseTime].
	Timeout time.Duration
	// ExpectedProcessingTime enters the computed wait; 0 is
	// [DefaultExpectedProcessingTime].
	ExpectedProcessingTime time.Duration
	// SuppressPeerLoss waives the peer-loss report for this wait.
	SuppressPeerLoss bool
}

// pending is the reliable message an exchange waits to have acknowledged.
type pending struct {
	counter    uint32
	datagram   []byte
	hasAck     bool
	ackCounter uint32
	started    time.Time
	done       chan sendResult
	resolved   bool
}

type sendResult struct {
	msg *Message
	err error
}

// resolve completes the send that waits on p, once.
func (p *pending) resolve(msg *Message, err error) {
	if p.resolved {
		return
	}
	p.resolved = true
	p.done <- sendResult{msg: msg, err: err}
}

// Exchange is one exchange this node initiated. Send and Next are not
// meant for concurrent use with each other on one exchange (an exchange
// is one conversation), but every method is safe to call from any
// goroutine.
type Exchange struct {
	m                *Manager
	session          *Session
	id               uint16
	protocol         uint16
	suppressPeerLoss bool
	initiator        bool
	index            uint32
	done             chan struct{}

	// mu guards the exchange's state: every field below it except
	// addressOverride, which newExchange fixes and dest reads without it.
	// It covers the delivery queue, the acknowledgement and retransmission
	// state, the close state and the activity times.
	mu              sync.Mutex
	addressOverride *net.UDPAddr
	// peerAdditional is [Options].PeerAdditionalMrpDelay.
	peerAdditional *time.Duration
	// onClosing is [Options].OnClosing, run once by notifyClosing.
	onClosing       func()
	onClosingOnce   sync.Once
	onSend          func(retransmission int)
	onReceive       func()
	queue           []*Message
	signal          chan struct{}
	sentToAck       *pending
	receivedToAck   *Message
	ackTimer        Timer
	retransTimer    Timer
	retransInterval time.Duration
	retransArmed    bool     // the retransmission timer runs (no transmission in flight)
	ackWaitEnded    *pending // the pending message whose ack wait AckWaitAbort ended
	retransCount    int
	lastTransmitAt  time.Time // the last transmission of a non-ack message
	sendOpts        SendOptions
	used            bool
	closing         bool
	closed          bool
	closeCause      error
	closeTimer      Timer
	receivedCount   int
	lastActive      time.Time
	createdAt       time.Time
	// queuedCounted are the queued messages counted as delivered work
	// (Manager.inflight), held the ones the reader took and has not yet
	// come back from: its next Next, Send, Close or Abort ends their
	// count, as the exchange's end ends every count.
	queuedCounted int
	held          int
}

// countQueuedLocked counts a message queued for the reader as work in
// hand. The caller holds x.mu.
func (x *Exchange) countQueuedLocked() {
	if x.closed {
		return
	}
	x.queuedCounted++
	x.m.inflight.Enter(inflight.ExchangeDelivered)
}

// takeCountedLocked moves one queued message's count to the reader. The
// caller holds x.mu.
func (x *Exchange) takeCountedLocked() {
	if x.queuedCounted > 0 {
		x.queuedCounted--
		x.held++
	}
}

// releaseHeldLocked ends the count of the messages the reader took: it
// is back on the exchange. The caller holds x.mu.
func (x *Exchange) releaseHeldLocked() {
	if x.held > 0 {
		x.m.inflight.Add(inflight.ExchangeDelivered, -int64(x.held))
		x.held = 0
	}
}

// newExchange builds an exchange we initiate (initiator) or one the peer
// opened; index is its key in the manager's table.
func newExchange(m *Manager, s *Session, id, protocol uint16, initiator bool, opts Options) *Exchange {
	index := uint32(id)
	if initiator {
		index |= initiatedExchangeFlag
	}
	return &Exchange{
		m:         m,
		session:   s,
		id:        id,
		protocol:  protocol,
		initiator: initiator,
		index:     index,
		// An exchange the peer opened is in use from its first message
		// (MessageExchange.ts:303).
		used:             !initiator,
		suppressPeerLoss: opts.SuppressPeerLoss,
		addressOverride:  opts.AddressOverride,
		peerAdditional:   opts.PeerAdditionalMrpDelay,
		onClosing:        opts.OnClosing,
		onSend:           opts.OnSend,
		onReceive:        opts.OnReceive,
		done:             make(chan struct{}),
		signal:           make(chan struct{}, 1),
		lastActive:       m.clock.Now(),
		createdAt:        m.clock.Now(),
	}
}

// ID is the exchange id.
func (x *Exchange) ID() uint16 { return x.id }

// dest is where the exchange's messages go: the override, or the
// session's current address.
func (x *Exchange) dest() *net.UDPAddr {
	if x.addressOverride != nil {
		return x.addressOverride
	}
	return x.session.Addr()
}

// Session is the session the exchange runs on.
func (x *Exchange) Session() *Session { return x.session }

// Done is closed once the exchange is closed and left the manager.
func (x *Exchange) Done() <-chan struct{} { return x.done }

// HasUnackedMessage reports whether a message we sent still waits for its
// acknowledgement — matter.js MessageExchange.ts:415-422. The exchange
// cannot send again in that state, so a handshake skips its best-effort
// error report.
func (x *Exchange) HasUnackedMessage() bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.sentToAck != nil
}

// RetransmissionCount is the number of retransmissions of the message the
// exchange sent last, reset by a new send and by its acknowledgement —
// matter.js MessageExchange.ts:410-413 (retransmissionCount): the counter
// of :263, reset at :508 (ack), :611 (send) and :786 (first
// transmission), incremented at :886 and taken back at :907 when only the
// final wait remains.
func (x *Exchange) RetransmissionCount() int {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.retransCount
}

// RetransmissionRestartSaving is how much restarting the exchange — a
// fresh exchange whose first transmission waits the base backoff — would
// shorten the wait until the next (re)transmission: the interval of the
// retransmission timer armed last minus the backoff of transmission 0,
// never negative, and 0 before the exchange armed a timer. Mirrors
// matter.js MessageExchange.ts:1258-1266 (retransmissionRestartSaving),
// which reads the last timer's interval whether or not it still runs.
func (x *Exchange) RetransmissionRestartSaving() time.Duration {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.retransTimer == nil {
		return 0
	}
	return max(0, x.retransInterval-x.backoffLocked(0))
}

// considerClosed is matter.js MessageExchange.ts:376-378: closed, or —
// for an exchange we initiated — closing.
func (x *Exchange) considerClosed() bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.closed || (x.initiator && x.closing)
}

// ConsiderClosed reports whether the exchange counts as closed: closed,
// or — for an exchange we initiated — closing. It is matter.js
// MessageExchange.considerClosed (MessageExchange.ts:376-378), which a
// CASE responder reads before its error report (CaseServer.ts:82-86).
func (x *Exchange) ConsiderClosed() bool { return x.considerClosed() }

// takeFirst removes msg, the message that opened a peer-opened exchange,
// from the queue [Exchange.Next] reads: its handler receives it directly.
func (x *Exchange) takeFirst(msg *Message) {
	x.mu.Lock()
	defer x.mu.Unlock()
	for i, q := range x.queue {
		if q == msg {
			x.queue = append(x.queue[:i], x.queue[i+1:]...)
			x.takeCountedLocked()
			return
		}
	}
}

func (x *Exchange) lastActivity() time.Time {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.lastActive
}

// notifySentLocked records a transmission (MessageExchange.ts:385-388).
func (x *Exchange) notifySentLocked() {
	x.lastActive = x.m.clock.Now()
	x.session.notifyActivity(false)
}

// Send sends one message on the exchange and, when it travels reliably,
// returns once the peer acknowledged it. Mirrors matter.js
// MessageExchange.ts:605-820 (send, #send) at 0ceb202d:
//
//   - a closed exchange refuses (:606-608);
//   - the message asks for an acknowledgement unless SuppressAck
//     (:660); a StatusReport travels on the Secure Channel protocol
//     whatever the exchange's protocol (:662-665);
//   - a send while the previous message is unacknowledged is an
//     [ErrFlow] (:669-673);
//   - the acknowledgement owed for the last received message rides
//     along, and its standalone-ack timer stops (:679-686);
//   - the message is retransmitted on the MRP schedule until
//     acknowledged; after the last transmission and the final wait the
//     send fails with a [*PeerUnresponsiveError] (:752-766, :879-966);
//   - a failure with nothing received on the exchange that shows the
//     peer gone reports it to the manager's PeerLost hook (:613-638).
//
// A ctx already done sends nothing; a ctx done while waiting for the
// acknowledgement stops the retransmissions and leaves the message
// unacknowledged (:793-805). Both return ctx's error.
func (x *Exchange) Send(ctx context.Context, opcode uint8, payload []byte, opts SendOptions) error {
	x.mu.Lock()
	x.releaseHeldLocked()
	if x.closed {
		cause := x.closeCause
		x.mu.Unlock()
		return closedError(cause)
	}
	if err := ctx.Err(); err != nil {
		x.mu.Unlock()
		return err
	}
	x.sendOpts = opts
	x.retransCount = 0
	p, err := x.transmitLocked(opcode, payload, nil)
	if err == nil && p != nil && isClosed(opts.AckWaitAbort) {
		// The ack wait was ended before the transmission completed
		// (MessageExchange.ts:794-797).
		x.endAckWaitLocked(p)
		p = nil
	}
	x.mu.Unlock()
	if err == nil && p != nil {
		err = x.awaitAck(ctx, p, opts.ExpectAckOnly, opts.AckWaitAbort)
	}
	if err != nil && x.reportsPeerLoss(opts.SuppressPeerLoss) && (errors.Is(err, ErrTransient) || errors.Is(err, ErrNetwork)) {
		x.m.exchangePeerLost(x, err)
	}
	return err
}

// closedError is the error of a call on a closed exchange.
func closedError(cause error) error {
	if cause == nil {
		return ErrExchangeClosed
	}
	return fmt.Errorf("%w: %w", ErrExchangeClosed, cause)
}

// reportsPeerLoss is matter.js MessageExchange.ts:631-638: only an
// exchange that never heard from the peer is evidence the peer is gone.
func (x *Exchange) reportsPeerLoss(suppressed bool) bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	return !x.suppressPeerLoss && !x.session.suppressPeerLoss && !suppressed && x.receivedCount == 0
}

// transmitLocked frames and sends one message. ackFor is the counter a
// standalone ack acknowledges, nil for any other message. It returns the
// pending entry of a reliable message.
func (x *Exchange) transmitLocked(opcode uint8, payload []byte, ackFor *uint32) (*pending, error) {
	isAck := ackFor != nil
	// Without MRP nothing asks for an acknowledgement and no standalone
	// ack is sent (MessageExchange.ts:660, :666-668).
	usesMRP := x.session.UsesMRP()
	if isAck && !usesMRP {
		return nil, nil
	}
	requiresAck := !x.sendOpts.SuppressAck && !isAck && usesMRP
	protocol := x.protocol
	if isAck || opcode == mrp.SCOpcodeStatusReport {
		protocol = mrp.SecureChannelProtocolID
	}
	if x.sentToAck != nil && !isAck {
		return nil, fmt.Errorf("%w: the previous message %d is not acknowledged yet", ErrFlow, x.sentToAck.counter)
	}
	x.used = true
	x.notifySentLocked()
	proto := message.ProtocolHeader{
		Initiator:  x.initiator,
		NeedsAck:   requiresAck,
		Opcode:     opcode,
		ExchangeID: x.id,
		ProtocolID: protocol,
	}
	switch {
	case isAck:
		proto.HasAck, proto.AckCounter = true, *ackFor
	case x.receivedToAck != nil && usesMRP:
		proto.HasAck, proto.AckCounter = true, x.receivedToAck.Counter
		x.stopAckTimerLocked()
		x.receivedToAck = nil
	}
	datagram, counter, err := x.m.seal(x.session, proto, payload, 0)
	if err != nil {
		return nil, err
	}
	if x.onSend != nil {
		x.onSend(0)
	}
	var p *pending
	if requiresAck && !x.sendOpts.DisableMRP {
		p = &pending{
			counter:    counter,
			datagram:   datagram,
			hasAck:     proto.HasAck && !isAck,
			ackCounter: proto.AckCounter,
			started:    x.m.clock.Now(),
			done:       make(chan sendResult, 1),
		}
		x.sentToAck = p
	}
	if err := x.m.send(x.session, x.dest(), datagram); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNetwork, err)
	}
	if !isAck {
		x.lastTransmitAt = x.m.clock.Now() // MessageExchange.ts:799-801
	}
	if p != nil {
		x.retransCount = 0
		x.armRetransLocked(p, x.backoffLocked(0), x.expectedProcessingTimeLocked())
	}
	return p, nil
}

func (x *Exchange) expectedProcessingTimeLocked() time.Duration {
	if x.sendOpts.ExpectedProcessingTime > 0 {
		return x.sendOpts.ExpectedProcessingTime
	}
	return DefaultExpectedProcessingTime
}

// awaitAck waits for the pending message's outcome.
func (x *Exchange) awaitAck(ctx context.Context, p *pending, expectAckOnly bool, ackWait <-chan struct{}) error {
	select {
	case <-ackWait:
		// MessageExchange.ts:812-825: the wait ends, the retransmissions
		// stop, the send resolves.
		x.mu.Lock()
		x.endAckWaitLocked(p)
		x.mu.Unlock()
		return nil
	case r := <-p.done:
		if r.err != nil {
			return r.err
		}
		if expectAckOnly && r.msg != nil && !r.msg.isStandaloneAck() {
			return &UnexpectedMessageError{Message: r.msg}
		}
		return nil
	case <-ctx.Done():
		// The ack wait ends with the timer: a retransmission already past
		// its unlock does not re-arm after the caller gave up
		// (W4-ctlnet-03). Mirrors matter.js MessageExchange.ts:812-818
		// (#endAckWait, then the timer stopped; the message stays pending
		// for close()).
		x.mu.Lock()
		x.endAckWaitLocked(p)
		x.mu.Unlock()
		return ctx.Err()
	}
}

// endAckWaitLocked ends the ack wait of p: it stays pending for Close but
// is never transmitted again. Mirrors matter.js MessageExchange.ts:987-997
// (#endAckWait, #retransmits).
func (x *Exchange) endAckWaitLocked(p *pending) {
	x.ackWaitEnded = p
	x.stopRetransTimerLocked()
}

// retransmitsLocked reports whether p is still retransmitted: pending and
// its ack wait not ended (MessageExchange.ts:995-997).
func (x *Exchange) retransmitsLocked(p *pending) bool {
	return x.sentToAck == p && x.ackWaitEnded != p
}

// isClosed reports whether c is closed; a nil channel never is.
func isClosed(c <-chan struct{}) bool {
	select {
	case <-c:
		return true
	default:
		return false
	}
}

// bdxProtocolID is the Bulk Data Exchange protocol id (Core Spec §11.21).
const bdxProtocolID uint16 = 0x0002

// bdxMaxRetransmissionInterval is the retransmission interval that still
// fits every attempt inside the 30 s a BDX peer waits for the next BDX
// message, less 100 ms for the final attempt to arrive. Mirrors matter.js
// MessageExchange.ts:63-77 (BDX_MAX_RETRANSMISSION_INTERVAL).
const bdxMaxRetransmissionInterval = 30*time.Second/(MaxTransmissions-1) - 100*time.Millisecond

// sendPad is the pad of our sends: [Exchange.sendAdditionalDelay] and the
// manager's fixed backoff (MessageExchange.ts:1236-1240).
func (x *Exchange) sendPad() Pad {
	return Pad{Additional: x.sendAdditionalDelay(), Fixed: x.m.localFixed}
}

// sendAdditionalDelay is the amplified backoff addition of our sends: the
// larger of our own network's margin and the peer's for the exchange's
// traffic class — the exchange's PeerAdditionalMrpDelay, else the
// session's peer margins (matter.js MessageExchange.ts:1215-1222,
// #sendAdditionalDelay).
func (x *Exchange) sendAdditionalDelay() time.Duration {
	if x.peerAdditional != nil {
		return max(x.m.localAdditional, *x.peerAdditional)
	}
	peer, _ := MarginFor(x.session.peerMarginsNow(), x.protocol)
	return max(x.m.localAdditional, peer)
}

// backoffLocked is the scheduled wait after transmission n: the MRP
// interval for the peer's state, plus InitialRetransmissionTime, capped
// at max(MaxRetransmissionTime, peer idle interval), and on a BDX exchange
// at [bdxMaxRetransmissionInterval] even below the idle interval — matter.js
// MessageExchange.ts:1234-1251 (#backOffFor).
func (x *Exchange) backoffLocked(n int) time.Duration {
	iv, active := x.session.timing()
	d := RetransmissionIntervalPadded(n, iv, active, x.m.rand(), x.sendPad())
	// Saturating: d is MaxInt64 once the unbounded fallback's backoff
	// saturated (W4-ctlnet-01); matter.js MessageExchange.ts:1242-1250.
	d = satAdd(d, x.sendOpts.InitialRetransmissionTime)
	if x.sendOpts.MaxRetransmissionTime > 0 {
		d = min(d, max(x.sendOpts.MaxRetransmissionTime, iv.Idle))
	}
	if x.protocol == bdxProtocolID {
		d = min(d, bdxMaxRetransmissionInterval)
	}
	return d
}

func (x *Exchange) armRetransLocked(p *pending, d, expectedProcessingTime time.Duration) {
	x.retransInterval = d
	x.retransArmed = true
	x.retransTimer = x.m.clock.AfterFunc(d, func() { x.retransmit(p, expectedProcessingTime) })
}

func (x *Exchange) stopRetransTimerLocked() {
	x.retransArmed = false
	if x.retransTimer != nil {
		x.retransTimer.Stop()
	}
}

func (x *Exchange) stopAckTimerLocked() {
	if x.ackTimer != nil {
		x.ackTimer.Stop()
		x.ackTimer = nil
	}
}

// retransmit transmits the pending message again, or — once its
// transmissions are spent — waits a final MaxPeerResponseTime less the
// last backoff for the peer's own retransmissions, then fails the send.
// Mirrors matter.js MessageExchange.ts:879-978 (#retransmitMessage,
// #initializeResubmission). expectedProcessingTime 0 is the final wait's
// expiry. It reports whether it transmitted (MessageExchange.ts:893-897).
func (x *Exchange) retransmit(p *pending, expectedProcessingTime time.Duration) bool {
	x.mu.Lock()
	if !x.retransmitsLocked(p) {
		x.mu.Unlock()
		return false
	}
	x.retransArmed = false
	x.retransCount++
	limit := x.sendOpts.MaxRetransmissions
	if limit == 0 {
		limit = MaxTransmissions
	}
	// Mirrors matter.js MessageExchange.ts:907-910 with considerClosed
	// (:390-392): closed, or an initiator closing — a responder exchange
	// keeps retransmitting within its close wait (W4-ctlnet-06).
	closing := x.closed || (x.initiator && x.closing)
	if closing || (limit > 0 && x.retransCount >= limit) {
		if expectedProcessingTime > 0 && !closing {
			final := MaxPeerResponseTimePadded(nil, false, IntervalsOf(x.m.local), expectedProcessingTime, x.sendPad()) - x.retransInterval
			if final > 0 {
				x.retransCount--
				x.armRetransLocked(p, final, 0)
				x.mu.Unlock()
				return false
			}
		}
		x.receivedToAck = nil
		p.resolve(nil, &PeerUnresponsiveError{Waited: x.m.clock.Now().Sub(p.started)})
		closeNow := x.closeTimer != nil
		x.mu.Unlock()
		if closeNow {
			x.finalClose(nil)
		}
		return false
	}
	x.notifySentLocked()
	x.lastTransmitAt = x.m.clock.Now()
	backoff := x.backoffLocked(x.retransCount)
	count := x.retransCount
	onSend := x.onSend
	x.mu.Unlock()
	if onSend != nil {
		onSend(count)
	}
	if x.m.retransmitted != nil {
		x.m.retransmitted()
	}
	if err := x.m.send(x.session, x.dest(), p.datagram); err != nil {
		x.m.log.Debug("matter.controller.retransmit_failed", slog.Int("exchange_id", int(x.id)), slog.Any("err", err))
	}
	x.m.log.Debug("matter.controller.retransmit", slog.Int("exchange_id", int(x.id)), slog.Int("retransmission", count), slog.Duration("backoff", backoff))
	x.mu.Lock()
	if x.retransmitsLocked(p) {
		x.armRetransLocked(p, backoff, expectedProcessingTime)
	}
	x.mu.Unlock()
	return true
}

// kickLocked decides whether a duplicate of the message our pending one
// answers proves the peer awake and our reply lost, so the pending message
// is retransmitted now instead of on its schedule; it returns the message
// to retransmit with its timer stopped, or nil. Only on BDX, never on the
// last transmission slot, never faster than the peer's active interval and
// only when the scheduled attempt lies beyond the peer's active threshold.
// Mirrors matter.js MessageExchange.ts:564-617
// (#kickRetransmissionForLiveness), the cadence with the medium margin
// (:588-589).
func (x *Exchange) kickLocked(dup *Message) *pending {
	if x.protocol != bdxProtocolID || x.closed || (x.initiator && x.closing) {
		return nil
	}
	p := x.sentToAck
	if p == nil || !x.retransArmed || !p.hasAck || dup.Counter != p.ackCounter {
		return nil
	}
	if limit := x.sendOpts.MaxRetransmissions; limit >= 0 && x.retransCount+1 >= cmp.Or(limit, MaxTransmissions) {
		return nil
	}
	iv, _ := x.session.timing()
	elapsed := x.m.clock.Now().Sub(x.lastTransmitAt)
	if !x.lastTransmitAt.IsZero() && elapsed < iv.Active+x.sendAdditionalDelay() {
		return nil
	}
	if remaining := max(0, x.retransInterval-elapsed); remaining <= iv.ActiveThreshold {
		return nil
	}
	x.stopRetransTimerLocked()
	return p
}

// onMessageReceived takes a message routed to the exchange. Mirrors
// matter.js MessageExchange.ts:445-533: a message of a foreign protocol
// (other than a standalone ack or a StatusReport) is dropped; a duplicate
// is only acknowledged; a retransmission of the message our pending one
// acknowledges makes us resend it; a message that fails to acknowledge
// our pending one is dropped; the acknowledgement of our pending message
// completes its send; a standalone ack ends there; a reliable message is
// owed an acknowledgement — an earlier one still owed goes out at once —
// and is queued for [Exchange.Next].
func (x *Exchange) onMessageReceived(msg *Message, duplicate bool) {
	x.mu.Lock()
	x.receivedCount++
	standalone := msg.isStandaloneAck()
	if msg.ProtocolID != x.protocol && !standalone && !msg.isStatusReport() {
		x.mu.Unlock()
		x.m.log.Debug("matter.controller.drop", slog.String("reason", "unexpected protocol"), slog.Int("exchange_id", int(x.id)))
		return
	}
	x.lastActive = x.m.clock.Now()
	if onReceive := x.onReceive; onReceive != nil {
		x.mu.Unlock()
		onReceive()
		x.mu.Lock()
	}
	if duplicate {
		x.onDuplicateLocked(msg)
		return
	}
	if p := x.sentToAck; p != nil && p.hasAck && msg.Counter == p.ackCounter {
		x.notifySentLocked()
		datagram := p.datagram
		x.mu.Unlock()
		_ = x.m.send(x.session, x.dest(), datagram)
		return
	}
	if p := x.sentToAck; p != nil {
		switch {
		case !msg.HasAck:
			x.mu.Unlock()
			x.m.log.Debug("matter.controller.drop", slog.String("reason", ErrFlow.Error()+": previous message ack is missing"), slog.Int("exchange_id", int(x.id)))
			return
		case msg.AckCounter != p.counter:
			if !standalone {
				x.mu.Unlock()
				x.m.log.Debug("matter.controller.drop", slog.String("reason", ErrFlow.Error()+": incorrect ack"), slog.Int("exchange_id", int(x.id)))
				return
			}
		default:
			x.stopRetransTimerLocked()
			x.retransCount = 0
			p.resolve(msg, nil)
			x.sentToAck = nil
			if standalone && x.closeTimer != nil {
				x.mu.Unlock()
				x.finalClose(nil)
				return
			}
		}
	}
	if standalone {
		x.mu.Unlock()
		return
	}
	if msg.RequiresAck {
		if old := x.receivedToAck; old != nil {
			x.stopAckTimerLocked()
			x.receivedToAck = nil
			_, _ = x.transmitLocked(mrp.StandaloneAckOpcode, nil, &old.Counter)
		}
		x.receivedToAck = msg
		x.ackTimer = x.m.clock.AfterFunc(StandaloneAckTimeout, func() { x.ackTimeout(msg) })
	}
	x.queue = append(x.queue, msg)
	x.countQueuedLocked()
	select {
	case x.signal <- struct{}{}:
	default:
	}
	x.mu.Unlock()
}

// onDuplicateLocked takes a duplicate with x.mu held and releases it: a
// retransmission of our pending message acknowledges the duplicate, so it
// stands in for the standalone ack (MessageExchange.ts:485-495).
func (x *Exchange) onDuplicateLocked(msg *Message) {
	if msg.RequiresAck {
		if p := x.kickLocked(msg); p != nil {
			ept := x.expectedProcessingTimeLocked()
			x.mu.Unlock()
			if x.retransmit(p, ept) {
				return
			}
			x.mu.Lock()
		}
		_, _ = x.transmitLocked(mrp.StandaloneAckOpcode, nil, &msg.Counter)
	}
	x.mu.Unlock()
}

// ackTimeout sends the standalone ack for msg when no outbound message
// carried it within [StandaloneAckTimeout] (MessageExchange.ts:232-241).
func (x *Exchange) ackTimeout(msg *Message) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.receivedToAck != msg {
		return
	}
	x.receivedToAck = nil
	x.ackTimer = nil
	_, _ = x.transmitLocked(mrp.StandaloneAckOpcode, nil, &msg.Counter)
}

// sendStandaloneAckFor acknowledges msg when it asks for it
// (MessageExchange.ts:862-872).
func (x *Exchange) sendStandaloneAckFor(msg *Message) {
	if !msg.RequiresAck {
		return
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	_, _ = x.transmitLocked(mrp.StandaloneAckOpcode, nil, &msg.Counter)
}

// Next waits for the peer's next message on the exchange — matter.js
// MessageExchange.ts:822-860. The wait is opts.Timeout, or
// [MaxPeerResponseTime] of our own intervals and the expected processing
// time; on expiry it is a [*PeerMessageMissingError], which reports the
// peer lost when nothing was received on the exchange. On a closed
// session the wait is unbounded but for ctx.
func (x *Exchange) Next(ctx context.Context, opts ReceiveOptions) (*Message, error) {
	timeout := opts.Timeout
	if timeout == 0 && !x.session.IsClosed() {
		ept := opts.ExpectedProcessingTime
		if ept == 0 {
			ept = DefaultExpectedProcessingTime
		}
		if x.session.UsesMRP() {
			timeout = MaxPeerResponseTimePadded(nil, false, IntervalsOf(x.m.local), ept, x.sendPad())
		} else {
			timeout = ReliableMaxPeerResponseTime(ept)
		}
	}
	var expired chan struct{}
	if timeout > 0 {
		expired = make(chan struct{})
		t := x.m.clock.AfterFunc(timeout, func() { close(expired) })
		defer t.Stop()
	}
	x.mu.Lock()
	x.releaseHeldLocked()
	x.mu.Unlock()
	for {
		x.mu.Lock()
		if len(x.queue) > 0 {
			msg := x.queue[0]
			x.queue = x.queue[1:]
			x.takeCountedLocked()
			x.mu.Unlock()
			return msg, nil
		}
		if x.closed {
			cause := x.closeCause
			x.mu.Unlock()
			return nil, closedError(cause)
		}
		x.mu.Unlock()
		select {
		case <-x.signal:
		case <-x.done:
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-expired:
			err := &PeerMessageMissingError{Timeout: timeout}
			if x.reportsPeerLoss(opts.SuppressPeerLoss) {
				x.m.exchangePeerLost(x, err)
			}
			return nil, err
		}
	}
}

// Close ends the exchange gracefully — matter.js MessageExchange.ts:1053-1130
// (close without a cause): an acknowledgement still owed goes out at
// once; with no message of ours unacknowledged the exchange closes now,
// otherwise it stays open for the maximum time our remaining
// transmissions can take, so a late acknowledgement is still taken, and
// closes then. An exchange that never sent closes at once. Close returns
// without waiting; [Exchange.Done] reports the end. It is idempotent.
func (x *Exchange) Close() {
	x.notifyClosing()
	x.mu.Lock()
	x.releaseHeldLocked()
	if x.closed || x.closeTimer != nil {
		x.mu.Unlock()
		return
	}
	if !x.used {
		x.mu.Unlock()
		x.finalClose(nil)
		return
	}
	x.closing = true
	if old := x.receivedToAck; old != nil {
		x.stopAckTimerLocked()
		x.receivedToAck = nil
		_, _ = x.transmitLocked(mrp.StandaloneAckOpcode, nil, &old.Counter)
	}
	if x.sentToAck == nil {
		x.mu.Unlock()
		x.finalClose(nil)
		return
	}
	iv, active := x.session.timing()
	var wait time.Duration
	pad := x.sendPad()
	for i := x.retransCount; i <= MaxTransmissions; i++ {
		wait += RetransmissionIntervalPadded(i, iv, active, 1, pad)
	}
	x.closeTimer = x.m.clock.AfterFunc(wait, func() { x.finalClose(nil) })
	x.mu.Unlock()
}

// notifyClosing runs [Options].OnClosing once.
func (x *Exchange) notifyClosing() {
	x.onClosingOnce.Do(func() {
		if x.onClosing != nil {
			x.onClosing()
		}
	})
}

// Abort closes the exchange at once with cause: an acknowledgement still
// owed goes out, nothing is waited for, and further calls fail with
// cause — matter.js MessageExchange.ts:1053-1105 (close with a cause).
func (x *Exchange) Abort(cause error) {
	x.abort(cause)
}

func (x *Exchange) abort(cause error) {
	x.notifyClosing()
	x.mu.Lock()
	x.releaseHeldLocked()
	if x.closed {
		x.mu.Unlock()
		return
	}
	x.closing = true
	if old := x.receivedToAck; old != nil && x.used {
		x.stopAckTimerLocked()
		x.receivedToAck = nil
		_, _ = x.transmitLocked(mrp.StandaloneAckOpcode, nil, &old.Counter)
	}
	x.mu.Unlock()
	x.finalClose(cause)
}

// finalClose stops every timer, completes a waiting send (matter.js
// resolves it, MessageExchange.ts:1145), ends Next with the close cause
// and removes the exchange (MessageExchange.ts:1132-1155,
// ExchangeManager.ts:494-501).
func (x *Exchange) finalClose(cause error) {
	x.notifyClosing()
	x.mu.Lock()
	if x.closed {
		x.mu.Unlock()
		return
	}
	if x.closeCause == nil {
		x.closeCause = cause
	}
	x.stopRetransTimerLocked()
	x.stopAckTimerLocked()
	if x.closeTimer != nil {
		x.closeTimer.Stop()
	}
	if p := x.sentToAck; p != nil {
		p.resolve(nil, nil)
	}
	x.releaseHeldLocked()
	if x.queuedCounted > 0 {
		x.m.inflight.Add(inflight.ExchangeDelivered, -int64(x.queuedCounted))
		x.queuedCounted = 0
	}
	x.closed = true
	close(x.done)
	x.mu.Unlock()
	x.m.removeExchange(x)
}

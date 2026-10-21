// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package btp

import (
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/clock"
)

// maximumSequenceNumber is the last sequence number before the wrap to 0
// (BtpSessionHandler.ts:27).
const maximumSequenceNumber = 255

// ErrDisconnected marks a GATT write that failed because the peer is gone
// — matter.js BleDisconnectedError (packages/protocol/src/ble/Ble.ts:15).
// A [Callbacks].Write error wrapping it ends the session without being
// reported to the sender (BtpSessionHandler.ts:539-555).
var ErrDisconnected = errors.New("btp: BLE disconnected")

// Role is the side of the BTP session: the GATT client (central) that
// writes C1 or the GATT server (peripheral) that indicates on C2.
type Role int

// The two roles.
const (
	RoleCentral Role = iota
	RolePeripheral
)

func (r Role) String() string {
	if r == RoleCentral {
		return "central"
	}
	return "peripheral"
}

// Callbacks are the session's way out — matter.js's writeBleCallback,
// disconnectBleCallback and handleMatterMessagePayload
// (BtpSessionHandler.ts:205-213). Each is called with no session lock
// held, as matter.js awaits each.
type Callbacks struct {
	// Write ships one BTP packet: on C1 for the central, as an indication
	// on C2 for the peripheral. An error wrapping [ErrDisconnected] is the
	// peer gone.
	Write func(packet []byte) error
	// Disconnect drops the BLE connection; called once, when the session
	// closes.
	Disconnect func() error
	// Message receives one reassembled Matter message.
	Message func(msg []byte) error
}

// Options are the session's clock and logger; the zero value is the
// system clock and no log.
type Options struct {
	// TimeSource paces the session's timers; nil is [clock.System].
	TimeSource clock.Clock
	Logger     *slog.Logger
}

// outgoing is one queued Matter message and how much of it is sent —
// matter.js keeps a DataReader per message (BtpSessionHandler.ts:42).
type outgoing struct {
	data []byte
	off  int
}

// Session is one BTP session — matter.js BtpSessionHandler
// (packages/protocol/src/ble/BtpSessionHandler.ts:31-715) at 0ceb202d. It
// is safe for concurrent use.
type Session struct {
	role         Role
	cb           Callbacks
	clock        clock.Clock
	log          *slog.Logger
	fragmentSize int
	windowSize   int

	mu sync.Mutex
	// Receive side (:32-35): the message being reassembled, the last
	// sequence number received, the last acknowledgement received.
	reassembling    bool
	incomingLen     int
	incoming        []byte
	prevIncomingSeq int
	prevIncomingAck int
	// Send side (:40-43): the last sequence number sent, the last
	// sequence number we acknowledged, the queue.
	seq            int
	prevAckedSeq   int
	queue          []*outgoing
	sendInProgress bool
	active         bool
	// pendingFirstAck are the messages sent while the peer has sent
	// nothing yet; nil once any packet arrived (:54-60).
	pendingFirstAck [][]byte
	pendingLive     bool
	onStall         func(replay [][]byte)

	ackReceiveTimer, sendAckTimer, idleTimer *sessionTimer

	closed     chan struct{}
	closedOnce sync.Once
}

// sessionTimer is matter.js's one-shot Timer (packages/general/src/time/StandardTime.ts):
// start restarts a running timer, a fired timer is no longer running. Its
// state is guarded by the session's mutex.
type sessionTimer struct {
	s       *Session
	d       time.Duration
	fn      func()
	t       clock.Timer
	gen     uint64
	running bool
}

func (t *sessionTimer) start() {
	t.stop()
	t.running = true
	t.gen++
	g := t.gen
	t.t = t.s.clock.AfterFunc(t.d, func() {
		t.s.mu.Lock()
		if !t.running || t.gen != g {
			t.s.mu.Unlock()
			return
		}
		t.running = false
		t.s.mu.Unlock()
		t.fn()
	})
}

func (t *sessionTimer) stop() {
	if t.t != nil {
		t.t.Stop()
		t.t = nil
	}
	t.running = false
	t.gen++
}

// NewPeripheral answers a central's handshake request and returns the
// peripheral's session — matter.js createFromHandshakeRequest
// (BtpSessionHandler.ts:87-157): the highest version both support, else
// the connection is dropped and the request refused; the segment size
// the minimum, or with maxDataSize above it the smallest of maxDataSize,
// the offered size (when above the minimum) and the maximum; the window
// the smaller of the central's and the maximum; the response written,
// and only then the acknowledgement timer started for it. maxDataSize 0
// is matter.js's undefined.
func NewPeripheral(maxDataSize int, handshakeRequest []byte, cb Callbacks, opts Options) (*Session, error) {
	req, err := DecodeHandshakeRequest(handshakeRequest)
	if err != nil {
		return nil, err
	}
	version, found := 0, false
	for _, v := range SupportedVersions {
		if slices.Contains(req.Versions, v) {
			version, found = v, true
			break
		}
	}
	if !found {
		_ = cb.Disconnect()
		vs := make([]string, len(req.Versions))
		for i, v := range req.Versions {
			vs[i] = strconv.Itoa(v)
		}
		return nil, protocolError("No supported BTP version found in " + strings.Join(vs, ","))
	}
	attMTU := MinimumATTMTU
	if maxDataSize > MinimumATTMTU {
		if int(req.ATTMTU) <= MinimumATTMTU {
			attMTU = min(maxDataSize, MaximumBTPMTU)
		} else {
			attMTU = min(int(req.ATTMTU), maxDataSize, MaximumBTPMTU)
		}
	}
	window := min(MaximumWindowSize, int(req.ClientWindowSize))
	resp := EncodeHandshakeResponse(HandshakeResponse{Version: version, ATTMTU: uint16(attMTU), WindowSize: uint8(window)}) //nolint:gosec // both clamped above
	s, err := newSession(RolePeripheral, version, attMTU, window, cb, opts)
	if err != nil {
		return nil, err
	}
	if err := cb.Write(resp); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.ackReceiveTimer.start()
	s.mu.Unlock()
	return s, nil
}

// NewCentral returns the central's session for the peripheral's handshake
// response — matter.js createAsCentral (BtpSessionHandler.ts:164-193):
// requestedSegmentSize, the size the central offered, must be at least
// the minimum; the segment size is the smallest of the peripheral's
// choice, the offered size and the maximum, so a peripheral echoing a
// larger size cannot undo a deliberate reduction.
func NewCentral(handshakeResponse []byte, cb Callbacks, requestedSegmentSize int, opts Options) (*Session, error) {
	if requestedSegmentSize < MinimumATTMTU {
		return nil, fmt.Errorf("btp: requested BTP segment size must be an integer of at least %d, got %d", MinimumATTMTU, requestedSegmentSize)
	}
	resp, err := DecodeHandshakeResponse(handshakeResponse)
	if err != nil {
		return nil, err
	}
	fragment := min(int(resp.ATTMTU), requestedSegmentSize, MaximumBTPMTU)
	return newSession(RoleCentral, resp.Version, fragment, int(resp.WindowSize), cb, opts)
}

// newSession is the constructor (BtpSessionHandler.ts:205-227): version 4
// only; the peripheral has nothing to acknowledge yet (the handshake
// request is unsequenced) and its handshake response was sequence 0; the
// central owes the acknowledgement of that response (sequence 0), so its
// send-ack timer starts at once, and its first packet is sequence 0.
func newSession(role Role, version, fragmentSize, windowSize int, cb Callbacks, opts Options) (*Session, error) {
	if version != 4 {
		return nil, protocolError(fmt.Sprintf("Unsupported BTP version %d", version))
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	s := &Session{
		role: role, cb: cb, clock: clock.Or(opts.TimeSource), log: opts.Logger,
		fragmentSize: fragmentSize, windowSize: windowSize,
		prevIncomingSeq: 255, prevIncomingAck: -1,
		seq: 0, prevAckedSeq: -1,
		active: true, pendingLive: true,
		closed: make(chan struct{}),
	}
	s.ackReceiveTimer = &sessionTimer{s: s, d: AckTimeout, fn: s.ackTimeoutTriggered}
	s.sendAckTimer = &sessionTimer{s: s, d: SendAckTimeout, fn: s.sendAckTimeoutTriggered}
	s.idleTimer = &sessionTimer{s: s, d: ConnIdleTimeout, fn: func() {
		s.log.Info("matter.btp.idle_timeout", slog.String("role", s.role.String()))
		_ = s.Close()
	}}
	if role == RolePeripheral {
		s.prevAckedSeq = s.prevIncomingSeq
	} else {
		s.mu.Lock()
		s.sendAckTimer.start()
		s.mu.Unlock()
		s.prevIncomingSeq = 0
		s.prevIncomingAck = -1
		s.seq = -1
	}
	return s, nil
}

// Role is the session's side.
func (s *Session) Role() Role { return s.role }

// FragmentSize is the negotiated segment size.
func (s *Session) FragmentSize() int { return s.fragmentSize }

// WindowSize is the negotiated window size.
func (s *Session) WindowSize() int { return s.windowSize }

// Closed is closed when the session closes — matter.js `closed`
// (BtpSessionHandler.ts:62-65). A suspended session does not close it.
func (s *Session) Closed() <-chan struct{} { return s.closed }

// OnStall observes the stall matter.js reports as stalledAfterHandshake
// (BtpSessionHandler.ts:67-84): a central session above the minimum
// segment size whose peer completed the handshake and then sent no BTP
// packet before the acknowledgement timeout, although we sent it Matter
// data. The session is suspended, f receives the unacknowledged Matter
// messages in submission order, and the transport owns what happens next.
// Without an observer the session closes on that timeout instead.
func (s *Session) OnStall(f func(replay [][]byte)) {
	s.mu.Lock()
	s.onStall = f
	s.mu.Unlock()
}

// HandleIncoming takes one BTP packet from the GATT layer and hands every
// completed Matter message to [Callbacks].Message — matter.js
// handleIncomingBleData (BtpSessionHandler.ts:229-370). Ignored on an
// inactive session. On any error the session closes; a protocol
// violation is then swallowed, every other error returned.
func (s *Session) HandleIncoming(data []byte) error {
	s.mu.Lock()
	if !s.active {
		s.mu.Unlock()
		s.log.Debug("matter.btp.rx_inactive")
		return nil
	}
	err := s.handleIncomingLocked(data)
	s.mu.Unlock()
	if err == nil {
		return nil
	}
	s.log.Warn("matter.btp.rx_error", slog.String("err", err.Error()))
	if cerr := s.Close(); cerr != nil {
		return cerr
	}
	var pe *ProtocolError
	if errors.As(err, &pe) {
		return nil
	}
	return err
}

func (s *Session) handleIncomingLocked(data []byte) error {
	if len(data) > s.fragmentSize {
		// Apple seems to read the ATT_MTU as the size of one ATT packet
		// (:240-252): three bytes over are accepted.
		if len(data) > s.fragmentSize+3 {
			return protocolError(fmt.Sprintf("Received data %d bytes exceeds fragment size of %d bytes", len(data), s.fragmentSize))
		}
		s.log.Warn("matter.btp.rx_oversized", slog.Int("bytes", len(data)), slog.Int("fragment_size", s.fragmentSize))
	}
	pk, err := DecodePacket(data)
	if err != nil {
		return err
	}
	h, p := pk.Header, pk.Payload
	if h.IsHandshakeRequest || h.HasManagementOpcode {
		return protocolError("BTP packet must not be a handshake request or have a management opcode.")
	}
	if len(p.SegmentPayload) == 0 && !h.HasAckNumber {
		return protocolError("BTP packet must have a segment payload or an ack number.")
	}
	if int(p.SequenceNumber) != (s.prevIncomingSeq+1)%256 {
		return protocolError("Expected and actual BTP packets sequence number does not match")
	}
	s.prevIncomingSeq = int(p.SequenceNumber)
	s.pendingFirstAck, s.pendingLive = nil, false
	if !s.sendAckTimer.running {
		s.sendAckTimer.start()
	}
	if h.HasAckNumber && p.AckNumber != nil {
		// A valid ack lies in the outstanding interval
		// (prevIncomingAck, seq], as a wrap-safe distance (:287-304).
		ack := int(*p.AckNumber)
		outstanding := modularDistance(s.prevIncomingAck, s.seq)
		if modularDistance(s.prevIncomingAck, ack) > outstanding {
			return protocolError(fmt.Sprintf("Invalid Ack Number, Ack Number: %d, Sequence Number: %d, Previous AckNumber: %d", ack, s.seq, s.prevIncomingAck))
		}
		s.ackReceiveTimer.stop()
		s.prevIncomingAck = ack
		if ack != s.seq {
			s.ackReceiveTimer.start()
		}
	}
	switch {
	case h.IsBeginningSegment:
		if s.reassembling {
			return protocolError("BTP message flow error! New beginning packet was received without previous message being completed.")
		}
		s.reassembling, s.incomingLen, s.incoming = true, int(*p.MessageLength), p.SegmentPayload
	case h.IsContinuingSegment || h.IsEndingSegment:
		if !s.reassembling {
			return protocolError("BTP Continuing or ending packet received without beginning packet.")
		}
		if len(p.SegmentPayload) == 0 {
			return protocolError("BTP Continuing or ending packet received without payload.")
		}
		if n := len(s.incoming) + len(p.SegmentPayload); n > s.incomingLen {
			return protocolError(fmt.Sprintf("BTP reassembled length %d exceeds declared message length %d", n, s.incomingLen))
		}
		s.incoming = append(s.incoming, p.SegmentPayload...)
	}
	if h.IsEndingSegment {
		if !s.reassembling {
			return protocolError("BTP beginning packet missing but ending packet received.")
		}
		if len(s.incoming) != s.incomingLen {
			return protocolError(fmt.Sprintf("BTP packet payload length does not match message length: %d !== %d", len(s.incoming), s.incomingLen))
		}
		msg := s.incoming
		s.reassembling, s.incomingLen, s.incoming = false, 0, nil
		s.mu.Unlock()
		err := s.cb.Message(msg)
		s.mu.Lock()
		if err != nil {
			return err
		}
	}
	// A received ack may have reopened the remote window: flush what was
	// held back, which piggybacks the ack (:359-362).
	if err := s.processSendQueueLocked(); err != nil {
		return err
	}
	return s.sendImmediateAckIfWindowLowLocked()
}

// Send fragments one Matter message into BTP segments and sends what the
// peer's window allows now — matter.js sendMatterMessage
// (BtpSessionHandler.ts:372-392). The rest goes out as acknowledgements
// reopen the window.
func (s *Session) Send(msg []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active {
		return &FlowError{Message: "BTP session is not active"}
	}
	if len(msg) == 0 {
		return &FlowError{Message: "BTP packet must not be empty"}
	}
	if len(msg) > 0xFFFF {
		// The message length field is 16 bits (BtpCodec.ts:182).
		return &FlowError{Message: fmt.Sprintf("BTP message of %d bytes exceeds the 16-bit message length", len(msg))}
	}
	data := append([]byte(nil), msg...)
	if s.pendingLive {
		s.pendingFirstAck = append(s.pendingFirstAck, data)
	}
	s.queue = append(s.queue, &outgoing{data: data})
	return s.processSendQueueLocked()
}

// processSendQueueLocked is processSendQueue (BtpSessionHandler.ts:394-512).
func (s *Session) processSendQueueLocked() error {
	if s.sendInProgress || !s.active || len(s.queue) == 0 {
		return nil
	}
	s.sendInProgress = true
	for len(s.queue) > 0 {
		hasAck := s.prevIncomingSeq != s.prevAckedSeq
		// §4.19.4.7: not into a full window, and not into the reserved
		// last slot without an ack (:403-408).
		if !s.canSend(hasAck) {
			break
		}
		if hasAck {
			s.sendAckTimer.stop()
		}
		m := s.queue[0]
		remaining := len(m.data) - m.off
		isBeginning := remaining == len(m.data)
		headerLen := 2
		if isBeginning {
			headerLen += 2
		}
		if hasAck {
			headerLen++
		}
		room := s.fragmentSize - headerLen
		if room <= 0 {
			s.sendInProgress = false
			return &FlowError{Message: fmt.Sprintf("BTP segment size %d leaves no room for payload", s.fragmentSize)}
		}
		isEnding := remaining <= room
		n := min(room, remaining)
		seg := m.data[m.off : m.off+n]
		m.off += n
		pk := Packet{
			Header:  Header{HasAckNumber: hasAck, IsBeginningSegment: isBeginning, IsContinuingSegment: !isBeginning, IsEndingSegment: isEnding},
			Payload: Payload{SequenceNumber: s.nextSequenceNumber(), SegmentPayload: seg},
		}
		if hasAck {
			a := uint8(s.prevIncomingSeq) //nolint:gosec // a sequence number, 0..255
			pk.Payload.AckNumber = &a
			// Commit the ack before the write, so a concurrent send cannot
			// ack the same sequence twice (:467-471).
			s.prevAckedSeq = s.prevIncomingSeq
		}
		if isBeginning {
			l := uint16(remaining) //nolint:gosec // Send bounds a message to 16 bits
			pk.Payload.MessageLength = &l
		}
		packet, err := EncodePacket(pk)
		if err != nil {
			s.sendInProgress = false
			return err
		}
		s.mu.Unlock()
		werr := s.cb.Write(packet)
		s.mu.Lock()
		if werr != nil {
			// The sequence number is spent whether or not the packet
			// arrived: the session cannot go on (:475-482).
			s.queue = nil
			s.sendInProgress = false
			return s.handleFailedWriteLocked(werr)
		}
		if !s.active {
			// Suspended during the write: the transport is someone else's
			// now (:484-490).
			s.queue = nil
			s.sendInProgress = false
			return nil
		}
		if !s.ackReceiveTimer.running {
			s.ackReceiveTimer.start()
		}
		if s.role == RoleCentral {
			// Unique data restarts the central's idle timer (:495-502).
			s.idleTimer.start()
		}
		if isEnding {
			s.queue = s.queue[1:]
		}
	}
	s.sendInProgress = false
	// An ack that could not be piggybacked would otherwise wait for the
	// send-ack timer, which may have fired while the send held the lock
	// (:510-513).
	return s.sendStandaloneAckLocked()
}

// Close closes the session: timers stopped, the BLE connection dropped,
// Closed closed — matter.js close (BtpSessionHandler.ts:514-532). The
// session is closed whether or not the disconnect succeeds; its error is
// returned.
func (s *Session) Close() error {
	s.mu.Lock()
	s.stopTimersLocked()
	if !s.active {
		s.mu.Unlock()
		return nil
	}
	s.log.Debug("matter.btp.close", slog.String("role", s.role.String()))
	s.active = false
	s.mu.Unlock()
	defer s.closedOnce.Do(func() { close(s.closed) })
	return s.cb.Disconnect()
}

// handleFailedWriteLocked ends the session after a failed write and
// returns the error unless it is a disconnect (BtpSessionHandler.ts:534-556).
// A write that fails after the session ended is only logged.
func (s *Session) handleFailedWriteLocked(err error) error {
	if !s.active {
		s.log.Debug("matter.btp.write_failed_after_end", slog.String("err", err.Error()))
		return nil
	}
	s.mu.Unlock()
	if cerr := s.Close(); cerr != nil {
		s.log.Debug("matter.btp.close_after_write_failed", slog.String("err", cerr.Error()))
	}
	s.mu.Lock()
	if errors.Is(err, ErrDisconnected) {
		s.log.Debug("matter.btp.write_disconnected", slog.String("err", err.Error()))
		return nil
	}
	return err
}

// Suspend ends the session without touching the transport, so the same
// BLE connection can carry a renegotiated session; Closed stays open and
// Disconnect is not called (BtpSessionHandler.ts:558-563).
func (s *Session) Suspend() {
	s.mu.Lock()
	s.stopTimersLocked()
	s.active = false
	s.mu.Unlock()
}

func (s *Session) stopTimersLocked() {
	s.sendAckTimer.stop()
	s.ackReceiveTimer.stop()
	s.idleTimer.stop()
}

// sendAckTimeoutTriggered sends the pending acknowledgement when the
// send-ack timer expires (BtpSessionHandler.ts:571-579).
func (s *Session) sendAckTimeoutTriggered() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active {
		return
	}
	if err := s.sendStandaloneAckLocked(); err != nil {
		s.log.Warn("matter.btp.standalone_ack_failed", slog.String("err", err.Error()))
	}
}

// sendImmediateAckIfWindowLowLocked acknowledges at once when the local
// receive window runs low, unless queued data will carry the ack
// (§4.19.4.8, BtpSessionHandler.ts:580-586).
func (s *Session) sendImmediateAckIfWindowLowLocked() error {
	if len(s.queue) > 0 || s.sendInProgress {
		return nil
	}
	if s.localWindowFreeSlots() > ImmediateAckWindowThreshold {
		return nil
	}
	return s.sendStandaloneAckLocked()
}

// sendStandaloneAckLocked sends a stand-alone acknowledgement when one is
// pending and the remote window has room (BtpSessionHandler.ts:587-640).
func (s *Session) sendStandaloneAckLocked() error {
	if s.sendInProgress || !s.active {
		return nil
	}
	ack := s.prevIncomingSeq
	if ack == s.prevAckedSeq {
		return nil
	}
	// An ack consumes a remote-window slot too (§4.19.4.7).
	if !s.canSend(true) {
		return nil
	}
	s.sendInProgress = true
	a := uint8(ack) //nolint:gosec // a sequence number, 0..255
	packet, err := EncodePacket(Packet{
		Header:  Header{HasAckNumber: true},
		Payload: Payload{AckNumber: &a, SequenceNumber: s.nextSequenceNumber()},
	})
	if err != nil {
		s.sendInProgress = false
		return err
	}
	s.prevAckedSeq = ack
	s.mu.Unlock()
	werr := s.cb.Write(packet)
	s.mu.Lock()
	if werr != nil {
		err := s.handleFailedWriteLocked(werr)
		s.sendInProgress = false
		return err
	}
	s.sendAckTimer.stop()
	if s.active && !s.ackReceiveTimer.running {
		s.ackReceiveTimer.start()
	}
	s.sendInProgress = false
	// Flush a message queued while the ack held the send lock (:636-637).
	return s.processSendQueueLocked()
}

// ackTimeoutTriggered is the acknowledgement-received timer: the session
// closes, or reports a stall (BtpSessionHandler.ts:642-665).
func (s *Session) ackTimeoutTriggered() {
	s.mu.Lock()
	if !s.active || s.prevIncomingAck == s.seq {
		s.mu.Unlock()
		return
	}
	if replay := s.messagesToReplayOnStallLocked(); replay != nil {
		s.log.Debug("matter.btp.stall", slog.Int("fragment_size", s.fragmentSize))
		s.pendingFirstAck, s.pendingLive = nil, false
		s.stopTimersLocked()
		s.active = false
		observer := s.onStall
		s.mu.Unlock()
		observer(replay)
		return
	}
	s.mu.Unlock()
	s.log.Warn("matter.btp.ack_timeout")
	_ = s.Close()
}

// messagesToReplayOnStallLocked is #messagesToReplayOnStall
// (BtpSessionHandler.ts:667-680).
func (s *Session) messagesToReplayOnStallLocked() [][]byte {
	if !s.pendingLive || len(s.pendingFirstAck) == 0 || s.onStall == nil {
		return nil
	}
	if s.role != RoleCentral || s.fragmentSize <= MinimumATTMTU {
		return nil
	}
	return slices.Clone(s.pendingFirstAck)
}

// nextSequenceNumber is getNextSequenceNumber (BtpSessionHandler.ts:680-690).
func (s *Session) nextSequenceNumber() uint8 {
	s.seq++
	if s.seq > maximumSequenceNumber {
		s.seq = 0
	}
	return uint8(s.seq) //nolint:gosec // wrapped to 0..255 above
}

// modularDistance is the wrap-safe forward distance modulo 256; the -1
// sentinels read as 255 (BtpSessionHandler.ts:691-694).
func modularDistance(from, to int) int {
	return ((to-from)%256 + 256) % 256
}

// remoteWindowFreeSlots gates our sending (§4.19.4.7, :696-699).
func (s *Session) remoteWindowFreeSlots() int {
	return s.windowSize - modularDistance(s.prevIncomingAck, s.seq)
}

// localWindowFreeSlots drives the immediate acknowledgement (§4.19.4.8, :701-706).
func (s *Session) localWindowFreeSlots() int {
	return s.windowSize - modularDistance(s.prevAckedSeq, s.prevIncomingSeq)
}

// canSend is §4.19.4.7: a data-only packet leaves the last slot free; an
// ack-bearing one may use it (:708-714).
func (s *Session) canSend(carriesAck bool) bool {
	free := s.remoteWindowFreeSlots()
	if free <= 0 {
		return false
	}
	if free <= WindowNoAckSendThreshold && !carriesAck {
		return false
	}
	return true
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package exchange

// A port of matter.js packages/protocol/test/protocol/MessageExchangeTest.ts
// at 862ad75f: peer-loss declaration, the unacked state, the BDX liveness
// kick and retransmission cap, timers that outlive their message, activity,
// close, cross-protocol StatusReports and the ack-wait abort.
//
// matter.js drives MessageExchange through ProtocolMocks.NodeSession and
// calls onMessageReceived directly; the Go exchange runs on a secure session
// of a Manager over a recording socket, and the inbound messages are handed
// to onMessageReceived the same way. Time is the manager's clocktest.Fake.

import (
	"bytes"
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/secure/sessionparams"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// mxConn records every datagram and lets a test hook into a transmission;
// the hook runs outside the conn's lock, so it may block one send while
// others proceed.
type mxConn struct {
	mu   sync.Mutex
	out  [][]byte
	sent chan struct{}
	hook func(n int)
	fail error
}

func (c *mxConn) Send(_ *net.UDPAddr, b []byte) error {
	c.mu.Lock()
	if c.fail != nil {
		c.mu.Unlock()
		return c.fail
	}
	c.out = append(c.out, append([]byte(nil), b...))
	n, hook := len(c.out), c.hook
	c.mu.Unlock()
	c.sent <- struct{}{}
	if hook != nil {
		hook(n)
	}
	return nil
}

func (c *mxConn) datagrams() [][]byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([][]byte(nil), c.out...)
}

// waitSent blocks until n datagrams in all were sent.
func (c *mxConn) waitSent(t *testing.T, n int) {
	t.Helper()
	for len(c.datagrams()) < n {
		select {
		case <-c.sent:
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d of %d datagrams sent", len(c.datagrams()), n)
		}
	}
}

type mxEnv struct {
	m     *Manager
	conn  *mxConn
	clock *fakeClock
	s     *Session
	peer  *securePeer
	lost  chan error
}

// mxNew is createExchange's setting (MessageExchangeTest.ts:43-60): a secure
// session (ProtocolMocks.NodeSession) whose peer loss is recorded.
func mxNew(t *testing.T, suppress bool, timing *sessionparams.Parameters) *mxEnv {
	t.Helper()
	e := &mxEnv{conn: &mxConn{sent: make(chan struct{}, 256)}, clock: newFakeClock(), lost: make(chan error, 4)}
	m, err := NewManager(e.conn, Config{
		Clock: e.clock, Rand: func() float64 { return 0.5 },
		PeerLost: func(_ *Session, cause error) { e.lost <- cause },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	e.m = m
	e.s, e.peer = newSecurePair(t, m, 0x0300, suppress)
	if timing != nil {
		e.s.SetTimingParameters(*timing)
	}
	return e
}

func (e *mxEnv) exchange(t *testing.T, protocol uint16) *Exchange {
	t.Helper()
	x, err := e.m.Initiate(e.s, protocol, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return x
}

func (e *mxEnv) peerLost() bool {
	select {
	case <-e.lost:
		return true
	default:
		return false
	}
}

// counts splits the datagrams into payload sends and standalone acks.
func (e *mxEnv) counts(t *testing.T) (payload, acks int) {
	t.Helper()
	for _, b := range e.conn.datagrams() {
		if d := e.peer.decode(t, b); d.proto.ProtocolID == mrp.SecureChannelProtocolID && d.proto.Opcode == mrp.StandaloneAckOpcode {
			acks++
		} else {
			payload++
		}
	}
	return payload, acks
}

// firstCounter is the message counter of the first datagram sent.
func (e *mxEnv) firstCounter(t *testing.T) uint32 {
	t.Helper()
	return e.peer.decode(t, e.conn.datagrams()[0]).hdr.MessageCounter
}

// inbound is fakeInboundMessage (MessageExchangeTest.ts:67-87).
func inbound(x *Exchange, counter uint32, protocol uint16, opcode uint8) *Message {
	return &Message{Counter: counter, ProtocolID: protocol, Opcode: opcode, ExchangeID: x.ID()}
}

func acking(m *Message, counter uint32) *Message {
	m.HasAck, m.AckCounter = true, counter
	return m
}

// nextNow is nextMessage({ timeout: 0 }): the fake moves to the deadline.
func (e *mxEnv) nextNow(x *Exchange) error {
	return e.clock.Resolve(context.Background(), func() error {
		_, err := x.Next(context.Background(), ReceiveOptions{Timeout: time.Nanosecond})
		return err
	})
}

func TestMessageExchangePeerLossDeclaration(t *testing.T) {
	t.Parallel()
	boom := errors.New("simulated network failure")
	t.Run("send()/declares peer lost when exchange has received no messages", func(t *testing.T) {
		t.Parallel()
		e := mxNew(t, false, nil)
		x := e.exchange(t, mrp.SecureChannelProtocolID)
		e.conn.fail = boom
		if err := x.Send(context.Background(), 0, nil, SendOptions{}); !errors.Is(err, ErrNetwork) {
			t.Fatalf("Send = %v", err)
		}
		if !e.peerLost() {
			t.Error("peer loss not declared")
		}
	})
	t.Run("send()/does not declare peer lost when exchange already received at least one message", func(t *testing.T) {
		t.Parallel()
		e := mxNew(t, false, nil)
		x := e.exchange(t, mrp.SecureChannelProtocolID)
		x.onMessageReceived(inbound(x, 1, mrp.SecureChannelProtocolID, 1), false)
		e.conn.fail = boom
		if err := x.Send(context.Background(), 0, nil, SendOptions{}); !errors.Is(err, ErrNetwork) {
			t.Fatalf("Send = %v", err)
		}
		if e.peerLost() {
			t.Error("peer loss declared")
		}
	})
	t.Run("nextMessage()/declares peer lost when exchange has received no messages", func(t *testing.T) {
		t.Parallel()
		e := mxNew(t, false, nil)
		x := e.exchange(t, mrp.SecureChannelProtocolID)
		if err := e.nextNow(x); err == nil {
			t.Fatal("Next returned a message")
		}
		if !e.peerLost() {
			t.Error("peer loss not declared")
		}
	})
	t.Run("nextMessage()/does not declare peer lost when exchange already received at least one message", func(t *testing.T) {
		t.Parallel()
		e := mxNew(t, false, nil)
		x := e.exchange(t, mrp.SecureChannelProtocolID)
		x.onMessageReceived(inbound(x, 1, mrp.SecureChannelProtocolID, 1), false)
		if err := e.nextNow(x); err != nil {
			t.Fatalf("draining Next = %v", err)
		}
		if err := e.nextNow(x); err == nil {
			t.Fatal("Next returned a message")
		}
		if e.peerLost() {
			t.Error("peer loss declared")
		}
	})
	t.Run("on a session that suppresses peer loss/does not declare peer lost when a send fails", func(t *testing.T) {
		t.Parallel()
		e := mxNew(t, true, nil)
		x := e.exchange(t, mrp.SecureChannelProtocolID)
		e.conn.fail = boom
		if err := x.Send(context.Background(), 0, nil, SendOptions{}); !errors.Is(err, ErrNetwork) {
			t.Fatalf("Send = %v", err)
		}
		if e.peerLost() {
			t.Error("peer loss declared")
		}
	})
	t.Run("on a session that suppresses peer loss/does not declare peer lost when no response arrives", func(t *testing.T) {
		t.Parallel()
		e := mxNew(t, true, nil)
		x := e.exchange(t, mrp.SecureChannelProtocolID)
		if err := e.nextNow(x); err == nil {
			t.Fatal("Next returned a message")
		}
		if e.peerLost() {
			t.Error("peer loss declared")
		}
	})
}

func TestMessageExchangeHasUnackedMessage(t *testing.T) {
	t.Parallel()
	t.Run("is false before any send", func(t *testing.T) {
		t.Parallel()
		e := mxNew(t, false, nil)
		if e.exchange(t, mrp.SecureChannelProtocolID).HasUnackedMessage() {
			t.Error("unacked before any send")
		}
	})
	t.Run("is true while a sent message awaits ack and clears once acked", func(t *testing.T) {
		t.Parallel()
		e := mxNew(t, false, nil)
		x := e.exchange(t, mrp.SecureChannelProtocolID)
		done := sendAsync(x, 1, nil, SendOptions{})
		e.conn.waitSent(t, 1)
		if !x.HasUnackedMessage() {
			t.Fatal("not unacked while awaiting the ack")
		}
		x.onMessageReceived(acking(inbound(x, 2, mrp.SecureChannelProtocolID, 1), e.firstCounter(t)), false)
		if err := waitErr(t, done); err != nil {
			t.Fatal(err)
		}
		if x.HasUnackedMessage() {
			t.Error("still unacked after the ack")
		}
	})
}

// slowPeer is SLOW_PEER (MessageExchangeTest.ts:203): both intervals 7 s,
// so the BDX cap of 7.4 s is the armed interval.
func slowPeer(threshold uint16) *sessionparams.Parameters {
	return &sessionparams.Parameters{SessionIdleInterval: 7000, SessionActiveInterval: 7000, SessionActiveThreshold: threshold}
}

type kickOptions struct {
	protocol         uint16
	gap              time.Duration
	timing           *sessionparams.Parameters
	unrelated        bool
	drainToRemaining int // 0: no drain
}

// sendThenDuplicate is MessageExchangeTest.ts:209-285.
func sendThenDuplicate(t *testing.T, o kickOptions) (retransmissions, standaloneAcks int) {
	t.Helper()
	if o.timing == nil {
		o.timing = slowPeer(200)
	}
	e := mxNew(t, false, o.timing)
	x := e.exchange(t, o.protocol)
	query := inbound(x, 10, o.protocol, 1)
	query.RequiresAck = true
	x.onMessageReceived(query, false)
	done := sendAsync(x, 1, nil, SendOptions{})
	e.conn.waitSent(t, 1)
	if !x.HasUnackedMessage() {
		t.Fatal("not unacked")
	}
	if o.drainToRemaining > 0 {
		target := MaxTransmissions - o.drainToRemaining
		for i := 0; i < 200 && x.RetransmissionCount() < target; i++ {
			e.clock.Advance(8 * time.Second)
		}
		if got := x.RetransmissionCount(); got != target {
			t.Fatalf("retransmission count = %d, want %d", got, target)
		}
	}
	sendsBefore, acksBefore := e.counts(t)
	e.clock.Advance(o.gap)
	dup := query
	if o.unrelated {
		dup = inbound(x, 99, o.protocol, 1)
		dup.RequiresAck = true
	}
	x.onMessageReceived(dup, true)
	sends, acks := e.counts(t)
	x.onMessageReceived(acking(inbound(x, 11, o.protocol, 1), e.firstCounter(t)), false)
	_ = waitErr(t, done)
	return sends - sendsBefore, acks - acksBefore
}

func TestMessageExchangeLivenessKickOnDuplicate(t *testing.T) {
	t.Parallel()
	t.Run("retransmits a BDX message when the peer re-asks for what it answers", func(t *testing.T) {
		t.Parallel()
		if r, _ := sendThenDuplicate(t, kickOptions{protocol: bdxProtocolID, gap: 7100 * time.Millisecond}); r != 1 {
			t.Errorf("retransmissions = %d, want 1", r)
		}
	})
	t.Run("lets the retransmission carry the ack instead of sending a standalone one", func(t *testing.T) {
		t.Parallel()
		if _, a := sendThenDuplicate(t, kickOptions{protocol: bdxProtocolID, gap: 7100 * time.Millisecond}); a != 0 {
			t.Errorf("standalone acks = %d, want 0", a)
		}
	})
	for _, tc := range []struct {
		name string
		o    kickOptions
	}{
		{"does not retransmit before the schedule's own cadence has elapsed", kickOptions{protocol: bdxProtocolID, gap: 6500 * time.Millisecond}},
		{"does not retransmit when the peer can stay awake for the remaining wait", kickOptions{protocol: bdxProtocolID, gap: 7100 * time.Millisecond, timing: slowPeer(500)}},
		{"acks rather than retransmitting once the retransmission budget is down to its last attempt", kickOptions{protocol: bdxProtocolID, gap: 7100 * time.Millisecond, drainToRemaining: 1}},
		{"does not retransmit for a non-BDX protocol", kickOptions{protocol: mrp.SecureChannelProtocolID, gap: 7100 * time.Millisecond}},
		{"does not retransmit for a duplicate of an unrelated message", kickOptions{protocol: bdxProtocolID, gap: 7100 * time.Millisecond, unrelated: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if r, a := sendThenDuplicate(t, tc.o); r != 0 || a != 1 {
				t.Errorf("retransmissions = %d, standalone acks = %d, want 0 and 1", r, a)
			}
		})
	}
}

func TestMessageExchangeRetransmissionTimersDoNotOutliveTheirMessage(t *testing.T) {
	t.Parallel()
	t.Run("stops retransmitting when the ack arrives while a transmission is in flight", func(t *testing.T) {
		t.Parallel()
		e := mxNew(t, false, &sessionparams.Parameters{SessionIdleInterval: 1000, SessionActiveInterval: 1000, SessionActiveThreshold: 4000})
		release, inFlight := make(chan struct{}), make(chan struct{})
		e.conn.hook = func(n int) {
			if n == 2 { // hold the first retransmission open
				close(inFlight)
				<-release
			}
		}
		x := e.exchange(t, mrp.SecureChannelProtocolID)
		done := sendAsync(x, 1, nil, SendOptions{})
		e.conn.waitSent(t, 1)
		_ = x.HasUnackedMessage() // the timer is armed
		advanced := make(chan struct{})
		go func() { e.clock.Advance(20 * time.Second); close(advanced) }()
		<-inFlight
		x.onMessageReceived(acking(inbound(x, 11, mrp.SecureChannelProtocolID, 1), e.firstCounter(t)), false)
		if x.HasUnackedMessage() {
			t.Fatal("still unacked")
		}
		close(release)
		<-advanced
		if err := waitErr(t, done); err != nil {
			t.Fatal(err)
		}
		for range 10 {
			e.clock.Advance(60 * time.Second)
		}
		if n := len(e.conn.datagrams()); n != 2 {
			t.Errorf("datagrams = %d, want 2", n)
		}
	})
}

// retransmissionsWithin is MessageExchangeTest.ts:414-469 without the
// network-profile delay.
func retransmissionsWithin(t *testing.T, protocol uint16, window time.Duration, timing *sessionparams.Parameters) int {
	t.Helper()
	e := mxNew(t, false, timing)
	// SLEEPY's activeThreshold 0 keeps the peer idle; a zero threshold is
	// "not stated" in Go (the 4 s fallback), so the peer goes idle by
	// staying silent past it.
	e.clock.Advance(5 * time.Second)
	x := e.exchange(t, protocol)
	done := sendAsync(x, 1, nil, SendOptions{})
	e.conn.waitSent(t, 1)
	_ = x.HasUnackedMessage()
	for elapsed := time.Duration(0); elapsed < window; elapsed += 500 * time.Millisecond {
		e.clock.Advance(500 * time.Millisecond)
	}
	sends, _ := e.counts(t)
	x.onMessageReceived(acking(inbound(x, 11, protocol, 1), e.firstCounter(t)), false)
	_ = waitErr(t, done)
	return sends - 1
}

func TestMessageExchangeBDXRetransmissionIntervalCap(t *testing.T) {
	t.Parallel()
	// SLEEPY, MessageExchangeTest.ts:501.
	sleepy := &sessionparams.Parameters{SessionIdleInterval: 20000, SessionActiveInterval: 300}
	t.Run("caps a slow peer's idle cadence too, since a peer mid-transfer is not idle", func(t *testing.T) {
		t.Parallel()
		if r := retransmissionsWithin(t, bdxProtocolID, 8*time.Second, sleepy); r != 1 {
			t.Errorf("retransmissions = %d, want 1", r)
		}
	})
	t.Run("leaves a non-BDX slow peer on its idle cadence", func(t *testing.T) {
		t.Parallel()
		if r := retransmissionsWithin(t, mrp.SecureChannelProtocolID, 8*time.Second, sleepy); r != 0 {
			t.Errorf("retransmissions = %d, want 0", r)
		}
	})
}

func TestMessageExchangeActivityTracking(t *testing.T) {
	t.Parallel()
	t.Run("updates lastActive on receive", func(t *testing.T) {
		t.Parallel()
		e := mxNew(t, false, nil)
		x := e.exchange(t, mrp.SecureChannelProtocolID)
		before := x.lastActivity()
		e.clock.Advance(time.Second)
		x.onMessageReceived(inbound(x, 1, mrp.SecureChannelProtocolID, 1), false)
		if got := x.lastActivity(); !got.Equal(before.Add(time.Second)) {
			t.Errorf("lastActive = %v, want %v", got, before.Add(time.Second))
		}
	})
	t.Run("updates lastActive on send", func(t *testing.T) {
		t.Parallel()
		e := mxNew(t, false, nil)
		x := e.exchange(t, mrp.SecureChannelProtocolID)
		before := x.lastActivity()
		e.clock.Advance(2 * time.Second)
		if err := x.Send(context.Background(), 0, nil, SendOptions{SuppressAck: true, DisableMRP: true}); err != nil {
			t.Fatal(err)
		}
		if got := x.lastActivity(); !got.Equal(before.Add(2 * time.Second)) {
			t.Errorf("lastActive = %v, want %v", got, before.Add(2*time.Second))
		}
	})
}

func TestMessageExchangeCloseNotification(t *testing.T) {
	t.Parallel()
	t.Run("emits closed once when destroyed after the exchange completed its close", func(t *testing.T) {
		t.Parallel()
		// Go reports the end through Done (destroy is Abort); a second close
		// must neither reopen nor close it again.
		e := mxNew(t, false, nil)
		x := e.exchange(t, mrp.SecureChannelProtocolID)
		if err := x.Send(context.Background(), 0, nil, SendOptions{SuppressAck: true, DisableMRP: true}); err != nil {
			t.Fatal(err)
		}
		x.Close()
		<-x.Done()
		x.Abort(nil)
		select {
		case <-x.Done():
		default:
			t.Error("Done reopened")
		}
		e.m.mu.Lock()
		registered := e.m.exchanges[x.index] != nil
		e.m.mu.Unlock()
		if registered {
			t.Error("exchange still registered")
		}
	})
}

func TestMessageExchangeCrossProtocolStatusReport(t *testing.T) {
	t.Parallel()
	t.Run("accepts SecureChannel StatusReport on a non-SecureChannel exchange and delivers it to the consumer", func(t *testing.T) {
		t.Parallel()
		e := mxNew(t, false, nil)
		x := e.exchange(t, bdxProtocolID)
		report := []byte{0x01, 0x00, 0x02, 0x00, 0x00, 0x00, 0x1f, 0x00} // MessageExchangeTest.ts:619
		msg := inbound(x, 1, mrp.SecureChannelProtocolID, mrp.SCOpcodeStatusReport)
		msg.Payload = report
		x.onMessageReceived(msg, false)
		got, err := x.Next(context.Background(), ReceiveOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if got.ProtocolID != mrp.SecureChannelProtocolID || got.Opcode != mrp.SCOpcodeStatusReport || !bytes.Equal(got.Payload, report) {
			t.Errorf("delivered %+v", got)
		}
	})
	t.Run("still rejects non-StatusReport SecureChannel messages on a non-SecureChannel exchange", func(t *testing.T) {
		t.Parallel()
		// matter.js throws MatterFlowError; the Go receive path drops it.
		e := mxNew(t, false, nil)
		x := e.exchange(t, bdxProtocolID)
		x.onMessageReceived(inbound(x, 1, mrp.SecureChannelProtocolID, 0x20), false) // PBKDFParamRequest
		x.mu.Lock()
		queued := len(x.queue)
		x.mu.Unlock()
		if queued != 0 {
			t.Error("message delivered")
		}
	})
	for _, tc := range []struct {
		name         string
		opcode       uint8
		payload      []byte
		wantProtocol uint16
	}{
		{"stamps the SecureChannel protocol id on outgoing StatusReports from a non-SecureChannel exchange", mrp.SCOpcodeStatusReport, []byte{0, 0, 2, 0, 0, 0, 0, 0}, mrp.SecureChannelProtocolID},
		{"keeps the exchange's protocol id on non-StatusReport outgoing messages", 0x10, []byte{0}, bdxProtocolID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := mxNew(t, false, nil)
			x := e.exchange(t, bdxProtocolID)
			if err := x.Send(context.Background(), tc.opcode, tc.payload, SendOptions{SuppressAck: true, DisableMRP: true}); err != nil {
				t.Fatal(err)
			}
			out := e.conn.datagrams()
			if len(out) != 1 {
				t.Fatalf("datagrams = %d", len(out))
			}
			if d := e.peer.decode(t, out[0]); d.proto.ProtocolID != tc.wantProtocol || d.proto.Opcode != tc.opcode {
				t.Errorf("sent protocol %d opcode 0x%02x", d.proto.ProtocolID, d.proto.Opcode)
			}
		})
	}
}

func TestMessageExchangeAckWaitAbort(t *testing.T) {
	t.Parallel()
	fired := func() chan struct{} { c := make(chan struct{}); close(c); return c }
	t.Run("transmits once despite an abort that already fired and does not wait for the ack", func(t *testing.T) {
		t.Parallel()
		e := mxNew(t, false, nil)
		x := e.exchange(t, mrp.SecureChannelProtocolID)
		if err := x.Send(context.Background(), 1, nil, SendOptions{AckWaitAbort: fired()}); err != nil {
			t.Fatal(err)
		}
		if n := len(e.conn.datagrams()); n != 1 {
			t.Errorf("datagrams = %d, want 1", n)
		}
	})
	t.Run("transmits despite an abort that fires while the message counter is allocated", func(t *testing.T) {
		t.Parallel()
		// Go allocates the counter while framing; the abort fires during the
		// transmission, the same point before the ack wait.
		e := mxNew(t, false, nil)
		abort := make(chan struct{})
		e.conn.hook = func(int) { close(abort) }
		x := e.exchange(t, mrp.SecureChannelProtocolID)
		if err := x.Send(context.Background(), 1, nil, SendOptions{AckWaitAbort: abort}); err != nil {
			t.Fatal(err)
		}
		if n := len(e.conn.datagrams()); n != 1 {
			t.Errorf("datagrams = %d, want 1", n)
		}
	})
	t.Run("ends the wait for the ack without retransmitting and resolves", func(t *testing.T) {
		t.Parallel()
		e := mxNew(t, false, nil)
		x := e.exchange(t, mrp.SecureChannelProtocolID)
		abort := make(chan struct{})
		done := sendAsync(x, 1, nil, SendOptions{AckWaitAbort: abort})
		e.conn.waitSent(t, 1)
		_ = x.HasUnackedMessage()
		close(abort)
		if err := waitErr(t, done); err != nil {
			t.Fatal(err)
		}
		e.clock.Advance(30 * time.Second)
		if n := len(e.conn.datagrams()); n != 1 {
			t.Errorf("datagrams = %d, want 1", n)
		}
	})
	t.Run("does not retransmit after the ack wait ended during a retransmission in flight", func(t *testing.T) {
		t.Parallel()
		e := mxNew(t, false, nil)
		retransmitted, released := make(chan struct{}), make(chan struct{})
		e.conn.hook = func(n int) {
			if n == 2 {
				close(retransmitted)
				<-released
			}
		}
		x := e.exchange(t, mrp.SecureChannelProtocolID)
		abort := make(chan struct{})
		done := sendAsync(x, 1, nil, SendOptions{AckWaitAbort: abort})
		e.conn.waitSent(t, 1)
		_ = x.HasUnackedMessage()
		advanced := make(chan struct{})
		go func() { e.clock.Advance(3 * time.Second); close(advanced) }()
		<-retransmitted
		close(abort)
		if err := waitErr(t, done); err != nil {
			t.Fatal(err)
		}
		other := inbound(x, 7, mrp.SecureChannelProtocolID, 1)
		other.RequiresAck = true
		x.sendStandaloneAckFor(other)
		close(released)
		<-advanced
		for range 20 {
			e.clock.Advance(3 * time.Second)
		}
		out := e.conn.datagrams()
		transmissions := 0
		for _, b := range out {
			if e.peer.decode(t, b).proto.Opcode == 1 {
				transmissions++
			}
		}
		if transmissions != 2 || len(out) != 3 {
			t.Errorf("transmissions = %d of %d datagrams, want 2 of 3", transmissions, len(out))
		}
	})
	t.Run("does not transmit when an abort that already fired may stop the transmission", func(t *testing.T) {
		t.Parallel()
		// matter.js's abort is the Go ctx.
		e := mxNew(t, false, nil)
		x := e.exchange(t, mrp.SecureChannelProtocolID)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := x.Send(ctx, 1, nil, SendOptions{}); !errors.Is(err, context.Canceled) {
			t.Fatalf("Send = %v, want context.Canceled", err)
		}
		if n := len(e.conn.datagrams()); n != 0 {
			t.Errorf("datagrams = %d, want 0", n)
		}
	})
}

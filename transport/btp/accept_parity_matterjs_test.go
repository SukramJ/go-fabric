// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package btp

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock"
)

// The peripheral's GATT server, Accept, follows matter.js
// packages/nodejs-ble/src/BlenoBleServer.ts at db021da0. matter.js has no
// test of that file at the pin (packages/nodejs-ble/test holds only the
// central's NobleBleChannelTest.ts and ScanControlTest.ts), so these cases
// are written from the server's lines, each named where it asserts.

// fakeGATT is a peripheral's GATT connection the test drives: it calls
// the registered handlers itself, records indications and confirms them
// at once, or when the test releases them (hold).
type fakeGATT struct {
	mu          sync.Mutex
	onWrite     func([]byte)
	onSub       func(int)
	onUnsub     func()
	indications [][]byte
	hold        chan struct{}
	disconnects int
	gone        chan struct{}
	goneOnce    sync.Once
}

func newFakeGATT() *fakeGATT { return &fakeGATT{gone: make(chan struct{})} }

func (f *fakeGATT) Remote() string { return "central-1" }

func (f *fakeGATT) OnWrite(h func([]byte)) { f.mu.Lock(); f.onWrite = h; f.mu.Unlock() }

func (f *fakeGATT) OnSubscribe(h func(int)) { f.mu.Lock(); f.onSub = h; f.mu.Unlock() }

func (f *fakeGATT) OnUnsubscribe(h func()) { f.mu.Lock(); f.onUnsub = h; f.mu.Unlock() }

func (f *fakeGATT) Indicate(ctx context.Context, value []byte) error {
	f.mu.Lock()
	f.indications = append(f.indications, append([]byte(nil), value...))
	hold := f.hold
	f.mu.Unlock()
	if hold == nil {
		return nil
	}
	select {
	case <-hold:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-f.gone:
		return errors.New("fake: gone")
	}
}

func (f *fakeGATT) Disconnect() error {
	f.mu.Lock()
	f.disconnects++
	f.mu.Unlock()
	f.goneOnce.Do(func() { close(f.gone) })
	return nil
}

func (f *fakeGATT) Disconnected() <-chan struct{} { return f.gone }

func (f *fakeGATT) write(b []byte) {
	f.mu.Lock()
	h := f.onWrite
	f.mu.Unlock()
	h(b)
}

func (f *fakeGATT) subscribe(attMTU int) {
	f.mu.Lock()
	h := f.onSub
	f.mu.Unlock()
	h(attMTU)
}

func (f *fakeGATT) unsubscribe() {
	f.mu.Lock()
	h := f.onUnsub
	f.mu.Unlock()
	h()
}

func (f *fakeGATT) indicated() [][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]byte(nil), f.indications...)
}

func (f *fakeGATT) isGone() bool {
	select {
	case <-f.gone:
		return true
	default:
		return false
	}
}

// opened records the connections Accept opened and the messages each
// received.
type opened struct {
	mu    sync.Mutex
	conns []*Conn
	msgs  [][]byte
}

func (o *opened) open(c *Conn) func([]byte) {
	o.mu.Lock()
	o.conns = append(o.conns, c)
	o.mu.Unlock()
	return func(m []byte) {
		o.mu.Lock()
		o.msgs = append(o.msgs, append([]byte(nil), m...))
		o.mu.Unlock()
	}
}

func (o *opened) count() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.conns)
}

func (o *opened) conn(t *testing.T) *Conn {
	t.Helper()
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.conns) != 1 {
		t.Fatalf("%d sessions opened, want 1", len(o.conns))
	}
	return o.conns[0]
}

// deadline is the test's 5-second bound on a wait.
func deadline(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func handshakeRequest(attMTU uint16) []byte {
	return EncodeHandshakeRequest(HandshakeRequest{Versions: []int{4}, ATTMTU: attMTU, ClientWindowSize: 5})
}

func acceptFake(t *testing.T, clk *fakeClock) (*fakeGATT, *opened) {
	t.Helper()
	f, o := newFakeGATT(), &opened{}
	if err := Accept(f, o.open, Options{TimeSource: clk}); err != nil {
		t.Fatal(err)
	}
	return f, o
}

// TestAcceptRefusesNil: no connection, no open function.
func TestAcceptRefusesNil(t *testing.T) {
	if err := Accept(nil, (&opened{}).open, Options{}); !errors.Is(err, ErrNoConnection) {
		t.Fatalf("nil connection: %v", err)
	}
	if err := Accept(newFakeGATT(), nil, Options{}); !errors.Is(err, ErrNoConnection) {
		t.Fatalf("nil open: %v", err)
	}
}

// TestAcceptHandshakeThenSubscribeParityMatterJS: the handshake request on
// C1 is kept and not answered (:256-263); the C2 subscription starts the
// session at min(ATT_MTU-3, offered, 244) (:299-300) with the response as
// the first indication, opens it, and later C1 writes feed it (:266-270).
func TestAcceptHandshakeThenSubscribeParityMatterJS(t *testing.T) {
	clk := newFakeClock()
	f, o := acceptFake(t, clk)
	f.write(handshakeRequest(100))
	if len(f.indicated()) != 0 || o.count() != 0 {
		t.Fatal("the handshake request was answered before the subscription")
	}
	f.subscribe(50)
	ind := f.indicated()
	if len(ind) != 1 {
		t.Fatalf("%d indications after the subscription, want the handshake response", len(ind))
	}
	resp, err := DecodeHandshakeResponse(ind[0])
	if err != nil {
		t.Fatal(err)
	}
	if resp.ATTMTU != 47 || resp.WindowSize != 5 || resp.Version != 4 {
		t.Fatalf("response = %+v, want ATT_MTU 47 (50-3), window 5, version 4", resp)
	}
	conn := o.conn(t)
	if conn.Session().FragmentSize() != 47 || conn.Remote().String() != "central-1" || !conn.Reliable() {
		t.Fatalf("conn: fragment %d, remote %s", conn.Session().FragmentSize(), conn.Remote())
	}
	// One Matter message in one segment, acknowledging the response (seq 0).
	f.write(mustEncode(t, Packet{
		Header:  Header{HasAckNumber: true, IsBeginningSegment: true, IsEndingSegment: true},
		Payload: Payload{AckNumber: u8(0), SequenceNumber: 0, MessageLength: u16(3), SegmentPayload: []byte{1, 2, 3}},
	}))
	o.mu.Lock()
	msgs := o.msgs
	o.mu.Unlock()
	if len(msgs) != 1 || !bytes.Equal(msgs[0], []byte{1, 2, 3}) {
		t.Fatalf("messages = %x", msgs)
	}
	// The subscription stopped the handshake timer (:297).
	clk.Advance(ConnRspTimeout)
	if f.isGone() {
		t.Fatal("the handshake timer dropped an established connection")
	}
}

// TestAcceptRefusesSubscribeBeforeHandshakeParityMatterJS: a subscription
// before a handshake request starts nothing (:289-291); the handshake that
// follows and a new subscription do.
func TestAcceptRefusesSubscribeBeforeHandshakeParityMatterJS(t *testing.T) {
	f, o := acceptFake(t, newFakeClock())
	f.subscribe(100)
	if len(f.indicated()) != 0 || o.count() != 0 || f.isGone() {
		t.Fatal("a subscription before the handshake request started a session or dropped the connection")
	}
	f.write(handshakeRequest(100))
	f.subscribe(100)
	if o.count() != 1 {
		t.Fatalf("%d sessions after handshake and subscription, want 1", o.count())
	}
}

// TestAcceptRefusesSecondSessionParityMatterJS: a second handshake and
// subscription on a running session start no second one (:292-296).
func TestAcceptRefusesSecondSessionParityMatterJS(t *testing.T) {
	clk := newFakeClock()
	f, o := acceptFake(t, clk)
	f.write(handshakeRequest(100))
	f.subscribe(100)
	f.write(handshakeRequest(100))
	f.subscribe(100)
	if o.count() != 1 || len(f.indicated()) != 1 {
		t.Fatalf("%d sessions, %d indications, want one of each", o.count(), len(f.indicated()))
	}
	// The kept second request restarted the timer, which the refused
	// subscription did not stop: matter.js drops the connection (its TODO
	// at :264).
	clk.Advance(ConnRspTimeout)
	if !f.isGone() {
		t.Fatal("a handshake request on a running session left the connection after the timeout")
	}
}

// TestAcceptHandshakeTimeoutParityMatterJS: a central that does not
// subscribe within BTP_CONN_RSP_TIMEOUT (15 s, BleConsts.ts:56) after its
// handshake request is disconnected (:148-150, :258, :413-416); a second
// request restarts the timer.
func TestAcceptHandshakeTimeoutParityMatterJS(t *testing.T) {
	clk := newFakeClock()
	f, o := acceptFake(t, clk)
	f.write(handshakeRequest(100))
	clk.Advance(10 * time.Second)
	f.write(handshakeRequest(100))
	clk.Advance(10 * time.Second)
	if f.isGone() {
		t.Fatal("disconnected 10 s after the second request")
	}
	clk.Advance(5 * time.Second)
	if !f.isGone() {
		t.Fatal("still connected 15 s after the handshake request")
	}
	f.subscribe(100)
	if o.count() != 0 {
		t.Fatal("a subscription after the timeout started a session")
	}
}

// TestAcceptUnsubscribeClosesParityMatterJS: the end of the C2
// subscription closes the server — the connection dropped, the session
// closed (:81-84, :461-475).
func TestAcceptUnsubscribeClosesParityMatterJS(t *testing.T) {
	f, o := acceptFake(t, newFakeClock())
	f.write(handshakeRequest(100))
	f.subscribe(100)
	conn := o.conn(t)
	f.unsubscribe()
	if !f.isGone() {
		t.Fatal("the unsubscription left the connection")
	}
	select {
	case <-conn.Closed():
	case <-deadline(t).Done():
		t.Fatal("the unsubscription left the session open")
	}
}

// TestAcceptDisconnectClosesParityMatterJS: the platform's disconnect
// closes the session (:195-213).
func TestAcceptDisconnectClosesParityMatterJS(t *testing.T) {
	f, o := acceptFake(t, newFakeClock())
	f.write(handshakeRequest(100))
	f.subscribe(100)
	conn := o.conn(t)
	f.goneOnce.Do(func() { close(f.gone) })
	select {
	case <-conn.Closed():
	case <-deadline(t).Done():
		t.Fatal("the disconnect left the session open")
	}
	if err := conn.Send([]byte{1}); err == nil {
		t.Fatal("a send on the closed session succeeded")
	}
}

// TestAcceptSessionCloseDisconnectsParityMatterJS: the session's own close
// — here a protocol violation, a sequence number out of order — drops the
// connection through its disconnect callback (:312-313).
func TestAcceptSessionCloseDisconnectsParityMatterJS(t *testing.T) {
	f, _ := acceptFake(t, newFakeClock())
	f.write(handshakeRequest(100))
	f.subscribe(100)
	f.write(mustEncode(t, Packet{
		Header:  Header{IsBeginningSegment: true, IsEndingSegment: true},
		Payload: Payload{SequenceNumber: 7, MessageLength: u16(1), SegmentPayload: []byte{1}},
	}))
	waitFor(t, f.isGone)
}

// TestAcceptRefusesDataWithoutSessionParityMatterJS: a C1 write that is no
// handshake request, without a session, is refused and changes nothing
// (:271-274).
func TestAcceptRefusesDataWithoutSessionParityMatterJS(t *testing.T) {
	f, o := acceptFake(t, newFakeClock())
	f.write([]byte{0x05, 0x00, 0x01, 0x00, 0x01})
	if f.isGone() || o.count() != 0 || len(f.indicated()) != 0 {
		t.Fatal("a data write without a session changed the connection")
	}
}

// TestAcceptIndicationWaitsForConfirmationParityMatterJS: an indication
// completes with the central's confirmation (:304-310, :329-336): the
// session is opened only once its handshake response was confirmed, and
// a connection that drops while the indication waits releases it.
func TestAcceptIndicationWaitsForConfirmationParityMatterJS(t *testing.T) {
	f, o := acceptFake(t, newFakeClock())
	hold := make(chan struct{})
	f.mu.Lock()
	f.hold = hold
	f.mu.Unlock()
	f.write(handshakeRequest(100))
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.subscribe(100)
	}()
	waitFor(t, func() bool { return len(f.indicated()) == 1 })
	select {
	case <-done:
		t.Fatal("the subscription completed before the response was confirmed")
	default:
	}
	if o.count() != 0 {
		t.Fatal("the session opened before its response was confirmed")
	}
	close(hold)
	<-done
	if o.count() != 1 {
		t.Fatal("the confirmed response opened no session")
	}

	// An unconfirmed indication of a connection that drops returns.
	f2, o2 := acceptFake(t, newFakeClock())
	f2.mu.Lock()
	f2.hold = make(chan struct{})
	f2.mu.Unlock()
	f2.write(handshakeRequest(100))
	done2 := make(chan struct{})
	go func() {
		defer close(done2)
		f2.subscribe(100)
	}()
	waitFor(t, func() bool { return len(f2.indicated()) == 1 })
	f2.goneOnce.Do(func() { close(f2.gone) })
	select {
	case <-done2:
	case <-deadline(t).Done():
		t.Fatal("the indication kept waiting after the connection dropped")
	}
	if o2.count() != 0 {
		t.Fatal("an unconfirmed response opened a session")
	}
}

// heldClock is a fake clock whose AfterFunc callbacks the test runs by
// hand, in the order it chooses.
type heldClock struct {
	*fakeClock
	mu  sync.Mutex
	fns []func()
}

func (c *heldClock) AfterFunc(d time.Duration, f func()) clock.Timer {
	c.mu.Lock()
	c.fns = append(c.fns, f)
	c.mu.Unlock()
	return c.fakeClock.AfterFunc(d, func() {})
}

func (c *heldClock) fn(i int) func() {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.fns[i]
}

// TestAcceptStaleTimerKeepsTheNewOne: the callback of a handshake timer
// that fired while a repeated request restarted the timer (it waited on
// the lock) leaves the new timer armed, which then drops the connection.
func TestAcceptStaleTimerKeepsTheNewOne(t *testing.T) {
	clk := &heldClock{fakeClock: newFakeClock()}
	g := newFakeGATT()
	if err := Accept(g, (&opened{}).open, Options{TimeSource: clk}); err != nil {
		t.Fatal(err)
	}
	g.write(handshakeRequest(100)) // timer A
	g.write(handshakeRequest(100)) // timer B replaces A
	clk.fn(0)()                    // A's callback, stale
	if g.isGone() {
		t.Fatal("the stale timer dropped the connection")
	}
	clk.fn(1)() // B fires
	if !g.isGone() {
		t.Fatal("the new timer did not drop the connection after the stale one ran")
	}
}

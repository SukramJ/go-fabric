// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bdx_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/bdx"
)

// arrivalEnd is a [bdx.ReceiveNotifier] the test feeds message by message,
// in the order a Matter exchange takes one: the notifier, then the
// acknowledgement of the pending send, then the delivery queue
// (client/exchange onMessageReceived). Its Send of a BlockEof waits for
// the BlockAckEof that acknowledges it, as an MRP send waits for the ack
// the receiver piggybacks on its reply.
type arrivalEnd struct {
	in     chan bdx.Message
	out    chan bdx.Message
	notify atomic.Pointer[func(bdx.Message)]
	acked  chan struct{}
	once   sync.Once
	// hold, when set, keeps Receive from reading until it is closed: the
	// flow's goroutine held before it takes the queued BlockAckEof.
	hold chan struct{}
}

func newArrivalEnd() *arrivalEnd {
	return &arrivalEnd{in: make(chan bdx.Message, 8), out: make(chan bdx.Message, 8), acked: make(chan struct{})}
}

func (e *arrivalEnd) OnReceive(fn func(bdx.Message)) { e.notify.Store(&fn) }

// arrive is the receive path: notifier, ack, queue.
func (e *arrivalEnd) arrive(m bdx.Message) {
	if fn := e.notify.Load(); fn != nil {
		(*fn)(m)
	}
	if m.ProtocolID == bdx.ProtocolID && bdx.MessageType(m.Opcode) == bdx.MessageBlockAckEOF {
		e.once.Do(func() { close(e.acked) })
	}
	e.in <- m
}

func (e *arrivalEnd) Send(ctx context.Context, protocolID uint16, opcode uint8, payload []byte) error {
	e.out <- bdx.Message{ProtocolID: protocolID, Opcode: opcode, Payload: append([]byte(nil), payload...)}
	if protocolID != bdx.ProtocolID || bdx.MessageType(opcode) != bdx.MessageBlockEOF {
		return nil
	}
	select {
	case <-e.acked:
		return nil
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}

func (e *arrivalEnd) Receive(ctx context.Context) (bdx.Message, error) {
	if e.hold != nil {
		select {
		case <-e.hold:
		case <-ctx.Done():
			return bdx.Message{}, ctx.Err()
		}
	}
	select {
	case m := <-e.in:
		return m, nil
	case <-ctx.Done():
		return bdx.Message{}, ctx.Err()
	}
}

func (e *arrivalEnd) MaxPayloadSize() int { return 1178 }
func (e *arrivalEnd) Close() error        { return nil }

var _ bdx.ReceiveNotifier = (*arrivalEnd)(nil)

// flowEvents records a transfer's hooks in order.
type flowEvents struct {
	mu     sync.Mutex
	events []string
	total  uint64
	// queued is the length of the delivery queue when finished ran.
	queued int
}

func (r *flowEvents) add(ev string) {
	r.mu.Lock()
	r.events = append(r.events, ev)
	r.mu.Unlock()
}

func (r *flowEvents) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.events...)
}

// serveSenderDrive serves a ReceiveInit offering sender drive on e — the
// responder as the driving sender, the OTA provider's role — and returns
// the result channel.
func serveSenderDrive(ctx context.Context, e *arrivalEnd, data []byte, rec *flowEvents) <-chan error {
	done := make(chan error, 1)
	go func() {
		_, err := bdx.Serve(ctx, bdx.NewMessenger(e, 0), bdx.ServeOptions{
			InitType: bdx.MessageReceiveInit,
			Init: bdx.Init{
				TransferControl: bdx.TransferControl{Version: bdx.Version, SenderDrive: true},
				MaxBlockSize:    1024, FileDesignator: []byte("ota/f"),
			},
			TransferFileDesignator: bdx.FileDesignator("ota/f"),
			Open:                   blobOpener(data),
			Progress: func(n uint64, _ *uint64) {
				rec.add("progress")
				rec.mu.Lock()
				rec.total = n
				rec.mu.Unlock()
			},
			Finished: func(uint64) {
				rec.mu.Lock()
				rec.queued = len(e.in)
				rec.mu.Unlock()
				rec.add("finished")
			},
			Cancelled: func() { rec.add("cancelled") },
		})
		done <- err
	}()
	return done
}

// awaitSent returns the next message the flow sent of type t, failing on
// another.
func awaitSent(t *testing.T, e *arrivalEnd, want bdx.MessageType) bdx.Message {
	t.Helper()
	select {
	case m := <-e.out:
		if m.ProtocolID != bdx.ProtocolID || bdx.MessageType(m.Opcode) != want {
			t.Fatalf("flow sent %d/%d, want %s", m.ProtocolID, m.Opcode, want)
		}
		return m
	case <-time.After(5 * time.Second):
		t.Fatalf("flow did not send %s", want)
		return bdx.Message{}
	}
}

func ackEOF(counter uint32) bdx.Message {
	return bdx.Message{ProtocolID: bdx.ProtocolID, Opcode: uint8(bdx.MessageBlockAckEOF), Payload: bdx.CounterOnly{BlockCounter: counter}.Encode()}
}

func awaitDone(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return")
		return nil
	}
}

// TestBlockAckEOFFinishesAtArrival: the BlockAckEof for the final counter
// finishes the transfer when it arrives, on the receive path — the
// finished hook has run when the arrival returns, before the ack reached
// the delivery queue Receive reads (chip TransferFacilitator.cpp:61 at
// 6170af84, the state step on receive) — after the final progress, as
// matter.js's processTransfer orders them (flow/Flow.ts:135, :139-141 at
// db021da0). The flow then takes the queued ack and returns; finished
// fires once.
func TestBlockAckEOFFinishesAtArrival(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		e := newArrivalEnd()
		data := testData(300)
		var rec flowEvents
		done := serveSenderDrive(ctx, e, data, &rec)
		awaitSent(t, e, bdx.MessageReceiveAccept)
		awaitSent(t, e, bdx.MessageBlockEOF)

		e.arrive(ackEOF(0))
		if got := rec.snapshot(); len(got) == 0 || got[len(got)-1] != "finished" {
			t.Fatalf("hooks when the arrival returned %v, want finished last", got)
		}
		if err := awaitDone(t, done); err != nil {
			t.Fatalf("Serve: %v", err)
		}
		want := []string{"progress", "progress", "finished"}
		if got := rec.snapshot(); !equalStrings(got, want) {
			t.Fatalf("hooks %v, want %v", got, want)
		}
		rec.mu.Lock()
		defer rec.mu.Unlock()
		if rec.queued != 0 || rec.total != uint64(len(data)) {
			t.Fatalf("finished saw %d queued messages, final progress %d; want 0 and %d", rec.queued, rec.total, len(data))
		}
	})
}

// TestScopeDisabledDuringTheEOFWaitIsCancelled: a scope disabled while the
// flow waits for the final acknowledgement — still in the BlockEof's send,
// or reading the BlockAckEof — ends the transfer cancelled, never
// finished: matter.js's processTransfer reports a closed flow with
// progressCancelled (flow/Flow.ts:137-138 at db021da0).
func TestScopeDisabledDuringTheEOFWaitIsCancelled(t *testing.T) {
	t.Parallel()
	t.Run("in the send", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancelCause(context.Background())
			defer cancel(nil)
			e := newArrivalEnd()
			var rec flowEvents
			done := serveSenderDrive(ctx, e, testData(300), &rec)
			awaitSent(t, e, bdx.MessageReceiveAccept)
			awaitSent(t, e, bdx.MessageBlockEOF)
			cancel(bdx.ErrScopeDisabled)
			if err := awaitDone(t, done); !errors.Is(err, bdx.ErrScopeDisabled) {
				t.Fatalf("Serve: %v, want the disabled scope", err)
			}
			if got := rec.snapshot(); !equalStrings(got, []string{"progress", "cancelled"}) {
				t.Fatalf("hooks %v, want cancelled and no finished", got)
			}
		})
	})
	t.Run("in the read", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancelCause(context.Background())
			defer cancel(nil)
			a, b := newPipe(1178)
			var rec flowEvents
			done := make(chan error, 1)
			go func() {
				_, err := bdx.Serve(ctx, bdx.NewMessenger(a, 0), bdx.ServeOptions{
					InitType: bdx.MessageReceiveInit,
					Init: bdx.Init{
						TransferControl: bdx.TransferControl{Version: bdx.Version, SenderDrive: true},
						MaxBlockSize:    1024, FileDesignator: []byte("ota/f"),
					},
					TransferFileDesignator: bdx.FileDesignator("ota/f"),
					Open:                   blobOpener(testData(300)),
					Progress:               func(uint64, *uint64) { rec.add("progress") },
					Finished:               func(uint64) { rec.add("finished") },
					Cancelled:              func() { rec.add("cancelled") },
				})
				done <- err
			}()
			for _, want := range []bdx.MessageType{bdx.MessageReceiveAccept, bdx.MessageBlockEOF} {
				m, err := b.Receive(ctx)
				if err != nil || bdx.MessageType(m.Opcode) != want {
					t.Fatalf("peer got %+v, %v; want %s", m, err, want)
				}
			}
			cancel(bdx.ErrScopeDisabled)
			if err := awaitDone(t, done); err == nil {
				t.Fatal("Serve succeeded with the scope disabled before the BlockAckEof")
			}
			if got := rec.snapshot(); len(got) == 0 || got[len(got)-1] != "cancelled" || contains(got, "finished") {
				t.Fatalf("hooks %v, want cancelled and no finished", got)
			}
		})
	})
}

// TestWrongBlockAckEOFCounterIsRefused: a BlockAckEof for another counter
// does not finish the transfer at its arrival; the read refuses it with
// BadBlockCounter as it always has (flow/OutboundFlow.ts:115-118,
// Flow.ts:103-111 at db021da0), and the transfer is cancelled.
func TestWrongBlockAckEOFCounterIsRefused(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		e := newArrivalEnd()
		var rec flowEvents
		done := serveSenderDrive(ctx, e, testData(300), &rec)
		awaitSent(t, e, bdx.MessageReceiveAccept)
		awaitSent(t, e, bdx.MessageBlockEOF)
		e.arrive(ackEOF(1))
		if got := rec.snapshot(); contains(got, "finished") {
			t.Fatalf("hooks %v after a wrong counter arrived", got)
		}
		err := awaitDone(t, done)
		var be *bdx.Error
		if !errors.As(err, &be) || be.Code != bdx.StatusBadBlockCounter {
			t.Fatalf("Serve: %v, want BadBlockCounter", err)
		}
		if got := rec.snapshot(); contains(got, "finished") || got[len(got)-1] != "cancelled" {
			t.Fatalf("hooks %v, want cancelled and no finished", got)
		}
	})
}

// TestTransferFinishedAtArrivalIsNoLongerActive: once the BlockAckEof
// finished the transfer at its arrival, the responder no longer counts the
// session as running, though its goroutine has not yet taken the queued
// ack: SessionFor finds none and a non-forced DisablePeerForScope — an OTA
// provider's ApplyUpdateRequest for a transfer it knows as completed —
// succeeds. matter.js closes the session in processTransfer's finally and
// removes it from the active sessions before anything else is dispatched
// (BdxSession.ts:197-198, BdxProtocol.ts:187-189 at db021da0).
func TestTransferFinishedAtArrivalIsNoLongerActive(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		r := bdx.NewResponder(nil)
		if !r.EnablePeerForScope(testPeer, "ota", memStorage{"img": testData(300)}, bdx.ScopeConfig{PreferredDriverModes: []bdx.DriverMode{bdx.SenderDrive}, MessageTimeout: time.Minute}) {
			t.Fatal("enable refused")
		}
		var rec flowEvents
		closed := make(chan struct{})
		off := r.OnSessionStarted(func(s *bdx.ServerSession, _ string) {
			s.OnFinished(func(uint64) { rec.add("finished") })
			s.OnCancelled(func() { rec.add("cancelled") })
			s.OnClosed(func() { close(closed) })
		})
		defer off()
		e := newArrivalEnd()
		e.hold = make(chan struct{})
		init := bdx.Init{TransferControl: bdx.TransferControl{SenderDrive: true}, MaxBlockSize: 1024, FileDesignator: []byte("ota/img")}
		payload, _ := init.Encode()
		served := make(chan struct{})
		go func() {
			r.HandleExchange(ctx, testPeer, bdx.Message{ProtocolID: bdx.ProtocolID, Opcode: uint8(bdx.MessageReceiveInit), Payload: payload}, e)
			close(served)
		}()
		awaitSent(t, e, bdx.MessageReceiveAccept)
		awaitSent(t, e, bdx.MessageBlockEOF)
		if r.SessionFor(testPeer, "ota") == nil {
			t.Fatal("no running session while the BlockEof waits for its ack")
		}
		e.arrive(ackEOF(0))
		if got := rec.snapshot(); !equalStrings(got, []string{"finished"}) {
			t.Fatalf("hooks %v when the arrival returned, want finished", got)
		}
		if s := r.SessionFor(testPeer, "ota"); s != nil {
			t.Fatal("the finished transfer still counts as running")
		}
		if err := r.DisablePeerForScope(testPeer, "ota", false); err != nil {
			t.Fatalf("non-forced disable after the finish: %v", err)
		}
		select {
		case <-closed:
			t.Fatal("session closed before its goroutine took the queued ack")
		default:
		}
		close(e.hold)
		select {
		case <-served:
		case <-time.After(5 * time.Second):
			t.Fatal("HandleExchange did not return")
		}
		<-closed
		if got := rec.snapshot(); !equalStrings(got, []string{"finished"}) {
			t.Fatalf("hooks %v, want finished alone", got)
		}
	})
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

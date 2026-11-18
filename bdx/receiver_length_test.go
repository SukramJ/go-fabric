// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bdx_test

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bdx"
)

// lengthSink counts what reaches it and how it was closed.
type lengthSink struct {
	n      int
	closed error
}

func (s *lengthSink) Write(p []byte) (int, error) { s.n += len(p); return len(p), nil }
func (s *lengthSink) Close(err error) error       { s.closed = err; return nil }

// sendBlocks answers the served SendInit's SendAccept with blocks of the
// given sizes, the last as BlockEof when eof is set.
func sendBlocks(ctx context.Context, t *testing.T, a *pipeEnd, eof bool, sizes ...int) {
	t.Helper()
	m := bdx.NewMessenger(a, time.Second)
	if _, _, err := m.Next(ctx, bdx.MessageSendAccept); err != nil {
		t.Fatalf("accept: %v", err)
	}
	for i, n := range sizes {
		blk := bdx.Block{BlockCounter: uint32(i), Data: testData(n)} //nolint:gosec // a few blocks
		var err error
		if eof && i == len(sizes)-1 {
			err = m.SendBlockEOF(ctx, blk)
		} else {
			err = m.SendBlock(ctx, blk)
		}
		if err != nil {
			return // the server refused an earlier block
		}
		if i < len(sizes)-1 {
			if _, err := m.ReadBlockAck(ctx); err != nil {
				return
			}
		}
	}
}

// TestServe_SendInitHoldsItsDefiniteLength (review II, R2-P6-bound-1908):
// a served SendInit's definite length is the length the transfer must
// meet — a block past it is refused with LengthTooLarge before it reaches
// the sink, and BlockEof short of it is LengthTooShort.
func TestServe_SendInitHoldsItsDefiniteLength(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		eof   bool
		sizes []int
		code  bdx.StatusCode
		sunk  int
	}{
		{"a block past the length", false, []int{11}, bdx.StatusLengthTooLarge, 0},
		{"the second block past it", false, []int{6, 6}, bdx.StatusLengthTooLarge, 6},
		{"BlockEof short of it", true, []int{5}, bdx.StatusLengthTooShort, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			a, b := newPipe(1178)
			sink := &lengthSink{}
			done := make(chan error, 1)
			go func() {
				_, err := bdx.Serve(ctx, bdx.NewMessenger(b, time.Second), bdx.ServeOptions{
					InitType: bdx.MessageSendInit,
					Init: bdx.Init{
						TransferControl: bdx.TransferControl{SenderDrive: true}, MaxBlockSize: 64,
						MaxLength: u64(10), FileDesignator: []byte("log/x"),
					},
					TransferFileDesignator: bdx.FileDesignator("log/x"), Sink: sink,
					PreferredDriverModes: []bdx.DriverMode{bdx.SenderDrive},
				})
				done <- err
			}()
			sendBlocks(ctx, t, a, tc.eof, tc.sizes...)
			expectBdxError(t, <-done, tc.code)
			if sink.n != tc.sunk || sink.closed == nil {
				t.Fatalf("the sink saw %d bytes (closed with %v), want %d and a discard", sink.n, sink.closed, tc.sunk)
			}
		})
	}
}

// lengthStorage is a SinkStorage whose sinks it keeps.
type lengthStorage struct{ sinks chan *lengthSink }

func (s lengthStorage) Open(context.Context, string) (bdx.Blob, error) { return nil, nil }
func (s lengthStorage) Create(context.Context, string) (bdx.Sink, error) {
	k := &lengthSink{}
	s.sinks <- k
	return k, nil
}

// TestResponder_ScopeMaxLength (review II, R2-P6-bound-1908): a scope's
// MaxLength refuses a SendInit announcing more before a sink is created,
// and ends a transfer without a definite length before a byte past it
// reaches the sink.
func TestResponder_ScopeMaxLength(t *testing.T) {
	t.Parallel()
	open := func(t *testing.T, length *uint64) (*pipeEnd, lengthStorage, context.Context) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		t.Cleanup(cancel)
		r := bdx.NewResponder(nil)
		st := lengthStorage{sinks: make(chan *lengthSink, 1)}
		if !r.EnablePeerForScope(testPeer, "log", st, bdx.ScopeConfig{PreferredDriverModes: []bdx.DriverMode{bdx.SenderDrive}, MessageTimeout: time.Second, MaxLength: 10}) {
			t.Fatal("enable refused")
		}
		a, b := newPipe(1178)
		init := bdx.Init{TransferControl: bdx.TransferControl{SenderDrive: true}, MaxBlockSize: 64, MaxLength: length, FileDesignator: []byte("log/x")}
		payload, _ := init.Encode()
		go r.HandleExchange(ctx, testPeer, bdx.Message{ProtocolID: bdx.ProtocolID, Opcode: uint8(bdx.MessageSendInit), Payload: payload}, b)
		return a, st, ctx
	}
	t.Run("an Init announcing more", func(t *testing.T) {
		t.Parallel()
		a, st, _ := open(t, u64(11))
		expectStatus(t, a, bdx.StatusLengthTooLarge)
		select {
		case <-st.sinks:
			t.Fatal("a sink was created")
		default:
		}
	})
	t.Run("an indefinite transfer past it", func(t *testing.T) {
		t.Parallel()
		a, st, ctx := open(t, nil)
		sendBlocks(ctx, t, a, false, 6, 6)
		sink := <-st.sinks
		expectStatus(t, a, bdx.StatusLengthTooLarge)
		if sink.n != 6 {
			t.Fatalf("the sink saw %d bytes, want the first block's 6", sink.n)
		}
	})
}

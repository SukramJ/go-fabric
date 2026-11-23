// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package tcp

import (
	"context"
	"encoding/binary"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// heapInUse is the live heap after a collection.
func heapInUse() uint64 {
	runtime.GC()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return ms.HeapAlloc
}

// waitFor polls cond for up to five seconds.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestConnectionAtItsInFlightBound drives one connection with messages of
// the largest payload while the handler of the first one does not return:
// up to [maxInFlightPerConn] messages are read and each gets its goroutine;
// the next one is not handed on, and the reader stops reading, so what the
// peer sends after it waits in the socket buffers. When the handler
// returns, every message is delivered in order. The test measures the
// goroutines and the live heap the connection holds at the bound and
// beyond it.
//
// Not parallel: it counts the process's goroutines.
func TestConnectionAtItsInFlightBound(t *testing.T) {
	const beyond = 16
	payload := DefaultMaxPayloadSize

	gate := make(chan struct{})
	var gateOnce sync.Once
	openGate := func() { gateOnce.Do(func() { close(gate) }) }
	t.Cleanup(openGate)
	var delivered atomic.Int64
	var orderMu sync.Mutex
	var order []uint32
	l, err := Listen(Config{LocalAddr: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() {
		served <- l.Serve(ctx, func(_ *Conn, msg []byte, _ func()) {
			seq := binary.LittleEndian.Uint32(msg)
			orderMu.Lock()
			order = append(order, seq)
			orderMu.Unlock()
			delivered.Add(1)
			if seq == 1 {
				<-gate // the first message after the warm-up holds its turn
			}
		}, nil)
	}()
	t.Cleanup(func() {
		openGate()
		cancel()
		if err := <-served; err != nil {
			t.Errorf("serve: %v", err)
		}
	})
	c := dialRaw(t, l)

	// One writer for the whole test, started before the baseline so its
	// goroutine is counted there.
	var written atomic.Int64
	frames := make(chan []byte, 128)
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for f := range frames {
			n, err := c.Write(f)
			written.Add(int64(n))
			if err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		close(frames)
		_ = c.Close()
		<-writerDone
	})
	frame := func(seq uint32) []byte {
		msg := make([]byte, payload)
		binary.LittleEndian.PutUint32(msg, seq)
		return frameOf(msg)
	}

	// Warm-up: the connection is accepted and served, its one message
	// handled and its goroutine gone.
	frames <- frame(0)
	waitFor(t, "the warm-up message", func() bool { return delivered.Load() == 1 })
	time.Sleep(50 * time.Millisecond)
	baseGoroutines := runtime.NumGoroutine()
	baseHeap := heapInUse()

	// Exactly the bound.
	for i := range maxInFlightPerConn {
		frames <- frame(uint32(1 + i)) //nolint:gosec // G115: i < 64
	}
	waitFor(t, "a goroutine per message in flight", func() bool {
		return runtime.NumGoroutine()-baseGoroutines >= maxInFlightPerConn
	})
	time.Sleep(100 * time.Millisecond)
	atBound := runtime.NumGoroutine() - baseGoroutines
	heapAtBound := int64(heapInUse()) - int64(baseHeap) //nolint:gosec // G115: heap sizes fit int64
	if atBound != maxInFlightPerConn || delivered.Load() != 2 {
		t.Fatalf("at the bound: %d goroutines above the baseline, %d messages delivered; want %d and 2 (the warm-up and the held one)",
			atBound, delivered.Load(), maxInFlightPerConn)
	}
	t.Logf("at the bound (%d messages of %d bytes in flight): +%d goroutines, live heap +%d bytes, %d bytes written by the peer",
		maxInFlightPerConn, payload, atBound, heapAtBound, written.Load())

	// Beyond it: the next messages are not handed on.
	for i := range beyond {
		frames <- frame(uint32(1 + maxInFlightPerConn + i)) //nolint:gosec // G115: small
	}
	time.Sleep(300 * time.Millisecond)
	pastBound := runtime.NumGoroutine() - baseGoroutines
	heapPastBound := int64(heapInUse()) - int64(baseHeap) //nolint:gosec // G115: heap sizes fit int64
	if pastBound != maxInFlightPerConn || delivered.Load() != 2 {
		t.Fatalf("past the bound: %d goroutines above the baseline, %d messages delivered; want %d and 2",
			pastBound, delivered.Load(), maxInFlightPerConn)
	}
	t.Logf("%d messages past the bound: +%d goroutines, live heap +%d bytes, %d of %d bytes written by the peer",
		beyond, pastBound, heapPastBound, written.Load(), int64(1+maxInFlightPerConn+beyond)*int64(FramingHeaderSize+payload))

	// The held handler returns: everything arrives, in order, and the
	// goroutines go.
	openGate()
	total := int64(1 + maxInFlightPerConn + beyond)
	waitFor(t, "every message", func() bool { return delivered.Load() == total })
	orderMu.Lock()
	for i, seq := range order {
		if seq != uint32(i) { //nolint:gosec // G115: i < 81
			orderMu.Unlock()
			t.Fatalf("message %d delivered as number %d: order %v", seq, i, order)
		}
	}
	orderMu.Unlock()
	waitFor(t, "the goroutines of the delivered messages to end", func() bool {
		return runtime.NumGoroutine() <= baseGoroutines
	})
}

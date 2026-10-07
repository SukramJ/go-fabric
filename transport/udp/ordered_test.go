// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package udp

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"
)

// keyOf reads a test datagram's stream key from its first byte; a 0 byte
// has no key.
func keyOf(buf []byte) (uint32, bool) {
	if len(buf) == 0 || buf[0] == 0 {
		return 0, false
	}
	return uint32(buf[0]), true
}

// TestServeOrderedKeepsAStreamInOrder: the datagrams of one stream are
// handled one at a time in arrival order, even when the first handler is
// slow; another stream does not wait; a handler that releases its turn lets
// the next datagram of its stream run while it goes on.
func TestServeOrderedKeepsAStreamInOrder(t *testing.T) {
	a, b := newLoopbackPair(t)
	var mu sync.Mutex
	var seen []string
	released := make(chan struct{})
	finish := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = a.ServeOrdered(ctx, keyOf, func(buf []byte, _ *net.UDPAddr, release func()) {
			switch string(buf[1:]) {
			case "slow":
				time.Sleep(100 * time.Millisecond)
			case "waits":
				release()
				close(released)
				<-finish
			}
			mu.Lock()
			seen = append(seen, string(buf))
			mu.Unlock()
		})
	}()
	send := func(s string) {
		if err := b.Send(a.LocalAddr(), []byte(s)); err != nil {
			t.Fatal(err)
		}
	}
	send("\x01slow")
	send("\x01fast")
	send("\x02other")
	send("\x01waits")
	send("\x01after")
	select {
	case <-released:
	case <-time.After(3 * time.Second):
		t.Fatal("the releasing handler never ran")
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		mu.Lock()
		n := len(seen)
		mu.Unlock()
		if n == 4 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	close(finish)
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	pos := map[string]int{}
	for i, s := range seen {
		pos[s] = i
	}
	if len(seen) != 5 {
		t.Fatalf("handled %q, want five datagrams", seen)
	}
	if pos["\x02other"] > pos["\x01fast"] {
		t.Errorf("stream 2 waited for stream 1's slow handler: %q", seen)
	}
	if pos["\x01slow"] > pos["\x01fast"] {
		t.Errorf("stream 1 out of order: %q", seen)
	}
	if pos["\x01after"] > pos["\x01waits"] {
		t.Errorf("the datagram after a released turn waited for the handler to return: %q", seen)
	}
}

// A datagram without a key is handled concurrently, as Serve handles it,
// and a nil handler is refused.
func TestServeOrderedUnkeyedAndNil(t *testing.T) {
	a, b := newLoopbackPair(t)
	if err := a.ServeOrdered(context.Background(), keyOf, nil); err == nil {
		t.Error("nil handler accepted")
	}
	got := make(chan string, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = a.ServeOrdered(ctx, keyOf, func(buf []byte, _ *net.UDPAddr, release func()) {
			release()
			release() // idempotent
			got <- string(buf)
		})
	}()
	if err := b.Send(a.LocalAddr(), []byte("\x00plain")); err != nil {
		t.Fatal(err)
	}
	select {
	case s := <-got:
		if s != "\x00plain" {
			t.Errorf("got %q", s)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("an unkeyed datagram was not handled")
	}
}

// The backlog behind one key is bounded.
func TestOrderedBacklogBound(t *testing.T) {
	block := make(chan struct{})
	o := &orderedDispatch{key: keyOf, queues: map[uint32]*orderedQueue{}, handler: func([]byte, *net.UDPAddr, func()) { <-block }}
	l := &Listener{}
	for range maxOrderedBacklog + 10 {
		o.enqueue(l, 1, []byte{1}, nil)
	}
	o.mu.Lock()
	n := len(o.queues[1].items)
	o.mu.Unlock()
	close(block)
	if n > maxOrderedBacklog {
		t.Errorf("backlog %d, want at most %d", n, maxOrderedBacklog)
	}
}

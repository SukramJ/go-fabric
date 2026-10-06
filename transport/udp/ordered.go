// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package udp

import (
	"context"
	"errors"
	"net"
	"sync"
)

// OrderedHandler handles one datagram of an ordered stream. release hands
// the stream's turn to the next datagram before the handler returns; a
// handler calls it before it waits for something only a later datagram of
// the same stream can bring (the peer's answer to what it just sent).
// Returning releases the turn too; release is idempotent.
type OrderedHandler func(buf []byte, src *net.UDPAddr, release func())

// maxOrderedBacklog bounds the datagrams queued behind one key; beyond it
// a datagram is dropped, as the unordered pool drops one when saturated.
const maxOrderedBacklog = 256

// ServeOrdered is [Listener.Serve] for a protocol whose datagrams fall into
// streams that must be handled in arrival order — the messages of one
// Matter session, which matter.js and chip handle in order on their single
// event loop. key names a datagram's stream; a datagram without one goes to
// handler concurrently as [Listener.Serve] does. Datagrams of one stream are
// handled one at a time, in the order they were read, each starting once
// its predecessor returned or released its turn.
func (l *Listener) ServeOrdered(ctx context.Context, key func(buf []byte) (uint32, bool), handler OrderedHandler) error {
	if handler == nil {
		return errors.New("udp: nil handler")
	}
	l.order = &orderedDispatch{key: key, handler: handler, queues: map[uint32]*orderedQueue{}}
	return l.Serve(ctx, func(buf []byte, src *net.UDPAddr) { handler(buf, src, func() {}) })
}

type orderedItem struct {
	buf []byte
	src *net.UDPAddr
}

type orderedQueue struct {
	items   []orderedItem
	running bool
}

// orderedDispatch runs one drain goroutine per stream with pending
// datagrams.
type orderedDispatch struct {
	key     func(buf []byte) (uint32, bool)
	handler OrderedHandler

	mu     sync.Mutex
	queues map[uint32]*orderedQueue
}

// enqueue appends a datagram to its stream and starts the stream's drain
// if it is idle. Called on the read goroutine, so the queue holds the
// datagrams in arrival order.
func (o *orderedDispatch) enqueue(l *Listener, key uint32, buf []byte, src *net.UDPAddr) {
	o.mu.Lock()
	defer o.mu.Unlock()
	q := o.queues[key]
	if q == nil {
		q = &orderedQueue{}
		o.queues[key] = q
	}
	if len(q.items) >= maxOrderedBacklog {
		return
	}
	q.items = append(q.items, orderedItem{buf: buf, src: src})
	if !q.running {
		q.running = true
		go o.drain(l, key, q)
	}
}

// drain hands the stream's datagrams to the handler one at a time. Each
// runs on its own goroutine, so a handler that releases its turn and goes
// on waiting does not hold up the stream.
func (o *orderedDispatch) drain(l *Listener, key uint32, q *orderedQueue) {
	for {
		o.mu.Lock()
		if len(q.items) == 0 {
			q.running = false
			delete(o.queues, key)
			o.mu.Unlock()
			return
		}
		it := q.items[0]
		q.items = q.items[1:]
		o.mu.Unlock()

		turn := make(chan struct{})
		var once sync.Once
		release := func() { once.Do(func() { close(turn) }) }
		go func() {
			defer release()
			l.safeDispatch(func(buf []byte, src *net.UDPAddr) { o.handler(buf, src, release) }, it.buf, it.src)
		}()
		<-turn
	}
}

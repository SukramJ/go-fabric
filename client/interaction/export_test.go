// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction

import (
	"context"
	"sync"
)

// QueuedInvokes is the number of commands waiting in the batch lanes of c,
// the test seam of the batcher's queue.
func QueuedInvokes(c *Interaction) int {
	c.batchMu.Lock()
	defer c.batchMu.Unlock()
	n := 0
	for _, lane := range c.lanes {
		n += len(lane.pending)
	}
	return n
}

// FlushingBatches is the number of batch flushes still running on c.
func FlushingBatches(c *Interaction) int { return int(c.flushing.Load()) }

// SetTimedWrite makes isTimedWrite name (cluster, attribute) timed and
// returns the restore; for a sequential test only (no attribute of the
// snapshot carries the timed quality at the pin).
func SetTimedWrite(cluster, attribute uint32) (restore func()) {
	prev := isTimedWrite
	isTimedWrite = func(c, a uint32) bool { return (c == cluster && a == attribute) || prev(c, a) }
	return func() { isTimedWrite = prev }
}

// HoldConnect wraps p in a provider whose connect — the step
// #invokeWithBatching awaits before it queues a command (connector) — is
// held: it closes connecting when called and returns once release is
// called, then connects as p does (when p connects at all). The test seam
// of ClientInvokeTest.ts:836-846 at 862ad75f, which replaces
// exchangeProvider.connect the same way. p's MaxPathsPerInvoke stays.
func HoldConnect(p Provider) (held Provider, connecting <-chan struct{}, release func()) {
	h := &heldConnect{Provider: p, connecting: make(chan struct{}), released: make(chan struct{})}
	var once sync.Once
	return h, h.connecting, func() { once.Do(func() { close(h.released) }) }
}

type heldConnect struct {
	Provider
	connecting chan struct{}
	released   chan struct{}
	called     sync.Once
}

func (h *heldConnect) connect(ctx context.Context, opts ExchangeOptions) error {
	h.called.Do(func() { close(h.connecting) })
	<-h.released
	if c, ok := h.Provider.(connector); ok {
		return c.connect(ctx, opts)
	}
	return nil
}

func (h *heldConnect) maxPathsPerInvoke() int {
	if p, ok := h.Provider.(pathsPerInvoker); ok {
		return p.maxPathsPerInvoke()
	}
	return 1
}

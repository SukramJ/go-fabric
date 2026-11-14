// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction

import (
	"context"
	"sync"

	"github.com/SukramJ/go-fabric/client/exchange"
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

// RecordNetworks wraps p in a provider that records the network of every
// exchange it initiates, in order — the test seam of ClientInvokeTest.ts's
// spyNetworkSelect (:421-436 at 862ad75f), which captures each id
// NetworkProfiles.select receives. p's MaxPathsPerInvoke and connect stay.
func RecordNetworks(p Provider) (recorded Provider, networks func() []string) {
	r := &networkRecorder{Provider: p}
	return r, func() []string {
		r.mu.Lock()
		defer r.mu.Unlock()
		return append([]string(nil), r.networks...)
	}
}

type networkRecorder struct {
	Provider
	mu       sync.Mutex
	networks []string
}

func (r *networkRecorder) InitiateExchange(ctx context.Context, opts ExchangeOptions) (*exchange.Exchange, error) {
	r.mu.Lock()
	r.networks = append(r.networks, opts.Network)
	r.mu.Unlock()
	return r.Provider.InitiateExchange(ctx, opts)
}

func (r *networkRecorder) connect(ctx context.Context, opts ExchangeOptions) error {
	if c, ok := r.Provider.(connector); ok {
		return c.connect(ctx, opts)
	}
	return nil
}

func (r *networkRecorder) maxPathsPerInvoke() int {
	if p, ok := r.Provider.(pathsPerInvoker); ok {
		return p.maxPathsPerInvoke()
	}
	return 1
}

// MaxAssemblyBytesPerSession is the report bytes a session's interactions
// may hold while their chunks arrive.
const MaxAssemblyBytesPerSession = maxAssemblyBytesPerSession

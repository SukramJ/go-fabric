// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction

import (
	"context"
	"sync"

	"thingfabric.dev/go-fabric/client/exchange"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/tlv"
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
// of ClientInvokeTest.ts:836-846 at db021da0, which replaces
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
const MaxAssemblyBytesPerSession = DefaultMaxReportBytesPerSession

// ReportChunkCharge is what a ReportData chunk is charged against the
// report bound: its payload and its values' decoded elements.
func ReportChunkCharge(payload []byte) int {
	rd, err := decodeReport(payload)
	if err != nil {
		panic(err)
	}
	return reportCharge(payload, rd)
}

// InvokeChunkCharge is ReportChunkCharge for an InvokeResponse chunk.
func InvokeChunkCharge(payload []byte) int {
	resp, err := im.UnmarshalInvokeResponseTLV(tlv.NewDecoder(payload), readFields)
	if err != nil {
		panic(err)
	}
	return invokeCharge(payload, &resp)
}

// ReportInterrupted starts a report on s and tells s it was interrupted
// as interruption says ("chunk-unacknowledged", "final-unacknowledged" or
// "chunk-missing"): the test seam of ClientInteractionSubscribeTest.ts:199-218
// at db91886c, which calls
// reportInterrupted(nextReportNumber(), "final-unacknowledged").
func ReportInterrupted(s *Subscription, interruption string) {
	s.reportInterrupted(s.nextReportNumber(), reportInterruption(interruption))
}

// EstablishedCount is the number of subscriptions c established that
// have not closed (#established).
func EstablishedCount(c *Interaction) int {
	c.estMu.Lock()
	defer c.estMu.Unlock()
	return len(c.established)
}

// RegisterPeerSubscription registers a fresh peer subscription of id
// through c as subscribeOnce does once the SubscribeResponse arrived: the
// window in which Close may already have run.
func RegisterPeerSubscription(c *Interaction, id uint32) (*Subscription, error) {
	peer, _ := c.provider.PeerAddress()
	s := c.subs.newSubscription(peer, SubscribeRequest{}, c.log)
	s.id = id
	return s, c.register(s, c.subs.AddPeer)
}

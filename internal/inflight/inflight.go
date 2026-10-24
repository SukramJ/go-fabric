// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package inflight counts the work a host's stack has in hand, so a test
// site that paces the stack on a fake clock moves the clock only while
// the stack has none: a datagram read and not yet handled, a message an
// exchange took and its reader has not finished with, a report queued
// and not yet sent, a host callback queued, a node's start in progress.
//
// It is the Go reading of matter.js MockTime's dependents
// (packages/testing/src/mocks/time.ts:32-51, :87-103 and :383-394 at
// 862ad75f, requireHostAsync): an operation registered there withholds
// virtual time until it settles. A component counts work it accepts and
// finishes; it never counts a wait on its peer or on the clock — the
// peer's progress may itself need the clock to move (a standalone
// acknowledgement's timer, a retransmission after a loss), and a counted
// wait would stall it.
//
// A component finds its host's [Group] on the clock it was built with
// ([Of]): a fabrictest site hands each host a clock that carries one
// ([WithGroup]). On any other clock — the system clock of a production
// host — Of is nil, and a nil *Group counts nothing at the cost of a nil
// check.
package inflight

import (
	"maps"
	"sync"
	"sync/atomic"

	"github.com/SukramJ/go-fabric/clock"
)

// The counters. Each names where its work is accepted and where it is
// finished (the CG report lists the sites).
const (
	// UDPDatagrams: a datagram read from the host's socket until the
	// listener's handler for it returned (or the listener dropped it).
	UDPDatagrams = "udp.datagrams"
	// ExchangeDelivered: a message an exchange queued for its reader, or
	// handed to it, until the reader's next call on the exchange (the
	// next Next, a Send, Close) or the exchange's end.
	ExchangeDelivered = "exchange.delivered"
	// SubscriptionPending: a report the subscription engine decided to
	// send until it was sent or dropped.
	SubscriptionPending = "subscription.pending"
	// DeviceCallbacks: a host callback queued on the node's serial queue
	// until it returned.
	DeviceCallbacks = "device.callbacks"
	// NodeStart and NodeStop: a node's Start or Stop in progress, outside
	// its waits on a peer.
	NodeStart = "node.start"
	NodeStop  = "node.stop"
	// NodeBuild: a test site building a host's node before its Start
	// (fabrictest Device.Start: node.New and the endpoints' assembly).
	// The construction is synchronous and waits on no clock, so a site
	// holds its clock for it without a budget: under the race detector
	// on a loaded machine it outlasts any budget a waiting operation
	// gets.
	NodeBuild = "node.build"
)

// Group is one host's counters. Its methods are safe for concurrent use;
// every method of a nil *Group is a no-op.
type Group struct {
	name string
	// changes counts every Add, so a reader tells a group that stands
	// from one that moves.
	changes atomic.Uint64
	mu      sync.Mutex
	counts  map[string]int64
}

// New returns an empty group; name labels it in a site's diagnostics.
func New(name string) *Group { return &Group{name: name, counts: map[string]int64{}} }

// Name is the group's label.
func (g *Group) Name() string {
	if g == nil {
		return ""
	}
	return g.name
}

// Add adds n (negative to finish work) to the counter.
func (g *Group) Add(counter string, n int64) {
	if g == nil || n == 0 {
		return
	}
	g.mu.Lock()
	v := g.counts[counter] + n
	if v == 0 {
		delete(g.counts, counter)
	} else {
		g.counts[counter] = v
	}
	g.mu.Unlock()
	g.changes.Add(1)
}

// Enter counts one unit of work accepted.
func (g *Group) Enter(counter string) { g.Add(counter, 1) }

// Leave counts one unit of work finished.
func (g *Group) Leave(counter string) { g.Add(counter, -1) }

// Busy reports whether any counter is above zero.
func (g *Group) Busy() bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, v := range g.counts {
		if v > 0 {
			return true
		}
	}
	return false
}

// Changes is the number of Adds so far.
func (g *Group) Changes() uint64 {
	if g == nil {
		return 0
	}
	return g.changes.Load()
}

// Snapshot is the counters that are not zero.
func (g *Group) Snapshot() map[string]int64 {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return maps.Clone(g.counts)
}

// carrier is a clock that carries a group.
type carrier struct {
	clock.Clock
	g *Group
}

// InflightGroup is the group the clock carries: the method [Of] reads,
// which a component's own clock wrapper forwards to the clock it wraps.
func (c carrier) InflightGroup() *Group { return c.g }

// WithGroup returns c carrying g: every component built with the returned
// clock counts its work in g. c is what the returned clock tells the
// time by.
func WithGroup(c clock.Clock, g *Group) clock.Clock {
	return carrier{Clock: clock.Or(c), g: g}
}

// Of is the group c carries, nil when it carries none.
func Of(c clock.Clock) *Group {
	if cc, ok := c.(interface{ InflightGroup() *Group }); ok {
		return cc.InflightGroup()
	}
	return nil
}

// Unwrap is the clock c tells the time by: c itself, or the clock a
// carrier wraps.
func Unwrap(c clock.Clock) clock.Clock {
	if cc, ok := c.(carrier); ok {
		return cc.Clock
	}
	return c
}

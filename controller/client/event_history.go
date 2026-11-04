// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import "sync"

// DefaultEventHistory is the number of events a node keeps
// ([Node.EventHistory]) when [Config].EventHistory is 0.
const DefaultEventHistory = 100

// RecordedEvent is one event a node's history holds: the event as the peer
// reported it and Decoded, its payload decoded through the event's
// definition — what [Hooks].Event receives as value: the raw
// [spec.Node] when the cluster or event has no definition, nil without a
// payload.
type RecordedEvent struct {
	EventData
	Decoded any
}

// eventHistory is a node's bounded event history: a ring of the newest
// events it delivered, in delivery order. It is memory only and has its
// own lock, so a reader never waits for the node's image.
//
// Ahead of matter.js (notes/parity/by_design.md
// BD-Matter-ClientNodeEventHistory): matter.js's ClientNode emits a peer's
// event to its observers and the ChangeNotificationService and keeps none
// (packages/node/src/node/client/ClientEventEmitter.ts:45-74 at
// 862ad75f). matterjs-server keeps one global list of the last 25
// events of every node (packages/ws-controller/src/server/
// WebSocketControllerHandler.ts:112, :368-380 at 0bd06f22).
type eventHistory struct {
	mu   sync.Mutex
	ring []RecordedEvent
	next int // the slot the next event takes once the ring is full
}

// newEventHistory is a history of capacity events: 0 is
// DefaultEventHistory, a negative capacity keeps none.
func newEventHistory(capacity int) *eventHistory {
	switch {
	case capacity == 0:
		capacity = DefaultEventHistory
	case capacity < 0:
		capacity = 0
	}
	return &eventHistory{ring: make([]RecordedEvent, 0, capacity)}
}

// record keeps e, dropping the oldest event when the history is full.
func (h *eventHistory) record(e RecordedEvent) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	switch {
	case cap(h.ring) == 0:
	case len(h.ring) < cap(h.ring):
		h.ring = append(h.ring, e)
	default:
		h.ring[h.next] = e
		h.next = (h.next + 1) % len(h.ring)
	}
}

// clear forgets every event.
func (h *eventHistory) clear() {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	clear(h.ring)
	h.ring, h.next = h.ring[:0], 0
}

// query returns the events numbered since or later, oldest first; with
// limit > 0 only the newest limit of them.
func (h *eventHistory) query(since uint64, limit int) []RecordedEvent {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []RecordedEvent
	for i := range len(h.ring) {
		e := h.ring[(h.next+i)%len(h.ring)]
		if e.Number >= since {
			out = append(out, e)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

// EventHistory returns the events the node delivered to [Hooks].Event —
// the default subscription's, its bootstrap read's and the
// newly-commissioned read's — whose number is since or later, oldest
// first; with limit > 0 only the newest limit of them. The history holds
// the newest [Config].EventHistory events (DefaultEventHistory when 0, none
// when negative), in memory only: it starts empty with every [New] and
// [Node.Erase] clears it. A host's own Read or Subscribe does not reach
// it. The result is a copy the caller may keep.
//
// matter.js's ClientNode keeps no event history (ClientEventEmitter.ts:45-74
// at 862ad75f): this is the module's (by_design.md
// BD-Matter-ClientNodeEventHistory), so a UI's event log and a server's
// diagnostics read one source.
func (n *Node) EventHistory(since uint64, limit int) []RecordedEvent {
	return n.events.query(since, limit)
}

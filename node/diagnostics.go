// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

// Diagnostics is the type of [Config].Diagnostics: the node's diagnostic
// trace, a bounded in-memory ring of the moments that explain a failed
// pairing or a lost session ([DiagnosticEvent]). The ring is memory only;
// the logs are the durable record.
type Diagnostics struct {
	// Capacity is the number of events kept, the oldest dropped first; 0
	// keeps [DefaultDiagnosticsCapacity].
	Capacity int
}

// DefaultDiagnosticsCapacity is the trace's capacity when
// [Diagnostics].Capacity is 0.
const DefaultDiagnosticsCapacity = 256

// capacity is the ring's capacity: Capacity, or the default below one.
func (d Diagnostics) capacity() int {
	if d.Capacity < 1 {
		return DefaultDiagnosticsCapacity
	}
	return d.Capacity
}

// DiagnosticEvents returns the node's diagnostic trace, newest first: a
// copy the caller may keep (the REST handler of a daemon serialises it).
func (n *Node) DiagnosticEvents() []DiagnosticEvent {
	return n.diagRing.Snapshot()
}

// OnDiagnosticEvent registers fn for every event the node records from
// now on. fn runs inline, on the goroutine that recorded the event — the
// receive path of a pairing or a session — after the trace stored it and
// outside its lock: it must not block, and may read
// [Node.DiagnosticEvents]. [Node.OnDiagnosticEventAsync] queues it
// instead. It returns the unsubscribe function.
func (n *Node) OnDiagnosticEvent(fn func(DiagnosticEvent)) (unsubscribe func()) {
	if fn == nil {
		return func() {}
	}
	return n.diagRing.Observe(fn)
}

// OnDiagnosticEventAsync is [Node.OnDiagnosticEvent] with fn queued on
// the node's callback goroutine: the event is taken when it is recorded,
// and fn runs later, one call after the other, while the node runs
// (between [Node.Start] and [Node.Stop]; Stop returns once every queued
// call ran). An event recorded while the node does not run is not
// delivered. fn must not stop the node.
func (n *Node) OnDiagnosticEventAsync(fn func(DiagnosticEvent)) (unsubscribe func()) {
	if fn == nil {
		return func() {}
	}
	q := n.callbacks
	return n.diagRing.Observe(func(e DiagnosticEvent) { q.Enqueue(func() { fn(e) }) })
}

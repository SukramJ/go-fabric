// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"slices"
	"sync"

	endpointpkg "github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
)

// writeRecord is the verdict of one write message, read from the
// dispatcher's results ([im.WriteOutcome.Results]): the attributes its
// writes changed, by [im.WriteResult.Commit], and the paths it wrote
// without a verdict.
type writeRecord struct {
	// changed are the attributes a successful write's Commit named,
	// without a list index, in the order the results named them.
	changed []im.ConcreteAttributePath
	// unjudged are the written paths of successful writes that carried
	// no Commit: each is taken to have changed for its report, and is not
	// among the changed ones a held notification is settled against.
	unjudged []im.ConcreteAttributePath
}

// recordOf is the record of a write message's results.
func recordOf(results []im.WriteResult) writeRecord {
	var r writeRecord
	for _, res := range results {
		if res.Status != im.StatusSuccess || !res.Path.HasEndpoint || !res.Path.HasCluster || !res.Path.HasAttribute {
			continue
		}
		if res.Commit == nil {
			r.unjudged = append(r.unjudged, attributeOf(res.Path.Endpoint, res.Path.Cluster, res.Path.Attribute))
			continue
		}
		for _, id := range res.Commit.Changed {
			p := attributeOf(res.Path.Endpoint, res.Path.Cluster, id)
			if !slices.Contains(r.changed, p) {
				r.changed = append(r.changed, p)
			}
		}
	}
	return r
}

// attributeOf is the concrete path of one attribute.
func attributeOf(endpoint uint16, cluster, attribute uint32) im.ConcreteAttributePath {
	return im.ConcreteAttributePath{
		Endpoint: endpoint, Cluster: cluster, Attribute: attribute,
		HasEndpoint: true, HasCluster: true, HasAttribute: true,
	}
}

// changedAt reports whether the record names p's attribute changed.
func (r writeRecord) changedAt(p im.ConcreteAttributePath) bool {
	return slices.Contains(r.changed, attributeOf(p.Endpoint, p.Cluster, p.Attribute))
}

// endpoints are the endpoints the record changed an attribute of, each
// once.
func (r writeRecord) endpoints() []uint16 {
	var out []uint16
	for _, p := range r.changed {
		if !slices.Contains(out, p.Endpoint) {
			out = append(out, p.Endpoint)
		}
	}
	return out
}

// merged is the record of every write in rs: the attributes any of them
// changed.
func merged(rs []writeRecord) writeRecord {
	if len(rs) == 1 {
		return rs[0]
	}
	var out writeRecord
	for _, r := range rs {
		for _, p := range r.changed {
			if !slices.Contains(out.changed, p) {
				out.changed = append(out.changed, p)
			}
		}
	}
	return out
}

// writeHoldTable makes a controller's write one commit per attribute data
// beside a host's change notifier (review II, R2-P4-fr-im-01), as
// [invokeCommitTable] makes a command one. The dispatcher advances a
// cluster's DataVersion for each written attribute its server says it
// changed (endpoint/dispatcher.go WriteAuthorized) and
// [Bridge.reportWrittenAttributes] reports it; a host notifier that fires
// for the same change — inside the write, or later against a memory that
// still holds the value before it — advanced and reported it a second
// time. While a write is applied to an endpoint, a notification's changed
// paths of that endpoint update the notifier's memory and are held here,
// neither advanced nor marked dirty; [Bridge.settleWrite] settles them when
// the last write that holds the endpoint ends, against the records of
// every write that ended while the hold was open. matter.js writes each attribute data in a transaction of
// its own (packages/protocol/src/action/server/AttributeWriteResponse.ts:426-435
// at db021da0, openForWrite then commit), and a commit advances the
// version once for whatever it changed
// (packages/node/src/behavior/state/managed/Datasource.ts:1446-1457 at
// db021da0).
type writeHoldTable struct {
	mu sync.Mutex
	// running counts the writes being applied per endpoint they name;
	// wildcard counts those with a path that names no endpoint, which
	// hold every endpoint.
	running  map[uint16]int
	wildcard int
	held     map[uint16]*heldWrite
	// overlap are, per endpoint, the records of the writes that changed
	// one of its attributes and ended while another write still held the
	// endpoint: the hold is settled against them too when it closes.
	overlap map[uint16][]writeRecord
}

// heldWrite is what notifications reported for one endpoint while a
// write to it was applied.
type heldWrite struct {
	paths []heldPath
	quiet []*quietPath
	// record is, once the hold is handed out, the merged record of every
	// write that ended while it was open, the last one's included.
	record writeRecord
}

// heldPath is one changed path a notification held, with the wiring's
// endpoint and manager it would have advanced and marked it on.
type heldPath struct {
	ep   *endpointpkg.Endpoint
	mgr  *subscription.Manager
	path im.ConcreteAttributePath
	// advanceOnly is a change whose report goes out on its own (a
	// quieter attribute's, by its throttle): the hold settles its
	// cluster's version and marks nothing.
	advanceOnly bool
	// markOnly is a change whose version advanced elsewhere (a server's
	// report view): the hold marks it and advances nothing.
	markOnly bool
}

// add holds p once: a host that notifies the same path again while a
// write is applied — a server that waits on its backend under a wildcard
// write holds every endpoint — grows the table by distinct paths only, not
// by its notification rate (R2-P6-bound-0409). Settlement is one advance
// per cluster and one mark per path either way. The caller holds the
// table's mutex.
func (h *heldWrite) add(p heldPath) {
	if !slices.Contains(h.paths, p) {
		h.paths = append(h.paths, p)
	}
}

// writeScope is the endpoints one write message names.
type writeScope struct {
	endpoints []uint16
	wildcard  bool
}

// writeScopeOf is the endpoints req's paths name; a path without one
// names every endpoint.
func writeScopeOf(req im.WriteRequest) writeScope {
	var s writeScope
	for _, w := range req.Writes {
		if !w.Path.HasEndpoint {
			s.wildcard = true
			continue
		}
		if !slices.Contains(s.endpoints, w.Path.Endpoint) {
			s.endpoints = append(s.endpoints, w.Path.Endpoint)
		}
	}
	return s
}

// begin opens the hold of every endpoint s names.
func (t *writeHoldTable) begin(s writeScope) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if s.wildcard {
		t.wildcard++
	}
	if len(s.endpoints) > 0 && t.running == nil {
		t.running = make(map[uint16]int, len(s.endpoints))
	}
	for _, id := range s.endpoints {
		t.running[id]++
	}
}

// holding reports whether a write to endpoint is being applied; the
// caller holds t.mu.
func (t *writeHoldTable) holding(endpoint uint16) bool {
	return t.wildcard > 0 || t.running[endpoint] > 0
}

// entry is endpoint's held record, made on first use; the caller holds
// t.mu.
func (t *writeHoldTable) entry(endpoint uint16) *heldWrite {
	h, ok := t.held[endpoint]
	if !ok {
		if t.held == nil {
			t.held = make(map[uint16]*heldWrite, 1)
		}
		h = &heldWrite{}
		t.held[endpoint] = h
	}
	return h
}

// holdPaths holds paths of ep while a write to it is applied and returns
// none of them; otherwise it returns paths, left to the caller.
func (t *writeHoldTable) holdPaths(ep *endpointpkg.Endpoint, mgr *subscription.Manager, paths []im.ConcreteAttributePath) []im.ConcreteAttributePath {
	if len(paths) == 0 {
		return paths
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.holding(ep.ID) {
		return paths
	}
	h := t.entry(ep.ID)
	for _, p := range paths {
		h.add(heldPath{ep: ep, mgr: mgr, path: p})
	}
	return nil
}

// holdAdvance holds the version advance of a change of attrs of cluster
// on ep, whose report goes out on its own, while a write to ep is applied
// and reports true; false when none is.
func (t *writeHoldTable) holdAdvance(ep *endpointpkg.Endpoint, cluster uint32, attrs []uint32) bool {
	if len(attrs) == 0 {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.holding(ep.ID) {
		return false
	}
	h := t.entry(ep.ID)
	for _, a := range attrs {
		h.add(heldPath{ep: ep, path: attributeOf(ep.ID, cluster, a), advanceOnly: true})
	}
	return true
}

// holdMarks holds the marks of attrs of cluster on ep — a report view's,
// which advances nothing — while a write to ep is applied and reports
// true; false when none is.
func (t *writeHoldTable) holdMarks(ep *endpointpkg.Endpoint, mgr *subscription.Manager, cluster uint32, attrs []uint32) bool {
	if len(attrs) == 0 {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.holding(ep.ID) {
		return false
	}
	h := t.entry(ep.ID)
	for _, a := range attrs {
		h.add(heldPath{ep: ep, mgr: mgr, path: attributeOf(ep.ID, cluster, a), markOnly: true})
	}
	return true
}

// holdQuiet leaves qp's check to the end of the write applied to
// endpoint and reports true; false when none is.
func (t *writeHoldTable) holdQuiet(endpoint uint16, qp *quietPath) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.holding(endpoint) {
		return false
	}
	h := t.entry(endpoint)
	if !slices.Contains(h.quiet, qp) {
		h.quiet = append(h.quiet, qp)
	}
	return true
}

// end closes the hold s opened, for the write whose record is rec, and
// returns what was held for every endpoint no write holds any longer —
// each entry with the merged record of the writes that ended while it was
// held. An endpoint another write still holds keeps its entry, and rec,
// when it changed one of the endpoint's attributes, waits with it for that
// write's end (review II, R2-P5-write-05): a notification the other write
// caused is settled against that write's record, not against this one's.
func (t *writeHoldTable) end(s writeScope, rec writeRecord) map[uint16]*heldWrite {
	t.mu.Lock()
	defer t.mu.Unlock()
	if s.wildcard {
		t.wildcard--
	}
	for _, id := range s.endpoints {
		t.running[id]--
		if t.running[id] <= 0 {
			delete(t.running, id)
		}
	}
	// The endpoints rec changed an attribute of whose hold stays open
	// keep it for the write that closes the hold.
	for _, id := range rec.endpoints() {
		if !t.holding(id) {
			continue
		}
		if t.overlap == nil {
			t.overlap = make(map[uint16][]writeRecord, 1)
		}
		t.overlap[id] = append(t.overlap[id], rec)
	}
	var out map[uint16]*heldWrite
	for id, h := range t.held {
		if t.holding(id) {
			continue
		}
		if out == nil {
			out = make(map[uint16]*heldWrite, 1)
		}
		h.record = merged(append(t.overlap[id], rec))
		out[id] = h
		delete(t.held, id)
	}
	for id := range t.overlap {
		if !t.holding(id) {
			delete(t.overlap, id)
		}
	}
	return out
}

// applyWriteHeld applies req through d with the notifications of the
// endpoints it names held for its duration ([Bridge.holdWrite]).
func (b *Bridge) applyWriteHeld(ctx context.Context, d im.Dispatcher, req im.WriteRequest) (out im.WriteOutcome) {
	b.holdWrite(writeScopeOf(req), func() []im.WriteResult {
		out = im.HandleWrite(ctx, d, req)
		return out.Results
	})
	return out
}

// holdWrite runs a write with the notifications of the endpoints scope
// names held for its duration ([writeHoldTable]), and settles them when it
// ends against the record of the results run returns
// ([im.WriteResult.Commit]) — after a panic too, with an empty record, so
// no hold outlives its write.
func (b *Bridge) holdWrite(scope writeScope, run func() []im.WriteResult) {
	b.writeHolds.begin(scope)
	var results []im.WriteResult
	defer func() { b.settleWrite(scope, recordOf(results)) }()
	results = run()
}

// settleWrite ends the hold of a write and settles what it held, judged by
// the write's record (rec) — merged, for an endpoint whose hold outlasted
// it, with the records of the writes that ended while the hold was open:
//
//   - every attribute the write changed is read again into the current
//     wiring's memory, whether or not a notification arrived, so the next
//     notification compares with the written value;
//   - a held path a write changed was advanced by the dispatcher and is
//     reported by [Bridge.reportWrittenAttributes]: it is neither advanced
//     nor marked again;
//   - any other held path — an attribute the host moved with the written
//     one — is marked dirty, and its cluster's DataVersion advanced once,
//     unless a write changed that cluster and advanced it already: one
//     commit, one advance (Datasource.ts:1446-1457 at db021da0).
//
// The rule rests on the record alone: a verdict that did not name a value
// that moved leaves a notification's change of it to be advanced and
// reported here.
func (b *Bridge) settleWrite(scope writeScope, rec writeRecord) {
	held := b.writeHolds.end(scope, rec)
	b.mu.RLock()
	wiring := b.listenerWiring
	b.mu.RUnlock()
	wiring.rememberWritten(rec.changed)
	for epID, h := range held {
		// The clusters of this endpoint the writes advanced already.
		advanced := make(map[uint32]struct{}, 1)
		for _, p := range h.record.changed {
			if p.Endpoint == epID {
				advanced[p.Cluster] = struct{}{}
			}
		}
		marks := make([]heldPath, 0, len(h.paths))
		for _, hp := range h.paths {
			if h.record.changedAt(hp.path) {
				continue
			}
			if _, done := advanced[hp.path.Cluster]; !done && !hp.markOnly {
				hp.ep.BumpDataVersionOf(hp.path.Cluster)
				advanced[hp.path.Cluster] = struct{}{}
			}
			if !hp.advanceOnly {
				marks = append(marks, hp)
			}
		}
		for _, hp := range marks {
			hp.mgr.OnAttributeChanged(hp.path)
		}
		for _, qp := range h.quiet {
			if h.record.changedAt(qp.path) {
				qp.refresh()
				continue
			}
			qp.check(advanced)
		}
	}
}

// rememberWritten reads every path of written the wiring remembers again
// into its memory: an endpoint's last values ([pathMemory]) and a quiet
// path's ([quietPath]). A nil wiring remembers nothing.
func (l *listenerSet) rememberWritten(written []im.ConcreteAttributePath) {
	if l == nil {
		return
	}
	for _, p := range written {
		if mem := l.values[p.Endpoint]; mem != nil {
			mem.refresh(p)
		}
		if qp := l.quiet[p]; qp != nil {
			qp.refresh()
		}
	}
}

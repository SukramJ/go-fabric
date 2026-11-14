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
// the write ends. matter.js writes each attribute data in a transaction of
// its own (packages/protocol/src/action/server/AttributeWriteResponse.ts:426-435
// at db021da0, openForWrite then commit), and a commit advances the
// version once for every property it changed
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
}

// heldWrite is what notifications reported for one endpoint while a
// write to it was applied.
type heldWrite struct {
	paths []heldPath
	quiet []*quietPath
}

// heldPath is one changed path a notification held, with the wiring's
// endpoint and manager it would have advanced and marked it on.
type heldPath struct {
	ep   *endpointpkg.Endpoint
	mgr  *subscription.Manager
	path im.ConcreteAttributePath
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
		h.paths = append(h.paths, heldPath{ep: ep, mgr: mgr, path: p})
	}
	return nil
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

// end closes the hold s opened and returns what was held for the
// endpoints s names — every endpoint for a wildcard — so far.
func (t *writeHoldTable) end(s writeScope) map[uint16]*heldWrite {
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
	if s.wildcard {
		out := t.held
		t.held = nil
		return out
	}
	var out map[uint16]*heldWrite
	for _, id := range s.endpoints {
		if h, ok := t.held[id]; ok {
			if out == nil {
				out = make(map[uint16]*heldWrite, 1)
			}
			out[id] = h
			delete(t.held, id)
		}
	}
	return out
}

// applyWriteHeld applies req through d with the notifications of the
// endpoints it names held for its duration ([writeHoldTable]), and settles
// them when it ends — after a panic too, so no hold outlives its write.
func (b *Bridge) applyWriteHeld(ctx context.Context, d im.Dispatcher, req im.WriteRequest, changes *endpointpkg.WriteChanges) im.WriteResponse {
	scope := writeScopeOf(req)
	b.writeHolds.begin(scope)
	defer b.settleWrite(scope, changes)
	return im.HandleWriteRequest(ctx, d, req)
}

// settleWrite ends the hold of a write and settles what it held, judged by
// the write's record (changes):
//
//   - every path the write changed is read again into the current
//     wiring's memory, whether or not a notification arrived, so the next
//     notification compares with the written value;
//   - a held path the write changed was advanced by the dispatcher and is
//     reported by [Bridge.reportWrittenAttributes]: it is neither advanced
//     nor marked again;
//   - any other held path — an attribute the host moved with the written
//     one — is marked dirty, and its cluster's DataVersion advanced once,
//     unless the write changed that cluster and advanced it already: one
//     commit, one advance (Datasource.ts:1446-1457 at db021da0).
//
// The rule rests on the record alone: a verdict that answered "unchanged"
// for a value that moved leaves a notification's change of it to be
// advanced and reported here.
func (b *Bridge) settleWrite(scope writeScope, changes *endpointpkg.WriteChanges) {
	held := b.writeHolds.end(scope)
	written := changes.ChangedAttributes()
	b.mu.RLock()
	wiring := b.listenerWiring
	b.mu.RUnlock()
	wiring.rememberWritten(written)
	for epID, h := range held {
		// The clusters of this endpoint the write advanced already.
		advanced := make(map[uint32]struct{}, 1)
		for _, p := range written {
			if p.Endpoint == epID {
				advanced[p.Cluster] = struct{}{}
			}
		}
		marks := make([]heldPath, 0, len(h.paths))
		for _, hp := range h.paths {
			if changes.Changed(hp.path) {
				continue
			}
			if _, done := advanced[hp.path.Cluster]; !done {
				hp.ep.BumpClusterDataVersion(hp.path.Cluster)
				advanced[hp.path.Cluster] = struct{}{}
			}
			marks = append(marks, hp)
		}
		for _, hp := range marks {
			hp.mgr.OnAttributeChanged(hp.path)
		}
		for _, qp := range h.quiet {
			if changes.Changed(qp.path) {
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

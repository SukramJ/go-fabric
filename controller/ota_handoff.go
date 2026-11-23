// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller

import (
	"context"
	"sync"
	"sync/atomic"

	"thingfabric.dev/go-fabric/controller/softwareupdate"
)

// otaHandoffs are the update manager's NodeDeleted calls the node hooks
// handed to goroutines (ota.go otaHooks, decision 219). Close waits for
// each, within its ctx, until it returns or reaches a host OnUpdateFailed
// of the same peer. Since C-17 NodeDeleted reports no event for the
// deleted node — its queued update ends silently (#forgetPeer,
// SoftwareUpdateManager.ts:312-342 at b25ed960) — but its queue run may
// still report another node's update failed (a stalled update whose
// consent is gone, reset at that run): that callback runs on the
// hand-off goroutine and releases the other node's hand-off, if any, not
// this one. A host that calls Close from it waits for this hand-off until
// Close's ctx ends (the class of R2-P6-conc-2301 and -err-2317). That
// bound is kept rather than releasing the hand-off before NodeDeleted:
// released first, Close could return while the manager still changes its
// state for the deleted node.
type otaHandoffs struct {
	mu      sync.Mutex
	closed  bool
	pending map[softwareupdate.Peer][]*otaHandoff // per peer, in hand-off order
	open    int                                   // hand-offs not yet released
	idle    chan struct{}                         // closed when open reaches 0 while Close waits

	// lastPeers holds, per node id, the address that node's hooks hand
	// NodeDeleted (ota.go otaHooks): set by every hook that saw the node
	// with an address, and seeded by startOTA for the nodes the registry
	// restored, so a node no hook saw is still forgotten (review II,
	// R2-C2-fsm-2302).
	lastPeers sync.Map // string → *atomic.Pointer[softwareupdate.Peer]
}

// track registers the address cell of node id's hooks.
func (h *otaHandoffs) track(id string) *atomic.Pointer[softwareupdate.Peer] {
	cell := &atomic.Pointer[softwareupdate.Peer]{}
	h.lastPeers.Store(id, cell)
	return cell
}

// untrack drops cell, node id's, once its Deleted ran.
func (h *otaHandoffs) untrack(id string, cell *atomic.Pointer[softwareupdate.Peer]) {
	h.lastPeers.CompareAndDelete(id, cell)
}

// seed gives node id's cell the address p unless a hook set one already.
func (h *otaHandoffs) seed(id string, p softwareupdate.Peer) {
	if v, ok := h.lastPeers.Load(id); ok {
		if cell, ok := v.(*atomic.Pointer[softwareupdate.Peer]); ok {
			cell.CompareAndSwap(nil, &p)
		}
	}
}

// otaHandoff is one NodeDeleted call; released once it returned or
// reached a host callback of its peer.
type otaHandoff struct{ released bool }

// start registers a hand-off for p; nil once Close began.
func (h *otaHandoffs) start(p softwareupdate.Peer) *otaHandoff {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil
	}
	if h.pending == nil {
		h.pending = map[softwareupdate.Peer][]*otaHandoff{}
	}
	o := &otaHandoff{}
	h.pending[p] = append(h.pending[p], o)
	h.open++
	return o
}

// release releases o; the caller holds h.mu.
func (h *otaHandoffs) releaseLocked(p softwareupdate.Peer, o *otaHandoff) {
	if o.released {
		return
	}
	o.released = true
	list := h.pending[p]
	for i, x := range list {
		if x == o {
			h.pending[p] = append(list[:i:i], list[i+1:]...)
			break
		}
	}
	if len(h.pending[p]) == 0 {
		delete(h.pending, p)
	}
	h.open--
	if h.open == 0 && h.idle != nil {
		close(h.idle)
		h.idle = nil
	}
}

// done releases o: its NodeDeleted returned.
func (h *otaHandoffs) done(p softwareupdate.Peer, o *otaHandoff) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.releaseLocked(p, o)
}

// reachedHost releases the oldest hand-off of p: the manager is about to
// call the host's OnUpdateFailed for p (since C-17 never from p's own
// NodeDeleted, so this releases only a hand-off whose NodeDeleted is still
// running when another change reports p).
func (h *otaHandoffs) reachedHost(p softwareupdate.Peer) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if list := h.pending[p]; len(list) > 0 {
		h.releaseLocked(p, list[0])
	}
}

// close refuses further hand-offs and waits, within ctx, until every one
// was released.
func (h *otaHandoffs) close(ctx context.Context) {
	h.mu.Lock()
	h.closed = true
	if h.open == 0 {
		h.mu.Unlock()
		return
	}
	if h.idle == nil {
		h.idle = make(chan struct{})
	}
	idle := h.idle
	h.mu.Unlock()
	select {
	case <-idle:
	case <-ctx.Done():
	}
}

// otaFailed is the host's OnUpdateFailed as the update manager calls it: a
// pending NodeDeleted hand-off of the peer is released first
// (otaHandoffs).
func (c *Controller) otaFailed(host func(softwareupdate.Peer)) func(softwareupdate.Peer) {
	return func(p softwareupdate.Peer) {
		c.otaHandoffs.reachedHost(p)
		if host != nil {
			host(p)
		}
	}
}

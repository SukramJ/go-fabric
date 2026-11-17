// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller

import (
	"context"
	"sync"

	"github.com/SukramJ/go-fabric/controller/softwareupdate"
)

// otaHandoffs are the update manager's NodeDeleted calls the node hooks
// handed to goroutines (ota.go otaHooks, decision 219). Close waits for
// each until it reaches the host's OnUpdateFailed, or returns without it:
// the manager's own part takes only its lock, while the host's callback
// may block or call Close itself — a wait for it would hold Close past
// its ctx, or wait for the goroutine that called it (the class of
// R2-P6-conc-2301 and -err-2317). The wait is bounded by Close's ctx
// either way.
type otaHandoffs struct {
	mu      sync.Mutex
	closed  bool
	pending map[softwareupdate.Peer][]*otaHandoff // per peer, in hand-off order
	open    int                                   // hand-offs not yet released
	idle    chan struct{}                         // closed when open reaches 0 while Close waits
}

// otaHandoff is one NodeDeleted call; released once it reached the host's
// callback or returned.
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

// reachedHost releases the oldest hand-off of p: its NodeDeleted is
// about to call the host's OnUpdateFailed for p.
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
// NodeDeleted hand-off of the peer is released first (otaHandoffs).
func (c *Controller) otaFailed(host func(softwareupdate.Peer)) func(softwareupdate.Peer) {
	return func(p softwareupdate.Peer) {
		c.otaHandoffs.reachedHost(p)
		if host != nil {
			host(p)
		}
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// commitKey carries the innermost open [writeCommit] in the ctx a
// controller write's veto and Sink are handed; each commit links to the
// one it was opened inside (writeCommit.parent).
type commitKey struct{}

// writeCommit is one controller write's commit window on a server and
// its change list: the attributes a Set changed while the write's veto and
// Sink ran, and the notifications of every store inside the window, told
// once the window is released. Its stores advance no version on their own;
// the write's own store advances it once, as matter.js advances a
// datasource's version once per transaction for every field it noted
// (Datasource.ts:1456-1457, :1476-1484 at db021da0) and hands the whole
// list on (:1319). A Set another goroutine makes while the window is open
// is folded into the commit too: the server cannot tell it from one its
// Sink makes.
//
// One window is open per server at a time: a second controller write of
// the server waits for the first's veto, Sink and store — as matter.js
// serialises a controller's writes to a datasource: a Datasource is a
// transaction resource (Datasource.ts:134), each attribute data's write
// adds it to the session's transaction and begins it
// (ProtocolService.ts:395-401), and acquiring its lock waits until no
// other transaction holds it (general/src/transaction/ResourceSet.ts:51-75)
// and then locks it to the one transaction (:102-104) — all at db021da0; there the synchronous writer does not wait but throws
// (ResourceSet.ts:82-99), as a Set here neither waits nor fails. The wait
// ends with the write's ctx. A write whose ctx carries the server's open
// commit anywhere in its chain — the veto or Sink of that write, directly
// or through another server's write — joins it, as matter.js's
// transaction re-enters a resource it holds (ResourceSet.ts:87-89); a
// wait that would close a cycle of windows is refused ([errWriteCycle])
// instead, as matter.js detects the deadlock (ResourceSet.ts:69, :146);
// the answer differs — matter.js fails the whole request with one status,
// this module the path's attribute data, as for every non-status error
// (notes/parity/by_design.md). The cycle check follows one nested write
// per commit: a Sink that runs two nested writes at once is not covered.
// A Sink that writes its own server with a ctx that does not carry the
// one it was handed waits for its own window and ends only with that
// ctx. A change listener is told after the window is released, so one
// that writes the server again does not wait for it.
type writeCommit struct {
	server *Server
	// parent is the open commit of the ctx the write was called with,
	// child the commit a write inside this one's veto or Sink opened on
	// another server, waiting the server this commit's write waits for a
	// window of, closed true once the window is released; guarded by
	// windowGraph.
	parent, child *writeCommit
	waiting       *Server
	closed        bool
	// changed and notifies are guarded by the server's mu while the
	// commit is open (Server.commit), the owner's alone after.
	changed  []uint32
	notifies []func()
}

// windowGraph guards every server's holder and free and every commit's
// parent, child, waiting and closed: the graph a wait for a window is
// checked against for a cycle.
var windowGraph sync.Mutex

// errWriteCycle refuses a controller write whose wait for a server's
// commit window would close a cycle of writes waiting for each other.
var errWriteCycle = errors.New("spec: write conflict: the write waits for a server whose write waits for this one")

// enterWindow enters s's commit window for a controller write called with
// ctx: joined is true, with the open commit, when ctx's chain carries it;
// otherwise the write waits until the window is free and opens its own
// commit c. The wait ends with ctx's error, and is refused with
// [errWriteCycle] when it would close a cycle.
func (s *Server) enterWindow(ctx context.Context) (c *writeCommit, joined bool, err error) {
	inner, _ := ctx.Value(commitKey{}).(*writeCommit)
	windowGraph.Lock()
	for k := inner; k != nil; k = k.parent {
		if k.server == s && s.holder == k {
			windowGraph.Unlock()
			return k, true, nil
		}
	}
	// A kept ctx carries commits that closed: the write runs inside the
	// first one still open.
	parent := inner
	for parent != nil && parent.closed {
		parent = parent.parent
	}
	for s.holder != nil {
		if parent != nil && s.waitCloses(parent) {
			windowGraph.Unlock()
			return nil, false, fmt.Errorf("%w (%s)", errWriteCycle, s.def.Name)
		}
		free := s.free
		if parent != nil {
			parent.waiting = s
		}
		windowGraph.Unlock()
		select {
		case <-free:
		case <-ctx.Done():
			windowGraph.Lock()
			if parent != nil {
				parent.waiting = nil
			}
			windowGraph.Unlock()
			return nil, false, ctx.Err()
		}
		windowGraph.Lock()
		if parent != nil {
			parent.waiting = nil
		}
	}
	c = &writeCommit{server: s, parent: parent}
	s.holder, s.free = c, make(chan struct{})
	if parent != nil {
		parent.child = c
	}
	windowGraph.Unlock()
	s.mu.Lock()
	s.commit = c
	s.mu.Unlock()
	return c, false, nil
}

// waitCloses reports whether a wait for s's window by the write whose
// innermost open commit is mine closes a cycle: s's holder, or the holder
// of the window its innermost write waits for, and so on, is one of the
// commits mine's chain holds. The caller holds windowGraph.
func (s *Server) waitCloses(mine *writeCommit) bool {
	h := s.holder
	for range 256 {
		if h == nil {
			return false
		}
		for k := mine; k != nil; k = k.parent {
			if k == h {
				return true
			}
		}
		d := h
		for d.child != nil {
			d = d.child
		}
		if d.waiting == nil {
			return false
		}
		h = d.waiting.holder
	}
	return false
}

// leaveWindow releases the window c holds: the collection ends, the next
// waiting write may enter.
func (s *Server) leaveWindow(c *writeCommit) {
	s.mu.Lock()
	if s.commit == c {
		s.commit = nil
	}
	s.mu.Unlock()
	windowGraph.Lock()
	c.closed = true
	if s.holder == c {
		s.holder = nil
		close(s.free)
	}
	if c.parent != nil && c.parent.child == c {
		c.parent.child = nil
	}
	windowGraph.Unlock()
}

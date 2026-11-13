// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"sync"

	"github.com/SukramJ/go-fabric/transport/mrp"
)

// A handler that has sent a chunk of a priming report, of a chunked read
// or of a chunked invoke response waits for the peer's StatusResponse
// before it sends the next one, and while it waits it is still running
// on the listener's dispatch: it holds one of the bridge-wide slots of
// the ordered listener (transport/udp, maxConcurrentDispatch), which
// counts a handler until it returns, not until it releases its session's
// turn. The wait follows the peer's MRP intervals, as matter.js's does
// (review II, R2-P1-req2-01: the length stays). Two things keep such
// waits from taking the slots other peers need:
//
//   - The wait ends when what it serves ends: the handler's context (the
//     bridge's Stop cancels it), the session ([Bridge.SessionClosed]
//     disarms every wait of the session) and, for a priming report, the
//     subscription ([Bridge.releaseSubscriptionRouting] closes its
//     priming abort).
//   - One session holds at most [maxChunkWaitsPerSession] interactions
//     that wait so at once; the request beyond is answered Busy — a
//     SubscribeRequest before anything is registered, a batch
//     InvokeRequest before any command runs, a ReadRequest after it was
//     read and chunked, instead of its first chunk: whether a read waits
//     at all depends on its chunk count, and a read that fits one chunk
//     is not refused (review II, R2-P1-dr-req-10).
//
// The cut is the bound per session, not releasing the dispatch slot for
// the wait: a released slot would leave the waiting goroutines without
// any bound, and the slot budget lives in the transport, which knows
// nothing of sessions. A peer holds at most
// operational.MaxSessionsPerPeer sessions, so at most that many times
// the bound of the slots.

// maxChunkWaitsPerSession bounds the interactions of one session that
// may wait for the peer's StatusResponse to a chunk at once — a
// subscription's priming report, a read that spans several chunks, a
// batch invoke whose response spans several. The number is the module's
// own choice: a controller runs one such interaction per exchange and
// rarely more than one or two at once on a session; connectedhomeip's
// node-wide pools of command and write handlers are 4 each
// (src/lib/core/CHIPConfig.h:922, :992 at 6170af84).
const maxChunkWaitsPerSession = 4

// chunkWaitClaims counts, per session, the interactions holding a chunk
// wait claim.
type chunkWaitClaims struct {
	mu sync.Mutex
	n  map[uint16]int
}

// claim takes one of sessionID's [maxChunkWaitsPerSession] claims; false
// when the session holds them all. A true claim is given back with
// [chunkWaitClaims.release].
func (c *chunkWaitClaims) claim(sessionID uint16) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.n[sessionID] >= maxChunkWaitsPerSession {
		return false
	}
	if c.n == nil {
		c.n = map[uint16]int{}
	}
	c.n[sessionID]++
	return true
}

// release gives back a claim [chunkWaitClaims.claim] granted.
func (c *chunkWaitClaims) release(sessionID uint16) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.n[sessionID] <= 1 {
		delete(c.n, sessionID)
		return
	}
	c.n[sessionID]--
}

// claimChunkWait takes one of sessionID's chunk wait claims when needed
// is set. release gives it back and is a no-op when nothing was taken;
// ok is false when the session holds every claim.
func (b *Bridge) claimChunkWait(sessionID uint16, needed bool) (release func(), ok bool) {
	if !needed {
		return func() {}, true
	}
	if !b.routing.chunkWaits.claim(sessionID) {
		return nil, false
	}
	return func() { b.routing.chunkWaits.release(sessionID) }, true
}

// endSessionChunkWaits disarms every StatusResponse wait of sessionID: a
// chunk loop waiting on one returns [errChunkWaitAborted] at once.
func (b *Bridge) endSessionChunkWaits(sessionID uint16) {
	b.routing.statusResponseWaits.Range(func(k, _ any) bool {
		if key, ok := k.(mrp.ExchangeKey); ok && key.SessionID == sessionID {
			b.disarmStatusResponseWait(key.SessionID, key.ExchangeID, key.Initiator)
		}
		return true
	})
}

// primingAbort registers the channel that ends subID's priming-report
// chunk waits when the subscription closes, and closes it at once when
// the subscription is already gone. The caller defers
// [Bridge.forgetPrimingAbort].
func (b *Bridge) primingAbort(subID uint32) <-chan struct{} {
	if subID == 0 {
		return nil // no manager: nothing can close it
	}
	ch := make(chan struct{})
	b.routing.primingAborts.Store(subID, ch)
	// The manager drops a subscription from its tables before it runs the
	// close hook: a close that ran before the Store is seen here, one
	// after it finds the channel.
	if m := b.subscriptionManagerLocked(); m != nil {
		if _, err := m.Get(subID); err != nil {
			b.closePrimingAbort(subID)
		}
	}
	return ch
}

// closePrimingAbort ends subID's priming waits; the one that removes the
// channel closes it.
func (b *Bridge) closePrimingAbort(subID uint32) {
	if v, loaded := b.routing.primingAborts.LoadAndDelete(subID); loaded {
		if ch, ok := v.(chan struct{}); ok {
			close(ch)
		}
	}
}

// forgetPrimingAbort drops subID's priming abort once its priming report
// is done.
func (b *Bridge) forgetPrimingAbort(subID uint32) {
	b.routing.primingAborts.Delete(subID)
}

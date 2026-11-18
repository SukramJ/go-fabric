// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"sync"
	"sync/atomic"

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
//
// Each fabric is held to a share as well: once the claims bridge-wide
// reach [chunkWaitFabricThreshold] — half the listener's dispatch budget —
// a fabric that holds more than the threshold divided by the fabrics the
// node holds ([Bridge.SetFabricCounter]) is answered Busy for a further
// claim, as the per-session bound answers. Below the threshold, and on a
// node of one fabric, nothing changes; a node whose host wired no counter
// keeps the per-session bound alone (review II, TM R3.7, decision 245). The rule is
// this module's: matter.js bounds a session's outgoing exchanges only
// (packages/protocol/src/protocol/ExchangeManager.ts:63 at db021da0);
// connectedhomeip reserves read and subscription resources per fabric
// (src/app/InteractionModelEngine.h:113-116, :674-687 at 6170af84).

// maxChunkWaitsPerSession bounds the interactions of one session that
// may wait for the peer's StatusResponse to a chunk at once — a
// subscription's priming report, a read that spans several chunks, a
// batch invoke whose response spans several. The number is the module's
// own choice: a controller runs one such interaction per exchange and
// rarely more than one or two at once on a session; connectedhomeip's
// node-wide pools of command and write handlers are 4 each
// (src/lib/core/CHIPConfig.h:922, :992 at 6170af84).
const maxChunkWaitsPerSession = 4

// chunkWaitFabricThreshold is the number of chunk wait claims bridge-wide
// from which a fabric is held to its share: half of the ordered
// listener's dispatch budget (transport/udp maxConcurrentDispatch, 1024),
// so a fabric over its share leaves the other half to the rest. The
// relation is held by the test that fills the budget.
const chunkWaitFabricThreshold = 512

// chunkWaitClaims counts the interactions holding a chunk wait claim, per
// session and per fabric.
type chunkWaitClaims struct {
	mu       sync.Mutex
	n        map[uint16]int
	byFabric map[uint8]int
	total    int
	// fabrics counts the fabrics the node holds ([Bridge.SetFabricCounter]).
	fabrics atomic.Pointer[func(context.Context) (int, error)]
}

// claim takes one of sessionID's [maxChunkWaitsPerSession] claims for
// fabric; false when the session holds them all, or when share is set and
// the fabric already holds more than share. A true claim is given back
// with [chunkWaitClaims.release].
func (c *chunkWaitClaims) claim(sessionID uint16, fabric uint8, share int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.n[sessionID] >= maxChunkWaitsPerSession {
		return false
	}
	if share > 0 && c.total >= chunkWaitFabricThreshold && c.byFabric[fabric] > share {
		return false
	}
	if c.n == nil {
		c.n, c.byFabric = map[uint16]int{}, map[uint8]int{}
	}
	c.n[sessionID]++
	c.byFabric[fabric]++
	c.total++
	return true
}

// release gives back a claim [chunkWaitClaims.claim] granted.
func (c *chunkWaitClaims) release(sessionID uint16, fabric uint8) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.n[sessionID] == 0 {
		return
	}
	c.total--
	if c.byFabric[fabric] <= 1 {
		delete(c.byFabric, fabric)
	} else {
		c.byFabric[fabric]--
	}
	if c.n[sessionID] <= 1 {
		delete(c.n, sessionID)
		return
	}
	c.n[sessionID]--
}

// atThreshold reports whether the claims bridge-wide reach
// [chunkWaitFabricThreshold].
func (c *chunkWaitClaims) atThreshold() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.total >= chunkWaitFabricThreshold
}

// share is a fabric's share of the threshold — the threshold over the
// fabrics the node holds — or 0, no share rule, without a counter, on a
// counter error and on a node of at most one fabric.
func (c *chunkWaitClaims) share(ctx context.Context) int {
	fn := c.fabrics.Load()
	if fn == nil {
		return 0
	}
	n, err := (*fn)(ctx)
	if err != nil || n <= 1 {
		return 0
	}
	return chunkWaitFabricThreshold / n
}

// SetFabricCounter wires the count of the fabrics the node holds, which
// sets each fabric's share of the chunk waits once they reach half the
// dispatch budget (review II, TM R3.7, decision 245). It is asked only
// then. Without it — nil clears it — the per-session bound is the only
// one.
func (b *Bridge) SetFabricCounter(fn func(context.Context) (int, error)) {
	if fn == nil {
		b.routing.chunkWaits.fabrics.Store(nil)
		return
	}
	b.routing.chunkWaits.fabrics.Store(&fn)
}

// claimChunkWait takes one of sessionID's chunk wait claims when needed
// is set. release gives it back and is a no-op when nothing was taken;
// ok is false when the session holds every claim, or its fabric more than
// its share ([chunkWaitFabricThreshold]).
func (b *Bridge) claimChunkWait(ctx context.Context, sessionID uint16, needed bool) (release func(), ok bool) {
	if !needed {
		return func() {}, true
	}
	var fabric uint8
	b.mu.RLock()
	sessions := b.sessions
	b.mu.RUnlock()
	if r, has := sessions.(SessionFabricResolver); has {
		fabric, _ = r.FabricFor(sessionID)
	}
	share := 0
	if b.routing.chunkWaits.atThreshold() {
		share = b.routing.chunkWaits.share(ctx)
	}
	if !b.routing.chunkWaits.claim(sessionID, fabric, share) {
		return nil, false
	}
	var once sync.Once
	return func() { once.Do(func() { b.routing.chunkWaits.release(sessionID, fabric) }) }, true
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

// reportAbort is the abort of an ongoing report's chunk waits: it closes
// when ctx ends — the engine's stop (W2-conc-06) — and when the
// subscription closes, as a priming report's ([Bridge.primingAbort]), so
// a report of a subscription a KeepSubscriptions=false request, a fabric's
// removal or an emptied grant ended returns at once instead of waiting
// out chunkStatusResponseTimeout (review II, R2-P6-fsm-0513). A
// subscription has one report in flight, so the priming table holds it.
// done stops it; the caller defers done.
func (b *Bridge) reportAbort(ctx context.Context, subID uint32) (abort <-chan struct{}, done func()) {
	ch := b.primingAbort(subID)
	if ch == nil {
		return ctx.Done(), func() {}
	}
	stop := context.AfterFunc(ctx, func() { b.closePrimingAbort(subID) })
	return ch, func() {
		stop()
		b.forgetPrimingAbort(subID)
	}
}

// forgetPrimingAbort drops subID's priming abort once its priming report
// is done.
func (b *Bridge) forgetPrimingAbort(subID uint32) {
	b.routing.primingAborts.Delete(subID)
}

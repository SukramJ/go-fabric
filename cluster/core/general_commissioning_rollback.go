// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"errors"
	"fmt"
)

// The order between a fail-safe's end and the next arm (core2 item 1b).
//
// Every path that ends a fail-safe with its expiry hook — the watcher,
// the wire disarm, ExpireFailSafe — disarms under g.mu and runs the hook
// (OperationalCredentials.OnFailSafeExpiry: the rollback of a pending
// AddNOC / UpdateNOC) after releasing it. An arm in that gap used to see
// a disarmed fail-safe and fire the fresh-arm hook
// (OperationalCredentials.ClearPendingState), which dropped the pending
// NOC before the rollback read it: the uncommitted fabric stayed. Now the
// rollback is recorded as in flight under the lock it disarms with, and an
// arm settles first: it waits for every rollback in flight, and expires a
// window that is due but whose watcher has not run, before it reads the
// armed state.

// expiryRollbackKey marks the context an expiry hook runs with, so an arm
// made from inside the rollback does not wait for itself.
type expiryRollbackKey struct{}

// trackRollbackLocked records hook as a rollback in flight and returns it
// wrapped: it runs with a marked context and ends the record when it
// returns, counting what it ended — the PASE sessions when
// onFailSafeRolledBack is wired, and windowFabric when the window's
// AddNOC added it (failSafeFabricAdded). A nil hook is nothing to wait
// for. Caller holds g.mu.
func (g *GeneralCommissioning) trackRollbackLocked(hook func(ctx context.Context, fabricIndex uint8), windowFabric uint8) func(ctx context.Context, fabricIndex uint8) {
	if hook == nil {
		return nil
	}
	if g.expiryRollbacksDone == nil {
		g.expiryRollbacksDone = make(chan struct{})
	}
	g.expiryRollbacks++
	pasesClosed := g.onFailSafeRolledBack != nil
	var removed uint8
	if g.failSafeFabricAdded {
		removed = windowFabric
	}
	return func(ctx context.Context, fabricIndex uint8) {
		defer func() {
			g.mu.Lock()
			if pasesClosed {
				g.rollbackPaseCloses++
			}
			if removed != 0 {
				if g.rollbackFabricRemovals == nil {
					g.rollbackFabricRemovals = map[uint8]uint64{}
				}
				g.rollbackFabricRemovals[removed]++
			}
			g.expiryRollbacks--
			if g.expiryRollbacks == 0 {
				close(g.expiryRollbacksDone)
				g.expiryRollbacksDone = nil
			}
			g.mu.Unlock()
		}()
		hook(context.WithValue(ctx, expiryRollbackKey{}, true), fabricIndex)
	}
}

// startFailSafeLocked runs onFailSafeStarting when an arm finds the
// fail-safe disarmed, before the arm is judged and recorded (an arm
// refused afterwards, as a CASE arm while a window is open is, has run it
// too), counted as a rollback in flight — an arm on
// another goroutine waits for it in settleExpiryLocked, so nothing is armed
// while it runs — and adds the fabrics it removed to
// rollbackFabricRemovals. Before, the new context's rollback ran after the
// arm was recorded, outside this accounting: an arm from a fabric whose
// earlier rollback had not landed removed that fabric and left the
// fail-safe armed for it (review II, R2-P2-dr-noc-04). With forSession, an
// arm from sessFabric's session that the rollback removed, or whose row it
// could not roll back and left pending (review II, R2-P2-rr-dev-01), is
// errGencommArmRolledBack. Nothing runs when the fail-safe is armed or no
// hook is wired. Caller holds g.mu; it is held again on return, and
// released in between.
func (g *GeneralCommissioning) startFailSafeLocked(ctx context.Context, sessFabric uint8, forSession bool) error {
	hook := g.onFailSafeStarting
	if hook == nil || g.failSafeArmed {
		return nil
	}
	removals := g.rollbackFabricRemovals[sessFabric]
	g.runFailSafeStartingLocked(ctx, hook)
	if forSession && sessFabric != 0 && g.rollbackFabricRemovals[sessFabric] != removals {
		return errGencommArmRolledBack
	}
	return nil
}

// runFailSafeStartingLocked runs hook outside g.mu as a rollback in flight
// and accounts the fabrics it returns — those it removed and those whose
// pending row it left — as rollbackFabricRemovals. Caller holds g.mu; it is held again on
// return — on a panic of the hook too.
func (g *GeneralCommissioning) runFailSafeStartingLocked(ctx context.Context, hook func(ctx context.Context) []uint8) {
	if g.expiryRollbacksDone == nil {
		g.expiryRollbacksDone = make(chan struct{})
	}
	g.expiryRollbacks++
	g.mu.Unlock()
	var removed []uint8
	defer func() {
		g.mu.Lock()
		for _, idx := range removed {
			if g.rollbackFabricRemovals == nil {
				g.rollbackFabricRemovals = map[uint8]uint64{}
			}
			g.rollbackFabricRemovals[idx]++
		}
		g.expiryRollbacks--
		if g.expiryRollbacks == 0 {
			close(g.expiryRollbacksDone)
			g.expiryRollbacksDone = nil
		}
	}()
	removed = hook(context.WithValue(ctx, expiryRollbackKey{}, true))
}

// settledExpiry is what settleExpiryLocked expired itself: the window the
// arm found due, and what its rollback ends.
type settledExpiry struct {
	expired bool
	// fabricIndex is the fabric the window was armed for at its end, and
	// fabricAdded that AddNOC re-stamped it so (the rollback removes it).
	fabricIndex uint8
	fabricAdded bool
	// pasesClosed: the rollback ran onFailSafeRolledBack, which ends every
	// PASE session (the bridge wires it so, bridge/pase_close.go).
	pasesClosed bool
}

// endedSession reports whether the rollback settleExpiryLocked ran itself
// ended the session of an arm from sessFabric (0: a PASE session): every
// PASE session when onFailSafeRolledBack ran, and a CASE session of the
// fabric the window's AddNOC added, which the rollback removed. A fail-safe
// armed for such a session would be armed for nobody: until it expires,
// OpenCommissioningWindow answers Busy and every other admin's ArmFailSafe
// BusyWithOtherAdmin (review II, R2-P1-dr-sec-03).
func (s settledExpiry) endedSession(sessFabric uint8) bool {
	if !s.expired {
		return false
	}
	if sessFabric == 0 {
		return s.pasesClosed
	}
	return s.fabricAdded && s.fabricIndex == sessFabric
}

// errGencommArmRolledBack answers an arm whose own session the rollback of
// the due window it settled ended. The IM layer answers it as FAILURE.
var errGencommArmRolledBack = errors.New("core: GeneralCommissioning: the fail-safe this arm found had expired, and its rollback ended the arming session")

// settleArmLocked is settleExpiryLocked for an arm from sessFabric's
// session: errGencommArmRolledBack when a rollback it settled ended that
// session — one it ran itself over a due window, or one in flight on
// another goroutine that it waited for (review II, R2-P1-rw-05) — or, on
// a disarmed fail-safe, the start of the new context (startFailSafeLocked)
// removed sessFabric. A
// rollback that finishes while the arm waits ran its PASE close or its
// fabric removal after the arm's session existed. Caller holds g.mu, as
// for settleExpiryLocked.
func (g *GeneralCommissioning) settleArmLocked(ctx context.Context, sessFabric uint8) error {
	paseCloses, removals := g.rollbackPaseCloses, g.rollbackFabricRemovals[sessFabric]
	settled, err := g.settleExpiryLocked(ctx)
	if err != nil {
		return err
	}
	if settled.endedSession(sessFabric) {
		return errGencommArmRolledBack
	}
	if (sessFabric == 0 && g.rollbackPaseCloses != paseCloses) || (sessFabric != 0 && g.rollbackFabricRemovals[sessFabric] != removals) {
		return errGencommArmRolledBack
	}
	return g.startFailSafeLocked(ctx, sessFabric, true)
}

// settleExpiryLocked returns once no fail-safe expiry is left half done: a
// window that is armed but due is expired here, its hook run by this
// caller, and a rollback running on another goroutine is waited for —
// unless ctx is that rollback's own, or ctx ends first, which is the
// error. It reports the window it expired itself. Caller holds g.mu; it is
// held again on return — on a panic of the rollback too, so the caller's
// deferred unlock frees it (review II, R2-P2-rr-dev-03) — and released in
// between.
func (g *GeneralCommissioning) settleExpiryLocked(ctx context.Context) (settledExpiry, error) {
	inRollback := ctx.Value(expiryRollbackKey{}) != nil
	var settled settledExpiry
	for {
		if g.failSafeArmed && !g.failSafeExpiresAt.After(g.clock.Now()) {
			fabricIndex := g.failSafeFabricIndex
			if w := g.failSafeWatch; w != nil {
				fabricIndex = w.fabricIndex
			}
			settled = settledExpiry{
				expired:     true,
				fabricIndex: g.failSafeFabricIndex,
				fabricAdded: g.failSafeFabricAdded,
				pasesClosed: g.onFailSafeRolledBack != nil,
			}
			after := g.expireLocked(fabricIndex)
			func() {
				g.mu.Unlock()
				defer g.mu.Lock()
				after(ctx)
			}()
			continue
		}
		done := g.expiryRollbacksDone
		if done == nil || inRollback {
			return settled, nil
		}
		g.mu.Unlock()
		select {
		case <-done:
			g.mu.Lock()
		case <-ctx.Done():
			g.mu.Lock()
			return settled, fmt.Errorf("core: GeneralCommissioning: arm while the expired fail-safe rolls back: %w", ctx.Err())
		}
	}
}

// FailSafeCommitFunc is the durable commit of CommissioningComplete
// ([GeneralCommissioning.SetFailSafeCommit]): it runs complete with
// everything the commit must not interleave with held off — on the node,
// OperationalCredentials' NOC commands, RemoveFabric and the fail-safe's
// rollback — and hands it commit, the last fallible write of the
// commissioning: on the node, the Wi-Fi network list, then the commit of
// the fabric row this fail-safe's AddNOC or UpdateNOC wrote. A commit
// error must leave what it wrote undone. [OperationalCredentials.CommitFailSafe]
// is one.
type FailSafeCommitFunc func(ctx context.Context, complete func(commit func(ctx context.Context) error))

// SetFailSafeCommit wires CommissioningComplete's durable commit. Without
// one CommissioningComplete commits the terms acceptance only. Pass nil
// to detach.
func (g *GeneralCommissioning) SetFailSafeCommit(fn FailSafeCommitFunc) {
	g.mu.Lock()
	g.failSafeCommit = fn
	g.mu.Unlock()
}

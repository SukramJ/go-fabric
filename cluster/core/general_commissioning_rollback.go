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
// another goroutine that it waited for (review II, R2-P1-rw-05). A
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
	return nil
}

// settleExpiryLocked returns once no fail-safe expiry is left half done: a
// window that is armed but due is expired here, its hook run by this
// caller, and a rollback running on another goroutine is waited for —
// unless ctx is that rollback's own, or ctx ends first, which is the
// error. It reports the window it expired itself. Caller holds g.mu; it is
// held again on return, and released in between.
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
			g.mu.Unlock()
			after(ctx)
			g.mu.Lock()
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

// commitPendingNOCLocked clears OperationalCredentials' durable pending-NOC
// markers ([pendingNOCKeys]) as part of CommissioningComplete, before
// the fail-safe is disarmed: while the marker stands, a restart rolls the
// fabric back, so CommissioningComplete must not answer success over it
// (core2 item 1a). The marker is the fail-safe context's durable half;
// the fail-safe's commit ends it, as matter.js persists the new fabric
// inside CommissioningComplete (FailsafeContext.ts:141-143 at db021da0).
// It reads the Settings this cluster was given — the node gives both
// clusters one store; without Settings there is nothing to clear here and
// OperationalCredentials.ClearPendingState clears it as before. Caller
// holds g.mu.
//
// The settings port has no call that clears two keys at once, so the
// commit reads both keys before it clears either — a failed read clears
// nothing — and clears them in [pendingNOCKeys] order, the AddNOC's
// first. A failed clear writes back the keys this commit already cleared,
// so the FAILURE it answers leaves both markers standing and a restart
// rolls back both commands. What is left: a node that goes down between
// the two clears, before it answers, comes back with the AddNOC committed
// and the UpdateNOC rolled back; and a write-back that fails as well
// leaves the same state behind a FAILURE, until the fail-safe's expiry
// rolls the AddNOC back from memory (review II, R2-P1-rw-06).
func (g *GeneralCommissioning) commitPendingNOCLocked(ctx context.Context) error {
	if g.tcSettings == nil {
		return nil
	}
	standing := make([]string, 0, len(pendingNOCKeys))
	for _, key := range pendingNOCKeys {
		v, _, err := g.tcSettings.GetSetting(ctx, key)
		if err != nil {
			return fmt.Errorf("core: GeneralCommissioning: read the pending NOC marker: %w", err)
		}
		standing = append(standing, v)
	}
	for i, key := range pendingNOCKeys {
		if standing[i] == "" {
			continue
		}
		if err := setPendingNOCSetting(ctx, g.tcSettings, key, ""); err != nil {
			wctx := context.WithoutCancel(ctx)
			for j := range i {
				if standing[j] == "" {
					continue
				}
				if werr := setPendingNOCSetting(wctx, g.tcSettings, pendingNOCKeys[j], standing[j]); werr != nil {
					err = errors.Join(err, fmt.Errorf("write the cleared marker back: %w", werr))
				}
			}
			return fmt.Errorf("core: GeneralCommissioning: commit the pending NOC: %w", err)
		}
	}
	return nil
}

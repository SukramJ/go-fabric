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
// returns. A nil hook is nothing to wait for. Caller holds g.mu.
func (g *GeneralCommissioning) trackRollbackLocked(hook func(ctx context.Context, fabricIndex uint8)) func(ctx context.Context, fabricIndex uint8) {
	if hook == nil {
		return nil
	}
	if g.expiryRollbacksDone == nil {
		g.expiryRollbacksDone = make(chan struct{})
	}
	g.expiryRollbacks++
	return func(ctx context.Context, fabricIndex uint8) {
		defer func() {
			g.mu.Lock()
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
// session: errGencommArmRolledBack when the rollback it ran itself ended
// that session. Caller holds g.mu, as for settleExpiryLocked.
func (g *GeneralCommissioning) settleArmLocked(ctx context.Context, sessFabric uint8) error {
	settled, err := g.settleExpiryLocked(ctx)
	if err != nil {
		return err
	}
	if settled.endedSession(sessFabric) {
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
func (g *GeneralCommissioning) commitPendingNOCLocked(ctx context.Context) error {
	if g.tcSettings == nil {
		return nil
	}
	for _, key := range pendingNOCKeys {
		v, ok, err := g.tcSettings.GetSetting(ctx, key)
		if err != nil {
			return fmt.Errorf("core: GeneralCommissioning: read the pending NOC marker: %w", err)
		}
		if !ok || v == "" {
			continue
		}
		if err := setPendingNOCSetting(ctx, g.tcSettings, key, ""); err != nil {
			return fmt.Errorf("core: GeneralCommissioning: commit the pending NOC: %w", err)
		}
	}
	return nil
}

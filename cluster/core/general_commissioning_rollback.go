// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
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

// settleExpiryLocked returns once no fail-safe expiry is left half done: a
// window that is armed but due is expired here, its hook run by this
// caller, and a rollback running on another goroutine is waited for —
// unless ctx is that rollback's own, or ctx ends first, which is the
// error. Caller holds g.mu; it is held again on return, and released in
// between.
func (g *GeneralCommissioning) settleExpiryLocked(ctx context.Context) error {
	inRollback := ctx.Value(expiryRollbackKey{}) != nil
	for {
		if g.failSafeArmed && !g.failSafeExpiresAt.After(g.clock.Now()) {
			fabricIndex := g.failSafeFabricIndex
			if w := g.failSafeWatch; w != nil {
				fabricIndex = w.fabricIndex
			}
			after := g.expireLocked(fabricIndex)
			g.mu.Unlock()
			after(ctx)
			g.mu.Lock()
			continue
		}
		done := g.expiryRollbacksDone
		if done == nil || inRollback {
			return nil
		}
		g.mu.Unlock()
		select {
		case <-done:
			g.mu.Lock()
		case <-ctx.Done():
			g.mu.Lock()
			return fmt.Errorf("core: GeneralCommissioning: arm while the expired fail-safe rolls back: %w", ctx.Err())
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
		if err := g.tcSettings.SetSetting(ctx, key, ""); err != nil {
			return fmt.Errorf("core: GeneralCommissioning: commit the pending NOC: %w", err)
		}
	}
	return nil
}

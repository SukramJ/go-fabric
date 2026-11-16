// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"crypto/ecdsa"
	"slices"
	"time"
)

// UpTimeFrom exposes upTimeFrom to the external parity tests: UpTime from
// an elapsed monotonic and an elapsed wall-clock time, which a
// clocktest.Fake cannot step apart (MockTime.stepWallClock moves the wall
// clock alone).
func (g *GeneralDiagnostics) UpTimeFrom(mono, wall time.Duration) time.Duration {
	return g.upTimeFrom(mono, wall)
}

// RegisterAuxiliaryProvider exposes registerAuxiliaryProvider to the
// external parity tests, which emit provider entries the node's one
// provider (Groupcast) cannot produce — the test's free observable,
// createProvider (AccessControlAuxiliaryTest.ts:32-34).
func (a *AccessControl) RegisterAuxiliaryProvider(ctx context.Context, src AuxiliaryACLSource) {
	a.registerAuxiliaryProvider(ctx, src)
}

// OnRollbackWaitsForNOC sets fn to run when the fail-safe's rollback finds
// a NOC command in flight and waits for it (nocContended).
func (o *OperationalCredentials) OnRollbackWaitsForNOC(fn func()) {
	o.mu.Lock()
	o.nocContended = fn
	o.mu.Unlock()
}

// CSRKeyPending reports whether the fail-safe context holds a CSR's
// operational key pair (pendingPrivKey).
func (o *OperationalCredentials) CSRKeyPending() bool {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.pendingPrivKey != nil
}

// CheckGroupKeyMapping exposes checkGroupKeyMapping to the external parity
// tests: the GroupKeyMap check a Groupcast binding passes through, whose
// key set id the Groupcast commands already hold to "min 1" before it.
func (g *GroupKeyManagement) CheckGroupKeyMapping(ctx context.Context, fabric uint8, groupID, keySetID uint16) error {
	return g.checkGroupKeyMapping(ctx, fabric, groupID, keySetID)
}

// NOCLockFree reports whether nocMu is free: a hook that runs it observes
// whether the command that called it still holds the lock (review II,
// R2-P6-conc-1003).
func (o *OperationalCredentials) NOCLockFree() bool {
	if o.nocMu.TryLock() {
		o.nocMu.Unlock()
		return true
	}
	return false
}

// FailCSRBuild makes every later CSR build fail with err (review II,
// N4-coresec-2).
func (o *OperationalCredentials) FailCSRBuild(err error) {
	o.nocMu.Lock()
	o.csrBuilder = func(*ecdsa.PrivateKey) ([]byte, error) { return nil, err }
	o.nocMu.Unlock()
}

// BeginAuxiliaryBatch and EndAuxiliaryBatch expose the batch a Groupcast
// command holds open (review II, R2-P6-conc-1007).
func (a *AccessControl) BeginAuxiliaryBatch()                  { a.beginAuxiliaryBatch() }
func (a *AccessControl) EndAuxiliaryBatch(ctx context.Context) { a.endAuxiliaryBatch(ctx) }

// StaleGroupFabrics reports the fabrics GroupKeyManagement holds marked
// stale (review II, R2-P6-err-1701 R-1).
func (g *GroupKeyManagement) StaleGroupFabrics() []uint8 {
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make([]uint8, 0, len(g.staleGroups))
	for f := range g.staleGroups {
		out = append(out, f)
	}
	return out
}
// PendingSceneChanges is the number of consequences waiting for st's table
// to load (review II, R2-P6-store-1202).
func PendingSceneChanges(st *ScenesState) int {
	st.mu.Lock()
	defer st.mu.Unlock()
	return len(st.pending)
}

// SceneTable loads st's table and returns a copy of it (review II,
// R2-P6-store-1212).
func SceneTable(st *ScenesState) []SceneEntry {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.ensureLoaded(context.Background())
	return slices.Clone(st.table)
}

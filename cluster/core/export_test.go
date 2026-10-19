// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
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
// provider (Groupcast) cannot produce — matter.js registerAuxAclProvider
// with a free observable (AccessControlAuxiliaryTest.ts:32-34).
func (a *AccessControl) RegisterAuxiliaryProvider(ctx context.Context, src AuxiliaryACLSource) {
	a.registerAuxiliaryProvider(ctx, src)
}

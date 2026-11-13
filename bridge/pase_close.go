// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"time"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/operational"
)

// paseCloseGrace is how long the PASE sessions outlive the
// CommissioningComplete response: a commissioner may send the command over
// the fabric-adopted PASE session, and the peer's ack of the response
// arrives on it (the delayed teardown RemoveFabric uses too).
const paseCloseGrace = 100 * time.Millisecond

// generalCommissioningClusterID is the GeneralCommissioning cluster.
const generalCommissioningClusterID uint32 = 0x0030

// sessionLister is the optional capability of a [SessionRegistry] to list
// its open sessions (operational.Manager.Sessions). The bridge needs it to
// clear the PASE sessions when a commissioning ends.
type sessionLister interface {
	Sessions() []operational.SessionInfo
}

// FailSafeExpired clears every PASE session open now, the ones AddNOC
// adopted onto a fabric included: the fail-safe expired without a
// CommissioningComplete, and what it covered is rolled back. Inside an
// interaction (ctx is the command's context — ArmFailSafe with expiry 0,
// RevokeCommissioning) the sessions close once its response is out,
// otherwise at once. The host calls it from the root's fail-safe-expiry
// hook (core.GeneralCommissioning.SetOnFailSafeExpired), after its own
// rollback; the node facade does. A session with the commissioning
// channel's implicit Administer stays open without it. Needs a
// [SessionRegistry] that lists its sessions (the operational manager
// does); without one it does nothing.
//
// Mirrors matter.js packages/protocol/src/common/FailsafeContext.ts:277-279
// at db021da0 (rollback, step 1: "Terminate any open PASE secure session by
// clearing any associated Secure Session Context at the Server" →
// closePaseSession with the current exchange).
func (b *Bridge) FailSafeExpired(ctx context.Context) {
	b.closePASESessions(ctx, 0)
}

// closePASESessionsOnCommissioningComplete clears the PASE sessions after
// a CommissioningComplete the root's GeneralCommissioning answered OK —
// Matter §11.10.6.6 step 4 — once the response is out and
// [paseCloseGrace] has elapsed. It reads the command's own response, as
// adoptPASESessionOnAddNOC does, so it runs for every host that mounts
// core.GeneralCommissioning, with or without the node facade.
//
// Mirrors matter.js packages/protocol/src/common/FailsafeContext.ts:152-153
// at db021da0 (completeCommission, step 4: "The Secure Session Context of
// any PASE session still established at the Server SHALL be cleared" →
// closePaseSession).
func (b *Bridge) closePASESessionsOnCommissioningComplete(ctx context.Context, resp im.InvokeResponse) {
	for _, e := range resp.Responses {
		if e.Path.Cluster != generalCommissioningClusterID || !e.HasResponse {
			continue
		}
		done, ok := e.Response.(core.CommissioningCompleteResponse)
		if !ok || done.ErrorCode != core.CommissioningErrorOK {
			continue
		}
		b.closePASESessions(ctx, paseCloseGrace)
		return
	}
}

// closePASESessions clears the PASE sessions open now, including the ones
// AddNOC adopted onto a fabric (they stay PASE sessions,
// operational.Manager.AdoptFabricIndex), once the response of the
// interaction ctx belongs to has been sent (at once without one) and grace
// has elapsed after it. The sessions are named when this is called and
// closed by id: a PASE session a new commissioning establishes within the
// grace is not this fail-safe context's and stays (matter.js closes the
// PASE session it finds when the hook runs, FailsafeContext.ts:187-192 at
// db021da0, closePaseSession).
func (b *Bridge) closePASESessions(ctx context.Context, grace time.Duration) {
	b.mu.RLock()
	reg := b.sessionRegistry
	b.mu.RUnlock()
	lister, ok := reg.(sessionLister)
	if !ok {
		return
	}
	var ids []uint16
	for _, s := range lister.Sessions() {
		if s.IsPASE {
			ids = append(ids, s.SessionID)
		}
	}
	if len(ids) == 0 {
		return
	}
	closeNow := func() {
		for _, id := range ids {
			_ = reg.Close(id) // gone already: nothing to clear
		}
	}
	run := closeNow
	if grace > 0 {
		run = func() { b.clk().AfterFunc(grace, closeNow) }
	}
	if !im.DeferAfterResponse(ctx, run) {
		run()
	}
}

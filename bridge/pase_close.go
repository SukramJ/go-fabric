// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"time"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/contract"
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
// otherwise — the fail-safe's timer, whose context belongs to an
// interaction long answered — at once.
//
// The bridge calls it itself: [Bridge.AttachRootClusters] wires it into the
// fail-safe expiry of a core.GeneralCommissioning among the root clusters
// (wireFailSafeExpiry), where it runs after the host's own
// fail-safe-expiry hook, for every host. A host that calls it from that
// hook as well — the wiring before the bridge had its own — loses nothing:
// a second call closes the same sessions again and finds them gone. Needs a
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

// wireFailSafeExpiry has the root's GeneralCommissioning clear the PASE
// sessions when its fail-safe expires — the bridge's own hook, beside the
// host's (core.GeneralCommissioning.SetOnFailSafeRolledBack). Before, the
// close was the host's call from its fail-safe-expiry hook, and a host
// that wired the bridge without the node facade and did not make it kept
// the commissioning channel's implicit Administer open (review II,
// R2-P0-sec-A03, owner decision 9). Like wireCommissioned and
// adoptPASESessionOnAddNOC it covers a host that mounts
// core.GeneralCommissioning; a host with a GeneralCommissioning server of
// its own calls [Bridge.FailSafeExpired].
func (b *Bridge) wireFailSafeExpiry(servers []contract.ClusterServer) {
	for _, s := range servers {
		if gc, ok := s.(*core.GeneralCommissioning); ok {
			gc.SetOnFailSafeRolledBack(b.FailSafeExpired)
		}
	}
}

// closePASESessionsOnRevoke is what a [CommissioningWindow] attached to
// the bridge runs when it is revoked: the PASE sessions open at that
// moment are closed, the ones AddNOC adopted onto a fabric included. The
// window decides when (after the response, when the revoke arrived over
// the PASE session itself). Mirrors matter.js
// packages/node/src/behaviors/administrator-commissioning/AdministratorCommissioningServer.ts:137-141
// at db021da0 (revokeCommissioning, "Step 1: Regardless of window state —
// terminate PASE sessions and expire PASE-held fail-safe":
// `SessionManager.getPaseSession()` → `paseSession.initiateClose()`).
func (b *Bridge) closePASESessionsOnRevoke() {
	if closeNow := b.paseSessionsCloser(); closeNow != nil {
		closeNow()
	}
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
	closeNow := b.paseSessionsCloser()
	if closeNow == nil {
		return
	}
	run := closeNow
	if grace > 0 {
		run = func() { b.clk().AfterFunc(grace, closeNow) }
	}
	if !im.DeferAfterResponse(ctx, run) {
		run()
	}
}

// paseSessionsCloser names the PASE sessions open now and returns what
// closes exactly those, by id; nil when there is none, or when the
// [SessionRegistry] does not list its sessions.
func (b *Bridge) paseSessionsCloser() func() {
	b.mu.RLock()
	reg := b.sessionRegistry
	b.mu.RUnlock()
	lister, ok := reg.(sessionLister)
	if !ok {
		return nil
	}
	var ids []uint16
	for _, s := range lister.Sessions() {
		if s.IsPASE {
			ids = append(ids, s.SessionID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	// Named for closing, the sessions are closing: no new PASE exchange
	// waits for their grace ([Bridge.paseSessionExists]).
	b.mu.Lock()
	if b.paseClosing == nil {
		b.paseClosing = make(map[uint16]struct{})
	}
	for _, id := range ids {
		b.paseClosing[id] = struct{}{}
	}
	b.mu.Unlock()
	return func() {
		for _, id := range ids {
			_ = reg.Close(id) // gone already: nothing to clear
		}
		b.mu.Lock()
		for _, id := range ids {
			delete(b.paseClosing, id)
		}
		b.mu.Unlock()
	}
}

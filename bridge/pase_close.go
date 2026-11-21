// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"log/slog"
	"strconv"
	"strings"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/operational"
)

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
// Mirrors matter.js packages/protocol/src/common/FailsafeContext.ts:281-283
// at b25ed960 (rollback, step 1: "Terminate any open PASE secure session by
// clearing any associated Secure Session Context at the Server" →
// closePaseSession with the current exchange).
func (b *Bridge) FailSafeExpired(ctx context.Context) {
	b.closePASESessions(ctx, nil)
}

// failSafeSessionsClosed is [Bridge.FailSafeExpired] that names the PASE
// sessions it closes: the root's GeneralCommissioning's session closer
// (core.GeneralCommissioning.SetFailSafeSessionCloser), so an arm for a PASE
// session opened after the close is not refused (review II,
// R2-P2-comm-11).
//
// The closer runs on every end of the fail-safe without commissioning —
// its timer, ArmFailSafe with expiry 0, an expiry by RevokeCommissioning,
// the removal of its fabric — and never on CommissioningComplete. Such an
// end is a failed commissioning attempt of the window the fail-safe was
// armed in, when that window is still open; one armed with no window open,
// or whose window closed or was replaced since, is not counted. Mirrors
// matter.js packages/protocol/src/protocol/DeviceCommissioner.ts:160-174 at
// b25ed960 (beginTimed: the window captured at the context's start, counted
// when the context is destroyed) and :181-184 (#countFailedAttempt returns
// when `this.#window !== window`). A successful commissioning is excluded by
// order there (FailsafeContext.ts:152 emits commissioned, which closes the
// window, before :159 closes the context); here the closer does not run on
// that path at all.
func (b *Bridge) failSafeSessionsClosed(ctx context.Context) []uint16 {
	closed := b.closePASESessions(ctx, nil)
	if armedIn := b.failSafeWindow.Swap(0); armedIn != 0 {
		if w := b.attemptWindow.Load(); w != nil {
			if generation, open := w.windowGeneration(); open && generation == armedIn {
				b.recordFailedAttempt(attemptFailSafeEnded)
			}
		}
	}
	return closed
}

// trackFailSafeAttempt is the root's GeneralCommissioning's attempt tracker
// (core.GeneralCommissioning.SetFailSafeAttemptTracker): when a fail-safe
// context starts it records the window open now (none: 0), and when the
// server closes it forgets the record, so the fail-safe the node's stop
// expires counts for no window — matter.js closes the window before the
// fail-safe at shutdown (DeviceCommissioner.ts:361-374 at b25ed960). It runs
// under the server's lock and takes no lock of the bridge's.
func (b *Bridge) trackFailSafeAttempt(_ context.Context, started bool) {
	var armedIn uint64
	if started {
		if w := b.attemptWindow.Load(); w != nil {
			if generation, open := w.windowGeneration(); open {
				armedIn = generation
			}
		}
	}
	b.failSafeWindow.Store(armedIn)
}

// wireFailSafeExpiry has the root's GeneralCommissioning clear the PASE
// sessions when its fail-safe expires — the bridge's own hook, beside the
// host's (core.GeneralCommissioning.SetFailSafeSessionCloser). Before, the
// close was the host's call from its fail-safe-expiry hook, and a host
// that wired the bridge without the node facade and did not make it kept
// the commissioning channel's implicit Administer open (review II,
// R2-P0-sec-A03, owner decision 9). Like wireCommissioned and
// adoptPASESessionOnAddNOC it covers a host that mounts
// core.GeneralCommissioning; a host with a GeneralCommissioning server of
// its own calls [Bridge.FailSafeExpired]. It wires the attempt tracker
// (core.GeneralCommissioning.SetFailSafeAttemptTracker) beside the closer:
// the two together count a fail-safe that ends without commissioning as a
// failed commissioning attempt ([Bridge.failSafeSessionsClosed]).
func (b *Bridge) wireFailSafeExpiry(servers []contract.ClusterServer) {
	for _, s := range servers {
		if gc, ok := s.(*core.GeneralCommissioning); ok {
			gc.SetFailSafeSessionCloser(b.failSafeSessionsClosed)
			gc.SetFailSafeAttemptTracker(b.trackFailSafeAttempt)
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
	if closeNow, _ := b.paseSessionsCloser(); closeNow != nil {
		closeNow()
	}
}

// closePASESessionsOnCommissioningComplete clears the PASE sessions open
// now — the one AddNOC adopted onto the fabric included — when the root's
// GeneralCommissioning answered a CommissioningComplete OK (Matter
// §11.10.6.6 step 4): at once, inside the command's handling and before
// its response goes out, silently (no CloseSession). It reads the
// command's own response, as adoptPASESessionOnAddNOC does, so it runs for
// every host that mounts core.GeneralCommissioning, with or without the
// node facade. ctx is not consulted: nothing waits for the response.
//
// Mirrors matter.js
// packages/node/src/behaviors/general-commissioning/GeneralCommissioningServer.ts:255
// at b25ed960 (commissioningComplete awaits completeCommission before :268
// builds the response) and packages/protocol/src/common/FailsafeContext.ts
// :156-157 (completeCommission, step 4: "The Secure Session Context of any
// PASE session still established at the Server SHALL be cleared" →
// closePaseSession) and :191-196 (closePaseSession →
// initiateForceClose, no current exchange): the session is marked peer
// lost (packages/protocol/src/session/NodeSession.ts:450-453), its
// exchanges close with no deferral (Session.ts:331-344) and it closes
// without gracefulClose (NodeSession.ts:485-504) — no timer on the path.
// The response cannot need the PASE session: the command is CASE-only
// (GeneralCommissioningServer.ts:222-227; here
// cluster/core/general_commissioning.go refuses it over PASE and closes
// nothing). Before, the bridge closed after the response plus a 100 ms
// grace, inside which a request on the PASE session was still served
// with the commissioning channel's implicit Administer grant.
//
// It logs the ids it closes (matter.session.pase_close,
// reason=commissioning_complete) as they close: the line a test harness
// reads for "Commissioning channel … is terminated" (TC-DD-3.5 step 15),
// where matter.js logs "Session ended" for the PASE session and chip
// "Released - Type:1 LSID" (matter.js
// support/chip-testing/test/cert/TC-DD-3.5.test.ts:71-74 at db021da0).
func (b *Bridge) closePASESessionsOnCommissioningComplete(_ context.Context, resp im.InvokeResponse) {
	for _, e := range resp.Responses {
		if e.Path.Cluster != generalCommissioningClusterID || !e.HasResponse {
			continue
		}
		done, ok := e.Response.(core.CommissioningCompleteResponse)
		if !ok || done.ErrorCode != core.CommissioningErrorOK {
			continue
		}
		closeNow, ids := b.paseSessionsCloser()
		if closeNow == nil {
			return
		}
		named := make([]string, len(ids))
		for i, id := range ids {
			named[i] = strconv.Itoa(int(id))
		}
		b.logger.Info("matter.session.pase_close",
			slog.String("reason", "commissioning_complete"),
			slog.String("ids", strings.Join(named, ",")))
		closeNow()
		return
	}
}

// closePASESessions clears the PASE sessions open now, including the ones
// AddNOC adopted onto a fabric (they stay PASE sessions,
// operational.Manager.AdoptFabricIndex), once the response of the
// interaction ctx belongs to has been sent (at once without one). The
// sessions are named when this is called and closed by id: a PASE session
// a new commissioning establishes before the response is out is not this
// fail-safe context's and stays (matter.js closes the PASE session it
// finds when the hook runs, FailsafeContext.ts:191-196 at b25ed960,
// closePaseSession). closing, when set, runs with the named ids just
// before they close. It returns the named ids.
func (b *Bridge) closePASESessions(ctx context.Context, closing func(ids []uint16)) []uint16 {
	closeNow, ids := b.paseSessionsCloser()
	if closeNow == nil {
		return nil
	}
	if closing != nil {
		closeIDs := closeNow
		closeNow = func() {
			closing(ids)
			closeIDs()
		}
	}
	if !im.DeferAfterResponse(ctx, closeNow) {
		closeNow()
	}
	return ids
}

// paseSessionsCloser names the PASE sessions open now and returns what
// closes exactly those, by id, with the ids; nil when there is none, or
// when the [SessionRegistry] does not list its sessions.
func (b *Bridge) paseSessionsCloser() (closeNow func(), ids []uint16) {
	b.mu.RLock()
	reg := b.sessionRegistry
	b.mu.RUnlock()
	lister, ok := reg.(sessionLister)
	if !ok {
		return nil, nil
	}
	for _, s := range lister.Sessions() {
		if s.IsPASE {
			ids = append(ids, s.SessionID)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	// Named for closing, the sessions are closing: no new PASE exchange
	// waits for the response their close follows ([Bridge.paseSessionExists]).
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
	}, ids
}

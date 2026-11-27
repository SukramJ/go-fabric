// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

// The commissioning window's limit of failed commissioning attempts, ported
// by meaning from matter.js
// packages/node/test/node/CommissioningAttemptLimitTest.ts at db91886c
// (describe "Commissioning attempt limit", :52) and the case
// AdministratorCommissioningServerTest.ts:523-536 of the same pin. A failed
// attempt is a PASE pairing that failed before its session was established
// or a fail-safe that ends without commissioning while the window it was
// armed in is still open (matter.js
// packages/protocol/src/protocol/DeviceCommissioner.ts:156-199 at db91886c).
//
// The matter.js cases commission over a MockSite; here the root's
// GeneralCommissioning, the commissioning window and the bridge are wired as
// a host without the node facade wires them, and an abandoned commissioning
// is what it leaves behind on the device: a fail-safe armed over the PASE
// session that ends without CommissioningComplete. Log assertions
// (captureLog) become assertions on the counter and the metrics.

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/cluster/wire"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/secure/spake2"
	"thingfabric.dev/go-fabric/transport/mrp"
)

const (
	gencommCmdArmFailSafeID           = 0x00
	gencommCmdCommissioningCompleteID = 0x04
	admcommCmdRevokeCommissioningID   = 0x02
)

// attemptRig is a bridge with the root's GeneralCommissioning and a
// commissioning window, wired as a host without the node facade wires them.
type attemptRig struct {
	b  *Bridge
	gc *core.GeneralCommissioning
	w  *CommissioningWindow
	ac *wire.AdministratorCommissioning
	m  *recordingMetrics
}

func newAttemptRig(t *testing.T, b *Bridge) *attemptRig {
	t.Helper()
	gc, err := core.NewGeneralCommissioning(core.GeneralCommissioningConfig{
		LocationCapability: core.RegulatoryIndoor, FailSafeMaxSeconds: 900,
	})
	if err != nil {
		t.Fatal(err)
	}
	r := &attemptRig{b: b, gc: gc, w: NewCommissioningWindow(), ac: wire.NewAdministratorCommissioning(), m: &recordingMetrics{}}
	b.AttachMetrics(r.m)
	b.AttachRootClusters([]contract.ClusterServer{gc})
	r.w.SetFailSafeChecker(gc)
	r.w.SetFailSafeExpirer(gc)
	b.AttachCommissioningWindow(r.w)
	r.ac.SetController(r.w)
	return r
}

// paseCtx is the context of a command over the PASE session.
func paseCtx() context.Context { return im.WithAuthModePASE(context.Background()) }

// caseCtx is the context of a command over a CASE session of fabric 1.
func caseCtx() context.Context { return im.WithFabricFilter(context.Background(), true, 1) }

// arm invokes ArmFailSafe(expiry) on ctx and returns its error code.
func (r *attemptRig) arm(ctx context.Context, t *testing.T, expiry uint16) uint8 {
	t.Helper()
	resp, err := r.gc.MatterInvoke(ctx, gencommCmdArmFailSafeID, core.ArmFailSafeRequest{ExpiryLengthSeconds: expiry})
	if err != nil {
		t.Fatalf("ArmFailSafe(%d): %v", expiry, err)
	}
	return resp.(core.ArmFailSafeResponse).ErrorCode
}

// abandon is one abandoned commissioning attempt: the commissioner's PASE
// session arms the fail-safe and the fail-safe ends without
// CommissioningComplete (ArmFailSafe 0 over the same session).
func (r *attemptRig) abandon(t *testing.T) {
	t.Helper()
	if code := r.arm(paseCtx(), t, 60); code != core.CommissioningErrorOK {
		t.Fatalf("ArmFailSafe(60) over PASE = %d, want OK", code)
	}
	if code := r.arm(paseCtx(), t, 0); code != core.CommissioningErrorOK {
		t.Fatalf("ArmFailSafe(0) over PASE = %d, want OK", code)
	}
}

func (r *attemptRig) attempts() int32 { return r.b.failedAttempts.Load() }

func (r *attemptRig) openOwn(t *testing.T) {
	t.Helper()
	if err := r.w.OpenOwnWindow(context.Background(), 0); err != nil {
		t.Fatalf("OpenOwnWindow: %v", err)
	}
}

func (r *attemptRig) openEnhanced(t *testing.T) {
	t.Helper()
	if err := r.w.OpenWindow(context.Background(), wire.OpenWindowParams{CommissioningTimeoutSeconds: 300, Discriminator: 0xABC}); err != nil {
		t.Fatalf("OpenWindow: %v", err)
	}
}

// TestCommissioningAttemptLimitParity ports CommissioningAttemptLimitTest.ts
// at db91886c. The node's own window reads WindowNotOpen on the
// AdministratorCommissioning cluster here (matter.js keeps the cluster's
// status for administrator windows); matter.js's BasicWindowOpen of
// DeviceCommissioner.windowStatus is the window's IsOpen.
func TestCommissioningAttemptLimitParity(t *testing.T) {
	t.Parallel()

	// :82 — DeviceCommissioner.ts:191-198 (the cap closes the window).
	t.Run("closes the commissioning window after 20 abandoned commissioning attempts", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			r := newAttemptRig(t, newUnstartedBridge(t))
			r.openOwn(t)
			for range paseMaxErrors - 1 {
				r.abandon(t)
			}
			if !r.w.IsOpen() {
				t.Fatalf("window closed after %d abandoned attempts, want it open until the twentieth", paseMaxErrors-1)
			}
			if got := r.attempts(); got != paseMaxErrors-1 {
				t.Fatalf("failed attempts = %d, want %d", got, paseMaxErrors-1)
			}
			r.abandon(t)
			if r.w.IsOpen() {
				t.Fatal("window still open after the twentieth abandoned attempt")
			}
			// Fail-safe ends are no PASE failures.
			if got := r.m.counter(contract.MetricPASEFailures); got != 0 {
				t.Errorf("MetricPASEFailures = %d, want 0", got)
			}
			if got := r.m.counter(contract.MetricPASELockouts); got != 1 {
				t.Errorf("MetricPASELockouts = %d, want 1", got)
			}
		})
	})

	// :95 — DeviceCommissioner.ts:289-299 (every open is a new window with
	// failedAttempts 0, the restart of the node's own window included).
	t.Run("starts counting again in a window the node opens anew", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			r := newAttemptRig(t, newUnstartedBridge(t))
			r.openOwn(t)
			for range paseMaxErrors - 1 {
				r.abandon(t)
			}
			r.openOwn(t) // the node restarts its own window
			r.abandon(t)
			if !r.w.IsOpen() {
				t.Fatal("window closed by the first attempt of a window the node opened anew")
			}
			if got := r.attempts(); got != 1 {
				t.Fatalf("failed attempts = %d in the new window, want 1", got)
			}
		})
	})

	// :119 — PaseServer.ts:39-44, :116-118: a pairing that established its
	// session does not emit pairingFailed; its fail-safe decides the
	// attempt. Ported as its outcome only: no pairing runs here, the
	// session's own 60 s arm (GeneralCommissioningServer.ts:57-67) is made
	// directly and runs out, and one attempt is counted. That the pairing
	// itself counts no PASE failure is not driven through a pairing (the
	// claim slot is released at Pake3 success, securechannel.go handlePase,
	// so a late failure of that handshake is not counted); the metric
	// assertion below only holds that the fail-safe's end does not move
	// MetricPASEFailures.
	t.Run("counts a pairing whose final status report fails only through its fail-safe", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			r := newAttemptRig(t, newUnstartedBridge(t))
			r.openOwn(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if err := r.gc.ArmOnPaseSession(ctx, 7); err != nil {
				t.Fatalf("ArmOnPaseSession: %v", err)
			}
			time.Sleep(61 * time.Second)
			synctest.Wait()
			if got := r.attempts(); got != 1 {
				t.Fatalf("failed attempts after the session's fail-safe ran out = %d, want 1", got)
			}
			if got := r.m.counter(contract.MetricPASEFailures); got != 0 {
				t.Errorf("MetricPASEFailures = %d, want 0: the pairing itself did not fail", got)
			}
		})
	})

	// :142 — FailsafeContext.ts:152/:159: the window closes on
	// commissioned before the context closes, so its end is not counted.
	t.Run("does not count a successful commissioning as a failed attempt", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			r := newAttemptRig(t, newUnstartedBridge(t))
			r.openOwn(t)
			if code := r.arm(paseCtx(), t, 60); code != core.CommissioningErrorOK {
				t.Fatalf("ArmFailSafe(60) over PASE = %d", code)
			}
			r.gc.SetCurrentFabric(1) // AddNOC re-stamps the fail-safe
			resp, err := r.gc.MatterInvoke(caseCtx(), gencommCmdCommissioningCompleteID, nil)
			if err != nil {
				t.Fatalf("CommissioningComplete: %v", err)
			}
			if code := resp.(core.CommissioningCompleteResponse).ErrorCode; code != core.CommissioningErrorOK {
				t.Fatalf("CommissioningComplete = %d, want OK", code)
			}
			if r.w.IsOpen() {
				t.Fatal("window still open after the commissioning completed")
			}
			if got := r.attempts(); got != 0 {
				t.Fatalf("failed attempts after a successful commissioning = %d, want 0", got)
			}
		})
	})

	// :153 — matter.js emits pairingFailed by hand on the window's
	// PaseServer; the PASE entry of the counter is the Go equivalent.
	t.Run("closes an enhanced commissioning window after 20 failed PASE pairings", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			r := newAttemptRig(t, newUnstartedBridge(t))
			r.openEnhanced(t)
			for range paseMaxErrors - 1 {
				r.b.recordFailedAttempt(attemptPASEPairing)
			}
			if got := r.w.CurrentWindow().Status; got != wire.WindowStatusEnhanced {
				t.Fatalf("Status after %d failed pairings = %v, want Enhanced", paseMaxErrors-1, got)
			}
			r.b.recordFailedAttempt(attemptPASEPairing)
			if got := r.w.CurrentWindow().Status; got != wire.WindowStatusClosed {
				t.Fatalf("Status after the twentieth failed pairing = %v, want Closed", got)
			}
		})
	})

	// Beyond the matter.js cases: the window a fail-safe was armed in
	// (DeviceCommissioner.ts:160-164, :181-184 `this.#window !== window`).
	t.Run("a fail-safe armed with no window open does not count", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			r := newAttemptRig(t, newUnstartedBridge(t))
			// A standing acceptor answers PASE with no window open (owner
			// decision O-5).
			if code := r.arm(paseCtx(), t, 60); code != core.CommissioningErrorOK {
				t.Fatalf("ArmFailSafe(60) over PASE = %d", code)
			}
			r.openOwn(t) // a window opening later does not adopt it
			if code := r.arm(paseCtx(), t, 0); code != core.CommissioningErrorOK {
				t.Fatalf("ArmFailSafe(0) = %d", code)
			}
			if got := r.attempts(); got != 0 {
				t.Fatalf("failed attempts = %d, want 0", got)
			}
		})
	})

	t.Run("a fail-safe armed over CASE does not count", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			r := newAttemptRig(t, newUnstartedBridge(t))
			r.openOwn(t)
			// With a window open the CASE arm is refused (GeneralCommissioningServer.ts:82-88) …
			if code := r.arm(caseCtx(), t, 60); code != core.CommissioningErrorBusyWithOtherAdmin {
				t.Fatalf("ArmFailSafe(60) over CASE with a window open = %d, want BusyWithOtherAdmin", code)
			}
			if err := r.w.RevokeWindow(context.Background()); err != nil {
				t.Fatal(err)
			}
			// … and without one it arms in no window.
			if code := r.arm(caseCtx(), t, 60); code != core.CommissioningErrorOK {
				t.Fatalf("ArmFailSafe(60) over CASE = %d, want OK", code)
			}
			if code := r.arm(caseCtx(), t, 0); code != core.CommissioningErrorOK {
				t.Fatalf("ArmFailSafe(0) over CASE = %d", code)
			}
			if got := r.attempts(); got != 0 {
				t.Fatalf("failed attempts = %d, want 0", got)
			}
		})
	})

	t.Run("a fail-safe that outlives its window does not count", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			r := newAttemptRig(t, newUnstartedBridge(t))
			r.openOwn(t)
			if code := r.arm(paseCtx(), t, 60); code != core.CommissioningErrorOK {
				t.Fatalf("ArmFailSafe(60) = %d", code)
			}
			r.openOwn(t) // restarted: a new window
			if code := r.arm(paseCtx(), t, 0); code != core.CommissioningErrorOK {
				t.Fatalf("ArmFailSafe(0) = %d", code)
			}
			if got := r.attempts(); got != 0 {
				t.Fatalf("failed attempts after the restart = %d, want 0", got)
			}
		})
	})

	t.Run("the twentieth failed attempt through a fail-safe end engages the PASE lockout", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			r := newAttemptRig(t, newUnstartedBridge(t))
			r.openOwn(t)
			for range paseMaxErrors {
				r.abandon(t)
			}
			if !r.b.paseLockedOut() {
				t.Fatal("PASE not locked out after the twentieth failed attempt (owner decision O-4)")
			}
		})
	})

	t.Run("the node's stop does not count", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			r := newAttemptRig(t, newUnstartedBridge(t))
			r.openOwn(t)
			if code := r.arm(paseCtx(), t, 60); code != core.CommissioningErrorOK {
				t.Fatalf("ArmFailSafe(60) = %d", code)
			}
			// node.Node's stop: the server closes, then the fail-safe
			// expires (DeviceCommissioner.ts:361-374 closes the window first).
			if err := r.gc.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			r.gc.ExpireFailSafe(context.Background())
			if !r.w.IsOpen() {
				t.Fatal("precondition: the window is open at the expiry")
			}
			if got := r.attempts(); got != 0 {
				t.Fatalf("failed attempts after the stop's expiry = %d, want 0", got)
			}
		})
	})
}

// TestCommissioningAttemptLimitPASEFailureParity ports :108 over the
// production router: a PBKDFParamRequest the acceptor rejects is one failed
// attempt, and the window stays open.
func TestCommissioningAttemptLimitPASEFailureParity(t *testing.T) {
	t.Parallel()
	t.Run("counts a failed PASE pairing as a failed commissioning attempt", func(t *testing.T) {
		t.Parallel()
		r := newAttemptRig(t, newStartedBridge(t))
		r.openOwn(t)
		a := NewPaseAdapterWithFactory(newVerifierFactory(t, nil))
		a.SetPBKDFParams(failureCountTestIterations, failureCountTestSalt(), 1)
		r.b.AttachPaseHandler(a)
		reqBytes := buildTestPBKDFParamRequestPasscodeID(t, bytes.Repeat([]byte{0x22}, spake2.PBKDFRandomSize), 5)
		_ = r.b.dispatchSecureChannel(loopbackSrc(), scHdr(), scProto(mrp.SCOpcodePBKDFParamRequest, 7, false, 0), reqBytes)
		if got := r.attempts(); got != 1 {
			t.Fatalf("failed attempts = %d, want 1", got)
		}
		if got := r.m.counter(contract.MetricPASEFailures); got != 1 {
			t.Fatalf("MetricPASEFailures = %d, want 1", got)
		}
		if !r.w.IsOpen() {
			t.Fatal("window closed by one failed pairing")
		}
	})
}

// TestAdministratorCommissioningAttemptLimitParity ports
// AdministratorCommissioningServerTest.ts:523-536 at db91886c
// (AdministratorCommissioningServer.ts:137-158: the window's state is read
// before the revoke closes the PASE session and the fail-safe).
func TestAdministratorCommissioningAttemptLimitParity(t *testing.T) {
	t.Parallel()
	t.Run("answers success when ending the fail-safe makes the twentieth failed commissioning attempt", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			r := newAttemptRig(t, newUnstartedBridge(t))
			r.openEnhanced(t)
			for range paseMaxErrors - 1 {
				r.abandon(t)
			}
			if got := r.w.CurrentWindow().Status; got != wire.WindowStatusEnhanced {
				t.Fatalf("Status after %d attempts = %v, want Enhanced", paseMaxErrors-1, got)
			}
			if code := r.arm(paseCtx(), t, 60); code != core.CommissioningErrorOK {
				t.Fatalf("ArmFailSafe(60) = %d", code)
			}
			if _, err := r.ac.MatterInvoke(caseCtx(), admcommCmdRevokeCommissioningID, nil); err != nil {
				if errors.Is(err, wire.ErrAdmCommWindowNotOpen) {
					t.Fatal("RevokeCommissioning answered WindowNotOpen: the window's state was read after the fail-safe closed it")
				}
				t.Fatalf("RevokeCommissioning: %v", err)
			}
			if got := r.w.CurrentWindow(); got.Status != wire.WindowStatusClosed {
				t.Fatalf("Status after the revoke = %v, want Closed", got.Status)
			}
			if got := r.attempts(); got != 0 && !r.b.paseLockedOut() {
				t.Fatalf("the twentieth attempt was not counted (attempts %d, no lockout)", got)
			}
			if !r.b.paseLockedOut() {
				t.Fatal("the revoke's twentieth attempt did not engage the lockout (owner decision O-4)")
			}
		})
	})
}

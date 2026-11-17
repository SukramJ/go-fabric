// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/jcm"
	gencomm "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
)

// consentFlow is a flow over a device that answers ArmFailSafe, armed once
// (60 s, cumulative 900 s), and a trust verification whose Consent is the
// given one, wired as the JCM step wires it.
func consentFlow(t *testing.T, consent func(context.Context, TrustInfo) (bool, error)) (*flow, *fakeClock, *failsafeDevice, *jcmTrust) {
	t.Helper()
	dev := &failsafeDevice{info: gencomm.BasicCommissioningInfo{FailSafeExpiryLengthSeconds: 60, MaxCumulativeFailsafeSeconds: 900}}
	f, clock := newTestFlow(t, dev.handle)
	if _, err := f.armFailsafe(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	tr := &jcmTrust{opts: &JointFabricOptions{Consent: consent}, info: jcm.NewInfo(), keepArmed: f.keepFailsafeArmed}
	return f, clock, dev, tr
}

// TestConsentOutlivesTheFailSafe: a consent that takes longer than the
// fail-safe still completes — the flow re-arms the fail-safe while the
// host decides, and stops re-arming once it has (review II,
// R2-P6-fsm-1860, owner decision 215). Before, the fail-safe ran out under
// a slow consent and the flow failed later at the CSR.
func TestConsentOutlivesTheFailSafe(t *testing.T) {
	t.Parallel()
	answer := make(chan bool)
	f, clock, dev, tr := consentFlow(t, func(ctx context.Context, _ TrustInfo) (bool, error) {
		select {
		case ok := <-answer:
			return ok, nil
		case <-ctx.Done():
			return false, ctx.Err()
		}
	})
	result := make(chan jcm.Result, 1)
	go func() { result <- tr.askUserForConsent(t.Context()) }()
	eventually(t, "the re-arm started", f.armIntervalRunning)
	for range 6 { // 150 s: two and a half default fail-safes
		armed := len(dev.armed())
		clock.Advance(BTPIdleAliveInterval)
		eventually(t, "a periodic re-arm", func() bool { return len(dev.armed()) > armed })
	}
	if left := f.failSafeTimeLeft(); left <= 0 {
		t.Fatalf("the fail-safe ran out while the host decided (%v left)", left)
	}
	answer <- true
	if r := <-result; r != jcm.Success {
		t.Fatalf("consent: %s", r)
	}
	if f.armIntervalRunning() {
		t.Fatal("the re-arm outlived the consent")
	}
}

// TestConsentEndsWithTheContext: Commission's context ending while the
// host decides fails the stage at once with the context's error, even for
// a Consent that ignores its ctx; the re-arm stops (R2-P6-fsm-1860).
func TestConsentEndsWithTheContext(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	defer close(release)
	f, _, _, tr := consentFlow(t, func(context.Context, TrustInfo) (bool, error) {
		<-release // ignores ctx
		return true, nil
	})
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan jcm.Result, 1)
	go func() { result <- tr.askUserForConsent(ctx) }()
	eventually(t, "the re-arm started", f.armIntervalRunning)
	cancel()
	select {
	case r := <-result:
		if r == jcm.Success || !errors.Is(tr.cause, context.Canceled) {
			t.Fatalf("consent %s, cause %v; want a failure caused by the cancellation", r, tr.cause)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the stage waited for a Consent that ignores its context")
	}
	if f.armIntervalRunning() {
		t.Fatal("the re-arm outlived the stage")
	}
}

// TestConsentErrorFailsTheStage: an error of Consent fails the stage with
// InternalError and the error as the cause.
func TestConsentErrorFailsTheStage(t *testing.T) {
	t.Parallel()
	boom := errors.New("prompt failed")
	_, _, _, tr := consentFlow(t, func(context.Context, TrustInfo) (bool, error) { return false, boom })
	if r := tr.askUserForConsent(t.Context()); r != jcm.InternalError || !errors.Is(tr.cause, boom) {
		t.Fatalf("consent %s, cause %v", r, tr.cause)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/jcm"
	gencomm "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	"github.com/SukramJ/go-fabric/im"
)

// consentFlow is a flow over a device that answers ArmFailSafe, armed once
// (60 s, cumulative 900 s), and a trust verification whose Consent is the
// given one, wired as the JCM step wires it.
func consentFlow(t *testing.T, consent func(context.Context, TrustInfo) (bool, error)) (*flow, *fakeClock, *failsafeDevice, *jcmTrust) {
	t.Helper()
	return consentFlowWith(t, gencomm.BasicCommissioningInfo{FailSafeExpiryLengthSeconds: 60, MaxCumulativeFailsafeSeconds: 900}, false, consent)
}

// consentFlowWith is consentFlow over a device stating info, on BLE when
// ble is set.
func consentFlowWith(t *testing.T, info gencomm.BasicCommissioningInfo, ble bool, consent func(context.Context, TrustInfo) (bool, error)) (*flow, *fakeClock, *failsafeDevice, *jcmTrust) {
	t.Helper()
	dev := &failsafeDevice{info: info}
	f, clock := newTestFlow(t, dev.handle)
	f.ble = ble
	if _, err := f.armFailsafe(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.stopArmInterval)
	tr := &jcmTrust{opts: &JointFabricOptions{Consent: consent}, info: jcm.NewInfo(), keepArmed: f.keepFailsafeArmed}
	return f, clock, dev, tr
}

// waitingConsent answers what answer gets, or the context's error.
func waitingConsent(answer <-chan bool) func(context.Context, TrustInfo) (bool, error) {
	return func(ctx context.Context, _ TrustInfo) (bool, error) {
		select {
		case ok := <-answer:
			return ok, nil
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
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

// TestConsentKeepsAShortFailSafe: with a FailSafeExpiryLengthSeconds of
// 10 s the re-arm comes every 5 s — half the fail-safe, not the 25 s BLE
// period — so the fail-safe does not lapse between two re-arms (Fable
// read F5).
func TestConsentKeepsAShortFailSafe(t *testing.T) {
	t.Parallel()
	answer := make(chan bool)
	f, clock, dev, tr := consentFlowWith(t, gencomm.BasicCommissioningInfo{FailSafeExpiryLengthSeconds: 10, MaxCumulativeFailsafeSeconds: 900}, false, waitingConsent(answer))
	result := make(chan jcm.Result, 1)
	go func() { result <- tr.askUserForConsent(t.Context()) }()
	eventually(t, "the re-arm started", f.armIntervalRunning)
	for range 8 { // 40 s: four fail-safes
		armed := len(dev.armed())
		clock.Advance(5 * time.Second)
		eventually(t, "a re-arm within half the fail-safe", func() bool { return len(dev.armed()) > armed })
		if left := f.failSafeTimeLeft(); left <= 0 {
			t.Fatalf("the fail-safe lapsed (%v left)", left)
		}
	}
	answer <- true
	if r := <-result; r != jcm.Success {
		t.Fatalf("consent: %s", r)
	}
}

// TestConsentEndsWhenTheFailSafeCannotBeKept: at the cumulative limit, and
// after a re-arm the device refuses, the consent wait ends at once and the
// stage fails with that cause — not a later step (Fable read F5).
func TestConsentEndsWhenTheFailSafeCannotBeKept(t *testing.T) {
	t.Parallel()
	t.Run("the cumulative limit", func(t *testing.T) {
		t.Parallel()
		answer := make(chan bool)
		defer close(answer)
		f, clock, _, tr := consentFlowWith(t, gencomm.BasicCommissioningInfo{FailSafeExpiryLengthSeconds: 60, MaxCumulativeFailsafeSeconds: 100}, false, waitingConsent(answer))
		result := make(chan jcm.Result, 1)
		go func() { result <- tr.askUserForConsent(t.Context()) }()
		eventually(t, "the re-arm started", f.armIntervalRunning)
		for range 5 { // 125 s, past the 100 s limit
			clock.Advance(BTPIdleAliveInterval)
		}
		select {
		case r := <-result:
			if r == jcm.Success || !errors.Is(tr.cause, ErrCommissioningTimeout) {
				t.Fatalf("consent %s, cause %v; want the cumulative timeout", r, tr.cause)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("the consent wait outlived the cumulative fail-safe")
		}
	})
	t.Run("a refused re-arm", func(t *testing.T) {
		t.Parallel()
		answer := make(chan bool)
		defer close(answer)
		f, clock, dev, tr := consentFlow(t, waitingConsent(answer))
		result := make(chan jcm.Result, 1)
		go func() { result <- tr.askUserForConsent(t.Context()) }()
		eventually(t, "the re-arm started", f.armIntervalRunning)
		dev.mu.Lock()
		dev.code = gencomm.CommissioningErrorBusyWithOtherAdmin
		dev.mu.Unlock()
		clock.Advance(BTPIdleAliveInterval)
		select {
		case r := <-result:
			var ge *GeneralCommissioningError
			if r == jcm.Success || !errors.As(tr.cause, &ge) {
				t.Fatalf("consent %s, cause %v; want the refused re-arm", r, tr.cause)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("the consent wait outlived a refused re-arm")
		}
	})
}

// TestConsentAwaitsAReArmInFlight: when the stage ends while a periodic
// re-arm waits for the device, the re-arm is cancelled and awaited before
// askUserForConsent returns, so it overlaps neither the next step nor an
// abort's disarm (Fable read F6).
func TestConsentAwaitsAReArmInFlight(t *testing.T) {
	t.Parallel()
	hold := make(chan struct{})
	t.Cleanup(func() { close(hold) })
	dev := &failsafeDevice{info: gencomm.BasicCommissioningInfo{FailSafeExpiryLengthSeconds: 60, MaxCumulativeFailsafeSeconds: 900}}
	held := make(chan struct{}, 1)
	var holding atomic.Bool
	handle := func(req imRequest) (uint8, []byte, bool) {
		if req.opcode == im.OpcodeInvokeRequest && holding.Load() {
			select {
			case held <- struct{}{}:
			default:
			}
			<-hold // the device sits on the periodic re-arm
		}
		return dev.handle(req)
	}
	f, clock := newTestFlow(t, handle)
	if _, err := f.armFailsafe(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	tr := &jcmTrust{opts: &JointFabricOptions{Consent: waitingConsent(nil)}, info: jcm.NewInfo(), keepArmed: f.keepFailsafeArmed}
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan jcm.Result, 1)
	go func() { result <- tr.askUserForConsent(ctx) }()
	eventually(t, "the re-arm started", f.armIntervalRunning)
	ai := f.currentArmInterval()
	holding.Store(true)
	go clock.Advance(BTPIdleAliveInterval) // the fake clock runs the re-arm inside Advance
	<-held
	cancel()
	select {
	case <-result:
	case <-time.After(5 * time.Second):
		t.Fatal("the stage did not end")
	}
	if n := ai.active.Load(); n != 0 {
		t.Fatalf("%d re-arm(s) still in flight when the stage returned", n)
	}
}

// TestConsentOnBLE: on BLE the re-arm timer runs already; the consent wait
// rides on it, the fail-safe outlives a slow consent, and the timer keeps
// running after the stage, as BLE needs (Fable read F7).
func TestConsentOnBLE(t *testing.T) {
	t.Parallel()
	answer := make(chan bool)
	f, clock, dev, tr := consentFlowWith(t, gencomm.BasicCommissioningInfo{FailSafeExpiryLengthSeconds: 60, MaxCumulativeFailsafeSeconds: 900}, true, waitingConsent(answer))
	f.startArmInterval(context.Background())
	result := make(chan jcm.Result, 1)
	go func() { result <- tr.askUserForConsent(t.Context()) }()
	for range 6 {
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
	if !f.armIntervalRunning() {
		t.Fatal("the BLE re-arm timer ended with the consent")
	}
}

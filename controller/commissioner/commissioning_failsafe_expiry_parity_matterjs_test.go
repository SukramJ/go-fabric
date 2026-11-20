// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

// A port of matter.js packages/node/test/node/CommissioningFailsafeExpiryTest.ts
// at b25ed960 (#4928): which commissioning step failures end the device's
// fail-safe — ArmFailSafe with ExpiryLengthSeconds 0 — and which leave it
// to expire. matter.js runs a whole commissioning against a mock device
// and replaces the steps from the failing one on (:62-79); here the flow
// runs over a scripted device that answers what the fail-safe bookkeeping
// sends (failsafeDevice), with the arm and the failing step as its steps.
// The device's "armed after the failure" is the flow's own end time and
// the expiries the device saw; "failed after 0 ms" of mock time is a flow
// that returns with the fake clock standing.
//
// "sends no expiry before the commissioner armed the failsafe" (:182-189)
// is not ported: this module disarms the fail-safe the PASE session armed
// as well (BD-chip-Commissioner-AbortDisarmsFailSafe, "Every exit after
// PASE"), pinned by TestFlowDisarmsFailsafeStartedNotAnswered.

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/interaction"
	gencomm "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	"github.com/SukramJ/go-fabric/im"
)

var expiryInfo = gencomm.BasicCommissioningInfo{FailSafeExpiryLengthSeconds: 60, MaxCumulativeFailsafeSeconds: 900}

// flowDevices maps a test flow to its scripted device (newLossyTestFlow).
var flowDevices sync.Map

// scriptedOf is the scripted device of a test flow.
func scriptedOf(t *testing.T, f *flow) *scriptedDevice {
	t.Helper()
	d, ok := flowDevices.Load(f)
	if !ok {
		t.Fatal("no scripted device for the flow")
	}
	return d.(*scriptedDevice)
}

// operationalInteraction is the interaction over a second scripted device
// on clock — the CASE session the Reconnect step hands the flow
// (transitionToCase: an interaction bound to that session).
func operationalInteraction(t *testing.T, clock *fakeClock, handle func(imRequest) (uint8, []byte, bool)) (*interaction.Interaction, *exchange.Session) {
	t.Helper()
	dev := &scriptedDevice{session: 0x0202, inbox: make(chan []byte, 1024), handle: handle}
	mgr, err := exchange.NewManager(&deviceConn{dev: dev}, exchange.Config{Clock: clock, Rand: func() float64 { return 0.5 }})
	if err != nil {
		t.Fatal(err)
	}
	s := newScriptedSession(t, mgr, dev)
	go dev.run()
	t.Cleanup(mgr.Close)
	return interaction.New(mgr, s, discard()), s
}

// executeStanding runs the flow while the fake clock stands. A flow that
// waited for a response timeout after the failure would wait until the
// wall-clock bound ends its context, and then return that abort instead of
// the step's error.
func executeStanding(t *testing.T, f *flow) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := f.execute(ctx)
	if ctx.Err() != nil {
		t.Fatalf("the flow waited after the failure: %v", err)
	}
	return err
}

func TestCommissioningFailsafeExpiry(t *testing.T) {
	t.Parallel()
	arm := func(f *flow) step {
		return step{7, 1, "GeneralCommissioning.ArmFailsafe", func(ctx context.Context) (stepResult, error) { return f.armFailsafe(ctx, 0) }}
	}
	failing := func(n int, err error) step {
		return step{n, 1, "Failing step", func(context.Context) (stepResult, error) { return 0, err }}
	}
	// ended is the device's ArmFailSafe(0) answered and the flow counting no
	// fail-safe any more — matter.js's armedAfterFailure false.
	ended := func(t *testing.T, f *flow, dev *failsafeDevice) {
		t.Helper()
		if !slices.Equal(dev.armed(), []uint16{60, 0}) {
			t.Fatalf("the device saw ArmFailSafe %v, want [60 0]: one expiry", dev.armed())
		}
		if !f.failSafeEnd.IsZero() || f.failsafeMayBeArmed() {
			t.Fatal("the flow still counts the fail-safe after the device ended it")
		}
	}
	// leftArmed is no expiry sent: the device keeps the fail-safe until it
	// expires.
	leftArmed := func(t *testing.T, dev *failsafeDevice) {
		t.Helper()
		if !slices.Equal(dev.armed(), []uint16{60}) {
			t.Fatalf("the device saw ArmFailSafe %v, want [60]: no expiry", dev.armed())
		}
	}

	t.Run("ends the failsafe when a step fails with a commissioning error", func(t *testing.T) {
		t.Parallel()
		dev := &failsafeDevice{info: expiryInfo}
		f, _ := newTestFlow(t, dev.handle)
		f.steps = []step{arm(f), failing(15, newError(ErrCommissioning, "Simulated commissioning failure (test)", nil))}
		if err := executeStanding(t, f); !errors.Is(err, ErrCommissioning) {
			t.Fatalf("error %v, want the CommissioningError", err)
		}
		ended(t, f, dev)
	})

	t.Run("ends the failsafe when a step fails with a status response", func(t *testing.T) {
		t.Parallel()
		dev := &failsafeDevice{info: expiryInfo}
		f, _ := newTestFlow(t, dev.handle)
		status := &interaction.StatusError{Status: im.StatusFailure}
		f.steps = []step{arm(f), failing(15, status)}
		// Reported as a CommissioningError (:117, repackErrorAs).
		var ce *CommissioningError
		if err := executeStanding(t, f); !errors.As(err, &ce) || !errors.Is(err, status) {
			t.Fatalf("error %v, want the status response as a CommissioningError", err)
		}
		ended(t, f, dev)
	})

	t.Run("ends the failsafe when a step fails with any other error", func(t *testing.T) {
		t.Parallel()
		dev := &failsafeDevice{info: expiryInfo}
		f, _ := newTestFlow(t, dev.handle)
		boom := errors.New("Simulated certificate issuer failure (test)")
		f.steps = []step{arm(f), failing(15, boom)}
		if err := executeStanding(t, f); err != boom { //nolint:errorlint // returned as is
			t.Fatalf("error %v, want the step's error as it is", err)
		}
		ended(t, f, dev)
	})

	t.Run("ends the failsafe when a step throws an operational reconnect failure", func(t *testing.T) {
		t.Parallel()
		dev := &failsafeDevice{info: expiryInfo}
		f, _ := newTestFlow(t, dev.handle)
		// A reconnect failure whose cause is an unresponsive peer: CASE could
		// not be established, the commissioning channel may still work.
		f.steps = []step{arm(f), failing(18, newError(ErrOperativeConnectionFailed, "Simulated finalization failure (test)", &exchange.PeerUnresponsiveError{Waited: 30 * time.Second}))}
		if err := executeStanding(t, f); !errors.Is(err, ErrOperativeConnectionFailed) {
			t.Fatalf("error %v, want the reconnect failure", err)
		}
		ended(t, f, dev)
	})

	t.Run("leaves the failsafe to expire when the device stopped responding", func(t *testing.T) {
		t.Parallel()
		dev := &failsafeDevice{info: expiryInfo}
		f, _ := newTestFlow(t, dev.handle)
		f.steps = []step{arm(f), failing(15, &exchange.PeerUnresponsiveError{Waited: 30 * time.Second})}
		var pu *exchange.PeerUnresponsiveError
		if err := executeStanding(t, f); !errors.As(err, &pu) {
			t.Fatalf("error %v, want the unresponsive peer", err)
		}
		leftArmed(t, dev)
	})

	t.Run("fails without delay when the step closed the commissioning session", func(t *testing.T) {
		t.Parallel()
		dev := &failsafeDevice{info: expiryInfo}
		f, _ := newTestFlow(t, dev.handle)
		boom := errors.New("Simulated failure after the session closed (test)")
		f.steps = []step{arm(f), {15, 1, "Failing step", func(context.Context) (stepResult, error) {
			f.interaction().Session().Close()
			return 0, boom
		}}}
		if err := executeStanding(t, f); err != boom { //nolint:errorlint // returned as is
			t.Fatalf("error %v, want the step's error as it is", err)
		}
		leftArmed(t, dev)
	})

	// The inverse of "sends no expiry before the commissioner armed the
	// failsafe" (:182-189), which is not ported: the device armed its
	// fail-safe when the PASE session was established, and the flow ends it
	// (BD-chip-Commissioner-AbortDisarmsFailSafe, Every exit after PASE).
	t.Run("ahead of matter.js: ends the PASE session's fail-safe before the flow armed", func(t *testing.T) {
		t.Parallel()
		dev := &failsafeDevice{info: expiryInfo}
		f, _ := newTestFlow(t, dev.handle)
		boom := errors.New("Simulated failure before the failsafe was armed (test)")
		f.steps = []step{failing(0, boom)}
		if err := executeStanding(t, f); err != boom { //nolint:errorlint // returned as is
			t.Fatalf("error %v, want the step's error as it is", err)
		}
		if !slices.Equal(dev.armed(), []uint16{0}) || f.failsafeMayBeArmed() {
			t.Fatalf("the device saw ArmFailSafe %v, want [0]: the PASE session's fail-safe ended", dev.armed())
		}
	})

	// reconnected is the flow after the Reconnect step: the commissioning
	// device armed over the PASE session, the operational one answers over
	// CASE and records its own ArmFailSafe(0)s.
	reconnected := func(t *testing.T, failure func(op *exchange.Session) error) (*flow, *failsafeDevice, *failsafeDevice) {
		t.Helper()
		pase := &failsafeDevice{info: expiryInfo}
		f, clock := newTestFlow(t, pase.handle)
		operational := &failsafeDevice{info: expiryInfo}
		ia, s := operationalInteraction(t, clock, operational.handle)
		f.steps = []step{
			arm(f),
			{18, 1, "Reconnect", func(context.Context) (stepResult, error) {
				f.mu.Lock()
				f.ia = ia
				f.mu.Unlock()
				return stepSuccess, nil
			}},
			{20, 1, "Failing step", func(context.Context) (stepResult, error) { return 0, failure(s) }},
		}
		return f, pase, operational
	}

	t.Run("ends the failsafe over the operational session after reconnect", func(t *testing.T) {
		t.Parallel()
		boom := errors.New("Simulated failure after reconnect (test)")
		f, pase, operational := reconnected(t, func(*exchange.Session) error { return boom })
		if err := executeStanding(t, f); err != boom { //nolint:errorlint // returned as is
			t.Fatalf("error %v, want the step's error as it is", err)
		}
		if !slices.Equal(pase.armed(), []uint16{60}) || !slices.Equal(operational.armed(), []uint16{0}) {
			t.Fatalf("ArmFailSafe over PASE %v and over CASE %v, want [60] and [0]", pase.armed(), operational.armed())
		}
		if !f.failSafeEnd.IsZero() || f.failsafeMayBeArmed() {
			t.Fatal("the flow still counts the fail-safe after the device ended it")
		}
	})

	t.Run("does not reconnect to end the failsafe after the operational session closed", func(t *testing.T) {
		t.Parallel()
		boom := errors.New("Simulated failure after the operational session closed (test)")
		f, pase, operational := reconnected(t, func(s *exchange.Session) error {
			s.Close()
			return boom
		})
		if err := executeStanding(t, f); err != boom { //nolint:errorlint // returned as is
			t.Fatalf("error %v, want the step's error as it is", err)
		}
		if !slices.Equal(pase.armed(), []uint16{60}) || len(operational.armed()) != 0 {
			t.Fatalf("ArmFailSafe over PASE %v and over CASE %v, want [60] and none", pase.armed(), operational.armed())
		}
	})
}

// TestFailsafeExpiryChecksTheResponse: the expiry's ArmFailSafeResponse is
// checked — a device that refuses it keeps its fail-safe, and the flow
// keeps counting it; the refusal is only logged and the step's error is
// what returns. Mirrors matter.js
// packages/protocol/src/peer/ControllerCommissioningFlow.ts:1108-1131 at
// b25ed960 (#expireFailsafe: #ensureGeneralCommissioningSuccess, then the
// end time cleared; a failure logged).
func TestFailsafeExpiryChecksTheResponse(t *testing.T) {
	t.Parallel()
	dev := &failsafeDevice{info: expiryInfo}
	f, _ := newTestFlow(t, func(req imRequest) (uint8, []byte, bool) {
		if expiry, ok := armRequest(req); ok && expiry == 0 {
			dev.code = gencomm.CommissioningErrorBusyWithOtherAdmin
		}
		return dev.handle(req)
	})
	boom := errors.New("step failure")
	f.steps = []step{
		{7, 1, "arm", func(ctx context.Context) (stepResult, error) { return f.armFailsafe(ctx, 0) }},
		{15, 1, "fail", func(context.Context) (stepResult, error) { return 0, boom }},
	}
	if err := executeStanding(t, f); err != boom { //nolint:errorlint // returned as is
		t.Fatalf("error %v, want the step's error", err)
	}
	if !slices.Equal(dev.armed(), []uint16{60, 0}) {
		t.Fatalf("ArmFailSafe %v, want [60 0]", dev.armed())
	}
	if f.failSafeEnd.IsZero() || !f.failsafeMayBeArmed() {
		t.Fatal("a refused expiry counts as the fail-safe's end")
	}
}

// TestFailsafeExpiryAwaitsTheRearmInFlight: a periodic re-arm already sent
// when a step fails is awaited — answered — before the expiry goes out, so
// it cannot arm the fail-safe again behind it. Mirrors matter.js
// packages/protocol/src/peer/ControllerCommissioningFlow.ts:1107 at
// b25ed960 (#expireFailsafe: `await this.#periodicArm`, the promise kept
// at :922-928).
func TestFailsafeExpiryAwaitsTheRearmInFlight(t *testing.T) {
	t.Parallel()
	// In a synctest bubble: synctest.Wait finds the flow durably blocked —
	// on the re-arm it awaits, or on the expiry it sent too early.
	synctest.Test(t, func(t *testing.T) {
		// The bubble starts in 2000, before the CA's certificates may.
		time.Sleep(time.Until(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)))
		dev := &failsafeDevice{info: expiryInfo}
		held, release := make(chan struct{}), make(chan struct{})
		var arms atomic.Int32
		f, clock := newTestFlow(t, func(req imRequest) (uint8, []byte, bool) {
			// The device holds its answer to the periodic re-arm.
			if expiry, ok := armRequest(req); ok && expiry != 0 && arms.Add(1) == 2 {
				close(held)
				<-release
			}
			return dev.handle(req)
		})
		sc := scriptedOf(t, f)
		boom := errors.New("step failure")
		var before atomic.Int32
		f.steps = []step{
			{7, 1, "arm", func(ctx context.Context) (stepResult, error) { return f.armFailsafe(ctx, 0) }},
			{15, 1, "fail", func(ctx context.Context) (stepResult, error) {
				f.startArmInterval(ctx)
				// The timer's re-arm runs inside Advance until it is answered.
				go clock.Advance(f.armPeriod())
				<-held
				before.Store(sc.received.Load())
				return 0, boom
			}},
		}
		done := make(chan error, 1)
		go func() { done <- f.execute(context.Background()) }()
		<-held
		synctest.Wait()
		if b, n := before.Load(), sc.received.Load(); b == 0 || n != b {
			close(release)
			t.Fatalf("%d datagrams went to the device while the re-arm was unanswered: the expiry did not wait", n-b)
		}
		close(release)
		if err := <-done; err != boom { //nolint:errorlint // returned as is
			t.Fatalf("error %v, want the step's error", err)
		}
		if !slices.Equal(dev.armed(), []uint16{60, 60, 0}) || !f.failSafeEnd.IsZero() {
			t.Fatalf("ArmFailSafe %v, want [60 60 0] and the fail-safe ended", dev.armed())
		}
		// Every goroutine of the bubble ends: the manager, then the device.
		f.interaction().Provider().Manager().Close()
		close(sc.inbox)
	})
}

// TestFailsafeExpirySkipCancelsTheRearmInFlight: when the expiry is not
// sent — the device stopped responding — a re-arm in flight is cancelled,
// not awaited, and the failure returns at once. Mirrors matter.js
// packages/protocol/src/peer/ControllerCommissioningFlow.ts:1098-1107 at
// b25ed960 (#expireFailsafe: the timer stopped, the returns of :1101 and
// :1102-1105 before `await this.#periodicArm`).
func TestFailsafeExpirySkipCancelsTheRearmInFlight(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		// The bubble starts in 2000, before the CA's certificates may.
		time.Sleep(time.Until(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)))
		dev := &failsafeDevice{info: expiryInfo}
		held, release := make(chan struct{}), make(chan struct{})
		var arms atomic.Int32
		f, clock := newTestFlow(t, func(req imRequest) (uint8, []byte, bool) {
			// The device holds its answer to the periodic re-arm.
			if expiry, ok := armRequest(req); ok && expiry != 0 && arms.Add(1) == 2 {
				close(held)
				<-release
			}
			return dev.handle(req)
		})
		sc := scriptedOf(t, f)
		f.steps = []step{
			{7, 1, "arm", func(ctx context.Context) (stepResult, error) { return f.armFailsafe(ctx, 0) }},
			{15, 1, "fail", func(ctx context.Context) (stepResult, error) {
				f.startArmInterval(ctx)
				go clock.Advance(f.armPeriod())
				<-held
				return 0, &exchange.PeerUnresponsiveError{Waited: 30 * time.Second}
			}},
		}
		done := make(chan error, 1)
		go func() { done <- f.execute(context.Background()) }()
		<-held
		synctest.Wait()
		var err error
		select {
		case err = <-done:
		default:
			close(release)
			<-done
			synctest.Wait()
			f.interaction().Provider().Manager().Close()
			close(sc.inbox)
			t.Fatal("the failure waited for the re-arm in flight although no expiry is sent")
		}
		close(release)
		synctest.Wait()
		f.interaction().Provider().Manager().Close()
		close(sc.inbox)
		var pu *exchange.PeerUnresponsiveError
		if !errors.As(err, &pu) || !slices.Equal(dev.armed(), []uint16{60, 60}) {
			t.Fatalf("error %v, ArmFailSafe %v, want the unresponsive peer and [60 60]: no expiry", err, dev.armed())
		}
	})
}

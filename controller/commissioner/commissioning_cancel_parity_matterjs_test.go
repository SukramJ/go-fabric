// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

// Ports of matter.js at db91886c, by meaning, of the cancel after PASE
// (#5049): packages/node/test/node/CommissioningCancelTest.ts (the
// describe at :298-402), SplitCommissioningTest.ts "completes when the
// hand-off outlasts the commissioning time limit" and ClientAttestationTest.ts
// cancel › "stops waiting for a DCL lookup when the commissioning is
// canceled". matter.js commissions a MockSite device and cancels at a
// named point; here the flow runs over a scripted device on the fake
// clock, and the point is the request the device withholds or the step
// that cancels. The caller's abort signal is the context — matter.js's two
// forms, "abort signal" and "cancel", are one here. Two of matter.js's
// cases are existing tests: "ends a failsafe the device armed before the
// cancel abandoned the ArmFailSafe response" is
// TestFlowDisarmsFailsafeStartedNotAnswered, "stops the operational CASE
// connection" is TestFlowReconnectAbort.

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/client/discovery"
	"thingfabric.dev/go-fabric/client/handshake"
	"thingfabric.dev/go-fabric/client/interaction"
	gencomm "thingfabric.dev/go-fabric/cluster/spec/generalcommissioning"
	"thingfabric.dev/go-fabric/controller/attestation"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/secure/sessionparams"
)

// errCallerGaveUp is matter.js's ImplementationError("Caller gave up"),
// the cancel's reason.
var errCallerGaveUp = errors.New("caller gave up")

// cancelDevice is a failsafeDevice that counts the expiries (ArmFailSafe
// with expiry 0), answers CommissioningComplete, and withholds what
// withhold names — calling onWithheld then, once.
type cancelDevice struct {
	failsafeDevice
	withhold      func(imRequest) bool
	onWithheld    func()
	expiryAnswers bool
	completeCode  gencomm.CommissioningErrorEnum
	onComplete    func()

	once     sync.Once
	expiries atomic.Int32
}

func newCancelDevice() *cancelDevice {
	return &cancelDevice{
		failsafeDevice: failsafeDevice{info: gencomm.BasicCommissioningInfo{FailSafeExpiryLengthSeconds: 60, MaxCumulativeFailsafeSeconds: 900}},
		expiryAnswers:  true,
	}
}

func (d *cancelDevice) handle(req imRequest) (opcode uint8, payload []byte, ok bool) {
	if d.withhold != nil && d.withhold(req) {
		if d.onWithheld != nil {
			d.once.Do(d.onWithheld)
		}
		return 0, nil, false
	}
	if expiry, ok := armRequest(req); ok && expiry == 0 {
		d.expiries.Add(1)
		if !d.expiryAnswers {
			return 0, nil, false
		}
	}
	if req.opcode == im.OpcodeInvokeRequest {
		if path, _, err := decodeInvoke(req.payload); err == nil && path.Command == gencomm.CmdCommissioningComplete {
			if d.onComplete != nil {
				d.onComplete()
			}
			path.Command = gencomm.CmdCommissioningCompleteResponse
			return im.OpcodeInvokeResponse, invokeResponseOf(path, gencomm.CommissioningCompleteResponse{ErrorCode: d.completeCode}), true
		}
	}
	return d.failsafeDevice.handle(req)
}

// isRead and isCommand name what a device withholds.
func isRead(req imRequest) bool { return req.opcode == im.OpcodeReadRequest }

func isCommand(command uint32) func(imRequest) bool {
	return func(req imRequest) bool {
		if req.opcode != im.OpcodeInvokeRequest {
			return false
		}
		path, _, err := decodeInvoke(req.payload)
		return err == nil && path.Command == command
	}
}

// armStep is step 7.1; regulatoryStep stands for a commissioning step's
// command (matter.js's case cancels at the CSRRequest); readStep for a
// step's read.
func armStep(f *flow) step {
	return step{7, 1, "GeneralCommissioning.ArmFailsafe", func(ctx context.Context) (stepResult, error) { return f.armFailsafe(ctx, 0) }}
}

func regulatoryStep(f *flow) step {
	return step{11, 1, "command", func(ctx context.Context) (stepResult, error) {
		_, err := f.invoke(ctx, gencomm.ClusterID, gencomm.CmdSetRegulatoryConfig, gencomm.SetRegulatoryConfigRequest{CountryCode: "XX", Breadcrumb: f.breadcrumb()}, false)
		return stepSuccess, err
	}}
}

func readStep(f *flow) step {
	return step{8, 1, "read", func(ctx context.Context) (stepResult, error) {
		_, _, err := f.readValues(ctx, rootAttr(gencomm.ClusterID, gencomm.AttrBreadcrumb))
		return stepSuccess, err
	}}
}

// isCancel holds when err is the cancel's reason and the context's error.
func isCancel(err error) bool {
	return errors.Is(err, errCallerGaveUp) && errors.Is(err, context.Canceled)
}

// TestCommissioningCancelParity is CommissioningCancelTest.ts's describe
// (:298-402).
func TestCommissioningCancelParity(t *testing.T) {
	t.Parallel()

	t.Run("stops a commissioning step and ends the failsafe on abort signal", func(t *testing.T) {
		t.Parallel()
		dev := newCancelDevice()
		ctx, cancel := context.WithCancelCause(context.Background())
		defer cancel(nil)
		dev.withhold = isCommand(gencomm.CmdSetRegulatoryConfig)
		dev.onWithheld = func() { cancel(errCallerGaveUp) }
		f, clock := newTestFlow(t, dev.handle)
		f.steps = []step{armStep(f), regulatoryStep(f)}
		_, err := runDriven(t, clock, func() error { return f.execute(ctx) })
		if !isCancel(err) || err.Error() != errCallerGaveUp.Error() || dev.expiries.Load() != 1 || f.failsafeMayBeArmed() {
			t.Fatalf("error %v, %d expiries, may be armed %v; want the reason, 1 and false", err, dev.expiries.Load(), f.failsafeMayBeArmed())
		}
	})

	// matter.js rejects with the reason itself (`throw asError(cancel.reason)`,
	// ControllerCommissioningFlow.ts:486): a reason that wraps the context's
	// error comes back as it is, its message kept.
	t.Run("rejects with the reason itself when it wraps the context's error", func(t *testing.T) {
		t.Parallel()
		dev := newCancelDevice()
		ctx, cancel := context.WithCancelCause(context.Background())
		defer cancel(nil)
		reason := fmt.Errorf("user quit: %w", context.Canceled)
		dev.withhold = isCommand(gencomm.CmdSetRegulatoryConfig)
		dev.onWithheld = func() { cancel(reason) }
		f, clock := newTestFlow(t, dev.handle)
		f.steps = []step{armStep(f), regulatoryStep(f)}
		_, err := runDriven(t, clock, func() error { return f.execute(ctx) })
		if err != reason || dev.expiries.Load() != 1 { //nolint:errorlint // the identity is the point
			t.Fatalf("error %v, %d expiries; want the reason itself (%v) and 1", err, dev.expiries.Load(), reason)
		}
	})

	t.Run("stops a read the device does not answer", func(t *testing.T) {
		t.Parallel()
		dev := newCancelDevice()
		dev.expiryAnswers = false
		ctx, cancel := context.WithCancelCause(context.Background())
		defer cancel(nil)
		var reads atomic.Int32
		dev.withhold = func(req imRequest) bool { return isRead(req) && reads.Add(1) > 1 } // the first read is BasicCommissioningInfo
		var clock *fakeClock
		var canceledAt atomic.Int64
		dev.onWithheld = func() { canceledAt.Store(clock.Now().UnixNano()); cancel(errCallerGaveUp) }
		var f *flow
		f, clock = newTestFlow(t, dev.handle)
		f.steps = []step{armStep(f), readStep(f)}
		end, err := runDriven(t, clock, func() error { return f.execute(ctx) })
		if !isCancel(err) || dev.expiries.Load() != 1 {
			t.Fatalf("error %v, %d expiries; want the reason and 1", err, dev.expiries.Load())
		}
		// The expiry gets no answer either: the commissioning waits only the
		// limit for a canceled expiry.
		if settled := end.Sub(time.Unix(0, canceledAt.Load())); settled >= 3*time.Second {
			t.Fatalf("settled %v after the cancel, want less than 3 s", settled)
		}
	})

	t.Run("runs no further step after a step that does not observe the cancel", func(t *testing.T) {
		t.Parallel()
		dev := newCancelDevice()
		ctx, cancel := context.WithCancelCause(context.Background())
		defer cancel(nil)
		f, _ := newTestFlow(t, dev.handle)
		ran := false
		f.steps = []step{
			armStep(f),
			{8, 1, "custom", func(context.Context) (stepResult, error) { cancel(errCallerGaveUp); return stepSuccess, nil }},
			{9, 1, "after", func(context.Context) (stepResult, error) { ran = true; return stepSuccess, nil }},
		}
		err := f.execute(ctx)
		if !isCancel(err) || ran || dev.expiries.Load() != 1 || f.failsafeMayBeArmed() {
			t.Fatalf("error %v, later step ran %v, %d expiries; want the reason, false and 1", err, ran, dev.expiries.Load())
		}
	})

	t.Run("completes the commissioning when the cancel comes after CommissioningComplete was sent", func(t *testing.T) {
		t.Parallel()
		dev := newCancelDevice()
		ctx, cancel := context.WithCancelCause(context.Background())
		defer cancel(nil)
		dev.onComplete = func() { cancel(errCallerGaveUp) }
		f, _ := newTestFlow(t, dev.handle)
		f.steps = []step{armStep(f), {20, 1, "GeneralCommissioning.Complete", f.completeCommissioning}}
		err := f.execute(ctx)
		if err != nil || dev.expiries.Load() != 0 || f.failsafeMayBeArmed() {
			t.Fatalf("error %v, %d expiries, may be armed %v; want nil, 0 and false", err, dev.expiries.Load(), f.failsafeMayBeArmed())
		}
	})

	t.Run("reports a CommissioningComplete failure instead of a cancel that came while it was pending", func(t *testing.T) {
		t.Parallel()
		dev := newCancelDevice()
		dev.completeCode = gencomm.CommissioningErrorInvalidAuthentication
		ctx, cancel := context.WithCancelCause(context.Background())
		defer cancel(nil)
		dev.onComplete = func() { cancel(errCallerGaveUp) }
		f, _ := newTestFlow(t, dev.handle)
		f.steps = []step{armStep(f), {20, 1, "GeneralCommissioning.Complete", f.completeCommissioning}}
		err := f.execute(ctx)
		if !errors.Is(err, ErrCommissioning) || errors.Is(err, errCallerGaveUp) || errors.Is(err, context.Canceled) {
			t.Fatalf("error %v, want the CommissioningError", err)
		}
	})

	t.Run("completes the commissioning when the cancel comes while an external finalization runs", func(t *testing.T) {
		t.Parallel()
		dev := newCancelDevice()
		ctx, cancel := context.WithCancelCause(context.Background())
		defer cancel(nil)
		f, _ := newTestFlow(t, dev.handle)
		// A finalization that does not observe the cancel (the callback's
		// ctx is Go's, matter.js's gets none).
		f.transition = func(context.Context, handshake.PeerAddress, bool) (*interaction.Interaction, error) {
			cancel(errCallerGaveUp)
			return nil, nil
		}
		f.steps = []step{armStep(f), {18, 1, "Reconnect", f.reconnectWithDevice}, {20, 1, "GeneralCommissioning.Complete", f.completeCommissioning}}
		if err := f.execute(ctx); err != nil {
			t.Fatalf("error %v, want nil", err)
		}
	})

	t.Run("does not start an external finalization after a cancel during the failsafe re-arm before it", func(t *testing.T) {
		t.Parallel()
		dev := newCancelDevice()
		ctx, cancel := context.WithCancelCause(context.Background())
		defer cancel(nil)
		// The re-arm for the CASE reconnect (320 s) is the device's cue.
		var arms atomic.Int32
		base := dev.failsafeDevice.handle
		f, _ := newTestFlow(t, func(req imRequest) (uint8, []byte, bool) {
			if expiry, ok := armRequest(req); ok && expiry != 0 && arms.Add(1) == 2 {
				cancel(errCallerGaveUp)
				return base(req)
			}
			return dev.handle(req)
		})
		var started atomic.Bool
		opts := Options{FinalizeCommissioning: func(context.Context, handshake.PeerAddress, *discovery.Device, *sessionparams.Parameters) error {
			started.Store(true)
			return nil
		}}
		pase := f.interaction().Session()
		f.transition = func(ctx context.Context, addr handshake.PeerAddress, concurrent bool) (*interaction.Interaction, error) {
			return f.c.transitionToCase(ctx, pase, addr, concurrent, opts)
		}
		f.steps = []step{armStep(f), {18, 1, "Reconnect", f.reconnectWithDevice}}
		err := f.execute(ctx)
		if !isCancel(err) || started.Load() || dev.expiries.Load() != 1 || f.failsafeMayBeArmed() {
			t.Fatalf("error %v, finalization started %v, %d expiries; want the reason, false and 1", err, started.Load(), dev.expiries.Load())
		}
	})

	t.Run("stops waiting for the failsafe expiry after a failed step when the cancel comes during it", func(t *testing.T) {
		t.Parallel()
		dev := newCancelDevice()
		ctx, cancel := context.WithCancelCause(context.Background())
		defer cancel(nil)
		var clock *fakeClock
		var canceledAt atomic.Int64
		dev.withhold = func(req imRequest) bool { e, ok := armRequest(req); return ok && e == 0 }
		dev.onWithheld = func() { canceledAt.Store(clock.Now().UnixNano()); cancel(errCallerGaveUp) }
		var f *flow
		f, clock = newTestFlow(t, dev.handle)
		gone := errors.New("the device stopped answering")
		f.steps = []step{armStep(f), {8, 1, "fail", func(context.Context) (stepResult, error) { return 0, gone }}}
		end, err := runDriven(t, clock, func() error { return f.execute(ctx) })
		if !isCancel(err) || errors.Is(err, gone) {
			t.Fatalf("error %v, want the cancel's reason, not the step's", err)
		}
		if settled := end.Sub(time.Unix(0, canceledAt.Load())); settled >= 3*time.Second {
			t.Fatalf("settled %v after the cancel, want less than 3 s", settled)
		}
	})
}

// TestSplitCommissioningHandOffOutlastsTimeLimit is SplitCommissioningTest.ts
// "completes when the hand-off outlasts the commissioning time limit":
// Stop ends the loop before the time-limit check (ControllerCommissioningFlow.ts:425-440
// at db91886c), so a hand-off past MaxCumulativeFailsafeSeconds completes.
func TestSplitCommissioningHandOffOutlastsTimeLimit(t *testing.T) {
	t.Parallel()
	dev := newCancelDevice()
	dev.info.MaxCumulativeFailsafeSeconds = 100
	f, clock := newTestFlow(t, dev.handle)
	f.transition = func(context.Context, handshake.PeerAddress, bool) (*interaction.Interaction, error) {
		clock.Advance(101 * time.Second)
		return nil, nil
	}
	f.steps = []step{armStep(f), {18, 1, "Reconnect", f.reconnectWithDevice}}
	if err := f.execute(context.Background()); err != nil {
		t.Fatalf("error %v, want nil: the hand-off decides", err)
	}
}

// blockingStore is the test trust store whose revocation lookup cancels
// the commissioning and then waits until released — a DCL lookup that
// takes no cancel signal.
type blockingStore struct {
	*attestation.StaticStore
	cancel  context.CancelCauseFunc
	release chan struct{}
}

func (s *blockingStore) IsRevoked([]byte, []byte, string) (bool, error) {
	s.cancel(errCallerGaveUp)
	<-s.release
	return false, nil
}

// TestClientAttestationCancelParity is ClientAttestationTest.ts cancel ›
// "stops waiting for a DCL lookup when the commissioning is canceled": the
// wait for the validation ends with the cancel and its reason is the
// step's error (ControllerCommissioningFlow.ts:1375-1377 at db91886c).
func TestClientAttestationCancelParity(t *testing.T) {
	t.Parallel()
	d := newAttestingDevice(t)
	f := attestingFlow(t, d, attestation.Policy{})
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	store := &blockingStore{StaticStore: attestation.TestStore(), cancel: cancel, release: make(chan struct{})}
	defer close(store.release)
	f.c.ctx.Attestation = store
	done := make(chan error, 1)
	go func() { _, err := f.deviceAttestation(ctx); done <- err }()
	if err := receive(t, "the attestation's return after the cancel", done); !isCancel(err) {
		t.Fatalf("deviceAttestation = %v, want the cancel's reason", err)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/handshake"
	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/spec"
	gencomm "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	"github.com/SukramJ/go-fabric/controller/ca"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// failsafeDevice answers what the failsafe bookkeeping sends: a read of
// BasicCommissioningInfo and ArmFailSafe, whose expiries it records.
type failsafeDevice struct {
	info gencomm.BasicCommissioningInfo
	code gencomm.CommissioningErrorEnum

	mu   sync.Mutex
	arms []uint16
}

func (d *failsafeDevice) armed() []uint16 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.arms)
}

func encodeTLV(f func(e *tlv.Encoder)) []byte {
	e := tlv.NewEncoder()
	f(e)
	b, _ := e.Bytes()
	return b
}

// reportOf encodes a ReportData with one value on the root endpoint.
func reportOf(cluster, attribute uint32, value spec.Encodable) []byte {
	return encodeTLV(func(e *tlv.Encoder) {
		e.StartStruct(tlv.AnonymousTag())
		e.StartArray(tlv.ContextTag(1))
		e.StartStruct(tlv.AnonymousTag())
		e.StartStruct(tlv.ContextTag(1))
		e.PutUint(tlv.ContextTag(0), 1)
		im.ConcreteAttributePath{Cluster: cluster, Attribute: attribute, HasEndpoint: true, HasCluster: true, HasAttribute: true}.MarshalTLV(e, tlv.ContextTag(1))
		value.EncodeTLV(e, tlv.ContextTag(2))
		_ = e.EndContainer()
		_ = e.EndContainer()
		_ = e.EndContainer()
		_ = e.EndContainer()
	})
}

func invokeResponseOf(path im.ConcreteCommandPath, fields spec.Encodable) []byte {
	path.HasEndpoint, path.HasCluster, path.HasCommand = true, true, true
	return encodeTLV(func(e *tlv.Encoder) {
		im.InvokeResponse{Responses: []im.InvokeResponseEntry{{Path: path, HasResponse: true, Response: fields}}}.MarshalTLV(e, func(e *tlv.Encoder, tag tlv.Tag, v any) {
			v.(spec.Encodable).EncodeTLV(e, tag)
		})
	})
}

// decodeInvoke decodes an InvokeRequest's single command and its fields.
func decodeInvoke(payload []byte) (im.ConcreteCommandPath, spec.Node, error) {
	req, err := im.UnmarshalInvokeRequestTLV(tlv.NewDecoder(payload), func(_ im.ConcreteCommandPath, dec *tlv.Decoder, el tlv.Element) (any, error) {
		return spec.ReadContainer(dec, el)
	})
	if err != nil || len(req.Invokes) != 1 {
		return im.ConcreteCommandPath{}, spec.Node{}, errors.New("not one command")
	}
	n, _ := req.Invokes[0].Fields.(spec.Node)
	return req.Invokes[0].Path, n, nil
}

func (d *failsafeDevice) handle(req imRequest) (opcode uint8, payload []byte, ok bool) {
	switch req.opcode {
	case im.OpcodeReadRequest:
		return im.OpcodeReportData, reportOf(gencomm.ClusterID, gencomm.AttrBasicCommissioningInfo, d.info), true
	case im.OpcodeInvokeRequest:
		path, fields, err := decodeInvoke(req.payload)
		if err != nil || path.Command != gencomm.CmdArmFailSafe {
			return 0, nil, false
		}
		var arm gencomm.ArmFailSafeRequest
		_ = arm.DecodeTLV(fields)
		d.mu.Lock()
		d.arms = append(d.arms, arm.ExpiryLengthSeconds)
		d.mu.Unlock()
		path.Command = gencomm.CmdArmFailSafeResponse
		return im.OpcodeInvokeResponse, invokeResponseOf(path, gencomm.ArmFailSafeResponse{ErrorCode: d.code}), true
	}
	return 0, nil, false
}

// newTestFlow is a flow over a scripted device on a fake clock.
func newTestFlow(t *testing.T, handle func(imRequest) (uint8, []byte, bool)) (*flow, *fakeClock) {
	t.Helper()
	return newLossyTestFlow(t, handle, nil)
}

// newLossyTestFlow is newTestFlow over a device that never receives the
// requests lose names (scriptedDevice.lose).
func newLossyTestFlow(t *testing.T, handle func(imRequest) (uint8, []byte, bool), lose func(imRequest) bool) (*flow, *fakeClock) {
	t.Helper()
	clock := newFakeClock()
	dev := &scriptedDevice{session: 0x0101, inbox: make(chan []byte, 1024), handle: handle, lose: lose}
	mgr, err := exchange.NewManager(&deviceConn{dev: dev}, exchange.Config{Clock: clock, Rand: func() float64 { return 0.5 }})
	if err != nil {
		t.Fatal(err)
	}
	s := newScriptedSession(t, mgr, dev)
	go dev.run()
	t.Cleanup(mgr.Close)
	authority, err := ca.New(ca.Config{})
	if err != nil {
		t.Fatal(err)
	}
	fabric, err := NewFabric(authority, FabricConfig{FabricID: 7, RootNodeID: 112233, VendorID: 0xFFF1, Label: "test"})
	if err != nil {
		t.Fatal(err)
	}
	c := &Commissioner{ctx: Context{Exchanges: mgr, Clock: clock, Rand: rand.Reader, Logger: discard()}, log: discard()}
	loc := DefaultRegulatoryLocation
	f := newFlow(c, interaction.New(mgr, s, discard()), Options{Fabric: fabric, RegulatoryLocation: &loc, RegulatoryCountryCode: "XX"},
		fabric.AddressOf(0x55), s.AttestationChallenge(),
		func(context.Context, handshake.PeerAddress, bool) (*interaction.Interaction, error) { return nil, nil })
	return f, clock
}

// advanceStep is a step that moves the fake clock.
func advanceStep(clock *fakeClock, d time.Duration, n int) step {
	return step{n, 1, "advance", func(context.Context) (stepResult, error) {
		clock.Advance(d)
		return stepSuccess, nil
	}}
}

// TestFlowRearmsBelowHalf: after a step with less than half the default
// failsafe left the failsafe is re-armed (:409-423); with more it is not.
// The first arm reads BasicCommissioningInfo for the default and the
// cumulative budget (:845-861).
func TestFlowRearmsBelowHalf(t *testing.T) {
	t.Parallel()
	dev := &failsafeDevice{info: gencomm.BasicCommissioningInfo{FailSafeExpiryLengthSeconds: 60, MaxCumulativeFailsafeSeconds: 900}}
	f, clock := newTestFlow(t, dev.handle)
	f.steps = []step{
		{7, 1, "arm", func(ctx context.Context) (stepResult, error) { return f.armFailsafe(ctx, 0) }},
		advanceStep(clock, 29*time.Second, 8),  // 31 s left: no re-arm
		advanceStep(clock, 2*time.Second, 9),   // 29 s left: re-arm
		advanceStep(clock, 20*time.Second, 10), // 40 s left: no re-arm
	}
	if err := f.execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := dev.armed(); !slices.Equal(got, []uint16{60, 60}) {
		t.Fatalf("ArmFailSafe expiries %v, want [60 60]", got)
	}
	if f.defaultFailSafe != time.Minute || f.lastBreadcrumb != 1 {
		t.Fatalf("default %v breadcrumb %d", f.defaultFailSafe, f.lastBreadcrumb)
	}
	// The requested length is capped by the cumulative maximum (:863-868).
	if _, err := f.armFailsafe(context.Background(), time.Hour); err != nil {
		t.Fatal(err)
	}
	if got := dev.armed(); got[len(got)-1] != 900 {
		t.Fatalf("capped arm %v", got)
	}
}

// TestFlowCumulativeTimeout: past the maximum cumulative failsafe a step's
// success is a CommissioningTimeoutError (:397-407), and the failsafe is
// disarmed (:457).
func TestFlowCumulativeTimeout(t *testing.T) {
	t.Parallel()
	dev := &failsafeDevice{info: gencomm.BasicCommissioningInfo{FailSafeExpiryLengthSeconds: 60, MaxCumulativeFailsafeSeconds: 100}}
	f, clock := newTestFlow(t, dev.handle)
	ran := false
	f.steps = []step{
		{7, 1, "arm", func(ctx context.Context) (stepResult, error) { return f.armFailsafe(ctx, 0) }},
		advanceStep(clock, 101*time.Second, 8),
		{9, 1, "never", func(context.Context) (stepResult, error) { ran = true; return stepSuccess, nil }},
	}
	err := f.execute(context.Background())
	if !errors.Is(err, ErrCommissioningTimeout) || !errors.Is(err, ErrCommissioning) || err.Error() != "commissioner: Commissioning time exceeds the maximum timeframe of 100s" {
		t.Fatalf("error %v", err)
	}
	if ran || !slices.Equal(dev.armed(), []uint16{60, 0}) || !f.failSafeEnd.IsZero() {
		t.Fatalf("ran %v, arms %v", ran, dev.armed())
	}
}

// TestFlowErrorClasses: a CommissioningError disarms and is returned as
// is; an IM status error disarms and is repacked as a CommissioningError
// (:437-462); any other error is returned without a disarm (:463-465); a
// recoverable error is logged and the next step runs (:431-436); Stop ends
// the loop (:426-428).
func TestFlowErrorClasses(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	status := &interaction.StatusError{Status: im.StatusFailure}
	rows := []struct {
		name   string
		err    error
		check  func(error) bool
		disarm bool
	}{
		{"commissioning error", newError(ErrDeviceAlreadyCommissionedToThisFabric, "x", nil), func(e error) bool { return errors.Is(e, ErrDeviceAlreadyCommissionedToThisFabric) }, true},
		{"status error", status, func(e error) bool {
			var ce *CommissioningError
			return errors.As(e, &ce) && ce.Kind == ErrCommissioning && errors.Is(e, status) //nolint:errorlint // the identity of the kind
		}, true},
		{"other error", boom, func(e error) bool { return e == boom }, false}, //nolint:errorlint // returned as is
	}
	for _, r := range rows {
		dev := &failsafeDevice{info: gencomm.BasicCommissioningInfo{FailSafeExpiryLengthSeconds: 60, MaxCumulativeFailsafeSeconds: 900}}
		f, _ := newTestFlow(t, dev.handle)
		f.steps = []step{
			{7, 1, "arm", func(ctx context.Context) (stepResult, error) { return f.armFailsafe(ctx, 0) }},
			{8, 1, "fail", func(context.Context) (stepResult, error) { return 0, r.err }},
		}
		err := f.execute(context.Background())
		want := []uint16{60}
		if r.disarm {
			want = append(want, 0)
		}
		if !r.check(err) || !slices.Equal(dev.armed(), want) {
			t.Errorf("%s: error %v, arms %v", r.name, err, dev.armed())
		}
	}

	dev := &failsafeDevice{}
	f, _ := newTestFlow(t, dev.handle)
	var order []int
	f.steps = []step{
		{98, 1, "label", func(context.Context) (stepResult, error) {
			order = append(order, 98)
			return 0, newError(errRecoverable, "label", nil)
		}},
		{18, 1, "reconnect", func(context.Context) (stepResult, error) { order = append(order, 18); return stepStop, nil }},
		{20, 1, "complete", func(context.Context) (stepResult, error) { order = append(order, 20); return stepSuccess, nil }},
		{10, 2, "recoverable", func(context.Context) (stepResult, error) {
			order = append(order, 10)
			return 0, newError(errRecoverable, "continue", nil)
		}},
	}
	if err := f.execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(order, []int{10, 18}) || len(dev.armed()) != 0 || f.results[[2]int{18, 1}] != stepStop {
		t.Fatalf("order %v, arms %v", order, dev.armed())
	}
}

// TestFlowResetsFailsafeAfterContextEnded: a step that fails because the
// caller's context ended still expires the fail-safe on the device —
// matter.js's #resetFailsafeTimer carries no abort signal
// (ControllerCommissioningFlow.ts:1092-1108 at db021da0, called at :457).
// Found on a BLE-to-Thread pairing whose deadline passed in step 18: the
// reset failed with "context canceled" and the device kept its fail-safe.
// Since review II (R2-P0-rv-C02) such a step is the caller's abort and
// the disarm is disarmAfterAbort's (TestFlowReconnectAbort); what this
// test holds is unchanged.
func TestFlowResetsFailsafeAfterContextEnded(t *testing.T) {
	t.Parallel()
	dev := &failsafeDevice{info: gencomm.BasicCommissioningInfo{FailSafeExpiryLengthSeconds: 60, MaxCumulativeFailsafeSeconds: 900}}
	f, _ := newTestFlow(t, dev.handle)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.steps = []step{
		{7, 1, "arm", func(ctx context.Context) (stepResult, error) { return f.armFailsafe(ctx, 0) }},
		{18, 1, "reconnect", func(ctx context.Context) (stepResult, error) {
			cancel()
			return 0, newError(ErrOperativeConnectionFailed, "Operative reconnection with device failed", ctx.Err())
		}},
	}
	err := f.execute(ctx)
	if !errors.Is(err, ErrOperativeConnectionFailed) || !slices.Equal(dev.armed(), []uint16{60, 0}) {
		t.Fatalf("error %v, arms %v, want the reset (0) after the ended context", err, dev.armed())
	}
}

// abortStep is a step during which the caller gives the commissioning up:
// it ends the caller's context and returns that context's error bare, as a
// step's read or invoke does.
func abortStep(n int, end context.CancelFunc) step {
	return step{n, 1, "abort", func(ctx context.Context) (stepResult, error) {
		end()
		<-ctx.Done()
		return 0, ctx.Err()
	}}
}

// TestFlowDisarmsFailsafeWhenCallerAborts: a commissioning whose context
// ends — cancelled or past its deadline — expires the fail-safe it armed,
// on a context of its own, and returns the context's error unchanged.
// matter.js's flow has no abort signal and rethrows such an error without
// the reset (ControllerCommissioningFlow.ts:463-465 at db021da0); chip's
// commissioner expires the fail-safe when StopPairing cancels
// (src/controller/CHIPDeviceController.cpp:2219-2247 at 6170af84) and
// ignores a failure to (:2233-2236).
func TestFlowDisarmsFailsafeWhenCallerAborts(t *testing.T) {
	t.Parallel()
	info := gencomm.BasicCommissioningInfo{FailSafeExpiryLengthSeconds: 60, MaxCumulativeFailsafeSeconds: 900}
	arm := func(f *flow) step {
		return step{7, 1, "arm", func(ctx context.Context) (stepResult, error) { return f.armFailsafe(ctx, 0) }}
	}

	t.Run("before the fail-safe is armed nothing is sent", func(t *testing.T) {
		t.Parallel()
		dev := &failsafeDevice{info: info}
		f, _ := newTestFlow(t, dev.handle)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		f.steps = []step{abortStep(0, cancel), arm(f)}
		err := f.execute(ctx)
		if err != context.Canceled || len(dev.armed()) != 0 { //nolint:errorlint // returned as is
			t.Fatalf("error %v, arms %v, want context.Canceled and no ArmFailSafe", err, dev.armed())
		}
	})

	t.Run("after the fail-safe is armed it is expired once", func(t *testing.T) {
		t.Parallel()
		dev := &failsafeDevice{info: info}
		f, _ := newTestFlow(t, dev.handle)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		f.steps = []step{arm(f), abortStep(8, cancel)}
		err := f.execute(ctx)
		if err != context.Canceled || !slices.Equal(dev.armed(), []uint16{60, 0}) || !f.failSafeEnd.IsZero() { //nolint:errorlint // returned as is
			t.Fatalf("error %v, arms %v, want context.Canceled and ArmFailSafe [60 0]", err, dev.armed())
		}
	})

	t.Run("a deadline that passes is an abort too", func(t *testing.T) {
		t.Parallel()
		dev := &failsafeDevice{info: info}
		f, _ := newTestFlow(t, dev.handle)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		f.steps = []step{arm(f), abortStep(8, func() {})}
		err := f.execute(ctx)
		if err != context.DeadlineExceeded || !slices.Equal(dev.armed(), []uint16{60, 0}) { //nolint:errorlint // returned as is
			t.Fatalf("error %v, arms %v, want context.DeadlineExceeded and ArmFailSafe [60 0]", err, dev.armed())
		}
	})

	t.Run("after CommissioningComplete nothing is sent", func(t *testing.T) {
		t.Parallel()
		dev := &failsafeDevice{info: info}
		f, _ := newTestFlow(t, func(req imRequest) (uint8, []byte, bool) {
			if req.opcode == im.OpcodeInvokeRequest {
				if path, _, err := decodeInvoke(req.payload); err == nil && path.Command == gencomm.CmdCommissioningComplete {
					path.Command = gencomm.CmdCommissioningCompleteResponse
					return im.OpcodeInvokeResponse, invokeResponseOf(path, gencomm.CommissioningCompleteResponse{}), true
				}
			}
			return dev.handle(req)
		})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		f.steps = []step{arm(f), {20, 1, "complete", f.completeCommissioning}, abortStep(98, cancel)}
		err := f.execute(ctx)
		if err != context.Canceled || !slices.Equal(dev.armed(), []uint16{60}) { //nolint:errorlint // returned as is
			t.Fatalf("error %v, arms %v, want context.Canceled and ArmFailSafe [60]", err, dev.armed())
		}
	})

	t.Run("on a closed session nothing is sent", func(t *testing.T) {
		t.Parallel()
		dev := &failsafeDevice{info: info}
		f, _ := newTestFlow(t, dev.handle)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		f.steps = []step{arm(f), {8, 1, "close and abort", func(ctx context.Context) (stepResult, error) {
			f.interaction().Session().Close()
			cancel()
			return 0, ctx.Err()
		}}}
		err := f.execute(ctx)
		if err != context.Canceled || !slices.Equal(dev.armed(), []uint16{60}) { //nolint:errorlint // returned as is
			t.Fatalf("error %v, arms %v, want context.Canceled and ArmFailSafe [60]", err, dev.armed())
		}
	})

	t.Run("a disarm that fails is logged and the abort is what returns", func(t *testing.T) {
		t.Parallel()
		// The device takes ArmFailSafe(0) and never answers it.
		dev := &failsafeDevice{info: info}
		var silent atomic.Int32
		f, _ := newTestFlow(t, func(req imRequest) (uint8, []byte, bool) {
			if req.opcode == im.OpcodeInvokeRequest {
				if _, fields, err := decodeInvoke(req.payload); err == nil {
					var arm gencomm.ArmFailSafeRequest
					if arm.DecodeTLV(fields) == nil && arm.ExpiryLengthSeconds == 0 {
						silent.Add(1)
						return 0, nil, false
					}
				}
			}
			return dev.handle(req)
		})
		var logged bytes.Buffer
		f.log = slog.New(slog.NewTextHandler(&logged, nil))
		f.abortDisarm = 50 * time.Millisecond
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		f.steps = []step{arm(f), abortStep(8, cancel)}
		start := time.Now()
		err := f.execute(ctx)
		if err != context.Canceled || silent.Load() != 1 { //nolint:errorlint // returned as is
			t.Fatalf("error %v, ArmFailSafe(0) sent %d times, want context.Canceled and once", err, silent.Load())
		}
		if took := time.Since(start); took > abortDisarmTimeout/2 {
			t.Fatalf("the abort took %v: the disarm is not bounded by its own context", took)
		}
		if !strings.Contains(logged.String(), "matter.controller.commissioning.reset_failsafe_failed") {
			t.Fatalf("the failed disarm is not logged: %q", logged.String())
		}
		if f.failSafeEnd.IsZero() {
			t.Fatal("the fail-safe counts as disarmed although the device never confirmed")
		}
	})
}

// armRequest is the expiry of the ArmFailSafe req carries; false when req
// is anything else.
func armRequest(req imRequest) (uint16, bool) {
	if req.opcode != im.OpcodeInvokeRequest {
		return 0, false
	}
	path, fields, err := decodeInvoke(req.payload)
	if err != nil || path.Cluster != gencomm.ClusterID || path.Command != gencomm.CmdArmFailSafe {
		return 0, false
	}
	var arm gencomm.ArmFailSafeRequest
	if arm.DecodeTLV(fields) != nil {
		return 0, false
	}
	return arm.ExpiryLengthSeconds, true
}

// TestFlowDisarmsFailsafeStartedNotAnswered (review II, R2-P0-rv-C01): the
// disarm after an abort is owed from the moment the flow starts an
// ArmFailSafe on the commissioning session, not from the device's answer
// — a request under way may have armed the device, and the flow cannot
// know. connectedhomeip's cancel branch sends its ArmFailSafe(0) whatever
// stage was cancelled (src/controller/CHIPDeviceController.cpp:2219-2247
// at 6170af84; a failed stage goes to kCleanup whatever it was,
// src/controller/AutoCommissioner.cpp:465-468); here nothing is sent
// before the first ArmFailSafe is started. A device without a fail-safe
// answers ArmFailSafe(0) with success and no effect (matter.js
// packages/node/src/behaviors/general-commissioning/GeneralCommissioningServer.ts:96-98
// at db021da0).
func TestFlowDisarmsFailsafeStartedNotAnswered(t *testing.T) {
	t.Parallel()
	info := gencomm.BasicCommissioningInfo{FailSafeExpiryLengthSeconds: 60, MaxCumulativeFailsafeSeconds: 900}
	arm := func(f *flow) step {
		return step{7, 1, "arm", func(ctx context.Context) (stepResult, error) { return f.armFailsafe(ctx, 0) }}
	}
	// unanswered is a device that takes the nth ArmFailSafe with an expiry
	// — it arms — and never answers it; the caller gives up at that
	// moment.
	unanswered := func(dev *failsafeDevice, n int, giveUp context.CancelFunc) func(imRequest) (uint8, []byte, bool) {
		var seen atomic.Int32
		return func(req imRequest) (uint8, []byte, bool) {
			if expiry, ok := armRequest(req); ok && expiry != 0 && int(seen.Add(1)) == n {
				dev.mu.Lock()
				dev.arms = append(dev.arms, expiry)
				dev.mu.Unlock()
				giveUp()
				return 0, nil, false
			}
			return dev.handle(req)
		}
	}

	t.Run("the first ArmFailSafe in flight, the device armed", func(t *testing.T) {
		t.Parallel()
		dev := &failsafeDevice{info: info}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		f, _ := newTestFlow(t, unanswered(dev, 1, cancel))
		f.steps = []step{arm(f)}
		err := f.execute(ctx)
		if err != context.Canceled || !slices.Equal(dev.armed(), []uint16{60, 0}) { //nolint:errorlint // returned as is
			t.Fatalf("error %v, arms %v, want context.Canceled and ArmFailSafe [60 0]: the device armed and was left armed", err, dev.armed())
		}
		if !f.failSafeEnd.IsZero() || f.failsafeMayBeArmed() {
			t.Fatalf("after the confirmed disarm the flow still counts a fail-safe: end %v", f.failSafeEnd)
		}
	})

	t.Run("the first ArmFailSafe in flight, the device never received it", func(t *testing.T) {
		t.Parallel()
		dev := &failsafeDevice{info: info}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var lost atomic.Int32
		f, _ := newLossyTestFlow(t, dev.handle, func(req imRequest) bool {
			if expiry, ok := armRequest(req); ok && expiry != 0 {
				lost.Add(1)
				cancel()
				return true
			}
			return false
		})
		f.steps = []step{arm(f)}
		err := f.execute(ctx)
		// The flow cannot tell this from the case above: one disarm, which
		// the device answers with success and no effect.
		if err != context.Canceled || !slices.Equal(dev.armed(), []uint16{0}) || lost.Load() != 1 { //nolint:errorlint // returned as is
			t.Fatalf("error %v, the device saw %v after %d lost ArmFailSafe, want context.Canceled, [0] and 1", err, dev.armed(), lost.Load())
		}
	})

	t.Run("the read before the first ArmFailSafe in flight: nothing is sent", func(t *testing.T) {
		t.Parallel()
		dev := &failsafeDevice{info: info}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		// Step 7.1 reads BasicCommissioningInfo before it arms.
		f, _ := newTestFlow(t, func(req imRequest) (uint8, []byte, bool) {
			if req.opcode == im.OpcodeReadRequest {
				cancel()
				return 0, nil, false
			}
			return dev.handle(req)
		})
		f.steps = []step{arm(f)}
		err := f.execute(ctx)
		if err != context.Canceled || len(dev.armed()) != 0 || f.failsafeMayBeArmed() { //nolint:errorlint // returned as is
			t.Fatalf("error %v, arms %v, want context.Canceled and no ArmFailSafe", err, dev.armed())
		}
	})

	t.Run("a re-arm in flight", func(t *testing.T) {
		t.Parallel()
		dev := &failsafeDevice{info: info}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		f, clock := newTestFlow(t, unanswered(dev, 2, cancel))
		// 29 s left after the second step: its bookkeeping re-arms.
		f.steps = []step{arm(f), advanceStep(clock, 31*time.Second, 8)}
		err := f.execute(ctx)
		if err != context.Canceled || !slices.Equal(dev.armed(), []uint16{60, 60, 0}) || !f.failSafeEnd.IsZero() { //nolint:errorlint // returned as is
			t.Fatalf("error %v, arms %v, want context.Canceled and ArmFailSafe [60 60 0]", err, dev.armed())
		}
	})

	t.Run("the disarm of an unanswered first ArmFailSafe failing", func(t *testing.T) {
		t.Parallel()
		// The device takes both and answers neither.
		dev := &failsafeDevice{info: info}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		first := unanswered(dev, 1, cancel)
		var disarms atomic.Int32
		f, _ := newTestFlow(t, func(req imRequest) (uint8, []byte, bool) {
			if expiry, ok := armRequest(req); ok && expiry == 0 {
				disarms.Add(1)
				return 0, nil, false
			}
			return first(req)
		})
		var logged bytes.Buffer
		f.log = slog.New(slog.NewTextHandler(&logged, nil))
		f.abortDisarm = 50 * time.Millisecond
		f.steps = []step{arm(f)}
		start := time.Now()
		err := f.execute(ctx)
		if err != context.Canceled || disarms.Load() != 1 { //nolint:errorlint // returned as is
			t.Fatalf("error %v, ArmFailSafe(0) sent %d times, want context.Canceled and once", err, disarms.Load())
		}
		if took := time.Since(start); took > abortDisarmTimeout/2 {
			t.Fatalf("the abort took %v: the disarm is not bounded by its own context", took)
		}
		if !strings.Contains(logged.String(), "matter.controller.commissioning.reset_failsafe_failed") {
			t.Fatalf("the failed disarm is not logged: %q", logged.String())
		}
		if !f.failsafeMayBeArmed() {
			t.Fatal("the fail-safe counts as disarmed although the device never confirmed")
		}
	})
}

// TestFlowReconnectAbort (review II, R2-P0-rv-C02): the caller's context
// ending in the Reconnect step — the flow's longest wait — is the abort
// it is in any other step, although the step wraps the transition's error
// in ErrOperativeConnectionFailed (matter.js
// OperativeConnectionFailedError, ControllerCommissioningFlow.ts:2048 at
// db021da0): the disarm is bounded by the flow's own context, skipped on a
// closed session, and the step's error is returned as it is — it matches
// ErrOperativeConnectionFailed and the context's error. An error that
// only looks like the context's, on a context that has not ended, stays
// the step's failure.
func TestFlowReconnectAbort(t *testing.T) {
	t.Parallel()
	info := gencomm.BasicCommissioningInfo{FailSafeExpiryLengthSeconds: 60, MaxCumulativeFailsafeSeconds: 900}
	// rig is a flow of step 7.1 and the real step 18.1 whose transition
	// is the operational connection's wait: it returns what transition
	// does. logged takes the flow's log.
	rig := func(t *testing.T, handle func(imRequest) (uint8, []byte, bool), transition func(ctx context.Context, f *flow) error) (*flow, *syncBuffer) {
		t.Helper()
		f, _ := newTestFlow(t, handle)
		logged := &syncBuffer{}
		f.log = slog.New(slog.NewTextHandler(logged, &slog.HandlerOptions{Level: slog.LevelDebug}))
		f.transition = func(ctx context.Context, _ handshake.PeerAddress, _ bool) (*interaction.Interaction, error) {
			return nil, transition(ctx, f)
		}
		f.steps = []step{
			{7, 1, "arm", func(ctx context.Context) (stepResult, error) { return f.armFailsafe(ctx, 0) }},
			{18, 1, "Reconnect", f.reconnectWithDevice},
		}
		return f, logged
	}
	// waitForContext is peer.Peer.Connect giving the wait up with the
	// caller's context: that context's error, bare (peer.waitFor).
	waitForContext := func(ctx context.Context, _ *flow) error {
		<-ctx.Done()
		return ctx.Err()
	}

	t.Run("the step's error is returned, matching the context's", func(t *testing.T) {
		t.Parallel()
		dev := &failsafeDevice{info: info}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		f, logged := rig(t, dev.handle, func(ctx context.Context, f *flow) error {
			cancel()
			return waitForContext(ctx, f)
		})
		err := f.execute(ctx)
		var ce *CommissioningError
		if !errors.As(err, &ce) || ce.Kind != ErrOperativeConnectionFailed || ce.Cause != context.Canceled || //nolint:errorlint // the identity of the kind and of the cause
			!errors.Is(err, ErrOperativeConnectionFailed) || !errors.Is(err, context.Canceled) {
			t.Fatalf("error %#v, want a CommissioningError of kind ErrOperativeConnectionFailed caused by context.Canceled", err)
		}
		if err.Error() != "commissioner: Operative reconnection with device failed: context canceled" {
			t.Fatalf("error %q", err)
		}
		// The first arm, the extension for the CASE reconnect (320 s,
		// TestEnsureFailsafeTimerFor), the disarm.
		if got := dev.armed(); !slices.Equal(got, []uint16{60, 320, 0}) || f.failsafeMayBeArmed() {
			t.Fatalf("arms %v, want [60 320 0]", got)
		}
		if l := logged.String(); !strings.Contains(l, "matter.controller.commissioning.aborted") || strings.Contains(l, "matter.controller.commissioning.step_failed") {
			t.Fatalf("the abort is logged as a failed step: %q", l)
		}
	})

	t.Run("a deadline that passes in the step", func(t *testing.T) {
		t.Parallel()
		dev := &failsafeDevice{info: info}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		f, logged := rig(t, dev.handle, waitForContext)
		err := f.execute(ctx)
		if !errors.Is(err, ErrOperativeConnectionFailed) || !errors.Is(err, context.DeadlineExceeded) || !slices.Equal(dev.armed(), []uint16{60, 320, 0}) {
			t.Fatalf("error %v, arms %v, want ErrOperativeConnectionFailed caused by context.DeadlineExceeded and [60 320 0]", err, dev.armed())
		}
		if l := logged.String(); !strings.Contains(l, "matter.controller.commissioning.aborted") {
			t.Fatalf("the abort is not logged: %q", l)
		}
	})

	t.Run("the disarm is bounded by the flow's own context", func(t *testing.T) {
		t.Parallel()
		// The device takes ArmFailSafe(0) and never answers it.
		dev := &failsafeDevice{info: info}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var disarms atomic.Int32
		f, logged := rig(t, func(req imRequest) (uint8, []byte, bool) {
			if expiry, ok := armRequest(req); ok && expiry == 0 {
				disarms.Add(1)
				return 0, nil, false
			}
			return dev.handle(req)
		}, func(ctx context.Context, f *flow) error {
			cancel()
			return waitForContext(ctx, f)
		})
		f.abortDisarm = 50 * time.Millisecond
		done := make(chan error, 1)
		start := time.Now()
		go func() { done <- f.execute(ctx) }()
		// The test's own deadline, as the bridge's tests bound a wait.
		waitCtx, stopWait := context.WithTimeout(t.Context(), abortDisarmTimeout/2)
		defer stopWait()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) || !errors.Is(err, ErrOperativeConnectionFailed) || disarms.Load() != 1 {
				t.Fatalf("error %v, ArmFailSafe(0) sent %d times, want the aborted Reconnect and once", err, disarms.Load())
			}
		case <-waitCtx.Done():
			// The exchange's response timeout runs on the fake clock,
			// which nothing advances: without the flow's bound the
			// disarm waits for good.
			t.Fatalf("the flow has not returned after %v: the disarm is not bounded by its own context", time.Since(start))
		}
		if !strings.Contains(logged.String(), "matter.controller.commissioning.reset_failsafe_failed") {
			t.Fatalf("the failed disarm is not logged: %q", logged.String())
		}
	})

	t.Run("on a closed session nothing is sent", func(t *testing.T) {
		t.Parallel()
		dev := &failsafeDevice{info: info}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		// A device without concurrent connection: the transition closes
		// the commissioning session before it connects
		// (Commissioner.transitionToCase).
		f, logged := rig(t, dev.handle, func(ctx context.Context, f *flow) error {
			f.interaction().Session().Close()
			cancel()
			return waitForContext(ctx, f)
		})
		concurrent := false
		f.data.supportsConcurrentConnection = &concurrent
		err := f.execute(ctx)
		if !errors.Is(err, context.Canceled) || !errors.Is(err, ErrOperativeConnectionFailed) || !slices.Equal(dev.armed(), []uint16{60}) {
			t.Fatalf("error %v, arms %v, want the aborted Reconnect and ArmFailSafe [60]", err, dev.armed())
		}
		l := logged.String()
		if strings.Contains(l, "matter.controller.commissioning.reset_failsafe_failed") {
			t.Fatalf("a disarm was attempted on the closed session: %q", l)
		}
		if !strings.Contains(l, "matter.controller.commissioning.abort_disarm_skipped") {
			t.Fatalf("the skipped disarm is not logged: %q", l)
		}
	})

	t.Run("an error that looks like the context's on a live context is the step's failure", func(t *testing.T) {
		t.Parallel()
		// A wait of the connection's own that ran out: not the caller's.
		dev := &failsafeDevice{info: info}
		f, logged := rig(t, dev.handle, func(context.Context, *flow) error {
			return fmt.Errorf("case: %w", context.DeadlineExceeded)
		})
		err := f.execute(context.Background())
		if !errors.Is(err, ErrOperativeConnectionFailed) || !slices.Equal(dev.armed(), []uint16{60, 320, 0}) {
			t.Fatalf("error %v, arms %v, want ErrOperativeConnectionFailed and the reset [60 320 0]", err, dev.armed())
		}
		if l := logged.String(); strings.Contains(l, "matter.controller.commissioning.aborted") || !strings.Contains(l, "matter.controller.commissioning.step_failed") {
			t.Fatalf("a failed step on a live context is logged as an abort: %q", l)
		}
	})

	t.Run("a failure of the step's own beside an ended context is the step's failure", func(t *testing.T) {
		t.Parallel()
		dev := &failsafeDevice{info: info}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		f, logged := rig(t, dev.handle, func(context.Context, *flow) error {
			cancel()
			return errors.New("peer: unreachable")
		})
		err := f.execute(ctx)
		if !errors.Is(err, ErrOperativeConnectionFailed) || errors.Is(err, context.Canceled) || !slices.Equal(dev.armed(), []uint16{60, 320, 0}) {
			t.Fatalf("error %v, arms %v, want ErrOperativeConnectionFailed and the reset [60 320 0]", err, dev.armed())
		}
		if l := logged.String(); strings.Contains(l, "matter.controller.commissioning.aborted") {
			t.Fatalf("a failure that is not the context's is logged as an abort: %q", l)
		}
	})
}

// syncBuffer is a log sink the flow and its timers may write beside each
// other.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// TestEnsureFailsafeTimerFor: the failsafe must outlive the response wait
// plus the margin; with less left it re-arms for at least the default
// (:895-951).
func TestEnsureFailsafeTimerFor(t *testing.T) {
	t.Parallel()
	dev := &failsafeDevice{info: gencomm.BasicCommissioningInfo{FailSafeExpiryLengthSeconds: 60, MaxCumulativeFailsafeSeconds: 900}}
	f, _ := newTestFlow(t, dev.handle)
	ctx := context.Background()
	if _, err := f.armFailsafe(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if err := f.ensureFailsafeTimerFor(ctx, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if got := dev.armed(); len(got) != 1 {
		t.Fatalf("a 60 s failsafe re-armed for a 5 s wait: %v", got)
	}
	if err := f.ensureFailsafeTimerFor(ctx, CASEReconnectTimeout); err != nil {
		t.Fatal(err)
	}
	// min(maximum response time, 5 min + 10 s) + 10 s = 320 s.
	if got := dev.armed(); len(got) != 2 || got[1] != 320 {
		t.Fatalf("arms %v, want a re-arm for 320 s", got)
	}
	dev.code = gencomm.CommissioningErrorBusyWithOtherAdmin
	_, err := f.armFailsafe(ctx, 0)
	if !errors.Is(err, ErrCommissioning) || err.Error() != `commissioner: Commission error for "armFailSafe": BusyWithOtherAdmin (4)` {
		t.Fatalf("ArmFailSafe error %v", err)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"context"
	"crypto/rand"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	gencomm "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	"github.com/SukramJ/go-fabric/controller/ca"
	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/controller/handshake"
	"github.com/SukramJ/go-fabric/controller/interaction"
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
	clock := newFakeClock()
	dev := &scriptedDevice{session: 0x0101, inbox: make(chan []byte, 1024), handle: handle}
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

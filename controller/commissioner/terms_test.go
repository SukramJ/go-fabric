// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	gencomm "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// termsValue is an encoder of one TLV value.
type termsValue func(e *tlv.Encoder, tag tlv.Tag)

func (v termsValue) EncodeTLV(e *tlv.Encoder, tag tlv.Tag) { v(e, tag) }

// termsDevice serves the root GeneralCommissioning's FeatureMap and, with
// the TC feature, its three requirement attributes to every read, and
// counts the commands it is sent.
type termsDevice struct {
	termsConfig

	mu       sync.Mutex
	invokes  int
	readPath []im.ConcreteAttributePath
}

func (d *termsDevice) handle(req imRequest) (opcode uint8, payload []byte, ok bool) {
	switch req.opcode {
	case im.OpcodeInvokeRequest:
		d.mu.Lock()
		d.invokes++
		d.mu.Unlock()
		return 0, nil, false
	case im.OpcodeReadRequest:
	default:
		return 0, nil, false
	}
	if r, err := im.UnmarshalReadRequestTLV(tlv.NewDecoder(req.payload)); err == nil {
		d.mu.Lock()
		d.readPath = append(d.readPath, r.AttributeRequests...)
		d.mu.Unlock()
	}
	values := map[uint32]spec.Encodable{spec.AttrFeatureMap: uintValue(0)}
	if d.tc {
		values[spec.AttrFeatureMap] = uintValue(gencomm.FeatureTermsAndConditions)
		values[gencomm.AttrTcMinRequiredVersion] = uintValue(d.min)
		values[gencomm.AttrTcAcknowledgementsRequired] = termsValue(func(e *tlv.Encoder, tag tlv.Tag) { e.PutBool(tag, d.required) })
		values[gencomm.AttrTcUpdateDeadline] = termsValue(func(e *tlv.Encoder, tag tlv.Tag) {
			if d.deadline == nil {
				e.PutNull(tag)
				return
			}
			e.PutUint(tag, uint64(*d.deadline))
		})
	}
	return im.OpcodeReportData, encodeTLV(func(e *tlv.Encoder) {
		e.StartStruct(tlv.AnonymousTag())
		e.StartArray(tlv.ContextTag(1))
		for _, a := range []uint32{spec.AttrFeatureMap, gencomm.AttrTcMinRequiredVersion, gencomm.AttrTcAcknowledgementsRequired, gencomm.AttrTcUpdateDeadline} {
			v, ok := values[a]
			if !ok {
				continue
			}
			e.StartStruct(tlv.AnonymousTag())
			e.StartStruct(tlv.ContextTag(1))
			e.PutUint(tlv.ContextTag(0), 1)
			im.ConcreteAttributePath{Cluster: gencomm.ClusterID, Attribute: a, HasEndpoint: true, HasCluster: true, HasAttribute: true}.MarshalTLV(e, tlv.ContextTag(1))
			v.EncodeTLV(e, tlv.ContextTag(2))
			_ = e.EndContainer()
			_ = e.EndContainer()
		}
		_ = e.EndContainer()
		_ = e.EndContainer()
	}), true
}

// termsConfig is what a termsDevice serves.
type termsConfig struct {
	tc       bool
	min      uint16
	required bool
	deadline *uint32
}

// TestTermsRequirementRead: step 0.1 reads the root GeneralCommissioning's
// FeatureMap and, with the TC feature, the requirement; a requirement the
// options do not meet is a *TermsRequiredError naming the version and the
// deadline, matching ErrCommissioning and a *CommissioningError
// (BD-Matter-Commissioner-TermsRequirementRead).
func TestTermsRequirementRead(t *testing.T) {
	t.Parallel()
	deadline := uint32(812_345_678)
	rows := []struct {
		name   string
		dev    termsConfig
		ack    *TermsAndConditionsAcknowledgement
		want   *TermsRequiredError
		noTerm bool
	}{
		{name: "no TC feature", dev: termsConfig{}, noTerm: true},
		{name: "required, no acknowledgement", dev: termsConfig{tc: true, min: 1, required: true}, want: &TermsRequiredError{Version: 1}},
		{name: "required, version met", dev: termsConfig{tc: true, min: 1, required: true}, ack: &TermsAndConditionsAcknowledgement{Version: 1, Acknowledgements: 1}},
		{name: "required, a later version", dev: termsConfig{tc: true, min: 2, required: true}, ack: &TermsAndConditionsAcknowledgement{Version: 3}},
		{name: "required, version below", dev: termsConfig{tc: true, min: 2, required: true, deadline: &deadline}, ack: &TermsAndConditionsAcknowledgement{Version: 1, Acknowledgements: 1}, want: &TermsRequiredError{Version: 2, Deadline: &deadline}},
		{name: "accepted before, no acknowledgement", dev: termsConfig{tc: true, min: 1, required: false}},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			dev := &termsDevice{termsConfig: r.dev}
			f, _ := newTestFlow(t, dev.handle)
			f.opts.TermsAndConditions = r.ack
			res, err := f.getInitialData(context.Background())
			if r.noTerm != (f.data.terms == nil) {
				t.Fatalf("terms %+v, want none %v", f.data.terms, r.noTerm)
			}
			if r.want == nil {
				if err != nil || res != stepSuccess {
					t.Fatalf("step 0.1: %v %v, want success", res, err)
				}
				return
			}
			var te *TermsRequiredError
			if !errors.As(err, &te) {
				t.Fatalf("step 0.1: %v, want a *TermsRequiredError", err)
			}
			if te.Version != r.want.Version || (te.Deadline == nil) != (r.want.Deadline == nil) ||
				(te.Deadline != nil && *te.Deadline != *r.want.Deadline) {
				t.Fatalf("requirement %+v, want %+v", te, r.want)
			}
			var ce *CommissioningError
			if !errors.Is(err, ErrCommissioning) || !errors.As(err, &ce) || ce.Kind != ErrCommissioning { //nolint:errorlint // the identity of the kind
				t.Fatalf("%v does not match ErrCommissioning as a *CommissioningError", err)
			}
			if got, want := err.Error(), "commissioner: "+ce.Message; got != want {
				t.Fatalf("message %q, want %q", got, want)
			}
		})
	}
}

// TestTermsRequirementReadPaths: the requirement rides the second read of
// step 0.1 — seven paths, within the nine a server has to accept
// (CHIPDeviceController.cpp:2581-2582 at 6170af84) — on the root endpoint.
func TestTermsRequirementReadPaths(t *testing.T) {
	t.Parallel()
	dev := &termsDevice{}
	f, _ := newTestFlow(t, dev.handle)
	if _, err := f.getInitialData(context.Background()); err != nil {
		t.Fatal(err)
	}
	dev.mu.Lock()
	defer dev.mu.Unlock()
	if len(dev.readPath) != 8+7 {
		t.Fatalf("%d paths over both reads, want 8 + 7", len(dev.readPath))
	}
	for _, a := range []uint32{spec.AttrFeatureMap, gencomm.AttrTcMinRequiredVersion, gencomm.AttrTcAcknowledgementsRequired, gencomm.AttrTcUpdateDeadline} {
		found := false
		for _, p := range dev.readPath[8:] {
			found = found || (p.Cluster == gencomm.ClusterID && p.Attribute == a && p.HasEndpoint && p.Endpoint == 0)
		}
		if !found {
			t.Errorf("the second read has no root GeneralCommissioning 0x%04X", a)
		}
	}
}

// TestTermsRequiredBeforeArmFailSafe: the flow ends with the requirement
// right after step 0.1 — no ArmFailSafe, no command at all reaches the
// device — and reports step 0.1 failed with it.
func TestTermsRequiredBeforeArmFailSafe(t *testing.T) {
	t.Parallel()
	dev := &termsDevice{termsConfig: termsConfig{tc: true, min: 1, required: true}}
	f, _ := newTestFlow(t, dev.handle)
	var failed []Step
	f.opts.Progress = func(s Step) {
		if s.Phase == StepFailed {
			failed = append(failed, s)
		}
	}
	f.initializeSteps()
	err := f.execute(context.Background())
	var te *TermsRequiredError
	if !errors.As(err, &te) || te.Version != 1 {
		t.Fatalf("execute: %v, want a *TermsRequiredError of version 1", err)
	}
	dev.mu.Lock()
	invokes := dev.invokes
	dev.mu.Unlock()
	if invokes != 0 {
		t.Fatalf("%d commands sent before the requirement ended the flow, want 0", invokes)
	}
	if len(failed) != 1 || failed[0].Number != 0 || failed[0].Sub != 1 || !errors.As(failed[0].Err, &te) {
		t.Fatalf("failed steps %+v, want step 0.1 with the requirement", failed)
	}
}

// TestGeneralCommissioningErrorTyped: every refused GeneralCommissioning
// command is a *GeneralCommissioningError carrying the command and the
// code, its message matter.js's (#ensureGeneralCommissioningSuccess,
// ControllerCommissioningFlow.ts:712-719), matching ErrCommissioning and a
// *CommissioningError of that message.
func TestGeneralCommissioningErrorTyped(t *testing.T) {
	t.Parallel()
	f, _ := newTestFlow(t, func(imRequest) (uint8, []byte, bool) { return 0, nil, false })
	for _, r := range []struct {
		command string
		code    gencomm.CommissioningErrorEnum
		debug   string
		msg     string
	}{
		{"armFailSafe", gencomm.CommissioningErrorBusyWithOtherAdmin, "", `Commission error for "armFailSafe": BusyWithOtherAdmin (4)`},
		{"setRegulatoryConfig", gencomm.CommissioningErrorValueOutsideRange, "too far", `Commission error for "setRegulatoryConfig": ValueOutsideRange (1), too far`},
		{"setTCAcknowledgements", gencomm.CommissioningErrorRequiredTcNotAccepted, "", `Commission error for "setTCAcknowledgements": RequiredTcNotAccepted (5)`},
		{"commissioningComplete", gencomm.CommissioningErrorTcAcknowledgementsNotReceived, "", `Commission error for "commissioningComplete": TcAcknowledgementsNotReceived (6)`},
	} {
		err := f.ensureGeneralCommissioningSuccess(r.command, r.code, r.debug)
		var ge *GeneralCommissioningError
		if !errors.As(err, &ge) || ge.Command != r.command || ge.Code != r.code || ge.DebugText != r.debug {
			t.Errorf("%s: %v, want a *GeneralCommissioningError of code %d", r.command, err, r.code)
			continue
		}
		var ce *CommissioningError
		if !errors.Is(err, ErrCommissioning) || !errors.As(err, &ce) || ce.Kind != ErrCommissioning || ce.Message != r.msg { //nolint:errorlint // the identity of the kind
			t.Errorf("%s: %v, want the CommissioningError %q", r.command, err, r.msg)
		}
		if err.Error() != "commissioner: "+r.msg {
			t.Errorf("%s: message %q", r.command, err.Error())
		}
	}
	if err := f.ensureGeneralCommissioningSuccess("armFailSafe", gencomm.CommissioningErrorOk, ""); err != nil {
		t.Errorf("OK: %v", err)
	}
}

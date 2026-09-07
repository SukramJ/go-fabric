// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package im

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/tlv"
)

// constraintFieldsError is a typed reader reject carrying ConstraintError.
type constraintFieldsError struct{}

func (constraintFieldsError) Error() string                { return "field out of range" }
func (constraintFieldsError) MatterStatusCode() StatusCode { return StatusConstraintError }

// invokeCountingDispatcher counts the commands that reach Invoke.
type invokeCountingDispatcher struct {
	fakeDispatcher
	invokes int
}

func (d *invokeCountingDispatcher) Invoke(ctx context.Context, p ConcreteCommandPath, f any) InvokeResult {
	d.invokes++
	return d.fakeDispatcher.Invoke(ctx, p, f)
}

// encodeTwoCommandInvoke builds an InvokeRequest with two commands whose
// fields structs each carry one uint field.
func encodeTwoCommandInvoke(t *testing.T) []byte {
	t.Helper()
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutBool(tlv.ContextTag(tagInvokeReqSuppressResponse), false)
	enc.PutBool(tlv.ContextTag(tagInvokeReqTimedRequest), false)
	enc.StartArray(tlv.ContextTag(tagInvokeReqInvokeRequests))
	for cmd := uint64(1); cmd <= 2; cmd++ {
		enc.StartStruct(tlv.AnonymousTag())
		enc.StartList(tlv.ContextTag(tagCmdDataPath))
		enc.PutUint(tlv.ContextTag(tagCmdPathEndpoint), 1)
		enc.PutUint(tlv.ContextTag(tagCmdPathCluster), 6)
		enc.PutUint(tlv.ContextTag(tagCmdPathCommand), cmd)
		_ = enc.EndContainer()
		enc.StartStruct(tlv.ContextTag(tagCmdDataFields))
		enc.PutUint(tlv.ContextTag(0), 0x10000)
		enc.PutUint(tlv.ContextTag(1), 7) // a second field the reject must drain past
		_ = enc.EndContainer()
		_ = enc.EndContainer()
	}
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	wire, err := enc.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	return wire
}

// TestInvokeRequest_TypedFieldsRejectBecomesPerCommandStatus pins that a
// [CommandFieldsReader] reject carrying a status marks only that command,
// leaves the batch decodable, and makes [HandleInvokeRequest] answer the
// command with the status without invoking it — matter.js
// CommandInvokeResponse.ts:446-448 (validate before invoke) and
// :472-496 (validation failure → CommandStatusIB).
func TestInvokeRequest_TypedFieldsRejectBecomesPerCommandStatus(t *testing.T) {
	t.Parallel()
	reader := func(path ConcreteCommandPath, dec *tlv.Decoder, _ tlv.Element) (any, error) {
		el, err := dec.Next() // first field only; the rest must be drained by the IM layer
		if err != nil {
			return nil, err
		}
		if path.Command == 1 && el.Uint > 0xFFFF {
			return nil, constraintFieldsError{}
		}
		if err := skipContainer(dec); err != nil {
			return nil, err
		}
		return "decoded", nil
	}
	req, err := UnmarshalInvokeRequestTLV(tlv.NewDecoder(encodeTwoCommandInvoke(t)), reader)
	if err != nil {
		t.Fatalf("UnmarshalInvokeRequestTLV: %v", err)
	}
	if len(req.Invokes) != 2 {
		t.Fatalf("decoded %d invokes, want 2 (the batch must survive one rejected command)", len(req.Invokes))
	}
	if req.Invokes[0].DecodeStatus != StatusConstraintError || req.Invokes[0].Fields != nil {
		t.Errorf("invoke 0: DecodeStatus=%v Fields=%v, want ConstraintError and nil", req.Invokes[0].DecodeStatus, req.Invokes[0].Fields)
	}
	if req.Invokes[1].DecodeStatus != StatusSuccess || req.Invokes[1].Fields != "decoded" {
		t.Errorf("invoke 1: DecodeStatus=%v Fields=%v, want Success and decoded fields", req.Invokes[1].DecodeStatus, req.Invokes[1].Fields)
	}

	d := &invokeCountingDispatcher{}
	resp := HandleInvokeRequest(context.Background(), d, req)
	if d.invokes != 1 {
		t.Errorf("dispatcher.Invoke called %d times, want 1 (the rejected command must not execute)", d.invokes)
	}
	if len(resp.Responses) != 2 {
		t.Fatalf("%d responses, want 2", len(resp.Responses))
	}
	if !resp.Responses[0].IsStatus || resp.Responses[0].Status.Status != StatusConstraintError {
		t.Errorf("response 0 = %+v, want CommandStatusIB(ConstraintError)", resp.Responses[0])
	}
}

// TestInvokeRequest_UntypedFieldsErrorStillFailsTheRequest is the
// negative control: a reader error without a status is a malformed
// request, exactly as before.
func TestInvokeRequest_UntypedFieldsErrorStillFailsTheRequest(t *testing.T) {
	t.Parallel()
	reader := func(_ ConcreteCommandPath, _ *tlv.Decoder, _ tlv.Element) (any, error) {
		return nil, errors.New("garbled")
	}
	if _, err := UnmarshalInvokeRequestTLV(tlv.NewDecoder(encodeTwoCommandInvoke(t)), reader); !errors.Is(err, ErrInvalidInvokeRequest) {
		t.Errorf("err = %v, want ErrInvalidInvokeRequest", err)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im_test

import (
	"testing"

	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/tlv"
)

// absentFieldsRequest is an InvokeRequestMessage of one CommandDataIB with
// a path and no CommandFields.
func absentFieldsRequest(t *testing.T) []byte {
	t.Helper()
	enc := tlv.NewEncoder()
	im.MarshalInvokeRequestTLV(enc, im.InvokeRequest{Invokes: []im.CommandInvocation{{
		Path: im.ConcreteCommandPath{Endpoint: 1, HasEndpoint: true, Cluster: 0x0003, Command: 0x00},
	}}}, func(*tlv.Encoder, tlv.Tag, any) {})
	wire, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return wire
}

// TestAbsentCommandFieldsReadAsAnEmptyStructure (decision 334): a
// CommandDataIB without CommandFields reaches the fields reader as an empty
// structure, the way an explicit {} does, so absent and empty take one
// decode path: a reader's typed refusal is the command's DecodeStatus, its
// value the command's fields. Without a reader nothing is read. Mirrors
// matter.js's reading of an absent request as {}
// (packages/node/src/node/integration/ProtocolService.ts:624-625 at
// db91886c) and its answer to an explicit {} lacking a mandatory field,
// INVALID_COMMAND (packages/protocol/src/action/server/CommandInvokeResponse.ts:529-535).
func TestAbsentCommandFieldsReadAsAnEmptyStructure(t *testing.T) {
	t.Parallel()
	wire := absentFieldsRequest(t)

	calls := 0
	refuse := func(path im.ConcreteCommandPath, dec *tlv.Decoder, el tlv.Element) (any, error) {
		calls++
		if path.Cluster != 0x0003 || el.Type != tlv.TypeStructure || !el.IsContainer {
			t.Errorf("reader got path %+v, element %+v; want the command's path and a structure", path, el)
		}
		return nil, &im.FieldError{Status: im.StatusInvalidCommand, Reason: "missing"}
	}
	req, err := im.UnmarshalInvokeRequestTLV(tlv.NewDecoder(wire), refuse)
	if err != nil || len(req.Invokes) != 1 {
		t.Fatalf("unmarshal: %v, %d invokes", err, len(req.Invokes))
	}
	if calls != 1 || req.Invokes[0].DecodeStatus != im.StatusInvalidCommand || req.Invokes[0].Fields != nil {
		t.Errorf("reader calls %d, %+v; want one call and INVALID_COMMAND", calls, req.Invokes[0])
	}

	value := func(_ im.ConcreteCommandPath, dec *tlv.Decoder, _ tlv.Element) (any, error) {
		if next, err := dec.Next(); err != nil || !next.IsEndContainer {
			t.Errorf("the structure is not empty: %+v, %v", next, err)
		}
		return "decoded", nil
	}
	if req, err := im.UnmarshalInvokeRequestTLV(tlv.NewDecoder(wire), value); err != nil || req.Invokes[0].Fields != "decoded" || !req.Invokes[0].DecodeStatus.IsSuccess() {
		t.Errorf("a decoded value: %+v, %v", req.Invokes, err)
	}

	if req, err := im.UnmarshalInvokeRequestTLV(tlv.NewDecoder(wire), nil); err != nil || req.Invokes[0].Fields != nil {
		t.Errorf("no reader: %+v, %v", req.Invokes, err)
	}
}

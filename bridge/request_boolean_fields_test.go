// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"testing"

	mattercore "thingfabric.dev/go-fabric/cluster/core"
	bidef "thingfabric.dev/go-fabric/cluster/spec/basicinformation"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/store"
	"thingfabric.dev/go-fabric/tlv"
	"thingfabric.dev/go-fabric/transport/message"
)

// booleanFieldPayload encodes a minimal Read, Subscribe, Write or Invoke
// request whose boolean fields are booleans, except the one named bad,
// which carries an unsigned integer; bad "" is the well-formed request.
// Tags: TlvInvokeRequest.ts:19-25, TlvWriteRequest.ts:18-25,
// TlvReadRequest.ts:21-30, TlvSubscribeRequest.ts:21-39,
// TlvAttributePath.ts:21-23, TlvEventPath.ts:19, :22 at db021da0.
func booleanFieldPayload(t *testing.T, opcode uint8, bad string) []byte {
	t.Helper()
	enc := tlv.NewEncoder()
	b := func(tag uint8, name string) {
		if name == bad {
			enc.PutUint(tlv.ContextTag(tag), 1)
			return
		}
		enc.PutBool(tlv.ContextTag(tag), false)
	}
	end := func() {
		if err := enc.EndContainer(); err != nil {
			t.Fatal(err)
		}
	}
	paths := func(attrTag, eventTag uint8) {
		enc.StartArray(tlv.ContextTag(attrTag))
		enc.StartList(tlv.AnonymousTag())
		b(0, "EnableTagCompression")
		enc.PutUint(tlv.ContextTag(2), 0) // endpoint
		enc.PutUint(tlv.ContextTag(3), 0x0028)
		enc.PutUint(tlv.ContextTag(4), 0) // DataModelRevision
		end()
		end()
		enc.StartArray(tlv.ContextTag(eventTag))
		enc.StartList(tlv.AnonymousTag())
		enc.PutUint(tlv.ContextTag(1), 0) // endpoint
		b(4, "IsUrgent")
		end()
		end()
	}
	enc.StartStruct(tlv.AnonymousTag())
	switch opcode {
	case im.OpcodeInvokeRequest:
		b(0, "SuppressResponse")
		b(1, "TimedRequest")
		enc.StartArray(tlv.ContextTag(2))
		end()
	case im.OpcodeWriteRequest:
		b(0, "SuppressResponse")
		b(1, "TimedRequest")
		enc.StartArray(tlv.ContextTag(2))
		end()
		b(3, "MoreChunkedMessages")
	case im.OpcodeReadRequest:
		paths(0, 1)
		b(3, "FabricFiltered")
	case im.OpcodeSubscribeRequest:
		b(0, "KeepSubscriptions")
		enc.PutUint(tlv.ContextTag(1), 0)
		enc.PutUint(tlv.ContextTag(2), 60)
		paths(3, 4)
		b(7, "FabricFiltered")
	default:
		t.Fatalf("opcode 0x%02X", opcode)
	}
	end()
	data, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestRequestBooleanFieldOfAnotherTypeAnsweredFailure: through the receive
// path, a Read, Subscribe, Write or Invoke whose boolean field carries
// another TLV type is answered with a StatusResponse FAILURE and not
// processed — a TimedRequest of another type no longer runs the request as
// untimed. Mirrors matter.js at db021da0: TlvBoolean.ts:22-24 throws
// UnexpectedDataError, InteractionMessenger.handleRequest
// (packages/protocol/src/interaction/InteractionMessenger.ts:344) decodes
// each request inside its try (:360, :377, :389, :395) and answers an error that carries no status with
// Status.Failure (:425-460), as SuppressResponseTest.ts:395 asserts for a
// statusless error. The well-formed request is the control: it passes the
// decode and is answered with its own response or its handler's status.
func TestRequestBooleanFieldOfAnotherTypeAnsweredFailure(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name             string
		opcode, response uint8
		fields           []string
	}{
		{"invoke", im.OpcodeInvokeRequest, im.OpcodeInvokeResponse, []string{"SuppressResponse", "TimedRequest"}},
		{"write", im.OpcodeWriteRequest, im.OpcodeWriteResponse, []string{"SuppressResponse", "TimedRequest", "MoreChunkedMessages"}},
		{"read", im.OpcodeReadRequest, im.OpcodeReportData, []string{"FabricFiltered", "EnableTagCompression", "IsUrgent"}},
		{"subscribe", im.OpcodeSubscribeRequest, im.OpcodeReportData, []string{"KeepSubscriptions", "FabricFiltered", "EnableTagCompression", "IsUrgent"}},
	}
	for _, c := range cases {
		t.Run("control "+c.name, func(t *testing.T) {
			t.Parallel()
			b := newStartedBridge(t)
			reply := dispatchReply(t, b, c.opcode, booleanFieldPayload(t, c.opcode, ""), 1)
			if reply == nil {
				t.Fatal("no reply")
			}
			_, hdrLen, err := message.UnmarshalHeader(reply)
			if err != nil {
				t.Fatal(err)
			}
			proto, protoLen, err := message.UnmarshalProtocolHeader(reply[hdrLen:])
			if err != nil {
				t.Fatal(err)
			}
			if proto.Opcode == im.OpcodeStatusResponse {
				// The handler's own refusal (a Subscribe that matches
				// nothing here: InvalidAction) is past the decode.
				if got := decodeStatusResponseCode(t, reply[hdrLen+protoLen:]); got == im.StatusFailure {
					t.Fatal("control answered FAILURE")
				}
				return
			}
			if proto.Opcode != c.response {
				t.Fatalf("reply opcode 0x%02X, want 0x%02X", proto.Opcode, c.response)
			}
		})
		for _, field := range c.fields {
			t.Run(c.name+" "+field, func(t *testing.T) {
				t.Parallel()
				b := newStartedBridge(t)
				reply := dispatchReply(t, b, c.opcode, booleanFieldPayload(t, c.opcode, field), 1)
				if reply == nil {
					t.Fatal("no reply")
				}
				if got := decodeWriteChunkedStatusResponse(t, reply); got != im.StatusFailure {
					t.Fatalf("status %v, want FAILURE", got)
				}
			})
		}
	}
}

// TestRequestWithoutFlagsParityMatterJS: through the secure receive path, a
// unicast Write without TimedRequest is written and answered, and an Invoke
// without SuppressResponse and TimedRequest runs and is answered — both
// flags are TlvOptionalField and read as false (TlvWriteRequest.ts:29,
// TlvInvokeRequest.ts:23, :30 at db91886c). Mirrors
// InteractionProtocolTest.ts:1438-1450 ("writes values when the request
// leaves out TimedRequest") and :2176-2193 ("invokes a command whose
// request leaves out SuppressResponse and TimedRequest") at db91886c.
func TestRequestWithoutFlagsParityMatterJS(t *testing.T) {
	t.Parallel()
	t.Run("writes values when the request leaves out TimedRequest", func(t *testing.T) {
		t.Parallel()
		var bi *mattercore.BasicInformation
		h := newSecureHarness(t, nil, func(*store.Store, uint8) []contract.ClusterServer {
			var err error
			if bi, err = mattercore.NewBasicInformation(mattercore.Config{VendorID: 0xFFF1, ProductID: 0x8001, NodeLabel: "", VendorName: "v", ProductName: "p", SerialNumber: "s"}); err != nil {
				t.Fatal(err)
			}
			return []contract.ClusterServer{bi}
		})
		h.allowAll()
		enc := tlv.NewEncoder()
		enc.StartStruct(tlv.AnonymousTag())
		enc.StartArray(tlv.ContextTag(2))
		enc.StartStruct(tlv.AnonymousTag())
		im.ConcreteAttributePath{Endpoint: 0, Cluster: 0x0028, Attribute: 0x0005, HasEndpoint: true, HasCluster: true, HasAttribute: true}.MarshalTLV(enc, tlv.ContextTag(1))
		enc.PutUTF8(tlv.ContextTag(2), "test")
		_ = enc.EndContainer()
		_ = enc.EndContainer()
		enc.PutUint(tlv.ContextTag(0xFF), 12)
		_ = enc.EndContainer()
		body, err := enc.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		op, payload, ok := h.exchange(im.OpcodeWriteRequest, body)
		if !ok || op != im.OpcodeWriteResponse {
			t.Fatalf("reply opcode 0x%02X (ok=%v), want WriteResponse", op, ok)
		}
		statuses := decodeTLVTree(t, payload).mustChild(t, 0).Children
		if len(statuses) != 1 {
			t.Fatalf("%d AttributeStatusIBs, want 1", len(statuses))
		}
		if got := im.StatusCode(statuses[0].mustChild(t, 1).mustChild(t, 0).El.Uint); got != im.StatusSuccess {
			t.Fatalf("NodeLabel write status %v, want SUCCESS", got)
		}
		if v, _ := bi.MatterRead(bidef.AttrNodeLabel); v.(tlv.BoundedString).Value != "test" {
			t.Fatalf("NodeLabel = %v, want test", v)
		}
	})
	t.Run("invokes a command whose request leaves out SuppressResponse and TimedRequest", func(t *testing.T) {
		t.Parallel()
		h, srv, ep := newScriptedHarness(t)
		enc := tlv.NewEncoder()
		enc.StartStruct(tlv.AnonymousTag())
		enc.StartArray(tlv.ContextTag(2))
		enc.StartStruct(tlv.AnonymousTag())
		im.ConcreteCommandPath{Endpoint: ep, Cluster: 0x0006, Command: 0x01, HasEndpoint: true, HasCluster: true, HasCommand: true}.MarshalTLV(enc, tlv.ContextTag(0))
		enc.StartStruct(tlv.ContextTag(1))
		_ = enc.EndContainer()
		_ = enc.EndContainer()
		_ = enc.EndContainer()
		enc.PutUint(tlv.ContextTag(0xFF), 12)
		_ = enc.EndContainer()
		body, err := enc.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		op, _, ok := h.exchange(im.OpcodeInvokeRequest, body)
		if !ok || op != im.OpcodeInvokeResponse {
			t.Fatalf("reply opcode 0x%02X (ok=%v), want InvokeResponse", op, ok)
		}
		if !srv.isOn() {
			t.Fatal("On did not run")
		}
	})
}

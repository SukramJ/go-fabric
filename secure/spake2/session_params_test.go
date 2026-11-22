// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spake2_test

import (
	"bytes"
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/secure/sessionparams"
	"thingfabric.dev/go-fabric/secure/spake2"
	"thingfabric.dev/go-fabric/tlv"
)

// TestPBKDFParamResponseCarriesFullSessionParameters: PBKDFParamResponse
// tag 5 is the whole SessionParameters structure (matter.js
// PaseServer.ts:151 sends the node's session parameters), and a decoder
// sees the whole structure.
func TestPBKDFParamResponseCarriesFullSessionParameters(t *testing.T) {
	t.Parallel()
	sp := sessionparams.Parameters{
		SessionIdleInterval: 500, SessionActiveInterval: 300, SessionActiveThreshold: 4000,
		DataModelRevision: 21, InteractionModelRevision: 12, SpecificationVersion: 0x01060100,
		MaxPathsPerInvoke: 1, SupportedTransports: sessionparams.TransportTCPServer, MaxTCPMessageSize: 64000,
	}
	resp := spake2.PBKDFParamResponse{
		InitiatorRandom: make([]byte, spake2.PBKDFRandomSize), ResponderRandom: make([]byte, spake2.PBKDFRandomSize),
		ResponderSessionID: 7, ResponderSessionParams: &sp,
	}
	back, err := spake2.DecodePBKDFParamResponse(resp.Marshal())
	if err != nil {
		t.Fatal(err)
	}
	if back.ResponderSessionParams == nil || *back.ResponderSessionParams != sp {
		t.Fatalf("ResponderSessionParams = %+v, want %+v", back.ResponderSessionParams, sp)
	}
}

// TestEmptySessionParamsOmitTag5: an empty SessionParameters structure
// states nothing, so the response carries no tag 5 at all.
func TestEmptySessionParamsOmitTag5(t *testing.T) {
	t.Parallel()
	empty := spake2.PBKDFParamResponse{
		InitiatorRandom: make([]byte, spake2.PBKDFRandomSize), ResponderRandom: make([]byte, spake2.PBKDFRandomSize),
		ResponderSessionID: 7, ResponderSessionParams: &sessionparams.Parameters{},
	}
	none := empty
	none.ResponderSessionParams = nil
	if !bytes.Equal(empty.Marshal(), none.Marshal()) {
		t.Fatal("an empty structure must omit tag 5")
	}
}

// TestPBKDFParamRequestDecodesFullSessionParameters: a 1.5+ commissioner's
// PBKDFParamRequest tag 5 with every field is kept whole.
func TestPBKDFParamRequestDecodesFullSessionParameters(t *testing.T) {
	t.Parallel()
	payload := make([]byte, 0, 80)
	payload = append(payload, 0x15, 0x30, 0x01, byte(spake2.PBKDFRandomSize))
	payload = append(payload, make([]byte, spake2.PBKDFRandomSize)...)
	payload = append(payload,
		0x25, 0x02, 0x2A, 0x00, // [2] session id 42
		0x25, 0x03, 0x00, 0x00, // [3] passcode id 0
		0x28, 0x04, // [4] false
		0x35, 0x05, // [5] SessionParameters
		0x26, 0x01, 0xF4, 0x01, 0x00, 0x00, // idle 500
		0x26, 0x06, 0x00, 0x00, 0x05, 0x01, // spec 0x01050000
		0x25, 0x07, 0x05, 0x00, // maxPaths 5
		0x25, 0x08, 0x02, 0x00, // tcpClient
		0x26, 0x09, 0x00, 0x20, 0x00, 0x00, // 8192
		0x18, 0x18,
	)
	req, err := spake2.DecodePBKDFParamRequest(payload)
	if err != nil {
		t.Fatal(err)
	}
	p := req.InitiatorSessionParams
	if p == nil || p.SessionIdleInterval != 500 || p.SpecificationVersion != 0x01050000 || p.MaxPathsPerInvoke != 5 ||
		p.SupportedTransports != sessionparams.TransportTCPClient || p.MaxTCPMessageSize != 8192 {
		t.Fatalf("InitiatorSessionParams = %+v", p)
	}
}

// TestPBKDFParamRequestRefusesMalformedSessionParameters: the
// PBKDFParamRequest's session parameters (tag 5) are decoded strictly
// ([sessionparams.DecodeFields]); a wrong-typed field refuses the whole
// request with ErrWireMalformed, the same error as any other malformed
// member, and a well-formed structure is read.
func TestPBKDFParamRequestRefusesMalformedSessionParameters(t *testing.T) {
	t.Parallel()
	request := func(params func(e *tlv.Encoder)) []byte {
		e := tlv.NewEncoder()
		e.StartStruct(tlv.AnonymousTag())
		e.PutOctets(tlv.ContextTag(1), make([]byte, spake2.PBKDFRandomSize))
		e.PutUint16(tlv.ContextTag(2), 7)
		e.PutUint16(tlv.ContextTag(3), 0)
		e.PutBool(tlv.ContextTag(4), false)
		e.StartStruct(tlv.ContextTag(5))
		params(e)
		_ = e.EndContainer()
		_ = e.EndContainer()
		b, err := e.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	if _, err := spake2.DecodePBKDFParamRequest(request(func(e *tlv.Encoder) { e.PutInt32(tlv.ContextTag(1), 500) })); !errors.Is(err, spake2.ErrWireMalformed) {
		t.Fatalf("signed SessionIdleInterval: err = %v, want ErrWireMalformed", err)
	}
	if _, err := spake2.DecodePBKDFParamRequest(request(func(e *tlv.Encoder) { e.PutUint32(tlv.ContextTag(3), 0x10000) })); !errors.Is(err, spake2.ErrWireMalformed) {
		t.Fatalf("SessionActiveThreshold above 16 bits: err = %v, want ErrWireMalformed", err)
	}
	req, err := spake2.DecodePBKDFParamRequest(request(func(e *tlv.Encoder) { e.PutUint64(tlv.ContextTag(1), 500) }))
	if err != nil {
		t.Fatal(err)
	}
	if req.InitiatorSessionParams == nil || req.InitiatorSessionParams.SessionIdleInterval != 500 {
		t.Fatalf("InitiatorSessionParams = %+v, want SessionIdleInterval 500", req.InitiatorSessionParams)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spake2_test

import (
	"bytes"
	"testing"

	"github.com/SukramJ/go-fabric/secure/sessionparams"
	"github.com/SukramJ/go-fabric/secure/spake2"
)

// TestPBKDFParamResponseCarriesFullSessionParameters: PBKDFParamResponse
// tag 5 is the whole SessionParameters structure (matter.js
// PaseServer.ts:151 sends the node's session parameters), and a decoder
// sees both the structure and the legacy triplet view.
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
	if m := back.ResponderMRPParams; m == nil || *m.IdleRetransTimeoutMs != 500 || *m.ActiveRetransTimeoutMs != 300 || *m.ActiveThresholdTimeMs != 4000 {
		t.Fatalf("legacy triplet view = %+v", m)
	}
}

// TestLegacyTripletStillEncodesAsTags1To3: a caller of the deprecated
// field gets the same bytes as before — the triplet is tags 1-3 of the
// structure with the same widths (PaseMessages.ts:28-33).
func TestLegacyTripletStillEncodesAsTags1To3(t *testing.T) {
	t.Parallel()
	idle, active, thresh := uint32(800), uint32(200), uint16(3000)
	legacy := spake2.PBKDFParamResponse{
		InitiatorRandom: make([]byte, spake2.PBKDFRandomSize), ResponderRandom: make([]byte, spake2.PBKDFRandomSize),
		ResponderSessionID: 7,
		ResponderMRPParams: &spake2.MRPParameters{IdleRetransTimeoutMs: &idle, ActiveRetransTimeoutMs: &active, ActiveThresholdTimeMs: &thresh},
	}
	full := legacy
	full.ResponderMRPParams = nil
	full.ResponderSessionParams = &sessionparams.Parameters{SessionIdleInterval: 800, SessionActiveInterval: 200, SessionActiveThreshold: 3000}
	if a, b := legacy.Marshal(), full.Marshal(); !bytes.Equal(a, b) {
		t.Fatalf("legacy % X\nfull   % X", a, b)
	}
	empty := legacy
	empty.ResponderMRPParams = &spake2.MRPParameters{}
	none := legacy
	none.ResponderMRPParams = nil
	if !bytes.Equal(empty.Marshal(), none.Marshal()) {
		t.Fatal("an empty triplet must omit tag 5")
	}
}

// TestPBKDFParamRequestDecodesFullSessionParameters: a 1.5+ commissioner's
// PBKDFParamRequest tag 5 with every field is kept whole and mirrored into
// the legacy triplet.
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
	if req.InitiatorMRPParams == nil || *req.InitiatorMRPParams.IdleRetransTimeoutMs != 500 || req.InitiatorMRPParams.ActiveRetransTimeoutMs != nil {
		t.Fatalf("legacy view = %+v", req.InitiatorMRPParams)
	}
}

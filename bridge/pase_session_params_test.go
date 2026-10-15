// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"testing"

	"github.com/SukramJ/go-fabric/secure/sessionparams"
	"github.com/SukramJ/go-fabric/secure/spake2"
)

// TestPaseAdapterAdvertisesDefaultSessionParameters: with nothing
// configured, PBKDFParamResponse tag 5 carries the same parameters Sigma2
// advertises (matter.js PaseServer.ts:151 and CaseServer.ts:272 both send
// `sessions.sessionParameters`), and a host's structure replaces them.
func TestPaseAdapterAdvertisesDefaultSessionParameters(t *testing.T) {
	t.Parallel()
	a := NewPaseAdapter(nil)
	a.SetPBKDFParams(1000, []byte("SPAKE2P Key Salt"), 1)
	a.randomSource = func() [spake2.PBKDFRandomSize]byte { return [spake2.PBKDFRandomSize]byte{0x11} }
	initRand := make([]byte, spake2.PBKDFRandomSize)
	respond := func() spake2.PBKDFParamResponse {
		t.Helper()
		_, respBytes, err := a.ProcessPBKDFParamRequest(buildTestPBKDFParamRequest(t, initRand))
		if err != nil {
			t.Fatalf("ProcessPBKDFParamRequest: %v", err)
		}
		resp, err := spake2.DecodePBKDFParamResponse(respBytes)
		if err != nil {
			t.Fatalf("DecodePBKDFParamResponse: %v", err)
		}
		return resp
	}
	if got, want := respond().ResponderSessionParams, DefaultSessionParameters(); got == nil || *got != want {
		t.Fatalf("default tag 5 = %+v, want %+v", got, want)
	}
	custom := SessionParametersFor(sessionparams.TransportTCPServer, 0)
	a.SetResponderSessionParams(&custom)
	if got := respond().ResponderSessionParams; got == nil || *got != custom || got.MaxTCPMessageSize != sessionparams.DefaultMaxTCPMessageSize {
		t.Fatalf("configured tag 5 = %+v, want %+v", got, custom)
	}
	a.SetResponderSessionParams(&sessionparams.Parameters{})
	if got := respond().ResponderSessionParams; got != nil {
		t.Fatalf("empty structure must omit tag 5, got %+v", got)
	}
}

// TestSessionParametersForStatesTCPOnlyWhenOffered: the node's defaults
// name no transport; with a TCP role the bitmap and the size bound are
// stated (SessionParameters.ts:58-63), the host's bound winning.
func TestSessionParametersForStatesTCPOnlyWhenOffered(t *testing.T) {
	t.Parallel()
	if d := DefaultSessionParameters(); d.SupportedTransports != 0 || d.MaxTCPMessageSize != 0 {
		t.Fatalf("defaults state transports: %+v", d)
	}
	if p := SessionParametersFor(0, 9000); p.MaxTCPMessageSize != 0 {
		t.Fatalf("size stated without TCP: %+v", p)
	}
	p := SessionParametersFor(sessionparams.TransportTCPClient|sessionparams.TransportTCPServer, 9000)
	if p.SupportedTransports != sessionparams.TransportTCPClient|sessionparams.TransportTCPServer || p.MaxTCPMessageSize != 9000 {
		t.Fatalf("%+v", p)
	}
	if p.MaxPathsPerInvoke != DefaultSessionParameters().MaxPathsPerInvoke {
		t.Fatal("the other fields must stay the defaults")
	}
}

// TestPaseAdapterLegacyTripletSettersStillWork: the deprecated setter
// and getter keep working through the structure until their removal.
func TestPaseAdapterLegacyTripletSettersStillWork(t *testing.T) {
	t.Parallel()
	a := NewPaseAdapter(nil)
	idle := uint32(777)
	a.SetResponderMRPParams(&spake2.MRPParameters{IdleRetransTimeoutMs: &idle}) //nolint:staticcheck // the deprecated triplet API stays under test until its removal
	a.mu.Lock()
	got := a.responderSessionParams
	a.mu.Unlock()
	if got == nil || got.SessionIdleInterval != 777 || got.MaxPathsPerInvoke != DefaultSessionParameters().MaxPathsPerInvoke {
		t.Fatalf("legacy setter produced %+v", got)
	}
	a.SetPBKDFParams(1000, []byte("SPAKE2P Key Salt"), 1)
	if _, _, err := a.ProcessPBKDFParamRequest(buildTestPBKDFParamRequestWithMRP(t, make([]byte, spake2.PBKDFRandomSize))); err != nil {
		t.Fatal(err)
	}
	if m := a.PeerMRPParams(); m == nil || *m.IdleRetransTimeoutMs != 800 {
		t.Fatalf("legacy getter = %+v", m)
	}
	if p := a.PeerSessionParams(); p == nil || p.SessionActiveThreshold != 3000 {
		t.Fatalf("PeerSessionParams = %+v", p)
	}
}

// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

// White-box tests for two CaseAdapter contracts: the Secure-Channel
// status code a Sigma1 addressing an unknown fabric earns, and the
// `established` gate that decides when the session-established callback
// fires. Lives in package bridge to reach the adapter and the
// Secure-Channel router directly.

import (
	"bytes"
	"encoding/binary"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/secure/sigma"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// statusReportCodes splits a Secure-Channel StatusReport body into its
// general and protocol codes ([mrp.EncodeStatusReport] layout).
func statusReportCodes(t *testing.T, body []byte) (general, protocol uint16) {
	t.Helper()
	if len(body) < 8 {
		t.Fatalf("StatusReport body too short: %d bytes", len(body))
	}
	return binary.LittleEndian.Uint16(body[0:2]), binary.LittleEndian.Uint16(body[6:8])
}

// missingFabricResolver reports every Sigma1 destination as unknown.
type missingFabricResolver struct{}

func (missingFabricResolver) ResolveSigma1Destination([32]byte, [sigma.RandomSize]byte) (*sigma.Identity, sigma.PeerVerifier, bool) {
	return nil, nil, false
}

// TestCaseAdapter_UnknownFabricSigma1_AnswersNoSharedTrustRoots pins the
// status code for a Sigma1 whose DestinationID addresses no fabric this
// node holds: StatusReport(NoSharedTrustRoots), and no Sigma2. matter.js
// CaseServer.ts:88-90 sends NoSharedTrustRoots for FabricNotFoundError
// and InvalidParam for everything else.
func TestCaseAdapter_UnknownFabricSigma1_AnswersNoSharedTrustRoots(t *testing.T) {
	t.Parallel()
	a, initiator, responder := pairedCaseAdapter(t)
	responder.SetIdentityResolver(missingFabricResolver{})
	fired := false
	a.SetOnSessionEstablished(func(sigma.SessionKeys, uint16) error { fired = true; return nil })

	sigma1Bytes, err := initiator.GenerateSigma1()
	if err != nil {
		t.Fatalf("GenerateSigma1: %v", err)
	}
	op, payload, err := a.ProcessSigma1(sigma1Bytes)
	if err != nil {
		t.Fatalf("ProcessSigma1: %v", err)
	}
	if op != mrp.SCOpcodeStatusReport {
		t.Fatalf("opcode = %#x, want StatusReport %#x (a Sigma2 must not be produced for an unknown fabric)", op, mrp.SCOpcodeStatusReport)
	}
	general, protocol := statusReportCodes(t, payload)
	if general != mrp.SCStatusGeneralFailure {
		t.Errorf("general code = %#x, want FAILURE %#x", general, mrp.SCStatusGeneralFailure)
	}
	if protocol != mrp.SCStatusProtocolNoSharedTrustRoots {
		t.Errorf("protocol code = %#x, want NoSharedTrustRoots %#x", protocol, mrp.SCStatusProtocolNoSharedTrustRoots)
	}
	if fired {
		t.Error("onEstablished fired for a rejected Sigma1")
	}
}

// erroringCaseHandler returns a fixed error from ProcessSigma1.
type erroringCaseHandler struct{ err error }

func (h erroringCaseHandler) ProcessSigma1([]byte) (opcode uint8, respPayload []byte, err error) {
	return 0, nil, h.err
}

func (h erroringCaseHandler) ProcessSigma3([]byte) (opcode uint8, respPayload []byte, err error) {
	return 0, nil, h.err
}

func (h erroringCaseHandler) ProcessSigma2Resume([]byte) (opcode uint8, respPayload []byte, err error) {
	return 0, nil, h.err
}

// TestHandleCase_ErrorMapsToSecureChannelStatusCode pins the router's
// mapping for a CaseHandler that surfaces an error: ErrNoSharedTrustRoots
// → NoSharedTrustRoots, any other failure → InvalidParameter (matter.js
// CaseServer.ts:88-95).
func TestHandleCase_ErrorMapsToSecureChannelStatusCode(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want uint16
	}{
		{"no shared trust roots", sigma.ErrNoSharedTrustRoots, mrp.SCStatusProtocolNoSharedTrustRoots},
		{"other failure", errors.New("sigma: signature verify failed"), mrp.SCStatusProtocolInvalidParameter},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			peer, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
			if err != nil {
				t.Fatalf("ListenUDP: %v", err)
			}
			t.Cleanup(func() { _ = peer.Close() })
			src, ok := peer.LocalAddr().(*net.UDPAddr)
			if !ok {
				t.Fatalf("peer LocalAddr type = %T", peer.LocalAddr())
			}
			b := newStartedBridge(t)
			b.AttachCaseHandler(erroringCaseHandler{err: tc.err})

			_ = b.dispatchSecureChannel(src, scHdr(), scProto(mrp.SCOpcodeSigma1, 9, false, 0), []byte{0x15, 0x18})

			_ = peer.SetReadDeadline(time.Now().Add(2 * time.Second))
			buf := make([]byte, 1500)
			n, _, err := peer.ReadFromUDP(buf)
			if err != nil {
				t.Fatalf("reading StatusReport from bridge: %v", err)
			}
			general, protocol := decodeStatusReportCodes(t, buf[:n])
			if general != mrp.SCStatusGeneralFailure {
				t.Errorf("general code = %#x, want FAILURE", general)
			}
			if protocol != tc.want {
				t.Errorf("protocol code = %#x, want %#x", protocol, tc.want)
			}
		})
	}
}

// TestCaseAdapter_Sigma1ReplayDoesNotReopenTheEstablishedGate pins that
// the idempotent Sigma1 replay (the responder returns its cached Sigma2,
// secure/sigma/protocol.go processSigma1Locked) leaves the established
// gate closed: a Sigma3 retransmit that follows must not fire
// onEstablished a second time, which would re-register the session and
// displace the live one.
func TestCaseAdapter_Sigma1ReplayDoesNotReopenTheEstablishedGate(t *testing.T) {
	t.Parallel()
	a, initiator, responder := pairedCaseAdapter(t)
	calls := 0
	a.SetOnSessionEstablished(func(sigma.SessionKeys, uint16) error { calls++; return nil })

	sigma1Bytes, err := initiator.GenerateSigma1()
	if err != nil {
		t.Fatalf("GenerateSigma1: %v", err)
	}
	op1, sigma2First, err := a.ProcessSigma1(sigma1Bytes)
	if err != nil || op1 != mrp.SCOpcodeSigma2 {
		t.Fatalf("ProcessSigma1: op=%#x err=%v", op1, err)
	}
	sigma2Struct, err := responder.ProcessSigma1(sigma1Bytes) // replay: cached Sigma2
	if err != nil {
		t.Fatalf("responder.ProcessSigma1 replay: %v", err)
	}
	sigma3Bytes, err := initiator.ProcessSigma2(sigma2Struct)
	if err != nil {
		t.Fatalf("initiator.ProcessSigma2: %v", err)
	}
	if _, _, err := a.ProcessSigma3(sigma3Bytes); err != nil {
		t.Fatalf("ProcessSigma3: %v", err)
	}
	if calls != 1 {
		t.Fatalf("calls after the handshake = %d, want 1", calls)
	}

	// MRP retransmit of Sigma1 (our Sigma2 ack was in flight), then of Sigma3.
	op2, sigma2Again, err := a.ProcessSigma1(sigma1Bytes)
	if err != nil || op2 != mrp.SCOpcodeSigma2 {
		t.Fatalf("ProcessSigma1 replay: op=%#x err=%v", op2, err)
	}
	if !bytes.Equal(sigma2First, sigma2Again) {
		t.Fatal("the Sigma1 replay produced a different Sigma2; the responder's idempotent replay is the premise of this test")
	}
	if _, _, err := a.ProcessSigma3(sigma3Bytes); err != nil {
		t.Fatalf("ProcessSigma3 retransmit: %v", err)
	}
	if calls != 1 {
		t.Errorf("onEstablished calls = %d after a Sigma1 + Sigma3 retransmit, want 1 (a replayed Sigma1 restarts no handshake)", calls)
	}
}

// TestCaseAdapter_SecondResumeWithFreshSessionIDFiresOnEstablished pins
// that every successful resume whose responder session id differs from
// the last established one installs its keys: with a session-id renewer
// wired the second resume on a live adapter announces a fresh id, and
// the peer would otherwise be handed a slot whose keys are the old ones.
func TestCaseAdapter_SecondResumeWithFreshSessionIDFiresOnEstablished(t *testing.T) {
	t.Parallel()
	ipk := newCaseTestIPK(t)
	respID := newCaseTestIdentity(t, 0xBBBB, 1, ipk)
	responder := sigma.NewResponder(respID, caseTestVerifier{}, 0x2001)
	responder.SetSessionIDRenewer(func(previous uint16) (uint16, bool) { return previous + 1, true })

	sigma1Bytes, secret, rid := buildResumeSigma1(t)
	responder.SetResumptionStore(&fakeResumptionStore{id: rid, secret: secret})

	a := NewCaseAdapter(responder)
	var seenIDs []uint16
	a.SetOnSessionEstablished(func(sigma.SessionKeys, uint16) error {
		seenIDs = append(seenIDs, responder.SessionID())
		return nil
	})

	for round := 1; round <= 2; round++ {
		op, _, err := a.ProcessSigma1(sigma1Bytes)
		if err != nil {
			t.Fatalf("ProcessSigma1 resume #%d: %v", round, err)
		}
		if op != mrp.SCOpcodeSigma2Resume {
			t.Fatalf("resume #%d: opcode=%#x, want Sigma2Resume", round, op)
		}
	}
	if len(seenIDs) != 2 {
		t.Fatalf("onEstablished fired %d time(s) for two resumes with fresh session ids, want 2 (ids seen: %v)", len(seenIDs), seenIDs)
	}
	if seenIDs[0] == seenIDs[1] {
		t.Errorf("both resumes established under session id %#x; the second must carry the renewed id", seenIDs[0])
	}
}

// TestCaseAdapter_SecondResumeWithSameSessionIDDoesNotRefire is the
// negative control for the gate: without a renewer the second resume
// re-announces the id already established, and the callback stays quiet
// so the live session is not re-registered.
func TestCaseAdapter_SecondResumeWithSameSessionIDDoesNotRefire(t *testing.T) {
	t.Parallel()
	ipk := newCaseTestIPK(t)
	respID := newCaseTestIdentity(t, 0xBBBB, 1, ipk)
	responder := sigma.NewResponder(respID, caseTestVerifier{}, 0x2001)

	sigma1Bytes, secret, rid := buildResumeSigma1(t)
	responder.SetResumptionStore(&fakeResumptionStore{id: rid, secret: secret})

	a := NewCaseAdapter(responder)
	calls := 0
	a.SetOnSessionEstablished(func(sigma.SessionKeys, uint16) error { calls++; return nil })
	for round := 1; round <= 2; round++ {
		if _, _, err := a.ProcessSigma1(sigma1Bytes); err != nil {
			t.Fatalf("ProcessSigma1 resume #%d: %v", round, err)
		}
	}
	if calls != 1 {
		t.Errorf("onEstablished fired %d time(s) for two resumes on the same session id, want 1", calls)
	}
}

// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package exchange

// Parity of the exchange layer's constants and formulas with matter.js at
// 0ceb202d (packages/protocol/src). The expected values are computed by
// hand from the cited TypeScript, not by the Go code under test.

import (
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// TestParityMRPConstants: protocol/MRP.ts:33-61 and
// ExchangeManager.ts:54, :147, SessionManager.ts:183, UnsecuredSession.ts:18.
func TestParityMRPConstants(t *testing.T) {
	t.Parallel()
	checks := []struct {
		name      string
		got, want any
	}{
		{"MAX_TRANSMISSIONS (MRP.ts:33)", MaxTransmissions, 5},
		{"BACKOFF_BASE (MRP.ts:36)", mrp.MRPBackoffThreshold, 1.6},
		{"BACKOFF_JITTER (MRP.ts:39)", mrp.MRPBackoffJitterFactor, 0.25},
		{"BACKOFF_MARGIN (MRP.ts:42)", mrp.MRPBackoffMargin, 1.1},
		{"STANDALONE_ACK_TIMEOUT (MRP.ts:48)", StandaloneAckTimeout, 200 * time.Millisecond},
		{"DEFAULT_EXPECTED_PROCESSING_TIME (MRP.ts:55)", DefaultExpectedProcessingTime, 2 * time.Second},
		{"PEER_RESPONSE_TIME_BUFFER (MRP.ts:61)", peerResponseTimeBuffer, 5 * time.Second},
		{"MAXIMUM_CONCURRENT_OUTGOING_EXCHANGES_PER_SESSION (ExchangeManager.ts:54)", MaxConcurrentExchangesPerSession, 30},
		{"exchange index flag (ExchangeManager.ts:147)", initiatedExchangeFlag, uint32(0x10000)},
		{"ID_SPACE_UPPER_BOUND (SessionManager.ts:183)", sessionIDUpperBound, uint16(0xFFFF)},
		{"UNICAST_UNSECURE_SESSION_ID (UnsecuredSession.ts:18)", UnsecuredSessionID, uint16(0)},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}

// TestParityRetransmissionInterval: MRP.ts:162-190 —
// floor(base · 1.1 · 1.6^max(0, n−1) · (1 + r · 0.25)), base the active
// interval for an active peer and the idle interval otherwise.
func TestParityRetransmissionInterval(t *testing.T) {
	t.Parallel()
	defaults := IntervalsOf(sessionparams.Parameters{})
	for _, c := range []struct {
		n      int
		active bool
		r      float64
		want   time.Duration
	}{
		{0, true, 0, 330},      // 300·1.1
		{1, true, 0, 330},      // threshold 1: no growth on the first retry
		{2, true, 0, 528},      // 330·1.6
		{3, true, 1, 1056},     // 330·2.56·1.25
		{4, true, 1, 1689},     // 330·4.096·1.25 = 1689.6
		{0, false, 1, 687},     // 500·1.1·1.25 = 687.5
		{4, false, 0.5, 2534},  // 550·4.096·1.125 = 2534.4
		{5, false, 1, 4505},    // 550·6.5536·1.25 = 4505.6
		{1, false, 0.999, 687}, // 550·1.24975 = 687.36
	} {
		if got := RetransmissionInterval(c.n, defaults, c.active, c.r); got != c.want*time.Millisecond {
			t.Errorf("RetransmissionInterval(%d, active=%v, r=%v) = %v, want %vms", c.n, c.active, c.r, got, c.want)
		}
	}
}

// TestParityMaxPeerResponseTime: MRP.ts:79-120 and :198-224 for UDP —
// our side assumed active, an active peer turning idle past its active
// threshold, the expected processing time and the 5 s buffer.
func TestParityMaxPeerResponseTime(t *testing.T) {
	t.Parallel()
	local := IntervalsOf(DefaultLocalParameters())
	// Local, active throughout: 412 + 412 + 660 + 1056 + 1689 = 4229.
	if got := MaxPeerResponseTime(nil, false, local, DefaultExpectedProcessingTime); got != 11229*time.Millisecond {
		t.Errorf("without peer = %v, want 11229ms", got)
	}
	// An idle peer with the defaults: 687 + 687 + 1100 + 1760 + 2816 = 7050.
	if got := MaxPeerResponseTime(&local, false, local, DefaultExpectedProcessingTime); got != 18279*time.Millisecond {
		t.Errorf("with idle peer = %v, want 18279ms", got)
	}
	// A peer with a 500 ms active threshold: 412, 412 (412 ≤ 500), then
	// idle from the third transmission (824 > 500): 1100 + 1760 + 2816.
	short := Intervals{Idle: 500 * time.Millisecond, Active: 300 * time.Millisecond, ActiveThreshold: 500 * time.Millisecond}
	if got := MaxPeerResponseTime(&short, true, local, 0); got != (6500+4229+5000)*time.Millisecond {
		t.Errorf("active peer turning idle = %v, want 15729ms", got)
	}
}

// TestParitySessionParameterFallbacks: SessionIntervals.ts:45-49,
// SessionParameters.ts:42-81, :118-164 and Specification.ts:110-124.
func TestParitySessionParameterFallbacks(t *testing.T) {
	t.Parallel()
	got := PeerParameters(nil)
	want := sessionparams.Parameters{
		SessionIdleInterval: 500, SessionActiveInterval: 300, SessionActiveThreshold: 4000,
		DataModelRevision: 17, InteractionModelRevision: 11, SpecificationVersion: 0, MaxPathsPerInvoke: 1,
	}
	if got != want {
		t.Errorf("PeerParameters(nil) = %+v, want %+v", got, want)
	}
	// TCP is cleared below Matter 1.5; MaxTCPMessageSize only with TCP
	// and 64000 when not stated.
	old := PeerParameters(&sessionparams.Parameters{SpecificationVersion: 0x01040000, SupportedTransports: sessionparams.TransportTCPServer, MaxTCPMessageSize: 9000})
	if old.SupportedTransports != 0 || old.MaxTCPMessageSize != 0 {
		t.Errorf("1.4 peer keeps TCP: %+v", old)
	}
	tcp := PeerParameters(&sessionparams.Parameters{SpecificationVersion: 0x01050000, SupportedTransports: sessionparams.TransportTCPServer})
	if tcp.SupportedTransports != sessionparams.TransportTCPServer || tcp.MaxTCPMessageSize != 64000 {
		t.Errorf("1.5 peer with TCP: %+v", tcp)
	}
	local := DefaultLocalParameters()
	if local.DataModelRevision != 21 || local.SpecificationVersion != 0x01060100 ||
		local.InteractionModelRevision != uint16(im.MatterInteractionModelRevision) || im.MatterInteractionModelRevision != 12 || local.MaxPathsPerInvoke != 1 ||
		local.SessionIdleInterval != 500 || local.SessionActiveInterval != 300 || local.SessionActiveThreshold != 4000 {
		t.Errorf("DefaultLocalParameters = %+v", local)
	}
	// The object spread of PaseClient.ts:168-171 / CaseClient.ts:184-188.
	over := Overlay(want, &sessionparams.Parameters{
		SessionIdleInterval: 1, SessionActiveInterval: 2, SessionActiveThreshold: 3, DataModelRevision: 4,
		InteractionModelRevision: 5, SpecificationVersion: 6, MaxPathsPerInvoke: 7, SupportedTransports: 8, MaxTCPMessageSize: 9,
	})
	if over != (sessionparams.Parameters{
		SessionIdleInterval: 1, SessionActiveInterval: 2, SessionActiveThreshold: 3, DataModelRevision: 4,
		InteractionModelRevision: 5, SpecificationVersion: 6, MaxPathsPerInvoke: 7, SupportedTransports: 8, MaxTCPMessageSize: 9,
	}) {
		t.Errorf("Overlay = %+v", over)
	}
	if Overlay(want, nil) != want || Overlay(want, &sessionparams.Parameters{}) != want {
		t.Error("Overlay with nothing stated changed the base")
	}
}

// TestParityStatusReport: ProtocolStatusMessage.ts:39-66 and
// SecureChannelMessenger.ts:234-259.
func TestParityStatusReport(t *testing.T) {
	t.Parallel()
	body := StatusReport{GeneralCode: 1, ProtocolID: 0, ProtocolCode: 2, ProtocolData: []byte{0xAA}}.Encode()
	if string(body) != "\x01\x00\x00\x00\x00\x00\x02\x00\xAA" {
		t.Fatalf("Encode = %x", body)
	}
	r, ok := DecodeStatusReport(body)
	if !ok || r.GeneralCode != 1 || r.ProtocolCode != 2 || string(r.ProtocolData) != "\xAA" {
		t.Fatalf("Decode = %+v", r)
	}
	if _, ok := DecodeStatusReport(body[:7]); ok {
		t.Fatal("a 7-byte body decoded")
	}
	if err := CheckStatusReport(StatusReport{}.Encode()); err != nil {
		t.Fatalf("Success: %v", err)
	}
	err := CheckStatusReport(body)
	var se *StatusReportError
	if !errors.As(err, &se) || se.Report.ProtocolCode != 2 || !errors.Is(err, ErrTransient) || se.Error() == "" {
		t.Fatalf("failure = %v", err)
	}
	// General success with a protocol code other than Success fails too.
	if CheckStatusReport(StatusReport{ProtocolCode: 3}.Encode()) == nil {
		t.Fatal("CloseSession accepted as success")
	}
	if CheckStatusReport(nil) == nil {
		t.Fatal("empty body accepted")
	}
	pu := &PeerUnresponsiveError{Waited: time.Second}
	pm := &PeerMessageMissingError{Timeout: time.Second}
	// RetransmissionLimitReachedError (errors.ts:17) is a
	// NoResponseTimeoutError, not a transient peer error.
	if errors.Is(ErrRetransmissionLimit, ErrTransient) {
		t.Fatal("ErrRetransmissionLimit is transient")
	}
	if pu.Error() == "" || pm.Error() == "" || pm.Is(ErrFlow) {
		t.Fatal("error strings")
	}
}

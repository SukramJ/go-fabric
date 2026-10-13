// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package handshake

// Parity of the CASE responder's rules with matter.js at 0ceb202d
// (packages/protocol/src/session/case/CaseServer.ts and the status codes of
// packages/types/src/protocol/definitions/). The expected values are read
// off the cited TypeScript, not computed by the code under test.

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/secure/sigma"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// TestParityCASEServerStatusCodes: the codes the responder answers with —
// GeneralStatusCode.Failure (general.ts:12) with
// SecureChannelStatusCode.NoSharedTrustRoots / InvalidParam
// (secure-channel.ts:70, :73), Success / Success for the session
// (general.ts:9, secure-channel.ts:67) — and the messenger's crypto
// processing time (SecureChannelMessenger.ts:55).
func TestParityCASEServerStatusCodes(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name      string
		got, want any
	}{
		{"GeneralStatusCode.Success", mrp.SCStatusGeneralSuccess, uint16(0)},
		{"GeneralStatusCode.Failure", mrp.SCStatusGeneralFailure, uint16(1)},
		{"SecureChannelStatusCode.Success", mrp.SCStatusProtocolSessionEstablishmentSuccess, uint16(0)},
		{"SecureChannelStatusCode.NoSharedTrustRoots", mrp.SCStatusProtocolNoSharedTrustRoots, uint16(1)},
		{"SecureChannelStatusCode.InvalidParam", statusInvalidParam, uint16(2)},
		{"EXPECTED_CRYPTO_PROCESSING_TIME", ExpectedCryptoProcessingTime, 35 * time.Second},
	} {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}

// TestParityCASEServerErrorReport: which failure gets which StatusReport —
// CaseServer.ts:88-94: FabricNotFoundError → NoSharedTrustRoots; a
// ChannelStatusResponseError (the peer's StatusReport) or a
// PeerUnresponsiveError (PeerMessageMissingError extends it) → none;
// anything else (UnexpectedDataError of a malformed Sigma1, a fabric id
// mismatch, a bad signature) → InvalidParam.
func TestParityCASEServerErrorReport(t *testing.T) {
	t.Parallel()
	report := func(err error) string {
		switch {
		case errors.Is(err, sigma.ErrNoSharedTrustRoots):
			return "NoSharedTrustRoots"
		case causedByPeerStatusOrUnresponsive(err):
			return "none"
		default:
			return "InvalidParam"
		}
	}
	for _, c := range []struct {
		name string
		err  error
		want string
	}{
		{"FabricNotFoundError", fmt.Errorf("wrapped: %w", sigma.ErrNoSharedTrustRoots), "NoSharedTrustRoots"},
		{"ChannelStatusResponseError", &exchange.StatusReportError{}, "none"},
		{"PeerUnresponsiveError", &exchange.PeerUnresponsiveError{}, "none"},
		{"PeerMessageMissingError", fmt.Errorf("wait: %w", &exchange.PeerMessageMissingError{}), "none"},
		{"UnexpectedDataError (malformed Sigma1)", ErrUnexpectedData, "InvalidParam"},
		{"fabric id mismatch (CaseServer.ts:305-307)", sigma.ErrFabricIDMismatch, "InvalidParam"},
		{"signature (CaseServer.ts:309)", sigma.ErrSignatureInvalid, "InvalidParam"},
	} {
		if got := report(c.err); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
}

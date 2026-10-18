// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package handshake_test

// A port of matter.js packages/protocol/test/session/secure/PasePairingTest.ts
// at 862ad75f: the PaseClient's onPbkdfParamResponse callback (describe
// "PBKDF iterations from the responder"), against a scripted responder.

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/controller/handshake"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// pasePairingCallbacks pairs against script and counts onPbkdfParamResponse.
func pasePairingCallbacks(t *testing.T, script func(opcode uint8, payload []byte) (reply, bool)) (int32, error) {
	t.Helper()
	m, _ := newScripted(t, script)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var calls atomic.Int32
	_, err := handshake.PASE(ctx, m, scriptAddr, testPasscode, handshake.PASEOptions{
		OnPBKDFParamResponse: func() { calls.Add(1) },
	})
	return calls.Load(), err
}

func TestPasePairingPBKDFIterationsFromTheResponder(t *testing.T) {
	t.Parallel()
	t.Run("calls onPbkdfParamResponse once the responder answers the PBKDFParamRequest", func(t *testing.T) {
		t.Parallel()
		// Iterations 1000; the test stops after Pake1 (a Pake2 that does
		// not decode), as matter.js's does (PasePairingTest.ts:141-143).
		calls, err := pasePairingCallbacks(t, func(opcode uint8, _ []byte) (reply, bool) {
			switch opcode {
			case mrp.SCOpcodePBKDFParamRequest:
				return reply{mrp.SCOpcodePBKDFParamResponse, pbkdfResponse(true)}, true
			case mrp.SCOpcodePake1:
				return reply{mrp.SCOpcodePake2, []byte{0x15, 0x18}}, true
			}
			return reply{}, false
		})
		if !errors.Is(err, handshake.ErrUnexpectedData) || calls != 1 {
			t.Fatalf("PASE = %v with %d callbacks, want the stop after Pake1 and one", err, calls)
		}
	})
	t.Run("does not call onPbkdfParamResponse when the responder does not answer", func(t *testing.T) {
		t.Parallel()
		// No PBKDFParamResponse: the responder fails the exchange instead.
		failure := exchange.StatusReport{GeneralCode: mrp.SCStatusGeneralFailure, ProtocolCode: mrp.SCStatusProtocolInvalidParameter}.Encode()
		calls, err := pasePairingCallbacks(t, func(opcode uint8, _ []byte) (reply, bool) {
			if opcode == mrp.SCOpcodePBKDFParamRequest {
				return reply{mrp.SCOpcodeStatusReport, failure}, true
			}
			return reply{}, false
		})
		if err == nil || calls != 0 {
			t.Fatalf("PASE = %v with %d callbacks, want a failure and none", err, calls)
		}
	})
}

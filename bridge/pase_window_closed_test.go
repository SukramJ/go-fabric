// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"

	"github.com/SukramJ/go-fabric/transport/mrp"
)

// TestPBKDFParamRequestOnAClosedWindowIsAnsweredInvalidParam: a
// PBKDFParamRequest that meets a closed commissioning window — a window
// attached and closed, no PASE acceptor — is answered with a failure
// report (GeneralFailure, InvalidParameter) and begins nothing: no claim,
// no counted failure. matter.js sends InvalidParam when no PASE server is
// set (SecureChannelProtocol.ts:129-137 at db021da0; review II, owner
// decision 97). The controls: a bridge
// without a window, and one whose window is open without an acceptor, keep
// the fail-closed noop (no answer); an acceptor attached while the window
// is closed runs the handshake.
func TestPBKDFParamRequestOnAClosedWindowIsAnsweredInvalidParam(t *testing.T) {
	t.Parallel()
	request := buildTestPBKDFParamRequest(t, bytes.Repeat([]byte{0x22}, 32))
	const exchange = uint16(0x3100)
	key := HandshakeKey{InitiatorNodeID: 0xD00D, ExchangeID: exchange}

	t.Run("closed window, no acceptor", func(t *testing.T) {
		t.Parallel()
		r := newUnsecuredRig(t)
		r.b.AttachCommissioningWindow(NewCommissioningWindow())
		peer := newPipelinePeer(t, r.b, 0xD00D)
		_ = peer.send(mrp.SCOpcodePBKDFParamRequest, exchange, request, nil)
		body, _, _ := peer.recv(mrp.SCOpcodeStatusReport, "the window-closed StatusReport")
		if len(body) < 8 {
			t.Fatalf("StatusReport body %x too short", body)
		}
		general, protocol := binary.LittleEndian.Uint16(body[0:2]), binary.LittleEndian.Uint16(body[6:8])
		if general != mrp.SCStatusGeneralFailure || protocol != mrp.SCStatusProtocolInvalidParameter {
			t.Fatalf("StatusReport general %#x protocol %#x, want GeneralFailure / InvalidParameter", general, protocol)
		}
		if got := r.b.paseFailures.Load(); got != 0 {
			t.Fatalf("PASE failures = %d, want 0", got)
		}
		if r.b.paseHandshakeOfAnother(HandshakeKey{}) {
			t.Fatal("the refused request took the single-active-PASE claim")
		}
	})

	t.Run("no window", func(t *testing.T) {
		t.Parallel()
		r := newUnsecuredRig(t)
		peer := newPipelinePeer(t, r.b, 0xD00D)
		_ = peer.send(mrp.SCOpcodePBKDFParamRequest, exchange, request, nil)
		requireAcknowledgedOnly(t, r.b, exchange, "a PBKDFParamRequest without a window")
	})

	t.Run("open window, no acceptor", func(t *testing.T) {
		t.Parallel()
		r := newUnsecuredRig(t)
		w := NewCommissioningWindow()
		if err := w.OpenOwnWindow(context.Background(), 0); err != nil {
			t.Fatal(err)
		}
		r.b.AttachCommissioningWindow(w)
		peer := newPipelinePeer(t, r.b, 0xD00D)
		_ = peer.send(mrp.SCOpcodePBKDFParamRequest, exchange, request, nil)
		requireAcknowledgedOnly(t, r.b, exchange, "a PBKDFParamRequest on an open window without an acceptor")
	})

	t.Run("closed window, acceptor attached", func(t *testing.T) {
		t.Parallel()
		r := newUnsecuredRig(t)
		r.b.AttachCommissioningWindow(NewCommissioningWindow())
		a := newPaseAdapterWithVerifier(t)
		a.SetPBKDFParams(1000, []byte("SPAKE2P Key Salt"), 1)
		r.b.AttachPaseHandler(a)
		peer := newPipelinePeer(t, r.b, 0xD00D)
		_ = peer.send(mrp.SCOpcodePBKDFParamRequest, exchange, request, nil)
		peer.recv(mrp.SCOpcodePBKDFParamResponse, "the PBKDFParamResponse")
		if !r.b.paseHandshakeOfAnother(HandshakeKey{InitiatorNodeID: 0xD00D, ExchangeID: exchange + 1}) {
			t.Fatalf("the handshake %+v holds no claim", key)
		}
	})
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// csrDevice answers CSRRequest with NOCSRElements whose csrNonce is the
// one received (echo) or a fixed foreign one.
type csrDevice struct{ echo bool }

type csrResponseValue struct{ elements []byte }

func (v csrResponseValue) EncodeTLV(e *tlv.Encoder, tag tlv.Tag) {
	e.StartStruct(tag)
	e.PutOctets(tlv.ContextTag(0), v.elements)
	e.PutOctets(tlv.ContextTag(1), make([]byte, 64))
	_ = e.EndContainer()
}

func (d *csrDevice) handle(req imRequest) (opcode uint8, payload []byte, ok bool) {
	if req.opcode != im.OpcodeInvokeRequest {
		return 0, nil, false
	}
	path, fields, err := decodeInvoke(req.payload)
	if err != nil || path.Command != cmdCSRRequest {
		return 0, nil, false
	}
	var sent []byte
	if err := spec.DecodeFields(fields, "CsrRequest",
		spec.Bind(0, "CSRNonce", true, &sent, spec.DecodeBytesIn(32, 32))); err != nil {
		return 0, nil, false
	}
	nonce := make([]byte, 32)
	if d.echo {
		copy(nonce, sent)
	} else {
		for i := range nonce {
			nonce[i] = byte(i)
		}
	}
	elements := encodeTLV(func(e *tlv.Encoder) {
		e.StartStruct(tlv.AnonymousTag())
		e.PutOctets(tlv.ContextTag(1), []byte{0x30, 0x00}) // a CSR the key extraction refuses
		e.PutOctets(tlv.ContextTag(2), nonce)
		_ = e.EndContainer()
	})
	path.Command = 0x05 // CSRResponse
	return im.OpcodeInvokeResponse, invokeResponseOf(path, csrResponseValue{elements}), true
}

// TestCertificatesCsrNonceEchoed: the nonce the device echoes in
// NOCSRElements must be the one CSRRequest sent — connectedhomeip
// DefaultDeviceAttestationVerifier.cpp:868-870 at 6170af84
// (VerifyNodeOperationalCSRInformation, `csrNonceSpan.data_equal(csrNonce)`).
// matter.js draws the nonce (ControllerCommissioningFlow.ts:1412 at
// db021da0) and never reads it back: ahead of matter.js,
// BD-chip-Commissioner-CsrNonceEchoed. The DAC key is nil here, so the
// signature check is skipped and the nonce comparison is the first gate
// after decoding.
func TestCertificatesCsrNonceEchoed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	f, _ := newTestFlow(t, (&csrDevice{echo: false}).handle)
	_, err := f.certificates(ctx)
	if !errors.Is(err, ErrCommissioning) {
		t.Fatalf("foreign nonce: got %v, want ErrCommissioning", err)
	}

	// The echoed nonce passes the gate; the flow then fails on the CSR
	// itself, which is not a nonce error.
	f2, _ := newTestFlow(t, (&csrDevice{echo: true}).handle)
	_, err = f2.certificates(ctx)
	if err == nil || errors.Is(err, ErrCommissioning) {
		t.Fatalf("echoed nonce: got %v, want the CSR parse error", err)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioner

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	opcredsdef "thingfabric.dev/go-fabric/cluster/spec/operationalcredentials"
	"thingfabric.dev/go-fabric/controller/attestation"
	"thingfabric.dev/go-fabric/im"
	secattestation "thingfabric.dev/go-fabric/secure/attestation"
	"thingfabric.dev/go-fabric/tlv"
)

// signedValue is an AttestationResponse or CSRResponse with a signature
// of the given length.
type signedValue struct {
	elements []byte
	sigLen   int
}

func (v signedValue) EncodeTLV(e *tlv.Encoder, tag tlv.Tag) {
	e.StartStruct(tag)
	e.PutOctets(tlv.ContextTag(0), v.elements)
	e.PutOctets(tlv.ContextTag(1), make([]byte, v.sigLen))
	_ = e.EndContainer()
}

// TestCSRSignatureOfAnyLength: a CSRResponse signature that is not 64
// bytes is decoded and fails as the NOCSR check AttestationSignatureInvalid,
// a commissioning error — not as a decode error of the response (review
// II, R2-P6-doc-1857 (b), owner decision 216; NocsrValidator.ts:100-109 at
// db91886c).
func TestCSRSignatureOfAnyLength(t *testing.T) {
	t.Parallel()
	dev := func(req imRequest) (uint8, []byte, bool) {
		path, _, err := decodeInvoke(req.payload)
		if req.opcode != im.OpcodeInvokeRequest || err != nil || path.Command != opcredsdef.CmdCsrRequest {
			return 0, nil, false
		}
		path.Command = opcredsdef.CmdCsrResponse
		return im.OpcodeInvokeResponse, invokeResponseOf(path, signedValue{elements: []byte{0x15, 0x18}, sigLen: 65}), true
	}
	f, _ := newTestFlow(t, dev)
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	f.dacKey = &key.PublicKey
	_, err := f.certificates(context.Background())
	var ne *attestation.NOCSRError
	if !errors.Is(err, ErrCommissioning) || !errors.As(err, &ne) || ne.Check != attestation.NOCSRAttestationSignatureInvalid {
		t.Fatalf("a 65-byte CSR signature: %v, want the NOCSR check AttestationSignatureInvalid", err)
	}
}

// TestAttestationSignatureOfAnyLength: an AttestationResponse signature
// that is not 64 bytes is decoded and becomes the error finding
// AttestationSignatureInvalid, which the policy judges — not a decode
// error of the response (R2-P6-doc-1857 (b), decision 216).
func TestAttestationSignatureOfAnyLength(t *testing.T) {
	t.Parallel()
	chain, err := secattestation.BuildTestChain(0xFFF1, 0x8000)
	if err != nil {
		t.Fatal(err)
	}
	cd, err := secattestation.BuildTestCertificationDeclaration(0xFFF1, 0x8000)
	if err != nil {
		t.Fatal(err)
	}
	dev := func(req imRequest) (uint8, []byte, bool) {
		path, fields, err := decodeInvoke(req.payload)
		if req.opcode != im.OpcodeInvokeRequest || err != nil {
			return 0, nil, false
		}
		switch path.Command {
		case opcredsdef.CmdCertificateChainRequest:
			r, err := spec.DecodeStruct[opcredsdef.CertificateChainRequestRequest](fields)
			if err != nil {
				return 0, nil, false
			}
			cert := chain.DAC
			if r.CertificateType == opcredsdef.CertificateChainTypePaiCertificate {
				cert = chain.PAI
			}
			path.Command = opcredsdef.CmdCertificateChainResponse
			return im.OpcodeInvokeResponse, invokeResponseOf(path, opcredsdef.CertificateChainResponse{Certificate: cert}), true
		case opcredsdef.CmdAttestationRequest:
			r, err := spec.DecodeStruct[opcredsdef.AttestationRequestRequest](fields)
			if err != nil {
				return 0, nil, false
			}
			elements := encodeTLV(func(e *tlv.Encoder) {
				e.StartStruct(tlv.AnonymousTag())
				e.PutOctets(tlv.ContextTag(1), cd)
				e.PutOctets(tlv.ContextTag(2), r.AttestationNonce)
				e.PutUint32(tlv.ContextTag(3), 0)
				_ = e.EndContainer()
			})
			path.Command = opcredsdef.CmdAttestationResponse
			return im.OpcodeInvokeResponse, invokeResponseOf(path, signedValue{elements: elements, sigLen: 65}), true
		}
		return 0, nil, false
	}
	f, _ := newTestFlow(t, dev)
	f.data.vendorID, f.data.productID = new(uint16(0xFFF1)), new(uint16(0x8000))
	var judged []attestation.Finding
	f.opts.OnAttestationFailure = attestation.Policy{Decide: func(fs []attestation.Finding) (bool, string, error) {
		judged = fs
		return false, "", nil
	}}
	if _, err := f.deviceAttestation(context.Background()); err == nil {
		t.Fatal("the attestation passed with a 65-byte signature")
	}
	if len(judged) != 1 || judged[0].Level != attestation.LevelError || judged[0].Check != attestation.AttestationSignatureInvalid {
		t.Fatalf("the policy judged %+v, want one error finding AttestationSignatureInvalid", judged)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package attestation

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/tlv"
)

// NOCSRCheck names a check of [ValidateNOCSR] — matter.js NocsrCheck
// (packages/protocol/src/certificate/NocsrValidator.ts:14-36 at b25ed960).
// The string values are matter.js's.
type NOCSRCheck string

// The checks, NocsrValidator.ts:14-36 at b25ed960.
const (
	// NOCSRFieldMissing: NOCSRElements or AttestationSignature is missing.
	NOCSRFieldMissing NOCSRCheck = "FieldMissing"
	// NOCSRResponseEmpty: NOCSRElements or AttestationSignature is empty.
	NOCSRResponseEmpty NOCSRCheck = "ResponseEmpty"
	// NOCSRElementsTooLong: NOCSRElements is longer than RESP_MAX (900
	// bytes, Matter Core §11.18.4.1).
	NOCSRElementsTooLong NOCSRCheck = "ElementsTooLong"
	// NOCSRAttestationSignatureInvalid: the AttestationSignature is
	// malformed or does not verify with the DAC public key.
	NOCSRAttestationSignatureInvalid NOCSRCheck = "AttestationSignatureInvalid"
	// NOCSRElementsUnparseable: NOCSRElements is not a valid NOCSR
	// Information structure.
	NOCSRElementsUnparseable NOCSRCheck = "ElementsUnparseable"
	// NOCSRCSRNonceMismatch: the CSRNonce in NOCSRElements is not the one
	// sent in CSRRequest.
	NOCSRCSRNonceMismatch NOCSRCheck = "CsrNonceMismatch"
	// NOCSRCSRInvalid: the PKCS #10 CSR is malformed, uses another
	// algorithm or key type, or its own signature does not verify.
	NOCSRCSRInvalid NOCSRCheck = "CsrInvalid"
)

// NOCSRError is a CSRResponse that failed one of the [NOCSRCheck]s —
// matter.js NocsrError (NocsrValidator.ts:38-47 at b25ed960), a
// CommissioningError there; the commissioner returns it as one.
type NOCSRError struct {
	Check   NOCSRCheck
	Message string
	// Err is the cause, when the failure wraps one (a decoder's error).
	Err error
}

func (e *NOCSRError) Error() string { return "attestation: " + string(e.Check) + ": " + e.Message }

// Unwrap returns the cause.
func (e *NOCSRError) Unwrap() error { return e.Err }

// NOCSRContext is what [ValidateNOCSR] works with — matter.js
// NocsrValidator.Context (NocsrValidator.ts:50-61 at b25ed960).
type NOCSRContext struct {
	// Challenge is the attestation challenge of the secure session the
	// CSRRequest was sent on.
	Challenge []byte
	// CSRNonce is the CSRNonce sent in CSRRequest.
	CSRNonce []byte
	// DACPublicKey is the public key of the DAC the device reported.
	DACPublicKey *ecdsa.PublicKey
}

// NOCSRResponse is a CSRResponse as decoded, so a mandatory field the
// device left out is marked absent — matter.js
// Partial<OperationalCredentials.CsrResponse> (NocsrValidator.ts:63-64 at
// b25ed960).
type NOCSRResponse struct {
	// Elements and Signature are NOCSRElements and AttestationSignature.
	Elements, Signature []byte
	// ElementsAbsent and SignatureAbsent mark a field the device left
	// out; the zero value is a response that carries both.
	ElementsAbsent, SignatureAbsent bool
}

// ValidateNOCSR validates a CSRResponse and returns the operational
// public key of its CSR, as its 65-byte uncompressed point. Every check is
// a hard failure, a [*NOCSRError]: unlike device attestation, no policy
// can accept it. Mirrors matter.js
// packages/protocol/src/certificate/NocsrValidator.ts:78-127 at b25ed960
// (NocsrValidator.validate), check for check and in its order (Matter
// Core §6.4.6.1.1, §11.18.4.1, §11.18.4.9).
func ValidateNOCSR(ctx NOCSRContext, resp NOCSRResponse) ([]byte, error) {
	// NocsrValidator.ts:82-84.
	if resp.ElementsAbsent || resp.SignatureAbsent {
		return nil, &NOCSRError{Check: NOCSRFieldMissing, Message: "CSRResponse lacks NOCSRElements or AttestationSignature"}
	}
	// NocsrValidator.ts:86-91.
	if len(resp.Elements) == 0 || len(resp.Signature) == 0 {
		return nil, &NOCSRError{Check: NOCSRResponseEmpty, Message: "CSRResponse has empty NOCSRElements or AttestationSignature"}
	}
	// NocsrValidator.ts:93-98.
	if len(resp.Elements) > respMax {
		return nil, &NOCSRError{Check: NOCSRElementsTooLong, Message: fmt.Sprintf("CSRResponse NOCSRElements has %d bytes, more than RESP_MAX of %d", len(resp.Elements), respMax)}
	}
	// The signature over NOCSRElements ‖ challenge, raw r‖s, verifies with
	// the DAC key; a signature that is not 64 bytes fails it as matter.js's
	// EcdsaSignature does (NocsrValidator.ts:100-109).
	if ctx.DACPublicKey == nil || !verifyRaw(ctx.DACPublicKey, slices.Concat(resp.Elements, ctx.Challenge), resp.Signature) {
		return nil, &NOCSRError{Check: NOCSRAttestationSignatureInvalid, Message: "CSRResponse AttestationSignature does not verify with the DAC public key"}
	}
	// NocsrValidator.ts:111-115.
	csr, nonce, err := decodeCertSigningRequest(resp.Elements)
	if err != nil {
		return nil, &NOCSRError{Check: NOCSRElementsUnparseable, Message: "CSRResponse NOCSRElements cannot be decoded: " + err.Error(), Err: err}
	}
	// NocsrValidator.ts:117-122.
	if !bytes.Equal(nonce, ctx.CSRNonce) {
		return nil, &NOCSRError{Check: NOCSRCSRNonceMismatch, Message: "CSRNonce in the CSRResponse does not match the nonce sent to the device"}
	}
	// NocsrValidator.ts:124-126.
	key, err := publicKeyFromCSR(csr)
	if err != nil {
		return nil, &NOCSRError{Check: NOCSRCSRInvalid, Message: "CSRResponse contains an invalid CSR: " + err.Error(), Err: err}
	}
	return key, nil
}

// decodeCertSigningRequest is TlvCertSigningRequest
// (packages/protocol/src/common/OperationalCredentialsTypes.ts:24-31 at
// b25ed960): [1] certSigningRequest, [2] csrNonce (32 bytes), [3]-[5]
// vendor reserved. The nonce is returned for [ValidateNOCSR] to compare.
func decodeCertSigningRequest(b []byte) (csr, nonce []byte, err error) {
	n, err := spec.ReadElement(tlv.NewDecoder(b))
	if err != nil {
		return nil, nil, err
	}
	err = spec.DecodeFields(n, "CertSigningRequest",
		spec.Bind(1, "certSigningRequest", true, &csr, spec.DecodeBytesIn(0, math.MaxInt)),
		spec.Bind(2, "csrNonce", true, &nonce, spec.DecodeBytesIn(32, 32)))
	if err != nil {
		return nil, nil, err
	}
	return csr, nonce, nil
}

// publicKeyFromCSR is Certificate.getPublicKeyFromCsr
// (packages/protocol/src/certificate/kinds/Certificate.ts:842-903 at
// b25ed960): version 0, an EC P-256 key, ecdsa-with-SHA256, the CSR's own
// signature verified; the key as its 65-byte uncompressed point.
func publicKeyFromCSR(der []byte) ([]byte, error) {
	req, err := x509.ParseCertificateRequest(der)
	if err != nil {
		return nil, fmt.Errorf("CSR: %w", err)
	}
	if req.Version != 0 {
		return nil, fmt.Errorf("attestation: unsupported CSR version %d", req.Version)
	}
	key, ok := req.PublicKey.(*ecdsa.PublicKey)
	if !ok || key.Curve != elliptic.P256() {
		return nil, errors.New("attestation: unsupported public key in CSR")
	}
	if req.SignatureAlgorithm != x509.ECDSAWithSHA256 {
		return nil, errors.New("attestation: unsupported signature algorithm in CSR")
	}
	if err := req.CheckSignature(); err != nil {
		return nil, fmt.Errorf("CSR signature: %w", err)
	}
	return key.Bytes()
}

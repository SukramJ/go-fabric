// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package jointfabric

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"errors"
	"fmt"
	"math/big"

	"github.com/SukramJ/go-fabric/secure/mattercert"
)

// Errors of the AddICAC checks.
var (
	errICACChain     = errors.New("jointfabric: the ICAC does not chain to the fabric's root")
	errICACPublicKey = errors.New("jointfabric: the ICAC certifies another key than the CSR's")
)

// keyUsageKeyCertSign is the keyCertSign bit of a Matter certificate's
// key usage (Matter §6.5.11.3).
const keyUsageKeyCertSign = 0x0020

// verifyICACChain checks an ICAC against the accessing fabric's RCAC: an
// ICA certificate with keyCertSign, issued by the RCAC (its authority key
// id and issuer RCAC id) and signed by its key. Mirrors VerifyAddICACStep1
// (joint-fabric-administrator-server.cpp:393-411 at 6170af84: ValidateCert
// with kKeyCertSign and CertType::kICA).
func verifyICACChain(rcacTLV, icacTLV []byte) error {
	rcac, err := mattercert.Decode(rcacTLV)
	if err != nil {
		return fmt.Errorf("%w: root: %w", errICACChain, err)
	}
	icac, err := mattercert.Decode(icacTLV)
	if err != nil {
		return fmt.Errorf("%w: %w", errICACChain, err)
	}
	if !icac.IsICA() || !icac.Extensions.HasKeyUsage || icac.Extensions.KeyUsage&keyUsageKeyCertSign == 0 {
		return fmt.Errorf("%w: not an ICA certificate with keyCertSign", errICACChain)
	}
	if rcac.Extensions.HasSubjectKeyID && icac.Extensions.HasAuthorityKeyID && !bytes.Equal(rcac.Extensions.SubjectKeyID, icac.Extensions.AuthorityKeyID) {
		return fmt.Errorf("%w: authority key id", errICACChain)
	}
	if rcac.Subject.HasRCACID && (!icac.Issuer.HasRCACID || icac.Issuer.MatterRCACID != rcac.Subject.MatterRCACID) {
		return fmt.Errorf("%w: issuer", errICACChain)
	}
	key, err := rcac.PublicKeyECDSA()
	if err != nil {
		return fmt.Errorf("%w: %w", errICACChain, err)
	}
	tbs, err := mattercert.TBSToDER(icac)
	if err != nil {
		return fmt.Errorf("%w: %w", errICACChain, err)
	}
	if len(icac.Signature) != 64 {
		return fmt.Errorf("%w: signature", errICACChain)
	}
	digest := sha256.Sum256(tbs)
	if !ecdsa.Verify(key, digest[:], new(big.Int).SetBytes(icac.Signature[:32]), new(big.Int).SetBytes(icac.Signature[32:])) {
		return fmt.Errorf("%w: signature", errICACChain)
	}
	return nil
}

// verifyICACPublicKey checks that the ICAC certifies the key of the
// administrator's CSR, a PKCS#10 request whose signature verifies.
// Mirrors VerifyAddICACPublicKey (:413-430).
func verifyICACPublicKey(csr, icacTLV []byte) error {
	req, err := x509.ParseCertificateRequest(csr) // :423
	if err != nil {
		return fmt.Errorf("%w: CSR: %w", errICACPublicKey, err)
	}
	if err := req.CheckSignature(); err != nil {
		return fmt.Errorf("%w: CSR: %w", errICACPublicKey, err)
	}
	pub, ok := req.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return fmt.Errorf("%w: CSR key", errICACPublicKey)
	}
	raw, err := pub.Bytes()
	if err != nil {
		return fmt.Errorf("%w: CSR key: %w", errICACPublicKey, err)
	}
	icac, err := mattercert.Decode(icacTLV) // :424
	if err != nil {
		return fmt.Errorf("%w: %w", errICACPublicKey, err)
	}
	if !bytes.Equal(icac.PublicKey, raw) { // :426-428
		return errICACPublicKey
	}
	return nil
}

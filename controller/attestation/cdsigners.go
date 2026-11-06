// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package attestation

// The keys that may sign a Certification Declaration, as CHIP trusts them —
// matter.js packages/protocol/src/certificate/CdSigners.ts at db021da0:
//
//   - the CSA "Certification Declaration Signing Key" 001 to 005, built in
//     ([KnownCDSigner]);
//   - the Matter test CD signer of Appendix F, built in and marked
//     ([KnownCDSigner], [IsTestCDSigner]); the caller applies its test
//     policy;
//   - a certificate the CSA "Matter Certification and Testing CA" issued
//     and signed ([VerifyCDSignerCertificate]), for a DCL answer.
//
// No other key is a CD signer, in particular no PAA or PAI. CHIP ships the
// same set: gCdSigningKeys and the keys chained to gCdRootCert
// (src/credentials/attestation_verifier/DefaultDeviceAttestationVerifier.cpp
// :95-270 at 6170af84).

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	secattestation "github.com/SukramJ/go-fabric/secure/attestation"
)

// csaCDRootSKID is the subject key identifier of the CSA "Matter
// Certification and Testing CA", the root every CD signing key chains to
// (CdSigners.ts:11-12 at db021da0).
var csaCDRootSKID = mustHex("97E469D0C50414C26FC701F77E947739098DF6A5")

// csaCDRootPublicKey is that root's key (CdSigners.ts:14-17 at db021da0).
var csaCDRootPublicKey = mustHex("04384a3ac3aa5dbecb720fe6211d3cad48626ff711a0a4929fadd860f8e277ffc0de1f3eb6337a34847b5a3a056ea8710e9377eeedee251703fdd9350125cb8e48")

// csaCDSigningKeys are the CSA "Certification Declaration Signing Key" 001
// to 005 by subject key identifier, as CHIP ships them built in
// (CdSigners.ts:19-43 at db021da0).
var csaCDSigningKeys = map[string]string{
	"FE343F959947763B61EE4539131338494FE67D8E": "04cdeee93e44f8b72be3d1a9c07e21968b9afff3b403f05e1669d7b1e5caee6fc7714b42e7e23695e92cd7635473a280ae688f37bb9489e11629b9b94ff7b09929",
	"DD04DB585B214C1C581587E6568DF487B6DDC701": "04031937e8f94251045df27457bb46253ee3754e8c12ae28557d8027b9d4c356d91b408cff32275017b25f8c8ba20516d5c83af2b724128013dfcc8f95d200a90b",
	"471035E7C04EAAA8BE7C4D4C13E3E4C20995A84B": "049f575cd5fdb7521f10a4df31f073912b614728f2d37f5b6b96bc2cbf7c0b119690577d555d21a09660a8b082a339ea530f7a812d8693b56fd764185fabcd32f5",
	"F68603A3692E981072419EA1E1AB3854BD7795D3": "047cfc8d8810a89cf4fa191778f2afec78f8517a97a3e57fc213bad888e3611e74ffb684bdeba8a68b25234a5c358f373bab9b6d304e1306dd7620a528d1161c0b",
	"637F2634AD62EAFE6AF662EFB96F6FD2FCBFFC2F": "04438a52c662a2a6d72647f95eb753136ee4ae0fdb3aa9c16931426b3d0867f9103ae7d7a1b887e9f813a6f26f76bbcda93593787c12778ca8f2975dc45a2042f9",
}

// The CSA CD root's validity in Matter epoch seconds, 2022-07-07T19:28:04Z
// to 2122-06-13T19:28:04Z (CdSigners.ts:45-47 at db021da0).
const (
	csaCDRootNotBefore int64 = 710537284
	csaCDRootNotAfter  int64 = 3864137284
)

func mustHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

// p256Point is an uncompressed EC P-256 point as an ECDSA key.
func p256Point(b []byte) *ecdsa.PublicKey {
	k, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), b)
	if err != nil {
		panic(err)
	}
	return k
}

// CDSigner is a trusted Certification Declaration signer — matter.js
// CdSigners.Signer (CdSigners.ts:64-74 at db021da0).
type CDSigner struct {
	// PublicKey verifies the CD signature.
	PublicKey *ecdsa.PublicKey
	// IsTestSigner marks the Matter test CD signer: it may sign only a CD
	// of certification_type test or provisional, and only a caller that
	// accepts test certificates trusts it.
	IsTestSigner bool
}

// KnownCDSigner is CdSigners.known (CdSigners.ts:76-83 at db021da0): the
// built-in CSA signing key or the Matter test CD signer skid names. Each
// call hands out its own key.
func KnownCDSigner(skid []byte) (CDSigner, bool) {
	if IsTestCDSigner(skid) {
		point, err := secattestation.TestCMSSignerPrivateKey.PublicKey.Bytes()
		if err != nil {
			panic(err)
		}
		return CDSigner{PublicKey: p256Point(point), IsTestSigner: true}, true
	}
	if point, ok := csaCDSigningKeys[strings.ToUpper(hex.EncodeToString(skid))]; ok {
		return CDSigner{PublicKey: p256Point(mustHex(point))}, true
	}
	return CDSigner{}, false
}

// IsTestCDSigner is CdSigners.isTestSigner (CdSigners.ts:85-88 at
// db021da0): skid names the Matter test CD signer of Appendix F.
func IsTestCDSigner(skid []byte) bool {
	return bytes.Equal(skid, secattestation.TestCMSSignerSKID)
}

// VerifyCDSignerCertificate is CdSigners.verifyCertificate (CdSigners.ts:
// 90-136 at db021da0): der is a CD signer for skid when its subject key
// identifier is skid, it is not the CSA CD root itself, its authority key
// identifier is the root's and the root's key verifies its signature, and
// the root was valid when the certificate became valid (CHIP validates the
// chain at the certificate's notBefore). It returns the certificate's EC
// P-256 key; CHIP's strict X.509 profile checks of the chain are not
// repeated.
func VerifyCDSignerCertificate(der, skid []byte) (*ecdsa.PublicKey, error) {
	c, err := parseAttCert(der, kindCDSigner, false)
	if err != nil {
		return nil, err
	}
	x := c.x509
	switch {
	case !bytes.Equal(x.SubjectKeyId, skid):
		return nil, fmt.Errorf("attestation: certificate has subject key identifier %x, not %x", x.SubjectKeyId, skid)
	case bytes.Equal(x.SubjectKeyId, csaCDRootSKID):
		return nil, errors.New("attestation: the CSA CD root certificate is not itself a CD signer")
	case !bytes.Equal(x.AuthorityKeyId, csaCDRootSKID):
		return nil, fmt.Errorf("attestation: certificate is issued by %x, not the CSA CD root %x", x.AuthorityKeyId, csaCDRootSKID)
	case c.notBefore < csaCDRootNotBefore || c.notBefore > csaCDRootNotAfter:
		return nil, errors.New("attestation: certificate became valid outside the validity period of the CSA CD root")
	}
	if err := VerifySignedBy(x, p256Point(csaCDRootPublicKey)); err != nil {
		return nil, fmt.Errorf("attestation: certificate signature does not verify against the CSA CD root: %w", err)
	}
	return P256Key(x)
}

// lookupCDSigner is the validator's signer lookup (DeviceAttestationValidator.ts
// :460-463 at db021da0): the built-in signers, then — with a trust store —
// what the store knows, which mirrors DclCertificateService.lookupCdSigner
// (dcl/DclCertificateService.ts:901-970 at db021da0): a CSA-issued
// certificate, never the test signer, so a store's answer is no test
// signer.
func lookupCDSigner(store PAAStore, skid []byte) (CDSigner, bool) {
	if s, ok := KnownCDSigner(skid); ok {
		return s, true
	}
	if store == nil {
		return CDSigner{}, false
	}
	if k, ok := store.CDSigner(skid); ok && k != nil {
		return CDSigner{PublicKey: k}, true
	}
	return CDSigner{}, false
}

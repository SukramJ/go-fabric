// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package attestation

import (
	"crypto/ecdsa"
	"crypto/mldsa"
	"crypto/x509"
	"errors"
	"fmt"

	"github.com/SukramJ/go-fabric/internal/forward"
)

// PQC Phase 1 of the attestation certificates: a PAA and a PAI may carry
// an ML-DSA-44 or ML-DSA-65 key and any attestation certificate an ML-DSA
// signature, the DAC key stays EC P-256 — matter.js's "pqc-phase-1"
// forward feature (packages/model/src/common/Specification.ts:93 at
// 862ad75f) over packages/general/src/crypto/MlDsa.ts and
// packages/protocol/src/certificate/kinds/ (AttestationCertificates.ts:15-80,
// CertificateSignature.ts). The keys, signatures and their RFC 9881
// encodings are the standard library's (crypto/mldsa, crypto/x509 of Go
// 1.27), which parse and verify them as matter.js's MlDsa namespace does.
// Like matter.js, the module keeps the feature off: an ML-DSA attestation
// certificate is refused with [ErrMLDSAAttestationDisabled] unless
// internal/forward switches PQCPhase1 on, which only the module's tests do.
//
// Phase 1 stops at attestation: operational certificates (RCAC, ICAC,
// NOC) and CASE stay ECDSA P-256, in matter.js as here — its certificate
// kinds admit ML-DSA only in Paa/Pai/Dac.fromAsn1 (postQuantum,
// AttestationCertificates.ts:31-39), never in the operational ones.

// maxPQCDERCertificateSize is matter.js MAX_PQC_DER_CERTIFICATE_SIZE
// (kinds/common.ts:19), the size limit of an ML-DSA attestation
// certificate.
const maxPQCDERCertificateSize = 10240

// ErrMLDSAAttestationDisabled is an attestation certificate that uses
// ML-DSA while PQC Phase 1 is off — matter.js MlDsaAttestationDisabledError
// (AttestationCertificates.ts:23-28): the certificate may be valid; this
// build does not accept it.
var ErrMLDSAAttestationDisabled = errors.New("attestation: ML-DSA attestation certificates are not supported: PQC Phase 1 is not enabled")

// KeyAlgorithm is a certificate key's algorithm — matter.js
// CertificateKeyAlgorithm (CertificateSignature.ts:32-38).
type KeyAlgorithm string

// The key algorithms of an attestation certificate.
const (
	KeyECDSAP256 KeyAlgorithm = "ECDSA-P256"
	KeyMLDSA44   KeyAlgorithm = "ML-DSA-44"
	KeyMLDSA65   KeyAlgorithm = "ML-DSA-65"
)

// keyStrength orders the key algorithms — STRENGTH
// (CertificateSignature.ts:50-54).
var keyStrength = map[KeyAlgorithm]int{KeyECDSAP256: 0, KeyMLDSA44: 1, KeyMLDSA65: 2}

// MayIssue reports whether a PAA with key algorithm paa may issue a PAI
// with key algorithm pai: the PAI's no stronger than its PAA's, as PQC
// Phase 1 requires — mayIssue (CertificateSignature.ts:40-48).
func MayIssue(paa, pai KeyAlgorithm) bool { return keyStrength[pai] <= keyStrength[paa] }

// KeyAlgorithmOf is the algorithm of c's key: EC P-256, or ML-DSA-44 or
// -65 (matter.js publicKey, CertificatePublicKey :60-62); any other key is
// an error.
func KeyAlgorithmOf(c *x509.Certificate) (KeyAlgorithm, error) {
	if _, err := P256Key(c); err == nil {
		return KeyECDSAP256, nil
	}
	if k, ok := c.PublicKey.(*mldsa.PublicKey); ok {
		switch k.Parameters() {
		case mldsa.MLDSA44():
			return KeyMLDSA44, nil
		case mldsa.MLDSA65():
			return KeyMLDSA65, nil
		}
	}
	return "", fmt.Errorf("attestation: public key algorithm %v is neither EC P-256 nor ML-DSA-44/65", c.PublicKeyAlgorithm)
}

// usesMLDSA reports whether c carries an ML-DSA key or signature — the
// check of parseAttestationCertificate (AttestationCertificates.ts:33).
func usesMLDSA(c *x509.Certificate) bool {
	return c.PublicKeyAlgorithm == x509.MLDSA || c.SignatureAlgorithm == x509.MLDSA44 ||
		c.SignatureAlgorithm == x509.MLDSA65 || c.SignatureAlgorithm == x509.MLDSA87
}

// VerifyIssuedBy verifies c's signature with the key of its issuer —
// Certificate.verifySignature over verifyCertificateSignature
// (CertificateSignature.ts:84-120): an ecdsa-with-SHA256 signature needs
// an EC P-256 issuer key, an ML-DSA signature an issuer key of its
// parameter set (FIPS 204 with the empty context); any other signature
// algorithm, or a key of another algorithm than the signature's, is an
// error, as is a signature that does not verify. ML-DSA needs PQC Phase 1.
func VerifyIssuedBy(c, issuer *x509.Certificate) error {
	switch c.SignatureAlgorithm {
	case x509.ECDSAWithSHA256:
		key, ok := issuer.PublicKey.(*ecdsa.PublicKey)
		if !ok {
			return fmt.Errorf("attestation: signature is ecdsa-with-SHA256 but the signer key is %v", issuer.PublicKeyAlgorithm)
		}
		if _, err := P256Key(issuer); err != nil {
			return err
		}
		return VerifySignedBy(c, key)
	case x509.MLDSA44, x509.MLDSA65:
		if !forward.Enabled(forward.PQCPhase1) {
			return ErrMLDSAAttestationDisabled
		}
		want := mldsa.MLDSA44()
		if c.SignatureAlgorithm == x509.MLDSA65 {
			want = mldsa.MLDSA65()
		}
		key, ok := issuer.PublicKey.(*mldsa.PublicKey)
		if !ok || key.Parameters() != want {
			return fmt.Errorf("attestation: signature is %v but the signer key is %v", c.SignatureAlgorithm, issuer.PublicKeyAlgorithm)
		}
		if err := mldsa.Verify(key, c.RawTBSCertificate, c.Signature, nil); err != nil {
			return fmt.Errorf("attestation: signature does not verify: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("attestation: signature algorithm %v is neither ecdsa-with-SHA256 nor ML-DSA", c.SignatureAlgorithm)
	}
}

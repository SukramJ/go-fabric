// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package ca

// The Matter TLV certificate encoder: the layout of matter.js
// packages/protocol/src/certificate/kinds/definitions/operational.ts
// (BaseMatterCertificate :123-153, TlvCertificateExtension :36-49) at
// 0ceb202d, which secure/mattercert decodes (decode.go, the tags of
// Matter §6.5.1.1 and §6.5.6.1). Ported from the test CA
// bridge/bridgetest carried until brief AJ, which now mints through this
// package.

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/asn1"
	"errors"
	"fmt"
	"io"
	"math/big"
	"time"

	"github.com/SukramJ/go-fabric/secure/mattercert"
	"github.com/SukramJ/go-fabric/tlv"
)

// Matter TLV certificate tags (secure/mattercert/decode.go:16-28) and the
// extension tags inside tag 10 (operational.ts:36-49).
const (
	certTagSerial        = 1
	certTagSigAlgo       = 2
	certTagIssuer        = 3
	certTagNotBefore     = 4
	certTagNotAfter      = 5
	certTagSubject       = 6
	certTagPubKeyAlgo    = 7
	certTagCurve         = 8
	certTagPublicKey     = 9
	certTagExtensions    = 10
	certTagSignature     = 11
	extTagBasicConstr    = 1
	extTagKeyUsage       = 2
	extTagExtKeyUsage    = 3
	extTagSubjectKeyID   = 4
	extTagAuthorityKeyID = 5
)

// DN attribute tags (operational.ts:56-63; secure/mattercert/decode.go:30-41).
const (
	dnTagNodeID   = 17
	dnTagICACID   = 19
	dnTagRCACID   = 20
	dnTagFabricID = 21
	dnTagCAT      = 22
)

// keyUsage bits as the TLV certificate carries them — matter.js
// kinds/definitions/base.ts:10-20 (ExtensionKeyUsageBitmap: digitalSignature
// bit 0, keyCertSign bit 5, cRLSign bit 6).
const (
	keyUsageDigitalSignature = 0x0001
	keyUsageKeyCertSign      = 0x0020
	keyUsageCRLSign          = 0x0040
)

// Extended key usage values of a NOC: clientAuth (2) then serverAuth (1)
// — matter.js CertificateAuthority.ts:313 (extendedKeyUsage: [2, 1]).
const (
	extKeyUsageClientAuth = 2
	extKeyUsageServerAuth = 1
)

// matterEpochOffset is the Unix time of the Matter epoch, 2000-01-01
// 00:00:00 UTC (matter.js MATTER_EPOCH_OFFSET_S).
const matterEpochOffset int64 = 946684800

// yearSeconds is matter.js's year: 365 days (kinds/definitions/asn.ts:10).
const yearSeconds int64 = 365 * 24 * 60 * 60

// matterDate is the certificate time of t moved by years. Mirrors matter.js
// packages/protocol/src/certificate/kinds/definitions/asn.ts:18-22
// (jsToMatterDate) at 0ceb202d: Unix seconds, floored, minus the Matter
// epoch offset, plus the years; the X.520 non-well-defined date (a zero
// time here) encodes as 0. A result outside the 32-bit field is an error.
func matterDate(t time.Time, years int64) (uint32, error) {
	if t.IsZero() {
		return 0, nil
	}
	v := t.Unix() - matterEpochOffset + years*yearSeconds
	if v < 0 || v > int64(^uint32(0)) {
		return 0, fmt.Errorf("ca: certificate time %v%+d years is outside the Matter epoch range", t, years)
	}
	return uint32(v), nil
}

// dnAttr is one DN attribute in the order it is encoded.
type dnAttr struct {
	tag   uint8
	value uint64
}

// certFields are the parts of a certificate the CA chooses.
type certFields struct {
	serial    uint64
	issuer    []dnAttr
	notBefore uint32
	notAfter  uint32
	subject   []dnAttr
	pub       []byte // 65-byte uncompressed P-256
	isCA      bool
	subjectID []byte // SHA-1 of pub
	authority []byte // the signer's key identifier
}

// serialBytes is the certificate id as matter.js writes it:
// Bytes.fromHex(toHex(id)) (CertificateAuthority.ts:228, :258, :299), the
// big-endian bytes without leading zeros, one byte at least
// (general/src/util/Number.ts:46-49 pads to an even hex length).
func serialBytes(id uint64) []byte {
	b := new(big.Int).SetUint64(id).Bytes()
	if len(b) == 0 {
		return []byte{0}
	}
	return b
}

// encodeCert writes the certificate TLV with sig as its signature. The
// top-level structure is in schema order (operational.ts:123-153); the
// DN lists and the extension list keep the order the caller gives
// (TlvTaggedListPreservingOrder, :36, :110), which is the order of
// matter.js's object literals in CertificateAuthority.ts.
func encodeCert(f *certFields, sig []byte) ([]byte, error) {
	e := tlv.NewEncoder()
	e.StartStruct(tlv.AnonymousTag())
	e.PutOctets(tlv.ContextTag(certTagSerial), serialBytes(f.serial))
	e.PutUint(tlv.ContextTag(certTagSigAlgo), mattercert.SigAlgoECDSAWithSHA256)
	e.StartList(tlv.ContextTag(certTagIssuer))
	for _, a := range f.issuer {
		e.PutUint(tlv.ContextTag(a.tag), a.value)
	}
	_ = e.EndContainer()
	e.PutUint(tlv.ContextTag(certTagNotBefore), uint64(f.notBefore))
	e.PutUint(tlv.ContextTag(certTagNotAfter), uint64(f.notAfter))
	e.StartList(tlv.ContextTag(certTagSubject))
	for _, a := range f.subject {
		e.PutUint(tlv.ContextTag(a.tag), a.value)
	}
	_ = e.EndContainer()
	e.PutUint(tlv.ContextTag(certTagPubKeyAlgo), mattercert.PubKeyAlgoEC)
	e.PutUint(tlv.ContextTag(certTagCurve), mattercert.CurvePrime256v1)
	e.PutOctets(tlv.ContextTag(certTagPublicKey), f.pub)
	e.StartList(tlv.ContextTag(certTagExtensions))
	e.StartStruct(tlv.ContextTag(extTagBasicConstr))
	e.PutBool(tlv.ContextTag(1), f.isCA)
	_ = e.EndContainer()
	if f.isCA {
		e.PutUint(tlv.ContextTag(extTagKeyUsage), keyUsageKeyCertSign|keyUsageCRLSign)
	} else {
		e.PutUint(tlv.ContextTag(extTagKeyUsage), keyUsageDigitalSignature)
		e.StartArray(tlv.ContextTag(extTagExtKeyUsage))
		e.PutUint(tlv.AnonymousTag(), extKeyUsageClientAuth)
		e.PutUint(tlv.AnonymousTag(), extKeyUsageServerAuth)
		_ = e.EndContainer()
	}
	e.PutOctets(tlv.ContextTag(extTagSubjectKeyID), f.subjectID)
	e.PutOctets(tlv.ContextTag(extTagAuthorityKeyID), f.authority)
	_ = e.EndContainer()
	e.PutOctets(tlv.ContextTag(certTagSignature), sig)
	_ = e.EndContainer()
	return e.Bytes()
}

// signCert encodes f and signs it with key: the signature covers the
// X.509 DER form of the TBS certificate (secure/mattercert TBSToDER, the
// bytes matter.js Certificate.sign hashes), ECDSA P-256 over SHA-256,
// carried as the 64-byte r‖s.
func signCert(f *certFields, key *ecdsa.PrivateKey, rand io.Reader) ([]byte, error) {
	unsigned, err := encodeCert(f, make([]byte, 64))
	if err != nil {
		return nil, err
	}
	probe, err := mattercert.Decode(unsigned)
	if err != nil {
		return nil, fmt.Errorf("ca: encoded certificate does not decode: %w", err)
	}
	der, err := mattercert.TBSToDER(probe)
	if err != nil {
		return nil, fmt.Errorf("ca: TBS: %w", err)
	}
	digest := sha256.Sum256(der)
	asn, err := ecdsa.SignASN1(rand, key, digest[:])
	if err != nil {
		return nil, fmt.Errorf("ca: sign: %w", err)
	}
	sig, err := rawSignature(asn)
	if err != nil {
		return nil, err
	}
	return encodeCert(f, sig)
}

// rawSignature turns an ASN.1 ECDSA signature into the 64-byte r‖s form
// Matter certificates carry.
func rawSignature(asn []byte) ([]byte, error) {
	var sig struct{ R, S *big.Int }
	if _, err := asn1.Unmarshal(asn, &sig); err != nil {
		return nil, fmt.Errorf("ca: signature: %w", err)
	}
	if sig.R.BitLen() > 256 || sig.S.BitLen() > 256 {
		return nil, errors.New("ca: signature: component exceeds 32 bytes")
	}
	out := make([]byte, 64)
	sig.R.FillBytes(out[:32])
	sig.S.FillBytes(out[32:])
	return out, nil
}

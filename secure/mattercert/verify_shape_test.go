// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package mattercert_test

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/secure/mattercert"
)

// nocChain builds root → ICAC → NOC with the given tweaks applied to the
// ICAC and NOC option sets, and returns the verifier plus the two leaf
// certificates.
func nocChain(t *testing.T, tweakICAC, tweakNOC func(*verifyTestCertOpts)) (v *mattercert.Verifier, noc, icac []byte) {
	t.Helper()
	rootPriv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	icacPriv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	nocPriv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	now := nowEpoch()

	rootRaw := buildSignedCert(t, verifyTestCertOpts{
		notBefore: now - 100, issuerRCACID: 0x0001,
		subjectHasRCACID: true, subjectRCACID: 0x0001, pubKey: marshalPub(rootPriv),
	}, rootPriv)
	rootCert, err := mattercert.Decode(rootRaw)
	if err != nil {
		t.Fatal(err)
	}
	icacOpts := verifyTestCertOpts{
		notBefore: now - 100, issuerRCACID: 0x0001,
		subjectHasICACID: true, subjectICACID: 0x0002,
		subjectHasFabricID: true, subjectFabricID: 0xBBBB,
		pubKey: marshalPub(icacPriv),
	}
	if tweakICAC != nil {
		tweakICAC(&icacOpts)
	}
	nocOpts := verifyTestCertOpts{
		notBefore: now - 100, issuerRCACID: 0x0001, issuerHasICACID: true, issuerICACID: 0x0002,
		subjectHasNodeID: true, subjectNodeID: 0xAAAA,
		subjectHasFabricID: true, subjectFabricID: 0xBBBB,
		pubKey: marshalPub(nocPriv),
	}
	if tweakNOC != nil {
		tweakNOC(&nocOpts)
	}
	v, err = mattercert.NewVerifier(rootCert.PublicKey, mattercert.SystemTime{})
	if err != nil {
		t.Fatal(err)
	}
	return v, buildSignedCert(t, nocOpts, icacPriv), buildSignedCert(t, icacOpts, rootPriv)
}

// TestVerifyAndExtractPubKey_StructuralPredicates pins the predicates a
// cryptographically valid chain must still satisfy — the ones matter.js
// Noc.ts / Icac.ts enforce and this verifier used to omit. The first
// case is the one with teeth: an ICAC issued for fabric A signing a NOC
// for fabric B under the same root opened a session on B.
func TestVerifyAndExtractPubKey_StructuralPredicates(t *testing.T) {
	t.Parallel()
	bTrue, bFalse := true, false
	ku := func(v uint16) *uint16 { return &v }
	cases := []struct {
		name      string
		icac, noc func(*verifyTestCertOpts)
		wantErr   error
	}{
		{"well-formed chain verifies", nil, nil, nil},
		{"ICAC fabric id differs from NOC fabric id", func(o *verifyTestCertOpts) { o.subjectFabricID = 0xCCCC }, nil, mattercert.ErrChainBroken},
		{"ICAC without fabric id is allowed", func(o *verifyTestCertOpts) { o.subjectHasFabricID = false }, nil, nil},
		{"NOC flagged as CA", nil, func(o *verifyTestCertOpts) { o.extIsCA = &bTrue }, mattercert.ErrMalformed},
		{"NOC without digitalSignature keyUsage", nil, func(o *verifyTestCertOpts) { o.extKeyUsage = ku(0x0060) }, mattercert.ErrMalformed},
		{"NOC without serverAuth/clientAuth EKU", nil, func(o *verifyTestCertOpts) { o.extEKU = []uint8{3} }, mattercert.ErrMalformed},
		{"NOC fabric id 0", nil, func(o *verifyTestCertOpts) { o.subjectFabricID = 0 }, mattercert.ErrMalformed},
		{"NOC node id outside the operational range", nil, func(o *verifyTestCertOpts) { o.subjectNodeID = 0xFFFF_FFF0_0000_0001 }, mattercert.ErrMalformed},
		{"NOC carrying an ICAC id in its subject", nil, func(o *verifyTestCertOpts) { o.subjectHasICACID, o.subjectICACID = true, 0x0002 }, mattercert.ErrMalformed},
		{"ICAC not a CA", func(o *verifyTestCertOpts) { o.extIsCA = &bFalse }, nil, mattercert.ErrMalformed},
		{"ICAC keyUsage without keyCertSign", func(o *verifyTestCertOpts) { o.extKeyUsage = ku(0x0001) }, nil, mattercert.ErrMalformed},
		{"ICAC with digitalSignature added is allowed", func(o *verifyTestCertOpts) { o.extKeyUsage = ku(0x0061) }, nil, nil},
		{"ICAC carrying an EKU", func(o *verifyTestCertOpts) { o.extEKU = []uint8{1} }, nil, mattercert.ErrMalformed},
		{"NOC without extensions at all", nil, func(o *verifyTestCertOpts) { o.noExtensions = true }, mattercert.ErrMalformed},
		{"NOC with three CATs is allowed", nil, func(o *verifyTestCertOpts) { o.subjectCATs = []uint32{0x00010001, 0x00020001, 0x00030002} }, nil},
		{"NOC with four CATs", nil, func(o *verifyTestCertOpts) { o.subjectCATs = []uint32{0x00010001, 0x00020001, 0x00030001, 0x00040001} }, mattercert.ErrMalformed},
		{"NOC CAT with version 0", nil, func(o *verifyTestCertOpts) { o.subjectCATs = []uint32{0x00010000} }, mattercert.ErrMalformed},
		{"NOC CAT identifier repeated", nil, func(o *verifyTestCertOpts) { o.subjectCATs = []uint32{0x00010001, 0x00010002} }, mattercert.ErrMalformed},
		{"NOC subjectKeyIdentifier not 160 bit", nil, func(o *verifyTestCertOpts) { o.extSKID = []byte{1, 2, 3} }, mattercert.ErrMalformed},
		{"ICAC carrying an RCAC id", func(o *verifyTestCertOpts) { o.subjectHasRCACID, o.subjectRCACID = true, 0x0001 }, nil, mattercert.ErrMalformed},
		{"ICAC carrying CATs", func(o *verifyTestCertOpts) { o.subjectCATs = []uint32{0x00010001} }, nil, mattercert.ErrMalformed},
		{"ICAC fabric id 0", func(o *verifyTestCertOpts) { o.subjectFabricID = 0 }, nil, mattercert.ErrMalformed},
		{"ICAC subjectKeyIdentifier not 160 bit", func(o *verifyTestCertOpts) { o.extSKID = []byte{9} }, nil, mattercert.ErrMalformed},
		{"NOC with a nested CAT-free chain and an oversized ICAC", func(o *verifyTestCertOpts) { o.extSKID = bytes.Repeat([]byte{0xAA}, 20) }, nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v, noc, icac := nocChain(t, tc.icac, tc.noc)
			_, err := v.VerifyAndExtractPubKey(noc, icac)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// TestVerifyAndExtractPubKey_CapsTLVSize — §6.1.3 caps an operational
// certificate at 400 TLV bytes (matter.js OperationalBase.ts:19); the
// Sigma path used to accept any length.
func TestVerifyAndExtractPubKey_CapsTLVSize(t *testing.T) {
	t.Parallel()
	v, noc, icac := nocChain(t, nil, nil)
	big := append(append([]byte(nil), noc...), make([]byte, mattercert.MaxOperationalCertTLVBytes)...)
	if _, err := v.VerifyAndExtractPubKey(big, icac); !errors.Is(err, mattercert.ErrMalformed) {
		t.Fatalf("a %d-byte NOC was not refused: %v", len(big), err)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mattercert_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/secure/mattercert"
)

// countingTime is a TimeSource that counts how often it is read.
type countingTime struct {
	t     time.Time
	reads atomic.Int64
}

func (c *countingTime) Now() time.Time {
	c.reads.Add(1)
	return c.t
}

// TestCertValidityNotEnforced pins BD-Matter-CertValidityNotEnforced
// (notes/parity/by_design.md): a NOC or an ICAC outside its validity
// period verifies, on either bound, and the verifier's clock is not read.
//
// matter.js's verification of an operational certificate ends with "notBefore
// date should be already reached, notAfter is not checked right now": a
// notBefore in the future is a warning whose throw is commented out, and
// notAfter is compared with nothing
// (packages/protocol/src/certificate/kinds/OperationalBase.ts:99-106 at
// db021da0). TestVerifyAndExtractPubKey_Expired, …_ICAC_Expired and
// TestCheckValidity_NotYetValid hold three of the four cases one by one;
// this test holds all four against one clock and adds the half no other
// test has — that the clock the verifier was built with is never asked.
func TestCertValidityNotEnforced(t *testing.T) {
	t.Parallel()
	// The verifier's clock: 2026-11-13T00:00:00Z, as Matter-epoch seconds
	// for the certificates' bounds.
	const matterEpochUnix = 946684800
	clockAt := time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)
	now := uint64(clockAt.Unix() - matterEpochUnix)
	const year = 365 * 24 * 3600

	type period struct{ notBefore, notAfter uint64 }
	valid := period{now - year, now + year}
	expired := period{now - 2*year, now - year}
	notYet := period{now + year, now + 2*year}

	for _, tc := range []struct {
		name      string
		icac, noc period
		withICAC  bool
	}{
		{"an expired NOC under the root", period{}, expired, false},
		{"a NOC that is not yet valid under the root", period{}, notYet, false},
		{"an expired NOC under an ICAC", valid, expired, true},
		{"an expired ICAC", expired, valid, true},
		{"an ICAC that is not yet valid", notYet, valid, true},
		{"an expired ICAC and an expired NOC", expired, expired, true},
		// The control: the same chain inside its period.
		{"control: NOC and ICAC inside their period", valid, valid, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rootPriv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			icacPriv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			nocPriv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)

			rootRaw := buildSignedCert(t, verifyTestCertOpts{
				notBefore: now - 10*year, notAfter: 0,
				issuerRCACID: 0x0001, subjectHasRCACID: true, subjectRCACID: 0x0001,
				pubKey: marshalPub(rootPriv),
			}, rootPriv)
			rootCert, err := mattercert.Decode(rootRaw)
			if err != nil {
				t.Fatalf("Decode root: %v", err)
			}

			nocOpts := verifyTestCertOpts{
				notBefore: tc.noc.notBefore, notAfter: tc.noc.notAfter,
				issuerRCACID:     0x0001,
				subjectHasNodeID: true, subjectNodeID: 0xAAAA,
				subjectHasFabricID: true, subjectFabricID: 0xBBBB,
				pubKey: marshalPub(nocPriv),
			}
			var icacRaw []byte
			signer := rootPriv
			if tc.withICAC {
				icacRaw = buildSignedCert(t, verifyTestCertOpts{
					notBefore: tc.icac.notBefore, notAfter: tc.icac.notAfter,
					issuerRCACID:     0x0001,
					subjectHasICACID: true, subjectICACID: 0x9999,
					pubKey: marshalPub(icacPriv),
				}, rootPriv)
				nocOpts.issuerHasICACID, nocOpts.issuerICACID = true, 0x9999
				signer = icacPriv
			}
			nocRaw := buildSignedCert(t, nocOpts, signer)

			clk := &countingTime{t: clockAt}
			v, err := mattercert.NewVerifier(rootCert.PublicKey, clk)
			if err != nil {
				t.Fatalf("NewVerifier: %v", err)
			}
			pub, err := v.VerifyAndExtractPubKey(nocRaw, icacRaw)
			if err != nil {
				t.Fatalf("VerifyAndExtractPubKey = %v; the chain verifies whatever its validity period says (OperationalBase.ts:99-106 at db021da0)", err)
			}
			if !pub.Equal(&nocPriv.PublicKey) {
				t.Fatal("the extracted key is not the NOC's")
			}
			if n := clk.reads.Load(); n != 0 {
				t.Fatalf("the verifier read its clock %d times; the by_design row says it is stored and never read", n)
			}
		})
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mattercert_test

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/secure/mattercert"
)

// A verifier built from the root certificate checks the authorityKeyIdentifier
// of a NOC the root signs directly against the root's subjectKeyIdentifier:
// matter.js Noc.ts:170-184 at 862ad75f picks the issuer by
// `Bytes.areEqual(authorityKeyIdentifier, root.cert.extensions.subjectKeyIdentifier)`
// and throws a CertificateError when neither the root nor an ICA matches. A
// verifier built from the root's public key alone cannot, and keeps
// accepting the signature-valid chain.
func TestVerifierFromRCACRefusesARootSignedNOCWithAForeignAKID(t *testing.T) {
	t.Parallel()
	chain := buildChainNoICA(t)
	root, err := mattercert.Decode(chain.rootRaw)
	if err != nil {
		t.Fatalf("Decode root: %v", err)
	}
	fromRCAC, err := mattercert.NewVerifierFromRCAC(chain.rootRaw, clock.System{})
	if err != nil {
		t.Fatalf("NewVerifierFromRCAC: %v", err)
	}
	if _, err := fromRCAC.VerifyAndExtractPubKey(chain.nocRaw, nil); err != nil {
		t.Fatalf("root-signed NOC naming the root: %v", err)
	}

	foreign := bytes.Repeat([]byte{0xA5}, 20)
	nocPriv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	now := nowEpoch()
	noc := buildSignedCert(t, verifyTestCertOpts{
		notBefore:          now - 100,
		issuerRCACID:       0x0001,
		subjectHasNodeID:   true,
		subjectNodeID:      0xAAAA,
		subjectHasFabricID: true,
		subjectFabricID:    0xBBBB,
		pubKey:             marshalPub(nocPriv),
		extAKID:            foreign,
	}, chain.rootPriv)

	if _, err := fromRCAC.VerifyAndExtractPubKey(noc, nil); !errors.Is(err, mattercert.ErrChainBroken) {
		t.Fatalf("root-signed NOC with a foreign AKID = %v, want ErrChainBroken", err)
	}
	fromKey, err := mattercert.NewVerifier(root.PublicKey, clock.System{})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	if _, err := fromKey.VerifyAndExtractPubKey(noc, nil); err != nil {
		t.Fatalf("NewVerifier (no root SKID) refused the signature-valid chain: %v", err)
	}
}

// With an ICAC, the ICAC's authorityKeyIdentifier must name the root
// (matter.js Icac.ts:159-163 at 862ad75f), checked once the root
// certificate is known.
func TestVerifierFromRCACRefusesAnICACWithAForeignAKID(t *testing.T) {
	t.Parallel()
	chain := buildChainWithICA(t)
	fromRCAC, err := mattercert.NewVerifierFromRCAC(chain.rootRaw, clock.System{})
	if err != nil {
		t.Fatalf("NewVerifierFromRCAC: %v", err)
	}
	if _, err := fromRCAC.VerifyAndExtractPubKey(chain.nocRaw, chain.icacRaw); err != nil {
		t.Fatalf("chain via ICAC: %v", err)
	}
	icacPriv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	now := nowEpoch()
	icac := buildSignedCert(t, verifyTestCertOpts{
		notBefore:        now - 100,
		issuerRCACID:     0x0001,
		subjectHasICACID: true,
		subjectICACID:    0x9999,
		pubKey:           marshalPub(icacPriv),
		extAKID:          bytes.Repeat([]byte{0xC3}, 20),
	}, chain.rootPriv)
	if _, err := fromRCAC.VerifyAndExtractPubKey(chain.nocRaw, icac); !errors.Is(err, mattercert.ErrChainBroken) {
		t.Fatalf("ICAC with a foreign AKID = %v, want ErrChainBroken", err)
	}
}

// NewVerifierFromRCAC refuses what is not a root certificate.
func TestNewVerifierFromRCACRefusesANonRoot(t *testing.T) {
	t.Parallel()
	chain := buildChainNoICA(t)
	if _, err := mattercert.NewVerifierFromRCAC(chain.nocRaw, nil); !errors.Is(err, mattercert.ErrMalformed) {
		t.Fatalf("NewVerifierFromRCAC(NOC) = %v, want ErrMalformed", err)
	}
	if _, err := mattercert.NewVerifierFromRCAC([]byte{0x15}, nil); err == nil {
		t.Fatal("NewVerifierFromRCAC accepted a truncated certificate")
	}
}

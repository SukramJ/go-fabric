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

	"thingfabric.dev/go-fabric/clock"
	"thingfabric.dev/go-fabric/secure/mattercert"
)

// A verifier built from the root certificate checks the authorityKeyIdentifier
// of a NOC the root signs directly against the root's subjectKeyIdentifier:
// matter.js Noc.ts:174-188 at b25ed960 picks the issuer by
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
// (matter.js Icac.ts:154-158 at b25ed960), checked once the root
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

// A self-signed root whose basic constraints say is-ca = false is refused,
// with or without a path length. Both references refuse it: connectedhomeip
// src/credentials/CHIPCert.cpp:1135 at 6170af84,
// `VerifyOrReturnError(certData.mCertFlags.Has(CertFlags::kIsCA), CHIP_ERROR_CERT_USAGE_NOT_ALLOWED);`,
// and matter.js packages/protocol/src/certificate/kinds/Rcac.ts:38-40 at
// b25ed960 ("Root certificate must be a CA."). R2-C2-par-0150.
func TestValidateRCACRefusesARootThatIsNotACAParityMatterJS(t *testing.T) {
	t.Parallel()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		edit func(o *rcacBuildOpts)
		ok   bool
	}{
		{"is-ca true (control)", func(*rcacBuildOpts) {}, true},
		{"is-ca false, pathLen 1", func(o *rcacBuildOpts) { o.basicConstraintsIsCA = false }, false},
		{"is-ca false, no pathLen", func(o *rcacBuildOpts) { o.basicConstraintsIsCA, o.basicConstraintsHasPathLen = false, false }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := validRCACOpts(priv)
			tc.edit(&o)
			c, err := mattercert.Decode(buildRCACTLV(t, priv, o))
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			err = mattercert.ValidateRCAC(c)
			if tc.ok {
				if err != nil {
					t.Fatalf("ValidateRCAC = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, mattercert.ErrInvalidRCAC) {
				t.Fatalf("ValidateRCAC = %v, want ErrInvalidRCAC", err)
			}
		})
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mattercert_test

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"slices"
	"testing"

	"thingfabric.dev/go-fabric/clock"
	"thingfabric.dev/go-fabric/secure/mattercert"
	"thingfabric.dev/go-fabric/tlv"
)

// A verifier built from the root certificate checks the authorityKeyIdentifier
// of a NOC the root signs directly against the root's subjectKeyIdentifier:
// matter.js Noc.ts:174-188 at db91886c picks the issuer by
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
// (matter.js Icac.ts:154-158 at db91886c), checked once the root
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
// db91886c ("Root certificate must be a CA."). R2-C2-par-0150.
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
			t.Parallel()
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

// A self-signed root whose subject carries an icac-id, a fabric id of 0
// or CATs is refused: both references refuse them (connectedhomeip
// src/credentials/CHIPCert.cpp:1129, ChipDN::GetCertType :606-687 at
// 6170af84; matter.js packages/protocol/src/certificate/kinds/Rcac.ts:69-71,
// :74-76, :84-86 at b25ed960). A firmware-signing-id stays admitted, as
// matter.js admits it, and so does a fabric id other than 0.
// R2-C2-par-0150, decision 516.
func TestValidateRCACRefusesARootSubjectOfAnotherKindParityMatterJS(t *testing.T) {
	t.Parallel()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		extra func(e *tlv.Encoder)
		ok    bool
	}{
		{"plain root (control)", nil, true},
		{"fabric id 1", func(e *tlv.Encoder) { e.PutUint(tlv.ContextTag(21), 1) }, true},
		{"firmware-signing-id", func(e *tlv.Encoder) { e.PutUint(tlv.ContextTag(18), 7) }, true},
		{"icac-id", func(e *tlv.Encoder) { e.PutUint(tlv.ContextTag(19), 2) }, false},
		{"fabric id 0", func(e *tlv.Encoder) { e.PutUint(tlv.ContextTag(21), 0) }, false},
		{"one CAT", func(e *tlv.Encoder) { e.PutUint(tlv.ContextTag(22), 0x0001_0001) }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			o := validRCACOpts(priv)
			o.subjectExtra = tc.extra
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

// A self-signed root whose subjectKeyIdentifier and authorityKeyIdentifier
// are not 20 bytes each is refused. matter.js
// packages/protocol/src/certificate/kinds/Rcac.ts:112-126 at db91886c
// requires both present and of byteLength 20; connectedhomeip refuses the
// same at decode (src/credentials/CHIPCert.cpp:143-144,
// src/lib/core/TLVReader.h:481, src/credentials/CHIPCert.h:50 at 6170af84).
// The TLV decoder refuses another length already; the DER decoder does
// not, so the root here comes through DecodeDER, signed over its rebuilt
// TBS so that only the key-id rule can refuse it. R2-C2-par-0150,
// decision 523.
func TestValidateRCACRefusesAKeyIdentifierOfAnotherLengthParityMatterJS(t *testing.T) {
	t.Parallel()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		n    int
		ok   bool
	}{
		{"20 bytes (control)", 20, true},
		{"19 bytes", 19, false},
		{"21 bytes", 21, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c, err := mattercert.Decode(buildRCACTLV(t, priv, validRCACOpts(priv)))
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			id := bytes.Repeat([]byte{0xA5}, tc.n)
			c.Extensions.SubjectKeyID, c.Extensions.AuthorityKeyID = id, append([]byte(nil), id...)
			c.TBSDER = nil
			tbs, err := mattercert.TBSToDER(c)
			if err != nil {
				t.Fatalf("TBSToDER: %v", err)
			}
			digest := sha256.Sum256(tbs)
			r, s, err := ecdsa.Sign(rand.Reader, priv, digest[:])
			if err != nil {
				t.Fatal(err)
			}
			c.Signature = append(r.FillBytes(make([]byte, 32)), s.FillBytes(make([]byte, 32))...)
			der, err := mattercert.EncodeDER(c)
			if err != nil {
				t.Fatalf("EncodeDER: %v", err)
			}
			back, err := mattercert.DecodeDER(der)
			if err != nil {
				t.Fatalf("DecodeDER: %v", err)
			}
			if len(back.Extensions.SubjectKeyID) != tc.n {
				t.Fatalf("DecodeDER kept a %d-byte subjectKeyIdentifier, want %d", len(back.Extensions.SubjectKeyID), tc.n)
			}
			err = mattercert.ValidateRCAC(back)
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

// A self-signed root that carries an extendedKeyUsage extension is
// refused, as matter.js refuses it: packages/protocol/src/certificate/kinds/Rcac.ts:107-110
// at db91886c ("The extended key usage extension SHALL NOT be present.",
// `if (extensions.extendedKeyUsage !== undefined)` throws). connectedhomeip's
// ValidateChipRCAC (src/credentials/CHIPCert.cpp:1116-1144 at 6170af84)
// reads no extendedKeyUsage; matter.js decides (decision 539). The root
// is re-signed over its rebuilt TBS, so that only the extendedKeyUsage
// rule can refuse it.
func TestValidateRCACRefusesAnExtendedKeyUsageParityMatterJS(t *testing.T) {
	t.Parallel()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		eku  []uint8
		ok   bool
	}{
		{"no extendedKeyUsage (control)", nil, true},
		{"clientAuth", []uint8{2}, false},
		{"serverAuth and clientAuth", []uint8{1, 2}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c, err := mattercert.Decode(buildRCACTLV(t, priv, validRCACOpts(priv)))
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if tc.eku != nil {
				c.Extensions.HasExtendedKeyUsage, c.Extensions.ExtendedKeyUsage = true, tc.eku
				// The encoding order: extendedKeyUsage (3) after keyUsage (2).
				i := slices.Index(c.Extensions.Order, 2)
				if i < 0 {
					t.Fatalf("no keyUsage in the extension order %v", c.Extensions.Order)
				}
				c.Extensions.Order = slices.Insert(c.Extensions.Order, i+1, 3)
			}
			c.TBSDER = nil
			tbs, err := mattercert.TBSToDER(c)
			if err != nil {
				t.Fatalf("TBSToDER: %v", err)
			}
			digest := sha256.Sum256(tbs)
			r, s, err := ecdsa.Sign(rand.Reader, priv, digest[:])
			if err != nil {
				t.Fatal(err)
			}
			c.Signature = append(r.FillBytes(make([]byte, 32)), s.FillBytes(make([]byte, 32))...)
			der, err := mattercert.EncodeDER(c)
			if err != nil {
				t.Fatalf("EncodeDER: %v", err)
			}
			back, err := mattercert.DecodeDER(der)
			if err != nil {
				t.Fatalf("DecodeDER: %v", err)
			}
			if back.Extensions.HasExtendedKeyUsage != (tc.eku != nil) {
				t.Fatalf("DecodeDER extendedKeyUsage present %v, want %v", back.Extensions.HasExtendedKeyUsage, tc.eku != nil)
			}
			err = mattercert.ValidateRCAC(back)
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

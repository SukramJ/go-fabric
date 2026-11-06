// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package jcm_test

// A port of matter.js
// packages/node/test/behaviors/operational-credentials/VendorIdVerificationTest.ts
// at db021da0 (matter.js #4880), by meaning: verification fails for
// certificates the peer sent that do not decode — a repeated or missing
// subject attribute of the NOC, a repeated one of the RCAC or the VVSC —
// and for a VVSC that fails its profile (VendorIdVerification.ts:217-256,
// Vvsc.ts). matter.js's verifyData returns false; jcm.Verify returns an
// error. matter.js's statement there is "content the VVSC check never
// reaches"; here the statement path is complete, so the cases beyond
// matter.js's six check it end to end (verifyStatement).

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // the SubjectKeyID derivation, not a security hash
	"crypto/sha256"
	"errors"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/client/jcm"
	opcreds "github.com/SukramJ/go-fabric/cluster/spec/operationalcredentials"
	"github.com/SukramJ/go-fabric/secure/mattercert"
)

// rawSign is a 64-byte r||s P-256 signature over SHA-256(msg).
func rawSign(t *testing.T, key *ecdsa.PrivateKey, msg []byte) []byte {
	t.Helper()
	digest := sha256.Sum256(msg)
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return sig
}

func pubOf(t *testing.T, k *ecdsa.PrivateKey) []byte {
	t.Helper()
	b, err := k.PublicKey.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func newKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// vvscSignedBy is a VVSC of key signed by issuer (VendorIdVerificationTest.ts
// :30-50 signs it with its own key; here a vendor's trust anchor does).
func vvscSignedBy(t *testing.T, key, issuer *ecdsa.PrivateKey, keyUsage uint16) ([]byte, []byte) {
	t.Helper()
	pub := pubOf(t, key)
	skid := sha1.Sum(pub)              //nolint:gosec // SubjectKeyID
	akid := sha1.Sum(pubOf(t, issuer)) //nolint:gosec // AuthorityKeyID
	c := &mattercert.Certificate{
		SerialNumber: []byte{0x01}, SignatureAlgorithm: mattercert.SigAlgoECDSAWithSHA256,
		// The DN orders set, as a decoded certificate carries them: the
		// TBS DER of a hand-built DN without one omits matter-vvs-id.
		Issuer:    mattercert.DistinguishedName{MatterRCACID: 1, HasRCACID: true, Order: []uint8{20}},
		Subject:   mattercert.DistinguishedName{MatterVVSID: 3, HasVVSID: true, Order: []uint8{23}},
		NotBefore: 1, NotAfter: 0x7FFF_FFFF,
		PublicKeyAlgorithm: mattercert.PubKeyAlgoEC, EllipticCurveID: mattercert.CurvePrime256v1, PublicKey: pub,
		Extensions: mattercert.CertificateExtensions{
			HasBasicConstraints: true,
			HasKeyUsage:         true, KeyUsage: keyUsage,
			HasSubjectKeyID: true, SubjectKeyID: skid[:],
			HasAuthorityKeyID: true, AuthorityKeyID: akid[:],
		},
	}
	tbs, err := mattercert.TBSToDER(c)
	if err != nil {
		t.Fatal(err)
	}
	c.Signature = rawSign(t, issuer, tbs)
	raw, err := mattercert.Encode(c)
	if err != nil {
		t.Fatal(err)
	}
	return raw, skid[:]
}

// anchorCert is a trust anchor of key in Matter TLV; its own signature is
// not read.
func anchorCert(t *testing.T, key *ecdsa.PrivateKey) []byte {
	t.Helper()
	raw, err := mattercert.Encode(&mattercert.Certificate{
		SerialNumber: []byte{0x02}, SignatureAlgorithm: mattercert.SigAlgoECDSAWithSHA256,
		Issuer:    mattercert.DistinguishedName{MatterRCACID: 1, HasRCACID: true},
		Subject:   mattercert.DistinguishedName{MatterRCACID: 1, HasRCACID: true},
		NotBefore: 1, NotAfter: 0x7FFF_FFFF,
		PublicKeyAlgorithm: mattercert.PubKeyAlgoEC, EllipticCurveID: mattercert.CurvePrime256v1, PublicKey: pubOf(t, key),
		Extensions: mattercert.CertificateExtensions{HasBasicConstraints: true, BasicConstraintsIsCA: true, HasKeyUsage: true, KeyUsage: 0x0060},
		Signature:  make([]byte, 64),
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// redo re-encodes a certificate with mutate applied to its decoded form
// (VendorIdVerificationTest.ts:52-63 edits the TLV stream).
func redo(t *testing.T, raw []byte, mutate func(c *mattercert.Certificate)) []byte {
	t.Helper()
	c, err := mattercert.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	mutate(c)
	out, err := mattercert.Encode(c)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func repeated(tag uint8) func(c *mattercert.Certificate) {
	return func(c *mattercert.Certificate) {
		i := slices.Index(c.Subject.Order, tag)
		c.Subject.Order = slices.Insert(c.Subject.Order, i+1, tag)
	}
}

// statementCase is a fabric whose administrator holds a statement signed
// by its VVSC, the VVSC signed by the vendor's trust anchor.
type statementCase struct {
	f          *jfFabric
	info       jcm.Info
	vvscKey    *ecdsa.PrivateKey
	vendorRoot *ecdsa.PrivateKey
	skid       []byte
}

func newStatementCase(t *testing.T, keyUsage uint16) *statementCase {
	t.Helper()
	sc := &statementCase{f: newJFFabric(t), vvscKey: newKey(t), vendorRoot: newKey(t)}
	var vvsc []byte
	vvsc, sc.skid = vvscSignedBy(t, sc.vvscKey, sc.vendorRoot, keyUsage)
	sc.info = sc.f.info()
	sc.info.AdminVVSC = vvsc
	rcac, err := mattercert.Decode(sc.f.rcac)
	if err != nil {
		t.Fatal(err)
	}
	// The statement signs statement_version || vendor_fabric_binding_message
	// || signer_skid; the binding message is the tail of the to-be-signed
	// message without a statement.
	tbs, err := jcm.ToBeSigned(sc.f.fabricIndex, make([]byte, 32), make([]byte, 16), rcac.PublicKey, sc.f.fabricID, sc.f.vendorID)
	if err != nil {
		t.Fatal(err)
	}
	binding := tbs[len(tbs)-76:]
	msg := append(append([]byte{0x21}, binding...), sc.skid...)
	sc.info.AdminVIDVerificationStatement = append(append([]byte{0x21}, sc.skid...), rawSign(t, sc.vvscKey, msg)...)
	return sc
}

// verifyWith signs the response over the to-be-signed message with the
// statement appended (CHIPCryptoPAL.h:150-155) and verifies it.
func (sc *statementCase) verifyWith(t *testing.T, info jcm.Info, lookup jcm.TrustAnchorLookup) error {
	t.Helper()
	rcac, err := mattercert.Decode(sc.f.rcac)
	if err != nil {
		t.Fatal(err)
	}
	challenge, attestation := make([]byte, 32), make([]byte, 16)
	tbs, err := jcm.ToBeSigned(info.AdminFabricIndex, challenge, attestation, rcac.PublicKey, sc.f.fabricID, sc.f.vendorID)
	if err != nil {
		t.Fatal(err)
	}
	tbs = append(tbs, info.AdminVIDVerificationStatement...)
	resp := opcreds.SignVidVerificationResponse{FabricIndex: info.AdminFabricIndex, FabricBindingVersion: jcm.FabricBindingVersion1, Signature: rawSign(t, sc.f.key, tbs)}
	return jcm.Verify(t.Context(), &info, challenge, attestation, resp, lookup)
}

func TestVendorIdVerificationParity(t *testing.T) {
	t.Parallel()
	t.Run("returns true for a conformant chain", func(t *testing.T) {
		t.Parallel()
		f := newJFFabric(t)
		if err := verify(t, f, f.info(), anchor(f.rcac, nil)); err != nil {
			t.Fatalf("Verify: %v", err)
		}
	})
	t.Run("returns false for a NOC that repeats a subject attribute", func(t *testing.T) {
		t.Parallel()
		f := newJFFabric(t)
		info := f.info()
		info.AdminNOC = redo(t, f.noc, repeated(17))
		if err := verify(t, f, info, anchor(f.rcac, nil)); err == nil {
			t.Fatal("a NOC with two matter-node-ids verified")
		}
	})
	t.Run("returns false for a NOC that lacks a required subject attribute", func(t *testing.T) {
		t.Parallel()
		f := newJFFabric(t)
		info := f.info()
		info.AdminNOC = redo(t, f.noc, func(c *mattercert.Certificate) {
			c.Subject.HasFabricID, c.Subject.MatterFabricID = false, 0
			c.Subject.Order = slices.DeleteFunc(c.Subject.Order, func(tag uint8) bool { return tag == 21 })
		})
		if err := verify(t, f, info, anchor(f.rcac, nil)); err == nil {
			t.Fatal("a NOC without a matter-fabric-id verified")
		}
	})
	t.Run("returns false for a VVSC that repeats a subject attribute", func(t *testing.T) {
		t.Parallel()
		sc := newStatementCase(t, 0x0001)
		info := sc.info
		info.AdminVVSC = redo(t, info.AdminVVSC, repeated(23))
		if err := sc.verifyWith(t, info, anchor(anchorCert(t, sc.vendorRoot), nil)); !errors.Is(err, jcm.ErrStatement) {
			t.Fatalf("Verify: %v, want ErrStatement", err)
		}
	})
	t.Run("returns false for a VVSC that fails its certificate profile", func(t *testing.T) {
		t.Parallel()
		sc := newStatementCase(t, 0x0001|0x0004) // digitalSignature and keyEncipherment
		if err := sc.verifyWith(t, sc.info, anchor(anchorCert(t, sc.vendorRoot), nil)); !errors.Is(err, jcm.ErrStatement) || !errors.Is(err, mattercert.ErrInvalidVVSC) {
			t.Fatalf("Verify: %v, want ErrStatement with ErrInvalidVVSC", err)
		}
	})
	t.Run("returns false for an RCAC that repeats a subject attribute", func(t *testing.T) {
		t.Parallel()
		f := newJFFabric(t)
		info := f.info()
		info.AdminRCAC = redo(t, f.rcac, repeated(20))
		if err := verify(t, f, info, anchor(f.rcac, nil)); err == nil {
			t.Fatal("an RCAC with two matter-rcac-ids verified")
		}
	})

	// The statement path beyond matter.js's cases, where it is still open
	// (VendorIdVerification.ts:232-240, :263-271).
	t.Run("verifies a statement signed by a VVSC of the vendor's trust anchor", func(t *testing.T) {
		t.Parallel()
		sc := newStatementCase(t, 0x0001)
		var asked []byte
		lookup := func(_ context.Context, vendor uint16, keyID []byte) ([]byte, error) {
			if vendor != sc.f.vendorID {
				t.Errorf("lookup for vendor %#x", vendor)
			}
			asked = keyID
			return anchorCert(t, sc.vendorRoot), nil
		}
		if err := sc.verifyWith(t, sc.info, lookup); err != nil {
			t.Fatalf("Verify: %v", err)
		}
		if root := sha1.Sum(pubOf(t, sc.vendorRoot)); !slices.Equal(asked, root[:]) { //nolint:gosec // key identifier
			t.Fatal("the trust anchor was not looked up by the VVSC's AuthorityKeyID")
		}
	})
	t.Run("refuses a VVSC no trust anchor of the vendor signed", func(t *testing.T) {
		t.Parallel()
		sc := newStatementCase(t, 0x0001)
		if err := sc.verifyWith(t, sc.info, anchor(anchorCert(t, newKey(t)), nil)); !errors.Is(err, jcm.ErrStatement) {
			t.Fatalf("Verify: %v, want ErrStatement", err)
		}
	})
	t.Run("refuses a statement whose signer is not the VVSC", func(t *testing.T) {
		t.Parallel()
		sc := newStatementCase(t, 0x0001)
		info := sc.info
		info.AdminVIDVerificationStatement = slices.Clone(info.AdminVIDVerificationStatement)
		info.AdminVIDVerificationStatement[1] ^= 0xFF
		if err := sc.verifyWith(t, info, anchor(anchorCert(t, sc.vendorRoot), nil)); !errors.Is(err, jcm.ErrStatement) {
			t.Fatalf("Verify: %v, want ErrStatement", err)
		}
	})
	t.Run("refuses a statement signature the VVSC did not make", func(t *testing.T) {
		t.Parallel()
		sc := newStatementCase(t, 0x0001)
		info := sc.info
		info.AdminVIDVerificationStatement = slices.Clone(info.AdminVIDVerificationStatement)
		info.AdminVIDVerificationStatement[40] ^= 0xFF
		if err := sc.verifyWith(t, info, anchor(anchorCert(t, sc.vendorRoot), nil)); !errors.Is(err, jcm.ErrSignature) {
			t.Fatalf("Verify: %v, want ErrSignature", err)
		}
	})
	t.Run("refuses a statement without a VVSC", func(t *testing.T) {
		t.Parallel()
		sc := newStatementCase(t, 0x0001)
		info := sc.info
		info.AdminVVSC = nil
		if err := sc.verifyWith(t, info, anchor(anchorCert(t, sc.vendorRoot), nil)); !errors.Is(err, jcm.ErrStatement) {
			t.Fatalf("Verify: %v, want ErrStatement", err)
		}
	})
}

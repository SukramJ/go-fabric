// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/secure/mattercert"
	"github.com/SukramJ/go-fabric/tlv"
)

// TestPin_ValidateRCAC_SubjectVsIssuer pins that
// AddTrustedRootCertificate runs the RCAC structural validation before
// stashing a trust root. A Root CA certificate must be self-signed per
// Matter §6.5 and chip src/credentials/CHIPCert.cpp ValidateChipRCAC; a
// root whose Subject RCAC-ID differs from its Issuer RCAC-ID is not a
// root at all, and installing it silently binds the fabric's whole CASE
// chain to an unvalidated trust anchor.
//
// The fixture pair differs in exactly one field — the Subject RCAC-ID —
// and is otherwise a valid, properly self-signed RCAC. Step 1 measures
// that difference directly against mattercert.ValidateRCAC, so the
// cluster-level assertions below cannot be satisfied by some unrelated
// defect in the fixture.
func TestPin_ValidateRCAC_SubjectVsIssuer(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	rootPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey root: %v", err)
	}

	const (
		issuerRCACID     uint64 = 0x0001
		foreignSubjectID uint64 = 0x0002
	)

	selfSigned := buildRootCertWithIDs(t, rootPriv, issuerRCACID, issuerRCACID)
	subjectMismatch := buildRootCertWithIDs(t, rootPriv, issuerRCACID, foreignSubjectID)

	// Step 1 — the fixtures differ in the Subject RCAC-ID and nothing else
	// that ValidateRCAC inspects. Both decode, and both are structurally
	// roots (Subject carries an RCAC-ID and no Node-ID), so the cluster
	// handler's IsRoot precondition cannot be what separates them.
	for _, f := range []struct {
		name string
		raw  []byte
	}{{"self-signed", selfSigned}, {"subject-mismatch", subjectMismatch}} {
		cert, derr := mattercert.Decode(f.raw)
		if derr != nil {
			t.Fatalf("Decode(%s fixture): %v", f.name, derr)
		}
		if !cert.IsRoot() {
			t.Fatalf("%s fixture: IsRoot() = false, want true", f.name)
		}
	}
	okCert, err := mattercert.Decode(selfSigned)
	if err != nil {
		t.Fatalf("Decode(self-signed): %v", err)
	}
	if verr := mattercert.ValidateRCAC(okCert); verr != nil {
		t.Fatalf("ValidateRCAC(self-signed fixture) = %v, want nil — fixture is not otherwise valid", verr)
	}
	badCert, err := mattercert.Decode(subjectMismatch)
	if err != nil {
		t.Fatalf("Decode(subject-mismatch): %v", err)
	}
	if verr := mattercert.ValidateRCAC(badCert); !errors.Is(verr, mattercert.ErrInvalidRCAC) {
		t.Fatalf("ValidateRCAC(subject-mismatch fixture) = %v, want ErrInvalidRCAC", verr)
	}

	// Step 2 — the handler must accept the self-signed root ...
	ocOK, err := core.NewOperationalCredentials(newFakeStore(), core.OpcredsConfig{SupportedFabrics: 5})
	if err != nil {
		t.Fatalf("NewOperationalCredentials: %v", err)
	}
	if _, ierr := ocOK.MatterInvoke(ctx, 0x0B,
		core.AddTrustedRootCertificateRequest{RootCACertificate: selfSigned}); ierr != nil {
		t.Fatalf("AddTrustedRootCertificate(self-signed): unexpected error: %v", ierr)
	}

	// ... and reject the one whose Subject does not match its Issuer,
	// surfacing mattercert.ErrInvalidRCAC so the rejection is provably the
	// RCAC validation and not some other guard on the path.
	ocBad, err := core.NewOperationalCredentials(newFakeStore(), core.OpcredsConfig{SupportedFabrics: 5})
	if err != nil {
		t.Fatalf("NewOperationalCredentials: %v", err)
	}
	_, ierr := ocBad.MatterInvoke(ctx, 0x0B,
		core.AddTrustedRootCertificateRequest{RootCACertificate: subjectMismatch})
	if ierr == nil {
		t.Fatal("AddTrustedRootCertificate(subject-mismatch): accepted a root that is not self-signed")
	}
	if !errors.Is(ierr, mattercert.ErrInvalidRCAC) {
		t.Errorf("AddTrustedRootCertificate(subject-mismatch) error = %v, want one wrapping mattercert.ErrInvalidRCAC", ierr)
	}
}

// buildRootCertWithIDs mints a signed Matter RCAC whose Issuer and Subject
// RCAC-IDs are chosen independently. The shared [buildCoreSignedCert]
// fixture hard-codes both to 0x0001, so it cannot express the
// Subject != Issuer case this pin needs.
//
// The certificate is signed by its own key over the DER-encoded TBS bytes,
// matching the verification path in mattercert — a fixture that merely
// carried a zero signature would be rejected for the wrong reason.
func buildRootCertWithIDs(t *testing.T, priv *ecdsa.PrivateKey, issuerRCACID, subjectRCACID uint64) []byte {
	t.Helper()

	pub := marshalTestPub(priv)

	// A probe carrying a zero signature is decodable, which is all
	// TBSToDER needs to derive the bytes that actually get signed.
	probeRaw := encodeRootCertTLV(t, pub, issuerRCACID, subjectRCACID, make([]byte, 64))
	probeCert, err := mattercert.Decode(probeRaw)
	if err != nil {
		t.Fatalf("buildRootCertWithIDs: decode probe: %v", err)
	}
	tbsDER, err := mattercert.TBSToDER(probeCert)
	if err != nil {
		t.Fatalf("buildRootCertWithIDs: TBSToDER: %v", err)
	}
	hash := sha256.Sum256(tbsDER)
	r, s, err := ecdsa.Sign(rand.Reader, priv, hash[:])
	if err != nil {
		t.Fatalf("buildRootCertWithIDs: sign: %v", err)
	}
	sig := make([]byte, 64)
	rb, sb := r.Bytes(), s.Bytes()
	copy(sig[32-len(rb):32], rb)
	copy(sig[64-len(sb):64], sb)

	return encodeRootCertTLV(t, pub, issuerRCACID, subjectRCACID, sig)
}

// encodeRootCertTLV writes the Matter Certificate TLV for a root CA.
// Extensions carry BasicConstraints (isCA, pathLen=1) and KeyUsage
// keyCertSign|cRLSign (0x60) so every ValidateRCAC check other than the
// Subject/Issuer comparison passes.
func encodeRootCertTLV(t *testing.T, pub []byte, issuerRCACID, subjectRCACID uint64, sig []byte) []byte {
	t.Helper()

	e := tlv.NewEncoder()
	e.StartStruct(tlv.AnonymousTag())
	e.PutOctets(tlv.ContextTag(1), []byte{0x01}) // serial number
	e.PutUint(tlv.ContextTag(2), testSigAlgoECDSA)

	e.StartList(tlv.ContextTag(3)) // Issuer DN
	e.PutUint(tlv.ContextTag(20), issuerRCACID)
	if err := e.EndContainer(); err != nil {
		t.Fatalf("encodeRootCertTLV: EndContainer issuer: %v", err)
	}

	e.PutUint(tlv.ContextTag(4), uint64(1000)) // NotBefore
	e.PutUint(tlv.ContextTag(5), uint64(0))    // NotAfter: never expires

	e.StartList(tlv.ContextTag(6)) // Subject DN
	e.PutUint(tlv.ContextTag(20), subjectRCACID)
	if err := e.EndContainer(); err != nil {
		t.Fatalf("encodeRootCertTLV: EndContainer subject: %v", err)
	}

	e.PutUint(tlv.ContextTag(7), testPubAlgoEC)
	e.PutUint(tlv.ContextTag(8), testCurvePrime256v1)
	e.PutOctets(tlv.ContextTag(9), pub)

	e.StartList(tlv.ContextTag(10)) // Extensions
	e.StartStruct(tlv.ContextTag(1))
	e.PutBool(tlv.ContextTag(1), true) // isCA
	e.PutUint(tlv.ContextTag(2), 1)    // pathLenConstraint
	if err := e.EndContainer(); err != nil {
		t.Fatalf("encodeRootCertTLV: EndContainer basicConstraints: %v", err)
	}
	e.PutUint(tlv.ContextTag(2), uint64(0x60)) // KeyUsage: keyCertSign|cRLSign
	if err := e.EndContainer(); err != nil {
		t.Fatalf("encodeRootCertTLV: EndContainer extensions: %v", err)
	}

	e.PutOctets(tlv.ContextTag(11), sig)

	if err := e.EndContainer(); err != nil {
		t.Fatalf("encodeRootCertTLV: EndContainer top: %v", err)
	}
	raw, err := e.Bytes()
	if err != nil {
		t.Fatalf("encodeRootCertTLV: Bytes: %v", err)
	}
	return raw
}

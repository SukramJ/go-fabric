// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Vendor ID Verification: a port of connectedhomeip
// src/credentials/tests/jcm/TestVendorIdVerificationClient.cpp at 6170af84,
// by meaning. chip's TestVendorIDVerificationDataModel (:53-121) signs on
// the JF-B fabric of the messaging context; here jfFabric is a 3-tier
// fabric (RCAC, ICAC, NOC) of controller/ca and signer answers
// SignVIDVerificationRequest with that fabric's operational key over the
// session's attestation challenge. Of chip's 13 cases the 9 TEST_F are
// ported; the 4 VendorIdVerificationClientLiveness TESTs (:405-461) pin
// GuardWithLiveness, a C++ lifetime guard with no Go counterpart — a
// verification here runs under its owner's context — and
// TestVerifyVendorIDHonoursContext stands in for them.
package jcm_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/client/jcm"
	opcreds "github.com/SukramJ/go-fabric/cluster/spec/operationalcredentials"
	"github.com/SukramJ/go-fabric/controller/ca"
	"github.com/SukramJ/go-fabric/secure/mattercert"
)

// jfFabric is a fabric whose administrator holds a NOC under an ICAC.
type jfFabric struct {
	fabricIndex uint8
	fabricID    uint64
	vendorID    uint16
	rcac, icac  []byte
	noc         []byte
	key         *ecdsa.PrivateKey
}

const adminCAT uint32 = 0xFFFF_0001

func newJFFabric(t *testing.T, cats ...uint32) *jfFabric {
	t.Helper()
	c, err := ca.New(ca.Config{IntermediateCA: true})
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := key.PublicKey.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	f := &jfFabric{fabricIndex: 2, fabricID: 0x0B0B, vendorID: 0xFFF1, rcac: c.RootCert(), icac: c.ICACCert(), key: key}
	if f.noc, err = c.GenerateNOC(pub, f.fabricID, 0x1234, cats); err != nil {
		t.Fatal(err)
	}
	return f
}

// info is the verification information of the fabric (:209-215).
func (f *jfFabric) info() jcm.Info {
	return jcm.Info{
		AdminEndpointID: 1, AdminFabricIndex: f.fabricIndex, AdminVendorID: f.vendorID, AdminFabricID: f.fabricID,
		AdminRCAC: append([]byte(nil), f.rcac...), AdminICAC: append([]byte(nil), f.icac...), AdminNOC: append([]byte(nil), f.noc...),
	}
}

// signer is the peer's SignVIDVerificationRequest (:67-117): an unknown
// fabric index fails, as FabricTable::SignVIDVerificationRequest does.
type signer struct {
	f           *jfFabric
	attestation []byte
	calls       int
}

func newSigner(f *jfFabric) *signer {
	att := make([]byte, 16)
	_, _ = rand.Read(att)
	return &signer{f: f, attestation: att}
}

var errBadFabricForIndex = errors.New("signer: no fabric for the index")

func (s *signer) SignVIDVerification(ctx context.Context, fabricIndex uint8, clientChallenge []byte) (opcreds.SignVidVerificationResponse, []byte, error) {
	s.calls++
	if err := ctx.Err(); err != nil {
		return opcreds.SignVidVerificationResponse{}, nil, err
	}
	if fabricIndex != s.f.fabricIndex {
		return opcreds.SignVidVerificationResponse{}, nil, errBadFabricForIndex
	}
	rcac, err := mattercert.Decode(s.f.rcac)
	if err != nil {
		return opcreds.SignVidVerificationResponse{}, nil, err
	}
	tbs, err := jcm.ToBeSigned(fabricIndex, clientChallenge, s.attestation, rcac.PublicKey, s.f.fabricID, s.f.vendorID)
	if err != nil {
		return opcreds.SignVidVerificationResponse{}, nil, err
	}
	digest := sha256.Sum256(tbs)
	r, ss, err := ecdsa.Sign(rand.Reader, s.f.key, digest[:])
	if err != nil {
		return opcreds.SignVidVerificationResponse{}, nil, err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	ss.FillBytes(sig[32:])
	return opcreds.SignVidVerificationResponse{FabricIndex: fabricIndex, FabricBindingVersion: jcm.FabricBindingVersion1, Signature: sig}, s.attestation, nil
}

// anchor is the test's OnLookupOperationalTrustAnchor (:165-182): the
// fabric's root, an invalid one, or none.
func anchor(root []byte, err error) jcm.TrustAnchorLookup {
	return func(context.Context, uint16, []byte) ([]byte, error) { return root, err }
}

func verify(t *testing.T, f *jfFabric, info jcm.Info, lookup jcm.TrustAnchorLookup) error {
	t.Helper()
	return jcm.VerifyVendorID(t.Context(), newSigner(f), &info, lookup, nil)
}

// TestVerificationSucceeds is :247-259.
func TestVerificationSucceeds(t *testing.T) {
	f := newJFFabric(t, adminCAT)
	if err := verify(t, f, f.info(), anchor(f.rcac, nil)); err != nil {
		t.Fatalf("VerifyVendorID: %v", err)
	}
}

// TestVerificationFailsDueToBadRCAC is :261-275.
func TestVerificationFailsDueToBadRCAC(t *testing.T) {
	f := newJFFabric(t)
	info := f.info()
	info.AdminRCAC = []byte("badrcac")
	if err := verify(t, f, info, anchor(f.rcac, nil)); err == nil {
		t.Fatal("a malformed RCAC verified")
	}
}

// TestVerificationFailsDueToBadICAC is :277-291.
func TestVerificationFailsDueToBadICAC(t *testing.T) {
	f := newJFFabric(t)
	info := f.info()
	info.AdminICAC = []byte("badicac")
	if err := verify(t, f, info, anchor(f.rcac, nil)); err == nil {
		t.Fatal("a malformed ICAC verified")
	}
}

// TestVerificationFailsDueToBadNOC is :293-307.
func TestVerificationFailsDueToBadNOC(t *testing.T) {
	f := newJFFabric(t)
	info := f.info()
	info.AdminNOC = []byte("badnoc")
	if err := verify(t, f, info, anchor(f.rcac, nil)); err == nil {
		t.Fatal("a malformed NOC verified")
	}
}

// TestVerificationFailsDueToBadFabricIndex is :309-323: the peer has no
// fabric of that index and refuses to sign.
func TestVerificationFailsDueToBadFabricIndex(t *testing.T) {
	f := newJFFabric(t)
	info := f.info()
	info.AdminFabricIndex = 57 // FabricIndex(12345) truncated, as chip's
	if err := verify(t, f, info, anchor(f.rcac, nil)); !errors.Is(err, errBadFabricForIndex) {
		t.Fatalf("VerifyVendorID = %v, want the signer's refusal", err)
	}
}

// TestVerificationFailsDueToBadFabricId is :325-339: the binding message
// names another fabric id, the signature does not verify.
func TestVerificationFailsDueToBadFabricId(t *testing.T) {
	f := newJFFabric(t)
	info := f.info()
	info.AdminFabricID = 12345
	if err := verify(t, f, info, anchor(f.rcac, nil)); !errors.Is(err, jcm.ErrSignature) {
		t.Fatalf("VerifyVendorID = %v, want ErrSignature", err)
	}
}

// TestVerificationFailsDueToBadVendorId is :341-355.
func TestVerificationFailsDueToBadVendorId(t *testing.T) {
	f := newJFFabric(t)
	info := f.info()
	info.AdminVendorID = 12345
	if err := verify(t, f, info, anchor(f.rcac, nil)); !errors.Is(err, jcm.ErrSignature) {
		t.Fatalf("VerifyVendorID = %v, want ErrSignature", err)
	}
}

// TestVerificationFailsDueToNoOperationalTrustAnchor is :357-371.
func TestVerificationFailsDueToNoOperationalTrustAnchor(t *testing.T) {
	f := newJFFabric(t)
	if err := verify(t, f, f.info(), anchor(nil, jcm.ErrNoTrustAnchor)); !errors.Is(err, jcm.ErrNoTrustAnchor) {
		t.Fatalf("VerifyVendorID = %v, want ErrNoTrustAnchor", err)
	}
}

// TestVerificationFailsDueToInvalidOperationalTrustAnchor is :373-387.
func TestVerificationFailsDueToInvalidOperationalTrustAnchor(t *testing.T) {
	f := newJFFabric(t)
	if err := verify(t, f, f.info(), anchor([]byte("root"), nil)); err == nil {
		t.Fatal("an invalid trust anchor verified")
	}
}

// TestVerificationFailsAgainstAnotherRoot: a trust anchor of another
// fabric does not anchor the chain (step 12bii, :116-120).
func TestVerificationFailsAgainstAnotherRoot(t *testing.T) {
	f, other := newJFFabric(t), newJFFabric(t)
	if err := verify(t, f, f.info(), anchor(other.rcac, nil)); err == nil {
		t.Fatal("another fabric's root anchored the chain")
	}
}

// TestVerifyVendorIDHonoursContext stands in for the liveness cases
// (:405-461): a verification whose context ended does not complete.
func TestVerifyVendorIDHonoursContext(t *testing.T) {
	f := newJFFabric(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	info := f.info()
	if err := jcm.VerifyVendorID(ctx, newSigner(f), &info, anchor(f.rcac, nil), nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("VerifyVendorID = %v, want context.Canceled", err)
	}
	if err := jcm.VerifyVendorID(t.Context(), nil, &info, anchor(f.rcac, nil), nil); !errors.Is(err, jcm.ErrNoSession) {
		t.Fatalf("VerifyVendorID without a signer = %v, want ErrNoSession", err)
	}
}

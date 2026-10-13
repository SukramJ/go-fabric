// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package attestation_test

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller/attestation"
	secattestation "github.com/SukramJ/go-fabric/secure/attestation"
)

func checks(fs []attestation.Finding) []string {
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = string(f.Level) + ":" + string(f.Check)
	}
	return out
}

// TestReferenceNodeChain: the chain and CD the reference node presents
// (secure/attestation.BuildTestChain, BuildTestCertificationDeclaration)
// validate against TestStore with the one finding DAV produces for them:
// a test CD (DAV:413-418).
func TestReferenceNodeChain(t *testing.T) {
	t.Parallel()
	chain, err := secattestation.BuildTestChain(0xFFF1, 0x8000)
	if err != nil {
		t.Fatal(err)
	}
	cd, err := secattestation.BuildTestCertificationDeclaration(0xFFF1, 0x8000)
	if err != nil {
		t.Fatal(err)
	}
	challenge, nonce := make([]byte, 16), make([]byte, 32)
	_, _ = rand.Read(challenge)
	_, _ = rand.Read(nonce)
	el := elements(t, cd, nonce)
	res, err := attestation.Validate(
		attestation.Context{Challenge: challenge, Store: attestation.TestStore()},
		attestation.Data{DAC: chain.DAC, PAI: chain.PAI, Elements: el, Signature: sign(t, chain.DACKey, el, challenge), Nonce: nonce, VendorID: 0xFFF1, ProductID: 0x8000},
	)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if got, want := checks(res.Findings), []string{"info:CertificationTypeTest"}; !slices.Equal(got, want) {
		t.Fatalf("findings %v, want %v", got, want)
	}
	if !res.DACPublicKey.Equal(&chain.DACKey.PublicKey) {
		t.Fatal("DAC public key differs")
	}
}

// TestNoStore: without a store only the local checks run and the two
// warnings of DAV:224-231 and :394-399 precede the CD content's finding.
func TestNoStore(t *testing.T) {
	t.Parallel()
	f := newFixture(t, defaultSpec(), defaultCD())
	f.ctx.Store = nil
	res, err := attestation.Validate(f.ctx, f.data)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"warning:DclServiceUnavailable", "warning:CdSignerVerificationSkipped", "info:CertificationTypeTest"}
	if got := checks(res.Findings); !slices.Equal(got, want) {
		t.Fatalf("findings %v, want %v", got, want)
	}
	// A chain the store would refuse passes without one.
	s := defaultSpec()
	s.paiAKID = make([]byte, 20)
	f = newFixture(t, s, defaultCD())
	f.ctx.Store = nil
	if _, err := attestation.Validate(f.ctx, f.data); err != nil {
		t.Fatalf("untrusted PAA without a store: %v", err)
	}
	// authorized_paa_list cannot be checked without the PAA (DAV:519-525).
	cd := defaultCD()
	cd.authorizedPAAs = [][]byte{make([]byte, 20)}
	f = newFixture(t, defaultSpec(), cd)
	f.ctx.Store = nil
	res, err = attestation.Validate(f.ctx, f.data)
	if err != nil {
		t.Fatal(err)
	}
	if got := checks(res.Findings); got[len(got)-1] != "warning:DclServiceUnavailable" {
		t.Fatalf("findings %v", got)
	}
}

// TestFindings: the non-fatal findings of the store branch and the CD.
func TestFindings(t *testing.T) {
	t.Parallel()
	paa := secattestation.TestPAAFFF1Cert
	cases := []struct {
		name  string
		store func() *attestation.StaticStore
		cd    func(*cdSpec)
		want  []string
	}{
		{"missing CD signer", func() *attestation.StaticStore {
			s := attestation.TestStore()
			s.CDSigners = nil
			return s
		}, nil, []string{"warning:CdSignerVerificationSkipped", "info:CertificationTypeTest"}},
		{"test root under production policy", func() *attestation.StaticStore {
			s := attestation.TestStore()
			s.AcceptTest = false
			return s
		}, nil, []string{"error:TrustedAsTestCertificate", "info:CertificationTypeTest"}},
		{"production root under production policy", func() *attestation.StaticStore {
			s := attestation.TestStore()
			s.AcceptTest = false
			s.PAAs = []attestation.PAA{{DER: paa, Production: true}}
			return s
		}, nil, []string{"info:CertificationTypeTest"}},
		{"root fetched after the DAC's notBefore", func() *attestation.StaticStore {
			s := attestation.TestStore()
			s.PAAs = []attestation.PAA{{DER: paa, FetchedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}}
			return s
		}, nil, []string{"info:PaaTrustStoreTimeMismatch", "info:CertificationTypeTest"}},
		{"root fetched before", func() *attestation.StaticStore {
			s := attestation.TestStore()
			s.PAAs = []attestation.PAA{{DER: paa, FetchedAt: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)}}
			return s
		}, nil, []string{"info:CertificationTypeTest"}},
		{"provisional CD", attestation.TestStore, func(c *cdSpec) { c.certType = 1 }, []string{"warning:CertificationTypeProvisional"}},
		{"official CD", attestation.TestStore, func(c *cdSpec) { c.certType = 2 }, []string{}},
		{"authorized PAA listed", attestation.TestStore, func(c *cdSpec) {
			c.authorizedPAAs = [][]byte{make([]byte, 20), secattestation.TestPAAFFF1SKID}
		}, []string{"info:CertificationTypeTest"}},
		{"dac origin matches", attestation.TestStore, func(c *cdSpec) {
			v, p := uint16(0xFFF1), uint16(0x8000)
			c.vid, c.originVID, c.originPID = 0xFFF1, &v, &p
		}, []string{"info:CertificationTypeTest"}},
	}
	for _, c := range cases {
		cd := defaultCD()
		if c.cd != nil {
			c.cd(&cd)
		}
		f := newFixture(t, defaultSpec(), cd)
		f.ctx.Store = c.store()
		res, err := attestation.Validate(f.ctx, f.data)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if got := checks(res.Findings); !slices.Equal(got, c.want) {
			t.Errorf("%s: findings %v, want %v", c.name, got, c.want)
		}
	}
}

// revokingStore revokes one certificate serial.
type revokingStore struct {
	*attestation.StaticStore
	serial []byte
	err    error
}

func (r revokingStore) IsRevoked(_, serial []byte, issuerDN string) (bool, error) {
	if issuerDN == "" {
		return false, errors.New("no issuer DN")
	}
	return bytes.Equal(serial, r.serial), r.err
}

// TestHardFailures: every hard failure of DAV on a tampered input.
func TestHardFailures(t *testing.T) {
	t.Parallel()
	otherKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	cases := []struct {
		name  string
		spec  func(*chainSpec)
		cd    func(*cdSpec)
		tweak func(*fixture)
		want  attestation.Check
	}{
		{"empty DAC", nil, nil, func(f *fixture) { f.data.DAC = nil }, attestation.CertificateUnparseable},
		{"undecodable PAI", nil, nil, func(f *fixture) { f.data.PAI = []byte{0x30, 0x03, 1, 2, 3} }, attestation.CertificateUnparseable},
		{"oversized DAC", nil, nil, func(f *fixture) { f.data.DAC = make([]byte, 601) }, attestation.CertificateUnparseable},
		{"vendor id mismatch", func(s *chainSpec) { s.dacVID = 0xFFF2 }, nil, nil, attestation.VendorIDMismatch},
		{"undecodable elements", nil, nil, func(f *fixture) { f.data.Elements = []byte{0x15} }, attestation.AttestationElementsUnparseable},
		{"wrong nonce", nil, nil, func(f *fixture) { f.data.Nonce = make([]byte, 32) }, attestation.AttestationNonceMismatch},
		{"wrong signature", nil, nil, func(f *fixture) { f.data.Signature[5] ^= 1 }, attestation.AttestationSignatureInvalid},
		{"short signature", nil, nil, func(f *fixture) { f.data.Signature = f.data.Signature[:63] }, attestation.AttestationSignatureInvalid},
		{"PAI from an unknown PAA", func(s *chainSpec) { s.paiAKID = make([]byte, 20) }, nil, nil, attestation.PaaNotTrusted},
		{"PAI not signed by the PAA", func(s *chainSpec) { s.paiSigner = otherKey }, nil, nil, attestation.CertificateChainInvalid},
		{"DAC not signed by the PAI", func(s *chainSpec) { s.dacSigner = otherKey }, nil, nil, attestation.CertificateChainInvalid},
		{"DAC notBefore before the PAI's", func(s *chainSpec) { s.dacNotBefore = s.paiNotBefore.Add(-time.Hour) }, nil, nil, attestation.CertificateExpired},
		{"DAC notBefore before the PAA's", func(s *chainSpec) {
			s.paiNotBefore = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
			s.dacNotBefore = time.Date(2020, 6, 1, 0, 0, 0, 0, time.UTC)
		}, nil, nil, attestation.CertificateExpired},
		{"PAA vendor id mismatch", func(s *chainSpec) { s.paiVID, s.dacVID = 0xFFF2, 0xFFF2 }, nil, nil, attestation.VendorIDMismatch},
		{"undecodable CD", nil, func(c *cdSpec) { c.corruptCMS = true }, nil, attestation.CertificationDeclarationUnparseable},
		{"undecodable CD content", nil, func(c *cdSpec) { c.corruptTLV = true }, nil, attestation.CertificationDeclarationUnparseable},
		{"CD signature invalid", nil, func(c *cdSpec) { c.signer = otherKey }, nil, attestation.CertificationDeclarationSignatureInvalid},
		{"CD vendor id mismatch", nil, nil, func(f *fixture) { f.data.VendorID = 0xFFF2 }, attestation.CertificationDeclarationFieldMismatch},
		{"product id not listed", nil, nil, func(f *fixture) { f.data.ProductID = 0x8001 }, attestation.CertificationDeclarationFieldMismatch},
		{"only a dac origin vendor id", nil, func(c *cdSpec) { v := uint16(0xFFF1); c.originVID = &v }, nil, attestation.CertificationDeclarationFieldMismatch},
		{"dac origin vendor mismatch", nil, func(c *cdSpec) {
			v, p := uint16(0xFFF3), uint16(0x8000)
			c.originVID, c.originPID = &v, &p
		}, nil, attestation.CertificationDeclarationFieldMismatch},
		{"dac origin product mismatch", nil, func(c *cdSpec) {
			v, p := uint16(0xFFF1), uint16(0x8001)
			c.originVID, c.originPID = &v, &p
		}, nil, attestation.CertificationDeclarationFieldMismatch},
		{"DAC product not in the CD", func(s *chainSpec) { s.dacPID = 0x8002 }, nil, nil, attestation.CertificationDeclarationFieldMismatch},
		{"PAA not authorized", nil, func(c *cdSpec) { c.authorizedPAAs = [][]byte{make([]byte, 20)} }, nil, attestation.CertificationDeclarationFieldMismatch},
		{"DAC revoked", nil, nil, func(f *fixture) {
			f.ctx.Store = revokingStore{StaticStore: attestation.TestStore(), serial: []byte{0x56, 0x78}}
		}, attestation.CertificateRevoked},
		{"PAI revoked", nil, nil, func(f *fixture) {
			f.ctx.Store = revokingStore{StaticStore: attestation.TestStore(), serial: []byte{0x12, 0x34}}
		}, attestation.CertificateRevoked},
	}
	for _, c := range cases {
		s, cd := defaultSpec(), defaultCD()
		if c.spec != nil {
			c.spec(&s)
		}
		if c.cd != nil {
			c.cd(&cd)
		}
		f := newFixture(t, s, cd)
		if c.tweak != nil {
			c.tweak(&f)
		}
		_, err := attestation.Validate(f.ctx, f.data)
		var ae *attestation.Error
		if !errors.As(err, &ae) || ae.Check != c.want {
			t.Errorf("%s: %v, want %s", c.name, err, c.want)
		}
	}
}

// TestStoreErrors: a trust-store fault is a plain error, not the peer's
// failure (DAV:270, the revocation lookup).
func TestStoreErrors(t *testing.T) {
	t.Parallel()
	f := newFixture(t, defaultSpec(), defaultCD())
	f.ctx.Store = revokingStore{StaticStore: attestation.TestStore(), err: errors.New("lookup failed")}
	_, err := attestation.Validate(f.ctx, f.data)
	var ae *attestation.Error
	if err == nil || errors.As(err, &ae) {
		t.Fatalf("revocation lookup failure: %v", err)
	}
	f = newFixture(t, defaultSpec(), defaultCD())
	bad := &attestation.StaticStore{PAAs: []attestation.PAA{{DER: secattestation.TestPAAFFF1Cert}}, AcceptTest: true}
	f.ctx.Store = &corruptPAA{bad}
	if _, err := attestation.Validate(f.ctx, f.data); err == nil || errors.As(err, &ae) {
		t.Fatalf("corrupt PAA: %v", err)
	}
}

// corruptPAA returns a PAA whose DER does not parse.
type corruptPAA struct{ *attestation.StaticStore }

func (c *corruptPAA) PAA(skid []byte) (attestation.PAA, bool) {
	p, ok := c.StaticStore.PAA(skid)
	p.DER = p.DER[:20]
	return p, ok
}

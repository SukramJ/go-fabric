// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package ca_test

// A port of matter.js packages/protocol/test/certificate/CertificateAuthorityTest.ts at
// 862ad75f: the 2- and 3-tier CA, its stored configuration, certificate ids, SKIs and CATs.
//
// matter.js's CertificateAuthority takes a StorageContext, a Configuration
// object or nothing; the Go CA takes a ca.Store, so "storage" and "an
// exported config" are both a ca.MemoryStore here, and ca.Stored is the
// Configuration. generateIntermediateCert left undefined is
// Config.DetectIntermediateCA. The tests the register already names
// (TestRootAndNOCRoundTrip, TestICACChain, TestStoreSaveAndLoad) cover the
// remaining it cases.

import (
	"bytes"
	"crypto/sha1" //nolint:gosec // RFC 5280 method-1 key identifiers are SHA-1
	"errors"
	"math/big"
	"slices"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/controller/ca"
	"github.com/SukramJ/go-fabric/secure/mattercert"
)

func newCA(t *testing.T, cfg ca.Config) *ca.CA {
	t.Helper()
	if cfg.Clock == nil {
		cfg.Clock = clocktest.New(issuedAt)
	}
	c, err := ca.New(cfg)
	if err != nil {
		t.Fatalf("ca.New: %v", err)
	}
	return c
}

// copyStore is a new MemoryStore holding what st holds — the exported
// configuration handed to a second CA.
func copyStore(t *testing.T, st *ca.MemoryStore) *ca.MemoryStore {
	t.Helper()
	s, ok, err := st.Load()
	if err != nil || !ok {
		t.Fatalf("Load: %v (%v)", err, ok)
	}
	out := &ca.MemoryStore{}
	if err := out.Save(s); err != nil {
		t.Fatal(err)
	}
	return out
}

func serial(t *testing.T, raw []byte) *big.Int {
	t.Helper()
	return new(big.Int).SetBytes(decode(t, raw).SerialNumber)
}

func sha1Of(b []byte) []byte {
	sum := sha1.Sum(b) //nolint:gosec // see the import
	return sum[:]
}

// TestCertificateAuthority2Tier: describe "2-tier PKI (default)" (:17).
func TestCertificateAuthority2Tier(t *testing.T) {
	t.Parallel()
	t.Run("refuses a NOC whose node ID is outside the operational range", func(t *testing.T) {
		t.Parallel()
		c := newCA(t, ca.Config{})
		noc, err := c.GenerateNOC(newKey(t), 1, 0xffff_ffff_0000_0001, nil) // :45
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.Verifier().VerifyAndExtractPubKey(noc, nil); !errors.Is(err, mattercert.ErrNonOperationalNodeID) {
			t.Fatalf("verify = %v, want ErrNonOperationalNodeID (OperationalNodeIdError)", err)
		}
	})
	t.Run("verifies a NOC whose node ID is in the operational range", func(t *testing.T) {
		t.Parallel()
		c := newCA(t, ca.Config{})
		noc, err := c.GenerateNOC(newKey(t), 1, 100, nil) // :55
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.Verifier().VerifyAndExtractPubKey(noc, nil); err != nil {
			t.Fatalf("verify: %v", err)
		}
	})
}

// TestCertificateAuthority3Tier: describe "3-tier PKI with ICAC" (:79).
func TestCertificateAuthority3Tier(t *testing.T) {
	t.Parallel()
	t.Run("persists ICAC to storage", func(t *testing.T) {
		t.Parallel()
		st := &ca.MemoryStore{}
		ca1 := newCA(t, ca.Config{Store: st, IntermediateCA: true})
		ca2 := newCA(t, ca.Config{Store: copyStore(t, st), DetectIntermediateCA: true})
		if ca1.ICACCert() == nil || ca2.ICACCert() == nil || !bytes.Equal(ca1.ICACCert(), ca2.ICACCert()) {
			t.Fatal("the ICAC did not survive the stored configuration")
		}
	})
	t.Run("auto-detects ICAC from storage when ica option not provided", func(t *testing.T) {
		t.Parallel()
		st := &ca.MemoryStore{}
		if newCA(t, ca.Config{Store: st, IntermediateCA: true}).ICACCert() == nil {
			t.Fatal("no ICAC")
		}
		if newCA(t, ca.Config{Store: st, DetectIntermediateCA: true}).ICACCert() == nil {
			t.Fatal("the stored ICAC was not detected")
		}
	})
	t.Run("loads ICAC from storage and sets intermediateCert=true (storage takes precedence)", func(t *testing.T) {
		t.Parallel()
		st := &ca.MemoryStore{}
		if newCA(t, ca.Config{Store: st, IntermediateCA: true}).ICACCert() == nil {
			t.Fatal("no ICAC")
		}
		ca2 := newCA(t, ca.Config{Store: st, DetectIntermediateCA: true})
		if ca2.ICACCert() == nil {
			t.Fatal("the stored ICAC was not loaded")
		}
		// The loaded CA signs with the ICAC: the 3-tier setting took hold.
		noc, err := ca2.GenerateNOC(newKey(t), 1, 100, nil)
		if err != nil {
			t.Fatal(err)
		}
		if decode(t, noc).Issuer.MatterICACID != 1 {
			t.Fatal("the NOC is not issued by the ICAC")
		}
	})
}

// TestCertificateAuthorityConfiguration: describe "Configuration behavior" (:166).
// The message is matter.js's ImplementationError text (:185, :194),
// compared case-insensitively (Go error strings start lower-case).
func TestCertificateAuthorityConfiguration(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name          string
		first, second bool
		want          string
	}{
		{
			"throws ImplementationError when intermediateCert=true but no ICAC data in storage", false, true,
			"Stored credentials contain ICAC certificate: false, but configuration expected it to be true",
		},
		{
			"throws ImplementationError when ICAC data exists but intermediateCert=false", true, false,
			"Stored credentials contain ICAC certificate: true, but configuration expected it to be false",
		},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			st := &ca.MemoryStore{}
			newCA(t, ca.Config{Store: st, IntermediateCA: r.first})
			_, err := ca.New(ca.Config{Store: st, IntermediateCA: r.second, Clock: clocktest.New(issuedAt)})
			if !errors.Is(err, ca.ErrConfig) || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(r.want)) {
				t.Fatalf("err = %v, want ErrConfig %q", err, r.want)
			}
		})
	}
}

// TestCertificateAuthorityCertificateIDs: describe "Certificate ID management" (:199).
func TestCertificateAuthorityCertificateIDs(t *testing.T) {
	t.Parallel()
	t.Run("increments certificate IDs correctly", func(t *testing.T) {
		t.Parallel()
		c := newCA(t, ca.Config{IntermediateCA: true})
		noc1, err := c.GenerateNOC(newKey(t), 1, 100, nil)
		if err != nil {
			t.Fatal(err)
		}
		noc2, err := c.GenerateNOC(newKey(t), 1, 101, nil)
		if err != nil {
			t.Fatal(err)
		}
		id1, id2 := serial(t, noc1), serial(t, noc2)
		if want := new(big.Int).Add(id1, big.NewInt(1)); id2.Cmp(want) != 0 {
			t.Fatalf("serials %v, %v: want consecutive", id1, id2)
		}
	})
	t.Run("allocates ICAC cert ID before NOC IDs", func(t *testing.T) {
		t.Parallel()
		c := newCA(t, ca.Config{IntermediateCA: true})
		noc, err := c.GenerateNOC(newKey(t), 1, 100, nil)
		if err != nil {
			t.Fatal(err)
		}
		if icacID, nocID := serial(t, c.ICACCert()), serial(t, noc); icacID.Int64() != 1 || nocID.Int64() != 2 { // :227-228
			t.Fatalf("ICAC id %v, NOC id %v; want 1, 2", icacID, nocID)
		}
	})
}

// TestCertificateAuthorityConfigurationExport: describe "Configuration export" (:232).
func TestCertificateAuthorityConfigurationExport(t *testing.T) {
	t.Parallel()
	t.Run("exports complete configuration with ICAC", func(t *testing.T) {
		t.Parallel()
		st := &ca.MemoryStore{}
		newCA(t, ca.Config{Store: st, IntermediateCA: true})
		cfg, _, _ := st.Load()
		switch {
		case cfg.RootCertID != 0 || cfg.ICAC == nil || cfg.ICAC.CertID != 1: // :237-238
			t.Fatalf("root id %d, ICAC %+v", cfg.RootCertID, cfg.ICAC)
		case len(cfg.ICAC.KeyPair.PrivateKey) == 0 || len(cfg.ICAC.KeyPair.PublicKey) == 0:
			t.Fatal("no icacKeyPair")
		case len(cfg.ICAC.KeyIdentifier) == 0:
			t.Fatal("no icacKeyIdentifier")
		case len(cfg.ICAC.CertBytes) == 0:
			t.Fatal("no icacCertBytes")
		}
	})
	t.Run("can reconstruct CA from exported config", func(t *testing.T) {
		t.Parallel()
		st := &ca.MemoryStore{}
		ca1 := newCA(t, ca.Config{Store: st, IntermediateCA: true})
		ca2 := newCA(t, ca.Config{Store: copyStore(t, st), DetectIntermediateCA: true})
		if !bytes.Equal(ca1.RootCert(), ca2.RootCert()) || !bytes.Equal(ca1.ICACCert(), ca2.ICACCert()) {
			t.Fatal("the reconstructed CA differs")
		}
	})
}

// TestCertificateAuthoritySKI: describe "SKI derivation" (:259).
func TestCertificateAuthoritySKI(t *testing.T) {
	t.Parallel()
	t.Run("derives root SKI as 160-bit SHA-1 of the public key (RFC 5280 method-1)", func(t *testing.T) {
		t.Parallel()
		rcac := decode(t, newCA(t, ca.Config{}).RootCert())
		ski := rcac.Extensions.SubjectKeyID
		if len(ski) != 20 || !bytes.Equal(ski, sha1Of(rcac.PublicKey)) || !bytes.Equal(rcac.Extensions.AuthorityKeyID, ski) {
			t.Fatalf("root SKI %x AKI %x", ski, rcac.Extensions.AuthorityKeyID)
		}
	})
	t.Run("derives ICAC SKI as 160-bit SHA-1 of the public key", func(t *testing.T) {
		t.Parallel()
		icac := decode(t, newCA(t, ca.Config{IntermediateCA: true}).ICACCert())
		if ski := icac.Extensions.SubjectKeyID; len(ski) != 20 || !bytes.Equal(ski, sha1Of(icac.PublicKey)) {
			t.Fatalf("ICAC SKI %x", ski)
		}
	})
	t.Run("derives NOC SKI as 160-bit SHA-1 of the public key", func(t *testing.T) {
		t.Parallel()
		c := newCA(t, ca.Config{})
		raw, err := c.GenerateNOC(newKey(t), 1, 100, nil)
		if err != nil {
			t.Fatal(err)
		}
		noc, rcac := decode(t, raw), decode(t, c.RootCert())
		ski := noc.Extensions.SubjectKeyID
		if len(ski) != 20 || !bytes.Equal(ski, sha1Of(noc.PublicKey)) {
			t.Fatalf("NOC SKI %x", ski)
		}
		if !bytes.Equal(noc.Extensions.AuthorityKeyID, rcac.Extensions.SubjectKeyID) {
			t.Fatal("NOC AKI is not the root SKI")
		}
	})
}

// TestCertificateAuthorityCATs: describe "NOC generation with CATs" (:300).
func TestCertificateAuthorityCATs(t *testing.T) {
	t.Parallel()
	t.Run("generates NOC with CASE Authenticated Tags", func(t *testing.T) {
		t.Parallel()
		c := newCA(t, ca.Config{IntermediateCA: true})
		cats := []uint32{0x00010001, 0x00020001} // :304
		raw, err := c.GenerateNOC(newKey(t), 1, 100, cats)
		if err != nil {
			t.Fatal(err)
		}
		if got := decode(t, raw).Subject.CASEAuthTags; !slices.Equal(got, cats) {
			t.Fatalf("CATs %v, want %v", got, cats)
		}
	})
}

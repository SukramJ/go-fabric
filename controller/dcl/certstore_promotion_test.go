// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
)

// TestCertificateStorePromotionTakesTheProductionBytes (review II,
// R2-P6-sec-1902): a test certificate stored under a key identifier the
// production ledger later lists is promoted with the production ledger's
// certificate, not with its own bytes; a forced update from the test
// ledger leaves the production entry's bytes; and when the production
// record cannot be fetched the entry stays a test certificate.
func TestCertificateStorePromotionTakesTheProductionBytes(t *testing.T) {
	t.Parallel()
	genuine := mint(t, certSpec{cn: "Test PAA", vid: u16(0xFFF1), ca: true, noAKID: true}, nil)
	other := mint(t, certSpec{cn: "Test PAA", vid: u16(0xFFF1), ca: true, noAKID: true, skid: genuine.skid()}, nil)
	if bytes.Equal(genuine.der, other.der) || !bytes.Equal(genuine.skid(), other.skid()) {
		t.Fatal("the two roots must share the key identifier and differ in their bytes")
	}
	emptyLedger := func() *fakeDCL {
		f := newFakeDCL(t)
		f.json("/dcl/pki/root-certificates", 200, map[string]any{"approvedRootCertificates": map[string]any{"certs": []any{}, "schemaVersion": 0}})
		return f
	}
	testLedger := newFakeDCL(t)
	testLedger.addRoot(other)
	first := openStore(t, CertificateOptions{Client: emptyLedger().client(), TestClient: testLedger.client(), FetchTestCertificates: true, Clock: newManualClock()})
	if p, ok := first.PAA(other.skid()); !ok || p.Production || !bytes.Equal(p.DER, other.der) {
		t.Fatalf("the test root stored: found %v, production %v", ok, p.Production)
	}

	t.Run("promoted with the production record", func(t *testing.T) {
		t.Parallel()
		prod := newFakeDCL(t)
		prod.addRoot(genuine)
		s := openStore(t, CertificateOptions{
			Client: prod.client(), TestClient: testLedger.client(), FetchTestCertificates: true, Clock: newManualClock(),
			Store: storeOf(t, first),
		})
		p, ok := s.PAA(genuine.skid())
		if !ok || !p.Production || !bytes.Equal(p.DER, genuine.der) {
			t.Fatalf("after the production ledger listed it: production %v, the production bytes %v", p.Production, bytes.Equal(p.DER, genuine.der))
		}
		// A forced update from the test ledger keeps the production bytes.
		s.Update(context.Background(), true)
		if p, _ := s.PAA(genuine.skid()); !p.Production || !bytes.Equal(p.DER, genuine.der) {
			t.Fatalf("after a forced update: production %v, the production bytes %v", p.Production, bytes.Equal(p.DER, genuine.der))
		}
	})
	t.Run("not promoted when the production record fails", func(t *testing.T) {
		t.Parallel()
		prod := newFakeDCL(t)
		prod.addRoot(genuine)
		subject := base64.StdEncoding.EncodeToString(genuine.cert.RawSubject)
		prod.mu.Lock()
		prod.failPath["/dcl/pki/certificates/"+encodeURIComponent(subject)+"/"+encodeURIComponent(colonHex(genuine.skid()))] = 500
		prod.mu.Unlock()
		s := openStore(t, CertificateOptions{
			Client: prod.client(), TestClient: testLedger.client(), FetchTestCertificates: true, Clock: newManualClock(),
			Store: storeOf(t, first),
		})
		if p, ok := s.PAA(other.skid()); !ok || p.Production {
			t.Fatalf("promoted without the production record: production %v", p.Production)
		}
	})
}

// TestStoreCertificateKeepsAProductionEntry (review II, R2-P6-sec-1902) is
// the standing guard of the one writer every source stores through: a
// non-production certificate (the test ledger, GitHub, AddCertificate)
// under the key identifier of a production entry changes nothing.
func TestStoreCertificateKeepsAProductionEntry(t *testing.T) {
	t.Parallel()
	genuine := mint(t, certSpec{cn: "Test PAA", vid: u16(0xFFF1), ca: true, noAKID: true}, nil)
	other := mint(t, certSpec{cn: "Test PAA", vid: u16(0xFFF1), ca: true, noAKID: true, skid: genuine.skid()}, nil)
	s := openStore(t, CertificateOptions{})
	id := genuine.skidHex()
	s.storeCertificate(id, genuine.der, CertificateMetadata{SubjectKeyID: id, IsProduction: true, Kind: KindPAA, IsRoot: true}, true)
	for _, kind := range []Kind{KindPAA, KindCDSigner} {
		s.storeCertificate(id, other.der, CertificateMetadata{SubjectKeyID: id, IsProduction: false, Kind: kind}, false)
		e, _ := s.lookup(id)
		if !e.IsProduction || !bytes.Equal(e.DER, genuine.der) || e.Kind != KindPAA {
			t.Fatalf("a test %s replaced the production entry: production %v, kind %s", kind, e.IsProduction, e.Kind)
		}
	}
}

// TestCertificateStoreHealsAnOldPromotion (review II, R2-P6-sec-1902): a
// store written before the rule may hold a production entry over a test
// source's bytes, unmarked as the production ledger's; the next update
// that sees the root listed fetches the production record and stores its
// bytes.
func TestCertificateStoreHealsAnOldPromotion(t *testing.T) {
	t.Parallel()
	genuine := mint(t, certSpec{cn: "Test PAA", vid: u16(0xFFF1), ca: true, noAKID: true}, nil)
	other := mint(t, certSpec{cn: "Test PAA", vid: u16(0xFFF1), ca: true, noAKID: true, skid: genuine.skid()}, nil)
	old, err := json.Marshal(map[string]any{"certificates": []any{map[string]any{
		"subjectKeyId": genuine.skidHex(), "isProduction": true, "kind": string(KindPAA), "isRoot": true, "der": other.der,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	mem := &MemoryStore{}
	_ = mem.Save(context.Background(), old)
	prod := newFakeDCL(t)
	prod.addRoot(genuine)
	s := openStore(t, CertificateOptions{Client: prod.client(), Store: mem, Clock: newManualClock(), UpdateInterval: -1})
	p, ok := s.PAA(genuine.skid())
	if !ok || !p.Production || !bytes.Equal(p.DER, genuine.der) {
		t.Fatalf("after the update: found %v, production %v, the production bytes %v", ok, p.Production, bytes.Equal(p.DER, genuine.der))
	}
}

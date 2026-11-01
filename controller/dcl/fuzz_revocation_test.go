// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"testing"
	"time"
)

// FuzzParseRevocationSet feeds ParseRevocationSet foreign JSON. The set
// is a file the operator installs, produced by a tool outside this
// module. Invariant: it parses or returns an error and never panics. A
// parsed set holds no more entries than the input has bytes.
func FuzzParseRevocationSet(f *testing.F) {
	f.Add([]byte(`[{"type":"revocation_set","issuer_subject_key_id":"AABB","issuer_name":"MA==","revoked_serial_numbers":["01","02:03"],"crl_signer_cert":"MIIB"}]`))
	f.Add([]byte(`[]`))
	f.Add([]byte(`[{"type":"other"}]`))
	f.Add([]byte(`{"type":"revocation_set"}`))
	f.Add([]byte(`[{"revoked_serial_numbers":[1,null,{}]}]`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<16 {
			return
		}
		entries, err := ParseRevocationSet(data)
		if err != nil {
			return
		}
		n := len(entries)
		for _, e := range entries {
			n += len(e.RevokedSerialNumbers)
		}
		if n > len(data) {
			t.Fatalf("parsed %d entries and serials from %d bytes", n, len(data))
		}
	})
}

// FuzzParseCRL feeds parseCRL foreign DER: a CRL a distribution point
// serves, which an attacker on the path can replace. Invariant: it parses
// or returns an error and never panics, with and without an authority
// name. A parsed CRL holds no more revoked serials than the input has
// bytes.
func FuzzParseCRL(f *testing.F) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		f.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Fuzz CRL Issuer"},
		NotBefore:             time.Unix(0, 0),
		NotAfter:              time.Unix(1<<32, 0),
		KeyUsage:              x509.KeyUsageCRLSign | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		SubjectKeyId:          []byte{1, 2, 3, 4},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		f.Fatal(err)
	}
	issuer, err := x509.ParseCertificate(der)
	if err != nil {
		f.Fatal(err)
	}
	crl, err := x509.CreateRevocationList(rand.Reader, &x509.RevocationList{
		Number:     big.NewInt(1),
		ThisUpdate: time.Unix(1000, 0),
		NextUpdate: time.Unix(2000, 0),
		RevokedCertificateEntries: []x509.RevocationListEntry{
			{SerialNumber: big.NewInt(0x01AB), RevocationTime: time.Unix(1000, 0)},
			{SerialNumber: big.NewInt(0xFF00FF), RevocationTime: time.Unix(1000, 0)},
		},
	}, issuer, key)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(crl, false)
	f.Add(crl, true)
	f.Add([]byte{0x30, 0x81}, false)
	f.Add([]byte{0x30, 0x00}, false)
	f.Add(nestedDER(1000), false) // W4-ctlint-01: deeper than derMaxDepth
	authority := issuer.RawSubject
	f.Fuzz(func(t *testing.T, data []byte, withAuthority bool) {
		if len(data) > 1<<16 {
			return
		}
		var name []byte
		if withAuthority {
			name = authority
		}
		r, err := parseCRL(data, name)
		if err != nil {
			return
		}
		if len(r.serials) > len(data) {
			t.Fatalf("parsed %d serials from %d bytes", len(r.serials), len(data))
		}
	})
}

// nestedDER is depth SEQUENCEs, each in long-form length, around a NULL.
func nestedDER(depth int) []byte {
	der := []byte{0x05, 0x00}
	for range depth {
		n := len(der)
		der = append([]byte{0x30, 0x84, byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)}, der...)
	}
	return der
}

// TestDERDepthIsBounded pins the nesting bound of derDecodeRec: a CRL
// nested 1000 levels deep is refused as malformed (matter.js's recursive
// DerCodec.#decodeRec, DerCodec.ts:429-442, throws a RangeError that
// #processRevocationPoint turns into a rejected point; in Go the stack
// overflow would be fatal), and a nesting within derMaxDepth still
// decodes. W4-ctlint-01.
func TestDERDepthIsBounded(t *testing.T) {
	t.Parallel()
	if _, err := derDecode(nestedDER(1000)); err == nil {
		t.Fatal("a 1000-level nesting decoded; want it refused")
	}
	if _, err := parseCRL(nestedDER(1000), nil); err == nil {
		t.Fatal("parseCRL accepted a 1000-level nesting")
	}
	n, err := derDecode(nestedDER(derMaxDepth))
	if err != nil {
		t.Fatalf("a %d-level nesting: %v", derMaxDepth, err)
	}
	for range derMaxDepth {
		if len(n.elements) != 1 {
			t.Fatalf("decoded tree lost a level: %+v", n)
		}
		n = n.elements[0]
	}
}

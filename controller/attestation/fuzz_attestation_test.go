// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package attestation

import (
	"encoding/hex"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	secattestation "github.com/SukramJ/go-fabric/secure/attestation"
	"github.com/SukramJ/go-fabric/tlv"
)

// The harnesses below cover the decoders of a commissionee's attestation
// answer, which come from the peer's bytes before anything is verified.
// They are harness only: the functions are the controller's attestation
// path and are not changed here.

// FuzzDecodeCDContent feeds decodeCDContent the signed content of a
// Certification Declaration. Invariant: it decodes or returns an error
// and never panics. A decoded declaration keeps the bounds the schema
// sets: 1 to 100 product ids, a certificate id of 19 UTF-16 units, and at most
// 10 authorized PAAs of 20 bytes each.
func FuzzDecodeCDContent(f *testing.F) {
	cd, err := secattestation.BuildTestCertificationDeclaration(0xFFF1, 0x8000)
	if err != nil {
		f.Fatal(err)
	}
	d, err := parseDeclaration(cd)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(d.signedData)
	f.Add([]byte{0x15, 0x18})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 1<<16 {
			return
		}
		c, err := decodeCDContent(b)
		if err != nil {
			return
		}
		if n := len(c.productIDs); n < 1 || n > 100 {
			t.Fatalf("%d product ids, outside 1..100", n)
		}
		if spec.StringLength(c.certificateID) != 19 {
			t.Fatalf("certificate id %q is not 19 UTF-16 units", c.certificateID)
		}
		if len(c.authorizedPAAList) > 10 {
			t.Fatalf("%d authorized PAAs, more than 10", len(c.authorizedPAAList))
		}
		for _, p := range c.authorizedPAAList {
			if len(p) != 20 {
				t.Fatalf("authorized PAA of %d bytes, want 20", len(p))
			}
		}
	})
}

// FuzzDecodeAttestationElements feeds decodeAttestationElements the
// AttestationElements of an AttestationResponse. Invariant: it decodes or
// returns an error and never panics.
func FuzzDecodeAttestationElements(f *testing.F) {
	cd, err := secattestation.BuildTestCertificationDeclaration(0xFFF1, 0x8000)
	if err != nil {
		f.Fatal(err)
	}
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutOctets(tlv.ContextTag(1), cd)
	enc.PutOctets(tlv.ContextTag(2), make([]byte, 32))
	enc.PutUint32(tlv.ContextTag(3), 0)
	enc.PutOctets(tlv.ContextTag(4), []byte{1, 2})
	if err := enc.EndContainer(); err != nil {
		f.Fatal(err)
	}
	seed, err := enc.Bytes()
	if err != nil {
		f.Fatal(err)
	}
	f.Add(seed)
	f.Add([]byte{0x15})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 1<<16 {
			return
		}
		_, _ = decodeAttestationElements(b)
	})
}

// FuzzParseAttCert feeds parseAttCert a DAC, PAI or PAA DER, with and
// without the authority key identifier requirement. Invariant: it parses
// or returns an error and never panics.
func FuzzParseAttCert(f *testing.F) {
	for _, s := range []string{certificatesNukiDAC, certificatesNukiPAI} {
		der, err := hex.DecodeString(s)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(der, true)
		f.Add(der, false)
	}
	chain, err := secattestation.BuildTestChain(0xFFF1, 0x8000)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(chain.DAC, true)
	f.Add(chain.PAI, true)
	f.Add([]byte{0x30, 0x00}, false)
	f.Fuzz(func(t *testing.T, der []byte, authorityKeyID bool) {
		if len(der) > 1<<16 {
			return
		}
		_, _ = parseAttCert(der, authorityKeyID)
	})
}

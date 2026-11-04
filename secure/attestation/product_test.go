// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package attestation

import (
	"crypto/x509/pkix"
	"encoding/asn1"
	"strings"
	"testing"
)

func TestValidateProductRefusesWhatItCannotRead(t *testing.T) {
	t.Parallel()
	if err := ValidateProduct(nil, 0xfff1, 0x8000); err == nil || !strings.Contains(err.Error(), "nil chain") {
		t.Fatalf("ValidateProduct(nil) = %v, want the nil-chain refusal", err)
	}
	chain, err := BuildTestChain(0xfff1, 0x8000)
	if err != nil {
		t.Fatalf("BuildTestChain: %v", err)
	}
	if err := ValidateProduct(&Chain{DAC: []byte{1, 2, 3}, PAI: chain.PAI}, 0xfff1, 0x8000); err == nil || !strings.Contains(err.Error(), "DAC") {
		t.Fatalf("ValidateProduct(unparseable DAC) = %v, want the DAC parse refusal", err)
	}
	if err := ValidateProduct(&Chain{DAC: chain.DAC, PAI: []byte{1, 2, 3}}, 0xfff1, 0x8000); err == nil || !strings.Contains(err.Error(), "PAI") {
		t.Fatalf("ValidateProduct(unparseable PAI) = %v, want the PAI parse refusal", err)
	}
	if err := ValidateProduct(chain, 0xfff1, 0x8000); err != nil {
		t.Fatalf("ValidateProduct(matching) = %v", err)
	}
}

// productIDsOf reads the Matter OIDs first (Certificate.ts:346-347
// vendorId/productId at 862ad75f); the `Mvid:`/`Mpid:` fallback of the
// common name applies only when the DN carries neither
// (Certificate.ts:380-385, parseMatterFallbackVidPid).
func TestProductIDsOfReadsTheOIDsElseTheCommonName(t *testing.T) {
	t.Parallel()
	attr := func(oid asn1.ObjectIdentifier, v any) pkix.AttributeTypeAndValue {
		return pkix.AttributeTypeAndValue{Type: oid, Value: v}
	}
	id := func(v uint16) *uint16 { return &v }
	cases := []struct {
		name     string
		dn       pkix.Name
		vid, pid *uint16
	}{
		{"both OIDs", pkix.Name{Names: []pkix.AttributeTypeAndValue{attr(oidMatterVendorID, "FFF1"), attr(oidMatterProductID, "8000")}}, id(0xfff1), id(0x8000)},
		{"OID wins over the commonName", pkix.Name{CommonName: "Mvid:0001 Mpid:0002", Names: []pkix.AttributeTypeAndValue{attr(oidMatterVendorID, "FFF1")}}, id(0xfff1), nil},
		{"a non-string value is not an id", pkix.Name{Names: []pkix.AttributeTypeAndValue{attr(oidMatterVendorID, 5), attr(oidMatterProductID, "8000")}}, nil, id(0x8000)},
		{"a non-hex value is not an id", pkix.Name{Names: []pkix.AttributeTypeAndValue{attr(oidMatterVendorID, "zz"), attr(oidMatterProductID, "FFFFF")}}, nil, nil},
		{"commonName fallback", pkix.Name{CommonName: "Mvid:FFF1 Mpid:8000"}, id(0xfff1), id(0x8000)},
		{"commonName with the vendor only", pkix.Name{CommonName: "ACME Mvid:FFF1"}, id(0xfff1), nil},
		{"nothing", pkix.Name{CommonName: "ACME"}, nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			vid, pid := productIDsOf(tc.dn)
			if !sameID(vid, tc.vid) || !sameID(pid, tc.pid) {
				t.Fatalf("productIDsOf = (%s, %s), want (%s, %s)", fmtID(vid), fmtID(pid), fmtID(tc.vid), fmtID(tc.pid))
			}
		})
	}
}

func sameID(a, b *uint16) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func fmtID(v *uint16) string {
	if v == nil {
		return "nil"
	}
	return strings.ToUpper(strings.TrimPrefix(asHex(*v), "0x"))
}

func asHex(v uint16) string {
	const digits = "0123456789abcdef"
	return "0x" + string([]byte{digits[v>>12&0xf], digits[v>>8&0xf], digits[v>>4&0xf], digits[v&0xf]})
}

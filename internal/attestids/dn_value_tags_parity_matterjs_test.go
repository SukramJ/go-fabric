// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package attestids

import (
	"encoding/asn1"
	"testing"
)

// TestParityMatterJS_DNValueTags: a matter-oid-vid value is read from its
// content bytes as text, whatever its DER string tag — matter.js
// parseSubjectOrIssuer, `Bytes.toString(Bytes.of(valueNode._bytes))` with
// no look at the tag (packages/protocol/src/certificate/kinds/Certificate.ts:321-326
// at b25ed960). So "FFF1" in UTF8String, PrintableString, IA5String,
// T61String, NumericString or OCTET STRING is 0xFFF1, and in BMPString
// (UTF-16) or UniversalString (UTF-32) it is malformed. Before, the value
// came from crypto/x509's decoded name: BMPString read as 0xFFF1, OCTET
// STRING as malformed (review II, R2-P6-par-2904, F-db021da0-49).
func TestParityMatterJS_DNValueTags(t *testing.T) {
	t.Parallel()
	utf16 := []byte{0, 'F', 0, 'F', 0, 'F', 0, '1'}
	utf32 := []byte{0, 0, 0, 'F', 0, 0, 0, 'F', 0, 0, 0, 'F', 0, 0, 0, '1'}
	ascii := []byte("FFF1")
	for _, c := range []struct {
		name    string
		tag     int
		content []byte
		ok      bool
	}{
		{"UTF8String", asn1.TagUTF8String, ascii, true},
		{"PrintableString", asn1.TagPrintableString, ascii, true},
		{"IA5String", asn1.TagIA5String, ascii, true},
		{"T61String", asn1.TagT61String, ascii, true},
		{"NumericString", asn1.TagNumericString, ascii, true},
		{"OCTET STRING", asn1.TagOctetString, ascii, true},
		{"BMPString", asn1.TagBMPString, utf16, false},
		{"UniversalString", 28, utf32, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			raw := rdnSequence(t, OIDVendorID, asn1.RawValue{Class: asn1.ClassUniversal, Tag: c.tag, Bytes: c.content})
			vid, _, err := FromRawName(raw, false)
			switch {
			case c.ok && (err != nil || vid == nil || *vid != 0xFFF1):
				t.Fatalf("vid %v, %v; want 0xFFF1", vid, err)
			case !c.ok && err == nil:
				t.Fatalf("vid %v accepted; want the malformed refusal", vid)
			}
			vid, _, err = FromRawName(raw, true)
			if !c.ok && (err != nil || vid != nil) {
				t.Fatalf("acceptMalformed: vid %v, %v; want absent", vid, err)
			}
		})
	}
}

// rdnSequence is the DER of a distinguished name of one attribute.
func rdnSequence(t *testing.T, oid asn1.ObjectIdentifier, value asn1.RawValue) []byte {
	t.Helper()
	oidDER, err := asn1.Marshal(oid)
	if err != nil {
		t.Fatal(err)
	}
	valueDER, err := asn1.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	atv, err := asn1.Marshal(asn1.RawValue{Class: asn1.ClassUniversal, Tag: asn1.TagSequence, IsCompound: true, Bytes: append(oidDER, valueDER...)})
	if err != nil {
		t.Fatal(err)
	}
	set, err := asn1.Marshal(asn1.RawValue{Class: asn1.ClassUniversal, Tag: asn1.TagSet, IsCompound: true, Bytes: atv})
	if err != nil {
		t.Fatal(err)
	}
	seq, err := asn1.Marshal(asn1.RawValue{Class: asn1.ClassUniversal, Tag: asn1.TagSequence, IsCompound: true, Bytes: set})
	if err != nil {
		t.Fatal(err)
	}
	return seq
}

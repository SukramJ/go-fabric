// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mattercert

import (
	"encoding/asn1"
	"testing"
)

// TestBuildDNStandardAttributes checks the string type and the OID each
// standard X.520 DN tag is written with: a UTF8String for 1-15, an
// IA5String for domainComponent (16), a PrintableString for 129-143, in
// arrival order among the Matter attributes — matter.js
// packages/protocol/src/certificate/kinds/Certificate.ts:895-964 and
// packages/general/src/crypto/X520.ts:28-79 at 862ad75f.
func TestBuildDNStandardAttributes(t *testing.T) {
	dn := DistinguishedName{
		MatterRCACID: 0xCACACACA00000001, HasRCACID: true,
		Attributes: []DNAttribute{{Tag: 132, Value: "US"}, {Tag: 7, Value: "Google"}, {Tag: 16, Value: "example"}, {Tag: 1, Value: "Matter Root"}},
		Order:      []uint8{132, 7, 16, dnTagMatterRCACID, 1},
	}
	rdns := buildDN(dn)
	want := []struct {
		oid asn1.ObjectIdentifier
		tag int
		val string
	}{
		{asn1.ObjectIdentifier{2, 5, 4, 6}, asn1.TagPrintableString, "US"},
		{asn1.ObjectIdentifier{2, 5, 4, 10}, asn1.TagUTF8String, "Google"},
		{oidDomainComponent, asn1.TagIA5String, "example"},
		{oidMatterRCACID, asn1.TagUTF8String, "CACACACA00000001"},
		{asn1.ObjectIdentifier{2, 5, 4, 3}, asn1.TagUTF8String, "Matter Root"},
	}
	if len(rdns) != len(want) {
		t.Fatalf("%d RDNs, want %d", len(rdns), len(want))
	}
	for i, w := range want {
		var attr struct {
			Type  asn1.ObjectIdentifier
			Value asn1.RawValue
		}
		if _, err := asn1.Unmarshal(rdns[i].Bytes, &attr); err != nil {
			t.Fatalf("RDN %d: %v", i, err)
		}
		if !attr.Type.Equal(w.oid) || attr.Value.Tag != w.tag || string(attr.Value.Bytes) != w.val {
			t.Fatalf("RDN %d: %v tag %d %q, want %v tag %d %q", i, attr.Type, attr.Value.Tag, attr.Value.Bytes, w.oid, w.tag, w.val)
		}
	}
	if _, _, ok := standardDNAttribute(17); ok {
		t.Fatal("tag 17 is a Matter attribute, not a standard one")
	}
	if _, _, ok := standardDNAttribute(144); ok {
		t.Fatal("tag 144 is no DN attribute")
	}
}

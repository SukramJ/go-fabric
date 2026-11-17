// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package attestids

import "testing"

// TestFromRawNameStructure: FromRawName refuses a DER name whose
// RDNSequence, an RDN set or an attribute does not parse, and rawAttributes
// skips an RDN that is not a set, an attribute that is not constructed, one
// whose elements do not parse and one whose first element is not an OBJECT
// IDENTIFIER ([FromRawName]'s doc states which rule is whose). Review II,
// R2-P6-par-2904.
func TestFromRawNameStructure(t *testing.T) {
	t.Parallel()
	refused := []struct {
		name string
		der  []byte
	}{
		{"not DER", []byte{0x01}},
		{"not a SEQUENCE", []byte{0x31, 0x00}},
		{"trailing bytes", []byte{0x30, 0x00, 0x00}},
		{"an RDN that does not parse", []byte{0x30, 0x02, 0x31, 0x05}},
		{"an attribute that does not parse", []byte{0x30, 0x04, 0x31, 0x02, 0x30, 0x05}},
	}
	for _, c := range refused {
		if _, _, err := FromRawName(c.der, false); err == nil {
			t.Errorf("%s: FromRawName(% x) = nil error, want the structure refused", c.name, c.der)
		}
	}
	skipped := []struct {
		name string
		der  []byte
	}{
		{"an RDN that is not a set", []byte{0x30, 0x02, 0x04, 0x00}},
		{"an attribute that is not constructed", []byte{0x30, 0x04, 0x31, 0x02, 0x04, 0x00}},
		{"an attribute whose element does not parse", []byte{0x30, 0x06, 0x31, 0x04, 0x30, 0x02, 0x02, 0x05}},
		{"an attribute whose type is not an OID", []byte{0x30, 0x09, 0x31, 0x07, 0x30, 0x05, 0x02, 0x01, 0x01, 0x0c, 0x00}},
	}
	for _, c := range skipped {
		attrs, err := rawAttributes(c.der)
		if err != nil || len(attrs) != 0 {
			t.Errorf("%s: rawAttributes(% x) = %v, %v; want the attribute skipped", c.name, c.der, attrs, err)
		}
	}
}

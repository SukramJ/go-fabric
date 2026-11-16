// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package attestids

import (
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func attr(t asn1.ObjectIdentifier, v any) pkix.AttributeTypeAndValue {
	return pkix.AttributeTypeAndValue{Type: t, Value: v}
}

func dn(a ...pkix.AttributeTypeAndValue) pkix.Name { return pkix.Name{Names: a} }

func idStr(p *uint16) string {
	if p == nil {
		return "-"
	}
	return fmt.Sprintf("%04X", *p)
}

// TestFromNamePolicies: one rule, two policies — the cases where they
// part, and the ones where they must not.
func TestFromNamePolicies(t *testing.T) {
	t.Parallel()
	cn := attr(OIDCommonName, "Matter Test DAC")
	vid, pid := attr(OIDVendorID, "FFF1"), attr(OIDProductID, "8000")
	for _, tc := range []struct {
		name                     string
		dn                       pkix.Name
		strictVID, strictPID     string
		strictErr                string // "" none, "attr" ErrMalformedAttribute, else a substring
		lenientVID, lenientPID   string
		lenientErr               string
		strictErrorWrapsSentinel bool
	}{
		{"both attributes", dn(cn, vid, pid), "FFF1", "8000", "", "FFF1", "8000", "", false},
		{"nothing", dn(cn), "-", "-", "", "-", "-", "", false},
		{"attribute wins over the commonName", dn(attr(OIDCommonName, "Mvid:0001 Mpid:0002"), vid), "FFF1", "-", "", "FFF1", "-", "", false},
		{"lowercase VendorID", dn(cn, attr(OIDVendorID, "fff1"), pid), "", "", "Invalid vendorId \"fff1\"", "-", "8000", "", true},
		{"non-string ProductID", dn(cn, vid, attr(OIDProductID, 5)), "", "", "Invalid productId \"\"", "FFF1", "-", "", true},
		{"repeated VendorID", dn(cn, vid, pid, vid), "", "", "contains vendorId more than once", "-", "8000", "", true},
		{"malformed attribute blocks the fallback", dn(attr(OIDCommonName, "Mvid:FFF2"), attr(OIDVendorID, "fff1")), "", "", "Invalid vendorId", "-", "-", "", true},
		{"commonName fallback", dn(attr(OIDCommonName, "Mvid:FFF1 Mpid:8000")), "FFF1", "8000", "", "FFF1", "8000", "", false},
		{"first commonName with a value", dn(attr(OIDCommonName, "Mvid:FFF1"), attr(OIDCommonName, "Mvid:FFF2 Mpid:8000")), "FFF1", "-", "", "FFF1", "-", "", false},
		{"64-byte window", dn(attr(OIDCommonName, strings.Repeat("x", 60)+"Mvid:FFF1")), "-", "-", "", "-", "-", "", false},
		{"malformed prefix refuses under both", dn(attr(OIDCommonName, "Matter Mpid:"), attr(OIDCommonName, "Mvid:FFF1")), "", "", "not followed by 4 uppercase hex digits", "", "", "not followed by 4 uppercase hex digits", false},
		{"other attribute types are not read", dn(attr(asn1.ObjectIdentifier{2, 5, 4, 10}, "Mvid:FFF3")), "-", "-", "", "-", "-", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			check := func(policy string, accept bool, wantV, wantP, wantErr string, sentinel bool) {
				t.Helper()
				v, p, err := FromName(tc.dn, accept)
				if wantErr != "" {
					if err == nil || !strings.Contains(err.Error(), wantErr) || errors.Is(err, ErrMalformedAttribute) != sentinel || v != nil || p != nil {
						t.Fatalf("%s: (%s, %s, %v), want an error containing %q (sentinel %v)", policy, idStr(v), idStr(p), err, wantErr, sentinel)
					}
					return
				}
				if err != nil || idStr(v) != wantV || idStr(p) != wantP {
					t.Fatalf("%s: (%s, %s, %v), want (%s, %s)", policy, idStr(v), idStr(p), err, wantV, wantP)
				}
			}
			check("strict", false, tc.strictVID, tc.strictPID, tc.strictErr, tc.strictErrorWrapsSentinel)
			check("lenient", true, tc.lenientVID, tc.lenientPID, tc.lenientErr, false)
		})
	}
}

// TestFromNamePoliciesPartOnlyOnAttributes: over every DN of up to three
// attributes from a pool of valid, malformed and repeated ids and of
// commonNames with and without a fallback, the two policies agree except
// where the strict one refuses a malformed or repeated id attribute — the
// one axis acceptMalformedVidPid moves. Held at the merge of the two
// former copies (R2-P5-port-12), each of which gave the same answers.
func TestFromNamePoliciesPartOnlyOnAttributes(t *testing.T) {
	t.Parallel()
	pool := make([]pkix.AttributeTypeAndValue, 0, 30)
	for _, v := range []any{"FFF1", "8000", "0000", "fff1", "FFF", "80G0", "0FFF1", "", 5} {
		pool = append(pool, attr(OIDVendorID, v), attr(OIDProductID, v))
	}
	for _, v := range []any{"ACME", "Mvid:FFF1 Mpid:8000", "Mvid:FFF2", "Mpid:00B1", "Mvid:fff1", "Matter Mpid:", "Mvid:FFF1 Mpid:12", strings.Repeat("x", 60) + "Mvid:FFF1", strings.Repeat("x", 56) + "Mvid:FFF1", strings.Repeat("é", 31) + "Mvid:FFF1", 7} {
		pool = append(pool, attr(OIDCommonName, v))
	}
	pool = append(pool, attr(asn1.ObjectIdentifier{2, 5, 4, 10}, "Mvid:FFF3"))
	var walk func(prefix []pkix.AttributeTypeAndValue, depth int)
	parted := 0
	walk = func(prefix []pkix.AttributeTypeAndValue, depth int) {
		for _, a := range pool {
			names := append(append([]pkix.AttributeTypeAndValue{}, prefix...), a)
			sv, sp, serr := FromName(dn(names...), false)
			lv, lp, lerr := FromName(dn(names...), true)
			switch {
			case errors.Is(serr, ErrMalformedAttribute):
				parted++
				if lerr != nil {
					t.Fatalf("%v: lenient refused (%v) where strict refused an attribute", names, lerr)
				}
			case serr != nil:
				if lerr == nil || lerr.Error() != serr.Error() {
					t.Fatalf("%v: strict %v, lenient %v", names, serr, lerr)
				}
			default:
				if lerr != nil || idStr(sv) != idStr(lv) || idStr(sp) != idStr(lp) {
					t.Fatalf("%v: strict (%s, %s), lenient (%s, %s, %v)", names, idStr(sv), idStr(sp), idStr(lv), idStr(lp), lerr)
				}
			}
			if depth > 1 {
				walk(names, depth-1)
			}
		}
	}
	walk(nil, 3)
	if parted == 0 {
		t.Fatal("the corpus never parts the policies; the walk is broken")
	}
}

// TestFallbackIDAndParseHexID: the commonName fallback and the id value's
// form.
func TestFallbackIDAndParseHexID(t *testing.T) {
	t.Parallel()
	if id, err := FallbackID("Matter Mvid:FFF1 Mpid:8000", "Mvid:"); err != nil || id == nil || *id != 0xFFF1 {
		t.Fatalf("Mvid: %v %v", id, err)
	}
	if id, err := FallbackID("no prefix", "Mpid:"); err != nil || id != nil {
		t.Fatalf("absent: %v %v", id, err)
	}
	if _, err := FallbackID("Mpid:80", "Mpid:"); err == nil {
		t.Fatal("a malformed Mpid: was accepted")
	}
	for in, want := range map[string]int{"FFF1": 0xFFF1, "0000": 0, "fff1": -1, "FFF": -1, "0FFF1": -1, "": -1} {
		got, ok := ParseHexID(in)
		if (want >= 0) != ok || (ok && int(got) != want) {
			t.Errorf("ParseHexID(%q) = %d, %v, want %d", in, got, ok, want)
		}
	}
}

// TestOneDefinitionOfTheIDAttributes: no production file outside this
// package spells the matter-oid-vid or matter-oid-pid OID, so the rule
// that reads them cannot grow a second copy unnoticed (R2-P5-port-12).
// Test files that build certificates may.
func TestOneDefinitionOfTheIDAttributes(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	self, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	needles := []string{"37244, 2, 1}", "37244, 2, 2}"}
	seen := 0
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); path != root && (strings.HasPrefix(name, ".") || name == "testdata" || name == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		seen++
		if filepath.Dir(path) == self {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, n := range needles {
			if strings.Contains(string(b), n) {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("%s defines a Matter id attribute OID (%s); use internal/attestids", rel, n)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if seen < 100 {
		t.Fatalf("walked %d Go files; the walk is broken", seen)
	}
}

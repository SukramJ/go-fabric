// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package snapshot

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/controller/dcl"
	secattestation "github.com/SukramJ/go-fabric/secure/attestation"
)

func colons(skid string) string {
	var parts []string
	for i := 0; i < len(skid); i += 2 {
		parts = append(parts, skid[i:i+2])
	}
	return strings.Join(parts, ":")
}

// TestFetch: Fetch writes the ledger's roots and the named CD signers
// with a SOURCE that Load reads back; an unusable root is left out.
func TestFetch(t *testing.T) {
	paaSKID := strings.ToUpper(hex.EncodeToString(secattestation.TestPAAFFF1SKID))
	cdDER, cdSKID, _ := cdSignerDER(t)
	pemOf := func(der []byte) string {
		return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body any
		switch {
		case r.URL.Path == "/dcl/pki/root-certificates":
			body = map[string]any{"approvedRootCertificates": map[string]any{"schemaVersion": 0, "certs": []map[string]string{
				{"subject": "A", "subjectKeyId": colons(paaSKID)},
				{"subject": "B", "subjectKeyId": colons(strings.Repeat("AB", 20))},
			}}}
		case strings.HasPrefix(r.URL.Path, "/dcl/pki/certificates/A/"):
			body = map[string]any{"approvedCertificates": map[string]any{
				"subject": "A", "subjectKeyId": colons(paaSKID), "schemaVersion": 0,
				"certs": []map[string]any{{"pemCert": pemOf(secattestation.TestPAAFFF1Cert), "subjectKeyId": colons(paaSKID), "isRoot": true}},
			}}
		case strings.HasPrefix(r.URL.Path, "/dcl/pki/certificates/B/"):
			// A root whose DER is not the stated key identifier's.
			body = map[string]any{"approvedCertificates": map[string]any{
				"subject": "B", "subjectKeyId": colons(strings.Repeat("AB", 20)), "schemaVersion": 0,
				"certs": []map[string]any{{"pemCert": pemOf(secattestation.TestPAANoVIDCert), "subjectKeyId": colons(strings.Repeat("AB", 20)), "isRoot": true}},
			}}
		case r.URL.Path == "/dcl/vendorinfo/vendors":
			body = map[string]any{"vendorInfo": []map[string]any{
				{"vendorID": 0x1234, "vendorName": "Lamps Inc", "companyLegalName": "Lamps Incorporated", "schemaVersion": 0},
			}, "pagination": map[string]any{"next_key": nil}}
		case r.URL.Path == "/dcl/pki/all-certificates":
			body = map[string]any{"certificates": []map[string]any{{
				"schemaVersion": 0, "subjectKeyId": colons(cdSKID),
				"certs": []map[string]any{{"pemCert": pemOf(cdDER), "subjectKeyId": colons(cdSKID)}},
			}}}
		default:
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
	defer srv.Close()

	dir := t.TempDir()
	src, err := Fetch(context.Background(), dcl.NewClient(srv.URL), []string{cdSKID}, dir, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if src.Ledger != srv.URL || !src.BuiltAt.Equal(testNow) || src.PAA.ExpectedCount != 1 || src.CDSigners.ExpectedCount != 1 {
		t.Fatalf("source %+v", src)
	}
	s, err := load(os.DirFS(dir), testNow)
	if err != nil {
		t.Fatal(err)
	}
	if paas, cds := s.Len(); paas != 1 || cds != 1 {
		t.Fatalf("loaded %d PAAs, %d CD signers", paas, cds)
	}
	if v, ok := s.Vendor(0x1234); !ok || v.Name != "Lamps Inc" || v.CompanyLegalName != "Lamps Incorporated" {
		t.Fatalf("vendor 0x1234: %+v (%v)", v, ok)
	}
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"approvedRootCertificates":{"schemaVersion":0,"certs":[]}}`))
	}))
	defer empty.Close()
	if _, err := Fetch(context.Background(), dcl.NewClient(empty.URL), nil, dir, testNow); err == nil {
		t.Fatal("a ledger without roots wrote a snapshot")
	}
}

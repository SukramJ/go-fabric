// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package dcl

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestClientRoots: the root list, a root by subject and key identifier
// (path segments encoded as encodeURIComponent does — a base64 subject
// carries "/", "+" and "="), and a certificate by key identifier.
func TestClientRoots(t *testing.T) {
	t.Parallel()
	f := newFakeDCL(t)
	pki := newTestPKI(t)
	f.addRoot(pki.paa)
	f.addBySKID(pki.pai)
	c := f.client()
	ctx := context.Background()

	refs, err := c.RootCertificates(ctx)
	if err != nil || len(refs) != 1 || refs[0].SubjectKeyID != colonHex(pki.paa.skid()) {
		t.Fatalf("RootCertificates %+v (%v)", refs, err)
	}
	certs, err := c.RootCertificateBySubject(ctx, refs[0])
	if err != nil || len(certs) != 1 || !certs[0].IsRoot || certs[0].SerialNumber != pki.paa.cert.SerialNumber.String() {
		t.Fatalf("RootCertificateBySubject %+v (%v)", certs, err)
	}
	der, err := pemToDER(certs[0].PEMCert)
	if err != nil || !bytes.Equal(der, pki.paa.der) {
		t.Fatalf("PEM %v", err)
	}
	// Another subject's answer is not the root asked for (:157-167).
	if _, err := c.RootCertificateBySubject(ctx, SubjectReference{Subject: refs[0].Subject, SubjectKeyID: "00:11"}); err == nil {
		t.Fatal("an unknown root was found")
	}
	// By key identifier, colons or not, either case (:141-143).
	for _, id := range []string{pki.pai.skidHex(), strings.ToLower(colonHex(pki.pai.skid()))} {
		got, err := c.CertificatesBySubjectKeyID(ctx, id)
		if err != nil || len(got) != 1 || got[0].SubjectKeyID != colonHex(pki.pai.skid()) {
			t.Fatalf("CertificatesBySubjectKeyID(%s) %+v (%v)", id, got, err)
		}
	}
	// None is NOT_FOUND here; the client reports the ledger's error body.
	_, err = c.CertificatesBySubjectKeyID(ctx, "0011")
	var re *ResponseError
	if !errors.As(err, &re) || re.Code != CodeNotFound || !IsNotFound(err) || re.Error() != "Error fetching /dcl/pki/all-certificates?subjectKeyId=00%3A11 from DCL: 5 - not found" {
		t.Fatalf("absent SKID: %v", err)
	}
}

// TestClientSchemaVersions: a root list of another schema version is an
// error; certificates and groups of another one are dropped.
func TestClientSchemaVersions(t *testing.T) {
	t.Parallel()
	f := newFakeDCL(t)
	pki := newTestPKI(t)
	c := f.client()
	ctx := context.Background()
	f.json("/dcl/pki/root-certificates", 200, map[string]any{"approvedRootCertificates": map[string]any{"certs": []any{}, "schemaVersion": 1}})
	if _, err := c.RootCertificates(ctx); err == nil || err.Error() != "Unsupported DCL Root Certificate schema version: 1" {
		t.Fatalf("schema 1: %v", err)
	}
	f.json("/dcl/pki/root-certificates", 200, map[string]any{})
	if _, err := c.RootCertificates(ctx); err == nil || !strings.Contains(err.Error(), "undefined") {
		t.Fatalf("no record: %v", err)
	}
	rec := certRecord(pki.pai, "c3ViamVjdA==", false)
	newer := certRecord(pki.dac, "c3ViamVjdA==", false)
	newer["schemaVersion"] = 1
	f.json("/dcl/pki/all-certificates?subjectKeyId=AA%3ABB", 200, map[string]any{"certificates": []any{
		map[string]any{"subject": "s", "subjectKeyId": "AA:BB", "schemaVersion": 0, "certs": []any{rec, newer}},
		map[string]any{"subject": "s", "subjectKeyId": "AA:BB", "schemaVersion": 2, "certs": []any{rec}},
	}})
	got, err := c.CertificatesBySubjectKeyID(ctx, "aabb")
	if err != nil || len(got) != 1 {
		t.Fatalf("supported records %d (%v)", len(got), err)
	}
}

// TestClientModels: the device model, the version list, one version with
// the decimal-string file size, each checked against what was asked.
func TestClientModels(t *testing.T) {
	t.Parallel()
	f := newFakeDCL(t)
	c := f.client()
	ctx := context.Background()
	f.json("/dcl/model/models/65521/32768", 200, map[string]any{"model": map[string]any{
		"vid": 65521, "pid": 32768, "deviceTypeId": 266, "productName": "Plug", "productLabel": "", "partNumber": "",
		"lsfUrl": "", "lsfRevision": 3, "enhancedSetupFlowTCUrl": "", "enhancedSetupFlowTCRevision": 2,
		"enhancedSetupFlowTCDigest": "abc", "enhancedSetupFlowTCFileSize": 10, "creator": "c", "schemaVersion": 0,
	}})
	m, err := c.Model(ctx, 0xFFF1, 0x8000)
	if err != nil || m.ProductName != "Plug" || m.DeviceTypeID != 266 || m.LSFRevision != 0 || m.EnhancedSetupFlowTCRevision != 0 || m.EnhancedSetupFlowTCDigest != "" {
		t.Fatalf("Model %+v (%v)", m, err)
	}
	if _, err := c.Model(ctx, 0xFFF1, 0x8001); !IsNotFound(err) {
		t.Fatalf("absent model: %v", err)
	}
	f.json("/dcl/model/models/65521/1", 200, map[string]any{"model": map[string]any{"vid": 65521, "pid": 2, "schemaVersion": 0}})
	if _, err := c.Model(ctx, 0xFFF1, 1); err == nil || err.Error() != "Model not found for VID: 65521, PID: 1" {
		t.Fatalf("other pid: %v", err)
	}

	f.json("/dcl/model/versions/65521/32768", 200, map[string]any{"modelVersions": map[string]any{
		"vid": 65521, "pid": 32768, "softwareVersions": []int{1, 2, 3}, "schemaVersion": 0,
	}})
	vs, err := c.ModelVersions(ctx, 0xFFF1, 0x8000)
	if err != nil || !slices.Equal(vs, []uint32{1, 2, 3}) {
		t.Fatalf("ModelVersions %v (%v)", vs, err)
	}
	f.json("/dcl/model/versions/65521/1", 200, map[string]any{"modelVersions": map[string]any{"vid": 65521, "pid": 1, "schemaVersion": 1}})
	if _, err := c.ModelVersions(ctx, 0xFFF1, 1); err == nil {
		t.Fatal("schema 1 versions accepted")
	}

	f.json("/dcl/model/versions/65521/32768/2", 200, map[string]any{"modelVersion": versionRecord(2, "https://example.invalid/2.ota", "1234", "c2hh", 1)})
	v, err := c.ModelVersion(ctx, 0xFFF1, 0x8000, 2)
	if err != nil || v.OTAFileSize != 1234 || v.OTAChecksum != "c2hh" || v.OTAChecksumType != 1 || !v.SoftwareVersionValid {
		t.Fatalf("ModelVersion %+v (%v)", v, err)
	}
	f.json("/dcl/model/versions/65521/32768/3", 200, map[string]any{"modelVersion": versionRecord(3, "", "n/a", "", 0)})
	if v, err := c.ModelVersion(ctx, 0xFFF1, 0x8000, 3); err != nil || v.OTAFileSize != 0 {
		t.Fatalf("non-decimal size %+v (%v)", v, err)
	}
	f.json("/dcl/model/versions/65521/32768/4", 200, map[string]any{"modelVersion": versionRecord(5, "", "0", "", 0)})
	if _, err := c.ModelVersion(ctx, 0xFFF1, 0x8000, 4); err == nil || err.Error() != "Model version not found for VID: 65521, PID: 32768, Software Version: 4" {
		t.Fatalf("other version: %v", err)
	}
}

func versionRecord(version int, url, size, checksum string, checksumType int) map[string]any {
	return map[string]any{
		"vid": 65521, "pid": 32768, "softwareVersion": version, "softwareVersionString": "v" + strings.Repeat("1", version),
		"cdVersionNumber": 1, "creator": "c", "firmwareInformation": "", "softwareVersionValid": true,
		"otaUrl": url, "otaFileSize": size, "otaChecksum": checksum, "otaChecksumType": checksumType,
		"minApplicableSoftwareVersion": 0, "maxApplicableSoftwareVersion": version - 1,
		"releaseNotesUrl": "", "specificationVersion": 0, "schemaVersion": 0,
	}
}

// TestClientRevocationPointsPaginated: the list is read page by page, the
// next request carrying pagination.key encoded; a point of another schema
// version is dropped; the by-issuer list of another schema version is an
// error.
func TestClientRevocationPointsPaginated(t *testing.T) {
	t.Parallel()
	f := newFakeDCL(t)
	c := f.client()
	ctx := context.Background()
	point := func(label string, schema int) map[string]any {
		return map[string]any{
			"vid": 65521, "pid": 0, "isPAA": true, "label": label, "crlSignerDelegator": "", "crlSignerCertificate": "pem",
			"issuerSubjectKeyID": "AABB", "dataURL": "https://crl.invalid/" + label, "dataFileSize": "12abc", "dataDigest": "",
			"dataDigestType": 0, "revocationType": 1, "schemaVersion": schema,
		}
	}
	f.json("/dcl/pki/revocation-points", 200, map[string]any{
		"PkiRevocationDistributionPoint": []any{point("a", 0)},
		"pagination":                     map[string]any{"next_key": "k/1=", "total": "3"},
	})
	f.json("/dcl/pki/revocation-points?pagination.key=k%2F1%3D", 200, map[string]any{
		"PkiRevocationDistributionPoint": []any{point("b", 0), point("c", 1)},
		"pagination":                     map[string]any{"next_key": nil},
	})
	got, err := c.RevocationPoints(ctx)
	if err != nil || len(got) != 2 || got[0].Label != "a" || got[1].Label != "b" || got[0].DataFileSize != 12 || got[0].IssuerSubjectKeyID != "AABB" {
		t.Fatalf("RevocationPoints %+v (%v)", got, err)
	}
	f.json("/dcl/pki/revocation-points/AABB", 200, map[string]any{
		"pkiRevocationDistributionPointsByIssuerSubjectKeyID": map[string]any{"issuerSubjectKeyID": "AABB", "points": []any{point("a", 0)}, "schemaVersion": 1},
	})
	if _, err := c.RevocationPointsByIssuer(ctx, "AABB"); err == nil {
		t.Fatal("schema 1 points accepted")
	}
	f.json("/dcl/pki/revocation-points/CCDD", 200, map[string]any{})
	if got, err := c.RevocationPointsByIssuer(ctx, "CCDD"); err != nil || len(got) != 0 {
		t.Fatalf("no record %+v (%v)", got, err)
	}
}

// TestClientFailures: a request slower than the timeout, an error body
// that is not JSON and a body that does not decode are ResponseErrors of
// code 500 carrying the cause (:109-119); an unreachable ledger too.
func TestClientFailures(t *testing.T) {
	t.Parallel()
	f := newFakeDCL(t)
	ctx := context.Background()
	f.json("/dcl/pki/root-certificates", 200, map[string]any{"approvedRootCertificates": map[string]any{"certs": []any{}, "schemaVersion": 0}})
	f.mu.Lock()
	f.delay = 300 * time.Millisecond
	f.mu.Unlock()
	c := f.client()
	c.Timeout = 50 * time.Millisecond
	_, err := c.RootCertificates(ctx)
	var re *ResponseError
	if !errors.As(err, &re) || re.Code != 500 || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout: %v", err)
	}
	f.mu.Lock()
	f.delay = 0
	f.mu.Unlock()
	if (&Client{}).timeout() != DefaultTimeout || (&Client{}).baseURL() != ProductionURL || NewClient(TestNetURL).baseURL() != TestNetURL {
		t.Fatal("defaults")
	}

	f.route("/dcl/model/versions/1/2", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("<html>bad gateway</html>"))
	})
	if _, err := c.ModelVersions(ctx, 1, 2); !errors.As(err, &re) || re.Code != 500 || re.Err == nil {
		t.Fatalf("non-JSON error body: %v", err)
	}
	f.route("/dcl/model/versions/1/3", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("{")) })
	if _, err := c.ModelVersions(ctx, 1, 3); !errors.As(err, &re) || re.Code != 500 {
		t.Fatalf("truncated body: %v", err)
	}
	dead := &Client{BaseURL: "http://127.0.0.1:1", Timeout: time.Second}
	if _, err := dead.RootCertificates(ctx); !errors.As(err, &re) || re.Code != 500 {
		t.Fatalf("unreachable: %v", err)
	}
	if _, err := (&Client{BaseURL: "::"}).RootCertificates(ctx); err == nil {
		t.Fatal("a bad base URL was accepted")
	}
}

func TestEncodeURIComponent(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"AB:CD":     "AB%3ACD",
		"a/b+c=":    "a%2Fb%2Bc%3D",
		"-_.!~*'()": "-_.!~*'()",
		"ä b":       "%C3%A4%20b",
		base64.StdEncoding.EncodeToString([]byte{0xfb, 0xff}): "%2B%2F8%3D",
	} {
		if got := encodeURIComponent(in); got != want {
			t.Errorf("encodeURIComponent(%q) = %q, want %q", in, got, want)
		}
	}
}

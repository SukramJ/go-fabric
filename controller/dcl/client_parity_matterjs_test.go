// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl

// A port of matter.js packages/protocol/test/dcl/DclClientTest.ts at
// 862ad75f: the ledger URL configuration, the record checks and the
// unset-field mapping of every DclClient fetch, and vendor pagination.
//
// matter.js maps a field the ledger sends as "" or 0 to undefined; the Go
// records keep the zero value, which means the same (types.go), so an
// "undefined" expectation is a zero-value check here.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
)

// dclClientMockFetch is matter.js's MockFetch
// (packages/general/src/util/MockFetch.ts) as an http.RoundTripper: a
// response matches when its pattern is a substring of (or a regexp
// matching) the request URL, the most recently added first; every request
// is logged. No request leaves the process.
type dclClientMockFetch struct {
	mu        sync.Mutex
	responses []dclClientMockResponse
	calls     []string
}

type dclClientMockResponse struct {
	sub    string
	re     *regexp.Regexp
	body   any
	status int
}

func (m *dclClientMockFetch) addResponse(pattern string, body any) {
	m.addStatus(pattern, body, http.StatusOK)
}

func (m *dclClientMockFetch) addStatus(pattern string, body any, status int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.responses = append(m.responses, dclClientMockResponse{sub: pattern, body: body, status: status})
}

func (m *dclClientMockFetch) addRegexp(re *regexp.Regexp, body any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.responses = append(m.responses, dclClientMockResponse{re: re, body: body, status: http.StatusOK})
}

func (m *dclClientMockFetch) callLog() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.calls)
}

func (m *dclClientMockFetch) RoundTrip(r *http.Request) (*http.Response, error) {
	u := r.URL.String()
	m.mu.Lock()
	m.calls = append(m.calls, u)
	var hit *dclClientMockResponse
	for i := len(m.responses) - 1; i >= 0; i-- {
		resp := &m.responses[i]
		if resp.re != nil && resp.re.MatchString(u) || resp.re == nil && strings.Contains(u, resp.sub) {
			hit = resp
			break
		}
	}
	m.mu.Unlock()
	if hit == nil {
		return nil, fmt.Errorf("no mock response for %s", u)
	}
	body, err := json.Marshal(hit.body)
	if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: hit.status, Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(bytes.NewReader(body)), Request: r,
	}, nil
}

// dclClientWith is a client of baseURL whose requests go to m.
func dclClientWith(m *dclClientMockFetch, baseURL string) *Client {
	return &Client{BaseURL: baseURL, HTTPClient: &http.Client{Transport: m}}
}

// dclClientWantError wants err to be a client *Error containing msg —
// rejectedWith(MatterDclError, msg).
func dclClientWantError(t *testing.T, err error, msg string) {
	t.Helper()
	var de *Error
	if !errors.As(err, &de) || !strings.Contains(de.Message, msg) {
		t.Fatalf("got %v, want a dcl.Error containing %q", err, msg)
	}
}

func dclClientEmptyRoots() map[string]any {
	return map[string]any{"approvedRootCertificates": map[string]any{"schemaVersion": 0, "certs": []any{}}}
}

// TestDclClientConstructor: describe("constructor") — the ledger URL the
// client asks (DclClientTest.ts:24-77). DclConfig is Client.BaseURL here.
func TestDclClientConstructor(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cases := []struct {
		name, base, pattern, host string
	}{
		{"defaults to production URL when no config provided", "", "on.dcl.csa-iot.org/dcl/pki/root-certificates", "on.dcl.csa-iot.org"},
		{"uses production config explicitly", ProductionURL, "on.dcl.csa-iot.org/dcl/pki/root-certificates", "on.dcl.csa-iot.org"},
		{"uses test config", TestNetURL, "on.test-net.dcl.csa-iot.org/dcl/pki/root-certificates", "on.test-net.dcl.csa-iot.org"},
		{"uses custom URL", "https://custom.dcl.local", "custom.dcl.local/dcl/pki/root-certificates", "custom.dcl.local"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := &dclClientMockFetch{}
			m.addResponse(tc.pattern, dclClientEmptyRoots())
			if _, err := dclClientWith(m, tc.base).RootCertificates(ctx); err != nil {
				t.Fatal(err)
			}
			calls := m.callLog()
			if len(calls) != 1 || !strings.Contains(calls[0], tc.host) {
				t.Fatalf("call log %v, want one call to %s", calls, tc.host)
			}
		})
	}
}

// TestDclClientFetchRootCertificateList: describe("fetchRootCertificateList")
// — the case the older TestClientRoots/TestClientSchemaVersions do not
// carry (DclClientTest.ts:114-124).
func TestDclClientFetchRootCertificateList(t *testing.T) {
	t.Parallel()
	t.Run("throws on HTTP error", func(t *testing.T) {
		t.Parallel()
		m := &dclClientMockFetch{}
		m.addStatus("/dcl/pki/root-certificates", map[string]any{"code": 500, "message": "Internal Server Error", "details": []any{}}, 500)
		_, err := dclClientWith(m, "").RootCertificates(context.Background())
		var re *ResponseError
		if !errors.As(err, &re) || re.Code != 500 || re.Message != "Internal Server Error" {
			t.Fatalf("got %v, want a ResponseError", err)
		}
	})
}

// TestDclClientFetchRootCertificateBySubject: describe(
// "fetchRootCertificateBySubject") (DclClientTest.ts:127-189).
func TestDclClientFetchRootCertificateBySubject(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	t.Run("fetches certificate by subject reference", func(t *testing.T) {
		t.Parallel()
		subject, skid := "testSubject", "testKeyId"
		m := &dclClientMockFetch{}
		m.addResponse("/dcl/pki/certificates/"+encodeURIComponent(subject)+"/"+encodeURIComponent(skid), map[string]any{
			"approvedCertificates": map[string]any{
				"subject": subject, "subjectKeyId": skid, "schemaVersion": 0,
				"certs": []any{map[string]any{"pemCert": "-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----", "schemaVersion": 0}},
			},
		})
		certs, err := dclClientWith(m, "").RootCertificateBySubject(ctx, SubjectReference{Subject: subject, SubjectKeyID: skid})
		if err != nil || len(certs) != 1 {
			t.Fatalf("certs %+v (%v)", certs, err)
		}
	})
	for _, tc := range []struct{ name, subject, skid string }{
		{"throws when subject mismatch", "wrongSubject", "testKeyId"},
		{"throws when subjectKeyId mismatch", "testSubject", "wrongKeyId"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := &dclClientMockFetch{}
			m.addRegexp(regexp.MustCompile(`/dcl/pki/certificates/`), map[string]any{
				"approvedCertificates": map[string]any{"subject": tc.subject, "subjectKeyId": tc.skid, "schemaVersion": 0, "certs": []any{}},
			})
			_, err := dclClientWith(m, "").RootCertificateBySubject(ctx, SubjectReference{Subject: "testSubject", SubjectKeyID: "testKeyId"})
			dclClientWantError(t, err, "Root certificate not found")
		})
	}
}

// TestDclClientFetchModelByVidPid: describe("fetchModelByVidPid")
// (DclClientTest.ts:191-227).
func TestDclClientFetchModelByVidPid(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	t.Run("fetches model info", func(t *testing.T) {
		t.Parallel()
		m := &dclClientMockFetch{}
		m.addResponse("/dcl/model/models/65521/32768", map[string]any{"model": map[string]any{
			"vid": 0xfff1, "pid": 0x8000, "schemaVersion": 0, "deviceTypeId": 22, "productName": "Test Product",
		}})
		model, err := dclClientWith(m, "").Model(ctx, 0xfff1, 0x8000)
		if err != nil || model.VID != 0xfff1 || model.PID != 0x8000 {
			t.Fatalf("model %+v (%v)", model, err)
		}
	})
	t.Run("throws on VID/PID mismatch", func(t *testing.T) {
		t.Parallel()
		m := &dclClientMockFetch{}
		m.addResponse("/dcl/model/models/65521/32768", map[string]any{"model": map[string]any{"vid": 0xfff2, "pid": 0x8000, "schemaVersion": 0}})
		_, err := dclClientWith(m, "").Model(ctx, 0xfff1, 0x8000)
		dclClientWantError(t, err, "Model not found")
	})
}

// dclClientModelVersion is the modelVersion fixture of
// DclClientTest.ts:230-255 with overrides.
func dclClientModelVersion(overrides map[string]any) map[string]any {
	mv := map[string]any{
		"vid": 0xfff1, "pid": 0x8000, "softwareVersion": 6, "softwareVersionString": "6.0", "cdVersionNumber": 1,
		"creator": "cosmos1test", "firmwareInformation": "", "softwareVersionValid": true, "otaUrl": "",
		"otaFileSize": "0", "otaChecksum": "", "otaChecksumType": 0, "minApplicableSoftwareVersion": 0,
		"maxApplicableSoftwareVersion": 5, "releaseNotesUrl": "", "specificationVersion": 0, "schemaVersion": 0,
	}
	for k, v := range overrides {
		mv[k] = v
	}
	return map[string]any{"modelVersion": mv}
}

// TestDclClientFetchModelVersionByVidPidSoftwareVersion: describe(
// "fetchModelVersionByVidPidSoftwareVersion") (DclClientTest.ts:229-304).
func TestDclClientFetchModelVersionByVidPidSoftwareVersion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fetch := func(t *testing.T, overrides map[string]any) SoftwareVersion {
		t.Helper()
		m := &dclClientMockFetch{}
		m.addResponse("/dcl/model/versions/65521/32768/6", dclClientModelVersion(overrides))
		v, err := dclClientWith(m, "").ModelVersion(ctx, 0xfff1, 0x8000, 6)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	t.Run("converts the file size and keeps set OTA fields", func(t *testing.T) {
		t.Parallel()
		v := fetch(t, map[string]any{
			"otaUrl": "https://example.com/ota.bin", "otaFileSize": "100", "otaChecksum": "abc=", "otaChecksumType": 1,
			"releaseNotesUrl": "https://example.com/notes", "specificationVersion": 0x01040200,
		})
		if v.OTAFileSize != 100 || v.OTAURL != "https://example.com/ota.bin" || v.OTAChecksum != "abc=" || v.OTAChecksumType != 1 ||
			v.ReleaseNotesURL != "https://example.com/notes" || v.SpecificationVersion != 0x01040200 {
			t.Fatalf("version %+v", v)
		}
	})
	t.Run("returns fields the DCL sends empty as undefined", func(t *testing.T) {
		t.Parallel()
		v := fetch(t, nil)
		if v.OTAFileSize != 0 || v.OTAURL != "" || v.OTAChecksum != "" || v.OTAChecksumType != 0 || v.ReleaseNotesURL != "" ||
			v.FirmwareInformation != "" || v.SpecificationVersion != 0 {
			t.Fatalf("version %+v", v)
		}
	})
	t.Run("ignores a file size that is not a decimal number", func(t *testing.T) {
		t.Parallel()
		if v := fetch(t, map[string]any{"otaFileSize": "12ab"}); v.OTAFileSize != 0 {
			t.Fatalf("file size %d", v.OTAFileSize)
		}
	})
}

// dclClientComplianceRecord is the record of DclClientTest.ts:307-315.
func dclClientComplianceRecord(overrides map[string]any) map[string]any {
	r := map[string]any{
		"vid": 0xfff1, "pid": 0x8000, "softwareVersion": 6, "certificationType": "matter",
		"specificationVersion": 0x01040200, "history": []any{}, "schemaVersion": 1,
	}
	for k, v := range overrides {
		r[k] = v
	}
	return map[string]any{"complianceInfo": r}
}

const dclClientCompliancePath = "/dcl/compliance/compliance-info/65521/32768/6/matter"

// TestDclClientFetchComplianceInfo: describe("fetchComplianceInfo")
// (DclClientTest.ts:306-357).
func TestDclClientFetchComplianceInfo(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	t.Run("returns the compliance record", func(t *testing.T) {
		t.Parallel()
		m := &dclClientMockFetch{}
		m.addResponse(dclClientCompliancePath, dclClientComplianceRecord(nil))
		info, err := dclClientWith(m, "").ComplianceInfo(ctx, 0xfff1, 0x8000, 6, "matter")
		if err != nil || info.SpecificationVersion != 0x01040200 {
			t.Fatalf("info %+v (%v)", info, err)
		}
	})
	// DclClientTest.ts:326-331.
	for _, mm := range []struct {
		field string
		value any
	}{{"vid", 0xfff2}, {"pid", 0x8001}, {"softwareVersion", 7}, {"certificationType", "zigbee"}} {
		t.Run("rejects a record with a different "+mm.field, func(t *testing.T) {
			t.Parallel()
			m := &dclClientMockFetch{}
			m.addResponse(dclClientCompliancePath, dclClientComplianceRecord(map[string]any{mm.field: mm.value}))
			_, err := dclClientWith(m, "").ComplianceInfo(ctx, 0xfff1, 0x8000, 6, "matter")
			dclClientWantError(t, err, "Compliance info not found")
		})
	}
	t.Run("rejects a record with an unsupported schema version", func(t *testing.T) {
		t.Parallel()
		m := &dclClientMockFetch{}
		m.addResponse(dclClientCompliancePath, dclClientComplianceRecord(map[string]any{"schemaVersion": 2}))
		_, err := dclClientWith(m, "").ComplianceInfo(ctx, 0xfff1, 0x8000, 6, "matter")
		dclClientWantError(t, err, "Unsupported DCL compliance info schema version 2")
	})
}

// dclClientModelRecord is the model fixture of DclClientTest.ts:451-482
// and :504-535 with overrides.
func dclClientModelRecord(overrides map[string]any) map[string]any {
	r := map[string]any{
		"vid": 0xfff1, "pid": 0x8000, "deviceTypeId": 22, "productName": "Test Product", "productLabel": "Label",
		"partNumber": "P1", "commissioningCustomFlow": 0, "commissioningCustomFlowUrl": "",
		"commissioningModeInitialStepsHint": 1, "commissioningModeInitialStepsInstruction": "",
		"commissioningModeSecondaryStepsHint": 4, "commissioningModeSecondaryStepsInstruction": "",
		"commissioningFallbackUrl": "", "discoveryCapabilitiesBitmask": 4, "userManualUrl": "", "supportUrl": "",
		"productUrl": "", "lsfUrl": "", "lsfRevision": 0, "enhancedSetupFlowOptions": 0, "enhancedSetupFlowTCUrl": "",
		"enhancedSetupFlowTCRevision": 0, "enhancedSetupFlowTCDigest": "", "enhancedSetupFlowTCFileSize": 0,
		"maintenanceUrl": "", "icdUserActiveModeTriggerHint": 0, "icdUserActiveModeTriggerInstruction": "",
		"factoryResetStepsHint": 0, "factoryResetStepsInstruction": "", "creator": "cosmos1test", "schemaVersion": 0,
	}
	for k, v := range overrides {
		r[k] = v
	}
	return map[string]any{"model": r}
}

// TestDclClientRecordsWithUnsetOptionalFields: describe("records with unset
// optional fields") (DclClientTest.ts:359-606).
func TestDclClientRecordsWithUnsetOptionalFields(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("returns empty vendor fields as undefined and skips unsupported schema versions", func(t *testing.T) {
		t.Parallel()
		m := &dclClientMockFetch{}
		m.addResponse("/dcl/vendorinfo/vendors", map[string]any{
			"vendorInfo": []any{
				map[string]any{
					"vendorID": 1, "vendorName": "Vendor1", "companyLegalName": "Vendor One Inc.", "companyPreferredName": "",
					"vendorLandingPageURL": "", "creator": "cosmos1test", "schemaVersion": 0,
				},
				map[string]any{"vendorID": 2, "vendorName": "Vendor2", "schemaVersion": 1},
			},
			"pagination": map[string]any{},
		})
		vendors, err := dclClientWith(m, "").Vendors(ctx)
		if err != nil || len(vendors) != 1 || vendors[0].VendorID != 1 {
			t.Fatalf("vendors %+v (%v)", vendors, err)
		}
		if vendors[0].CompanyPreferredName != "" || vendors[0].VendorLandingPageURL != "" {
			t.Fatalf("vendor %+v", vendors[0])
		}
	})

	t.Run("returns empty certificate fields as undefined and skips unsupported schema versions", func(t *testing.T) {
		t.Parallel()
		cert := map[string]any{
			"pemCert": "-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----", "serialNumber": "1", "issuer": "",
			"authorityKeyId": "", "rootSubject": "", "rootSubjectKeyId": "", "isRoot": true, "owner": "cosmos1test",
			"subject": "MB8x", "subjectAsText": "CN=Test PAA", "subjectKeyId": "AA:BB", "approvals": []any{}, "rejects": []any{},
			"vid": 0xfff1, "certificateType": "DeviceAttestationPKI", "schemaVersion": 0,
		}
		newer := map[string]any{}
		for k, v := range cert {
			newer[k] = v
		}
		newer["schemaVersion"] = 1
		m := &dclClientMockFetch{}
		m.addResponse("/dcl/pki/all-certificates?subjectKeyId=AA%3ABB", map[string]any{"certificates": []any{
			map[string]any{"subject": "MB8x", "subjectKeyId": "AA:BB", "certs": []any{cert, newer}, "schemaVersion": 0},
			map[string]any{"subject": "MB8y", "subjectKeyId": "AA:BB", "certs": []any{cert}, "schemaVersion": 1},
		}})
		certs, err := dclClientWith(m, "").CertificatesBySubjectKeyID(ctx, "AABB")
		if err != nil || len(certs) != 1 {
			t.Fatalf("certs %+v (%v)", certs, err)
		}
		if c := certs[0]; c.Issuer != "" || c.AuthorityKeyID != "" || c.RootSubject != "" || c.RootSubjectKeyID != "" {
			t.Fatalf("cert %+v", c)
		}
	})

	t.Run("skips root certificates with an unsupported schema version", func(t *testing.T) {
		t.Parallel()
		m := &dclClientMockFetch{}
		m.addResponse("/dcl/pki/certificates/MB8x/AA%3ABB", map[string]any{"approvedCertificates": map[string]any{
			"subject": "MB8x", "subjectKeyId": "AA:BB", "schemaVersion": 0,
			"certs": []any{
				map[string]any{"pemCert": "cert0", "issuer": "", "schemaVersion": 0},
				map[string]any{"pemCert": "cert1", "issuer": "", "schemaVersion": 1},
			},
		}})
		certs, err := dclClientWith(m, "").RootCertificateBySubject(ctx, SubjectReference{Subject: "MB8x", SubjectKeyID: "AA:BB"})
		if err != nil || len(certs) != 1 || certs[0].PEMCert != "cert0" || certs[0].Issuer != "" {
			t.Fatalf("certs %+v (%v)", certs, err)
		}
	})

	t.Run("returns empty model fields as undefined and keeps revisions only with their URL", func(t *testing.T) {
		t.Parallel()
		m := &dclClientMockFetch{}
		m.addResponse("/dcl/model/models/65521/32768", dclClientModelRecord(map[string]any{
			"supportUrl": "https://example.com/support", "lsfUrl": "https://example.com/lsf.json", "lsfRevision": 2,
			"enhancedSetupFlowTCRevision": 3, "enhancedSetupFlowTCFileSize": 10,
		}))
		model, err := dclClientWith(m, "").Model(ctx, 0xfff1, 0x8000)
		if err != nil {
			t.Fatal(err)
		}
		if model.SupportURL != "https://example.com/support" || model.LSFURL != "https://example.com/lsf.json" || model.LSFRevision != 2 ||
			model.UserManualURL != "" || model.CommissioningCustomFlowURL != "" || model.CommissioningModeInitialStepsInstruction != "" ||
			model.MaintenanceURL != "" || model.EnhancedSetupFlowTCURL != "" || model.EnhancedSetupFlowTCRevision != 0 ||
			model.EnhancedSetupFlowTCFileSize != 0 || model.DiscoveryCapabilitiesBitmask != 4 {
			t.Fatalf("model %+v", model)
		}
	})

	t.Run("keeps Terms and Conditions details when their URL is set", func(t *testing.T) {
		t.Parallel()
		m := &dclClientMockFetch{}
		m.addResponse("/dcl/model/models/65521/32768", dclClientModelRecord(map[string]any{
			"enhancedSetupFlowOptions": 1, "enhancedSetupFlowTCUrl": "https://example.com/tc.json", "enhancedSetupFlowTCRevision": 3,
			"enhancedSetupFlowTCDigest": "abc=", "enhancedSetupFlowTCFileSize": 10, "maintenanceUrl": "https://example.com/maintenance",
		}))
		model, err := dclClientWith(m, "").Model(ctx, 0xfff1, 0x8000)
		if err != nil {
			t.Fatal(err)
		}
		if model.EnhancedSetupFlowTCURL != "https://example.com/tc.json" || model.EnhancedSetupFlowTCRevision != 3 ||
			model.EnhancedSetupFlowTCDigest != "abc=" || model.EnhancedSetupFlowTCFileSize != 10 || model.LSFRevision != 0 ||
			model.ICDUserActiveModeTriggerInstruction != "" || model.FactoryResetStepsInstruction != "" {
			t.Fatalf("model %+v", model)
		}
	})

	t.Run("returns empty compliance fields as undefined", func(t *testing.T) {
		t.Parallel()
		m := &dclClientMockFetch{}
		m.addResponse(dclClientCompliancePath, map[string]any{"complianceInfo": map[string]any{
			"vid": 0xfff1, "pid": 0x8000, "softwareVersion": 6, "certificationType": "matter", "certificationRoute": "",
			"reason": "", "transport": "", "compliantPlatformUsed": "", "compliantPlatformVersion": "",
			"certificationIdOfSoftwareComponent": "", "OSVersion": "", "familyId": "", "programType": "endProduct",
			"programTypeVersion": "", "parentChild": "", "supportedClusters": "",
			"history": []any{map[string]any{
				"softwareVersionCertificationStatus": 2, "date": "2025-01-01T00:00:00Z", "reason": "", "cDVersionNumber": 1, "schemaVersion": 0,
			}},
			"specificationVersion": 0, "schemaVersion": 0,
		}})
		info, err := dclClientWith(m, "").ComplianceInfo(ctx, 0xfff1, 0x8000, 6, "matter")
		if err != nil {
			t.Fatal(err)
		}
		if info.Transport != "" || info.SpecificationVersion != 0 || info.SupportedClusters != "" || info.ProgramTypeVersion != "" ||
			info.ParentChild != "" || info.ProgramType != "endProduct" || info.Reason != "" || info.CertificationRoute != "" ||
			info.FamilyID != "" || len(info.History) != 1 || info.History[0].Reason != "" {
			t.Fatalf("info %+v", info)
		}
		// The deprecated fields are not part of the record (:592-599).
		typ := reflect.TypeFor[ComplianceInfo]()
		for _, deprecated := range []string{"CompliantPlatformUsed", "CompliantPlatformVersion", "CertificationIDOfSoftwareComponent", "OSVersion"} {
			if _, ok := typ.FieldByName(deprecated); ok {
				t.Errorf("ComplianceInfo has the deprecated field %s", deprecated)
			}
		}
	})
}

// TestDclClientFetchAllVendors: describe("fetchAllVendors")
// (DclClientTest.ts:608-630).
func TestDclClientFetchAllVendors(t *testing.T) {
	t.Parallel()
	t.Run("handles pagination correctly", func(t *testing.T) {
		t.Parallel()
		m := &dclClientMockFetch{}
		m.addResponse("/dcl/vendorinfo/vendors", map[string]any{
			"vendorInfo": []any{
				map[string]any{"vendorID": 1, "vendorName": "Vendor1", "schemaVersion": 0},
				map[string]any{"vendorID": 2, "vendorName": "Vendor2", "schemaVersion": 0},
			},
			"pagination": map[string]any{"next_key": "page2"},
		})
		m.addResponse("pagination.key=page2", map[string]any{
			"vendorInfo": []any{map[string]any{"vendorID": 3, "vendorName": "Vendor3", "schemaVersion": 0}},
			"pagination": map[string]any{},
		})
		vendors, err := dclClientWith(m, "").Vendors(context.Background())
		if err != nil || len(vendors) != 3 || vendors[0].VendorID != 1 || vendors[2].VendorID != 3 {
			t.Fatalf("vendors %+v (%v)", vendors, err)
		}
	})
}

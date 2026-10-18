// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl

// A port of matter.js packages/protocol/test/dcl/DclOtaUpdateServiceTest.ts
// at 862ad75f: the ledger lookup of an applicable update, its compliance
// record's specification version, the download and the image's
// verification. The service's image storage (find, delete, migration,
// offline) is the host's in Go and is not ported.
//
// The Go surface: checkForUpdate's isProduction is which ledger client
// OTAVersions carries (true: Client only, false: TestClient only), and
// downloadUpdate is Download followed by VerifyImage
// (dclOtaDownloadUpdate). The HTTP traffic runs on dclOtaMockFetch, a
// transport that mirrors matter.js MockFetch: no socket is opened.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/bdx/otaimage"
)

// dclOtaMock is one MockFetch response (MockFetch.ts:68-80).
type dclOtaMock struct {
	url    string
	status int
	body   []byte
}

// dclOtaMockFetch mirrors matter.js MockFetch (packages/general/src/util/
// MockFetch.ts:140-190): the most recently added response whose URL the
// request URL includes answers; none is a failed request.
type dclOtaMockFetch struct {
	mu    sync.Mutex
	mocks []dclOtaMock
	calls []string
}

func (m *dclOtaMockFetch) addJSON(url string, body any, status ...int) {
	b, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	m.addBytes(url, b, status...)
}

func (m *dclOtaMockFetch) addBytes(url string, body []byte, status ...int) {
	s := http.StatusOK
	if len(status) > 0 {
		s = status[0]
	}
	m.mu.Lock()
	m.mocks = append(m.mocks, dclOtaMock{url: url, status: s, body: body})
	m.mu.Unlock()
}

func (m *dclOtaMockFetch) callLog() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.calls...)
}

func (m *dclOtaMockFetch) RoundTrip(r *http.Request) (*http.Response, error) {
	u := r.URL.String()
	m.mu.Lock()
	m.calls = append(m.calls, u)
	var found *dclOtaMock
	for i := len(m.mocks) - 1; i >= 0; i-- {
		if strings.Contains(u, m.mocks[i].url) {
			found = &m.mocks[i]
			break
		}
	}
	m.mu.Unlock()
	if found == nil {
		return nil, fmt.Errorf("No mock found for URL: %s", u)
	}
	return &http.Response{
		StatusCode: found.status, Status: fmt.Sprintf("%d %s", found.status, http.StatusText(found.status)),
		Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(found.body)), Request: r,
	}, nil
}

func (m *dclOtaMockFetch) httpClient() *http.Client { return &http.Client{Transport: m} }

// dclOtaService is `new DclOtaUpdateService(environment)` asked with
// isProduction true (Client against DclConfig.production) or false
// (TestClient against DclConfig.test).
func dclOtaService(m *dclOtaMockFetch, production bool) *OTAVersions {
	if production {
		return &OTAVersions{Client: &Client{HTTPClient: m.httpClient()}}
	}
	return &OTAVersions{TestClient: &Client{BaseURL: TestNetURL, HTTPClient: m.httpClient()}}
}

// dclOtaDownloader is the service downloadUpdate runs on: the mock carries
// the image download.
func dclOtaDownloader(m *dclOtaMockFetch) *OTAVersions {
	return &OTAVersions{Client: &Client{HTTPClient: m.httpClient()}}
}

// dclOtaDownloadUpdate is downloadUpdate's network half and #verifyUpdate
// (DclOtaUpdateService.ts:388-424, :667-709) on the Go surface.
func dclOtaDownloadUpdate(ctx context.Context, o *OTAVersions, u SoftwareVersion) (otaimage.Header, error) {
	var buf bytes.Buffer
	if err := o.Download(ctx, u, &buf); err != nil {
		return otaimage.Header{}, err
	}
	return VerifyImage(bytes.NewReader(buf.Bytes()), u, nil)
}

// dclOtaImage is createOtaImage (dcl-ota-test-helpers.ts:15-32).
func dclOtaImage(t *testing.T, vid, pid uint16, version uint32) []byte {
	t.Helper()
	lo := version - 1
	img, err := otaimage.Create(otaimage.CreateOptions{
		VendorID: vid, ProductID: pid, SoftwareVersion: version, SoftwareVersionString: fmt.Sprintf("v%d.0.0", version),
		MinApplicableSoftwareVersion: &lo, MaxApplicableSoftwareVersion: &lo, Payload: []byte{1, 2, 3, 4},
	})
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// dclOtaVersionsList is mockVersionsList (dcl-ota-test-helpers.ts:37-44).
var dclOtaVersionsList = map[string]any{"modelVersions": map[string]any{
	"vid": 0xfff1, "pid": 0x8000, "softwareVersions": []int{1, 2, 3}, "schemaVersion": 0,
}}

// dclOtaVersionMetadata is createVersionMetadata
// (dcl-ota-test-helpers.ts:49-77).
func dclOtaVersionMetadata(version int, valid, hasOTA bool, overrides map[string]any) map[string]any {
	url := ""
	if hasOTA {
		url = fmt.Sprintf("https://example.com/ota-v%d.bin", version)
	}
	r := map[string]any{
		"vid": 0xfff1, "pid": 0x8000, "softwareVersion": version, "softwareVersionString": fmt.Sprintf("v%d.0.0", version),
		"cdVersionNumber": 1, "creator": "cosmos1test", "firmwareInformation": "", "softwareVersionValid": valid,
		"otaUrl": url, "otaFileSize": "1024", "otaChecksum": "checksum123", "otaChecksumType": 1,
		"minApplicableSoftwareVersion": version - 1, "maxApplicableSoftwareVersion": version - 1,
		"releaseNotesUrl":      fmt.Sprintf("https://example.com/release-notes-v%d", version),
		"specificationVersion": 0, "schemaVersion": 0,
	}
	for k, v := range overrides {
		r[k] = v
	}
	return map[string]any{"modelVersion": r}
}

func dclOtaMeta(version int) map[string]any { return dclOtaVersionMetadata(version, true, true, nil) }

// dclOtaUpdateInfo is the updateInfo of the downloadUpdate cases
// (DclOtaUpdateServiceTest.ts:562-575): version 3, applicable over 2.
func dclOtaUpdateInfo(url string, size int) SoftwareVersion {
	return SoftwareVersion{
		VID: 0xfff1, PID: 0x8000, SoftwareVersion: 3, SoftwareVersionString: "v3.0.0", CDVersionNumber: 1,
		SoftwareVersionValid: true, OTAURL: url, OTAFileSize: uint64(size), //nolint:gosec // a test image's length
		MinApplicableSoftwareVersion: 2, MaxApplicableSoftwareVersion: 2,
	}
}

func dclOtaU32(v uint32) *uint32 { return &v }

func TestDclOtaUpdateServiceCheckForUpdate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const list, v2, v3 = "/dcl/model/versions/65521/32768", "/dcl/model/versions/65521/32768/2", "/dcl/model/versions/65521/32768/3"
	const compliance2 = "/dcl/compliance/compliance-info/65521/32768/2/matter"
	check := func(o *OTAVersions, current uint32, target *uint32) (OTAUpdate, bool) {
		return o.CheckForUpdate(ctx, 0xfff1, 0x8000, current, target)
	}

	t.Run("finds available update when newer version exists", func(t *testing.T) {
		t.Parallel()
		m := &dclOtaMockFetch{}
		m.addJSON(list, dclOtaVersionsList)
		m.addJSON(v3, dclOtaMeta(3))
		u, ok := check(dclOtaService(m, true), 2, nil)
		if !ok || u.SoftwareVersion.SoftwareVersion != 3 || u.SoftwareVersionString != "v3.0.0" ||
			u.OTAURL != "https://example.com/ota-v3.bin" || u.OTAFileSize != 1024 {
			t.Fatalf("%+v %v", u, ok)
		}
	})

	t.Run("returns undefined when no newer versions available", func(t *testing.T) {
		t.Parallel()
		m := &dclOtaMockFetch{}
		m.addJSON(list, dclOtaVersionsList)
		if u, ok := check(dclOtaService(m, true), 3, nil); ok {
			t.Fatalf("%+v", u)
		}
	})

	t.Run("skips invalid versions and finds next valid one", func(t *testing.T) {
		t.Parallel()
		m := &dclOtaMockFetch{}
		m.addJSON(list, dclOtaVersionsList)
		m.addJSON(v3, dclOtaVersionMetadata(3, false, true, nil))
		m.addJSON(v2, dclOtaVersionMetadata(2, true, true, nil))
		if u, ok := check(dclOtaService(m, true), 1, nil); !ok || u.SoftwareVersion.SoftwareVersion != 2 {
			t.Fatalf("%+v %v", u, ok)
		}
	})

	t.Run("skips versions without OTA URL", func(t *testing.T) {
		t.Parallel()
		m := &dclOtaMockFetch{}
		m.addJSON(list, dclOtaVersionsList)
		m.addJSON(v3, dclOtaVersionMetadata(3, true, false, nil))
		m.addJSON(v2, dclOtaVersionMetadata(2, true, true, nil))
		if u, ok := check(dclOtaService(m, true), 1, nil); !ok || u.SoftwareVersion.SoftwareVersion != 2 {
			t.Fatalf("%+v %v", u, ok)
		}
	})

	t.Run("returns undefined when all newer versions are invalid", func(t *testing.T) {
		t.Parallel()
		m := &dclOtaMockFetch{}
		m.addJSON(list, dclOtaVersionsList)
		m.addJSON(v3, dclOtaVersionMetadata(3, false, true, nil))
		m.addJSON(v2, dclOtaVersionMetadata(2, false, true, nil))
		if u, ok := check(dclOtaService(m, true), 1, nil); ok {
			t.Fatalf("%+v", u)
		}
	})

	t.Run("checks version applicability range", func(t *testing.T) {
		t.Parallel()
		m := &dclOtaMockFetch{}
		m.addJSON(list, dclOtaVersionsList)
		m.addJSON(v3, dclOtaVersionMetadata(3, true, true, map[string]any{"minApplicableSoftwareVersion": 2, "maxApplicableSoftwareVersion": 2}))
		m.addJSON(v2, dclOtaVersionMetadata(2, true, true, map[string]any{"minApplicableSoftwareVersion": 0, "maxApplicableSoftwareVersion": 0}))
		o := dclOtaService(m, true)
		if u, ok := check(o, 2, nil); !ok || u.SoftwareVersion.SoftwareVersion != 3 {
			t.Fatalf("current 2: %+v %v", u, ok)
		}
		if u, ok := check(o, 1, nil); ok {
			t.Fatalf("current 1: %+v", u)
		}
	})

	t.Run("returns no update on DCL fetch failure", func(t *testing.T) {
		t.Parallel()
		m := &dclOtaMockFetch{}
		m.addJSON(list, map[string]any{"error": "Not found"}, http.StatusNotFound)
		if u, ok := check(dclOtaService(m, true), 1, nil); ok {
			t.Fatalf("%+v", u)
		}
	})

	t.Run("uses test DCL when production is false", func(t *testing.T) {
		t.Parallel()
		m := &dclOtaMockFetch{}
		m.addJSON("test-net.dcl.csa-iot.org"+list, dclOtaVersionsList)
		m.addJSON("test-net.dcl.csa-iot.org"+v3, dclOtaMeta(3))
		if _, ok := check(dclOtaService(m, false), 2, nil); !ok {
			t.Fatal("no update")
		}
		if !dclOtaAny(m.callLog(), func(u string) bool { return strings.Contains(u, "test-net") }) {
			t.Fatalf("calls %v", m.callLog())
		}
	})

	t.Run("checks for a specific target version when provided", func(t *testing.T) {
		t.Parallel()
		m := &dclOtaMockFetch{}
		m.addJSON(v2, dclOtaMeta(2))
		if u, ok := check(dclOtaService(m, true), 1, dclOtaU32(2)); !ok || u.SoftwareVersion.SoftwareVersion != 2 {
			t.Fatalf("%+v %v", u, ok)
		}
		calls := m.callLog()
		if len(calls) == 0 || !strings.Contains(calls[0], v2) || dclOtaAny(calls, func(u string) bool { return strings.HasSuffix(u, list) }) {
			t.Fatalf("calls %v", calls)
		}
	})

	complianceInfo := func(spec, schema int) map[string]any {
		return map[string]any{"complianceInfo": map[string]any{
			"vid": 0xfff1, "pid": 0x8000, "softwareVersion": 2, "certificationType": "matter",
			"history": []any{}, "specificationVersion": spec, "schemaVersion": schema,
		}}
	}
	targeted := func(t *testing.T, m *dclOtaMockFetch) OTAUpdate {
		t.Helper()
		u, ok := check(dclOtaService(m, true), 1, dclOtaU32(2))
		if !ok || u.SoftwareVersion.SoftwareVersion != 2 {
			t.Fatalf("%+v %v", u, ok)
		}
		return u
	}

	t.Run("takes the specification version from the compliance record", func(t *testing.T) {
		t.Parallel()
		m := &dclOtaMockFetch{}
		m.addJSON(v2, dclOtaMeta(2))
		m.addJSON(compliance2, complianceInfo(0x01040200, 1))
		if u := targeted(t, m); u.SpecificationVersion != 0x01040200 {
			t.Fatalf("specification version %#x", u.SpecificationVersion)
		}
	})

	t.Run("returns the update without specification version when the compliance record is missing", func(t *testing.T) {
		t.Parallel()
		m := &dclOtaMockFetch{}
		m.addJSON(v2, dclOtaMeta(2))
		m.addJSON(compliance2, map[string]any{"code": 5, "message": "rpc error: not found", "details": []any{}}, http.StatusNotFound)
		if u := targeted(t, m); u.SpecificationVersion != 0 {
			t.Fatalf("specification version %#x", u.SpecificationVersion)
		}
	})

	t.Run("treats a zero specification version in the compliance record as unknown", func(t *testing.T) {
		t.Parallel()
		m := &dclOtaMockFetch{}
		m.addJSON(v2, dclOtaMeta(2))
		m.addJSON(compliance2, complianceInfo(0, 0))
		if u := targeted(t, m); u.SpecificationVersion != 0 {
			t.Fatalf("specification version %#x", u.SpecificationVersion)
		}
	})

	t.Run("ignores a compliance record with an unsupported schema version", func(t *testing.T) {
		t.Parallel()
		m := &dclOtaMockFetch{}
		m.addJSON(v2, dclOtaMeta(2))
		m.addJSON(compliance2, complianceInfo(0x01040200, 2))
		if u := targeted(t, m); u.SpecificationVersion != 0 {
			t.Fatalf("specification version %#x", u.SpecificationVersion)
		}
	})

	t.Run("returns undefined when target version is not applicable", func(t *testing.T) {
		t.Parallel()
		m := &dclOtaMockFetch{}
		m.addJSON(v3, dclOtaVersionMetadata(3, true, true, map[string]any{"minApplicableSoftwareVersion": 2, "maxApplicableSoftwareVersion": 2}))
		if u, ok := check(dclOtaService(m, true), 1, dclOtaU32(3)); ok {
			t.Fatalf("%+v", u)
		}
	})

	t.Run("rejects DCL versions with non-HTTPS OTA URLs", func(t *testing.T) {
		t.Parallel()
		m := &dclOtaMockFetch{}
		m.addJSON(list, dclOtaVersionsList)
		m.addJSON(v3, dclOtaVersionMetadata(3, true, true, map[string]any{"otaUrl": "http://insecure.example.com/ota-v3.bin"}))
		if u, ok := check(dclOtaService(m, true), 2, nil); ok {
			t.Fatalf("%+v", u)
		}
	})

	t.Run("rejects DCL versions with file:// protocol URLs", func(t *testing.T) {
		t.Parallel()
		m := &dclOtaMockFetch{}
		m.addJSON(list, map[string]any{"modelVersions": map[string]any{
			"vid": 0xfff1, "pid": 0x8000, "softwareVersions": []int{2}, "schemaVersion": 0,
		}})
		m.addJSON(v2, dclOtaVersionMetadata(2, true, true, map[string]any{"otaUrl": "file:///tmp/ota-v2.bin"}))
		if u, ok := check(dclOtaService(m, true), 1, nil); ok {
			t.Fatalf("%+v", u)
		}
	})
}

func dclOtaAny(calls []string, f func(string) bool) bool {
	for _, c := range calls {
		if f(c) {
			return true
		}
	}
	return false
}

func TestDclOtaUpdateServiceDownloadUpdate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const url = "https://example.com/ota-v3.bin"
	wantOTAError := func(t *testing.T, err error, want string) {
		t.Helper()
		var oe *OTAError
		if !errors.As(err, &oe) || !strings.Contains(err.Error(), want) {
			t.Fatalf("want an OTAError %q, got %v", want, err)
		}
	}

	// The stored file name (ota/fff1.8000.prod.3) is the host's image
	// store in Go; the case holds the download and the validation.
	t.Run("downloads and validates OTA image successfully", func(t *testing.T) {
		t.Parallel()
		img := dclOtaImage(t, 0xfff1, 0x8000, 3)
		m := &dclOtaMockFetch{}
		m.addBytes(url, img)
		h, err := dclOtaDownloadUpdate(ctx, dclOtaDownloader(m), dclOtaUpdateInfo(url, len(img)))
		if err != nil || h.SoftwareVersion != 3 || h.VendorID != 0xfff1 || h.ProductID != 0x8000 {
			t.Fatalf("%+v %v", h, err)
		}
	})

	t.Run("throws error on download failure", func(t *testing.T) {
		t.Parallel()
		m := &dclOtaMockFetch{}
		m.addJSON(url, map[string]any{"error": "Not found"}, http.StatusNotFound)
		_, err := dclOtaDownloadUpdate(ctx, dclOtaDownloader(m), dclOtaUpdateInfo(url, 0))
		wantOTAError(t, err, "Failed to download OTA image")
	})

	for _, tc := range []struct {
		name     string
		vid, pid uint16
		version  uint32
		want     string
	}{
		{"throws error on vendor ID mismatch", 0xfff2, 0x8000, 3, "vendor ID mismatch"},
		{"throws error on product ID mismatch", 0xfff1, 0x8001, 3, "product ID mismatch"},
		{"throws error on software version mismatch", 0xfff1, 0x8000, 4, "software version mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			img := dclOtaImage(t, tc.vid, tc.pid, tc.version)
			m := &dclOtaMockFetch{}
			m.addBytes(url, img)
			_, err := dclOtaDownloadUpdate(ctx, dclOtaDownloader(m), dclOtaUpdateInfo(url, len(img)))
			wantOTAError(t, err, tc.want)
		})
	}

	t.Run("validates OTA image digest", func(t *testing.T) {
		t.Parallel()
		// DclOtaUpdateServiceTest.ts:711-716: a prefix stating 100 bytes
		// and a 10-byte header, then 90 bytes of garbage.
		bad := make([]byte, 16+90)
		copy(bad, []byte{0x1e, 0xf1, 0xee, 0x1b, 100, 0, 0, 0, 0, 0, 0, 0, 10, 0, 0, 0})
		m := &dclOtaMockFetch{}
		m.addBytes(url, bad)
		if _, err := dclOtaDownloadUpdate(ctx, dclOtaDownloader(m), dclOtaUpdateInfo(url, len(bad))); err == nil {
			t.Fatal("a garbage image was accepted")
		}
	})

	t.Run("errors when trying to download file:// protocol for local files", func(t *testing.T) {
		t.Parallel()
		img := dclOtaImage(t, 0xfff1, 0x8000, 3)
		m := &dclOtaMockFetch{}
		m.addBytes("file:///tmp/ota-v3.bin", img)
		_, err := dclOtaDownloadUpdate(ctx, dclOtaDownloader(m), dclOtaUpdateInfo("file:///tmp/ota-v3.bin", len(img)))
		if err == nil || !strings.Contains(err.Error(), `Unsupported protocol "file:"`) {
			t.Fatal(err)
		}
		if calls := m.callLog(); len(calls) != 0 {
			t.Fatalf("fetched %v", calls)
		}
	})

	t.Run("throws error for unsupported protocols", func(t *testing.T) {
		t.Parallel()
		_, err := dclOtaDownloadUpdate(ctx, dclOtaDownloader(&dclOtaMockFetch{}), dclOtaUpdateInfo("ftp://example.com/ota-v3.bin", 0))
		wantOTAError(t, err, `Unsupported protocol "ftp:" in OTA URL`)
	})

	t.Run("throws error for invalid URLs", func(t *testing.T) {
		t.Parallel()
		_, err := dclOtaDownloadUpdate(ctx, dclOtaDownloader(&dclOtaMockFetch{}), dclOtaUpdateInfo("not-a-valid-url", 0))
		wantOTAError(t, err, "Invalid OTA URL")
	})
}

func TestDclOtaUpdateServiceIntegration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const list, v3, url = "/dcl/model/versions/65521/32768", "/dcl/model/versions/65521/32768/3", "https://example.com/ota-v3.bin"

	t.Run("checks for update and downloads it", func(t *testing.T) {
		t.Parallel()
		res, err := otaimage.CreateWithChecksum(otaimage.CreateOptions{
			VendorID: 0xfff1, ProductID: 0x8000, SoftwareVersion: 3, SoftwareVersionString: "v3.0.0",
			MinApplicableSoftwareVersion: dclOtaU32(2), MaxApplicableSoftwareVersion: dclOtaU32(2), Payload: []byte{1, 2, 3, 4},
		})
		if err != nil {
			t.Fatal(err)
		}
		m := &dclOtaMockFetch{}
		m.addJSON(list, dclOtaVersionsList)
		m.addJSON(v3, dclOtaVersionMetadata(3, true, true, map[string]any{
			"otaFileSize": strconv.Itoa(len(res.Image)), "otaChecksum": res.FullFileChecksum, "otaChecksumType": res.FullFileChecksumType,
		}))
		m.addBytes(url, res.Image)
		o := dclOtaService(m, true)
		u, ok := o.CheckForUpdate(ctx, 0xfff1, 0x8000, 2, nil)
		if !ok || u.OTAFileSize != uint64(len(res.Image)) {
			t.Fatalf("%+v %v", u, ok)
		}
		// The stored file name is the host's image store in Go.
		if h, err := dclOtaDownloadUpdate(ctx, o, u.SoftwareVersion); err != nil || h.SoftwareVersion != 3 {
			t.Fatalf("%+v %v", h, err)
		}
	})

	t.Run("rejects an unsupported OTA checksum type with an OtaUpdateError", func(t *testing.T) {
		t.Parallel()
		res, err := otaimage.CreateWithChecksum(otaimage.CreateOptions{
			VendorID: 0xfff1, ProductID: 0x8000, SoftwareVersion: 3, SoftwareVersionString: "v3.0.0", Payload: []byte{1, 2, 3, 4},
		})
		if err != nil {
			t.Fatal(err)
		}
		m := &dclOtaMockFetch{}
		m.addJSON(list, dclOtaVersionsList)
		m.addJSON(v3, dclOtaVersionMetadata(3, true, true, map[string]any{
			"otaFileSize": strconv.Itoa(len(res.Image)), "otaChecksum": res.FullFileChecksum, "otaChecksumType": 99,
		}))
		m.addBytes(url, res.Image)
		o := dclOtaService(m, true)
		u, ok := o.CheckForUpdate(ctx, 0xfff1, 0x8000, 2, nil)
		if !ok {
			t.Fatal("no update")
		}
		var oe *OTAError
		if _, err := dclOtaDownloadUpdate(ctx, o, u.SoftwareVersion); !errors.As(err, &oe) || !strings.Contains(err.Error(), "Unsupported OTA checksum type") {
			t.Fatal(err)
		}
	})
}

func TestDclOtaUpdateServiceCustomDclConfig(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const list, v3 = "/dcl/model/versions/65521/32768", "/dcl/model/versions/65521/32768/3"

	t.Run("uses custom production config for prod queries", func(t *testing.T) {
		t.Parallel()
		m := &dclOtaMockFetch{}
		m.addJSON("custom-prod.dcl"+list, dclOtaVersionsList)
		m.addJSON("custom-prod.dcl"+v3, dclOtaMeta(3))
		o := &OTAVersions{Client: &Client{BaseURL: "https://custom-prod.dcl", HTTPClient: m.httpClient()}}
		if u, ok := o.CheckForUpdate(ctx, 0xfff1, 0x8000, 2, nil); !ok || u.SoftwareVersion.SoftwareVersion != 3 {
			t.Fatalf("%+v %v", u, ok)
		}
		calls := m.callLog()
		if dclOtaAny(calls, func(u string) bool { return !strings.Contains(u, "custom-prod.dcl") }) ||
			dclOtaAny(calls, func(u string) bool { return strings.Contains(u, "on.dcl.csa-iot.org") }) {
			t.Fatalf("calls %v", calls)
		}
	})

	t.Run("uses custom test config for test queries", func(t *testing.T) {
		t.Parallel()
		m := &dclOtaMockFetch{}
		m.addJSON("on.dcl.csa-iot.org"+list, map[string]any{"code": 5, "message": "Not found", "details": []any{}}, http.StatusNotFound)
		m.addJSON("custom-test.dcl"+list, dclOtaVersionsList)
		m.addJSON("custom-test.dcl"+v3, dclOtaMeta(3))
		o := &OTAVersions{TestClient: &Client{BaseURL: "https://custom-test.dcl", HTTPClient: m.httpClient()}}
		if u, ok := o.CheckForUpdate(ctx, 0xfff1, 0x8000, 2, nil); !ok || u.SoftwareVersion.SoftwareVersion != 3 {
			t.Fatalf("%+v %v", u, ok)
		}
		calls := m.callLog()
		if !dclOtaAny(calls, func(u string) bool { return strings.Contains(u, "custom-test.dcl") }) ||
			dclOtaAny(calls, func(u string) bool { return strings.Contains(u, "on.test-net.dcl.csa-iot.org") }) {
			t.Fatalf("calls %v", calls)
		}
	})

	t.Run("uses defaults when no options provided", func(t *testing.T) {
		t.Parallel()
		m := &dclOtaMockFetch{}
		m.addJSON(list, dclOtaVersionsList)
		m.addJSON(v3, dclOtaMeta(3))
		if _, ok := dclOtaService(m, true).CheckForUpdate(ctx, 0xfff1, 0x8000, 2, nil); !ok {
			t.Fatal("no update")
		}
		if !dclOtaAny(m.callLog(), func(u string) bool { return strings.Contains(u, "on.dcl.csa-iot.org") }) {
			t.Fatalf("calls %v", m.callLog())
		}
	})
}

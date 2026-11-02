// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/bdx/otaimage"
)

func otaImage(t *testing.T, version uint32) []byte {
	t.Helper()
	img, err := otaimage.Create(otaimage.CreateOptions{
		VendorID: 0xFFF1, ProductID: 0x8000, SoftwareVersion: version, SoftwareVersionString: fmt.Sprintf("v%d", version),
		Payload: bytes.Repeat([]byte{0xA5}, 300),
	})
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func sum256(b []byte) string {
	s := sha256.Sum256(b)
	return base64.StdEncoding.EncodeToString(s[:])
}

// TestOTAVersions: the highest applicable version above the current one
// (#queryDclForUpdate :228-270) — present with its checksum; a version
// that is not valid, has no https URL or whose range excludes the
// current one is skipped (#isVersionApplicable :786-819); a vendor the
// ledger does not know has none; a targeted version alone is checked.
func TestOTAVersions(t *testing.T) {
	t.Parallel()
	f := newFakeDCL(t)
	ctx := context.Background()
	img := otaImage(t, 3)
	f.json("/dcl/model/versions/65521/32768", 200, map[string]any{"modelVersions": map[string]any{
		"vid": 65521, "pid": 32768, "softwareVersions": []int{1, 2, 3, 4, 5, 6}, "schemaVersion": 0,
	}})
	rec := func(v int, url string, valid bool, minV, maxV int) map[string]any {
		r := versionRecord(v, url, strconv.Itoa(len(img)), sum256(img), 1)
		r["softwareVersionValid"], r["minApplicableSoftwareVersion"], r["maxApplicableSoftwareVersion"] = valid, minV, maxV
		return map[string]any{"modelVersion": r}
	}
	f.json("/dcl/model/versions/65521/32768/6", 200, rec(6, "https://ota.invalid/6", false, 0, 5))
	f.json("/dcl/model/versions/65521/32768/5", 200, rec(5, "http://ota.invalid/5", true, 0, 4))
	f.json("/dcl/model/versions/65521/32768/4", 200, rec(4, "https://ota.invalid/4", true, 2, 3))
	f.json("/dcl/model/versions/65521/32768/3", 200, rec(3, "https://ota.invalid/3", true, 0, 2))
	f.bytes("/img/3", img)
	o := &OTAVersions{Client: f.client()}

	u, ok := o.CheckForUpdate(ctx, 0xFFF1, 0x8000, 1, nil)
	if !ok || u.SoftwareVersion.SoftwareVersion != 3 || u.Source != SourceProduction || u.OTAChecksum != sum256(img) {
		t.Fatalf("CheckForUpdate %+v %v", u, ok)
	}
	if _, ok := o.CheckForUpdate(ctx, 0xFFF1, 0x8000, 6, nil); ok {
		t.Fatal("an update over the newest version")
	}
	if _, ok := o.CheckForUpdate(ctx, 0xFFF1, 0x8001, 1, nil); ok {
		t.Fatal("an update for an unknown product")
	}
	target := uint32(3)
	if u, ok := o.CheckForUpdate(ctx, 0xFFF1, 0x8000, 1, &target); !ok || u.SoftwareVersion.SoftwareVersion != 3 {
		t.Fatalf("targeted %+v %v", u, ok)
	}
	target = 4
	if _, ok := o.CheckForUpdate(ctx, 0xFFF1, 0x8000, 1, &target); ok {
		t.Fatal("a targeted version outside its range")
	}
	target = 9
	if _, ok := o.CheckForUpdate(ctx, 0xFFF1, 0x8000, 1, &target); ok {
		t.Fatal("an unknown targeted version")
	}

	// Lookup: in the ledger with its checksum, or not.
	v, ok, err := o.Lookup(ctx, 0xFFF1, 0x8000, 3)
	if err != nil || !ok || v.OTAChecksum != sum256(img) || v.OTAFileSize != uint64(len(img)) {
		t.Fatalf("Lookup %+v %v %v", v, ok, err)
	}
	if _, ok, err := o.Lookup(ctx, 0xFFF1, 0x8000, 9); ok || err != nil {
		t.Fatalf("absent version %v %v", ok, err)
	}
	if _, _, err := (&OTAVersions{}).Lookup(ctx, 1, 2, 3); err == nil {
		t.Fatal("a lookup without a client")
	}

	// Download and verify.
	// The ledger's URLs are https; the fake serves plain http, which
	// Download admits (:679-684).
	var buf bytes.Buffer
	dl := u.SoftwareVersion
	dl.OTAURL = f.url("/img/3")
	if err := o.Download(ctx, dl, &buf); err != nil || !bytes.Equal(buf.Bytes(), img) {
		t.Fatalf("Download %v", err)
	}
	if h, err := VerifyImage(bytes.NewReader(buf.Bytes()), u.SoftwareVersion, nil); err != nil || h.SoftwareVersion != 3 {
		t.Fatalf("VerifyImage %+v %v", h, err)
	}
}

// TestOTATestNet: the test-net ledger is asked too and the higher version
// wins (checkForUpdate :348-385); a failing ledger finds nothing.
func TestOTATestNet(t *testing.T) {
	t.Parallel()
	prod, test := newFakeDCL(t), newFakeDCL(t)
	for i, f := range []*fakeDCL{prod, test} {
		v := 2 + i
		f.json("/dcl/model/versions/65521/32768", 200, map[string]any{"modelVersions": map[string]any{
			"vid": 65521, "pid": 32768, "softwareVersions": []int{v}, "schemaVersion": 0,
		}})
		r := versionRecord(v, "https://ota.invalid/x", "10", "", 0)
		f.json(fmt.Sprintf("/dcl/model/versions/65521/32768/%d", v), 200, map[string]any{"modelVersion": r})
	}
	o := &OTAVersions{Client: prod.client(), TestClient: test.client()}
	u, ok := o.CheckForUpdate(context.Background(), 0xFFF1, 0x8000, 1, nil)
	if !ok || u.SoftwareVersion.SoftwareVersion != 3 || u.Source != SourceTest {
		t.Fatalf("%+v %v", u, ok)
	}
	target := uint32(2)
	if u, ok := o.CheckForUpdate(context.Background(), 0xFFF1, 0x8000, 1, &target); !ok || u.Source != SourceProduction {
		t.Fatalf("targeted %+v %v", u, ok)
	}
	prod.mu.Lock()
	prod.failPath["/dcl/model/versions/65521/32768"] = 500
	prod.mu.Unlock()
	if u, ok := o.CheckForUpdate(context.Background(), 0xFFF1, 0x8000, 1, nil); !ok || u.Source != SourceTest {
		t.Fatal("a failing production ledger hid the test-net update")
	}
	// A version record that answers another version (:735-740).
	test.json("/dcl/model/versions/65521/32768/3", 200, map[string]any{"modelVersion": versionRecord(4, "https://x", "1", "", 0)})
	if _, ok := o.CheckForUpdate(context.Background(), 0xFFF1, 0x8000, 1, nil); ok {
		t.Fatal("a mismatching record was an update")
	}
}

// TestVerifyImage: #verifyUpdate (:388-424) — the whole-file checksum of
// the stated type, the ledger's file size unless the checksum matched,
// and the header's vendor, product and version.
func TestVerifyImage(t *testing.T) {
	t.Parallel()
	img := otaImage(t, 3)
	good := SoftwareVersion{VID: 0xFFF1, PID: 0x8000, SoftwareVersion: 3, OTAChecksum: sum256(img), OTAFileSize: uint64(len(img))}
	if _, err := VerifyImage(bytes.NewReader(img), good, nil); err != nil {
		t.Fatal(err)
	}
	s512 := sha512.Sum512(img)
	alt := good
	alt.OTAChecksumType, alt.OTAChecksum = otaimage.DigestSHA512, base64.StdEncoding.EncodeToString(s512[:])
	if _, err := VerifyImage(bytes.NewReader(img), alt, nil); err != nil {
		t.Fatalf("SHA-512: %v", err)
	}
	sizeOnly := good
	sizeOnly.OTAChecksum = ""
	if _, err := VerifyImage(bytes.NewReader(img), sizeOnly, nil); err != nil {
		t.Fatalf("size only: %v", err)
	}
	for name, tc := range map[string]struct {
		mutate func(*SoftwareVersion)
		want   string
	}{
		"checksum":     {func(u *SoftwareVersion) { u.OTAChecksum = "AAAA" }, "checksum mismatch"},
		"type":         {func(u *SoftwareVersion) { u.OTAChecksumType = 2 }, "Unsupported OTA checksum type: 2"},
		"size":         {func(u *SoftwareVersion) { u.OTAChecksum, u.OTAFileSize = "", 5 }, "size mismatch"},
		"vendor":       {func(u *SoftwareVersion) { u.VID = 1 }, "vendor ID mismatch"},
		"product":      {func(u *SoftwareVersion) { u.PID = 1 }, "product ID mismatch"},
		"version":      {func(u *SoftwareVersion) { u.SoftwareVersion = 4 }, "software version mismatch"},
		"not an image": {func(*SoftwareVersion) {}, "identifier"},
	} {
		u := good
		tc.mutate(&u)
		data := img
		if name == "not an image" {
			data = []byte("plain text, no OTA image here")
		}
		if _, err := VerifyImage(bytes.NewReader(data), u, nil); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// A size mismatch with a matching checksum is a warning (OtaImageReader.ts:94-106).
	warn := good
	warn.OTAFileSize = 5
	if _, err := VerifyImage(bytes.NewReader(img), warn, nil); err != nil {
		t.Fatalf("size mismatch under a matching checksum: %v", err)
	}
}

// TestOTADownload: only http and https URLs are fetched (:667-684); a
// failing server is an OTAError.
func TestOTADownload(t *testing.T) {
	t.Parallel()
	f := newFakeDCL(t)
	o := &OTAVersions{Client: f.client()}
	ctx := context.Background()
	var oe *OTAError
	for url, want := range map[string]string{
		"":                 "No OTA URL",
		"ftp://x.invalid/": `Unsupported protocol "ftp:"`,
		"not a url":        "Invalid OTA URL",
		f.url("/missing"):  "Failed to download OTA image: 404",
	} {
		err := o.Download(ctx, SoftwareVersion{OTAURL: url}, &bytes.Buffer{})
		if !errors.As(err, &oe) || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v", url, err)
		}
	}
	if err := (&OTAVersions{}).Download(ctx, SoftwareVersion{OTAURL: "http://127.0.0.1:1/x"}, &bytes.Buffer{}); !errors.As(err, &oe) || oe.Unwrap() == nil {
		t.Fatalf("unreachable: %v", err)
	}
}

// TestOTADownloadBoundedByTheAnnouncedSize pins that Download copies at
// most OTAFileSize bytes: an OTA host that answers more than the ledger
// announced gets an OTAError, and an answer of the announced size passes.
// W4-ctlint-02.
func TestOTADownloadBoundedByTheAnnouncedSize(t *testing.T) {
	t.Parallel()
	f := newFakeDCL(t)
	f.bytes("/image.ota", bytes.Repeat([]byte{0xAA}, 4096))
	o := &OTAVersions{Client: f.client()}
	var buf bytes.Buffer
	var oe *OTAError
	if err := o.Download(context.Background(), SoftwareVersion{OTAURL: f.url("/image.ota"), OTAFileSize: 1024}, &buf); !errors.As(err, &oe) || !strings.Contains(err.Error(), "exceeds its announced size") {
		t.Errorf("an answer past OTAFileSize: %v, want the OTAError", err)
	}
	if buf.Len() > 1025 {
		t.Errorf("copied %d bytes, want at most OTAFileSize+1", buf.Len())
	}
	buf.Reset()
	if err := o.Download(context.Background(), SoftwareVersion{OTAURL: f.url("/image.ota"), OTAFileSize: 4096}, &buf); err != nil || buf.Len() != 4096 {
		t.Errorf("an answer of the announced size: %d bytes, %v", buf.Len(), err)
	}
}

// TestOTAVersionWithoutMaximum (W4-ctlint-06): a ledger record without
// maxApplicableSoftwareVersion has no upper bound — matter.js checks the
// maximum only when it is defined (DclOtaUpdateService.ts:811-816); a
// stated maximum still applies.
func TestOTAVersionWithoutMaximum(t *testing.T) {
	t.Parallel()
	f := newFakeDCL(t)
	img := otaImage(t, 3)
	f.json("/dcl/model/versions/65521/32768", 200, map[string]any{"modelVersions": map[string]any{
		"vid": 65521, "pid": 32768, "softwareVersions": []int{3}, "schemaVersion": 0,
	}})
	r := versionRecord(3, "https://ota.invalid/3", strconv.Itoa(len(img)), sum256(img), 1)
	r["softwareVersionValid"], r["minApplicableSoftwareVersion"] = true, 1
	delete(r, "maxApplicableSoftwareVersion")
	f.json("/dcl/model/versions/65521/32768/3", 200, map[string]any{"modelVersion": r})
	o := &OTAVersions{Client: f.client()}
	u, ok := o.CheckForUpdate(context.Background(), 0xFFF1, 0x8000, 2, nil)
	if !ok || u.SoftwareVersion.SoftwareVersion != 3 || u.SoftwareVersion.MaxApplicableSoftwareVersion != math.MaxUint32 {
		t.Fatalf("CheckForUpdate %+v %v, want version 3 with an unbounded maximum", u, ok)
	}
	if !o.IsApplicable(SoftwareVersion{SoftwareVersionValid: true, OTAURL: "https://x", MaxApplicableSoftwareVersion: 1}, 2) {
		return
	}
	t.Fatal("a stated maximum of 1 applied to version 2")
}

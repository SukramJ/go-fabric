// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package softwareupdate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/bdx/otaimage"
	"github.com/SukramJ/go-fabric/controller/dcl"
)

// The image-store cases of matter.js's DclOtaUpdateServiceTest
// (packages/protocol/test/dcl/DclOtaUpdateServiceTest.ts at 862ad75f) that
// need the store's own download, delete patterns, recorded specification
// versions and offline refusals. matter.js's fetchMock is an httptest
// server here: its request log is the fetch call log, and a ledger record
// pointing at it is the DeviceSoftwareVersionModelDclSchemaWithSource the
// cases pass. A service is online with a ledger (a dcl.OTAVersions that
// asks no ledger, so only downloads reach the network) and offline
// without one (ImagesConfig.Ledger, matter.js's offline option).

// fetchMock serves OTA images and records every request.
type fetchMock struct {
	srv   *httptest.Server
	mu    sync.Mutex
	files map[string][]byte
	calls []string
}

func newFetchMock(t *testing.T) *fetchMock {
	t.Helper()
	f := &fetchMock{files: map[string][]byte{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.calls = append(f.calls, r.URL.Path)
		b, ok := f.files[r.URL.Path]
		f.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(b)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// add serves b at path and returns its URL (fetchMock.addResponse).
func (f *fetchMock) add(path string, b []byte) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files[path] = b
	return f.srv.URL + path
}

func (f *fetchMock) url(path string) string { return f.srv.URL + path }

// callLog is fetchMock.getCallLog: the paths fetched so far.
func (f *fetchMock) callLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// createOtaImage is dcl-ota-test-helpers.ts createOtaImage.
func createOtaImage(t *testing.T, vid, pid uint16, version uint32) []byte {
	t.Helper()
	b, err := otaimage.Create(otaimage.CreateOptions{
		VendorID: vid, ProductID: pid, SoftwareVersion: version, SoftwareVersionString: fmt.Sprintf("v%d.0.0", version),
		Payload: bytes.Repeat([]byte{0xA5}, 256),
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// ledgerUpdate is an update of the production ledger whose image is at
// url — the cases' updateInfo literals (e.g. :771-784, :1847-1861).
func ledgerUpdate(url string, vid, pid uint16, version uint32, size int, spec uint32) UpdateInfo {
	v := dcl.SoftwareVersion{
		VID: vid, PID: pid, SoftwareVersion: version, SoftwareVersionString: fmt.Sprintf("v%d.0.0", version),
		CDVersionNumber: 1, SoftwareVersionValid: true, OTAURL: url, OTAFileSize: uint64(size), //nolint:gosec // a test image
		MinApplicableSoftwareVersion: version - 1, MaxApplicableSoftwareVersion: version - 1, SpecificationVersion: spec,
	}
	return UpdateInfo{
		VendorID: vid, ProductID: pid, SoftwareVersion: version, SoftwareVersionString: v.SoftwareVersionString,
		MinApplicable: version - 1, MaxApplicable: version - 1, SpecificationVersion: spec, Source: SourceProd, ledger: &v,
	}
}

func quietLog() *slog.Logger { return slog.New(slog.DiscardHandler) }

func onlineImages(b Blobs) *Images {
	return NewImages(ImagesConfig{Blobs: b, Ledger: &dcl.OTAVersions{}, Logger: quietLog()})
}

func offlineImages(b Blobs) *Images {
	return NewImages(ImagesConfig{Blobs: b, Logger: quietLog()})
}

func filenames(st []StoredImage) []string {
	out := make([]string, 0, len(st))
	for _, s := range st {
		out = append(out, s.Filename)
	}
	return out
}

func hasBlob(t *testing.T, b Blobs, name string) bool {
	t.Helper()
	_, err := b.Get(context.Background(), name)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	return err == nil
}

func mustDownload(t *testing.T, imgs *Images, info UpdateInfo, force bool) string {
	t.Helper()
	name, err := imgs.DownloadUpdate(context.Background(), info, force)
	if err != nil {
		t.Fatalf("DownloadUpdate(force=%v): %v", force, err)
	}
	return name
}

func TestImagesDownloadUpdateParity(t *testing.T) {
	t.Parallel()

	// :768-806
	t.Run("reuses existing valid OTA file instead of re-downloading", func(t *testing.T) {
		t.Parallel()
		f := newFetchMock(t)
		img := createOtaImage(t, 0xfff1, 0x8000, 3)
		info := ledgerUpdate(f.add("/ota-v3.bin", img), 0xfff1, 0x8000, 3, len(img), 0)
		imgs := onlineImages(nil)
		first := mustDownload(t, imgs, info, true)
		second := mustDownload(t, imgs, info, false)
		if first != second || first != "fff1.8000.prod.3" {
			t.Fatalf("designators %q and %q", first, second)
		}
		if got := f.callLog(); len(got) != 1 {
			t.Fatalf("call log %v, want the first download only", got)
		}
	})

	// :1260-1296
	t.Run("re-downloads file when force is true", func(t *testing.T) {
		t.Parallel()
		f := newFetchMock(t)
		img := createOtaImage(t, 0xfff1, 0x8000, 3)
		info := ledgerUpdate(f.add("/ota-v3.bin", img), 0xfff1, 0x8000, 3, len(img), 0)
		imgs := onlineImages(nil)
		mustDownload(t, imgs, info, false)
		after := len(f.callLog())
		mustDownload(t, imgs, info, false)
		if n := len(f.callLog()); n != after {
			t.Fatalf("%d fetches after a second download without force, want %d", n, after)
		}
		mustDownload(t, imgs, info, true)
		if n := len(f.callLog()); n <= after {
			t.Fatalf("%d fetches after a forced download, want more than %d", n, after)
		}
	})
}

// deleteFixture is the delete cases' beforeEach (:1113-1155): two prod
// images of vendor fff1, products 8000 (v3) and 8001 (v2).
func deleteFixture(t *testing.T) (*Images, *MemoryBlobs, *fetchMock) {
	t.Helper()
	f := newFetchMock(t)
	b := &MemoryBlobs{}
	imgs := onlineImages(b)
	img1 := createOtaImage(t, 0xfff1, 0x8000, 3)
	img2 := createOtaImage(t, 0xfff1, 0x8001, 2)
	mustDownload(t, imgs, ledgerUpdate(f.add("/ota1.bin", img1), 0xfff1, 0x8000, 3, len(img1), 0), true)
	mustDownload(t, imgs, ledgerUpdate(f.add("/ota2.bin", img2), 0xfff1, 0x8001, 2, len(img2), 0), true)
	return imgs, b, f
}

func TestImagesDeleteParity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	vendor, product := uint16(0xfff1), uint16(0x8000)
	yes := true
	remainingIs8001 := func(t *testing.T, imgs *Images) {
		t.Helper()
		if r := imgs.Find(ctx, FindOptions{}); len(r) != 1 || r[0].ProductID != 0x8001 {
			t.Fatalf("remaining %+v, want the 8001 image", r)
		}
	}

	// :1172-1180
	t.Run("deletes specific product file", func(t *testing.T) {
		t.Parallel()
		imgs, _, _ := deleteFixture(t)
		if _, err := imgs.Delete(ctx, DeleteOptions{VendorID: &vendor, ProductID: &product, IsProduction: &yes}); err != nil {
			t.Fatal(err)
		}
		remainingIs8001(t, imgs)
	})

	// :1182-1190
	t.Run("deletes all files for a vendor", func(t *testing.T) {
		t.Parallel()
		imgs, _, _ := deleteFixture(t)
		if n, err := imgs.Delete(ctx, DeleteOptions{VendorID: &vendor, IsProduction: &yes}); err != nil || n != 2 {
			t.Fatalf("Delete = %d, %v, want 2", n, err)
		}
		if r := imgs.Find(ctx, FindOptions{}); len(r) != 0 {
			t.Fatalf("remaining %+v", r)
		}
	})

	// :1192-1196
	t.Run("does not throw error when file not found with correct pattern", func(t *testing.T) {
		t.Parallel()
		imgs, _, _ := deleteFixture(t)
		if n, err := imgs.Delete(ctx, DeleteOptions{Filename: "0.0.test.1"}); err != nil || n != 0 {
			t.Fatalf("Delete = %d, %v, want 0", n, err)
		}
	})

	// :1198-1202
	t.Run("throws error when file not found with invalid pattern", func(t *testing.T) {
		t.Parallel()
		imgs, _, _ := deleteFixture(t)
		if _, err := imgs.Delete(ctx, DeleteOptions{Filename: "nonexistent"}); !errors.Is(err, ErrOTA) || !strings.Contains(err.Error(), "invalid OTA filename format") {
			t.Fatalf("Delete = %v", err)
		}
	})

	// :1204-1208
	t.Run("throws no error when specific product not found", func(t *testing.T) {
		t.Parallel()
		imgs, _, _ := deleteFixture(t)
		other := uint16(0x9999)
		if n, err := imgs.Delete(ctx, DeleteOptions{VendorID: &vendor, ProductID: &other, IsProduction: &yes}); err != nil || n != 0 {
			t.Fatalf("Delete = %d, %v, want 0", n, err)
		}
	})

	// :1210-1219
	t.Run("deletes specific version by filename", func(t *testing.T) {
		t.Parallel()
		imgs, _, _ := deleteFixture(t)
		if n, err := imgs.Delete(ctx, DeleteOptions{Filename: "fff1.8000.prod.3"}); err != nil || n != 1 {
			t.Fatalf("Delete = %d, %v, want 1", n, err)
		}
		remainingIs8001(t, imgs)
	})

	// :1221-1256
	t.Run("deletes all versions for a mode", func(t *testing.T) {
		t.Parallel()
		imgs, _, f := deleteFixture(t)
		img4 := createOtaImage(t, 0xfff1, 0x8000, 4)
		mustDownload(t, imgs, ledgerUpdate(f.add("/ota4.bin", img4), 0xfff1, 0x8000, 4, len(img4), 0), true)
		if r := imgs.Find(ctx, FindOptions{}); len(r) != 3 {
			t.Fatalf("stored %v, want 3", filenames(r))
		}
		if n, err := imgs.Delete(ctx, DeleteOptions{VendorID: &vendor, ProductID: &product, Mode: ModeProd}); err != nil || n != 2 {
			t.Fatalf("Delete = %d, %v, want 2", n, err)
		}
		remainingIs8001(t, imgs)
	})

	// delete's own contract (:1245-1247): a pattern without the vendor is
	// refused; and a product narrows a delete without a mode too
	// (BD-Matter-OtaImages-DeleteProductWithoutMode).
	t.Run("refuses a pattern without a vendor and narrows by product without a mode", func(t *testing.T) {
		t.Parallel()
		imgs, _, _ := deleteFixture(t)
		if _, err := imgs.Delete(ctx, DeleteOptions{ProductID: &product}); !errors.Is(err, ErrOTA) {
			t.Fatalf("Delete without vendor = %v", err)
		}
		if n, err := imgs.Delete(ctx, DeleteOptions{VendorID: &vendor, ProductID: &product}); err != nil || n != 1 {
			t.Fatalf("Delete = %d, %v, want 1", n, err)
		}
		remainingIs8001(t, imgs)
	})
}

func TestImagesOpenParity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// :1300-1329
	t.Run("returns file designator for existing file", func(t *testing.T) {
		t.Parallel()
		f := newFetchMock(t)
		img := createOtaImage(t, 0xfff1, 0x8000, 3)
		imgs := onlineImages(nil)
		mustDownload(t, imgs, ledgerUpdate(f.add("/ota-v3.bin", img), 0xfff1, 0x8000, 3, len(img), 0), true)
		blob, err := imgs.Open(ctx, "fff1.8000.prod.3")
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(io.NewSectionReader(blob, 0, blob.Size()))
		if err != nil || !bytes.Equal(got, img) {
			t.Fatalf("read %d bytes (%v), want the stored image", len(got), err)
		}
	})

	// :1331-1337
	t.Run("throws error for non-existent file with invalid pattern", func(t *testing.T) {
		t.Parallel()
		_, err := offlineImages(nil).Open(ctx, "nonexistent")
		if !errors.Is(err, fs.ErrNotExist) || !strings.Contains(err.Error(), "invalid OTA filename format") {
			t.Fatalf("Open = %v", err)
		}
	})

	// :1339-1343
	t.Run("throws error for non-existent file with valid pattern", func(t *testing.T) {
		t.Parallel()
		_, err := offlineImages(nil).Open(ctx, "0.0.test.1")
		if !errors.Is(err, fs.ErrNotExist) || !strings.Contains(err.Error(), "OTA file not found") {
			t.Fatalf("Open = %v", err)
		}
	})
}

func TestImagesSpecificationVersionParity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const meta = "fff1.8000.prod.3" + metaSuffix
	vendor, product := uint16(0xfff1), uint16(0x8000)
	// storedUpdate (:1347-1366): the v3 image served by the mock.
	storedUpdate := func(t *testing.T, spec uint32) (UpdateInfo, *fetchMock) {
		t.Helper()
		f := newFetchMock(t)
		img := createOtaImage(t, 0xfff1, 0x8000, 3)
		return ledgerUpdate(f.add("/ota3.bin", img), 0xfff1, 0x8000, 3, len(img), spec), f
	}
	entry := func(t *testing.T, imgs *Images) StoredImage {
		t.Helper()
		found := imgs.Find(ctx, FindOptions{VendorID: &vendor, ProductID: &product})
		if len(found) != 1 {
			t.Fatalf("found %v, want one image", filenames(found))
		}
		return found[0]
	}

	// :1368-1386
	t.Run("keeps the specification version of a stored image", func(t *testing.T) {
		t.Parallel()
		info, _ := storedUpdate(t, 0x01040200)
		imgs := onlineImages(nil)
		mustDownload(t, imgs, info, true)
		if got := entry(t, imgs).SpecificationVersion; got != 0x01040200 {
			t.Fatalf("specification version %#x", got)
		}
		yes := true
		u, ok := imgs.CheckForUpdate(ctx, 0xfff1, 0x8000, 2, CheckOptions{IncludeStored: true, IsProduction: &yes})
		if !ok || u.SoftwareVersion != 3 || u.SpecificationVersion != 0x01040200 {
			t.Fatalf("update %+v (%v)", u, ok)
		}
	})

	// :1388-1397
	t.Run("drops the specification version when an image is stored again without one", func(t *testing.T) {
		t.Parallel()
		info, _ := storedUpdate(t, 0x01040200)
		imgs := onlineImages(nil)
		mustDownload(t, imgs, info, true)
		without := info
		without.SpecificationVersion = 0
		mustDownload(t, imgs, without, true)
		if got := entry(t, imgs).SpecificationVersion; got != 0 {
			t.Fatalf("specification version %#x, want none", got)
		}
	})

	// :1399-1408
	t.Run("records the specification version when an already stored image is reused", func(t *testing.T) {
		t.Parallel()
		info, f := storedUpdate(t, 0)
		imgs := onlineImages(nil)
		mustDownload(t, imgs, info, true)
		with := info
		with.SpecificationVersion = 0x01040200
		mustDownload(t, imgs, with, false)
		if got := entry(t, imgs).SpecificationVersion; got != 0x01040200 {
			t.Fatalf("specification version %#x", got)
		}
		if n := len(f.callLog()); n != 1 {
			t.Fatalf("%d fetches, want the reuse to fetch nothing", n)
		}
	})

	// :1410-1417
	t.Run("does not store an invalid specification version", func(t *testing.T) {
		t.Parallel()
		info, _ := storedUpdate(t, 0)
		b := &MemoryBlobs{}
		mustDownload(t, onlineImages(b), info, true)
		if hasBlob(t, b, meta) {
			t.Fatal("a specification version record was stored")
		}
	})

	// :1419-1440
	t.Run("removes unreadable metadata when reading it", func(t *testing.T) {
		t.Parallel()
		info, _ := storedUpdate(t, 0x01040200)
		b := &MemoryBlobs{}
		imgs := onlineImages(b)
		mustDownload(t, imgs, info, true)
		if err := b.Put(ctx, meta, []byte("not json")); err != nil {
			t.Fatal(err)
		}
		if got := entry(t, imgs).SpecificationVersion; got != 0 {
			t.Fatalf("specification version %#x, want none", got)
		}
		if hasBlob(t, b, meta) {
			t.Fatal("the unreadable record was kept")
		}
	})

	// :1442-1454
	t.Run("removes the specification version when a forced download replaces the image and fails", func(t *testing.T) {
		t.Parallel()
		info, f := storedUpdate(t, 0x01040200)
		b := &MemoryBlobs{}
		imgs := onlineImages(b)
		mustDownload(t, imgs, info, true)
		broken := info
		v := *info.ledger
		v.OTAURL = f.url("/broken.bin")
		broken.ledger = &v
		if _, err := imgs.DownloadUpdate(ctx, broken, true); !errors.Is(err, ErrOTA) {
			t.Fatalf("DownloadUpdate of a missing image = %v", err)
		}
		if hasBlob(t, b, meta) {
			t.Fatal("the specification version outlived the replaced image")
		}
	})

	// :1456-1468
	t.Run("removes the specification version with the stored image", func(t *testing.T) {
		t.Parallel()
		info, _ := storedUpdate(t, 0x01040200)
		b := &MemoryBlobs{}
		imgs := onlineImages(b)
		mustDownload(t, imgs, info, true)
		if !hasBlob(t, b, meta) {
			t.Fatal("no specification version record")
		}
		if n, err := imgs.Delete(ctx, DeleteOptions{Filename: "fff1.8000.prod.3"}); err != nil || n != 1 {
			t.Fatalf("Delete = %d, %v", n, err)
		}
		if hasBlob(t, b, meta) {
			t.Fatal("the record outlived the image")
		}
	})

	// :1470-1479
	t.Run("removes the specification version when all versions of a mode are deleted", func(t *testing.T) {
		t.Parallel()
		info, _ := storedUpdate(t, 0x01040200)
		b := &MemoryBlobs{}
		imgs := onlineImages(b)
		mustDownload(t, imgs, info, true)
		if n, err := imgs.Delete(ctx, DeleteOptions{VendorID: &vendor, ProductID: &product, Mode: ModeProd}); err != nil || n != 1 {
			t.Fatalf("Delete = %d, %v", n, err)
		}
		if hasBlob(t, b, meta) {
			t.Fatal("the record outlived the image")
		}
	})

	// :1481-1490
	t.Run("removes the specification version when all files of a vendor are deleted", func(t *testing.T) {
		t.Parallel()
		info, _ := storedUpdate(t, 0x01040200)
		b := &MemoryBlobs{}
		imgs := onlineImages(b)
		mustDownload(t, imgs, info, true)
		if n, err := imgs.Delete(ctx, DeleteOptions{VendorID: &vendor}); err != nil || n != 1 {
			t.Fatalf("Delete = %d, %v", n, err)
		}
		if hasBlob(t, b, meta) {
			t.Fatal("the record outlived the image")
		}
	})

	// :1492-1510
	t.Run("takes the specification version for a local image from the options", func(t *testing.T) {
		t.Parallel()
		img := createOtaImage(t, 0xfff1, 0x8000, 3)
		imgs := onlineImages(nil)
		info, err := UpdateInfoFromImage(bytes.NewReader(img), LocalImageOptions{SpecificationVersion: 0x01060100})
		if err != nil {
			t.Fatal(err)
		}
		if info.Source != SourceLocal || info.MaxApplicable != 2 || info.MinApplicable != 0 {
			t.Fatalf("update info %+v", info)
		}
		if _, err := imgs.Store(ctx, img, ModeLocal, &info); err != nil {
			t.Fatal(err)
		}
		if got := entry(t, imgs).SpecificationVersion; got != 0x01060100 {
			t.Fatalf("specification version %#x", got)
		}
	})
}

// flakyBlobs fails the next read of a stored image (not of its record) —
// the cases' failNextRead (:1949-1956), which fails the next openBlob of a
// PersistedFileDesignator.
type flakyBlobs struct {
	MemoryBlobs
	mu   sync.Mutex
	fail error
}

func (f *flakyBlobs) failNextRead(err error) {
	f.mu.Lock()
	f.fail = err
	f.mu.Unlock()
}

func (f *flakyBlobs) Get(ctx context.Context, name string) ([]byte, error) {
	f.mu.Lock()
	err := f.fail
	if err != nil && !strings.HasSuffix(name, metaSuffix) {
		f.fail = nil
	} else {
		err = nil
	}
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return f.MemoryBlobs.Get(ctx, name)
}

func TestImagesOfflineParity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	vendor, product := uint16(0xfff1), uint16(0x8000)
	// storedV3Service (:1873-1879): v3 stored as prod, offline unless asked.
	storedV3 := func(t *testing.T, f *fetchMock, b Blobs, offline bool) (*Images, UpdateInfo) {
		t.Helper()
		img := createOtaImage(t, 0xfff1, 0x8000, 3)
		imgs := onlineImages(b)
		if offline {
			imgs = offlineImages(b)
		}
		info := ledgerUpdate(f.url("/ota-v3.bin"), 0xfff1, 0x8000, 3, 0, 0)
		if _, err := imgs.Store(ctx, img, ModeProd, &info); err != nil {
			t.Fatal(err)
		}
		return imgs, info
	}
	storedNames := func(imgs *Images) []string {
		return filenames(imgs.Find(ctx, FindOptions{VendorID: &vendor, ProductID: &product}))
	}
	badChecksum := func(info UpdateInfo) UpdateInfo {
		v := *info.ledger
		v.OTAChecksum, v.OTAChecksumType = "x", 99
		info.ledger = &v
		return info
	}

	// :1881-1901
	t.Run("asks the DCL nothing and finds only stored updates", func(t *testing.T) {
		t.Parallel()
		f := newFetchMock(t)
		imgs, _ := storedV3(t, f, nil, true)
		u, ok := imgs.CheckForUpdate(ctx, 0xfff1, 0x8000, 2, CheckOptions{IncludeStored: true})
		if !ok || u.SoftwareVersion != 3 || u.Source != SourceProd {
			t.Fatalf("stored update %+v (%v)", u, ok)
		}
		if u, ok := imgs.CheckForUpdate(ctx, 0xfff1, 0x8000, 2, CheckOptions{}); ok {
			t.Fatalf("found %+v without the stored images", u)
		}
		if got := f.callLog(); len(got) != 0 {
			t.Fatalf("call log %v", got)
		}
	})

	// :1903-1913
	t.Run("serves a stored image and downloads none it lacks", func(t *testing.T) {
		t.Parallel()
		f := newFetchMock(t)
		f.add("/ota-v4.bin", createOtaImage(t, 0xfff1, 0x8000, 4))
		imgs, info := storedV3(t, f, nil, true)
		if name := mustDownload(t, imgs, info, false); name != "fff1.8000.prod.3" {
			t.Fatalf("designator %q", name)
		}
		v4 := ledgerUpdate(f.url("/ota-v4.bin"), 0xfff1, 0x8000, 4, 0, 0)
		if _, err := imgs.DownloadUpdate(ctx, v4, false); !errors.Is(err, ErrOTA) || !strings.Contains(err.Error(), "offline") {
			t.Fatalf("DownloadUpdate of v4 = %v", err)
		}
		if got := f.callLog(); len(got) != 0 {
			t.Fatalf("call log %v", got)
		}
	})

	// :1915-1927
	t.Run("keeps a stored image when a forced download is refused", func(t *testing.T) {
		t.Parallel()
		f := newFetchMock(t)
		imgs, info := storedV3(t, f, nil, true)
		if _, err := imgs.DownloadUpdate(ctx, info, true); !errors.Is(err, ErrOTA) || !strings.Contains(err.Error(), "stored image is kept") {
			t.Fatalf("forced DownloadUpdate = %v", err)
		}
		if got := storedNames(imgs); !slices.Equal(got, []string{"fff1.8000.prod.3"}) {
			t.Fatalf("stored %v", got)
		}
		if got := f.callLog(); len(got) != 0 {
			t.Fatalf("call log %v", got)
		}
	})

	// :1929-1942
	t.Run("removes a stored image that fails validation and downloads none", func(t *testing.T) {
		t.Parallel()
		f := newFetchMock(t)
		imgs, info := storedV3(t, f, nil, true)
		_, err := imgs.DownloadUpdate(ctx, badChecksum(info), false)
		want := "no valid OTA image fff1.8000.prod.3 (v3.0.0) is stored and the service is offline, so none is downloaded"
		if !errors.Is(err, ErrOTA) || !strings.Contains(err.Error(), want) {
			t.Fatalf("DownloadUpdate = %v", err)
		}
		if got := storedNames(imgs); len(got) != 0 {
			t.Fatalf("stored %v", got)
		}
		if got := f.callLog(); len(got) != 0 {
			t.Fatalf("call log %v", got)
		}
	})

	// :1944-2010, once per kind of read error.
	for _, kind := range []struct {
		name string
		err  error
	}{
		{"storage error", errors.New("read failed")},
		{"I/O error", io.ErrUnexpectedEOF},
	} {
		// :1958-1978
		t.Run("keeps a stored image it cannot read ("+kind.name+")", func(t *testing.T) {
			t.Parallel()
			f := newFetchMock(t)
			b := &flakyBlobs{}
			imgs, info := storedV3(t, f, b, true)
			b.failNextRead(kind.err)
			_, err := imgs.DownloadUpdate(ctx, info, false)
			want := "cannot read stored OTA image fff1.8000.prod.3 (v3.0.0) to validate it and the service is offline; the stored image is kept"
			if !errors.Is(err, ErrOTA) || !errors.Is(err, kind.err) || !strings.Contains(err.Error(), want) {
				t.Fatalf("DownloadUpdate = %v", err)
			}
			if got := storedNames(imgs); !slices.Equal(got, []string{"fff1.8000.prod.3"}) {
				t.Fatalf("stored %v", got)
			}
			if got := f.callLog(); len(got) != 0 {
				t.Fatalf("call log %v", got)
			}
		})

		// :1980-1994
		t.Run("online, replaces a stored image it cannot read ("+kind.name+")", func(t *testing.T) {
			t.Parallel()
			f := newFetchMock(t)
			f.add("/ota-v3.bin", createOtaImage(t, 0xfff1, 0x8000, 3))
			b := &flakyBlobs{}
			imgs, info := storedV3(t, f, b, false)
			b.failNextRead(kind.err)
			if name := mustDownload(t, imgs, info, false); name != "fff1.8000.prod.3" {
				t.Fatalf("designator %q", name)
			}
			if got := f.callLog(); !slices.Equal(got, []string{"/ota-v3.bin"}) {
				t.Fatalf("call log %v", got)
			}
		})

		// :1996-2009
		t.Run("online, removes a stored image it cannot read even when no replacement arrives ("+kind.name+")", func(t *testing.T) {
			t.Parallel()
			f := newFetchMock(t)
			b := &flakyBlobs{}
			imgs, info := storedV3(t, f, b, false)
			b.failNextRead(kind.err)
			if _, err := imgs.DownloadUpdate(ctx, info, false); !errors.Is(err, ErrOTA) {
				t.Fatalf("DownloadUpdate = %v", err)
			}
			if got := storedNames(imgs); len(got) != 0 {
				t.Fatalf("stored %v", got)
			}
		})
	}

	// :2012-2027
	t.Run("keeps a stored image that fails validation when forced", func(t *testing.T) {
		t.Parallel()
		f := newFetchMock(t)
		imgs, info := storedV3(t, f, nil, true)
		if _, err := imgs.DownloadUpdate(ctx, badChecksum(info), true); !errors.Is(err, ErrOTA) || !strings.Contains(err.Error(), "stored image is kept") {
			t.Fatalf("DownloadUpdate = %v", err)
		}
		if got := storedNames(imgs); !slices.Equal(got, []string{"fff1.8000.prod.3"}) {
			t.Fatalf("stored %v", got)
		}
		if got := f.callLog(); len(got) != 0 {
			t.Fatalf("call log %v", got)
		}
	})

	// :2029-2041
	t.Run("reads no image from a URL", func(t *testing.T) {
		t.Parallel()
		f := newFetchMock(t)
		url := f.add("/ota-v3.bin", createOtaImage(t, 0xfff1, 0x8000, 3))
		if _, err := offlineImages(nil).UpdateInfoFromURL(ctx, url, LocalImageOptions{}); !errors.Is(err, ErrOTA) || !strings.Contains(err.Error(), "offline") {
			t.Fatalf("UpdateInfoFromURL = %v", err)
		}
		if got := f.callLog(); len(got) != 0 {
			t.Fatalf("call log %v", got)
		}
	})

	// createUpdateInfoFromFile online (:924-981): the image's header is the
	// update, of the local source, and DownloadUpdate fetches it from the
	// URL into the local mode; another scheme is refused.
	t.Run("reads a local update from a URL online and downloads it", func(t *testing.T) {
		t.Parallel()
		f := newFetchMock(t)
		url := f.add("/ota-v3.bin", createOtaImage(t, 0xfff1, 0x8000, 3))
		imgs := onlineImages(nil)
		if _, err := imgs.UpdateInfoFromURL(ctx, "file:///tmp/ota.bin", LocalImageOptions{}); !errors.Is(err, ErrOTA) {
			t.Fatalf("UpdateInfoFromURL of a file URL = %v", err)
		}
		info, err := imgs.UpdateInfoFromURL(ctx, url, LocalImageOptions{SpecificationVersion: 0x01060100})
		if err != nil {
			t.Fatal(err)
		}
		if info.Source != SourceLocal || info.SoftwareVersion != 3 || info.MaxApplicable != 2 {
			t.Fatalf("update info %+v", info)
		}
		if name := mustDownload(t, imgs, info, false); name != "fff1.8000.local.3" {
			t.Fatalf("designator %q", name)
		}
		if got := imgs.Find(ctx, FindOptions{Mode: ModeLocal}); len(got) != 1 || got[0].SpecificationVersion != 0x01060100 {
			t.Fatalf("stored %+v", got)
		}
	})
}

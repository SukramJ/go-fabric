// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package softwareupdate

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"thingfabric.dev/go-fabric/controller/dcl"
)

// TestImagesRequireTheLedgersChecksum: a ledger update — of the
// production or the test-net ledger — whose record states no checksum is
// refused before any request, with ErrOTA and the warning line
// dcl.ota.no_checksum, and a stored image of it is kept but not reused;
// AllowUnverifiedLedgerImages admits it and logs each admission. A record
// with a checksum and an image the host names by URL need no option.
func TestImagesRequireTheLedgersChecksum(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	img := createOtaImage(t, 0xfff1, 0x8000, 3)
	logged := func() (*slog.Logger, *bytes.Buffer) {
		var buf bytes.Buffer
		return slog.New(slog.NewTextHandler(&buf, nil)), &buf
	}

	for _, source := range []string{SourceProd, SourceTest} {
		f := newFetchMock(t)
		info := ledgerUpdate(f.add("/ota.bin", img), 0xfff1, 0x8000, 3, len(img), 0)
		info.Source = source
		blobs := &MemoryBlobs{}
		log, out := logged()
		imgs := NewImages(ImagesConfig{Blobs: blobs, Ledger: hostLedger(0), Logger: log})
		_, err := imgs.DownloadUpdate(ctx, info, false)
		if !errors.Is(err, ErrOTA) || !strings.Contains(err.Error(), "states no checksum") {
			t.Fatalf("%s: DownloadUpdate of a record without a checksum: %v; want ErrOTA naming the missing checksum", source, err)
		}
		if calls := f.callLog(); len(calls) != 0 {
			t.Fatalf("%s: fetched %v; want the refusal before any request", source, calls)
		}
		if names, _ := blobs.List(ctx); len(names) != 0 {
			t.Fatalf("%s: stored %v; want nothing", source, names)
		}
		if !strings.Contains(out.String(), "level=WARN msg=dcl.ota.no_checksum ") {
			t.Fatalf("%s: log %q; want the warning dcl.ota.no_checksum", source, out.String())
		}

		// The option admits it, and logs the admission.
		log, out = logged()
		admitting := NewImages(ImagesConfig{Blobs: blobs, Ledger: hostLedger(0), Logger: log, AllowUnverifiedLedgerImages: true})
		name, err := admitting.DownloadUpdate(ctx, info, false)
		if err != nil || len(f.callLog()) != 1 {
			t.Fatalf("%s: with AllowUnverifiedLedgerImages: %q, err %v, requests %v; want the image downloaded", source, name, err, f.callLog())
		}
		if !strings.Contains(out.String(), "level=WARN msg=dcl.ota.no_checksum_admitted ") {
			t.Fatalf("%s: log %q; want the admission logged", source, out.String())
		}

		// The stored image is kept and not reused without the option.
		if _, err := imgs.DownloadUpdate(ctx, info, false); !errors.Is(err, ErrOTA) {
			t.Fatalf("%s: DownloadUpdate of the stored image's record without a checksum: %v; want ErrOTA", source, err)
		}
		if names, _ := blobs.List(ctx); !slices.Contains(names, name) || len(f.callLog()) != 1 {
			t.Fatalf("%s: stored %v, requests %v; want %s kept and nothing fetched", source, names, f.callLog(), name)
		}

		// A record with the checksum needs no option.
		if got, err := imgs.DownloadUpdate(ctx, withChecksum(info, img), true); err != nil || got != name {
			t.Fatalf("%s: DownloadUpdate of a record with its checksum: %q, err %v", source, got, err)
		}
	}

	// An image the host names by URL carries no checksum and needs none.
	f := newFetchMock(t)
	imgs := NewImages(ImagesConfig{Ledger: hostLedger(0), Logger: quietLog()})
	local, err := imgs.UpdateInfoFromURL(ctx, f.add("/local.ota", img), LocalImageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if name, err := imgs.DownloadUpdate(ctx, local, false); err != nil || name != "fff1.8000.local.3" {
		t.Fatalf("DownloadUpdate of the host's image: %q, err %v", name, err)
	}
}

// TestImagesServeStoredLedgerImagesBoundToTheirChecksum: the checksum
// rule holds for stored images too. A stored prod or test image is
// offered (CheckForUpdate with IncludeStored), reused (DownloadUpdate of
// its stored update) and opened only when its provenance record names a
// ledger checksum it was checked against or the host as its supplier; one
// stored from a record without a checksum, and one without a record
// (stored before the store kept them), only under
// AllowUnverifiedLedgerImages. A local image needs no record. A stored
// image without a record is bound to its checksum, without a request,
// when a ledger update with the checksum reuses it.
func TestImagesServeStoredLedgerImagesBoundToTheirChecksum(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	img := createOtaImage(t, 0xfff1, 0x8000, 3)
	const name = "fff1.8000.prod.3"
	offered := func(imgs *Images) bool {
		u, ok := imgs.CheckForUpdate(ctx, 0xfff1, 0x8000, 2, CheckOptions{IncludeStored: true})
		return ok && u.SoftwareVersion == 3
	}
	opens := func(imgs *Images) bool {
		b, err := imgs.Open(ctx, name)
		if err == nil {
			closeBlob(b)
		}
		return err == nil
	}
	// strict and admitting views of one set of blobs.
	views := func(blobs Blobs) (*Images, *Images) {
		return NewImages(ImagesConfig{Blobs: blobs, Logger: quietLog()}),
			NewImages(ImagesConfig{Blobs: blobs, Logger: quietLog(), AllowUnverifiedLedgerImages: true})
	}

	t.Run("an image stored before the provenance record", func(t *testing.T) {
		t.Parallel()
		blobs := &MemoryBlobs{}
		if err := blobs.Put(ctx, name, img); err != nil {
			t.Fatal(err)
		}
		strict, admitting := views(blobs)
		if offered(strict) || opens(strict) {
			t.Fatalf("offered %v, opened %v; want neither without the option", offered(strict), opens(strict))
		}
		u, ok := admitting.CheckForUpdate(ctx, 0xfff1, 0x8000, 2, CheckOptions{IncludeStored: true})
		if !ok || !opens(admitting) {
			t.Fatalf("with the option: offered %v, opened %v; want both", ok, opens(admitting))
		}
		if _, err := strict.DownloadUpdate(ctx, u, false); !errors.Is(err, ErrOTA) {
			t.Fatalf("DownloadUpdate of the stored update without the option: %v; want ErrOTA", err)
		}
		if names, _ := blobs.List(ctx); !slices.Contains(names, name) {
			t.Fatalf("stored %v; want %s kept", names, name)
		}
		if got, err := admitting.DownloadUpdate(ctx, u, false); err != nil || got != name {
			t.Fatalf("DownloadUpdate of the stored update with the option: %q, err %v", got, err)
		}
	})

	t.Run("an image stored from a record without a checksum", func(t *testing.T) {
		t.Parallel()
		f := newFetchMock(t)
		blobs := &MemoryBlobs{}
		info := ledgerUpdate(f.add("/ota.bin", img), 0xfff1, 0x8000, 3, len(img), 0)
		if _, err := NewImages(ImagesConfig{Blobs: blobs, Ledger: hostLedger(0), Logger: quietLog(), AllowUnverifiedLedgerImages: true}).DownloadUpdate(ctx, info, false); err != nil {
			t.Fatal(err)
		}
		strict, admitting := views(blobs)
		if offered(strict) || opens(strict) || !offered(admitting) || !opens(admitting) {
			t.Fatalf("strict: offered %v, opened %v; admitting: offered %v, opened %v; want only the admitting store to",
				offered(strict), opens(strict), offered(admitting), opens(admitting))
		}
	})

	t.Run("an image checked against its record's checksum", func(t *testing.T) {
		t.Parallel()
		f := newFetchMock(t)
		blobs := &MemoryBlobs{}
		info := withChecksum(ledgerUpdate(f.add("/ota.bin", img), 0xfff1, 0x8000, 3, len(img), 0), img)
		if _, err := NewImages(ImagesConfig{Blobs: blobs, Ledger: hostLedger(0), Logger: quietLog()}).DownloadUpdate(ctx, info, false); err != nil {
			t.Fatal(err)
		}
		strict, _ := views(blobs)
		if !offered(strict) || !opens(strict) {
			t.Fatalf("offered %v, opened %v; want both without the option", offered(strict), opens(strict))
		}
	})

	t.Run("an image the host stored or supplied", func(t *testing.T) {
		t.Parallel()
		blobs := &MemoryBlobs{}
		strict, _ := views(blobs)
		if _, err := strict.Store(ctx, img, ModeProd, nil); err != nil {
			t.Fatal(err)
		}
		if !offered(strict) || !opens(strict) {
			t.Fatalf("a prod image the host stored: offered %v, opened %v; want both", offered(strict), opens(strict))
		}
		local := &MemoryBlobs{}
		if err := local.Put(ctx, "fff1.8000.local.3", img); err != nil {
			t.Fatal(err)
		}
		strictLocal, _ := views(local)
		if !offered(strictLocal) {
			t.Fatal("a local image without a provenance record is not offered")
		}
	})

	t.Run("a ledger update with the checksum binds an image stored without a record", func(t *testing.T) {
		t.Parallel()
		f := newFetchMock(t)
		blobs := &MemoryBlobs{}
		if err := blobs.Put(ctx, name, img); err != nil {
			t.Fatal(err)
		}
		online := NewImages(ImagesConfig{Blobs: blobs, Ledger: hostLedger(0), Logger: quietLog()})
		info := withChecksum(ledgerUpdate(f.add("/ota.bin", img), 0xfff1, 0x8000, 3, len(img), 0), img)
		if got, err := online.DownloadUpdate(ctx, info, false); err != nil || got != name || len(f.callLog()) != 0 {
			t.Fatalf("DownloadUpdate: %q, err %v, requests %v; want the stored image reused", got, err, f.callLog())
		}
		strict, _ := views(blobs)
		if !offered(strict) || !opens(strict) {
			t.Fatalf("after the reuse: offered %v, opened %v; want both", offered(strict), opens(strict))
		}
	})
}

// TestImagesTakeTheLedgerPathUnlessHostSupplied: the download route and
// the checksum rule follow where an update came from, not its Source
// alone. A ledger update with its Source changed to local or to an
// unknown value keeps the ledger's rules — https only, the checksum
// required — and only UpdateInfoFromURL's update takes the host's route.
func TestImagesTakeTheLedgerPathUnlessHostSupplied(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	img := createOtaImage(t, 0xfff1, 0x8000, 3)
	var plainHits atomic.Int32
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		plainHits.Add(1)
		_, _ = w.Write(img)
	}))
	t.Cleanup(plain.Close)
	imgs := NewImages(ImagesConfig{Ledger: &dcl.OTAVersions{}, Logger: quietLog()})
	for _, source := range []string{SourceLocal, "unknown", ""} {
		info := ledgerUpdate(plain.URL+"/ota.bin", 0xfff1, 0x8000, 3, len(img), 0)
		info.Source = source
		if _, err := imgs.DownloadUpdate(ctx, info, true); !errors.Is(err, ErrOTA) || !strings.Contains(err.Error(), "states no checksum") {
			t.Fatalf("Source %q, no checksum: %v; want the checksum required", source, err)
		}
		if _, err := imgs.DownloadUpdate(ctx, withChecksum(info, img), true); !errors.Is(err, ErrOTA) || !strings.Contains(err.Error(), `"http:"`) {
			t.Fatalf("Source %q, an http URL: %v; want the ledger's https rule", source, err)
		}
	}
	if n := plainHits.Load(); n != 0 {
		t.Fatalf("%d request(s) over http; want none", n)
	}
}

// TestManagerOffersOnlyServableStoredImages: the provider's candidates
// (UpdateExistsFor) are the stored images the store serves — a stored
// prod image not bound to a ledger checksum does not mask a lower one the
// host stored; AllowUnverifiedLedgerImages admits it as the store does.
func TestManagerOffersOnlyServableStoredImages(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, admit := range []bool{false, true} {
		blobs := &MemoryBlobs{}
		imgs := NewImages(ImagesConfig{Blobs: blobs, Logger: quietLog(), AllowUnverifiedLedgerImages: admit})
		if _, err := imgs.Store(ctx, createOtaImage(t, 0xfff1, 0x8000, 3), ModeProd, nil); err != nil {
			t.Fatal(err)
		}
		if err := blobs.Put(ctx, "fff1.8000.prod.4", createOtaImage(t, 0xfff1, 0x8000, 4)); err != nil {
			t.Fatal(err)
		}
		r := newManagerRig(t, func(c *Config) { c.Images = imgs })
		candidates := r.m.storedCandidates(ctx, 0xfff1, 0x8000, 2)
		got := make([]uint32, 0, len(candidates))
		for _, c := range candidates {
			got = append(got, c.SoftwareVersion)
		}
		want := []uint32{3}
		if admit {
			want = []uint32{4, 3}
		}
		if !slices.Equal(got, want) {
			t.Fatalf("AllowUnverifiedLedgerImages %v: candidates %v; want %v", admit, got, want)
		}
	}
}

// failOriginPut fails every write of a provenance record.
type failOriginPut struct{ *MemoryBlobs }

func (f failOriginPut) Put(ctx context.Context, name string, data []byte) error {
	if strings.HasSuffix(name, originSuffix) {
		return errors.New("no room")
	}
	return f.MemoryBlobs.Put(ctx, name, data)
}

// TestImagesProvenanceNamesItsBytes: a stored image's former provenance
// record does not vouch for bytes written after it — it is removed before
// the image is written, and a record that cannot be written leaves none.
func TestImagesProvenanceNamesItsBytes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mem := &MemoryBlobs{}
	img := createOtaImage(t, 0xfff1, 0x8000, 3)
	if _, err := NewImages(ImagesConfig{Blobs: mem, Logger: quietLog()}).Store(ctx, img, ModeProd, nil); err != nil {
		t.Fatal(err)
	}
	imgs := NewImages(ImagesConfig{Blobs: failOriginPut{mem}, Logger: quietLog()})
	if !imgs.vouched(ctx, "fff1.8000.prod.3") {
		t.Fatal("the host-stored image is not vouched for")
	}
	if _, err := imgs.Store(ctx, img, ModeProd, nil); err != nil {
		t.Fatal(err)
	}
	if imgs.vouched(ctx, "fff1.8000.prod.3") {
		t.Fatal("the former record vouches for an image whose record could not be written")
	}
}

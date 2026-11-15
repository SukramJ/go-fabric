// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package softwareupdate

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"
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

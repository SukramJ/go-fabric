// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package softwareupdate

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/controller/dcl"
)

// TestImagesHonourTheLedgersMaxImageSize (review II, R2-P0-bound-C01): both
// of the store's downloads — a ledger update's (DownloadUpdate) and an
// image's by URL (UpdateInfoFromURL), each held in memory until it is
// verified — go through dcl.OTAVersions.Download, so the bound the host
// sets on the ledger it hands in (ImagesConfig.Ledger, MaxImageSize) bounds
// what the store buffers. A record that announces more is refused without
// a fetch and nothing is stored; an image by URL announces no size and is
// cut at the bound; without a host bound the same images pass.
func TestImagesHonourTheLedgersMaxImageSize(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newFetchMock(t)
	img := createOtaImage(t, 0xfff1, 0x8000, 3)
	url := f.add("/ota.bin", img)
	info := ledgerUpdate(url, 0xfff1, 0x8000, 3, len(img), 0)

	blobs := &MemoryBlobs{}
	bounded := NewImages(ImagesConfig{Blobs: blobs, Ledger: &dcl.OTAVersions{MaxImageSize: int64(len(img)) - 1}, Logger: quietLog()})
	_, err := bounded.DownloadUpdate(ctx, info, false)
	if !errors.Is(err, ErrOTA) || !errors.Is(err, dcl.ErrOTAImageTooLarge) || !strings.Contains(err.Error(), "MaxImageSize") {
		t.Fatalf("DownloadUpdate of a %d-byte image under a bound one byte smaller: %v, want ErrOTA wrapping dcl.ErrOTAImageTooLarge", len(img), err)
	}
	if calls := f.callLog(); len(calls) != 0 {
		t.Fatalf("fetched %v, want the refusal before the download", calls)
	}
	if names, _ := blobs.List(ctx); len(names) != 0 {
		t.Fatalf("stored %v, want nothing", names)
	}
	_, err = bounded.UpdateInfoFromURL(ctx, url, LocalImageOptions{})
	if !errors.Is(err, ErrOTA) || !errors.Is(err, dcl.ErrOTAImageTooLarge) {
		t.Fatalf("UpdateInfoFromURL of a %d-byte image under a bound one byte smaller: %v, want ErrOTA wrapping dcl.ErrOTAImageTooLarge", len(img), err)
	}

	// The same images at exactly the bound, and under the default one.
	for _, ledger := range []*dcl.OTAVersions{{MaxImageSize: int64(len(img))}, {}} {
		imgs := NewImages(ImagesConfig{Ledger: ledger, Logger: quietLog()})
		if _, err := imgs.DownloadUpdate(ctx, info, false); err != nil {
			t.Fatalf("DownloadUpdate under a bound of %d: %v", ledger.MaxImageSize, err)
		}
		if _, err := imgs.UpdateInfoFromURL(ctx, url, LocalImageOptions{}); err != nil {
			t.Fatalf("UpdateInfoFromURL under a bound of %d: %v", ledger.MaxImageSize, err)
		}
	}
}

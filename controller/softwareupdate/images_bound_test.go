// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package softwareupdate

import (
	"context"
	"errors"
	"strings"
	"testing"

	"thingfabric.dev/go-fabric/controller/dcl"
)

// TestImagesHonourTheLedgersMaxImageSize (review II, R2-P0-bound-C01): both
// of the store's downloads — a ledger update's and an image's by URL
// (DownloadUpdate of UpdateInfoFromURL's update; UpdateInfoFromURL itself
// reads the header only), each held in memory until it is verified — go
// through dcl.OTAVersions (Download, DownloadHostURL),
// whose MaxImageSize bounds both, so the bound the host
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
	info := withChecksum(ledgerUpdate(url, 0xfff1, 0x8000, 3, len(img), 0), img)

	blobs := &MemoryBlobs{}
	bounded := NewImages(ImagesConfig{Blobs: blobs, Ledger: hostLedger(int64(len(img)) - 1), Logger: quietLog()})
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
	// UpdateInfoFromURL reads the header only (R2-P6-bound-1903); the
	// download of its update is cut at the bound.
	local, err := bounded.UpdateInfoFromURL(ctx, url, LocalImageOptions{})
	if err != nil {
		t.Fatalf("UpdateInfoFromURL: %v", err)
	}
	_, err = bounded.DownloadUpdate(ctx, local, false)
	if !errors.Is(err, ErrOTA) || !errors.Is(err, dcl.ErrOTAImageTooLarge) {
		t.Fatalf("DownloadUpdate by URL of a %d-byte image under a bound one byte smaller: %v, want ErrOTA wrapping dcl.ErrOTAImageTooLarge", len(img), err)
	}

	// The same images at exactly the bound, and under the default one.
	for _, ledger := range []*dcl.OTAVersions{hostLedger(int64(len(img))), hostLedger(0)} {
		imgs := NewImages(ImagesConfig{Ledger: ledger, Logger: quietLog()})
		if _, err := imgs.DownloadUpdate(ctx, info, false); err != nil {
			t.Fatalf("DownloadUpdate under a bound of %d: %v", ledger.MaxImageSize, err)
		}
		if _, err := imgs.UpdateInfoFromURL(ctx, url, LocalImageOptions{}); err != nil {
			t.Fatalf("UpdateInfoFromURL under a bound of %d: %v", ledger.MaxImageSize, err)
		}
	}
}

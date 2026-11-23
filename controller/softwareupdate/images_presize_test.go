// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package softwareupdate

import (
	"context"
	"testing"
)

// TestImagesBufferedDownloadIsSizedFromTheRecord holds part (a) of
// R2-P7-C1-01: a ledger image whose record announces its size downloads
// into a buffer of that size for a Blobs without streaming, not into one
// that regrows as the bytes arrive. Held on the allocation of the call:
// the image once in the buffer and once in MemoryBlobs.Put's copy, with a
// margin for the rest of the call (TLS, the checks); a regrowing buffer
// allocates about twice the image again. Not held under -race, which
// allocates several times more.
func TestImagesBufferedDownloadIsSizedFromTheRecord(t *testing.T) {
	const (
		vid, pid        = 0xfff1, 0x8000
		version  uint32 = 3
		payload         = 8 << 20
	)
	img := bigOtaImage(t, vid, pid, version, payload)
	srv := newVolumeServer(t, img, false)
	info := withChecksum(ledgerUpdate(srv.srv.URL+"/ota.bin", vid, pid, version, len(img), 0), img)
	blobs := plainBlobs{&MemoryBlobs{}}
	imgs := NewImages(ImagesConfig{Blobs: blobs, Ledger: hostLedger(0), Logger: quietLog()})
	var err error
	alloc, peak := allocDuring(func() { _, err = imgs.DownloadUpdate(context.Background(), info, false) })
	if err != nil {
		t.Fatalf("DownloadUpdate: %v", err)
	}
	t.Logf("image %d bytes: the process allocated %d bytes during the call (x%.2f), heap objects rose by up to %d",
		len(img), alloc, float64(alloc)/float64(len(img)), peak)
	if raceEnabled {
		return
	}
	if limit := uint64(len(img)) * 5 / 2; alloc > limit {
		t.Fatalf("the call allocated %d bytes for a %d-byte image; want at most %d (2.5x)", alloc, len(img), limit)
	}
}

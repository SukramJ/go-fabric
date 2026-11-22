// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package softwareupdate

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"thingfabric.dev/go-fabric/controller/dcl"
)

// withChecksum is info with its record's whole-file checksum: the
// SHA-256 of img, base64, as the ledger states it.
func withChecksum(info UpdateInfo, img []byte) UpdateInfo {
	sum := sha256.Sum256(img)
	v := *info.ledger
	v.OTAChecksum = base64.StdEncoding.EncodeToString(sum[:])
	info.ledger = &v
	return info
}

// TestImagesDownloadByOrigin: the store downloads a ledger update through
// dcl.OTAVersions.Download — https only and, without a host client, to a
// public address only — and an image the host named by URL
// (UpdateInfoFromURL, then DownloadUpdate of its update) through
// DownloadHostURL, over http and from the local network.
func TestImagesDownloadByOrigin(t *testing.T) {
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

	// The host's URL: http, loopback, no host client.
	local, err := imgs.UpdateInfoFromURL(ctx, plain.URL+"/image.ota", LocalImageOptions{})
	if err != nil {
		t.Fatalf("UpdateInfoFromURL over http from loopback: %v", err)
	}
	name, err := imgs.DownloadUpdate(ctx, local, true)
	if err != nil || name != "fff1.8000.local.3" || plainHits.Load() != 2 {
		t.Fatalf("DownloadUpdate of the host's URL: %q, err %v, %d request(s); want the local image downloaded again", name, err, plainHits.Load())
	}

	// A ledger record: its http URL is refused before a request, its
	// https URL on loopback at the dial.
	f := newFetchMock(t)
	httpsInfo := withChecksum(ledgerUpdate(f.add("/ota.bin", img), 0xfff1, 0x8000, 3, len(img), 0), img)
	httpInfo := withChecksum(ledgerUpdate(plain.URL+"/ota.bin", 0xfff1, 0x8000, 3, len(img), 0), img)
	before := plainHits.Load()
	if _, err := imgs.DownloadUpdate(ctx, httpInfo, true); !errors.Is(err, ErrOTA) || plainHits.Load() != before {
		t.Fatalf("DownloadUpdate of a ledger record with an http URL: err %v, %d request(s); want ErrOTA before a request", err, plainHits.Load()-before)
	}
	if _, err := imgs.DownloadUpdate(ctx, httpsInfo, true); !errors.Is(err, ErrOTA) || len(f.callLog()) != 0 {
		t.Fatalf("DownloadUpdate of a ledger record on loopback without a host client: err %v, requests %v; want ErrOTA at the dial", err, f.callLog())
	}
	// With the host's client the same record downloads.
	hosted := NewImages(ImagesConfig{Ledger: hostLedger(0), Logger: quietLog()})
	if name, err := hosted.DownloadUpdate(ctx, httpsInfo, true); err != nil || name != "fff1.8000.prod.3" {
		t.Fatalf("DownloadUpdate through a host client: %q, err %v", name, err)
	}
	// A test-net update takes the test-net client: its host client here,
	// while the production client has none.
	testInfo := httpsInfo
	testInfo.Source = SourceTest
	split := hostLedger(0)
	split.Client = &dcl.Client{BaseURL: "https://" + noLedgerHost}
	if name, err := NewImages(ImagesConfig{Ledger: split, Logger: quietLog()}).DownloadUpdate(ctx, testInfo, true); err != nil || name != "fff1.8000.test.3" {
		t.Fatalf("DownloadUpdate of a test-net update through the test-net client: %q, err %v", name, err)
	}
}

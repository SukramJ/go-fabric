// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package softwareupdate

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller/dcl"
)

// TestUpdateInfoFromURLReadsTheHeaderOnly (review II, R2-P6-bound-1903):
// the image host sends the header and a few payload bytes and then never
// ends the answer; UpdateInfoFromURL returns the update from the header
// without waiting for the rest.
func TestUpdateInfoFromURLReadsTheHeaderOnly(t *testing.T) {
	t.Parallel()
	img := createOtaImage(t, 0xfff1, 0x8000, 3)
	headerEnd := headerPrefix + int(binary.LittleEndian.Uint32(img[12:16]))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(img[:headerEnd+8])
		w.(http.Flusher).Flush()
		<-r.Context().Done() // never ends the body
	}))
	t.Cleanup(srv.Close)
	imgs := NewImages(ImagesConfig{Ledger: &dcl.OTAVersions{}, Logger: quietLog()})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	u, err := imgs.UpdateInfoFromURL(ctx, srv.URL+"/image.ota", LocalImageOptions{})
	if err != nil {
		t.Fatalf("UpdateInfoFromURL: %v", err)
	}
	if u.VendorID != 0xfff1 || u.ProductID != 0x8000 || u.SoftwareVersion != 3 {
		t.Fatalf("update %+v", u)
	}
}

// TestHeaderWriter: the writer keeps the bytes up to the end of the header
// the prefix states, however the download splits them, and ends the
// download there; a header size above otaimage.MaxHeaderSize ends it
// after the prefix.
func TestHeaderWriter(t *testing.T) {
	t.Parallel()
	img := createOtaImage(t, 0xfff1, 0x8000, 3)
	headerEnd := headerPrefix + int(binary.LittleEndian.Uint32(img[12:16]))
	for _, chunk := range []int{1, 7, headerPrefix, headerEnd, len(img)} {
		var h headerWriter
		var err error
		for off := 0; off < len(img) && err == nil; off += chunk {
			_, err = h.Write(img[off:min(off+chunk, len(img))])
		}
		if !errors.Is(err, errHeaderRead) || len(h.buf) < headerEnd || len(h.buf) > headerPrefix+1024 {
			t.Fatalf("chunk %d: err %v, kept %d bytes, header ends at %d", chunk, err, len(h.buf), headerEnd)
		}
		if _, err := UpdateInfoFromImage(bytes.NewReader(h.buf), LocalImageOptions{}); err != nil {
			t.Fatalf("chunk %d: %v", chunk, err)
		}
	}
	big := append([]byte(nil), img...)
	binary.LittleEndian.PutUint32(big[12:16], 1<<20)
	var h headerWriter
	if _, err := h.Write(big); !errors.Is(err, errHeaderRead) || len(h.buf) > headerPrefix+1024 {
		t.Fatalf("oversized header: err %v, kept %d", err, len(h.buf))
	}
}

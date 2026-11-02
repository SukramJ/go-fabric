// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package otaimage_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"reflect"
	"runtime"
	"testing"

	"github.com/SukramJ/go-fabric/bdx/otaimage"
)

// FuzzReadHeader feeds ReadHeader, and DecodeHeader on the bytes after
// the prefix, an OTA image's beginning. That is foreign input: an image
// a provider serves or an operator drops in the provider's directory.
// Invariant: both decode or return an error and never panic. A header
// they accept encodes again and decodes to the same header. ReadHeader
// allocates no more for the header than the input supplies, whatever
// size the prefix claims.
func FuzzReadHeader(f *testing.F) {
	for _, digest := range []uint8{1, 7} {
		img, err := otaimage.Create(otaimage.CreateOptions{
			VendorID: 0xFFF1, ProductID: 0x8001, SoftwareVersion: 2, SoftwareVersionString: "2.0",
			Payload: bytes.Repeat([]byte{0xAB}, 64), ImageDigestType: digest,
		})
		if err != nil {
			f.Fatal(err)
		}
		f.Add(img)
	}
	f.Add(hugeHeaderPrefix())
	f.Add(oversizeHeader())
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 1<<16 {
			return
		}
		h, _, raw, err := otaimage.ReadHeader(bytes.NewReader(b))
		if err == nil {
			if len(raw) > len(b) {
				t.Fatalf("ReadHeader returned %d header bytes from %d input bytes", len(raw), len(b))
			}
			roundTrip(t, h)
		}
		if len(b) >= 16 {
			if h, err := otaimage.DecodeHeader(b[16:]); err == nil {
				roundTrip(t, h)
			}
		}
	})
}

func roundTrip(t *testing.T, h otaimage.Header) {
	t.Helper()
	enc, err := h.EncodeTLV()
	if err != nil {
		t.Fatalf("accepted header %+v does not encode: %v", h, err)
	}
	back, err := otaimage.DecodeHeader(enc)
	if err != nil {
		t.Fatalf("re-encoded header does not decode: %v", err)
	}
	if !reflect.DeepEqual(back, h) {
		t.Fatalf("round trip changed the header: %+v, want %+v", back, h)
	}
}

// hugeHeaderPrefix is a 16-byte prefix whose header size claims 4 GiB,
// with nothing after it.
func hugeHeaderPrefix() []byte {
	b := make([]byte, 16)
	binary.LittleEndian.PutUint32(b[0:4], otaimage.FileIdentifier)
	binary.LittleEndian.PutUint64(b[4:12], 1<<40)
	binary.LittleEndian.PutUint32(b[12:16], 0xFFFF_FFFF)
	return b
}

// TestReadHeaderClaimedSizeIsNotAllocated: a prefix that claims a 4 GiB
// header and ends there is "OTA file ended unexpectedly". The reader
// collects only the bytes that arrive, as matter.js OtaImageReader
// #processHeader does (packages/protocol/src/ota/OtaImageReader.ts:157-189
// at 862ad75f). It never allocates the claimed size up front. FuzzReadHeader
// found the up-front allocation.
func TestReadHeaderClaimedSizeIsNotAllocated(t *testing.T) {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, _, _, err := otaimage.ReadHeader(bytes.NewReader(hugeHeaderPrefix()))
	runtime.ReadMemStats(&after)
	if !errors.Is(err, otaimage.ErrInvalid) {
		t.Fatalf("ReadHeader = %v, want ErrInvalid", err)
	}
	if grew := after.TotalAlloc - before.TotalAlloc; grew > 1<<20 {
		t.Fatalf("ReadHeader allocated %d bytes for a header that never arrived", grew)
	}
}

// oversizeHeader is a prefix claiming a header one byte over
// MaxHeaderSize, followed by that many bytes.
func oversizeHeader() []byte {
	b := make([]byte, 16+otaimage.MaxHeaderSize+1)
	binary.LittleEndian.PutUint32(b[0:4], otaimage.FileIdentifier)
	binary.LittleEndian.PutUint64(b[4:12], uint64(len(b)))
	binary.LittleEndian.PutUint32(b[12:16], otaimage.MaxHeaderSize+1)
	return b
}

// TestReadHeaderRefusesOversizeHeader (W4-ctlint-07): a header TLV over
// MaxHeaderSize is ErrInvalid before it is read — chip's kMaxHeaderSize
// (src/lib/core/OTAImageHeader.cpp:53, :128); matter.js has no cap
// (OtaImageReader.ts:157-191), so this is ahead of it. A short image's
// error wraps the read error (W4-ctlint-14).
func TestReadHeaderRefusesOversizeHeader(t *testing.T) {
	t.Parallel()
	if _, _, _, err := otaimage.ReadHeader(bytes.NewReader(oversizeHeader())); !errors.Is(err, otaimage.ErrInvalid) {
		t.Fatalf("ReadHeader = %v, want ErrInvalid", err)
	}
	if _, _, _, err := otaimage.ReadHeader(bytes.NewReader([]byte{1, 2})); !errors.Is(err, otaimage.ErrInvalid) || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("short prefix: %v, want ErrInvalid wrapping io.ErrUnexpectedEOF", err)
	}
	transport := errors.New("link down")
	ok := oversizeHeader()[:16]
	binary.LittleEndian.PutUint32(ok[12:16], 8)
	if _, _, _, err := otaimage.ReadHeader(io.MultiReader(bytes.NewReader(ok), errReader{transport})); !errors.Is(err, transport) {
		t.Fatalf("transport error hidden: %v", err)
	}
}

type errReader struct{ err error }

func (r errReader) Read([]byte) (int, error) { return 0, r.err }

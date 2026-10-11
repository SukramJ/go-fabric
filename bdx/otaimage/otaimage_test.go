// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package otaimage_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	"github.com/SukramJ/go-fabric/bdx/otaimage"
)

func image(t *testing.T, digest uint8) []byte {
	t.Helper()
	img, err := otaimage.Create(otaimage.CreateOptions{
		VendorID: 0xFFF1, ProductID: 0x8001, SoftwareVersion: 2, SoftwareVersionString: "2.0",
		Payload: bytes.Repeat([]byte{0xAB}, 1000), ImageDigestType: digest,
	})
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// TestDigestTypes: every digest matter.js computes (Crypto.ts:64-69)
// round-trips through ExtractPayload; another is refused
// (OtaImageReader.ts:315-318).
func TestDigestTypes(t *testing.T) {
	t.Parallel()
	for _, d := range []uint8{otaimage.DigestSHA256, otaimage.DigestSHA384, otaimage.DigestSHA512, otaimage.DigestSHA3256} {
		if _, err := otaimage.ExtractPayload(bytes.NewReader(image(t, d)), io.Discard, nil); err != nil {
			t.Fatalf("digest %d: %v", d, err)
		}
	}
	if _, err := otaimage.Create(otaimage.CreateOptions{ImageDigestType: 2}); !errors.Is(err, otaimage.ErrInvalid) {
		t.Fatal("sha256_128 accepted")
	}
}

// TestRefusals mirrors OtaImageReader's refusals: identifier (:178-180),
// truncation (:159-162), payload size (:258-260), digest (:325-329), total
// size (:128-134), checksum (:89-93).
func TestRefusals(t *testing.T) {
	t.Parallel()
	good := image(t, otaimage.DigestSHA256)

	badID := append([]byte(nil), good...)
	badID[0] = 0
	if _, err := otaimage.ValidateFile(bytes.NewReader(badID), otaimage.FileOptions{}); !errors.Is(err, otaimage.ErrInvalid) {
		t.Fatal("identifier")
	}
	if _, err := otaimage.ValidateFile(bytes.NewReader(good[:20]), otaimage.FileOptions{}); !errors.Is(err, otaimage.ErrInvalid) {
		t.Fatal("truncated header")
	}
	if _, err := otaimage.ValidateFile(bytes.NewReader(good[:len(good)-1]), otaimage.FileOptions{}); !errors.Is(err, otaimage.ErrInvalid) {
		t.Fatal("short payload")
	}
	flipped := append([]byte(nil), good...)
	flipped[len(flipped)-1] ^= 0xFF
	// file() does not check the digest — only extractPayload does.
	if _, err := otaimage.ValidateFile(bytes.NewReader(flipped), otaimage.FileOptions{}); err != nil {
		t.Fatalf("ValidateFile checked the digest: %v", err)
	}
	if _, err := otaimage.ExtractPayload(bytes.NewReader(flipped), io.Discard, nil); !errors.Is(err, otaimage.ErrInvalid) {
		t.Fatal("digest")
	}
	wrong := uint64(len(good) + 1)
	if _, err := otaimage.ExtractPayload(bytes.NewReader(good), io.Discard, &wrong); !errors.Is(err, otaimage.ErrInvalid) {
		t.Fatal("total size")
	}
	if _, err := otaimage.ValidateFile(bytes.NewReader(good), otaimage.FileOptions{ExpectedTotalSize: &wrong}); !errors.Is(err, otaimage.ErrInvalid) {
		t.Fatal("total size (file)")
	}
	sum := sha256.Sum256(good)
	right := base64.StdEncoding.EncodeToString(sum[:])
	// A matching checksum wins over a size mismatch (:94-106).
	if _, err := otaimage.ValidateFile(bytes.NewReader(good), otaimage.FileOptions{ExpectedChecksum: right, ExpectedTotalSize: &wrong}); err != nil {
		t.Fatal(err)
	}
	if _, err := otaimage.ValidateFile(bytes.NewReader(good), otaimage.FileOptions{ExpectedChecksum: "AAAA"}); !errors.Is(err, otaimage.ErrInvalid) {
		t.Fatal("checksum")
	}
}

// TestHeaderBounds mirrors OtaImageHeader.ts's field bounds.
func TestHeaderBounds(t *testing.T) {
	t.Parallel()
	long := string(bytes.Repeat([]byte("x"), 65))
	h := otaimage.Header{VendorID: 1, ProductID: 1, SoftwareVersion: 1, SoftwareVersionString: long, ImageDigestType: 1, ImageDigest: make([]byte, 32)}
	b, err := h.EncodeTLV()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := otaimage.DecodeHeader(b); !errors.Is(err, otaimage.ErrInvalid) {
		t.Fatal("65-character version string accepted")
	}
	h.SoftwareVersionString = "1"
	h.ImageDigest = make([]byte, 65)
	b, _ = h.EncodeTLV()
	if _, err := otaimage.DecodeHeader(b); !errors.Is(err, otaimage.ErrInvalid) {
		t.Fatal("65-byte digest accepted")
	}
	if _, err := otaimage.DecodeHeader([]byte{0x15, 0x18}); !errors.Is(err, otaimage.ErrInvalid) {
		t.Fatal("empty header accepted")
	}
	// The prefix is '<IQI' (ota_image_tool.py FIXED_HEADER_FORMAT).
	img := image(t, otaimage.DigestSHA256)
	if binary.LittleEndian.Uint64(img[4:12]) != uint64(len(img)) {
		t.Fatal("total size counts prefix, header and payload")
	}
}

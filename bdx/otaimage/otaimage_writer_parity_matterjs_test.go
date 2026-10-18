// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package otaimage_test

// A port of matter.js packages/protocol/test/ota/OtaImageWriterTest.ts at
// 862ad75f: OtaImageWriter.create (fields, digest, total size, full-file
// checksum and its algorithm), read back with the reader. create's result
// is CreateWithChecksum's Result here; an IdentifiedHashAlgorithm name is
// its HashAlgorithmId (SHA-256 1, SHA-384 7, SHA-512 8).

import (
	"bytes"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/binary"
	"regexp"
	"testing"

	"github.com/SukramJ/go-fabric/bdx/otaimage"
)

func TestOtaImageWriterCreate(t *testing.T) {
	t.Run("creates a valid OTA image with all required fields", func(t *testing.T) {
		payload := []byte{0x01, 0x02, 0x03, 0x04, 0x05}
		res := otaCreate(t, otaimage.CreateOptions{
			VendorID: 0xfff1, ProductID: 0x8000, SoftwareVersion: 5, SoftwareVersionString: "v5.0.0",
			MinApplicableSoftwareVersion: u32(4), MaxApplicableSoftwareVersion: u32(4), Payload: payload,
		})
		if len(res.Image) <= len(payload) || res.FullFileChecksum == "" || res.FullFileChecksumType != otaimage.DigestSHA256 {
			t.Fatalf("result = %d bytes, checksum %q type %d", len(res.Image), res.FullFileChecksum, res.FullFileChecksumType)
		}
		if id := binary.LittleEndian.Uint32(res.Image); id != 0x1beef11e {
			t.Fatalf("identifier %#x", id)
		}
	})

	t.Run("creates OTA image without optional min/max versions", func(t *testing.T) {
		res := otaCreate(t, otaimage.CreateOptions{
			VendorID: 0xfff1, ProductID: 0x8000, SoftwareVersion: 10, SoftwareVersionString: "v10.0.0",
			Payload: []byte{0xaa, 0xbb, 0xcc, 0xdd},
		})
		h := otaHeader(t, res.Image)
		if h.MinApplicableSoftwareVersion != nil || h.MaxApplicableSoftwareVersion != nil {
			t.Fatalf("header = %+v", h)
		}
	})

	t.Run("creates OTA image with optional releaseNotesUrl", func(t *testing.T) {
		url := "https://example.com/release-notes"
		res := otaCreate(t, otaimage.CreateOptions{
			VendorID: 0xfff1, ProductID: 0x8000, SoftwareVersion: 3, SoftwareVersionString: "v3.0.0",
			ReleaseNotesURL: &url, Payload: []byte{0x11, 0x22, 0x33},
		})
		h := otaHeader(t, res.Image)
		if h.ReleaseNotesURL == nil || *h.ReleaseNotesURL != url {
			t.Fatalf("releaseNotesUrl = %v", h.ReleaseNotesURL)
		}
	})

	t.Run("computes correct SHA-256 digest of payload", func(t *testing.T) {
		payload := []byte{0xde, 0xad, 0xbe, 0xef}
		want := sha256.Sum256(payload)
		h := otaHeader(t, otaCreate(t, basic(payload)).Image)
		if !bytes.Equal(h.ImageDigest, want[:]) || h.ImageDigestType != 1 {
			t.Fatalf("digest %x type %d, want %x type 1", h.ImageDigest, h.ImageDigestType, want)
		}
	})

	t.Run("sets correct total size in OTA file", func(t *testing.T) {
		opts := basic(make([]byte, 100))
		opts.SoftwareVersion, opts.SoftwareVersionString = 2, "v2.0.0"
		res := otaCreate(t, opts)
		if total := binary.LittleEndian.Uint64(res.Image[4:]); total != uint64(len(res.Image)) {
			t.Fatalf("total size %d, image %d bytes", total, len(res.Image))
		}
	})
}

func TestOtaImageWriterRoundTripWithOtaImageReader(t *testing.T) {
	t.Run("writes and reads OTA image with all fields", func(t *testing.T) {
		payload := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06}
		url := "https://example.com/v7-notes"
		res := otaCreate(t, otaimage.CreateOptions{
			VendorID: 0xfff1, ProductID: 0x8000, SoftwareVersion: 7, SoftwareVersionString: "v7.0.0-beta",
			MinApplicableSoftwareVersion: u32(5), MaxApplicableSoftwareVersion: u32(6), ReleaseNotesURL: &url,
			Payload: payload,
		})
		h := otaHeader(t, res.Image)
		if h.VendorID != 0xfff1 || h.ProductID != 0x8000 || h.SoftwareVersion != 7 ||
			h.SoftwareVersionString != "v7.0.0-beta" || h.PayloadSize != uint64(len(payload)) ||
			h.MinApplicableSoftwareVersion == nil || *h.MinApplicableSoftwareVersion != 5 ||
			h.MaxApplicableSoftwareVersion == nil || *h.MaxApplicableSoftwareVersion != 6 ||
			h.ReleaseNotesURL == nil || *h.ReleaseNotesURL != url || h.ImageDigestType != 1 {
			t.Fatalf("header = %+v", h)
		}
	})

	t.Run("writes and fully validates OTA image with OtaImageReader.file", func(t *testing.T) {
		res := otaCreate(t, otaimage.CreateOptions{
			VendorID: 0xfff2, ProductID: 0x9000, SoftwareVersion: 12, SoftwareVersionString: "v12.1.0",
			MinApplicableSoftwareVersion: u32(10), MaxApplicableSoftwareVersion: u32(11),
			Payload: []byte{0xca, 0xfe, 0xba, 0xbe},
		})
		h, err := otaimage.ValidateFile(bytes.NewReader(res.Image), otaimage.FileOptions{ExpectedTotalSize: u64(len(res.Image))})
		if err != nil {
			t.Fatal(err)
		}
		if h.VendorID != 0xfff2 || h.ProductID != 0x9000 || h.SoftwareVersion != 12 ||
			h.SoftwareVersionString != "v12.1.0" ||
			h.MinApplicableSoftwareVersion == nil || *h.MinApplicableSoftwareVersion != 10 ||
			h.MaxApplicableSoftwareVersion == nil || *h.MaxApplicableSoftwareVersion != 11 {
			t.Fatalf("header = %+v", h)
		}
	})

	t.Run("writes and validates OTA image without optional fields", func(t *testing.T) {
		res := otaCreate(t, otaimage.CreateOptions{
			VendorID: 0xfff3, ProductID: 0x7000, SoftwareVersion: 20, SoftwareVersionString: "v20.0.0",
			Payload: []byte{0xff, 0xee, 0xdd},
		})
		h, err := otaimage.ValidateFile(bytes.NewReader(res.Image), otaimage.FileOptions{ExpectedTotalSize: u64(len(res.Image))})
		if err != nil {
			t.Fatal(err)
		}
		if h.VendorID != 0xfff3 || h.ProductID != 0x7000 || h.SoftwareVersion != 20 ||
			h.MinApplicableSoftwareVersion != nil || h.MaxApplicableSoftwareVersion != nil || h.ReleaseNotesURL != nil {
			t.Fatalf("header = %+v", h)
		}
	})

	t.Run("creates OTA image that validates with correct expected size", func(t *testing.T) {
		payload := bytes.Repeat([]byte{0xab}, 256)
		res := otaCreate(t, otaimage.CreateOptions{
			VendorID: 0xfff1, ProductID: 0x8000, SoftwareVersion: 15, SoftwareVersionString: "v15.0.0",
			MinApplicableSoftwareVersion: u32(14), MaxApplicableSoftwareVersion: u32(14), Payload: payload,
		})
		h, err := otaimage.ValidateFile(bytes.NewReader(res.Image), otaimage.FileOptions{ExpectedTotalSize: u64(len(res.Image))})
		if err != nil {
			t.Fatal(err)
		}
		if h.PayloadSize != uint64(len(payload)) {
			t.Fatalf("payload size %d", h.PayloadSize)
		}
	})

	t.Run("creates different digests for different payloads", func(t *testing.T) {
		h1 := otaHeader(t, otaCreate(t, basic([]byte{0x01, 0x02, 0x03})).Image)
		h2 := otaHeader(t, otaCreate(t, basic([]byte{0x04, 0x05, 0x06})).Image)
		if bytes.Equal(h1.ImageDigest, h2.ImageDigest) {
			t.Fatal("equal digests for different payloads")
		}
	})

	t.Run("computes full file checksum (base64 encoded)", func(t *testing.T) {
		res := otaCreate(t, otaimage.CreateOptions{
			VendorID: 0xfff1, ProductID: 0x8000, SoftwareVersion: 10, SoftwareVersionString: "v10.0.0",
			Payload: []byte{0xaa, 0xbb, 0xcc, 0xdd},
		})
		if res.FullFileChecksum == "" || res.FullFileChecksumType != otaimage.DigestSHA256 {
			t.Fatalf("checksum %q type %d", res.FullFileChecksum, res.FullFileChecksumType)
		}
		if !regexp.MustCompile(`(?i)^[A-Z0-9+/]+=*$`).MatchString(res.FullFileChecksum) {
			t.Fatalf("checksum %q is not base64", res.FullFileChecksum)
		}
		if _, err := otaimage.ValidateFile(bytes.NewReader(res.Image), otaimage.FileOptions{
			ExpectedTotalSize: u64(len(res.Image)), ChecksumType: otaimage.DigestSHA256, ExpectedChecksum: res.FullFileChecksum,
		}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("different images have different full file checksums", func(t *testing.T) {
		r1 := otaCreate(t, basic([]byte{0x01, 0x02}))
		r2 := otaCreate(t, basic([]byte{0x03, 0x04}))
		if r1.FullFileChecksum == r2.FullFileChecksum {
			t.Fatal("equal checksums for different images")
		}
	})
}

func TestOtaImageWriterConfigurableHashAlgorithms(t *testing.T) {
	t.Run("defaults to SHA-256 for both digests when not specified", func(t *testing.T) {
		res := otaCreate(t, basic([]byte{0x01, 0x02, 0x03}))
		if res.FullFileChecksumType != otaimage.DigestSHA256 {
			t.Fatalf("checksum type %d", res.FullFileChecksumType)
		}
		if h := otaHeader(t, res.Image); h.ImageDigestType != 1 {
			t.Fatalf("digest type %d", h.ImageDigestType)
		}
	})

	t.Run("uses custom imageDigestType when specified", func(t *testing.T) {
		opts := basic([]byte{0x01, 0x02, 0x03})
		opts.ImageDigestType = otaimage.DigestSHA512
		h := otaHeader(t, otaCreate(t, opts).Image)
		if h.ImageDigestType != 8 || len(h.ImageDigest) != 64 {
			t.Fatalf("digest type %d, length %d", h.ImageDigestType, len(h.ImageDigest))
		}
	})

	t.Run("uses custom fullFileChecksumType when specified", func(t *testing.T) {
		opts := basic([]byte{0x01, 0x02, 0x03})
		opts.FullFileChecksumType = otaimage.DigestSHA384
		res := otaCreate(t, opts)
		if res.FullFileChecksumType != 7 {
			t.Fatalf("checksum type %d", res.FullFileChecksumType)
		}
		if sum, err := base64.StdEncoding.DecodeString(res.FullFileChecksum); err != nil || len(sum) != 48 {
			t.Fatalf("checksum %d bytes, %v", len(sum), err)
		}
	})

	t.Run("can use different hash algorithms for image digest and full file checksum", func(t *testing.T) {
		opts := basic([]byte{0xaa, 0xbb, 0xcc})
		opts.ImageDigestType, opts.FullFileChecksumType = otaimage.DigestSHA256, otaimage.DigestSHA512
		res := otaCreate(t, opts)
		if res.FullFileChecksumType != 8 {
			t.Fatalf("checksum type %d", res.FullFileChecksumType)
		}
		if h := otaHeader(t, res.Image); h.ImageDigestType != 1 || len(h.ImageDigest) != 32 {
			t.Fatalf("digest type %d, length %d", h.ImageDigestType, len(h.ImageDigest))
		}
		if sum, err := base64.StdEncoding.DecodeString(res.FullFileChecksum); err != nil || len(sum) != 64 {
			t.Fatalf("checksum %d bytes, %v", len(sum), err)
		}
	})

	t.Run("computes correct digest for different hash algorithms", func(t *testing.T) {
		payload := []byte{0xde, 0xad, 0xbe, 0xef}
		o256, o512 := basic(payload), basic(payload)
		o256.ImageDigestType, o512.ImageDigestType = otaimage.DigestSHA256, otaimage.DigestSHA512
		h256 := otaHeader(t, otaCreate(t, o256).Image)
		h512 := otaHeader(t, otaCreate(t, o512).Image)
		w256, w512 := sha256.Sum256(payload), sha512.Sum512(payload)
		if !bytes.Equal(h256.ImageDigest, w256[:]) || !bytes.Equal(h512.ImageDigest, w512[:]) {
			t.Fatalf("digests %x / %x", h256.ImageDigest, h512.ImageDigest)
		}
		if bytes.Equal(h256.ImageDigest, h512.ImageDigest) {
			t.Fatal("equal digests")
		}
	})
}

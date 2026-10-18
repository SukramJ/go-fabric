// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package otaimage_test

// A port of matter.js packages/protocol/test/ota/OtaImageReaderTest.ts at
// 862ad75f: header, file validation (whole-file checksum or payload
// digest), payload extraction and refusals. OtaImageReader.header / file /
// extractPayload are ReadHeader / ValidateFile / ExtractPayload here; a
// ReadableStream is an io.Reader, calculateFullChecksum: false is
// FileOptions.SkipFullChecksum.

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/bdx/otaimage"
)

// otaCreate is OtaImageWriter.create: the image and its full-file checksum.
func otaCreate(t *testing.T, opts otaimage.CreateOptions) otaimage.Result {
	t.Helper()
	res, err := otaimage.CreateWithChecksum(opts)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func otaHeader(t *testing.T, image []byte) otaimage.Header {
	t.Helper()
	h, _, _, err := otaimage.ReadHeader(bytes.NewReader(image))
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func u32(v uint32) *uint32 { return &v }

func u64(v int) *uint64 { n := uint64(v); return &n } //nolint:gosec // test sizes are small

// wantOtaError asserts an OtaImageError whose message matches substr
// (matter.js's rejectedWith(/…/)).
func wantOtaError(t *testing.T, err error, substr string) {
	t.Helper()
	if err == nil || !errors.Is(err, otaimage.ErrInvalid) || !strings.Contains(err.Error(), substr) {
		t.Fatalf("err = %v, want an ota image error matching %q", err, substr)
	}
}

func basic(payload []byte) otaimage.CreateOptions {
	return otaimage.CreateOptions{
		VendorID: 0xfff1, ProductID: 0x8000, SoftwareVersion: 1, SoftwareVersionString: "v1.0.0",
		Payload: payload,
	}
}

func corrupt(image []byte) []byte {
	c := bytes.Clone(image)
	c[len(c)-1] ^= 0xff // flip the last byte
	return c
}

func TestOtaImageReaderHeader(t *testing.T) {
	t.Run("reads OTA image header correctly", func(t *testing.T) {
		payload := []byte{0x01, 0x02, 0x03, 0x04}
		res := otaCreate(t, otaimage.CreateOptions{
			VendorID: 0xfff1, ProductID: 0x8000, SoftwareVersion: 5, SoftwareVersionString: "v5.0.0",
			MinApplicableSoftwareVersion: u32(4), MaxApplicableSoftwareVersion: u32(4), Payload: payload,
		})
		h := otaHeader(t, res.Image)
		if h.VendorID != 0xfff1 || h.ProductID != 0x8000 || h.SoftwareVersion != 5 ||
			h.SoftwareVersionString != "v5.0.0" || h.PayloadSize != uint64(len(payload)) ||
			h.MinApplicableSoftwareVersion == nil || *h.MinApplicableSoftwareVersion != 4 ||
			h.MaxApplicableSoftwareVersion == nil || *h.MaxApplicableSoftwareVersion != 4 ||
			h.ImageDigestType != 1 {
			t.Fatalf("header = %+v", h)
		}
	})

	t.Run("reads OTA image header without optional fields", func(t *testing.T) {
		res := otaCreate(t, otaimage.CreateOptions{
			VendorID: 0xfff2, ProductID: 0x9000, SoftwareVersion: 10, SoftwareVersionString: "v10.0.0",
			Payload: []byte{0xaa, 0xbb, 0xcc},
		})
		h := otaHeader(t, res.Image)
		if h.VendorID != 0xfff2 || h.MinApplicableSoftwareVersion != nil ||
			h.MaxApplicableSoftwareVersion != nil || h.ReleaseNotesURL != nil {
			t.Fatalf("header = %+v", h)
		}
	})

	t.Run("reads header with custom hash algorithm", func(t *testing.T) {
		opts := basic([]byte{0x01, 0x02, 0x03})
		opts.ImageDigestType = otaimage.DigestSHA512
		h := otaHeader(t, otaCreate(t, opts).Image)
		// HashAlgorithmId["SHA-512"] = 8, HASH_ALGORITHM_OUTPUT_LENGTHS["SHA-512"] = 64.
		if h.ImageDigestType != 8 || len(h.ImageDigest) != 64 {
			t.Fatalf("digest type %d, length %d", h.ImageDigestType, len(h.ImageDigest))
		}
	})
}

func TestOtaImageReaderFile(t *testing.T) {
	t.Run("validates OTA image with digest check", func(t *testing.T) {
		res := otaCreate(t, basic([]byte{0xde, 0xad, 0xbe, 0xef}))
		h, err := otaimage.ValidateFile(bytes.NewReader(res.Image), otaimage.FileOptions{ExpectedTotalSize: u64(len(res.Image))})
		if err != nil {
			t.Fatal(err)
		}
		if h.VendorID != 0xfff1 || h.SoftwareVersion != 1 {
			t.Fatalf("header = %+v", h)
		}
	})

	t.Run("validates OTA image with custom hash algorithm", func(t *testing.T) {
		opts := basic([]byte{0x11, 0x22, 0x33})
		opts.ImageDigestType = otaimage.DigestSHA384
		res := otaCreate(t, opts)
		h, err := otaimage.ValidateFile(bytes.NewReader(res.Image), otaimage.FileOptions{ExpectedTotalSize: u64(len(res.Image))})
		if err != nil {
			t.Fatal(err)
		}
		if h.ImageDigestType != 7 { // HashAlgorithmId["SHA-384"]
			t.Fatalf("digest type %d", h.ImageDigestType)
		}
	})

	t.Run("computes full file checksum when enabled", func(t *testing.T) {
		res := otaCreate(t, basic([]byte{0xca, 0xfe}))
		if _, err := otaimage.ValidateFile(bytes.NewReader(res.Image), otaimage.FileOptions{ExpectedTotalSize: u64(len(res.Image))}); err != nil {
			t.Fatal(err)
		}
		if res.FullFileChecksum == "" {
			t.Fatal("no full file checksum")
		}
	})

	t.Run("validates full file checksum when expectedChecksum provided", func(t *testing.T) {
		res := otaCreate(t, basic([]byte{0xaa, 0xbb}))
		if _, err := otaimage.ValidateFile(bytes.NewReader(res.Image), otaimage.FileOptions{
			ExpectedTotalSize: u64(len(res.Image)), ChecksumType: otaimage.DigestSHA256, ExpectedChecksum: res.FullFileChecksum,
		}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("throws error when full file checksum doesn't match", func(t *testing.T) {
		res := otaCreate(t, basic([]byte{0xaa, 0xbb}))
		_, err := otaimage.ValidateFile(bytes.NewReader(res.Image), otaimage.FileOptions{
			ExpectedTotalSize: u64(len(res.Image)), ChecksumType: otaimage.DigestSHA256, ExpectedChecksum: "invalid-checksum",
		})
		wantOtaError(t, err, "checksum mismatch")
	})

	t.Run("supports custom checksum types", func(t *testing.T) {
		opts := basic([]byte{0x01, 0x02})
		opts.FullFileChecksumType = otaimage.DigestSHA512
		res := otaCreate(t, opts)
		if _, err := otaimage.ValidateFile(bytes.NewReader(res.Image), otaimage.FileOptions{
			ExpectedTotalSize: u64(len(res.Image)), ChecksumType: otaimage.DigestSHA512, ExpectedChecksum: res.FullFileChecksum,
		}); err != nil {
			t.Fatal(err)
		}
		sum, err := base64.StdEncoding.DecodeString(res.FullFileChecksum)
		if err != nil || len(sum) != 64 { // SHA-512 = 64 bytes
			t.Fatalf("checksum %d bytes, %v", len(sum), err)
		}
	})

	t.Run("validates payload digest during file reading", func(t *testing.T) {
		image := corrupt(otaCreate(t, basic([]byte{0xff, 0xee, 0xdd})).Image)
		_, err := otaimage.ValidateFile(bytes.NewReader(image), otaimage.FileOptions{
			ExpectedTotalSize: u64(len(image)), SkipFullChecksum: true,
		})
		wantOtaError(t, err, "digest")
	})

	t.Run("rejects an unsupported image digest type with an OtaImageError", func(t *testing.T) {
		payload := []byte{0x01, 0x02, 0x03}
		header, err := otaimage.Header{
			VendorID: 0xfff1, ProductID: 0x8000, SoftwareVersion: 1, SoftwareVersionString: "v1.0.0",
			PayloadSize: uint64(len(payload)), ImageDigestType: 99, ImageDigest: make([]byte, 32),
		}.EncodeTLV()
		if err != nil {
			t.Fatal(err)
		}
		image := binary.LittleEndian.AppendUint32(nil, 0x1beef11e)
		image = binary.LittleEndian.AppendUint64(image, uint64(16+len(header)+len(payload)))
		image = binary.LittleEndian.AppendUint32(image, uint32(len(header))) //nolint:gosec // small
		image = append(append(image, header...), payload...)
		_, err = otaimage.ValidateFile(bytes.NewReader(image), otaimage.FileOptions{SkipFullChecksum: true})
		wantOtaError(t, err, "Unsupported OTA image digest type")
	})

	t.Run("handles different hash algorithms for payload validation", func(t *testing.T) {
		opts := basic([]byte{0x12, 0x34, 0x56})
		opts.ImageDigestType = otaimage.DigestSHA384
		res := otaCreate(t, opts)
		h, err := otaimage.ValidateFile(bytes.NewReader(res.Image), otaimage.FileOptions{ExpectedTotalSize: u64(len(res.Image))})
		if err != nil {
			t.Fatal(err)
		}
		if h.ImageDigestType != 7 {
			t.Fatalf("digest type %d", h.ImageDigestType)
		}
	})
}

func TestOtaImageReaderExtractPayload(t *testing.T) {
	t.Run("extracts payload to writable stream", func(t *testing.T) {
		payload := []byte{0xca, 0xfe, 0xba, 0xbe}
		res := otaCreate(t, basic(payload))
		var out bytes.Buffer
		h, err := otaimage.ExtractPayload(bytes.NewReader(res.Image), &out, u64(len(res.Image)))
		if err != nil {
			t.Fatal(err)
		}
		if h.VendorID != 0xfff1 || h.SoftwareVersion != 1 {
			t.Fatalf("header = %+v", h)
		}
		if !bytes.Equal(out.Bytes(), payload) {
			t.Fatalf("payload %x, want %x", out.Bytes(), payload)
		}
	})

	t.Run("validates digest while extracting payload", func(t *testing.T) {
		image := corrupt(otaCreate(t, basic([]byte{0x11, 0x22, 0x33, 0x44})).Image)
		var out bytes.Buffer
		_, err := otaimage.ExtractPayload(bytes.NewReader(image), &out, u64(len(image)))
		wantOtaError(t, err, "digest")
	})
}

func TestOtaImageReaderErrorHandling(t *testing.T) {
	t.Run("throws error for invalid/truncated file", func(t *testing.T) {
		if _, _, _, err := otaimage.ReadHeader(bytes.NewReader([]byte{0x00, 0x00, 0x00, 0x00})); err == nil {
			t.Fatal("a truncated file was accepted")
		}
	})

	t.Run("throws error for size mismatch", func(t *testing.T) {
		res := otaCreate(t, basic([]byte{0x01, 0x02}))
		_, err := otaimage.ValidateFile(bytes.NewReader(res.Image), otaimage.FileOptions{ExpectedTotalSize: u64(len(res.Image) + 100)})
		wantOtaError(t, err, "size")
	})
}

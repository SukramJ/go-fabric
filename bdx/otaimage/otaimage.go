// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package otaimage reads and writes Matter OTA software image files
// (Matter Core §11.21.2): a fixed little-endian prefix — file identifier
// 0x1BEEF11E (uint32), total size (uint64), header size (uint32) — then the
// header as an anonymous TLV structure, then the payload.
//
// Mirrors matter.js packages/protocol/src/ota/: OtaImageHeader.ts (the
// header's fields and bounds), OtaImageReader.ts (header, file and
// extractPayload) and OtaImageWriter.ts (create). The same layout is
// connectedhomeip's src/app/ota_image_tool.py (HEADER_MAGIC, '<IQI'
// FIXED_HEADER_FORMAT, HeaderTag 0-9 at the harness pin 6170af84).
package otaimage

import (
	"bytes"
	"crypto/sha256"
	"crypto/sha3"
	"crypto/sha512"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"io"

	"github.com/SukramJ/go-fabric/tlv"
)

// FileIdentifier opens every OTA image (OtaImageReader.ts:178).
const FileIdentifier uint32 = 0x1BEEF11E

// prefixSize is the fixed prefix: identifier, total size, header size.
const prefixSize = 4 + 8 + 4

// Digest algorithm identifiers from the IANA Named Information Hash
// Algorithm Registry that matter.js computes (packages/general/src/crypto/
// Crypto.ts:54-74, HashAlgorithmId / hashAlgorithmForId).
const (
	DigestSHA256  uint8 = 1
	DigestSHA384  uint8 = 7
	DigestSHA512  uint8 = 8
	DigestSHA3256 uint8 = 10
)

// ErrInvalid wraps every refusal of an image's content (matter.js
// OtaImageError, OtaImageReader.ts:22-26).
var ErrInvalid = errors.New("ota image")

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// Header is the OTA image header (OtaImageHeader.ts:24-117).
type Header struct {
	VendorID                     uint16  // tag 0
	ProductID                    uint16  // tag 1
	SoftwareVersion              uint32  // tag 2
	SoftwareVersionString        string  // tag 3, 1 to 64
	PayloadSize                  uint64  // tag 4
	MinApplicableSoftwareVersion *uint32 // tag 5, optional
	MaxApplicableSoftwareVersion *uint32 // tag 6, optional
	ReleaseNotesURL              *string // tag 7, optional, 1 to 256
	ImageDigestType              uint8   // tag 8
	ImageDigest                  []byte  // tag 9, max 64
}

// EncodeTLV encodes the header as the anonymous TLV structure of the image.
func (h Header) EncodeTLV() ([]byte, error) {
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutUint(tlv.ContextTag(0), uint64(h.VendorID))
	enc.PutUint(tlv.ContextTag(1), uint64(h.ProductID))
	enc.PutUint(tlv.ContextTag(2), uint64(h.SoftwareVersion))
	enc.PutUTF8(tlv.ContextTag(3), h.SoftwareVersionString)
	enc.PutUint(tlv.ContextTag(4), h.PayloadSize)
	if h.MinApplicableSoftwareVersion != nil {
		enc.PutUint(tlv.ContextTag(5), uint64(*h.MinApplicableSoftwareVersion))
	}
	if h.MaxApplicableSoftwareVersion != nil {
		enc.PutUint(tlv.ContextTag(6), uint64(*h.MaxApplicableSoftwareVersion))
	}
	if h.ReleaseNotesURL != nil {
		enc.PutUTF8(tlv.ContextTag(7), *h.ReleaseNotesURL)
	}
	enc.PutUint(tlv.ContextTag(8), uint64(h.ImageDigestType))
	enc.PutOctets(tlv.ContextTag(9), h.ImageDigest)
	if err := enc.EndContainer(); err != nil {
		return nil, err
	}
	return enc.Bytes()
}

// DecodeHeader decodes the TLV header structure and holds every field to
// its type and bound, as matter.js's TlvObject decode does.
func DecodeHeader(b []byte) (Header, error) {
	dec := tlv.NewDecoder(b)
	open, err := dec.Next()
	if err != nil {
		return Header{}, invalid("header: %v", err)
	}
	if !open.IsContainer || open.Type != tlv.TypeStructure {
		return Header{}, invalid("header is not a structure")
	}
	var h Header
	seen := map[uint32]bool{}
	for {
		el, err := dec.Next()
		if err != nil {
			return Header{}, invalid("header: %v", err)
		}
		if el.IsEndContainer {
			break
		}
		if el.Tag.Kind != tlv.TagKindContext {
			return Header{}, invalid("header field without a context tag")
		}
		n := el.Tag.Number
		seen[n] = true
		switch n {
		case 0:
			v, err := uintField(el, 16, "vendorId")
			h.VendorID = uint16(v) //nolint:gosec // bounded by uintField
			if err != nil {
				return Header{}, err
			}
		case 1:
			v, err := uintField(el, 16, "productId")
			h.ProductID = uint16(v) //nolint:gosec // bounded by uintField
			if err != nil {
				return Header{}, err
			}
		case 2:
			v, err := uintField(el, 32, "softwareVersion")
			h.SoftwareVersion = uint32(v) //nolint:gosec // bounded by uintField
			if err != nil {
				return Header{}, err
			}
		case 3:
			s, err := stringField(el, 1, 64, "softwareVersionString")
			if err != nil {
				return Header{}, err
			}
			h.SoftwareVersionString = s
		case 4:
			v, err := uintField(el, 64, "payloadSize")
			if err != nil {
				return Header{}, err
			}
			h.PayloadSize = v
		case 5, 6:
			v, err := uintField(el, 32, "applicableSoftwareVersion")
			if err != nil {
				return Header{}, err
			}
			u := uint32(v) //nolint:gosec // bounded by uintField
			if n == 5 {
				h.MinApplicableSoftwareVersion = &u
			} else {
				h.MaxApplicableSoftwareVersion = &u
			}
		case 7:
			s, err := stringField(el, 1, 256, "releaseNotesUrl")
			if err != nil {
				return Header{}, err
			}
			h.ReleaseNotesURL = &s
		case 8:
			v, err := uintField(el, 8, "imageDigestType")
			if err != nil {
				return Header{}, err
			}
			h.ImageDigestType = uint8(v) //nolint:gosec // bounded by uintField
		case 9:
			if !isOctets(el.Type) {
				return Header{}, invalid("imageDigest is not an octet string")
			}
			if len(el.Octets) > 64 {
				return Header{}, invalid("imageDigest longer than 64 bytes")
			}
			h.ImageDigest = append([]byte(nil), el.Octets...)
		default:
			if el.IsContainer {
				return Header{}, invalid("unexpected container field %d", n)
			}
		}
	}
	for _, n := range []uint32{0, 1, 2, 3, 4, 8, 9} {
		if !seen[n] {
			return Header{}, invalid("mandatory header field %d missing", n)
		}
	}
	return h, nil
}

func uintField(el tlv.Element, bits int, name string) (uint64, error) {
	if el.Type < tlv.TypeUnsignedInt1 || el.Type > tlv.TypeUnsignedInt8 {
		return 0, invalid("%s is not an unsigned integer", name)
	}
	if bits < 64 && el.Uint >= 1<<bits {
		return 0, invalid("%s %d exceeds %d bits", name, el.Uint, bits)
	}
	return el.Uint, nil
}

func stringField(el tlv.Element, minLen, maxLen int, name string) (string, error) {
	if el.Type < tlv.TypeUTF8Str1 || el.Type > tlv.TypeUTF8Str8 {
		return "", invalid("%s is not a string", name)
	}
	// matter.js bounds a string by its JavaScript length, UTF-16 code
	// units (TlvString.ts:81-90).
	n := 0
	for _, r := range el.String {
		n++
		if r > 0xFFFF {
			n++
		}
	}
	if n < minLen || n > maxLen {
		return "", invalid("%s length %d outside %d to %d", name, n, minLen, maxLen)
	}
	return el.String, nil
}

func isOctets(t tlv.ElementType) bool { return t >= tlv.TypeOctetStr1 && t <= tlv.TypeOctetStr8 }

// NewDigest returns the hash for an image digest type, nil for one matter.js
// does not compute (Crypto.ts:64-74).
func NewDigest(digestType uint8) hash.Hash {
	switch digestType {
	case DigestSHA256:
		return sha256.New()
	case DigestSHA384:
		return sha512.New384()
	case DigestSHA512:
		return sha512.New()
	case DigestSHA3256:
		return sha3.New256()
	}
	return nil
}

// Prefix is the fixed prefix of an image.
type Prefix struct {
	TotalSize  uint64
	HeaderSize uint32
}

// ReadHeader reads the prefix and the header from r, leaving r at the first
// payload byte. It returns the raw header bytes (prefix included) as well.
//
// Mirrors OtaImageReader.#processHeader (OtaImageReader.ts:149-206): an
// image that ends before its header is complete is "OTA file ended
// unexpectedly"; a wrong identifier is refused.
func ReadHeader(r io.Reader) (Header, Prefix, []byte, error) {
	var prefix [prefixSize]byte
	if _, err := io.ReadFull(r, prefix[:]); err != nil {
		return Header{}, Prefix{}, nil, invalid("OTA file ended unexpectedly")
	}
	if binary.LittleEndian.Uint32(prefix[0:4]) != FileIdentifier {
		return Header{}, Prefix{}, nil, invalid("Invalid OTA file identifier")
	}
	p := Prefix{TotalSize: binary.LittleEndian.Uint64(prefix[4:12]), HeaderSize: binary.LittleEndian.Uint32(prefix[12:16])}
	headerTLV := make([]byte, p.HeaderSize)
	if _, err := io.ReadFull(r, headerTLV); err != nil {
		return Header{}, Prefix{}, nil, invalid("OTA file ended unexpectedly")
	}
	h, err := DecodeHeader(headerTLV)
	if err != nil {
		return Header{}, Prefix{}, nil, err
	}
	return h, p, append(prefix[:], headerTLV...), nil
}

// FileOptions are the optional checks of [ValidateFile]
// (OtaImageReader.file's options and expectedTotalSize).
type FileOptions struct {
	// ExpectedTotalSize, when set, must equal the total size the prefix
	// states — unless ExpectedChecksum matched, which wins.
	ExpectedTotalSize *uint64
	// ExpectedChecksum is the base64 SHA-256 of the whole file (the DCL's
	// OtaChecksum); empty skips the comparison.
	ExpectedChecksum string
}

// ValidateFile reads a whole image and checks that its payload has the size
// the header states, and the optional whole-file checksum and total size.
// It does not check the payload digest: matter.js's file() computes the
// whole-file checksum instead when it can (calculateFullChecksum, default
// true), leaving the digest to [ExtractPayload].
//
// Mirrors OtaImageReader.file (OtaImageReader.ts:65-110) with
// #processPayloadDigest's full-checksum branch (:246-260).
func ValidateFile(r io.Reader, opts FileOptions) (Header, error) {
	h, p, raw, err := ReadHeader(r)
	if err != nil {
		return Header{}, err
	}
	sum := sha256.New()
	sum.Write(raw)
	n, err := io.Copy(sum, r)
	if err != nil {
		return Header{}, fmt.Errorf("ota image: read payload: %w", err)
	}
	if uint64(n) != h.PayloadSize { //nolint:gosec // io.Copy never returns a negative count
		return Header{}, invalid("OTA payload size mismatch: expected %d, got %d", h.PayloadSize, n)
	}
	matched := false
	if opts.ExpectedChecksum != "" {
		got := base64.StdEncoding.EncodeToString(sum.Sum(nil))
		if got != opts.ExpectedChecksum {
			return Header{}, invalid("OTA full file checksum mismatch: expected %q, got %q", opts.ExpectedChecksum, got)
		}
		matched = true
	}
	if opts.ExpectedTotalSize != nil && *opts.ExpectedTotalSize != p.TotalSize && !matched {
		return Header{}, invalid("OTA file size mismatch: expected %d, got %d", *opts.ExpectedTotalSize, p.TotalSize)
	}
	return h, nil
}

// ExtractPayload reads a whole image, writes its payload to w and checks the
// payload against the header's digest and size, and the prefix's total size
// against expectedTotalSize when set.
//
// Mirrors OtaImageReader.extractPayload (OtaImageReader.ts:116-137) with
// #extractAndValidatePayload (:282-332).
func ExtractPayload(r io.Reader, w io.Writer, expectedTotalSize *uint64) (Header, error) {
	h, p, _, err := ReadHeader(r)
	if err != nil {
		return Header{}, err
	}
	digest := NewDigest(h.ImageDigestType)
	if digest == nil {
		return Header{}, invalid("Unsupported OTA image digest type: %d", h.ImageDigestType)
	}
	n, err := io.Copy(io.MultiWriter(digest, w), r)
	if err != nil {
		return Header{}, fmt.Errorf("ota image: read payload: %w", err)
	}
	if uint64(n) != h.PayloadSize { //nolint:gosec // io.Copy never returns a negative count
		return Header{}, invalid("OTA payload size mismatch: expected %d, got %d", h.PayloadSize, n)
	}
	if got := digest.Sum(nil); !bytes.Equal(got, h.ImageDigest) {
		return Header{}, invalid("OTA file digest mismatch %x vs. %x", got, h.ImageDigest)
	}
	if expectedTotalSize != nil && *expectedTotalSize != p.TotalSize {
		return Header{}, invalid("OTA file size mismatch: expected %d, got %d", *expectedTotalSize, p.TotalSize)
	}
	return h, nil
}

// CreateOptions describe an image [Create] writes (OtaImageWriter.create's
// options).
type CreateOptions struct {
	VendorID                     uint16
	ProductID                    uint16
	SoftwareVersion              uint32
	SoftwareVersionString        string
	MinApplicableSoftwareVersion *uint32
	MaxApplicableSoftwareVersion *uint32
	ReleaseNotesURL              *string
	Payload                      []byte
	// ImageDigestType defaults to SHA-256.
	ImageDigestType uint8
}

// Create writes an image. Mirrors OtaImageWriter.create
// (OtaImageWriter.ts:41-116): the header with the payload digest, the
// prefix whose total size counts prefix, header and payload.
func Create(opts CreateOptions) ([]byte, error) {
	dt := opts.ImageDigestType
	if dt == 0 {
		dt = DigestSHA256
	}
	digest := NewDigest(dt)
	if digest == nil {
		return nil, invalid("Unsupported OTA image digest type: %d", dt)
	}
	digest.Write(opts.Payload)
	h := Header{
		VendorID:                     opts.VendorID,
		ProductID:                    opts.ProductID,
		SoftwareVersion:              opts.SoftwareVersion,
		SoftwareVersionString:        opts.SoftwareVersionString,
		PayloadSize:                  uint64(len(opts.Payload)),
		MinApplicableSoftwareVersion: opts.MinApplicableSoftwareVersion,
		MaxApplicableSoftwareVersion: opts.MaxApplicableSoftwareVersion,
		ReleaseNotesURL:              opts.ReleaseNotesURL,
		ImageDigestType:              dt,
		ImageDigest:                  digest.Sum(nil),
	}
	headerTLV, err := h.EncodeTLV()
	if err != nil {
		return nil, err
	}
	out := binary.LittleEndian.AppendUint32(nil, FileIdentifier)
	out = binary.LittleEndian.AppendUint64(out, uint64(prefixSize+len(headerTLV)+len(opts.Payload)))
	out = binary.LittleEndian.AppendUint32(out, uint32(len(headerTLV))) //nolint:gosec // a header is far below 4 GiB
	out = append(out, headerTLV...)
	return append(out, opts.Payload...), nil
}

// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package setup

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/SukramJ/go-fabric/tlv"
)

// qrPrefix opens every QR onboarding payload. Mirrors matter.js
// packages/types/src/schema/PairingCodeSchema.ts:232 (PREFIX).
const qrPrefix = "MT:"

// MaxQRCodeLength is the longest single onboarding payload, "MT:" prefix
// counted. Mirrors matter.js packages/types/src/schema/PairingCodeSchema.ts:40
// (MATTER_QR_CODE_SINGLE_PAYLOAD_MAX_LENGTH).
const MaxQRCodeLength = 255

// maxVendorID is the last vendor identifier an onboarding payload may name
// (the last test vendor). Mirrors matter.js
// packages/types/src/datatype/VendorId.ts:21-26 (VendorId's 0..0xFFF4 bound).
const maxVendorID = 0xFFF4

// PBKDF iteration bounds the TLV tail's tag 0x01 is held to. Mirrors matter.js
// packages/types/src/schema/PairingCodeSchema.ts:125-128 (pbkdfIterations bound
// by CRYPTO_PBKDF_ITERATIONS_MIN / _MAX).
const (
	pbkdfIterationsMin = 1000
	pbkdfIterationsMax = 100000
)

// ErrInvalidCode wraps every decode failure of [DecodeQRCode],
// [DecodeQRCodes] and [DecodeManualCode].
var ErrInvalidCode = errors.New("setup: invalid pairing code")

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrInvalidCode}, args...)...)
}

// Extra is the optional TLV data of a QR onboarding payload (Matter Core
// §5.1.5): an anonymous structure after the 88-bit header. It mirrors
// matter.js packages/types/src/schema/PairingCodeSchema.ts:122-142
// (QrCodeTlvDataDefaultFields) plus the manufacturer-specific elements the
// default schema does not name.
type Extra struct {
	// Raw is the TLV data as the code carries it. [QRCode] appends it
	// unchanged after the header.
	Raw []byte
	// SerialNumber is tag 0x00. The wire carries a UTF-8 string or an
	// unsigned integer; an integer is rendered in decimal and
	// SerialNumberNumeric is set.
	SerialNumber        string
	SerialNumberNumeric bool
	// PBKDFIterations (tag 0x01) and PBKDFSalt (tag 0x02) are present
	// together or not at all; 0 / nil is absent.
	PBKDFIterations uint32
	PBKDFSalt       []byte
	// NumberOfDevices is tag 0x03; 0 is absent.
	NumberOfDevices uint8
	// CommissioningTimeout is tag 0x04 in seconds;
	// HasCommissioningTimeout tells an explicit 0 from absent.
	CommissioningTimeout    uint16
	HasCommissioningTimeout bool
	// Vendor holds every manufacturer-specific element (tags 0x80..0xFF)
	// as its encoded TLV bytes, context tag included.
	Vendor map[uint8][]byte
}

// ManualPayload is the content of a manual pairing code (Matter Core
// §5.1.4). Mirrors matter.js packages/types/src/schema/PairingCodeSchema.ts:409-421
// (ManualPairingData) as decodeInternal fills it.
type ManualPayload struct {
	// ShortDiscriminator is the upper four bits of the 12-bit
	// discriminator, the only part a manual code carries.
	ShortDiscriminator uint8
	// Passcode is the 27-bit setup passcode.
	Passcode uint32
	// VendorID and ProductID are set by the 21-digit form only; 0 is
	// "unspecified" (§2.5.2, §2.5.3).
	VendorID  uint16
	ProductID uint16
	// HasVendorProduct is true for the 21-digit form (VID_PID_PRESENT).
	HasVendorProduct bool
}

// IsValidPasscode reports whether passcode is a usable setup passcode:
// 0x0000001..0x5F5E0FE and not one of the trivial codes. Mirrors matter.js
// packages/types/src/schema/PairingCodeSchema.ts:171-178 (isValidPasscode)
// and its list :160-162 (INVALID_PASSCODES); it is [IsValidSetupPIN].
func IsValidPasscode(passcode uint32) bool { return IsValidSetupPIN(passcode) }

// assertValidPayloadIdentity rejects a vendor/product pair an onboarding
// payload may not carry. Mirrors matter.js
// packages/types/src/schema/PairingCodeSchema.ts:217-230
// (assertValidPayloadIdentity); 0 is "unspecified" (:188-200).
func assertValidPayloadIdentity(vendorID, productID uint32) error {
	if vendorID != 0 && vendorID > maxVendorID {
		return invalid("Invalid vendor ID %d in onboarding payload", vendorID)
	}
	if productID > 0xFFFF {
		return invalid("Invalid product ID %d in onboarding payload", productID)
	}
	if productID == 0 && vendorID != 0 {
		return invalid("An onboarding payload naming vendor ID %d must name a product too", vendorID)
	}
	return nil
}

// DecodeQRCode decodes a single "MT:…" onboarding payload, the inverse of
// [QRCode]. A concatenated code ("MT:a*b") is rejected; use [DecodeQRCodes].
func DecodeQRCode(s string) (Payload, error) {
	ps, err := DecodeQRCodes(s)
	if err != nil {
		return Payload{}, err
	}
	if len(ps) != 1 {
		return Payload{}, invalid("code carries %d payloads, want 1", len(ps))
	}
	return ps[0], nil
}

// DecodeQRCodes decodes an "MT:…" onboarding string, which may concatenate
// several payloads with '*'.
//
// Mirrors matter.js packages/types/src/schema/PairingCodeSchema.ts:330-345
// (QrPairingCodeSchema.decodeInternal) followed by the validation decode
// runs by default (Schema.ts:18-25): version 0 and a valid passcode (:279-286),
// the vendor/product identity (:289-293). A TLV tail is parsed with
// [DecodeExtra] when it decodes and kept as raw bytes otherwise.
func DecodeQRCodes(s string) ([]Payload, error) {
	if !strings.HasPrefix(s, qrPrefix) {
		return nil, invalid("The pairing code should start with MT:")
	}
	parts := strings.Split(s[len(qrPrefix):], "*")
	out := make([]Payload, 0, len(parts))
	for _, part := range parts {
		p, err := decodeQRPart(part)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

func decodeQRPart(part string) (Payload, error) {
	data, err := base38Decode(part)
	if err != nil {
		return Payload{}, err
	}
	if len(data) < 11 {
		return Payload{}, invalid("payload carries %d bytes, want at least 11", len(data))
	}
	// Mirrors matter.js PairingCodeSchema.ts:91-99 (QrCodeDataSchema): the
	// 88-bit little-endian header.
	var bits [2]uint64
	for i := range 8 {
		bits[0] |= uint64(data[i]) << (8 * i)
	}
	for i := 8; i < 11; i++ {
		bits[1] |= uint64(data[i]) << (8 * (i - 8))
	}
	field := func(offset, width uint) uint64 {
		var v uint64
		for b := range width {
			pos := offset + b
			if bits[pos/64]>>(pos%64)&1 == 1 {
				v |= 1 << b
			}
		}
		return v
	}
	// G115 below: field masks every value to its width, which fits its type.
	p := Payload{
		Version:       uint8(field(0, 3)),          //nolint:gosec // G115: masked to width
		VendorID:      uint16(field(3, 16)),        //nolint:gosec // G115: masked to width
		ProductID:     uint16(field(19, 16)),       //nolint:gosec // G115: masked to width
		CustomFlow:    uint8(field(35, 2)),         //nolint:gosec // G115: masked to width
		DiscoveryCaps: DiscoveryCaps(field(37, 8)), //nolint:gosec // G115: masked to width
		Discriminator: uint16(field(45, 12)),       //nolint:gosec // G115: masked to width
		Passcode:      uint32(field(57, 27)),       //nolint:gosec // G115: masked to width
	}
	// PairingCodeSchema.ts:279-286 (validate).
	if p.Version != 0 {
		return Payload{}, invalid("Unsupported onboarding payload version %d", p.Version)
	}
	if !IsValidPasscode(p.Passcode) {
		return Payload{}, invalid("Invalid passcode %d", p.Passcode)
	}
	if err := assertValidPayloadIdentity(uint32(p.VendorID), uint32(p.ProductID)); err != nil {
		return Payload{}, err
	}
	// PairingCodeSchema.ts:342: TlvData (if any) follows the fixed-length data.
	// matter.js's decode keeps the tail as bytes and parses it only in a
	// separate decodeTlvData call (:354); a tail DecodeExtra refuses is
	// kept here as Raw alone, so every code matter.js decodes decodes.
	if len(data) > 11 {
		extra, err := DecodeExtra(data[11:])
		if err != nil {
			extra = Extra{Raw: append([]byte(nil), data[11:]...)}
		}
		p.Extra = &extra
	}
	return p, nil
}

// base38Decode is the inverse of base38Encode. Mirrors matter.js
// packages/types/src/schema/Base38Schema.ts:44-101 (decodeInternal and
// decodeBase38, including the group-overflow check :97-99).
func base38Decode(s string) ([]byte, error) {
	switch len(s) % 5 {
	case 0, 2, 4:
	default:
		return nil, invalid("Invalid base38 encoded string length: %d", len(s))
	}
	out := make([]byte, 0, len(s)/5*3+2)
	group := func(off, chars, bytes int) error {
		v := uint32(0)
		for i := chars - 1; i >= 0; i-- {
			c := strings.IndexByte(base38Alphabet, s[off+i])
			if c < 0 {
				return invalid("Unexpected character %q at %d", s[off+i], off+i)
			}
			v = v*38 + uint32(c) //nolint:gosec // G115: an index of the 38-character alphabet
		}
		if v >= 1<<(8*bytes) {
			return invalid("Base38 group at %d decodes to more than %d bytes", off, bytes)
		}
		for i := range bytes {
			out = append(out, byte(v>>(8*i))) //nolint:gosec // G115: one byte of the group, truncation intended
		}
		return nil
	}
	off := 0
	for off < len(s) {
		var err error
		switch rem := len(s) - off; {
		case rem >= 5:
			err = group(off, 5, 3)
			off += 5
		case rem == 4:
			err = group(off, 4, 2)
			off += 4
		default:
			err = group(off, 2, 1)
			off += 2
		}
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// DecodeExtra decodes a QR payload's TLV data (§5.1.5). Mirrors matter.js
// packages/types/src/schema/PairingCodeSchema.ts:354-377 (decodeTlvData):
// the tag ranges of assertValidOnboardingTlvTags (:248-275), the default
// fields (:122-142), the PBKDF both-or-neither rule (:238-242) and a serial
// number that is a UTF-8 string or an unsigned integer (:358-375).
// Manufacturer-specific elements are kept in [Extra.Vendor].
func DecodeExtra(b []byte) (Extra, error) {
	e := Extra{Raw: append([]byte(nil), b...)}
	dec := tlv.NewDecoder(b)
	top, err := dec.Next()
	if err != nil {
		return Extra{}, invalid("TLV data: %v", err)
	}
	if top.Type != tlv.TypeStructure {
		return Extra{}, invalid("TLV data must be an anonymous structure")
	}
	for {
		start := dec.Pos()
		el, err := dec.Next()
		if err != nil {
			return Extra{}, invalid("TLV data: %v", err)
		}
		if el.IsEndContainer {
			break
		}
		if el.Tag.Kind != tlv.TagKindContext {
			return Extra{}, invalid("Onboarding payload top-level TLV elements must use context-specific tags")
		}
		tag := el.Tag.Number
		if tag > 0x04 && tag < 0x80 {
			return Extra{}, invalid("Reserved onboarding payload TLV tag 0x%02X: manufacturer-specific tags must use 0x80–0xFF", tag)
		}
		if el.IsContainer {
			if err := skipRest(dec); err != nil {
				return Extra{}, err
			}
		}
		if tag >= 0x80 {
			if e.Vendor == nil {
				e.Vendor = map[uint8][]byte{}
			}
			e.Vendor[uint8(tag)] = append([]byte(nil), b[start:dec.Pos()]...) //nolint:gosec // G115: a context tag, 0x80..0xFF
			continue
		}
		if err := e.setDefault(tag, el); err != nil {
			return Extra{}, err
		}
	}
	if dec.Remaining() != 0 {
		return Extra{}, invalid("TLV data: %d trailing bytes", dec.Remaining())
	}
	if (e.PBKDFIterations == 0) != (e.PBKDFSalt == nil) {
		return Extra{}, invalid("PBKDF iteration count and salt must both be present or both absent")
	}
	return e, nil
}

func (e *Extra) setDefault(tag uint32, el tlv.Element) error {
	isUint := el.Type >= tlv.TypeUnsignedInt1 && el.Type <= tlv.TypeUnsignedInt8
	switch tag {
	case 0x00:
		switch {
		case el.Type >= tlv.TypeUTF8Str1 && el.Type <= tlv.TypeUTF8Str8:
			e.SerialNumber = el.String
		case isUint:
			e.SerialNumber = strconv.FormatUint(el.Uint, 10)
			e.SerialNumberNumeric = true
		default:
			return invalid("Invalid serial number TLV data")
		}
	case 0x01:
		if !isUint || el.Uint < pbkdfIterationsMin || el.Uint > pbkdfIterationsMax {
			return invalid("invalid PBKDF iteration count")
		}
		e.PBKDFIterations = uint32(el.Uint)
	case 0x02:
		if el.Type < tlv.TypeOctetStr1 || el.Type > tlv.TypeOctetStr8 || len(el.Octets) < 16 || len(el.Octets) > 32 {
			return invalid("invalid PBKDF salt")
		}
		e.PBKDFSalt = append([]byte(nil), el.Octets...)
	case 0x03:
		if !isUint || el.Uint < 1 || el.Uint > 0xFF {
			return invalid("invalid number of devices")
		}
		e.NumberOfDevices = uint8(el.Uint)
	case 0x04:
		if !isUint || el.Uint > 0xFFFF {
			return invalid("invalid commissioning timeout")
		}
		e.CommissioningTimeout = uint16(el.Uint)
		e.HasCommissioningTimeout = true
	}
	return nil
}

// skipRest consumes the rest of a container whose opening marker the
// decoder has just returned.
func skipRest(dec *tlv.Decoder) error {
	for depth := 1; depth > 0; {
		el, err := dec.Next()
		if err != nil {
			return invalid("TLV data: %v", err)
		}
		switch {
		case el.IsEndContainer:
			depth--
		case el.IsContainer:
			depth++
		}
	}
	return nil
}

// DecodeManualCode decodes an 11- or 21-digit manual pairing code, the
// inverse of [ManualCode]. Characters other than digits are ignored.
//
// Mirrors matter.js packages/types/src/schema/PairingCodeSchema.ts:461-493
// (ManualPairingCodeSchema.decodeInternal) followed by the default
// validation (Schema.ts:18-25): the passcode (:429-431) and the
// vendor/product identity (:457-459).
func DecodeManualCode(s string) (ManualPayload, error) {
	// :462 — "we SHALL be robust against other characters".
	var sb strings.Builder
	for i := range len(s) {
		if s[i] >= '0' && s[i] <= '9' {
			sb.WriteByte(s[i])
		}
	}
	code := sb.String()
	if len(code) != 11 && len(code) != 21 {
		return ManualPayload{}, invalid("Invalid pairing code")
	}
	first := int(code[0] - '0')
	// :466-469 — a first digit of 8 or 9 marks a future format.
	if first >= 8 {
		return ManualPayload{}, invalid("Unsupported onboarding payload version (first digit %c)", code[0])
	}
	// :470-472 — the Verhoeff check digit.
	check, err := verhoeffCheck(code[:len(code)-1])
	if err != nil {
		return ManualPayload{}, invalid("%v", err)
	}
	if check != int(code[len(code)-1]-'0') {
		return ManualPayload{}, invalid("Invalid checksum")
	}
	// :473-482 — VID_PID_PRESENT and the length state the same thing.
	hasVP := first&(1<<2) != 0
	if hasVP != (len(code) == 21) {
		if len(code) == 21 {
			return ManualPayload{}, invalid("A 21-digit manual pairing code must set VID_PID_PRESENT")
		}
		return ManualPayload{}, invalid("An 11-digit manual pairing code must not set VID_PID_PRESENT")
	}
	num := func(a, b int) uint32 {
		v, _ := strconv.ParseUint(code[a:b], 10, 32) // digits only, at most 5
		return uint32(v)
	}
	// :483-484.
	mp := ManualPayload{
		ShortDiscriminator: uint8((first&0x03)<<2) | uint8((num(1, 6)>>14)&0x3),
		Passcode:           (num(1, 6) & 0x3FFF) | num(6, 10)<<14,
		HasVendorProduct:   hasVP,
	}
	var vid, pid uint32
	if hasVP {
		vid, pid = num(10, 15), num(15, 20)
	}
	if !IsValidPasscode(mp.Passcode) {
		return ManualPayload{}, invalid("Invalid passcode %d", mp.Passcode)
	}
	if err := assertValidPayloadIdentity(vid, pid); err != nil {
		return ManualPayload{}, err
	}
	// Five decimal digits reach 99999; the identity check above already
	// bounds both to 16 bits — restated here so the narrowing is
	// visibly guarded.
	if vid > 0xFFFF || pid > 0xFFFF {
		return ManualPayload{}, invalid("Invalid vendor/product ID %d/%d in onboarding payload", vid, pid)
	}
	mp.VendorID, mp.ProductID = uint16(vid), uint16(pid)
	return mp, nil
}

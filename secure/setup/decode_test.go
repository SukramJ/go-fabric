// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package setup

import (
	"bytes"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

// The vectors below are copied from matter.js at 0ceb202d,
// packages/types/test/schema/PairingCodeSchemaTest.ts; each cites its line.

// qrVector is PairingCodeSchemaTest.ts:27-40 (QR_CODE, QR_CODE_DATA).
var qrVector = Payload{
	VendorID:      9050,
	ProductID:     65279,
	DiscoveryCaps: DiscoveryBLE,
	Discriminator: 2976,
	Passcode:      34567890,
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestDecodeQRCode_MatterJSVectors(t *testing.T) {
	t.Parallel()
	// :105-109.
	p, err := DecodeQRCode("MT:YNJV7VSC00CMVH7SR00")
	if err != nil {
		t.Fatal(err)
	}
	if p != qrVector {
		t.Fatalf("decoded %+v, want %+v", p, qrVector)
	}
	// :42-46 + :111-115 — a TLV tail 010203 (not a structure, so only the
	// raw bytes round-trip; decodeTlvData is not run by decode in matter.js).
	if p, err := DecodeQRCode("MT:YNJV7VSC00CMVH7E4810AK00"); err != nil || p.Extra == nil || !bytes.Equal(p.Extra.Raw, []byte{1, 2, 3}) {
		t.Fatalf("decode with tail: %v %+v", err, p)
	}
	enc, err := QRCode(Payload{
		VendorID: 9050, ProductID: 65279, DiscoveryCaps: DiscoveryBLE,
		Discriminator: 2976, Passcode: 34567890, Extra: &Extra{Raw: mustHex(t, "010203")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if enc != "MT:YNJV7VSC00CMVH7E4810AK00" {
		t.Fatalf("encoded %s", enc)
	}
	// :48-49 + :117-121 — a multi code.
	ps, err := DecodeQRCodes("MT:YNJV7VSC00CMVH7SR00*YNJV7VSC00CMVH7SR00")
	if err != nil || len(ps) != 2 || ps[1] != qrVector {
		t.Fatalf("multi: %v %+v", err, ps)
	}
	if _, err := DecodeQRCode("MT:YNJV7VSC00CMVH7SR00*YNJV7VSC00CMVH7SR00"); err == nil {
		t.Fatal("DecodeQRCode accepted a multi code")
	}
	// :206-217 — unspecified vendor and product decode as 0 and re-encode.
	p, err = DecodeQRCode("MT:000004KP00KA0648G00")
	if err != nil {
		t.Fatal(err)
	}
	if p.VendorID != 0 || p.ProductID != 0 {
		t.Fatalf("vendor/product %d/%d", p.VendorID, p.ProductID)
	}
	if enc, _ := QRCode(p); enc != "MT:000004KP00KA0648G00" {
		t.Fatalf("re-encoded %s", enc)
	}
}

func TestDecodeQRCode_Rejects(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ code, want string }{
		{"MT:YNJV7VSC00ANG46Y900", "Invalid passcode 12345678"},                                        // :168-171
		{"MT:ZNJV7VSC00CMVH7SR00", "Unsupported onboarding payload version 1"},                         // :186-191
		{"MT:Y.K904KP00KA0648G00", "An onboarding payload naming vendor ID 65521 must name a product"}, // :200-204
		{"MT:U34J029Q00KA0648G00", "Invalid vendor ID 65525"},                                          // :219-221
		{"YNJV7VSC00CMVH7SR00", "should start with MT:"},
		{"MT:YNJV7VSC00CMVH7SR0", "Invalid base38 encoded string length"},
		{"MT:YNJV7VSC00CMVH7SR!0", "Unexpected character"},
		{"MT:.....", "decodes to more than 3 bytes"},
		{"MT:00000", "want at least 11"},
	} {
		_, err := DecodeQRCode(tc.code)
		if err == nil || !errors.Is(err, ErrInvalidCode) || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err %v, want %q", tc.code, err, tc.want)
		}
	}
}

func TestQRCode_RoundTrip(t *testing.T) {
	t.Parallel()
	for _, p := range []Payload{
		qrVector,
		{Passcode: 1, Discriminator: 0}, // :125-134
		{Passcode: 20202021, Discriminator: 3840, DiscoveryCaps: DiscoveryOnIP, VendorID: 0xFFF1, ProductID: 0x8000},
		{Passcode: 99999998, Discriminator: 4095, CustomFlow: 2, VendorID: 0xFFF4, ProductID: 0xFFFF, DiscoveryCaps: 0xFF},
	} {
		code, err := QRCode(p)
		if err != nil {
			t.Fatal(err)
		}
		got, err := DecodeQRCode(code)
		if err != nil {
			t.Fatalf("%s: %v", code, err)
		}
		if got != p {
			t.Fatalf("%s: %+v, want %+v", code, got, p)
		}
	}
}

func TestQRCode_RoundTripWithTLVTail(t *testing.T) {
	t.Parallel()
	// :258-276 — serial number and a vendor-specific field, from the spec.
	raw := mustHex(t, "152C810656656E646F722C000A3132333435363738393018")
	p := qrVector
	p.Extra = &Extra{Raw: raw}
	code, err := QRCode(p)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeQRCode(code)
	if err != nil {
		t.Fatal(err)
	}
	if got.Extra == nil || !bytes.Equal(got.Extra.Raw, raw) {
		t.Fatalf("extra %+v", got.Extra)
	}
	if got.Extra.SerialNumber != "1234567890" || got.Extra.SerialNumberNumeric {
		t.Fatalf("serial %q", got.Extra.SerialNumber)
	}
	if v := got.Extra.Vendor[0x81]; !bytes.Equal(v, mustHex(t, "2C810656656E646F72")) {
		t.Fatalf("vendor element %x", v)
	}
	// Re-encoding the decoded payload gives the same code.
	if again, _ := QRCode(got); again != code {
		t.Fatalf("re-encoded %s, want %s", again, code)
	}
}

func TestDecodeExtra(t *testing.T) {
	t.Parallel()
	// :244-256 — serial number as an unsigned integer.
	e, err := DecodeExtra(mustHex(t, "152600d202964918"))
	if err != nil || e.SerialNumber != "1234567890" || !e.SerialNumberNumeric {
		t.Fatalf("numeric serial: %v %+v", err, e)
	}
	// :278-289 — both PBKDF parameters.
	e, err = DecodeExtra(mustHex(t, "152501e80330021000112233445566778899aabbccddeeff18"))
	if err != nil || e.PBKDFIterations != 1000 || len(e.PBKDFSalt) != 16 {
		t.Fatalf("pbkdf: %v %+v", err, e)
	}
	e, err = DecodeExtra(mustHex(t, "1524030524040a18"))
	if err != nil || e.NumberOfDevices != 5 || !e.HasCommissioningTimeout || e.CommissioningTimeout != 10 {
		t.Fatalf("devices/timeout: %v %+v", err, e)
	}
	// :344-350 — a manufacturer tag in 0x80..0xFF; a nested container too.
	e, err = DecodeExtra(mustHex(t, "1524800735812401011818"))
	if err != nil || len(e.Vendor) != 2 {
		t.Fatalf("vendor: %v %+v", err, e)
	}
	for _, tc := range []struct{ hex, want string }{
		{"152501e80318", "both be present"},                               // :303-307
		{"1530021000112233445566778899aabbccddeeff18", "both be present"}, // :309-313
		{"1524050118", "Reserved onboarding payload TLV tag"},             // :317-325
		{"15247f0118", "Reserved onboarding payload TLV tag"},
		{"154400000118", "context-specific tags"}, // :334-342
		{"15040118", "context-specific tags"},
		{"15", "TLV data"},
		{"0401", "anonymous structure"},
		{"152800", "Invalid serial number"},
		{"1524016418", "PBKDF iteration"},
		{"153002020000250103e818", "PBKDF salt"},
		{"1524030018", "number of devices"},
		{"152c0400", "commissioning timeout"},
		{"151800", "trailing"},
		{"", "TLV data"},
	} {
		_, err := DecodeExtra(mustHex(t, tc.hex))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err %v, want %q", tc.hex, err, tc.want)
		}
	}
	// A tail decodeTlvData refuses is kept raw: matter.js's decode does not
	// parse it (PairingCodeSchema.ts:342).
	p := qrVector
	p.Extra = &Extra{Raw: mustHex(t, "1524050118")}
	code, err := QRCode(p)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeQRCode(code)
	if err != nil || got.Extra == nil || !bytes.Equal(got.Extra.Raw, p.Extra.Raw) || got.Extra.Vendor != nil {
		t.Fatalf("raw tail: %v %+v", err, got.Extra)
	}
}

func TestQRCode_TooLong(t *testing.T) {
	t.Parallel()
	// :137-159 — 135 bytes of string fit, 136 do not.
	tail := func(n int) []byte {
		b := append(mustHex(t, "152c82"), byte(n))
		b = append(b, bytes.Repeat([]byte("1"), n)...)
		return append(b, 0x18)
	}
	p := qrVector
	p.Extra = &Extra{Raw: tail(135)}
	code, err := QRCode(p)
	if err != nil || len(code) != 255 {
		t.Fatalf("135: %v len %d", err, len(code))
	}
	p.Extra = &Extra{Raw: tail(136)}
	if _, err := QRCode(p); err == nil || !strings.Contains(err.Error(), "257 characters (max 255)") {
		t.Fatalf("136: %v", err)
	}
}

// manualVectors is PairingCodeSchemaTest.ts:56-81.
var manualVectors = []struct {
	discriminator uint16
	short         uint8
	passcode      uint32
	code          string
}{
	{2976, 11, 34567890, "26318621095"},
	{10, 0, 23456780, "0112 761 4316"},
	{2001, 7, 23456789, "1604-371-4310"},
}

func TestDecodeManualCode_MatterJSVectors(t *testing.T) {
	t.Parallel()
	for _, v := range manualVectors {
		// :377-385 — the encoder agrees with matter.js.
		enc, err := ManualCode(v.discriminator, v.passcode)
		if err != nil {
			t.Fatal(err)
		}
		digits := strings.NewReplacer(" ", "", "-", "").Replace(v.code)
		if enc != digits {
			t.Errorf("ManualCode(%d,%d) = %s, want %s", v.discriminator, v.passcode, enc, digits)
		}
		// :387-394.
		m, err := DecodeManualCode(v.code)
		if err != nil {
			t.Fatal(err)
		}
		if m.ShortDiscriminator != v.short || m.Passcode != v.passcode || m.HasVendorProduct {
			t.Errorf("%s: %+v", v.code, m)
		}
	}
	// :462-467 — the plan's own 21-digit example.
	m, err := DecodeManualCode("749701123365521327694")
	if err != nil {
		t.Fatal(err)
	}
	if !m.HasVendorProduct || m.VendorID != 0xFFF1 || m.ProductID != 0x8001 {
		t.Fatalf("21-digit: %+v", m)
	}
}

func TestDecodeManualCode_RoundTrip(t *testing.T) {
	t.Parallel()
	// :396-415 plus the range bounds.
	for _, tc := range []struct {
		d uint16
		p uint32
	}{{1234, 123456}, {1234, 1}, {0, 20202021}, {4095, 99999998}, {0xF00, 20202021}} {
		code, err := ManualCode(tc.d, tc.p)
		if err != nil {
			t.Fatal(err)
		}
		m, err := DecodeManualCode(code)
		if err != nil {
			t.Fatalf("%s: %v", code, err)
		}
		if m.Passcode != tc.p || uint16(m.ShortDiscriminator) != tc.d>>8 {
			t.Fatalf("%s: %+v", code, m)
		}
	}
}

func TestDecodeManualCode_Rejects(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ code, want string }{
		{"00852607537", "Invalid passcode 12345678"},                                                  // :425-428
		{"80000000000", "Unsupported onboarding payload version"},                                     // :437-439
		{"349701123365521327696", "A 21-digit manual pairing code must set VID_PID_PRESENT"},          // :443-448
		{"74970112334", "An 11-digit manual pairing code must not set VID_PID_PRESENT"},               // :450-454
		{"749701123365521000006", "An onboarding payload naming vendor ID 65521 must name a product"}, // :471-476
		{"749701123365521655363", "Invalid product ID 65536"},                                         // :482-485
		{"26318621096", "Invalid checksum"},
		{"2631862109", "Invalid pairing code"},
		{"", "Invalid pairing code"},
	} {
		_, err := DecodeManualCode(tc.code)
		if err == nil || !errors.Is(err, ErrInvalidCode) || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err %v, want %q", tc.code, err, tc.want)
		}
	}
}

func TestDecode_EveryInvalidPasscode(t *testing.T) {
	t.Parallel()
	// PairingCodeSchema.ts:160-162 (INVALID_PASSCODES) — every one, through
	// both decoders. ManualCode/QRCode refuse only 0, so the codes are built
	// with the bit layout directly for the others.
	for _, pc := range []uint32{11111111, 22222222, 33333333, 44444444, 55555555, 66666666, 77777777, 88888888, 99999999, 12345678, 87654321} {
		if IsValidPasscode(pc) {
			t.Errorf("IsValidPasscode(%d) = true", pc)
		}
		if pc <= 99999998 {
			code, err := ManualCode(0, pc)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := DecodeManualCode(code); err == nil || !strings.Contains(err.Error(), "Invalid passcode") {
				t.Errorf("manual %d: %v", pc, err)
			}
			q, err := QRCode(Payload{Passcode: pc})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := DecodeQRCode(q); err == nil || !strings.Contains(err.Error(), "Invalid passcode") {
				t.Errorf("qr %d: %v", pc, err)
			}
		}
	}
	if IsValidPasscode(0) || IsValidPasscode(0x5F5E0FF) || !IsValidPasscode(0x5F5E0FE) || !IsValidPasscode(1) {
		t.Error("range bounds") // :535-545
	}
}

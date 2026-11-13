// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package setup

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"
)

// Parity with matter.js packages/types/test/schema/PairingCodeSchemaTest.ts
// and Base38SchemaTest.ts at 862ad75f: the QR and manual pairing codes, the
// QR code's TLV data, assertValidPayloadIdentity, isValidPasscode and the
// base38 codec. The vectors are the file's own; each case names its it.
// Not here (BD-Matter-SetupCodecShape): matter.js's decode(…, false) that
// skips validation, the flowType a manual-code encode takes, and the TLV
// data encoder — the Go decoders always validate, ManualCode is the
// standard-flow code and ManualCodeLong the one that carries VID/PID, and
// a QR code's TLV data is written as the bytes a caller gives.

// wantErr fails unless err is set and its text holds want.
func wantErr(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("err = %v, want one naming %q", err, want)
	}
}

func TestQrPairingCodeCodecParityMatterJS(t *testing.T) {
	t.Parallel()
	const (
		qr        = "MT:YNJV7VSC00CMVH7SR00"
		qrWithTLV = "MT:YNJV7VSC00CMVH7E4810AK00"
	)
	withTLV := qrVector
	withTLV.Extra = &Extra{Raw: []byte{0x01, 0x02, 0x03}}

	t.Run("encodes the data", func(t *testing.T) {
		t.Parallel()
		if got, err := QRCode(qrVector); err != nil || got != qr {
			t.Fatalf("QRCode = %q, %v; want %q", got, err, qr)
		}
	})
	t.Run("encodes the data with TLV Data", func(t *testing.T) {
		t.Parallel()
		if got, err := QRCode(withTLV); err != nil || got != qrWithTLV {
			t.Fatalf("QRCode = %q, %v; want %q", got, err, qrWithTLV)
		}
	})
	t.Run("encodes the data of a multi qrcode", func(t *testing.T) {
		t.Parallel()
		// A multi code is the codes joined by '*', each after the first
		// without its "MT:" prefix (PairingCodeSchemaTest.ts:48).
		a, _ := QRCode(qrVector)
		b, _ := QRCode(withTLV)
		if got := a + "*" + b[3:]; got != qr+"*"+qrWithTLV[3:] {
			t.Fatalf("multi code %q", got)
		}
	})
	t.Run("decodes the data", func(t *testing.T) {
		t.Parallel()
		p, err := DecodeQRCode(qr)
		if err != nil || p.VendorID != 9050 || p.ProductID != 65279 || p.Discriminator != 2976 || p.Passcode != 34567890 || p.DiscoveryCaps != DiscoveryBLE || p.Extra != nil {
			t.Fatalf("DecodeQRCode = %+v, %v", p, err)
		}
	})
	t.Run("decodes the data with TLV Data", func(t *testing.T) {
		t.Parallel()
		p, err := DecodeQRCode(qrWithTLV)
		if err != nil || p.Extra == nil || !bytes.Equal(p.Extra.Raw, []byte{1, 2, 3}) {
			t.Fatalf("DecodeQRCode = %+v, %v", p, err)
		}
	})
	t.Run("decodes the data of a multi qrcode", func(t *testing.T) {
		t.Parallel()
		ps, err := DecodeQRCodes(qr + "*" + qrWithTLV[3:])
		if err != nil || len(ps) != 2 || ps[0].Extra != nil || ps[1].Extra == nil {
			t.Fatalf("DecodeQRCodes = %+v, %v", ps, err)
		}
	})
	t.Run("round-trips a short numeric passcode", func(t *testing.T) {
		t.Parallel()
		p := qrVector
		p.Passcode = 1
		code, err := QRCode(p)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := DecodeQRCode(code); err != nil || got.Passcode != 1 {
			t.Fatalf("passcode %d, %v", got.Passcode, err)
		}
	})
	tlvDataOf := func(length int) *Extra {
		raw := append(append([]byte{0x15, 0x2c, 0x82, byte(length)}, bytes.Repeat([]byte("1"), length)...), 0x18)
		return &Extra{Raw: raw}
	}
	t.Run("encodes a payload of the maximum length", func(t *testing.T) {
		t.Parallel()
		p := qrVector
		p.Extra = tlvDataOf(135)
		if got, err := QRCode(p); err != nil || len(got) != 255 {
			t.Fatalf("QRCode length %d, %v; want 255", len(got), err)
		}
	})
	t.Run("rejects a payload one base38 group past it", func(t *testing.T) {
		t.Parallel()
		p := qrVector
		p.Extra = tlvDataOf(136)
		_, err := QRCode(p)
		wantErr(t, err, "257")
	})
	t.Run("rejects encoding an invalid passcode", func(t *testing.T) {
		t.Parallel()
		p := qrVector
		p.Passcode = 12345678
		_, err := QRCode(p)
		// matter.js's text names the value; this package's names the rule
		// (errInvalidPasscode).
		wantErr(t, err, "invalid passcode")
	})
	t.Run("rejects decoding an invalid passcode", func(t *testing.T) {
		t.Parallel()
		_, err := DecodeQRCode("MT:YNJV7VSC00ANG46Y900")
		wantErr(t, err, "Invalid passcode")
	})
	t.Run("rejects encoding a reserved version", func(t *testing.T) {
		t.Parallel()
		p := qrVector
		p.Version = 1
		_, err := QRCode(p)
		wantErr(t, err, "version 1")
	})
	t.Run("rejects decoding a reserved version", func(t *testing.T) {
		t.Parallel()
		_, err := DecodeQRCode("MT:ZNJV7VSC00CMVH7SR00")
		wantErr(t, err, "version 1")
	})
	t.Run("rejects decoding a product ID of 0 beside a real vendor ID", func(t *testing.T) {
		t.Parallel()
		_, err := DecodeQRCode("MT:Y.K904KP00KA0648G00")
		wantErr(t, err, "An onboarding payload naming vendor ID 65521 must name a product too")
	})
	t.Run("reports an unspecified vendor and product as absent, not as 0", func(t *testing.T) {
		t.Parallel()
		// The Go payload's absent identifier is 0, the value the wire carries.
		p, err := DecodeQRCode("MT:000004KP00KA0648G00")
		if err != nil || p.VendorID != 0 || p.ProductID != 0 {
			t.Fatalf("DecodeQRCode = %+v, %v", p, err)
		}
	})
	t.Run("writes an absent identifier back as the 0 the wire carries", func(t *testing.T) {
		t.Parallel()
		p, err := DecodeQRCode("MT:000004KP00KA0648G00")
		if err != nil {
			t.Fatal(err)
		}
		if got, err := QRCode(p); err != nil || got != "MT:000004KP00KA0648G00" {
			t.Fatalf("QRCode = %q, %v", got, err)
		}
	})
	t.Run("rejects decoding a vendor ID past the last test vendor", func(t *testing.T) {
		t.Parallel()
		_, err := DecodeQRCode("MT:U34J029Q00KA0648G00")
		wantErr(t, err, "Invalid vendor ID 65525")
	})
}

func TestQrCodeTlvDataParityMatterJS(t *testing.T) {
	t.Parallel()
	decode := func(t *testing.T, h string) (Extra, error) {
		t.Helper()
		raw, err := hex.DecodeString(h)
		if err != nil {
			t.Fatal(err)
		}
		return DecodeExtra(raw)
	}
	t.Run("encodes and decodes just serialNumber as string", func(t *testing.T) {
		t.Parallel()
		e, err := decode(t, "152C000A3132333435363738393018")
		if err != nil || e.SerialNumber != "1234567890" || e.SerialNumberNumeric {
			t.Fatalf("DecodeExtra = %+v, %v", e, err)
		}
	})
	t.Run("encodes and decodes just serialNumber as number", func(t *testing.T) {
		t.Parallel()
		e, err := decode(t, "152600d202964918")
		if err != nil || e.SerialNumber != "1234567890" || !e.SerialNumberNumeric {
			t.Fatalf("DecodeExtra = %+v, %v", e, err)
		}
	})
	t.Run("encodes and decodes serial and vendor specific field with custom schema", func(t *testing.T) {
		t.Parallel()
		e, err := decode(t, "152C810656656E646F722C000A3132333435363738393018")
		if err != nil || e.SerialNumber != "1234567890" || hex.EncodeToString(e.Vendor[0x81]) != "2c810656656e646f72" {
			t.Fatalf("DecodeExtra = %+v, %v", e, err)
		}
	})
	t.Run("encodes and decodes both PBKDF parameters together", func(t *testing.T) {
		t.Parallel()
		e, err := decode(t, "152501e80330021000112233445566778899aabbccddeeff18")
		if err != nil || e.PBKDFIterations != 1000 || hex.EncodeToString(e.PBKDFSalt) != "00112233445566778899aabbccddeeff" {
			t.Fatalf("DecodeExtra = %+v, %v", e, err)
		}
	})
	t.Run("rejects decoding PBKDF iterations without salt", func(t *testing.T) {
		t.Parallel()
		_, err := decode(t, "152501e80318")
		wantErr(t, err, "PBKDF iteration count and salt must both be present or both absent")
	})
	t.Run("rejects decoding PBKDF salt without iterations", func(t *testing.T) {
		t.Parallel()
		_, err := decode(t, "1530021000112233445566778899aabbccddeeff18")
		wantErr(t, err, "PBKDF iteration count and salt must both be present or both absent")
	})
	t.Run("rejects decoding a reserved-range top-level tag", func(t *testing.T) {
		t.Parallel()
		for _, h := range []string{"1524050118", "15247f0118"} {
			_, err := decode(t, h)
			wantErr(t, err, "Reserved onboarding payload TLV tag")
		}
	})
	t.Run("rejects a non-context-specific top-level tag", func(t *testing.T) {
		t.Parallel()
		for _, h := range []string{"154400000118", "15040118"} {
			_, err := decode(t, h)
			wantErr(t, err, "must use context-specific tags")
		}
	})
	t.Run("accepts a manufacturer-specific tag in range 0x80-0xFF", func(t *testing.T) {
		t.Parallel()
		e, err := decode(t, "1524800718")
		if err != nil || hex.EncodeToString(e.Vendor[0x80]) != "248007" {
			t.Fatalf("DecodeExtra = %+v, %v", e, err)
		}
	})
}

func TestAssertValidPayloadIdentityParityMatterJS(t *testing.T) {
	t.Parallel()
	// An absent identifier is 0 in Go, the wire's value: matter.js's
	// "however it spells them" is one spelling here.
	t.Run("reads a wire 0 the same as an absent identifier", func(t *testing.T) {
		t.Parallel()
		wantErr(t, assertValidPayloadIdentity(0xfff1, 0), "An onboarding payload naming vendor ID 65521 must name a product too")
	})
	t.Run("accepts a payload that names neither, however it spells them", func(t *testing.T) {
		t.Parallel()
		if err := assertValidPayloadIdentity(0, 0); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("rejects a vendor past the last test vendor whichever form it arrives in", func(t *testing.T) {
		t.Parallel()
		wantErr(t, assertValidPayloadIdentity(0xfff5, 0x8001), "Invalid vendor ID 65525")
	})
}

func TestManualPairingCodeCodecParityMatterJS(t *testing.T) {
	t.Parallel()
	t.Run("round-trips short numeric passcodes using canonical manual code encoding", func(t *testing.T) {
		t.Parallel()
		code, err := ManualCode(1234, 123456)
		if err != nil || len(code) != 11 {
			t.Fatalf("ManualCode = %q, %v", code, err)
		}
		if got, err := DecodeManualCode(code); err != nil || got.Passcode != 123456 {
			t.Fatalf("passcode %d, %v", got.Passcode, err)
		}
	})
	t.Run("round-trips passcode value 1", func(t *testing.T) {
		t.Parallel()
		code, err := ManualCode(1234, 1)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := DecodeManualCode(code); err != nil || got.Passcode != 1 {
			t.Fatalf("passcode %d, %v", got.Passcode, err)
		}
	})
	t.Run("rejects encoding an invalid passcode", func(t *testing.T) {
		t.Parallel()
		_, err := ManualCode(10, 12345678)
		wantErr(t, err, "invalid passcode")
	})
	for _, c := range []struct{ name, code, want string }{
		{"rejects decoding an invalid passcode", "00852607537", "Invalid passcode"},
		{"rejects decoding a manual code whose first digit marks a future format", "80000000000", "Unsupported onboarding payload version"},
		{"rejects a 21-digit code whose VID_PID_PRESENT bit is clear", "349701123365521327696", "A 21-digit manual pairing code must set VID_PID_PRESENT"},
		{"rejects an 11-digit code whose VID_PID_PRESENT bit is set", "74970112334", "An 11-digit manual pairing code must not set VID_PID_PRESENT"},
		{"rejects a product ID of 0 beside a real vendor ID", "749701123365521000006", "An onboarding payload naming vendor ID 65521 must name a product too"},
		{"rejects a product ID the field cannot hold", "749701123365521655363", "Invalid product ID 65536"},
		{"rejects a vendor ID past the last test vendor as an invalid payload", "749701123370000327699", "Invalid vendor ID 70000"},
		{"rejects a vendor ID past the last test vendor as an invalid payload (65525)", "749701123365525327699", "Invalid vendor ID 65525"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, err := DecodeManualCode(c.code)
			wantErr(t, err, c.want)
		})
	}
	t.Run("accepts the plan's own 21-digit example", func(t *testing.T) {
		t.Parallel()
		mp, err := DecodeManualCode("749701123365521327694")
		if err != nil || mp.VendorID != 0xfff1 || mp.ProductID != 0x8001 {
			t.Fatalf("DecodeManualCode = %+v, %v", mp, err)
		}
	})
	t.Run("decodes the last test vendor ID", func(t *testing.T) {
		t.Parallel()
		mp, err := DecodeManualCode("749701123365524327693")
		if err != nil || mp.VendorID != 0xfff4 {
			t.Fatalf("DecodeManualCode = %+v, %v", mp, err)
		}
	})
	t.Run("encodes a custom flow with VID/PID as a 21-digit code", func(t *testing.T) {
		t.Parallel()
		code, err := ManualCodeLong(2976, 34567890, 9050, 65279)
		if err != nil || len(code) != 21 {
			t.Fatalf("ManualCodeLong = %q, %v", code, err)
		}
		if mp, err := DecodeManualCode(code); err != nil || mp.VendorID != 9050 || mp.ProductID != 65279 {
			t.Fatalf("DecodeManualCode = %+v, %v", mp, err)
		}
	})
	t.Run("encodes a standard flow without VID/PID", func(t *testing.T) {
		t.Parallel()
		if code, err := ManualCode(2976, 34567890); err != nil || len(code) != 11 {
			t.Fatalf("ManualCode = %q, %v", code, err)
		}
	})
}

func TestIsValidPasscodeParityMatterJS(t *testing.T) {
	t.Parallel()
	for passcode, want := range map[uint32]bool{
		1: true, 99999998: true, 20202021: true, // accepts the range bounds
		0: false, 99999999: false, // rejects out-of-range values
	} {
		if got := IsValidPasscode(passcode); got != want {
			t.Errorf("IsValidPasscode(%d) = %v, want %v", passcode, got, want)
		}
	}
	// rejects the invalid-passcode list
	for _, passcode := range []uint32{0, 11111111, 22222222, 33333333, 44444444, 55555555, 66666666, 77777777, 88888888, 99999999, 12345678, 87654321} {
		if IsValidPasscode(passcode) {
			t.Errorf("IsValidPasscode(%d) = true", passcode)
		}
	}
}

func TestBase38SchemaParityMatterJS(t *testing.T) {
	t.Parallel()
	const encoded = "-MOA57ZU02IT2L2BJ00"
	decoded, _ := hex.DecodeString("88ffa7915040004751dd02")
	t.Run("encodes a string", func(t *testing.T) {
		t.Parallel()
		if got := base38Encode(decoded); got != encoded {
			t.Fatalf("base38Encode = %q, want %q", got, encoded)
		}
	})
	t.Run("decodes a string", func(t *testing.T) {
		t.Parallel()
		if got, err := base38Decode(encoded); err != nil || !bytes.Equal(got, decoded) {
			t.Fatalf("base38Decode = %x, %v", got, err)
		}
	})
	t.Run("reads back everything encode writes", func(t *testing.T) {
		t.Parallel()
		for length := 1; length <= 24; length++ {
			b := make([]byte, length)
			for i := range b {
				b[i] = byte((i*37 + 11) & 0xff)
			}
			if got, err := base38Decode(base38Encode(b)); err != nil || !bytes.Equal(got, b) {
				t.Errorf("%d bytes: %x, %v", length, got, err)
			}
		}
	})
	t.Run("rejects a length no group count can produce", func(t *testing.T) {
		t.Parallel()
		for _, s := range []string{"0", "000"} {
			if _, err := base38Decode(s); err == nil {
				t.Errorf("base38Decode(%q) accepted", s)
			}
		}
	})
	t.Run("rejects a group holding more than the bytes it stands for", func(t *testing.T) {
		t.Parallel()
		for _, s := range []string{"..", "....."} {
			if _, err := base38Decode(s); err == nil {
				t.Errorf("base38Decode(%q) accepted", s)
			}
		}
	})
}

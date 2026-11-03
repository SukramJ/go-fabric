// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build chiptool

package chiptool

import (
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"strconv"
	"strings"
)

// The onboarding payloads the DD cases hand the DUT, built field by field
// as matter.js's harness builds them (support/chip-testing/test/cert/
// tc-dd-support.ts at 862ad75f): the negative cases need values the
// module's own encoders refuse to write — a reserved version, a trivial
// passcode, a product id of 0 — so the bits and digits are laid out here,
// not encoded through secure/setup, and read back without judging them.

// base38Alphabet is § 5.1.3's Base-38 alphabet.
const base38Alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ-."

// base38Decode reads a Base-38 string: five characters per three bytes,
// four for a trailing two, two for a trailing one, least significant
// character first.
func base38Decode(s string) ([]byte, error) {
	var out []byte
	for off := 0; off < len(s); {
		chars, n := 5, 3
		switch rem := len(s) - off; {
		case rem == 4:
			chars, n = 4, 2
		case rem == 2:
			chars, n = 2, 1
		case rem < 5:
			return nil, fmt.Errorf("base38: %d characters left over", rem)
		}
		v := 0
		for i := chars - 1; i >= 0; i-- {
			c := strings.IndexByte(base38Alphabet, s[off+i])
			if c < 0 {
				return nil, fmt.Errorf("base38: %q is no Base-38 character", s[off+i])
			}
			v = v*38 + c
		}
		for i := range n {
			out = append(out, byte(v>>(8*i))) //nolint:gosec // one byte of the group
		}
		off += chars
	}
	return out, nil
}

// base38Encode is base38Decode's inverse.
func base38Encode(b []byte) string {
	var sb strings.Builder
	for off := 0; off < len(b); off += 3 {
		n := min(3, len(b)-off)
		v := 0
		for i := n - 1; i >= 0; i-- {
			v = v<<8 | int(b[off+i])
		}
		chars := map[int]int{3: 5, 2: 4, 1: 2}[n]
		for range chars {
			sb.WriteByte(base38Alphabet[v%38])
			v /= 38
		}
	}
	return sb.String()
}

// qrBits are the offset and width of § 5.1.3.1 Table 59's fields in the
// payload's fixed 11 bytes (tc-dd-support.ts:456-462).
var qrBits = map[string][2]int{
	"version": {0, 3}, "vendorId": {3, 16}, "productId": {19, 16}, "flowType": {35, 2},
	"discoveryCapabilities": {37, 8}, "discriminator": {45, 12}, "passcode": {57, 27},
}

// qrFieldOrder is the order the evidence names the fields in.
var qrFieldOrder = []string{"prefix", "version", "vendorId", "productId", "flowType", "discoveryCapabilities", "discriminator", "passcode", "tlv"}

// qrPayloadFields reads every field of a QR payload, valid or not
// (tc-dd-support.ts:601-621 qrPayloadFields), as decimal strings; tlv is
// the bytes after the fixed structure in hex.
func qrPayloadFields(payload string) (map[string]string, error) {
	i := strings.IndexByte(payload, ':')
	if i < 0 {
		return nil, fmt.Errorf("%q carries no onboarding payload prefix", payload)
	}
	data, err := base38Decode(payload[i+1:])
	if err != nil {
		return nil, err
	}
	if len(data) < 11 {
		return nil, fmt.Errorf("%q carries %d bytes, not the fixed 11", payload, len(data))
	}
	f := map[string]string{"prefix": payload[:i+1], "tlv": hex.EncodeToString(data[11:])}
	for name, at := range qrBits {
		v := 0
		for b := range at[1] {
			pos := at[0] + b
			v |= int(data[pos/8]>>(pos%8)&1) << b
		}
		f[name] = strconv.Itoa(v)
	}
	return f, nil
}

// qrPayloadWith is the payload with fields substituted bit by bit, the TLV
// data that follows carried through (tc-dd-support.ts:513-544
// qrPayloadWith): it writes what matter.js's own encoder refuses.
func qrPayloadWith(payload string, fields map[string]int) (string, error) {
	if !strings.HasPrefix(payload, "MT:") {
		return "", fmt.Errorf("cannot substitute fields into %q, which is not a QR onboarding payload", payload)
	}
	data, err := base38Decode(payload[3:])
	if err != nil {
		return "", err
	}
	for name, v := range fields {
		at, ok := qrBits[name]
		if !ok {
			return "", fmt.Errorf("no QR payload field %q", name)
		}
		if v < 0 || v >= 1<<at[1] {
			return "", fmt.Errorf("%s %d does not fit %d bits", name, v, at[1])
		}
		for b := range at[1] {
			pos := at[0] + b
			mask := byte(1) << (pos % 8)
			if v>>b&1 == 1 {
				data[pos/8] |= mask
			} else {
				data[pos/8] &^= mask
			}
		}
	}
	return "MT:" + base38Encode(data), nil
}

// checkGeneratedPayload is tc-dd-support.ts:641-672: the payload carries
// the stated fields, and every other one of unchangedFrom's ("" states
// none).
func checkGeneratedPayload(payload string, stated map[string]string, unchangedFrom string) (string, error) {
	got, err := qrPayloadFields(payload)
	if err != nil {
		return "", err
	}
	want := map[string]string{}
	if unchangedFrom != "" {
		if want, err = qrPayloadFields(unchangedFrom); err != nil {
			return "", err
		}
	}
	maps.Copy(want, stated)
	detail := describeFields(payload, got, qrFieldOrder)
	if wrong := mismatches(got, want, qrFieldOrder); len(wrong) > 0 {
		return "", fmt.Errorf("%s; expected %s", detail, strings.Join(wrong, ", "))
	}
	return detail, nil
}

// manualDigitsOrder is the order the evidence names a manual code's parts
// in (tc-dd-support.ts:1583-1610 ManualPairingCodeDigits).
var manualDigitsOrder = []string{"length", "futureFormat", "vidPidPresent", "shortDiscriminator", "passcode", "vendorId", "productId", "checkDigit", "checkDigitCorrect"}

// manualParts are the parts § 5.1.4.1 Table 62 packs into a manual code
// (tc-dd-support.ts:1521-1543 ManualPairingCodeParts).
type manualParts struct {
	futureFormat, vidPidPresent bool
	discriminator               int
	passcode                    int
	vendorID, productID         *int
	checkDigit                  *int
}

func intp(v int) *int { return &v }

// manualPairingCode lays the parts out as digits, whatever the
// specification makes of them (tc-dd-support.ts:1553-1576).
func manualPairingCode(p manualParts) (string, error) {
	switch {
	case p.discriminator < 0 || p.discriminator > 0xfff:
		return "", fmt.Errorf("discriminator %d does not fit a manual pairing code", p.discriminator)
	case p.passcode < 0 || p.passcode > 0x7ffffff:
		return "", fmt.Errorf("passcode %d does not fit a manual pairing code", p.passcode)
	case (p.vendorID == nil) != (p.productID == nil):
		return "", errors.New("a manual pairing code carries a vendor and a product id together or not at all")
	}
	chunk1 := p.discriminator >> 10
	if p.vidPidPresent {
		chunk1 |= 1 << 2
	}
	if p.futureFormat {
		chunk1 = 8 // FUTURE_FORMAT_DIGIT (:1515)
	}
	digits := fmt.Sprintf("%d%05d%04d", chunk1, (p.discriminator&0x300)<<6|p.passcode&0x3fff, p.passcode>>14)
	if p.vendorID != nil {
		digits += fmt.Sprintf("%05d%05d", *p.vendorID, *p.productID)
	}
	if p.checkDigit != nil {
		return digits + strconv.Itoa(*p.checkDigit), nil
	}
	return digits + strconv.Itoa(verhoeff(digits)), nil
}

// manualPairingCodeDigits reads a manual code back without judging it
// (tc-dd-support.ts:1613-1634).
func manualPairingCodeDigits(code string) (map[string]string, error) {
	var sb strings.Builder
	for _, r := range code {
		if r >= '0' && r <= '9' {
			sb.WriteRune(r)
		}
	}
	d := sb.String()
	if len(d) != 11 && len(d) != 21 {
		return nil, fmt.Errorf("%q is %d digits, which is no manual pairing code length", code, len(d))
	}
	num := func(a, b int) int { v, _ := strconv.Atoi(d[a:b]); return v }
	header := num(0, 1)
	check := num(len(d)-1, len(d))
	f := map[string]string{
		"length":             strconv.Itoa(len(d)),
		"futureFormat":       strconv.FormatBool(header >= 8),
		"vidPidPresent":      strconv.FormatBool(header&(1<<2) != 0),
		"shortDiscriminator": strconv.Itoa((header&0x03)<<2 | num(1, 6)>>14&0x03),
		"passcode":           strconv.Itoa(num(1, 6)&0x3fff | num(6, 10)<<14),
		"vendorId":           "",
		"productId":          "",
		"checkDigit":         strconv.Itoa(check),
		"checkDigitCorrect":  strconv.FormatBool(verhoeff(d[:len(d)-1]) == check),
	}
	if len(d) == 21 {
		f["vendorId"], f["productId"] = strconv.Itoa(num(10, 15)), strconv.Itoa(num(15, 20))
	}
	return f, nil
}

// checkGeneratedManualCode is tc-dd-support.ts:1657-1687: the code
// carries a correct check digit unless stated otherwise, every part of
// unchangedFrom but its check digit, the stated parts, and differs from
// differsFrom ("" for none of the two).
func checkGeneratedManualCode(code string, stated map[string]string, unchangedFrom, differsFrom string) (string, error) {
	got, err := manualPairingCodeDigits(code)
	if err != nil {
		return "", err
	}
	want := map[string]string{"checkDigitCorrect": "true"}
	if unchangedFrom != "" {
		src, err := manualPairingCodeDigits(unchangedFrom)
		if err != nil {
			return "", err
		}
		delete(src, "checkDigit")
		maps.Copy(want, src)
	}
	maps.Copy(want, stated)
	detail := describeFields(code, got, manualDigitsOrder)
	wrong := mismatches(got, want, manualDigitsOrder)
	if differsFrom != "" && code == differsFrom {
		wrong = append(wrong, "a code other than "+differsFrom)
	}
	if len(wrong) > 0 {
		return "", fmt.Errorf("%s; expected %s", detail, strings.Join(wrong, ", "))
	}
	return detail, nil
}

// mismatches names each field want states that got does not carry
// (tc-dd-support.ts:559-567 statedMismatches).
func mismatches(got, want map[string]string, order []string) []string {
	var wrong []string
	for _, k := range order {
		if v, ok := want[k]; ok && got[k] != v {
			wrong = append(wrong, k+"="+v)
		}
	}
	return wrong
}

func describeFields(code string, f map[string]string, order []string) string {
	parts := make([]string, 0, len(order))
	for _, k := range order {
		if v := f[k]; v != "" {
			parts = append(parts, k+"="+v)
		}
	}
	return fmt.Sprintf("generated %s carrying %s", code, strings.Join(parts, " "))
}

// The Verhoeff check digit § 5.1.4.1 puts last (Dihedral group D5).
var (
	verhoeffD = [10][10]int{
		{0, 1, 2, 3, 4, 5, 6, 7, 8, 9},
		{1, 2, 3, 4, 0, 6, 7, 8, 9, 5},
		{2, 3, 4, 0, 1, 7, 8, 9, 5, 6},
		{3, 4, 0, 1, 2, 8, 9, 5, 6, 7},
		{4, 0, 1, 2, 3, 9, 5, 6, 7, 8},
		{5, 9, 8, 7, 6, 0, 4, 3, 2, 1},
		{6, 5, 9, 8, 7, 1, 0, 4, 3, 2},
		{7, 6, 5, 9, 8, 2, 1, 0, 4, 3},
		{8, 7, 6, 5, 9, 3, 2, 1, 0, 4},
		{9, 8, 7, 6, 5, 4, 3, 2, 1, 0},
	}
	verhoeffP = [8][10]int{
		{0, 1, 2, 3, 4, 5, 6, 7, 8, 9},
		{1, 5, 7, 6, 2, 8, 3, 0, 9, 4},
		{5, 8, 0, 3, 7, 9, 6, 1, 4, 2},
		{8, 9, 1, 6, 0, 4, 3, 5, 2, 7},
		{9, 4, 5, 3, 1, 2, 6, 8, 7, 0},
		{4, 2, 8, 6, 5, 7, 3, 9, 0, 1},
		{2, 7, 9, 3, 8, 0, 6, 4, 1, 5},
		{7, 0, 4, 6, 9, 1, 3, 2, 5, 8},
	}
	verhoeffInv = [10]int{0, 4, 3, 2, 1, 5, 6, 7, 8, 9}
)

// verhoeff is the check digit of a string of decimal digits.
func verhoeff(digits string) int {
	c := 0
	for i := range len(digits) {
		c = verhoeffD[c][verhoeffP[(i+1)%8][digits[len(digits)-1-i]-'0']]
	}
	return verhoeffInv[c]
}

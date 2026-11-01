// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package tlv

import (
	"strings"
	"unicode/utf8"
)

// decodeUTF8 turns a UTF-8 string element's body into a Go string the way
// matter.js reads it: DataReader.readUtf8String decodes with a default
// (non-fatal) TextDecoder (packages/general/src/util/DataReader.ts:66-68
// at 0ceb202d), so an ill-formed sequence does not fail the decode but
// becomes U+FFFD — one per maximal subpart, the WHATWG Encoding
// Standard's UTF-8 decoder. A well-formed body is returned unchanged.
func decodeUTF8(body []byte) string {
	if utf8.Valid(body) {
		return string(body)
	}
	var b strings.Builder
	b.Grow(len(body) + 8)
	for i := 0; i < len(body); {
		c := body[i]
		if c < 0x80 {
			b.WriteByte(c)
			i++
			continue
		}
		var need int
		lower, upper := byte(0x80), byte(0xBF)
		switch {
		case c >= 0xC2 && c <= 0xDF:
			need = 1
		case c == 0xE0:
			need, lower = 2, 0xA0
		case c == 0xED:
			need, upper = 2, 0x9F
		case c >= 0xE1 && c <= 0xEF:
			need = 2
		case c == 0xF0:
			need, lower = 3, 0x90
		case c == 0xF4:
			need, upper = 3, 0x8F
		case c >= 0xF1 && c <= 0xF3:
			need = 3
		default:
			b.WriteRune(utf8.RuneError)
			i++
			continue
		}
		j := i + 1
		for range need {
			if j >= len(body) || body[j] < lower || body[j] > upper {
				break
			}
			lower, upper = 0x80, 0xBF
			j++
		}
		if j-i == need+1 {
			b.Write(body[i:j])
		} else {
			// The maximal subpart [i, j) is replaced by one U+FFFD; the
			// byte that broke it is read again as a new lead byte.
			b.WriteRune(utf8.RuneError)
		}
		i = j
	}
	return b.String()
}

// utf16Length is the length of s in UTF-16 code units, the unit of a
// JavaScript string's length; s is well-formed (decodeUTF8's output).
func utf16Length(s string) uint64 {
	var n uint64
	for _, r := range s {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}

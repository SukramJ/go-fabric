// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package otbr

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// KeyFormat is the key casing an OTBR's REST surface uses
// (OtbrRestCapability.ts:9-18): pascal on older builds, camel on
// post-2024 builds.
type KeyFormat string

// The key formats.
const (
	KeyFormatCamel  KeyFormat = "camel"
	KeyFormatPascal KeyFormat = "pascal"
)

// irregularKeys are the acronym-bearing keys whose camel form is not the
// first letter lowered: python-otbr-api's _PASCAL_TO_CAMEL table
// (caseNormalizer.ts:8-17).
var irregularKeys = map[string]string{
	"BaId":           "baId",
	"IP6AddressList": "ip6AddressList",
	"MACCounters":    "macCounters",
	"PSKc":           "pskc",
}

// normalizeKey is caseNormalizer.ts:19-24.
func normalizeKey(key string) string {
	if mapped, ok := irregularKeys[key]; ok {
		return mapped
	}
	if key == "" {
		return key
	}
	return strings.ToLower(key[:1]) + key[1:]
}

// DetectKeyFormat classifies a response object by its key casing
// (caseNormalizer.ts:32-47): an upper-case first letter on any key is
// pascal, a non-empty object of lower-case-first keys camel, an object
// without a non-empty key no signal ("").
func DetectKeyFormat(raw map[string]any) KeyFormat {
	sawKey := false
	for key := range raw {
		if key == "" {
			continue
		}
		sawKey = true
		if key[0] >= 'A' && key[0] <= 'Z' {
			return KeyFormatPascal
		}
	}
	if sawKey {
		return KeyFormatCamel
	}
	return ""
}

// NormalizeKeys rewrites the keys of every object in value to camelCase,
// recursively (caseNormalizer.ts:49-66); arrays are walked, everything
// else passes through. It is idempotent.
func NormalizeKeys(value any) any {
	switch v := value.(type) {
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = NormalizeKeys(item)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, item := range v {
			out[normalizeKey(k)] = NormalizeKeys(item)
		}
		return out
	}
	return value
}

// decodeJSON parses a body, numbers kept as json.Number so a 64-bit counter
// keeps its precision (translation.ts:140-160 decodes such strings
// directly for the same reason).
func decodeJSON(body []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, &json.SyntaxError{Offset: dec.InputOffset()}
	}
	return v, nil
}

// asNumber is a finite JSON number (OtbrRestClient.ts:110-125,
// translation.ts:44-46); false otherwise.
func asNumber(v any) (float64, bool) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, false
	}
	f, err := n.Float64()
	if err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
		return 0, false
	}
	return f, true
}

// asUint is a finite JSON number that is an integer within [0, max].
func asUint(v any, maxValue uint64) (uint64, bool) {
	f, ok := asNumber(v)
	if !ok || f < 0 || f != math.Trunc(f) || f > float64(maxValue) {
		return 0, false
	}
	return uint64(f), true
}

var hexDigits = regexp.MustCompile(`^[0-9a-fA-F]*$`)

// parseHexBytes decodes hex, refusing non-hex characters, an odd length
// and, with expectedLen > 0, a wrong byte count (parseHexBytes.ts:10-28).
func parseHexBytes(s, where string, expectedLen int) ([]byte, error) {
	if !hexDigits.MatchString(s) {
		return nil, restError(CodeProtocol, "%s: not hex", where)
	}
	if len(s)%2 != 0 {
		return nil, restError(CodeProtocol, "%s: odd hex length", where)
	}
	if expectedLen > 0 && len(s) != expectedLen*2 {
		return nil, restError(CodeProtocol, "%s: expected %d bytes, got %d", where, expectedLen, len(s)/2)
	}
	b, _ := hex.DecodeString(s)
	return b, nil
}

var rloc16String = regexp.MustCompile(`^(?:0x[0-9a-fA-F]{1,4}|[0-9]{1,5})$`)

// parseRloc16 reads a 16-bit RLOC from a JSON number (legacy builds) or a
// hex string like "0x4800" (post-2024 builds); false for an absent,
// malformed or out-of-range value (parseHexBytes.ts:33-47).
func parseRloc16(v any) (uint16, bool) {
	switch x := v.(type) {
	case json.Number:
		n, ok := asUint(x, math.MaxUint16)
		return uint16(n), ok
	case string:
		if !rloc16String.MatchString(x) {
			return 0, false
		}
		var n uint64
		var err error
		if strings.HasPrefix(x, "0x") {
			n, err = strconv.ParseUint(x[2:], 16, 32)
		} else {
			n, err = strconv.ParseUint(x, 10, 32)
		}
		if err != nil || n > math.MaxUint16 {
			return 0, false
		}
		return uint16(n), true
	}
	return 0, false
}

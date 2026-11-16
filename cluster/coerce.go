// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package cluster

import "math"

// The bridge's attribute decoder surfaces a TLV-decoded write value as a
// width-independent Go numeric — unsigned integers as uint64, signed as
// int64, floats as float64 (see bridge/attribute_value_reader.go). A
// cluster server that stores a narrow native type (enum8, int16, …) must
// therefore accept any numeric and narrow it, rather than asserting one
// exact Go type: a strict `value.(uint8)` rejects the uint64 the decoder
// produces and the whole Write fails with IM status Failure.

// writeInt funnels any supported numeric write value to int64. Returns
// false for non-numeric values (string, []byte, struct, nil), for a uint64
// beyond int64 and for a float that is not a whole number within int64:
// none of them has an int64 it could be narrowed from without wrapping.
func writeInt(v any) (int64, bool) {
	switch x := v.(type) {
	case int:
		return int64(x), true
	case int8:
		return int64(x), true
	case int16:
		return int64(x), true
	case int32:
		return int64(x), true
	case int64:
		return x, true
	case uint8:
		return int64(x), true
	case uint16:
		return int64(x), true
	case uint32:
		return int64(x), true
	case uint64:
		if x > math.MaxInt64 {
			return 0, false
		}
		return int64(x), true
	case float32:
		return wholeFloat(float64(x))
	case float64:
		return wholeFloat(x)
	}
	return 0, false
}

// wholeFloat is f as an int64 when it is a whole number within int64's
// range (2^63 itself is not: it is the first float beyond MaxInt64).
func wholeFloat(f float64) (int64, bool) {
	if math.IsNaN(f) || f != math.Trunc(f) || f < math.MinInt64 || f >= math.MaxInt64 {
		return 0, false
	}
	return int64(f), true
}

// AsUint8 coerces a decoded write value (uint64/int64/native ints/a
// whole float) to uint8 — the Go type cluster servers store for enum8 /
// uint8 attributes such as Thermostat.SystemMode or WindowCovering modes.
// A value outside 0..255 is refused (false), not wrapped (review II,
// R2-P6-api-1310: 258 used to read as 2).
func AsUint8(v any) (uint8, bool) {
	n, ok := writeInt(v)
	if !ok || n < 0 || n > math.MaxUint8 {
		return 0, false
	}
	return uint8(n), true
}

// AsInt16 coerces a decoded write value to int16 — the Go type cluster
// servers store for signed 16-bit attributes such as Thermostat
// setpoints (centi-degrees Celsius). A value outside int16 is refused
// (false), not wrapped (review II, R2-P6-api-1310: 65541 used to read as
// 5).
func AsInt16(v any) (int16, bool) {
	n, ok := writeInt(v)
	if !ok || n < math.MinInt16 || n > math.MaxInt16 {
		return 0, false
	}
	return int16(n), true
}

// AsUintMax coerces a decoded write value to an unsigned integer no
// larger than maxValue, rejecting — rather than wrapping — a negative
// value, a value above maxValue and a non-integer. It is the checked
// counterpart of [AsUint8] for a width or constraint other than uint8's: matter.js validates a written number
// against the attribute's TLV type bounds before any behavior runs
// (packages/types/src/tlv/TlvNumber.ts validateBoundaries) and answers
// ConstraintError on a miss, so a cluster server that narrowed
// silently would accept 256 as 0.
func AsUintMax(v any, maxValue uint64) (uint64, bool) {
	var n uint64
	switch x := v.(type) {
	case uint8:
		n = uint64(x)
	case uint16:
		n = uint64(x)
	case uint32:
		n = uint64(x)
	case uint64:
		n = x
	case int, int8, int16, int32, int64:
		i, _ := writeInt(x)
		if i < 0 {
			return 0, false
		}
		n = uint64(i)
	default:
		return 0, false
	}
	if n > maxValue {
		return 0, false
	}
	return n, true
}

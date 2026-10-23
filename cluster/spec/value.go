// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec

import "github.com/SukramJ/go-fabric/tlv"

// Value is a scalar a controller writes — an attribute value
// (client.Node.Write, fabrictest.Write) or a command field — as an
// [Encodable]: one of the integer widths, an enum or bitmap of a generated
// package, bool, a float, a string, an octet string, or the null of a
// nullable attribute. A generated struct, request or [List] is its own
// Encodable; Value covers what a definition types as a scalar, so a write
// needs no tlv:
//
//	fabrictest.Write(ctx, pair, path, spec.Int16(2250))
//	fabrictest.Write(ctx, pair, path, spec.Uint(onoffdef.StartUpOnOffToggle))
//	fabrictest.Write(ctx, pair, path, spec.Null[uint16]())
//
// A non-null value of a nullable attribute is the plain scalar
// (spec.Uint16(5)), as the wire has no separate form for it. Integers go
// on the wire at the smallest width that holds them, as every encoder of
// this module and matter.js's TlvUInt/TlvInt write them
// (packages/types/src/tlv/TlvNumber.ts); the width of the constructor is
// the Go type the value is held in, and the peer checks the attribute's
// range. matter.js encodes a written value by the attribute's model
// (packages/protocol/src/action/request/Write.ts:149-185 at 862ad75f,
// TlvOfModel(attribute)); a Go value carries its type, so the constructor
// names it.
//
// The zero Value encodes TLV null, as [Null] does. [Value.Get] returns the
// value and whether it is one.
// fabric:reachable:reason="generic: every typed write instantiates it, and RTA records the instantiation, not the generic declaration"
type Value[T any] struct {
	v   T
	put Put[T]
}

// EncodeTLV implements [Encodable]: the value under tag, or TLV null for
// [Null] and the zero Value.
func (v Value[T]) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	if v.put == nil {
		enc.PutNull(tag)
		return
	}
	v.put(enc, tag, v.v)
}

// Get returns the value; ok false for the null.
func (v Value[T]) Get() (value T, ok bool) { return v.v, v.put != nil }

// IsNull reports whether v is the null.
func (v Value[T]) IsNull() bool { return v.put == nil }

// Uint is an unsigned integer, enum or bitmap of any width — a generated
// enum or bitmap type (onoffdef.StartUpOnOffEnum) keeps its type.
// fabric:reachable:reason="generic: every typed write instantiates it, and RTA records the instantiation, not the generic declaration"
func Uint[T Unsigned](v T) Value[T] { return Value[T]{v: v, put: PutUint[T]} }

// Int is a signed integer of any width.
// fabric:reachable:reason="generic: every typed write instantiates it, and RTA records the instantiation, not the generic declaration"
func Int[T Signed](v T) Value[T] { return Value[T]{v: v, put: PutInt[T]} }

// Uint8 is a uint8 (and an enum8 or map8 without its generated type).
func Uint8(v uint8) Value[uint8] { return Uint(v) }

// Uint16 is a uint16.
func Uint16(v uint16) Value[uint16] { return Uint(v) }

// Uint32 is a uint32.
func Uint32(v uint32) Value[uint32] { return Uint(v) }

// Uint64 is a uint64.
func Uint64(v uint64) Value[uint64] { return Uint(v) }

// Int8 is an int8.
func Int8(v int8) Value[int8] { return Int(v) }

// Int16 is an int16 — a temperature in 0.01 °C, for one.
func Int16(v int16) Value[int16] { return Int(v) }

// Int32 is an int32.
func Int32(v int32) Value[int32] { return Int(v) }

// Int64 is an int64.
func Int64(v int64) Value[int64] { return Int(v) }

// Bool is a boolean.
func Bool(v bool) Value[bool] { return Value[bool]{v: v, put: PutBool} }

// Float32 is a single-precision float.
func Float32(v float32) Value[float32] { return Value[float32]{v: v, put: PutFloat32} }

// Float64 is a double-precision float.
func Float64(v float64) Value[float64] { return Value[float64]{v: v, put: PutFloat64} }

// String is a character string (UTF-8).
func String(v string) Value[string] { return Value[string]{v: v, put: PutString} }

// Bytes is an octet string.
func Bytes(v []byte) Value[[]byte] { return Value[[]byte]{v: v, put: PutBytes} }

// Null is the null of a nullable attribute of type T.
// fabric:reachable:reason="generic: every typed write instantiates it, and RTA records the instantiation, not the generic declaration"
func Null[T any]() Value[T] { return Value[T]{} }

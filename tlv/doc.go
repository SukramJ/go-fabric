// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package tlv implements the Matter TLV (Type-Length-Value) codec
// per Matter Core Specification §A.7.
//
// TLV is Matter's self-describing binary format. Every element is a
// (control-byte, tag, length, value) tuple; nested containers use a
// trailing End-of-Container marker. The codec supports:
//
//   - Signed / unsigned integers (1, 2, 4, 8 bytes).
//   - Booleans, null.
//   - Single- and double-precision floats.
//   - UTF-8 strings and octet strings (1/2/4/8-byte length prefixes).
//   - Containers: Structure, Array, List.
//   - The eight tag forms: Anonymous, Context-specific, Common
//     Profile (2/4-byte), Implicit Profile (2/4-byte), Fully-Qualified
//     (6/8-byte). The encoder writes all eight; the decoder refuses an
//     Implicit Profile tag with [ErrImplicitProfileTag], because the
//     package sets no implicit profile to resolve it against.
//
// The package is dependency-free Go stdlib; it is consumed by the
// Interaction Model layer ([..]/im/) and by every cluster server
// implementation under [..]/cluster/.
//
// Encoding pattern:
//
//	enc := tlv.NewEncoder()
//	enc.StartStruct(tlv.AnonymousTag())
//	enc.PutBool(tlv.ContextTag(0), true)
//	enc.PutUint(tlv.ContextTag(1), 42)
//	if err := enc.EndContainer(); err != nil {
//	    return err
//	}
//	wire, err := enc.Bytes()
//	if err != nil {
//	    return err
//	}
//
// Decoding pattern:
//
//	dec := tlv.NewDecoder(wire)
//	for {
//	    el, err := dec.Next()
//	    if errors.Is(err, io.EOF) { break }
//	    if err != nil { return err }
//	    // dispatch on el.Type / el.Tag
//	}
package tlv

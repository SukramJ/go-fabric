// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thread

import (
	"encoding/binary"
	"fmt"
)

// TLV is one frame of a MeshCoP TLV stream: [type:1][length:1][value],
// length 0xFF escaping to a two-byte big-endian length after it.
//
// Mirrors matter.js packages/protocol/src/thread/BasicTlvCodec.ts:12-19
// (BasicTlvEntry). The type is a byte, so the 0..255 range check of
// BasicTlv.encode (:57-59) is the Go type.
type TLV struct {
	Type  TLVType
	Value []byte
}

// WalkTLVs splits a MeshCoP TLV stream into its frames, each value a
// copy. A truncated header, extended-length header or value fails with
// [ErrThreadTLV]; empty input is an empty stream.
//
// Mirrors matter.js packages/protocol/src/thread/BasicTlvCodec.ts:22-52
// (BasicTlv.walk).
func WalkTLVs(blob []byte) ([]TLV, error) {
	out := []TLV{}
	for offset := 0; offset < len(blob); {
		frame := blob[offset:]
		if len(frame) < 2 {
			return nil, fmt.Errorf("%w: truncated TLV header at offset %d", ErrThreadTLV, offset)
		}
		typ, length, valueStart := frame[0], int(frame[1]), offset+2
		if length == 0xff {
			if len(frame) < 4 {
				return nil, fmt.Errorf("%w: truncated extended-length TLV header at offset %d", ErrThreadTLV, offset)
			}
			length, valueStart = int(binary.BigEndian.Uint16(frame[2:])), offset+4
		}
		valueEnd := valueStart + length
		if valueEnd > len(blob) {
			return nil, fmt.Errorf("%w: truncated TLV value at offset %d (type=%d, length=%d)", ErrThreadTLV, offset, typ, length)
		}
		out = append(out, TLV{Type: TLVType(typ), Value: append([]byte{}, blob[valueStart:valueEnd]...)})
		offset = valueEnd
	}
	return out, nil
}

// EncodeTLVs writes entries as a MeshCoP TLV stream, a value of 255 bytes
// or more in the extended-length form. A value over 0xFFFF bytes fails
// with [ErrInvalidTLV].
//
// Mirrors matter.js packages/protocol/src/thread/BasicTlvCodec.ts:54-84
// (encode).
func EncodeTLVs(entries []TLV) ([]byte, error) {
	total := 0
	for _, e := range entries {
		n := len(e.Value)
		if n > 0xffff {
			return nil, fmt.Errorf("%w: invalid TLV length %d: must be <= 0xFFFF", ErrInvalidTLV, n)
		}
		total += n + 2
		if n >= 0xff {
			total += 2
		}
	}
	out := make([]byte, 0, total)
	for _, e := range entries {
		n := len(e.Value)
		out = append(out, byte(e.Type))
		if n >= 0xff {
			out = binary.BigEndian.AppendUint16(append(out, 0xff), uint16(n)) //nolint:gosec // G115: n <= 0xFFFF checked above
		} else {
			out = append(out, byte(n))
		}
		out = append(out, e.Value...)
	}
	return out, nil
}

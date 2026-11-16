// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spake2_test

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/SukramJ/go-fabric/secure/spake2"
)

// TestPASEDecodersTakeBytesAfterTheMessageEnd pins 5a-3 S-5: every PASE
// decoder reads one top-level structure and does not look at what
// follows its end-of-container, as matter.js decodes one element and
// drops the reader (packages/types/src/tlv/TlvSchema.ts:48-50 at
// db021da0). A message with two bytes appended decodes to the same
// values as the message alone; refusing them would be stricter than
// matter.js and is a divergence, not a fix.
func TestPASEDecodersTakeBytesAfterTheMessageEnd(t *testing.T) {
	t.Parallel()
	tail := []byte{0x18, 0x00}
	point := make([]byte, spake2.PointSize)
	point[0] = 0x04
	random := bytes.Repeat([]byte{0xA5}, spake2.PBKDFRandomSize)
	resp := spake2.PBKDFParamResponse{
		InitiatorRandom:    random,
		ResponderRandom:    random,
		ResponderSessionID: 7,
		Parameters:         &spake2.PBKDFParameters{Iterations: 1000, Salt: bytes.Repeat([]byte{1}, 16)},
	}
	cases := []struct {
		name   string
		msg    []byte
		decode func([]byte) (any, error)
	}{
		{"PBKDFParamRequest", buildPBKDFParamRequestPayload(random, 3, 0, false), func(b []byte) (any, error) { return spake2.DecodePBKDFParamRequest(b) }},
		{"PBKDFParamResponse", resp.Marshal(), func(b []byte) (any, error) { return spake2.DecodePBKDFParamResponse(b) }},
		{"Pake1", spake2.EncodePake1(point), func(b []byte) (any, error) { return spake2.DecodePake1(b) }},
		{"Pake2", spake2.EncodePake2(&spake2.Pake2Output{Y: point, CB: make([]byte, 32)}), func(b []byte) (any, error) {
			pB, cB, err := spake2.DecodePake2(b)
			return [2][]byte{pB, cB}, err
		}},
		{"Pake3", spake2.EncodePake3(make([]byte, 32)), func(b []byte) (any, error) { return spake2.DecodePake3(b) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			want, err := c.decode(c.msg)
			if err != nil {
				t.Fatalf("control: the message alone is refused: %v", err)
			}
			got, err := c.decode(append(append([]byte(nil), c.msg...), tail...))
			if err != nil {
				t.Fatalf("with %x appended: %v, want the message decoded", tail, err)
			}
			if !equalDecoded(want, got) {
				t.Errorf("with %x appended decoded to %+v, want %+v", tail, got, want)
			}
		})
	}
}

func equalDecoded(a, b any) bool { return reflect.DeepEqual(a, b) }

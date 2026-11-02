// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spake2

import (
	"bytes"
	"testing"

	"github.com/SukramJ/go-fabric/tlv"
)

// pbkdfParamRequestSeed is a PBKDFParamRequest in the shape a commissioner
// sends (tags 1-5, the session-parameter struct nested at 5).
func pbkdfParamRequestSeed(withParams bool) []byte {
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutOctets(tlv.ContextTag(1), bytes.Repeat([]byte{0x5A}, 32))
	enc.PutUint(tlv.ContextTag(2), 0x1234)
	enc.PutUint(tlv.ContextTag(3), 0)
	enc.PutBool(tlv.ContextTag(4), false)
	if withParams {
		enc.StartStruct(tlv.ContextTag(5))
		enc.PutUint32(tlv.ContextTag(1), 500)
		enc.PutUint32(tlv.ContextTag(2), 300)
		_ = enc.EndContainer()
	}
	_ = enc.EndContainer()
	out, _ := enc.Bytes()
	return out
}

// FuzzDecodePBKDFParamRequest feeds arbitrary bytes to the decoder of the
// first message a PASE responder parses, before any authentication
// (W6-test-06). The module has no encoder for it on this side, so the
// property is the decoder's own: no panic, and an accepted request carries
// the 32-byte random the response echoes.
func FuzzDecodePBKDFParamRequest(f *testing.F) {
	f.Add(pbkdfParamRequestSeed(false))
	f.Add(pbkdfParamRequestSeed(true))
	f.Add([]byte{})
	f.Add([]byte{0x15, 0x18})
	f.Fuzz(func(t *testing.T, data []byte) {
		req, err := DecodePBKDFParamRequest(data)
		if err != nil {
			return
		}
		if len(req.InitiatorRandom) != 32 {
			t.Fatalf("accepted a PBKDFParamRequest with a %d-byte initiatorRandom", len(req.InitiatorRandom))
		}
	})
}

// FuzzDecodePake1 feeds arbitrary bytes to the Pake1 decoder (W6-test-06):
// no panic, and an accepted pA is a point-sized octet string that
// EncodePake1 writes back to bytes the decoder reads as the same pA.
func FuzzDecodePake1(f *testing.F) {
	f.Add(EncodePake1(bytes.Repeat([]byte{0x04}, PointSize)))
	f.Add(EncodePake1([]byte{0x04}))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		pA, err := DecodePake1(data)
		if err != nil {
			return
		}
		again, err := DecodePake1(EncodePake1(pA))
		if err != nil || !bytes.Equal(again, pA) {
			t.Fatalf("Pake1 round trip: %x -> %x (%v)", pA, again, err)
		}
	})
}

// FuzzDecodePake3 is FuzzDecodePake1 for Pake3's confirmation tag cA.
func FuzzDecodePake3(f *testing.F) {
	f.Add(EncodePake3(bytes.Repeat([]byte{0xCA}, ConfirmTagSize)))
	f.Add(EncodePake3(nil))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		cA, err := DecodePake3(data)
		if err != nil {
			return
		}
		again, err := DecodePake3(EncodePake3(cA))
		if err != nil || !bytes.Equal(again, cA) {
			t.Fatalf("Pake3 round trip: %x -> %x (%v)", cA, again, err)
		}
	})
}

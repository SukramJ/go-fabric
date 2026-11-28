// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package valueenc_test

import (
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/internal/valueenc"
	"thingfabric.dev/go-fabric/tlv"
)

// malformedWritersP10 write one complete, balanced, anonymously tagged
// element whose inner tags break the container rules of tlv.Validate.
var malformedWritersP10 = map[string]func(*tlv.Encoder, tlv.Tag){
	"anonymous member of a Structure": func(enc *tlv.Encoder, tag tlv.Tag) {
		enc.StartStruct(tag)
		enc.PutUint(tlv.AnonymousTag(), 1)
		_ = enc.EndContainer()
	},
	"context-tagged member of an Array": func(enc *tlv.Encoder, tag tlv.Tag) {
		// Array { ctx(1) uint8 5 }
		_ = enc.PutRawElement(tag, []byte{0x16, 0x24, 0x01, 0x05, 0x18})
	},
}

// TestMalformedElementIsRefusedP10 (P10-wire-6402): an element that
// passes the one-element check but breaks the tag rules of its containers
// is refused by Encode and by Scratch.Encode ([valueenc.ErrMalformed],
// wrapping tlv's error), so it costs its own path instead of reaching the
// message. Red at d1ab4e970: both elements were returned with a nil error.
func TestMalformedElementIsRefusedP10(t *testing.T) {
	var s valueenc.Scratch
	for name, write := range malformedWritersP10 {
		for entry, encode := range map[string]func(func(*tlv.Encoder, tlv.Tag)) ([]byte, error){
			"Encode":         valueenc.Encode,
			"Scratch.Encode": s.Encode,
		} {
			el, err := encode(write)
			if !errors.Is(err, valueenc.ErrMalformed) || errors.Is(err, valueenc.ErrNotOneElement) {
				t.Fatalf("%s, %s: element % X err %v, want ErrMalformed", name, entry, el, err)
			}
			if el != nil {
				t.Fatalf("%s, %s: element % X returned beside the error", name, entry, el)
			}
		}
	}
	// Control: a well-formed Structure still encodes.
	el, err := valueenc.Encode(func(enc *tlv.Encoder, tag tlv.Tag) {
		enc.StartStruct(tag)
		enc.PutUint(tlv.ContextTag(1), 1)
		_ = enc.EndContainer()
	})
	if err != nil || len(el) == 0 {
		t.Fatalf("well-formed Structure: element % X err %v", el, err)
	}
}

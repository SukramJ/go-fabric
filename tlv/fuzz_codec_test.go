// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package tlv

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"testing"
)

// FuzzDecoderNext walks arbitrary bytes with [Decoder.Next] until the
// stream ends or the decoder rejects it.
//
// TLV is the encoding every other decoder in this module is built on, so
// this is the widest attacker-reachable surface there is: an inbound
// datagram's payload is TLV before anything has decided what it means.
// The asserted property is that the walk terminates and does not panic —
// each [Decoder.Next] consumes at least the control byte, so a decoder
// that ever failed to advance would surface here as a hang rather than
// as a wrong answer. [Validate] is exercised on the same input because
// it is the strict-mode entry point north-bound code calls on bytes it
// is about to emit or accept.
//
// Which error a malformed stream produces is deliberately not asserted.
func FuzzDecoderNext(f *testing.F) {
	// Known-good: every matter.js-generated wire fixture the parity test
	// already pins, so the corpus starts from bytes a reference encoder
	// actually produced.
	var fixtures []tlvFixture
	if err := json.Unmarshal(tlvFixturesJSON, &fixtures); err != nil {
		f.Fatalf("unmarshal tlv-wire-fixtures.json: %v", err)
	}
	if len(fixtures) == 0 {
		f.Fatal("tlv-wire-fixtures.json is empty")
	}
	for _, fx := range fixtures {
		raw, err := hex.DecodeString(fx.BytesHex)
		if err != nil {
			f.Fatalf("fixture %q: decode hex: %v", fx.Label, err)
		}
		f.Add(raw)
	}

	// Deliberately malformed shapes, each aimed at a different rejection
	// path in the decoder.
	f.Add([]byte{})
	f.Add([]byte{0x1F})                         // element type outside Table 73
	f.Add([]byte{0xE0})                         // fully-qualified 8-byte tag with no tag bytes
	f.Add([]byte{0x15})                         // structure opened, never closed
	f.Add([]byte{0x18})                         // end-of-container outside any container
	f.Add([]byte{0x0C, 0xFF})                   // UTF-8 string claiming 255 bytes of body
	f.Add([]byte{0x38})                         // end-of-container carrying a context tag-control
	f.Add([]byte{0x16, 0x24, 0x01, 0x05, 0x18}) // context tag inside an array

	f.Fuzz(func(t *testing.T, payload []byte) {
		d := NewDecoder(payload)
		for {
			el, err := d.Next()
			if err != nil {
				if !errors.Is(err, io.EOF) && d.Remaining() < 0 {
					t.Fatalf("decoder read past the end of the buffer: pos=%d len=%d", d.Pos(), len(payload))
				}
				break
			}
			if d.Pos() > len(payload) {
				t.Fatalf("element %+v left the read cursor at %d, past the %d-byte buffer", el, d.Pos(), len(payload))
			}
		}
		_ = Validate(payload)
	})
}

// FuzzEncodeDecodeRoundTrip asserts decode(encode(x)) == x across the
// value space of the encoder's typed writers.
//
// Every field is written into one anonymous structure under its own
// context tag, which is also the shape [Validate] has to accept: a
// stream this module produced and then refused to read back would be a
// defect on either side, and this target does not care which.
//
// The one deliberate asymmetry is character strings. Per Matter
// §7.19.2.40 the decoder truncates a UTF-8 string at the first IS1
// (0x1F) separator — see the citation on [Decoder.readValue] — so the
// expected value for a string containing IS1 is its prefix, not the
// input.
func FuzzEncodeDecodeRoundTrip(f *testing.F) {
	f.Add(uint64(0), int64(0), "", []byte(nil), float32(0), float64(0), false)
	f.Add(uint64(20202021), int64(-1), "NodeLabel", []byte{0x04, 0xAA}, float32(1.5), 3.25, true)
	f.Add(^uint64(0), int64(math.MinInt64), "ünïcödé", bytes.Repeat([]byte{0xFF}, 64), float32(math.MaxFloat32), math.MaxFloat64, true)
	// Deliberately awkward: an IS1 inside the string, and a NaN float.
	f.Add(uint64(1), int64(1), "before\x1Fafter", []byte{}, float32(math.NaN()), math.NaN(), false)

	f.Fuzz(func(t *testing.T, u uint64, i int64, s string, octets []byte, f32 float32, f64 float64, b bool) {
		e := NewEncoder()
		e.StartStruct(AnonymousTag())
		e.PutUint(ContextTag(1), u)
		e.PutInt(ContextTag(2), i)
		e.PutUTF8(ContextTag(3), s)
		e.PutOctets(ContextTag(4), octets)
		e.PutFloat32(ContextTag(5), f32)
		e.PutFloat64(ContextTag(6), f64)
		e.PutBool(ContextTag(7), b)
		e.PutNull(ContextTag(8))
		if err := e.EndContainer(); err != nil {
			t.Fatalf("EndContainer: %v", err)
		}
		wire, err := e.Bytes()
		if err != nil {
			t.Fatalf("Bytes: %v", err)
		}
		if err := Validate(wire); err != nil {
			t.Fatalf("the encoder produced bytes Validate rejects: %v (wire=%x)", err, wire)
		}

		d := NewDecoder(wire)
		top, err := d.Next()
		if err != nil {
			t.Fatalf("decode top: %v (wire=%x)", err, wire)
		}
		if !top.IsContainer || top.Type != TypeStructure {
			t.Fatalf("top element = %+v, want an anonymous structure", top)
		}
		got := make(map[uint32]Element, 8)
		for {
			el, err := d.Next()
			if err != nil {
				t.Fatalf("decode child: %v (wire=%x)", err, wire)
			}
			if el.IsEndContainer {
				break
			}
			got[el.Tag.Number] = el
		}
		if len(got) != 8 {
			t.Fatalf("decoded %d children, want 8 (wire=%x)", len(got), wire)
		}

		if got[1].Uint != u {
			t.Fatalf("uint round-trip: got %d, want %d", got[1].Uint, u)
		}
		if got[2].Int != i {
			t.Fatalf("int round-trip: got %d, want %d", got[2].Int, i)
		}
		wantStr := s
		if idx := bytes.IndexByte([]byte(s), 0x1F); idx >= 0 {
			wantStr = s[:idx]
		}
		if got[3].String != wantStr {
			t.Fatalf("utf8 round-trip: got %q, want %q", got[3].String, wantStr)
		}
		if !bytes.Equal(got[4].Octets, octets) {
			t.Fatalf("octets round-trip: got %x, want %x", got[4].Octets, octets)
		}
		// The decoder widens a 4-byte float to float64; comparing against
		// the widened input is the round-trip, not a tolerance.
		if want := float64(f32); !sameFloat(got[5].Float, want) {
			t.Fatalf("float32 round-trip: got %v, want %v", got[5].Float, want)
		}
		if !sameFloat(got[6].Float, f64) {
			t.Fatalf("float64 round-trip: got %v, want %v", got[6].Float, f64)
		}
		if got[7].Bool != b {
			t.Fatalf("bool round-trip: got %v, want %v", got[7].Bool, b)
		}
		if !got[8].IsNull {
			t.Fatalf("null round-trip: got %+v, want IsNull", got[8])
		}
	})
}

// sameFloat compares two float64s treating NaN as equal to NaN — the
// round-trip question is whether the bit pattern survived, and Go's ==
// answers "no" for every NaN regardless of encoding.
func sameFloat(a, b float64) bool {
	if math.IsNaN(a) && math.IsNaN(b) {
		return true
	}
	return a == b
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package checkin

import (
	"bytes"
	"testing"
)

// FuzzCheckinDecode feeds arbitrary payloads to the Check-In decoder a
// controller runs on every Check-In it receives from an ICD (W6-test-06):
// no panic on any payload, and what Encode builds under a key Decode
// authenticates back to the same counter and application data.
func FuzzCheckinDecode(f *testing.F) {
	key := bytes.Repeat([]byte{0x6B}, 16)
	seed, err := Encode(key, 7, []byte{0x01, 0x02})
	if err != nil {
		f.Fatal(err)
	}
	f.Add(seed, uint32(7), []byte{0x01, 0x02})
	f.Add([]byte{}, uint32(0), []byte{})
	f.Add(bytes.Repeat([]byte{0xFF}, MinPayloadLength), uint32(0xFFFFFFFF), []byte(nil))
	f.Fuzz(func(t *testing.T, payload []byte, counter uint32, app []byte) {
		_, _, _ = Decode(key, payload)
		_, _, _ = DecodeICD(key, payload)
		enc, err := Encode(key, counter, app)
		if err != nil {
			return
		}
		gotCounter, gotApp, err := Decode(key, enc)
		if err != nil {
			t.Fatalf("Decode(Encode(%d, %x)): %v", counter, app, err)
		}
		if gotCounter != counter || !bytes.Equal(gotApp, app) {
			t.Fatalf("round trip: (%d, %x) -> (%d, %x)", counter, app, gotCounter, gotApp)
		}
	})
}

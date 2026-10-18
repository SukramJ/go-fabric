// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package btp

import (
	"bytes"
	"encoding/hex"
	"testing"
)

// FuzzDecodePacket feeds arbitrary bytes to the segment decoder and to a
// peripheral session: neither may panic, a decoded packet that encodes
// again must round-trip, and the session must survive any input by
// closing or carrying on.
func FuzzDecodePacket(f *testing.F) {
	for _, seed := range []string{
		"0d000044000400000049b6a902",
		"0600040000",
		"0e000004",
		"080001",
		"65000044",
		"",
		"05",
		"0d00010900090807060504030201",
	} {
		b, _ := hex.DecodeString(seed)
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if p, err := DecodePacket(data); err == nil {
			// The reserved header bits do not survive; everything else does.
			want := append([]byte{data[0] & 0x0f}, data[1:]...)
			if enc, err := EncodePacket(p); err == nil && !bytes.Equal(enc, want) {
				t.Fatalf("round trip %x -> %x", data, enc)
			}
		}
		_, _ = DecodeHandshakeRequest(data)
		_, _ = DecodeHandshakeResponse(data)
		_, _ = DecodeAdvertisementData(data)
		s, err := NewPeripheral(MaximumBTPMTU, unhex(t, "656c04000000f40008"), Callbacks{
			Write:      func([]byte) error { return nil },
			Disconnect: func() error { return nil },
			Message:    func([]byte) error { return nil },
		}, Options{TimeSource: newFakeClock()})
		if err != nil {
			t.Fatal(err)
		}
		_ = s.HandleIncoming(data)
		_ = s.HandleIncoming(data)
		_ = s.Close()
	})
}

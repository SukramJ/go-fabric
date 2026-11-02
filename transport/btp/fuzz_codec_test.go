// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package btp

import (
	"bytes"
	"encoding/hex"
	"log/slog"
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

// FuzzSessionPackets frames a message into a run of data packets with
// correct sequence numbers — a beginning segment carrying the length, then
// continuing segments, the last one ending — and feeds them to a peripheral
// session one by one, with an acknowledgement on the packets the fuzzer
// picks (W1-wire-09, W6-test-08). It reaches what FuzzDecodePacket's single
// datagram cannot: the continuing and ending branches, the reassembly bound
// and the ack-validity arithmetic. Nothing may panic; a run with no
// acknowledgement must deliver the message whole, exactly once.
func FuzzSessionPackets(f *testing.F) {
	f.Add([]byte("hello"), uint8(0), []byte{})
	f.Add(bytes.Repeat([]byte{0xA5}, 600), uint8(17), []byte{0, 1, 0, 3})
	f.Add(bytes.Repeat([]byte{0x5A}, 2000), uint8(255), []byte{1, 1, 1, 1, 1, 1, 1, 1, 1, 1})
	f.Add([]byte{0x01}, uint8(1), []byte{0xFF})
	f.Fuzz(func(t *testing.T, msg []byte, seg uint8, acks []byte) {
		if len(msg) == 0 || len(msg) > 0x2000 {
			return
		}
		var got [][]byte
		s, err := NewPeripheral(MaximumBTPMTU, unhex(t, "656c04000000f40008"), Callbacks{
			Write:      func([]byte) error { return nil },
			Disconnect: func() error { return nil },
			Message: func(m []byte) error {
				got = append(got, bytes.Clone(m))
				return nil
			},
		}, Options{TimeSource: newFakeClock(), Logger: slog.New(slog.DiscardHandler)})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = s.Close() }()
		// The handshake's ATT_MTU is 0xF4: a beginning segment spends up
		// to five bytes on its header, a continuing one up to three.
		size := 1 + int(seg)%(0xF4-5)
		acked := false
		rest := msg
		for i := 0; len(rest) > 0; i++ {
			n := min(size, len(rest))
			pk := Packet{Payload: Payload{SequenceNumber: uint8(i % 256), SegmentPayload: rest[:n]}} //nolint:gosec // i%256 fits a byte
			if i == 0 {
				pk.Header.IsBeginningSegment = true
				l := uint16(len(msg)) //nolint:gosec // len(msg) <= 0x2000, checked above
				pk.Payload.MessageLength = &l
			} else {
				pk.Header.IsContinuingSegment = true
			}
			rest = rest[n:]
			if len(rest) == 0 {
				// A last segment after the first is continuing and ending
				// (EncodePacket refuses an ending segment alone).
				pk.Header.IsEndingSegment = true
			}
			if i < len(acks) && acks[i]&1 == 1 {
				a := acks[i] >> 1
				pk.Header.HasAckNumber, pk.Payload.AckNumber = true, &a
				acked = true
			}
			data, err := EncodePacket(pk)
			if err != nil {
				t.Fatalf("encode packet %d: %v", i, err)
			}
			if err := s.HandleIncoming(data); err != nil {
				return
			}
		}
		if acked {
			return
		}
		if len(got) != 1 || !bytes.Equal(got[0], msg) {
			t.Fatalf("delivered %d messages, want the %d-byte message once", len(got), len(msg))
		}
	})
}

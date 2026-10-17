// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package btp

// A port of matter.js packages/protocol/test/codec/BtpCodecTest.ts at
// 0ceb202d: every vector and every refusal, with matter.js's messages.

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
)

const btpVectorPayload = "0400000049b6a902a9a5773dbb8cafd90120a7c7000015300120cb0c120a3499327ddaec4ebe60889df0f1bf80d8a4dea1dd6ffef16ef58ecafe25028e17240300280418"

func TestBtpCodecDecodeHandshakeRequest(t *testing.T) {
	got, err := DecodeHandshakeRequest(unhex(t, "656c04000000b90006"))
	if err != nil {
		t.Fatal(err)
	}
	want := HandshakeRequest{Versions: []int{4}, ATTMTU: 185, ClientWindowSize: 6}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestBtpCodecDecodeHandshakeRequestMultipleVersions(t *testing.T) {
	// Even version index in the low nibble, odd in the high: [4,5,6] => 54 06 00 00.
	got, err := DecodeHandshakeRequest(unhex(t, "656c54060000b90006"))
	if err != nil {
		t.Fatal(err)
	}
	want := HandshakeRequest{Versions: []int{4, 5, 6}, ATTMTU: 185, ClientWindowSize: 6}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestBtpCodecRoundTripsMultipleVersions(t *testing.T) {
	req := HandshakeRequest{Versions: []int{4, 5, 6}, ATTMTU: 185, ClientWindowSize: 6}
	got, err := DecodeHandshakeRequest(EncodeHandshakeRequest(req))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Versions, req.Versions) {
		t.Fatalf("versions %v, want %v", got.Versions, req.Versions)
	}
}

func TestBtpCodecDecodePacket(t *testing.T) {
	got, err := DecodePacket(unhex(t, "0d000044"+"00"+btpVectorPayload))
	if err != nil {
		t.Fatal(err)
	}
	want := Packet{
		Header:  Header{HasAckNumber: true, IsEndingSegment: true, IsBeginningSegment: true},
		Payload: Payload{AckNumber: u8(0), SequenceNumber: 0, MessageLength: u16(0x44), SegmentPayload: unhex(t, btpVectorPayload)},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestBtpCodecIsHandshakeResponse(t *testing.T) {
	cases := []struct {
		hex  string
		want bool
	}{
		{"656c04f40005", true},
		{"656c04f400", false},         // another length
		{"656c04000000b90006", false}, // another length
		{"0d6c04f40005", false},       // no handshake header
		{"650004f40005", false},       // no handshake opcode
	}
	for _, c := range cases {
		if got := IsHandshakeResponse(unhex(t, c.hex)); got != c.want {
			t.Errorf("IsHandshakeResponse(%s) = %v, want %v", c.hex, got, c.want)
		}
	}
}

func TestBtpCodecEncodeHandshakeResponse(t *testing.T) {
	got := EncodeHandshakeResponse(HandshakeResponse{Version: 4, ATTMTU: 256, WindowSize: 6})
	if want := unhex(t, "656c04000106"); !bytes.Equal(got, want) {
		t.Fatalf("got %x, want %x", got, want)
	}
}

func TestBtpCodecEncodePacket(t *testing.T) {
	payload := unhex(t, btpVectorPayload)
	cases := []struct {
		name string
		p    Packet
		want string
	}{
		{
			"both ackNumber and messageLength undefined",
			Packet{Header: Header{IsEndingSegment: true, IsContinuingSegment: true}, Payload: Payload{SegmentPayload: payload}},
			"0600" + btpVectorPayload,
		},
		{
			"ackNumber undefined",
			Packet{Header: Header{IsEndingSegment: true, IsBeginningSegment: true}, Payload: Payload{MessageLength: u16(0x44), SegmentPayload: payload}},
			"05004400" + btpVectorPayload,
		},
		{
			"messageLength undefined",
			Packet{Header: Header{HasAckNumber: true, IsEndingSegment: true, IsContinuingSegment: true}, Payload: Payload{AckNumber: u8(0), SegmentPayload: payload}},
			"0e0000" + btpVectorPayload,
		},
	}
	for _, c := range cases {
		got, err := EncodePacket(c.p)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if want := unhex(t, c.want); !bytes.Equal(got, want) {
			t.Errorf("%s: got %x, want %x", c.name, got, want)
		}
	}
}

func wantProtocolError(t *testing.T, err error, msg string) {
	t.Helper()
	var pe *ProtocolError
	if !errors.As(err, &pe) || pe.Message != msg {
		t.Fatalf("error %v, want ProtocolError %q", err, msg)
	}
}

func TestBtpCodecErrors(t *testing.T) {
	_, err := DecodeHandshakeRequest(unhex(t, "0d6c04000000b90006"))
	wantProtocolError(t, err, "Header for expected BTP Handshake Request is incorrect.")
	_, err = DecodeHandshakeRequest(unhex(t, "656d04000000b90006"))
	wantProtocolError(t, err, "Management Opcode for BTP Handshake Request is incorrect.")
	_, err = DecodeHandshakeRequest(unhex(t, "656c00000000b90006"))
	wantProtocolError(t, err, "No valid version provided.")
	_, err = DecodePacket(unhex(t, "65000044"+"00"+btpVectorPayload))
	wantProtocolError(t, err, "Management Opcode for BTPHandshake Request is not expected")

	payload := unhex(t, btpVectorPayload)
	// matter.js's DECODED_PACKET_4 sets ackNumber 15231, which no uint8
	// holds; any value shows the refusal.
	encodeCases := []struct {
		p   Packet
		msg string
	}{
		{
			Packet{Header: Header{IsEndingSegment: true, IsBeginningSegment: true}, Payload: Payload{AckNumber: u8(0x7f), SegmentPayload: payload}},
			"Ack number shouldn't be set because header flag is not set.",
		},
		{
			Packet{Header: Header{HasAckNumber: true, IsEndingSegment: true, IsBeginningSegment: true}, Payload: Payload{SegmentPayload: payload}},
			"Ack number needs to be set because header flag is set.",
		},
		{
			Packet{Header: Header{IsEndingSegment: true, IsContinuingSegment: true}, Payload: Payload{MessageLength: u16(0x44), SegmentPayload: payload}},
			"Message length shouldn't be set because the package is not a beginning segment.",
		},
		{
			Packet{Header: Header{IsEndingSegment: true, IsBeginningSegment: true}, Payload: Payload{SegmentPayload: payload}},
			"Message length needs to be set because packet is a beginning segment.",
		},
		{
			Packet{Header: Header{HasManagementOpcode: true, IsEndingSegment: true, IsBeginningSegment: true}, Payload: Payload{SegmentPayload: payload}},
			"Please use the specific methods to encode a Handshake packet",
		},
	}
	for _, c := range encodeCases {
		_, err := EncodePacket(c.p)
		wantProtocolError(t, err, c.msg)
	}
}

// The refusals of BtpCodec.ts:156-167 the matter.js test does not reach.
func TestBtpCodecFlagConsistency(t *testing.T) {
	_, err := EncodePacket(Packet{Header: Header{IsBeginningSegment: true, IsContinuingSegment: true}, Payload: Payload{MessageLength: u16(1), SegmentPayload: []byte{1}}})
	wantProtocolError(t, err, "Beginning and continuing segment flags can't be set at the same time.")
	_, err = EncodePacket(Packet{Header: Header{IsEndingSegment: true}, Payload: Payload{SegmentPayload: []byte{1}}})
	wantProtocolError(t, err, "Ending segment flag can't be set without continuing segment flag.")
	_, err = EncodePacket(Packet{Header: Header{IsContinuingSegment: true}})
	wantProtocolError(t, err, "Payload needs to be set because header flag indicates a message with payload.")
}

func TestBtpCodecTruncated(t *testing.T) {
	for _, h := range []string{"", "05", "0500", "050044", "08"} {
		if _, err := DecodePacket(unhex(t, h)); !errors.Is(err, ErrTruncated) {
			t.Errorf("DecodePacket(%q) = %v, want ErrTruncated", h, err)
		}
	}
	for _, h := range []string{"", "65", "656c04"} {
		if _, err := DecodeHandshakeResponse(unhex(t, h)); !errors.Is(err, ErrTruncated) {
			t.Errorf("DecodeHandshakeResponse(%q) = %v, want ErrTruncated", h, err)
		}
	}
	for _, h := range []string{"", "656c", "656c0400", "656c04000000b900"} {
		if _, err := DecodeHandshakeRequest(unhex(t, h)); !errors.Is(err, ErrTruncated) {
			t.Errorf("DecodeHandshakeRequest(%q) = %v, want ErrTruncated", h, err)
		}
	}
}

// The service data vectors of matter.js
// packages/protocol/test/common/BleScannerTest.ts:12-13 at 0ceb202d, and
// the advertisement encoder of BtpCodec.ts:296-316 round-tripped.
func TestBtpCodecAdvertisement(t *testing.T) {
	cases := []struct {
		hex  string
		want Advertisement
	}{
		{"00c9067c11018000", Advertisement{Discriminator: 1737, VendorID: 4476, ProductID: 32769}},
		{"00e8037c11018000", Advertisement{Discriminator: 1000, VendorID: 4476, ProductID: 32769}},
	}
	for _, c := range cases {
		got, err := DecodeAdvertisementServiceData(unhex(t, c.hex))
		if err != nil || got != c.want {
			t.Errorf("DecodeAdvertisementServiceData(%s) = %+v, %v; want %+v", c.hex, got, err, c.want)
		}
	}
	data := EncodeAdvertisementData(1737, 4476, 32769, true, false)
	if want := unhex(t, "0201060b16f6ff00c9067c11018001"); !bytes.Equal(data, want) {
		t.Fatalf("EncodeAdvertisementData = %x, want %x", data, want)
	}
	got, err := DecodeAdvertisementData(data)
	if err != nil || got != (Advertisement{Discriminator: 1737, VendorID: 4476, ProductID: 32769, HasAdditionalAdvertisementData: true}) {
		t.Fatalf("DecodeAdvertisementData = %+v, %v", got, err)
	}
	ext := EncodeAdvertisementData(1737, 4476, 32769, false, true)
	if want := unhex(t, "0201060b16f6ff00c90600000000"+"02"); !bytes.Equal(ext, want) {
		t.Fatalf("extended announcement = %x, want %x", ext, want)
	}
	for _, bad := range []string{"0301060b16f6ff00c9067c11018001", "0201060b16f5ff00c9067c11018001"} {
		if _, err := DecodeAdvertisementData(unhex(t, bad)); !errors.Is(err, ErrInvalidAdvertisement) {
			t.Errorf("DecodeAdvertisementData(%s) = %v, want ErrInvalidAdvertisement", bad, err)
		}
	}
	if _, err := DecodeAdvertisementServiceData(unhex(t, "01c9067c11018000")); !errors.Is(err, ErrInvalidAdvertisement) {
		t.Errorf("opcode 1: %v", err)
	}
	if _, err := DecodeAdvertisementServiceData(unhex(t, "00c9067c")); !errors.Is(err, ErrTruncated) {
		t.Errorf("short service data: %v", err)
	}
}

// BleConsts.ts:23-26 and the segment-size derivation of
// BtpSessionHandlerTest.ts:1506-1517.
func TestBtpConsts(t *testing.T) {
	for _, u := range []string{"fff6", "FFF6", "0000fff6-0000-1000-8000-00805f9b34fb", "0000FFF6-0000-1000-8000-00805F9B34FB"} {
		if !IsServiceUUID(u) {
			t.Errorf("IsServiceUUID(%q) = false", u)
		}
	}
	if IsServiceUUID("fff7") {
		t.Error("IsServiceUUID(fff7) = true")
	}
	for in, want := range map[int]int{100: 97, 247: MaximumBTPMTU, 500: MaximumBTPMTU, 23: MinimumATTMTU, 10: MinimumATTMTU, 0: MinimumATTMTU} {
		if got := SegmentSizeFromATTMTU(in); got != want {
			t.Errorf("SegmentSizeFromATTMTU(%d) = %d, want %d", in, got, want)
		}
	}
}

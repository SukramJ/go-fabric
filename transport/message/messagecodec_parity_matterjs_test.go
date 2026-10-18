// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package message

// A port of matter.js packages/protocol/test/codec/MessageCodecTest.ts at
// 862ad75f: the fixed decode/encode vectors, the privacy-enhanced header and
// the refusal to encode extensions.
//
// matter.js decodes in two steps (decodePacket → decodePayload); the Go
// surface is UnmarshalHeader + UnmarshalProtocolHeader, and encodes through
// Header.MarshalPacket + ProtocolHeader.MarshalPayload. The matter.js
// protocolId (vendor<<16 | protocol) is the Go VendorID/ProtocolID pair.

import (
	"bytes"
	"encoding/hex"
	"errors"
	"reflect"
	"testing"
)

func mcHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// MessageCodecTest.ts:11-13, :25-50.
const (
	mcEncoded  = "040000000a4ff2177ea0c8a7cb6a63520520d3640000153001204715a406c6b0496ad52039e347db8528cb69a1cb2fce6f2318552ae65e103aca250233dc240300280435052501881325022c011818"
	mcPayload  = "153001204715a406c6b0496ad52039e347db8528cb69a1cb2fce6f2318552ae65e103aca250233dc240300280435052501881325022c011818"
	mcEncoded2 = "01000000218712797ea0c8a7cb6a63520621d36400000a4ff217153001204715a406c6b0496ad52039e347db8528cb69a1cb2fce6f2318552ae65e103aca3002201783302d95a4a9fb0decb8fdd6564b90a957681459aeee069961bea61d7b247125039d8935042501e80330022099f813dd41bd081a1c63e811828f0662594bca89cd9d4ed26f7427fdb2a027361835052501881325022c011818"
	mcPayload2 = "153001204715a406c6b0496ad52039e347db8528cb69a1cb2fce6f2318552ae65e103aca3002201783302d95a4a9fb0decb8fdd6564b90a957681459aeee069961bea61d7b247125039d8935042501e80330022099f813dd41bd081a1c63e811828f0662594bca89cd9d4ed26f7427fdb2a027361835052501881325022c011818"
)

// mcDecoded is DECODED (MessageCodecTest.ts:25-50) in Go form.
func mcDecoded() (Header, ProtocolHeader) {
	return Header{
			SessionID: 0, SessionType: SessionUnsecured,
			HasSourceNodeID: true, SourceNodeID: 5936706156730294398,
			MessageCounter: 401755914,
		}, ProtocolHeader{
			ProtocolID: 0, Initiator: true, ExchangeID: 25811,
			Opcode: 0x20, NeedsAck: true,
		}
}

// mcDecoded2 is DECODED_2 (MessageCodecTest.ts:56-81) in Go form.
func mcDecoded2() (Header, ProtocolHeader) {
	return Header{
			SessionID: 0, SessionType: SessionUnsecured,
			DestSize: DestNodeID, DestNodeID: 5936706156730294398,
			MessageCounter: 2031257377,
		}, ProtocolHeader{
			ProtocolID: 0, Initiator: false, ExchangeID: 25811,
			Opcode: 0x21, NeedsAck: true, HasAck: true, AckCounter: 401755914,
		}
}

func mcDecode(t *testing.T, wire []byte) (Header, ProtocolHeader, []byte) {
	t.Helper()
	h, n, err := UnmarshalHeader(wire)
	if err != nil {
		t.Fatalf("UnmarshalHeader: %v", err)
	}
	p, m, err := UnmarshalProtocolHeader(wire[n:])
	if err != nil {
		t.Fatalf("UnmarshalProtocolHeader: %v", err)
	}
	return h, p, wire[n+m:]
}

func mcSameHeader(t *testing.T, got, want Header) {
	t.Helper()
	got.Raw = nil
	if got.SessionID != want.SessionID || got.SessionType != want.SessionType ||
		got.HasSourceNodeID != want.HasSourceNodeID || got.SourceNodeID != want.SourceNodeID ||
		got.DestSize != want.DestSize || got.DestNodeID != want.DestNodeID || got.DestGroupID != want.DestGroupID ||
		got.MessageCounter != want.MessageCounter || got.Privacy != want.Privacy ||
		got.Control != want.Control || got.HasExtension != want.HasExtension ||
		got.SecurityFlags() != want.SecurityFlags() {
		t.Errorf("header = %+v, want %+v", got, want)
	}
}

func mcEncode(t *testing.T, h Header, p ProtocolHeader, payload []byte) []byte {
	t.Helper()
	hb, err := h.MarshalPacket()
	if err != nil {
		t.Fatalf("MarshalPacket: %v", err)
	}
	pb, err := p.MarshalPayload()
	if err != nil {
		t.Fatalf("MarshalPayload: %v", err)
	}
	return append(append(hb, pb...), payload...)
}

func TestMessageCodecDecode(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, wire, payload string
		decoded             func() (Header, ProtocolHeader)
	}{
		{"decodes a message", mcEncoded, mcPayload, mcDecoded},
		{"decodes a message 2", mcEncoded2, mcPayload2, mcDecoded2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h, p, payload := mcDecode(t, mcHex(t, tc.wire))
			wantH, wantP := tc.decoded()
			mcSameHeader(t, h, wantH)
			if !reflect.DeepEqual(p, wantP) {
				t.Errorf("payload header = %+v, want %+v", p, wantP)
			}
			if !bytes.Equal(payload, mcHex(t, tc.payload)) {
				t.Errorf("payload = %X", payload)
			}
		})
	}

	t.Run("decodes the cleartext prefix of a privacy-enhanced group packet (CHIP vector)", func(t *testing.T) {
		t.Parallel()
		// MessageCodecTest.ts:150-152. Go decodes the cleartext prefix with
		// UnmarshalHeader; the obfuscated region is Raw[4:n] and is unmasked
		// in place before the header is read again (groups.tryDecrypt), so
		// the counter and ids read here are still obfuscated.
		wire := mcHex(t, "067ddb81d926afce24c8a0981bdd44f4e7302b2f915a66c9596290ebe4408217b3c0c921a2fca4e1")
		h, n, err := UnmarshalHeader(wire)
		if err != nil {
			t.Fatalf("UnmarshalHeader: %v", err)
		}
		if !h.Privacy || h.SessionID != 0xdb7d || h.NonceSecurityFlags() != 0x81 || h.SessionType != SessionGroup {
			t.Errorf("cleartext prefix = %+v", h)
		}
		if got := hex.EncodeToString(h.Raw[4:n]); got != "d926afce24c8a0981bdd44f4e730" {
			t.Errorf("obfuscated region = %s", got)
		}
		if got := len(wire) - n; got != 22 {
			t.Errorf("application payload = %d bytes, want 22", got)
		}
	})

	t.Run("decodes obfuscated header fields once deobfuscated", func(t *testing.T) {
		t.Parallel()
		// MessageCodecTest.ts:173-180: flags 0x06 (S + group), the region
		// deobfuscated back into the header bytes.
		wire := append([]byte{0x06, 0x7d, 0xdb, 0x81}, mcHex(t, "7956341201000000000000000200")...)
		h, _, err := UnmarshalHeader(wire)
		if err != nil {
			t.Fatalf("UnmarshalHeader: %v", err)
		}
		if h.MessageCounter != 0x12345679 || !h.HasSourceNodeID || h.SourceNodeID != 1 ||
			h.DestSize != DestGroup || h.DestNodeID != 0 || h.DestGroupID != 2 {
			t.Errorf("fields = %+v", h)
		}
	})
}

func TestMessageCodecEncodePrivacy(t *testing.T) {
	t.Parallel()
	t.Run("sets the privacy bit in the encoded packet header", func(t *testing.T) {
		t.Parallel()
		b, err := Header{
			SessionID: 0xdb7d, SessionType: SessionGroup, MessageCounter: 0x12345679,
			DestSize: DestGroup, DestGroupID: 2, HasSourceNodeID: true, SourceNodeID: 1,
			Privacy: true,
		}.MarshalPacket()
		if err != nil {
			t.Fatalf("MarshalPacket: %v", err)
		}
		if b[3] != 0x81 {
			t.Errorf("security flags = 0x%02X, want 0x81", b[3])
		}
	})
}

func TestMessageCodecEncode(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, wire, payload string
		decoded             func() (Header, ProtocolHeader)
	}{
		{"encodes a message", mcEncoded, mcPayload, mcDecoded},
		{"encodes a message 2", mcEncoded2, mcPayload2, mcDecoded2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h, p := tc.decoded()
			if got := mcEncode(t, h, p, mcHex(t, tc.payload)); !bytes.Equal(got, mcHex(t, tc.wire)) {
				t.Errorf("encoded = %X", got)
			}
		})
	}

	t.Run("writes vendor id before protocol id and round-trips (§4.4.3.4)", func(t *testing.T) {
		t.Parallel()
		h, p := mcDecoded()
		p.HasVendorID, p.VendorID, p.ProtocolID = true, 0xfff1, 0x1234 // protocolId 0xfff11234
		pb, err := p.MarshalPayload()
		if err != nil {
			t.Fatal(err)
		}
		if got := hex.EncodeToString(pb[4:8]); got != "f1ff3412" {
			t.Errorf("vendor+protocol = %s, want f1ff3412", got)
		}
		_, back, _ := mcDecode(t, mcEncode(t, h, p, mcHex(t, mcPayload)))
		if uint32(back.VendorID)<<16|uint32(back.ProtocolID) != 0xfff11234 {
			t.Errorf("round-trip protocol id = %04X%04X", back.VendorID, back.ProtocolID)
		}
	})

	t.Run("throws when encoding a message with securityExtensions data", func(t *testing.T) {
		t.Parallel()
		_, p := mcDecoded()
		p.SecuredExtension = mcHex(t, "0102030405060708090a0b0c0d0e0f10")
		if _, err := p.MarshalPayload(); !errors.Is(err, ErrEncodeSecuredExtensions) {
			t.Errorf("err = %v, want ErrEncodeSecuredExtensions", err)
		}
	})

	t.Run("throws when encoding a message with securityExtensions flag", func(t *testing.T) {
		t.Parallel()
		_, p := mcDecoded()
		p.HasSecuredExt = true
		if _, err := p.MarshalPayload(); !errors.Is(err, ErrEncodeSecuredExtensions) {
			t.Errorf("err = %v, want ErrEncodeSecuredExtensions", err)
		}
	})

	t.Run("throws when encoding a message with messageExtension data", func(t *testing.T) {
		t.Parallel()
		h, _ := mcDecoded()
		h.MessageExtension = mcHex(t, "0102030405060708090a0b0c0d0e0f10")
		if _, err := h.MarshalPacket(); !errors.Is(err, ErrEncodeExtensions) {
			t.Errorf("err = %v, want ErrEncodeExtensions", err)
		}
	})

	t.Run("throws when encoding a message with messageExtension flag", func(t *testing.T) {
		t.Parallel()
		h, _ := mcDecoded()
		h.HasExtension = true
		if _, err := h.MarshalPacket(); !errors.Is(err, ErrEncodeExtensions) {
			t.Errorf("err = %v, want ErrEncodeExtensions", err)
		}
	})
}

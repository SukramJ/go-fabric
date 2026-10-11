// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package sessionparams_test

import (
	"bytes"
	"testing"

	"github.com/SukramJ/go-fabric/secure/sessionparams"
	"github.com/SukramJ/go-fabric/tlv"
)

// TestEncodeShapeMatchesTlvSessionParameters pins the wire shape of a
// structure stating every field: context tags 1-9, TlvUInt32 for the
// intervals, the specification version and MaxTCPMessageSize, TlvUInt16
// for the rest (matter.js PaseMessages.ts:28-58). Each field is written
// with its spec width regardless of magnitude.
func TestEncodeShapeMatchesTlvSessionParameters(t *testing.T) {
	t.Parallel()
	p := sessionparams.Parameters{
		SessionIdleInterval: 500, SessionActiveInterval: 300, SessionActiveThreshold: 4000,
		DataModelRevision: 21, InteractionModelRevision: 12, SpecificationVersion: 0x01060100,
		MaxPathsPerInvoke: 1, SupportedTransports: sessionparams.TransportTCPServer, MaxTCPMessageSize: 64000,
	}
	enc := tlv.NewEncoder()
	p.Encode(enc, tlv.ContextTag(5))
	got, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{
		0x35, 0x05, // structure, context tag 5
		0x26, 0x01, 0xF4, 0x01, 0x00, 0x00, // [1] uint32 500
		0x26, 0x02, 0x2C, 0x01, 0x00, 0x00, // [2] uint32 300
		0x25, 0x03, 0xA0, 0x0F, // [3] uint16 4000
		0x25, 0x04, 0x15, 0x00, // [4] uint16 21
		0x25, 0x05, 0x0C, 0x00, // [5] uint16 12
		0x26, 0x06, 0x00, 0x01, 0x06, 0x01, // [6] uint32 0x01060100
		0x25, 0x07, 0x01, 0x00, // [7] uint16 1
		0x25, 0x08, 0x04, 0x00, // [8] uint16 bitmap tcpServer
		0x26, 0x09, 0x00, 0xFA, 0x00, 0x00, // [9] uint32 64000
		0x18, // end
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("wire\n got % X\nwant % X", got, want)
	}
	dec := tlv.NewDecoder(got)
	if _, err := dec.Next(); err != nil {
		t.Fatal(err)
	}
	back, err := sessionparams.DecodeFields(dec)
	if err != nil {
		t.Fatal(err)
	}
	if back != p {
		t.Fatalf("round trip\n got %+v\nwant %+v", back, p)
	}
}

// TestMaxTCPMessageSizeOnlyWithTCP: tag 9 is written only when the
// bitmap names TCP (SessionParameters.ts:55-62), and an unstated field is
// omitted.
func TestMaxTCPMessageSizeOnlyWithTCP(t *testing.T) {
	t.Parallel()
	encode := func(p sessionparams.Parameters) []byte {
		enc := tlv.NewEncoder()
		p.Encode(enc, tlv.ContextTag(5))
		b, err := enc.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	noTCP := encode(sessionparams.Parameters{MaxPathsPerInvoke: 1, MaxTCPMessageSize: 64000})
	if bytes.Contains(noTCP, []byte{0x26, 0x09}) {
		t.Fatalf("tag 9 written without TCP: % X", noTCP)
	}
	withTCP := encode(sessionparams.Parameters{SupportedTransports: sessionparams.TransportTCPClient, MaxTCPMessageSize: 1500})
	if !bytes.Contains(withTCP, []byte{0x26, 0x09, 0xDC, 0x05, 0x00, 0x00}) {
		t.Fatalf("tag 9 missing with TCP: % X", withTCP)
	}
	if got := encode(sessionparams.Parameters{}); !bytes.Equal(got, []byte{0x35, 0x05, 0x18}) {
		t.Fatalf("empty structure = % X", got)
	}
	if !(sessionparams.Parameters{}).IsEmpty() || (sessionparams.Parameters{MaxPathsPerInvoke: 1}).IsEmpty() {
		t.Fatal("IsEmpty")
	}
}

// TestEffectiveTransportsGateOnMatter15: a peer below specification
// version 1.5.0, or one that states none, gets its TCP bits cleared
// (SessionParameters.ts:17, :50-54); a TCP-capable peer without a stated
// size gets matter.js's 64000 fallback (:59-62).
func TestEffectiveTransportsGateOnMatter15(t *testing.T) {
	t.Parallel()
	both := sessionparams.TransportTCPClient | sessionparams.TransportTCPServer
	cases := []struct {
		name    string
		p       sessionparams.Parameters
		wantT   uint16
		wantMax uint32
	}{
		{"1.6.1 peer with TCP", sessionparams.Parameters{SpecificationVersion: 0x01060100, SupportedTransports: both, MaxTCPMessageSize: 9000}, both, 9000},
		{"1.5.0 peer, no size stated", sessionparams.Parameters{SpecificationVersion: 0x01050000, SupportedTransports: sessionparams.TransportTCPServer}, sessionparams.TransportTCPServer, 64000},
		{"1.4 peer claiming TCP", sessionparams.Parameters{SpecificationVersion: 0x01040000, SupportedTransports: both, MaxTCPMessageSize: 9000}, 0, 0},
		{"version unknown", sessionparams.Parameters{SupportedTransports: both}, 0, 0},
		{"no transports", sessionparams.Parameters{SpecificationVersion: 0x01060100}, 0, 0},
	}
	for _, tc := range cases {
		if got := tc.p.EffectiveTransports(); got != tc.wantT {
			t.Errorf("%s: transports 0x%X, want 0x%X", tc.name, got, tc.wantT)
		}
		if got := tc.p.EffectiveMaxTCPMessageSize(); got != tc.wantMax {
			t.Errorf("%s: max tcp %d, want %d", tc.name, got, tc.wantMax)
		}
	}
}

// TestDecodeSkipsUnknownAndNested: a future tag and a nested container
// inside the structure are skipped; truncated input is an error.
func TestDecodeSkipsUnknownAndNested(t *testing.T) {
	t.Parallel()
	b := []byte{
		0x35, 0x05,
		0x25, 0x07, 0x02, 0x00, // [7] 2
		0x24, 0x0A, 0x01, // [10] uint8 1 — unknown
		0x35, 0x0B, 0x24, 0x01, 0x01, 0x18, // [11] nested structure
		0x25, 0x08, 0x02, 0x00, // [8] tcpClient
		0x18,
	}
	dec := tlv.NewDecoder(b)
	if _, err := dec.Next(); err != nil {
		t.Fatal(err)
	}
	p, err := sessionparams.DecodeFields(dec)
	if err != nil {
		t.Fatal(err)
	}
	if p.MaxPathsPerInvoke != 2 || p.SupportedTransports != sessionparams.TransportTCPClient {
		t.Fatalf("decoded %+v", p)
	}
	dec = tlv.NewDecoder(b[:8])
	if _, err := dec.Next(); err != nil {
		t.Fatal(err)
	}
	if _, err := sessionparams.DecodeFields(dec); err == nil {
		t.Fatal("truncated structure decoded without error")
	}
}

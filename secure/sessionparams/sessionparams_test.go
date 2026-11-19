// SPDX-License-Identifier: Apache-2.0
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
// bitmap names TCP (SessionParameters.ts:58-63), and an unstated field
// other than the bitmap is omitted.
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
	// The bitmap is stated always, 0 without TCP (R2-P6-par-2905).
	if got := encode(sessionparams.Parameters{}); !bytes.Equal(got, []byte{0x35, 0x05, 0x25, 0x08, 0x00, 0x00, 0x18}) {
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

// TestDecodeSkipsDeepNesting: a doubly nested container at an unknown tag
// is skipped whole, and a container truncated mid-way is an error.
func TestDecodeSkipsDeepNesting(t *testing.T) {
	t.Parallel()
	b := []byte{
		0x35, 0x05,
		0x35, 0x0B, 0x35, 0x01, 0x24, 0x01, 0x01, 0x18, 0x18, // [11]{ [1]{ [1]:1 } }
		0x25, 0x07, 0x03, 0x00, // [7] 3
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
	if p.MaxPathsPerInvoke != 3 {
		t.Fatalf("decoded %+v", p)
	}
	dec = tlv.NewDecoder(b[:7]) // cut inside the nested container
	if _, err := dec.Next(); err != nil {
		t.Fatal(err)
	}
	if _, err := sessionparams.DecodeFields(dec); err == nil {
		t.Fatal("truncated nested container decoded without error")
	}
}

// TestDecodeIsStrict: the session parameters are decoded as matter.js
// decodes TlvSessionParameters (PaseMessages.ts:28-55 at b25ed960). Refused:
// a member without a context tag (TlvObject.ts:212-214), a known field of
// another TLV type than an unsigned integer (TlvNumber.ts:124-131), a value
// of fields 1-7 and 9 that does not fit its field (:145-155), and a bitmap
// (field 8) value above 16 bits — the last as connectedhomeip
// (src/protocols/secure_channel/PairingSession.cpp:247-248 at 6170af84),
// not as matter.js, whose bitmap wrapper does not bound the wire value.
// Accepted: an unknown
// context tag of any type (TlvObject.ts:215-220), a wider encoding whose
// value fits, a repeated field — the last value wins (:233).
func TestDecodeIsStrict(t *testing.T) {
	t.Parallel()
	refused := []struct {
		name   string
		member []byte
	}{
		{"anonymous tag", []byte{0x04, 0x07}},
		{"common-profile tag", []byte{0x44, 0x07, 0x00, 0x03}},
		{"fully-qualified tag at an unknown number", []byte{0xC4, 0xF1, 0xFF, 0x01, 0x00, 0x0A, 0x00, 0x03}},
		{"signed integer at [1]", []byte{0x20, 0x01, 0x05}},
		{"string at [3]", []byte{0x2C, 0x03, 0x01, 'a'}},
		{"boolean at [7]", []byte{0x28, 0x07}},
		{"null at [8]", []byte{0x34, 0x08}},
		{"float at [2]", []byte{0x2A, 0x02, 0x00, 0x00, 0x80, 0x3F}},
		{"structure at [9]", []byte{0x35, 0x09, 0x18}},
		{"array at [5]", []byte{0x36, 0x05, 0x18}},
		{"uint32 0x10000 at the uint16 [3]", []byte{0x26, 0x03, 0x00, 0x00, 0x01, 0x00}},
		{"uint64 1<<32 at the uint32 [1]", []byte{0x27, 0x01, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00}},
		{"uint32 0x10000 at the uint16 bitmap [8]", []byte{0x26, 0x08, 0x00, 0x00, 0x01, 0x00}},
	}
	for _, c := range refused {
		b := append(append([]byte{0x15}, c.member...), 0x18)
		dec := tlv.NewDecoder(b)
		if _, err := dec.Next(); err != nil {
			t.Fatal(err)
		}
		if p, err := sessionparams.DecodeFields(dec); err == nil {
			t.Errorf("%s: decoded %+v, want an error", c.name, p)
		}
	}
	accepted := []struct {
		name   string
		member []byte
		want   sessionparams.Parameters
	}{
		{"signed integer at the unknown [10]", []byte{0x20, 0x0A, 0x05}, sessionparams.Parameters{}},
		{"string at the unknown [200]", []byte{0x2C, 0xC8, 0x01, 'a'}, sessionparams.Parameters{}},
		{"uint64 holding 500 at the uint16 [3]", []byte{0x27, 0x03, 0xF4, 0x01, 0, 0, 0, 0, 0, 0}, sessionparams.Parameters{SessionActiveThreshold: 500}},
		{"uint64 holding 0xFFFFFFFF at the uint32 [1]", []byte{0x27, 0x01, 0xFF, 0xFF, 0xFF, 0xFF, 0, 0, 0, 0}, sessionparams.Parameters{SessionIdleInterval: 0xFFFFFFFF}},
		{"uint8 at the uint32 [9]", []byte{0x24, 0x09, 0x10}, sessionparams.Parameters{MaxTCPMessageSize: 0x10}},
		{"repeated [7], last wins", []byte{0x24, 0x07, 0x02, 0x24, 0x07, 0x05}, sessionparams.Parameters{MaxPathsPerInvoke: 5}},
	}
	for _, c := range accepted {
		b := append(append([]byte{0x15}, c.member...), 0x18)
		dec := tlv.NewDecoder(b)
		if _, err := dec.Next(); err != nil {
			t.Fatal(err)
		}
		p, err := sessionparams.DecodeFields(dec)
		if err != nil {
			t.Errorf("%s: %v, want accepted", c.name, err)
			continue
		}
		if p != c.want {
			t.Errorf("%s: decoded %+v, want %+v", c.name, p, c.want)
		}
	}
}

// TestClampUnauthenticated: an unauthenticated peer's intervals above one
// hour are reduced to one hour; values within the bound, a field not
// stated and every other field are kept (review II, owner decision 45).
func TestClampUnauthenticated(t *testing.T) {
	t.Parallel()
	in := sessionparams.Parameters{
		SessionIdleInterval:    0xFFFFFFFF,
		SessionActiveInterval:  sessionparams.MaxUnauthenticatedInterval + 1,
		SessionActiveThreshold: 0xFFFF,
		SpecificationVersion:   0x01060100,
	}
	got := sessionparams.ClampUnauthenticated(in)
	want := in
	want.SessionIdleInterval = sessionparams.MaxUnauthenticatedInterval
	want.SessionActiveInterval = sessionparams.MaxUnauthenticatedInterval
	if got != want {
		t.Fatalf("clamped %+v, want %+v", got, want)
	}
	within := sessionparams.Parameters{SessionIdleInterval: 500, SessionActiveInterval: sessionparams.MaxUnauthenticatedInterval}
	if got := sessionparams.ClampUnauthenticated(within); got != within {
		t.Fatalf("within the bound %+v, want %+v", got, within)
	}
	if got := sessionparams.ClampUnauthenticated(sessionparams.Parameters{}); !got.IsEmpty() {
		t.Fatalf("not stated %+v", got)
	}
}

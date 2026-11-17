// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thread

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// The replay rules of encodeWithReplay beyond the matter.js cases: a
// cleared field drops its TLV, a removed unknown TLV is dropped, a field
// the source did not carry follows in type order, and a duplicate unknown
// TLV replays once per remaining copy.
func TestDatasetReplayEdges(t *testing.T) {
	blob := encodeTLVs(t,
		TLV{Type: TLVNetworkName, Value: []byte("Foo")},
		TLV{Type: 0xaa, Value: []byte{1}},
		TLV{Type: 0xaa, Value: []byte{1}},
		TLV{Type: TLVChannel, Value: []byte{0x0f}},
		TLV{Type: 0xbb, Value: []byte{2}},
	)
	ds := decodeDataset(t, blob)
	ds.NetworkName = nil
	ds.UnknownTLVs = ds.UnknownTLVs[1:] // one 0xaa and the 0xbb remain
	ds.UnknownTLVs = ds.UnknownTLVs[:1] // drop 0xbb
	ds.DelayTimer = ptr[uint32](0x80000001)
	ds.PANID = ptr[uint16](0xabcd)
	walked, err := WalkTLVs(encodeDataset(t, ds))
	if err != nil {
		t.Fatal(err)
	}
	want := []TLV{
		{Type: 0xaa, Value: []byte{1}},
		{Type: TLVChannel, Value: []byte{0x0f}},
		{Type: TLVPANID, Value: []byte{0xab, 0xcd}},
		{Type: TLVDelayTimer, Value: []byte{0x80, 0x00, 0x00, 0x01}},
	}
	if !reflect.DeepEqual(walked, want) {
		t.Fatalf("walked = %v, want %v", walked, want)
	}
}

func TestDatasetFieldsRoundTrip(t *testing.T) {
	ds := &OperationalDataset{
		Channel:          ptr[uint16](11),
		PANID:            ptr[uint16](0x1234),
		ExtPANID:         bytes.Repeat([]byte{1}, 8),
		NetworkName:      ptr("Net"),
		PSKc:             bytes.Repeat([]byte{2}, 16),
		NetworkKey:       bytes.Repeat([]byte{3}, 16),
		MeshLocalPrefix:  bytes.Repeat([]byte{4}, 8),
		SecurityPolicy:   &SecurityPolicy{RotationTime: 672, Flags: 0xf7f8},
		ActiveTimestamp:  bytes.Repeat([]byte{5}, 8),
		PendingTimestamp: bytes.Repeat([]byte{6}, 8),
		DelayTimer:       ptr[uint32](0xfedcba98),
		ChannelMask:      []byte{0, 4, 0, 0x1f, 0xff, 0xe0},
		UnknownTLVs:      []TLV{},
	}
	b := encodeDataset(t, ds)
	back := decodeDataset(t, b)
	back.Raw, back.original = nil, nil
	if !reflect.DeepEqual(back, ds) {
		t.Fatalf("round trip = %+v, want %+v", back, ds)
	}
	for _, typ := range []TLVType{TLVPANID, TLVExtPANID, TLVMeshLocalPrefix, TLVSecurityPolicy, TLVActiveTimestamp, TLVPendingTimestamp, TLVDelayTimer, TLVChannel} {
		if _, err := DecodeDataset(encodeTLVs(t, TLV{Type: typ, Value: []byte{1, 2}[:typeBadLen(typ)]})); !errors.Is(err, ErrThreadDataset) {
			t.Errorf("type %d with a bad length: err = %v", typ, err)
		}
	}
	if _, err := DecodeDataset([]byte{0x00}); !errors.Is(err, ErrThreadTLV) {
		t.Errorf("truncated stream: err = %v", err)
	}
	if _, err := (&OperationalDataset{PSKc: make([]byte, 0x10000)}).Encode(); !errors.Is(err, ErrInvalidTLV) {
		t.Errorf("oversized value: err = %v", err)
	}
}

// typeBadLen is a value length the type rejects: 1 for PANID, 2 for the
// rest (CHANNEL takes 1 or 3).
func typeBadLen(typ TLVType) int {
	if typ == TLVPANID {
		return 1
	}
	return 2
}

func TestDecodeUTF8MatchesTextDecoder(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"OpenThread", "OpenThread"},
		{"\xef\xbb\xbfNet", "Net"},
		{"\xe2\x82\xac", "€"},
		{"\xf0\x9f\x98\x80", "😀"},
		{"\xe2\x82A", "\uFFFDA"},
		{"\xff\xff", "\uFFFD\uFFFD"},
		{"\xed\xa0\x80", "\uFFFD\uFFFD\uFFFD"},
		{"\xf0\x9f\x98", "\uFFFD"},
		{"\xc0\xaf", "\uFFFD\uFFFD"},
	} {
		if got := decodeUTF8([]byte(c.in)); got != c.want {
			t.Errorf("decodeUTF8(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDecodeDatasetHexWhitespace(t *testing.T) {
	ds, err := DecodeDatasetHex(string(rune(0xfeff)) + "00" + string(rune(0xa0)) + "01\t0f" + string(rune(0x3000)))
	if err != nil || ds.Channel == nil || *ds.Channel != 0x0f {
		t.Fatalf("ds = %+v, err = %v", ds, err)
	}
	if _, err := DecodeDatasetHex("000"); !errors.Is(err, ErrThreadDataset) {
		t.Fatalf("odd length: err = %v", err)
	}
}

// TestRedactCarriesNoSecret: the redacted copy of a decoded dataset holds
// neither the network key nor the PSKc in any field — Raw, the source
// bytes, included — so printing it does not print them (review II,
// R2-P6-sec-2022; matter.js keeps raw, a recorded divergence).
func TestRedactCarriesNoSecret(t *testing.T) {
	key := []byte{0xde, 0xad, 0xbe, 0xef, 0x10, 0x32, 0x54, 0x76, 0x98, 0xba, 0xdc, 0xfe, 0x01, 0x23, 0x45, 0x67}
	pskc := []byte{0xa1, 0xb2, 0xc3, 0xd4, 0xe5, 0xf6, 0x07, 0x18, 0x29, 0x3a, 0x4b, 0x5c, 0x6d, 0x7e, 0x8f, 0x90}
	blob := encodeTLVs(t,
		TLV{Type: TLVChannel, Value: []byte{0x00, 0x00, 0x0f}},
		TLV{Type: TLVNetworkKey, Value: key},
		TLV{Type: TLVPSKc, Value: pskc},
		TLV{Type: TLVNetworkName, Value: []byte("Lab")},
	)
	ds := decodeDataset(t, blob)
	r := ds.Redact()
	enc, err := r.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if r.Raw != nil || r.NetworkKey != nil || r.PSKc != nil {
		t.Fatalf("redacted Raw %x, NetworkKey %x, PSKc %x", r.Raw, r.NetworkKey, r.PSKc)
	}
	for _, secret := range [][]byte{key, pskc} {
		for _, form := range []string{fmt.Sprintf("%+v", *r), fmt.Sprintf("%x", enc), fmt.Sprintf("%v", *r)} {
			if strings.Contains(form, fmt.Sprintf("%x", secret)) || strings.Contains(form, strings.Trim(fmt.Sprint(secret), "[]")) {
				t.Fatalf("the redacted dataset prints a secret: %s", form)
			}
		}
	}
	if !bytes.Equal(ds.Raw, blob) || !bytes.Equal(ds.NetworkKey, key) {
		t.Fatal("Redact changed its input")
	}
	if r.NetworkName == nil || *r.NetworkName != "Lab" {
		t.Fatalf("the redacted dataset lost its name: %v", r.NetworkName)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thread

// A port of matter.js packages/protocol/test/thread/BasicTlvCodecTest.ts at
// 862ad75f: walking and encoding the MeshCoP TLV stream, extended length
// included.

import (
	"bytes"
	"encoding/hex"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func wantTruncated(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, ErrThreadTLV) || !strings.Contains(strings.ToLower(err.Error()), "truncated") {
		t.Fatalf("err = %v, want a truncated ErrThreadTLV", err)
	}
}

func TestBasicTlvCodecWalk(t *testing.T) {
	t.Run("walks two short TLVs", func(t *testing.T) {
		entries, err := WalkTLVs(mustHex(t, "0001ff0203aabbcc"))
		if err != nil {
			t.Fatal(err)
		}
		want := []TLV{{Type: 0x00, Value: []byte{0xff}}, {Type: 0x02, Value: []byte{0xaa, 0xbb, 0xcc}}}
		if !reflect.DeepEqual(entries, want) {
			t.Fatalf("entries = %v, want %v", entries, want)
		}
	})

	t.Run("returns an empty array for empty input", func(t *testing.T) {
		entries, err := WalkTLVs([]byte{})
		if err != nil || entries == nil || len(entries) != 0 {
			t.Fatalf("entries = %v, %v; want [] and no error", entries, err)
		}
	})

	t.Run("throws on a truncated TLV", func(t *testing.T) {
		_, err := WalkTLVs(mustHex(t, "0005aabb"))
		wantTruncated(t, err)
	})

	t.Run("throws on a header-only truncation", func(t *testing.T) {
		_, err := WalkTLVs([]byte{0x00})
		wantTruncated(t, err)
	})

	t.Run("decodes a TLV using the 0xFF extended-length escape", func(t *testing.T) {
		value := make([]byte, 300)
		for i := range value {
			value[i] = byte(i)
		}
		blob := append([]byte{0x05, 0xff, byte(len(value) >> 8), byte(len(value))}, value...)
		entries, err := WalkTLVs(blob)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 || entries[0].Type != 0x05 || !bytes.Equal(entries[0].Value, value) {
			t.Fatalf("entries = %v", entries)
		}
	})

	t.Run("throws on a truncated extended-length header", func(t *testing.T) {
		_, err := WalkTLVs([]byte{0x05, 0xff, 0x01})
		wantTruncated(t, err)
	})
}

func TestBasicTlvCodecEncode(t *testing.T) {
	t.Run("emits the basic length form for values <255 bytes", func(t *testing.T) {
		got, err := EncodeTLVs([]TLV{{Type: 0x00, Value: []byte{0xff}}, {Type: 0x02, Value: []byte{0xaa, 0xbb, 0xcc}}})
		if err != nil {
			t.Fatal(err)
		}
		if want := mustHex(t, "0001ff0203aabbcc"); !bytes.Equal(got, want) {
			t.Fatalf("encoded = %x, want %x", got, want)
		}
	})

	t.Run("emits the 0xFF extended-length escape for values >=255 bytes", func(t *testing.T) {
		got, err := EncodeTLVs([]TLV{{Type: 0x05, Value: make([]byte, 255)}})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1+1+2+255 || !bytes.Equal(got[:4], []byte{0x05, 0xff, 0x00, 0xff}) {
			t.Fatalf("encoded len %d, head %x", len(got), got[:4])
		}
	})

	t.Run("round-trips arbitrary entries", func(t *testing.T) {
		long := make([]byte, 512)
		for i := range long {
			long[i] = byte(i * 7)
		}
		entries := []TLV{
			{Type: 0x00, Value: []byte{0x0f}},
			{Type: 0x02, Value: []byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88}},
			{Type: 0x05, Value: long},
			{Type: 0xfe, Value: []byte{}},
		}
		encoded, err := EncodeTLVs(entries)
		if err != nil {
			t.Fatal(err)
		}
		walked, err := WalkTLVs(encoded)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(walked, entries) {
			t.Fatalf("walked = %v, want %v", walked, entries)
		}
	})

	t.Run("rejects a value larger than 0xFFFF bytes", func(t *testing.T) {
		_, err := EncodeTLVs([]TLV{{Type: 0x05, Value: make([]byte, 0x10000)}})
		if !errors.Is(err, ErrInvalidTLV) || !strings.Contains(strings.ToLower(err.Error()), "length") {
			t.Fatalf("err = %v, want an ErrInvalidTLV naming the length", err)
		}
	})

	// The TLV type is a byte (TLVType is uint8), so 0x100 cannot be built:
	// the range check of BasicTlvCodec.ts:57-59 is the Go type. The case
	// pins that, and that the top of the range encodes.
	t.Run("rejects a TLV type outside 0..255", func(t *testing.T) {
		if k := reflect.TypeOf(TLV{}.Type).Kind(); k != reflect.Uint8 {
			t.Fatalf("TLV.Type kind = %v, want uint8", k)
		}
		got, err := EncodeTLVs([]TLV{{Type: 0xff, Value: []byte{}}})
		if err != nil || !bytes.Equal(got, []byte{0xff, 0x00}) {
			t.Fatalf("encoded = %x, %v", got, err)
		}
	})
}

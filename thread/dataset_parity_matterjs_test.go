// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thread

// A port of matter.js packages/protocol/test/thread/OperationalDatasetTest.ts
// at 862ad75f: decoding a Thread operational dataset, re-encoding it
// byte-identically (non-canonical forms and unknown TLVs included), hex
// input and redaction.

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// synthetic1Hex is SYNTHETIC_1_HEX (OperationalDatasetTest.ts:11-12).
const synthetic1Hex = "00010f02081122334455667788030a4f70656e5468726561640410000102030405060708090a0b0c0d0e0f0e080000000000010000"

func encodeTLVs(t *testing.T, entries ...TLV) []byte {
	t.Helper()
	b, err := EncodeTLVs(entries)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func decodeDataset(t *testing.T, b []byte) *OperationalDataset {
	t.Helper()
	ds, err := DecodeDataset(b)
	if err != nil {
		t.Fatal(err)
	}
	return ds
}

func encodeDataset(t *testing.T, ds *OperationalDataset) []byte {
	t.Helper()
	b, err := ds.Encode()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func ptr[T any](v T) *T { return &v }

func TestOperationalDatasetDecode(t *testing.T) {
	t.Run("decodes the synthetic-1 fixture into named fields", func(t *testing.T) {
		ds := decodeDataset(t, mustHex(t, synthetic1Hex))
		pskc := make([]byte, 16)
		for i := range pskc {
			pskc[i] = byte(i)
		}
		switch {
		case ds.Channel == nil || *ds.Channel != 0x0f:
			t.Errorf("channel = %v", ds.Channel)
		case !bytes.Equal(ds.ExtPANID, []byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88}):
			t.Errorf("extPanId = %x", ds.ExtPANID)
		case ds.NetworkName == nil || *ds.NetworkName != "OpenThread":
			t.Errorf("networkName = %v", ds.NetworkName)
		case !bytes.Equal(ds.PSKc, pskc):
			t.Errorf("pskc = %x", ds.PSKc)
		case !bytes.Equal(ds.ActiveTimestamp, []byte{0, 0, 0, 0, 0, 1, 0, 0}):
			t.Errorf("activeTimestamp = %x", ds.ActiveTimestamp)
		}
	})

	// Go has one byte type; the ArrayBuffer case is a sub-slice of a larger
	// buffer (the byteOffset view of the matter.js test).
	t.Run("accepts pre-parsed Bytes (ArrayBuffer) equivalently to a Uint8Array", func(t *testing.T) {
		b := mustHex(t, synthetic1Hex)
		backing := append(append([]byte{0xee, 0xee}, b...), 0xee)
		ds := decodeDataset(t, backing[2:2+len(b)])
		if ds.NetworkName == nil || *ds.NetworkName != "OpenThread" || !bytes.Equal(ds.Raw, b) {
			t.Fatalf("networkName = %v, raw = %x", ds.NetworkName, ds.Raw)
		}
	})

	t.Run("preserves the original blob in `raw`", func(t *testing.T) {
		blob := mustHex(t, synthetic1Hex)
		ds := decodeDataset(t, blob)
		if !bytes.Equal(ds.Raw, blob) {
			t.Fatalf("raw = %x", ds.Raw)
		}
		blob[0] ^= 0xff // raw is a copy (blob.slice())
		if ds.Raw[0] != 0x00 {
			t.Fatal("raw aliases the input")
		}
	})

	t.Run("captures unknown TLVs in `unknownTlvs`", func(t *testing.T) {
		ds := decodeDataset(t, encodeTLVs(t,
			TLV{Type: 0x00, Value: []byte{0x0f}},
			TLV{Type: 0xaa, Value: []byte{0xde, 0xad, 0xbe, 0xef}},
			TLV{Type: 0xab, Value: []byte{}},
		))
		want := []TLV{{Type: 0xaa, Value: []byte{0xde, 0xad, 0xbe, 0xef}}, {Type: 0xab, Value: []byte{}}}
		if !reflect.DeepEqual(ds.UnknownTLVs, want) {
			t.Fatalf("unknownTlvs = %v, want %v", ds.UnknownTLVs, want)
		}
	})

	t.Run("decodes the security policy as a 4-byte (rotation, flags) pair", func(t *testing.T) {
		ds := decodeDataset(t, encodeTLVs(t, TLV{Type: 0x0c, Value: []byte{0x02, 0xa3, 0xff, 0xf8}}))
		if want := (SecurityPolicy{RotationTime: 0x02a3, Flags: 0xfff8}); ds.SecurityPolicy == nil || *ds.SecurityPolicy != want {
			t.Fatalf("securityPolicy = %v, want %v", ds.SecurityPolicy, want)
		}
	})

	t.Run("decodes the canonical 3-byte CHANNEL form", func(t *testing.T) {
		ds := decodeDataset(t, encodeTLVs(t, TLV{Type: 0x00, Value: []byte{0x00, 0x00, 0x19}}))
		if ds.Channel == nil || *ds.Channel != 0x19 {
			t.Fatalf("channel = %v", ds.Channel)
		}
	})

	t.Run("rejects malformed fixed-width TLVs", func(t *testing.T) {
		_, err := DecodeDataset(encodeTLVs(t, TLV{Type: 0x05, Value: []byte{0x01}}))
		if !errors.Is(err, ErrThreadDataset) || !strings.Contains(err.Error(), "NETWORK_KEY") {
			t.Fatalf("err = %v, want an ErrThreadDataset naming NETWORK_KEY", err)
		}
	})
}

func TestOperationalDatasetEncode(t *testing.T) {
	t.Run("round-trips the synthetic-1 fixture byte-identically", func(t *testing.T) {
		blob := mustHex(t, synthetic1Hex)
		if got := encodeDataset(t, decodeDataset(t, blob)); !bytes.Equal(got, blob) {
			t.Fatalf("encoded = %x, want %x", got, blob)
		}
	})

	t.Run("preserves unknown TLVs through a decode/encode round-trip", func(t *testing.T) {
		blob := encodeTLVs(t,
			TLV{Type: 0x00, Value: []byte{0x0f}},
			TLV{Type: 0xaa, Value: []byte{0xde, 0xad, 0xbe, 0xef}},
			TLV{Type: 0x03, Value: []byte("Foo")},
			TLV{Type: 0xbb, Value: []byte{0x01}},
		)
		if got := encodeDataset(t, decodeDataset(t, blob)); !bytes.Equal(got, blob) {
			t.Fatalf("encoded = %x, want %x", got, blob)
		}
	})

	t.Run("replays a non-canonical 1-byte CHANNEL encoding when fields are unmodified", func(t *testing.T) {
		blob := encodeTLVs(t, TLV{Type: 0x00, Value: []byte{0x0f}}, TLV{Type: 0x01, Value: []byte{0x12, 0x34}})
		ds := decodeDataset(t, blob)
		if ds.Channel == nil || *ds.Channel != 0x0f {
			t.Fatalf("channel = %v", ds.Channel)
		}
		if got := encodeDataset(t, ds); !bytes.Equal(got, blob) {
			t.Fatalf("encoded = %x, want %x", got, blob)
		}
	})

	t.Run("re-encodes a mutated channel in canonical 3-byte form", func(t *testing.T) {
		ds := decodeDataset(t, encodeTLVs(t, TLV{Type: 0x00, Value: []byte{0x0f}}))
		ds.Channel = ptr[uint16](0x19)
		walked, err := WalkTLVs(encodeDataset(t, ds))
		if err != nil {
			t.Fatal(err)
		}
		if want := []TLV{{Type: 0x00, Value: []byte{0x00, 0x00, 0x19}}}; !reflect.DeepEqual(walked, want) {
			t.Fatalf("walked = %v, want %v", walked, want)
		}
	})

	t.Run("emits canonical encoding for an in-memory dataset (no _originalTlvs)", func(t *testing.T) {
		ds := &OperationalDataset{
			Channel:     ptr[uint16](0x19),
			PANID:       ptr[uint16](0x1234),
			UnknownTLVs: []TLV{{Type: 0xaa, Value: []byte{0x42}}},
			Raw:         []byte{},
		}
		walked, err := WalkTLVs(encodeDataset(t, ds))
		if err != nil {
			t.Fatal(err)
		}
		want := []TLV{
			{Type: 0x00, Value: []byte{0x00, 0x00, 0x19}},
			{Type: 0x01, Value: []byte{0x12, 0x34}},
			{Type: 0xaa, Value: []byte{0x42}},
		}
		if !reflect.DeepEqual(walked, want) {
			t.Fatalf("walked = %v, want %v", walked, want)
		}
	})
}

func TestOperationalDatasetDecodeHex(t *testing.T) {
	t.Run("decodes a hex string identically to the byte form", func(t *testing.T) {
		fromHex, err := DecodeDatasetHex(synthetic1Hex)
		if err != nil {
			t.Fatal(err)
		}
		if fromBytes := decodeDataset(t, mustHex(t, synthetic1Hex)); !reflect.DeepEqual(fromHex, fromBytes) {
			t.Fatalf("fromHex = %+v, fromBytes = %+v", fromHex, fromBytes)
		}
	})

	t.Run("tolerates whitespace in the hex", func(t *testing.T) {
		ds, err := DecodeDatasetHex("00 01 0f  02 08 1122334455667788  03 0a 4f70656e5468726561640410000102030405060708090a0b0c0d0e0f 0e08 0000000000010000")
		if err != nil || ds.ExtPANID == nil {
			t.Fatalf("ds = %+v, err = %v", ds, err)
		}
	})

	t.Run("throws a typed error on malformed hex", func(t *testing.T) {
		if _, err := DecodeDatasetHex("zzzz"); !errors.Is(err, ErrThreadDataset) {
			t.Fatalf("err = %v, want ErrThreadDataset", err)
		}
	})
}

func TestOperationalDatasetRedact(t *testing.T) {
	t.Run("drops PSKC and NETWORK_KEY but preserves other fields", func(t *testing.T) {
		ds := &OperationalDataset{
			Channel:     ptr[uint16](0x19),
			PANID:       ptr[uint16](0x1234),
			NetworkName: ptr("OpenThread"),
			PSKc:        []byte{0x11, 0x22},
			NetworkKey:  make([]byte, 16),
			UnknownTLVs: []TLV{},
			Raw:         []byte{},
		}
		r := ds.Redact()
		switch {
		case r.PSKc != nil || r.NetworkKey != nil:
			t.Errorf("pskc = %x, networkKey = %x; want both unset", r.PSKc, r.NetworkKey)
		case r.Channel == nil || *r.Channel != 0x19, r.PANID == nil || *r.PANID != 0x1234:
			t.Errorf("channel = %v, panId = %v", r.Channel, r.PANID)
		case r.NetworkName == nil || *r.NetworkName != "OpenThread":
			t.Errorf("networkName = %v", r.NetworkName)
		}
	})

	t.Run("does not mutate the input dataset", func(t *testing.T) {
		pskc, networkKey := []byte{0x11}, make([]byte, 16)
		ds := &OperationalDataset{PSKc: pskc, NetworkKey: networkKey, UnknownTLVs: []TLV{}, Raw: []byte{}}
		ds.Redact()
		if &ds.PSKc[0] != &pskc[0] || &ds.NetworkKey[0] != &networkKey[0] {
			t.Fatal("redact replaced the input's secrets")
		}
	})

	t.Run("is a no-op when secrets are already absent", func(t *testing.T) {
		ds := &OperationalDataset{Channel: ptr[uint16](0x19), UnknownTLVs: []TLV{}, Raw: []byte{}}
		r := ds.Redact()
		if r.PSKc != nil || r.NetworkKey != nil || r.Channel == nil || *r.Channel != 0x19 {
			t.Fatalf("redacted = %+v", r)
		}
	})
}

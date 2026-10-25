// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/protocol/test/icd/CheckInMessageTest.ts at
// 862ad75f: the nonce, the encoding and the decoding of a Check-In payload
// against the CHIP SDK's three test vectors (CheckIn_Message_test_vectors.h,
// CheckInMessageTest.ts:12-38), the tampering and short-payload refusals,
// the ICD round trip, and the counter validation with its uint32 wrap and
// the refresh boundary at 2^31. matter.js's rejectedWith(UnexpectedDataError)
// is errors.Is(err, ErrMalformed).
package checkin

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
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

type vector struct {
	key, applicationData, nonce, payload string
	counter                              uint32
}

// vectors are CheckInMessageTest.ts:13-38 verbatim.
var vectors = []vector{
	{
		// vector 1 — empty application data
		key:             "d90e13180d00baadd20cf5ed4913d3ff",
		applicationData: "",
		counter:         12,
		nonce:           "4580d2c6f1310dc4eb64f1f8e8",
		payload:         "4580d2c6f1310dc4eb64f1f8e8bdc21fb5195d747dd2879b2b0d43ce5b1c565078",
	},
	{
		// vector 2 — 4 bytes application data ("This")
		key:             "18fdbceaef01955b0ec875eda3ae6ee8",
		applicationData: "54686973",
		counter:         15,
		nonce:           "9b02ed21ee0c7b491985502e37",
		payload:         "9b02ed21ee0c7b491985502e372dbd7b3f8b4f8e3c5ad99419389f41a8d609938c67a86d65",
	},
	{
		// vector 3 — 9 bytes application data
		key:             "d90e13180d00baadd20cf5ed4913d3ff",
		applicationData: "546869732069732061",
		counter:         11,
		nonce:           "aa84bc60886a63a8475d5dbeb5",
		payload:         "aa84bc60886a63a8475d5dbeb56d635fa95285ae33626613c7636ce3e3b2a8b13a8c89bef76891e8e296",
	},
}

func TestCheckInMessage(t *testing.T) {
	t.Parallel()

	t.Run("generateNonce", func(t *testing.T) {
		t.Parallel()
		for i, v := range vectors {
			t.Run(fmt.Sprintf("matches vector %d", i+1), func(t *testing.T) {
				t.Parallel()
				if got := Nonce(mustHex(t, v.key), v.counter); !bytes.Equal(got, mustHex(t, v.nonce)) {
					t.Fatalf("nonce = %x, want %s", got, v.nonce)
				}
			})
		}
	})

	t.Run("encode", func(t *testing.T) {
		t.Parallel()
		for i, v := range vectors {
			t.Run(fmt.Sprintf("matches vector %d", i+1), func(t *testing.T) {
				t.Parallel()
				got, err := Encode(mustHex(t, v.key), v.counter, mustHex(t, v.applicationData))
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got, mustHex(t, v.payload)) {
					t.Fatalf("payload = %x, want %s", got, v.payload)
				}
			})
		}
	})

	t.Run("decode", func(t *testing.T) {
		t.Parallel()
		for i, v := range vectors {
			t.Run(fmt.Sprintf("recovers counter and applicationData for vector %d", i+1), func(t *testing.T) {
				t.Parallel()
				counter, appData, err := Decode(mustHex(t, v.key), mustHex(t, v.payload))
				if err != nil {
					t.Fatal(err)
				}
				if counter != v.counter {
					t.Errorf("counter = %d, want %d", counter, v.counter)
				}
				if !bytes.Equal(appData, mustHex(t, v.applicationData)) {
					t.Errorf("applicationData = %x, want %s", appData, v.applicationData)
				}
			})
		}

		v := vectors[0]
		rejects := func(t *testing.T, key, payload []byte) {
			t.Helper()
			if _, _, err := Decode(key, payload); !errors.Is(err, ErrMalformed) {
				t.Fatalf("Decode = %v, want ErrMalformed", err)
			}
		}
		t.Run("rejects tampered nonce (first byte flipped)", func(t *testing.T) {
			t.Parallel()
			tampered := mustHex(t, v.payload)
			tampered[0] ^= 0xff
			rejects(t, mustHex(t, v.key), tampered)
		})
		t.Run("rejects tampered ciphertext (last byte flipped)", func(t *testing.T) {
			t.Parallel()
			tampered := mustHex(t, v.payload)
			tampered[len(tampered)-1] ^= 0xff
			rejects(t, mustHex(t, v.key), tampered)
		})
		t.Run("rejects wrong key (all-zero 16 bytes)", func(t *testing.T) {
			t.Parallel()
			rejects(t, make([]byte, 16), mustHex(t, v.payload))
		})
		t.Run("rejects too-short payload (2 bytes)", func(t *testing.T) {
			t.Parallel()
			rejects(t, mustHex(t, v.key), make([]byte, 2))
		})
	})

	t.Run("encodeIcd / decodeIcd", func(t *testing.T) {
		t.Parallel()
		t.Run("round-trips counter and activeModeThreshold", func(t *testing.T) {
			t.Parallel()
			key := mustHex(t, vectors[0].key)
			payload, err := EncodeICD(key, 42, 5000)
			if err != nil {
				t.Fatal(err)
			}
			counter, threshold, err := DecodeICD(key, payload)
			if err != nil {
				t.Fatal(err)
			}
			if counter != 42 || threshold != 5000 {
				t.Fatalf("decoded (%d, %d), want (42, 5000)", counter, threshold)
			}
		})
		t.Run("rejects applicationData shorter than 2 bytes", func(t *testing.T) {
			t.Parallel()
			key := mustHex(t, vectors[0].key)
			payload, err := Encode(key, vectors[0].counter, []byte{0x01})
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := DecodeICD(key, payload); !errors.Is(err, ErrMalformed) {
				t.Fatalf("DecodeICD = %v, want ErrMalformed", err)
			}
		})
	})

	t.Run("validateCounter", func(t *testing.T) {
		t.Parallel()
		t.Run("valid, not needing refresh: counter 15, start 10, lastOffset 2", func(t *testing.T) {
			t.Parallel()
			r := ValidateCounter(15, 10, 2)
			if !r.Valid || r.Offset != 5 || r.RefreshNeeded {
				t.Fatalf("%+v", r)
			}
		})
		t.Run("invalid (replay): counter 12, start 10, lastOffset 2", func(t *testing.T) {
			t.Parallel()
			if r := ValidateCounter(12, 10, 2); r.Valid {
				t.Fatalf("%+v", r)
			}
		})
		t.Run("invalid (stale): counter 11, start 10, lastOffset 2", func(t *testing.T) {
			t.Parallel()
			if r := ValidateCounter(11, 10, 2); r.Valid {
				t.Fatalf("%+v", r)
			}
		})
		t.Run("valid with uint32 wrap: counter 5, start 0xfffffffa, lastOffset 1", func(t *testing.T) {
			t.Parallel()
			r := ValidateCounter(5, 0xfffffffa, 1)
			if !r.Valid || r.Offset != 11 {
				t.Fatalf("%+v", r)
			}
		})
		t.Run("valid, refresh needed: counter 0x80000001, start 1, lastOffset 5", func(t *testing.T) {
			t.Parallel()
			r := ValidateCounter(0x80000001, 1, 5)
			if !r.Valid || !r.RefreshNeeded {
				t.Fatalf("%+v", r)
			}
		})
		// Spec §4.22.3.4.1: key refresh is required when the offset is >= 2^31
		// (not >). Both sides of the boundary are locked.
		t.Run("refresh NOT needed at offset 2^31 - 1", func(t *testing.T) {
			t.Parallel()
			r := ValidateCounter(0x7fffffff, 0, 0)
			if !r.Valid || r.RefreshNeeded {
				t.Fatalf("%+v", r)
			}
		})
		t.Run("refresh needed at exactly offset 2^31", func(t *testing.T) {
			t.Parallel()
			r := ValidateCounter(0x80000000, 0, 0)
			if !r.Valid || !r.RefreshNeeded {
				t.Fatalf("%+v", r)
			}
		})
	})
}

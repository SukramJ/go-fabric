// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package groups

// A port of matter.js packages/protocol/test/codec/MessageCodecTest.ts at
// 862ad75f: the privacy-enhanced packet truncated below the MIC length. The
// Go subject is the group decode (tryDecrypt), where the privacy region is
// unmasked; the rest of the file is ported in transport/message.

import (
	"encoding/hex"
	"testing"
)

func TestMessageCodecPrivacyTruncated(t *testing.T) {
	t.Parallel()
	t.Run("rejects a privacy-enhanced packet truncated below the MIC length", func(t *testing.T) {
		t.Parallel()
		// MessageCodecTest.ts:167: an 18-byte header + 8 payload bytes.
		wire, err := hex.DecodeString("067ddb81d926afce24c8a0981bdd44f4e7302b2f915a66c95962")
		if err != nil {
			t.Fatal(err)
		}
		// A real privacy key and no AEAD: without the early MIC-length
		// check the decode would unmask and reach the nil cipher.
		c := candidate{epoch: epochKey{privacy: make([]byte, keySize)}}
		if msg, ok := tryDecrypt(wire, 0xdb7d, true, c); ok || msg != nil {
			t.Errorf("tryDecrypt accepted a packet without a full MIC")
		}
	})
}

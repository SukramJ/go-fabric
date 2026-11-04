// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package aesccm

import (
	"bytes"
	"encoding/hex"
	"testing"
)

// ccmVectors are the vectors of matter.js
// packages/general/test/crypto/aes/CcmTest.ts:34-143 at 862ad75f: NIST
// CCM vectors within Matter's constraints (13-byte nonce, 16-byte tag) and
// the ones matter.js added during diagnostics. Fields: name, key, nonce,
// adata, pt, ct, tag.
var ccmVectors = [][7]string{
	{"NIST tcId 0", "0953fa93e7caac9638f58820220a398e", "00800000011201000012345678", "", "fffd034b50057e400000010000", "b5e5bfdacbaf6cb7fb6bff871f", "b0d6dd827d35bf372fa6425dcd17d356"},
	{"NIST tcId 1", "0953fa93e7caac9638f58820220a398e", "00800148202345000012345678", "", "120104320308ba072f", "79d7dbc0c9b4d43eeb", "281508e50d58dbbd27c39597800f4733"},
	{"NIST tcId 2", "0953fa93e7caac9638f58820220a398e", "00802b38322fe3000012345678", "", "120104fa0205a6000a", "53273086b8c5ee00bd", "d52b87a8ce6290a772d472b8c62bdc13"},
	{"NIST tcId 3", "be635105434859f484fc798e043ce40e", "00800000021201000012345678", "", "23450100", "b0e5d0ad", "6078e0ddbb7cd43faea57c7051e5b4ae"},
	{"NIST tcId 4", "be635105434859f484fc798e043ce40e", "00800148342345000012345678", "", "120102001234567800", "5c39da1792b1fee9ec", "a9233958aced64f2343b9d610e876440"},
	{"NIST tcId 5", "0953fa93e7caac9638f58820220a398e", "008b0148352345000012345678", "", "000300a6ac00000002", "0d0d730f94d7f3509d", "dda1694adb791652fb6ae04682f19b29"},
	{"NIST tcId 6", "0953fa93e7caac9638f58820220a398e", "008b0148362345000012345678", "", "000300a6ac00000003", "d85d806bbed248614f", "ef7f4d55e47d21522ebe3d5bc735a5c5"},
	{"NIST tcId 7", "be635105434859f484fc798e043ce40e", "00800000031201000012345678", "", "23450101", "7777ed35", "35d84e18784c4bf3cb1b4c191dc555cc"},
	{"NIST tcId 8", "be635105434859f484fc798e043ce40e", "00800000041201000012345678", "", "23450101", "ae214660", "d146b28beafe7f984f9430502d07aafe"},
	{"NIST tcId 9", "be635105434859f484fc798e043ce40e", "00800000051201000012345678", "", "23450100", "7d3ae62a", "52ee03ab84e1a33365e8a61275665f71"},
	{"NIST tcId 10", "63964771734fbd76e3b40519d1d94a48", "010007080d1234973612345677", "f4a002c7fb1e4ca0a469a021de0db875", "ea0a00576f726c64", "de1547118463123e", "14604c1ddb4f5987064b1736f3923962"},
	{"Matter message payload 1", "bacb178b2588443d5d5b1e4559e7accc", "00221453000000000000000000", "001d350022145300", "05028d040100153600172403312504fcff18172402002403302404001817240200240330240401181724020024033024040218172402002403302404031817240200240328240402181724020024032824040418172403312404031818290324ff0118", "ec2b931025dada82ed67521c966d2454d131a271023be699e4e2796650f568e590fd9b65f456c720a60a0da127eaa53974c5d41d3d933ed7b58a9ce5b5cb96ad94a7762611c48774cf75458327e74c34668a45dc9943546f8a6aa1dcd40bd4b8014bef", "b49954a097a60cbdff333ee3f2fd1f49"},
}

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestCcmVectorsParityMatterJS mirrors
// packages/general/test/crypto/aes/CcmTest.ts:145-181 at 862ad75f: each
// vector encrypts to its ciphertext and tag and decrypts back.
func TestCcmVectorsParityMatterJS(t *testing.T) {
	t.Parallel()
	for _, v := range ccmVectors {
		t.Run(v[0], func(t *testing.T) {
			t.Parallel()
			c, err := New(unhex(t, v[1]))
			if err != nil {
				t.Fatal(err)
			}
			nonce, aad, pt := unhex(t, v[2]), unhex(t, v[3]), unhex(t, v[4])
			sealed, err := c.Seal(nil, nonce, pt, aad)
			if err != nil {
				t.Fatal(err)
			}
			if got := hex.EncodeToString(sealed); got != v[5]+v[6] {
				t.Errorf("encrypts: %s, want %s%s", got, v[5], v[6])
			}
			opened, err := c.Open(nil, nonce, unhex(t, v[5]+v[6]), aad)
			if err != nil || !bytes.Equal(opened, pt) {
				t.Errorf("decrypts: %x, %v; want %s", opened, err, v[4])
			}
		})
	}
}

// TestCcmTagVerificationParityMatterJS mirrors
// packages/general/test/crypto/aes/CcmTest.ts:200-227 at 862ad75f: NIST
// tcId 10 refused with a byte flipped in each 32-bit word of its tag, and
// with the wrong key.
func TestCcmTagVerificationParityMatterJS(t *testing.T) {
	t.Parallel()
	var v [7]string
	for _, x := range ccmVectors {
		if x[0] == "NIST tcId 10" {
			v = x
		}
	}
	c, err := New(unhex(t, v[1]))
	if err != nil {
		t.Fatal(err)
	}
	nonce, aad := unhex(t, v[2]), unhex(t, v[3])
	ctLen, tagLen := len(v[5])/2, len(v[6])/2
	for word := range tagLen / 4 {
		corrupted := unhex(t, v[5]+v[6])
		corrupted[ctLen+word*4] ^= 0xff
		if _, err := c.Open(nil, nonce, corrupted, aad); err == nil {
			t.Errorf("a tag tampered in word %d opened", word)
		}
	}
	wrong, err := New(bytes.Repeat([]byte{0xff}, 16))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wrong.Open(nil, nonce, unhex(t, v[5]+v[6]), aad); err == nil {
		t.Error("decrypting with the wrong key succeeded")
	}
}

// TestStandardCryptoAeadParityMatterJS mirrors the AEAD vectors of
// packages/general/test/crypto/StandardCryptoTest.ts:42-52 and
// packages/nodejs/test/crypto/NodeJsCryptoTest.ts:47-74 at 862ad75f
// ("encrypts", "decrypts"): the same two message vectors, sealed and
// opened with Matter's AES-CCM.
func TestStandardCryptoAeadParityMatterJS(t *testing.T) {
	t.Parallel()
	c2, err := New(unhex(t, "4e4c1353a133397f7a7557c1fbd9ca38"))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := c2.Seal(nil, unhex(t, "00ec8ceb000000000000000000"),
		unhex(t, "0609523c01000fe399001528003601153501370024000024013e24020b1835012400001818181824ff0118"), unhex(t, "00c7a200ec8ceb00"))
	if err != nil || hex.EncodeToString(sealed) != "cb50871ccd35d430b9d9f9f2a50c07f6b0e68ac78f671de670bc6622c3538b10184ac58e70475301edae3d45dd169bfad3a4367cb8eb821676b162" {
		t.Errorf("encrypts: %x, %v", sealed, err)
	}
	c1, err := New(unhex(t, "abf227feffea8c38e688ddcbffc459f1"))
	if err != nil {
		t.Fatal(err)
	}
	opened, err := c1.Open(nil, unhex(t, "000ce399000000000000000000"), unhex(t, "c4527bd6965518e8382edbbd28f27f42492d0766124f9961a772"), unhex(t, "00456a000ce39900"))
	if err != nil || hex.EncodeToString(opened) != "03104f3c0000e98ceb00" {
		t.Errorf("decrypts: %x, %v", opened, err)
	}
}

// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package channel_test

import (
	"bytes"
	"crypto/aes"
	"encoding/binary"
	"testing"

	"github.com/SukramJ/go-fabric/secure/channel"
)

// TestPrivacyKeystream_CoversTheWholeProtectedRegion pins the keystream
// for a region longer than one AES block: 20 bytes (counter 4 + source
// node id 8 + 64-bit destination node id 8) is a valid privacy region,
// and matter.js runs the CTR cipher across all of it
// (MessagePrivacy.ts:53-56). The second block is AES over the same
// counter block with the counter at 2 — the AES-CCM counter layout —
// and the first block equals [channel.PrivacyMask], so callers that
// still ask for one block get what they got before.
func TestPrivacyKeystream_CoversTheWholeProtectedRegion(t *testing.T) {
	t.Parallel()
	key := bytes.Repeat([]byte{0x2A}, channel.PrivacyKeySize)
	mic := bytes.Repeat([]byte{0x5A}, 16)
	const sessionID uint16 = 0x1234

	stream, err := channel.PrivacyKeystream(key, sessionID, mic, 20)
	if err != nil {
		t.Fatalf("PrivacyKeystream: %v", err)
	}
	if len(stream) != 32 {
		t.Fatalf("keystream length = %d, want 32 (two whole blocks for a 20-byte region)", len(stream))
	}
	first, err := channel.PrivacyMask(key, sessionID, mic)
	if err != nil {
		t.Fatalf("PrivacyMask: %v", err)
	}
	if !bytes.Equal(stream[:16], first) {
		t.Errorf("first keystream block differs from PrivacyMask:\n got=%x\nwant=%x", stream[:16], first)
	}

	// Second block: 0x01 || SessionID(BE) || MIC[5:16] || 0x0002.
	block := make([]byte, 16)
	block[0] = 0x01
	binary.BigEndian.PutUint16(block[1:3], sessionID)
	copy(block[3:14], mic[5:16])
	binary.BigEndian.PutUint16(block[14:16], 2)
	cipher, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	want := make([]byte, 16)
	cipher.Encrypt(want, block)
	if !bytes.Equal(stream[16:32], want) {
		t.Errorf("second keystream block:\n got=%x\nwant=%x", stream[16:32], want)
	}

	// A 20-byte region masks and unmasks with the same stream, and no
	// byte past the first block is left untouched.
	region := make([]byte, 20)
	if err := channel.ApplyPrivacyMask(stream, region); err != nil {
		t.Fatalf("ApplyPrivacyMask (20 bytes): %v", err)
	}
	if bytes.Equal(region[16:], make([]byte, 4)) {
		t.Fatal("bytes 16..20 of the region were not masked")
	}
	if err := channel.ApplyPrivacyMask(stream, region); err != nil {
		t.Fatalf("ApplyPrivacyMask (unmask): %v", err)
	}
	if !bytes.Equal(region, make([]byte, 20)) {
		t.Fatalf("mask is not its own inverse over 20 bytes: %x", region)
	}
}

// TestApplyPrivacyMask_RejectsRegionLongerThanKeystream — a one-block
// mask over a 20-byte region is an error, never a silent partial mask.
func TestApplyPrivacyMask_RejectsRegionLongerThanKeystream(t *testing.T) {
	t.Parallel()
	if err := channel.ApplyPrivacyMask(make([]byte, 16), make([]byte, 20)); err == nil {
		t.Fatal("a 20-byte region with a 16-byte keystream must be rejected")
	}
}

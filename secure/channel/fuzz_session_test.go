// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package channel

import (
	"bytes"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/secure/aesccm"
	"github.com/SukramJ/go-fabric/transport/message"
)

// newFuzzPair builds the Alice/Bob session pair the package's own
// round-trip tests use. Sessions are per-iteration rather than shared:
// [Session.Decrypt] advances a replay window, so a session reused across
// fuzz iterations would make the outcome depend on execution order.
func newFuzzPair(t *testing.T) (alice, bob *Session) {
	t.Helper()
	aCfg, bCfg := aliceBobConfigs()
	a, err := New(aCfg)
	if err != nil {
		t.Fatalf("alice: %v", err)
	}
	b, err := New(bCfg)
	if err != nil {
		t.Fatalf("bob: %v", err)
	}
	return a, b
}

// FuzzSessionDecrypt feeds arbitrary datagrams through the split a
// receiver performs: parse the cleartext message header, then hand the
// remainder to [Session.Decrypt] as ciphertext.
//
// Decrypt runs on bytes that have not been authenticated yet — that is
// what it is for — so every input here is attacker-supplied. Asserted:
// no panic and no hang. Nothing is asserted about which frames
// authenticate: a fuzzer will not forge an AES-CCM tag, and demanding a
// particular error would pin the failure taxonomy rather than the
// safety property.
func FuzzSessionDecrypt(f *testing.F) {
	// Known-good: a genuine sealed frame, header and all, produced by the
	// same session pair the round-trip tests use.
	{
		aCfg, _ := aliceBobConfigs()
		alice, err := New(aCfg)
		if err != nil {
			f.Fatalf("seed session: %v", err)
		}
		var hdr message.Header
		hdr.SessionID = 0x1234
		out, err := alice.Encrypt(&hdr, 0, []byte("hello matter session"))
		if err != nil {
			f.Fatalf("seed encrypt: %v", err)
		}
		f.Add(append(hdr.Marshal(), out.Ciphertext...))
	}

	// Deliberately malformed: no header at all, a header with no body, a
	// header followed by a body shorter than the 16-byte MIC, and a
	// header whose ciphertext is all-ones.
	f.Add([]byte{})
	f.Add(message.Header{SessionID: 1, MessageCounter: 1}.Marshal())
	f.Add(append(message.Header{SessionID: 1, MessageCounter: 1}.Marshal(), 0x00, 0x01, 0x02))
	f.Add(append(message.Header{SessionID: 1, MessageCounter: 1}.Marshal(), bytes.Repeat([]byte{0xFF}, 32)...))

	f.Fuzz(func(t *testing.T, datagram []byte) {
		_, bob := newFuzzPair(t)
		hdr, n, err := message.UnmarshalHeader(datagram)
		if err != nil {
			// Not a decodable header — still exercise Decrypt with an
			// in-memory header so the AEAD path sees the raw bytes.
			hdr, n = message.Header{}, 0
		}
		_, _, _ = bob.Decrypt(&hdr, hdr.SecurityFlags(), datagram[n:])
	})
}

// FuzzSessionEncryptDecryptRoundTrip asserts decrypt(encrypt(x)) == x
// over the plaintext space and the security-flag byte that feeds nonce
// construction.
//
// The flags byte is fuzzed alongside the payload because it is an input
// to the nonce on both sides: a divergence between the sealing and the
// opening derivation would show up as an authentication failure on a
// frame the same process just produced.
func FuzzSessionEncryptDecryptRoundTrip(f *testing.F) {
	f.Add([]byte("hello matter session"), uint16(0x1234), uint8(0))
	f.Add([]byte{}, uint16(0), uint8(0))
	f.Add(bytes.Repeat([]byte{0xA5}, 1024), uint16(0xFFFF), uint8(0xFF))
	f.Add([]byte{0x00}, uint16(1), uint8(0x80)) // privacy bit set

	f.Fuzz(func(t *testing.T, plaintext []byte, sessionID uint16, secFlags uint8) {
		alice, bob := newFuzzPair(t)
		hdr := message.Header{SessionID: sessionID}
		out, err := alice.Encrypt(&hdr, secFlags, plaintext)
		if errors.Is(err, aesccm.ErrPlaintextTooLong) {
			// AES-CCM with a 13-byte nonce caps the message at 0xFFFF
			// bytes (secure/aesccm/aesccm.go:74). A payload past that is
			// out of the round-trip's domain, not a defect.
			return
		}
		if err != nil {
			t.Fatalf("Encrypt: %v", err)
		}
		got, duplicate, err := bob.Decrypt(&hdr, secFlags, out.Ciphertext)
		if err != nil {
			t.Fatalf("Decrypt of a freshly sealed frame failed: %v", err)
		}
		if duplicate {
			t.Fatalf("first frame of a fresh session flagged as a duplicate (counter=%d)", out.Counter)
		}
		if !bytes.Equal(got, plaintext) {
			t.Fatalf("round-trip: got %x, want %x", got, plaintext)
		}
	})
}

// FuzzPrivacyMask drives the header-obfuscation path: derive a privacy
// key from arbitrary session-key bytes, build a mask from an arbitrary
// MIC, and apply it.
//
// Asserted: no panic, and — where a mask is produced — the XOR symmetry
// the encoder and decoder both rely on, i.e. applying the same mask
// twice restores the original header slice. That is the round-trip for
// this path; a masking step that was not its own inverse would silently
// corrupt every privacy-flagged frame.
func FuzzPrivacyMask(f *testing.F) {
	sessionKey := bytes.Repeat([]byte{0x0F}, PrivacyKeySize)
	mic := bytes.Repeat([]byte{0x5A}, 16)
	f.Add(sessionKey, uint16(0x1234), mic, []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07})
	f.Add([]byte{}, uint16(0), []byte{}, []byte{})
	f.Add(sessionKey, uint16(0), mic[:4], []byte{0xFF})                   // MIC too short
	f.Add(sessionKey[:8], uint16(7), mic, bytes.Repeat([]byte{0x11}, 17)) // short key, oversized slice
	f.Add(sessionKey, uint16(0xFFFF), mic, make([]byte, PrivacyKeySize))

	f.Fuzz(func(t *testing.T, sessionKey []byte, sessionID uint16, mic, headerSlice []byte) {
		privKey, err := DerivePrivacyKey(sessionKey)
		if err != nil {
			return
		}
		mask, err := PrivacyMask(privKey, sessionID, mic)
		if err != nil {
			return
		}
		if len(mask) != PrivacyKeySize {
			t.Fatalf("PrivacyMask returned %d bytes, want %d", len(mask), PrivacyKeySize)
		}
		original := append([]byte(nil), headerSlice...)
		if err := ApplyPrivacyMask(mask, headerSlice); err != nil {
			return
		}
		if err := ApplyPrivacyMask(mask, headerSlice); err != nil {
			t.Fatalf("second ApplyPrivacyMask on the same slice failed: %v", err)
		}
		if !bytes.Equal(headerSlice, original) {
			t.Fatalf("privacy mask is not its own inverse: got %x, want %x", headerSlice, original)
		}
	})
}

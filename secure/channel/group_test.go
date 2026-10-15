// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package channel

import (
	"bytes"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/secure/aesccm"
	"github.com/SukramJ/go-fabric/transport/message"
)

func groupHeader(privacy bool) message.Header {
	return message.Header{
		SessionID: 0x5FEF, MessageCounter: 0x01020304,
		HasSourceNodeID: true, SourceNodeID: 0x1B669,
		DestSize: message.DestGroup, DestGroupID: 0x0101,
		SessionType: message.SessionGroup, Privacy: privacy,
	}
}

// TestEncryptGroupOpensWithTheReceiveNonce: the ciphertext EncryptGroup
// seals opens under the nonce and AAD the receive path builds — the
// Security Flags byte, the counter and the source node id, the
// unobfuscated header (GroupSession.ts:259-267 / :360-372).
func TestEncryptGroupOpensWithTheReceiveNonce(t *testing.T) {
	t.Parallel()
	key := bytes.Repeat([]byte{0xA5}, 16)
	hdr := groupHeader(true)
	plain := []byte("protocol header and payload")
	raw, ct, err := EncryptGroup(key, hdr.SourceNodeID, hdr, plain)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, hdr.Marshal()) || len(ct) != len(plain)+aesccm.TagSize {
		t.Fatalf("header % X, ciphertext %d bytes", raw, len(ct))
	}
	ccm, _ := aesccm.New(key)
	got, err := ccm.Open(nil, buildNonce(raw[3], hdr.MessageCounter, hdr.SourceNodeID), ct, raw)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("open: %q, %v", got, err)
	}
	// The nonce is the sender's node id, not a value of the header.
	if _, err := ccm.Open(nil, buildNonce(raw[3], hdr.MessageCounter, 0x1B66A), ct, raw); err == nil {
		t.Error("opened under another source node id")
	}
}

func TestEncryptGroupRefusals(t *testing.T) {
	t.Parallel()
	key := make([]byte, 16)
	uni := groupHeader(false)
	uni.SessionType = message.SessionUnsecured
	noGroup := groupHeader(false)
	noGroup.DestSize = message.DestNone
	for name, h := range map[string]message.Header{"unicast": uni, "no group id": noGroup} {
		if _, _, err := EncryptGroup(key, 1, h, nil); !errors.Is(err, ErrNotGroupHeader) {
			t.Errorf("%s: %v, want ErrNotGroupHeader", name, err)
		}
	}
	noSource := groupHeader(false)
	noSource.HasSourceNodeID = false
	if _, _, err := EncryptGroup(key, 1, noSource, nil); !errors.Is(err, message.ErrGroupHeader) {
		t.Errorf("no source node id: %v, want ErrGroupHeader", err)
	}
	if _, _, err := EncryptGroup(make([]byte, 15), 1, groupHeader(false), nil); err == nil {
		t.Error("a 15-byte key sealed")
	}
}

// TestObfuscateIsTheReceiveKeystream: ObfuscateGroupHeader masks the
// region past the Security Flags with the keystream the receive path
// removes (PrivacyKeystream + ApplyPrivacyMask), and the first four bytes
// stay in the clear (GroupSession.ts:276-286).
func TestObfuscateIsTheReceiveKeystream(t *testing.T) {
	t.Parallel()
	opKey := bytes.Repeat([]byte{0x3C}, 16)
	pk, err := DerivePrivacyKey(opKey)
	if err != nil {
		t.Fatal(err)
	}
	hdr := groupHeader(true)
	raw, ct, err := EncryptGroup(opKey, hdr.SourceNodeID, hdr, []byte{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	wire, err := ObfuscateGroupHeader(pk, hdr.SessionID, raw, ct)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(wire[:4], raw[:4]) || bytes.Equal(wire[4:], raw[4:]) || len(wire) != len(raw) {
		t.Fatalf("wire % X from % X", wire, raw)
	}
	region := bytes.Clone(wire[4:])
	mask, err := PrivacyKeystream(pk, hdr.SessionID, ct[len(ct)-aesccm.TagSize:], len(region))
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyPrivacyMask(mask, region); err != nil || !bytes.Equal(region, raw[4:]) {
		t.Fatalf("deobfuscated % X, want % X (%v)", region, raw[4:], err)
	}
	if !bytes.Equal(raw, hdr.Marshal()) {
		t.Error("ObfuscateGroupHeader modified its input")
	}
	// Obfuscate is its own inverse, over more than one block too.
	nonce, _ := PrivacyNonce(hdr.SessionID, ct[len(ct)-aesccm.TagSize:])
	long := bytes.Repeat([]byte{0x77}, 20)
	once, _ := Obfuscate(pk, long, nonce)
	twice, _ := Obfuscate(pk, once, nonce)
	if !bytes.Equal(twice, long) {
		t.Error("Obfuscate is not symmetric over two blocks")
	}
	stream, _ := PrivacyKeystream(pk, hdr.SessionID, ct[len(ct)-aesccm.TagSize:], 20)
	for i := range long {
		if once[i] != long[i]^stream[i] {
			t.Fatalf("byte %d differs from the receive keystream", i)
		}
	}
}

// TestPrivacyNonceLayout pins MessagePrivacy.buildNonce: the session id
// big-endian, then MIC bytes 5..15; any MIC but 16 bytes is refused.
func TestPrivacyNonceLayout(t *testing.T) {
	t.Parallel()
	mic := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	n, err := PrivacyNonce(0xABCD, mic)
	if err != nil {
		t.Fatal(err)
	}
	if want := []byte{0xAB, 0xCD, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}; !bytes.Equal(n, want) {
		t.Errorf("nonce % X, want % X", n, want)
	}
	for _, l := range []int{15, 17} {
		if _, err := PrivacyNonce(1, make([]byte, l)); !errors.Is(err, ErrPrivacyMICShort) {
			t.Errorf("%d-byte MIC: %v", l, err)
		}
	}
}

func TestObfuscateRefusals(t *testing.T) {
	t.Parallel()
	if _, err := Obfuscate(make([]byte, 15), nil, make([]byte, PrivacyNonceSize)); !errors.Is(err, ErrPrivacyKeySource) {
		t.Errorf("short key: %v", err)
	}
	if _, err := Obfuscate(make([]byte, 16), nil, make([]byte, 12)); err == nil {
		t.Error("short nonce accepted")
	}
	if _, err := ObfuscateGroupHeader(make([]byte, 16), 1, []byte{1, 2, 3}, make([]byte, 16)); err == nil {
		t.Error("3-byte header accepted")
	}
	if _, err := ObfuscateGroupHeader(make([]byte, 16), 1, make([]byte, 8), make([]byte, 15)); !errors.Is(err, ErrPrivacyMICShort) {
		t.Errorf("short ciphertext: %v", err)
	}
	if _, err := ObfuscateGroupHeader(make([]byte, 15), 1, make([]byte, 8), make([]byte, 16)); !errors.Is(err, ErrPrivacyKeySource) {
		t.Errorf("short privacy key: %v", err)
	}
}

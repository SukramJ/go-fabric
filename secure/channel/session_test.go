// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package channel

import (
	"bytes"
	"errors"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/transport/message"
)

func aliceBobConfigs() (a, b Config) {
	keyAB := []byte{
		0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
		0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
	}
	keyBA := []byte{
		0xf0, 0xe1, 0xd2, 0xc3, 0xb4, 0xa5, 0x96, 0x87,
		0x78, 0x69, 0x5a, 0x4b, 0x3c, 0x2d, 0x1e, 0x0f,
	}
	a = Config{
		EncryptKey: keyAB, DecryptKey: keyBA,
		LocalNodeID:    0x1111_2222_3333_4444,
		PeerNodeID:     0x5555_6666_7777_8888,
		InitialCounter: 1,
	}
	b = Config{
		EncryptKey: keyBA, DecryptKey: keyAB,
		LocalNodeID:    0x5555_6666_7777_8888,
		PeerNodeID:     0x1111_2222_3333_4444,
		InitialCounter: 1000,
	}
	return a, b
}

// TestRoundTripAliceToBob — Alice encrypts, Bob decrypts; Bob
// recovers the original plaintext.
func TestRoundTripAliceToBob(t *testing.T) {
	aCfg, bCfg := aliceBobConfigs()
	alice, err := New(aCfg)
	if err != nil {
		t.Fatalf("alice: %v", err)
	}
	bob, err := New(bCfg)
	if err != nil {
		t.Fatalf("bob: %v", err)
	}

	var hdr message.Header
	hdr.SessionID = 0x1234
	plaintext := []byte("hello matter session")
	out, err := alice.Encrypt(&hdr, 0, plaintext)
	if err != nil {
		t.Fatalf("alice.Encrypt: %v", err)
	}
	if hdr.MessageCounter != 1 {
		t.Errorf("counter=%d, want 1", hdr.MessageCounter)
	}
	// Secure unicast omits the Source Node ID on the wire (S flag = 0) —
	// the nonce binds to the local node id via session context, not the
	// header. Mirrors matter.js NodeSession.ts encode.
	if hdr.HasSourceNodeID || hdr.SourceNodeID != 0 {
		t.Errorf("secure unicast must not stamp source node id: has=%v src=%X",
			hdr.HasSourceNodeID, hdr.SourceNodeID)
	}

	got, _, err := bob.Decrypt(&hdr, 0, out.Ciphertext)
	if err != nil {
		t.Fatalf("bob.Decrypt: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("decrypted=%q, want %q", got, plaintext)
	}
}

// TestNewKeepsItsOwnKeys: a session copies the keys it is built from, so the
// caller may overwrite its buffers once New returned — which the PASE
// initiator does with the bytes it derived the keys from
// (client/handshake/pase.go; review II, W1-sec-05). A session that aliased
// them would go on encrypting under an all-zero key.
func TestNewKeepsItsOwnKeys(t *testing.T) {
	aCfg, bCfg := aliceBobConfigs()
	// The fixture's two configs share their key slices: give Alice her own.
	aCfg.EncryptKey = bytes.Clone(aCfg.EncryptKey)
	aCfg.DecryptKey = bytes.Clone(aCfg.DecryptKey)
	alice, err := New(aCfg)
	if err != nil {
		t.Fatalf("alice: %v", err)
	}
	clear(aCfg.EncryptKey)
	clear(aCfg.DecryptKey)
	bob, err := New(bCfg)
	if err != nil {
		t.Fatalf("bob: %v", err)
	}
	var hdr message.Header
	hdr.SessionID = 0x1234
	out, err := alice.Encrypt(&hdr, 0, []byte("after the caller's copy is gone"))
	if err != nil {
		t.Fatalf("alice.Encrypt: %v", err)
	}
	if got, _, err := bob.Decrypt(&hdr, 0, out.Ciphertext); err != nil || !bytes.Equal(got, []byte("after the caller's copy is gone")) {
		t.Fatalf("bob.Decrypt = %q, %v: Alice's session lost its key with the caller's buffer", got, err)
	}
	var back message.Header
	back.SessionID = 0x1234
	reply, err := bob.Encrypt(&back, 0, []byte("and back"))
	if err != nil {
		t.Fatalf("bob.Encrypt: %v", err)
	}
	if got, _, err := alice.Decrypt(&back, 0, reply.Ciphertext); err != nil || !bytes.Equal(got, []byte("and back")) {
		t.Fatalf("alice.Decrypt = %q, %v: Alice's session lost its decrypt key with the caller's buffer", got, err)
	}
}

// TestReplayDuplicateFlagged — second copy of the same frame still
// authenticates (we need the plaintext to extract the ProtocolHeader
// for the StandaloneAck) but Decrypt flags it as duplicate so the
// caller skips re-processing. Per Matter §4.12 + matter.js
// MessageExchange.ts:294-300.
func TestReplayDuplicateFlagged(t *testing.T) {
	aCfg, bCfg := aliceBobConfigs()
	alice, _ := New(aCfg)
	bob, _ := New(bCfg)

	var hdr message.Header
	out, err := alice.Encrypt(&hdr, 0, []byte("once"))
	if err != nil {
		t.Fatal(err)
	}
	if _, dup, err := bob.Decrypt(&hdr, 0, out.Ciphertext); err != nil || dup {
		t.Fatalf("first Decrypt: err=%v dup=%v (want err=nil dup=false)", err, dup)
	}
	plain, dup, err := bob.Decrypt(&hdr, 0, out.Ciphertext)
	if err != nil {
		t.Fatalf("second Decrypt err=%v (want nil)", err)
	}
	if !dup {
		t.Fatalf("dup=false on retransmit; want true so caller can skip re-processing")
	}
	if !bytes.Equal(plain, []byte("once")) {
		t.Fatalf("plaintext on duplicate = %q, want preserved", plain)
	}
}

// TestTamperedCiphertextRejected — flipping a payload byte must
// surface ErrUnauthenticated.
func TestTamperedCiphertextRejected(t *testing.T) {
	aCfg, bCfg := aliceBobConfigs()
	alice, _ := New(aCfg)
	bob, _ := New(bCfg)
	var hdr message.Header
	out, _ := alice.Encrypt(&hdr, 0, []byte("important"))
	out.Ciphertext[0] ^= 0x01
	if _, _, err := bob.Decrypt(&hdr, 0, out.Ciphertext); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("err = %v, want ErrUnauthenticated", err)
	}
}

// TestTamperedHeaderRejected — the header is the AAD; mutating it
// invalidates the MIC.
func TestTamperedHeaderRejected(t *testing.T) {
	aCfg, bCfg := aliceBobConfigs()
	alice, _ := New(aCfg)
	bob, _ := New(bCfg)
	var hdr message.Header
	out, _ := alice.Encrypt(&hdr, 0, []byte("important"))
	hdr.SessionID = 0xFFFF // controller-level Session-ID swap
	if _, _, err := bob.Decrypt(&hdr, 0, out.Ciphertext); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("err = %v, want ErrUnauthenticated", err)
	}
}

// TestMonotonicCounters — successive encrypts step the counter by 1.
func TestMonotonicCounters(t *testing.T) {
	aCfg, _ := aliceBobConfigs()
	alice, _ := New(aCfg)
	var prev uint32
	for i := range 5 {
		var hdr message.Header
		out, err := alice.Encrypt(&hdr, 0, []byte{byte(i)})
		if err != nil {
			t.Fatal(err)
		}
		if i > 0 && out.Counter != prev+1 {
			t.Fatalf("step %d: counter=%d, prev=%d", i, out.Counter, prev)
		}
		prev = out.Counter
	}
}

// TestEncryptOnClosedSessionFails locks the post-Close invariant.
func TestEncryptOnClosedSessionFails(t *testing.T) {
	aCfg, _ := aliceBobConfigs()
	alice, _ := New(aCfg)
	alice.Close()
	if _, err := alice.Encrypt(&message.Header{}, 0, nil); !errors.Is(err, ErrSessionInactive) {
		t.Fatalf("err = %v, want ErrSessionInactive", err)
	}
}

// TestConcurrentEncryptAndClose exercises the closed flag under
// concurrent access: one goroutine keeps encrypting while another
// closes the session, mirroring a live send racing an inbound
// shutdown signal. Run with `go test -race`.
func TestConcurrentEncryptAndClose(t *testing.T) {
	aCfg, _ := aliceBobConfigs()
	alice, err := New(aCfg)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 2000 {
			var hdr message.Header
			_, _ = alice.Encrypt(&hdr, 0, []byte("x"))
		}
	}()
	go func() {
		defer wg.Done()
		alice.Close()
	}()
	wg.Wait()
}

// TestNewRejectsBadKey passes a non-16-byte key.
func TestNewRejectsBadKey(t *testing.T) {
	if _, err := New(Config{EncryptKey: make([]byte, 8), DecryptKey: make([]byte, 16)}); err == nil {
		t.Fatal("expected error on short EncryptKey")
	}
	if _, err := New(Config{EncryptKey: make([]byte, 16), DecryptKey: make([]byte, 8)}); err == nil {
		t.Fatal("expected error on short DecryptKey")
	}
}

// TestSecurityFlagsBindToNonce changes the secFlags byte; decryption
// must fail because the nonce diverges.
func TestSecurityFlagsBindToNonce(t *testing.T) {
	aCfg, bCfg := aliceBobConfigs()
	alice, _ := New(aCfg)
	bob, _ := New(bCfg)
	var hdr message.Header
	out, _ := alice.Encrypt(&hdr, 0x00, []byte("p"))
	if _, _, err := bob.Decrypt(&hdr, 0x80, out.Ciphertext); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("err = %v, want ErrUnauthenticated (secFlags drift)", err)
	}
}

// TestNonceConstructionMatchesSpec locks the byte layout per Matter
// Core Spec §4.4.3 — secFlags || counter (LE) || srcNodeID (LE).
func TestNonceConstructionMatchesSpec(t *testing.T) {
	got := buildNonce(0x05, 0x11223344, 0xAABBCCDD_EEFF0011)
	want := []byte{
		0x05,                   // secFlags
		0x44, 0x33, 0x22, 0x11, // counter LE
		0x11, 0x00, 0xFF, 0xEE, 0xDD, 0xCC, 0xBB, 0xAA, // srcNodeID LE
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("nonce=% X, want % X", got, want)
	}
}

// TestCloseDropsCipherReferences pins the second half of the Close
// contract: the AES-CCM instances built from the session keys are
// released, not just the raw key bytes. crypto/aes keeps the expanded
// round-key schedule inside the cipher (for AES-128 the first round key
// is the key), so a closed session that still held its ciphers would
// still carry live I2R / R2I key material.
func TestCloseDropsCipherReferences(t *testing.T) {
	aCfg, bCfg := aliceBobConfigs()
	alice, err := New(aCfg)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := New(bCfg)
	if err != nil {
		t.Fatal(err)
	}
	hdr := message.Header{SessionID: 1}
	out, err := alice.Encrypt(&hdr, 0, []byte("hello"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	alice.Close()
	if alice.encCipher.Load() != nil {
		t.Error("encCipher still referenced after Close")
	}
	if alice.decCipher.Load() != nil {
		t.Error("decCipher still referenced after Close")
	}

	bob.Close()
	if _, _, err := bob.Decrypt(&hdr, 0, out.Ciphertext); !errors.Is(err, ErrSessionInactive) {
		t.Fatalf("Decrypt after Close: err = %v, want ErrSessionInactive", err)
	}
}

// TestDecryptAcceptsCounterOvertakenAtSessionStart — the first two
// messages of a session arrive out of order: the overtaken one is new,
// not a duplicate, because the reception window starts at counter 0 as
// both references start it (matter.js
// packages/protocol/src/session/NodeSession.ts:124; connectedhomeip
// src/protocols/secure_channel/PairingSession.cpp:50).
func TestDecryptAcceptsCounterOvertakenAtSessionStart(t *testing.T) {
	aCfg, bCfg := aliceBobConfigs()
	aCfg.InitialCounter = 5000
	alice, err := New(aCfg)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := New(bCfg)
	if err != nil {
		t.Fatal(err)
	}
	var h1, h2 message.Header
	first, err := alice.Encrypt(&h1, 0, []byte("first"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := alice.Encrypt(&h2, 0, []byte("second"))
	if err != nil {
		t.Fatal(err)
	}
	if _, dup, err := bob.Decrypt(&h2, 0, second.Ciphertext); err != nil || dup {
		t.Fatalf("second message first: err=%v dup=%v (want nil, false)", err, dup)
	}
	if _, dup, err := bob.Decrypt(&h1, 0, first.Ciphertext); err != nil || dup {
		t.Fatalf("overtaken first message: err=%v dup=%v (want nil, false)", err, dup)
	}
	if _, dup, err := bob.Decrypt(&h1, 0, first.Ciphertext); err != nil || !dup {
		t.Fatalf("first message again: err=%v dup=%v (want nil, true)", err, dup)
	}
}

// TestRolloverDueOnceNearExhaustion — the session reports, once, that its
// outbound counter has come within 100 000 messages of the 32-bit maximum,
// the point at which matter.js winds the session down
// (packages/protocol/src/protocol/MessageCounter.ts:13 ROLLOVER_INFO_DIFFERENCE,
// :71-76; packages/protocol/src/session/NodeSession.ts:398-412).
func TestRolloverDueOnceNearExhaustion(t *testing.T) {
	aCfg, _ := aliceBobConfigs()
	aCfg.InitialCounter = 0xFFFFFFFF - 100_001
	alice, err := New(aCfg)
	if err != nil {
		t.Fatal(err)
	}
	var h message.Header
	if _, err := alice.Encrypt(&h, 0, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if alice.RolloverDue() {
		t.Fatalf("RolloverDue after counter %#x, want false (one short of the margin)", h.MessageCounter)
	}
	if _, err := alice.Encrypt(&h, 0, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if !alice.RolloverDue() {
		t.Fatalf("RolloverDue after counter %#x, want true", h.MessageCounter)
	}
	if alice.RolloverDue() {
		t.Fatal("RolloverDue a second time, want false: the notice is given once")
	}
}

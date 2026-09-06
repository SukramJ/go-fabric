// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package channel

import (
	"bytes"
	"testing"

	"github.com/SukramJ/go-fabric/transport/message"
)

// TestDecryptNonceUsesTheSessionPeerNotTheHeaderSourceNodeID pins the
// nonce input to the peer id the session was established with. The
// sender builds its nonce from its own node id (Encrypt binds
// localNodeID), and the receiver has that id as PeerNodeID — a header
// that names a different Source Node ID must not change the nonce, or
// the frame fails the tag on a mismatch and the nonce becomes partly
// sender-chosen on a match. Mirrors matter.js NodeSession.ts:187
// (generateNonce with this.#peerNodeId).
func TestDecryptNonceUsesTheSessionPeerNotTheHeaderSourceNodeID(t *testing.T) {
	t.Parallel()
	keyAB := bytes.Repeat([]byte{0x01}, 16)
	keyBA := bytes.Repeat([]byte{0x02}, 16)
	const (
		aliceID uint64 = 0xAAAA_BBBB_CCCC_DDDD
		bobID   uint64 = 0x1111_2222_3333_4444
	)
	alice, err := New(Config{EncryptKey: keyAB, DecryptKey: keyBA, LocalNodeID: aliceID, PeerNodeID: bobID, InitialCounter: 10})
	if err != nil {
		t.Fatal(err)
	}
	bob, err := New(Config{EncryptKey: keyBA, DecryptKey: keyAB, LocalNodeID: bobID, PeerNodeID: aliceID, InitialCounter: 20})
	if err != nil {
		t.Fatal(err)
	}

	hdr := message.Header{SessionID: 7}
	out, err := alice.Encrypt(&hdr, hdr.SecurityFlags(), []byte("hello"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	// The same frame, re-labelled on the way: a Source Node ID that is
	// not alice's. The AAD is the in-memory header (Raw nil), so only
	// the nonce derivation can tell the two apart.
	relabelled := hdr
	relabelled.HasSourceNodeID = true
	relabelled.SourceNodeID = 0xDEAD_BEEF_0000_0001
	plain, _, err := bob.Decrypt(&relabelled, relabelled.SecurityFlags(), out.Ciphertext)
	if err == nil {
		// The re-labelled header changes the AAD (S flag + 8 bytes), so a
		// successful Open here would mean the tag did not bind the
		// header at all — a different defect. Report it as such.
		t.Fatalf("Decrypt accepted a header whose AAD differs from the sealed one: %q", plain)
	}

	// The honest frame: header as sealed, node id supplied only by the
	// session. This is the path every secure unicast frame takes.
	plain, _, err = bob.Decrypt(&hdr, hdr.SecurityFlags(), out.Ciphertext)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if string(plain) != "hello" {
		t.Fatalf("plaintext = %q", plain)
	}

	// And the discriminating case: a frame sealed WITH a Source Node ID
	// on the wire that is not the peer's. Alice's nonce is still built
	// from her own id, so the header field is a label only; bob must
	// decrypt it with the session's peer id and succeed. Deriving the
	// nonce from the header made this fail.
	labelled := message.Header{SessionID: 7, HasSourceNodeID: true, SourceNodeID: 0xDEAD_BEEF_0000_0001}
	out2, err := alice.Encrypt(&labelled, labelled.SecurityFlags(), []byte("again"))
	if err != nil {
		t.Fatalf("Encrypt (labelled): %v", err)
	}
	plain, _, err = bob.Decrypt(&labelled, labelled.SecurityFlags(), out2.Ciphertext)
	if err != nil {
		t.Fatalf("Decrypt (labelled): nonce must come from the session peer id, not the header: %v", err)
	}
	if string(plain) != "again" {
		t.Fatalf("plaintext = %q", plain)
	}
}

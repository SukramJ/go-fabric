// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package commissioning_test

import (
	"bytes"
	"crypto/hkdf"
	"crypto/sha256"
	"testing"

	"github.com/SukramJ/go-fabric/commissioning"
	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/secure/operational"
	"github.com/SukramJ/go-fabric/secure/spake2"
	"github.com/SukramJ/go-fabric/transport/message"
)

// TestPASEResponderKeysMatchTheOperationalPasePath runs one SPAKE2+
// handshake and holds this package's PASE key schedule against the one
// the running bridge uses (secure/operational OpenFromPase, itself the
// port of matter.js NodeSession.ts:76-86 via PaseServer.ts:184-192).
//
// Both halves are needed. The attestation challenge is what the DAC
// signature over AttestationElements / NOCSRElements binds to; a
// challenge from any other derivation is one the commissioner cannot
// reproduce, so the signature check fails and pairing aborts at the
// attestation step. The key orientation decides whether the responder's
// replies are readable at all: a session that encrypts with I2R talks
// past an initiator that decrypts with R2I.
func TestPASEResponderKeysMatchTheOperationalPasePath(t *testing.T) {
	t.Parallel()

	cfg := validPASEConfig()
	responder, err := commissioning.NewPASEResponder(cfg)
	if err != nil {
		t.Fatalf("NewPASEResponder: %v", err)
	}
	prover, err := spake2.NewProver(cfg.Passcode, cfg.Salt, cfg.Iterations, cfg.IDA, cfg.IDB, nil)
	if err != nil {
		t.Fatalf("NewProver: %v", err)
	}
	pake1, err := prover.GeneratePake1()
	if err != nil {
		t.Fatalf("GeneratePake1: %v", err)
	}
	pake2, err := responder.HandlePake1(pake1)
	if err != nil {
		t.Fatalf("HandlePake1: %v", err)
	}
	cA, err := prover.ProcessPake2(pake2.Y, pake2.CB)
	if err != nil {
		t.Fatalf("ProcessPake2: %v", err)
	}
	if err := responder.HandlePake3(cA); err != nil {
		t.Fatalf("HandlePake3: %v", err)
	}
	ke := prover.SharedSecret()

	// The production path, fed the same Ke the prover holds.
	entry, err := operational.NewManager(nil).OpenFromPase(cfg.LocalNodeID, cfg.PeerNodeID, 0x0101, ke)
	if err != nil {
		t.Fatalf("operational OpenFromPase: %v", err)
	}
	t.Cleanup(entry.Session.Close)

	challenge, err := responder.AttestationChallenge()
	if err != nil {
		t.Fatalf("AttestationChallenge: %v", err)
	}
	if !bytes.Equal(challenge, entry.AttestationChallenge) {
		t.Errorf("AttestationChallenge = %x, want %x (the operational path's third HKDF slice)",
			challenge, entry.AttestationChallenge)
	}

	// The initiator's view of the same key block: matter.js
	// NodeSession.ts:76-86 — I2R = keys[0:16], R2I = keys[16:32]; an
	// initiator encrypts with I2R and decrypts with R2I.
	block, err := hkdf.Key(sha256.New, ke, nil, "SessionKeys", 48)
	if err != nil {
		t.Fatalf("hkdf: %v", err)
	}
	initiator, err := channel.New(channel.Config{
		EncryptKey:  block[0:16],
		DecryptKey:  block[16:32],
		LocalNodeID: cfg.PeerNodeID,
		PeerNodeID:  cfg.LocalNodeID,
	})
	if err != nil {
		t.Fatalf("initiator channel: %v", err)
	}
	t.Cleanup(initiator.Close)

	sess, err := responder.Session()
	if err != nil {
		t.Fatalf("Session: %v", err)
	}
	t.Cleanup(sess.Close)

	assertReadable(t, "responder→initiator", sess, initiator)
	assertReadable(t, "initiator→responder", initiator, sess)
}

// assertReadable encrypts one frame on from and requires to to decrypt
// it — the only test of key orientation that does not restate the key
// layout on the receiving side.
func assertReadable(t *testing.T, direction string, from, to *channel.Session) {
	t.Helper()
	var hdr message.Header
	hdr.SessionID = 0x1234
	want := []byte("pase key orientation " + direction)
	out, err := from.Encrypt(&hdr, 0, want)
	if err != nil {
		t.Fatalf("%s: Encrypt: %v", direction, err)
	}
	got, _, err := to.Decrypt(&hdr, 0, out.Ciphertext)
	if err != nil {
		t.Errorf("%s: the peer cannot decrypt the frame: %v — the session keys are swapped or misderived", direction, err)
		return
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s: decrypted %q, want %q", direction, got, want)
	}
}

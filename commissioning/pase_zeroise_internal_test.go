// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package commissioning

import (
	"bytes"
	"crypto/hkdf"
	"crypto/sha256"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/secure/spake2"
	"github.com/SukramJ/go-fabric/transport/message"
)

// Whitebox: these cases hold the responder's own buffers and read them
// after the step that must have overwritten them (review II,
// R2-P0-rv-B03).

func zeroisePASEConfig() PASEConfig {
	return PASEConfig{
		Passcode:   20202021,
		Salt:       bytes.Repeat([]byte{0x53}, 16),
		Iterations: 1000,
	}
}

// pairedResponder runs a handshake up to and including Pake1/Pake2 and
// returns the responder with the prover's cA.
func pairedResponder(t *testing.T) (*PASEResponder, *spake2.Prover, []byte) {
	t.Helper()
	cfg := zeroisePASEConfig()
	responder, err := NewPASEResponder(cfg)
	if err != nil {
		t.Fatal(err)
	}
	prover, err := spake2.NewProver(cfg.Passcode, cfg.Salt, cfg.Iterations, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(prover.Close)
	pake1, err := prover.GeneratePake1()
	if err != nil {
		t.Fatal(err)
	}
	pake2, err := responder.HandlePake1(pake1)
	if err != nil {
		t.Fatal(err)
	}
	cA, err := prover.ProcessPake2(pake2.Y, pake2.CB)
	if err != nil {
		t.Fatal(err)
	}
	return responder, prover, cA
}

// TestPASEResponderOverwritesKeAndOnCloseItsKeys: the shared secret is gone
// from the verifier once HandlePake3 has derived the key block from it;
// the block serves Session and AttestationChallenge as often as they are
// asked, and Close overwrites it — without reaching into the session or
// the challenge handed out before.
func TestPASEResponderOverwritesKeAndOnCloseItsKeys(t *testing.T) {
	t.Parallel()
	responder, prover, cA := pairedResponder(t)
	if err := responder.HandlePake3(cA); err != nil {
		t.Fatal(err)
	}
	if ke := responder.verifier.SharedSecret(); ke != nil {
		t.Errorf("the verifier still hands out Ke after HandlePake3: % x", ke)
	}
	block := responder.keys
	if len(block) != 48 || bytes.Equal(block, make([]byte, 48)) {
		t.Fatalf("no key block after HandlePake3: % x", block)
	}

	// Both results, twice each: the second call serves the same values.
	sess, err := responder.Session()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := responder.Session(); err != nil {
		t.Fatalf("a second Session: %v", err)
	}
	challenge, err := responder.AttestationChallenge()
	if err != nil {
		t.Fatal(err)
	}
	again, err := responder.AttestationChallenge()
	if err != nil || !bytes.Equal(again, challenge) {
		t.Fatalf("a second AttestationChallenge = % x, %v, want % x", again, err, challenge)
	}
	want := bytes.Clone(challenge)
	// The initiator's side of the same keys, from the prover's Ke.
	peer, err := newPeerOf(prover)
	if err != nil {
		t.Fatal(err)
	}

	responder.Close()
	if !bytes.Equal(block, make([]byte, 48)) {
		t.Errorf("the key block is still readable after Close: % x", block)
	}
	if !bytes.Equal(challenge, want) {
		t.Errorf("Close reached into the challenge handed out before: % x", challenge)
	}
	var hdr message.Header
	hdr.SessionID = 1
	out, err := sess.Encrypt(&hdr, 0, []byte("after Close"))
	if err != nil {
		t.Fatalf("the session taken before Close: %v", err)
	}
	if got, _, err := peer.Decrypt(&hdr, 0, out.Ciphertext); err != nil || !bytes.Equal(got, []byte("after Close")) {
		t.Fatalf("the peer reads %q, %v: the session lost its key with the responder's block", got, err)
	}
	if _, err := responder.Session(); !errors.Is(err, ErrPASEStateMismatch) {
		t.Errorf("Session after Close: %v, want ErrPASEStateMismatch", err)
	}
	if _, err := responder.AttestationChallenge(); !errors.Is(err, ErrPASEStateMismatch) {
		t.Errorf("AttestationChallenge after Close: %v, want ErrPASEStateMismatch", err)
	}
	if err := responder.HandlePake3(cA); !errors.Is(err, ErrPASEStateMismatch) {
		t.Errorf("HandlePake3 after Close: %v, want ErrPASEStateMismatch", err)
	}
	responder.Close() // a second Close does nothing
}

// TestPASEResponderOverwritesAfterAFailedOrAbandonedHandshake: a cA that
// does not match leaves no key block and a verifier that holds nothing;
// so does a Close before Pake3.
func TestPASEResponderOverwritesAfterAFailedOrAbandonedHandshake(t *testing.T) {
	t.Parallel()

	t.Run("a wrong cA", func(t *testing.T) {
		t.Parallel()
		responder, _, cA := pairedResponder(t)
		wrong := bytes.Clone(cA)
		wrong[0] ^= 0x01
		if err := responder.HandlePake3(wrong); !errors.Is(err, spake2.ErrConfirmationFailed) {
			t.Fatalf("HandlePake3 = %v, want ErrConfirmationFailed", err)
		}
		if responder.keys != nil {
			t.Errorf("a key block after a failed confirmation: % x", responder.keys)
		}
		// The verifier is closed, not merely failed: the right cA finds
		// no state to be compared against.
		if err := responder.verifier.ProcessPake3(cA); !errors.Is(err, spake2.ErrSessionState) {
			t.Errorf("the verifier after the failure: %v, want ErrSessionState", err)
		}
		if _, err := responder.Session(); !errors.Is(err, ErrPASEStateMismatch) {
			t.Errorf("Session after the failure: %v, want ErrPASEStateMismatch", err)
		}
	})

	t.Run("closed before Pake3", func(t *testing.T) {
		t.Parallel()
		responder, _, cA := pairedResponder(t)
		responder.Close()
		if err := responder.HandlePake3(cA); !errors.Is(err, ErrPASEStateMismatch) {
			t.Fatalf("HandlePake3 after Close: %v, want ErrPASEStateMismatch", err)
		}
		if err := responder.verifier.ProcessPake3(cA); !errors.Is(err, spake2.ErrSessionState) {
			t.Errorf("the verifier after Close: %v, want ErrSessionState", err)
		}
		if _, err := responder.HandlePake1(nil); !errors.Is(err, ErrPASEStateMismatch) {
			t.Errorf("HandlePake1 after Close: %v, want ErrPASEStateMismatch", err)
		}
	})
}

// newPeerOf builds the initiator's session from the prover's Ke: it
// encrypts with I2R and decrypts with R2I.
func newPeerOf(prover *spake2.Prover) (*channel.Session, error) {
	ke := prover.SharedSecret()
	defer clear(ke)
	block, err := hkdf.Key(sha256.New, ke, nil, "SessionKeys", 48)
	if err != nil {
		return nil, err
	}
	defer clear(block)
	return channel.New(channel.Config{EncryptKey: block[0:16], DecryptKey: block[16:32]})
}

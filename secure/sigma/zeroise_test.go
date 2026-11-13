// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package sigma

import (
	"bytes"
	"crypto"
	"errors"
	"io"
	"testing"
)

// CASE key material is overwritten once it has served or the handshake has
// failed (review II, W1-sec-05). These cases hold the very buffers — a
// struct field's slice, or what secretHook sees just before the overwrite —
// and read them afterwards. None is parallel: they replace secretHook.

// held is one buffer the hook saw: the slice itself and what it held.
type held struct {
	name   string
	buf    []byte
	before []byte
}

// holdSecrets records every buffer secretHook sees until the test ends.
func holdSecrets(t *testing.T) *[]held {
	t.Helper()
	var seen []held
	prev := secretHook
	secretHook = func(name string, b []byte) {
		seen = append(seen, held{name: name, buf: b, before: bytes.Clone(b)})
	}
	t.Cleanup(func() { secretHook = prev })
	return &seen
}

func allZero(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}

// wantOverwritten: each named buffer was seen exactly times times with a
// value in it, and reads zero now.
func wantOverwritten(t *testing.T, seen []held, times int, names ...string) {
	t.Helper()
	for _, name := range names {
		n := 0
		for _, h := range seen {
			if h.name != name || len(h.before) == 0 {
				continue
			}
			n++
			if allZero(h.before) {
				t.Errorf("%s: the hook saw no value", name)
			}
			if !allZero(h.buf) {
				t.Errorf("%s: still readable after the handshake step: % x", name, h.buf)
			}
		}
		if n != times {
			t.Errorf("%s: handed to the overwrite %d times, want %d", name, n, times)
		}
	}
}

func count(seen []held, name string) int {
	n := 0
	for _, h := range seen {
		if h.name == name && len(h.before) > 0 {
			n++
		}
	}
	return n
}

// fullHandshake runs Sigma1–Sigma3 between a fresh initiator and r.
func fullHandshake(t *testing.T, f peerFixture, r *Responder, sessionID uint16) *Initiator {
	t.Helper()
	i := f.initiator(t, sessionID, nil)
	s1, err := i.GenerateSigma1()
	if err != nil {
		t.Fatal(err)
	}
	sigma2, err := r.ProcessSigma1(s1)
	if err != nil {
		t.Fatal(err)
	}
	s3, err := i.ProcessSigma2Bytes(sigma2.Marshal())
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ProcessSigma3(s3); err != nil {
		t.Fatal(err)
	}
	return i
}

// TestCASESecretsOverwrittenAfterSuccess: a completed handshake leaves the
// session keys and the shared secret of the resumption record on both
// sides, and none of S2K, S3K, their salts or the session-key material.
func TestCASESecretsOverwrittenAfterSuccess(t *testing.T) {
	seen := holdSecrets(t)
	f := newPeerFixture(t)
	responder := f.responder(0x2222)
	initiator := fullHandshake(t, f, responder, 0x1111)
	wantOverwritten(t, *seen, 2,
		"sigma2.salt", "sigma2.s2k", "sigma3.salt", "sigma3.s3k", "sessionKeys.salt", "sessionKeyMaterial")
	if n := count(*seen, "sharedSecret"); n != 0 {
		t.Fatalf("the shared secret of a finished handshake was overwritten %d times; it is the resumption record", n)
	}
	if responder.ephPriv != nil || initiator.ephPriv != nil {
		t.Error("an ephemeral private key is still referenced after the shared secret was derived")
	}
	res, ok := initiator.Result()
	keys, kok := responder.SessionKeys()
	if !ok || !kok || !constantTimeKeysEqual(res.Keys, keys) || allZero(keys.I2RKey[:]) {
		t.Fatal("the session keys did not survive the handshake")
	}
	secret := responder.ECDHSharedSecret()
	if len(secret) == 0 || allZero(secret) || !bytes.Equal(secret, res.SharedSecret) {
		t.Fatalf("the shared secret did not survive the handshake: responder % x, initiator % x", secret, res.SharedSecret)
	}
}

// TestResponderOverwritesSharedSecretAfterFailedSigma3: a Sigma3 that does
// not authenticate takes the shared secret with it.
func TestResponderOverwritesSharedSecretAfterFailedSigma3(t *testing.T) {
	seen := holdSecrets(t)
	f := newPeerFixture(t)
	responder := f.responder(0x2222)
	initiator := f.initiator(t, 0x1111, nil)
	s1, err := initiator.GenerateSigma1()
	if err != nil {
		t.Fatal(err)
	}
	sigma2, err := responder.ProcessSigma1(s1)
	if err != nil {
		t.Fatal(err)
	}
	s3, err := initiator.ProcessSigma2Bytes(sigma2.Marshal())
	if err != nil {
		t.Fatal(err)
	}
	shared := responder.shared
	if len(shared) == 0 || allZero(shared) {
		t.Fatal("no shared secret after Sigma1")
	}
	s3[len(s3)-8] ^= 0x01
	if err := responder.ProcessSigma3(s3); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("tampered Sigma3: %v, want ErrUnauthenticated", err)
	}
	if !allZero(shared) {
		t.Errorf("the shared secret is still readable after the failed Sigma3: % x", shared)
	}
	if got := responder.ECDHSharedSecret(); got != nil {
		t.Fatalf("ECDHSharedSecret after the failed Sigma3: % x", got)
	}
	if _, ok := responder.SessionKeys(); ok {
		t.Fatal("session keys after the failed Sigma3")
	}
	wantOverwritten(t, *seen, 1, "sharedSecret")
	// One S3K from the initiator's Sigma3, one from the responder's attempt
	// to open it.
	wantOverwritten(t, *seen, 2, "sigma3.salt", "sigma3.s3k")
}

// TestInitiatorOverwritesSharedSecretAfterFailedSigma2: a Sigma2 that does
// not authenticate leaves the initiator without the shared secret and S2K.
func TestInitiatorOverwritesSharedSecretAfterFailedSigma2(t *testing.T) {
	f := newPeerFixture(t)
	responder := f.responder(0x2222)
	initiator := f.initiator(t, 0x1111, nil)
	s1, err := initiator.GenerateSigma1()
	if err != nil {
		t.Fatal(err)
	}
	sigma2, err := responder.ProcessSigma1(s1)
	if err != nil {
		t.Fatal(err)
	}
	sigma2.Encrypted2[len(sigma2.Encrypted2)/2] ^= 0x01
	seen := holdSecrets(t)
	if _, err := initiator.ProcessSigma2Bytes(sigma2.Marshal()); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("tampered Sigma2: %v, want ErrUnauthenticated", err)
	}
	wantOverwritten(t, *seen, 1, "sharedSecret", "sigma2.salt", "sigma2.s2k")
	if initiator.shared != nil || initiator.ephPriv != nil {
		t.Error("the failed initiator still references the shared secret or its ephemeral key")
	}
	if _, ok := initiator.Result(); ok {
		t.Fatal("a result after the failed Sigma2")
	}
}

// failingSigner refuses to sign: the responder's Sigma1 step then fails
// after the shared secret exists.
type failingSigner struct{ pub crypto.PublicKey }

func (s failingSigner) Public() crypto.PublicKey { return s.pub }

func (failingSigner) Sign(io.Reader, []byte, crypto.SignerOpts) ([]byte, error) {
	return nil, errors.New("test signer: refused")
}

// TestResponderOverwritesSharedSecretWhenSigma1StepFails: an error after
// the ECDH — here the signer's — leaves no shared secret behind.
func TestResponderOverwritesSharedSecretWhenSigma1StepFails(t *testing.T) {
	f := newPeerFixture(t)
	f.controller.Signer = failingSigner{pub: &f.controller.PrivateKey.PublicKey}
	responder := f.responder(0x2222)
	s1, err := f.initiator(t, 0x1111, nil).GenerateSigma1()
	if err != nil {
		t.Fatal(err)
	}
	seen := holdSecrets(t)
	if _, err := responder.ProcessSigma1(s1); err == nil {
		t.Fatal("Sigma1 succeeded with a signer that refuses")
	}
	wantOverwritten(t, *seen, 1, "sharedSecret")
	if responder.shared != nil || responder.ephPriv != nil {
		t.Error("the responder still references the shared secret or its ephemeral key")
	}
}

// TestResponderOverwritesSharedSecretOnReset: a second handshake on the
// same responder ends the first one's shared secret; the copy the caller
// took for the resumption record is its own.
func TestResponderOverwritesSharedSecretOnReset(t *testing.T) {
	f := newPeerFixture(t)
	responder := f.responder(0x2222)
	fullHandshake(t, f, responder, 0x1111)
	first, taken := responder.shared, responder.ECDHSharedSecret()
	if allZero(first) {
		t.Fatal("no shared secret after the first handshake")
	}
	fullHandshake(t, f, responder, 0x1112)
	if !allZero(first) {
		t.Errorf("the first handshake's shared secret is still readable: % x", first)
	}
	if allZero(taken) {
		t.Fatal("the caller's copy of the first shared secret was overwritten")
	}
	if second := responder.ECDHSharedSecret(); len(second) == 0 || allZero(second) || bytes.Equal(second, taken) {
		t.Fatalf("the second handshake's shared secret: % x", second)
	}
}

// TestResumptionKeysOverwritten: the resumption keys and the session-key
// material end with the step on both sides; the records — the store's and
// the initiator's — stay, they are valid for the next resumption.
func TestResumptionKeysOverwritten(t *testing.T) {
	f := newPeerFixture(t)
	resp1 := f.responder(0x2222)
	first := fullHandshake(t, f, resp1, 0x1111)
	firstRes, _ := first.Result()
	storeRec := &ResumptionRecord{
		SharedSecret: resp1.ECDHSharedSecret(),
		ResumptionID: resp1.ResumptionID(),
		FabricIndex:  1,
		PeerNodeID:   f.device.NodeID,
	}
	initRec := &ResumptionRecord{
		SharedSecret: firstRes.SharedSecret,
		ResumptionID: firstRes.ResumptionID,
		PeerNodeID:   f.controller.NodeID,
	}
	want := bytes.Clone(storeRec.SharedSecret)
	second := f.initiator(t, 0x1112, initRec)
	resp2 := f.responder(0x2223)
	resp2.SetResumptionStore(mapResumptionStore{string(storeRec.ResumptionID): storeRec})

	seen := holdSecrets(t)
	s1, err := second.GenerateSigma1()
	if err != nil {
		t.Fatal(err)
	}
	result, err := resp2.ProcessSigma1WithResume(s1)
	if err != nil || !result.IsResume() {
		t.Fatalf("no resumption: %v", err)
	}
	if err := second.ProcessSigma2Resume(MarshalSigma2Resume(*result.Sigma2Resume)); err != nil {
		t.Fatal(err)
	}
	wantOverwritten(t, *seen, 2, "resume.s1rk", "resume.s2rk", "resume.sessionKeyMaterial")
	if !bytes.Equal(storeRec.SharedSecret, want) || !bytes.Equal(initRec.SharedSecret, want) {
		t.Fatal("a resumption record's shared secret was overwritten while the record is valid")
	}
	res, ok := second.Result()
	if !ok || !constantTimeKeysEqual(res.Keys, result.ResumeKeys) || !bytes.Equal(res.SharedSecret, want) {
		t.Fatal("the resumed session's keys or shared secret did not survive")
	}
	if got := resp2.ECDHSharedSecret(); !bytes.Equal(got, want) {
		t.Fatalf("the responder's shared secret after the resumption: % x", got)
	}
}

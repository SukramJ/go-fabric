// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package operational

import (
	"bytes"
	"testing"

	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/transport/message"
)

// TestOpenFromPaseOverwritesTheKeyBlock: the 48 bytes the PASE session
// keys and the attestation challenge are cut from are overwritten once the
// session and its entry hold their own copies — on the device side too,
// where they were left to the collector (review II, R2-P0-rv-B03). The
// test holds the very buffer through secretHook and reads it afterwards;
// it is not parallel, because it replaces the hook.
func TestOpenFromPaseOverwritesTheKeyBlock(t *testing.T) {
	type seen struct {
		name   string
		buf    []byte
		before []byte
	}
	var held []seen
	prev := secretHook
	secretHook = func(name string, b []byte) {
		held = append(held, seen{name: name, buf: b, before: bytes.Clone(b)})
	}
	t.Cleanup(func() { secretHook = prev })

	open := map[string]func(m *Manager, ke []byte) (*Entry, error){
		"OpenFromPase": func(m *Manager, ke []byte) (*Entry, error) {
			return m.OpenFromPase(0, 0, 0x2222, ke)
		},
		"OpenFromPaseWithID": func(m *Manager, ke []byte) (*Entry, error) {
			id, err := m.AllocateID()
			if err != nil {
				return nil, err
			}
			return m.OpenFromPaseWithID(id, 0, 0, 0x2222, ke)
		},
	}
	for name, fn := range open {
		t.Run(name, func(t *testing.T) {
			held = nil
			ke := bytes.Repeat([]byte{0x5A}, 16)
			entry, err := fn(NewManager(newMinimalFakeStore()), ke)
			if err != nil {
				t.Fatal(err)
			}
			if len(held) != 1 || held[0].name != "pase.sessionKeyMaterial" {
				t.Fatalf("the overwrite saw %d buffers (%v), want the one key block", len(held), held)
			}
			block := held[0]
			if len(block.before) != 48 || bytes.Equal(block.before, make([]byte, 48)) {
				t.Fatalf("the hook saw no key block: % x", block.before)
			}
			if !bytes.Equal(block.buf, make([]byte, 48)) {
				t.Errorf("the key block is still readable after %s: % x", name, block.buf)
			}
			// The caller's Ke is the caller's: it is not touched here.
			if !bytes.Equal(ke, bytes.Repeat([]byte{0x5A}, 16)) {
				t.Errorf("%s overwrote the shared secret it was handed: % x", name, ke)
			}
			// What must live on does: the challenge in the entry, and the
			// two keys in the session — a peer built from the block as it
			// was reads what the session seals and is read by it.
			if !bytes.Equal(entry.AttestationChallenge, block.before[32:48]) {
				t.Errorf("AttestationChallenge = % x, want % x", entry.AttestationChallenge, block.before[32:48])
			}
			peer, err := channel.New(channel.Config{EncryptKey: block.before[0:16], DecryptKey: block.before[16:32]})
			if err != nil {
				t.Fatal(err)
			}
			var hdr message.Header
			hdr.SessionID = 0x2222
			out, err := entry.Session.Encrypt(&hdr, 0, []byte("under R2I"))
			if err != nil {
				t.Fatal(err)
			}
			if got, _, err := peer.Decrypt(&hdr, 0, out.Ciphertext); err != nil || !bytes.Equal(got, []byte("under R2I")) {
				t.Fatalf("the peer reads %q, %v: the session lost its key with the block", got, err)
			}
			var back message.Header
			back.SessionID = entry.SessionID
			in, err := peer.Encrypt(&back, 0, []byte("under I2R"))
			if err != nil {
				t.Fatal(err)
			}
			if got, _, err := entry.Session.Decrypt(&back, 0, in.Ciphertext); err != nil || !bytes.Equal(got, []byte("under I2R")) {
				t.Fatalf("the session reads %q, %v: it lost its key with the block", got, err)
			}
		})
	}
}

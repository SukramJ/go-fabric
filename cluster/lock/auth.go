// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package lock

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"

	lockdef "github.com/SukramJ/go-fabric/cluster/spec/doorlock"

	"github.com/SukramJ/go-fabric/store"
)

// AuthStore persists a lock's users, credentials and schedules and the
// node's credential key: the nonvolatile State of matter.js's
// DoorLockServer (DoorLockServer.ts:1538-1574). *store.Store implements it
// on matter_doorlock_users, matter_doorlock_credentials,
// matter_doorlock_schedules and matter_settings; a host with its own store
// implements the four methods.
type AuthStore = lockdef.AuthStore

var _ AuthStore = (*store.Store)(nil)

// Cipher encrypts the credential data a lock stores: matter.js Cipher,
// DoorLockServer.cipher (DoorLockServer.ts:245-250), which a deployment
// overrides for an HSM. Decrypt returns what Encrypt was given.
type Cipher interface {
	Encrypt(plain []byte) ([]byte, error)
	Decrypt(data []byte) ([]byte, error)
}

// credentialKeySize is the 16-byte key matter.js generates
// (DoorLockServer.ts:155).
const credentialKeySize = 16

// errCiphertext reports stored credential data the key does not open.
var errCiphertext = errors.New("lock: credential data does not decrypt")

// gcmCipher is AES-128-GCM with a random nonce per encryption, the nonce
// ahead of the sealed data. matter.js encrypts with AES-128-CCM
// (AesCipher, DoorLockServer.ts:248-250); the Go standard library has no
// CCM, and the module takes no dependency for one.
type gcmCipher struct{ aead cipher.AEAD }

// NewCredentialCipher returns the default [Cipher]: AES-128-GCM under a
// 16-byte key.
func NewCredentialCipher(key []byte) (Cipher, error) {
	if len(key) != credentialKeySize {
		return nil, fmt.Errorf("%w: a credential key of %d bytes, want %d", ErrConfig, len(key), credentialKeySize)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("lock: credential cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("lock: credential cipher: %w", err)
	}
	return gcmCipher{aead}, nil
}

func (c gcmCipher) Encrypt(plain []byte) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize(), c.aead.NonceSize()+len(plain)+c.aead.Overhead())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("lock: credential nonce: %w", err)
	}
	return c.aead.Seal(nonce, nonce, plain, nil), nil
}

func (c gcmCipher) Decrypt(data []byte) ([]byte, error) {
	n := c.aead.NonceSize()
	if len(data) < n+c.aead.Overhead() {
		return nil, errCiphertext
	}
	plain, err := c.aead.Open(nil, data[:n], data[n:], nil)
	if err != nil {
		return nil, errCiphertext
	}
	return plain, nil
}

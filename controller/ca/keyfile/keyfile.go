// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package keyfile keeps a P-256 key in a passphrase-encrypted file and
// signs with it as a crypto.Signer — the alternative to the SQLite record
// for [ca.Config.RootSigner], [ca.Config.ICACSigner] and
// commissioner.FabricConfig.OperationalSigner. Standard library only: the
// passphrase is stretched with PBKDF2-HMAC-SHA256 (crypto/pbkdf2) into an
// AES-256-GCM key that seals the 32-byte private scalar; the file's
// parameters are the GCM additional data, so an edited header fails to
// open like a wrong passphrase.
//
// # Wiring a PKCS#11 or KMS signer
//
// The seams take any crypto.Signer whose Public is a P-256
// *ecdsa.PublicKey and whose Sign returns an ASN.1 ECDSA signature over
// the SHA-256 digest it is handed — the contract crypto/ecdsa's own
// PrivateKey.Sign has. A PKCS#11 module (an HSM, a TPM through its
// PKCS#11 bridge, a smart card) or a cloud KMS client (an asymmetric
// EC_SIGN_P256_SHA256 key) is wrapped in such a type in the host; it may
// add a KeyRef() string method ([ca.KeyReferencer]) to name the key in the
// stored record — a PKCS#11 URI, the KMS key version's resource name.
// The private key never enters the process: the CA's certificates and
// the controller's CASE Sigma3 signature are signed by the device. A
// signer is called under the CA's lock, once per certificate and once
// per CASE handshake, so a remote one's latency is the pairing's and the
// reconnect's, not the traffic's.
package keyfile

import (
	"bytes"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

// Format is the file format's version.
const Format = 1

// KDF names the passphrase stretching of format 1.
const KDF = "pbkdf2-hmac-sha256"

// Iterations is the PBKDF2 iteration count a new file is written with
// (OWASP's 2023 figure for PBKDF2-HMAC-SHA256).
const Iterations = 600_000

// Bounds on a file's iteration count: below minIterations the file is
// refused, above maxIterations opening it would hang.
const (
	minIterations = 100_000
	maxIterations = 10_000_000
)

// ErrDecrypt is a file that does not open: a wrong passphrase or a
// changed file.
var ErrDecrypt = errors.New("keyfile: wrong passphrase or corrupted file")

// file is the JSON on disk.
type file struct {
	Format     int    `json:"format"`
	KDF        string `json:"kdf"`
	Iterations int    `json:"iterations"`
	Salt       []byte `json:"salt"`
	Nonce      []byte `json:"nonce"`
	// PublicKey is the 65-byte uncompressed public key, readable without
	// the passphrase; it is checked against the private key on open.
	PublicKey  []byte `json:"publicKey"`
	Ciphertext []byte `json:"ciphertext"`
}

// aad is the additional data: everything but nonce and ciphertext.
func (f *file) aad() []byte {
	b, _ := json.Marshal(struct {
		Format     int    `json:"format"`
		KDF        string `json:"kdf"`
		Iterations int    `json:"iterations"`
		Salt       []byte `json:"salt"`
		PublicKey  []byte `json:"publicKey"`
	}{f.Format, f.KDF, f.Iterations, f.Salt, f.PublicKey})
	return b
}

func (f *file) aead(passphrase []byte) (cipher.AEAD, error) {
	key, err := pbkdf2.Key(sha256.New, string(passphrase), f.Salt, f.Iterations, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Signer is a P-256 key from a key file. It is safe for concurrent use.
type Signer struct {
	key  *ecdsa.PrivateKey
	path string
}

// Public implements crypto.Signer.
func (s *Signer) Public() crypto.PublicKey { return &s.key.PublicKey }

// Sign implements crypto.Signer: an ASN.1 ECDSA signature over digest,
// as ecdsa.PrivateKey.Sign.
func (s *Signer) Sign(r io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
	return s.key.Sign(r, digest, opts)
}

// KeyRef implements ca.KeyReferencer: "file:" and the path.
func (s *Signer) KeyRef() string { return "file:" + s.path }

// Open reads the key file at path.
func Open(path string, passphrase []byte) (*Signer, error) {
	b, err := os.ReadFile(path) //nolint:gosec // G304: the operator names the key file
	if err != nil {
		return nil, err
	}
	var f file
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("keyfile: %s: %w", path, err)
	}
	if f.Format != Format || f.KDF != KDF {
		return nil, fmt.Errorf("keyfile: %s: format %d / %q, want %d / %q", path, f.Format, f.KDF, Format, KDF)
	}
	if f.Iterations < minIterations || f.Iterations > maxIterations || len(f.Salt) < 16 {
		return nil, fmt.Errorf("keyfile: %s: weak or invalid parameters", path)
	}
	aead, err := f.aead(passphrase)
	if err != nil {
		return nil, err
	}
	if len(f.Nonce) != aead.NonceSize() {
		return nil, fmt.Errorf("keyfile: %s: nonce of %d bytes", path, len(f.Nonce))
	}
	scalar, err := aead.Open(nil, f.Nonce, f.Ciphertext, f.aad())
	if err != nil {
		return nil, ErrDecrypt
	}
	key, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), scalar)
	clear(scalar)
	if err != nil {
		return nil, fmt.Errorf("keyfile: %s: %w", path, err)
	}
	pub, err := key.PublicKey.Bytes()
	if err != nil || !bytes.Equal(pub, f.PublicKey) {
		return nil, fmt.Errorf("keyfile: %s: the public key does not match the private key", path)
	}
	return &Signer{key: key, path: path}, nil
}

// Create writes key to a new file at path (mode 0600; an existing file is
// an error) under passphrase and returns its signer. A nil key generates
// one.
func Create(path string, passphrase []byte, key *ecdsa.PrivateKey) (*Signer, error) {
	return create(path, passphrase, key, Iterations)
}

func create(path string, passphrase []byte, key *ecdsa.PrivateKey, iterations int) (*Signer, error) {
	if len(passphrase) == 0 {
		return nil, errors.New("keyfile: empty passphrase")
	}
	if key == nil {
		k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, err
		}
		key = k
	}
	if key.Curve != elliptic.P256() {
		return nil, errors.New("keyfile: not a P-256 key")
	}
	pub, err := key.PublicKey.Bytes()
	if err != nil {
		return nil, err
	}
	scalar, err := key.Bytes()
	if err != nil {
		return nil, err
	}
	defer clear(scalar)
	f := file{Format: Format, KDF: KDF, Iterations: iterations, Salt: make([]byte, 32), PublicKey: pub}
	if _, err := rand.Read(f.Salt); err != nil {
		return nil, err
	}
	aead, err := f.aead(passphrase)
	if err != nil {
		return nil, err
	}
	f.Nonce = make([]byte, aead.NonceSize())
	if _, err := rand.Read(f.Nonce); err != nil {
		return nil, err
	}
	f.Ciphertext = aead.Seal(nil, f.Nonce, scalar, f.aad())
	b, err := json.MarshalIndent(&f, "", "  ")
	if err != nil {
		return nil, err
	}
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // G304: the operator names the key file
	if err != nil {
		return nil, err
	}
	if _, err := out.Write(append(b, '\n')); err != nil {
		_ = out.Close()
		_ = os.Remove(path)
		return nil, err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	return &Signer{key: key, path: path}, nil
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package cli

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"thingfabric.dev/go-fabric/store"
)

// sealKeySize is the AES-256 key store.NewAESGCMSealer takes.
const sealKeySize = 32

// openSealKey is --seal-key: nil without it (the database is protected by
// its file alone), else the sealer controller.Config.Sealer and the tool's
// own settings store seal the database's secrets with — the wiring
// docs/security.md ("Sealing at rest") recommends (R2-P6-sec-2604).
// "file:<path>" names a file holding the 32-byte key as 64 hex digits; a
// missing file is created with a new random key, readable by its owner
// only. The key must stay outside the database, and every later run needs
// the same file: a database sealed under it does not open without it.
func openSealKey(arg string) (store.Sealer, error) {
	if arg == "" {
		return nil, nil
	}
	path, ok := strings.CutPrefix(arg, "file:")
	if !ok || path == "" {
		return nil, fmt.Errorf("--seal-key %q: want file:<path>", arg)
	}
	key, err := readSealKey(path)
	if errors.Is(err, fs.ErrNotExist) {
		key, err = createSealKey(path)
	}
	if err != nil {
		return nil, fmt.Errorf("--seal-key: %w", err)
	}
	s, err := store.NewAESGCMSealer(key)
	if err != nil {
		return nil, fmt.Errorf("--seal-key: %w", err)
	}
	return s, nil
}

// readSealKey reads a key file: 64 hex digits, surrounding white space
// ignored.
func readSealKey(path string) ([]byte, error) {
	b, err := os.ReadFile(path) //nolint:gosec // G304: the operator names the key file
	if err != nil {
		return nil, err
	}
	key, err := hex.DecodeString(string(bytes.TrimSpace(b)))
	if err != nil || len(key) != sealKeySize {
		return nil, fmt.Errorf("%s: want %d bytes as %d hex digits", path, sealKeySize, 2*sealKeySize)
	}
	return key, nil
}

// createSealKey writes a new random key to path, which must not exist,
// with mode 0600.
func createSealKey(path string) ([]byte, error) {
	key := make([]byte, sealKeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // G304: the operator names the key file
	if err != nil {
		return nil, err
	}
	if _, err := f.WriteString(hex.EncodeToString(key) + "\n"); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return nil, err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	return key, nil
}

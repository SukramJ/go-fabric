// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/SukramJ/go-fabric/controller/ca"
	"github.com/SukramJ/go-fabric/controller/ca/keyfile"
)

// passphraseEnv is the environment variable --ca-key file: reads the key
// file's passphrase from.
const passphraseEnv = "GOFABRIC_KEY_PASSPHRASE" //nolint:gosec // G101: the variable's name, not a credential

// openCAKey is --ca-key: nil without it (the root key stays in the
// database), else the root key's signer. "file:<path>" opens the key file;
// a missing file is created — with the root key the database still holds
// (it then leaves the database) or, for a new database, a new key.
func openCAKey(e *env, cs ca.SettingStore) (crypto.Signer, error) {
	if e.opts.caKey == "" {
		return nil, nil
	}
	path, ok := strings.CutPrefix(e.opts.caKey, "file:")
	if !ok || path == "" {
		return nil, fmt.Errorf("--ca-key %q: want file:<path>", e.opts.caKey)
	}
	pass := os.Getenv(passphraseEnv)
	if pass == "" {
		return nil, fmt.Errorf("--ca-key: set the key file's passphrase in %s", passphraseEnv)
	}
	s, err := keyfile.Open(path, []byte(pass))
	if err == nil {
		e.log.Debug("controller.ca_key", "ref", s.KeyRef())
		return s, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("--ca-key: %w", err)
	}
	var key *ecdsa.PrivateKey
	stored, ok, err := cs.Load()
	if err != nil {
		return nil, err
	}
	if ok && stored.RootKeyPair != nil && len(stored.RootKeyPair.PrivateKey) > 0 {
		if key, err = ecdsa.ParseRawPrivateKey(elliptic.P256(), stored.RootKeyPair.PrivateKey); err != nil {
			return nil, fmt.Errorf("--ca-key: the stored root key: %w", err)
		}
	} else if ok {
		return nil, fmt.Errorf("--ca-key: %s does not exist and the database holds no root key to move into it (%q)", path, stored.RootKeyRef)
	}
	if s, err = keyfile.Create(path, []byte(pass), key); err != nil {
		return nil, fmt.Errorf("--ca-key: %w", err)
	}
	if key != nil {
		e.log.Info("controller.ca_key.moved", "ref", s.KeyRef())
	} else {
		e.log.Info("controller.ca_key.created", "ref", s.KeyRef())
	}
	return s, nil
}

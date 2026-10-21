// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package dcl

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// Store keeps the trust store between runs — the "certificates" storage
// of matter.js's DclCertificateService (DclCertificateService.ts:333-335:
// the index and one DER per key identifier), here one blob the
// [CertificateStore] writes whole.
type Store interface {
	// Load returns what Save last stored; nil and no error when nothing is.
	Load(ctx context.Context) ([]byte, error)
	// Save replaces what is stored.
	Save(ctx context.Context, data []byte) error
}

// MemoryStore is a [Store] in memory, the default: nothing survives the
// process.
type MemoryStore struct {
	// mu guards data.
	mu   sync.Mutex
	data []byte
}

// Load implements [Store].
func (m *MemoryStore) Load(context.Context) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]byte(nil), m.data...), nil
}

// Save implements [Store].
func (m *MemoryStore) Save(_ context.Context, data []byte) error {
	m.mu.Lock()
	m.data = append([]byte(nil), data...)
	m.mu.Unlock()
	return nil
}

// FileStore is a [Store] in the file the host names, written through a
// temporary file and a rename.
type FileStore struct {
	// Path is the file.
	Path string
}

// Load implements [Store]; a missing file is nothing stored.
func (f FileStore) Load(context.Context) ([]byte, error) {
	b, err := os.ReadFile(f.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return b, err
}

// Save implements [Store].
func (f FileStore) Save(_ context.Context, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(f.Path), filepath.Base(f.Path)+".*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), f.Path)
}

// Settings is a string key-value store — *store.Store's GetSetting and
// SetSetting, which [SettingStore] keeps the trust store under.
type Settings interface {
	GetSetting(ctx context.Context, key string) (string, bool, error)
	SetSetting(ctx context.Context, key, value string) error
}

// SettingStore is a [Store] under one key of a [Settings] store.
type SettingStore struct {
	Settings Settings
	// Key is the setting's key.
	Key string
}

// Load implements [Store].
func (s SettingStore) Load(ctx context.Context) ([]byte, error) {
	v, ok, err := s.Settings.GetSetting(ctx, s.Key)
	if err != nil || !ok {
		return nil, err
	}
	return []byte(v), nil
}

// Save implements [Store].
func (s SettingStore) Save(ctx context.Context, data []byte) error {
	return s.Settings.SetSetting(ctx, s.Key, string(data))
}

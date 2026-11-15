// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/store"
)

// TestStoreSealerSealsTheBuiltStore (seal round 2): with Store.Sealer the
// node opens its credential store with store.NewSealed — a secret it writes
// is sealed in the file, so a store without the sealer reads it as
// store.ErrNoSealer — and Validate refuses a Sealer beside a Credentials
// override.
func TestStoreSealerSealsTheBuiltStore(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db := openDB(t, filepath.Join(t.TempDir(), "node.db"))
	sealer, err := store.NewAESGCMSealer(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	cfg := minimalConfig(db)
	cfg.Store.Sealer = sealer
	n := newNode(t, cfg)
	const sealedKey = "netcomm.wifi.networks" // a sealed setting (store/seal.go isSealedSetting)
	if err := n.Store().SetSetting(ctx, sealedKey, "[]"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.New(db).GetSetting(ctx, sealedKey); !errors.Is(err, store.ErrNoSealer) {
		t.Errorf("a store without the sealer reads the node's secret: %v, want store.ErrNoSealer", err)
	}
	if v, ok, err := n.Store().GetSetting(ctx, sealedKey); err != nil || !ok || v != "[]" {
		t.Errorf("the node reads its own secret: %q %v %v", v, ok, err)
	}

	bad := minimalConfig(nil)
	bad.Store = node.Store{DB: db, Credentials: store.New(db), Sealer: sealer}
	if err := bad.Validate(); !errors.Is(err, node.ErrInvalidConfig) || !strings.Contains(err.Error(), "Store.Sealer") {
		t.Errorf("Sealer beside Credentials: %v, want ErrInvalidConfig", err)
	}
}

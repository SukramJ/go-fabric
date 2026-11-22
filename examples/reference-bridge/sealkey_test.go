// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"thingfabric.dev/go-fabric/node"
	"thingfabric.dev/go-fabric/store"
)

// TestSealKeySealsTheNodeStore holds R2-P6-sec-2604 on the daemon:
// --seal-key file: creates its key file (0600) and the node opens --db
// sealed — the seal canary is written and sealed; a --dump-surface
// composition takes no key; a malformed argument or key file is refused.
func TestSealKeySealsTheNodeStore(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "seal.key")
	var dumpCfg node.Config
	if err := applySealKey(&dumpCfg, flags{sealKey: "file:" + keyPath}, &dumpEnv{}); err != nil || dumpCfg.Store.Sealer != nil {
		t.Fatalf("dump: sealer %v err %v", dumpCfg.Store.Sealer, err)
	}
	if _, err := os.Stat(keyPath); !os.IsNotExist(err) {
		t.Fatalf("the dump created the key file: %v", err)
	}
	var sealer store.Sealer
	path := filepath.Join(dir, "daemon.db")
	testNodeWith(t, newFleet(), path, func(cfg *node.Config) {
		if err := applySealKey(cfg, flags{sealKey: "file:" + keyPath}, nil); err != nil {
			t.Fatal(err)
		}
		sealer = cfg.Store.Sealer
	})
	if sealer == nil {
		t.Fatal("no sealer")
	}
	if fi, err := os.Stat(keyPath); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("key file: %v %v", fi, err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var sealed bool
	if err := db.QueryRowContext(context.Background(), `SELECT sealed FROM matter_settings WHERE key = 'store.seal_canary'`).Scan(&sealed); err != nil || !sealed {
		t.Fatalf("seal canary: sealed=%v err=%v", sealed, err)
	}
	short := filepath.Join(dir, "short.key")
	if err := os.WriteFile(short, []byte("abcd"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{keyPath, "file:", "file:" + short} {
		if _, err := openSealKey(bad); err == nil {
			t.Errorf("--seal-key %q accepted", bad)
		}
	}
}

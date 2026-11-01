// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/SukramJ/go-fabric/node"
)

// Store.Path: the node opens the file, Stop closes it, and the next node on
// the same file reads what the first one stored.
func TestStorePathOpensAndCloses(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "path.db")
	cfg := minimalConfig(nil)
	cfg.Store = node.Store{Path: path}
	n, err := node.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := n.Store().SetSetting(ctx, "bs.path", "kept"); err != nil {
		t.Fatal(err)
	}
	if err := n.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, err := n.Store().GetSetting(ctx, "bs.path"); err == nil {
		t.Fatal("the store answers after Stop: the node did not close its handle")
	}

	again := newNode(t, cfg)
	v, found, err := again.Store().GetSetting(ctx, "bs.path")
	if err != nil || !found || v != "kept" {
		t.Fatalf("second node reads %q %v %v, want the first node's value", v, found, err)
	}
}

// Path and DB exclude each other; Path alone satisfies Validate.
func TestStorePathValidate(t *testing.T) {
	cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "db.db")))
	cfg.Store.Path = filepath.Join(t.TempDir(), "path.db")
	if err := cfg.Validate(); !errors.Is(err, node.ErrInvalidConfig) {
		t.Fatalf("Path with DB: %v, want ErrInvalidConfig", err)
	}
	cfg.Store.DB = nil
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Path alone: %v", err)
	}
	cfg.Store.Path = ""
	if err := cfg.Validate(); !errors.Is(err, node.ErrInvalidConfig) {
		t.Fatalf("no store: %v, want ErrInvalidConfig", err)
	}
}

// OpenSQLite builds the file: URI with net/url: a path holding '#' and '?'
// opens that very file, and the pragmas still reach every connection
// (review W5-ex-01 — concatenated, the '#' or '?' ended the file name and
// dropped foreign_keys without an error).
func TestOpenSQLiteEscapesThePath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "iter#02", "a?b")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "store#1?x=y.db")
	db, err := node.OpenSQLite(path, "journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec("CREATE TABLE t (x INTEGER)"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the database is not at the path it was opened with: %v", err)
	}
	var fk int
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil || fk != 1 {
		t.Errorf("foreign_keys = %d (%v), want 1", fk, err)
	}
	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		t.Errorf("journal_mode = %q (%v), want wal", mode, err)
	}
}

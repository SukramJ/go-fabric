// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package store_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/SukramJ/go-fabric/store"
)

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
	db, err := store.OpenSQLite(path, "journal_mode(WAL)")
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

// A relative path opens the file relative to the working directory: the
// URI carries no authority (file:rel.db, not file://rel.db, which SQLite
// refuses as an invalid authority).
func TestOpenSQLiteRelativePath(t *testing.T) {
	t.Chdir(t.TempDir())
	db, err := store.OpenSQLite("rel.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec("CREATE TABLE t (x INTEGER)"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := os.Stat("rel.db"); err != nil {
		t.Fatalf("the database is not at the relative path: %v", err)
	}
}

// A store file OpenSQLite creates, and the -wal and -shm files SQLite
// creates beside it, are the owner's alone (0600): the file holds the
// operational keys unencrypted (review W5-ex-05).
func TestOpenSQLiteCreatesTheFileOwnerOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.db")
	db, err := store.OpenSQLite(path, "journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec("CREATE TABLE t (x INTEGER); INSERT INTO t VALUES (1)"); err != nil {
		t.Fatalf("write: %v", err)
	}
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if perm := info.Mode().Perm(); perm&0o077 != 0 {
			t.Errorf("%s has mode %v, want no group or other access", filepath.Base(p), perm)
		}
	}
}

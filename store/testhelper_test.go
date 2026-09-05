// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package store_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/SukramJ/go-fabric/store"
)

// connectionPragmas are the modernc.org/sqlite `_pragma` query parameters
// applied to every connection the pool opens, not just the first.
// Connection-scoped pragmas must ride on the DSN because setting one via
// ExecContext primes a single pooled connection and leaves the rest on
// their defaults.
//
// foreign_keys is the load-bearing one: SQLite defaults it to OFF and
// resets it per connection, so ON DELETE CASCADE silently no-ops on any
// connection that never ran the pragma — which would let this package's
// cascade assertions pass while orphaned child rows survived.
const connectionPragmas = "_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"

// openTestDB opens a fresh file-backed SQLite database in t's temp
// directory with the matter_* schema already applied, and registers a
// cleanup to close it. Tests share the schema text, never the data.
//
// It applies the schema through the exported [store.Apply], so these tests
// stand on the same DDL a host gets rather than on a fixture beside it — the
// arrangement that let a test fixture and a deployment's tables drift.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "matter.db")
	db, err := sql.Open("sqlite", "file:"+path+"?"+connectionPragmas)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Apply(context.Background(), db); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	return db
}

// uncompressedP256Fixture returns a deterministic 65-byte uncompressed
// P-256 public key. The bytes do not need to be on the curve for the
// store layer (it stores raw blobs); on-curve validation belongs in
// the fabric package.
func uncompressedP256Fixture(seed byte) []byte {
	out := make([]byte, 65)
	out[0] = 0x04
	for i := 1; i < 65; i++ {
		out[i] = seed + byte(i)
	}
	return out
}

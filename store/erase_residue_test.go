// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package store_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/SukramJ/go-fabric/store"
)

// TestStoreEraseLeavesNoResidue: after Erase the erased secret's bytes are
// in none of the store's files — the database, its -wal and its -shm —
// neither while the handle is still open (a crash right after the reset)
// nor after Close. The secret is written twice: once as the row Erase
// removes, and once in rows a plain delete removed before the reset (as
// node.Node.FactoryReset's RemoveFabric does), whose pages sit on the
// free list. Both journal modes the module opens are covered.
func TestStoreEraseLeavesNoResidue(t *testing.T) {
	t.Parallel()
	const marker = "ERASE-RESIDUE-MARKER-6f1c2a9e-SECRET"
	for _, mode := range []struct {
		name    string
		pragmas []string
	}{
		{"rollback journal", nil},
		{"WAL", []string{walMode}},
	} {
		t.Run(mode.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			dir := t.TempDir()
			db, err := store.OpenSQLite(filepath.Join(dir, "matter.db"), mode.pragmas...)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = db.Close() }()
			if err := store.Apply(ctx, db); err != nil {
				t.Fatal(err)
			}
			s := store.New(db)
			// Rows deleted before the reset: enough to spill onto pages
			// of their own, which a plain DELETE hands to the free list.
			for i := range 300 {
				if err := s.SetSetting(ctx, fmt.Sprintf("earlier.%d", i), marker+"-earlier-row-padding-padding"); err != nil {
					t.Fatal(err)
				}
			}
			for i := range 300 {
				if err := s.DeleteSetting(ctx, fmt.Sprintf("earlier.%d", i)); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.SetSetting(ctx, "netcomm.wifi.networks", marker); err != nil {
				t.Fatal(err)
			}
			if err := s.Erase(ctx); err != nil {
				t.Fatal(err)
			}
			if hits := filesHolding(t, dir, marker); len(hits) > 0 {
				t.Errorf("after Erase, handle open: the secret is still in %v", hits)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			if hits := filesHolding(t, dir, marker); len(hits) > 0 {
				t.Errorf("after Erase and Close: the secret is still in %v", hits)
			}
		})
	}
}

// TestScrubLeavesNoResidue: Scrub on its own removes the bytes of rows a
// plain delete removed — what node.Node.FactoryReset relies on for the
// endpoint records of a file whose credential store is a host override.
func TestScrubLeavesNoResidue(t *testing.T) {
	t.Parallel()
	const marker = "SCRUB-RESIDUE-MARKER-0a9d47e2-SECRET"
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.OpenSQLite(filepath.Join(dir, "matter.db"), walMode)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err := store.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	s := store.New(db)
	for i := range 300 {
		if err := s.SetSetting(ctx, fmt.Sprintf("deleted.%d", i), marker+"-row-padding-padding"); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 300 {
		if err := s.DeleteSetting(ctx, fmt.Sprintf("deleted.%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if hits := filesHolding(t, dir, marker); len(hits) == 0 {
		t.Fatal("the deleted rows' bytes are in no file before Scrub: the test cannot see a residue")
	}
	if err := store.Scrub(ctx, db); err != nil {
		t.Fatal(err)
	}
	if hits := filesHolding(t, dir, marker); len(hits) > 0 {
		t.Errorf("after Scrub the deleted rows' bytes are still in %v", hits)
	}
}

// filesHolding names the files of dir whose bytes contain needle.
func filesHolding(t *testing.T, dir, needle string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var hits []string
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(dir, e.Name())) //nolint:gosec // the test's own temp directory
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(b, []byte(needle)) {
			hits = append(hits, e.Name())
		}
	}
	return hits
}

// TestStoreEraseWhoseCheckpointAReaderBlocks (review II, R2-P1-dr-rest-02):
// another connection holds a read transaction on the WAL database, so the
// scrub's checkpoint cannot truncate the -wal. Erase still empties the
// store — its deletes committed — and says only the scrub is missing
// (ErrScrubIncomplete); a Scrub once the reader is gone finishes it.
func TestStoreEraseWhoseCheckpointAReaderBlocks(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "matter.db")
	db, err := store.OpenSQLite(path, walMode)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	// One connection whose blocked checkpoint answers at once (the
	// opener's 5 s busy timeout would only make the test wait).
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, `PRAGMA busy_timeout = 0`); err != nil {
		t.Fatal(err)
	}
	if err := store.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	s := store.New(db)
	if err := s.SetSetting(ctx, "erased.secret", "value"); err != nil {
		t.Fatal(err)
	}
	reader, err := store.OpenSQLite(path, walMode)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	tx, err := reader.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	var rows int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM matter_settings`).Scan(&rows); err != nil {
		t.Fatal(err)
	}

	if err := s.Erase(ctx); !errors.Is(err, store.ErrScrubIncomplete) {
		t.Fatalf("Erase = %v while a reader held the -wal; want ErrScrubIncomplete", err)
	}
	if v, ok, err := s.GetSetting(ctx, "erased.secret"); err != nil || ok {
		t.Fatalf("setting after Erase: %q %v %v; want erased — the deletes committed", v, ok, err)
	}
	if err := store.Scrub(ctx, db); !errors.Is(err, store.ErrScrubIncomplete) {
		t.Fatalf("Scrub with the reader still there = %v; want ErrScrubIncomplete", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := store.Scrub(ctx, db); err != nil {
		t.Fatalf("Scrub after the reader left: %v", err)
	}
}

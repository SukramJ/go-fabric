// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package store_test

import (
	"bytes"
	"context"
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

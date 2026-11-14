// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/SukramJ/go-fabric/node"
	"github.com/SukramJ/go-fabric/store"
)

// TestFactoryResetWhoseScrubAReaderBlocksStillPins (review II,
// R2-P1-dr-rest-02): another connection holds a read transaction on the
// node's WAL database, so the scrub's checkpoint cannot truncate the -wal.
// The erase committed, so the reset still pins a fresh UniqueID; the
// error says the rows are gone and only the scrub is missing. Before, the
// scrub's error read as a failed erase, no pin was written, and the node
// came back serving the UniqueID it derived before the reset.
func TestFactoryResetWhoseScrubAReaderBlocksStillPins(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "node.db")
	db, err := store.OpenSQLite(path, "journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	// One connection whose blocked checkpoint answers at once (the
	// opener's 5 s busy timeout would only make the test wait).
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, `PRAGMA busy_timeout = 0`); err != nil {
		t.Fatal(err)
	}
	n, err := node.New(ctx, minimalConfig(db))
	if err != nil {
		t.Fatal(err)
	}
	resetLifecycle(t, n, true)
	before, ok, err := store.New(db).GetSetting(ctx, store.SettingUniqueID)
	if err != nil || !ok {
		t.Fatalf("UniqueID pin before the reset: %q %v %v", before, ok, err)
	}

	reader, err := store.OpenSQLite(path, "journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close() })
	tx, err := reader.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	var rows int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM matter_settings`).Scan(&rows); err != nil {
		t.Fatal(err)
	}

	resetErr := n.FactoryReset(ctx)
	if !errors.Is(resetErr, store.ErrScrubIncomplete) {
		t.Fatalf("FactoryReset = %v while a reader held the -wal; want store.ErrScrubIncomplete", resetErr)
	}
	after, ok, err := store.New(db).GetSetting(ctx, store.SettingUniqueID)
	if err != nil || !ok || after == before {
		t.Fatalf("UniqueID pin after the reset: %q %v %v (before %q); want a fresh one — the reset error was %v", after, ok, err, before, resetErr)
	}
	// Once the reader is gone, a later scrub of the file finishes.
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := store.Scrub(ctx, db); err != nil {
		t.Fatalf("Scrub after the reader left: %v", err)
	}
}

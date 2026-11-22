// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller

import (
	"context"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/controller/client/sqlstore"
	"thingfabric.dev/go-fabric/store"
)

// warnings records the messages of the warnings logged.
type warnings struct {
	mu   sync.Mutex
	msgs []string
}

func (w *warnings) Enabled(context.Context, slog.Level) bool { return true }
func (w *warnings) Handle(_ context.Context, r slog.Record) error {
	if r.Level >= slog.LevelWarn {
		w.mu.Lock()
		w.msgs = append(w.msgs, r.Message)
		w.mu.Unlock()
	}
	return nil
}
func (w *warnings) WithAttrs([]slog.Attr) slog.Handler { return w }
func (w *warnings) WithGroup(string) slog.Handler      { return w }

func (w *warnings) count(msg string) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := 0
	for _, m := range w.msgs {
		if m == msg {
			n++
		}
	}
	return n
}

// TestOpenStoresSaysAnOwedScrubOnce (review II, fx-csql F2, decision 132):
// a sealing whose scrub a reader blocks opens both stores and logs the
// owed scrub once; the next open, the reader gone, scrubs and says
// nothing. Before, the controller logged nothing of it.
func TestOpenStoresSaysAnOwedScrubOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "controller.db")
	db := openSealerDB(t, path)
	if err := store.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := sqlstore.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := sqlstore.New(db).Save("n", 0, onboardingCodeCluster, map[uint32][]byte{0: []byte(keptCode)}, nil); err != nil {
		t.Fatal(err)
	}
	// One connection whose blocked checkpoint answers at once, as
	// sqlstore's TestNewSealedScrubsAScrubItOwes.
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, `PRAGMA busy_timeout = 0`); err != nil {
		t.Fatal(err)
	}
	reader, err := store.OpenSQLite(path, "journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	tx, err := reader.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var rows int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM matter_client_values`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	cfg := Config{DB: db, Sealer: aesSealer(t, 7)}
	w := &warnings{}
	if _, _, err := openStores(ctx, cfg, slog.New(w)); err != nil {
		t.Fatalf("openStores with a scrub a reader blocks: %v", err)
	}
	if n := w.count("matter.store.seal_scrub_owed"); n != 1 {
		t.Fatalf("the owed scrub was logged %d times, want once", n)
	}
	_ = tx.Rollback()
	again := &warnings{}
	if _, _, err := openStores(ctx, cfg, slog.New(again)); err != nil {
		t.Fatal(err)
	}
	if n := again.count("matter.store.seal_scrub_owed"); n != 0 {
		t.Fatalf("the open after the scrub logged an owed scrub %d times", n)
	}
}

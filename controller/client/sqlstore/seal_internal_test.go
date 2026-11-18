// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package sqlstore

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/SukramJ/go-fabric/internal/sealseam"
	"github.com/SukramJ/go-fabric/store"
)

func internalDB(t *testing.T) (db *sql.DB, path string) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "client.db")
	db, err := store.OpenSQLite(path, "journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return db, path
}

func internalSealer(t *testing.T, b byte) store.Sealer {
	t.Helper()
	s, err := store.NewAESGCMSealer(bytes.Repeat([]byte{b}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// sealedRegistry is a marked registry under sealer 1 holding a sealed code.
func sealedRegistry(t *testing.T) (db *sql.DB, path string) {
	t.Helper()
	db, path = internalDB(t)
	s, err := NewSealed(t.Context(), db, internalSealer(t, 1))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save("n", 0, onboardingCluster, map[uint32][]byte{0: []byte("MT:CODE")}, nil); err != nil {
		t.Fatal(err)
	}
	return db, path
}

func exec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(t.Context(), q, args...); err != nil {
		t.Fatal(err)
	}
}

// TestSealCanaryHoldsItsOwnValue: a value sealed under the canary's own aad
// that is not the canary's value is store.ErrUnseal.
func TestSealCanaryHoldsItsOwnValue(t *testing.T) {
	t.Parallel()
	s := internalSealer(t, 1)
	other, err := s.Seal([]byte("another value"), canaryAAD())
	if err != nil {
		t.Fatal(err)
	}
	if err := openCanary(s, other); !errors.Is(err, store.ErrUnseal) {
		t.Errorf("a canary holding another value = %v, want store.ErrUnseal", err)
	}
}

// TestDropEmptySealMark: the controller's factory reset drops the registry's
// seal canary only once the registry holds no row; the seam carries the
// registry's hooks.
func TestDropEmptySealMark(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db, _ := sealedRegistry(t)
	if err := sealseam.RegistryDropEmptySealMark(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, present, _ := readCanary(ctx, db); !present {
		t.Error("the mark of a registry that still holds a row was dropped")
	}
	exec(t, db, `DELETE FROM matter_client_values`)
	if err := sealseam.RegistryDropEmptySealMark(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, present, _ := readCanary(ctx, db); !present {
		t.Error("the mark was dropped while a version row remained")
	}
	exec(t, db, `DELETE FROM matter_client_versions`)
	if err := sealseam.RegistryDropEmptySealMark(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, present, _ := readCanary(ctx, db); present {
		t.Error("the mark of an empty registry stayed")
	}

	// The reseal hooks run sqlstore's transaction.
	moved, _ := sealedRegistry(t)
	tx, err := moved.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := sealseam.RegistryResealTx(ctx, tx, internalSealer(t, 1), internalSealer(t, 2)); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := sealseam.RegistryDropScrubOwed(ctx, moved); err != nil {
		t.Fatal(err)
	}
	if s, err := NewSealed(ctx, moved, internalSealer(t, 2)); err != nil {
		t.Errorf("NewSealed under the new key after the seam's reseal: %v", err)
	} else if owed, err := s.ScrubOwed(ctx); err != nil || owed {
		t.Errorf("ScrubOwed after the seam dropped the mark = %v, %v", owed, err)
	}

	closed, _ := internalDB(t)
	_ = closed.Close()
	if err := dropEmptySealMark(ctx, closed); err == nil {
		t.Error("dropEmptySealMark on a closed handle: want an error")
	}
	if err := dropScrubOwed(ctx, closed); err == nil {
		t.Error("dropScrubOwed on a closed handle: want an error")
	}
	if _, _, err := readCanary(ctx, closed); err == nil {
		t.Error("readCanary on a closed handle: want an error")
	}
	if _, err := clearSecretValue(ctx, closed); err == nil {
		t.Error("clearSecretValue on a closed handle: want an error")
	}
	if _, _, err := (&Store{db: closed, sealer: internalSealer(t, 1)}).sealUnmarked(ctx); err == nil {
		t.Error("sealUnmarked on a closed handle: want an error")
	}
	if err := (&Store{db: closed, sealer: internalSealer(t, 1)}).openMarked(ctx); err == nil {
		t.Error("openMarked on a closed handle: want an error")
	}
}

// TestSealUnmarkedSeesAConcurrentMark: a NewSealed that found no canary and
// then finds one inside its transaction — another NewSealed marked the
// registry meanwhile — writes nothing and opens it as marked.
func TestSealUnmarkedSeesAConcurrentMark(t *testing.T) {
	t.Parallel()
	db, _ := sealedRegistry(t)
	marked, n, err := (&Store{db: db, sealer: internalSealer(t, 2)}).sealUnmarked(t.Context())
	if err != nil || !marked || n != 0 {
		t.Errorf("sealUnmarked on a marked registry = %v, %d, %v; want marked and nothing sealed", marked, n, err)
	}
}

// TestRefuseClearSecretWriteReadError: a store without a sealer whose check
// of the canary cannot read refuses the write.
func TestRefuseClearSecretWriteReadError(t *testing.T) {
	t.Parallel()
	db, _ := internalDB(t)
	exec(t, db, `DROP TABLE matter_client_metadata`)
	if err := New(db).Save("n", 0, onboardingCluster, map[uint32][]byte{0: []byte("MT:X")}, nil); err == nil {
		t.Error("a clear code whose canary check cannot read: want an error")
	}
}

// TestSealWritesTheDatabaseRefuses: each write of NewSealed's sealing and of
// Reseal's transaction that the database refuses fails the call and leaves
// the registry as it was — under the old key, without a new canary.
func TestSealWritesTheDatabaseRefuses(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	owedRow := `INSERT INTO matter_client_metadata (key, value) VALUES ('sqlstore.seal_scrub_owed', 1)`
	for _, c := range []struct {
		name    string
		setup   []string
		trigger string
	}{
		{"the write lock", []string{owedRow}, `CREATE TRIGGER refuse BEFORE UPDATE ON matter_client_metadata BEGIN SELECT RAISE(ABORT, 'refused'); END`},
		{"a resealed value", nil, `CREATE TRIGGER refuse BEFORE UPDATE ON matter_client_values BEGIN SELECT RAISE(ABORT, 'refused'); END`},
		{"the new canary", nil, `CREATE TRIGGER refuse BEFORE INSERT ON matter_client_metadata WHEN NEW.key = 'sqlstore.seal_canary' BEGIN SELECT RAISE(ABORT, 'refused'); END`},
		{"the owed mark", nil, `CREATE TRIGGER refuse BEFORE INSERT ON matter_client_metadata WHEN NEW.key = 'sqlstore.seal_scrub_owed' BEGIN SELECT RAISE(ABORT, 'refused'); END`},
	} {
		t.Run("Reseal: "+c.name, func(t *testing.T) {
			t.Parallel()
			db, _ := sealedRegistry(t)
			for _, q := range c.setup {
				exec(t, db, q)
			}
			exec(t, db, c.trigger)
			if err := Reseal(ctx, db, internalSealer(t, 1), internalSealer(t, 2)); err == nil {
				t.Fatal("Reseal succeeded")
			}
			if _, err := (&Store{db: db, sealer: internalSealer(t, 1)}).Load("n"); err != nil {
				t.Errorf("the registry under the old key after the refusal: %v", err)
			}
		})
		t.Run("NewSealed: "+c.name, func(t *testing.T) {
			t.Parallel()
			db, _ := internalDB(t)
			if err := New(db).Save("n", 0, onboardingCluster, map[uint32][]byte{0: []byte("MT:CLEAR")}, nil); err != nil {
				t.Fatal(err)
			}
			for _, q := range c.setup {
				exec(t, db, q)
			}
			exec(t, db, c.trigger)
			if _, err := NewSealed(ctx, db, internalSealer(t, 1)); err == nil {
				t.Fatal("NewSealed succeeded")
			}
			if _, present, _ := readCanary(ctx, db); present {
				t.Error("a refused sealing wrote the canary")
			}
		})
	}
}

// TestResealWithAScrubAReaderBlocks: a scrub a reader blocks after Reseal's
// commit is store.ErrScrubIncomplete; the values are under the new key and
// the scrub stays owed for the next NewSealed.
func TestResealWithAScrubAReaderBlocks(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db, path := sealedRegistry(t)
	db.SetMaxOpenConns(1)
	exec(t, db, `PRAGMA busy_timeout = 0`)
	reader, err := store.OpenSQLite(path, "journal_mode(WAL)")
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
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM matter_client_values`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if err := Reseal(ctx, db, internalSealer(t, 1), internalSealer(t, 2)); !errors.Is(err, store.ErrScrubIncomplete) {
		t.Fatalf("Reseal while a reader held the -wal = %v, want store.ErrScrubIncomplete", err)
	}
	s, err := NewSealed(ctx, db, internalSealer(t, 2))
	if err != nil {
		t.Fatal(err)
	}
	if owed, err := s.ScrubOwed(ctx); err != nil || !owed {
		t.Errorf("ScrubOwed with the reader there = %v, %v; want true", owed, err)
	}
}

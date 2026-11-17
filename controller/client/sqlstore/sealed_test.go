// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package sqlstore_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/SukramJ/go-fabric/controller/client/sqlstore"
	"github.com/SukramJ/go-fabric/store"
)

const (
	onboardingCluster uint32 = 0xFFFF_FFFC // controller/client onboarding.go
	icdCluster        uint32 = 0xFFFF_FFFD // controller/client icd.go
)

func sealerOf(t *testing.T, b byte) store.Sealer {
	t.Helper()
	s, err := store.NewAESGCMSealer(bytes.Repeat([]byte{b}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// value is the value Load returns for the node's endpoint 0 cluster's
// attribute.
func value(t *testing.T, s *sqlstore.Store, cluster, attr uint32) []byte {
	t.Helper()
	eps, err := s.Load("n")
	if err != nil {
		t.Fatal(err)
	}
	for _, ep := range eps {
		for _, c := range ep.Clusters {
			if ep.Number == 0 && c.ID == cluster {
				return c.Values[attr]
			}
		}
	}
	return nil
}

// TestSealedStoreSealsTheSecretValues: under NewSealed the onboarding code
// and the ICD keys are sealed in the file and read back, the other values
// of their clusters stay clear; a restart under the same key reads them; a
// store without the sealer reads a sealed value as store.ErrNoSealer and
// may not overwrite one; NewSealed under another key fails with
// store.ErrUnseal before writing; a sealed value moved to another row does
// not open.
func TestSealedStoreSealsTheSecretValues(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openTestDB(t)
	code, key, pending := []byte("MT:SECRETCODE0000"), bytes.Repeat([]byte{0xA1}, 16), bytes.Repeat([]byte{0xB2}, 16)
	s, err := sqlstore.NewSealed(ctx, db, sealerOf(t, 7))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save("n", 0, onboardingCluster, map[uint32][]byte{0: code, 1: {0x04, 0x01}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Save("n", 0, icdCluster, map[uint32][]byte{0: key, 1: pending, 2: {0x07}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Save("n", 0, 0x0006, map[uint32][]byte{0: {0x08}}, nil); err != nil { // an attribute value
		t.Fatal(err)
	}
	rows, err := db.QueryContext(ctx, `SELECT cluster, attribute, value, sealed FROM matter_client_values`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var cluster, attr uint32
		var v []byte
		var sealed bool
		if err := rows.Scan(&cluster, &attr, &v, &sealed); err != nil {
			t.Fatal(err)
		}
		secret := cluster == onboardingCluster && attr == 0 || cluster == icdCluster && attr <= 1
		if sealed != secret {
			t.Errorf("0x%X/%d: sealed %v, want %v", cluster, attr, sealed, secret)
		}
		for _, plain := range [][]byte{code, key, pending} {
			if bytes.Contains(v, plain) {
				t.Errorf("0x%X/%d holds a secret in clear", cluster, attr)
			}
		}
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if got := value(t, s, onboardingCluster, 0); !bytes.Equal(got, code) {
		t.Errorf("the code reads back as %q", got)
	}
	if got := value(t, s, 0x0006, 0); !bytes.Equal(got, []byte{0x08}) {
		t.Errorf("the attribute value reads back as %x", got)
	}

	again, err := sqlstore.NewSealed(ctx, db, sealerOf(t, 7))
	if err != nil {
		t.Fatalf("NewSealed under the same key: %v", err)
	}
	if got := value(t, again, icdCluster, 1); !bytes.Equal(got, pending) {
		t.Errorf("the pending key after a restart reads %x", got)
	}
	plain := sqlstore.New(db)
	if _, err := plain.Load("n"); !errors.Is(err, store.ErrNoSealer) {
		t.Errorf("Load without the sealer: %v, want store.ErrNoSealer", err)
	}
	if err := plain.Save("n", 0, onboardingCluster, map[uint32][]byte{0: []byte("MT:CLEAR")}, nil); !errors.Is(err, store.ErrNoSealer) {
		t.Errorf("a clear overwrite of a sealed value: %v, want store.ErrNoSealer", err)
	}
	if _, err := sqlstore.NewSealed(ctx, db, sealerOf(t, 8)); !errors.Is(err, store.ErrUnseal) {
		t.Errorf("NewSealed under another key: %v, want store.ErrUnseal", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE matter_client_values SET node_id = 'm' WHERE cluster = ? AND attribute = 0`, onboardingCluster); err != nil {
		t.Fatal(err)
	}
	if _, err := again.Load("m"); !errors.Is(err, store.ErrUnseal) {
		t.Errorf("a sealed value moved to another node: %v, want store.ErrUnseal", err)
	}
}

// TestNewSealedSealsAClearRegistry: a registry a store without a sealer
// wrote — v1 rows, then Apply — opens under NewSealed, which seals its
// clear secret values and scrubs the file, as store.NewSealed does for the
// device's store; under the sealer a secret value planted in clear is
// refused with store.ErrUnseal; v2's Down refuses a sealed row.
func TestNewSealedSealsAClearRegistry(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "client.db")
	db, err := store.OpenSQLite(path, "journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	v1 := sqlstore.Migrations()[0]
	if _, err := db.ExecContext(ctx, v1.Up); err != nil {
		t.Fatal(err)
	}
	code := []byte("MT:CLEARCODE00000")
	if _, err := db.ExecContext(ctx, `INSERT INTO matter_client_values(node_id, endpoint, cluster, attribute, value) VALUES('n', 0, ?, 0, ?)`, onboardingCluster, code); err != nil {
		t.Fatal(err)
	}
	if err := sqlstore.Apply(ctx, db); err != nil {
		t.Fatalf("Apply on a v1 database: %v", err)
	}
	s, err := sqlstore.NewSealed(ctx, db, sealerOf(t, 7))
	if err != nil {
		t.Fatal(err)
	}
	var sealed bool
	var raw []byte
	if err := db.QueryRowContext(ctx, `SELECT value, sealed FROM matter_client_values WHERE cluster = ?`, onboardingCluster).Scan(&raw, &sealed); err != nil {
		t.Fatal(err)
	}
	if !sealed || bytes.Contains(raw, code) {
		t.Errorf("the clear code was not sealed: sealed %v, %x", sealed, raw)
	}
	if got := value(t, s, onboardingCluster, 0); !bytes.Equal(got, code) {
		t.Errorf("the adopted code reads back as %q", got)
	}
	for _, f := range []string{path, path + "-wal"} {
		b, err := os.ReadFile(f)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		if bytes.Contains(b, code) {
			t.Errorf("%s still holds the clear code after NewSealed", filepath.Base(f))
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO matter_client_values(node_id, endpoint, cluster, attribute, value) VALUES('p', 0, ?, 0, ?)`, icdCluster, []byte{1, 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load("p"); !errors.Is(err, store.ErrUnseal) {
		t.Errorf("a clear secret value under the sealer: %v, want store.ErrUnseal", err)
	}
	if _, err := db.ExecContext(ctx, sqlstore.Migrations()[1].Down); err == nil {
		t.Error("v2's Down dropped the seal mark of a sealed row")
	}
}

// TestSealedValuesNameTheirSource holds the store's copies of the client
// package's ids of its secret values to their declarations.
func TestSealedValuesNameTheirSource(t *testing.T) {
	t.Parallel()
	for file, decls := range map[string][]string{
		"onboarding.go": {"onboardingCluster uint32 = 0xFFFF_FFFC", "onbCode     uint32 = 0"},
		"icd.go":        {"icdCluster uint32 = 0xFFFF_FFFD", "icdFieldKey              uint32 = 0", "icdFieldPendingKey       uint32 = 1"},
	} {
		b, err := os.ReadFile(filepath.Join("..", file))
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range decls {
			if !bytes.Contains(b, []byte(d)) {
				t.Errorf("controller/client/%s no longer declares %q: update sqlstore's secret values", file, d)
			}
		}
	}
}

// refusingSealer fails every Seal and Open.
type refusingSealer struct{}

var errRefused = errors.New("sealer refused")

func (refusingSealer) Seal([]byte, []byte) ([]byte, error) { return nil, errRefused }
func (refusingSealer) Open([]byte, []byte) ([]byte, error) { return nil, errRefused }

// switchSealer is a sealer whose Seal fails while refuse is set.
type switchSealer struct {
	store.Sealer
	refuse bool
}

func (s *switchSealer) Seal(plain, aad []byte) ([]byte, error) {
	if s.refuse {
		return nil, errRefused
	}
	return s.Sealer.Seal(plain, aad)
}

// TestSealedStoreErrors: NewSealed refuses a nil sealer and a database it
// cannot read; a Seal that fails leaves the clear values as they were on
// the way in and fails a Save of a secret value with nothing written;
// Upgrade leaves a database without the tables alone and reports one it
// cannot read.
func TestSealedStoreErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	if _, err := sqlstore.NewSealed(ctx, openTestDB(t), nil); err == nil {
		t.Error("NewSealed with a nil sealer")
	}

	db := openTestDB(t)
	if err := sqlstore.New(db).Save("n", 0, icdCluster, map[uint32][]byte{0: {1, 2}, 4: {3}}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlstore.NewSealed(ctx, db, refusingSealer{}); !errors.Is(err, errRefused) {
		t.Errorf("NewSealed whose Seal fails: %v, want the sealer's error", err)
	}
	if got := value(t, sqlstore.New(db), icdCluster, 0); !bytes.Equal(got, []byte{1, 2}) {
		t.Errorf("the clear key after a failed sealing: %x, want it unchanged", got)
	}

	s, err := sqlstore.NewSealed(ctx, openTestDB(t), sealerOf(t, 7))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save("n", 0, onboardingCluster, map[uint32][]byte{0: []byte("MT:X")}, nil); err != nil {
		t.Fatal(err)
	}
	// A Seal that fails once the store is open (NewSealed seals the canary).
	down := &switchSealer{Sealer: sealerOf(t, 7)}
	failing, err := sqlstore.NewSealed(ctx, openTestDB(t), down)
	if err != nil {
		t.Fatal(err)
	}
	down.refuse = true
	if err := failing.Save("n", 0, onboardingCluster, map[uint32][]byte{0: []byte("MT:X"), 1: {4}}, nil); !errors.Is(err, errRefused) {
		t.Errorf("Save whose Seal fails: %v, want the sealer's error", err)
	}
	if eps, err := failing.Load("n"); err != nil || len(eps) != 0 {
		t.Errorf("after the failed Save: %+v, %v; want nothing written", eps, err)
	}

	// Without the metadata table the sealing cannot record its scrub, and
	// is refused before anything is sealed.
	noMeta := openTestDB(t)
	if err := sqlstore.New(noMeta).Save("n", 0, onboardingCluster, map[uint32][]byte{0: []byte("MT:X")}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := noMeta.ExecContext(ctx, `DROP TABLE matter_client_metadata`); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlstore.NewSealed(ctx, noMeta, sealerOf(t, 7)); err == nil {
		t.Error("NewSealed without matter_client_metadata")
	}
	if got := value(t, sqlstore.New(noMeta), onboardingCluster, 0); string(got) != "MT:X" {
		t.Errorf("the code after the refused sealing: %q, want it unchanged", got)
	}

	// A write of the sealing the database refuses leaves the code clear and
	// owes no scrub.
	for _, c := range []struct{ name, trigger string }{
		{"the sealed value", `CREATE TRIGGER refuse BEFORE UPDATE ON matter_client_values BEGIN SELECT RAISE(ABORT, 'refused'); END`},
		{"the owed-scrub mark", `CREATE TRIGGER refuse BEFORE INSERT ON matter_client_metadata BEGIN SELECT RAISE(ABORT, 'refused'); END`},
	} {
		refusing := openTestDB(t)
		if err := sqlstore.New(refusing).Save("n", 0, onboardingCluster, map[uint32][]byte{0: []byte("MT:X")}, nil); err != nil {
			t.Fatal(err)
		}
		if _, err := refusing.ExecContext(ctx, c.trigger); err != nil {
			t.Fatal(err)
		}
		if _, err := sqlstore.NewSealed(ctx, refusing, sealerOf(t, 7)); err == nil {
			t.Errorf("%s refused: NewSealed succeeded", c.name)
		}
	}
	// A mark the database will not delete after the scrub keeps the scrub
	// owed for the next NewSealed, as an unfinished scrub does, and opens
	// the store (decision 132).
	kept := openTestDB(t)
	if err := sqlstore.New(kept).Save("n", 0, onboardingCluster, map[uint32][]byte{0: []byte("MT:X")}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := kept.ExecContext(ctx, `CREATE TRIGGER refuse BEFORE DELETE ON matter_client_metadata BEGIN SELECT RAISE(ABORT, 'refused'); END`); err != nil {
		t.Fatal(err)
	}
	if s, err := sqlstore.NewSealed(ctx, kept, sealerOf(t, 7)); err != nil {
		t.Errorf("the mark's deletion refused: NewSealed = %v, want the store", err)
	} else if owed, err := s.ScrubOwed(ctx); err != nil || !owed {
		t.Errorf("the mark's deletion refused: ScrubOwed = %v, %v; want true", owed, err)
	}

	if err := sqlstore.Upgrade(ctx, openEmptyDB(t)); err != nil {
		t.Errorf("Upgrade of a database without the tables: %v", err)
	}
	closed := openTestDB(t)
	_ = closed.Close()
	if err := sqlstore.Upgrade(ctx, closed); err == nil {
		t.Error("Upgrade on a closed database")
	}
	if _, err := sqlstore.NewSealed(ctx, closed, sealerOf(t, 7)); err == nil {
		t.Error("NewSealed on a closed database")
	}
	if _, err := sqlstore.New(closed).ScrubOwed(ctx); err == nil {
		t.Error("ScrubOwed on a closed database")
	}
}

// clearRegistry is a WAL database holding a clear onboarding code among
// filler rows, as a registry written without a sealer leaves it.
func clearRegistry(t *testing.T, code []byte) (db *sql.DB, path string) {
	t.Helper()
	ctx := context.Background()
	path = filepath.Join(t.TempDir(), "client.db")
	db, err := store.OpenSQLite(path, "journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := sqlstore.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	plain := sqlstore.New(db)
	if err := plain.Save("n", 0, onboardingCluster, map[uint32][]byte{0: code}, nil); err != nil {
		t.Fatal(err)
	}
	for i := range 50 {
		if err := plain.Save(fmt.Sprintf("filler-%02d", i), 1, 0x0006, map[uint32][]byte{0: bytes.Repeat([]byte{byte(i)}, 64)}, nil); err != nil {
			t.Fatal(err)
		}
	}
	return db, path
}

// fileHolds reports whether the database file or its -wal holds plain.
func fileHolds(t *testing.T, path string, plain []byte) bool {
	t.Helper()
	for _, f := range []string{path, path + "-wal"} {
		b, err := os.ReadFile(f)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		if bytes.Contains(b, plain) {
			return true
		}
	}
	return false
}

// TestNewSealedScrubsAScrubItOwes (R2-P6-store-2303, decision 132): the
// scrub a sealing owes is recorded with the sealing and run by every
// NewSealed until it finished, as store.NewSealed does for the device's
// store. A process that ends after the sealing committed and before the
// scrub leaves the clear code in the file; the next NewSealed scrubs it.
// A scrub a reader blocks does not fail NewSealed: the store opens sealed
// and strict, ScrubOwed says the scrub is owed, and the next NewSealed,
// the reader gone, scrubs.
func TestNewSealedScrubsAScrubItOwes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	code := []byte("MT:OWEDSCRUBCODE0")

	t.Run("a process that ended before the scrub", func(t *testing.T) {
		t.Parallel()
		db, path := clearRegistry(t, code)
		if n, err := sqlstore.SealWithoutScrub(ctx, db, sealerOf(t, 7)); err != nil || n != 1 {
			t.Fatalf("the sealing: %d, %v; want the code sealed", n, err)
		}
		if !fileHolds(t, path, code) {
			t.Fatal("the clear code is gone without a scrub: the test shows nothing")
		}
		if _, err := sqlstore.NewSealed(ctx, db, sealerOf(t, 7)); err != nil {
			t.Fatal(err)
		}
		if fileHolds(t, path, code) {
			t.Error("the next NewSealed left the clear code in the file: the owed scrub was not run")
		}
	})

	t.Run("v2's Down while the scrub is owed", func(t *testing.T) {
		t.Parallel()
		db, _ := clearRegistry(t, code)
		if n, err := sqlstore.SealWithoutScrub(ctx, db, sealerOf(t, 7)); err != nil || n != 1 {
			t.Fatalf("the sealing: %d, %v; want the code sealed", n, err)
		}
		// No sealed row is left, but the clear code the sealing replaced may
		// still be in the file: the owed scrub's record must survive.
		if _, err := db.ExecContext(ctx, `DELETE FROM matter_client_values`); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, sqlstore.Migrations()[1].Down); err == nil {
			t.Error("v2's Down dropped the record of a scrub still owed")
		}
	})
	t.Run("a checkpoint a reader blocks", func(t *testing.T) {
		t.Parallel()
		db, path := clearRegistry(t, code)
		// One connection whose blocked checkpoint answers at once, as
		// store's TestStoreEraseWhoseCheckpointAReaderBlocks.
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
		s, err := sqlstore.NewSealed(ctx, db, sealerOf(t, 7))
		if err != nil || s == nil {
			t.Fatalf("NewSealed while a reader held the -wal = %v, %v; want the sealed store and no error (decision 132)", s, err)
		}
		if owed, err := s.ScrubOwed(ctx); err != nil || !owed {
			t.Errorf("ScrubOwed with the reader there = %v, %v; want true", owed, err)
		}
		if got := value(t, s, onboardingCluster, 0); !bytes.Equal(got, code) {
			t.Errorf("the code under the owed scrub reads %q", got)
		}
		if err := sqlstore.New(db).Save("n", 0, onboardingCluster, map[uint32][]byte{0: []byte("MT:CLEAR")}, nil); !errors.Is(err, store.ErrNoSealer) {
			t.Errorf("a clear code written while the scrub is owed = %v, want store.ErrNoSealer", err)
		}
		_ = tx.Rollback()
		again, err := sqlstore.NewSealed(ctx, db, sealerOf(t, 7))
		if err != nil {
			t.Fatalf("NewSealed with the reader gone: %v", err)
		}
		if owed, err := again.ScrubOwed(ctx); err != nil || owed {
			t.Errorf("ScrubOwed after the next NewSealed = %v, %v; want false", owed, err)
		}
		if fileHolds(t, path, code) {
			t.Error("the clear code is still in the file after the second NewSealed: the owed scrub was not run")
		}
	})
}

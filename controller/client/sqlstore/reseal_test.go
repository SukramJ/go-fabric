// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package sqlstore_test

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"thingfabric.dev/go-fabric/controller/client/sqlstore"
	"thingfabric.dev/go-fabric/store"
)

// TestReseal (R2-P6-store-2302, decision 195): sqlstore.Reseal moves every
// sealed value of the registry and its seal canary from the old sealer to
// the new one in one transaction and scrubs: the new key opens the
// registry and reads every secret, the old one no longer does, and no
// value sealed under the old key is left in the files. A nil sealer, a
// wrong old key and a clear secret value are refused and change nothing.
func TestReseal(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "client.db")
	db, err := store.OpenSQLite(path, "journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := sqlstore.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	old, next := sealerOf(t, 1), sealerOf(t, 2)
	s, err := sqlstore.NewSealed(ctx, db, old)
	if err != nil {
		t.Fatal(err)
	}
	code, key := []byte("MT:RESEALCODE0000"), bytes.Repeat([]byte{0xC3}, 16)
	if err := s.Save("n", 0, onboardingCluster, map[uint32][]byte{0: code}, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Save("n", 0, icdCluster, map[uint32][]byte{0: key, 2: {7}}, nil); err != nil {
		t.Fatal(err)
	}
	var oldCode []byte
	if err := db.QueryRowContext(ctx, `SELECT value FROM matter_client_values WHERE cluster = ? AND attribute = 0`, onboardingCluster).Scan(&oldCode); err != nil {
		t.Fatal(err)
	}
	_, oldCanary := registryCanary(t, db)

	for name, pair := range map[string][2]store.Sealer{"nil old": {nil, next}, "nil next": {old, nil}} {
		if err := sqlstore.Reseal(ctx, db, pair[0], pair[1]); err == nil {
			t.Errorf("Reseal with a %s sealer succeeded", name)
		}
	}
	if err := sqlstore.Reseal(ctx, db, sealerOf(t, 3), next); !errors.Is(err, store.ErrUnseal) {
		t.Errorf("Reseal under a wrong old key = %v, want store.ErrUnseal", err)
	}
	if _, canary := registryCanary(t, db); !bytes.Equal(canary, oldCanary) {
		t.Error("a refused Reseal changed the seal canary")
	}

	if err := sqlstore.Reseal(ctx, db, old, next); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlstore.NewSealed(ctx, db, old); !errors.Is(err, store.ErrUnseal) {
		t.Errorf("NewSealed under the old key after Reseal = %v, want store.ErrUnseal", err)
	}
	s2, err := sqlstore.NewSealed(ctx, db, next)
	if err != nil {
		t.Fatal(err)
	}
	if got := value(t, s2, onboardingCluster, 0); !bytes.Equal(got, code) {
		t.Errorf("the code after Reseal reads %q", got)
	}
	if got := value(t, s2, icdCluster, 0); !bytes.Equal(got, key) {
		t.Errorf("the ICD key after Reseal reads %x", got)
	}
	if owed, err := s2.ScrubOwed(ctx); err != nil || owed {
		t.Errorf("ScrubOwed after Reseal = %v, %v; want false", owed, err)
	}
	for name, v := range map[string][]byte{"the code sealed under the old key": oldCode, "the old seal canary": oldCanary, "the clear code": code, "the clear key": key} {
		if fileHolds(t, path, v) {
			t.Errorf("%s is still in the file after Reseal", name)
		}
	}

	// A clear secret value: refused, nothing moved.
	if _, err := db.ExecContext(ctx, `INSERT INTO matter_client_values(node_id, endpoint, cluster, attribute, value, sealed) VALUES('p', 0, ?, 1, x'0101', 0)`, icdCluster); err != nil {
		t.Fatal(err)
	}
	if err := sqlstore.Reseal(ctx, db, next, old); !errors.Is(err, store.ErrUnseal) {
		t.Errorf("Reseal over a clear secret value = %v, want store.ErrUnseal", err)
	}
	if got := value(t, s2, onboardingCluster, 0); !bytes.Equal(got, code) {
		t.Errorf("the code after a refused Reseal reads %q under the current key", got)
	}
}

// TestResealRefusals: Reseal refuses a registry before schema v3, fails
// with a new sealer that cannot seal and leaves the registry as it was,
// reseals a sealed registry that carries no canary (the release before v3
// sealed it) and marks it, and when the owed mark cannot be deleted after
// the scrub says store.ErrScrubIncomplete with the values under the new
// key and ScrubOwed still true.
func TestResealRefusals(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	v2 := openEmptyDB(t)
	for _, m := range sqlstore.Migrations()[:2] {
		if _, err := v2.ExecContext(ctx, m.Up); err != nil {
			t.Fatal(err)
		}
	}
	if err := sqlstore.Reseal(ctx, v2, sealerOf(t, 1), sealerOf(t, 2)); err == nil || !strings.Contains(err.Error(), "v3") {
		t.Errorf("Reseal on a v2 registry = %v; want the schema step named", err)
	}

	// A registry the release before v3 sealed, then Apply.
	unmarked := v2SealedRegistry(t, 1, []byte("MT:X"))
	if err := sqlstore.Apply(ctx, unmarked); err != nil {
		t.Fatal(err)
	}
	var sealedBefore []byte
	if err := unmarked.QueryRowContext(ctx, `SELECT value FROM matter_client_values WHERE node_id = 'n'`).Scan(&sealedBefore); err != nil {
		t.Fatal(err)
	}
	down := &switchSealer{Sealer: sealerOf(t, 2), refuse: true}
	if err := sqlstore.Reseal(ctx, unmarked, sealerOf(t, 1), down); !errors.Is(err, errRefused) {
		t.Errorf("Reseal with a new sealer that cannot seal = %v, want its error", err)
	}
	if present, _ := registryCanary(t, unmarked); present {
		t.Error("a failed Reseal wrote the canary")
	}
	var sealedAfter []byte
	if err := unmarked.QueryRowContext(ctx, `SELECT value FROM matter_client_values WHERE node_id = 'n'`).Scan(&sealedAfter); err != nil || !bytes.Equal(sealedAfter, sealedBefore) {
		t.Errorf("the sealed code after a failed Reseal changed (%v)", err)
	}
	if err := sqlstore.Reseal(ctx, unmarked, sealerOf(t, 1), sealerOf(t, 2)); err != nil {
		t.Fatal(err)
	}
	if present, _ := registryCanary(t, unmarked); !present {
		t.Error("Reseal of a registry without the canary did not mark it")
	}
	if _, err := sqlstore.NewSealed(ctx, unmarked, sealerOf(t, 1)); !errors.Is(err, store.ErrUnseal) {
		t.Errorf("NewSealed under the old key after Reseal = %v, want store.ErrUnseal", err)
	}
	if moved, err := sqlstore.NewSealed(ctx, unmarked, sealerOf(t, 2)); err != nil {
		t.Errorf("NewSealed under the new key after Reseal: %v", err)
	} else if got := value(t, moved, onboardingCluster, 0); string(got) != "MT:X" {
		t.Errorf("the code after Reseal reads %q", got)
	}

	v1 := openEmptyDB(t)
	if _, err := v1.ExecContext(ctx, sqlstore.Migrations()[0].Up); err != nil {
		t.Fatal(err)
	}
	if err := sqlstore.Reseal(ctx, v1, sealerOf(t, 1), sealerOf(t, 2)); err == nil || !strings.Contains(err.Error(), "v3") {
		t.Errorf("Reseal on a v1 registry = %v; want the schema step named", err)
	}

	kept := openTestDB(t)
	k, err := sqlstore.NewSealed(ctx, kept, sealerOf(t, 1))
	if err != nil {
		t.Fatal(err)
	}
	if err := k.Save("n", 0, onboardingCluster, map[uint32][]byte{0: []byte("MT:K")}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := kept.ExecContext(ctx, `CREATE TRIGGER keep_owed BEFORE DELETE ON matter_client_metadata
WHEN OLD.key = 'sqlstore.seal_scrub_owed' BEGIN SELECT RAISE(ABORT, 'kept'); END`); err != nil {
		t.Fatal(err)
	}
	if err := sqlstore.Reseal(ctx, kept, sealerOf(t, 1), sealerOf(t, 2)); !errors.Is(err, store.ErrScrubIncomplete) {
		t.Fatalf("Reseal whose owed mark stays = %v, want store.ErrScrubIncomplete", err)
	}
	n, err := sqlstore.NewSealed(ctx, kept, sealerOf(t, 2))
	if err != nil {
		t.Fatal(err)
	}
	if owed, err := n.ScrubOwed(ctx); err != nil || !owed {
		t.Errorf("ScrubOwed after the mark stayed = %v, %v; want true", owed, err)
	}
	if got := value(t, n, onboardingCluster, 0); string(got) != "MT:K" {
		t.Errorf("the code under the new key reads %q", got)
	}

	closed := openTestDB(t)
	_ = closed.Close()
	if err := sqlstore.Reseal(ctx, closed, sealerOf(t, 1), sealerOf(t, 2)); err == nil {
		t.Error("Reseal on a closed handle: want an error")
	}
}

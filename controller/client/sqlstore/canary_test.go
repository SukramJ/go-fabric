// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package sqlstore_test

import (
	"bytes"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/controller/client/sqlstore"
	"github.com/SukramJ/go-fabric/store"
)

// registryCanary reads the registry's seal canary: whether its row is
// there, and its sealed value.
func registryCanary(t *testing.T, db *sql.DB) (present bool, data []byte) {
	t.Helper()
	err := db.QueryRowContext(t.Context(), `SELECT data FROM matter_client_metadata WHERE key = ?`, sqlstore.MetadataSealCanary).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return true, data
}

// TestSealCanaryMarksTheRegistry (R2-P6-store-2304, N6a-seal-1, decision
// 194): the first NewSealed marks the registry with its own seal canary —
// a known value sealed under the sealer in a reserved row of
// matter_client_metadata — even when it holds nothing to seal. Then a
// sealer under another key fails NewSealed with store.ErrUnseal although
// the registry holds no sealed value; a store without the sealer refuses
// to write a secret value into it (store.ErrNoSealer) and writes nothing,
// while it still writes what is not secret; and a clear secret value
// planted beside the mark is refused, not adopted, and stays as it was.
func TestSealCanaryMarksTheRegistry(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db := openTestDB(t)
	if _, err := sqlstore.NewSealed(ctx, db, sealerOf(t, 1)); err != nil {
		t.Fatal(err)
	}
	present, data := registryCanary(t, db)
	if !present || len(data) < 28 {
		t.Fatalf("the seal canary after the first NewSealed: present %v, %d bytes; want a sealed value", present, len(data))
	}
	if _, err := sqlstore.NewSealed(ctx, db, sealerOf(t, 2)); !errors.Is(err, store.ErrUnseal) {
		t.Errorf("NewSealed under another key on a marked registry without a sealed value = %v, want store.ErrUnseal", err)
	}
	if _, err := sqlstore.NewSealed(ctx, db, sealerOf(t, 1)); err != nil {
		t.Errorf("NewSealed under the key: %v", err)
	}

	plain := sqlstore.New(db)
	if err := plain.Save("n", 0, onboardingCluster, map[uint32][]byte{0: []byte("MT:CLEARCODE"), 1: {4}}, nil); !errors.Is(err, store.ErrNoSealer) {
		t.Errorf("a store without the sealer writes a code into a marked registry = %v, want store.ErrNoSealer", err)
	}
	if err := plain.Save("n", 0, icdCluster, map[uint32][]byte{1: {1, 2}}, nil); !errors.Is(err, store.ErrNoSealer) {
		t.Errorf("a store without the sealer writes a pending ICD key into a marked registry = %v, want store.ErrNoSealer", err)
	}
	var rows int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM matter_client_values`).Scan(&rows); err != nil || rows != 0 {
		t.Errorf("rows after the refused writes = %d, %v; want none", rows, err)
	}
	if err := plain.Save("n", 0, 0x0006, map[uint32][]byte{0: {1}}, nil); err != nil {
		t.Errorf("a value that is not secret, into a marked registry: %v", err)
	}

	if _, err := db.ExecContext(ctx, `INSERT INTO matter_client_values(node_id, endpoint, cluster, attribute, value, sealed) VALUES('p', 0, ?, 0, ?, 0)`, icdCluster, []byte{9, 9}); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlstore.NewSealed(ctx, db, sealerOf(t, 1)); !errors.Is(err, store.ErrUnseal) {
		t.Errorf("NewSealed over a clear secret value in a marked registry = %v, want store.ErrUnseal", err)
	}
	var v []byte
	var sealed bool
	if err := db.QueryRowContext(ctx, `SELECT value, sealed FROM matter_client_values WHERE node_id = 'p'`).Scan(&v, &sealed); err != nil || sealed || string(v) != "\x09\x09" {
		t.Errorf("the planted value after the refusal = %x sealed %v, %v; want it unchanged", v, sealed, err)
	}
}

// TestSealCanaryRefusals: a canary whose sealed value was removed, and one
// that holds another sealed value of the registry (it opens only with its
// own aad), fail NewSealed with store.ErrUnseal; NewSealed on a registry
// without schema v3 names the step; a NewSealed whose Seal fails writes no
// canary.
func TestSealCanaryRefusals(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	db := openTestDB(t)
	if _, err := sqlstore.NewSealed(ctx, db, sealerOf(t, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE matter_client_metadata SET data = NULL WHERE key = ?`, sqlstore.MetadataSealCanary); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlstore.NewSealed(ctx, db, sealerOf(t, 1)); !errors.Is(err, store.ErrUnseal) {
		t.Errorf("NewSealed over a canary without its sealed value = %v, want store.ErrUnseal", err)
	}

	moved := openTestDB(t)
	s, err := sqlstore.NewSealed(ctx, moved, sealerOf(t, 1))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save("n", 0, onboardingCluster, map[uint32][]byte{0: []byte("MT:X")}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := moved.ExecContext(ctx, `UPDATE matter_client_metadata SET data = (SELECT value FROM matter_client_values WHERE node_id = 'n') WHERE key = ?`, sqlstore.MetadataSealCanary); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlstore.NewSealed(ctx, moved, sealerOf(t, 1)); !errors.Is(err, store.ErrUnseal) {
		t.Errorf("NewSealed over a canary holding another sealed value = %v, want store.ErrUnseal", err)
	}

	v2 := openEmptyDB(t)
	for _, m := range sqlstore.Migrations()[:2] {
		if _, err := v2.ExecContext(ctx, m.Up); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := sqlstore.NewSealed(ctx, v2, sealerOf(t, 1)); err == nil || !strings.Contains(err.Error(), "v3") {
		t.Errorf("NewSealed on a v2 registry = %v; want the schema step named", err)
	}

	refused := openTestDB(t)
	if _, err := sqlstore.NewSealed(ctx, refused, refusingSealer{}); !errors.Is(err, errRefused) {
		t.Errorf("NewSealed whose Seal fails, on an empty registry = %v, want the sealer's error", err)
	}
	if present, _ := registryCanary(t, refused); present {
		t.Error("a NewSealed that failed wrote the canary")
	}
}

// TestRegistryUpgradeFromV2 (decision 157): a registry the release before
// schema v3 sealed — a sealed code, no canary — opens after Apply under
// its key, which NewSealed checks on the sealed value and then marks the
// registry with; under another key it fails with store.ErrUnseal before
// anything is written. A clear code in a v2 registry is sealed on the way
// in. v3's Down returns a marked registry to v2's schema.
func TestRegistryUpgradeFromV2(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	code := []byte("MT:UPGRADECODE000")

	// A v2 registry as the release before v3 sealed it: the rows of a
	// sealed store, then v3's Down, which takes the canary with it.
	db := openTestDB(t)
	s, err := sqlstore.NewSealed(ctx, db, sealerOf(t, 1))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save("n", 0, onboardingCluster, map[uint32][]byte{0: code}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, sqlstore.Migrations()[2].Down); err != nil {
		t.Fatalf("v3 down on a marked registry: %v", err)
	}
	byList := openEmptyDB(t)
	for _, m := range sqlstore.Migrations()[:2] {
		if _, err := byList.ExecContext(ctx, m.Up); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := sqlstore.SchemaFingerprint(ctx, db)
	want, _ := sqlstore.SchemaFingerprint(ctx, byList)
	if got != want {
		t.Fatal("v3 down did not leave v2's schema")
	}

	if err := sqlstore.Apply(ctx, db); err != nil {
		t.Fatalf("Apply on a v2 registry: %v", err)
	}
	if present, _ := registryCanary(t, db); present {
		t.Fatal("the upgrade wrote a canary")
	}
	if _, err := sqlstore.NewSealed(ctx, db, sealerOf(t, 2)); !errors.Is(err, store.ErrUnseal) {
		t.Errorf("NewSealed under another key on an upgraded registry = %v, want store.ErrUnseal", err)
	}
	if present, _ := registryCanary(t, db); present {
		t.Error("a NewSealed under another key marked the registry")
	}
	up, err := sqlstore.NewSealed(ctx, db, sealerOf(t, 1))
	if err != nil {
		t.Fatalf("NewSealed under the key on an upgraded registry: %v", err)
	}
	if got := value(t, up, onboardingCluster, 0); !bytes.Equal(got, code) {
		t.Errorf("the code after the upgrade reads %q", got)
	}
	if present, _ := registryCanary(t, db); !present {
		t.Error("NewSealed did not mark the upgraded registry")
	}

	clearV2 := openEmptyDB(t)
	for _, m := range sqlstore.Migrations()[:2] {
		if _, err := clearV2.ExecContext(ctx, m.Up); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := clearV2.ExecContext(ctx, `INSERT INTO matter_client_values(node_id, endpoint, cluster, attribute, value) VALUES('n', 0, ?, 1, ?)`, icdCluster, []byte{7, 7}); err != nil {
		t.Fatal(err)
	}
	if err := sqlstore.Apply(ctx, clearV2); err != nil {
		t.Fatal(err)
	}
	sealed, err := sqlstore.NewSealed(ctx, clearV2, sealerOf(t, 1))
	if err != nil {
		t.Fatalf("NewSealed on a clear v2 registry: %v", err)
	}
	if got := value(t, sealed, icdCluster, 1); string(got) != "\x07\x07" {
		t.Errorf("the pending key after the sealing reads %x", got)
	}
}

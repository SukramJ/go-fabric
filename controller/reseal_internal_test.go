// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/SukramJ/go-fabric/controller/client/sqlstore"
	"github.com/SukramJ/go-fabric/store"
)

// TestEraseDropsTheRegistrysSealMark (R2-P6-store-2304, decision 194): the
// controller's factory reset is the way back to an unsealed registry, as
// store.Store.Erase is for the store — after Erase neither store carries a
// seal mark, so a controller under another key opens the database and one
// without a sealer writes a kept onboarding code into the registry again.
func TestEraseDropsTheRegistrysSealMark(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db := openSealerDB(t, filepath.Join(t.TempDir(), "controller.db"))
	c, err := New(ctx, sealerConfig(db, aesSealer(t, 7)))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.nodeStore.Save("n", 0, onboardingCodeCluster, map[uint32][]byte{0: []byte(keptCode)}, nil); err != nil {
		t.Fatal(err)
	}
	if err := c.Erase(ctx); err != nil {
		t.Fatal(err)
	}
	if err := sqlstore.New(db).Save("m", 0, onboardingCodeCluster, map[uint32][]byte{0: []byte(keptCode)}, nil); err != nil {
		t.Errorf("a kept code without a sealer after the factory reset: %v", err)
	}
	if err := sqlstore.New(db).EraseNode("m"); err != nil {
		t.Fatal(err)
	}
	other, err := New(ctx, sealerConfig(db, aesSealer(t, 8)))
	if err != nil {
		t.Fatalf("New under another key after the factory reset: %v", err)
	}
	if err := other.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlstore.NewSealed(ctx, db, aesSealer(t, 7)); !errors.Is(err, store.ErrUnseal) {
		t.Errorf("the registry under the erased key after the new controller marked it = %v, want store.ErrUnseal", err)
	}
}

// TestNewOpensWithAnOwedRegistryScrub (R2-P6-store-2303, decision 132):
// a registry whose owed scrub cannot be closed out — here its mark will not
// go — no longer fails New: the controller opens sealed, the registry
// reports the scrub owed, and the kept code reads back.
func TestNewOpensWithAnOwedRegistryScrub(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db := openSealerDB(t, filepath.Join(t.TempDir(), "controller.db"))
	c, err := New(ctx, sealerConfig(db, nil))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.nodeStore.Save("n", 0, onboardingCodeCluster, map[uint32][]byte{0: []byte(keptCode)}, nil); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TRIGGER keep_owed BEFORE DELETE ON matter_client_metadata
WHEN OLD.key = 'sqlstore.seal_scrub_owed' BEGIN SELECT RAISE(ABORT, 'kept'); END`); err != nil {
		t.Fatal(err)
	}
	sealed, err := New(ctx, sealerConfig(db, aesSealer(t, 7)))
	if err != nil {
		t.Fatalf("New over a registry whose owed scrub stays marked: %v", err)
	}
	defer func() { _ = sealed.Close(ctx) }()
	if owed, err := sealed.nodeStore.ScrubOwed(ctx); err != nil || !owed {
		t.Errorf("the registry's ScrubOwed = %v, %v; want true", owed, err)
	}
	eps, err := sealed.nodeStore.Load("n")
	if err != nil || len(eps) != 1 || string(eps[0].Clusters[0].Values[0]) != keptCode {
		t.Errorf("the kept code under the owed scrub: %+v, %v", eps, err)
	}
}

// TestResealMovesTheControllersDatabase (R2-P6-store-2302, decision 195):
// controller.Reseal moves both stores of a controller's database — the CA,
// the fabric and the resumption records of store, the kept codes of the
// node registry — to the new sealer in one transaction and scrubs once:
// New under the new key opens it and reads everything back, New under the
// old key fails with store.ErrUnseal, and no value sealed under the old
// key is left in the files. A registry value that does not open under the
// old key fails Reseal and rolls back store's half too, so New under the
// old key still opens.
func TestResealMovesTheControllersDatabase(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "controller.db")
	db := openSealerDB(t, path)
	old, next := aesSealer(t, 1), aesSealer(t, 2)
	c, err := New(ctx, sealerConfig(db, old))
	if err != nil {
		t.Fatal(err)
	}
	fabricID := c.Fabric().FabricID
	if err := c.nodeStore.Save("n", 0, onboardingCodeCluster, map[uint32][]byte{0: []byte(keptCode)}, nil); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(ctx); err != nil {
		t.Fatal(err)
	}
	var oldCA string
	if err := db.QueryRowContext(ctx, `SELECT value FROM matter_settings WHERE key = 'controller.ca'`).Scan(&oldCA); err != nil {
		t.Fatal(err)
	}
	var oldCode []byte
	if err := db.QueryRowContext(ctx, `SELECT value FROM matter_client_values WHERE node_id = 'n'`).Scan(&oldCode); err != nil {
		t.Fatal(err)
	}

	for name, pair := range map[string][2]store.Sealer{"nil old": {nil, next}, "nil next": {old, nil}} {
		if err := Reseal(ctx, db, pair[0], pair[1]); err == nil {
			t.Errorf("Reseal with a %s sealer succeeded", name)
		}
	}
	// One transaction: a registry value that does not open under the old
	// key fails Reseal after store's half ran, and store's half rolls back.
	if _, err := db.ExecContext(ctx, `UPDATE matter_client_values SET node_id = 'moved' WHERE node_id = 'n'`); err != nil {
		t.Fatal(err)
	}
	if err := Reseal(ctx, db, old, next); !errors.Is(err, store.ErrUnseal) {
		t.Errorf("Reseal over a registry value that does not open = %v, want store.ErrUnseal", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE matter_client_values SET node_id = 'n' WHERE node_id = 'moved'`); err != nil {
		t.Fatal(err)
	}
	if again, err := New(ctx, sealerConfig(db, old)); err != nil {
		t.Fatalf("New under the old key after a failed Reseal: %v", err)
	} else if err := again.Close(ctx); err != nil {
		t.Fatal(err)
	}

	if err := Reseal(ctx, db, old, next); err != nil {
		t.Fatal(err)
	}
	if stale, err := New(ctx, sealerConfig(db, old)); !errors.Is(err, store.ErrUnseal) {
		if stale != nil {
			_ = stale.Close(ctx)
		}
		t.Errorf("New under the old key after Reseal = %v, want store.ErrUnseal", err)
	}
	moved, err := New(ctx, sealerConfig(db, next))
	if err != nil {
		t.Fatalf("New under the new key after Reseal: %v", err)
	}
	defer func() { _ = moved.Close(ctx) }()
	if f := moved.Fabric(); f.FabricID != fabricID {
		t.Errorf("the fabric after Reseal: %x, want %x", f.FabricID, fabricID)
	}
	eps, err := moved.nodeStore.Load("n")
	if err != nil || len(eps) != 1 || string(eps[0].Clusters[0].Values[0]) != keptCode {
		t.Errorf("the kept code after Reseal: %+v, %v", eps, err)
	}
	for _, owed := range []func() (bool, error){
		func() (bool, error) { return moved.st.ScrubOwed(ctx) },
		func() (bool, error) { return moved.nodeStore.ScrubOwed(ctx) },
	} {
		if o, err := owed(); err != nil || o {
			t.Errorf("a scrub owed after Reseal: %v, %v", o, err)
		}
	}
	noClearSecret(t, path, "after Reseal", []byte(oldCA), oldCode, []byte(keptCode))
}

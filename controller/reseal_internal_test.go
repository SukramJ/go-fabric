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

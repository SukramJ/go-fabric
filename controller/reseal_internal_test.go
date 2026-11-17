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

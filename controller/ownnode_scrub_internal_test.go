// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/SukramJ/go-fabric/controller/client/sqlstore"
	"github.com/SukramJ/go-fabric/store"
)

// TestRetiredOwnNodeLeavesNoClearBytes (review II, R2-P6-store-2316): the
// own node's record of an earlier release — its passcode and salt in clear
// — is retired on a sealed controller's start, and its bytes do not stay
// in the database file. Before, the delete ran after the sealing's scrub
// and left them in free pages.
func TestRetiredOwnNodeLeavesNoClearBytes(t *testing.T) {
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
	const record = `{"nodeId":0,"passcode":"OWNPASS-20202021","salt":"OWNSALT-5F3A9C"}`
	if err := store.New(db).SetSetting(ctx, ownNodeKey, record); err != nil {
		t.Fatal(err)
	}
	c, err := New(ctx, sealerConfig(db, aesSealer(t, 7)))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.New(db).GetSetting(ctx, ownNodeKey); err != nil || ok {
		t.Fatalf("the own node's record after the start: ok %v, %v; want retired", ok, err)
	}
	noClearSecret(t, path, "the retired own node", []byte("OWNPASS-20202021"), []byte("OWNSALT-5F3A9C"))
}

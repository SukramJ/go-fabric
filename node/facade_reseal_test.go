// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net/url"
	"path/filepath"
	"testing"

	"thingfabric.dev/go-fabric/node"
)

// TestFacadeResealRotatesTheKey (#584, decision 561): a host that imports
// the facade only — node and the standard library, no store — seals a node
// store, rotates the key with node.Reseal, and tells a wrong key
// (node.ErrUnseal) from a missing sealer (node.ErrNoSealer). The database
// handle Reseal takes is opened with database/sql under the "sqlite"
// driver the node itself registers.
func TestFacadeResealRotatesTheKey(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "node.db")
	first, err := node.NewAESGCMSealer(bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	next, err := node.NewAESGCMSealer(bytes.Repeat([]byte{2}, 32))
	if err != nil {
		t.Fatal(err)
	}
	// The node opens Path itself; seal fills its Sealer, nil runs it
	// without one.
	start := func(seal func(*node.Store)) (*node.Node, error) {
		cfg := minimalConfig(nil)
		cfg.Store = node.Store{Path: path}
		if seal != nil {
			seal(&cfg.Store)
		}
		return node.New(ctx, cfg)
	}
	underFirst := func(s *node.Store) { s.Sealer = first }
	underNext := func(s *node.Store) { s.Sealer = next }
	const secret = "netcomm.wifi.networks" // a sealed setting (store_sealer_test.go)
	n, err := start(underFirst)
	if err != nil {
		t.Fatalf("New under the first key: %v", err)
	}
	if err := n.Store().SetSetting(ctx, secret, "[]"); err != nil {
		t.Fatal(err)
	}
	if err := n.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}

	dsn := url.URL{Scheme: "file", OmitHost: true, Path: path, RawQuery: url.Values{"_pragma": {"foreign_keys(1)"}}.Encode()}
	db, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		t.Fatal(err)
	}
	if err := node.Reseal(ctx, db, next, first); !errors.Is(err, node.ErrUnseal) {
		t.Errorf("Reseal from a wrong old key: %v, want node.ErrUnseal", err)
	}
	if err := node.Reseal(ctx, db, first, next); err != nil {
		t.Fatalf("Reseal: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	if m, err := start(underFirst); !errors.Is(err, node.ErrUnseal) {
		if m != nil {
			_ = m.Stop(context.Background())
		}
		t.Errorf("New under the old key after the rotation: %v, want node.ErrUnseal", err)
	}
	n, err = start(underNext)
	if err != nil {
		t.Fatalf("New under the new key: %v", err)
	}
	if v, ok, err := n.Store().GetSetting(ctx, secret); err != nil || !ok || v != "[]" {
		t.Errorf("the secret under the new key: %q %v %v", v, ok, err)
	}
	if err := n.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	n, err = start(nil)
	if err != nil {
		t.Fatalf("New without a sealer: %v", err)
	}
	defer func() { _ = n.Stop(context.Background()) }()
	if _, _, err := n.Store().GetSetting(ctx, secret); !errors.Is(err, node.ErrNoSealer) {
		t.Errorf("the secret without a sealer: %v, want node.ErrNoSealer", err)
	}
}

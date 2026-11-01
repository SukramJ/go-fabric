// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/SukramJ/go-fabric/node"
)

// Store.Path: the node opens the file, Stop closes it, and the next node on
// the same file reads what the first one stored.
func TestStorePathOpensAndCloses(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "path.db")
	cfg := minimalConfig(nil)
	cfg.Store = node.Store{Path: path}
	n, err := node.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := n.Store().SetSetting(ctx, "bs.path", "kept"); err != nil {
		t.Fatal(err)
	}
	if err := n.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, err := n.Store().GetSetting(ctx, "bs.path"); err == nil {
		t.Fatal("the store answers after Stop: the node did not close its handle")
	}

	again := newNode(t, cfg)
	v, found, err := again.Store().GetSetting(ctx, "bs.path")
	if err != nil || !found || v != "kept" {
		t.Fatalf("second node reads %q %v %v, want the first node's value", v, found, err)
	}
}

// Path and DB exclude each other; Path alone satisfies Validate.
func TestStorePathValidate(t *testing.T) {
	cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "db.db")))
	cfg.Store.Path = filepath.Join(t.TempDir(), "path.db")
	if err := cfg.Validate(); !errors.Is(err, node.ErrInvalidConfig) {
		t.Fatalf("Path with DB: %v, want ErrInvalidConfig", err)
	}
	cfg.Store.DB = nil
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Path alone: %v", err)
	}
	cfg.Store.Path = ""
	if err := cfg.Validate(); !errors.Is(err, node.ErrInvalidConfig) {
		t.Fatalf("no store: %v, want ErrInvalidConfig", err)
	}
}

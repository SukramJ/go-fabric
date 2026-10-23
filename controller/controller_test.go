// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/attestation"
	"github.com/SukramJ/go-fabric/mdns"
)

// silentConn is a DNS-SD socket on which nothing answers.
type silentConn struct{ out chan mdns.QueryResponse }

func (c silentConn) Send([]byte) error                    { return nil }
func (c silentConn) Responses() <-chan mdns.QueryResponse { return c.out }
func (c silentConn) Close() error                         { return nil }

func silentNames(log *slog.Logger) (*discovery.Names, error) {
	return discovery.NewNames(silentConn{out: make(chan mdns.QueryResponse)}, log), nil
}

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "controller.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func config(db *sql.DB) controller.Config {
	return controller.Config{
		DB: db, Attestation: attestation.TestStore(), Listen: "127.0.0.1:0", Label: "test",
		Logger: slog.New(slog.DiscardHandler), Names: silentNames,
	}
}

// TestNewKeepsTheFabric opens the controller twice on one database: the
// second run loads the fabric the first created, and no node is paired.
func TestNewKeepsTheFabric(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openDB(t)
	var fabricID, nodeID uint64
	for run := range 2 {
		c, err := controller.New(ctx, config(db))
		if err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		f := c.Fabric()
		if run == 0 {
			fabricID, nodeID = f.FabricID, f.RootNodeID
			if fabricID == 0 || nodeID == 0 {
				t.Errorf("first run: fabric %x node %x", fabricID, nodeID)
			}
		} else if f.FabricID != fabricID || f.RootNodeID != nodeID {
			t.Errorf("run %d: fabric %x node %x, want the first run's %x / %x", run, f.FabricID, f.RootNodeID, fabricID, nodeID)
		}
		if n := len(c.Nodes()); n != 0 || c.Registry() == nil || c.Commissioner() == nil {
			t.Errorf("run %d: %d nodes, registry %v, commissioner %v", run, n, c.Registry() != nil, c.Commissioner() != nil)
		}
		if err := c.Close(ctx); err != nil {
			t.Errorf("run %d: Close: %v", run, err)
		}
		if err := c.Close(ctx); err != nil {
			t.Errorf("run %d: second Close: %v", run, err)
		}
	}
}

// TestNewRequires refuses a configuration without its database or its
// trust store: attestation has no default.
func TestNewRequires(t *testing.T) {
	t.Parallel()
	for name, cfg := range map[string]controller.Config{
		"no DB":          {Attestation: attestation.TestStore()},
		"no Attestation": {DB: openDB(t)},
	} {
		if _, err := controller.New(context.Background(), cfg); !errors.Is(err, controller.ErrConfiguration) {
			t.Errorf("%s: %v, want ErrConfiguration", name, err)
		}
	}
}

// TestPairErrors: a malformed code is refused; a valid one no device
// answers ends at the deadline with ErrNoDevice.
func TestPairErrors(t *testing.T) {
	t.Parallel()
	cfg := config(openDB(t))
	cfg.ManualStart = true
	c, err := controller.New(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close(context.Background()) }()
	if _, err := c.Pair(context.Background(), "not a code", controller.PairOptions{}); err == nil || errors.Is(err, controller.ErrNoDevice) {
		t.Errorf("malformed code: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := c.Pair(ctx, "34970112332", controller.PairOptions{}); !errors.Is(err, controller.ErrNoDevice) {
		t.Errorf("silent network: %v, want ErrNoDevice", err)
	}
}

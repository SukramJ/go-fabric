// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/mdns"
)

// silentConn is a DNS-SD socket on which nothing answers.
type silentConn struct{ out chan mdns.QueryResponse }

func (c silentConn) Send([]byte) error                    { return nil }
func (c silentConn) Responses() <-chan mdns.QueryResponse { return c.out }
func (c silentConn) Close() error                         { return nil }

func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.DiscardHandler))
	openNames = func(log *slog.Logger) (*discovery.Names, error) {
		return discovery.NewNames(silentConn{out: make(chan mdns.QueryResponse)}, log), nil
	}
	os.Exit(m.Run())
}

// TestQuickstartControllerStack opens the controller twice on one
// database: the second run is the same fabric, and with no node paired run
// says so.
func TestQuickstartControllerStack(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "controller.db")
	db, err := openDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var fabricID, nodeID uint64
	for run := range 2 {
		c, err := open(ctx, db, "127.0.0.1:0", slog.Default())
		if err != nil {
			t.Fatalf("run %d: open: %v", run, err)
		}
		f := c.Fabric()
		if run == 0 {
			fabricID, nodeID = f.FabricID, f.RootNodeID
		} else if f.FabricID != fabricID || f.RootNodeID != nodeID {
			t.Errorf("run %d: fabric %x node %x, want the first run's %x / %x", run, f.FabricID, f.RootNodeID, fabricID, nodeID)
		}
		if got := len(c.Nodes()); got != 0 {
			t.Errorf("run %d: %d commissioned nodes, want none", run, got)
		}
		_ = c.Close(ctx)
	}
	if err := run(ctx, filepath.Join(t.TempDir(), "empty.db"), ""); err == nil || !strings.Contains(err.Error(), "no paired node") {
		t.Errorf("run without a node = %v, want the no-paired-node error", err)
	}
}

// TestQuickstartControllerPairErrors: a malformed code is refused, a valid
// one that no device answers ends at the deadline.
func TestQuickstartControllerPairErrors(t *testing.T) {
	ctx := context.Background()
	if err := run(ctx, filepath.Join(t.TempDir(), "controller.db"), "not a code"); err == nil {
		t.Error("run accepted a malformed code")
	}
	// The stack opens on a context of its own; only the pairing is bounded
	// short, so a slow schema apply (the race detector) does not end it.
	db, err := openDB(filepath.Join(t.TempDir(), "controller2.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	c, err := open(ctx, db, "127.0.0.1:0", slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close(ctx) }()
	short, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	if _, err := c.Pair(short, "34970112332", controller.PairOptions{}); !errors.Is(err, controller.ErrNoDevice) {
		t.Errorf("pair on a silent network = %v, want ErrNoDevice", err)
	}
}

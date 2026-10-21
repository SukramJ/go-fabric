// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
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

// TestQuickstartControllerStack opens the stack twice on one database: the
// second run is the same fabric, and with no node paired run says so.
func TestQuickstartControllerStack(t *testing.T) {
	ctx := context.Background()
	db := filepath.Join(t.TempDir(), "controller.db")
	var fabricID, nodeID uint64
	for run := range 2 {
		s, err := open(ctx, db, "127.0.0.1:0", slog.Default())
		if err != nil {
			t.Fatalf("run %d: open: %v", run, err)
		}
		if run == 0 {
			fabricID, nodeID = s.fabric.FabricID, s.fabric.RootNodeID
		} else if s.fabric.FabricID != fabricID || s.fabric.RootNodeID != nodeID {
			t.Errorf("run %d: fabric %x node %x, want the first run's %x / %x", run, s.fabric.FabricID, s.fabric.RootNodeID, fabricID, nodeID)
		}
		if got := len(s.reg.Commissioned()); got != 0 {
			t.Errorf("run %d: %d commissioned nodes, want none", run, got)
		}
		s.close(ctx)
	}
	if err := run(ctx, db, ""); err == nil || !strings.Contains(err.Error(), "no paired node") {
		t.Errorf("run without a node = %v, want the no-paired-node error", err)
	}
}

// TestQuickstartControllerPairErrors: a malformed code is refused, a valid
// one that no device answers ends at the deadline.
func TestQuickstartControllerPairErrors(t *testing.T) {
	s, err := open(context.Background(), filepath.Join(t.TempDir(), "controller.db"), "127.0.0.1:0", slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer s.close(context.Background())
	if err := s.pair(context.Background(), "not a code"); err == nil {
		t.Error("pair accepted a malformed code")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := s.pair(ctx, "34970112332"); err == nil || !strings.Contains(err.Error(), "no commissionable device") {
		t.Errorf("pair on a silent network = %v, want the no-device error", err)
	}
}

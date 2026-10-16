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

	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/node"
)

// lightEndpoint returns the endpoint number the running node gave the
// light, false when the topology has none.
func lightEndpoint(n *node.Node) (uint16, bool) {
	topo := n.Bridge().Topology()
	if topo == nil {
		return 0, false
	}
	for _, ep := range topo.Bridged() {
		if ep.SourceKey != nil && ep.SourceKey.String() == "light-1" {
			return ep.ID, true
		}
	}
	return 0, false
}

// TestQuickstartDevice builds the program's node on loopback, starts it,
// finds the light and the pairing codes, stops it, and starts a second
// node on the same database: the light keeps its endpoint number.
func TestQuickstartDevice(t *testing.T) {
	ctx := context.Background()
	db, err := openDB(filepath.Join(t.TempDir(), "quickstart.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	loopback := node.Network{Listen: "127.0.0.1:0", PreferIPv4: true}

	var first uint16
	for run := range 2 {
		n, err := newNode(ctx, db, loopback, mdns.NewNoop())
		if err != nil {
			t.Fatalf("run %d: newNode: %v", run, err)
		}
		if err := n.Start(ctx); err != nil {
			t.Fatalf("run %d: Start: %v", run, err)
		}
		id, ok := lightEndpoint(n)
		if !ok {
			t.Fatalf("run %d: the light is not in the topology", run)
		}
		if run == 0 {
			first = id
		} else if id != first {
			t.Errorf("run %d: the light moved from endpoint %d to %d", run, first, id)
		}
		qr, manual, err := n.PairingCodes()
		if err != nil || !strings.HasPrefix(qr, "MT:") || len(manual) != 11 {
			t.Errorf("run %d: PairingCodes() = %q, %q, %v", run, qr, manual, err)
		}
		if err := n.Stop(ctx); err != nil {
			t.Fatalf("run %d: Stop: %v", run, err)
		}
	}
}

// TestQuickstartDeviceRunStops: the node run serves until its context
// ends, then stops and returns nil.
func TestQuickstartDeviceRunStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	db, err := openDB(filepath.Join(t.TempDir(), "quickstart.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	n, err := newNode(ctx, db, node.Network{Listen: "127.0.0.1:0", PreferIPv4: true}, mdns.NewNoop())
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- n.Run(ctx) }()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, ok := lightEndpoint(n); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the running node never served the light")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after its context ended")
	}
}

func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.DiscardHandler))
	os.Exit(m.Run())
}

// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"log/slog"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/controller/discovery"
	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/node"
)

// silentMDNS is an mDNS socket that sends nowhere and receives nothing.
type silentMDNS struct{ out chan mdns.QueryResponse }

func (s *silentMDNS) Send([]byte) error                    { return nil }
func (s *silentMDNS) Responses() <-chan mdns.QueryResponse { return s.out }
func (s *silentMDNS) Close() error                         { return nil }

func silentNames(t *testing.T) *discovery.Names {
	t.Helper()
	names := discovery.NewNames(&silentMDNS{out: make(chan mdns.QueryResponse)}, slog.New(slog.DiscardHandler))
	t.Cleanup(func() { _ = names.Close() })
	return names
}

func waitAttached(t *testing.T, n *node.Node, want []uint16) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !slices.Equal(n.Bindings().Attached(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("attached %v, want %v", n.Bindings().Attached(), want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestNodeBindingsFollowTheTopology: the node's binding manager listens to
// the Binding server of every endpoint the bridge serves once the node
// runs, and leaves one whose endpoint goes (BindingServer.ts:120-126,
// :160-163). Without Config.Client there is no peer set.
func TestNodeBindingsFollowTheTopology(t *testing.T) {
	t.Parallel()
	db := openDB(t, filepath.Join(t.TempDir(), "node.db"))
	n := newNode(t, minimalConfig(db))
	if n.Peers() != nil {
		t.Fatal("a peer set without the client stack")
	}
	if n.Bindings() == nil {
		t.Fatal("no binding manager")
	}
	sw, err := device.NewOnOffLightSwitch(device.Options{StableKey: endpoint.StringKey("switch"), Name: "switch"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := n.Aggregator().Add(ctx, sw); err != nil {
		t.Fatal(err)
	}
	if got := n.Bindings().Attached(); len(got) != 0 {
		t.Fatalf("attached before Start: %v", got)
	}
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	var swEP uint16
	for _, ep := range n.Bridge().Topology().Bridged() {
		if ep.SourceKey.String() == "switch" {
			swEP = ep.ID
		}
	}
	if swEP == 0 {
		t.Fatal("the switch has no endpoint")
	}
	waitAttached(t, n, []uint16{swEP})
	if err := n.Aggregator().Remove(ctx, endpoint.StringKey("switch")); err != nil {
		t.Fatal(err)
	}
	waitAttached(t, n, []uint16{})
	if err := n.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}

// TestNodeClientStack: Config.Client builds the client socket, its
// exchanges and the peer set; Stop releases them.
func TestNodeClientStack(t *testing.T) {
	t.Parallel()
	db := openDB(t, filepath.Join(t.TempDir(), "node.db"))
	cfg := minimalConfig(db)
	cfg.Client = node.Client{Enabled: true, Listen: "127.0.0.1:0", Names: silentNames(t)}
	n := newNode(t, cfg)
	if n.Peers() == nil {
		t.Fatal("no peer set")
	}
	ctx := context.Background()
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := n.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := n.Stop(ctx); err != nil {
		t.Fatal(err)
	}

	bad := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "bad.db")))
	bad.Client = node.Client{Enabled: true, Listen: "not an address", Names: silentNames(t)}
	if _, err := node.New(bad); err == nil {
		t.Fatal("New accepted an unusable client address")
	}
}

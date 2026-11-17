// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/node"
)

// TestPartMovesBeforeTheNodeRuns (R2-P6-fsm-2501, decision 225): on a node
// that has not started, a part moves between two bridged nodes the
// Aggregator holds and the started node serves it under its new owner;
// on the running node the move is refused and changes nothing.
func TestPartMovesBeforeTheNodeRuns(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	n := newNode(t, minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db"))))
	newOwner := func(key string) *device.BridgedNode {
		d, err := device.NewBridgedNode(device.Options{StableKey: endpoint.StringKey(key), Name: key})
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	a, b := newOwner("a"), newOwner("b")
	lamp, err := device.NewOnOffLight(device.Options{StableKey: endpoint.StringKey("lamp"), Name: "Lamp"})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.AddPart(lamp.Device); err != nil {
		t.Fatal(err)
	}
	for _, d := range []*device.BridgedNode{a, b} {
		if err := n.Aggregator().Add(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.AddPart(lamp.Device); err != nil {
		t.Fatalf("a move before the node runs: %v", err)
	}
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	partsOf := func(key string) []string {
		for _, d := range n.Topology().Devices {
			if d.Key.String() == key {
				var keys []string
				for _, p := range d.Parts {
					keys = append(keys, p.Key.String())
				}
				return keys
			}
		}
		return nil
	}
	if !slices.Equal(partsOf("b"), []string{"lamp"}) || len(partsOf("a")) != 0 {
		t.Fatalf("after the move the node serves a %v, b %v; want the lamp under b", partsOf("a"), partsOf("b"))
	}
	ep, ok := lamp.EndpointNumber()
	if !ok {
		t.Fatal("the moved part has no endpoint number")
	}
	if err := a.AddPart(lamp.Device); !errors.Is(err, device.ErrComposition) {
		t.Fatalf("a move on the running node: %v, want ErrComposition", err)
	}
	if !slices.Contains(b.Parts(), lamp.Device) || len(a.Parts()) != 0 {
		t.Errorf("the refused move changed the owners: a %v, b %v", a.Parts(), b.Parts())
	}
	if got, ok := lamp.EndpointNumber(); !ok || got != ep {
		t.Errorf("the refused move changed the part's endpoint: %d, %v; was %d", got, ok, ep)
	}
}

// snapshotProbe is a bridged endpoint whose Spec, once armed, runs probe:
// the Aggregator's snapshot calls it while the bridge's start assembles.
type snapshotProbe struct {
	*plug
	armed *atomic.Bool
	probe func()
}

func (s snapshotProbe) Spec() endpoint.Spec {
	if s.armed.CompareAndSwap(true, false) {
		s.probe()
	}
	return s.plug.Spec()
}

// TestPartDoesNotMoveDuringTheStartsSnapshot (decision 225, round 3): the
// node counts as running before the bridge's start takes its snapshot, so
// a move attempted while that snapshot is read is refused and the started
// node serves the part where the snapshot found it.
func TestPartDoesNotMoveDuringTheStartsSnapshot(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	n := newNode(t, minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db"))))
	a, err := device.NewBridgedNode(device.Options{StableKey: endpoint.StringKey("a"), Name: "a"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := device.NewBridgedNode(device.Options{StableKey: endpoint.StringKey("b"), Name: "b"})
	if err != nil {
		t.Fatal(err)
	}
	lamp, err := device.NewOnOffLight(device.Options{StableKey: endpoint.StringKey("lamp"), Name: "Lamp"})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.AddPart(lamp.Device); err != nil {
		t.Fatal(err)
	}
	armed := &atomic.Bool{}
	var moveErr error
	probe := snapshotProbe{plug: newPlug(t, "probe"), armed: armed, probe: func() { moveErr = b.AddPart(lamp.Device) }}
	for _, ep := range []node.Endpoint{probe, a, b} {
		if err := n.Aggregator().Add(ctx, ep); err != nil {
			t.Fatal(err)
		}
	}
	armed.Store(true)
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if armed.Load() {
		t.Fatal("the start's snapshot did not read the probe")
	}
	if !errors.Is(moveErr, device.ErrComposition) {
		t.Fatalf("a move during the start's snapshot: %v, want ErrComposition", moveErr)
	}
	if !slices.Contains(a.Parts(), lamp.Device) || len(b.Parts()) != 0 {
		t.Errorf("the refused move changed the owners: a %v, b %v", a.Parts(), b.Parts())
	}
}

// TestFailedStartLetsPartsMoveAgain (decision 225, round 3): a Start whose
// bridge fails to start leaves the node not running, so its parts move.
func TestFailedStartLetsPartsMoveAgain(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	busy, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db")))
	cfg.Network.Listen = busy.LocalAddr().String()
	n := newNode(t, cfg)
	a, err := device.NewBridgedNode(device.Options{StableKey: endpoint.StringKey("a"), Name: "a"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := device.NewBridgedNode(device.Options{StableKey: endpoint.StringKey("b"), Name: "b"})
	if err != nil {
		t.Fatal(err)
	}
	lamp, err := device.NewOnOffLight(device.Options{StableKey: endpoint.StringKey("lamp"), Name: "Lamp"})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.AddPart(lamp.Device); err != nil {
		t.Fatal(err)
	}
	for _, d := range []*device.BridgedNode{a, b} {
		if err := n.Aggregator().Add(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	if err := n.Start(ctx); err == nil || !strings.Contains(err.Error(), "bridge start") {
		t.Fatalf("Start on an address in use: %v, want the bridge's start to fail", err)
	}
	if err := b.AddPart(lamp.Device); err != nil {
		t.Errorf("a move after the failed Start: %v", err)
	}
}

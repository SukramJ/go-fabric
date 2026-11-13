// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/node"
)

// freeUDPAddr returns a loopback UDP address nothing is bound to.
func freeUDPAddr(t *testing.T) string {
	t.Helper()
	probe, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := probe.LocalAddr().String()
	if err := probe.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

// TestStopWithDoneContextReleasesThePort (review II, R2-P1-dr-req-01): a
// Node.Stop whose context is already done — the `<-ctx.Done(); n.Stop(ctx)`
// idiom — still takes the bridge down, so the next node binds the same
// port. Before, about half of these Stops left the bridge serving on a
// node marked stopped, and no second Stop released it.
func TestStopWithDoneContextReleasesThePort(t *testing.T) {
	t.Parallel()
	addr := freeUDPAddr(t)
	done, cancel := context.WithCancel(t.Context())
	cancel()
	const runs = 20
	for i := range runs {
		cfg := minimalConfig(nil)
		cfg.Store = node.Store{Path: filepath.Join(t.TempDir(), "n.db")}
		cfg.Network.Listen = addr
		n := newNode(t, cfg)
		if err := n.Start(t.Context()); err != nil {
			t.Fatalf("node %d: Start on %s: %v", i, addr, err)
		}
		if err := n.Stop(done); err != nil {
			t.Fatalf("node %d: Stop with a done context: %v", i, err)
		}
		if got := n.LocalAddr(); got != "" {
			t.Fatalf("node %d: still bound to %s after Stop", i, got)
		}
	}
}

// gatedPlug is a plug whose Spec, once armed, waits for release after
// closing entered — the node's snapshotter calls it inside a bridge Start.
type gatedPlug struct {
	*plug
	armed            chan struct{}
	entered, release chan struct{}
	once             sync.Once
}

func (g *gatedPlug) Spec() endpoint.Spec {
	select {
	case <-g.armed:
		g.once.Do(func() {
			close(g.entered)
			<-g.release
		})
	default:
	}
	return g.plug.Spec()
}

// TestStopThatDidNotStopTheBridgeLeavesTheNodeStoppable (review II,
// R2-P1-dr-req-01): when the bridge's Stop gives up — its context ends
// while a bridge Start holds the lifecycle — the node is not marked
// stopped and keeps its store: a later Stop with a live context takes
// the bridge down and frees the port. Before, the first Stop marked the
// node stopped and closed its store under a serving bridge, and every
// later Stop returned nil at once.
func TestStopThatDidNotStopTheBridgeLeavesTheNodeStoppable(t *testing.T) {
	t.Parallel()
	addr := freeUDPAddr(t)
	cfg := minimalConfig(nil)
	cfg.Store = node.Store{Path: filepath.Join(t.TempDir(), "n.db")}
	cfg.Network.Listen = addr
	n := newNode(t, cfg)
	ctx, cancelCtx := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancelCtx()
	g := &gatedPlug{plug: newPlug(t, "gated"), armed: make(chan struct{}), entered: make(chan struct{}), release: make(chan struct{})}
	if err := n.Aggregator().Add(ctx, g); err != nil {
		t.Fatal(err)
	}
	close(g.armed)
	// A bridge Start the host runs directly holds the bridge's lifecycle
	// until the snapshot returns.
	bridgeStarted := make(chan error, 1)
	go func() { bridgeStarted <- n.Bridge().Start(ctx) }()
	select {
	case <-g.entered:
	case <-ctx.Done():
		t.Fatal("the bridge Start never reached the snapshot")
	}

	done, cancel := context.WithCancel(t.Context())
	cancel()
	if err := n.Stop(done); err == nil {
		close(g.release)
		<-bridgeStarted
		t.Fatal("Stop with a done context returned nil while a bridge Start held the lifecycle")
	}
	if _, err := n.FabricInfos(ctx); err != nil {
		t.Errorf("the store after a Stop that did not stop the bridge: %v", err)
	}
	if err := n.Start(ctx); !errors.Is(err, node.ErrStopped) {
		t.Errorf("Start after a Stop began: %v, want ErrStopped", err)
	}
	close(g.release)
	if err := <-bridgeStarted; err != nil {
		t.Fatalf("the bridge Start: %v", err)
	}
	if err := n.Stop(ctx); err != nil {
		t.Fatalf("the second Stop: %v", err)
	}
	if got := n.LocalAddr(); got != "" {
		t.Fatalf("the bridge still bound to %s after the second Stop", got)
	}
	free, err := net.ListenPacket("udp4", addr)
	if err != nil {
		t.Fatalf("the port after the second Stop: %v", err)
	}
	_ = free.Close()
}

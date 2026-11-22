// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/node"
)

// releasedPort binds addr itself, at once, and closes it again: the proof
// that a stopped node released the port, which is what the stop tests
// claim. A failure here is the node's.
func releasedPort(t *testing.T, addr, after string) {
	t.Helper()
	pc, err := net.ListenPacket("udp4", addr)
	if err != nil {
		t.Fatalf("the port %s %s: %v", addr, after, err)
	}
	if err := pc.Close(); err != nil {
		t.Fatal(err)
	}
}

// startOn starts n, which is configured to listen on addr, right after
// releasedPort proved the port free. The port is in the ephemeral range,
// so between the test's close and the node's bind another socket on the
// host can be assigned it: a Start that fails with EADDRINUSE then meets
// a foreign socket, not the node's, and is retried once with a log line
// — a collision visible as such, not a flake (R2-P6-test-3104,
// N5-tflake-2, N6a-flake-4).
func startOn(ctx context.Context, t *testing.T, addr string, newN func() *node.Node) *node.Node {
	t.Helper()
	n := newN()
	err := n.Start(ctx)
	if errors.Is(err, syscall.EADDRINUSE) {
		t.Logf("Start on %s met a foreign socket right after the port was released (%v); retrying once", addr, err)
		_ = n.Stop(context.Background())
		releasedPort(t, addr, "before the retry")
		n = newN()
		err = n.Start(ctx)
	}
	if err != nil {
		t.Fatalf("Start on %s: %v", addr, err)
	}
	return n
}

// TestStopWithDoneContextReleasesThePort (review II, R2-P1-dr-req-01): a
// Node.Stop whose context is already done — the `<-ctx.Done(); n.Stop(ctx)`
// idiom — still takes the bridge down, so the next node binds the same
// port. Before, about half of these Stops left the bridge serving on a
// node marked stopped, and no second Stop released it.
//
// The first node binds a port the system assigns, and every later node
// that port: no probe bind opens a window before the first node, and
// after each Stop the test binds the port itself before the next node
// does (R2-P6-test-3104).
func TestStopWithDoneContextReleasesThePort(t *testing.T) {
	t.Parallel()
	done, cancel := context.WithCancel(t.Context())
	cancel()
	nodeOn := func(listen string) func() *node.Node {
		return func() *node.Node {
			cfg := minimalConfig(nil)
			cfg.Store = node.Store{Path: filepath.Join(t.TempDir(), "n.db")}
			cfg.Network.Listen = listen
			return newNode(t, cfg)
		}
	}
	first := nodeOn("127.0.0.1:0")()
	if err := first.Start(t.Context()); err != nil {
		t.Fatalf("node 0: Start: %v", err)
	}
	addr := first.LocalAddr()
	n := first
	const runs = 20
	for i := range runs {
		if i > 0 {
			n = startOn(t.Context(), t, addr, nodeOn(addr))
		}
		if err := n.Stop(done); err != nil {
			t.Fatalf("node %d: Stop with a done context: %v", i, err)
		}
		if got := n.LocalAddr(); got != "" {
			t.Fatalf("node %d: still bound to %s after Stop", i, got)
		}
		releasedPort(t, addr, fmt.Sprintf("after node %d's Stop", i))
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
	// The node binds a port the system assigns; the test reads it once the
	// bridge Start below has bound it (no probe bind, R2-P6-test-3104).
	cfg := minimalConfig(nil)
	cfg.Store = node.Store{Path: filepath.Join(t.TempDir(), "n.db")}
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
	addr := n.LocalAddr()
	if addr == "" {
		t.Fatal("the bridge Start bound no port")
	}
	if err := n.Stop(ctx); err != nil {
		t.Fatalf("the second Stop: %v", err)
	}
	if got := n.LocalAddr(); got != "" {
		t.Fatalf("the bridge still bound to %s after the second Stop", got)
	}
	releasedPort(t, addr, "after the second Stop")
}

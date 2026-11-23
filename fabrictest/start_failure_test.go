// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest_test

import (
	"context"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/clock"
	"thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/fabrictest"
	"thingfabric.dev/go-fabric/internal/inflight"
	"thingfabric.dev/go-fabric/node"
	"thingfabric.dev/go-fabric/store"
)

// flakyEndpoint is a bridged endpoint whose Spec loses its StableKey while
// broken is set, so the Aggregator refuses it.
type flakyEndpoint struct {
	node.Endpoint
	broken *atomic.Bool
}

func (f flakyEndpoint) Spec() endpoint.Spec {
	s := f.Endpoint.Spec()
	if f.broken.Load() {
		s.StableKey = nil
	}
	return s
}

// TestFailedStartLeavesNothingBound (R2-P6-err-2510): a Start that fails
// after the node was built — here the Aggregator refuses an endpoint —
// stops that node, so its client socket on the site's network is free and
// the next Start succeeds.
func TestFailedStartLeavesNothingBound(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	broken := &atomic.Bool{}
	d := site.AddDevice(fabrictest.Topology{
		Bridged:   []node.Endpoint{flakyEndpoint{Endpoint: newPlug(t), broken: broken}},
		Configure: func(cfg *node.Config) { cfg.Client = &node.ClientConfig{} },
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := d.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	broken.Store(true)
	if err := d.Start(ctx); err == nil {
		t.Fatal("Start with a refused endpoint succeeded")
	}
	broken.Store(false)
	if err := d.Start(ctx); err != nil {
		t.Fatalf("Start after a failed Start: %v", err)
	}
}

// stallingStore is a device's credential store whose ListFabrics, once
// armed and called from the node's Start, outlives the Start's context:
// it waits for that context to end and then on a timer of the host's
// clock, as a started node's step waits on the site's clock.
type stallingStore struct {
	store.Port
	clk   clock.Clock
	armed atomic.Bool
}

func (s *stallingStore) ListFabrics(ctx context.Context) ([]store.FabricRecord, error) {
	buf := make([]byte, 16<<10)
	if strings.Contains(string(buf[:runtime.Stack(buf, false)]), "node.(*Node).Start(") && s.armed.CompareAndSwap(true, false) {
		<-ctx.Done()
		<-clock.After(s.clk, time.Hour)
		return nil, ctx.Err()
	}
	return s.Port.ListFabrics(ctx)
}

// TestFailedStartStopsTheNodeOnTheSitesClock (review II, R2-C2-test-2501):
// a Start whose context ends while the node's own Start still waits on the
// site's clock stops that node under the site's pacing, bounded: the
// failed Start returns its error instead of waiting on a clock nobody
// moves, and the next Start succeeds.
func TestFailedStartStopsTheNodeOnTheSitesClock(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	st := &stallingStore{}
	d := site.AddDevice(fabrictest.Topology{
		Bridged: []node.Endpoint{newPlug(t)},
		Configure: func(cfg *node.Config) {
			if st.Port == nil {
				st.Port, st.clk = cfg.Store.Credentials, cfg.Clock
			}
			cfg.Store.Credentials = st
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := d.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	st.armed.Store(true)
	short, cancelShort := context.WithTimeout(ctx, 2*time.Second)
	defer cancelShort()
	done := make(chan error, 1)
	go func() { done <- d.Start(short) }()
	// A stop of the node that waits on a clock nobody moves hangs here to
	// the package timeout.
	if err := <-done; err == nil {
		t.Fatal("Start whose context ended during the node's start succeeded")
	}
	if err := d.Start(ctx); err != nil {
		t.Fatalf("Start after the failed Start: %v", err)
	}
}

// TestFailedStopIsFinishedByTheNext (review II, R2-C2-test-2502): a Stop
// that fails — here its context ended before it began — leaves the device
// running, so the next Stop (or the site's closer) stops the node, and a
// Start after it succeeds.
func TestFailedStopIsFinishedByTheNext(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	d := site.AddDevice(fabrictest.Topology{Bridged: []node.Endpoint{newPlug(t)}})
	ended, cancelEnded := context.WithCancel(context.Background())
	cancelEnded()
	if err := d.Stop(ended); err == nil {
		t.Fatal("Stop with an ended context succeeded")
	}
	if !d.Running() {
		t.Error("the device reads stopped after a Stop that failed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := d.Stop(ctx); err != nil {
		t.Fatalf("Stop after the failed Stop: %v", err)
	}
	if d.Running() {
		t.Error("the device reads running after its Stop")
	}
	if err := d.Start(ctx); err != nil {
		t.Fatalf("Start after the failed and the finished Stop: %v", err)
	}
}

// TestFailedFactoryResetIsFinishedByTheNext (review II, decision 532, the
// class of R2-C2-test-2502): a FactoryReset of a running device that
// fails — here its context ended before it began — leaves the device
// running, so the next FactoryReset finishes the reset and brings the
// device back, and a Stop after it stops the node.
func TestFailedFactoryResetIsFinishedByTheNext(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	d := site.AddDevice(fabrictest.Topology{Bridged: []node.Endpoint{newPlug(t)}})
	ended, cancelEnded := context.WithCancel(context.Background())
	cancelEnded()
	if err := d.FactoryReset(ended); err == nil {
		t.Fatal("FactoryReset with an ended context succeeded")
	}
	if !d.Running() {
		t.Error("the device reads stopped after a FactoryReset that failed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := d.FactoryReset(ctx); err != nil {
		t.Fatalf("FactoryReset after the failed FactoryReset: %v", err)
	}
	if !d.Running() {
		t.Error("the device reads stopped after the FactoryReset of a running device")
	}
	if err := d.Stop(ctx); err != nil {
		t.Fatalf("Stop after the finished FactoryReset: %v", err)
	}
	if d.Running() {
		t.Error("the device reads running after its Stop")
	}
}

// TestStartGivenUpLeavesTheBuildCountOnce (review II, R3-test-3802): a
// Start whose context ends after the node was built, so Site.Run gives up
// on the node's start before its callback ran, leaves the node.build count
// once — by the failed Start or by the callback, never by both — and the
// count is back at zero, not below it, once the callback ran.
func TestStartGivenUpLeavesTheBuildCountOnce(t *testing.T) {
	t.Parallel()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	var clk clock.Clock
	var cancelStart atomic.Pointer[context.CancelFunc]
	d := site.AddDevice(fabrictest.Topology{
		Bridged: []node.Endpoint{newPlug(t)},
		Configure: func(cfg *node.Config) {
			if clk == nil {
				clk = cfg.Clock
			}
		},
		Prepare: func(*node.Node) {
			if c := cancelStart.Swap(nil); c != nil {
				(*c)()
			}
		},
	})
	group := inflight.Of(clk)
	if group == nil {
		t.Fatal("the device's clock carries no inflight group")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := d.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	short, cancelShort := context.WithCancel(ctx)
	defer cancelShort()
	cancelStart.Store(&cancelShort)
	if err := d.Start(short); err == nil {
		t.Fatal("Start whose context ended after the build succeeded")
	}
	// The callback Site.Run gave up on may still run: once the site is
	// quiet it ran (its goroutine was started before Run gave up, and
	// leaving the count is its first step), and the count must be zero,
	// not below it.
	if err := site.Settle(ctx); err != nil {
		t.Fatal(err)
	}
	if got := group.Snapshot()[inflight.NodeBuild]; got != 0 {
		t.Fatalf("node.build count %d after the failed Start, want 0", got)
	}
	if err := d.Start(ctx); err != nil {
		t.Fatalf("Start after the failed Start: %v", err)
	}
}

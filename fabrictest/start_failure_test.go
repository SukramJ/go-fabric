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
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Start whose context ended during the node's start succeeded")
		}
	case <-time.After(45 * time.Second):
		t.Fatal("the failed Start did not return: its stop of the node waits on a clock nobody moves")
	}
	if err := d.Start(ctx); err != nil {
		t.Fatalf("Start after the failed Start: %v", err)
	}
}

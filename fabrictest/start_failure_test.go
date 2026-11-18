// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
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

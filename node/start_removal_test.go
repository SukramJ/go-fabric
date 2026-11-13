// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/mdns"
	"github.com/SukramJ/go-fabric/node"
)

// removingAdvertiser runs remove once, when the node publishes its first
// operational record: in Start, after the fabrics' CASE identities loaded
// (an administrator can reach the node over CASE from then on) and before
// Start returns.
type removingAdvertiser struct {
	*mdns.Noop
	once   sync.Once
	remove func(ctx context.Context)
}

func (a *removingAdvertiser) Publish(ctx context.Context, svc mdns.Service) error {
	if svc.ServiceType == mdns.ServiceTypeOperational {
		a.once.Do(func() { a.remove(ctx) })
	}
	return a.Noop.Publish(ctx, svc)
}

// TestLastFabricRemovedDuringStartReopensTheWindow: a commissioned node
// whose last fabric an administrator removes while the node is still
// starting — its CASE identity loaded, Start not yet returned — ends
// uncommissioned with its own window open, as a removal after Start does.
// Start's own view of the fabric table, read before the removal, does not
// mark it commissioned again.
func TestLastFabricRemovedDuringStartReopensTheWindow(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "node.db")
	first, c, _ := commissioned(t, minimalConfig(openDB(t, path)))
	fabricIndex := c.FabricIndex()
	if err := first.Stop(ctx); err != nil {
		t.Fatal(err)
	}

	var n *node.Node
	removed := make(chan error, 1)
	cfg := minimalConfig(openDB(t, path))
	cfg.Advertiser = &removingAdvertiser{Noop: mdns.NewNoop(), remove: func(ctx context.Context) {
		removed <- n.RemoveFabric(ctx, fabricIndex)
	}}
	n = newNode(t, cfg)
	opened := make(chan struct{}, 1)
	defer n.Observe(node.Observers{OnWindowTransition: func(w node.WindowSnapshot) {
		if w.Open && w.Own {
			select {
			case opened <- struct{}{}:
			default:
			}
		}
	}})()
	if err := n.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	select {
	case err := <-removed:
		if err != nil {
			t.Fatalf("RemoveFabric during Start: %v", err)
		}
	default:
		t.Fatal("Start published no operational record: the removal did not run during Start")
	}
	select {
	case <-opened:
	case <-ctx.Done():
		t.Fatalf("no own commissioning window after the last fabric was removed during Start (window %+v)", n.Window())
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/bridge/bridgetest"
	"thingfabric.dev/go-fabric/mdns"
	"thingfabric.dev/go-fabric/node"
)

// TestHostHooksAddToTheFacades (R2-P5-apinode-07): a host that sets the
// public slot of the hooks the facade wires — the window's transition hook,
// GeneralCommissioning's commissioning-complete hook, OperationalCredentials'
// fabric-removed and fabric-updated hooks — adds to the facade's handler and
// runs after it: the facade still announces the window, reports the
// commissioning and removes the fabric. Before, the host's Set… replaced the
// facade's handler.
func TestHostHooksAddToTheFacades(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	adv := mdns.NewNoop()
	cfg := minimalConfig(openDB(t, filepath.Join(t.TempDir(), "node.db")))
	cfg.Advertiser = adv
	var mu sync.Mutex
	var events []string
	record := func(e string) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	}
	cfg.Observers.OnCommissioningComplete = func(uint8) { record("facade complete") }
	cfg.Observers.OnFabricRemoved = func(node.FabricInfo) { record("facade removed") }
	n := newNode(t, cfg)
	n.Bridge().CommissioningWindow().SetTransitionHook(func() { record("host transition") })
	n.Root().GeneralCommissioning.SetOnCommissioningComplete(func(context.Context, uint8) { record("host complete") })
	n.Root().OperationalCredentials.SetOnFabricRemoved(func(context.Context, uint8) { record("host removed") })
	n.Root().OperationalCredentials.SetOnFabricUpdated(func(context.Context, uint8) { record("host updated") })
	if err := n.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// The facade's transition handler announced the own window.
	waitRecord(t, adv, "the own window", func(_, mode uint64, ok bool) bool { return ok && mode == 1 })

	opts := commissionerOptions()
	opts.Advertiser = adv
	c := bridgetest.Dial(t, n.Bridge().LocalAddr(), opts)
	if _, err := c.Commission(ctx); err != nil {
		t.Fatalf("Commission: %v", err)
	}
	if err := n.RemoveFabric(ctx, c.FabricIndex()); err != nil {
		t.Fatalf("RemoveFabric: %v", err)
	}
	if fabrics, err := n.FabricInfos(ctx); err != nil || len(fabrics) != 0 {
		t.Fatalf("fabrics after RemoveFabric: %+v (%v)", fabrics, err)
	}
	waitLive(t, "the facade's complete and removed, each before the host's, and the host's transition", func() bool {
		mu.Lock()
		got := append([]string(nil), events...)
		mu.Unlock()
		return before(got, "facade complete", "host complete") && before(got, "facade removed", "host removed") && contains(got, "host transition")
	})
}

func contains(s []string, e string) bool {
	for _, x := range s {
		if x == e {
			return true
		}
	}
	return false
}

// before reports whether a occurs in s, b occurs in s, and the first a
// comes before the first b.
func before(s []string, a, b string) bool {
	ia, ib := -1, -1
	for i, x := range s {
		if x == a && ia < 0 {
			ia = i
		}
		if x == b && ib < 0 {
			ib = i
		}
	}
	return ia >= 0 && ib >= 0 && ia < ib
}

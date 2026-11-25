// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package esphome_test

import (
	"context"
	"log/slog"
	"net/netip"
	"sync"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/client/discovery"

	"thingfabric.dev/go-fabric/ble/esphome"
	"thingfabric.dev/go-fabric/ble/esphome/esphometest"
)

// swapListBrowserA8 answers each browse with its current list, after the
// browse's timeout as listBrowser does.
type swapListBrowserA8 struct {
	mu   sync.Mutex
	list []discovery.Instance
}

func (b *swapListBrowserA8) set(list ...discovery.Instance) {
	b.mu.Lock()
	b.list = list
	b.mu.Unlock()
}

func (b *swapListBrowserA8) Browse(ctx context.Context, _ string) ([]discovery.Instance, error) {
	<-ctx.Done()
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]discovery.Instance(nil), b.list...), nil
}

// TestAliasIsDialledWhenItsDeviceLeaves: an alias forgotten because the
// device it named left the set is dialled at once — leave asks for the
// next browse — not only at the next Rescan (review II, R4-fsm-4305).
func TestAliasIsDialledWhenItsDeviceLeaves(t *testing.T) {
	x := fake(t, esphometest.Options{Name: "x", MAC: "AABBCCDDEE51", Key: testKey})
	x2 := fake(t, esphometest.Options{Name: "x2", MAC: "AABBCCDDEE51", Key: testKey})
	xa, x2a := netip.MustParseAddrPort(x.Addr()), netip.MustParseAddrPort(x2.Addr())
	inner := &swapListBrowserA8{}
	inner.set(instanceAt("x-proxy", "aabbccddee51", xa), instanceAt("x-alias", "aabbccddee51", x2a))
	b := paced(inner)
	log := &capture{}
	central(t, esphome.Options{Logger: slog.New(log), Discover: &esphome.DiscoverOptions{
		Browser: b, Keys: esphome.Keys{"AA:BB:CC:DD:EE:51": testKey},
		// No browse comes from the Rescan within the test.
		Rescan: time.Hour, Timeout: 20 * time.Millisecond,
	}})
	// Both names are dialled at once: whichever answers first joins, the
	// other is recorded as its alias.
	var joined, other string
	eventually(t, "one name joined and the other recorded as its alias", func() bool {
		_, a := log.find("ble.esphome.proxy_discovered", "x-proxy")
		_, l := log.find("ble.esphome.proxy_discovered", "x-alias")
		switch {
		case a:
			joined, other = "x-proxy", "x-alias"
		case l:
			joined, other = "x-alias", "x-proxy"
		}
		return joined != "" && log.sameDeviceSkips() >= 1
	})
	// The joined device leaves: no longer advertised, and its connection
	// lost — the loss asks for one browse, which drops it and forgets the
	// alias.
	if joined == "x-proxy" {
		inner.set(instanceAt("x-alias", "aabbccddee51", x2a))
		x.Close()
	} else {
		inner.set(instanceAt("x-proxy", "aabbccddee51", xa))
		x2.Close()
	}
	eventually(t, "the alias dialled after its device left", func() bool {
		_, ok := log.find("ble.esphome.proxy_discovered", other)
		return ok
	})
}

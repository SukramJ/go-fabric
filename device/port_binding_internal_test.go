// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock"

	"github.com/SukramJ/go-fabric/cluster/spec"
	ccdef "github.com/SukramJ/go-fabric/cluster/spec/colorcontrol"
	ctdef "github.com/SukramJ/go-fabric/cluster/spec/commoditytariff"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/internal/deviceseam"
)

// TestTariffTimeSourceSubscriptionFollowsTheDevice (R2-P6-store-2515): the
// CommodityTariff server's subscription on a declared TimeSource is the
// built device's — one after New, kept while attached, none after the
// detach (Aggregator.Remove, Replace, Node.Stop), one again when a node
// adds it back — and a device New refuses leaves none.
func TestTariffTimeSourceSubscriptionFollowsTheDevice(t *testing.T) {
	t.Parallel()
	src := &tariffTime{now: time.Unix(1_700_000_000, 0)}
	decl := ctdef.Declare{Features: ctdef.FeaturePricing, TimeSource: src}
	// A hook on a cluster the type does not mount is refused after the
	// servers are built (attachHooks).
	unmounted := clusterOption{decl: spec.ClusterDeclaration{Cluster: ccdef.ClusterID, Attach: func(contract.ClusterServer) error { return nil }}}
	if _, err := NewElectricalEnergyTariff(testOptions("refused"), decl, unmounted); err == nil {
		t.Fatal("New with a hook on an unmounted cluster succeeded")
	}
	if n := src.active.Load(); n != 0 {
		t.Fatalf("subscriptions after a refused New %d, want 0", n)
	}
	d, err := NewElectricalEnergyTariff(testOptions("tariff"), decl)
	if err != nil {
		t.Fatal(err)
	}
	if n := src.active.Load(); n != 1 {
		t.Fatalf("subscriptions after New %d, want 1", n)
	}
	detach, ok, err := deviceseam.Attach(d, &fakeHost{clk: clock.System{}})
	if !ok || err != nil {
		t.Fatalf("attach: %v %v", ok, err)
	}
	if n := src.active.Load(); n != 1 {
		t.Fatalf("subscriptions while attached %d, want 1", n)
	}
	src.cb() // syncs the server
	detach()
	if n := src.active.Load(); n != 0 {
		t.Fatalf("subscriptions after the detach %d, want 0", n)
	}
	detach, _, err = deviceseam.Attach(d, &fakeHost{clk: clock.System{}})
	if err != nil {
		t.Fatal(err)
	}
	defer detach()
	if n := src.active.Load(); n != 1 {
		t.Fatalf("subscriptions after a second attach %d, want 1", n)
	}
}

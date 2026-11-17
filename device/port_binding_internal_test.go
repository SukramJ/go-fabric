// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package device

import (
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock"

	ctdef "github.com/SukramJ/go-fabric/cluster/spec/commoditytariff"
	"github.com/SukramJ/go-fabric/internal/deviceseam"
)

// TestTariffTimeSourceSubscriptionFollowsTheNode (R2-P6-store-2515): the
// CommodityTariff server's subscription on a declared TimeSource lives
// while the device is added — none after New, one while attached, none
// after the detach (Aggregator.Remove, Replace, Node.Stop).
func TestTariffTimeSourceSubscriptionFollowsTheNode(t *testing.T) {
	t.Parallel()
	src := &tariffTime{now: time.Unix(1_700_000_000, 0)}
	d, err := NewElectricalEnergyTariff(testOptions("tariff"), ctdef.Declare{Features: ctdef.FeaturePricing, TimeSource: src})
	if err != nil {
		t.Fatal(err)
	}
	if n := src.active.Load(); n != 0 {
		t.Fatalf("subscriptions after New %d, want 0", n)
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
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/fabrictest"
)

// TestVendorInfoOffline: an offline controller's vendor information
// service (matter.js DclBehavior.vendorInfoService with dcl.offline,
// packages/node/src/behavior/system/dcl/DclBehavior.ts:101-115 at
// 862ad75f) serves the embedded snapshot's vendors and the static ones
// without the ledger, and is built once.
func TestVendorInfoOffline(t *testing.T) {
	t.Parallel()
	_, _, c := siteController(t, fabrictest.Topology{}, func(cfg *controller.Config) { cfg.DCL.Offline = true })
	svc, err := c.VendorInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !svc.Offline() {
		t.Fatal("the service is online")
	}
	if v, ok := svc.InfoFor(4939); !ok || v.VendorName == "" {
		t.Fatalf("InfoFor(4939) = %+v, %v", v, ok)
	}
	if n := len(svc.Vendors()); n < 10 {
		t.Fatalf("%d vendors: the snapshot's seed is missing", n)
	}
	again, err := c.VendorInfo(context.Background())
	if err != nil || again != svc {
		t.Fatalf("a second service: %v", err)
	}
}

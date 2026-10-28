// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/SukramJ/go-fabric/controller/dcl"
	"github.com/SukramJ/go-fabric/controller/dcl/snapshot"
)

// DCLConfig configures the controller's ledger services — matter.js
// DclBehavior's state (packages/node/src/behavior/system/dcl/DclBehavior.ts:180-206
// at 862ad75f): the production ledger's URL and the offline switch, which
// keeps every service off the network.
type DCLConfig struct {
	// ProductionURL is the production ledger; "" is dcl.ProductionURL
	// (DclConfig.production).
	ProductionURL string
	// Offline keeps the services off the network (dcl.offline, :188-198).
	Offline bool
	// VendorStore keeps the vendors the ledger answered across runs; nil
	// keeps them in memory (a dcl.SettingStore over the host's settings,
	// say).
	VendorStore dcl.Store
	// VendorUpdateInterval is the vendor refresh's period; 0 is
	// dcl.DefaultVendorUpdateInterval, negative disables it.
	VendorUpdateInterval time.Duration
}

// VendorInfo is the controller's vendor information service, built on the
// first call — matter.js DclBehavior.vendorInfoService (DclBehavior.ts:101-115),
// which creates DclVendorInfoService with the production configuration and
// the node's offline switch: seeded from the DCL snapshot embedded in the
// module (controller/dcl/snapshot), refreshed from the ledger unless
// offline. The first call waits for the first refresh; a failed refresh is
// logged and leaves the seeded and static vendors. Close closes it.
func (c *Controller) VendorInfo(ctx context.Context) (*dcl.VendorInfoService, error) {
	c.vendorMu.Lock()
	defer c.vendorMu.Unlock()
	if c.vendorInfo != nil {
		return c.vendorInfo, nil
	}
	opts := dcl.VendorInfoOptions{
		Client: &dcl.Client{BaseURL: c.dcl.ProductionURL, Logger: c.log}, Store: c.dcl.VendorStore,
		UpdateInterval: c.dcl.VendorUpdateInterval, Offline: c.dcl.Offline, Clock: c.clock, Logger: c.log,
	}
	if s, err := snapshot.Embedded(); s != nil {
		opts.Seed = s.VendorSeed()
	} else if err != nil {
		return nil, fmt.Errorf("controller: DCL snapshot: %w", err)
	}
	svc, err := dcl.NewVendorInfoService(ctx, opts)
	if err != nil {
		return nil, err
	}
	c.vendorInfo = svc
	return svc, nil
}

func (c *Controller) closeVendorInfo() {
	c.vendorMu.Lock()
	svc := c.vendorInfo
	c.vendorInfo = nil
	c.vendorMu.Unlock()
	if svc != nil {
		svc.Close()
	}
}

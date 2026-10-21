// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/controller/client"
)

// Discover finds the commissionable devices id selects on the site's
// DNS-SD answers until the first one is found or timeout passes on the
// site's clock — matter.js Peers.discover({ timeout })
// (node/client/Peers.ts:204-206, a ContinuousDiscovery) as far as one
// result goes. With a fake site clock it runs under [Site.Run]; a search
// that finds nothing returns an empty slice.
func (c *Controller) Discover(ctx context.Context, id discovery.Identifier, timeout time.Duration) ([]discovery.Device, error) {
	s := discovery.NewScanner(c.Names())
	defer s.Close()
	var found []discovery.Device
	err := c.site.Run(ctx, func(ctx context.Context) error {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		t := c.site.clk.AfterFunc(timeout, cancel)
		defer t.Stop()
		_, err := s.FindCommissionable(ctx, id, func(d discovery.Device) {
			found = append(found, d)
			cancel()
		})
		return err
	})
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return found, err
	}
	return found, nil
}

// commissionDiscoveryTimeout is CommissioningDiscovery's default timeout,
// the spec's minimum commissioning window
// (behavior/system/controller/discovery/CommissioningDiscovery.ts:40-45).
const commissionDiscoveryTimeout = 3 * time.Minute

// CommissionNode commissions d through the controller's client node
// registry — matter.js controller.peers.commission({ passcode,
// discriminator }) (node/client/Peers.ts:211-213, a CommissioningDiscovery)
// as MockSite.addCommissionedPair runs it: the device is discovered by its
// long discriminator, the registry's node for it (client.Registry.ForDevice)
// is commissioned (client.Registry.Commission) with d's passcode unless o
// names one, and started. With a fake site clock it runs under [Site.Run].
func (c *Controller) CommissionNode(ctx context.Context, d *Device, o client.CommissionOptions) (*client.Node, error) {
	found, err := c.Discover(ctx, discovery.ByLongDiscriminator(d.Discriminator()), commissionDiscoveryTimeout)
	if err != nil {
		return nil, fmt.Errorf("fabrictest: discover device %d: %w", d.index, err)
	}
	if len(found) == 0 {
		return nil, fmt.Errorf("fabrictest: device %d was not discovered", d.index)
	}
	reg := c.Registry()
	n, err := reg.ForDevice(ctx, found[0])
	if err != nil {
		return nil, err
	}
	if o.Passcode == 0 {
		o.Passcode = d.Passcode()
	}
	if err := c.site.Run(ctx, func(ctx context.Context) error { return reg.Commission(ctx, n, o) }); err != nil {
		return n, fmt.Errorf("fabrictest: commission device %d: %w", d.index, err)
	}
	return n, nil
}

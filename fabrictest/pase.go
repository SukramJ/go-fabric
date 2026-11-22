// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest

import (
	"context"
	"fmt"
	"net"

	"thingfabric.dev/go-fabric/client/exchange"
	"thingfabric.dev/go-fabric/client/interaction"
	"thingfabric.dev/go-fabric/controller/commissioner"
)

// PASE establishes a PASE session from the controller to d with d's
// passcode — the session a commissioner holds before AddNOC, with no
// fabric — and returns a pair whose wire helpers speak over it and which
// has no peer (internal/fabrictestseam.PASESession is the session). It is
// the session matter.js's tests build with
// ProtocolMocks.NodeSession({ fabric: undefined }) (CommandInvokeResponseTest.ts
// "refuses a fabric-scoped command over a PASE session"), established
// over the wire: the commissioner's EstablishPASE
// (controller/commissioner.Commissioner.EstablishPASE), so d's
// commissioning window must be open. The wire helpers accept the pair;
// [Pair.Commissioned] is false. With a fake site clock it runs under
// [Site.Run].
func (c *Controller) PASE(ctx context.Context, d *Device) (*Pair, error) {
	// One stack for the whole call, read under the lock a Restart takes
	// (R2-P6-conc-2509).
	c.mu.RLock()
	com, mgr := c.com, c.mgr
	c.mu.RUnlock()
	var s *exchange.Session
	err := c.site.Run(ctx, func(ctx context.Context) error {
		var err error
		s, _, err = com.EstablishPASE(ctx, commissioner.PASEOptions{Passcode: d.Passcode(), Addresses: []*net.UDPAddr{d.Addr()}})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("fabrictest: PASE to device %d: %w", d.index, err)
	}
	// The interaction runs on the controller's host clock, which counts
	// its work in the controller's in-flight group as the rest of the
	// stack's (R2-P6-conc-2513): the site does not move a fake clock while
	// it has work in hand.
	ix := interaction.Bind(interaction.SessionProvider(mgr, s), interaction.Config{Logger: c.opts.Logger, Clock: c.site.hostClock(c.group)})
	return &Pair{Controller: c, Device: d, interaction: ix, paseSession: s}, nil
}

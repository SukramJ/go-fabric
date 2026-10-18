// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest

import (
	"context"
	"fmt"
	"net"

	"github.com/SukramJ/go-fabric/controller/commissioner"
	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/controller/interaction"
)

// PASE establishes a PASE session from the controller to d with d's
// passcode — the session a commissioner holds before AddNOC, with no
// fabric — and returns a pair whose Interaction speaks over it and whose
// Peer is nil. It is the session matter.js's tests build with
// ProtocolMocks.NodeSession({ fabric: undefined }) (CommandInvokeResponseTest.ts
// "refuses a fabric-scoped command over a PASE session"), established
// over the wire: the commissioner's EstablishPASE
// (controller/commissioner.Commissioner.EstablishPASE), so d's
// commissioning window must be open. The wire helpers accept the pair;
// [Pair.Commissioned] is false. With a fake site clock it runs under
// [Site.Run].
func (c *Controller) PASE(ctx context.Context, d *Device) (*Pair, error) {
	var s *exchange.Session
	err := c.site.Run(ctx, func(ctx context.Context) error {
		var err error
		s, _, err = c.com.EstablishPASE(ctx, commissioner.PASEOptions{Passcode: d.Passcode(), Addresses: []*net.UDPAddr{d.Addr()}})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("fabrictest: PASE to device %d: %w", d.index, err)
	}
	ix := interaction.Bind(interaction.SessionProvider(c.mgr, s), interaction.Config{Logger: c.opts.Logger, Clock: c.site.clk})
	return &Pair{Controller: c, Device: d, Interaction: ix, PASESession: s}, nil
}

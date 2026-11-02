// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest

import (
	"context"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/client/peer"
	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/internal/fabrictestseam"
	"github.com/SukramJ/go-fabric/mdns"
)

// init installs the seams the module's own tests reach below the harness
// (internal/fabrictestseam, review 2026-11 W5-ex-12): a site's device's
// advertiser and endpoints, a controller's client machinery and a pair's
// transport. They are not this package's API.
func init() {
	fabrictestseam.Advertiser = func(d any) mdns.Advertiser {
		dev := d.(*Device)
		dev.mu.Lock()
		defer dev.mu.Unlock()
		return dev.adv
	}
	fabrictestseam.Endpoints = func(d any) []*endpoint.Endpoint {
		if t := d.(*Device).Node().Bridge().Topology(); t != nil {
			return t.Endpoints
		}
		return nil
	}
	fabrictestseam.Exchanges = func(c any) *exchange.Manager {
		ctl := c.(*Controller)
		ctl.mu.RLock()
		defer ctl.mu.RUnlock()
		return ctl.mgr
	}
	fabrictestseam.Names = func(c any) *discovery.Names {
		ctl := c.(*Controller)
		ctl.mu.RLock()
		defer ctl.mu.RUnlock()
		return ctl.names
	}
	fabrictestseam.Peers = func(c any) *peer.Set {
		ctl := c.(*Controller)
		ctl.mu.RLock()
		defer ctl.mu.RUnlock()
		return ctl.set
	}
	fabrictestseam.Resumption = func(c any) *peer.MemoryResumptionStore {
		ctl := c.(*Controller)
		ctl.mu.RLock()
		defer ctl.mu.RUnlock()
		return ctl.resume
	}
	fabrictestseam.Subscriptions = func(c any) *interaction.Subscriptions {
		ctl := c.(*Controller)
		ctl.mu.RLock()
		defer ctl.mu.RUnlock()
		return ctl.subs
	}
	fabrictestseam.Peer = func(p any) *peer.Peer { return p.(*Pair).peer }
	fabrictestseam.Interaction = func(p any) *interaction.Interaction { return p.(*Pair).interaction }
	fabrictestseam.PASESession = func(p any) *exchange.Session { return p.(*Pair).paseSession }
	fabrictestseam.Connect = func(ctx context.Context, p any) (*interaction.Interaction, error) {
		return connect(ctx, p.(*Pair))
	}
	fabrictestseam.Exchange = func(ctx context.Context, p any, opcode uint8, payload []byte) (*exchange.Message, error) {
		return rawExchange(ctx, p.(*Pair), opcode, payload)
	}
}

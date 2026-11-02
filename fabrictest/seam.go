// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest

import (
	"context"
	"fmt"

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
		dev := asDevice(d)
		dev.mu.Lock()
		defer dev.mu.Unlock()
		return dev.adv
	}
	fabrictestseam.Endpoints = func(d any) []*endpoint.Endpoint {
		if t := asDevice(d).Node().Bridge().Topology(); t != nil {
			return t.Endpoints
		}
		return nil
	}
	fabrictestseam.Exchanges = func(c any) *exchange.Manager {
		ctl := asController(c)
		ctl.mu.RLock()
		defer ctl.mu.RUnlock()
		return ctl.mgr
	}
	fabrictestseam.Names = func(c any) *discovery.Names {
		ctl := asController(c)
		ctl.mu.RLock()
		defer ctl.mu.RUnlock()
		return ctl.names
	}
	fabrictestseam.Peers = func(c any) *peer.Set {
		ctl := asController(c)
		ctl.mu.RLock()
		defer ctl.mu.RUnlock()
		return ctl.set
	}
	fabrictestseam.Resumption = func(c any) *peer.MemoryResumptionStore {
		ctl := asController(c)
		ctl.mu.RLock()
		defer ctl.mu.RUnlock()
		return ctl.resume
	}
	fabrictestseam.Subscriptions = func(c any) *interaction.Subscriptions {
		ctl := asController(c)
		ctl.mu.RLock()
		defer ctl.mu.RUnlock()
		return ctl.subs
	}
	fabrictestseam.Peer = func(p any) *peer.Peer { return asPair(p).peer }
	fabrictestseam.Interaction = func(p any) *interaction.Interaction { return asPair(p).interaction }
	fabrictestseam.PASESession = func(p any) *exchange.Session { return asPair(p).paseSession }
	fabrictestseam.Connect = func(ctx context.Context, p any) (*interaction.Interaction, error) {
		return connect(ctx, asPair(p))
	}
	fabrictestseam.Exchange = func(ctx context.Context, p any, opcode uint8, payload []byte) (*exchange.Message, error) {
		return rawExchange(ctx, asPair(p), opcode, payload)
	}
}

// asDevice, asController and asPair take a seam's argument back to
// fabrictest's own type; another value is a test's programming error.
func asDevice(v any) *Device {
	d, ok := v.(*Device)
	if !ok {
		panic(fmt.Sprintf("fabrictestseam: %T is not a *fabrictest.Device", v))
	}
	return d
}

func asController(v any) *Controller {
	c, ok := v.(*Controller)
	if !ok {
		panic(fmt.Sprintf("fabrictestseam: %T is not a *fabrictest.Controller", v))
	}
	return c
}

func asPair(v any) *Pair {
	p, ok := v.(*Pair)
	if !ok {
		panic(fmt.Sprintf("fabrictestseam: %T is not a *fabrictest.Pair", v))
	}
	return p
}

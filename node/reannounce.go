// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"

	"github.com/SukramJ/go-fabric/mdns"
)

// startReannounce starts the advertiser's re-announce loop when it has one
// (mdns.Reannouncer) and Network.DisableReannounce is not set (issue
// #119). The loop is this module's own device, not a matter.js port:
// matter.js re-sends answers on a query, never unsolicited
// (mdns.Reannouncer's doc, MdnsServer.ts:158-164 at 0ceb202d). It runs
// under the node's lifetime; Stop ends it before the advertiser closes.
// Called with n.mu held.
func (n *Node) startReannounce() {
	r, ok := n.advertiser.(mdns.Reannouncer)
	if !ok || n.cfg.Network.DisableReannounce {
		return
	}
	interval := n.cfg.Network.ReannounceInterval
	if interval == 0 {
		interval = DefaultReannounceInterval
	}
	// The trigger channel is not kept: TriggerReannounce goes through the
	// advertiser's own method, which also works with the loop off.
	n.stopReannounce, _ = r.StartReannounceLoop(n.life, interval)
}

// stopReannounceLoop ends the loop startReannounce began, once. Called
// with n.mu held.
func (n *Node) stopReannounceLoop() {
	if n.stopReannounce != nil {
		n.stopReannounce()
		n.stopReannounce = nil
	}
}

// TriggerReannounce re-publishes the node's DNS-SD records now — for a
// host that observes a link change (interface up, address assigned) and
// should not wait for the next periodic re-announce. It forwards to the
// advertiser's mdns.Reannouncer.TriggerReannounce and is a no-op on an
// advertiser that is not one (mdns.Noop, a custom Advertiser).
func (n *Node) TriggerReannounce(ctx context.Context) {
	if r, ok := n.advertiser.(mdns.Reannouncer); ok {
		r.TriggerReannounce(ctx)
	}
}

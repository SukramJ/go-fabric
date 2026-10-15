// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns

import (
	"context"
	"time"
)

// Reannouncer is an [Advertiser] that re-publishes its records on its
// own: periodically, and on demand after a link change. It is optional —
// [Advertiser] does not carry it — so a holder of the interface (the node
// facade) starts the loop on an advertiser that has one and skips it on
// one that does not. [Zeroconf] implements it.
//
// The loop is this module's own device, not a matter.js port: matter.js
// sends no unsolicited periodic announcement, its MdnsServer re-sends an
// answer as multicast on a query once a quarter of the record's TTL has
// passed since it was last multicast (matter.js
// packages/protocol/src/mdns/MdnsServer.ts:158-164 at 0ceb202d). The
// upstream library [Zeroconf] runs on probes and announces once per
// Register and never again, so the periodic re-publish stands in for it.
type Reannouncer interface {
	// StartReannounceLoop re-publishes every active record every interval
	// until cancel is called or ctx ends; a value sent on trigger
	// re-publishes at once ([Zeroconf.StartReannounceLoop]).
	StartReannounceLoop(ctx context.Context, interval time.Duration) (cancel func(), trigger chan<- struct{})
	// TriggerReannounce re-publishes every active record now, outside the
	// periodic cadence ([Zeroconf.TriggerReannounce]).
	TriggerReannounce(ctx context.Context)
}

// Enabled is an [Advertiser] that states whether it puts records on the
// network, so a diagnostic need not assert the concrete type. It is
// optional; [IsEnabled] reads it.
type Enabled interface {
	// Enabled is true when the advertiser publishes on the network.
	Enabled() bool
}

// IsEnabled reports whether a publishes on the network: its Enabled()
// when it implements [Enabled], true otherwise (an advertiser that does
// not say is taken at its word that it advertises), false for nil.
func IsEnabled(a Advertiser) bool {
	if a == nil {
		return false
	}
	if e, ok := a.(Enabled); ok {
		return e.Enabled()
	}
	return true
}

// Enabled implements [Enabled]: a Zeroconf advertiser publishes on the
// network.
func (z *Zeroconf) Enabled() bool { return true }

// Enabled implements [Enabled]: a Noop advertiser publishes nothing.
func (n *Noop) Enabled() bool { return false }

var (
	_ Reannouncer = (*Zeroconf)(nil)
	_ Enabled     = (*Zeroconf)(nil)
	_ Enabled     = (*Noop)(nil)
)

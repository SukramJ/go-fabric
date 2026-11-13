// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

import (
	"log/slog"
	"net/netip"

	"github.com/SukramJ/go-fabric/client/discovery"
)

// HintAddresses hands the running connection process addresses learned
// outside DNS-SD — a commissioner reads them from the device over the
// commissioning session while the node's operational record has not
// appeared yet — and returns how many it queued. Each is attempted as a
// discovered address is, beside whatever discovery delivers: an address
// already attempted or queued is passed over, one with an invalid address
// or port 0 too, and without a running process (none started, or it
// ended) nothing is queued. CASE authenticates the node, so an address
// needs no trust of its own: at a wrong one the attempt finds nobody.
//
// A hinted address ranks after the discovered ones (shiftLocked), and
// while only hinted attempts are in flight the next discovered address
// starts without the stagger (hintBesideLocked): discovery, when it
// answers, takes over as it would without the hint. An address discovery
// advertises too is a discovered one from then on (addAddressLocked).
//
// This module's: neither matter.js nor connectedhomeip feeds an address
// into the operational connection other than through discovery (and, in
// connectedhomeip, the commissioning session's own IP address).
func (p *Peer) HintAddresses(addrs ...netip.AddrPort) int {
	p.mu.Lock()
	c := p.connecting
	p.mu.Unlock()
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ctx.Err() != nil || c.session != nil {
		return 0
	}
	n := 0
	for _, ap := range addrs {
		if !ap.Addr().IsValid() || ap.Port() == 0 {
			continue
		}
		a := discovery.Address{IP: ap.Addr().Unmap(), Port: ap.Port()}
		if c.queuedLocked(a) {
			continue
		}
		c.hinted[a.AddrPort()] = true
		for _, v := range c.expand(a) {
			c.pendingAddLocked(v)
		}
		n++
		p.log.Info("matter.controller.peer.address_hinted", slog.String("address", a.String()))
	}
	return n
}

// hintedLocked reports whether a is a hinted address discovery has not
// advertised (HintAddresses).
func (c *connection) hintedLocked(a discovery.Address) bool { return c.hinted[a.AddrPort()] }

// hintBesideLocked reports whether the next pending address starts
// without the stagger: every attempt in flight is at a hinted address and
// a discovered address is pending. A hint is a guess the device made
// before it had announced itself; the address discovery delivers is the
// one matter.js would attempt, and it does not wait DelayBeforeNextAddress
// behind the guess.
func (c *connection) hintBesideLocked() bool {
	if len(c.attempts) == 0 {
		return false
	}
	for _, att := range c.attempts {
		if !c.hintedLocked(att.addr) {
			return false
		}
	}
	for _, x := range c.pending {
		if !c.hintedLocked(x.addr) {
			return true
		}
	}
	return false
}

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
// discovered address is, beside whatever discovery delivers, and its
// attempt goes on retrying as a discovered address's does until the
// process ends or deletes it (connect). An address is queued once per
// connection process: one hinted before — whether its attempt still runs
// or was deleted since — is passed over, and so is an address attempted
// or queued already, an invalid address, port 0 and an IPv6 link-local
// address without a zone. Without a running process (none started, or it
// ended) nothing is queued. CASE authenticates the node, so an address
// needs no trust of its own: at a wrong one the attempt finds nobody.
//
// A hinted address ranks after the discovered ones (shiftLocked), and
// while only hinted attempts are in flight the first discovered address
// starts without the stagger, once per process (hintBesideLocked):
// discovery, when it answers, takes over as it would without the hint.
// An address discovery advertises too is a discovered one from then on
// (addAddressLocked).
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
		ip := ap.Addr().Unmap()
		if !ip.IsValid() || ap.Port() == 0 || (ip.Is6() && ip.IsLinkLocalUnicast() && ip.Zone() == "") {
			continue
		}
		a := discovery.Address{IP: ip, Port: ap.Port()}
		if _, seen := c.hinted[a.AddrPort()]; seen || c.queuedLocked(a) {
			continue
		}
		c.hinted[a.AddrPort()] = hintActive
		for _, v := range c.expand(a) {
			c.pendingAddLocked(v)
		}
		n++
		p.log.Info("matter.controller.peer.address_hinted", slog.String("address", a.String()))
	}
	return n
}

// hintState is what the process knows of a hinted address.
type hintState uint8

const (
	// hintActive: hinted, and not advertised by discovery.
	hintActive hintState = iota + 1
	// hintDiscovered: hinted, then advertised: a discovered address now;
	// kept so a later hint passes it over.
	hintDiscovered
)

// hintedLocked reports whether a is a hinted address discovery has not
// advertised (HintAddresses).
func (c *connection) hintedLocked(a discovery.Address) bool {
	return c.hinted[a.AddrPort()] == hintActive
}

// hintBesideLocked reports whether the next pending address starts
// without the stagger: the exemption is unused in this process, every
// attempt in flight is at a hinted address and a discovered address is
// pending. A hint is a guess the device made before it had announced
// itself; the address discovery delivers is the one matter.js would
// attempt, and it does not wait DelayBeforeNextAddress behind the guess.
// One discovered address starts this way, as one starts beside the anchor
// (anchorBesideLocked); the addresses after it keep the stagger.
func (c *connection) hintBesideLocked() bool {
	if c.hintBesideUsed || len(c.attempts) == 0 {
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

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
// or queued already, an invalid address, port 0, an IPv6 link-local
// address without a zone and an address that is no unicast destination
// (unicastDestination: unspecified, multicast, the IPv4 limited broadcast).
// Without a running process (none started, or it ended) nothing is queued.
// CASE authenticates the node, so an address needs no trust of its own: at
// a wrong one the attempt finds nobody. It does send there, though, so a
// process takes at most [MaxHintedAddresses] hinted addresses, over all
// calls; the ones past it are passed over, which the process logs once.
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
	n, over := 0, 0
	for _, ap := range addrs {
		ip := ap.Addr().Unmap()
		if !unicastDestination(ip) || ap.Port() == 0 || (ip.Is6() && ip.IsLinkLocalUnicast() && ip.Zone() == "") {
			continue
		}
		a := discovery.Address{IP: ip, Port: ap.Port()}
		if _, seen := c.hinted[a.AddrPort()]; seen || c.queuedLocked(a) {
			continue
		}
		if len(c.hinted) >= MaxHintedAddresses {
			over++
			continue
		}
		c.hinted[a.AddrPort()] = hintActive
		for _, v := range c.expand(a) {
			c.pendingAddLocked(v)
		}
		n++
		p.log.Info("matter.controller.peer.address_hinted", slog.String("address", a.String()))
	}
	if over > 0 && !c.hintLimitLogged {
		c.hintLimitLogged = true
		p.log.Warn("matter.controller.peer.address_hint_limit", slog.Int("max", MaxHintedAddresses), slog.Int("dropped", over))
	}
	return n
}

// MaxHintedAddresses bounds the hinted addresses of one connection process
// ([Peer.HintAddresses]). The addresses are a device's own statement that
// nothing verifies, and every one becomes a CASE target:
// without a bound a commissionee steers one Sigma1 per address it names at
// hosts of its choosing, and grows the candidate list for as long as the
// connection runs.
//
// 96 is what one GeneralDiagnostics NetworkInterfaces attribute — the one
// source of hints this module has, the commissioner's reconnect probe —
// can hold: at most 8 interfaces, each with at most 8 IPv6 and 4 IPv4
// addresses (matter.js packages/model/src/standard/elements/
// general-diagnostics.element.ts:27, :195 and :191 at db021da0, the
// constraints "max 8", "max 8" and "max 4"; this module's copy is the
// generated cluster/spec/generaldiagnostics definition, and the
// commissioner's TestReconnectProbeBoundsReportedAddresses holds the
// product against it). A device that reports other addresses on a later
// probe does not get a second allowance: the bound is per process.
const MaxHintedAddresses = 96

// unicastDestination reports whether ip can be the destination of a
// datagram to one host: not the unspecified address, not a multicast
// address, and in IPv4 neither in 0.0.0.0/8 ("this network") nor in
// 240.0.0.0/4 (reserved; the limited broadcast 255.255.255.255 is its
// last address). A directed broadcast cannot be told from a host address
// without the subnet and is not looked for.
func unicastDestination(ip netip.Addr) bool {
	if !ip.IsValid() || ip.IsUnspecified() || ip.IsMulticast() {
		return false
	}
	if ip.Is4() {
		first := ip.As4()[0]
		return first != 0 && first < 240
	}
	return true
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

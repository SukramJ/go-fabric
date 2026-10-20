// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"log/slog"

	"github.com/SukramJ/go-fabric/mdns"
)

// MDNS configures the DNS-SD advertiser the node builds when
// [Config].Advertiser is nil. The zero value advertises on the network.
type MDNS struct {
	// Disabled publishes nothing (mdns.NewNoop): no commissioner discovers
	// the node, which a controller then reaches by address alone.
	Disabled bool
	// HostName, when set, is the SRV target of every record
	// (mdns.Zeroconf.HostName, without ".local"); empty is the name the
	// advertiser derives from the interface's MAC.
	HostName string
}

// MDNSFinding is one result of [Node.DiagnoseMDNS] (mdns.Finding).
type MDNSFinding = mdns.Finding

// MDNSService is one record the node advertises ([Node.MDNSServices],
// mdns.Service).
type MDNSService = mdns.Service

// defaultAdvertiser is the advertiser of a nil Config.Advertiser: Zeroconf
// with the subtype responder, or Noop when Network.MDNS.Disabled. The
// subtype responder is what answers the `_L<discriminator>._sub` browse
// every commissioner filtering by discriminator sends (chip-tool's
// `pairing onnetwork-long`, Apple Home, Google Home); a responder that
// cannot bind leaves the primary records publishing and is logged.
//
// Mirrors matter.js packages/node/src/behavior/system/network/NetworkServer.ts:207-209
// (discoveryCapabilities defaults to onIpNetwork) and
// packages/node/src/node/server/ServerEnvironment.ts:155 with
// ServerNetworkRuntime.ts:88-97: a ServerNode loads the MdnsService and
// advertises through it unless the host configures otherwise.
func (n *Node) defaultAdvertiser() mdns.Advertiser {
	m := n.cfg.Network.MDNS
	if m.Disabled {
		return mdns.NewNoop()
	}
	zc := mdns.NewZeroconf()
	zc.HostName = m.HostName
	responder, err := mdns.NewSubtypeResponder(n.logger)
	if err != nil {
		n.logger.Warn("mdns.subtype_responder", slog.String("err", err.Error()),
			slog.String("hint", "discriminator-filtered browse will find nothing"))
		return zc
	}
	responder.Start(n.life)
	// Zeroconf.Close closes the attached responder.
	zc.AttachSubtypeResponder(responder)
	return zc
}

// resolveAdvertiser builds the node's own advertiser for a nil
// Config.Advertiser.
func (n *Node) resolveAdvertiser() {
	if n.advertiser != nil {
		return
	}
	n.advertiser = n.defaultAdvertiser()
	n.ownsAdvertiser = true
	n.cfg.Advertiser = n.advertiser
}

// MDNSEnabled reports whether the node's advertiser publishes on the
// network (mdns.IsEnabled): false for Noop.
func (n *Node) MDNSEnabled() bool { return mdns.IsEnabled(n.advertiser) }

// MDNSServices returns the records the node's advertiser currently
// publishes (mdns.Advertiser.Active).
func (n *Node) MDNSServices() []MDNSService { return n.advertiser.Active() }

// DiagnoseMDNS inspects the records the node currently advertises and
// reports what would keep a controller from finding or commissioning it
// (mdns.Diagnose); nil when the advertiser publishes nothing on purpose
// ([MDNS].Disabled, or a host's Noop).
func (n *Node) DiagnoseMDNS() []MDNSFinding {
	if !n.MDNSEnabled() {
		return nil
	}
	return mdns.Diagnose(n.advertiser.Active())
}

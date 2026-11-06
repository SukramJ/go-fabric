// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"github.com/SukramJ/go-fabric/internal/netwatch"
	"github.com/SukramJ/go-fabric/mdns"
)

// netwatchDefaultInterval is the interface check interval of a zero
// [Network.InterfaceCheck].
const netwatchDefaultInterval = netwatch.DefaultInterval

// MDNS configures the DNS-SD advertiser the node builds when
// [Config].Advertiser is nil. The zero value advertises on the network.
type MDNS struct {
	// Off publishes nothing (mdns.NewNoop): no commissioner discovers the
	// node, which a controller then reaches by address alone.
	Off bool
	// HostName, when set, is the SRV target of every record
	// (mdns.Responder.HostName, without ".local"): a host name the OS
	// responder owns and publishes the address records of. Empty is the
	// name the advertiser derives from the interface's MAC, whose address
	// records it publishes itself.
	HostName string
}

// MDNSFinding is one result of [Node.DiagnoseMDNS] (mdns.Finding).
type MDNSFinding = mdns.Finding

// MDNSService is one record the node advertises ([Node.MDNSServices],
// mdns.Service).
type MDNSService = mdns.Service

// defaultAdvertiser is the advertiser of a nil Config.Advertiser: the
// module's mDNS responder, or Noop when Network.MDNS.Off. The responder
// publishes every record of a service — the subtype PTRs the
// `_L<discriminator>._sub` browse of chip-tool, Apple Home and Google Home
// asks for included — and binds its sockets on the first Publish, so New
// starts nothing.
//
// Mirrors matter.js packages/node/src/behavior/system/network/NetworkServer.ts:207-209
// (discoveryCapabilities defaults to onIpNetwork) and
// packages/node/src/node/server/ServerEnvironment.ts:155 with
// ServerNetworkRuntime.ts:88-97: a ServerNode loads the MdnsService and
// advertises through it unless the host configures otherwise.
func (n *Node) defaultAdvertiser() mdns.Advertiser {
	m := n.cfg.Network.MDNS
	if m.Off {
		return mdns.NewNoop()
	}
	r := mdns.NewResponder()
	r.SetLogger(n.logger)
	r.HostName = m.HostName
	r.SetInterfaceCheckInterval(n.cfg.Network.interfaceCheck())
	return r
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

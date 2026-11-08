// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"slices"

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
	// Interfaces, when non-empty, restricts the advertiser to the named
	// host interfaces ("eth0", "br30"): it announces and answers on them
	// alone, with their addresses. Empty advertises on every
	// multicast-capable interface. The default filter still applies to a
	// named interface — a container or virtualisation bridge (docker0,
	// hassio, veth…, mdns.DefaultInterfaceFilter) stays out when named. A
	// name the host does not have is allowed, as interfaces come and go:
	// nothing is announced on it until it appears (the interface check),
	// and [Node.DiagnoseMDNS] names it. The UDP listen socket is not
	// scoped by it ([Network].Listen takes a host address). The case for
	// it is a dual-homed host whose gateway relays mDNS between its
	// networks: the controllers on one network cache the other network's
	// address as well; advertising on the controllers' network alone
	// keeps it out. Ahead of matter.js, whose MdnsServer publishes on
	// every multicast interface (BD-matterjs-mdns-interface-allowlist).
	Interfaces []string
}

// interfaceFilter is the responder's InterfaceFilter for the
// configuration: the default container filter, and with Interfaces every
// interface not named as well.
func (m MDNS) interfaceFilter() func(string) bool {
	if len(m.Interfaces) == 0 {
		return mdns.DefaultInterfaceFilter
	}
	allow := slices.Clone(m.Interfaces)
	return func(name string) bool {
		return mdns.DefaultInterfaceFilter(name) || !slices.Contains(allow, name)
	}
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
	r.InterfaceFilter = m.interfaceFilter()
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
// (mdns.Diagnose), and with [MDNS].Interfaces each named interface the
// host does not have, the default filter keeps out or that carries no
// advertised address (mdns.DiagnoseInterfaces); nil when the advertiser
// publishes nothing on purpose ([MDNS].Off, or a host's Noop). Errors
// come first.
func (n *Node) DiagnoseMDNS() []MDNSFinding {
	if !n.MDNSEnabled() {
		return nil
	}
	out := mdns.Diagnose(n.advertiser.Active())
	if n.ownsAdvertiser {
		out = append(out, mdns.DiagnoseInterfaces(n.cfg.Network.MDNS.Interfaces)...)
		rank := func(f MDNSFinding) int {
			if f.Severity == mdns.SeverityError {
				return 0
			}
			return 1
		}
		slices.SortStableFunc(out, func(a, b MDNSFinding) int { return rank(a) - rank(b) })
	}
	return out
}

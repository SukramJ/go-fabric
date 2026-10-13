// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package discovery is the controller's DNS-SD browser: it finds
// commissionable nodes on the network and follows the operational
// instance of a commissioned one (ADR 0020, stage 1).
//
// It ports the shape of matter.js's discovery stack at 0ceb202d:
//
//   - [Names] and [Name] are the shared record cache, matter.js
//     DnssdNames and DnssdName (packages/general/src/net/dns-sd/).
//   - [Service] is the live view of one DNS-SD service name, matter.js
//     IpService; [Service.Resolve] runs IpServiceResolution.
//   - [Solicitor] is the query loop with its retry schedule, matter.js
//     DnssdSolicitor / QueryMulticaster.
//   - [DiscoveryData] and [ParseDiscoveryData] are the TXT parser of
//     packages/protocol/src/common/Scanner.ts.
//   - [Scanner] is matter.js's CommissionableMdnsScanner
//     (packages/protocol/src/mdns/).
//   - [Names.Service] gives the operational service of one node, the way
//     a matter.js Peer owns an IpService for its
//     `<CFID>-<NODEID>._matter._tcp.local` name.
//
// matter.js runs this stack on one JavaScript event loop. The Go port
// keeps that model: every [Names] owns one goroutine that holds all cache,
// service, solicitor and scanner state; the exported methods hand their
// work to it and wait. Work matter.js defers to a microtask (record-change
// notification, deletion of an unused name) runs on the loop right after
// the operation that caused it, and work it defers to a macrotask (query
// coalescing, service-change coalescing) runs after that. User callbacks
// never run on the loop.
//
// The package imports mdns and the standard library only; it does not
// import bridge/, node/, device/ or endpoint/ (ADR 0020).
package discovery

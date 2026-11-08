// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package topology is the network graph of the controller's commissioned
// nodes: a port of matterjs-server's NetworkTopologyService at 0bd06f22
// (github.com/matter-js/matterjs-server,
// packages/ws-controller/src/controller/NetworkTopologyService.ts) and the
// derivation it shares with its dashboard
// (packages/ws-client/src/topology/topology-utils.ts), whose wire types
// (packages/ws-client/src/models/model.ts) name this package's.
//
// matter.js has no topology service; its controller's network behaviour
// holds the subscription state only. This package is ahead of matter.js
// and registered as such in notes/parity/by_design.md.
//
// [Build] is the pure derivation: from one [Image] per node — the typed
// NetworkCommissioning, GeneralDiagnostics, ThreadNetworkDiagnostics and
// WiFiNetworkDiagnostics states of its root endpoint, read with [ImageOf]
// — it yields a [Topology]: a [Node] per commissioned node, a
// border_router [Node] for every Thread neighbour the border-router
// registry knows (thread/brdiscovery, [Config].BorderRouters) and a
// thread_unknown [Node] for every other one no commissioned node matches,
// a wifi_ap [Node] per access point, and a [Connection] per
// Thread pair (both directions folded, [Direction]) and per Wi-Fi station.
// [Service] keeps it current over a controller.Controller: a trailing
// debounce after a node's change, a periodic rebuild, [Service.Refresh]
// re-reading the diagnostics of every online node, and
// [Service.OnChanged] observers called only when the graph moved.
// [Changes] carries the nodes' hooks to the service; it is installed on
// the controller.Config before controller.New.
// [Service.AddSource] merges nodes the controller has not commissioned —
// a [Source] the service builds from but never reads.
//
// The border-router registry is thread/brdiscovery's, built on the
// controller's DNS-SD cache: a neighbour it knows by extended address is
// a border_router node (id br_<EXTADDR>, role router, its network, host,
// vendor and model names), and its network names label the Thread nodes
// and externals by extended PAN id.
//
// Left out: the route-table supplement (an edge from a RouteTable entry,
// via_route_table, path_cost), and feeding thread/otbr's border-router
// list from the registry, a separate decision. Each pin lift of
// matterjs-server re-reads both.
package topology

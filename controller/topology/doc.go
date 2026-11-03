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
// thread_unknown [Node] for every Thread neighbour no commissioned node
// matches, a wifi_ap [Node] per access point, and a [Connection] per
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
// Left out, as go-fabric-server's port leaves them out: the border-router
// registry (matterjs-server's kind border_router, its br_ ids and the
// network name by extended PAN id) and the route-table supplement (an
// edge from a RouteTable entry, via_route_table, path_cost). The registry
// browses _meshcop over mDNS; this module's border-router client is the
// OTBR REST client of thread/otbr (ADR 0027 decision 1) and no _meshcop
// browse exists. A neighbour that is a border router is therefore a
// thread_unknown node. Each pin lift of matterjs-server re-reads both.
package topology

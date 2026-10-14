// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package client is the controller's image of a remote node (ADR 0020,
// stage 4, first half): what the node reports, kept as endpoints and
// clusters, cached and persisted, and kept live by one sustained
// subscription. It is the port of matter.js packages/node/src/ at
// 0ceb202d: node/ClientNode.ts and node/ClientNodeLifecycle.ts (the node
// and its connection state), node/client/ClientStructure.ts (the tree the
// peer's Descriptor describes), node/client/PeerBehavior.ts (the shape of
// a cluster), node/client/ClientNodeInteraction.ts (read, subscribe,
// write and invoke through the image), node/client/ClientEventEmitter.ts
// (events), the subscription and connection-state half of
// behavior/system/network/NetworkClient.ts, storage/client/ (the cache
// and its persistence) and node/NodePhysicalProperties.ts with
// protocol/src/peer/PhysicalDeviceProperties.ts (the physical properties
// and the subscription interval bounds).
//
// # The data model
//
// ADR 0020 decision 6: the controller has no device model of its own. A
// [Node] knows nothing of a remote node but what the node reports — the
// Descriptor's DeviceTypeList, ServerList and PartsList decide the tree, a
// cluster's global attributes decide its shape — and its typed side is
// the module's one data model: the generated cluster/spec definitions and
// the schema/ tables. matter.js builds a TypeScript class per discovered
// cluster shape (PeerBehavior.ts:105-139); Go has no run-time types, so a
// [Cluster] carries the analysis of that shape (PeerBehavior.ts:422-467)
// and its values stay [spec.Node], decoded on access through the
// definition ([Cluster.Decoded], [spec.DecodeValue]).
//
// A cluster is known when spec.Lookup has its definition (this package
// links every generated definition), named when only schema.ClusterName
// has it — it carries its name and untyped values — and unknown otherwise,
// named "Cluster$<hex>" as matter.js names it (PeerBehavior.ts:399-401,
// :426-427).
//
// # The registry
//
// [Registry] is matter.js Peers (node/client/Peers.ts) with the per-node
// half of CommissioningClient and NetworkClient's fabric-label
// reconciliation: it restores every node a [Store] holds, allocates the
// local ids, finds or creates a node for a discovered device
// ([Registry.ForDevice]) or a peer address ([Registry.ForAddress]),
// commissions and decommissions through the node one fabric operation at
// a time, culls expired commissionable nodes and addresses, and reacts to
// the peer's BasicInformation leave, shutDown and startUp. A node's
// commissioning record ([Record]) is stored beside its settings.
// controller/client/sqlstore is a durable [Store].
//
// # Not here
//
// Groups, ICD clients, split commissioning, matter.js's discovery runners
// (the host runs discovery.Scanner and hands the registry a device), TCP
// and the transport preference, matter.js's remote API
// (applyWireChanges), writes through the cache (RemoteWriter.ts) and
// generated client behaviour types. Writes go to the peer; the image
// follows when the peer reports the new value.
package client

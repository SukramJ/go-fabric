// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package node is the facade over the bridge's bring-up: one [Config], one
// [New], [Node.Start] / [Node.Stop] / [Node.Run], and an [Aggregator] the
// host adds its bridged endpoints to (ADR 0018 decision 5).
//
// It mirrors matter.js ServerNode (packages/node/src/node/ServerNode.ts:
// 106-126 create, :264-298 the root endpoint composition) and Node
// (Node.ts:113-131 start, :178-181 run, :188 stop): a node is configured
// by behaviour ids — `network`, `commissioning`, `productDescription`,
// `basicInformation` — and its root carries the system servers without
// the host composing them.
//
// What the facade performs is the reference daemon's bring-up
// (examples/reference-bridge/main.go:84-497), in the order
// bridge/bridge.go's package doc lists the ports, with every port wired:
//
//   - New: the stores (schemas applied on Config.Store.DB), the group
//     state, the endpoint assembler, bridge.New, the device-type
//     validation mode, AttachACLLister, AttachGroupMessaging,
//     AttachAuxiliaryACL, AttachDiagnosticEvents, the root servers
//     ([RootClusters]) and AttachRootClusters, the Aggregator servers and
//     AttachAggregatorClusters, then the security wiring —
//     AttachSessionLookup, AttachAckTracker, AttachSubscriptionManager,
//     AttachSessionRegistry, AttachCaseHandlerProvider,
//     AttachSubscriptionStore, AttachOperationalResolver,
//     AttachCaseInitiatorProvider — the commissioning window
//     (AttachCommissioningWindow, with the PASE provider attached while a
//     window is open), the fabric hooks and the event-number persistence.
//   - Start: bridge.Start, the DiagnosticLogs provider, StartUp and
//     BootReason, the operational-time keeper, both PartsList providers,
//     the stored fabrics' CASE identities, ReestablishFormerSubscriptions,
//     the OTA requestor, AnnounceFabric for each fabric, and the node's
//     own commissioning window when no fabric exists.
//
// The citations of examples/reference-bridge files and lines in this
// package name the daemon as it stood before it moved onto the facade
// (commit 30fe7f1; brief R): the code they cite is the facade's now.
//
// Left to the configuration on purpose: a nil Config.Advertiser publishes
// nothing, a nil Config.OTA mounts no OTA requestor. Left to the host:
// GeneralDiagnostics' TestEventTrigger enable key, the PowerSource
// endpoints of PowerSourceConfiguration, and the bridge's observer hooks
// (SetOnReassembled, SetOnFabricAdded) — reachable through [Node.Root] and
// [Node.Bridge].
package node

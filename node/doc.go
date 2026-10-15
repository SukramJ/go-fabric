// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package node is the facade over the bridge's bring-up: one [Config], one
// [New], [Node.Start] / [Node.Stop] / [Node.Run], and an [Aggregator] the
// host adds its bridged endpoints to (ADR 0018 decision 5).
//
// It mirrors matter.js ServerNode (packages/node/src/node/ServerNode.ts:
// 107-127 create, :274-309 the root endpoint composition) and Node
// (Node.ts:114-132 start, :183-186 run, :193 stop): a node is configured
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
//     window is open), the fabric hooks and the event-number persistence;
//     with Config.OTAProvider the OTA provider and AttachBDXResponder;
//     with Config.CommissionerControl the CommissionerControl server on
//     the Aggregator.
//   - Start: bridge.Start, the DiagnosticLogs provider, StartUp and
//     BootReason, the operational-time keeper, both PartsList providers,
//     the stored fabrics' CASE identities, ReestablishFormerSubscriptions,
//     the OTA requestor, AnnounceFabric for each fabric, and the node's
//     own commissioning window when no fabric exists; the OTA provider's
//     ACL entry once a fabric exists (and after each CommissioningComplete).
//
// The citations of examples/reference-bridge files and lines in this
// package name the daemon as it stood before it moved onto the facade
// (commit 30fe7f1; brief R): the code they cite is the facade's now.
//
// A host that assembles its own topology — its own endpoint.Assembler
// over its own device model — hands the bridge its Snapshotter through
// Config.Topology and gets no [Aggregator]; everything else stays the
// facade's, the group state included (Config.Groups, [Node.Groups]). Its operator surface reads [Node.Sessions] and
// [Node.Subscriptions], and removes a fabric without a commissioner
// through [Node.RemoveFabric] (issue #87).
//
// Left to the configuration on purpose: a nil Config.Advertiser publishes
// nothing, a nil Config.OTA mounts no OTA requestor, a nil
// Config.OTAProvider no OTA provider, a nil Config.CommissionerControl no
// CommissionerControl server. Left to the host:
// GeneralDiagnostics' TestEventTrigger enable key, the PowerSource
// endpoints of PowerSourceConfiguration, and the bridge's observer hooks
// (SetOnReassembled, SetOnFabricAdded) — reachable through [Node.Root] and
// [Node.Bridge]. The fabric, commissioning-complete and window events reach
// the host through [Config].Observers, chained after the facade's own
// handlers; a host that set the cluster hooks the facade wires
// (OperationalCredentials SetOnFabricRemoved / SetOnFabricUpdated,
// GeneralCommissioning SetOnCommissioningComplete, the window's
// SetTransitionHook) would replace the facade's wiring. A window with
// credentials other than the configured ones is [Node.OpenBasicWindow].
package node

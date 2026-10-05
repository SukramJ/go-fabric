// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package subscription implements the Matter subscription state
// machine layered on top of the [im] message codecs.
//
// Matter Core Spec §8.5 governs subscriptions:
//
//   - A commissioner sends SubscribeRequest with a path list, a
//     MinIntervalFloor (lower-bound seconds between reports) and a
//     MaxIntervalCeiling (upper-bound; the bridge MUST report at
//     least this often as a keep-alive).
//   - The bridge accepts or rejects the subscription. On accept it
//     allocates a SubscriptionID and emits an initial ReportData
//     containing the current values for every requested path.
//   - Subsequent reports fire when an attribute changes (gated by
//     MinInterval) or after MaxInterval expires (keep-alive).
//   - Each subscription is fabric-scoped and counts against the
//     per-fabric MaxSubscriptions quota.
//
// go-fabric v1.1 ships a baseline implementation: subscriptions
// live in RAM, reports are produced by a single Tick goroutine that
// drives every Subscription's MaxInterval timer, and attribute
// changes flow in via [Manager.OnAttributeChanged] (called by the
// bridge core when the source DP fires).
//
// Subscriptions survive a restart the way matter.js keeps them
// (docs/adr/0008): [Subscription.PeerSubscription] captures the persisted
// form, [Manager.Restore] re-creates a former subscription under its old
// id, and [Manager.SetOnSubscriptionTerminated] tells a subscription that
// ended for good apart from one that merely lost its session. The bridge
// drives the rest — the store, the CASE session to the controller, the
// priming report.
package subscription

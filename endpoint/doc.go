// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package endpoint turns flat endpoint descriptions into the Matter
// endpoint topology the bridge advertises to commissioners.
//
// Its input is [Snapshot] — a set of [Spec] values carrying no device
// model at all. Whoever owns a model walks it, decides what deserves an
// endpoint and resolves the operator-facing labels through its own
// naming authority; this package then allocates and
// persists endpoint ids, builds the three-tier root/aggregator
// scaffolding, materialises the cluster surface and dispatches
// Interaction Model requests into it.
//
// The assembler is non-reactive: it consumes a snapshot and produces a
// topology, then is done. Re-running it on a change is the caller's:
// bridge.Bridge.Reassemble assembles again, and the node facade calls it
// when the host adds, removes or replaces a device (node.Aggregator Add,
// Remove, Replace).
//
// What a source identity is made of is the owner's business, so
// [Spec.StableKey] carries an opaque [SourceKey], and the endpoint number
// it maps to is kept by a [Store]: the module ships endpoint/sqlitestore
// (the node's SQLite database) and endpoint/memstore, and a host may
// implement [Store] over its own table. The same source must produce the
// same key, and therefore the same Matter endpoint ID, across restarts.
//
// Endpoint 0 is the root bridge endpoint and is never persisted —
// the assembler synthesises it from the bridge configuration on every
// run, as is the Aggregator at 1. Bridged endpoints occupy 2..65534.
package endpoint

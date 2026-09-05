// SPDX-License-Identifier: MIT
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
// topology, then is done. Re-running on model changes is the caller's
// responsibility (the bridge core subscribes to the event bus and
// triggers re-assembly when devices are added, removed, or change
// reachability).
//
// Endpoint identity is NOT persisted here. What a source identity is
// made of is the owner's business, so [Spec.StableKey] carries an
// opaque [SourceKey] and the owner implements [Store] over its own
// table. The same source must produce the same key, and therefore the
// same Matter endpoint ID, across restarts.
//
// Endpoint 0 is the root bridge endpoint and is never persisted —
// the assembler synthesises it from the bridge configuration on every
// run. Bridged endpoints occupy 1..65534.
package endpoint

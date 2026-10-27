// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package otbr reaches an OpenThread Border Router (ot-br-posix) over its
// REST API and adapts it to the Thread Border Router port of the cluster
// servers (cluster/threadbr BorderRouter, ADR 0027 decision 4). The module
// joins no Thread mesh: the border router is a host on the LAN, by default
// at port 8081, and Matter over Thread reaches this module as UDP over the
// border router's IPv6 routes.
//
// [Client] is a semantic port of matter.js
// packages/thread-br-client/src/otbr-rest/OtbrRestClient.ts at 862ad75f:
// the read-only endpoints under /node and /diagnostics, the key-casing
// detection of OtbrRestCapability.ts (pascal on older builds, camel after
// 2024) and caseNormalizer.ts, the parse helpers of parseHexBytes.ts and
// the error classification of OtbrRestError.ts; [Probe] is OtbrRestProbe.ts
// and [Translate] the subset of translation.ts the diagnostics server
// reads. Standard library only (net/http).
//
// The client's mutating operations — the Thread interface's state, the
// active and pending datasets — are unexported: the one way to them is
// [BorderRouter], whose activation the Thread Border Router Management
// server runs under the node's fail-safe. matter.js's factoryReset,
// setCommissionerState, the joiner operations and the collection API's
// actions are not wrapped at all: thread-br-client's README warns that they
// "can silently partition or brick a network", and nothing the servers
// serve needs them. MeshCoP (CoAP over DTLS with EC-JPAKE) stays out (ADR
// 0027 decision 5).
//
// The package sits in the support layer (script/archcheck): it imports the
// cluster servers' port, which no server imports back.
package otbr

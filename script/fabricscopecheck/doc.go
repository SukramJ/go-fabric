// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package fabricscopecheck guards two rules of fabric-scoped data: the
// removal of a fabric's state, and its scoping on a read.
//
// The removal: every cluster server the module builds for a cluster with a
// fabric-scoped attribute implements the removal contract,
// contract.FabricScopedState, or is listed with the reason it holds no
// such state (owner decision 37, review II).
//
// matter.js strips every attribute whose access is fabric-scoped (F) or
// fabric-sensitive (S) by schema, whatever the server's logic
// (packages/node/src/behavior/cluster/FabricScopedDataHandler.ts:41-73 at
// db91886c; AttributeModel.fabricScoped). A Go server keeps such state in
// its own fields, so the node reaches it only through the contract
// (node/fabric_state.go); a server without it keeps a removed fabric's
// entries and hands them to the next fabric with the same index.
//
// # The rule
//
// [TestEveryFabricScopedServerHasARemovalPath] reads parity/schema.json and
// takes every cluster with an attribute whose effective access names
// fabric "F" or "S". For each such cluster:
//
//   - the registry ([servers]) names the module's server types for it,
//     and each must implement contract.FabricScopedState, or [listed]
//     gives the reason the cluster's server holds no fabric state;
//   - every package that builds a server over the cluster's generated
//     definition — a call of spec.New, spec.NewServer or mustInstance with
//     the definition package's Definition, outside cluster/spec — must
//     have a registry entry for the cluster in that package, or the
//     cluster is listed. This is how a new hand-written server is found.
//
// An entry or listing of a cluster the schema does not mark fails as
// stale. [TestCheckFindsAServerWithoutARemovalPath] is the negative case:
// the same check over a fixture fails for a server without the contract,
// for a fabric-scoped cluster nobody registers, and for a package that
// builds a server the registry does not name.
//
// What the rule does not see: a second server type for a cluster in a
// package that has one registered, and a server built without its
// definition (AccessControl, whose entry is kept by hand).
//
// # The scoping
//
// The dispatcher scopes every fabric-scoped attribute of the model on a
// request, whatever the server returns: endpoint.TopologyDispatcher
// attaches the generated scope (schema.AttributeFabricScope) and
// im.MaskFabricScoped applies it to the encoded value. A server returns
// every fabric's entries whole. redaction_test.go holds both halves:
// [TestNoHandWrittenRedactionOutsideTheMask] finds a field withheld by
// hand outside the mask (a Redacted field beyond the deprecated ones, code
// that sets one, a reader beyond the bridge writer's deprecated branches,
// a type that redacts), and [TestEveryFabricSensitivePathHasARowAndAWalkCase]
// holds every list of the snapshot with fabric-sensitive data to its row
// in the generated table and to the node walk's selection. Each has its
// negative case.
package fabricscopecheck

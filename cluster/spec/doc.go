// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package spec is the runtime half of the generated cluster definitions:
// the types a generated definition is made of, and what a cluster server
// derives from one definition plus the host's declared features and
// optional elements.
//
// The split mirrors matter.js. Its cluster types
// (packages/types/src/clusters/*.ts) are generated from the model; its
// behaviors (packages/node/src/behaviors/<name>/*Server.ts) are written by
// hand and inherit everything the model decides — which elements a feature
// selection makes present (ValidatedElements), how a written value is
// checked (ValueValidator), how a payload is encoded (TlvOfModel). Here the
// generated half lives in one package per cluster under cluster/spec/
// (written by script/clustergen from parity/schema.json, never by hand), and
// this package is the matter.js machinery those definitions plug into:
//
//   - [Cluster] and its parts: ids, typed enum / bitmap / struct metadata,
//     the conformance of every element as matter.js's AST, access, quality
//     and constraint.
//   - [Conformance.Applicability]: matter.js's computeApplicability
//     (packages/model/src/aspects/Conformance.ts), ported.
//   - [New] / [Instance]: AttributeList, AcceptedCommandList,
//     GeneratedCommandList, EventList, FeatureMap and ClusterRevision for a
//     feature selection, the feature-selection check, the read / write /
//     invoke privileges and event priorities.
//   - [Instance.ValidateWrite]: the write checks matter.js's
//     AttributeWriteResponse and ValueValidator apply, with their status
//     codes.
//   - The TLV codecs generated structs are built from ([Node], the Put* and
//     Decode* helpers, [Nullable]) and the registry ([Register],
//     [DecodeRequest]) the bridge decodes generated command payloads
//     through.
//
// A server author writes only what matter.js writes by hand: the host port
// and the cluster's rules. See docs/adr/0013-generated-cluster-definitions.md.
package spec

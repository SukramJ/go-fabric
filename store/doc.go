// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package store persists the Matter bridge's operational state:
// fabrics, node identities (NOC chain + private key + IPK), group
// keys, and access-control lists.
//
// This package is purely the typed access surface — it borrows an
// already-migrated *sql.DB from whatever opened it and never opens one.
// The DDL its queries are written against is embedded here and reachable
// as [Schema]; a host either feeds that text through its own migration
// tool or calls [Apply], but does not transcribe it.
//
// Persistence model (Matter Core Spec §11.18 / §11.2 / §11.2.10):
//
//   - Fabric (matter_fabrics) — keyed by stack-assigned fabric_index
//     (1..254). Holds FabricID, NodeID, RootPublicKey, VendorID,
//     Label, CompressedFabricID.
//   - NodeIdentity (matter_node_identities) — one per fabric. Holds
//     NOC (raw bytes), optional ICAC, the P-256 private scalar
//     matching the NOC's public key, and the IPK.
//   - GroupKeySet (matter_group_keys) — per (fabric, group_key_set_id).
//     Up to three EpochKey/EpochStart pairs per spec.
//   - GroupKeyMap (matter_group_key_map) — binds GroupID →
//     GroupKeySetID per fabric.
//   - ACL (matter_acl_entries) — per-fabric ordered list of ACEs.
//     Subjects and Targets ride as JSON because the access path is
//     always "load whole ACL for fabric".
//
// Cascade-delete on fabric removal is handled at the schema level
// (FOREIGN KEY ... ON DELETE CASCADE).
//
// The private key persists raw. File-system level protection is the
// operator's responsibility for v1.1; an at-rest encryption ADR is
// expected for v1.2.
package store

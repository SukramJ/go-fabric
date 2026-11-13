// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package store persists the Matter bridge's operational state:
// fabrics, node identities (NOC chain + private key + IPK), group
// keys, and access-control lists.
//
// This package is purely the typed access surface — it borrows an
// already-migrated *sql.DB from whatever opened it and never opens one.
// The DDL its queries are written against is embedded here and reachable
// as [Schema]; a host either feeds that text through its own migration
// tool or calls [Apply], but does not transcribe it. A tool that needs
// versioned steps takes [Migrations], and [SchemaFingerprint] tells a host
// whether its database carries this release's schema.
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
//     GroupKeySetID per fabric; the key set need not exist yet.
//   - GroupTable (matter_group_table) — the endpoints of each group.
//   - Groupcast groups (matter_groupcast_groups) — the Groupcast
//     cluster's per-group multicast policy and auxiliary-ACL flag.
//   - ACL (matter_acl_entries) — per-fabric ordered list of ACEs.
//     Subjects and Targets ride as JSON because the access path is
//     always "load whole ACL for fabric".
//
// Cascade-delete on fabric removal is handled at the schema level
// (FOREIGN KEY ... ON DELETE CASCADE).
//
// The operational private key and the fabric records persist in clear,
// and so do the group key sets, the shared secrets of the CASE resumption
// records (matter_resumption) and, in the settings table, the committed
// Wi-Fi networks with their credentials, which cluster/core's Wi-Fi
// NetworkCommissioning server writes under netcomm.wifi.networks. Whoever
// can read the database, or a copy of it, reads them; file-system level
// protection is the operator's responsibility. Two kinds of secret are
// sealed at rest: the keys of the ICD registrations, which the store
// seals under SettingICDKey (icd_cipher.go, ADR 0029), and DoorLock
// credential data, which cluster/lock encrypts before the store sees it
// (SettingDoorLockCredentialKey). Both sealing keys are settings of the
// same database. Whether the store should seal every secret uniformly is
// an open architecture question of the 2026-11 review; this list is what
// holds today.
//
// The factory reset ([Store.Erase]) deletes every matter_* row and then
// rewrites the file (VACUUM) and empties the write-ahead log
// (wal_checkpoint(TRUNCATE)), so the erased bytes, and those of rows
// deleted before it, remain in none of the database file, its -wal and its
// -shm. Copies outside those files — flash wear-levelling, file-system
// snapshots, backups — are beyond what the store can promise.
package store

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
// # Secrets at rest
//
// The store holds the node's secrets: the operational private key (and,
// while an UpdateNOC is uncommitted, the key it replaced), the IPK, the
// group epoch keys, the shared secrets of the CASE resumption records
// (matter_resumption), the ICDToken keys of the ICD registrations, the
// DoorLock credential data, and, in the settings table, the committed
// Wi-Fi networks with their credentials, which cluster/core's Wi-Fi
// NetworkCommissioning server writes under netcomm.wifi.networks.
//
// A Store from [New] keeps them as they are, except two kinds it or its
// caller seals under a key kept as a setting of the same database: the ICD
// keys, under [SettingICDKey] (icd_cipher.go, ADR 0029), and DoorLock
// credential data, which cluster/lock encrypts before the store sees it
// ([SettingDoorLockCredentialKey]). That keeps them out of a read of one
// table, not out of a copy of the file: whoever can read the database, or a
// copy of it, reads every secret, and file-system level protection is the
// operator's responsibility.
//
// A Store from [NewSealed] seals every secret column under a [Sealer] the
// host supplies, whose key lives outside the database — only then are the
// secrets a copy of the file holds, the old ones in its free pages and its
// -wal included, unreadable without that key; the non-secret columns stay
// readable. Each sealed value is bound to its table, column and row; each
// row carries its seal mark (schema v10), and a sealed store refuses a
// secret row without it ([ErrUnseal]). Sealing protects the confidentiality
// of a copy of the database; it gives no integrity or rollback protection —
// an older sealed value of the same row, written back, opens. [NewSealed]
// names the columns and the settings (the JointFabricDatastore record
// under jointfabric.datastore: among them), fails with [ErrUnseal] when
// its sealer does not open a sealed value the database holds, and seals a
// clear database on the way in; [AESGCMSealer] is the stdlib sealer. The key's provisioning
// and rotation are the host's.
//
// # Store errors
//
// One rule decides what a refused store write does to state the node
// serves. Every change a controller causes — an attribute write, a
// command's change of a table such as a scene, a NodeLabel — is stored
// with the change: the store write comes first, and a refused store
// answers FAILURE and applies nothing, so nothing a controller was told
// failed is served or comes back after a restart. matter.js hands a change
// to its store in the commit's first phase (Datasource.ts:1224-1262 at
// db021da0); the rollback on a refused store is the entry
// BD-Matter-NonVolatileWrite-StoredBeforeApplied of
// notes/parity/by_design.md. The internal consequences of a change — the
// OTA requestor's progress, what a removal takes along (a removed group's
// or fabric's scenes), the controller role's node cache — are applied
// whatever the store answers, logged when it refuses, and stored again
// with the next change. endpoint.Persistence states the rule for a bridged
// endpoint's NodeLabel and scene table.
//
// The factory reset ([Store.Erase]) deletes every matter_* row and then
// rewrites the file (VACUUM) and empties the write-ahead log
// (wal_checkpoint(TRUNCATE)), so the erased bytes, and those of rows
// deleted before it, remain in none of the database file, its -wal and its
// -shm. Copies outside those files — flash wear-levelling, file-system
// snapshots, backups — are beyond what the store can promise.
package store

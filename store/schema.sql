-- SPDX-License-Identifier: MIT
-- Copyright (C) 2026 SukramJ.
--
-- The DDL for the matter_* tables this package reads and writes. It is
-- embedded into the package and reachable as store.Schema(), so a host does
-- not transcribe it: a copy is exactly what drifts unnoticed, and the
-- queries in this package are written against THIS text.
--
-- The package still takes an already-migrated *sql.DB and never opens a
-- database. A host with its own migration tool feeds this text through it;
-- a host without one calls store.Apply. Every statement is idempotent
-- (IF NOT EXISTS / INSERT OR IGNORE) so applying it on every boot is safe.
--
-- Endpoint identity (matter_endpoints) is deliberately absent: its key is
-- the host's own source identity, so it lives behind endpoint.Store and its
-- SQLite implementation ships its own schema in endpoint/sqlitestore.

-- matter_fabrics records the operational fabrics the bridge participates
-- in. Mirrors the Fabric Descriptor from Matter Core Spec §11.18.5.
-- fabric_index is the stack-assigned 1..254 identifier used as foreign
-- key by every per-fabric child table.
--
-- root_cert holds the full Matter Certificate TLV envelope the
-- commissioner sent via AddTrustedRootCertificate. TrustedRootCertificates
-- serves list<octet_string<400>> of exactly those bytes; serving the bare
-- 65-byte EC-P256 public key instead makes a controller discard the whole
-- Subscribe-Initial ReportData stream. root_public_key is kept alongside it
-- as a cache: it is extractable from root_cert but is read on every Sigma1
-- lookup and on the CompressedFabricID HKDF (Matter §4.13.2.4).
CREATE TABLE IF NOT EXISTS matter_fabrics (
    fabric_index    INTEGER PRIMARY KEY CHECK(fabric_index BETWEEN 1 AND 254),
    fabric_id       BLOB    NOT NULL,
    node_id         BLOB    NOT NULL,
    root_public_key BLOB    NOT NULL,
    vendor_id       INTEGER NOT NULL CHECK(vendor_id BETWEEN 0 AND 65535),
    label           TEXT    NOT NULL DEFAULT '',
    compressed_id   BLOB    NOT NULL,
    created_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    root_cert       BLOB
);

-- (fabric_id, root_public_key) is unique per Matter §11.18.5: the same
-- root MAY NOT add the same fabric twice.
CREATE UNIQUE INDEX IF NOT EXISTS matter_fabrics_id_root
    ON matter_fabrics(fabric_id, root_public_key);

-- matter_node_identities holds the per-fabric node operational
-- credentials: the NOC + optional ICAC + the private key matching the
-- NOC's public key + the Identity Protection Key. One identity per
-- fabric (the bridge has exactly one node per fabric).
--
-- ipk is the RAW AddNOC.IPKValue as the commissioner sent it. The CASE
-- handshake keys on the derived operational IPK instead — see
-- sigma.DeriveOperationalIPK — so a reader must not feed this column
-- straight into sigma.Identity.
CREATE TABLE IF NOT EXISTS matter_node_identities (
    fabric_index    INTEGER PRIMARY KEY,
    noc             BLOB    NOT NULL,
    icac            BLOB,
    private_key     BLOB    NOT NULL,
    ipk             BLOB    NOT NULL,
    created_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(fabric_index) REFERENCES matter_fabrics(fabric_index) ON DELETE CASCADE
);

-- matter_group_keys persists the EpochKey triples per
-- (fabric, group_key_set_id) per Matter §11.2.10. epoch_key_1 and
-- epoch_key_2 are optional (nullable) — the active key may rotate
-- without filling all three slots.
CREATE TABLE IF NOT EXISTS matter_group_keys (
    fabric_index        INTEGER NOT NULL,
    group_key_set_id    INTEGER NOT NULL CHECK(group_key_set_id BETWEEN 0 AND 65535),
    security_policy     INTEGER NOT NULL CHECK(security_policy BETWEEN 0 AND 1),
    epoch_key_0         BLOB    NOT NULL,
    epoch_start_0       INTEGER NOT NULL,
    epoch_key_1         BLOB,
    epoch_start_1       INTEGER,
    epoch_key_2         BLOB,
    epoch_start_2       INTEGER,
    PRIMARY KEY(fabric_index, group_key_set_id),
    FOREIGN KEY(fabric_index) REFERENCES matter_fabrics(fabric_index) ON DELETE CASCADE
);

-- matter_group_key_map binds GroupID -> GroupKeySetID per fabric
-- (Matter §11.2.10.4 GroupKeyMap attribute).
--
-- group_key_set_id deliberately carries no foreign key to
-- matter_group_keys: a GroupKeyMap entry may name a key set that is not
-- written yet. matter.js accepts such a write and leaves the existence
-- check commented out because certification tests write the map first
-- (GroupKeyManagementServer.ts #validateGroupKeyMap); an entry whose key
-- set is missing authenticates nothing. KeySetRemove drops the entries
-- that name the removed set explicitly (store.RemoveGroupKeySet). A
-- database created with the earlier foreign key is rebuilt by
-- store.Upgrade, which store.Apply runs.
CREATE TABLE IF NOT EXISTS matter_group_key_map (
    fabric_index        INTEGER NOT NULL,
    group_id            INTEGER NOT NULL CHECK(group_id BETWEEN 0 AND 65535),
    group_key_set_id    INTEGER NOT NULL,
    PRIMARY KEY(fabric_index, group_id),
    FOREIGN KEY(fabric_index) REFERENCES matter_fabrics(fabric_index) ON DELETE CASCADE
);

-- matter_group_table persists the group membership of this node's
-- endpoints per fabric — the GroupKeyManagement GroupTable attribute
-- (Matter §11.2.6.2, GroupInfoMapStruct) that the Groups cluster's
-- AddGroup / RemoveGroup maintain. endpoints_json is the ordered endpoint
-- list as a JSON array. Mirrors matter.js GroupKeyManagementServer state
-- `groupTable`, which is persisted (packages/node/src/behaviors/
-- group-key-management/GroupKeyManagementServer.ts addEndpointForGroup /
-- removeEndpoint).
CREATE TABLE IF NOT EXISTS matter_group_table (
    fabric_index        INTEGER NOT NULL,
    group_id            INTEGER NOT NULL CHECK(group_id BETWEEN 1 AND 65535),
    group_name          TEXT    NOT NULL DEFAULT '',
    endpoints_json      TEXT    NOT NULL,
    PRIMARY KEY(fabric_index, group_id),
    FOREIGN KEY(fabric_index) REFERENCES matter_fabrics(fabric_index) ON DELETE CASCADE
);

-- matter_groupcast_groups persists the Groupcast cluster's per-group
-- properties: the multicast address policy (MulticastAddrPolicyEnum:
-- 0 IanaAddr, 1 PerGroup) and whether the group carries an auxiliary
-- access control entry. A row is what makes a group a Groupcast member
-- beyond the group table. Mirrors matter.js GroupcastServer state
-- `groupProperties` (GroupPropertiesStruct: GroupId, McastAddrPolicy,
-- HasAuxiliaryAcl, FabricIndex; quality N), which together with
-- GroupKeyManagement's groupTable and groupKeyMap is the source the
-- Membership attribute is derived from
-- (packages/node/src/behaviors/groupcast/GroupcastServer.ts).
CREATE TABLE IF NOT EXISTS matter_groupcast_groups (
    fabric_index        INTEGER NOT NULL,
    group_id            INTEGER NOT NULL CHECK(group_id BETWEEN 1 AND 65535),
    mcast_addr_policy   INTEGER NOT NULL CHECK(mcast_addr_policy BETWEEN 0 AND 255),
    has_auxiliary_acl   INTEGER NOT NULL DEFAULT 0 CHECK(has_auxiliary_acl IN (0, 1)),
    PRIMARY KEY(fabric_index, group_id),
    FOREIGN KEY(fabric_index) REFERENCES matter_fabrics(fabric_index) ON DELETE CASCADE
);

-- matter_acl_entries persists the per-fabric AccessControl list
-- (Matter §11.2.12). Subjects + Targets are JSON-encoded inline because
-- they are short list-of-records and the access path is always
-- "load whole ACL for fabric" — relational normalisation buys nothing
-- here. position orders entries; the Matter spec evaluates ACEs in
-- list order.
CREATE TABLE IF NOT EXISTS matter_acl_entries (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    fabric_index    INTEGER NOT NULL,
    privilege       INTEGER NOT NULL CHECK(privilege BETWEEN 1 AND 5),
    auth_mode       INTEGER NOT NULL CHECK(auth_mode BETWEEN 1 AND 3),
    subjects_json   TEXT    NOT NULL,
    targets_json    TEXT,
    position        INTEGER NOT NULL,
    FOREIGN KEY(fabric_index) REFERENCES matter_fabrics(fabric_index) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS matter_acl_position
    ON matter_acl_entries(fabric_index, position);

-- matter_resumption persists CASE-resumption identifiers per Matter
-- Core Spec §4.13.2.4. ResumptionID is the 16-byte token the bridge
-- emits in Sigma2 so a returning peer can re-establish a session via
-- Sigma1 with the resumption-id field populated, skipping the full
-- handshake.
--
-- case_authenticated_tags carries the CATs from the initiator's NOC
-- subject as JSON (e.g. [1099511627777]); NULL and '[]' both mean
-- "no CATs". They must survive across restarts so a resumed session
-- re-applies the same fabric-scoped privilege.
CREATE TABLE IF NOT EXISTS matter_resumption (
    fabric_index    INTEGER NOT NULL,
    peer_node_id    BLOB    NOT NULL,
    resumption_id   BLOB    NOT NULL,
    shared_secret   BLOB    NOT NULL,
    created_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_used_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    case_authenticated_tags BLOB NOT NULL DEFAULT '[]',
    PRIMARY KEY(fabric_index, peer_node_id),
    FOREIGN KEY(fabric_index) REFERENCES matter_fabrics(fabric_index) ON DELETE CASCADE
);

-- Resumption ID is globally unique (Matter §4.13.2.4: 16-byte random
-- with negligible collision probability). Index lets the responder
-- look up by ID alone when the initiator's NodeID is unknown until
-- decode.
CREATE UNIQUE INDEX IF NOT EXISTS matter_resumption_id ON matter_resumption(resumption_id);

-- matter_diagnostics persists the GeneralDiagnostics counters that need
-- to survive restarts: RebootCount plus accumulated
-- TotalOperationalHours from prior process lifetimes.
--
-- Single-row table (id=1 invariant): there is exactly one bridge per
-- process, the counters are global, no fabric- or endpoint-scoping is
-- needed.
CREATE TABLE IF NOT EXISTS matter_diagnostics (
    id                       INTEGER PRIMARY KEY CHECK (id = 1),
    reboot_count             INTEGER NOT NULL DEFAULT 0,
    base_operational_hours   INTEGER NOT NULL DEFAULT 0,
    updated_at               INTEGER NOT NULL
);

-- Seed the singleton row so subsequent UPSERTs hit an existing record.
INSERT OR IGNORE INTO matter_diagnostics (id, reboot_count, base_operational_hours, updated_at)
    VALUES (1, 0, 0, CAST(strftime('%s','now') AS INTEGER));

-- matter_metadata is a key-value store for per-process Matter counters.
--
-- next_fabric_index makes AddFabric allocate monotonically rather than
-- re-using a freshly-removed slot.
CREATE TABLE IF NOT EXISTS matter_metadata (
    key   TEXT    PRIMARY KEY,
    value INTEGER NOT NULL
);

INSERT OR IGNORE INTO matter_metadata (key, value) VALUES ('next_fabric_index', 1);

-- matter_settings is a key-value store for per-process Matter strings
-- that must survive restarts (writable cluster attributes such as
-- BasicInformation.NodeLabel / Location per Matter §11.1.6.6 "N"
-- quality). Kept separate from matter_metadata, whose value column is
-- INTEGER for counters.
CREATE TABLE IF NOT EXISTS matter_settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- matter_persistent_subscriptions is the deprecated first attempt at
-- subscription persistence. Nothing in this module writes or reads it; it
-- is still created so a host that pinned the old store API keeps working
-- through the deprecation window. matter_server_subscriptions replaces it.
CREATE TABLE IF NOT EXISTS matter_persistent_subscriptions (
    id                  INTEGER  PRIMARY KEY AUTOINCREMENT,
    fabric_index        INTEGER  NOT NULL,
    node_id             BLOB     NOT NULL,
    paths_json          TEXT     NOT NULL,
    intervals_json      TEXT     NOT NULL,
    created_at          TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Index on fabric_index so the load path filters by fabric efficiently
-- on fabric-removal teardown.
CREATE INDEX IF NOT EXISTS matter_persistent_subscriptions_fabric
    ON matter_persistent_subscriptions(fabric_index);

-- matter_server_subscriptions holds the server subscriptions of CASE
-- sessions that are active right now, so they can be re-established
-- under their old SubscriptionId after a restart (docs/adr/0008, mirroring
-- matter.js SubscriptionsServer). One row per SubscriptionId; payload is
-- the im/subscription.PeerSubscription encoding and is opaque to this
-- package. The bridge clears the table when it loads it at start-up and
-- writes back each subscription that becomes active again.
CREATE TABLE IF NOT EXISTS matter_server_subscriptions (
    subscription_id     INTEGER  PRIMARY KEY,
    fabric_index        INTEGER  NOT NULL,
    peer_node_id        BLOB     NOT NULL,
    payload             BLOB     NOT NULL,
    updated_at          TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS matter_server_subscriptions_fabric
    ON matter_server_subscriptions(fabric_index);

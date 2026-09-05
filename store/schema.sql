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
CREATE TABLE IF NOT EXISTS matter_group_key_map (
    fabric_index        INTEGER NOT NULL,
    group_id            INTEGER NOT NULL CHECK(group_id BETWEEN 0 AND 65535),
    group_key_set_id    INTEGER NOT NULL,
    PRIMARY KEY(fabric_index, group_id),
    FOREIGN KEY(fabric_index) REFERENCES matter_fabrics(fabric_index) ON DELETE CASCADE,
    FOREIGN KEY(fabric_index, group_key_set_id)
        REFERENCES matter_group_keys(fabric_index, group_key_set_id) ON DELETE CASCADE
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

-- matter_persistent_subscriptions stores active subscriptions that
-- survive a restart. On boot the bridge re-arms every row as an in-memory
-- subscription so controllers that had active subscriptions before the
-- restart receive ongoing reports without re-subscribing, per Matter 1.4
-- §10.6.9.
--
-- paths_json holds a JSON array of ConcreteAttributePath objects
-- (serialised with the same field names as the Go struct so the store
-- layer decodes without a custom mapper). intervals_json holds
-- {"min":N,"max":N} for the negotiated cadence.
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

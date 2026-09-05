-- SPDX-License-Identifier: MIT
-- Copyright (C) 2026 SukramJ.
--
-- Test-only DDL for the matter_* tables this package reads and writes.
-- The package itself takes an already-migrated *sql.DB and never creates a
-- table, so the schema a host applies through its own migration tool is
-- authoritative in production; this file exists so the package's tests can
-- stand up that shape without one.
--
-- It is a flattened end-state of the migration set the tables were
-- introduced by, so the two can drift: a column added host-side without
-- being added here makes a test compile and pass against a schema no
-- deployment has. The column comments below therefore say what each field
-- is for, not merely that it exists, so a reader can tell an omission from
-- a deliberate difference.

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
CREATE TABLE matter_fabrics (
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
CREATE UNIQUE INDEX matter_fabrics_id_root
    ON matter_fabrics(fabric_id, root_public_key);

-- matter_node_identities holds the per-fabric node operational
-- credentials: the NOC + optional ICAC + the private key matching the
-- NOC's public key + the Identity Protection Key. One identity per
-- fabric (the bridge has exactly one node per fabric).
CREATE TABLE matter_node_identities (
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
CREATE TABLE matter_group_keys (
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
CREATE TABLE matter_group_key_map (
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
CREATE TABLE matter_acl_entries (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    fabric_index    INTEGER NOT NULL,
    privilege       INTEGER NOT NULL CHECK(privilege BETWEEN 1 AND 5),
    auth_mode       INTEGER NOT NULL CHECK(auth_mode BETWEEN 1 AND 3),
    subjects_json   TEXT    NOT NULL,
    targets_json    TEXT,
    position        INTEGER NOT NULL,
    FOREIGN KEY(fabric_index) REFERENCES matter_fabrics(fabric_index) ON DELETE CASCADE
);

CREATE UNIQUE INDEX matter_acl_position
    ON matter_acl_entries(fabric_index, position);

-- matter_endpoints persists the (source-identity -> endpoint_id) mapping
-- so the same host data point receives the same Matter endpoint
-- identifier across restarts. Endpoint 0 is the root bridge endpoint and
-- is never in this table; bridged endpoints occupy 1..65534.
CREATE TABLE matter_endpoints (
    central_name    TEXT    NOT NULL,
    device_address  TEXT    NOT NULL,
    channel_no      INTEGER NOT NULL,
    dp_kind         TEXT    NOT NULL CHECK(dp_kind IN ('custom','generic','calculated','combined','measurement')),
    dp_key          TEXT    NOT NULL,
    endpoint_id     INTEGER NOT NULL CHECK(endpoint_id BETWEEN 1 AND 65534),
    device_type     INTEGER NOT NULL CHECK(device_type BETWEEN 0 AND 65535),
    created_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY(central_name, device_address, channel_no, dp_kind, dp_key)
);

-- endpoint_id is globally unique across the entire bridge (Matter
-- spec: a controller addresses endpoints by ID without disambiguation).
CREATE UNIQUE INDEX matter_endpoints_id_unique
    ON matter_endpoints(endpoint_id);

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
CREATE TABLE matter_resumption (
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
CREATE UNIQUE INDEX matter_resumption_id ON matter_resumption(resumption_id);

-- matter_exposures persists the operator-managed allowlist the endpoint
-- assembler consults at materialisation time. Default state is empty:
-- no rows means nothing is exposed, so the bridge cannot fail open.
CREATE TABLE matter_exposures (
    central_name    TEXT    NOT NULL,
    device_address  TEXT    NOT NULL,
    channel_no      INTEGER NOT NULL,
    dp_kind         TEXT    NOT NULL CHECK(dp_kind IN ('custom','generic','calculated','combined','measurement')),
    dp_key          TEXT    NOT NULL,
    enabled         INTEGER NOT NULL DEFAULT 0 CHECK(enabled IN (0,1)),
    friendly_name   TEXT    NOT NULL DEFAULT '',
    created_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    actor           TEXT    NOT NULL DEFAULT '',
    PRIMARY KEY(central_name, device_address, channel_no, dp_kind, dp_key)
);

-- Fast-path lookup for the assembler's "is this central's source
-- enabled?" probe — avoids a full-table scan when one host has a few
-- hundred channels.
CREATE INDEX matter_exposures_central
    ON matter_exposures(central_name, enabled);

-- matter_diagnostics persists the GeneralDiagnostics counters that need
-- to survive restarts: RebootCount plus accumulated
-- TotalOperationalHours from prior process lifetimes.
--
-- Single-row table (id=1 invariant): there is exactly one bridge per
-- process, the counters are global, no fabric- or endpoint-scoping is
-- needed.
CREATE TABLE matter_diagnostics (
    id                       INTEGER PRIMARY KEY CHECK (id = 1),
    reboot_count             INTEGER NOT NULL DEFAULT 0,
    base_operational_hours   INTEGER NOT NULL DEFAULT 0,
    updated_at               INTEGER NOT NULL
);

-- Seed the singleton row so subsequent UPSERTs hit an existing record.
INSERT INTO matter_diagnostics (id, reboot_count, base_operational_hours, updated_at)
    VALUES (1, 0, 0, CAST(strftime('%s','now') AS INTEGER));

-- matter_metadata is a key-value store for per-process Matter counters.
--
-- next_fabric_index makes AddFabric allocate monotonically rather than
-- re-using a freshly-removed slot. next_endpoint_id is the high-water
-- mark for bridged endpoint numbers: without it the allocator hands out
-- the smallest unused number, so a number freed by an unpaired device is
-- reissued to an unrelated one and controllers — which cache their
-- accessory list by endpoint number — see the new device arrive under the
-- removed device's identity. Bridged endpoints start at 2 (0 = RootNode,
-- 1 = Aggregator).
CREATE TABLE matter_metadata (
    key   TEXT    PRIMARY KEY,
    value INTEGER NOT NULL
);

INSERT INTO matter_metadata (key, value) VALUES ('next_fabric_index', 1);
INSERT INTO matter_metadata (key, value) VALUES ('next_endpoint_id', 2);

-- matter_settings is a key-value store for per-process Matter strings
-- that must survive restarts (writable cluster attributes such as
-- BasicInformation.NodeLabel / Location per Matter §11.1.6.6 "N"
-- quality). Kept separate from matter_metadata, whose value column is
-- INTEGER for counters.
CREATE TABLE matter_settings (
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
CREATE TABLE matter_persistent_subscriptions (
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
CREATE INDEX matter_persistent_subscriptions_fabric
    ON matter_persistent_subscriptions(fabric_index);

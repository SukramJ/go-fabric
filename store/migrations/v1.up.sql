-- SPDX-License-Identifier: Apache-2.0
-- Copyright (C) 2026 SukramJ.
--
-- Migration v1 (go-fabric v0.1.0): the matter_* DDL as v0.1.0 shipped it
-- (git show v0.1.0:store/schema.sql, comments removed). matter_group_key_map
-- still carries its foreign key into matter_group_keys; v3 drops it.

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

CREATE UNIQUE INDEX IF NOT EXISTS matter_fabrics_id_root
    ON matter_fabrics(fabric_id, root_public_key);

CREATE TABLE IF NOT EXISTS matter_node_identities (
    fabric_index    INTEGER PRIMARY KEY,
    noc             BLOB    NOT NULL,
    icac            BLOB,
    private_key     BLOB    NOT NULL,
    ipk             BLOB    NOT NULL,
    created_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(fabric_index) REFERENCES matter_fabrics(fabric_index) ON DELETE CASCADE
);

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

CREATE TABLE IF NOT EXISTS matter_group_key_map (
    fabric_index        INTEGER NOT NULL,
    group_id            INTEGER NOT NULL CHECK(group_id BETWEEN 0 AND 65535),
    group_key_set_id    INTEGER NOT NULL,
    PRIMARY KEY(fabric_index, group_id),
    FOREIGN KEY(fabric_index) REFERENCES matter_fabrics(fabric_index) ON DELETE CASCADE,
    FOREIGN KEY(fabric_index, group_key_set_id)
        REFERENCES matter_group_keys(fabric_index, group_key_set_id) ON DELETE CASCADE
);

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

CREATE UNIQUE INDEX IF NOT EXISTS matter_resumption_id ON matter_resumption(resumption_id);

CREATE TABLE IF NOT EXISTS matter_diagnostics (
    id                       INTEGER PRIMARY KEY CHECK (id = 1),
    reboot_count             INTEGER NOT NULL DEFAULT 0,
    base_operational_hours   INTEGER NOT NULL DEFAULT 0,
    updated_at               INTEGER NOT NULL
);

INSERT OR IGNORE INTO matter_diagnostics (id, reboot_count, base_operational_hours, updated_at)
    VALUES (1, 0, 0, CAST(strftime('%s','now') AS INTEGER));

CREATE TABLE IF NOT EXISTS matter_metadata (
    key   TEXT    PRIMARY KEY,
    value INTEGER NOT NULL
);

INSERT OR IGNORE INTO matter_metadata (key, value) VALUES ('next_fabric_index', 1);

CREATE TABLE IF NOT EXISTS matter_settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS matter_persistent_subscriptions (
    id                  INTEGER  PRIMARY KEY AUTOINCREMENT,
    fabric_index        INTEGER  NOT NULL,
    node_id             BLOB     NOT NULL,
    paths_json          TEXT     NOT NULL,
    intervals_json      TEXT     NOT NULL,
    created_at          TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS matter_persistent_subscriptions_fabric
    ON matter_persistent_subscriptions(fabric_index);

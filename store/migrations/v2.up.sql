-- SPDX-License-Identifier: MIT
-- Copyright (C) 2026 SukramJ.
--
-- Migration v2 (go-fabric v0.2.0): the group table, the Groupcast group
-- properties and the server subscriptions (CHANGELOG [0.2.0], store).
-- The statements are store/schema.sql's.

CREATE TABLE IF NOT EXISTS matter_group_table (
    fabric_index        INTEGER NOT NULL,
    group_id            INTEGER NOT NULL CHECK(group_id BETWEEN 1 AND 65535),
    group_name          TEXT    NOT NULL DEFAULT '',
    endpoints_json      TEXT    NOT NULL,
    PRIMARY KEY(fabric_index, group_id),
    FOREIGN KEY(fabric_index) REFERENCES matter_fabrics(fabric_index) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS matter_groupcast_groups (
    fabric_index        INTEGER NOT NULL,
    group_id            INTEGER NOT NULL CHECK(group_id BETWEEN 1 AND 65535),
    mcast_addr_policy   INTEGER NOT NULL CHECK(mcast_addr_policy BETWEEN 0 AND 255),
    has_auxiliary_acl   INTEGER NOT NULL DEFAULT 0 CHECK(has_auxiliary_acl IN (0, 1)),
    PRIMARY KEY(fabric_index, group_id),
    FOREIGN KEY(fabric_index) REFERENCES matter_fabrics(fabric_index) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS matter_server_subscriptions (
    subscription_id     INTEGER  PRIMARY KEY,
    fabric_index        INTEGER  NOT NULL,
    peer_node_id        BLOB     NOT NULL,
    payload             BLOB     NOT NULL,
    updated_at          TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS matter_server_subscriptions_fabric
    ON matter_server_subscriptions(fabric_index);

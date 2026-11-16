-- SPDX-License-Identifier: Apache-2.0
-- Copyright (C) 2026 SukramJ.
--
-- Migration v9 (review II, decisions 107 and 109, Q-port-6): the CASE
-- resumption record carries the peer's session parameters, so one record
-- per peer serves the client role as well as the responder's
-- (store.ResumptionRecord.SessionParameters, 0 is absent); and
-- matter_owed_fabric_events keeps the fabric hooks a rollback owes until
-- the node acknowledges them (store.OwedFabricEvent). The statements are
-- store/schema.sql's.

ALTER TABLE matter_resumption ADD COLUMN session_idle_interval INTEGER NOT NULL DEFAULT 0 CHECK(session_idle_interval BETWEEN 0 AND 4294967295);
ALTER TABLE matter_resumption ADD COLUMN session_active_interval INTEGER NOT NULL DEFAULT 0 CHECK(session_active_interval BETWEEN 0 AND 4294967295);
ALTER TABLE matter_resumption ADD COLUMN session_active_threshold INTEGER NOT NULL DEFAULT 0 CHECK(session_active_threshold BETWEEN 0 AND 65535);
ALTER TABLE matter_resumption ADD COLUMN data_model_revision INTEGER NOT NULL DEFAULT 0 CHECK(data_model_revision BETWEEN 0 AND 65535);
ALTER TABLE matter_resumption ADD COLUMN interaction_model_revision INTEGER NOT NULL DEFAULT 0 CHECK(interaction_model_revision BETWEEN 0 AND 65535);
ALTER TABLE matter_resumption ADD COLUMN specification_version INTEGER NOT NULL DEFAULT 0 CHECK(specification_version BETWEEN 0 AND 4294967295);
ALTER TABLE matter_resumption ADD COLUMN max_paths_per_invoke INTEGER NOT NULL DEFAULT 0 CHECK(max_paths_per_invoke BETWEEN 0 AND 65535);
ALTER TABLE matter_resumption ADD COLUMN supported_transports INTEGER NOT NULL DEFAULT 0 CHECK(supported_transports BETWEEN 0 AND 65535);
ALTER TABLE matter_resumption ADD COLUMN max_tcp_message_size INTEGER NOT NULL DEFAULT 0 CHECK(max_tcp_message_size BETWEEN 0 AND 4294967295);

CREATE TABLE IF NOT EXISTS matter_owed_fabric_events (
    id                  INTEGER  PRIMARY KEY AUTOINCREMENT,
    kind                INTEGER  NOT NULL CHECK(kind BETWEEN 1 AND 2),
    fabric_index        INTEGER  NOT NULL CHECK(fabric_index BETWEEN 1 AND 254),
    fabric_id           BLOB     NOT NULL,
    node_id             BLOB     NOT NULL,
    root_public_key     BLOB     NOT NULL,
    compressed_id       BLOB     NOT NULL,
    vendor_id           INTEGER  NOT NULL CHECK(vendor_id BETWEEN 0 AND 65535),
    label               TEXT     NOT NULL DEFAULT ''
);

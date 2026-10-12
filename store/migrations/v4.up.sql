-- SPDX-License-Identifier: MIT
-- Copyright (C) 2026 SukramJ.
--
-- Migration v4 (go-fabric v0.3.0): the OTA requestor's DefaultOTAProviders
-- (CHANGELOG [0.3.0], ADR 0019). The statement is store/schema.sql's.

CREATE TABLE IF NOT EXISTS matter_ota_default_providers (
    fabric_index        INTEGER  NOT NULL,
    position            INTEGER  NOT NULL,
    provider_node_id    BLOB     NOT NULL,
    endpoint            INTEGER  NOT NULL CHECK(endpoint BETWEEN 0 AND 65535),
    PRIMARY KEY(fabric_index, position),
    FOREIGN KEY(fabric_index) REFERENCES matter_fabrics(fabric_index) ON DELETE CASCADE
);

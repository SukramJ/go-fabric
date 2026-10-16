-- SPDX-License-Identifier: Apache-2.0
-- Copyright (C) 2026 SukramJ.
--
-- Migration v1: matter_endpoints and matter_endpoint_allocation, the DDL
-- the package has carried since go-fabric v0.1.0. The statements are
-- schema.sql's.

CREATE TABLE IF NOT EXISTS matter_endpoints (
    source_key   TEXT    PRIMARY KEY,
    scope        TEXT    NOT NULL,
    endpoint_id  INTEGER NOT NULL UNIQUE CHECK(endpoint_id BETWEEN 1 AND 65534),
    device_type  INTEGER NOT NULL CHECK(device_type BETWEEN 0 AND 65535)
);

CREATE INDEX IF NOT EXISTS matter_endpoints_scope ON matter_endpoints(scope);

CREATE TABLE IF NOT EXISTS matter_endpoint_allocation (
    id               INTEGER PRIMARY KEY CHECK (id = 1),
    next_endpoint_id INTEGER NOT NULL CHECK(next_endpoint_id BETWEEN 2 AND 65535)
);

INSERT OR IGNORE INTO matter_endpoint_allocation (id, next_endpoint_id) VALUES (1, 2);

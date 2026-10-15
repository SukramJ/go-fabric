-- SPDX-License-Identifier: Apache-2.0
-- Copyright (C) 2026 SukramJ.
--
-- Migration v1: the client node store (brief AL §8). The statements are
-- controller/client/sqlstore/schema.sql's.

CREATE TABLE IF NOT EXISTS matter_client_values (
    node_id     TEXT     NOT NULL,
    endpoint    INTEGER  NOT NULL CHECK(endpoint BETWEEN 0 AND 65535),
    cluster     INTEGER  NOT NULL CHECK(cluster BETWEEN 0 AND 4294967295),
    attribute   INTEGER  NOT NULL CHECK(attribute BETWEEN 0 AND 4294967295),
    value       BLOB     NOT NULL,
    PRIMARY KEY(node_id, endpoint, cluster, attribute)
);

CREATE TABLE IF NOT EXISTS matter_client_versions (
    node_id     TEXT     NOT NULL,
    endpoint    INTEGER  NOT NULL CHECK(endpoint BETWEEN 0 AND 65535),
    cluster     INTEGER  NOT NULL CHECK(cluster BETWEEN 0 AND 4294967295),
    version     INTEGER  CHECK(version BETWEEN 0 AND 4294967295),
    PRIMARY KEY(node_id, endpoint, cluster)
);

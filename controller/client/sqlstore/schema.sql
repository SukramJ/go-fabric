-- SPDX-License-Identifier: Apache-2.0
-- Copyright (C) 2026 SukramJ.
--
-- The DDL for the matter_client_* tables controller/client/sqlstore reads
-- and writes: the images, settings and commissioning records of a
-- controller's client nodes. It is embedded and reachable as
-- sqlstore.Schema(); a host with its own migration tool feeds it (or
-- sqlstore.Migrations()) through that tool, a host without one calls
-- sqlstore.Apply. Every statement is idempotent.

-- matter_client_values holds one attribute value per row: a node's
-- endpoint, cluster and attribute id, and the value's TLV encoding.
CREATE TABLE IF NOT EXISTS matter_client_values (
    node_id     TEXT     NOT NULL,
    endpoint    INTEGER  NOT NULL CHECK(endpoint BETWEEN 0 AND 65535),
    cluster     INTEGER  NOT NULL CHECK(cluster BETWEEN 0 AND 4294967295),
    attribute   INTEGER  NOT NULL CHECK(attribute BETWEEN 0 AND 4294967295),
    value       BLOB     NOT NULL,
    PRIMARY KEY(node_id, endpoint, cluster, attribute)
);

-- matter_client_versions holds one row per stored cluster: its DataVersion,
-- NULL when unknown. The row exists for every cluster a Save wrote, so a
-- cluster without values loads as client.MemoryStore keeps it.
CREATE TABLE IF NOT EXISTS matter_client_versions (
    node_id     TEXT     NOT NULL,
    endpoint    INTEGER  NOT NULL CHECK(endpoint BETWEEN 0 AND 65535),
    cluster     INTEGER  NOT NULL CHECK(cluster BETWEEN 0 AND 4294967295),
    version     INTEGER  CHECK(version BETWEEN 0 AND 4294967295),
    PRIMARY KEY(node_id, endpoint, cluster)
);

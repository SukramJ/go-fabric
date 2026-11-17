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
-- endpoint, cluster and attribute id, and the value's TLV encoding; sealed
-- (migration v2) marks a secret value a sealed store sealed.
CREATE TABLE IF NOT EXISTS matter_client_values (
    node_id     TEXT     NOT NULL,
    endpoint    INTEGER  NOT NULL CHECK(endpoint BETWEEN 0 AND 65535),
    cluster     INTEGER  NOT NULL CHECK(cluster BETWEEN 0 AND 4294967295),
    attribute   INTEGER  NOT NULL CHECK(attribute BETWEEN 0 AND 4294967295),
    value       BLOB     NOT NULL,
    sealed INTEGER NOT NULL DEFAULT 0 CHECK(sealed IN (0, 1)),
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

-- matter_client_metadata holds the store's own counters and marks
-- (migration v2): sqlstore.seal_scrub_owed while a sealing's scrub of the
-- file has not finished (sqlstore.NewSealed); and the seal canary
-- sqlstore.seal_canary, whose data (migration v3) is a known value sealed
-- under the registry's sealer. Every other row leaves data NULL.
CREATE TABLE IF NOT EXISTS matter_client_metadata (
    key         TEXT     PRIMARY KEY NOT NULL,
    value       INTEGER  NOT NULL,
    data        BLOB
);

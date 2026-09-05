-- SPDX-License-Identifier: MIT
-- Copyright (C) 2026 SukramJ.
--
-- The DDL this package's queries are written against, embedded and
-- reachable as sqlitestore.Schema(). Every statement is idempotent, so a
-- host may apply it on every boot.
--
-- It is deliberately separate from store.Schema(): a host that brings its
-- own endpoint.Store never imports this package and never creates these
-- tables, and the two schemas can be applied in either order because
-- neither references the other's tables.

-- matter_endpoints maps one source identity to the Matter endpoint number
-- it was given.
--
-- source_key is endpoint.SourceKey.String() — the host's own rendering of
-- its source identity, which this module treats as opaque. That rendering
-- is the primary key, which is why the port requires it to be stable
-- byte-for-byte across releases: a changed rendering does not migrate a
-- row, it orphans it and allocates a new endpoint number.
--
-- endpoint_id is UNIQUE as well as monotonically allocated. The uniqueness
-- is the last line of defence: two sources sharing a number would serve one
-- controller two accessories under one identity.
CREATE TABLE IF NOT EXISTS matter_endpoints (
    source_key   TEXT    PRIMARY KEY,
    scope        TEXT    NOT NULL,
    endpoint_id  INTEGER NOT NULL UNIQUE CHECK(endpoint_id BETWEEN 1 AND 65534),
    device_type  INTEGER NOT NULL CHECK(device_type BETWEEN 0 AND 65535)
);

-- Garbage collection lists one scope at a time, and the assembler's read
-- path is "every row in this scope, ascending".
CREATE INDEX IF NOT EXISTS matter_endpoints_scope ON matter_endpoints(scope);

-- matter_endpoint_allocation holds the high-water mark for endpoint
-- numbers in its single row (id = 1).
--
-- A high-water mark rather than "smallest unused number" is the whole
-- point: a controller caches its accessory list by endpoint number and is
-- never told to re-read it, so a number freed by a removed device and
-- reissued to an unrelated one makes the new device arrive under the
-- removed device's identity. The counter therefore only ever advances, and
-- RemoveEndpoint does not return a number to the pool. Mirrors matter.js
-- packages/node/src/storage/server/ServerEndpointStores.ts assignNumber,
-- which allocates from a persisted counter and never rewinds it.
--
-- It starts at 2 because endpoint 0 is the RootNode and endpoint 1 the
-- Aggregator; only bridged endpoints are stored here.
CREATE TABLE IF NOT EXISTS matter_endpoint_allocation (
    id               INTEGER PRIMARY KEY CHECK (id = 1),
    next_endpoint_id INTEGER NOT NULL CHECK(next_endpoint_id BETWEEN 2 AND 65535)
);

INSERT OR IGNORE INTO matter_endpoint_allocation (id, next_endpoint_id) VALUES (1, 2);

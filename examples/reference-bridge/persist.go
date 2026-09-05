// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	// The pure-Go SQLite driver. It is already a direct requirement of
	// go-fabric itself (go.mod), so a host that persists this way adds no
	// dependency the module does not already carry.
	_ "modernc.org/sqlite"

	"github.com/SukramJ/go-fabric/endpoint"
)

// schemaDDL creates the tables go-fabric's store package queries plus the
// one table this host owns.
//
// The module deliberately ships no migration: store.New borrows an
// already-migrated *sql.DB and the host owns the DDL. The matter_* half
// below is transcribed from the module's own reference shape in
// store/testdata/schema.sql — that file lives under testdata/, so it can be
// read but never imported or embedded from here, and a copy is the only way
// a host outside the module can apply it.
//
// matter_endpoints is NOT part of that reference shape and never can be: it
// is keyed by the host's own source identity ([endpoint.SourceKey]), which
// the module treats as opaque. Every host writes this table itself.
const schemaDDL = `
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
INSERT OR IGNORE INTO matter_metadata (key, value) VALUES ('next_endpoint_id', 2);

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

CREATE TABLE IF NOT EXISTS matter_endpoints (
    source_key   TEXT    PRIMARY KEY,
    scope        TEXT    NOT NULL,
    endpoint_id  INTEGER NOT NULL UNIQUE CHECK(endpoint_id BETWEEN 1 AND 65534),
    device_type  INTEGER NOT NULL
);
`

// openDB opens (creating on first run) the daemon's SQLite file and applies
// the schema. Foreign keys are switched on per connection because SQLite
// defaults them off, and the module's cascade-delete on fabric removal is a
// schema-level FOREIGN KEY.
func openDB(ctx context.Context, path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", path, err)
	}
	if _, err := db.ExecContext(ctx, schemaDDL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return db, nil
}

// endpointRange is the Matter range a bridged endpoint number lives in:
// 0 is the root, 1 the Aggregator, 65535 is reserved.
const (
	endpointIDMin uint16 = 2
	endpointIDMax uint16 = 65534
)

// asUint16 narrows a column value, refusing anything the column's CHECK
// constraint should already have excluded. A silent truncation here would
// hand a controller a different endpoint than the row names.
func asUint16(v int64, column string) (uint16, error) {
	if v < 0 || v > 65535 {
		return 0, fmt.Errorf("column %s value %d is outside the uint16 range", column, v)
	}
	return uint16(v), nil
}

// sqliteEndpointStore implements [endpoint.Store] over matter_endpoints.
//
// The allocation rule is the one the interface documents and a controller
// depends on: numbers come off a monotonic high-water mark and a removed
// source's number is never reissued. Reissuing it would hand a new accessory
// the identity a controller still has cached for the old one.
type sqliteEndpointStore struct{ db *sql.DB }

func newEndpointStore(db *sql.DB) *sqliteEndpointStore { return &sqliteEndpointStore{db: db} }

// GetEndpoint implements [endpoint.Store].
func (s *sqliteEndpointStore) GetEndpoint(ctx context.Context, key endpoint.SourceKey) (endpoint.Record, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT scope, endpoint_id, device_type FROM matter_endpoints WHERE source_key = ?`,
		key.String())
	var (
		scope      string
		endpointID int64
		deviceType int64
	)
	if err := row.Scan(&scope, &endpointID, &deviceType); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// The assembler reads exactly this sentinel as "allocate one".
			return endpoint.Record{}, endpoint.ErrNotFound
		}
		return endpoint.Record{}, fmt.Errorf("get endpoint %q: %w", key.String(), err)
	}
	rec, err := recordFrom(key, scope, endpointID, deviceType)
	if err != nil {
		return endpoint.Record{}, fmt.Errorf("get endpoint %q: %w", key.String(), err)
	}
	return rec, nil
}

// recordFrom assembles a Record from raw column values, narrowing the two
// integer columns through the range check.
func recordFrom(key endpoint.SourceKey, scope string, endpointID, deviceType int64) (endpoint.Record, error) {
	id, err := asUint16(endpointID, "endpoint_id")
	if err != nil {
		return endpoint.Record{}, err
	}
	dt, err := asUint16(deviceType, "device_type")
	if err != nil {
		return endpoint.Record{}, err
	}
	return endpoint.Record{Key: key, Scope: scope, EndpointID: id, DeviceType: dt}, nil
}

// UpsertEndpointAssigning implements [endpoint.Store]. A zero EndpointID
// draws the next number from matter_metadata.next_endpoint_id inside the
// same transaction as the insert, so two concurrent assemblies cannot be
// handed the same number.
func (s *sqliteEndpointStore) UpsertEndpointAssigning(ctx context.Context, rec endpoint.Record) (uint16, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	id := rec.EndpointID
	if id == 0 {
		// An existing row wins over a fresh allocation: the caller does not
		// know the number, but the source has already been given one.
		err := tx.QueryRowContext(ctx,
			`SELECT endpoint_id FROM matter_endpoints WHERE source_key = ?`, rec.Key.String()).Scan(&id)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("lookup existing: %w", err)
		}
	}
	if id == 0 {
		var next int64
		if err := tx.QueryRowContext(ctx,
			`SELECT value FROM matter_metadata WHERE key = 'next_endpoint_id'`).Scan(&next); err != nil {
			return 0, fmt.Errorf("read next_endpoint_id: %w", err)
		}
		if next < int64(endpointIDMin) || next > int64(endpointIDMax) {
			return 0, fmt.Errorf("next_endpoint_id %d out of the bridged range [%d, %d]", next, endpointIDMin, endpointIDMax)
		}
		id, err = asUint16(next, "next_endpoint_id")
		if err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE matter_metadata SET value = ? WHERE key = 'next_endpoint_id'`, next+1); err != nil {
			return 0, fmt.Errorf("advance next_endpoint_id: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO matter_endpoints (source_key, scope, endpoint_id, device_type)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(source_key) DO UPDATE SET scope = excluded.scope, device_type = excluded.device_type`,
		rec.Key.String(), rec.Scope, int64(id), int64(rec.DeviceType)); err != nil {
		return 0, fmt.Errorf("upsert endpoint: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return id, nil
}

// ListEndpoints implements [endpoint.Store]. An empty scope matches every row.
func (s *sqliteEndpointStore) ListEndpoints(ctx context.Context, scope string) ([]endpoint.Record, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT source_key, scope, endpoint_id, device_type FROM matter_endpoints
		 WHERE ? = '' OR scope = ? ORDER BY endpoint_id ASC`, scope, scope)
	if err != nil {
		return nil, fmt.Errorf("list endpoints: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []endpoint.Record
	for rows.Next() {
		var (
			key        string
			rowScope   string
			endpointID int64
			deviceType int64
		)
		if err := rows.Scan(&key, &rowScope, &endpointID, &deviceType); err != nil {
			return nil, fmt.Errorf("scan endpoint row: %w", err)
		}
		rec, err := recordFrom(endpoint.StringKey(key), rowScope, endpointID, deviceType)
		if err != nil {
			return nil, fmt.Errorf("endpoint row %q: %w", key, err)
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate endpoint rows: %w", err)
	}
	return out, nil
}

// RemoveEndpoint implements [endpoint.Store]. Idempotent by contract: the
// assembler's garbage collection does not coordinate with concurrent
// removals, so a missing row is a success. The number is deliberately NOT
// returned to the pool — next_endpoint_id only ever advances.
func (s *sqliteEndpointStore) RemoveEndpoint(ctx context.Context, key endpoint.SourceKey) error {
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM matter_endpoints WHERE source_key = ?`, key.String()); err != nil {
		return fmt.Errorf("remove endpoint %q: %w", key.String(), err)
	}
	return nil
}

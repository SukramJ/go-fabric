// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package sqlitestore is a persistent [endpoint.Store] over SQLite.
//
// It exists because endpoint identity is the one piece of bridge state that
// MUST survive a restart. A controller keys its accessory list on the
// endpoint number and is never told to re-read it, so a bridge that forgets
// its numbers either loses every accessory on every commissioned controller
// or — worse — hands a new device the number a removed one held. An
// in-memory store makes a bridge run and quietly makes it wrong.
//
// The package takes an already-open [database/sql.DB] and never opens one,
// so it imports no driver: the host picks its own (this module's own tests
// use modernc.org/sqlite) and owns the DSN. It is a separate package from
// [endpoint] for the same reason it is separate from [store] — a host that
// brings its own endpoint.Store never imports it, links nothing of it, and
// creates none of its tables.
//
// Usage:
//
//	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")
//	...
//	if err := sqlitestore.Apply(ctx, db); err != nil { ... }
//	asm, err := endpoint.New(sqlitestore.New(db), cfg, logger)
//
// A host whose [endpoint.SourceKey] is not an [endpoint.StringKey] and
// that reads keys back from ListEndpoints passes [WithKeyDecoder].
package sqlitestore

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"

	"github.com/SukramJ/go-fabric/endpoint"
)

// schemaDDL is embedded rather than left for the host to transcribe: the
// queries below are written against this exact text, and a hand-copy is
// what drifts from it unnoticed.
//
//go:embed schema.sql
var schemaDDL string

// Bounds on a stored endpoint number. The port documents the persisted
// range as [1, 65534] — 0 is the RootNode and 65535 is reserved — while
// fresh allocation starts at 2 because endpoint 1 is the Aggregator. A
// number below allocMin can therefore still be stored (a host restoring one
// it allocated elsewhere), but is never handed out by this package.
const (
	endpointIDMin uint16 = 1
	allocMin      uint16 = 2
	endpointIDMax uint16 = 65534
)

// ErrEndpointsExhausted is returned when every number from 2 to 65534 is
// held by a row. Only removing a source frees one; allocation then wraps
// to it (matter.js ServerEndpointStores.ts #nextFreeNumber, :169-183).
var ErrEndpointsExhausted = errors.New("sqlitestore: no endpoint numbers left below 65535")

// Schema returns the DDL for the tables this package reads and writes, as
// one SQL script. A host with its own migration tool feeds this text
// through it; a host without one calls [Apply].
func Schema() string { return schemaDDL }

// Apply executes [Schema] against db. Every statement is idempotent, so
// calling it on every boot is the intended use.
func Apply(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, schemaDDL); err != nil {
		return fmt.Errorf("sqlitestore: apply schema: %w", err)
	}
	return nil
}

// Store is a SQLite-backed [endpoint.Store].
type Store struct {
	db     *sql.DB
	decode func(string) endpoint.SourceKey
}

// Option configures a [Store].
type Option func(*Store)

// WithKeyDecoder tells the store how to turn a persisted
// [endpoint.SourceKey.String] rendering back into the host's own key type.
// The default wraps it in an [endpoint.StringKey].
//
// A host whose keys are StringKey needs nothing here. Any other host that
// compares the keys [Store.ListEndpoints] returns with its own, as Go
// interface values, does: an [endpoint.StringKey] and a host's own key type
// never compare equal even when both render the same text. Lookups by key
// (GetEndpoint, RemoveEndpoint) match the rendering and need no decoder,
// and since v0.13.0 the assembler compares no listed key — it collects
// nothing (endpoint.Snapshot) — so a missing decoder no longer costs an
// endpoint number.
//
// The decoder is the host's, because parsing a rendering back into its
// parts is only guesswork when someone other than its author does it.
func WithKeyDecoder(decode func(rendered string) endpoint.SourceKey) Option {
	return func(s *Store) {
		if decode != nil {
			s.decode = decode
		}
	}
}

// New returns a [Store] over db. The database must already carry the
// tables — see [Apply].
func New(db *sql.DB, opts ...Option) *Store {
	s := &Store{
		db:     db,
		decode: func(rendered string) endpoint.SourceKey { return endpoint.StringKey(rendered) },
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// GetEndpoint implements [endpoint.Store]. The returned record carries the
// key the caller passed, not a decoded one, so a host's own key type
// survives the round trip unchanged.
func (s *Store) GetEndpoint(ctx context.Context, key endpoint.SourceKey) (endpoint.Record, error) {
	var (
		scope      string
		endpointID int64
		deviceType int64
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT scope, endpoint_id, device_type FROM matter_endpoints WHERE source_key = ?`,
		key.String()).Scan(&scope, &endpointID, &deviceType)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// The assembler reads exactly this sentinel as "allocate one";
			// any other error here would fail the whole assembly instead.
			return endpoint.Record{}, endpoint.ErrNotFound
		}
		return endpoint.Record{}, fmt.Errorf("sqlitestore: get endpoint %q: %w", key.String(), err)
	}
	rec, err := recordFrom(key, scope, endpointID, deviceType)
	if err != nil {
		return endpoint.Record{}, fmt.Errorf("sqlitestore: get endpoint %q: %w", key.String(), err)
	}
	return rec, nil
}

// UpsertEndpointAssigning implements [endpoint.Store].
//
// The whole operation runs in one transaction, so two concurrent
// assemblies cannot be handed the same number: the counter read, the
// counter bump and the insert commit together or not at all.
//
// The transaction is opened with BEGIN IMMEDIATE rather than through
// [database/sql.DB.BeginTx], and that is load-bearing rather than stylistic.
// database/sql issues a plain BEGIN, which SQLite starts as a read
// transaction and upgrades on the first write; two of these racing cannot
// both upgrade, and SQLite refuses the second with SQLITE_BUSY *without*
// consulting the busy handler, because waiting would deadlock. Taking the
// write lock up front makes the loser wait out the host's busy_timeout
// instead — so a host's DSN should carry one (`_pragma=busy_timeout(...)`
// for modernc.org/sqlite).
func (s *Store) UpsertEndpointAssigning(ctx context.Context, rec endpoint.Record) (uint16, error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return 0, fmt.Errorf("sqlitestore: acquire connection: %w", err)
	}
	defer func() { _ = conn.Close() }()

	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return 0, fmt.Errorf("sqlitestore: begin immediate: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			// The rollback must still run when ctx is the reason we are
			// here, or the connection returns to the pool mid-transaction.
			_, _ = conn.ExecContext(context.WithoutCancel(ctx), `ROLLBACK`)
		}
	}()

	id, err := resolveID(ctx, conn, rec)
	if err != nil {
		return 0, err
	}
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO matter_endpoints (source_key, scope, endpoint_id, device_type)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(source_key) DO UPDATE SET scope = excluded.scope, device_type = excluded.device_type`,
		rec.Key.String(), rec.Scope, int64(id), int64(rec.DeviceType)); err != nil {
		return 0, fmt.Errorf("sqlitestore: upsert endpoint %q: %w", rec.Key.String(), err)
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return 0, fmt.Errorf("sqlitestore: commit: %w", err)
	}
	committed = true
	return id, nil
}

// txConn is the subset of [database/sql.Conn] the allocation helpers use.
// They run inside a transaction the caller opened on that one connection,
// which is why they take it rather than a *sql.DB.
type txConn interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// resolveID decides which number the upsert writes, inside the caller's
// transaction.
//
// A number already stored for this source wins over anything the caller
// passed: the caller may be re-stating a record it built from a snapshot,
// and the stored number is the one a controller has cached. Only when the
// source is new does a caller-supplied number, or a fresh allocation, take
// effect.
func resolveID(ctx context.Context, tx txConn, rec endpoint.Record) (uint16, error) {
	var stored int64
	err := tx.QueryRowContext(ctx,
		`SELECT endpoint_id FROM matter_endpoints WHERE source_key = ?`, rec.Key.String()).Scan(&stored)
	switch {
	case err == nil:
		return asUint16(stored, "endpoint_id")
	case !errors.Is(err, sql.ErrNoRows):
		return 0, fmt.Errorf("sqlitestore: look up existing endpoint %q: %w", rec.Key.String(), err)
	}

	if rec.EndpointID != 0 {
		if rec.EndpointID < endpointIDMin || rec.EndpointID > endpointIDMax {
			return 0, fmt.Errorf("sqlitestore: endpoint id %d is outside [%d, %d]",
				rec.EndpointID, endpointIDMin, endpointIDMax)
		}
		// Keep the counter above a number the caller placed by hand,
		// otherwise the next allocation collides with it and the UNIQUE
		// index turns a recoverable bookkeeping slip into a failed
		// assembly.
		if err := raiseCounter(ctx, tx, int64(rec.EndpointID)+1); err != nil {
			return 0, err
		}
		return rec.EndpointID, nil
	}
	return allocate(ctx, tx)
}

// allocate hands out the next free number and advances the counter past
// it. Mirrors matter.js
// packages/node/src/storage/server/ServerEndpointStores.ts at 0ceb202d:
//
//   - the counter is the persisted next number (:13, :37-43), lifted past
//     the highest number a row holds — matter.js reserves every persisted
//     number at load (:52-60) and lifts the counter past each it claims
//     (:156-158), so a row above the counter is never reissued;
//   - from the counter, the first number no row holds, wrapping to
//     allocMin after endpointIDMax (#nextFreeNumber :169-181, which wraps
//     to 1 after 0xFFFE and skips the numbers held; 1 is the Aggregator
//     here);
//   - [ErrEndpointsExhausted] only when a full pass finds none (:183).
//
// Until the top of the range is reached a removed row's number is not
// reissued. Once a row holds the top number, the lift puts the counter
// past it on every allocation, so the pass wraps and takes the lowest free
// number from allocMin; matter.js keeps its in-memory counter after the
// number it last assigned (:178) and walks on from there.
func allocate(ctx context.Context, tx txConn) (uint16, error) {
	var next, highest int64
	if err := tx.QueryRowContext(ctx,
		`SELECT next_endpoint_id FROM matter_endpoint_allocation WHERE id = 1`).Scan(&next); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, errors.New("sqlitestore: allocation row missing — was Apply run on this database?")
		}
		return 0, fmt.Errorf("sqlitestore: read allocation counter: %w", err)
	}
	if next < int64(allocMin) {
		return 0, fmt.Errorf("sqlitestore: allocation counter %d is below the first bridged endpoint %d", next, allocMin)
	}
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(endpoint_id), 0) FROM matter_endpoints`).Scan(&highest); err != nil {
		return 0, fmt.Errorf("sqlitestore: read highest endpoint: %w", err)
	}
	next = max(next, highest+1)

	id, err := firstFree(ctx, tx, next, int64(endpointIDMax))
	if err == nil && id == 0 {
		// The pass wraps to allocMin and ends below where it started.
		id, err = firstFree(ctx, tx, int64(allocMin), min(next-1, int64(endpointIDMax)))
	}
	if err != nil {
		return 0, err
	}
	if id == 0 {
		return 0, ErrEndpointsExhausted
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE matter_endpoint_allocation SET next_endpoint_id = ? WHERE id = 1`, id+1); err != nil {
		return 0, fmt.Errorf("sqlitestore: advance allocation counter: %w", err)
	}
	return asUint16(id, "endpoint_id")
}

// firstFree returns the lowest number in [lo, hi] no row holds, or 0 when
// every one is held (or the range is empty). It is lo itself when lo is
// free, and otherwise the successor of the first held number at or above
// lo whose successor is free — one indexed query rather than a walk, so a
// nearly full range costs no more than an empty one.
func firstFree(ctx context.Context, tx txConn, lo, hi int64) (int64, error) {
	if lo > hi {
		return 0, nil
	}
	var id sql.NullInt64
	err := tx.QueryRowContext(ctx,
		`SELECT MIN(n) FROM (
		   SELECT ?1 AS n WHERE NOT EXISTS (SELECT 1 FROM matter_endpoints WHERE endpoint_id = ?1)
		   UNION ALL
		   SELECT e.endpoint_id + 1 FROM matter_endpoints e
		    WHERE e.endpoint_id >= ?1 AND e.endpoint_id < ?2
		      AND NOT EXISTS (SELECT 1 FROM matter_endpoints f WHERE f.endpoint_id = e.endpoint_id + 1)
		 )`, lo, hi).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("sqlitestore: find a free endpoint number: %w", err)
	}
	if !id.Valid {
		return 0, nil
	}
	return id.Int64, nil
}

// raiseCounter moves the high-water mark up to floor, never down.
func raiseCounter(ctx context.Context, tx txConn, floor int64) error {
	if floor > int64(endpointIDMax)+1 {
		floor = int64(endpointIDMax) + 1
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE matter_endpoint_allocation SET next_endpoint_id = ?
		 WHERE id = 1 AND next_endpoint_id < ?`, floor, floor); err != nil {
		return fmt.Errorf("sqlitestore: raise allocation counter: %w", err)
	}
	return nil
}

// ListEndpoints implements [endpoint.Store]. An empty scope matches every
// row. Keys come back through the configured decoder — see
// [WithKeyDecoder].
func (s *Store) ListEndpoints(ctx context.Context, scope string) ([]endpoint.Record, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT source_key, scope, endpoint_id, device_type FROM matter_endpoints
		 WHERE ? = '' OR scope = ? ORDER BY endpoint_id ASC`, scope, scope)
	if err != nil {
		return nil, fmt.Errorf("sqlitestore: list endpoints: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []endpoint.Record
	for rows.Next() {
		var (
			rendered   string
			rowScope   string
			endpointID int64
			deviceType int64
		)
		if err := rows.Scan(&rendered, &rowScope, &endpointID, &deviceType); err != nil {
			return nil, fmt.Errorf("sqlitestore: scan endpoint row: %w", err)
		}
		rec, err := recordFrom(s.decode(rendered), rowScope, endpointID, deviceType)
		if err != nil {
			return nil, fmt.Errorf("sqlitestore: endpoint row %q: %w", rendered, err)
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlitestore: iterate endpoint rows: %w", err)
	}
	return out, nil
}

// RemoveEndpoint implements [endpoint.Store]. Idempotent by contract, and
// deliberately does NOT return the number to the pool: the counter only
// ever advances.
func (s *Store) RemoveEndpoint(ctx context.Context, key endpoint.SourceKey) error {
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM matter_endpoints WHERE source_key = ?`, key.String()); err != nil {
		return fmt.Errorf("sqlitestore: remove endpoint %q: %w", key.String(), err)
	}
	return nil
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

// asUint16 narrows a column value, refusing anything the column's CHECK
// constraint should already have excluded. A silent truncation here would
// hand a controller a different endpoint than the row names.
func asUint16(v int64, column string) (uint16, error) {
	if v < 0 || v > 65535 {
		return 0, fmt.Errorf("sqlitestore: column %s value %d is outside the uint16 range", column, v)
	}
	return uint16(v), nil //nolint:gosec // G115: bounded by the check above
}

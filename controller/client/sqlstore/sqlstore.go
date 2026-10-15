// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package sqlstore is a durable [client.Store] on SQLite (brief AL §8;
// ADR 0020, stage 4): the images, settings and commissioning records of a
// controller's client nodes, in the tables matter_client_values and
// matter_client_versions. It is the place of matter.js's storage contexts
// under nodes/<id>/endpoints/<ep>/<cluster>/<attr> with the version under
// "__version__" (packages/node/src/storage/client/ at 0ceb202d).
//
// The schema ships the way store/ ships the device's: [Schema] is the DDL
// as one idempotent script, [Apply] runs it, [Migrations] are the same
// schema as versioned steps for a host's own migration tool, and
// [SchemaFingerprint] tells a host that its tables match this release's.
// The list is this package's own, so a device-side host's pin on
// len(store.Migrations()) does not move for a controller table.
//
// The package takes an open *sql.DB and never opens one; it is written
// against modernc.org/sqlite, which the module already depends on.
package sqlstore

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"maps"
	"slices"

	"github.com/SukramJ/go-fabric/controller/client"
)

// Store is a [client.Store] over a database the schema was applied to. It
// is safe for concurrent use: database/sql is, and SQLite serializes the
// writers.
type Store struct {
	db *sql.DB
}

var _ client.Store = (*Store)(nil)

// New returns the store over db, which [Apply] (or the host's migration
// of [Migrations]) prepared.
func New(db *sql.DB) *Store { return &Store{db: db} }

type clusterKey struct {
	ep      uint16
	cluster uint32
}

// Load implements client.Store: the endpoints in number order, their
// clusters in id order. A cluster with a version row but no values loads
// empty, as client.MemoryStore keeps a cluster a Save wrote.
func (s *Store) Load(nodeID string) ([]client.StoredEndpoint, error) {
	ctx := context.Background()
	clusters := map[clusterKey]*client.StoredCluster{}
	get := func(k clusterKey) *client.StoredCluster {
		c := clusters[k]
		if c == nil {
			c = &client.StoredCluster{ID: k.cluster, Values: map[uint32][]byte{}}
			clusters[k] = c
		}
		return c
	}
	rows, err := s.db.QueryContext(ctx, `SELECT endpoint, cluster, attribute, value FROM matter_client_values WHERE node_id = ?`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("sqlstore: load %q: %w", nodeID, err)
	}
	for rows.Next() {
		var k clusterKey
		var attr uint32
		var value []byte
		if err := rows.Scan(&k.ep, &k.cluster, &attr, &value); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("sqlstore: load %q: %w", nodeID, err)
		}
		get(k).Values[attr] = value
	}
	if err := closeRows(rows); err != nil {
		return nil, fmt.Errorf("sqlstore: load %q: %w", nodeID, err)
	}
	rows, err = s.db.QueryContext(ctx, `SELECT endpoint, cluster, version FROM matter_client_versions WHERE node_id = ?`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("sqlstore: load %q: %w", nodeID, err)
	}
	for rows.Next() {
		var k clusterKey
		var version sql.NullInt64
		if err := rows.Scan(&k.ep, &k.cluster, &version); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("sqlstore: load %q: %w", nodeID, err)
		}
		c := get(k)
		if version.Valid {
			v := uint32(version.Int64) //nolint:gosec // the column's CHECK bounds it
			c.Version = &v
		}
	}
	if err := closeRows(rows); err != nil {
		return nil, fmt.Errorf("sqlstore: load %q: %w", nodeID, err)
	}
	keys := make([]clusterKey, 0, len(clusters))
	for k := range clusters {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b clusterKey) int {
		return cmp.Or(cmp.Compare(a.ep, b.ep), cmp.Compare(a.cluster, b.cluster))
	})
	out := []client.StoredEndpoint{}
	for _, k := range keys {
		if len(out) == 0 || out[len(out)-1].Number != k.ep {
			out = append(out, client.StoredEndpoint{Number: k.ep})
		}
		ep := &out[len(out)-1]
		ep.Clusters = append(ep.Clusters, *clusters[k])
	}
	return out, nil
}

func closeRows(rows *sql.Rows) error {
	err := rows.Err()
	if cerr := rows.Close(); err == nil {
		err = cerr
	}
	return err
}

// Save implements client.Store in one transaction: each given attribute is
// written, a nil value deleted (EndpointStore.ts:104-105 deletes an
// undefined key); the cluster's version is set to version, NULL for nil,
// as client.MemoryStore replaces it.
func (s *Store) Save(nodeID string, ep uint16, cluster uint32, values map[uint32][]byte, version *uint32) error {
	ctx := context.Background()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlstore: save %q: %w", nodeID, err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, attr := range slices.Sorted(maps.Keys(values)) {
		v := values[attr]
		if v == nil {
			_, err = tx.ExecContext(ctx, `DELETE FROM matter_client_values WHERE node_id = ? AND endpoint = ? AND cluster = ? AND attribute = ?`, nodeID, ep, cluster, attr)
		} else {
			_, err = tx.ExecContext(ctx, `INSERT INTO matter_client_values(node_id, endpoint, cluster, attribute, value) VALUES(?, ?, ?, ?, ?)
				ON CONFLICT(node_id, endpoint, cluster, attribute) DO UPDATE SET value = excluded.value`, nodeID, ep, cluster, attr, v)
		}
		if err != nil {
			return fmt.Errorf("sqlstore: save %q: %w", nodeID, err)
		}
	}
	var ver any
	if version != nil {
		ver = int64(*version)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO matter_client_versions(node_id, endpoint, cluster, version) VALUES(?, ?, ?, ?)
		ON CONFLICT(node_id, endpoint, cluster) DO UPDATE SET version = excluded.version`, nodeID, ep, cluster, ver); err != nil {
		return fmt.Errorf("sqlstore: save %q: %w", nodeID, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlstore: save %q: %w", nodeID, err)
	}
	return nil
}

// erase deletes the rows of both tables that where selects.
func (s *Store) erase(what, where string, args ...any) error {
	ctx := context.Background()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlstore: erase %s: %w", what, err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, table := range []string{"matter_client_values", "matter_client_versions"} {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE `+where, args...); err != nil { //nolint:gosec // fixed table names and clauses
			return fmt.Errorf("sqlstore: erase %s: %w", what, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlstore: erase %s: %w", what, err)
	}
	return nil
}

// EraseCluster implements client.Store.
func (s *Store) EraseCluster(nodeID string, ep uint16, cluster uint32) error {
	return s.erase("cluster", `node_id = ? AND endpoint = ? AND cluster = ?`, nodeID, ep, cluster)
}

// EraseEndpoint implements client.Store.
func (s *Store) EraseEndpoint(nodeID string, ep uint16) error {
	return s.erase("endpoint", `node_id = ? AND endpoint = ?`, nodeID, ep)
}

// EraseNode implements client.Store.
func (s *Store) EraseNode(nodeID string) error {
	return s.erase("node", `node_id = ?`, nodeID)
}

// NodeIDs implements client.Store: the distinct node ids of both tables,
// sorted.
func (s *Store) NodeIDs() ([]string, error) {
	rows, err := s.db.QueryContext(context.Background(),
		`SELECT node_id FROM matter_client_values UNION SELECT node_id FROM matter_client_versions ORDER BY node_id`)
	if err != nil {
		return nil, fmt.Errorf("sqlstore: node ids: %w", err)
	}
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("sqlstore: node ids: %w", err)
		}
		out = append(out, id)
	}
	if err := closeRows(rows); err != nil {
		return nil, fmt.Errorf("sqlstore: node ids: %w", err)
	}
	return out, nil
}

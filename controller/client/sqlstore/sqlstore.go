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
	"encoding/binary"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/store"
)

// Store is a [client.Store] over a database the schema was applied to. It
// is safe for concurrent use: database/sql is, and SQLite serializes the
// writers.
type Store struct {
	db *sql.DB
	// sealer seals the secret values ([NewSealed]); nil keeps them clear.
	sealer store.Sealer
}

var _ client.Store = (*Store)(nil)

// New returns the store over db, which [Apply] (or the host's migration
// of [Migrations]) prepared. It keeps every value as it is; a sealed
// value it reads is [store.ErrNoSealer], and it refuses to overwrite one
// with a clear value, and to write a secret value at all into a registry
// that carries the seal canary ([NewSealed]): [store.ErrNoSealer], with
// nothing of the Save written.
func New(db *sql.DB) *Store { return &Store{db: db} }

// The secret values the store seals, each its own row: a node's kept
// onboarding code — the QR or manual pairing code, which holds the
// device's setup passcode (controller/client onboarding.go
// onboardingCluster, onbCode) — and the controller's ICD Check-In key and
// pending key on a node (controller/client icd.go icdCluster, icdFieldKey,
// icdFieldPendingKey); [secretValues] lists them.
// TestSealedValuesNameTheirSource holds the copies to them. The other values of those clusters — the code's source, time and
// checks, the registration's counters, subject and client type — are not
// secret and stay clear.
const (
	onboardingCluster   uint32 = 0xFFFF_FFFC
	onboardingCodeValue uint32 = 0
	icdCluster          uint32 = 0xFFFF_FFFD
	icdKeyValue         uint32 = 0
	icdPendingKeyValue  uint32 = 1
)

// isSecret reports whether the value of cluster's attribute is sealed.
func isSecret(cluster, attribute uint32) bool {
	return slices.Contains(secretValues, secretValue{cluster, attribute})
}

// sealAAD is the aad of one sealed value, binding it to its table, column
// and row, so a value moved to another row does not open; each part is
// length-prefixed, as store's sealAAD builds its own.
func sealAAD(nodeID string, ep uint16, cluster, attribute uint32) []byte {
	b := []byte(sealAADPrefix)
	add := func(p []byte) { b = appendAADPart(b, p) }
	add([]byte("matter_client_values"))
	add([]byte("value"))
	add([]byte(nodeID))
	add(binary.BigEndian.AppendUint16(nil, ep))
	add(binary.BigEndian.AppendUint32(nil, cluster))
	add(binary.BigEndian.AppendUint32(nil, attribute))
	return b
}

// NewSealed returns the store over db with its secret values sealed under
// s — the kept onboarding codes and the ICD Check-In keys ([isSecret]);
// every other value stays clear. db must carry migration v3 ([Apply]). It
// does what [store.NewSealed] does for the device's store
// (store/seal.go, NewSealed and sealClearRows), on the registry's own
// tables.
//
// The seal canary (decision 194). The first NewSealed marks the registry
// with one: a reserved matter_client_metadata row holds a known value
// sealed under s. Every later NewSealed opens the canary first, so a
// sealer under another key fails with [store.ErrUnseal] before anything is
// written — whether or not the registry holds a sealed value yet — and a
// registry that carries the canary has a secret value in clear refused
// with ErrUnseal instead of adopted. A store without a sealer ([New])
// refuses to write a secret value into a registry that carries it
// ([store.ErrNoSealer]). The canary is not tamper protection: whoever can
// write the file can delete the row, and the registry then counts as
// never sealed. [Reseal] moves it to a new sealer; the controller's
// factory reset drops it once the registry is empty.
//
// The first NewSealed (no canary yet) checks the key on one sealed value
// the registry already holds, if any, then seals every secret value it
// finds in clear — a registry a store without a sealer wrote — and writes
// the canary, in one transaction that also records that a scrub of the
// file is owed when it sealed a value; a failure leaves the registry as it
// was. While a scrub is owed NewSealed rewrites the file and empties the
// -wal ([store.Scrub]), so the clear values do not stay behind in free
// pages, and then deletes the record. A scrub that does not finish (a full
// disk, a reader holding the -wal) does not fail NewSealed (decision 132):
// the values are sealed, the store is returned, the scrub stays owed
// ([Store.ScrubOwed]) and every later NewSealed tries it again.
//
// While the store is open it writes no secret value in clear, and a secret
// value it reads without the seal mark is refused with [store.ErrUnseal].
func NewSealed(ctx context.Context, db *sql.DB, s store.Sealer) (*Store, error) {
	if s == nil {
		return nil, errors.New("sqlstore: NewSealed: nil Sealer")
	}
	st := &Store{db: db, sealer: s}
	if err := checkSchema(ctx, db, "NewSealed"); err != nil {
		return nil, err
	}
	_, marked, err := readCanary(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("sqlstore: NewSealed: %w", err)
	}
	if !marked {
		// The transaction reads the canary again: a NewSealed under another
		// key may have marked the registry since.
		if marked, _, err = st.sealUnmarked(ctx); err != nil {
			return nil, err
		}
	}
	if marked {
		if err := st.openMarked(ctx); err != nil {
			return nil, err
		}
	}
	if owed, err := st.ScrubOwed(ctx); err != nil {
		return nil, fmt.Errorf("sqlstore: NewSealed: %w", err)
	} else if !owed {
		return st, nil
	}
	if err := store.Scrub(ctx, db); err != nil {
		// Owed, and visible through ScrubOwed: the values are sealed, and a
		// full disk or a reader must not keep the controller down
		// (decision 132, as store's sealClearRows).
		return st, nil //nolint:nilerr // the owed scrub is a state, not a failure
	}
	// A mark that will not go keeps ScrubOwed true and the next NewSealed
	// scrubs again: the state of an unfinished scrub.
	_, _ = db.ExecContext(ctx, `DELETE FROM matter_client_metadata WHERE key = ?`, metadataKeySealScrubOwed)
	return st, nil
}

// metadataKeySealScrubOwed marks a sealing whose [store.Scrub] has not
// finished: set in the sealing's (or [Reseal]'s) transaction, deleted once
// the scrub ran (store's metadataKeySealScrubOwed, in the registry's own
// table; [Store.ScrubOwed]).
const metadataKeySealScrubOwed = "sqlstore.seal_scrub_owed"

// openMarked is NewSealed's open of a registry that carries the canary:
// the canary opened under the sealer (the key check) and a clear secret
// value refused; nothing is written.
func (s *Store) openMarked(ctx context.Context) error {
	data, _, err := readCanary(ctx, s.db)
	if err != nil {
		return fmt.Errorf("sqlstore: NewSealed: %w", err)
	}
	if err := openCanary(s.sealer, data); err != nil {
		return fmt.Errorf("sqlstore: NewSealed: the sealer does not open the registry's seal canary: %w", err)
	}
	if v, err := clearSecretValue(ctx, s.db); err != nil {
		return fmt.Errorf("sqlstore: NewSealed: %w", err)
	} else if v != "" {
		return fmt.Errorf("sqlstore: NewSealed: %w: the value of %s is not sealed in a sealed registry", store.ErrUnseal, v)
	}
	return nil
}

// sealUnmarked seals a registry without the canary in one transaction that
// holds the write lock first: the canary read again (marked reports one a
// concurrent NewSealed wrote meanwhile, and nothing is written), the key
// check on one sealed value, every clear secret value sealed and the
// canary written, with the scrub owed recorded when it sealed a value. It
// returns how many values it sealed.
func (s *Store) sealUnmarked(ctx context.Context) (marked bool, sealed int, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, 0, fmt.Errorf("sqlstore: seal: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	// A write first, so the transaction holds the write lock before it
	// reads, as store's sealUnmarked takes it.
	if _, err := tx.ExecContext(ctx, `UPDATE matter_client_metadata SET value = value WHERE key = ?`, metadataKeySealScrubOwed); err != nil {
		return false, 0, fmt.Errorf("sqlstore: seal: %w", err)
	}
	if _, marked, err := readCanary(ctx, tx); err != nil {
		return false, 0, fmt.Errorf("sqlstore: seal: %w", err)
	} else if marked {
		return true, 0, nil
	}
	// The key check before the sealing: a wrong key must not seal the
	// clear values beside values it does not open.
	var (
		nodeID        string
		ep            uint16
		cluster, attr uint32
		value         []byte
	)
	err = tx.QueryRowContext(ctx, `SELECT node_id, endpoint, cluster, attribute, value FROM matter_client_values WHERE sealed = 1 LIMIT 1`).Scan(&nodeID, &ep, &cluster, &attr, &value)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return false, 0, fmt.Errorf("sqlstore: open sealed store: %w", err)
	default:
		if _, err := s.sealer.Open(value, sealAAD(nodeID, ep, cluster, attr)); err != nil {
			return false, 0, fmt.Errorf("sqlstore: open sealed store: %w: %w", store.ErrUnseal, err)
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT node_id, endpoint, cluster, attribute, value FROM matter_client_values WHERE sealed = 0 AND cluster IN (`+secretClusters()+`)`) //nolint:gosec // the package's constants
	if err != nil {
		return false, 0, fmt.Errorf("sqlstore: seal: %w", err)
	}
	type row struct {
		nodeID        string
		ep            uint16
		cluster, attr uint32
		value         []byte
	}
	var plain []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.nodeID, &r.ep, &r.cluster, &r.attr, &r.value); err != nil {
			_ = rows.Close()
			return false, 0, fmt.Errorf("sqlstore: seal: %w", err)
		}
		if isSecret(r.cluster, r.attr) {
			plain = append(plain, r)
		}
	}
	if err := closeRows(rows); err != nil {
		return false, 0, fmt.Errorf("sqlstore: seal: %w", err)
	}
	for _, r := range plain {
		v, err := s.sealer.Seal(r.value, sealAAD(r.nodeID, r.ep, r.cluster, r.attr))
		if err != nil {
			return false, 0, fmt.Errorf("sqlstore: seal: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE matter_client_values SET value = ?, sealed = 1 WHERE node_id = ? AND endpoint = ? AND cluster = ? AND attribute = ?`, v, r.nodeID, r.ep, r.cluster, r.attr); err != nil {
			return false, 0, fmt.Errorf("sqlstore: seal: %w", err)
		}
	}
	if err := writeCanary(ctx, tx, s.sealer); err != nil {
		return false, 0, fmt.Errorf("sqlstore: seal: %w", err)
	}
	if len(plain) > 0 {
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO matter_client_metadata (key, value) VALUES (?, 1)`, metadataKeySealScrubOwed); err != nil {
			return false, 0, fmt.Errorf("sqlstore: seal: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return false, 0, fmt.Errorf("sqlstore: seal: %w", err)
	}
	return false, len(plain), nil
}

// readValue is the stored value of one row as the store reads it: a
// sealed value opened, or [store.ErrNoSealer] without a sealer; a secret
// value in clear under a sealer is [store.ErrUnseal].
func (s *Store) readValue(nodeID string, ep uint16, cluster, attr uint32, value []byte, sealed bool) ([]byte, error) {
	switch {
	case sealed && s.sealer == nil:
		return nil, store.ErrNoSealer
	case sealed:
		v, err := s.sealer.Open(value, sealAAD(nodeID, ep, cluster, attr))
		if err != nil {
			return nil, fmt.Errorf("%w: %w", store.ErrUnseal, err)
		}
		return v, nil
	case s.sealer != nil && isSecret(cluster, attr):
		return nil, fmt.Errorf("%w: a secret value in clear under a sealer", store.ErrUnseal)
	}
	return value, nil
}

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
	rows, err := s.db.QueryContext(ctx, `SELECT endpoint, cluster, attribute, value, sealed FROM matter_client_values WHERE node_id = ?`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("sqlstore: load %q: %w", nodeID, err)
	}
	for rows.Next() {
		var k clusterKey
		var attr uint32
		var value []byte
		var sealed bool
		if err := rows.Scan(&k.ep, &k.cluster, &attr, &value, &sealed); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("sqlstore: load %q: %w", nodeID, err)
		}
		if value, err = s.readValue(nodeID, k.ep, k.cluster, attr, value, sealed); err != nil {
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
			err = s.writeValue(ctx, tx, nodeID, ep, cluster, attr, v)
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

// writeValue writes one value: a secret one sealed under a sealer; without
// one, in clear, and a sealed row it would overwrite, or a secret value in
// a registry that carries the seal canary, is refused with
// [store.ErrNoSealer] (nothing is written).
func (s *Store) writeValue(ctx context.Context, tx *sql.Tx, nodeID string, ep uint16, cluster, attr uint32, v []byte) error {
	if s.sealer == nil && isSecret(cluster, attr) {
		if err := refuseClearSecretWrite(ctx, tx); err != nil {
			return err
		}
	}
	if s.sealer != nil && isSecret(cluster, attr) {
		sealedValue, err := s.sealer.Seal(v, sealAAD(nodeID, ep, cluster, attr))
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO matter_client_values(node_id, endpoint, cluster, attribute, value, sealed) VALUES(?, ?, ?, ?, ?, 1)
			ON CONFLICT(node_id, endpoint, cluster, attribute) DO UPDATE SET value = excluded.value, sealed = 1`, nodeID, ep, cluster, attr, sealedValue)
		return err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO matter_client_values(node_id, endpoint, cluster, attribute, value, sealed) VALUES(?, ?, ?, ?, ?, 0)
		ON CONFLICT(node_id, endpoint, cluster, attribute) DO UPDATE SET value = excluded.value WHERE matter_client_values.sealed = 0`, nodeID, ep, cluster, attr, v)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return store.ErrNoSealer
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

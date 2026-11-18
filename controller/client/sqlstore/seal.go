// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package sqlstore

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/SukramJ/go-fabric/internal/sealseam"
	"github.com/SukramJ/go-fabric/store"
)

func init() {
	sealseam.RegistryDropEmptySealMark = dropEmptySealMark
	sealseam.RegistryResealTx = func(ctx context.Context, tx *sql.Tx, old, next sealseam.Sealer) error {
		return resealTx(ctx, tx, old, next)
	}
	sealseam.RegistryDropScrubOwed = dropScrubOwed
}

// dropEmptySealMark deletes the seal canary when the registry holds no
// row of matter_client_values or matter_client_versions
// ([sealseam.RegistryDropEmptySealMark]).
func dropEmptySealMark(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `DELETE FROM matter_client_metadata WHERE key = ?
		AND NOT EXISTS (SELECT 1 FROM matter_client_values) AND NOT EXISTS (SELECT 1 FROM matter_client_versions)`, metadataKeySealCanary); err != nil {
		return fmt.Errorf("sqlstore: drop the seal canary: %w", err)
	}
	return nil
}

// secretValue names one secret value of the registry: a cluster's
// attribute, sealed on every node and endpoint.
type secretValue struct{ cluster, attribute uint32 }

// secretValues are the values the store seals ([isSecret]); the seal
// canary is not one of them. TestSecretsAddedAfterV3ShipAnUpgradeStep
// holds the list to its v3 baseline: in a registry the canary marks a
// clear secret value is refused, so a value added here later needs the
// upgrade step that seals it there first.
var secretValues = []secretValue{
	{onboardingCluster, onboardingCodeValue},
	{icdCluster, icdKeyValue},
	{icdCluster, icdPendingKeyValue},
}

// secretClusters is the SQL list of the clusters secretValues names, for
// an IN clause: the package's constants.
func secretClusters() string {
	var ids []string
	for _, v := range secretValues {
		id := strconv.FormatUint(uint64(v.cluster), 10)
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	return strings.Join(ids, ", ")
}

// sealAADPrefix opens every aad of the registry, naming the scheme and its
// version.
const sealAADPrefix = "go-fabric/controller/client/sqlstore/seal/v1"

// appendAADPart appends one length-prefixed part of an aad, as store's
// sealAAD builds its own.
func appendAADPart(b, p []byte) []byte {
	b = binary.AppendUvarint(b, uint64(len(p)))
	return append(b, p...)
}

// metadataKeySealCanary is the reserved matter_client_metadata row that
// marks a sealed registry, its seal canary (decision 194, the shape of
// store's decision 130): one known value sealed under the sealer in the
// row's data column (migration v3), with the aad of that row. The first
// [NewSealed] writes it, NewSealed opens it first, [Reseal] moves it last.
// The row's value is 1.
const metadataKeySealCanary = "sqlstore.seal_canary"

// sealCanaryPlain is the canary's value before sealing.
var sealCanaryPlain = []byte("go-fabric/controller/client/sqlstore/seal/canary/v1")

// canaryAAD is the aad of the seal canary: its table, column and row.
func canaryAAD() []byte {
	b := []byte(sealAADPrefix)
	b = appendAADPart(b, []byte("matter_client_metadata"))
	b = appendAADPart(b, []byte("data"))
	return appendAADPart(b, []byte(metadataKeySealCanary))
}

// querier is what the seal's reads run on: the handle or a transaction.
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// readCanary reads the seal canary's sealed value on q: present is false
// on a registry without it.
func readCanary(ctx context.Context, q querier) (data []byte, present bool, err error) {
	err = q.QueryRowContext(ctx, `SELECT data FROM matter_client_metadata WHERE key = ?`, metadataKeySealCanary).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read the seal canary: %w", err)
	}
	return data, true, nil
}

// openCanary opens the canary's sealed value under s: a canary without
// one, one that does not open and one whose value is not the canary's are
// [store.ErrUnseal].
func openCanary(s store.Sealer, data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("%w: the seal canary holds no sealed value", store.ErrUnseal)
	}
	plain, err := s.Open(data, canaryAAD())
	if err != nil {
		return fmt.Errorf("%w: %w", store.ErrUnseal, err)
	}
	if !bytes.Equal(plain, sealCanaryPlain) {
		return fmt.Errorf("%w: the seal canary holds another value", store.ErrUnseal)
	}
	return nil
}

// writeCanary writes the canary sealed under s on tx.
func writeCanary(ctx context.Context, tx *sql.Tx, s store.Sealer) error {
	data, err := s.Seal(sealCanaryPlain, canaryAAD())
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO matter_client_metadata (key, value, data) VALUES (?, 1, ?)`, metadataKeySealCanary, data); err != nil {
		return fmt.Errorf("write the seal canary: %w", err)
	}
	return nil
}

// refuseClearSecretWrite is the check a store without a sealer makes on
// tx before it writes a secret value in clear: a registry that carries the
// canary refuses it with [store.ErrNoSealer].
func refuseClearSecretWrite(ctx context.Context, tx *sql.Tx) error {
	var one int
	err := tx.QueryRowContext(ctx, `SELECT 1 FROM matter_client_metadata WHERE key = ?`, metadataKeySealCanary).Scan(&one)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil
	case err != nil:
		return fmt.Errorf("read the seal canary: %w", err)
	}
	return fmt.Errorf("%w: the node registry is sealed", store.ErrNoSealer)
}

// clearSecretValue names the first secret value q holds without the seal
// mark, or "" when there is none.
func clearSecretValue(ctx context.Context, q querier) (string, error) {
	rows, err := q.QueryContext(ctx, `SELECT node_id, endpoint, cluster, attribute FROM matter_client_values WHERE sealed = 0 AND cluster IN (`+secretClusters()+`)`) //nolint:gosec // the package's constants
	if err != nil {
		return "", fmt.Errorf("inspect matter_client_values: %w", err)
	}
	found := ""
	for rows.Next() {
		var (
			nodeID        string
			ep            uint16
			cluster, attr uint32
		)
		if err := rows.Scan(&nodeID, &ep, &cluster, &attr); err != nil {
			_ = rows.Close()
			return "", fmt.Errorf("inspect matter_client_values: %w", err)
		}
		if isSecret(cluster, attr) {
			found = fmt.Sprintf("node %q endpoint %d cluster 0x%X attribute %d", nodeID, ep, cluster, attr)
			break
		}
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return "", fmt.Errorf("inspect matter_client_values: %w", err)
	}
	return found, nil
}

// checkSchema is the schema check of [NewSealed] and [Reseal]: the seal
// mark of migration v2 and the canary's column of migration v3.
func checkSchema(ctx context.Context, q querier, op string) error {
	for _, c := range []struct{ table, column string }{
		{"matter_client_values", "sealed"},
		{"matter_client_metadata", "data"},
	} {
		has, err := hasColumn(ctx, q, c.table, c.column)
		if err != nil {
			return fmt.Errorf("sqlstore: %s: inspect %s: %w", op, c.table, err)
		}
		if !has {
			return fmt.Errorf("sqlstore: %s: %s has no %s column: the registry needs schema v3 (sqlstore.Apply or sqlstore.Migrations)", op, c.table, c.column)
		}
	}
	return nil
}

// ScrubOwed reports whether the registry owes the scrub of a sealing or a
// [Reseal]: the values are sealed, but the clear (or old-key) bytes they
// replaced may remain in the file's free pages, its -wal or a kept
// -journal until a [store.Scrub] of the file finishes — a full disk or a
// reader holding the -wal stops one. [NewSealed] retries the scrub on
// every open while it is owed and opens the store either way (decision
// 132, as [store.Store.ScrubOwed]); a host that wants to fail closed
// checks ScrubOwed after NewSealed and stops, and one that does not logs
// it.
func (s *Store) ScrubOwed(ctx context.Context) (bool, error) {
	var owed int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM matter_client_metadata WHERE key = ?`, metadataKeySealScrubOwed).Scan(&owed); err != nil {
		return false, fmt.Errorf("sqlstore: scrub owed: %w", err)
	}
	return owed > 0, nil
}

// Reseal moves a sealed registry from the sealer old to the sealer next —
// the rotation of the host's key — in one transaction: the seal canary
// opened under old (the key check), every sealed value opened under old
// and sealed under next, the canary last; then the [store.Scrub] that
// removes the values sealed under old from the file, with the mark of an
// owed scrub until it finished — [store.Reseal]'s shape on the registry's
// tables (decision 195). A wrong old key or a value that does not open
// under it is [store.ErrUnseal], and so is a secret value in clear (the
// host opens the registry with [NewSealed] under old first, which seals
// it); either leaves the registry as it was. A registry the release before
// schema v3 sealed carries no canary: every sealed value opening under old
// is the key check, and Reseal marks it. Every error after the commit is
// [store.ErrScrubIncomplete]: the values are sealed under next, the scrub
// stays marked as owed ([Store.ScrubOwed]) and the next NewSealed runs it
// again.
//
// It is for a host that keeps the registry alone. A controller's database
// holds store's tables beside it under the same sealer: controller.Reseal
// moves both in one transaction, so no state exists in which one opens
// under the new key and the other only under the old.
//
// No Store may be open on db while Reseal runs, and no other process may
// write the file. db must carry schema v3. There is no unseal.
func Reseal(ctx context.Context, db *sql.DB, old, next store.Sealer) error {
	if old == nil || next == nil {
		return errors.New("sqlstore: Reseal: nil Sealer")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlstore: Reseal: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := resealTx(ctx, tx, old, next); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlstore: Reseal: commit: %w", err)
	}
	if err := store.Scrub(ctx, db); err != nil {
		return fmt.Errorf("sqlstore: Reseal: the values are sealed under the new sealer: %w", err)
	}
	if err := dropScrubOwed(ctx, db); err != nil {
		// The mark stays, so ScrubOwed still says owed and the next
		// NewSealed scrubs again: the same state as an unfinished scrub.
		return fmt.Errorf("sqlstore: Reseal: the values are sealed under the new sealer, the owed scrub stays marked: %w: %w", store.ErrScrubIncomplete, err)
	}
	return nil
}

// resealTx is [Reseal]'s transaction on tx, without the commit and the
// scrub: the schema check, the write lock, the canary opened under old, a
// clear secret value refused, every sealed value moved from old to next,
// the canary last, and the owed scrub marked. controller.Reseal runs it
// beside store's in one transaction ([sealseam.RegistryResealTx]).
func resealTx(ctx context.Context, tx *sql.Tx, old, next store.Sealer) error {
	// A write first, so the transaction holds the write lock before it
	// reads, the schema check included (sealUnmarked's rule). A registry
	// before v2 has no matter_client_metadata to write: the schema check
	// names the step then.
	if _, err := tx.ExecContext(ctx, `UPDATE matter_client_metadata SET value = value WHERE key = ?`, metadataKeySealScrubOwed); err != nil {
		if serr := checkSchema(ctx, tx, "Reseal"); serr != nil {
			return serr
		}
		return fmt.Errorf("sqlstore: Reseal: %w", err)
	}
	if err := checkSchema(ctx, tx, "Reseal"); err != nil {
		return err
	}
	data, marked, err := readCanary(ctx, tx)
	if err != nil {
		return fmt.Errorf("sqlstore: Reseal: %w", err)
	}
	if marked {
		if err := openCanary(old, data); err != nil {
			return fmt.Errorf("sqlstore: Reseal: the old sealer does not open the registry's seal canary: %w", err)
		}
	}
	if v, err := clearSecretValue(ctx, tx); err != nil {
		return fmt.Errorf("sqlstore: Reseal: %w", err)
	} else if v != "" {
		return fmt.Errorf("sqlstore: Reseal: %w: the value of %s is not sealed (sqlstore.NewSealed seals it)", store.ErrUnseal, v)
	}
	rows, err := tx.QueryContext(ctx, `SELECT node_id, endpoint, cluster, attribute, value FROM matter_client_values WHERE sealed = 1`)
	if err != nil {
		return fmt.Errorf("sqlstore: Reseal: %w", err)
	}
	type row struct {
		nodeID        string
		ep            uint16
		cluster, attr uint32
		value         []byte
	}
	var held []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.nodeID, &r.ep, &r.cluster, &r.attr, &r.value); err != nil {
			_ = rows.Close()
			return fmt.Errorf("sqlstore: Reseal: %w", err)
		}
		held = append(held, r)
	}
	if err := closeRows(rows); err != nil {
		return fmt.Errorf("sqlstore: Reseal: %w", err)
	}
	for _, r := range held {
		aad := sealAAD(r.nodeID, r.ep, r.cluster, r.attr)
		plain, err := old.Open(r.value, aad)
		if err != nil {
			return fmt.Errorf("sqlstore: Reseal: node %q: %w: %w", r.nodeID, store.ErrUnseal, err)
		}
		v, err := next.Seal(plain, aad)
		if err != nil {
			return fmt.Errorf("sqlstore: Reseal: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE matter_client_values SET value = ? WHERE node_id = ? AND endpoint = ? AND cluster = ? AND attribute = ?`, v, r.nodeID, r.ep, r.cluster, r.attr); err != nil {
			return fmt.Errorf("sqlstore: Reseal: %w", err)
		}
	}
	if err := writeCanary(ctx, tx, next); err != nil {
		return fmt.Errorf("sqlstore: Reseal: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO matter_client_metadata (key, value) VALUES (?, 1)`, metadataKeySealScrubOwed); err != nil {
		return fmt.Errorf("sqlstore: Reseal: %w", err)
	}
	return nil
}

// dropScrubOwed deletes the mark of an owed scrub once a [store.Scrub]
// finished.
func dropScrubOwed(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `DELETE FROM matter_client_metadata WHERE key = ?`, metadataKeySealScrubOwed); err != nil {
		return fmt.Errorf("sqlstore: drop the owed scrub's mark: %w", err)
	}
	return nil
}

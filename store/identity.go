// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// IdentityRecord is the per-fabric node operational credential bundle.
// One identity per fabric: the bridge has exactly one operational node
// per fabric.
type IdentityRecord struct {
	// FabricIndex links this identity to matter_fabrics.fabric_index.
	FabricIndex uint8
	// NOC is the Node Operational Certificate bytes (Matter-cert TLV
	// per §11.18.5.2).
	NOC []byte
	// ICAC is the optional Intermediate CA certificate. nil means
	// the NOC is signed directly by the fabric root.
	ICAC []byte
	// PrivateKey is the 32-byte raw P-256 scalar matching the NOC's
	// public key. A [Store] made by [NewSealed] seals it at rest; one made
	// by [New] persists it as it is.
	PrivateKey []byte
	// IPK is the 16-byte Identity Protection Key for this fabric
	// (used in CASE Sigma).
	IPK []byte
}

// identityTable is the table the identity's secrets are sealed in.
const identityTable = "matter_node_identities"

// UpsertIdentity inserts or replaces the identity row for
// rec.FabricIndex. The fabric must already exist (FK constraint). A
// pending UpdateNOC's prior credentials stay. Without a sealer it refuses
// a row a sealer sealed, whose prior key it would leave sealed under a
// clear mark.
func (s *Store) UpsertIdentity(ctx context.Context, rec IdentityRecord) error {
	if s.sealer != nil {
		return s.inTx(ctx, "upsert identity", func(tx *sql.Tx) error { return s.upsertSealedIdentity(ctx, tx, rec) })
	}
	res, err := s.db.ExecContext(
		ctx, `
INSERT INTO matter_node_identities
    (fabric_index, noc, icac, private_key, ipk)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(fabric_index) DO UPDATE SET
    noc         = excluded.noc,
    icac        = excluded.icac,
    private_key = excluded.private_key,
    ipk         = excluded.ipk
WHERE matter_node_identities.sealed = 0`,
		rec.FabricIndex, rec.NOC, nullableBytes(rec.ICAC), rec.PrivateKey, rec.IPK,
	)
	if err != nil {
		return fmt.Errorf("store: upsert identity: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("store: upsert identity: %w", err)
	} else if n == 0 {
		return fmt.Errorf("store: upsert identity: %w", errSealedNoSealer)
	}
	return nil
}

// upsertSealedIdentity is UpsertIdentity under a sealer, in tx: the row's
// secrets sealed, a clear prior key a store without a sealer left sealed
// with them. Its first statement writes, so the transaction holds the
// write lock before it reads.
func (s *Store) upsertSealedIdentity(ctx context.Context, tx *sql.Tx, rec IdentityRecord) error {
	if _, err := tx.ExecContext(ctx, `UPDATE matter_node_identities SET sealed = sealed WHERE fabric_index = ?`, rec.FabricIndex); err != nil {
		return err
	}
	var (
		prior  []byte
		sealed bool
	)
	err := tx.QueryRowContext(ctx, `SELECT prior_private_key, sealed FROM matter_node_identities WHERE fabric_index = ?`, rec.FabricIndex).Scan(&prior, &sealed)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if !sealed {
		if prior, err = s.sealColumn(prior, identityTable, "prior_private_key", rec.FabricIndex); err != nil {
			return err
		}
	}
	key, err := s.sealColumn(rec.PrivateKey, identityTable, "private_key", rec.FabricIndex)
	if err != nil {
		return err
	}
	ipk, err := s.sealColumn(rec.IPK, identityTable, "ipk", rec.FabricIndex)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO matter_node_identities
    (fabric_index, noc, icac, private_key, ipk, prior_private_key, sealed)
VALUES (?, ?, ?, ?, ?, ?, 1)
ON CONFLICT(fabric_index) DO UPDATE SET
    noc               = excluded.noc,
    icac              = excluded.icac,
    private_key       = excluded.private_key,
    ipk               = excluded.ipk,
    prior_private_key = excluded.prior_private_key,
    sealed            = 1`,
		rec.FabricIndex, rec.NOC, nullableBytes(rec.ICAC), key, ipk, nullableBytes(prior))
	return err
}

// GetIdentity returns the identity for fabricIndex. Returns
// [ErrIdentityNotFound] when no row exists.
func (s *Store) GetIdentity(ctx context.Context, fabricIndex uint8) (IdentityRecord, error) {
	var (
		rec    IdentityRecord
		icac   sql.NullString
		sealed bool
	)
	rec.FabricIndex = fabricIndex
	err := s.db.QueryRowContext(ctx, `
SELECT noc, icac, private_key, ipk, sealed FROM matter_node_identities
WHERE fabric_index = ?`, fabricIndex).Scan(&rec.NOC, &icac, &rec.PrivateKey, &rec.IPK, &sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return IdentityRecord{}, ErrIdentityNotFound
	}
	if err != nil {
		return IdentityRecord{}, fmt.Errorf("store: get identity: %w", err)
	}
	if icac.Valid {
		rec.ICAC = []byte(icac.String)
	}
	if rec.PrivateKey, err = s.openColumn(rec.PrivateKey, sealed, identityTable, "private_key", fabricIndex); err != nil {
		return IdentityRecord{}, fmt.Errorf("store: get identity: %w", err)
	}
	if rec.IPK, err = s.openColumn(rec.IPK, sealed, identityTable, "ipk", fabricIndex); err != nil {
		return IdentityRecord{}, fmt.Errorf("store: get identity: %w", err)
	}
	return rec, nil
}

// nullableBytes turns a nil byte slice into a SQL NULL. modernc.org/sqlite
// stores []byte{} and NULL as distinct values, and we want NULL for
// "no ICAC".
func nullableBytes(b []byte) any {
	if b == nil {
		return nil
	}
	return b
}

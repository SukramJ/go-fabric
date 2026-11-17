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

// ipkSize is the IPK's size, the length cluster/core's AddNOC accepts
// (operational_credentials.go, IPKValue); UpsertIdentity and GetIdentity
// refuse another.
const ipkSize = 16

// checkIdentity is the write side of the identity's lengths: a non-empty
// private key and an ipkSize IPK, what GetIdentity accepts
// (store/memstore copies the rule).
func checkIdentity(rec IdentityRecord) error {
	if len(rec.PrivateKey) == 0 {
		return errors.New("store: empty private_key")
	}
	if len(rec.IPK) != ipkSize {
		return fmt.Errorf("store: ipk length=%d (want %d)", len(rec.IPK), ipkSize)
	}
	return nil
}

// identityTable is the table the identity's secrets are sealed in.
const identityTable = "matter_node_identities"

// UpsertIdentity inserts or replaces the identity row for
// rec.FabricIndex. The fabric must already exist (FK constraint). The
// private key must not be empty and the IPK must be 16 bytes, what
// GetIdentity accepts. A pending UpdateNOC's prior credentials stay. Without a sealer it refuses
// a row a sealer sealed, whose prior key it would leave sealed under a
// clear mark.
func (s *Store) UpsertIdentity(ctx context.Context, rec IdentityRecord) error {
	if err := checkIdentity(rec); err != nil {
		return fmt.Errorf("store: upsert identity: %w", err)
	}
	if s.sealer != nil {
		return s.inTx(ctx, "upsert identity", func(tx *sql.Tx) error { return s.upsertSealedIdentity(ctx, tx, rec) })
	}
	if err := s.refuseClearSecretWrite(ctx, s.db); err != nil {
		return fmt.Errorf("store: upsert identity: %w", err)
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
		return fmt.Errorf("store: upsert identity: %w", ErrNoSealer)
	}
	return nil
}

// upsertSealedIdentity is UpsertIdentity under a sealer, in tx: the row's
// secrets sealed, a sealed prior key kept; a row without the seal mark is
// refused ([ErrUnseal]). Its first statement writes, so the transaction
// holds the write lock before it reads.
func (s *Store) upsertSealedIdentity(ctx context.Context, tx *sql.Tx, rec IdentityRecord) error {
	if _, err := tx.ExecContext(ctx, `UPDATE matter_node_identities SET sealed = sealed WHERE fabric_index = ?`, rec.FabricIndex); err != nil {
		return err
	}
	var (
		prior  []byte
		sealed bool
	)
	err := tx.QueryRowContext(ctx, `SELECT prior_private_key, sealed FROM matter_node_identities WHERE fabric_index = ?`, rec.FabricIndex).Scan(&prior, &sealed)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		// a new identity
	case err != nil:
		return err
	case !sealed:
		return clearRowUnderSealer(identityTable)
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
	if len(rec.PrivateKey) == 0 {
		return IdentityRecord{}, errors.New("store: get identity: empty private_key")
	}
	if len(rec.IPK) != ipkSize {
		return IdentityRecord{}, fmt.Errorf("store: get identity: ipk length=%d (want %d)", len(rec.IPK), ipkSize)
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

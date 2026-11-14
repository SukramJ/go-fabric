// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package store

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
)

// FabricRecord mirrors the Fabric Descriptor from Matter Core Spec
// §11.18.5. fabric_index is stack-assigned (1..254); the remaining
// fields are commissioner-supplied or derived.
type FabricRecord struct {
	// FabricIndex is the stack-assigned identifier in [1, 254]. The
	// special value 0 means "not yet assigned" — callers passing a
	// zero index to [Store.AddFabric] receive a freshly allocated one.
	FabricIndex uint8
	// FabricID is the 64-bit fabric identifier set by the commissioner.
	FabricID uint64
	// NodeID is the 64-bit operational node identifier of this bridge
	// inside the fabric.
	NodeID uint64
	// RootPublicKey is the trust anchor — the issuing root CA public
	// key in uncompressed P-256 form (65 bytes, 0x04 prefix). Cached
	// alongside RootCert so the CASE sigma key lookup +
	// CompressedFabricID HKDF (Matter §4.13.2.4) can skip the cert
	// decode on every Sigma1.
	RootPublicKey []byte
	// RootCert is the full Matter Certificate TLV envelope received via
	// AddTrustedRootCertificate (Matter §11.18.7.12). Mirrors matter.js
	// `Fabric.rootCert` (Fabric.ts:68); served verbatim as each entry
	// of OperationalCredentials.TrustedRootCertificates
	// (matter.js OperationalCredentialsServer.ts:470-472). Apple Home
	// validates every entry as a Matter Certificate TLV and silently
	// drops the entire ReportData stream on schema mismatch.
	//
	// May be nil on legacy fabric rows persisted before the 012
	// migration; those fabrics are omitted from TrustedRootCertificates
	// rather than re-served as the old EC-pubkey-as-cert (which
	// triggers Bug I again).
	RootCert []byte
	// VendorID is the 16-bit IANA-assigned vendor identifier embedded
	// in the NOC.
	VendorID uint16
	// Label is the user-visible fabric name (max 32 utf-8 chars per
	// Matter spec; not enforced here — that's a cluster-server concern).
	Label string
	// CompressedID is the 8-byte compressed fabric identifier
	// (HKDF over RootPublicKey + FabricID per §4.13.2.4).
	CompressedID [8]byte
	// Pending is the row's commit state. [Fabrics.AddFabric] takes
	// [FabricCommitted] or [FabricPendingAdd]; afterwards only
	// UpdateFabricCredentials, CommitFabric and RollbackFabric move it. A
	// pending row is read like any other: a reader that does not care sees
	// no difference.
	Pending FabricPending
	// VIDVerificationStatement and VVSC are the fabric's vendor ID
	// verification data (SetVIDVerificationStatement, Matter §11.18.6.17);
	// nil is none, and an empty slice is stored as none and read back nil.
	VIDVerificationStatement []byte
	VVSC                     []byte
}

// FabricCredentials is what an UpdateNOC changes on a fabric, as it shall
// stand after the update ([Fabrics.UpdateFabricCredentials]): the
// operational identity's NOC, ICAC and private key and the node id the new
// NOC carries. The IPK, the root, the fabric id, the vendor id and the VID
// verification data are not part of it: an UpdateNOC keeps them.
type FabricCredentials struct {
	// NOC is the new Node Operational Certificate; it must not be empty.
	NOC []byte
	// ICAC is the new intermediate certificate; nil means none.
	ICAC []byte
	// PrivateKey is the operational key matching the NOC, stored as
	// [IdentityRecord.PrivateKey] is; it must not be empty.
	PrivateKey []byte
	// NodeID is the node id of the new NOC.
	NodeID uint64
	// VendorID, VIDVerificationStatement and VVSC are ignored.
	//
	// Deprecated: [Fabrics.UpdateFabricCredentials] does not write the
	// vendor id or the VID verification data, which an UpdateNOC does not
	// change: writing back the values the caller had read lost a
	// SetVIDVerificationStatement that landed in between (review II,
	// R2-P2-dr-noc-02). [Fabrics.UpdateFabricVendorID] and
	// [Fabrics.UpdateFabricVIDVerification] write them.
	VendorID                 uint16
	VIDVerificationStatement []byte
	VVSC                     []byte
}

// FabricPending is a fabric row's commit state: whether a fail-safe's
// AddNOC or UpdateNOC wrote it and CommissioningComplete has not yet
// committed it. The row carries the state, so it leaves with the row and
// survives a restart with it. The values are the matter_fabrics.pending
// column's.
type FabricPending uint8

const (
	// FabricCommitted is a committed row, the zero value.
	FabricCommitted FabricPending = iota
	// FabricPendingAdd is a row an uncommitted AddNOC installed; its
	// rollback deletes it.
	FabricPendingAdd
	// FabricPendingUpdate is a row whose credentials an uncommitted
	// UpdateNOC replaced; the store keeps the values it replaced, and its
	// rollback moves them back.
	FabricPendingUpdate
)

// String names the state.
func (p FabricPending) String() string {
	switch p {
	case FabricCommitted:
		return "committed"
	case FabricPendingAdd:
		return "pending add"
	case FabricPendingUpdate:
		return "pending update"
	}
	return "FabricPending(" + strconv.Itoa(int(p)) + ")"
}

// AddFabric inserts a new fabric. If rec.FabricIndex is 0 the next
// free 1..254 slot is allocated; otherwise the caller's value is used
// (and the insert fails on collision).
//
// Returns the assigned fabric_index. Returns [ErrFabricExhausted] when
// no free slot exists.
//
// Index allocation follows the matter.js FabricManager #nextFabricIndex
// monotonic-counter strategy (FabricManager.ts:163-164,186-188): a persisted
// counter advances after each successful AddFabric so
// a freshly-removed index is not immediately reused — Apple Home caches
// fabric metadata by fabricIndex and may conflate old and new entries on
// rapid re-use. Falls back to the lowest-free-slot scan when the
// matter_metadata table is absent (pre-013 schema).
func (s *Store) AddFabric(ctx context.Context, rec FabricRecord) (uint8, error) {
	if err := insertablePending(rec.Pending); err != nil {
		return 0, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("store: add fabric: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	idx := rec.FabricIndex
	if idx == 0 {
		// Prefer the persisted counter; fall back to the lowest-free scan
		// when it returns the same index as an occupied slot (first call
		// after migration-013, or if the counter points at an occupied
		// slot due to manual inserts).
		candidate, cErr := nextFabricIndexFromCounter(ctx, tx)
		if cErr != nil {
			// Counter read failed — fall back to scan.
			candidate, cErr = nextFreeFabricIndex(ctx, tx)
			if cErr != nil {
				return 0, cErr
			}
		}
		idx = candidate
	}

	if _, err := tx.ExecContext(
		ctx, `
INSERT INTO matter_fabrics
    (fabric_index, fabric_id, node_id, root_public_key, root_cert, vendor_id, label, compressed_id,
     pending, vid_statement, vvsc)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		idx,
		uint64ToBE(rec.FabricID),
		uint64ToBE(rec.NodeID),
		rec.RootPublicKey,
		rec.RootCert,
		rec.VendorID,
		rec.Label,
		rec.CompressedID[:],
		rec.Pending,
		noneIfEmpty(rec.VIDVerificationStatement),
		noneIfEmpty(rec.VVSC),
	); err != nil {
		return 0, fmt.Errorf("store: add fabric: insert: %w", err)
	}

	// Advance the persistent counter past the just-allocated index.
	// Best-effort: a failure here does not roll back the fabric insert
	// because the scan-based fallback still produces correct results.
	if rec.FabricIndex == 0 {
		// Collect currently-occupied indices (including the just-inserted
		// one) so bumpNextFabricIndex can skip them all.
		rows, _ := tx.QueryContext(ctx, `SELECT fabric_index FROM matter_fabrics ORDER BY fabric_index ASC`)
		var occupied []uint8
		if rows != nil {
			for rows.Next() {
				var fi int
				if scanErr := rows.Scan(&fi); scanErr == nil && fi >= 1 && fi <= 254 {
					occupied = append(occupied, uint8(fi)) //nolint:gosec // bounded by check; see #20
				}
			}
			_ = rows.Close()
		}
		_ = bumpNextFabricIndex(ctx, tx, occupied)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("store: add fabric: commit: %w", err)
	}
	return idx, nil
}

// GetFabric returns the fabric record for fabricIndex. Returns
// [ErrFabricNotFound] when no row exists.
func (s *Store) GetFabric(ctx context.Context, fabricIndex uint8) (FabricRecord, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT `+fabricColumns+`
FROM matter_fabrics WHERE fabric_index = ?`, fabricIndex)
	rec, err := scanFabric(row)
	if errors.Is(err, sql.ErrNoRows) {
		return FabricRecord{}, ErrFabricNotFound
	}
	if err != nil {
		return FabricRecord{}, fmt.Errorf("store: get fabric: %w", err)
	}
	return rec, nil
}

// ListFabrics returns every fabric ordered by fabric_index ascending.
func (s *Store) ListFabrics(ctx context.Context) ([]FabricRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT `+fabricColumns+`
FROM matter_fabrics ORDER BY fabric_index ASC`)
	if err != nil {
		return nil, fmt.Errorf("store: list fabrics: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []FabricRecord
	for rows.Next() {
		rec, err := scanFabric(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list fabrics: scan: %w", err)
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list fabrics: rows: %w", err)
	}
	return out, nil
}

// UpdateFabricLabel rewrites the label for fabricIndex. Returns
// [ErrFabricNotFound] when no row exists.
func (s *Store) UpdateFabricLabel(ctx context.Context, fabricIndex uint8, label string) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE matter_fabrics SET label = ?, updated_at = CURRENT_TIMESTAMP
WHERE fabric_index = ?`, label, fabricIndex)
	if err != nil {
		return fmt.Errorf("store: update fabric label: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: update fabric label: rows affected: %w", err)
	}
	if n == 0 {
		return ErrFabricNotFound
	}
	return nil
}

// UpdateFabricVendorID rewrites the vendor id of fabricIndex — the
// VendorID a SetVIDVerificationStatement supplies, which matter.js
// Fabric.updateVendorVerificationData stores as the fabric's rootVendorId.
// Returns [ErrFabricNotFound] when no row exists.
func (s *Store) UpdateFabricVendorID(ctx context.Context, fabricIndex uint8, vendorID uint16) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE matter_fabrics SET vendor_id = ?, updated_at = CURRENT_TIMESTAMP
WHERE fabric_index = ?`, vendorID, fabricIndex)
	if err != nil {
		return fmt.Errorf("store: update fabric vendor id: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: update fabric vendor id: rows affected: %w", err)
	}
	if n == 0 {
		return ErrFabricNotFound
	}
	return nil
}

// UpdateFabricNodeID rewrites the operational NodeID for fabricIndex.
// UpdateNOC installs a NOC that may carry a new NodeID for the same
// fabric; the stored row must follow so destinationID resolution and
// the operational mDNS instance name (<compressedID>-<nodeID>) match
// the new certificate. Mirrors matter.js Fabric.ts:548 lifting nodeId
// from the new NOC into the rebuilt fabric. Returns
// [ErrFabricNotFound] when no row exists.
func (s *Store) UpdateFabricNodeID(ctx context.Context, fabricIndex uint8, nodeID uint64) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE matter_fabrics SET node_id = ?, updated_at = CURRENT_TIMESTAMP
WHERE fabric_index = ?`, uint64ToBE(nodeID), fabricIndex)
	if err != nil {
		return fmt.Errorf("store: update fabric node id: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: update fabric node id: rows affected: %w", err)
	}
	if n == 0 {
		return ErrFabricNotFound
	}
	return nil
}

// UpdateFabricVIDVerification implements [Fabrics]: one UPDATE of the
// row's live vid_statement and vvsc; the prior_* columns of a pending
// update are not touched.
func (s *Store) UpdateFabricVIDVerification(ctx context.Context, fabricIndex uint8, statement, vvsc []byte) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE matter_fabrics SET vid_statement = ?, vvsc = ?, updated_at = CURRENT_TIMESTAMP
WHERE fabric_index = ?`, noneIfEmpty(statement), noneIfEmpty(vvsc), fabricIndex)
	if err != nil {
		return fmt.Errorf("store: update fabric VID verification: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: update fabric VID verification: rows affected: %w", err)
	}
	if n == 0 {
		return ErrFabricNotFound
	}
	return nil
}

// UpdateFabricCredentials implements [Fabrics] in one transaction. Its
// first statement is the write that marks a committed row and keeps its
// prior values, so the transaction takes SQLite's write lock before it
// reads anything and a racing call waits for it (the busy timeout) and then
// finds the row marked. With a read first, the racing call failed with
// SQLITE_BUSY_SNAPSHOT instead (storetest's fabrics/concurrent-updates).
// CommitFabric and RollbackFabric open with their write for the same
// reason.
func (s *Store) UpdateFabricCredentials(ctx context.Context, fabricIndex uint8, c FabricCredentials) error {
	if len(c.NOC) == 0 || len(c.PrivateKey) == 0 {
		return errors.New("store: update fabric credentials: a NOC and a private key are required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: update fabric credentials: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `
UPDATE matter_fabrics
SET pending = ?, prior_node_id = node_id, prior_vendor_id = vendor_id,
    prior_vid_statement = vid_statement, prior_vvsc = vvsc
WHERE fabric_index = ? AND pending = ?`, FabricPendingUpdate, fabricIndex, FabricCommitted)
	if err != nil {
		return fmt.Errorf("store: update fabric credentials: mark: %w", err)
	}
	marked, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: update fabric credentials: mark: %w", err)
	}
	if marked == 0 {
		var one int
		err := tx.QueryRowContext(ctx, `SELECT 1 FROM matter_fabrics WHERE fabric_index = ?`, fabricIndex).Scan(&one)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrFabricNotFound
		}
		if err != nil {
			return fmt.Errorf("store: update fabric credentials: %w", err)
		}
	} else if _, err := tx.ExecContext(ctx, `
UPDATE matter_node_identities
SET prior_noc = noc, prior_icac = icac, prior_private_key = private_key
WHERE fabric_index = ?`, fabricIndex); err != nil {
		return fmt.Errorf("store: update fabric credentials: keep the prior identity: %w", err)
	}
	res, err = tx.ExecContext(ctx, `
UPDATE matter_node_identities SET noc = ?, icac = ?, private_key = ?
WHERE fabric_index = ?`, c.NOC, nullableBytes(c.ICAC), c.PrivateKey, fabricIndex)
	if err != nil {
		return fmt.Errorf("store: update fabric credentials: identity: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("store: update fabric credentials: identity: %w", err)
	} else if n == 0 {
		return ErrIdentityNotFound
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE matter_fabrics
SET node_id = ?, updated_at = CURRENT_TIMESTAMP
WHERE fabric_index = ?`, uint64ToBE(c.NodeID), fabricIndex); err != nil {
		return fmt.Errorf("store: update fabric credentials: fabric: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: update fabric credentials: commit: %w", err)
	}
	return nil
}

// CommitFabric implements [Fabrics] in one transaction, the write first.
func (s *Store) CommitFabric(ctx context.Context, fabricIndex uint8) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: commit fabric: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `
UPDATE matter_fabrics
SET pending = ?, prior_node_id = NULL, prior_vendor_id = NULL,
    prior_vid_statement = NULL, prior_vvsc = NULL, updated_at = CURRENT_TIMESTAMP
WHERE fabric_index = ? AND pending <> ?`, FabricCommitted, fabricIndex, FabricCommitted)
	if err != nil {
		return fmt.Errorf("store: commit fabric: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: commit fabric: %w", err)
	}
	if n == 0 {
		var one int
		err := tx.QueryRowContext(ctx, `SELECT 1 FROM matter_fabrics WHERE fabric_index = ?`, fabricIndex).Scan(&one)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrFabricNotFound
		}
		if err != nil {
			return fmt.Errorf("store: commit fabric: %w", err)
		}
		return nil // committed already
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE matter_node_identities SET prior_noc = NULL, prior_icac = NULL, prior_private_key = NULL
WHERE fabric_index = ?`, fabricIndex); err != nil {
		return fmt.Errorf("store: commit fabric: identity: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit fabric: commit: %w", err)
	}
	return nil
}

// RollbackFabric implements [Fabrics] in one transaction, the write first:
// a pending add's row is deleted (the foreign keys' ON DELETE CASCADE takes
// its rows, as in RemoveFabric); a pending update's identity is restored
// while the row still says so, then the row. A prior node id or vendor id
// that is not there (a row no UpdateFabricCredentials marked) keeps the
// live one rather than failing the NOT NULL column; an identity without a
// prior NOC keeps its credentials.
func (s *Store) RollbackFabric(ctx context.Context, fabricIndex uint8) (FabricPending, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return FabricCommitted, fmt.Errorf("store: rollback fabric: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `DELETE FROM matter_fabrics WHERE fabric_index = ? AND pending = ?`, fabricIndex, FabricPendingAdd)
	if err != nil {
		return FabricCommitted, fmt.Errorf("store: rollback fabric: delete: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return FabricCommitted, fmt.Errorf("store: rollback fabric: delete: %w", err)
	} else if n > 0 {
		if err := tx.Commit(); err != nil {
			return FabricCommitted, fmt.Errorf("store: rollback fabric: commit: %w", err)
		}
		return FabricPendingAdd, nil
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE matter_node_identities
SET noc = prior_noc, icac = prior_icac, private_key = prior_private_key,
    prior_noc = NULL, prior_icac = NULL, prior_private_key = NULL
WHERE fabric_index = ? AND prior_noc IS NOT NULL
  AND EXISTS (SELECT 1 FROM matter_fabrics WHERE fabric_index = ? AND pending = ?)`, fabricIndex, fabricIndex, FabricPendingUpdate); err != nil {
		return FabricCommitted, fmt.Errorf("store: rollback fabric: identity: %w", err)
	}
	res, err = tx.ExecContext(ctx, `
UPDATE matter_fabrics
SET node_id = COALESCE(prior_node_id, node_id), vendor_id = COALESCE(prior_vendor_id, vendor_id),
    vid_statement = prior_vid_statement, vvsc = prior_vvsc,
    prior_node_id = NULL, prior_vendor_id = NULL, prior_vid_statement = NULL, prior_vvsc = NULL,
    pending = ?, updated_at = CURRENT_TIMESTAMP
WHERE fabric_index = ? AND pending = ?`, FabricCommitted, fabricIndex, FabricPendingUpdate)
	if err != nil {
		return FabricCommitted, fmt.Errorf("store: rollback fabric: restore: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return FabricCommitted, fmt.Errorf("store: rollback fabric: restore: %w", err)
	} else if n > 0 {
		if err := tx.Commit(); err != nil {
			return FabricCommitted, fmt.Errorf("store: rollback fabric: commit: %w", err)
		}
		return FabricPendingUpdate, nil
	}
	var one int
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM matter_fabrics WHERE fabric_index = ?`, fabricIndex).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return FabricCommitted, ErrFabricNotFound
	}
	if err != nil {
		return FabricCommitted, fmt.Errorf("store: rollback fabric: %w", err)
	}
	return FabricCommitted, nil
}

// RemoveFabric deletes the fabric and (via FK CASCADE) its
// node_identity, group_keys, group_key_map and acl_entries. Returns
// [ErrFabricNotFound] when no row exists.
func (s *Store) RemoveFabric(ctx context.Context, fabricIndex uint8) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM matter_fabrics WHERE fabric_index = ?`, fabricIndex)
	if err != nil {
		return fmt.Errorf("store: remove fabric: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: remove fabric: rows affected: %w", err)
	}
	if n == 0 {
		return ErrFabricNotFound
	}
	return nil
}

// nextFabricIndexFromCounter reads the persisted next_fabric_index counter
// from matter_metadata and verifies the candidate is actually free. If the
// counter points at an occupied slot it advances until it finds a free one.
// Returns [ErrFabricExhausted] when all 254 slots are taken.
// Mirrors matter.js FabricManager.ts:163-164 — allocate from #nextFabricIndex
// then bump.
func nextFabricIndexFromCounter(ctx context.Context, tx *sql.Tx) (uint8, error) {
	candidate, err := getNextFabricIndexFromMetadata(ctx, tx)
	if err != nil {
		return 0, err
	}

	// Collect occupied indices so we can skip them.
	rows, err := tx.QueryContext(ctx, `SELECT fabric_index FROM matter_fabrics ORDER BY fabric_index ASC`)
	if err != nil {
		return 0, fmt.Errorf("store: next fabric index (counter): list: %w", err)
	}
	defer func() { _ = rows.Close() }()

	occupied := make(map[uint8]struct{}, 16)
	for rows.Next() {
		var got int
		if scanErr := rows.Scan(&got); scanErr != nil {
			return 0, fmt.Errorf("store: next fabric index (counter): scan: %w", scanErr)
		}
		if got >= 1 && got <= 254 {
			occupied[uint8(got)] = struct{}{} //nolint:gosec // bounded by check; see #20
		}
	}
	if rErr := rows.Err(); rErr != nil {
		return 0, fmt.Errorf("store: next fabric index (counter): rows: %w", rErr)
	}

	if len(occupied) >= 254 {
		return 0, ErrFabricExhausted
	}

	// Walk forward from candidate until we find a free slot, wrapping at 255→1.
	for range 254 {
		if _, taken := occupied[candidate]; !taken {
			return candidate, nil
		}
		if candidate == 254 {
			candidate = 1
		} else {
			candidate++
		}
	}
	return 0, ErrFabricExhausted
}

// nextFreeFabricIndex scans 1..254 for the lowest unused index in tx.
// Returns [ErrFabricExhausted] when all slots are taken.
func nextFreeFabricIndex(ctx context.Context, tx *sql.Tx) (uint8, error) {
	rows, err := tx.QueryContext(ctx, `SELECT fabric_index FROM matter_fabrics ORDER BY fabric_index ASC`)
	if err != nil {
		return 0, fmt.Errorf("store: next fabric index: %w", err)
	}
	defer func() { _ = rows.Close() }()

	want := uint8(1)
	for rows.Next() {
		var got int
		if err := rows.Scan(&got); err != nil {
			return 0, fmt.Errorf("store: next fabric index: scan: %w", err)
		}
		// CHECK constraint on fabric_index narrows got to [1, 254];
		// the cast is therefore safe.
		if got < 1 || got > 254 {
			return 0, fmt.Errorf("store: next fabric index: stored value %d out of range", got)
		}
		if uint8(got) != want { //nolint:gosec // bounded by the range check above; see #20
			return want, nil
		}
		if want == 254 {
			// occupied slot 254 — every smaller slot was contiguous
			// up to here. No room left.
			return 0, ErrFabricExhausted
		}
		want++
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("store: next fabric index: rows: %w", err)
	}
	if want > 254 {
		return 0, ErrFabricExhausted
	}
	return want, nil
}

// scanRow is satisfied by both *sql.Row and *sql.Rows.
type scanRow interface {
	Scan(dest ...any) error
}

// fabricColumns are the columns [scanFabric] reads, in its order.
const fabricColumns = `fabric_index, fabric_id, node_id, root_public_key, root_cert, vendor_id, label, compressed_id,
       pending, vid_statement, vvsc`

func scanFabric(r scanRow) (FabricRecord, error) {
	var (
		rec      FabricRecord
		fabricID []byte
		nodeID   []byte
		compID   []byte
	)
	if err := r.Scan(&rec.FabricIndex, &fabricID, &nodeID, &rec.RootPublicKey, &rec.RootCert, &rec.VendorID, &rec.Label, &compID,
		&rec.Pending, &rec.VIDVerificationStatement, &rec.VVSC); err != nil {
		return FabricRecord{}, err
	}
	if rec.Pending > FabricPendingUpdate {
		return FabricRecord{}, fmt.Errorf("store: pending=%d is no FabricPending", rec.Pending)
	}
	if len(fabricID) != 8 {
		return FabricRecord{}, fmt.Errorf("store: fabric_id length=%d, want 8", len(fabricID))
	}
	if len(nodeID) != 8 {
		return FabricRecord{}, fmt.Errorf("store: node_id length=%d, want 8", len(nodeID))
	}
	if len(compID) != 8 {
		return FabricRecord{}, fmt.Errorf("store: compressed_id length=%d, want 8", len(compID))
	}
	rec.FabricID = binary.BigEndian.Uint64(fabricID)
	rec.NodeID = binary.BigEndian.Uint64(nodeID)
	copy(rec.CompressedID[:], compID)
	return rec, nil
}

// insertablePending refuses a state AddFabric cannot insert: only a
// committed row or an AddNOC's pending one is new; an UpdateNOC's state
// needs a row to update.
func insertablePending(p FabricPending) error {
	switch p {
	case FabricCommitted, FabricPendingAdd:
		return nil
	case FabricPendingUpdate:
		return errors.New("store: add fabric: a row cannot be inserted as a pending update")
	}
	return fmt.Errorf("store: add fabric: %v is no fabric state", p)
}

// noneIfEmpty turns an empty or nil byte slice into SQL NULL: the VID
// columns hold NULL for none, so an empty slice reads back nil.
func noneIfEmpty(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

func uint64ToBE(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}

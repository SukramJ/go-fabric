// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// SecurityPolicy mirrors Matter §11.2.10 GroupKeySecurityPolicy.
type SecurityPolicy uint8

const (
	// SecurityPolicyTrustFirst (value 0) — trust the most recent
	// epoch first.
	SecurityPolicyTrustFirst SecurityPolicy = 0
	// SecurityPolicyCacheAndSync (value 1) — cache all three epoch
	// keys, sync via timing.
	SecurityPolicyCacheAndSync SecurityPolicy = 1
)

// GroupKeySet mirrors a single (fabric, group_key_set_id) row in
// matter_group_keys. The three (EpochKey, EpochStart) pairs are
// independent: EpochKey1/2 are nil when the slot is empty.
type GroupKeySet struct {
	FabricIndex    uint8
	GroupKeySetID  uint16
	SecurityPolicy SecurityPolicy
	EpochKey0      []byte
	EpochStart0    uint64
	EpochKey1      []byte // nil when slot empty
	EpochStart1    uint64
	EpochKey2      []byte // nil when slot empty
	EpochStart2    uint64
}

// UpsertGroupKeySet inserts or replaces a key-set row. The fabric
// must already exist (FK constraint). EpochKey0 must be 16 bytes;
// EpochKey1 and EpochKey2 are 16 bytes or absent, an empty one stored as
// absent — the lengths a read accepts ([checkGroupKeySet]).
func (s *Store) UpsertGroupKeySet(ctx context.Context, rec GroupKeySet) error {
	rec, err := checkGroupKeySet(rec)
	if err != nil {
		return fmt.Errorf("store: upsert group key set: %w", err)
	}
	if err := s.refuseClearSecretWrite(ctx, s.db); err != nil {
		return fmt.Errorf("store: upsert group key set: %w", err)
	}
	var keys [3][]byte
	for i, k := range [3][]byte{rec.EpochKey0, rec.EpochKey1, rec.EpochKey2} {
		var err error
		if keys[i], err = s.sealColumn(k, "matter_group_keys", groupEpochKeyColumns[i], rec.FabricIndex, rec.GroupKeySetID); err != nil {
			return fmt.Errorf("store: upsert group key set: %w", err)
		}
	}
	if _, err := s.db.ExecContext(
		ctx, `
INSERT INTO matter_group_keys
    (fabric_index, group_key_set_id, security_policy,
     epoch_key_0, epoch_start_0,
     epoch_key_1, epoch_start_1,
     epoch_key_2, epoch_start_2, sealed)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(fabric_index, group_key_set_id) DO UPDATE SET
    security_policy = excluded.security_policy,
    epoch_key_0     = excluded.epoch_key_0,
    epoch_start_0   = excluded.epoch_start_0,
    epoch_key_1     = excluded.epoch_key_1,
    epoch_start_1   = excluded.epoch_start_1,
    epoch_key_2     = excluded.epoch_key_2,
    epoch_start_2   = excluded.epoch_start_2,
    sealed          = excluded.sealed`,
		rec.FabricIndex, rec.GroupKeySetID, uint8(rec.SecurityPolicy),
		keys[0], int64(rec.EpochStart0), //nolint:gosec // start time fits in int64 in practice; see #20
		nullableBytes(keys[1]), nullableEpochStart(rec.EpochKey1, rec.EpochStart1),
		nullableBytes(keys[2]), nullableEpochStart(rec.EpochKey2, rec.EpochStart2),
		s.sealer != nil,
	); err != nil {
		return fmt.Errorf("store: upsert group key set: %w", err)
	}
	return nil
}

// GetGroupKeySet returns one key-set. Returns [ErrGroupKeySetNotFound]
// when no row exists.
func (s *Store) GetGroupKeySet(ctx context.Context, fabricIndex uint8, groupKeySetID uint16) (GroupKeySet, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT fabric_index, group_key_set_id, security_policy,
       epoch_key_0, epoch_start_0,
       epoch_key_1, epoch_start_1,
       epoch_key_2, epoch_start_2, sealed
FROM matter_group_keys
WHERE fabric_index = ? AND group_key_set_id = ?`, fabricIndex, groupKeySetID)
	rec, err := s.scanGroupKeySet(row)
	if errors.Is(err, sql.ErrNoRows) {
		return GroupKeySet{}, ErrGroupKeySetNotFound
	}
	if err != nil {
		return GroupKeySet{}, fmt.Errorf("store: get group key set: %w", err)
	}
	return rec, nil
}

// ListGroupKeySets returns every key-set for fabricIndex ordered by
// group_key_set_id ascending. A row holding an epoch key of a length a
// write refuses — left by an earlier release or an edit of the file — is
// left out, so one bad row does not take the fabric's other key sets (the
// IPK's among them) down; [Store.GetGroupKeySet] of that row returns the
// error. A sealed value that does not open still fails the list.
func (s *Store) ListGroupKeySets(ctx context.Context, fabricIndex uint8) ([]GroupKeySet, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT fabric_index, group_key_set_id, security_policy,
       epoch_key_0, epoch_start_0,
       epoch_key_1, epoch_start_1,
       epoch_key_2, epoch_start_2, sealed
FROM matter_group_keys WHERE fabric_index = ?
ORDER BY group_key_set_id ASC`, fabricIndex)
	if err != nil {
		return nil, fmt.Errorf("store: list group key sets: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []GroupKeySet
	for rows.Next() {
		rec, err := s.scanGroupKeySet(rows)
		if errors.Is(err, errMalformedKey) {
			continue // GetGroupKeySet reports it; the fabric's other key sets stay usable
		}
		if err != nil {
			return nil, fmt.Errorf("store: list group key sets: scan: %w", err)
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list group key sets: rows: %w", err)
	}
	return out, nil
}

// RemoveGroupKeySet deletes one key-set together with the GroupKeyMap
// entries of the same fabric that name it, in one transaction. Mirrors
// matter.js GroupKeyManagementServer.keySetRemove, which filters those
// entries out of groupKeyMap (core§11.2.7.4.1). The map carries no foreign
// key to the key sets (an entry may name a set not written yet), so the
// removal is explicit rather than a cascade.
func (s *Store) RemoveGroupKeySet(ctx context.Context, fabricIndex uint8, groupKeySetID uint16) error {
	return s.inTx(ctx, "remove group key set", func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
DELETE FROM matter_group_key_map WHERE fabric_index = ? AND group_key_set_id = ?`,
			fabricIndex, groupKeySetID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `
DELETE FROM matter_group_keys WHERE fabric_index = ? AND group_key_set_id = ?`,
			fabricIndex, groupKeySetID)
		return err
	})
}

// RemoveGroupKeysByFabric deletes all key-sets and GroupKeyMap entries
// for fabricIndex. Called in the AddNOC failure rollback path after the
// fabric row exists but a subsequent step fails, so no orphaned key
// material persists for a fabric that never completed commissioning.
func (s *Store) RemoveGroupKeysByFabric(ctx context.Context, fabricIndex uint8) error {
	return s.inTx(ctx, "remove group keys by fabric", func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
DELETE FROM matter_group_key_map WHERE fabric_index = ?`, fabricIndex); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `
DELETE FROM matter_group_keys WHERE fabric_index = ?`, fabricIndex)
		return err
	})
}

// inTx runs fn in one transaction, wrapping a failure with op.
func (s *Store) inTx(ctx context.Context, op string, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: %s: %w", op, err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(tx); err != nil {
		return fmt.Errorf("store: %s: %w", op, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: %s: %w", op, err)
	}
	return nil
}

// GroupKeyMapping binds a GroupID to a GroupKeySetID inside a fabric
// (Matter §11.2.10.4 GroupKeyMap attribute).
type GroupKeyMapping struct {
	FabricIndex   uint8
	GroupID       uint16
	GroupKeySetID uint16
}

// SetGroupKeyMapping upserts a GroupID → GroupKeySetID binding for
// fabricIndex: a GroupID the map holds keeps its place, a new one goes
// after the last. The fabric must exist (FK); the key set need not — see
// the matter_group_key_map comment in schema.sql.
func (s *Store) SetGroupKeyMapping(ctx context.Context, m GroupKeyMapping) error {
	if _, err := s.db.ExecContext(ctx, `
INSERT INTO matter_group_key_map (fabric_index, group_id, group_key_set_id, position)
VALUES (?, ?, ?, (SELECT COALESCE(MAX(position) + 1, 0) FROM matter_group_key_map WHERE fabric_index = ?))
ON CONFLICT(fabric_index, group_id) DO UPDATE SET
    group_key_set_id = excluded.group_key_set_id`,
		m.FabricIndex, m.GroupID, m.GroupKeySetID, m.FabricIndex); err != nil {
		return fmt.Errorf("store: set group key mapping: %w", err)
	}
	return nil
}

// ReplaceGroupKeyMappings implements [GroupKeys] in one transaction: the
// fabric's rows are deleted and mappings inserted in input order, each
// with its index as its position; a
// duplicate GroupID fails the primary key, an unknown fabric the foreign
// key, and either rolls the whole write back.
func (s *Store) ReplaceGroupKeyMappings(ctx context.Context, fabricIndex uint8, mappings []GroupKeyMapping) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: replace group key mappings: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM matter_group_key_map WHERE fabric_index = ?`, fabricIndex); err != nil {
		return fmt.Errorf("store: replace group key mappings: delete: %w", err)
	}
	for i, m := range mappings {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO matter_group_key_map (fabric_index, group_id, group_key_set_id, position) VALUES (?, ?, ?, ?)`,
			fabricIndex, m.GroupID, m.GroupKeySetID, i); err != nil {
			return fmt.Errorf("store: replace group key mappings: insert %d (group %d): %w", i, m.GroupID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: replace group key mappings: commit: %w", err)
	}
	return nil
}

// RemoveGroupKeyMapping deletes a (fabric, group_id) binding.
func (s *Store) RemoveGroupKeyMapping(ctx context.Context, fabricIndex uint8, groupID uint16) error {
	if _, err := s.db.ExecContext(ctx, `
DELETE FROM matter_group_key_map WHERE fabric_index = ? AND group_id = ?`,
		fabricIndex, groupID); err != nil {
		return fmt.Errorf("store: remove group key mapping: %w", err)
	}
	return nil
}

// ListGroupKeyMappings returns every (group_id → group_key_set_id) row
// for fabricIndex in the order the map was written (position, then
// group_id for the rows a release before v11 stored).
func (s *Store) ListGroupKeyMappings(ctx context.Context, fabricIndex uint8) ([]GroupKeyMapping, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT fabric_index, group_id, group_key_set_id
FROM matter_group_key_map WHERE fabric_index = ?
ORDER BY position ASC, group_id ASC`, fabricIndex)
	if err != nil {
		return nil, fmt.Errorf("store: list group key mappings: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []GroupKeyMapping
	for rows.Next() {
		var m GroupKeyMapping
		if err := rows.Scan(&m.FabricIndex, &m.GroupID, &m.GroupKeySetID); err != nil {
			return nil, fmt.Errorf("store: list group key mappings: scan: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list group key mappings: rows: %w", err)
	}
	return out, nil
}

// epochKeySize is an epoch key's size, the length cluster/core's
// KeySetWrite accepts (group_key_management.go, epochKeyLen); a key set
// read with another is refused.
const epochKeySize = 16

// errMalformedKey is a stored key of a length a write would have refused:
// a row an earlier release, or an edit of the file, left.
var errMalformedKey = errors.New("store: malformed key")

// checkGroupKeySet is the write side of the epoch key lengths: EpochKey0
// epochKeySize bytes, EpochKey1 and EpochKey2 epochKeySize bytes or absent;
// an empty slot is normalised to absent (nil, its start 0), so a write and
// its read agree in both backends (store/memstore copies the rule).
func checkGroupKeySet(rec GroupKeySet) (GroupKeySet, error) {
	if len(rec.EpochKey0) != epochKeySize {
		return rec, fmt.Errorf("store: epoch_key_0 length=%d (want %d)", len(rec.EpochKey0), epochKeySize)
	}
	for i, k := range []*[]byte{&rec.EpochKey1, &rec.EpochKey2} {
		if len(*k) == 0 {
			*k = nil
			continue
		}
		if len(*k) != epochKeySize {
			return rec, fmt.Errorf("store: epoch_key_%d length=%d (want %d)", i+1, len(*k), epochKeySize)
		}
	}
	if rec.EpochKey1 == nil {
		rec.EpochStart1 = 0
	}
	if rec.EpochKey2 == nil {
		rec.EpochStart2 = 0
	}
	return rec, nil
}

// groupEpochKeyColumns are the epoch key columns, by slot.
var groupEpochKeyColumns = [3]string{"epoch_key_0", "epoch_key_1", "epoch_key_2"}

// scanGroupKeySet reads one key set, its epoch keys opened.
func (s *Store) scanGroupKeySet(r scanRow) (GroupKeySet, error) {
	var (
		rec    GroupKeySet
		policy uint8
		key1   sql.NullString
		key2   sql.NullString
		start0 int64
		start1 sql.NullInt64
		start2 sql.NullInt64
		sealed bool
	)
	if err := r.Scan(
		&rec.FabricIndex, &rec.GroupKeySetID, &policy,
		&rec.EpochKey0, &start0,
		&key1, &start1,
		&key2, &start2, &sealed,
	); err != nil {
		return GroupKeySet{}, err
	}
	rec.SecurityPolicy = SecurityPolicy(policy)
	rec.EpochStart0 = uint64(start0) //nolint:gosec // round-trip of stored value; see #20
	if key1.Valid {
		rec.EpochKey1 = []byte(key1.String)
	}
	if start1.Valid {
		rec.EpochStart1 = uint64(start1.Int64) //nolint:gosec // round-trip of stored value; see #20
	}
	if key2.Valid {
		rec.EpochKey2 = []byte(key2.String)
	}
	if start2.Valid {
		rec.EpochStart2 = uint64(start2.Int64) //nolint:gosec // round-trip of stored value; see #20
	}
	for i, k := range [3]*[]byte{&rec.EpochKey0, &rec.EpochKey1, &rec.EpochKey2} {
		plain, err := s.openColumn(*k, sealed, "matter_group_keys", groupEpochKeyColumns[i], rec.FabricIndex, rec.GroupKeySetID)
		if err != nil {
			return GroupKeySet{}, err
		}
		if i > 0 && plain != nil && len(plain) == 0 {
			// An empty optional slot an earlier release stored is read
			// as absent, as a write now stores it.
			plain = nil
			if i == 1 {
				rec.EpochStart1 = 0
			} else {
				rec.EpochStart2 = 0
			}
		}
		if plain != nil && len(plain) != epochKeySize {
			return GroupKeySet{}, fmt.Errorf("%w: %s length=%d (want %d)", errMalformedKey, groupEpochKeyColumns[i], len(plain), epochKeySize)
		}
		*k = plain
	}
	return rec, nil
}

// nullableEpochStart pairs an EpochStart with its key — when the key
// is nil, the start is also stored as NULL so the two slots stay
// consistent.
func nullableEpochStart(key []byte, start uint64) any {
	if key == nil {
		return nil
	}
	return int64(start) //nolint:gosec // start time fits in int64 in practice; see #20
}

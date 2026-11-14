// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"slices"
)

// Setting keys for persisted writable cluster attributes. Matter
// §11.1.6 marks NodeLabel (0x0005) and Location (0x0006) with the "N"
// (non-volatile) quality — a commissioner write must survive a daemon
// restart. matter.js persists them through the behavior state storage
// (BasicInformationServer state with persistent qualities).
const (
	// SettingNodeLabel holds the commissioner-written
	// BasicInformation.NodeLabel.
	SettingNodeLabel = "basic_information.node_label"
	// SettingLocation holds the commissioner-written
	// BasicInformation.Location (ISO-3166 2-letter code).
	SettingLocation = "basic_information.location"
	// SettingUniqueID holds BasicInformation.UniqueID. The attribute
	// carries quality F (fixed for the lifetime of the device, Matter
	// §11.1.5.13), which a derivation can only approximate: it is stable
	// exactly as long as its inputs are, so a config edit to the bridge
	// label would move the node's identity under every controller cache.
	// Persisting the value on first boot and feeding it back through
	// core.Config.UniqueID makes the attribute genuinely fixed — the same
	// shape matter.js uses (BasicInformationServer's uniqueId, quality
	// "FN": created once, restored from state afterwards).
	SettingUniqueID = "basic_information.unique_id"
	// SettingTermsAcceptance holds the committed terms-and-conditions
	// acceptance of GeneralCommissioning's TC feature, "<version>:<bits>"
	// (cluster/core general_commissioning_tc.go); erased with the store.
	SettingTermsAcceptance = "general_commissioning.tc_acceptance"
)

// MetadataKeyEventNumber is the matter_metadata key holding the
// persisted EventNumber ceiling. The event log seeds its counter from
// this value at boot and re-persists a new ceiling ahead of use, so
// event numbers stay monotonic across restarts (Matter §7.14.2.1;
// chip EventManagement's counter-epoch pattern).
const MetadataKeyEventNumber = "event_number_ceiling"

// GetSetting returns the persisted string for key. ok=false when the
// key has never been written (callers keep their configured default).
func (s *Store) GetSetting(ctx context.Context, key string) (value string, ok bool, err error) {
	row := s.db.QueryRowContext(ctx, `SELECT value FROM matter_settings WHERE key = ?`, key)
	if err := row.Scan(&value); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("store: get setting %q: %w", key, err)
	}
	return value, true, nil
}

// SetSetting upserts the persisted string for key.
func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	if _, err := s.db.ExecContext(ctx, `
INSERT INTO matter_settings (key, value) VALUES (?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value); err != nil {
		return fmt.Errorf("store: set setting %q: %w", key, err)
	}
	return nil
}

// SetSettings implements [Settings]: every key of values is upserted in one
// transaction, in ascending key order, so a failure on any key rolls back
// the ones before it and the call leaves none of them written.
func (s *Store) SetSettings(ctx context.Context, values map[string]string) error {
	if len(values) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: set settings: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, k := range slices.Sorted(maps.Keys(values)) {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO matter_settings (key, value) VALUES (?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value`, k, values[k]); err != nil {
			return fmt.Errorf("store: set settings: %q: %w", k, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: set settings: commit: %w", err)
	}
	return nil
}

// DeleteSetting removes key; a key that is not there is no error.
func (s *Store) DeleteSetting(ctx context.Context, key string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM matter_settings WHERE key = ?`, key); err != nil {
		return fmt.Errorf("store: delete setting %q: %w", key, err)
	}
	return nil
}

// GetMetadataCounter returns the persisted integer counter for key
// from matter_metadata. ok=false when the key has never been written.
func (s *Store) GetMetadataCounter(ctx context.Context, key string) (value uint64, ok bool, err error) {
	row := s.db.QueryRowContext(ctx, `SELECT value FROM matter_metadata WHERE key = ?`, key)
	var v int64
	if err := row.Scan(&v); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("store: get metadata %q: %w", key, err)
	}
	if v < 0 {
		return 0, false, fmt.Errorf("store: metadata %q negative: %d", key, v)
	}
	return uint64(v), true, nil
}

// SetMetadataCounter upserts the integer counter for key in
// matter_metadata.
func (s *Store) SetMetadataCounter(ctx context.Context, key string, value uint64) error {
	if value > 1<<62 {
		return fmt.Errorf("store: metadata %q overflows int64: %d", key, value)
	}
	if _, err := s.db.ExecContext(ctx, `
INSERT INTO matter_metadata (key, value) VALUES (?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, int64(value)); err != nil { //nolint:gosec // bounded by the 1<<62 guard above
		return fmt.Errorf("store: set metadata %q: %w", key, err)
	}
	return nil
}

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
	"strings"
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
// key has never been written (callers keep their configured default). A
// sealed value is opened ([NewSealed]).
func (s *Store) GetSetting(ctx context.Context, key string) (value string, ok bool, err error) {
	value, ok, err = s.readSetting(ctx, s.db, key)
	if err != nil {
		return "", false, fmt.Errorf("store: get setting %q: %w", key, err)
	}
	return value, ok, nil
}

// errReservedSetting is a Settings write of the seal canary's key.
var errReservedSetting = errors.New("store: the key is the store's seal canary (reserved)")

// checkSettingWrite refuses a write of the seal canary's key, and a secret
// setting a Store without a sealer would write in clear into a sealed
// database ([ErrNoSealer]).
func (s *Store) checkSettingWrite(ctx context.Context, q sqlExecQuerier, key string) error {
	if key == settingSealCanary {
		return errReservedSetting
	}
	if isSealedSetting(key) {
		return s.refuseClearSecretWrite(ctx, q)
	}
	return nil
}

// upsertSetting is the statement of SetSetting and SetSettings.
const upsertSetting = `
INSERT INTO matter_settings (key, value, sealed) VALUES (?, ?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value, sealed = excluded.sealed`

// SetSetting upserts the persisted string for key, sealed when the store
// has a sealer and the key holds a secret ([NewSealed]).
func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	if err := s.checkSettingWrite(ctx, s.db, key); err != nil {
		return fmt.Errorf("store: set setting %q: %w", key, err)
	}
	stored, sealed, err := s.sealSetting(key, value)
	if err != nil {
		return fmt.Errorf("store: set setting %q: %w", key, err)
	}
	if _, err := s.db.ExecContext(ctx, upsertSetting, key, stored, sealed); err != nil {
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
		if err := s.checkSettingWrite(ctx, tx, k); err != nil {
			return fmt.Errorf("store: set settings: %q: %w", k, err)
		}
		stored, sealed, err := s.sealSetting(k, values[k])
		if err != nil {
			return fmt.Errorf("store: set settings: %q: %w", k, err)
		}
		if _, err := tx.ExecContext(ctx, upsertSetting, k, stored, sealed); err != nil {
			return fmt.Errorf("store: set settings: %q: %w", k, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: set settings: commit: %w", err)
	}
	return nil
}

// DeleteSetting removes key; a key that is not there is no error.
//
// Deprecated: use [Store.DeleteSettings], which every [Settings] backend
// implements. Removable in v0.25.0.
func (s *Store) DeleteSetting(ctx context.Context, key string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM matter_settings WHERE key = ? AND key <> ?`, key, settingSealCanary); err != nil {
		return fmt.Errorf("store: delete setting %q: %w", key, err)
	}
	return nil
}

// DeleteSettings implements [Settings]: every key is deleted in one
// transaction, so a failure on any key keeps all of them.
func (s *Store) DeleteSettings(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: delete settings: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, k := range keys {
		if _, err := tx.ExecContext(ctx, `DELETE FROM matter_settings WHERE key = ? AND key <> ?`, k, settingSealCanary); err != nil {
			return fmt.Errorf("store: delete settings: %q: %w", k, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: delete settings: commit: %w", err)
	}
	return nil
}

// DeleteSettingsWithPrefix implements [Settings] in one statement: the
// keys from prefix up to, not including, prefix with its final "." raised
// to "/" — exactly the keys that begin with prefix under the BINARY
// collation the key column compares with.
func (s *Store) DeleteSettingsWithPrefix(ctx context.Context, prefix string) error {
	if err := checkSettingsPrefix(prefix); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM matter_settings WHERE key >= ? AND key < ? AND key <> ?`,
		prefix, settingsPrefixEnd(prefix), settingSealCanary); err != nil {
		return fmt.Errorf("store: delete settings with prefix %q: %w", prefix, err)
	}
	return nil
}

// checkSettingsPrefix refuses a prefix [Settings.DeleteSettingsWithPrefix]
// does not accept: one that does not end in ".", the empty one included.
func checkSettingsPrefix(prefix string) error {
	if !strings.HasSuffix(prefix, ".") {
		return fmt.Errorf(`store: delete settings with prefix %q: a prefix must end in "."`, prefix)
	}
	return nil
}

// settingsPrefixEnd is the first key after every key that begins with
// prefix, which ends in ".": the prefix with that dot raised to "/".
func settingsPrefixEnd(prefix string) string {
	return prefix[:len(prefix)-1] + "/"
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

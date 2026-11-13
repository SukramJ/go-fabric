// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package store

import (
	"context"
	"errors"
	"fmt"
)

// erasedTables are the matter_* tables of the schema (store/schema.sql),
// every one the factory reset empties.
var erasedTables = []string{
	"matter_fabrics",
	"matter_node_identities",
	"matter_group_keys",
	"matter_group_key_map",
	"matter_group_table",
	"matter_groupcast_groups",
	"matter_acl_entries",
	"matter_resumption",
	"matter_diagnostics",
	"matter_metadata",
	"matter_settings",
	"matter_persistent_subscriptions",
	"matter_server_subscriptions",
	"matter_ota_default_providers",
	"matter_thermostat_presets",
	"matter_doorlock_users",
	"matter_doorlock_credentials",
	"matter_doorlock_schedules",
	"matter_icd_registrations",
}

// Erase empties every matter_* table in one transaction and seeds the
// next fabric index as the schema does ([Eraser]), then scrubs the file:
// a DELETE only unlinks rows, and their bytes — and those of every row a
// plain delete removed earlier, RemoveFabric's among them — stay in the
// database's free pages and in the write-ahead log. VACUUM rebuilds the
// database without its free pages, and wal_checkpoint(TRUNCATE) copies the
// rebuilt pages into the file and truncates the -wal to zero bytes, so
// none of the erased bytes remain in the database file, its -wal or its
// -shm (TestStoreEraseLeavesNoResidue). It runs on the store's own handle,
// whoever opened it. secure_delete is not used: it zeroes only the rows
// its own connection deletes, so it misses the earlier deletes, and in WAL
// mode the log keeps them until a checkpoint anyway. Going beyond matter.js,
// whose SQLite driver erases with plain DELETE
// (packages/nodejs/src/storage/sqlite/SqliteStorageDriver.ts:201,207,305).
func (s *Store) Erase(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: erase: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, table := range erasedTables {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+table); err != nil { //nolint:gosec // table names are the package's constants
			return fmt.Errorf("store: erase %s: %w", table, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO matter_metadata (key, value) VALUES ('next_fabric_index', 1)`); err != nil {
		return fmt.Errorf("store: erase: seed next fabric index: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: erase: %w", err)
	}
	return s.scrub(ctx)
}

// scrub rewrites the database without its free pages and empties the
// write-ahead log. In rollback-journal mode the checkpoint is a no-op that
// reports busy 0. A checkpoint a reader blocks reports busy 1: the -wal
// may then still hold erased bytes, and Erase says so.
func (s *Store) scrub(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `VACUUM`); err != nil {
		return fmt.Errorf("store: erase: vacuum: %w", err)
	}
	var busy, logFrames, checkpointed int
	if err := s.db.QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &logFrames, &checkpointed); err != nil {
		return fmt.Errorf("store: erase: wal checkpoint: %w", err)
	}
	if busy != 0 {
		return errors.New("store: erase: wal checkpoint blocked by a reader: the -wal may still hold erased data")
	}
	return nil
}

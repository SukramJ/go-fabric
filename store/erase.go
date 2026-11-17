// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"sync"
)

// schemaSeedRE matches one seeding statement of [Schema]: an INSERT OR
// IGNORE up to its semicolon.
var schemaSeedRE = regexp.MustCompile(`(?ms)^INSERT OR IGNORE INTO [^;]*;`)

// schemaSeeds are the seeding statements of [Schema], in order: what
// [Store.Erase] runs again after its deletes, so an erased store holds
// what a freshly applied schema holds — a seed a later schema adds
// included (TestEraseLeavesWhatAFreshSchemaHolds).
var schemaSeeds = sync.OnceValue(func() []string { return schemaSeedRE.FindAllString(schemaDDL, -1) })

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
	"matter_owed_fabric_events",
}

// ErrScrubIncomplete is the error of an [Store.Erase] whose deletes
// committed — every row is gone — and of a [Scrub], when the scrub that
// follows did not finish: a VACUUM that failed (a full disk) or a
// checkpoint another connection's reader blocked. The erased rows' bytes
// may then remain in the database's free pages or its -wal until a later
// [Scrub] of the same file succeeds. A caller tells it apart from a failed
// erase with errors.Is.
var ErrScrubIncomplete = errors.New("store: erased, but the scrub did not finish: the erased bytes may remain in the file")

// Erase empties every matter_* table in one transaction and seeds the rows
// the schema seeds — every INSERT OR IGNORE of [Schema]: the next fabric
// index and the diagnostics row ([Eraser]) — then scrubs the file:
// a DELETE only unlinks rows, and their bytes — and those of every row a
// plain delete removed earlier, RemoveFabric's among them — stay in the
// database's free pages and in the write-ahead log. VACUUM rebuilds the
// database without its free pages, and wal_checkpoint(TRUNCATE) copies the
// rebuilt pages into the file and truncates the -wal to zero bytes, so
// none of the erased bytes remain in the database file, its -wal or its
// -shm (TestStoreEraseLeavesNoResidue) — [Scrub]. It runs on the store's own handle,
// whoever opened it. secure_delete is not used: it zeroes only the rows
// its own connection deletes, so it misses the earlier deletes, and in WAL
// mode the log keeps them until a checkpoint anyway. A scrub that does not
// finish after the deletes committed is [ErrScrubIncomplete]: the store is
// erased, and a later [Scrub] can remove the bytes. Going beyond matter.js,
// whose SQLite driver erases with plain DELETE
// (packages/nodejs/src/storage/sqlite/SqliteStorageDriver.ts:201, :207,
// :305 at db021da0; by_design BD-Matter-StoreEraseScrub).
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
	for _, seed := range schemaSeeds() {
		if _, err := tx.ExecContext(ctx, seed); err != nil {
			return fmt.Errorf("store: erase: seed: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: erase: %w", err)
	}
	return Scrub(ctx, s.db)
}

// Scrub is Erase's last step on its own, for a database whose rows another
// store deleted — the endpoint records node.Node.FactoryReset erases from a
// file whose credential store is a host override, or erases after this
// store's Erase: it rewrites the database without its free pages and
// empties the write-ahead log, so the deleted rows' bytes remain in none of
// the database file, its -wal or its -shm. In rollback-journal mode the
// checkpoint is a no-op that reports busy 0. A checkpoint a reader blocks
// reports busy 1: the -wal may then still hold erased bytes, and Scrub
// says so. Every error it returns is [ErrScrubIncomplete]; running it
// again once the reader is gone finishes the scrub.
//
// Scrub runs on one connection of the pool with PRAGMA
// journal_size_limit = 0 for the run (the connection's own limit restored
// after it): a rollback journal SQLite keeps between transactions — in
// journal_mode PERSIST, and in any rollback mode under
// locking_mode EXCLUSIVE — still held the pre-images of the deleted or
// sealed pages after the VACUUM; with the limit at 0 SQLite truncates it
// at the VACUUM's commit (measured: TestNewSealedLeavesNoClearSecretPerJournalMode,
// which switching only PERSIST to DELETE for the run did not satisfy under
// EXCLUSIVE). The disk blocks a truncated or deleted -journal or -wal
// occupied are not overwritten by any SQLite call.
func Scrub(ctx context.Context, db *sql.DB) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrScrubIncomplete, err)
	}
	defer func() { _ = conn.Close() }()
	var limit int64
	if err := conn.QueryRowContext(ctx, `PRAGMA journal_size_limit`).Scan(&limit); err != nil {
		return fmt.Errorf("%w: journal size limit: %w", ErrScrubIncomplete, err)
	}
	if limit != 0 {
		if _, err := conn.ExecContext(ctx, `PRAGMA journal_size_limit = 0`); err != nil {
			return fmt.Errorf("%w: journal size limit: %w", ErrScrubIncomplete, err)
		}
		defer func() {
			_, _ = conn.ExecContext(context.WithoutCancel(ctx), `PRAGMA journal_size_limit = `+strconv.FormatInt(limit, 10))
		}()
	}
	if _, err := conn.ExecContext(ctx, `VACUUM`); err != nil {
		return fmt.Errorf("%w: vacuum: %w", ErrScrubIncomplete, err)
	}
	var busy, logFrames, checkpointed int
	if err := conn.QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &logFrames, &checkpointed); err != nil {
		return fmt.Errorf("%w: wal checkpoint: %w", ErrScrubIncomplete, err)
	}
	if busy != 0 {
		return fmt.Errorf("%w: the wal checkpoint was blocked by a reader, so the -wal may still hold erased data", ErrScrubIncomplete)
	}
	return nil
}

// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package store

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
)

// schemaDDL is the DDL this package's queries are written against.
//
// It is embedded rather than kept under testdata/ because a host outside
// this module cannot import a testdata file: before it was embedded, the
// module's own example transcribed 140 lines of it, and two copies of one
// schema drift with nothing to catch it.
//
//go:embed schema.sql
var schemaDDL string

// Schema returns the DDL for the matter_* tables this package reads and
// writes, as one SQL script.
//
// A host that owns a migration tool feeds this text through it — as the
// body of a new migration, or as a fixture its own migration is diffed
// against — and keeps ownership of when it runs. A host without one calls
// [Apply]. Either way the text is the module's, so a column added here
// arrives with the dependency bump instead of being missed.
//
// It creates no endpoint table: endpoint identity is keyed on the host's
// own source identity and lives behind endpoint.Store, whose SQLite
// implementation ships its own schema.
func Schema() string { return schemaDDL }

// Apply executes [Schema] against db.
//
// Every statement is idempotent (`IF NOT EXISTS`, `INSERT OR IGNORE`), so
// calling this on every boot is the intended use rather than a first-run
// special case. It does not open a database and does not set pragmas: the
// cascade deletes in the schema need `PRAGMA foreign_keys(1)` on every
// pooled connection, which is a property of the DSN the host opened and
// cannot be fixed up from here.
//
// After the DDL it runs [Upgrade], which brings a database created by an
// earlier release of this package in line with the current schema.
func Apply(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, schemaDDL); err != nil {
		return fmt.Errorf("matter store: apply schema: %w", err)
	}
	return Upgrade(ctx, db)
}

// Upgrade rewrites the parts of an existing database that an idempotent
// `CREATE TABLE IF NOT EXISTS` cannot reach: a table created by an earlier
// release keeps its old definition, so a changed constraint needs an
// explicit rebuild. Each step detects whether it is due and does nothing
// otherwise, so Upgrade is safe on every boot and on a fresh database.
//
// [Apply] runs it. A host that feeds [Schema] through its own migration
// tool calls it once after that tool has run.
//
// Steps:
//
//   - matter_group_key_map loses the foreign key from
//     (fabric_index, group_key_set_id) to matter_group_keys. A GroupKeyMap
//     entry may name a key set that does not exist yet — matter.js accepts
//     the write (GroupKeyManagementServer.ts #validateGroupKeyMap, the
//     key-set check left commented out because certification tests write
//     the map first) — and the constraint turned that write into a Failure.
func Upgrade(ctx context.Context, db *sql.DB) error {
	if err := dropGroupKeyMapKeySetForeignKey(ctx, db); err != nil {
		return fmt.Errorf("matter store: upgrade: %w", err)
	}
	return nil
}

// dropGroupKeyMapKeySetForeignKey rebuilds matter_group_key_map without its
// foreign key to matter_group_keys when the table still carries one. SQLite
// cannot drop a constraint in place: the rows move to a new table of the
// current definition, which then takes the old name — all in one
// transaction. Nothing references matter_group_key_map, so the rebuild
// needs no foreign_keys pragma change.
func dropGroupKeyMapKeySetForeignKey(ctx context.Context, db *sql.DB) error {
	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pragma_foreign_key_list('matter_group_key_map') WHERE "table" = 'matter_group_keys'`,
	).Scan(&n); err != nil {
		return fmt.Errorf("inspect matter_group_key_map: %w", err)
	}
	if n == 0 {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("rebuild matter_group_key_map: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, stmt := range []string{
		`CREATE TABLE matter_group_key_map_rebuild (
    fabric_index        INTEGER NOT NULL,
    group_id            INTEGER NOT NULL CHECK(group_id BETWEEN 0 AND 65535),
    group_key_set_id    INTEGER NOT NULL,
    PRIMARY KEY(fabric_index, group_id),
    FOREIGN KEY(fabric_index) REFERENCES matter_fabrics(fabric_index) ON DELETE CASCADE
)`,
		`INSERT INTO matter_group_key_map_rebuild (fabric_index, group_id, group_key_set_id)
    SELECT fabric_index, group_id, group_key_set_id FROM matter_group_key_map`,
		`DROP TABLE matter_group_key_map`,
		`ALTER TABLE matter_group_key_map_rebuild RENAME TO matter_group_key_map`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("rebuild matter_group_key_map: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("rebuild matter_group_key_map: %w", err)
	}
	return nil
}

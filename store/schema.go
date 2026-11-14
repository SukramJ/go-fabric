// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package store

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
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
// A host whose migration tool needs the schema as versioned steps rather
// than one idempotent script takes [Migrations] instead; the two produce
// the same schema ([SchemaFingerprint]).
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
		return fmt.Errorf("store: apply schema: %w", err)
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
//   - matter_icd_registrations' icd_key, stored in clear before v0.12.1,
//     is sealed under the store's ICD key ([SettingICDKey]); the schema is
//     unchanged, so the step has no migration of its own.
//   - matter_fabrics and matter_node_identities gain the columns of
//     migration v8 (the pending mark, the VID verification data, the prior
//     values of an uncommitted UpdateNOC), each one its table lacks, with
//     v8's statements, in one transaction.
//   - The settings matter.fabric.<index>.vid.statement and
//     matter.fabric.<index>.vid.vvsc, where releases before v8 kept a
//     fabric's VID verification data, move into that fabric's
//     vid_statement and vvsc columns and are deleted, in one transaction
//     ([moveVIDSettings]). Once moved there is no key left, so the step
//     finds nothing to do on the next boot.
func Upgrade(ctx context.Context, db *sql.DB) error {
	if err := dropGroupKeyMapKeySetForeignKey(ctx, db); err != nil {
		return fmt.Errorf("store: upgrade: %w", err)
	}
	if err := sealClearICDKeys(ctx, db); err != nil {
		return fmt.Errorf("store: upgrade: %w", err)
	}
	if err := addFabricPendingColumns(ctx, db); err != nil {
		return fmt.Errorf("store: upgrade: %w", err)
	}
	if err := moveVIDSettings(ctx, db); err != nil {
		return fmt.Errorf("store: upgrade: %w", err)
	}
	return nil
}

// addFabricPendingColumns adds each column of migration v8 that its
// table does not have yet, with v8's own statement, so the table text is
// the one [Migrations] produces. It decides per column: a database from an
// earlier release may lack one of the two tables, which the schema's
// CREATE TABLE IF NOT EXISTS then creates with the columns already in it.
// The statements run in one transaction, so a failure adds none of them;
// a table that does not exist is left alone.
func addFabricPendingColumns(ctx context.Context, db *sql.DB) error {
	var due []string
	for _, m := range addColumnRE.FindAllStringSubmatch(mustMigrationFile("v8.up.sql"), -1) {
		var tables, columns int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, m[2]).Scan(&tables); err != nil {
			return fmt.Errorf("inspect %s: %w", m[2], err)
		}
		if tables == 0 {
			continue
		}
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, m[2], m[3]).Scan(&columns); err != nil {
			return fmt.Errorf("inspect %s: %w", m[2], err)
		}
		if columns == 0 {
			due = append(due, m[1])
		}
	}
	if len(due) == 0 {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("add the fabric pending columns: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, stmt := range due {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("add the fabric pending columns: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("add the fabric pending columns: %w", err)
	}
	return nil
}

// addColumnRE matches one ALTER TABLE … ADD COLUMN statement of a
// migration: the statement, its table and its column.
var addColumnRE = regexp.MustCompile(`(?m)^(ALTER TABLE (\w+) ADD COLUMN (\w+) [^;]*);`)

// vidSettingFields are the two settings a release before v8 kept a
// fabric's VID verification data under, "matter.fabric.<index>.vid.<field>",
// with the column each one moves into.
var vidSettingFields = map[string]string{"statement": "vid_statement", "vvsc": "vvsc"}

// vidSettingKey is the key cluster/core's vidSettingKey builds
// (cluster/core/vid_verification.go:65-67): the fabric index in decimal.
func vidSettingKey(fabricIndex int, field string) string {
	return "matter.fabric." + strconv.Itoa(fabricIndex) + ".vid." + field
}

// moveVIDSettings moves every VID setting into its fabric's column and
// deletes the key, in one transaction. The value is decoded as
// cluster/core's vidVerification decodes it
// (cluster/core/vid_verification.go:73-78): hex, a decoding error ignored
// with what decoded before it kept, an empty value meaning none; the column
// takes NULL for none. A key present wins over the column, since only a
// release before v8 writes the key and so it is the later write. A key
// whose fabric the table does not hold (a removed fabric's emptied value)
// is deleted without a move. Keys of another shape are not the caller's
// and stay.
func moveVIDSettings(ctx context.Context, db *sql.DB) error {
	var columns int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('matter_fabrics') WHERE name = 'vid_statement'`).Scan(&columns); err != nil {
		return fmt.Errorf("move the VID settings: %w", err)
	}
	if columns == 0 {
		return nil // no matter_fabrics: nothing to move into
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("move the VID settings: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	moved, err := readVIDSettings(ctx, tx)
	if err != nil || len(moved) == 0 {
		return err
	}
	for _, m := range moved {
		var value any
		if b, _ := hex.DecodeString(m.value); len(b) > 0 {
			value = b
		}
		if _, err := tx.ExecContext(ctx, `UPDATE matter_fabrics SET `+vidSettingFields[m.field]+` = ? WHERE fabric_index = ?`, value, m.fabricIndex); err != nil { //nolint:gosec // the column is one of vidSettingFields' constants
			return fmt.Errorf("move the VID setting %q: %w", m.key, err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM matter_settings WHERE key = ?`, m.key); err != nil {
			return fmt.Errorf("move the VID setting %q: %w", m.key, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("move the VID settings: %w", err)
	}
	return nil
}

type vidSetting struct {
	key, value, field string
	fabricIndex       int
}

// readVIDSettings returns the settings whose key is exactly one
// [vidSettingKey] builds for a fabric index 0..255 (the uint8 the caller
// formats).
func readVIDSettings(ctx context.Context, tx *sql.Tx) ([]vidSetting, error) {
	keys := make(map[string]vidSetting, 2*256)
	for i := range 256 {
		for field := range vidSettingFields {
			k := vidSettingKey(i, field)
			keys[k] = vidSetting{key: k, field: field, fabricIndex: i}
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT key, value FROM matter_settings WHERE key LIKE 'matter.fabric.%.vid.%'`)
	if err != nil {
		return nil, fmt.Errorf("move the VID settings: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []vidSetting
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("move the VID settings: %w", err)
		}
		if m, ok := keys[k]; ok {
			m.value = v
			out = append(out, m)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("move the VID settings: %w", err)
	}
	return out, nil
}

// dropGroupKeyMapKeySetForeignKey rebuilds matter_group_key_map without its
// foreign key to matter_group_keys when the table still carries one. SQLite
// cannot drop a constraint in place: the rows move to a new table of the
// current definition, which then takes the old name — all in one
// transaction; the statements are migration v3's ([Migrations]). Nothing
// references matter_group_key_map, so the rebuild needs no foreign_keys
// pragma change.
func dropGroupKeyMapKeySetForeignKey(ctx context.Context, db *sql.DB) error {
	var n int
	if err := db.QueryRowContext(
		ctx,
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
	if _, err := tx.ExecContext(ctx, mustMigrationFile("v3.up.sql")); err != nil {
		return fmt.Errorf("rebuild matter_group_key_map: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("rebuild matter_group_key_map: %w", err)
	}
	return nil
}

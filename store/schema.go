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
func Apply(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, schemaDDL); err != nil {
		return fmt.Errorf("matter store: apply schema: %w", err)
	}
	return nil
}

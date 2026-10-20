// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"database/sql"
	"fmt"

	// The pure-Go SQLite driver behind [Store].Path. A host that hands the
	// node its own handle ([Store].DB) picks its driver itself.
	_ "modernc.org/sqlite"
)

// sqliteDriver is the database/sql driver name modernc.org/sqlite
// registers.
const sqliteDriver = "sqlite"

// sqlitePragmas are applied to every connection of the pool the node
// opens for [Store].Path, as the DSN's `_pragma` parameters (a pragma set
// by a statement reaches one pooled connection only): foreign_keys carries
// the store's cascade on fabric removal, busy_timeout makes a contended
// write wait instead of failing — what the reference daemon's openDB set
// before the facade opened the file itself.
const sqlitePragmas = "_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"

// openStorePath opens the SQLite file at path (created on first use) for
// [Store].Path; the node owns the handle and closes it on [Node.Stop].
func openStorePath(path string) (*sql.DB, error) {
	db, err := sql.Open(sqliteDriver, "file:"+path+"?"+sqlitePragmas)
	if err != nil {
		return nil, fmt.Errorf("node: open store %q: %w", path, err)
	}
	return db, nil
}

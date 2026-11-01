// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"

	// The pure-Go SQLite driver behind [Store].Path. A host that hands the
	// node its own handle ([Store].DB) picks its driver itself.
	_ "modernc.org/sqlite"
)

// sqliteDriver is the database/sql driver name modernc.org/sqlite
// registers.
const sqliteDriver = "sqlite"

// sqlitePragmas are applied to every connection of the pool
// [OpenSQLite] opens, as the DSN's `_pragma` parameters (a pragma set by a
// statement reaches one pooled connection only): foreign_keys carries the
// store's cascade on fabric removal, busy_timeout makes a contended write
// wait instead of failing — what the reference daemon's openDB set before
// the facade opened the file itself.
var sqlitePragmas = []string{"foreign_keys(1)", "busy_timeout(5000)"}

// OpenSQLite opens the SQLite database file at path (created on first
// use) with the module's pure-Go driver (modernc.org/sqlite), foreign keys
// on and a busy timeout of 5 s on every pooled connection, plus the
// `_pragma` values in pragmas (journal_mode(WAL), for one). The `file:`
// URI is built with net/url, so a path holding `#`, `?` or `%` names that
// file and keeps the pragmas instead of ending the file name early. The
// caller owns the handle. It opens the node's SQLite files: [Store].Path,
// fabrictest and the examples.
//
// A file OpenSQLite creates is readable and writable by its owner only
// (0600): it holds the node's private operational keys (a controller's
// CA root key too) unencrypted. SQLite creates the -wal and -shm files
// with the database file's permissions. An existing file keeps the
// permissions it has.
func OpenSQLite(path string, pragmas ...string) (*sql.DB, error) {
	if f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600); err == nil { //nolint:gosec // the caller's own store path
		_ = f.Close()
	} else if !errors.Is(err, fs.ErrExist) {
		return nil, fmt.Errorf("node: create sqlite %q: %w", path, err)
	}
	dsn := url.URL{
		Scheme:   "file",
		Path:     path,
		RawQuery: url.Values{"_pragma": append(append([]string(nil), sqlitePragmas...), pragmas...)}.Encode(),
	}
	db, err := sql.Open(sqliteDriver, dsn.String())
	if err != nil {
		return nil, fmt.Errorf("node: open sqlite %q: %w", path, err)
	}
	return db, nil
}

// openStorePath opens the SQLite file at path for [Store].Path; the node
// owns the handle and closes it on [Node.Stop].
func openStorePath(path string) (*sql.DB, error) {
	db, err := OpenSQLite(path)
	if err != nil {
		return nil, fmt.Errorf("node: open store %q: %w", path, err)
	}
	return db, nil
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"database/sql"
	"fmt"

	"github.com/SukramJ/go-fabric/store"
)

// openStorePath opens the SQLite file at path for [Store].Path with the
// module's opener ([store.OpenSQLite]: escaped `file:` URI, foreign keys
// and busy timeout on every connection, a new file created 0600); the
// node owns the handle and closes it on [Node.Stop].
func openStorePath(path string) (*sql.DB, error) {
	db, err := store.OpenSQLite(path)
	if err != nil {
		return nil, fmt.Errorf("node: open store %q: %w", path, err)
	}
	return db, nil
}

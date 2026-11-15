// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package store

import "database/sql"

// SealedWithoutCheck is a sealed Store on db that skips NewSealed — its
// key check and its upgrade — so a test reads a sealed database under
// another key row by row.
func SealedWithoutCheck(db *sql.DB, s Sealer) *Store { return &Store{db: db, sealer: s} }

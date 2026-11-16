// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package sqlstore

import (
	"context"
	"database/sql"

	"github.com/SukramJ/go-fabric/store"
)

// SealWithoutScrub runs NewSealed's sealing transaction and stops before
// the scrub: a process that ends after the sealing committed.
func SealWithoutScrub(ctx context.Context, db *sql.DB, s store.Sealer) (int, error) {
	return (&Store{db: db, sealer: s}).sealClearValues(ctx)
}

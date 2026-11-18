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
	_, n, err := (&Store{db: db, sealer: s}).sealUnmarked(ctx)
	return n, err
}

// MetadataSealCanary is the reserved matter_client_metadata key of the
// registry's seal mark.
const MetadataSealCanary = metadataKeySealCanary

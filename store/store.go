// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package store

import (
	"database/sql"
	"errors"
)

// Store is the typed access surface over the matter_* tables of this
// package's schema ([Schema]). The *sql.DB comes from the host — or from
// [OpenSQLite] — and must already carry the schema.
type Store struct {
	db *sql.DB
}

// New returns a Store backed by db. The DB must already have the
// matter_* tables: [Apply] creates them, or the host's migration tool
// runs [Migrations].
func New(db *sql.DB) *Store {
	return &Store{db: db}
}

// Sentinel errors. Callers use [errors.Is] to detect them.
var (
	// ErrFabricNotFound is returned when a fabric_index lookup misses.
	ErrFabricNotFound = errors.New("store: fabric not found")
	// ErrFabricExhausted is returned when [Store.AddFabric] runs out
	// of free fabric_index slots (1..254 are all taken).
	ErrFabricExhausted = errors.New("store: fabric index exhausted")
	// ErrIdentityNotFound is returned when [Store.GetIdentity] misses.
	ErrIdentityNotFound = errors.New("store: identity not found")
	// ErrGroupKeySetNotFound is returned when a (fabric, group_key_set_id)
	// lookup misses.
	ErrGroupKeySetNotFound = errors.New("store: group key set not found")
)

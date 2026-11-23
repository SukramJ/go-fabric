// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package sealseam carries the one-way seams through which the controller
// reaches the seal of the two stores it keeps on one database — store's
// and its node registry's (controller/client/sqlstore) — where one
// operation must span both: the factory reset drops the registry's seal
// mark once the registry is empty, and controller.Reseal moves both
// stores to a new sealer in one transaction (decisions 194 and 195).
//
// Direction of the edge: packages store and controller/client/sqlstore
// install the hooks from their init; package controller calls them. The
// seam exists because each part is a step inside a transaction or a reset
// the controller composes, not an operation a host should call alone:
// exported, a step that reseals one store inside a caller's transaction
// would let a host commit one store under the new key and the other under
// the old, the state the combined Reseal exists to remove. The public
// operations stay store.Reseal and sqlstore.Reseal, each whole.
//
// The seam is module-internal, so it cannot become a consumer-visible
// surface. This package must not import store (store imports it to install
// the hooks): the sealer is threaded through as [Sealer], whose method set
// is store.Sealer's, so either converts to the other implicitly.
package sealseam

import (
	"context"
	"database/sql"
)

// Sealer is store.Sealer's method set.
type Sealer interface {
	Seal(plain, aad []byte) ([]byte, error)
	Open(sealed, aad []byte) ([]byte, error)
}

// RegistryDropEmptySealMark deletes the node registry's seal canary from
// db when the registry holds no row: the controller's factory reset, once
// it erased every node, as store.Store.Erase drops store's canary. A
// registry that still holds a row keeps its mark, and the call is no
// error.
var RegistryDropEmptySealMark func(ctx context.Context, db *sql.DB) error

// StoreResealTx is store.Reseal's transaction on tx without the commit and
// the scrub: the schema check, the key check on store's seal canary under
// old, a clear secret row refused, every sealed value moved from old to
// next, the canary last and the owed scrub marked. An error leaves tx for
// the caller to roll back.
var StoreResealTx func(ctx context.Context, tx *sql.Tx, old, next Sealer) error

// StoreDropScrubOwed deletes store's mark of an owed scrub once a
// store.Scrub of the file finished.
var StoreDropScrubOwed func(ctx context.Context, db *sql.DB) error

// RegistryResealTx is sqlstore.Reseal's transaction on tx without the
// commit and the scrub, as [StoreResealTx] is store's: on the node
// registry's tables and its own seal canary.
var RegistryResealTx func(ctx context.Context, tx *sql.Tx, old, next Sealer) error

// RegistryDropScrubOwed deletes the node registry's mark of an owed scrub
// once a store.Scrub of the file finished.
var RegistryDropScrubOwed func(ctx context.Context, db *sql.DB) error

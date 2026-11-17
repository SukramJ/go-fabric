// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/SukramJ/go-fabric/internal/sealseam"
	"github.com/SukramJ/go-fabric/store"
)

// Reseal moves a controller's database from the sealer old to the sealer
// next — the rotation of [Config].Sealer's key — in one transaction over
// both stores the controller keeps there: store's (the CA, the fabric, the
// resumption records, the group key sets; store.Reseal) and the node
// registry's (the kept onboarding codes and ICD keys; sqlstore.Reseal).
// Each store's seal canary is opened under old first (the key check) and
// written under next after its values; then one store.Scrub of the file
// removes the values sealed under old, with each store's mark of an owed
// scrub until it finished (decision 195). Two separate calls would leave,
// between them, a database whose one store opens only under next and the
// other only under old, which no New can open; one transaction leaves
// either the old state or the new.
//
// A wrong old key, a value that does not open under it and a secret in
// clear (New under old seals it first) are store.ErrUnseal, and leave the
// database as it was. Every error after the commit wraps
// store.ErrScrubIncomplete: the values are sealed under next, the scrub
// stays owed and the next New under next runs it again.
//
// No controller may be open on db while Reseal runs, and no other process
// may write the file: a running controller holds the old sealer. db must
// carry the schemas a New applied. There is no unseal: the way back to a
// clear database is a restore or a factory reset ([Controller.Erase]).
func Reseal(ctx context.Context, db *sql.DB, old, next store.Sealer) error {
	if old == nil || next == nil {
		return errors.New("controller: Reseal: nil Sealer")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("controller: Reseal: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := sealseam.StoreResealTx(ctx, tx, old, next); err != nil {
		return fmt.Errorf("controller: Reseal: %w", err)
	}
	if err := sealseam.RegistryResealTx(ctx, tx, old, next); err != nil {
		return fmt.Errorf("controller: Reseal: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("controller: Reseal: commit: %w", err)
	}
	if err := store.Scrub(ctx, db); err != nil {
		return fmt.Errorf("controller: Reseal: the values are sealed under the new sealer: %w", err)
	}
	if err := errors.Join(sealseam.StoreDropScrubOwed(ctx, db), sealseam.RegistryDropScrubOwed(ctx, db)); err != nil {
		// A mark that stays keeps ScrubOwed true and the next New scrubs
		// again: the same state as an unfinished scrub.
		return fmt.Errorf("controller: Reseal: the values are sealed under the new sealer, the owed scrub stays marked: %w: %w", store.ErrScrubIncomplete, err)
	}
	return nil
}

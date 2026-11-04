// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package store_test

import (
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/store"
)

// TestStoreEraseErrors: an erase that cannot begin or empty a table
// reports it; the store's contract test (storetest eraseCase) holds the
// successful erase.
func TestStoreEraseErrors(t *testing.T) {
	t.Parallel()
	t.Run("a table the schema lost", func(t *testing.T) {
		t.Parallel()
		db := openTestDB(t)
		if _, err := db.ExecContext(context.Background(), `DROP TABLE matter_icd_registrations`); err != nil {
			t.Fatal(err)
		}
		if err := store.New(db).Erase(context.Background()); err == nil {
			t.Fatal("erase succeeded without the table")
		}
	})
	t.Run("a closed database", func(t *testing.T) {
		t.Parallel()
		db := openTestDB(t)
		s := store.New(db)
		_ = db.Close()
		if err := s.Erase(context.Background()); err == nil {
			t.Fatal("erase succeeded on a closed database")
		}
	})
}

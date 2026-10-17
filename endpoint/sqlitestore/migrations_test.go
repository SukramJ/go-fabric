// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package sqlitestore_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/SukramJ/go-fabric/endpoint/sqlitestore"
)

// TestMigrationsMatchApply: the steps, applied to an empty database, give
// the schema Apply gives; Down reverts them; the fingerprint sees the
// tables — the guard store and controller/client/sqlstore hold.
func TestMigrationsMatchApply(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	empty := func() *sql.DB {
		db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "m.db")+"?"+connectionPragmas)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close() })
		return db
	}
	byList := empty()
	for _, m := range sqlitestore.Migrations() {
		if _, err := byList.ExecContext(ctx, m.Up); err != nil {
			t.Fatalf("%s up: %v", m.Version, err)
		}
	}
	byApply := openAt(t, filepath.Join(t.TempDir(), "a.db"))
	a, err := sqlitestore.SchemaFingerprint(ctx, byList)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := sqlitestore.SchemaFingerprint(ctx, byApply); a != b {
		t.Fatalf("SchemaFingerprint: migrations %s, Apply %s", a, b)
	}
	if err := sqlitestore.Apply(ctx, byList); err != nil {
		t.Fatal(err)
	}
	if after, _ := sqlitestore.SchemaFingerprint(ctx, byList); after != a {
		t.Fatal("Apply after the list moved the fingerprint")
	}
	none, _ := sqlitestore.SchemaFingerprint(ctx, empty())
	if none == a {
		t.Fatal("the fingerprint ignores the tables")
	}
	ms := sqlitestore.Migrations()
	for i := len(ms) - 1; i >= 0; i-- {
		if _, err := byList.ExecContext(ctx, ms[i].Down); err != nil {
			t.Fatalf("%s down: %v", ms[i].Version, err)
		}
	}
	if down, _ := sqlitestore.SchemaFingerprint(ctx, byList); down != none {
		t.Fatal("Down left schema behind")
	}
}

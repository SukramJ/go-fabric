// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package store_test

import (
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/store"
)

// TestSchemaCreatesEveryTableThePackageQueries is the guard on the reason
// the DDL moved out of testdata/ and into the package: a host feeds
// [store.Schema] to its own migration tool, so an embed that silently comes
// back empty — or a table dropped from the file — must fail here rather
// than at the host's first query.
//
// The table list is derived from the queries in this package, not from the
// file, so removing a CREATE TABLE does not quietly remove its assertion
// too.
func TestSchemaCreatesEveryTableThePackageQueries(t *testing.T) {
	t.Parallel()

	ddl := store.Schema()
	for _, table := range []string{
		"matter_fabrics",
		"matter_node_identities",
		"matter_group_keys",
		"matter_group_key_map",
		"matter_acl_entries",
		"matter_resumption",
		"matter_diagnostics",
		"matter_metadata",
		"matter_settings",
		"matter_persistent_subscriptions",
	} {
		if !strings.Contains(ddl, "CREATE TABLE IF NOT EXISTS "+table) {
			t.Errorf("Schema() does not create %s", table)
		}
	}

	// Endpoint identity is keyed on the host's own source identity and
	// lives behind endpoint.Store; a matter_endpoints table appearing here
	// would mean this package had started owning it.
	if strings.Contains(ddl, "CREATE TABLE IF NOT EXISTS matter_endpoints") {
		t.Error("Schema() creates matter_endpoints — that table belongs to endpoint/sqlitestore")
	}
}

// TestApplyIsIdempotent covers the boot path: [store.Apply] is documented as
// safe to call on every start, and a host that believes that only finds out
// on the second run.
func TestApplyIsIdempotent(t *testing.T) {
	t.Parallel()

	db := openTestDB(t) // applies the schema once
	if err := store.Apply(t.Context(), db); err != nil {
		t.Fatalf("second Apply: %v", err)
	}

	// The seeded singleton rows must not have been duplicated by the
	// second pass either — INSERT OR IGNORE, not INSERT.
	var diagnostics int
	if err := db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM matter_diagnostics`).Scan(&diagnostics); err != nil {
		t.Fatalf("count matter_diagnostics: %v", err)
	}
	if diagnostics != 1 {
		t.Errorf("matter_diagnostics holds %d rows after two Apply calls, want the single seeded row", diagnostics)
	}
}

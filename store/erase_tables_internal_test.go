// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package store

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
)

// TestErasedTablesAreTheSchemasTables holds the hand-kept erasedTables to
// the schema: every matter_* table a freshly applied schema creates is
// erased, and nothing else is listed. A table a later schema version adds
// without adding it to the list would otherwise survive the factory reset
// unnoticed (review II, R2-P1-misc-02).
func TestErasedTablesAreTheSchemasTables(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "matter.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	rows, err := db.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name LIKE 'matter\_%' ESCAPE '\'`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var schema []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		schema = append(schema, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	listed := slices.Clone(erasedTables)
	slices.Sort(schema)
	slices.Sort(listed)
	if !slices.Equal(schema, listed) {
		t.Errorf("erasedTables %v, the schema's matter_* tables %v", listed, schema)
	}
}

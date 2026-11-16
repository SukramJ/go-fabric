// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package store_test

import (
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/store"
)

// timeColumns are the columns whose value is the time a row was written;
// the comparison below names them, it does not compare them.
var timeColumns = map[string]bool{"updated_at": true, "created_at": true, "last_used_at": true}

// tableRows returns every matter_* table's rows, each as its columns'
// values in text form, a time column as "<time>".
func tableRows(t *testing.T, db *sql.DB) map[string][]string {
	t.Helper()
	ctx := t.Context()
	names, err := db.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name LIKE 'matter\_%' ESCAPE '\' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for names.Next() {
		var n string
		if err := names.Scan(&n); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, n)
	}
	_ = names.Close()
	out := map[string][]string{}
	for _, table := range tables {
		rows, err := db.QueryContext(ctx, `SELECT * FROM `+table+` ORDER BY 1`)
		if err != nil {
			t.Fatal(err)
		}
		cols, _ := rows.Columns()
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			parts := make([]string, len(cols))
			for i, c := range cols {
				parts[i] = c + "=" + fmt.Sprint(vals[i])
				if timeColumns[c] {
					parts[i] = c + "=<time>"
				}
			}
			out[table] = append(out[table], strings.Join(parts, " "))
		}
		_ = rows.Close()
	}
	return out
}

// TestEraseLeavesWhatAFreshSchemaHolds (R2-P6-store-0610): after Erase
// every matter_* table holds what a freshly applied schema holds, row for
// row — the seeded rows (the next fabric index, the diagnostics singleton)
// included, the time columns aside.
func TestEraseLeavesWhatAFreshSchemaHolds(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	fresh := tableRows(t, openTestDB(t))
	db := openTestDB(t)
	s := store.New(db)
	writeSecrets(t, s)
	if _, err := db.ExecContext(ctx, `UPDATE matter_diagnostics SET reboot_count = 7`); err != nil {
		t.Fatal(err)
	}
	if err := s.Erase(ctx); err != nil {
		t.Fatal(err)
	}
	if got := tableRows(t, db); !reflect.DeepEqual(got, fresh) {
		t.Errorf("after Erase:\n%v\nfresh schema:\n%v", got, fresh)
	}
}

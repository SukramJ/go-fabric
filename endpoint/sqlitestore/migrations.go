// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package sqlitestore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migration is one versioned step of the matter_endpoints schema, for a
// host that runs its own migration tool and cannot call [Apply] — the
// shape of store.Migration, declared here because that type's
// documentation and pin are the device store's.
type Migration struct {
	// Version names the step: "v1", "v2", … in order; never reused, and a
	// released step's text never changes.
	Version string
	// Up moves a database from the previous step to this one.
	Up string
	// Down reverts Up.
	Down string
}

// migrationVersions are the steps, in order:
//
//   - v1 — v0.1.0: matter_endpoints and matter_endpoint_allocation.
var migrationVersions = []string{"v1"}

// Migrations returns the schema's steps in order. Applied one after the
// other to an empty database they produce the schema [Apply] produces —
// the same [SchemaFingerprint] — which TestMigrationsMatchApply holds. A
// host embeds each step as one migration of its own tool and pins
// len(Migrations()) and the last Version, so the next release's step fails
// that pin at the dependency bump rather than at a live write.
func Migrations() []Migration {
	out := make([]Migration, 0, len(migrationVersions))
	for _, v := range migrationVersions {
		out = append(out, Migration{Version: v, Up: mustMigrationFile(v + ".up.sql"), Down: mustMigrationFile(v + ".down.sql")})
	}
	return out
}

func mustMigrationFile(name string) string {
	b, err := migrationFiles.ReadFile("migrations/" + name)
	if err != nil {
		panic(fmt.Sprintf("sqlitestore: embedded migration %s: %v", name, err))
	}
	return string(b)
}

var schemaObjectRE = regexp.MustCompile(`(?m)^CREATE (TABLE|(?:UNIQUE )?INDEX) IF NOT EXISTS (\w+)`)

// schemaObjects are the tables and indexes [Schema] declares, read from
// the DDL itself.
var schemaObjects = sync.OnceValues(func() (tables, indexes map[string]bool) {
	tables, indexes = map[string]bool{}, map[string]bool{}
	for _, m := range schemaObjectRE.FindAllStringSubmatch(schemaDDL, -1) {
		if m[1] == "TABLE" {
			tables[m[2]] = true
		} else {
			indexes[m[2]] = true
		}
	}
	return tables, indexes
})

// SchemaFingerprint returns the SHA-256, hex-encoded, of db's
// matter_endpoints schema: each table [Schema] creates, each index it
// declares and the automatic indexes of the tables' keys, as sqlite_schema
// records them (type, name, table and the CREATE statement with identifier
// quotes dropped and whitespace collapsed) — the method of
// store.SchemaFingerprint. Rows and other tables do not count.
func SchemaFingerprint(ctx context.Context, db *sql.DB) (string, error) {
	rs, err := db.QueryContext(ctx, `SELECT type, name, tbl_name, COALESCE(sql, '') FROM sqlite_schema`)
	if err != nil {
		return "", fmt.Errorf("sqlitestore: read schema: %w", err)
	}
	defer func() { _ = rs.Close() }()
	tables, indexes := schemaObjects()
	var lines []string
	for rs.Next() {
		var typ, name, tbl, ddl string
		if err := rs.Scan(&typ, &name, &tbl, &ddl); err != nil {
			return "", fmt.Errorf("sqlitestore: read schema: %w", err)
		}
		ours := typ == "table" && tables[name] ||
			typ == "index" && (indexes[name] || tables[tbl] && strings.HasPrefix(name, "sqlite_autoindex_"+tbl+"_"))
		if ours {
			lines = append(lines, typ+"|"+name+"|"+tbl+"|"+normaliseDDL(ddl))
		}
	}
	if err := rs.Err(); err != nil {
		return "", fmt.Errorf("sqlitestore: read schema: %w", err)
	}
	slices.Sort(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:]), nil
}

// normaliseDDL drops identifier quotes and collapses whitespace, with none
// left beside a parenthesis or comma (store's normaliseDDL).
func normaliseDDL(s string) string {
	s = strings.ReplaceAll(s, `"`, "")
	s = strings.Join(strings.Fields(s), " ")
	for _, p := range []string{"(", ")", ","} {
		s = strings.ReplaceAll(s, " "+p, p)
		s = strings.ReplaceAll(s, p+" ", p)
	}
	return s
}

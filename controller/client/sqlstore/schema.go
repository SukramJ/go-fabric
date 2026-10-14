// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package sqlstore

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

// schemaDDL is the DDL this package's queries are written against.
//
//go:embed schema.sql
var schemaDDL string

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Schema returns the DDL of the matter_client_* tables as one idempotent
// SQL script — store.Schema's counterpart for the controller's client
// nodes. A host with its own migration tool feeds it through that tool; a
// host without one calls [Apply].
func Schema() string { return schemaDDL }

// Apply executes [Schema] against db, then [Upgrade]. Every statement is
// idempotent, so it is meant to run on every boot. It neither opens the
// database nor sets pragmas.
func Apply(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, schemaDDL); err != nil {
		return fmt.Errorf("sqlstore: apply schema: %w", err)
	}
	return Upgrade(ctx, db)
}

// Upgrade rewrites what an idempotent CREATE TABLE cannot reach in a
// database an earlier release created. The first release has no such
// step; a host that runs [Schema] through its own tool still calls it
// after, so a later step arrives with the dependency bump.
func Upgrade(context.Context, *sql.DB) error { return nil }

// Migration is one versioned step of the matter_client_* schema, for a
// host that runs its own migration tool — the shape of store.Migration,
// declared here because that type's documentation and pin are the device
// tables'.
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
//   - v1 — matter_client_values and matter_client_versions (brief AL §8).
var migrationVersions = []string{"v1"}

// Migrations returns the schema's steps in order. Applied one after the
// other to an empty database they produce the schema [Apply] produces —
// the same [SchemaFingerprint] — which TestMigrationsMatchApply holds.
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
		panic(fmt.Sprintf("sqlstore: embedded migration %s: %v", name, err))
	}
	return string(b)
}

var schemaTableRE = regexp.MustCompile(`(?m)^CREATE TABLE IF NOT EXISTS (\w+)`)

// schemaTables are the tables [Schema] declares, read from the DDL.
var schemaTables = sync.OnceValue(func() map[string]bool {
	out := map[string]bool{}
	for _, m := range schemaTableRE.FindAllStringSubmatch(schemaDDL, -1) {
		out[m[1]] = true
	}
	return out
})

// SchemaFingerprint returns the SHA-256, hex-encoded, of db's
// matter_client_* schema: each table [Schema] creates and the automatic
// indexes of its keys, as sqlite_schema records them (type, name, table
// and the CREATE statement with identifier quotes dropped and whitespace
// collapsed) — the method of store.SchemaFingerprint. Rows and other
// tables do not count.
func SchemaFingerprint(ctx context.Context, db *sql.DB) (string, error) {
	rs, err := db.QueryContext(ctx, `SELECT type, name, tbl_name, COALESCE(sql, '') FROM sqlite_schema`)
	if err != nil {
		return "", fmt.Errorf("sqlstore: read schema: %w", err)
	}
	defer func() { _ = rs.Close() }()
	tables := schemaTables()
	var lines []string
	for rs.Next() {
		var typ, name, tbl, ddl string
		if err := rs.Scan(&typ, &name, &tbl, &ddl); err != nil {
			return "", fmt.Errorf("sqlstore: read schema: %w", err)
		}
		ours := typ == "table" && tables[name] || typ == "index" && tables[tbl] && strings.HasPrefix(name, "sqlite_autoindex_"+tbl+"_")
		if ours {
			lines = append(lines, typ+"|"+name+"|"+tbl+"|"+normaliseDDL(ddl))
		}
	}
	if err := rs.Err(); err != nil {
		return "", fmt.Errorf("sqlstore: read schema: %w", err)
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

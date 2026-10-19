// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package store

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

// Migration is one versioned step of the matter_* schema, for a host that
// runs its own migration tool and cannot call [Apply] (issue #62).
//
// Up and Down are SQL scripts of one or more statements. A host embeds
// each step as one migration of its own tool, in [Migrations] order, and
// pins len(Migrations()) and the last Version: the next release's step
// then fails that pin at the dependency bump rather than at a live write.
type Migration struct {
	// Version names the step: "v1", "v2", … in order. It is never reused
	// and a released step's text never changes.
	Version string
	// Up moves a database from the previous step to this one.
	Up string
	// Down reverts Up. It is DROP TABLE for an additive step and the
	// reverse rebuild for a rebuild. v3's Down is lossy: it restores the
	// foreign key from matter_group_key_map into matter_group_keys, so a
	// GroupKeyMap entry naming a key set that does not exist is dropped
	// rather than failing the step.
	Down string
}

// migrationVersions are the steps, in order, with the release that
// introduced each one (the CHANGELOG's store entries and the release
// tags' store/schema.sql):
//
//   - v1 — v0.1.0: the DDL v0.1.0 shipped; matter_group_key_map with its
//     foreign key into matter_group_keys.
//   - v2 — v0.2.0: matter_group_table, matter_groupcast_groups,
//     matter_server_subscriptions (and its fabric index).
//   - v3 — v0.2.0: the matter_group_key_map rebuild without that foreign
//     key ([Upgrade]'s step).
//   - v4 — v0.3.0: matter_ota_default_providers.
//   - v5 — v0.8.3: matter_thermostat_presets.
var migrationVersions = []string{"v1", "v2", "v3", "v4", "v5"}

// Migrations returns the schema's steps in order. Applied to an empty
// database one after the other, they produce the schema [Apply] produces —
// the same [SchemaFingerprint] — which TestMigrationsMatchApply holds.
// [Schema] stays the flattened form of the list, and [Apply] / [Upgrade]
// keep working as before.
func Migrations() []Migration {
	out := make([]Migration, 0, len(migrationVersions))
	for _, v := range migrationVersions {
		out = append(out, Migration{
			Version: v,
			Up:      mustMigrationFile(v + ".up.sql"),
			Down:    mustMigrationFile(v + ".down.sql"),
		})
	}
	return out
}

func mustMigrationFile(name string) string {
	b, err := migrationFiles.ReadFile("migrations/" + name)
	if err != nil {
		panic(fmt.Sprintf("store: embedded migration %s: %v", name, err))
	}
	return string(b)
}

// schemaObjects are the tables and indexes [Schema] declares, by name,
// read from the embedded DDL itself so the fingerprint follows the file
// with no second list to keep. [SchemaFingerprint] hashes these only: a
// host's own tables and its own indexes on these tables do not move it.
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

var schemaObjectRE = regexp.MustCompile(`(?m)^CREATE (TABLE|(?:UNIQUE )?INDEX) IF NOT EXISTS (\w+)`)

// SchemaFingerprint returns the SHA-256, hex-encoded, of db's matter_*
// schema: every table [Schema] creates, every index its DDL declares by
// name and the automatic indexes SQLite derives from those tables' own
// PRIMARY KEY / UNIQUE constraints, as sqlite_schema records them (type, name, table, and the CREATE statement
// normalised — identifier quotes dropped, whitespace collapsed — so a table
// rebuilt and renamed hashes like one created under its name). Rows and
// other tables do not count.
//
// After [Apply] it is the release's value, recorded in the CHANGELOG; a
// host that carries the schema through its own migrations compares its
// database's fingerprint with that value to know its tables match this
// release's at column and constraint level, not only by name. An index a
// host adds on one of these tables, another table and the rows do not
// count.
func SchemaFingerprint(ctx context.Context, db *sql.DB) (string, error) {
	rows, err := schemaRows(ctx, db)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(strings.Join(rows, "\n")))
	return hex.EncodeToString(sum[:]), nil
}

// schemaRows reads the matter_* part of sqlite_schema as sorted
// "type|name|tbl_name|normalised sql" lines.
func schemaRows(ctx context.Context, db *sql.DB) ([]string, error) {
	rs, err := db.QueryContext(ctx, `SELECT type, name, tbl_name, COALESCE(sql, '') FROM sqlite_schema`)
	if err != nil {
		return nil, fmt.Errorf("matter store: read schema: %w", err)
	}
	defer func() { _ = rs.Close() }()
	var out []string
	for rs.Next() {
		var typ, name, tbl, ddl string
		if err := rs.Scan(&typ, &name, &tbl, &ddl); err != nil {
			return nil, fmt.Errorf("matter store: read schema: %w", err)
		}
		if !inSchema(typ, name, tbl) {
			continue
		}
		out = append(out, typ+"|"+name+"|"+tbl+"|"+normaliseDDL(ddl))
	}
	if err := rs.Err(); err != nil {
		return nil, fmt.Errorf("matter store: read schema: %w", err)
	}
	slices.Sort(out)
	return out, nil
}

// inSchema reports whether a sqlite_schema row belongs to the module's
// DDL: one of its tables, one of its named indexes, or an automatic index
// of one of its tables.
func inSchema(typ, name, tbl string) bool {
	tables, indexes := schemaObjects()
	switch typ {
	case "table":
		return tables[name]
	case "index":
		return indexes[name] || tables[tbl] && strings.HasPrefix(name, "sqlite_autoindex_"+tbl+"_")
	}
	return false
}

// normaliseDDL drops identifier quotes and collapses whitespace, with none
// left beside a parenthesis or comma. SQLite keeps a CREATE statement's
// text as written, except that ALTER TABLE … RENAME rewrites the name
// quoted — so the same table reads differently created and rebuilt.
func normaliseDDL(s string) string {
	s = strings.ReplaceAll(s, `"`, "")
	s = strings.Join(strings.Fields(s), " ")
	for _, p := range []string{"(", ")", ","} {
		s = strings.ReplaceAll(s, " "+p, p)
		s = strings.ReplaceAll(s, p+" ", p)
	}
	return s
}

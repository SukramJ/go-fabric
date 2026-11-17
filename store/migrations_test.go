// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package store_test

import (
	"bytes"
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/store"
)

// openEmptyDB opens a fresh database without the schema.
func openEmptyDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "empty.db"), walMode)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// applyUp runs the Up of every step up to and including version.
func applyUp(t *testing.T, db *sql.DB, version string) {
	t.Helper()
	for _, m := range store.Migrations() {
		if _, err := db.ExecContext(t.Context(), m.Up); err != nil {
			t.Fatalf("%s up: %v", m.Version, err)
		}
		if m.Version == version {
			return
		}
	}
	if version != "" {
		t.Fatalf("no migration %s", version)
	}
}

// schemaOf is the matter_* rows of sqlite_schema the fingerprint hashes,
// read the way a reviewer reads them: type, name and the normalised
// statement.
func schemaOf(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), `SELECT type, name, COALESCE(sql, '') FROM sqlite_schema WHERE name LIKE 'matter\_%' ESCAPE '\' OR tbl_name LIKE 'matter\_%' ESCAPE '\'`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var typ, name, ddl string
		if err := rows.Scan(&typ, &name, &ddl); err != nil {
			t.Fatal(err)
		}
		ddl = strings.ReplaceAll(ddl, `"`, "")
		ddl = strings.Join(strings.Fields(ddl), " ")
		for _, p := range []string{"(", ")", ","} {
			ddl = strings.ReplaceAll(ddl, " "+p, p)
			ddl = strings.ReplaceAll(ddl, p+" ", p)
		}
		out = append(out, typ+" "+name+" "+ddl)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	slices.Sort(out)
	return out
}

func fingerprint(t *testing.T, db *sql.DB) string {
	t.Helper()
	fp, err := store.SchemaFingerprint(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	return fp
}

// TestMigrationsMatchApply is issue #62's guard: the list applied to a
// fresh database and Apply on another leave the same sqlite_schema (name,
// type, normalised statement), so Migrations and Schema cannot drift.
func TestMigrationsMatchApply(t *testing.T) {
	t.Parallel()
	byList := openEmptyDB(t)
	applyUp(t, byList, "")
	byApply := openTestDB(t)

	list, apply := schemaOf(t, byList), schemaOf(t, byApply)
	if !slices.Equal(list, apply) {
		t.Errorf("Migrations and Apply disagree:\nmigrations:\n  %s\napply:\n  %s",
			strings.Join(list, "\n  "), strings.Join(apply, "\n  "))
	}
	if a, b := fingerprint(t, byList), fingerprint(t, byApply); a != b {
		t.Errorf("SchemaFingerprint: migrations %s, Apply %s", a, b)
	}
	// Apply on a database the list built changes nothing: a host that
	// moves from its own tool to Apply keeps its schema.
	before := fingerprint(t, byList)
	if err := store.Apply(t.Context(), byList); err != nil {
		t.Fatal(err)
	}
	if after := fingerprint(t, byList); after != before {
		t.Errorf("Apply after the list moved the fingerprint %s → %s", before, after)
	}
}

// TestMigrationsPin is the pin a host keeps: the number of steps and the
// last version. A new step moves both, here and in the host.
func TestMigrationsPin(t *testing.T) {
	t.Parallel()
	ms := store.Migrations()
	if len(ms) != 11 {
		t.Fatalf("len(Migrations()) = %d, want 11", len(ms))
	}
	if last := ms[len(ms)-1].Version; last != "v11" {
		t.Errorf("last migration = %s, want v11", last)
	}
	seen := map[string]bool{}
	for _, m := range ms {
		if seen[m.Version] || m.Up == "" || m.Down == "" {
			t.Errorf("migration %q: duplicate or empty", m.Version)
		}
		seen[m.Version] = true
	}
}

// TestSchemaFingerprintRelease pins this release's fingerprint, the value
// the CHANGELOG records for a host to compare with. A schema change moves
// it: record the new value in the CHANGELOG entry of that change.
func TestSchemaFingerprintRelease(t *testing.T) {
	t.Parallel()
	const want = "891c60c399943bef8c2d5962e2a6b7f783a119bb740b975226c893be817f47b5"
	if got := fingerprint(t, openTestDB(t)); got != want {
		t.Errorf("SchemaFingerprint after Apply = %s, want %s", got, want)
	}
}

// TestSchemaFingerprintIgnoresOtherTablesAndRows: a host's own table, its
// own index on a module table and the rows do not move the fingerprint; a
// changed column or a dropped declared index does.
func TestSchemaFingerprintIgnoresOtherTablesAndRows(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	base := fingerprint(t, db)
	for _, stmt := range []string{
		`CREATE TABLE host_things (id INTEGER PRIMARY KEY AUTOINCREMENT)`,
		`CREATE TABLE matter_endpoints (id INTEGER PRIMARY KEY)`,
		`INSERT INTO matter_settings (key, value) VALUES ('k', 'v')`,
	} {
		if _, err := db.ExecContext(t.Context(), stmt); err != nil {
			t.Fatal(err)
		}
	}
	if got := fingerprint(t, db); got != base {
		t.Errorf("fingerprint moved with host tables and rows: %s → %s", base, got)
	}
	// A host's own index on one of the module's tables is the host's.
	if _, err := db.ExecContext(t.Context(), `CREATE INDEX host_fabrics_label ON matter_fabrics(label)`); err != nil {
		t.Fatal(err)
	}
	if got := fingerprint(t, db); got != base {
		t.Errorf("fingerprint moved with a host index on matter_fabrics: %s → %s", base, got)
	}
	// Dropping an index the module declares does move it.
	other := openTestDB(t)
	if _, err := other.ExecContext(t.Context(), `DROP INDEX matter_acl_position`); err != nil {
		t.Fatal(err)
	}
	if got := fingerprint(t, other); got == base {
		t.Error("fingerprint did not move with a declared index dropped")
	}
	if _, err := db.ExecContext(t.Context(), `ALTER TABLE matter_settings ADD COLUMN extra TEXT`); err != nil {
		t.Fatal(err)
	}
	if got := fingerprint(t, db); got == base {
		t.Error("fingerprint did not move with a column added to a matter_* table")
	}
	_ = db.Close()
	if _, err := store.SchemaFingerprint(context.Background(), db); err == nil {
		t.Error("SchemaFingerprint on a closed database returned no error")
	}
}

// TestSchemaTablesAreTheSchemas: the fingerprint's table list is every
// table Apply creates, and Apply creates no matter_* table outside it.
func TestSchemaTablesAreTheSchemas(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	before := fingerprint(t, db)
	// Dropping any table Apply created must move the fingerprint: a table
	// missing from its list would not.
	rows, err := db.QueryContext(t.Context(), `SELECT name FROM sqlite_schema WHERE type = 'table' AND name LIKE 'matter\_%' ESCAPE '\'`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, n)
	}
	_ = rows.Close()
	if len(tables) != 20 {
		t.Errorf("Apply created %d matter_* tables, want 20: %v", len(tables), tables)
	}
	for _, table := range tables {
		other := openTestDB(t)
		if _, err := other.ExecContext(t.Context(), `PRAGMA foreign_keys = OFF; DROP TABLE `+table); err != nil {
			t.Fatal(err)
		}
		if fingerprint(t, other) == before {
			t.Errorf("dropping %s did not move the fingerprint: it is missing from the hashed tables", table)
		}
	}
}

// TestMigrationsDown: every Down reverts its Up — the whole list down
// leaves no matter_* object, and v3's Down restores the foreign key while
// keeping the entries it can.
func TestMigrationsDown(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db := openEmptyDB(t)
	ms := store.Migrations()
	fps := make([]string, 0, 1+len(ms))
	fps = append(fps, fingerprint(t, db))
	for _, m := range ms {
		if _, err := db.ExecContext(ctx, m.Up); err != nil {
			t.Fatalf("%s up: %v", m.Version, err)
		}
		fps = append(fps, fingerprint(t, db))
	}
	for i := len(ms) - 1; i >= 0; i-- {
		if _, err := db.ExecContext(ctx, ms[i].Down); err != nil {
			t.Fatalf("%s down: %v", ms[i].Version, err)
		}
		if got := fingerprint(t, db); got != fps[i] {
			t.Errorf("%s down: schema is not the one before its up", ms[i].Version)
		}
	}
	if left := schemaOf(t, db); len(left) != 0 {
		t.Errorf("objects left after every down: %v", left)
	}
}

// TestMigrationV3RebuildKeepsRows: v3 on a v2 database keeps the
// GroupKeyMap rows and drops the foreign key; its Down brings the key
// back and drops the entry no key set satisfies.
func TestMigrationV3RebuildKeepsRows(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db := openEmptyDB(t)
	applyUp(t, db, "v2")
	insertLegacyFabric(t, db, 1, 1)
	insertLegacyKeySet(t, db, 1, 5)
	if err := insertLegacyMapping(t, db, 1, 0x0101, 5); err != nil {
		t.Fatal(err)
	}
	if err := insertLegacyMapping(t, db, 1, 0x0202, 9); err == nil {
		t.Fatal("v2 accepted a mapping to a missing key set; v1 carries the foreign key")
	}
	m := store.Migrations()[2]
	if _, err := db.ExecContext(ctx, m.Up); err != nil {
		t.Fatal(err)
	}
	if err := insertLegacyMapping(t, db, 1, 0x0202, 9); err != nil {
		t.Fatalf("v3 refused a mapping to a missing key set: %v", err)
	}
	if n, _ := legacyMappings(t, db, 1); n != 2 {
		t.Fatalf("mappings after v3 = %d, want 2", n)
	}
	if _, err := db.ExecContext(ctx, m.Down); err != nil {
		t.Fatal(err)
	}
	if n, set := legacyMappings(t, db, 1); n != 1 || set != 5 {
		t.Errorf("mappings after v3 down = %d (first set %d), want only the one naming set 5", n, set)
	}
}

// TestApplyUpgradesEveryStep: a database at any step, Apply'd, has the
// release's schema — Upgrade's rebuild included for a database at v1 or
// v2, whose matter_group_key_map still carries the foreign key.
func TestApplyUpgradesEveryStep(t *testing.T) {
	t.Parallel()
	want := fingerprint(t, openTestDB(t))
	for _, m := range store.Migrations() {
		db := openEmptyDB(t)
		applyUp(t, db, m.Version)
		if err := store.Apply(t.Context(), db); err != nil {
			t.Fatalf("Apply on %s: %v", m.Version, err)
		}
		if got := fingerprint(t, db); got != want {
			t.Errorf("Apply on a database at %s: fingerprint %s, want %s", m.Version, got, want)
		}
	}
}

// TestMigrationV9KeepsResumptionRows: a resumption record stored on a v8
// database reads back after Apply with no session parameters (0 is
// absent), and an owed fabric event can be recorded; v9's Down keeps the
// record, drops its session parameters and the owed events with their
// table.
func TestMigrationV9KeepsResumptionRows(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db := openEmptyDB(t)
	applyUp(t, db, "v8")
	const fabric = `INSERT INTO matter_fabrics (fabric_index, fabric_id, node_id, root_public_key, vendor_id, compressed_id, pending)
VALUES (1, X'0000000000000001', X'0000000000000002', X'04', 65521, X'0102030405060708', 1)`
	if _, err := db.ExecContext(ctx, fabric); err != nil {
		t.Fatal(err)
	}
	id, secret := strings.Repeat("ab", 16), strings.Repeat("cd", 32)
	if _, err := db.ExecContext(ctx, `INSERT INTO matter_resumption (fabric_index, peer_node_id, resumption_id, shared_secret)
VALUES (1, X'0000000000000005', X'`+id+`', X'`+secret+`')`); err != nil {
		t.Fatal(err)
	}
	if err := store.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	s := store.New(db)
	rec, err := s.GetResumptionByPeer(ctx, 1, 5)
	if err != nil {
		t.Fatalf("the v8 record after Apply: %v", err)
	}
	if rec.SessionParameters != (store.ResumptionRecord{}).SessionParameters {
		t.Errorf("the v8 record's session parameters = %+v, want none", rec.SessionParameters)
	}
	if was, err := s.RollbackFabric(ctx, 1); err != nil || was != store.FabricPendingAdd {
		t.Fatalf("RollbackFabric = %v, %v", was, err)
	}
	if owed, err := s.OwedFabricEvents(ctx); err != nil || len(owed) != 1 {
		t.Fatalf("OwedFabricEvents = %+v, %v; want the rollback's", owed, err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO matter_fabrics (fabric_index, fabric_id, node_id, root_public_key, vendor_id, compressed_id)
VALUES (2, X'0000000000000003', X'0000000000000002', X'04', 65521, X'0102030405060708')`); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertResumption(ctx, store.ResumptionRecord{
		FabricIndex: 2, PeerNodeID: 6,
		ResumptionID: []byte(strings.Repeat("r", 16)), SharedSecret: []byte(strings.Repeat("s", 32)),
	}); err != nil {
		t.Fatal(err)
	}
	m := store.Migrations()[8]
	if m.Version != "v9" {
		t.Fatalf("step 9 is %s", m.Version)
	}
	if _, err := db.ExecContext(ctx, m.Down); err != nil {
		t.Fatalf("v9 down: %v", err)
	}
	var rows int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM matter_resumption WHERE fabric_index = 2`).Scan(&rows); err != nil || rows != 1 {
		t.Errorf("resumption rows after v9 down = %d, %v; want 1", rows, err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_schema WHERE name = 'matter_owed_fabric_events'`).Scan(&rows); err != nil || rows != 0 {
		t.Errorf("matter_owed_fabric_events after v9 down: %d, %v; want gone", rows, err)
	}
}

// TestApplyUpgradesAClearICDKey: a database at v7, v8 or v9 that holds an
// ICD key in clear — as releases before the sealing stored it — upgrades
// with Apply: the key is sealed under the store's ICD key and reads back.
func TestApplyUpgradesAClearICDKey(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"v7", "v8", "v9"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			db := openEmptyDB(t)
			applyUp(t, db, version)
			if _, err := db.ExecContext(ctx, `INSERT INTO matter_fabrics (fabric_index, fabric_id, node_id, root_public_key, vendor_id, compressed_id)
VALUES (1, X'0000000000000001', X'0000000000000002', X'04', 65521, X'0102030405060708')`); err != nil {
				t.Fatal(err)
			}
			key := bytes.Repeat([]byte{0x5A}, 16)
			if _, err := db.ExecContext(ctx, `INSERT INTO matter_icd_registrations (fabric_index, position, check_in_node_id, monitored_subject, icd_key, client_type)
VALUES (1, 0, X'0000000000000011', X'0000000000000022', ?, 0)`, key); err != nil {
				t.Fatal(err)
			}
			if err := store.Apply(ctx, db); err != nil {
				t.Fatalf("Apply on %s with a clear ICD key: %v", version, err)
			}
			var raw []byte
			if err := db.QueryRowContext(ctx, `SELECT icd_key FROM matter_icd_registrations`).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			if len(raw) == len(key) || bytes.Contains(raw, key) {
				t.Errorf("the key is still clear after Apply: %x", raw)
			}
			got, err := store.New(db).ListICDRegistrations(ctx)
			if err != nil || len(got) != 1 || !bytes.Equal(got[0].Key, key) {
				t.Errorf("ListICDRegistrations after Apply = %+v, %v; want the key", got, err)
			}
		})
	}
}

// TestMigrationV11KeepsKeyMapRows: key-map entries stored on a v10
// database read back after Apply by GroupID, as they did (position 0); a
// map written after it reads in its written order; v11's Down keeps the
// entries and drops their order.
func TestMigrationV11KeepsKeyMapRows(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db := openEmptyDB(t)
	applyUp(t, db, "v10")
	insertLegacyFabric(t, db, 1, 1)
	insertLegacyFabric(t, db, 2, 2)
	for _, g := range []uint16{0x0300, 0x0100, 0x0200} {
		if err := insertLegacyMapping(t, db, 1, g, 1); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	s := store.New(db)
	groups := func(f uint8) []uint16 {
		maps, err := s.ListGroupKeyMappings(ctx, f)
		if err != nil {
			t.Fatal(err)
		}
		out := make([]uint16, 0, len(maps))
		for _, m := range maps {
			out = append(out, m.GroupID)
		}
		return out
	}
	if got := groups(1); !slices.Equal(got, []uint16{0x0100, 0x0200, 0x0300}) {
		t.Errorf("v10 entries after Apply = %x, want by GroupID", got)
	}
	if err := s.ReplaceGroupKeyMappings(ctx, 2, []store.GroupKeyMapping{{GroupID: 0x0200, GroupKeySetID: 1}, {GroupID: 0x0100, GroupKeySetID: 1}}); err != nil {
		t.Fatal(err)
	}
	if got := groups(2); !slices.Equal(got, []uint16{0x0200, 0x0100}) {
		t.Errorf("a map written after v11 = %x, want its written order", got)
	}
	m := store.Migrations()[10]
	if m.Version != "v11" {
		t.Fatalf("step 11 is %s", m.Version)
	}
	if _, err := db.ExecContext(ctx, m.Down); err != nil {
		t.Fatalf("v11 down: %v", err)
	}
	if n, _ := legacyMappings(t, db, 2); n != 2 {
		t.Errorf("fabric 2's entries after v11 down = %d, want 2", n)
	}
}

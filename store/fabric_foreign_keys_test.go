// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package store_test

import (
	"bytes"
	"database/sql"
	"errors"
	"net/url"
	"path/filepath"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/store"
)

// openWithoutForeignKeys opens path a second time with a DSN that sets no
// foreign_keys pragma, as a host's own handle may: SQLite's default is
// OFF, so the schema's ON DELETE CASCADE does nothing on it.
func openWithoutForeignKeys(t *testing.T, path string) *sql.DB {
	t.Helper()
	dsn := url.URL{Scheme: "file", OmitHost: true, Path: path, RawQuery: "_pragma=busy_timeout(5000)"}
	db, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	var fk int
	if err := db.QueryRowContext(t.Context(), `PRAGMA foreign_keys`).Scan(&fk); err != nil || fk != 0 {
		t.Fatalf("foreign_keys on the second handle = %d, %v; want 0", fk, err)
	}
	return db
}

// seedEveryFabricTable writes one row of every fabric-owned area for f.
func seedEveryFabricTable(t *testing.T, s *store.Store, f uint8) {
	t.Helper()
	ctx := t.Context()
	for what, err := range map[string]error{
		"identity":   s.UpsertIdentity(ctx, store.IdentityRecord{FabricIndex: f, NOC: []byte{f}, PrivateKey: randomSecret(32), IPK: randomSecret(16)}),
		"acl":        s.ReplaceACL(ctx, f, []store.ACLEntry{{Privilege: store.PrivilegeView, AuthMode: store.AuthModeCASE, Subjects: []uint64{1}}}),
		"key set":    s.UpsertGroupKeySet(ctx, store.GroupKeySet{FabricIndex: f, GroupKeySetID: 1, EpochKey0: randomSecret(16), EpochStart0: 1}),
		"key map":    s.SetGroupKeyMapping(ctx, store.GroupKeyMapping{FabricIndex: f, GroupID: 1, GroupKeySetID: 1}),
		"group":      s.UpsertGroupTableEntry(ctx, store.GroupTableEntry{FabricIndex: f, GroupID: 1, Endpoints: []uint16{1}}),
		"groupcast":  s.UpsertGroupcastGroup(ctx, store.GroupcastGroup{FabricIndex: f, GroupID: 1}),
		"resumption": s.UpsertResumption(ctx, store.ResumptionRecord{FabricIndex: f, PeerNodeID: 7, ResumptionID: bytes.Repeat([]byte{f}, 16), SharedSecret: randomSecret(32)}),
		"ota":        s.ReplaceDefaultOTAProviders(ctx, f, []store.OTAProviderLocation{{ProviderNodeID: 1}}),
		"icd":        s.ReplaceICDRegistrations(ctx, f, []store.ICDRegistration{{CheckInNodeID: 1, MonitoredSubject: 1, Key: randomSecret(16)}}),
	} {
		if err != nil {
			t.Fatalf("seed %s: %v", what, err)
		}
	}
}

// fabricRowsLeft counts f's rows in every fabric-owned table.
func fabricRowsLeft(t *testing.T, db *sql.DB, f uint8) map[string]int {
	t.Helper()
	left := map[string]int{}
	for _, table := range store.FabricOwnedTables() {
		var n int
		if err := db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM `+table+` WHERE fabric_index = ?`, f).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if n > 0 {
			left[table] = n
		}
	}
	return left
}

// TestFabricRemovalWithoutForeignKeys (R2-P6-sec-0601): RemoveFabric and
// the rollback of a pending AddNOC delete every fabric-owned row
// themselves, so a handle without PRAGMA foreign_keys(1) — on which the
// schema's cascade does nothing — keeps none of a removed fabric's key
// material; the other fabric's rows stay. Apply and Upgrade refuse such a
// handle.
func TestFabricRemovalWithoutForeignKeys(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "matter.db")
	withFK, err := store.OpenSQLite(path, walMode)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = withFK.Close() })
	if err := store.Apply(ctx, withFK); err != nil {
		t.Fatal(err)
	}
	db := openWithoutForeignKeys(t, path)
	if err := store.Apply(ctx, db); !errors.Is(err, store.ErrForeignKeysOff) {
		t.Errorf("Apply on a handle without foreign keys = %v, want ErrForeignKeysOff", err)
	}
	if err := store.Upgrade(ctx, db); !errors.Is(err, store.ErrForeignKeysOff) {
		t.Errorf("Upgrade on a handle without foreign keys = %v, want ErrForeignKeysOff", err)
	}
	s := store.New(db)
	removed, err := s.AddFabric(ctx, newFabricRecord(0, 1))
	if err != nil {
		t.Fatal(err)
	}
	kept, err := s.AddFabric(ctx, newFabricRecord(0, 2))
	if err != nil {
		t.Fatal(err)
	}
	pendingRec := newFabricRecord(0, 3)
	pendingRec.Pending = store.FabricPendingAdd
	pending, err := s.AddFabric(ctx, pendingRec)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []uint8{removed, kept, pending} {
		seedEveryFabricTable(t, s, f)
	}

	if err := s.RemoveFabric(ctx, removed); err != nil {
		t.Fatal(err)
	}
	if left := fabricRowsLeft(t, db, removed); len(left) > 0 {
		t.Errorf("after RemoveFabric the fabric's rows are left: %v", left)
	}
	if was, err := s.RollbackFabric(ctx, pending); err != nil || was != store.FabricPendingAdd {
		t.Fatalf("RollbackFabric = %v, %v", was, err)
	}
	if left := fabricRowsLeft(t, db, pending); len(left) > 0 {
		t.Errorf("after the rollback of a pending add the fabric's rows are left: %v", left)
	}
	if left := fabricRowsLeft(t, db, kept); len(left) != len(store.FabricOwnedTables()) {
		t.Errorf("the other fabric's rows = %v, want one in each of %v", left, store.FabricOwnedTables())
	}
	if err := s.RemoveFabric(ctx, removed); !errors.Is(err, store.ErrFabricNotFound) {
		t.Errorf("RemoveFabric of a removed fabric = %v, want ErrFabricNotFound", err)
	}
}

// TestFabricOwnedTablesAreTheForeignKeys holds the class of R2-P6-sec-0601:
// the tables RemoveFabric and RollbackFabric delete from are exactly the
// tables whose foreign key names matter_fabrics, so a table a later schema
// step hangs off the fabric is removed with it whether or not the handle
// has foreign keys on.
func TestFabricOwnedTablesAreTheForeignKeys(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db := openTestDB(t)
	rows, err := db.QueryContext(ctx, `
SELECT m.name FROM sqlite_master m, pragma_foreign_key_list(m.name) f
WHERE m.type = 'table' AND f."table" = 'matter_fabrics' ORDER BY m.name`)
	if err != nil {
		t.Fatal(err)
	}
	var fks []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		fks = append(fks, name)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		t.Fatal(err)
	}
	owned := slices.Sorted(slices.Values(store.FabricOwnedTables()))
	if !slices.Equal(owned, fks) {
		t.Errorf("fabric-owned tables = %v; the schema's foreign keys into matter_fabrics = %v", owned, fks)
	}
}

// TestFabricRemovalRollsBackAFailedDelete: when one fabric-owned table
// cannot be deleted from, RemoveFabric and the rollback of a pending add
// fail and change nothing — the fabric row stays with its other rows.
func TestFabricRemovalRollsBackAFailedDelete(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db := openTestDB(t)
	s := store.New(db)
	idx, err := s.AddFabric(ctx, newFabricRecord(0, 1))
	if err != nil {
		t.Fatal(err)
	}
	pendingRec := newFabricRecord(0, 2)
	pendingRec.Pending = store.FabricPendingAdd
	pending, err := s.AddFabric(ctx, pendingRec)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []uint8{idx, pending} {
		if err := s.ReplaceACL(ctx, f, []store.ACLEntry{{Privilege: store.PrivilegeView, AuthMode: store.AuthModeCASE, Subjects: []uint64{1}}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `DROP TABLE matter_icd_registrations`); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveFabric(ctx, idx); err == nil {
		t.Error("RemoveFabric with a fabric-owned table gone succeeded")
	}
	if _, err := s.RollbackFabric(ctx, pending); err == nil {
		t.Error("RollbackFabric of a pending add with a fabric-owned table gone succeeded")
	}
	for _, f := range []uint8{idx, pending} {
		if _, err := s.GetFabric(ctx, f); err != nil {
			t.Errorf("fabric %d after the failed removal: %v", f, err)
		}
		if acl, err := s.ListACL(ctx, f); err != nil || len(acl) != 1 {
			t.Errorf("fabric %d's ACL after the failed removal = %d entries, %v", f, len(acl), err)
		}
	}
}

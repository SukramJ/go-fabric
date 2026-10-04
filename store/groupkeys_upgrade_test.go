// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package store_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/SukramJ/go-fabric/store"
)

// TestGroupKeyMapping_KeySetNeedNotExist pins the matter.js acceptance of a
// GroupKeyMap entry whose key set is not written yet
// (GroupKeyManagementServer.ts #validateGroupKeyMap leaves the existence
// check commented out because certification tests write the map first).
func TestGroupKeyMapping_KeySetNeedNotExist(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := store.New(openTestDB(t))
	addTestFabric(t, s, 1, 1)

	if err := s.SetGroupKeyMapping(ctx, store.GroupKeyMapping{FabricIndex: 1, GroupID: 0x0101, GroupKeySetID: 0x0042}); err != nil {
		t.Fatalf("mapping to a key set not written yet: %v", err)
	}
	got, err := s.ListGroupKeyMappings(ctx, 1)
	if err != nil || len(got) != 1 || got[0].GroupKeySetID != 0x0042 {
		t.Fatalf("ListGroupKeyMappings = %+v, %v", got, err)
	}
}

// TestRemoveGroupKeySet_DropsOnlyItsMappings: KeySetRemove removes the
// fabric's entries naming the removed set and leaves every other one
// (matter.js keySetRemove's groupKeyMap filter, core§11.2.7.4.1).
func TestRemoveGroupKeySet_DropsOnlyItsMappings(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := store.New(openTestDB(t))
	addTestFabric(t, s, 1, 1)
	addTestFabric(t, s, 2, 2)
	for _, f := range []uint8{1, 2} {
		if err := s.UpsertGroupKeySet(ctx, store.GroupKeySet{FabricIndex: f, GroupKeySetID: 7, EpochKey0: epochKey(f), EpochStart0: 1}); err != nil {
			t.Fatal(err)
		}
	}
	for _, m := range []store.GroupKeyMapping{
		{FabricIndex: 1, GroupID: 1, GroupKeySetID: 7},
		{FabricIndex: 1, GroupID: 2, GroupKeySetID: 8}, // names a set never written
		{FabricIndex: 2, GroupID: 1, GroupKeySetID: 7},
	} {
		if err := s.SetGroupKeyMapping(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RemoveGroupKeySet(ctx, 1, 7); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ListGroupKeyMappings(ctx, 1); len(got) != 1 || got[0].GroupID != 2 {
		t.Errorf("fabric 1 mappings after removing set 7 = %+v, want only group 2", got)
	}
	if got, _ := s.ListGroupKeyMappings(ctx, 2); len(got) != 1 {
		t.Errorf("fabric 2 lost its mapping: %+v", got)
	}
	if err := s.RemoveGroupKeysByFabric(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ListGroupKeyMappings(ctx, 1); len(got) != 0 {
		t.Errorf("RemoveGroupKeysByFabric left mappings %+v", got)
	}
}

// legacyGroupKeyMapDDL is matter_group_key_map as releases before the
// upgrade created it, with the foreign key into matter_group_keys.
const legacyGroupKeyMapDDL = `
CREATE TABLE matter_group_key_map (
    fabric_index        INTEGER NOT NULL,
    group_id            INTEGER NOT NULL CHECK(group_id BETWEEN 0 AND 65535),
    group_key_set_id    INTEGER NOT NULL,
    PRIMARY KEY(fabric_index, group_id),
    FOREIGN KEY(fabric_index) REFERENCES matter_fabrics(fabric_index) ON DELETE CASCADE,
    FOREIGN KEY(fabric_index, group_key_set_id)
        REFERENCES matter_group_keys(fabric_index, group_key_set_id) ON DELETE CASCADE
);`

// TestUpgrade_RebuildsLegacyGroupKeyMap: a database created with the old
// foreign key keeps its rows, loses the constraint, still cascades on
// fabric removal, and a second Apply changes nothing.
func TestUpgrade_RebuildsLegacyGroupKeyMap(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "legacy.db")+"?"+connectionPragmas)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	// The legacy table first: the schema's IF NOT EXISTS then leaves it
	// alone, as it does on a real database from an earlier release.
	if _, err := db.ExecContext(ctx, `
CREATE TABLE matter_fabrics (
    fabric_index    INTEGER PRIMARY KEY CHECK(fabric_index BETWEEN 1 AND 254),
    fabric_id       BLOB    NOT NULL,
    node_id         BLOB    NOT NULL,
    root_public_key BLOB    NOT NULL,
    vendor_id       INTEGER NOT NULL CHECK(vendor_id BETWEEN 0 AND 65535),
    label           TEXT    NOT NULL DEFAULT '',
    compressed_id   BLOB    NOT NULL,
    created_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    root_cert       BLOB
);
CREATE TABLE matter_group_keys (
    fabric_index        INTEGER NOT NULL,
    group_key_set_id    INTEGER NOT NULL CHECK(group_key_set_id BETWEEN 0 AND 65535),
    security_policy     INTEGER NOT NULL CHECK(security_policy BETWEEN 0 AND 1),
    epoch_key_0         BLOB    NOT NULL,
    epoch_start_0       INTEGER NOT NULL,
    epoch_key_1         BLOB,
    epoch_start_1       INTEGER,
    epoch_key_2         BLOB,
    epoch_start_2       INTEGER,
    PRIMARY KEY(fabric_index, group_key_set_id),
    FOREIGN KEY(fabric_index) REFERENCES matter_fabrics(fabric_index) ON DELETE CASCADE
);`+legacyGroupKeyMapDDL); err != nil {
		t.Fatalf("legacy DDL: %v", err)
	}
	s := store.New(db)
	addTestFabric(t, s, 1, 1)
	if err := s.UpsertGroupKeySet(ctx, store.GroupKeySet{FabricIndex: 1, GroupKeySetID: 5, EpochKey0: epochKey(5), EpochStart0: 1}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetGroupKeyMapping(ctx, store.GroupKeyMapping{FabricIndex: 1, GroupID: 0x0101, GroupKeySetID: 5}); err != nil {
		t.Fatal(err)
	}
	// The defect the upgrade removes: the legacy table refuses a mapping
	// to a key set that does not exist.
	if err := s.SetGroupKeyMapping(ctx, store.GroupKeyMapping{FabricIndex: 1, GroupID: 0x0202, GroupKeySetID: 9}); err == nil {
		t.Fatal("legacy table accepted a mapping to a missing key set — the fixture does not reproduce the old schema")
	}

	for range 2 { // the second pass must be a no-op
		if err := store.Apply(ctx, db); err != nil {
			t.Fatalf("Apply: %v", err)
		}
	}
	var fks int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_foreign_key_list('matter_group_key_map') WHERE "table" = 'matter_group_keys'`).Scan(&fks); err != nil {
		t.Fatal(err)
	}
	if fks != 0 {
		t.Fatalf("matter_group_key_map still references matter_group_keys (%d foreign key rows)", fks)
	}
	if got, _ := s.ListGroupKeyMappings(ctx, 1); len(got) != 1 || got[0].GroupID != 0x0101 || got[0].GroupKeySetID != 5 {
		t.Fatalf("rows after the rebuild = %+v", got)
	}
	if err := s.SetGroupKeyMapping(ctx, store.GroupKeyMapping{FabricIndex: 1, GroupID: 0x0202, GroupKeySetID: 9}); err != nil {
		t.Fatalf("mapping to a missing key set after the upgrade: %v", err)
	}
	if err := s.RemoveFabric(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ListGroupKeyMappings(ctx, 1); len(got) != 0 {
		t.Errorf("fabric removal no longer cascades into the rebuilt table: %+v", got)
	}
}

// TestUpgrade_Errors covers the failure branches: a closed database, and a
// rebuild that cannot create its scratch table (the transaction rolls back
// and the legacy table survives untouched).
func TestUpgrade_Errors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	closed := openTestDB(t)
	_ = closed.Close()
	if err := store.Upgrade(ctx, closed); err == nil {
		t.Error("Upgrade on a closed database: want error")
	}

	db := openTestDB(t)
	if _, err := db.ExecContext(ctx, `DROP TABLE matter_group_key_map;`+legacyGroupKeyMapDDL+`
CREATE TABLE matter_group_key_map_rebuild (x INTEGER);`); err != nil {
		t.Fatal(err)
	}
	if err := store.Upgrade(ctx, db); err == nil {
		t.Fatal("Upgrade with an occupied scratch table name: want error")
	}
	var fks int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_foreign_key_list('matter_group_key_map') WHERE "table" = 'matter_group_keys'`).Scan(&fks); err != nil {
		t.Fatal(err)
	}
	if fks == 0 {
		t.Error("a failed rebuild left a half-migrated matter_group_key_map behind")
	}
}

// TestRemoveGroupKeys_StatementFailure covers the in-transaction failure
// branch of the two removals: without the key map table the first DELETE
// fails and nothing is committed.
func TestRemoveGroupKeys_StatementFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openTestDB(t)
	s := store.New(db)
	addTestFabric(t, s, 1, 1)
	if err := s.UpsertGroupKeySet(ctx, store.GroupKeySet{FabricIndex: 1, GroupKeySetID: 3, EpochKey0: epochKey(3), EpochStart0: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `DROP TABLE matter_group_key_map`); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveGroupKeySet(ctx, 1, 3); err == nil {
		t.Error("RemoveGroupKeySet without the key map table: want error")
	}
	if err := s.RemoveGroupKeysByFabric(ctx, 1); err == nil {
		t.Error("RemoveGroupKeysByFabric without the key map table: want error")
	}
	if _, err := s.GetGroupKeySet(ctx, 1, 3); err != nil {
		t.Errorf("the failed removal committed the key set deletion: %v", err)
	}
	closed := closedStore(t)
	if err := closed.RemoveGroupKeysByFabric(ctx, 1); err == nil {
		t.Error("RemoveGroupKeysByFabric on a closed database: want error")
	}
}

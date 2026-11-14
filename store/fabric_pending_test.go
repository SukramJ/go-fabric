// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package store_test

import (
	"bytes"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/store"
)

// insertLegacyFabric inserts newFabricRecord(fabricIndex, seed) with the
// columns of migration v1 only, for a test that builds a database of an
// earlier step: the Store's own queries name the columns of the current
// schema, which such a database does not have yet.
func insertLegacyFabric(t *testing.T, db *sql.DB, fabricIndex uint8, seed byte) {
	t.Helper()
	rec := newFabricRecord(fabricIndex, seed)
	if _, err := db.ExecContext(t.Context(), `
INSERT INTO matter_fabrics (fabric_index, fabric_id, node_id, root_public_key, vendor_id, label, compressed_id)
VALUES (?, ?, ?, ?, ?, ?, ?)`, fabricIndex, binary.BigEndian.AppendUint64(nil, rec.FabricID), binary.BigEndian.AppendUint64(nil, rec.NodeID),
		rec.RootPublicKey, rec.VendorID, rec.Label, rec.CompressedID[:]); err != nil {
		t.Fatalf("insert legacy fabric %d: %v", fabricIndex, err)
	}
}

// settingsOf is every row of matter_settings as "key=value", sorted.
func settingsOf(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), `SELECT key, value FROM matter_settings`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			t.Fatal(err)
		}
		out = append(out, k+"="+v)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	slices.Sort(out)
	return out
}

// TestUpgradeMovesVIDSettingsIntoFabrics: a database built by the v1..v7
// steps, holding the VID verification settings a release before v8 wrote
// (cluster/core/vid_verification.go:87-94: lower-case hex, an empty value
// for none), ends after Apply with the data in its fabrics' columns and the
// keys gone; keys of another shape stay; a second Upgrade changes nothing;
// and a key a release before v8 wrote again afterwards wins over the column.
func TestUpgradeMovesVIDSettingsIntoFabrics(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db := openEmptyDB(t)
	applyUp(t, db, "v7")
	insertLegacyFabric(t, db, 1, 1)
	insertLegacyFabric(t, db, 2, 2)
	statement := bytes.Repeat([]byte{0xAB}, 85)
	settings := map[string]string{
		"matter.fabric.1.vid.statement":  hex.EncodeToString(statement),
		"matter.fabric.1.vid.vvsc":       "0102",
		"matter.fabric.2.vid.statement":  "",
		"matter.fabric.2.vid.vvsc":       "",
		"matter.fabric.9.vid.statement":  "abcd", // no fabric 9: deleted, not moved
		"matter.fabric.01.vid.statement": "ff",   // not a key the caller builds: stays
		"matter.fabric.1.vid.other":      "ff",   // likewise
		store.SettingNodeLabel:           "hall",
	}
	for k, v := range settings {
		if _, err := db.ExecContext(ctx, `INSERT INTO matter_settings (key, value) VALUES (?, ?)`, k, v); err != nil {
			t.Fatal(err)
		}
	}

	if err := store.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	s := store.New(db)
	one, err := s.GetFabric(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(one.VIDVerificationStatement, statement) || !bytes.Equal(one.VVSC, []byte{1, 2}) {
		t.Errorf("fabric 1 after the upgrade: statement %x, VVSC %x", one.VIDVerificationStatement, one.VVSC)
	}
	if one.Pending != store.FabricCommitted {
		t.Errorf("fabric 1 after the upgrade is %v, want committed", one.Pending)
	}
	two, err := s.GetFabric(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if two.VIDVerificationStatement != nil || two.VVSC != nil {
		t.Errorf("fabric 2 (empty settings) after the upgrade: statement %x, VVSC %x; want none", two.VIDVerificationStatement, two.VVSC)
	}
	want := []string{"basic_information.node_label=hall", "matter.fabric.01.vid.statement=ff", "matter.fabric.1.vid.other=ff"}
	if got := settingsOf(t, db); !slices.Equal(got, want) {
		t.Errorf("settings after the upgrade = %v, want %v", got, want)
	}

	fp, settingsBefore := fingerprint(t, db), settingsOf(t, db)
	if err := store.Upgrade(ctx, db); err != nil {
		t.Fatal(err)
	}
	if fingerprint(t, db) != fp || !slices.Equal(settingsOf(t, db), settingsBefore) {
		t.Error("a second Upgrade changed the schema or the settings")
	}
	if again, _ := s.GetFabric(ctx, 1); !bytes.Equal(again.VIDVerificationStatement, statement) || !bytes.Equal(again.VVSC, []byte{1, 2}) {
		t.Error("a second Upgrade changed fabric 1's VID data")
	}

	if err := s.SetSetting(ctx, "matter.fabric.1.vid.vvsc", ""); err != nil {
		t.Fatal(err)
	}
	if err := store.Upgrade(ctx, db); err != nil {
		t.Fatal(err)
	}
	if again, _ := s.GetFabric(ctx, 1); again.VVSC != nil || !bytes.Equal(again.VIDVerificationStatement, statement) {
		t.Errorf("a later empty vvsc key did not clear the column: statement %x, VVSC %x", again.VIDVerificationStatement, again.VVSC)
	}
}

// TestPendingFabricsSurviveReopen: every commit state, and an update's
// prior values, are on disk: after closing the database and opening it
// again (Apply included, as at a node's start) the rows read as before, the
// update's rollback brings the prior credentials back and the add's
// rollback deletes its row.
func TestPendingFabricsSurviveReopen(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "matter.db")
	open := func() (*sql.DB, *store.Store) {
		t.Helper()
		db, err := store.OpenSQLite(path, walMode)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Apply(ctx, db); err != nil {
			t.Fatal(err)
		}
		return db, store.New(db)
	}
	identity := func(f uint8) store.IdentityRecord {
		return store.IdentityRecord{FabricIndex: f, NOC: []byte{1, f}, ICAC: []byte{2, f}, PrivateKey: bytes.Repeat([]byte{f}, 32), IPK: bytes.Repeat([]byte{3}, 16)}
	}

	db, s := open()
	add := func(seed byte, state store.FabricPending) uint8 {
		rec := newFabricRecord(0, seed)
		rec.Pending = state
		rec.VIDVerificationStatement = []byte{seed}
		idx, err := s.AddFabric(ctx, rec)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.UpsertIdentity(ctx, identity(idx)); err != nil {
			t.Fatal(err)
		}
		return idx
	}
	committed := add(1, store.FabricCommitted)
	pendingAdd := add(2, store.FabricPendingAdd)
	updated := add(3, store.FabricCommitted)
	beforeFabric, err := s.GetFabric(ctx, updated)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateFabricCredentials(ctx, updated, store.FabricCredentials{NOC: []byte{9}, PrivateKey: []byte{9}, NodeID: 9}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, s = open()
	t.Cleanup(func() { _ = db.Close() })
	for idx, want := range map[uint8]store.FabricPending{committed: store.FabricCommitted, pendingAdd: store.FabricPendingAdd, updated: store.FabricPendingUpdate} {
		if got, err := s.GetFabric(ctx, idx); err != nil || got.Pending != want {
			t.Errorf("fabric %d after reopening: %v, %v; want %v", idx, got.Pending, err, want)
		}
	}
	if state, err := s.RollbackFabric(ctx, updated); err != nil || state != store.FabricPendingUpdate {
		t.Fatalf("RollbackFabric of the update after reopening = %v, %v", state, err)
	}
	if got, err := s.GetFabric(ctx, updated); err != nil || !reflect.DeepEqual(got, beforeFabric) {
		t.Errorf("the updated fabric after its rollback = %+v, %v; want %+v", got, err, beforeFabric)
	}
	if got, err := s.GetIdentity(ctx, updated); err != nil || !reflect.DeepEqual(got, identity(updated)) {
		t.Errorf("the updated identity after its rollback = %+v, %v; want %+v", got, err, identity(updated))
	}
	if state, err := s.RollbackFabric(ctx, pendingAdd); err != nil || state != store.FabricPendingAdd {
		t.Fatalf("RollbackFabric of the add after reopening = %v, %v", state, err)
	}
	if _, err := s.GetFabric(ctx, pendingAdd); !errors.Is(err, store.ErrFabricNotFound) {
		t.Errorf("the rolled-back add after reopening: %v, want ErrFabricNotFound", err)
	}
}

// TestCommitAndRollbackErasePriorColumns is R2-P2-dr-noc-06: after
// CommitFabric and after RollbackFabric of an update, no prior value is
// left in the row — not the replaced NOC, ICAC or operational private key,
// not the node id, vendor id or VID data. A leftover is unobservable
// through the port, so the columns are read.
func TestCommitAndRollbackErasePriorColumns(t *testing.T) {
	t.Parallel()
	for _, end := range []string{"commit", "rollback"} {
		t.Run(end, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "matter.db"), walMode)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			if err := store.Apply(ctx, db); err != nil {
				t.Fatal(err)
			}
			s := store.New(db)
			rec := newFabricRecord(0, 1)
			rec.VIDVerificationStatement, rec.VVSC = []byte{4}, []byte{5}
			idx, err := s.AddFabric(ctx, rec)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.UpsertIdentity(ctx, store.IdentityRecord{FabricIndex: idx, NOC: []byte{1}, ICAC: []byte{2}, PrivateKey: bytes.Repeat([]byte{3}, 32), IPK: bytes.Repeat([]byte{6}, 16)}); err != nil {
				t.Fatal(err)
			}
			if err := s.UpdateFabricCredentials(ctx, idx, store.FabricCredentials{NOC: []byte{9}, ICAC: []byte{9}, PrivateKey: bytes.Repeat([]byte{9}, 32), NodeID: 9}); err != nil {
				t.Fatal(err)
			}
			if got := priorColumns(t, db, idx); got != allPriorColumns {
				t.Fatalf("prior columns set by the update = %#b, want %#b: the read does not see them", got, allPriorColumns)
			}
			if end == "commit" {
				err = s.CommitFabric(ctx, idx)
			} else {
				_, err = s.RollbackFabric(ctx, idx)
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := priorColumns(t, db, idx); got != 0 {
				t.Fatalf("prior columns left after the %s = %#b (bit i: %v), want none", end, got, priorColumnNames)
			}
		})
	}
}

// priorColumnNames are the prior_* columns, identity first.
var priorColumnNames = []string{"prior_noc", "prior_icac", "prior_private_key", "prior_node_id", "prior_vendor_id", "prior_vid_statement", "prior_vvsc"}

const allPriorColumns = 1<<7 - 1

// priorColumns is a bit set of the prior_* columns of fabric idx that are
// not NULL, bit i for priorColumnNames[i].
func priorColumns(t *testing.T, db *sql.DB, idx uint8) int {
	t.Helper()
	row := db.QueryRowContext(t.Context(), `
SELECT i.prior_noc IS NOT NULL, i.prior_icac IS NOT NULL, i.prior_private_key IS NOT NULL,
       f.prior_node_id IS NOT NULL, f.prior_vendor_id IS NOT NULL, f.prior_vid_statement IS NOT NULL, f.prior_vvsc IS NOT NULL
FROM matter_fabrics f JOIN matter_node_identities i ON i.fabric_index = f.fabric_index
WHERE f.fabric_index = ?`, idx)
	var set [7]bool
	if err := row.Scan(&set[0], &set[1], &set[2], &set[3], &set[4], &set[5], &set[6]); err != nil {
		t.Fatal(err)
	}
	bits := 0
	for i, b := range set {
		if b {
			bits |= 1 << i
		}
	}
	return bits
}

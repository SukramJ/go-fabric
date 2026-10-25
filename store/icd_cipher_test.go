// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package store_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/store"
)

// TestICDKeyErrors: Upgrade on a database without registrations creates
// no key; a stored key that is not 16 hex bytes fails the registration
// and the listing of a sealed key; a sealed key cut short does not open.
func TestICDKeyErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openTestDB(t)
	s := store.New(db)
	if err := store.Upgrade(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.GetSetting(ctx, store.SettingICDKey); ok {
		t.Fatal("Upgrade created a key without a registration")
	}
	idx, err := s.AddFabric(ctx, store.FabricRecord{
		FabricID: 1, NodeID: 2, RootPublicKey: uncompressedP256Fixture(1), RootCert: []byte{0x15, 0x18},
		VendorID: 0xFFF1, CompressedID: [8]byte{1, 2, 3, 4, 5, 6, 7, 8},
	})
	if err != nil {
		t.Fatal(err)
	}
	reg := []store.ICDRegistration{{CheckInNodeID: 1, MonitoredSubject: 1, Key: bytes.Repeat([]byte{1}, 16)}}
	if err := s.ReplaceICDRegistrations(ctx, idx, reg); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE matter_icd_registrations SET icd_key = substr(icd_key, 1, 20)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ListICDRegistrations(ctx); err == nil {
		t.Fatal("a sealed key cut short opened")
	}
	if err := s.SetSetting(ctx, store.SettingICDKey, "not hex"); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceICDRegistrations(ctx, idx, reg); err == nil {
		t.Fatal("a registration under a malformed key setting succeeded")
	}
	if _, err := s.ListICDRegistrations(ctx); err == nil {
		t.Fatal("a listing under a malformed key setting succeeded")
	}
	// A clear row under a malformed key setting: Upgrade cannot seal it.
	if _, err := db.ExecContext(ctx, `UPDATE matter_icd_registrations SET icd_key = ?`, bytes.Repeat([]byte{1}, 16)); err != nil {
		t.Fatal(err)
	}
	if err := store.Upgrade(ctx, db); err == nil {
		t.Fatal("Upgrade sealed under a malformed key setting")
	}
	if err := s.SetSetting(ctx, store.SettingICDKey, "abcd"); err != nil {
		t.Fatal(err)
	}
	if err := store.Upgrade(ctx, db); err == nil {
		t.Fatal("Upgrade sealed under a 2-byte key")
	}
	// A clear row Upgrade cannot read (a node id of 3 bytes) fails it.
	if _, err := db.ExecContext(ctx, `UPDATE matter_icd_registrations SET icd_key = ?, check_in_node_id = ?`, bytes.Repeat([]byte{1}, 16), []byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM matter_settings WHERE key = ?`, store.SettingICDKey); err != nil {
		t.Fatal(err)
	}
	if err := store.Upgrade(ctx, db); err == nil {
		t.Fatal("Upgrade sealed a malformed row")
	}
	// Without the table (a database before migration v7) Upgrade has
	// nothing to seal.
	if _, err := db.ExecContext(ctx, `DROP TABLE matter_icd_registrations`); err != nil {
		t.Fatal(err)
	}
	if err := store.Upgrade(ctx, db); err != nil {
		t.Fatalf("Upgrade without the table: %v", err)
	}
}

// TestICDKeysSealedAtRest: a registration's ICDToken key is stored sealed
// under the store's ICD key — the column holds neither the key nor 16
// bytes — and comes back clear; a key an earlier release stored in clear
// is read as it is and sealed by Upgrade; a sealed key moved to another
// registration does not open.
func TestICDKeysSealedAtRest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openTestDB(t)
	s := store.New(db)
	idx, err := s.AddFabric(ctx, store.FabricRecord{
		FabricID: 1, NodeID: 2, RootPublicKey: uncompressedP256Fixture(1), RootCert: []byte{0x15, 0x18},
		VendorID: 0xFFF1, CompressedID: [8]byte{1, 2, 3, 4, 5, 6, 7, 8},
	})
	if err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{0xA5}, 16)
	if err := s.ReplaceICDRegistrations(ctx, idx, []store.ICDRegistration{{CheckInNodeID: 0x11, MonitoredSubject: 0x22, Key: key}}); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err := db.QueryRowContext(ctx, `SELECT icd_key FROM matter_icd_registrations`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if len(raw) == 16 || bytes.Contains(raw, key) {
		t.Fatalf("the stored key is clear: %x", raw)
	}
	if _, ok, err := s.GetSetting(ctx, store.SettingICDKey); err != nil || !ok {
		t.Fatalf("no %s setting (%v)", store.SettingICDKey, err)
	}
	got, err := s.ListICDRegistrations(ctx)
	if err != nil || len(got) != 1 || !bytes.Equal(got[0].Key, key) {
		t.Fatalf("ListICDRegistrations %+v, %v", got, err)
	}

	// A clear key as v0.12.0 stored it: read as it is, sealed by Upgrade.
	clearKey := bytes.Repeat([]byte{0x5A}, 16)
	if _, err := db.ExecContext(ctx, `UPDATE matter_icd_registrations SET icd_key = ?`, clearKey); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ListICDRegistrations(ctx); err != nil || !bytes.Equal(got[0].Key, clearKey) {
		t.Fatalf("a clear key: %+v, %v", got, err)
	}
	if err := store.Upgrade(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT icd_key FROM matter_icd_registrations`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if len(raw) == 16 {
		t.Fatal("Upgrade left the clear key")
	}
	if got, err := s.ListICDRegistrations(ctx); err != nil || !bytes.Equal(got[0].Key, clearKey) {
		t.Fatalf("after Upgrade: %+v, %v", got, err)
	}

	// The sealed key bound to another monitored subject does not open.
	if _, err := db.ExecContext(ctx, `UPDATE matter_icd_registrations SET monitored_subject = ?`, []byte{0, 0, 0, 0, 0, 0, 0, 0x33}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ListICDRegistrations(ctx); err == nil {
		t.Fatal("a sealed key moved to another registration opened")
	}
}

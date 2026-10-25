// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package store_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/SukramJ/go-fabric/store"
)

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

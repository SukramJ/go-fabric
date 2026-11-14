// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package store_test

import (
	"context"
	"testing"

	store "github.com/SukramJ/go-fabric/store"
)

// TestSettings_GetSetting_MissReturnsNotOK verifies that GetSetting
// reports ok=false for a key that has never been written, so callers
// keep their configured default.
func TestSettings_GetSetting_MissReturnsNotOK(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := store.New(openTestDB(t))

	got, ok, err := s.GetSetting(ctx, store.SettingNodeLabel)
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if ok {
		t.Fatal("GetSetting: ok=true for a key never written")
	}
	if got != "" {
		t.Fatalf("GetSetting: value=%q, want empty on miss", got)
	}
}

// TestSettings_SetSetting_RoundTrips verifies that a value written via
// SetSetting is returned unchanged by a subsequent GetSetting.
func TestSettings_SetSetting_RoundTrips(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := store.New(openTestDB(t))

	if err := s.SetSetting(ctx, store.SettingNodeLabel, "living-room-bridge"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	got, ok, err := s.GetSetting(ctx, store.SettingNodeLabel)
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if !ok {
		t.Fatal("GetSetting: ok=false after SetSetting")
	}
	if got != "living-room-bridge" {
		t.Fatalf("GetSetting = %q, want living-room-bridge", got)
	}
}

// TestSettings_SetSetting_UpsertOverwrites verifies that a second
// SetSetting call for the same key overwrites the previously persisted
// value rather than erroring on the UNIQUE key constraint.
func TestSettings_SetSetting_UpsertOverwrites(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := store.New(openTestDB(t))

	if err := s.SetSetting(ctx, store.SettingLocation, "DE"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	if err := s.SetSetting(ctx, store.SettingLocation, "FR"); err != nil {
		t.Fatalf("SetSetting overwrite: %v", err)
	}
	got, ok, err := s.GetSetting(ctx, store.SettingLocation)
	if err != nil {
		t.Fatalf("GetSetting: %v", err)
	}
	if !ok {
		t.Fatal("GetSetting: ok=false after overwrite")
	}
	if got != "FR" {
		t.Fatalf("GetSetting = %q, want FR (overwritten value)", got)
	}
}

// TestSettings_GetMetadataCounter_MissReturnsNotOK verifies that
// GetMetadataCounter reports ok=false for a key that has never been
// written.
func TestSettings_GetMetadataCounter_MissReturnsNotOK(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := store.New(openTestDB(t))

	got, ok, err := s.GetMetadataCounter(ctx, store.MetadataKeyEventNumber)
	if err != nil {
		t.Fatalf("GetMetadataCounter: %v", err)
	}
	if ok {
		t.Fatal("GetMetadataCounter: ok=true for a key never written")
	}
	if got != 0 {
		t.Fatalf("GetMetadataCounter: value=%d, want 0 on miss", got)
	}
}

// TestSettings_SetMetadataCounter_RoundTrips verifies that a counter
// written via SetMetadataCounter is returned unchanged by a subsequent
// GetMetadataCounter.
func TestSettings_SetMetadataCounter_RoundTrips(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := store.New(openTestDB(t))

	if err := s.SetMetadataCounter(ctx, store.MetadataKeyEventNumber, 4096); err != nil {
		t.Fatalf("SetMetadataCounter: %v", err)
	}
	got, ok, err := s.GetMetadataCounter(ctx, store.MetadataKeyEventNumber)
	if err != nil {
		t.Fatalf("GetMetadataCounter: %v", err)
	}
	if !ok {
		t.Fatal("GetMetadataCounter: ok=false after SetMetadataCounter")
	}
	if got != 4096 {
		t.Fatalf("GetMetadataCounter = %d, want 4096", got)
	}
}

// TestSettings_SetMetadataCounter_UpsertOverwrites verifies that a
// second SetMetadataCounter call for the same key overwrites the
// previously persisted counter.
func TestSettings_SetMetadataCounter_UpsertOverwrites(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := store.New(openTestDB(t))

	if err := s.SetMetadataCounter(ctx, store.MetadataKeyEventNumber, 100); err != nil {
		t.Fatalf("SetMetadataCounter: %v", err)
	}
	if err := s.SetMetadataCounter(ctx, store.MetadataKeyEventNumber, 200); err != nil {
		t.Fatalf("SetMetadataCounter overwrite: %v", err)
	}
	got, ok, err := s.GetMetadataCounter(ctx, store.MetadataKeyEventNumber)
	if err != nil {
		t.Fatalf("GetMetadataCounter: %v", err)
	}
	if !ok {
		t.Fatal("GetMetadataCounter: ok=false after overwrite")
	}
	if got != 200 {
		t.Fatalf("GetMetadataCounter = %d, want 200 (overwritten value)", got)
	}
}

// TestSettings_DeleteSetting removes a written key, so GetSetting misses
// it afterwards; deleting a key that is not there is no error.
func TestSettings_DeleteSetting(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := store.New(openTestDB(t))

	if err := s.SetSetting(ctx, store.SettingNodeLabel, "kitchen"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	if err := s.DeleteSetting(ctx, store.SettingNodeLabel); err != nil {
		t.Fatalf("DeleteSetting: %v", err)
	}
	if _, ok, err := s.GetSetting(ctx, store.SettingNodeLabel); err != nil || ok {
		t.Fatalf("GetSetting after delete: ok=%v err=%v, want a miss", ok, err)
	}
	if err := s.DeleteSetting(ctx, "never-written"); err != nil {
		t.Fatalf("DeleteSetting of a missing key: %v", err)
	}
}

// TestSetSettingsAllOrNothing provokes a failure inside SetSettings'
// transaction: a trigger on matter_settings aborts the insert of one key,
// which the store writes after the others (keys in ascending order). None
// of the keys may be left behind, the one an earlier write held keeps its
// old value, and with the trigger gone the same call writes them all.
func TestSetSettingsAllOrNothing(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db := openTestDB(t)
	s := store.New(db)
	if err := s.SetSetting(ctx, "multi.a", "old"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
CREATE TRIGGER refuse_multi_z BEFORE INSERT ON matter_settings
WHEN NEW.key = 'multi.z' BEGIN SELECT RAISE(ABORT, 'refused by the test'); END`); err != nil {
		t.Fatal(err)
	}
	values := map[string]string{"multi.a": "new", "multi.b": "2", "multi.z": "26"}
	if err := s.SetSettings(ctx, values); err == nil {
		t.Fatal("SetSettings with a refused key: want error")
	}
	if v, ok, err := s.GetSetting(ctx, "multi.a"); err != nil || !ok || v != "old" {
		t.Errorf("multi.a after the failed SetSettings = %q, %v, %v; want old", v, ok, err)
	}
	for _, k := range []string{"multi.b", "multi.z"} {
		if _, ok, err := s.GetSetting(ctx, k); err != nil || ok {
			t.Errorf("%s after the failed SetSettings: ok=%v err=%v; want absent", k, ok, err)
		}
	}
	if _, err := db.ExecContext(ctx, `DROP TRIGGER refuse_multi_z`); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSettings(ctx, values); err != nil {
		t.Fatal(err)
	}
	for k, want := range values {
		if v, ok, err := s.GetSetting(ctx, k); err != nil || !ok || v != want {
			t.Errorf("GetSetting(%q) = %q, %v, %v; want %q", k, v, ok, err, want)
		}
	}
}

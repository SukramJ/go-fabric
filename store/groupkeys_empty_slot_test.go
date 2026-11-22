// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package store_test

import (
	"context"
	"testing"

	store "thingfabric.dev/go-fabric/store"
)

// TestGroupKey_EmptyOptionalSlotReadsAbsent: an empty epoch key 1 an
// earlier release stored is read as absent (start 0), as a write now
// stores it, so the set is listed and served (Fable R3-1, wave S).
func TestGroupKey_EmptyOptionalSlotReadsAbsent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := openTestDB(t)
	s := store.New(db)
	addTestFabric(t, s, 1, 1)
	if err := s.UpsertGroupKeySet(ctx, store.GroupKeySet{
		FabricIndex: 1, GroupKeySetID: 0, SecurityPolicy: store.SecurityPolicyTrustFirst,
		EpochKey0: epochKey(0x10), EpochStart0: 1,
	}); err != nil {
		t.Fatalf("UpsertGroupKeySet: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE matter_group_keys SET epoch_key_1 = X'', epoch_start_1 = 5 WHERE fabric_index = 1 AND group_key_set_id = 0`); err != nil {
		t.Fatalf("plant: %v", err)
	}
	got, err := s.GetGroupKeySet(ctx, 1, 0)
	if err != nil {
		t.Fatalf("GetGroupKeySet: %v", err)
	}
	if got.EpochKey1 != nil || got.EpochStart1 != 0 {
		t.Errorf("slot 1 = %x start %d, want absent", got.EpochKey1, got.EpochStart1)
	}
	list, err := s.ListGroupKeySets(ctx, 1)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListGroupKeySets = %d sets, %v; want the set", len(list), err)
	}
}

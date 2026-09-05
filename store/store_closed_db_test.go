// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Error-injection tests: every Store method called against a closed *sql.DB must
// return a non-nil error. closedStore opens a fresh DB, closes it immediately,
// and wraps it in a Store. All subsequent operations fail with
// "sql: database is closed", exercising every error-return branch.

package store_test

import (
	"context"
	"testing"

	mstore "github.com/SukramJ/go-fabric/store"
)

// closedStore opens a fresh DB, immediately closes it, and wraps it in a Store.
// Every method call on the returned Store will fail with "sql: database is closed".
func closedStore(t *testing.T) *mstore.Store {
	t.Helper()
	db := openTestDB(t)
	_ = db.Close() // close before returning — all subsequent ops fail
	return mstore.New(db)
}

func TestFabric_AddFabric_ClosedDB(t *testing.T) {
	t.Parallel()
	s := closedStore(t)
	_, err := s.AddFabric(context.Background(), mstore.FabricRecord{FabricIndex: 1})
	if err == nil {
		t.Error("AddFabric on closed DB: want error, got nil")
	}
}

func TestFabric_GetFabric_ClosedDB(t *testing.T) {
	t.Parallel()
	s := closedStore(t)
	_, err := s.GetFabric(context.Background(), 1)
	if err == nil {
		t.Error("GetFabric on closed DB: want error, got nil")
	}
}

func TestFabric_ListFabrics_ClosedDB(t *testing.T) {
	t.Parallel()
	s := closedStore(t)
	_, err := s.ListFabrics(context.Background())
	if err == nil {
		t.Error("ListFabrics on closed DB: want error, got nil")
	}
}

func TestFabric_UpdateFabricLabel_ClosedDB(t *testing.T) {
	t.Parallel()
	s := closedStore(t)
	err := s.UpdateFabricLabel(context.Background(), 1, "new")
	if err == nil {
		t.Error("UpdateFabricLabel on closed DB: want error, got nil")
	}
}

func TestFabric_RemoveFabric_ClosedDB(t *testing.T) {
	t.Parallel()
	s := closedStore(t)
	err := s.RemoveFabric(context.Background(), 1)
	if err == nil {
		t.Error("RemoveFabric on closed DB: want error, got nil")
	}
}

func TestACL_ListACL_ClosedDB(t *testing.T) {
	t.Parallel()
	s := closedStore(t)
	_, err := s.ListACL(context.Background(), 1)
	if err == nil {
		t.Error("ListACL on closed DB: want error, got nil")
	}
}

func TestACL_ReplaceACL_ClosedDB(t *testing.T) {
	t.Parallel()
	s := closedStore(t)
	err := s.ReplaceACL(context.Background(), 1, []mstore.ACLEntry{
		{Privilege: mstore.PrivilegeView, AuthMode: mstore.AuthModeCASE, Subjects: []uint64{1}},
	})
	if err == nil {
		t.Error("ReplaceACL on closed DB: want error, got nil")
	}
}

func TestResumption_Upsert_ClosedDB(t *testing.T) {
	t.Parallel()
	s := closedStore(t)
	err := s.UpsertResumption(context.Background(), mstore.ResumptionRecord{
		FabricIndex:  1,
		PeerNodeID:   0x1234,
		ResumptionID: make([]byte, 16),
		SharedSecret: make([]byte, 32),
	})
	if err == nil {
		t.Error("UpsertResumption on closed DB: want error, got nil")
	}
}

func TestResumption_GetByID_ClosedDB(t *testing.T) {
	t.Parallel()
	s := closedStore(t)
	_, err := s.GetResumptionByID(context.Background(), make([]byte, 16))
	if err == nil {
		t.Error("GetResumptionByID on closed DB: want error, got nil")
	}
}

func TestResumption_GetByPeer_ClosedDB(t *testing.T) {
	t.Parallel()
	s := closedStore(t)
	_, err := s.GetResumptionByPeer(context.Background(), 1, 0x1234)
	if err == nil {
		t.Error("GetResumptionByPeer on closed DB: want error, got nil")
	}
}

func TestResumption_Remove_ClosedDB(t *testing.T) {
	t.Parallel()
	s := closedStore(t)
	err := s.RemoveResumption(context.Background(), 1, 0x1234)
	if err == nil {
		t.Error("RemoveResumption on closed DB: want error, got nil")
	}
}

func TestGroupKeys_UpsertGroupKeySet_ClosedDB(t *testing.T) {
	t.Parallel()
	s := closedStore(t)
	err := s.UpsertGroupKeySet(context.Background(), mstore.GroupKeySet{FabricIndex: 1})
	if err == nil {
		t.Error("UpsertGroupKeySet on closed DB: want error, got nil")
	}
}

func TestGroupKeys_GetGroupKeySet_ClosedDB(t *testing.T) {
	t.Parallel()
	s := closedStore(t)
	_, err := s.GetGroupKeySet(context.Background(), 1, 0)
	if err == nil {
		t.Error("GetGroupKeySet on closed DB: want error, got nil")
	}
}

func TestGroupKeys_ListGroupKeySets_ClosedDB(t *testing.T) {
	t.Parallel()
	s := closedStore(t)
	_, err := s.ListGroupKeySets(context.Background(), 1)
	if err == nil {
		t.Error("ListGroupKeySets on closed DB: want error, got nil")
	}
}

func TestGroupKeys_RemoveGroupKeySet_ClosedDB(t *testing.T) {
	t.Parallel()
	s := closedStore(t)
	err := s.RemoveGroupKeySet(context.Background(), 1, 0)
	if err == nil {
		t.Error("RemoveGroupKeySet on closed DB: want error, got nil")
	}
}

func TestGroupKeys_SetGroupKeyMapping_ClosedDB(t *testing.T) {
	t.Parallel()
	s := closedStore(t)
	err := s.SetGroupKeyMapping(context.Background(), mstore.GroupKeyMapping{FabricIndex: 1, GroupID: 1, GroupKeySetID: 0})
	if err == nil {
		t.Error("SetGroupKeyMapping on closed DB: want error, got nil")
	}
}

func TestGroupKeys_RemoveGroupKeyMapping_ClosedDB(t *testing.T) {
	t.Parallel()
	s := closedStore(t)
	err := s.RemoveGroupKeyMapping(context.Background(), 1, 1)
	if err == nil {
		t.Error("RemoveGroupKeyMapping on closed DB: want error, got nil")
	}
}

func TestGroupKeys_ListGroupKeyMappings_ClosedDB(t *testing.T) {
	t.Parallel()
	s := closedStore(t)
	_, err := s.ListGroupKeyMappings(context.Background(), 1)
	if err == nil {
		t.Error("ListGroupKeyMappings on closed DB: want error, got nil")
	}
}

func TestDiagnostics_LoadDiagnostics_ClosedDB(t *testing.T) {
	t.Parallel()
	s := closedStore(t)
	_, err := s.LoadDiagnostics(context.Background())
	if err == nil {
		t.Error("LoadDiagnostics on closed DB: want error, got nil")
	}
}

func TestDiagnostics_SaveDiagnostics_ClosedDB(t *testing.T) {
	t.Parallel()
	s := closedStore(t)
	err := s.SaveDiagnostics(context.Background(), mstore.DiagnosticsRecord{})
	if err == nil {
		t.Error("SaveDiagnostics on closed DB: want error, got nil")
	}
}

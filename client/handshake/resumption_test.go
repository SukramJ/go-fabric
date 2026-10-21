// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package handshake_test

import (
	"testing"

	"github.com/SukramJ/go-fabric/client/handshake"
)

// TestMemoryResumptionStore_DeleteForgetsThePeer: DeleteResumptionRecord
// removes a peer's record and reports whether there was one (matter.js
// SessionManager.deleteResumptionRecord, session/SessionManager.ts:468-480
// at 0ceb202d); the record handed out is a copy.
func TestMemoryResumptionStore_DeleteForgetsThePeer(t *testing.T) {
	t.Parallel()
	store := &handshake.MemoryResumptionStore{}
	peer := handshake.PeerAddress{FabricIndex: 1, NodeID: 42}
	if ok, err := store.DeleteResumptionRecord(peer); err != nil || ok {
		t.Fatalf("delete of an unknown peer = %v, %v; want false, nil", ok, err)
	}
	rec := handshake.ResumptionRecord{Peer: peer, SharedSecret: []byte{1, 2, 3}, ResumptionID: make([]byte, 16), PeerCATs: []uint32{0x0001_0001}}
	if err := store.SaveResumptionRecord(rec); err != nil {
		t.Fatal(err)
	}
	got, ok := store.FindResumptionRecord(peer)
	if !ok || got.PeerCATs[0] != 0x0001_0001 {
		t.Fatalf("find = %+v, %v", got, ok)
	}
	got.SharedSecret[0] = 9
	if again, _ := store.FindResumptionRecord(peer); again.SharedSecret[0] != 1 {
		t.Error("the record handed out shares its secret with the store")
	}
	if ok, err := store.DeleteResumptionRecord(peer); err != nil || !ok {
		t.Fatalf("delete = %v, %v; want true, nil", ok, err)
	}
	if _, ok := store.FindResumptionRecord(peer); ok {
		t.Error("the record survived its deletion")
	}
}

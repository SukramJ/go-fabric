// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/store/memstore"
)

// TestUpdateNOCDropsResumptionRecordsParity pins that UpdateNOC's
// fabric-updated hook deletes the fabric's resumption records, so no peer
// resumes a session established under the replaced NOC. Mirrors matter.js
// packages/protocol/src/common/FailsafeContext.ts:167-170 (replaceFabric →
// SessionManager.deleteResumptionRecordsForFabric, SessionManager.ts:491).
// The node here holds only the store and an empty CASE identity table, so
// the hook stops after the drop (the identity reload fails).
func TestUpdateNOCDropsResumptionRecordsParity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := memstore.New()
	idx, err := st.AddFabric(ctx, store.FabricRecord{FabricID: 1, NodeID: 2, RootPublicKey: append([]byte{0x04}, bytes.Repeat([]byte{1}, 64)...)})
	if err != nil {
		t.Fatalf("AddFabric: %v", err)
	}
	rec := store.ResumptionRecord{FabricIndex: idx, PeerNodeID: 7, ResumptionID: bytes.Repeat([]byte{2}, 16), SharedSecret: bytes.Repeat([]byte{3}, 32)}
	if err := st.UpsertResumption(ctx, rec); err != nil {
		t.Fatalf("UpsertResumption: %v", err)
	}
	logger := slog.New(slog.DiscardHandler)
	n := &Node{st: st, logger: logger, caseIDs: newCaseIdentities(logger, nil)}

	n.fabricUpdated(ctx, idx)

	if _, err := st.GetResumptionByPeer(ctx, idx, 7); !errors.Is(err, store.ErrResumptionNotFound) {
		t.Errorf("resumption record after UpdateNOC: err = %v, want ErrResumptionNotFound", err)
	}
}

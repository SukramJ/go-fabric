// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/store/memstore"
)

// TestLocalOperateEntrySkipsAFabricAnAddNOCHasNotCommitted: a fabric whose
// row is FabricPendingAdd gets no node entry — its ACL is AddNOC's until
// the commit, and the node's pass at the fabric's CommissioningComplete
// adds it; a committed fabric gets it at once.
func TestLocalOperateEntrySkipsAFabricAnAddNOCHasNotCommitted(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := memstore.New()
	n := &Node{st: st, logger: slog.New(slog.DiscardHandler)}
	add := func(id uint64, pending store.FabricPending) uint8 {
		idx, err := st.AddFabric(ctx, store.FabricRecord{
			FabricID: id, NodeID: 2, Pending: pending,
			RootPublicKey: append([]byte{0x04}, bytes.Repeat([]byte{byte(id)}, 64)...),
		})
		if err != nil {
			t.Fatal(err)
		}
		return idx
	}
	pending, committed := add(1, store.FabricPendingAdd), add(2, store.FabricCommitted)
	if n.addLocalOperateEntryLocked(ctx, pending, 1, 0x0029, "test") {
		t.Error("the node's entry was stored on a fabric AddNOC has not committed")
	}
	if acl, err := st.ListACL(ctx, pending); err != nil || len(acl) != 0 {
		t.Errorf("the pending fabric's ACL %v, %v; want it untouched", acl, err)
	}
	if !n.addLocalOperateEntryLocked(ctx, committed, 1, 0x0029, "test") {
		t.Error("a committed fabric did not get the node's entry")
	}
}

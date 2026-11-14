// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store/memstore"
)

// failedUpdateThenAdd is the state of review II R2-P1-rw-06 and
// R2-P2-comm-12 = R2-P2-store-05, on the fabric row (owner decision 36): a
// committed fabric whose UpdateNOC failed in the armed fail-safe, and an
// AddNOC that fail-safe then admitted. It returns the rig, the updated
// fabric with its credentials before the update, and the added fabric.
func failedUpdateThenAdd(ctx context.Context, t *testing.T, st *rowStore) (r *rowRig, first uint8, oldNOC []byte, oldNode uint64, added uint8) {
	t.Helper()
	r = newRowRig(t, st, nil)
	first, firstRoot := r.commission(ctx, t)
	oldNOC, oldNode = credentialsOf(ctx, t, st, first)
	r.armOver(ctx, t, first, 60)
	st.setFault(failOn("UpdateFabricNodeID", "UpdateFabricCredentials"))
	if resp := r.updateNOC(ctx, t, first, firstRoot, oldNode+1); resp.StatusCode == 0 {
		t.Fatalf("UpdateNOC over a failing store answered OK")
	}
	st.setFault(nil)
	_, _, added = commissionTestFabric(im.WithFabricFilter(ctx, true, first), t, r.oc)
	return r, first, oldNOC, oldNode, added
}

// agreesWithAnswer restarts the server on st and fails the test unless the
// added fabric is there exactly when CommissioningComplete answered success
// and the updated fabric holds its credentials from before the update.
func agreesWithAnswer(ctx context.Context, t *testing.T, st *rowStore, ok bool, first uint8, oldNOC []byte, oldNode uint64, added uint8) {
	t.Helper()
	restartRow(t, st)
	_, err := st.GetFabric(ctx, added)
	if present := err == nil; present != ok {
		t.Fatalf("CommissioningComplete answered success=%v, yet after the restart the added fabric is present=%v", ok, present)
	}
	if noc, node := credentialsOf(ctx, t, st, first); !bytes.Equal(noc, oldNOC) || node != oldNode {
		t.Fatalf("after the restart fabric %d holds node 0x%X (NOC changed %v); want its credentials from before the failed UpdateNOC", first, node, !bytes.Equal(noc, oldNOC))
	}
}

// TestGencomm_CommissioningCompleteCommitsTheRowOrNothing pins review II
// R2-P1-rw-06 on the row: CommissioningComplete's commit is one store
// write. A fault at it answers FAILURE, and a restart then rolls the
// added fabric back; without one it answers success and a restart keeps
// it. The failed UpdateNOC wrote nothing either way.
func TestGencomm_CommissioningCompleteCommitsTheRowOrNothing(t *testing.T) {
	t.Parallel()
	for _, fault := range []bool{false, true} {
		t.Run(fmt.Sprintf("fault %v", fault), func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			st := newRowStore(memstore.New())
			r, first, oldNOC, oldNode, added := failedUpdateThenAdd(ctx, t, st)
			if fault {
				st.setFault(failOn("CommitFabric"))
			}
			ok := r.commissioningComplete(ctx, t, added)
			st.setFault(nil)
			if ok == fault {
				t.Fatalf("CommissioningComplete answered success=%v with a fault at its commit %v", ok, fault)
			}
			agreesWithAnswer(ctx, t, st, ok, first, oldNOC, oldNode, added)
		})
	}
}

// TestGencomm_CommissioningCompleteCrashPoints walks the node going down
// after each of CommissioningComplete's store writes, before or after it
// answers (R2-P2-comm-12 = R2-P2-store-05): whatever the point, the
// restart agrees with the answer the commissioner got — there is no state
// between the commit's writes, since the commit is one.
func TestGencomm_CommissioningCompleteCrashPoints(t *testing.T) {
	t.Parallel()
	for landed := range 3 {
		t.Run(fmt.Sprintf("down after %d writes", landed), func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			st := newRowStore(memstore.New())
			r, first, oldNOC, oldNode, added := failedUpdateThenAdd(ctx, t, st)
			st.crashAfterWrites(landed)
			ok := r.commissioningComplete(ctx, t, added)
			st.crashAfterWrites(-1)
			agreesWithAnswer(ctx, t, st, ok, first, oldNOC, oldNode, added)
		})
	}
}

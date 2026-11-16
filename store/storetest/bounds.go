// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//nolint:thelper // each case is a test body that Run runs as a subtest
package storetest

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/store"
)

// resumptionRecordID is a resumption id distinct for each (fabric, peer).
func resumptionRecordID(f uint8, peer uint64) store.ResumptionRecord {
	rec := resumptionRec(f, peer, 0)
	rec.ResumptionID[0], rec.ResumptionID[1], rec.ResumptionID[2] = f, byte(peer), byte(peer>>8)
	return rec
}

// resumptionCapPerFabric (R2-P6-bound-0106): a fabric holds at most
// [store.ResumptionRecordsPerFabric] resumption records; the record written
// least recently goes when one more peer's is written, a rewritten record
// counts as just written, and another fabric's records are not touched.
func resumptionCapPerFabric(ctx context.Context, t *testing.T, p store.Port) {
	const n = store.ResumptionRecordsPerFabric
	one, two := addFabric(ctx, t, p, 1), addFabric(ctx, t, p, 2)
	for peer := range uint64(n) {
		must(t, p.UpsertResumption(ctx, resumptionRecordID(two, peer)))
	}
	for peer := range uint64(n) {
		must(t, p.UpsertResumption(ctx, resumptionRecordID(one, peer)))
	}
	// Peer 0 is rewritten (a resumed session), so peer 1 is now the least
	// recently written.
	must(t, p.UpsertResumption(ctx, resumptionRecordID(one, 0)))
	must(t, p.UpsertResumption(ctx, resumptionRecordID(one, n)))
	held := func(f uint8, peer uint64) bool {
		_, err := p.GetResumptionByPeer(ctx, f, peer)
		if err != nil && !errors.Is(err, store.ErrResumptionNotFound) {
			t.Fatalf("GetResumptionByPeer(%d, %d): %v", f, peer, err)
		}
		return err == nil
	}
	if held(one, 1) {
		t.Errorf("fabric %d holds %d+1 records: the least recently written (peer 1) was not evicted", one, n)
	}
	for _, peer := range []uint64{0, 2, n - 1, n} {
		if !held(one, peer) {
			t.Errorf("fabric %d lost peer %d's record", one, peer)
		}
	}
	if _, err := p.GetResumptionByID(ctx, resumptionRecordID(one, 1).ResumptionID); !errors.Is(err, store.ErrResumptionNotFound) {
		t.Errorf("the evicted record's resumption id still resolves: %v", err)
	}
	for peer := range uint64(n) {
		if !held(two, peer) {
			t.Errorf("a write on fabric %d evicted fabric %d's peer %d", one, two, peer)
		}
	}
}

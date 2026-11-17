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

// resumptionCap is the port's per-fabric cap on resumption records
// (store/port.go, Resumption): the number every backend keeps.
const resumptionCap = 16

// resumptionRecordID is a resumption id distinct for each (fabric, peer).
func resumptionRecordID(f uint8, peer uint64) store.ResumptionRecord {
	rec := resumptionRec(f, peer, 0)
	rec.ResumptionID[0], rec.ResumptionID[1], rec.ResumptionID[2] = f, byte(peer&0xff), byte(peer>>8&0xff)
	return rec
}

// resumptionCapPerFabric (R2-P6-bound-0106): a fabric holds at most
// resumptionCap resumption records (store/port.go, Resumption); the record written
// least recently goes when one more peer's is written, a rewritten record
// counts as just written, and another fabric's records are not touched.
func resumptionCapPerFabric(ctx context.Context, t *testing.T, p store.Port) {
	const n = resumptionCap
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

// groupKeyMapOrder (F-38, decision 148): the key map reads back in the
// order it was written — ReplaceGroupKeyMappings' input order;
// SetGroupKeyMapping appends a GroupID the map does not hold and rewrites
// one it holds in its place; a removal keeps the others' order.
func groupKeyMapOrder(ctx context.Context, t *testing.T, p store.Port) {
	f := addFabric(ctx, t, p, 1)
	ids := func() []uint16 {
		maps, err := p.ListGroupKeyMappings(ctx, f)
		must(t, err)
		out := make([]uint16, 0, len(maps))
		for _, m := range maps {
			out = append(out, m.GroupID)
		}
		return out
	}
	must(t, p.ReplaceGroupKeyMappings(ctx, f, []store.GroupKeyMapping{
		{GroupID: 0x0300, GroupKeySetID: 1}, {GroupID: 0x0100, GroupKeySetID: 1}, {GroupID: 0x0200, GroupKeySetID: 2},
	}))
	wantEqual(t, "the order a replace wrote", ids(), []uint16{0x0300, 0x0100, 0x0200})
	must(t, p.SetGroupKeyMapping(ctx, store.GroupKeyMapping{FabricIndex: f, GroupID: 0x0050, GroupKeySetID: 3}))
	must(t, p.SetGroupKeyMapping(ctx, store.GroupKeyMapping{FabricIndex: f, GroupID: 0x0300, GroupKeySetID: 4}))
	wantEqual(t, "the order after a set of a new and of a held GroupID", ids(), []uint16{0x0300, 0x0100, 0x0200, 0x0050})
	must(t, p.RemoveGroupKeyMapping(ctx, f, 0x0100))
	must(t, p.SetGroupKeyMapping(ctx, store.GroupKeyMapping{FabricIndex: f, GroupID: 0x0100, GroupKeySetID: 1}))
	wantEqual(t, "the order after a removal and a set", ids(), []uint16{0x0300, 0x0200, 0x0050, 0x0100})
	must(t, p.RemoveGroupKeySet(ctx, f, 2))
	wantEqual(t, "the order after a key set's entries went", ids(), []uint16{0x0300, 0x0050, 0x0100})
}

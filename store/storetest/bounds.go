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

// resumptionClientCap is the port's per-fabric cap on the records the CASE
// client writes (ResumptionRecord.Initiator; owner decision 273).
const resumptionClientCap = 1024

// resumptionRecordID is a resumption id distinct for each (fabric, peer).
func resumptionRecordID(f uint8, peer uint64) store.ResumptionRecord {
	rec := resumptionRec(f, peer, 0)
	rec.ResumptionID[0], rec.ResumptionID[1], rec.ResumptionID[2] = f, byte(peer&0xff), byte(peer>>8&0xff)
	return rec
}

// resumptionCapPerFabric (R2-P6-bound-0106): a fabric holds at most
// resumptionCap resumption records of the responder (store/port.go,
// Resumption; resumptionCapRespondersOnly for the client's); the record written
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

// resumptionCapRespondersOnly (R2-P7-sec-01, decision 270): the cap bounds
// the records the CASE responder wrote — those a foreign peer can make the
// node write — and leaves the records the node's own CASE client wrote
// (ResumptionRecord.Initiator) neither counted nor evicted. The role reads
// back by peer and by id, and a rewrite by the other role takes its role.
func resumptionCapRespondersOnly(ctx context.Context, t *testing.T, p store.Port) {
	const clients, responders = 40, 20
	f := addFabric(ctx, t, p, 1)
	client := func(peer uint64) store.ResumptionRecord {
		rec := resumptionRecordID(f, peer)
		rec.Initiator = true
		return rec
	}
	// The client's records are written around the responder's, so the
	// oldest and the newest records of the fabric are the client's.
	for peer := range uint64(clients / 2) {
		must(t, p.UpsertResumption(ctx, client(peer)))
	}
	for i := range uint64(responders) {
		must(t, p.UpsertResumption(ctx, resumptionRecordID(f, clients+i)))
	}
	for peer := uint64(clients / 2); peer < clients; peer++ {
		must(t, p.UpsertResumption(ctx, client(peer)))
	}
	role := func(peer uint64) (initiator, held bool) {
		rec, err := p.GetResumptionByPeer(ctx, f, peer)
		if errors.Is(err, store.ErrResumptionNotFound) {
			return false, false
		}
		if err != nil {
			t.Fatalf("GetResumptionByPeer(%d): %v", peer, err)
		}
		return rec.Initiator, true
	}
	for peer := range uint64(clients) {
		if initiator, held := role(peer); !held || !initiator {
			t.Errorf("the client's record of peer %d: held %v, Initiator %v; want held, true", peer, held, initiator)
		}
	}
	const evicted = responders - resumptionCap
	for i := range uint64(responders) {
		initiator, held := role(clients + i)
		if want := i >= evicted; held != want || initiator {
			t.Errorf("the responder's record %d (peer %d): held %v, Initiator %v; want held %v, false", i, clients+i, held, initiator, want)
		}
	}
	if rec, err := p.GetResumptionByID(ctx, client(0).ResumptionID); err != nil || !rec.Initiator {
		t.Errorf("GetResumptionByID of a client's record: Initiator %v, %v; want true", rec.Initiator, err)
	}
	// The responder rewrites the client's record of peer 0: it is the
	// responder's now, the newest, and the oldest responder's record goes.
	must(t, p.UpsertResumption(ctx, resumptionRecordID(f, 0)))
	if initiator, held := role(0); !held || initiator {
		t.Errorf("peer 0 after the responder's rewrite: held %v, Initiator %v; want held, false", held, initiator)
	}
	if _, held := role(clients + evicted); held {
		t.Errorf("the responder's oldest record (peer %d) survived a %dth responder record", clients+evicted, resumptionCap+1)
	}
	// The client rewrites the responder's newest: it leaves the cap, and the
	// next responder record evicts none.
	must(t, p.UpsertResumption(ctx, client(clients+responders-1)))
	must(t, p.UpsertResumption(ctx, resumptionRecordID(f, clients+responders)))
	if _, held := role(clients + evicted + 1); !held {
		t.Errorf("a responder record (peer %d) was evicted while the fabric held %d of the responder's", clients+evicted+1, resumptionCap)
	}
	if initiator, held := role(clients + responders - 1); !held || !initiator {
		t.Errorf("peer %d after the client's rewrite: held %v, Initiator %v; want held, true", clients+responders-1, held, initiator)
	}
}

// resumptionCapPerRole (owner decision 273): the records the CASE client
// wrote (ResumptionRecord.Initiator) have a per-fabric bound of their own,
// resumptionClientCap, the least recently written evicted first; each role
// evicts only its own records — a client write never evicts a responder's
// record, a responder write never a client's.
func resumptionCapPerRole(ctx context.Context, t *testing.T, p store.Port) {
	const over, responderBase = 6, 1 << 12
	f := addFabric(ctx, t, p, 1)
	client := func(peer uint64) store.ResumptionRecord {
		rec := resumptionRecordID(f, peer)
		rec.Initiator = true
		return rec
	}
	held := func(peer uint64) bool {
		_, err := p.GetResumptionByPeer(ctx, f, peer)
		if err != nil && !errors.Is(err, store.ErrResumptionNotFound) {
			t.Fatalf("GetResumptionByPeer(%d): %v", peer, err)
		}
		return err == nil
	}
	for i := range uint64(resumptionCap) {
		must(t, p.UpsertResumption(ctx, resumptionRecordID(f, responderBase+i)))
	}
	for peer := range uint64(resumptionClientCap + over) {
		must(t, p.UpsertResumption(ctx, client(peer)))
	}
	for peer := range uint64(resumptionClientCap + over) {
		if want := peer >= over; held(peer) != want {
			t.Fatalf("the client's record of peer %d after %d client writes: held %v, want %v", peer, resumptionClientCap+over, !want, want)
		}
	}
	for i := range uint64(resumptionCap) {
		if !held(responderBase + i) {
			t.Fatalf("a client write evicted the responder's record of peer %d", responderBase+i)
		}
	}
	// A full responder set rewritten by new peers: the responder's records
	// turn over, the client's stay.
	for i := range uint64(resumptionCap) {
		must(t, p.UpsertResumption(ctx, resumptionRecordID(f, responderBase+resumptionCap+i)))
	}
	for i := range uint64(resumptionCap) {
		if held(responderBase + i) {
			t.Errorf("the responder's record of peer %d survived %d newer ones", responderBase+i, resumptionCap)
		}
	}
	for peer := uint64(over); peer < resumptionClientCap+over; peer++ {
		if !held(peer) {
			t.Fatalf("a responder write evicted the client's record of peer %d", peer)
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

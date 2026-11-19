// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package handshake_test

import (
	"bytes"
	"context"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/client/handshake"
	"github.com/SukramJ/go-fabric/secure/sessionparams"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/store/memstore"
)

// TestStoreResumptionOneRecordForBothRoles: the client's record lands in
// the store's one row per peer with its session parameters and CATs, and a
// row the responder wrote (no session parameters) is the client's record
// too — matter.js keeps one record per peer for both roles
// (SessionManager.ts:261, :896-908; CaseClient.ts:128, :379;
// CaseServer.ts:115, :219, :344 at db021da0; review II, R2-P5-port-06,
// F-db021da0-19).
func TestStoreResumptionOneRecordForBothRoles(t *testing.T) {
	t.Parallel()
	st := memstore.New()
	key := make([]byte, 65)
	key[0] = 0x04
	fi, err := st.AddFabric(context.Background(), store.FabricRecord{FabricID: 0x1002, NodeID: 0x2002, RootPublicKey: key, RootCert: []byte{0x15, 0x18}, VendorID: 0xFFF1})
	if err != nil {
		t.Fatal(err)
	}
	rs := handshake.NewStoreResumption(st)
	peer := handshake.PeerAddress{FabricIndex: fi, NodeID: 0xABCD}
	if _, ok := rs.FindResumptionRecord(peer); ok {
		t.Fatal("a record before any was saved")
	}
	if ok, err := rs.DeleteResumptionRecord(peer); ok || err != nil {
		t.Fatalf("delete of none = %v, %v", ok, err)
	}

	params := sessionparams.Parameters{SessionIdleInterval: 800, SessionActiveInterval: 350, SessionActiveThreshold: 4000, MaxPathsPerInvoke: 4, SupportedTransports: 1 << 1, MaxTCPMessageSize: 64000}
	written := handshake.ResumptionRecord{
		Peer:                  peer,
		SharedSecret:          bytes.Repeat([]byte{0x5A}, 32),
		ResumptionID:          bytes.Repeat([]byte{0x01}, 16),
		SessionParameters:     params,
		CaseAuthenticatedTags: []uint32{0x00010001},
		Initiator:             true,
	}
	if err := rs.SaveResumptionRecord(written); err != nil {
		t.Fatal(err)
	}
	// The responder reads the client's record by id; the store holds its
	// role, which keeps it out of the per-fabric cap (R2-P7-sec-01).
	row, err := st.GetResumptionByID(context.Background(), written.ResumptionID)
	if err != nil || row.FabricIndex != fi || row.PeerNodeID != 0xABCD || row.SessionParameters != params || !slices.Equal(row.CASEAuthTags, written.CaseAuthenticatedTags) || !row.Initiator {
		t.Fatalf("store row %+v, %v", row, err)
	}
	for name, find := range map[string]func() (handshake.ResumptionRecord, bool){
		"by peer": func() (handshake.ResumptionRecord, bool) { return rs.FindResumptionRecord(peer) },
		"by id":   func() (handshake.ResumptionRecord, bool) { return rs.FindByResumptionID(written.ResumptionID) },
	} {
		got, ok := find()
		if !ok || got.Peer != peer || !bytes.Equal(got.SharedSecret, written.SharedSecret) || !bytes.Equal(got.ResumptionID, written.ResumptionID) ||
			got.SessionParameters != params || !slices.Equal(got.CaseAuthenticatedTags, written.CaseAuthenticatedTags) || !got.Initiator {
			t.Errorf("%s: %+v, %v", name, got, ok)
		}
	}

	// The responder replaces the row (no session parameters): the client
	// reads that one.
	responder := store.ResumptionRecord{FabricIndex: fi, PeerNodeID: 0xABCD, ResumptionID: bytes.Repeat([]byte{0x02}, 16), SharedSecret: bytes.Repeat([]byte{0x6B}, 32)}
	if err := st.UpsertResumption(context.Background(), responder); err != nil {
		t.Fatal(err)
	}
	got, ok := rs.FindResumptionRecord(peer)
	if !ok || !bytes.Equal(got.ResumptionID, responder.ResumptionID) || got.SessionParameters != (sessionparams.Parameters{}) || got.CaseAuthenticatedTags != nil || got.Initiator {
		t.Fatalf("after the responder's save: %+v, %v", got, ok)
	}
	if ok, err := rs.DeleteResumptionRecord(peer); !ok || err != nil {
		t.Fatalf("delete = %v, %v", ok, err)
	}
	if _, err := st.GetResumptionByPeer(context.Background(), fi, 0xABCD); err == nil {
		t.Error("the row survived the delete")
	}
}

// unopenedResumption is a store.Resumption whose record of the peer is
// stored but does not open (store.ErrUnseal), as a sealed store's row
// under another key; it counts the removals.
type unopenedResumption struct {
	store.Resumption
	removed int
}

func (*unopenedResumption) GetResumptionByPeer(context.Context, uint8, uint64) (store.ResumptionRecord, error) {
	return store.ResumptionRecord{}, store.ErrUnseal
}

func (u *unopenedResumption) RemoveResumption(context.Context, uint8, uint64) error {
	u.removed++
	return nil
}

// TestStoreResumptionDeletesARecordThatDoesNotOpen: forgetting a peer
// removes its stored record even when the record does not open; only a
// missing record is reported as none.
func TestStoreResumptionDeletesARecordThatDoesNotOpen(t *testing.T) {
	t.Parallel()
	st := &unopenedResumption{}
	ok, err := handshake.NewStoreResumption(st).DeleteResumptionRecord(handshake.PeerAddress{FabricIndex: 1, NodeID: 0xABCD})
	if err != nil || !ok {
		t.Errorf("DeleteResumptionRecord = %v, %v; want true, nil", ok, err)
	}
	if st.removed != 1 {
		t.Errorf("RemoveResumption ran %d times, want once", st.removed)
	}
}

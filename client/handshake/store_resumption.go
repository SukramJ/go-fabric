// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package handshake

import (
	"context"
	"errors"

	"github.com/SukramJ/go-fabric/store"
)

// StoreResumption is a [ResumptionStore] over the store's resumption
// records ([store.Resumption]): the one record per peer — fabric index and
// node id — that the node's CASE responder reads and writes, read and
// written by the CASE client too. A field of the record's session
// parameters that is zero is absent ([store.ResumptionRecord]); a
// resumption takes the unsecured session's parameters for it — what is
// assumed of the peer for the handshake's exchange (case.go, the resume
// branch) — overlaid by the record's and then by those the peer states in
// Sigma2Resume (matter.js CaseClient.ts:184-188 at db021da0). Each save
// replaces the peer's record, whichever role made it, and stores the role
// of the save ([ResumptionRecord].Initiator): the store caps each role per
// fabric on its own (16 responder records, 1024 client records), so the
// responder's records never evict the client's, nor the reverse.
//
// matter.js keeps one resumption record per peer address for both roles:
// packages/protocol/src/session/SessionManager.ts:261 at db021da0, read by
// the CASE client by address (session/case/CaseClient.ts:128) and by the
// responder by id (CaseServer.ts:115), saved by both (CaseClient.ts:379;
// CaseServer.ts:219, :344) through saveResumptionRecord
// (SessionManager.ts:896-908).
//
// The interface takes no context; each call runs on
// [context.Background], as the store's own calls of the responder do on
// theirs. A store error on a lookup reads as no record.
type StoreResumption struct {
	st store.Resumption
}

// NewStoreResumption returns the [ResumptionStore] over st.
func NewStoreResumption(st store.Resumption) *StoreResumption { return &StoreResumption{st: st} }

var _ ResumptionStore = (*StoreResumption)(nil)

// FindResumptionRecord implements [ResumptionStore].
func (s *StoreResumption) FindResumptionRecord(peer PeerAddress) (ResumptionRecord, bool) {
	rec, err := s.st.GetResumptionByPeer(context.Background(), peer.FabricIndex, peer.NodeID)
	if err != nil {
		return ResumptionRecord{}, false
	}
	return recordFromStore(rec), true
}

// FindByResumptionID implements [ResumptionStore].
func (s *StoreResumption) FindByResumptionID(id []byte) (ResumptionRecord, bool) {
	rec, err := s.st.GetResumptionByID(context.Background(), id)
	if err != nil {
		return ResumptionRecord{}, false
	}
	return recordFromStore(rec), true
}

// SaveResumptionRecord implements [ResumptionStore].
func (s *StoreResumption) SaveResumptionRecord(rec ResumptionRecord) error {
	return s.st.UpsertResumption(context.Background(), store.ResumptionRecord{
		FabricIndex:       rec.Peer.FabricIndex,
		PeerNodeID:        rec.Peer.NodeID,
		ResumptionID:      append([]byte(nil), rec.ResumptionID...),
		SharedSecret:      append([]byte(nil), rec.SharedSecret...),
		CASEAuthTags:      append([]uint32(nil), rec.CaseAuthenticatedTags...),
		SessionParameters: rec.SessionParameters,
		Initiator:         rec.Initiator,
	})
}

// DeleteResumptionRecord implements [ResumptionStore]. Only a missing
// record is none: a stored record that does not read — one a sealed store
// cannot open ([store.ErrUnseal]), or any other read error — is removed
// and reported as one.
func (s *StoreResumption) DeleteResumptionRecord(peer PeerAddress) (bool, error) {
	ctx := context.Background()
	if _, err := s.st.GetResumptionByPeer(ctx, peer.FabricIndex, peer.NodeID); errors.Is(err, store.ErrResumptionNotFound) {
		return false, nil
	}
	if err := s.st.RemoveResumption(ctx, peer.FabricIndex, peer.NodeID); err != nil {
		return false, err
	}
	return true, nil
}

// recordFromStore is the client's form of a stored record.
func recordFromStore(rec store.ResumptionRecord) ResumptionRecord {
	var cats []uint32
	if len(rec.CASEAuthTags) > 0 {
		cats = append(cats, rec.CASEAuthTags...)
	}
	return ResumptionRecord{
		Peer:                  PeerAddress{FabricIndex: rec.FabricIndex, NodeID: rec.PeerNodeID},
		SharedSecret:          append([]byte(nil), rec.SharedSecret...),
		ResumptionID:          append([]byte(nil), rec.ResumptionID...),
		SessionParameters:     rec.SessionParameters,
		CaseAuthenticatedTags: cats,
		Initiator:             rec.Initiator,
	}
}

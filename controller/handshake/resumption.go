// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package handshake

import (
	"sync"

	"github.com/SukramJ/go-fabric/secure/sessionparams"
)

// PeerAddress names a peer on one of our fabrics: our fabric index and
// the peer's operational node id — matter.js
// packages/protocol/src/peer/PeerAddress.ts, the key of
// SessionManager.findResumptionRecordByAddress (SessionManager.ts:851-857,
// called at CaseClient.ts:127).
type PeerAddress struct {
	FabricIndex uint8
	NodeID      uint64
}

// ResumptionRecord is what a CASE client keeps of its last session with a
// peer to resume the next one — matter.js ResumptionRecord
// (session/SessionManager.ts:94-96, stored by :859-867; built at
// CaseClient.ts:357-364, updated at
// :216-217).
type ResumptionRecord struct {
	// Peer is the peer the record belongs to.
	Peer PeerAddress
	// SharedSecret is the ECDH shared secret of the session.
	SharedSecret []byte
	// ResumptionID is the responder's resumption id (16 bytes).
	ResumptionID []byte
	// SessionParameters is the peer's parameters of the session.
	SessionParameters sessionparams.Parameters
	// CaseAuthenticatedTags are the session's CATs.
	CaseAuthenticatedTags []uint32
	// PeerCATs are the peer's CATs, lifted from its NOC at the full
	// handshake (notes/parity/by_design.md
	// BD-Matter-CaseInitiatorPeerCATs); a resumed session carries them
	// forward. Usually empty: a device NOC rarely names CATs.
	PeerCATs []uint32
}

// ResumptionStore keeps resumption records by peer.
type ResumptionStore interface {
	// FindResumptionRecord returns the record of peer, if any.
	FindResumptionRecord(peer PeerAddress) (ResumptionRecord, bool)
	// SaveResumptionRecord stores rec, replacing the peer's earlier one.
	SaveResumptionRecord(rec ResumptionRecord) error
}

// MemoryResumptionStore is a [ResumptionStore] in memory. The zero value
// is ready to use; it is safe for concurrent use.
type MemoryResumptionStore struct {
	mu      sync.Mutex
	records map[PeerAddress]ResumptionRecord
}

// FindResumptionRecord implements [ResumptionStore].
func (s *MemoryResumptionStore) FindResumptionRecord(peer PeerAddress) (ResumptionRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[peer]
	return rec.clone(), ok
}

// SaveResumptionRecord implements [ResumptionStore].
func (s *MemoryResumptionStore) SaveResumptionRecord(rec ResumptionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.records == nil {
		s.records = make(map[PeerAddress]ResumptionRecord)
	}
	s.records[rec.Peer] = rec.clone()
	return nil
}

func (r ResumptionRecord) clone() ResumptionRecord {
	r.SharedSecret = append([]byte(nil), r.SharedSecret...)
	r.ResumptionID = append([]byte(nil), r.ResumptionID...)
	r.CaseAuthenticatedTags = append([]uint32(nil), r.CaseAuthenticatedTags...)
	r.PeerCATs = append([]uint32(nil), r.PeerCATs...)
	return r
}

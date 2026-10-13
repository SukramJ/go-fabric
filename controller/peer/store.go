// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package peer

import (
	"slices"
	"sync"

	"github.com/SukramJ/go-fabric/controller/handshake"
)

// Store keeps peer descriptors across restarts. A peer saves its
// descriptor whenever it changes — matter.js emits Peer.updated
// (Peer.ts:99-106, :575-579) for the node layer to persist; here the peer
// calls the store itself. A store error is logged, never fatal, as
// matter.js logs a rejected save worker (general/src/util/Multiplex.ts:41-47).
type Store interface {
	// LoadPeers returns the descriptors stored for a fabric.
	LoadPeers(fabricIndex uint8) ([]Descriptor, error)
	// SavePeer stores d, replacing the peer's earlier descriptor.
	SavePeer(d Descriptor) error
	// DeletePeer removes the peer's descriptor.
	DeletePeer(addr handshake.PeerAddress) error
}

// MemoryStore is a [Store] in memory. The zero value is ready to use; it
// is safe for concurrent use.
type MemoryStore struct {
	mu    sync.Mutex
	peers map[handshake.PeerAddress]Descriptor
	saves int
}

// LoadPeers implements [Store], in node id order.
func (s *MemoryStore) LoadPeers(fabricIndex uint8) ([]Descriptor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Descriptor
	for addr, d := range s.peers {
		if addr.FabricIndex == fabricIndex {
			out = append(out, d.clone())
		}
	}
	slices.SortFunc(out, func(a, b Descriptor) int {
		switch {
		case a.Address.NodeID < b.Address.NodeID:
			return -1
		case a.Address.NodeID > b.Address.NodeID:
			return 1
		}
		return 0
	})
	return out, nil
}

// SavePeer implements [Store].
func (s *MemoryStore) SavePeer(d Descriptor) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.peers == nil {
		s.peers = make(map[handshake.PeerAddress]Descriptor)
	}
	s.peers[d.Address] = d.clone()
	s.saves++
	return nil
}

// DeletePeer implements [Store].
func (s *MemoryStore) DeletePeer(addr handshake.PeerAddress) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.peers, addr)
	return nil
}

// Saves is the number of SavePeer calls so far.
func (s *MemoryStore) Saves() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saves
}

// ResumptionStore is the resumption-record store a peer set uses: a
// handshake.ResumptionStore that can also forget a peer's record, as
// matter.js SessionManager.deleteResumptionRecord does
// (session/SessionManager.ts:468-480), for the NoSharedTrustRoots retry
// (PeerConnection.ts:681-685) and [Peer.Delete] (Peer.ts:532-541).
type ResumptionStore interface {
	handshake.ResumptionStore
	// DeleteResumptionRecord removes the record of peer and reports
	// whether there was one.
	DeleteResumptionRecord(peer handshake.PeerAddress) (bool, error)
}

// MemoryResumptionStore is a [ResumptionStore] in memory. The zero value
// is ready to use; it is safe for concurrent use.
type MemoryResumptionStore struct {
	mu      sync.Mutex
	records map[handshake.PeerAddress]handshake.ResumptionRecord
}

// FindResumptionRecord implements handshake.ResumptionStore.
func (s *MemoryResumptionStore) FindResumptionRecord(peer handshake.PeerAddress) (handshake.ResumptionRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[peer]
	return rec, ok
}

// SaveResumptionRecord implements handshake.ResumptionStore.
func (s *MemoryResumptionStore) SaveResumptionRecord(rec handshake.ResumptionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.records == nil {
		s.records = make(map[handshake.PeerAddress]handshake.ResumptionRecord)
	}
	s.records[rec.Peer] = rec
	return nil
}

// DeleteResumptionRecord implements [ResumptionStore].
func (s *MemoryResumptionStore) DeleteResumptionRecord(peer handshake.PeerAddress) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.records[peer]
	delete(s.records, peer)
	return ok, nil
}

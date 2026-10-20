// SPDX-License-Identifier: Apache-2.0
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
	// mu guards peers and saves.
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

// ResumptionStore is the resumption-record store a peer set uses:
// handshake.ResumptionStore, whose DeleteResumptionRecord serves the
// NoSharedTrustRoots retry (PeerConnection.ts:681-685) and [Peer.Delete]
// (Peer.ts:529-541).
type ResumptionStore = handshake.ResumptionStore

// MemoryResumptionStore is a [ResumptionStore] in memory
// (handshake.MemoryResumptionStore).
type MemoryResumptionStore = handshake.MemoryResumptionStore

// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package client

import (
	"maps"
	"slices"
	"sync"
)

// Store persists a node's image: endpoint → cluster → attribute values and
// the cluster's DataVersion. It replaces matter.js's ClientNodeStore,
// ClientEndpointStore and LocalWriter (packages/node/src/storage/client/ at
// 0ceb202d), whose layout is nodes/<id>/endpoints/<ep>/<cluster>/<attr>
// with the version under "__version__". A value is the attribute's TLV
// encoding. Implementations must be safe for concurrent use.
type Store interface {
	// Load returns every stored endpoint of the node with every cluster's
	// values; an unknown node has none.
	Load(nodeID string) ([]StoredEndpoint, error)
	// Save stores values of one cluster: a nil value deletes its key
	// (EndpointStore.ts:58-131); version is the cluster's DataVersion, nil
	// for an unknown one.
	Save(nodeID string, ep uint16, cluster uint32, values map[uint32][]byte, version *uint32) error
	// EraseCluster removes one cluster (DatasourceCache.erase,
	// LocalWriter.ts:38-40).
	EraseCluster(nodeID string, ep uint16, cluster uint32) error
	// EraseEndpoint removes an endpoint (ClientEndpointStore.erase).
	EraseEndpoint(nodeID string, ep uint16) error
	// EraseNode removes the node (ClientNodeStore.erase,
	// ClientNodeStore.ts:88-107).
	EraseNode(nodeID string) error
	// NodeIDs are the ids of the nodes the store holds, sorted — matter.js
	// ClientNodeStores.knownIds (storage/client/ClientNodeStores.ts:117-121),
	// the storage contexts restored at construction (:44-57). A [Registry]
	// restores one node per id.
	NodeIDs() ([]string, error)
}

// StoredEndpoint is one endpoint of a node as a [Store] holds it.
type StoredEndpoint struct {
	Number   uint16
	Clusters []StoredCluster
}

// StoredCluster is one cluster of an endpoint as a [Store] holds it.
type StoredCluster struct {
	ID      uint32
	Values  map[uint32][]byte
	Version *uint32
}

// MemoryStore is a [Store] in memory. The zero value is ready.
type MemoryStore struct {
	mu    sync.Mutex
	nodes map[string]map[uint16]map[uint32]*StoredCluster
}

var _ Store = (*MemoryStore)(nil)

// Load implements [Store]: the endpoints in number order, their clusters in
// id order, copies.
func (m *MemoryStore) Load(nodeID string) ([]StoredEndpoint, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	eps := m.nodes[nodeID]
	out := make([]StoredEndpoint, 0, len(eps))
	for _, ep := range slices.Sorted(maps.Keys(eps)) {
		se := StoredEndpoint{Number: ep}
		for _, id := range slices.Sorted(maps.Keys(eps[ep])) {
			c := eps[ep][id]
			sc := StoredCluster{ID: id, Values: make(map[uint32][]byte, len(c.Values))}
			for k, v := range c.Values {
				sc.Values[k] = slices.Clone(v)
			}
			if c.Version != nil {
				v := *c.Version
				sc.Version = &v
			}
			se.Clusters = append(se.Clusters, sc)
		}
		out = append(out, se)
	}
	return out, nil
}

// Save implements [Store].
func (m *MemoryStore) Save(nodeID string, ep uint16, cluster uint32, values map[uint32][]byte, version *uint32) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.nodes == nil {
		m.nodes = map[string]map[uint16]map[uint32]*StoredCluster{}
	}
	if m.nodes[nodeID] == nil {
		m.nodes[nodeID] = map[uint16]map[uint32]*StoredCluster{}
	}
	if m.nodes[nodeID][ep] == nil {
		m.nodes[nodeID][ep] = map[uint32]*StoredCluster{}
	}
	c := m.nodes[nodeID][ep][cluster]
	if c == nil {
		c = &StoredCluster{ID: cluster, Values: map[uint32][]byte{}}
		m.nodes[nodeID][ep][cluster] = c
	}
	for k, v := range values {
		if v == nil {
			delete(c.Values, k)
			continue
		}
		c.Values[k] = slices.Clone(v)
	}
	c.Version = nil
	if version != nil {
		v := *version
		c.Version = &v
	}
	return nil
}

// EraseCluster implements [Store].
func (m *MemoryStore) EraseCluster(nodeID string, ep uint16, cluster uint32) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.nodes[nodeID][ep], cluster)
	return nil
}

// EraseEndpoint implements [Store].
func (m *MemoryStore) EraseEndpoint(nodeID string, ep uint16) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.nodes[nodeID], ep)
	return nil
}

// EraseNode implements [Store].
func (m *MemoryStore) EraseNode(nodeID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.nodes, nodeID)
	return nil
}

// NodeIDs implements [Store]: every id saved for since its last
// EraseNode, sorted.
func (m *MemoryStore) NodeIDs() ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Sorted(maps.Keys(m.nodes)), nil
}

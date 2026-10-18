// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/im"
)

// ConnectionStateFor exposes the connection-state decision to the tests.
var ConnectionStateFor = connectionStateFor

// ObserveRequests sets the test seam that sees every subscription request
// the node hands to its interaction — the first one and each refreshed one.
func ObserveRequests(n *Node, f func(interaction.SubscribeRequest)) {
	n.mu.Lock()
	n.requestObserver = f
	n.mu.Unlock()
}

// SaveRecord stores rec as node id's commissioning record.
func SaveRecord(st Store, id string, rec Record) error {
	values, err := rec.encode()
	if err != nil {
		return err
	}
	return st.Save(id, 0, recordCluster, values, nil)
}

// WaitRegistry waits for the registry's background work (the reactions
// and the label reconciliation) to end.
func WaitRegistry(r *Registry) { r.work.Wait() }

// HoldOperation registers a fabric operation of kind on n, as Commission
// and Decommission do, until release is called.
func HoldOperation(r *Registry, n *Node, kind string) (release func(), err error) {
	op, err := r.registerOperation(n, kind)
	if err != nil {
		return nil, err
	}
	return func() {
		r.mu.Lock()
		delete(r.ops, n)
		r.mu.Unlock()
		close(op.done)
	}, nil
}

// SaveSettingsAutoSubscribe sets and stores the node's autoSubscribe.
func SaveSettingsAutoSubscribe(n *Node) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.settings.autoSubscribe = true
	n.saveSettingsLocked()
	return nil
}

// KnownVersions is the version filters a read of req sends — matter.js
// clientStructureOf(peer).injectVersionFilters(request)
// (node/src/testing/node-helpers.ts:540, ClientStructure.ts:286-323).
func KnownVersions(n *Node, req interaction.ReadRequest) []im.DataVersionFilter {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.s.withKnownVersions(newScope(req.Attributes, req.NoFabricFilter), req.DataVersionFilters, false)
}

// CacheFabricFiltered is the fabric filter of the interactions the cache
// takes data from — matter.js clientStructureOf(peer).cacheFabricFiltered.
func CacheFabricFiltered(n *Node) bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.s.subscribedFabricFiltered
}

// UpdateRecord edits the node's commissioning record and stores it — what
// matter.js does with peer.set({ commissioning: … }).
func UpdateRecord(n *Node, f func(r *Record)) { n.updateRecord(f) }

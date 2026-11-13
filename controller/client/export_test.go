// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/spec/basicinformation"
	"github.com/SukramJ/go-fabric/icd"
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
	return n.s.cacheFabricFiltered()
}

// UpdateRecord edits the node's commissioning record and stores it — what
// matter.js does with peer.set({ commissioning: … }).
func UpdateRecord(n *Node, f func(r *Record)) { n.updateRecord(f) }

// MaxEventNumber is the highest event number the node stored —
// matter.js peer.stateOf(NetworkClient).maxEventNumber.
func MaxEventNumber(n *Node) uint64 {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.settings.maxEventNumber
}

// DeliverEvent hands e to the node as an event of a report.
func DeliverEvent(n *Node, e interaction.EventData) { n.deliverEvent(&e) }

// ICDTable is the registry's ICD peer table of a fabric — matter.js
// FabricManager.for(index).icd in the IcdClient tests.
func ICDTable(r *Registry, fabricIndex uint8) *icd.PeerTable { return r.icdTable(fabricIndex) }

// SetICDCounterStart moves the stored registration's counter start, as
// IcdClientTest.ts:188-194 writes state.counterStart.
func SetICDCounterStart(n *Node, start uint32) {
	n.icd.mu.Lock()
	if n.icd.reg != nil {
		n.icd.reg.CounterStart = start
	}
	n.icd.mu.Unlock()
}

// RebaseICDPeer sets the fed peer's counter state and the stored
// registration's, as IcdClientTest.ts:188-194 and :701 write them.
func RebaseICDPeer(n *Node, counterStart, lastOffset uint32) {
	c := n.icd
	c.mu.Lock()
	fed, h := c.fed, c.handler
	if c.reg != nil {
		c.reg.CounterStart, c.reg.LastOffset = counterStart, lastOffset
	}
	c.mu.Unlock()
	if fed == nil {
		return
	}
	t := c.env.table(fed.FabricIndex)
	if e, ok := t.PeerFor(fed.NodeID); ok {
		e.CounterStart, e.LastOffset = counterStart, lastOffset
		t.AddPeer(e, h)
	}
}

// SetICDFedOffset raises the fed peer's replay floor alone, as
// IcdClientTest.ts:698-701 writes fabric.icd.peerFor(..).lastOffset.
func SetICDFedOffset(n *Node, lastOffset uint32) {
	c := n.icd
	c.mu.Lock()
	fed, h := c.fed, c.handler
	c.mu.Unlock()
	t := c.env.table(fed.FabricIndex)
	if e, ok := t.PeerFor(fed.NodeID); ok {
		e.LastOffset = lastOffset
		t.AddPeer(e, h)
	}
}

// SetNewlyCommissioned marks n newly commissioned, so its next Start runs
// the initial read that emits events (NetworkClient.ts:303-308).
func SetNewlyCommissioned(n *Node) {
	n.mu.Lock()
	n.newlyCommissioned = true
	n.mu.Unlock()
}

// PatchRemoveFabric makes r's decommissions call fn in place of their
// RemoveFabric invoke — matter.js patchRemoveFabric
// (DecommissionTest.ts:36-47 at 862ad75f), which replaces the peer's
// OperationalCredentialsClient.removeFabric. fn gets the fabric index the
// decommission removes and the invoke it replaces, which it may call with
// any index. The returned restore removes the patch.
func PatchRemoveFabric(r *Registry, fn func(fabricIndex uint8, invoke func(fabricIndex uint8) (Result, error)) (Result, error)) (restore func()) {
	r.mu.Lock()
	r.removeFabric = fn
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		r.removeFabric = nil
		r.mu.Unlock()
	}
}

// EmitLeave delivers a BasicInformation Leave event of fabricIndex to r as
// n's subscription does (onEvent) and waits for the reaction it starts —
// matter.js's leave.emit on the peer's BasicInformationClient events
// (DecommissionTest.ts:231 at 862ad75f).
func EmitLeave(r *Registry, n *Node, fabricIndex uint8) {
	r.onEvent(n, 0, basicinformation.ClusterID, basicinformation.EventLeave, basicinformation.LeaveEvent{FabricIndex: fabricIndex})
	r.work.Wait()
}

// HoldsDefaultSubscription reports whether n holds a default subscription,
// established or being established; Stop clears it first.
func HoldsDefaultSubscription(n *Node) bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.subOwner != nil
}

// Started reports whether n is started.
func Started(n *Node) bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.started
}

// StoredDefaultSubscription is the default subscription st holds in node
// id's settings, nil when it holds none.
func StoredDefaultSubscription(st Store, id string) (*SubscribeOptions, error) {
	eps, err := st.Load(id)
	if err != nil {
		return nil, err
	}
	for _, ep := range eps {
		if ep.Number != 0 {
			continue
		}
		for _, c := range ep.Clusters {
			if c.ID != settingsCluster {
				continue
			}
			s, err := decodeSettings(c.Values)
			return s.defaultSubscription, err
		}
	}
	return nil, nil
}

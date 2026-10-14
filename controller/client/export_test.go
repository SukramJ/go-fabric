// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package client

import "github.com/SukramJ/go-fabric/controller/interaction"

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

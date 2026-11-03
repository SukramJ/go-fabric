// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"errors"
	"fmt"

	"github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/endpoint/sqlitestore"
	"github.com/SukramJ/go-fabric/store"
)

// FactoryReset returns the node to its factory state and stops it: every
// fabric is removed through the RemoveFabric fan-out ([Node.RemoveFabric]:
// the Leave event, the sessions and subscriptions, the CASE identity, the
// group state, the host's OnFabricRemoved), then the node stops, and what
// it persisted is erased — the credential store ([store.Eraser]: fabrics,
// ACL, group keys, resumption records, subscriptions, settings and
// counters, the pinned UniqueID and the Wi-Fi networks among them) and
// every endpoint record of [Config].Store.Endpoints, so the numbers they
// held are free. A node that is stopped already has its store erased; one
// whose [Store].Path the stop closed has the file reopened for the erase.
//
// A Go node is single-use — [Node.Stop] ends it, as [ErrStopped] says —
// so the reset does not bring the node back: the host comes back by
// building a new node on the same store and starting it if the old one
// ran, exactly as after a restart (fabrictest.Device.FactoryReset does).
// The new node starts uncommissioned, with a fresh UniqueID pin and a
// device-type validation that reports every endpoint again. A host's own
// [Config].EndpointPersistence is the host's to erase.
//
// FactoryReset is serialised with Start and Stop. Mirrors matter.js
// ServerNode.erase / eraseWithMutex (packages/node/src/node/ServerNode.ts:
// 159-200: go offline, reset, erase the storage, come back online if the
// node was) and resetStorage (:241-256, peers first, then the sessions,
// fabrics, occurrences, credentials and the node store); the node that
// comes back is a new one here (BD-Matter-FactoryResetEndsTheNode).
func (n *Node) FactoryReset(ctx context.Context) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	var errs []error
	if !n.stopped {
		recs, err := n.st.ListFabrics(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("node: factory reset: list fabrics: %w", err))
		}
		for _, rec := range recs {
			if err := n.root.OperationalCredentials.RemoveFabric(ctx, rec.FabricIndex); err != nil && !errors.Is(err, store.ErrFabricNotFound) {
				errs = append(errs, fmt.Errorf("node: factory reset: remove fabric %d: %w", rec.FabricIndex, err))
			}
		}
		n.keepOwnedDB = true
		err = n.stopLocked(ctx)
		n.keepOwnedDB = false
		if err != nil {
			errs = append(errs, err)
		}
	}
	errs = append(errs, n.eraseStores(ctx))
	n.closeOwnedDB()
	return errors.Join(errs...)
}

// eraseStores erases the credential store and the endpoint records; a
// store file the stop closed is reopened for it.
func (n *Node) eraseStores(ctx context.Context) error {
	st, endpoints := n.st, n.cfg.Store.Endpoints
	if n.ownedDB == nil && n.cfg.Store.Path != "" {
		db, err := openStorePath(n.cfg.Store.Path)
		if err != nil {
			return fmt.Errorf("node: factory reset: %w", err)
		}
		defer func() { _ = db.Close() }()
		st, endpoints = store.New(db), sqlitestore.New(db)
	}
	var errs []error
	if err := st.Erase(ctx); err != nil {
		errs = append(errs, fmt.Errorf("node: factory reset: erase the store: %w", err))
	}
	if endpoints != nil {
		errs = append(errs, eraseEndpointRecords(ctx, endpoints))
	}
	return errors.Join(errs...)
}

// eraseEndpointRecords removes every endpoint record (matter.js erases the
// node store, ServerNodeStore.erase, which holds the endpoint numbers).
func eraseEndpointRecords(ctx context.Context, s endpoint.Store) error {
	recs, err := s.ListEndpoints(ctx, "")
	if err != nil {
		return fmt.Errorf("node: factory reset: list endpoints: %w", err)
	}
	var errs []error
	for _, rec := range recs {
		if err := s.RemoveEndpoint(ctx, rec.Key); err != nil {
			errs = append(errs, fmt.Errorf("node: factory reset: remove endpoint %d: %w", rec.EndpointID, err))
		}
	}
	return errors.Join(errs...)
}

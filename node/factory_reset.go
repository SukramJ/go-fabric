// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/SukramJ/go-fabric/bootid"
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
// The stores erased are the ones the node used: a [Store].Credentials or
// [Store].Endpoints override as it is, and the store New built on the file
// for the other. A SQLite file the node's stores are on is scrubbed after
// every delete ([store.Scrub]), so none of the erased bytes — endpoint
// records included — remain in it. A scrub that does not finish (another
// connection's reader blocks the checkpoint, a full disk fails the VACUUM)
// does not undo the reset: every other step completes, the fresh UniqueID
// included, and the error wraps [store.ErrScrubIncomplete] — the rows are
// gone, their bytes may remain in the file until a later [store.Scrub] of
// it succeeds.
//
// A Go node is single-use — [Node.Stop] ends it, as [ErrStopped] says —
// so the reset does not bring the node back: the host comes back by
// building a new node on the same store and starting it if the old one
// ran, exactly as after a restart (fabrictest.Device.FactoryReset does).
// The new node starts uncommissioned, with a fresh random UniqueID pinned
// (unless UniqueID rotation is on) and a device-type validation that
// reports every endpoint again. A host's own
// [Config].EndpointPersistence is the host's to erase.
//
// A reset whose stop does not complete — its context ends while a bridge
// Start or Stop the host runs holds the bridge's lifecycle — erases and
// closes nothing and returns the stop's error: the fabrics are removed,
// the node is not stopped and keeps its store, and a later FactoryReset
// finishes the reset.
//
// FactoryReset is serialised with Start and Stop. Mirrors matter.js
// ServerNode.erase / eraseWithMutex (packages/node/src/node/ServerNode.ts:
// 159-200: go offline, reset, erase the storage, come back online if the
// node was) and resetStorage (:242-256, peers first, then the sessions,
// fabrics, occurrences, credentials and the node store); the node that
// comes back is a new one here (BD-Matter-FactoryResetEndsTheNode).
func (n *Node) FactoryReset(ctx context.Context) error {
	n.lifecycle <- struct{}{}
	defer n.leaveLifecycle()
	var errs []error
	if !n.stopped {
		recs, err := n.st.ListFabrics(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("node: factory reset: list fabrics: %w", err))
		}
		for i := range recs {
			idx := recs[i].FabricIndex
			if err := n.root.OperationalCredentials.RemoveFabric(ctx, idx); err != nil && !errors.Is(err, store.ErrFabricNotFound) {
				errs = append(errs, fmt.Errorf("node: factory reset: remove fabric %d: %w", idx, err))
			}
		}
		n.keepOwnedDB = true
		err = n.stopLocked(ctx)
		n.keepOwnedDB = false
		if err != nil {
			// The bridge did not stop: it still serves on the store, so
			// nothing is erased or closed under it. The node stays
			// stoppable; a later FactoryReset or Stop finishes the work
			// (review II, R2-P1-rw-03).
			return errors.Join(append(errs, err)...)
		}
	}
	errs = append(errs, n.eraseStores(ctx))
	n.closeOwnedDB()
	return errors.Join(errs...)
}

// eraseStores erases the credential store and the endpoint records the
// node used: a host override as it is, and a store New built on a file
// the stop closed on the file reopened for it.
//
// The endpoint records go first: the SQLite credential store's Erase ends
// with the scrub that leaves no erased byte in the file ([store.Scrub]), so
// it runs after every delete on that file. Endpoint records New built on a
// file whose credential store is a host override get the scrub on their
// own (TestFactoryResetLeavesNoEndpointRecordResidue).
func (n *Node) eraseStores(ctx context.Context) error {
	st, endpoints, db := n.st, n.cfg.Store.Endpoints, n.cfg.Store.DB
	if n.ownedDB == nil && n.cfg.Store.Path != "" && (n.builtCredentials || n.builtEndpoints) {
		reopened, err := openStorePath(n.cfg.Store.Path)
		if err != nil {
			return fmt.Errorf("node: factory reset: %w", err)
		}
		defer func() { _ = reopened.Close() }()
		db = reopened
		if n.builtCredentials {
			st = store.New(db)
		}
		if n.builtEndpoints {
			endpoints = sqlitestore.New(db)
		}
	}
	var errs []error
	if endpoints != nil {
		errs = append(errs, eraseEndpointRecords(ctx, endpoints))
	}
	// A scrub that did not finish (store.ErrScrubIncomplete) follows a
	// committed erase: the rows are gone, so the fresh UniqueID is pinned
	// as after a complete one, and the error says only the bytes may
	// remain (review II, R2-P1-dr-rest-02).
	eraseErr := st.Erase(ctx)
	if eraseErr != nil {
		errs = append(errs, fmt.Errorf("node: factory reset: erase the store: %w", eraseErr))
	}
	if eraseErr == nil || errors.Is(eraseErr, store.ErrScrubIncomplete) {
		if err := n.pinFreshUniqueID(ctx, st); err != nil {
			errs = append(errs, fmt.Errorf("node: factory reset: pin a fresh UniqueID: %w", err))
		}
	}
	if n.builtEndpoints && !n.builtCredentials && db != nil {
		if err := store.Scrub(ctx, db); err != nil {
			errs = append(errs, fmt.Errorf("node: factory reset: scrub the endpoint records: %w", err))
		}
	}
	return errors.Join(errs...)
}

// pinFreshUniqueID pins a fresh random UniqueID — 16 random bytes in hex,
// DeriveUniqueID's shape — in the erased store, so the node that comes back
// serves another one (persisted.uniqueID reads a stored pin first). matter.js
// draws one when the erased state holds none (BasicInformationServer.ts:89-91,
// createUniqueId :161-170 at 862ad75f), so its reset node serves a new
// UniqueID (testing/node-helpers.ts:164-165). With UniqueID rotation on the
// pin is never read, and none is written.
func (n *Node) pinFreshUniqueID(ctx context.Context, st store.Settings) error {
	salt := n.persisted.salt
	if salt == nil {
		salt = bootid.Salt
	}
	if salt() != [16]byte{} {
		return nil
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return err
	}
	return st.SetSetting(ctx, store.SettingUniqueID, hex.EncodeToString(b[:]))
}

// eraseEndpointRecords erases every endpoint record and restarts the
// numbering at the first bridged number, so the node that comes back
// numbers as a fresh install. Mirrors matter.js
// packages/node/src/storage/server/ServerNodeStore.ts erase (:134-139),
// which erases the endpoint stores, and ServerEndpointStores.ts erase
// (:63-77): storage.clearAll(), a fresh holder map, #nextNumber = 1.
func eraseEndpointRecords(ctx context.Context, s endpoint.Store) error {
	if err := s.EraseEndpoints(ctx); err != nil {
		return fmt.Errorf("node: factory reset: erase endpoints: %w", err)
	}
	return nil
}

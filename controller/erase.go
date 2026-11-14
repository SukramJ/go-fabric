// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller

import (
	"context"
	"errors"
	"fmt"

	"github.com/SukramJ/go-fabric/controller/client/sqlstore"
	"github.com/SukramJ/go-fabric/store"
)

// Erase is the controller's factory reset: it closes the controller and
// erases what it persisted in [Config].DB — every commissioned node with
// its cached values, the fabric record and the CA's key material
// ("controller.ca", "controller.ca.fabric"), and the rest of the node store
// the controller keeps there. A [Config].CAKey held outside the database
// (an HSM) is the host's to retire.
//
// Erase deletes nothing while a node is still stopping, nor while another
// Close it could not wait for within ctx is still running: a node whose
// report queue a hook holds past ctx ([client.Node.Stop]) still writes its
// queued values and the store's final flush, and a row deleted before
// them would come back. Erase waits for those stops within ctx — a node's
// second Close does — and when ctx ends first it returns ctx's error with
// the database untouched; an Erase called again once the hook returned
// completes the reset. Concurrent Erase calls run one after the other.
//
// An error that wraps [store.ErrScrubIncomplete] is a completed reset whose
// scrub of the file did not finish (another connection's reader holds its
// -wal, a full disk fails the VACUUM): every row is gone, and the erased
// bytes may remain in the file until a later [store.Scrub] of [Config].DB
// succeeds.
//
// A Go controller is single-use, as the node is: the host comes back by
// building a new controller on the same DB, which mints a new CA and
// fabric and holds no peer (BD-Matter-FactoryResetEndsTheNode). Mirrors
// matter.js ServerNode.resetStorage for a controller
// (packages/node/src/node/ServerNode.ts:242-256: the peers first, then
// ServerEnvironment.eraseCredentials, packages/node/src/node/server/
// ServerEnvironment.ts:72-89, which erases the CertificateAuthority, and
// the node store).
func (c *Controller) Erase(ctx context.Context) error {
	errs := []error{c.Close(ctx)}
	// A second Close returns on ctx while the first may still stop the
	// nodes, before it could record them for the wait below: nothing is
	// erased then, as for a node still stopping.
	if !c.closeFinished() {
		errs = append(errs, errors.New("controller: erase: the controller is still closing, nothing erased"))
		return errors.Join(errs...)
	}
	c.eraseMu.Lock()
	defer c.eraseMu.Unlock()
	// Node.Stop's rule: Erase does nothing after a Stop that failed
	// (controller/client/node.go, Node.Stop). A second Registry.Close waits
	// within ctx for the stops the first could not wait for.
	if reg := c.stopping; reg != nil {
		if err := reg.Close(ctx); err != nil {
			errs = append(errs, fmt.Errorf("controller: erase: nodes still stopping, nothing erased: %w", err))
			return errors.Join(errs...)
		}
		c.stopping = nil
	}
	nodes := sqlstore.New(c.db)
	ids, err := nodes.NodeIDs() //nolint:contextcheck // sqlstore's node API carries no context
	if err != nil {
		errs = append(errs, fmt.Errorf("controller: erase: list nodes: %w", err))
	}
	for _, id := range ids {
		if err := nodes.EraseNode(id); err != nil { //nolint:contextcheck // as above
			errs = append(errs, fmt.Errorf("controller: erase node %s: %w", id, err))
		}
	}
	if err := store.New(c.db).Erase(ctx); err != nil {
		errs = append(errs, fmt.Errorf("controller: erase: %w", err))
	}
	return errors.Join(errs...)
}

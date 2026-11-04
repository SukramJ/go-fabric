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

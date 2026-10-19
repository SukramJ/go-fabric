// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest

import (
	"context"
	"errors"
	"fmt"

	"github.com/SukramJ/go-fabric/store"
)

// Grant replaces the device's access control list on the pair's fabric with
// one CASE entry for the controller's node id at privilege, on every
// endpoint and cluster: the pair's interactions then act as a subject of
// exactly that privilege. It is matter.js MockServerNode.online's
// accessLevel (mock-server-node.ts:173-186), reached over the wire: the
// device's access check reads the list from its store on every request.
//
// The entry list is written to the device's store directly, as a local
// change, so a Grant below Administer is not undone over the wire; a test
// that needs the administrator back calls Grant with
// store.PrivilegeAdminister.
func (p *Pair) Grant(ctx context.Context, privilege store.Privilege) error {
	if !p.Commissioned() {
		return ErrNotCommissioned
	}
	if privilege < store.PrivilegeView || privilege > store.PrivilegeAdminister {
		return fmt.Errorf("fabrictest: privilege %d", privilege)
	}
	fabrics, err := p.Device.Node().Fabrics(ctx)
	if err != nil {
		return fmt.Errorf("fabrictest: device fabrics: %w", err)
	}
	for _, f := range fabrics {
		if f.NodeID != p.Address.NodeID {
			continue
		}
		return p.Device.Store().ReplaceACL(ctx, f.FabricIndex, []store.ACLEntry{{
			FabricIndex: f.FabricIndex, Privilege: privilege, AuthMode: store.AuthModeCASE,
			Subjects: []uint64{p.Controller.opts.NodeID},
		}})
	}
	return errors.New("fabrictest: the device holds no fabric of the pair")
}

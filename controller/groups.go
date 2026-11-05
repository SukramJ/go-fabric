// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package controller

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/SukramJ/go-fabric/groups"
	"github.com/SukramJ/go-fabric/store"
)

// The controller's group sender on its one identity. A group message is
// sealed with the fabric's operational group key and sent to the group's
// multicast address by a node of the fabric; matter.js's controller is
// such a node — a ServerNode with the ControllerBehavior
// (ControllerBehavior.ts:43-50 at 862ad75f) that serves GroupKeyManagement
// and sends through its own interface. matterjs-server composes its
// controller so (packages/ws-controller/src/controller/ControllerNode.ts:33
// at 0bd06f2). Until phase 37 the reference controller
// ran a second node for it (BD-chip-ControllerCLI-OwnNode); now the
// controller holds the key sets it writes to the members in its own
// database and sends as RootNodeID through its own socket.

// openGroups composes the manager and gives it the controller's socket,
// or [Config.GroupTransport].
func (c *Controller) openGroups(ctx context.Context, cfg Config, log *slog.Logger) error {
	st := store.New(cfg.DB)
	if err := c.ensureFabricRow(ctx, st); err != nil {
		return err
	}
	m, err := groups.NewManager(st, log)
	if err != nil {
		return err
	}
	m.SetClock(c.clock)
	if cfg.GroupTransport != nil {
		m.SetTransport(cfg.GroupTransport)
	} else {
		m.SetTransport(c.udp)
	}
	if err := m.Load(ctx); err != nil {
		return err
	}
	c.groups = m
	return nil
}

// ensureFabricRow keeps the controller's own fabric in the node tables of
// its database, at the commissioner's fabric index and with RootNodeID as
// the node id — the row the group key sets hang on. A row left by the
// former own node (the same fabric, that node's id) is taken over; a row
// of another fabric at that index is a configuration error.
func (c *Controller) ensureFabricRow(ctx context.Context, st *store.Store) error {
	idx := c.fabricIndex()
	rec, err := st.GetFabric(ctx, idx)
	switch {
	case errors.Is(err, store.ErrFabricNotFound):
		var compressed [8]byte
		if hf, ok := c.fabric.Lookup(c.fabric.AddressOf(c.fabric.RootNodeID)); ok && hf.Identity != nil {
			compressed = hf.Identity.CompressedFabricID
		}
		_, err = st.AddFabric(ctx, store.FabricRecord{
			FabricIndex: idx, FabricID: c.fabric.FabricID, NodeID: c.fabric.RootNodeID, RootPublicKey: c.fabric.RootPublicKey,
			RootCert: c.fabric.CA.RootCert(), VendorID: c.fabric.RootVendorID, Label: c.fabric.CurrentLabel(), CompressedID: compressed,
		})
		return err
	case err != nil:
		return err
	case rec.FabricID != c.fabric.FabricID:
		return fmt.Errorf("%w: the database holds another fabric at index %d", ErrConfiguration, idx)
	case rec.NodeID != c.fabric.RootNodeID:
		return st.UpdateFabricNodeID(ctx, idx, c.fabric.RootNodeID)
	}
	return nil
}

// Groups is the controller's group manager: the key sets it holds for the
// groups it created and the senders over them ([groups.Manager.Sender]).
func (c *Controller) Groups() *groups.Manager { return c.groups }

// SetGroupKey keeps a key set in the controller's own tables and maps
// groupID to it — what KeySetWrite and a GroupKeyMap write do on a
// member node — so that [Groups] can send to the group. The caller writes
// the same key set to the members.
func (c *Controller) SetGroupKey(ctx context.Context, keySet store.GroupKeySet, groupID uint16) error {
	st := store.New(c.db)
	keySet.FabricIndex = c.fabricIndex()
	if err := st.UpsertGroupKeySet(ctx, keySet); err != nil {
		return err
	}
	if err := st.SetGroupKeyMapping(ctx, store.GroupKeyMapping{FabricIndex: keySet.FabricIndex, GroupID: groupID, GroupKeySetID: keySet.GroupKeySetID}); err != nil {
		return err
	}
	return c.groups.Reload(ctx, keySet.FabricIndex)
}

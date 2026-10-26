// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package client

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/client/peer"
	gencomm "github.com/SukramJ/go-fabric/cluster/spec/generalcommissioning"
	"github.com/SukramJ/go-fabric/controller/commissioner"
)

// CompleteOptions configures [Registry.CompleteCommissioning].
type CompleteOptions struct {
	// Fabric is the shared fabric the commissioning is finalized on — the
	// one the initiating commissioner installed the device's NOC for; nil
	// is the registry's first. A fabric the registry does not hold is
	// [ErrForeignFabric].
	Fabric *commissioner.Fabric
	// DiscoveryData and SessionParameters seed the peer: what the
	// initiating commissioner's FinalizeCommissioning was handed
	// (commissioner.Options.FinalizeCommissioning).
	DiscoveryData     *discovery.Device
	SessionParameters *SessionParameters
	// AutoSubscribe, DefaultSubscription and NoInitialRead are a
	// commission's ([CommissionOptions]).
	AutoSubscribe       *bool
	DefaultSubscription *SubscribeOptions
	NoInitialRead       bool
}

// CompleteCommissioning finishes, on this registry, a commissioning
// another commissioner ran up to its operational hand-off (PASE, the NOC,
// commissioner.Options.FinalizeCommissioning) — Peers.completeCommissioning
// (packages/node/src/node/client/Peers.ts:236-359 at 862ad75f): the node
// of nodeID on the shared fabric, unless it is commissioned already
// ([ErrAlreadyCommissioned], :258-266), is created, its peer seeded with
// the discovery data and session parameters (:278-287), set up as a
// commission sets it up and started (:289-300); CommissioningComplete goes
// out with the extended fail-safe response timeout (:302-314), and a
// status or error code other than Ok is a commissioner.CommissioningError
// (:316-340). Any failure up to an Ok deletes the node, so none is left
// behind (:341-353).
func (r *Registry) CompleteCommissioning(ctx context.Context, nodeID uint64, o CompleteOptions) (*Node, error) {
	fabric := o.Fabric
	if fabric == nil {
		fabric = r.cfg.Fabrics[0]
	} else if !slices.Contains(r.cfg.Fabrics, fabric) {
		return nil, fmt.Errorf("%w: cannot complete commissioning of node %d: the registry does not hold the fabric", ErrForeignFabric, nodeID)
	}
	addr := fabric.AddressOf(nodeID)
	if n := r.GetByAddress(addr); n != nil && n.IsCommissioned() {
		return nil, fmt.Errorf("%w: %s", ErrAlreadyCommissioned, n.ID())
	}
	n, err := r.ForAddress(addr, "") //nolint:contextcheck // the peer's service resolves under the DNS-SD cache's context
	if err != nil {
		return nil, err
	}
	err = r.runFabricOperation(n, opCommission, func() error {
		if err := r.complete(ctx, n, addr, o); err != nil {
			if derr := r.deleteNode(context.WithoutCancel(ctx), n, nil); derr != nil {
				r.log.Warn("matter.controller.client.registry.complete_cleanup_failed", slog.String("node", n.ID()), slog.Any("err", derr))
			}
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return n, nil
}

func (r *Registry) complete(ctx context.Context, n *Node, addr PeerAddress, o CompleteOptions) error {
	if o.DiscoveryData != nil || o.SessionParameters != nil {
		d := peer.Descriptor{Address: addr, ReportedSessionParameters: o.SessionParameters}
		if o.DiscoveryData != nil {
			txt := o.DiscoveryData.TXTData
			d.DiscoveryData = &txt
		}
		//nolint:contextcheck // the peer's service resolves under the DNS-SD cache's context
		p, err := r.cfg.Peers.AddKnownPeer(d)
		if err != nil {
			return err
		}
		pd := p.Descriptor()
		n.updateRecord(func(rec *Record) { rec.applyPeer(pd) })
	}
	n.mu.Lock()
	n.settings.defaultSubscription = o.DefaultSubscription.clone()
	n.settings.autoSubscribe = o.AutoSubscribe == nil || *o.AutoSubscribe
	n.s.subscribedFabricFiltered = n.settings.defaultSubscription == nil || !n.settings.defaultSubscription.NoFabricFilter
	n.cfg.NoInitialRead = o.NoInitialRead
	n.newlyCommissioned = true
	n.saveSettingsLocked()
	n.mu.Unlock()
	if err := n.Start(ctx); err != nil {
		return err
	}
	res, err := n.Invoke(ctx, CommandAt(0, gencomm.ClusterID, gencomm.CmdCommissioningComplete), gencomm.CommissioningCompleteRequest{},
		InvokeOptions{ExtendedFailSafeTimeout: true})
	if err != nil {
		if se, ok := asStatusError(err); ok {
			return &commissioner.CommissioningError{Kind: commissioner.ErrCommissioning, Message: fmt.Sprintf("CommissioningComplete failed for %d: status %v", addr.NodeID, se.Status), Cause: err}
		}
		return err
	}
	var resp gencomm.CommissioningCompleteResponse
	if !res.HasFields || resp.DecodeTLV(res.Fields) != nil {
		return &commissioner.CommissioningError{Kind: commissioner.ErrCommissioning, Message: fmt.Sprintf("CommissioningComplete for %d returned no response", addr.NodeID)}
	}
	if resp.ErrorCode != gencomm.CommissioningErrorOk {
		return &commissioner.CommissioningError{Kind: commissioner.ErrCommissioning, Message: fmt.Sprintf("CommissioningComplete failed for %d: %d %s", addr.NodeID, resp.ErrorCode, resp.DebugText)}
	}
	now := r.clock.Now()
	n.updateRecord(func(rec *Record) { rec.CommissionedAt = now })
	r.log.Info("matter.controller.client.registry.commissioning_completed", slog.String("node", n.ID()), slog.String("peer", peerAddressString(addr)))
	if h := n.cfg.Hooks.Commissioned; h != nil {
		h()
	}
	return nil
}

// asStatusError is err's interaction status, when it carries one.
func asStatusError(err error) (*interaction.StatusError, bool) {
	var se *interaction.StatusError
	if ok := errors.As(err, &se); ok {
		return se, true
	}
	return nil, false
}

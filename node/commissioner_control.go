// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package node

import (
	"fmt"
	"time"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	commctrldef "github.com/SukramJ/go-fabric/cluster/spec/commissionercontrol"
)

// CommissionerControlConfig mounts the CommissionerControl server (0x0751)
// on the Aggregator endpoint, where the Aggregator device type lists it
// under FabricSynchronization (device/aggregator_gen.go) and
// connectedhomeip's fabric-sync bridge serves it (kAggregatorEndpointId,
// examples/fabric-sync/bridge/CommissionerControlDelegate.h:30 at the
// harness pin). The two ports are the host's: the approval, and the
// commissioner that commissions an approved client's node — the node
// itself has none (ADR 0020: node does not import controller/); a host
// fills Commissioner with controller/commissioner.
type CommissionerControlConfig struct {
	// Approver approves requests. Required.
	Approver mattercore.CommissioningApprover
	// Commissioner commissions an approved client's node. Required.
	Commissioner mattercore.ReverseCommissioner
	// SupportedDeviceCategories is the attribute; a fabric-synchronising
	// bridge states FabricSynchronization.
	SupportedDeviceCategories commctrldef.SupportedDeviceCategoryBitmap
	// ApprovalTimeout and ApprovalValidity are
	// mattercore.CommissionerControlConfig's.
	ApprovalTimeout, ApprovalValidity time.Duration
}

// buildCommissionerControl builds the server Config.CommissionerControl
// asks for, nil without one.
func (n *Node) buildCommissionerControl() (*mattercore.CommissionerControl, error) {
	cfg := n.cfg.CommissionerControl
	if cfg == nil {
		return nil, nil
	}
	cc, err := mattercore.NewCommissionerControl(mattercore.CommissionerControlConfig{
		Approver: cfg.Approver, Commissioner: cfg.Commissioner, SupportedDeviceCategories: cfg.SupportedDeviceCategories,
		ApprovalTimeout: cfg.ApprovalTimeout, ApprovalValidity: cfg.ApprovalValidity, Logger: n.logger,
	})
	if err != nil {
		return nil, fmt.Errorf("node: %w", err)
	}
	return cc, nil
}

// CommissionerControl returns the CommissionerControl server — nil unless
// [Config].CommissionerControl mounts one on the Aggregator.
func (n *Node) CommissionerControl() *mattercore.CommissionerControl { return n.commissionerControl }

// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"testing"

	commctrldef "github.com/SukramJ/go-fabric/cluster/spec/commissionercontrol"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
)

// TestCommissionerControlMatchesTheGeneratedDefinition holds the
// CommissionerControl server against matter.js
// commissioner-control.element.ts at 0ceb202d (0x0751, revision 1:
// SupportedDeviceCategories, RequestCommissioningApproval and
// CommissionNode accepted, ReverseOpenCommissioningWindow generated,
// CommissioningRequestResult emitted) — the lists connectedhomeip's
// CommissionerControlCluster.cpp serves too (AcceptedCommands :105-110,
// GeneratedCommands :112-118, FeatureMap 0 :67-68).
func TestCommissionerControlMatchesTheGeneratedDefinition(t *testing.T) {
	t.Parallel()
	c, err := NewCommissionerControl(CommissionerControlConfig{
		Approver:                  approverFunc(func(context.Context, CommissioningApprovalRequest) (bool, error) { return true, nil }),
		Commissioner:              reverseCommissionerFunc(func(context.Context, ReverseCommissioning) error { return nil }),
		SupportedDeviceCategories: commctrldef.SupportedDeviceCategoryFabricSynchronization,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	spectest.CheckServer(t, c, commctrldef.Definition, 0)
	if v, ok := c.MatterRead(commctrldef.AttrSupportedDeviceCategories); !ok || v != uint32(commctrldef.SupportedDeviceCategoryFabricSynchronization) {
		t.Fatalf("SupportedDeviceCategories %v %v", v, ok)
	}
	if !c.Emits(commctrldef.EventCommissioningRequestResult) {
		t.Fatal("CommissioningRequestResult not in EventList")
	}
	if _, err := NewCommissionerControl(CommissionerControlConfig{}); err == nil {
		t.Error("no approver accepted")
	}
	if _, err := NewCommissionerControl(CommissionerControlConfig{Approver: approverFunc(nil)}); err == nil {
		t.Error("no commissioner accepted")
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package wire

import (
	"context"
	"errors"

	"thingfabric.dev/go-fabric/cluster"
	"thingfabric.dev/go-fabric/cluster/spec"
	scenesdef "thingfabric.dev/go-fabric/cluster/spec/scenesmanagement"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
)

// ScenesManagement is a minimal stub for the Matter ScenesManagement
// cluster (0x0062). Mandatory on every OnOffLight, DimmableLight, and
// OnOffPlugInUnit device-type per matter.js. The stub advertises
// SceneTableSize=0 and an empty FabricSceneInfo list, and rejects all
// writes / commands.
//
// Mirrors matter.js packages/model/src/standard/elements/
// scenes-management.element.ts — ClusterRevision = 1 (HEAD
// @matter/model 0.16.11). Mandatory attributes: SceneTableSize uint16
// at 0x0001, FabricSceneInfo list at 0x0002.
//
// Deprecated: the ScenesManagement server is cluster/core's
// (core.ScenesManagement, built with core.NewScenesManagement), the one the
// device kit mounts. Removable in v0.25.0.
type ScenesManagement struct{}

// Cluster ID, revision and attribute ids, the generated definition's
// (cluster/spec/scenesmanagement, ADR 0013). The stub serves no feature
// and no command, so it takes nothing else from it: the real server is
// cluster/core's.
const (
	scenesManagementClusterID       = scenesdef.ClusterID
	scenesManagementClusterRevision = scenesdef.Revision

	scenesManagementAttrSceneTableSize  = scenesdef.AttrSceneTableSize
	scenesManagementAttrFabricSceneInfo = scenesdef.AttrFabricSceneInfo
)

// errScenesStub surfaces from Write / Invoke on the stub.
var errScenesStub = errors.New("wire: ScenesManagement is a read-only stub (it serves no scenes)")

// Compile-time assertions.
var (
	_ contract.ClusterServer          = ScenesManagement{}
	_ contract.ClusterAttributeLister = ScenesManagement{}
)

// MatterClusterID implements [contract.ClusterServer].
func (ScenesManagement) MatterClusterID() uint32 { return scenesManagementClusterID }

// MatterRead implements [contract.ClusterServer].
func (ScenesManagement) MatterRead(attrID uint32) (any, bool) {
	switch attrID {
	case scenesManagementAttrSceneTableSize:
		// 0 = empty scene table (the stub serves no scenes).
		return uint16(0), true
	case scenesManagementAttrFabricSceneInfo:
		// Empty list — no scenes stored per fabric.
		return []any{}, true
	case cluster.AttrGlobalFeatureMap:
		return uint32(0), true
	case cluster.AttrGlobalClusterRevision:
		return scenesManagementClusterRevision, true
	}
	return nil, false
}

// MatterWrite rejects every write.
func (ScenesManagement) MatterWrite(_ context.Context, attrID uint32, _ any) error {
	return spec.Errorf(im.StatusUnsupportedWrite, "%w: attrID 0x%04X", errScenesStub, attrID)
}

// MatterWriteChanged implements [contract.ChangeReportingWriter]: the
// server stores no written value, so no write changes it.
func (s ScenesManagement) MatterWriteChanged(ctx context.Context, attrID uint32, value any) (bool, error) {
	return false, s.MatterWrite(ctx, attrID, value)
}

// MatterInvoke rejects every command with a typed UnsupportedCommand
// (0x81); errScenesStub alone would answer StatusFailure (0x01).
// connectedhomeip answers a command it does not handle UnsupportedCommand
// (`src/app/clusters/scenes-server/ScenesManagementCluster.cpp:406-407` at
// 6170af84, the default of InvokeCommand), and matter.js so answers a
// command the cluster does not carry, in its protocol layer
// (`packages/protocol/src/action/server/CommandInvokeResponse.ts:269-271`
// at b25ed960); matter.js's ScenesManagementServer is an implemented
// server, not a stub that refuses commands.
func (ScenesManagement) MatterInvoke(_ context.Context, cmdID uint32, _ any) (any, error) {
	return nil, spec.Errorf(im.StatusUnsupportedCommand, "%w: no commands supported, cmdID 0x%02X", errScenesStub, cmdID)
}

// MatterReportable returns nil — no subscribe-able attributes.
func (ScenesManagement) MatterReportable() []uint32 { return nil }

// MatterAttributes lists the mandatory ScenesManagement attributes.
func (ScenesManagement) MatterAttributes() []uint32 {
	return []uint32{scenesManagementAttrSceneTableSize, scenesManagementAttrFabricSceneInfo}
}

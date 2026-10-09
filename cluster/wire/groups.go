// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package wire

import (
	"context"
	"errors"
	"fmt"

	"github.com/SukramJ/go-fabric/cluster"
	groupsdef "github.com/SukramJ/go-fabric/cluster/spec/groups"
	"github.com/SukramJ/go-fabric/contract"
)

// Groups is a minimal stub for the Matter Groups cluster (0x0004).
// It advertises the GroupNames bit and rejects all writes / commands.
//
// Mirrors matter.js packages/model/src/standard/elements/
// groups.element.ts — ClusterRevision = 4 (HEAD @matter/model 0.16.11).
//
// Deprecated: group membership is stack state. Configure
// endpoint.Config.Groups and the assembler mounts the real
// [github.com/SukramJ/go-fabric/cluster/core.Groups] server on every
// endpoint whose device type mandates it — replacing this stub where a
// source still supplies it (docs/adr/0009). Removal permissible in v0.3.0.
type Groups struct{}

// Cluster ID, revision, the attribute id and the GroupNames feature bit,
// the generated definition's (cluster/spec/groups, ADR 0013). The stub
// serves no command, so it takes nothing else from it: the real server is
// cluster/core's.
const (
	groupsClusterID       = groupsdef.ClusterID
	groupsClusterRevision = groupsdef.Revision

	groupsAttrNameSupport = groupsdef.AttrNameSupport
	groupsFeatureGN       = uint32(groupsdef.FeatureGroupNames)
)

// errGroupsReadOnly surfaces from Write / Invoke on the Groups stub.
var errGroupsReadOnly = errors.New("matter: Groups cluster is a read-only stub")

// Compile-time assertions.
var (
	_ contract.ClusterServer          = Groups{}
	_ contract.ClusterAttributeLister = Groups{}
)

// MatterClusterID implements [contract.ClusterServer].
func (Groups) MatterClusterID() uint32 { return groupsClusterID }

// MatterRead implements [contract.ClusterServer]. Only the
// mandatory NameSupport bitmap8 + the global FeatureMap /
// ClusterRevision are exposed.
func (Groups) MatterRead(attrID uint32) (any, bool) {
	switch attrID {
	case groupsAttrNameSupport:
		// NameSupport bitmap8 bit 7 (0x80) mandates GroupNames support.
		// matter.js groups.element.ts:31 declares the field with default
		// bit 7 set under M conformance; chip's GroupsCluster.cpp:228
		// advertises 0x80 unconditionally and its test asserts the bit.
		return uint8(0x80), true
	case cluster.AttrGlobalFeatureMap:
		// FeatureMap bit 0 = GN (GroupNames). matter.js
		// groups.element.ts:23 declares the bit with M conformance and
		// default 1; chip's GroupsCluster.cpp:223 encodes
		// Feature::kGroupNames unconditionally.
		return groupsFeatureGN, true
	case cluster.AttrGlobalClusterRevision:
		return groupsClusterRevision, true
	}
	return nil, false
}

// MatterWrite rejects every write — Groups is a read-only stub.
func (Groups) MatterWrite(_ context.Context, attrID uint32, _ any) error {
	return fmt.Errorf("%w: attrID 0x%04X", errGroupsReadOnly, attrID)
}

// MatterInvoke rejects every command. The bridge dispatcher maps errors
// whose message contains "no commands" to IM StatusCode UnsupportedCommand
// (0x81). Returning errGroupsReadOnly alone would fall through to StatusFailure
// (0x01); wrapping with the "no commands" sentinel ensures the controller
// receives the correct Matter status code.
// matter.js packages/node/src/behaviors/groups/GroupsServer.ts + chip
// src/app/clusters/groups-server/groups-server.cpp both require a valid
// status-code response for unsupported commands on a stub cluster.
func (Groups) MatterInvoke(_ context.Context, cmdID uint32, _ any) (any, error) {
	return nil, fmt.Errorf("%w: no commands supported (HM has no group management), cmdID 0x%02X", errGroupsReadOnly, cmdID)
}

// MatterReportable returns nil — no subscribe-able attributes.
func (Groups) MatterReportable() []uint32 { return nil }

// MatterAttributes lists the mandatory Groups attributes.
func (Groups) MatterAttributes() []uint32 {
	return []uint32{groupsAttrNameSupport}
}

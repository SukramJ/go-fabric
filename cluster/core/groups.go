// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"unicode/utf16"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	groupsdef "github.com/SukramJ/go-fabric/cluster/spec/groups"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/groups"
	"github.com/SukramJ/go-fabric/im"
)

// Groups cluster (0x0004) identifiers: the generated definition's
// (cluster/spec/groups, ADR 0013, from matter.js groups.element.ts). The
// attribute and command lists, FeatureMap, ClusterRevision, the invoke
// privileges and the statuses of a refused write come from it as well. The
// requests keep the bridge's hand-written decoders into the request
// structs below (notes/parity/by_design.md BD-Matter-Groups-HandDecoders);
// the group table is the groups.Manager's.
const (
	// GroupsClusterID is the Groups cluster id.
	GroupsClusterID = groupsdef.ClusterID

	groupsAttrNameSupport = groupsdef.AttrNameSupport

	groupsCmdAddGroup              = groupsdef.CmdAddGroup
	groupsCmdViewGroup             = groupsdef.CmdViewGroup
	groupsCmdGetGroupMembership    = groupsdef.CmdGetGroupMembership
	groupsCmdRemoveGroup           = groupsdef.CmdRemoveGroup
	groupsCmdRemoveAllGroups       = groupsdef.CmdRemoveAllGroups
	groupsCmdAddGroupIfIdentifying = groupsdef.CmdAddGroupIfIdentifying

	// groupsNameSupportGroupNames is NameSupportBitmap bit 7 (GroupNames).
	groupsNameSupportGroupNames = uint8(groupsdef.NameSupportGroupNames)
	// groupsFeatureGroupNames is FeatureMap bit 0 (GN).
	groupsFeatureGroupNames = uint32(groupsdef.FeatureGroupNames)
	// groupsMaxNameLength is GroupName's "max 16" constraint.
	groupsMaxNameLength = 16
	// groupsCapacityBase is the 0xFE matter.js reports Capacity against.
	groupsCapacityBase = 0xFE
)

// AddGroupRequest is Groups AddGroup (Matter §1.3.7.1).
type AddGroupRequest struct {
	GroupID   uint16
	GroupName string
}

// AddGroupResponse is Groups AddGroupResponse (Matter §1.3.7.7).
type AddGroupResponse struct {
	Status  im.StatusCode
	GroupID uint16
}

// ViewGroupRequest is Groups ViewGroup (Matter §1.3.7.2).
type ViewGroupRequest struct {
	GroupID uint16
}

// ViewGroupResponse is Groups ViewGroupResponse (Matter §1.3.7.8).
type ViewGroupResponse struct {
	Status    im.StatusCode
	GroupID   uint16
	GroupName string
}

// GetGroupMembershipRequest is Groups GetGroupMembership (Matter §1.3.7.3).
type GetGroupMembershipRequest struct {
	GroupList []uint16
}

// GetGroupMembershipResponse is Groups GetGroupMembershipResponse (Matter
// §1.3.7.9). Capacity is nullable on the wire; this server always reports
// a value, as matter.js does.
type GetGroupMembershipResponse struct {
	Capacity  uint8
	GroupList []uint16
}

// RemoveGroupRequest is Groups RemoveGroup (Matter §1.3.7.4).
type RemoveGroupRequest struct {
	GroupID uint16
}

// RemoveGroupResponse is Groups RemoveGroupResponse (Matter §1.3.7.10).
type RemoveGroupResponse struct {
	Status  im.StatusCode
	GroupID uint16
}

// AddGroupIfIdentifyingRequest is Groups AddGroupIfIdentifying (Matter
// §1.3.7.6).
type AddGroupIfIdentifyingRequest struct {
	GroupID   uint16
	GroupName string
}

// groupsStatusError carries the status a Groups command answers with when
// matter.js throws a StatusResponseError instead of returning a response.
type groupsStatusError struct {
	code im.StatusCode
	msg  string
}

func (e groupsStatusError) Error() string { return "matter: Groups: " + e.msg }

// MatterStatusCode implements [im.StatusCodeError].
func (e groupsStatusError) MatterStatusCode() im.StatusCode { return e.code }

// Groups is the Groups cluster server of one endpoint. Group membership is
// stack state kept by a [groups.Manager] shared by every endpoint and by
// GroupKeyManagement; this server only reads and changes it for its own
// endpoint, so a fresh instance per dispatch serves as well as a long-lived
// one.
//
// Mirrors matter.js packages/node/src/behaviors/groups/GroupsServer.ts,
// which enables the GroupNames feature by default and keeps membership in
// the root's GroupKeyManagementServer (addEndpointForGroup /
// removeEndpoint). Groupcast adoption (Groups cluster rev 5 INVALID_IN_STATE
// paths) is inert there with the default GroupKeyManagementServer and is
// not modelled here.
type Groups struct {
	endpoint     uint16
	groups       *groups.Manager
	identifyTime func() uint16
	// scenes is the endpoint's scene table, whose scenes of a removed
	// group go with it (matter.js GroupsServer → ScenesManagementServer
	// removeScenesForGroupOnFabric / removeScenesForAllGroupsForFabric).
	scenes *ScenesState
}

// SetScenes ties the endpoint's scene table to its groups: removing a group
// removes the fabric's scenes of that group.
func (s *Groups) SetScenes(st *ScenesState) { s.scenes = st }

// NewGroups returns the Groups server of endpoint. identifyTime reports the
// endpoint's Identify.IdentifyTime and may be nil (never identifying).
func NewGroups(endpoint uint16, m *groups.Manager, identifyTime func() uint16) (*Groups, error) {
	if m == nil {
		return nil, errors.New("matter: Groups: group manager is required")
	}
	return &Groups{endpoint: endpoint, groups: m, identifyTime: identifyTime}, nil
}

// groupsInst is the definition bound to GN, the feature the default
// GroupsServer enables.
var groupsInst = mustInstance(groupsdef.Definition, spec.Options{Features: groupsFeatureGroupNames})

// Compile-time assertions.
var (
	_ contract.ClusterServer                 = (*Groups)(nil)
	_ contract.ClusterAttributeLister        = (*Groups)(nil)
	_ contract.ClusterCommandLister          = (*Groups)(nil)
	_ contract.ClusterCommandInvokePrivilege = (*Groups)(nil)
)

// MatterClusterID implements [contract.ClusterServer].
func (s *Groups) MatterClusterID() uint32 { return GroupsClusterID }

// MatterRead implements [contract.ClusterServer].
func (s *Groups) MatterRead(attrID uint32) (any, bool) {
	switch attrID {
	case groupsAttrNameSupport:
		// GroupsServer.initialize: nameSupport.groupNames follows the
		// GroupNames feature, which the default server enables.
		return groupsNameSupportGroupNames, true
	case cluster.AttrGlobalFeatureMap, cluster.AttrGlobalClusterRevision:
		return groupsInst.ReadGlobal(attrID)
	case cluster.AttrGlobalAcceptedCommandList:
		return s.MatterAcceptedCommands(), true
	case cluster.AttrGlobalGeneratedCommandList:
		return s.MatterGeneratedCommands(), true
	case cluster.AttrGlobalAttributeList:
		return append(
			groupsInst.MatterAttributes(),
			cluster.AttrGlobalGeneratedCommandList,
			cluster.AttrGlobalAcceptedCommandList,
			cluster.AttrGlobalAttributeList,
			cluster.AttrGlobalFeatureMap,
			cluster.AttrGlobalClusterRevision,
		), true
	}
	return nil, false
}

// MatterWrite implements [contract.ClusterServer]: Groups has no writable
// attribute.
func (s *Groups) MatterWrite(_ context.Context, attrID uint32, value any) error {
	_, err := groupsInst.ValidateWrite(attrID, value, nil)
	return refusedWrite{sentinel: fmt.Errorf("matter: Groups attribute 0x%04X is read-only", attrID), status: err}
}

// MatterReportable implements [contract.ClusterServer].
func (s *Groups) MatterReportable() []uint32 { return []uint32{groupsAttrNameSupport} }

// MatterAttributes implements [contract.ClusterAttributeLister].
func (s *Groups) MatterAttributes() []uint32 { return groupsInst.MatterAttributes() }

// MatterAcceptedCommands implements [contract.ClusterCommandLister].
func (s *Groups) MatterAcceptedCommands() []uint32 { return groupsInst.MatterAcceptedCommands() }

// MatterGeneratedCommands implements [contract.ClusterCommandLister]: the
// four responses share their request's id.
func (s *Groups) MatterGeneratedCommands() []uint32 { return groupsInst.MatterGeneratedCommands() }

// MinInvokePrivilege implements [contract.ClusterCommandInvokePrivilege]:
// the definition's — AddGroup, RemoveGroup, RemoveAllGroups and
// AddGroupIfIdentifying need Manage, ViewGroup and GetGroupMembership
// Operate (access "F M" / "F O").
func (s *Groups) MinInvokePrivilege(cmdID uint32) uint8 { return groupsInst.MinInvokePrivilege(cmdID) }

// MatterInvoke implements [contract.ClusterServer]. Every command is
// fabric-scoped; without an accessing fabric it answers UnsupportedAccess,
// the status matter.js's invoke path gives a fabric-scoped command on a
// session without one.
func (s *Groups) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	_, fabric := im.FabricFilterFromContext(ctx)
	if fabric == 0 {
		return nil, groupsStatusError{im.StatusUnsupportedAccess, "command requires an accessing fabric"}
	}
	switch cmdID {
	case groupsCmdAddGroup:
		req, ok := fields.(AddGroupRequest)
		if !ok {
			return nil, groupsStatusError{im.StatusInvalidCommand, fmt.Sprintf("AddGroupRequest expected, got %T", fields)}
		}
		return s.addGroup(ctx, fabric, req.GroupID, req.GroupName)
	case groupsCmdViewGroup:
		req, ok := fields.(ViewGroupRequest)
		if !ok {
			return nil, groupsStatusError{im.StatusInvalidCommand, fmt.Sprintf("ViewGroupRequest expected, got %T", fields)}
		}
		return s.viewGroup(ctx, fabric, req.GroupID)
	case groupsCmdGetGroupMembership:
		req, ok := fields.(GetGroupMembershipRequest)
		if !ok {
			return nil, groupsStatusError{im.StatusInvalidCommand, fmt.Sprintf("GetGroupMembershipRequest expected, got %T", fields)}
		}
		return s.getGroupMembership(ctx, fabric, req.GroupList)
	case groupsCmdRemoveGroup:
		req, ok := fields.(RemoveGroupRequest)
		if !ok {
			return nil, groupsStatusError{im.StatusInvalidCommand, fmt.Sprintf("RemoveGroupRequest expected, got %T", fields)}
		}
		return s.removeGroup(ctx, fabric, req.GroupID)
	case groupsCmdRemoveAllGroups:
		return s.removeAllGroups(ctx, fabric)
	case groupsCmdAddGroupIfIdentifying:
		req, ok := fields.(AddGroupIfIdentifyingRequest)
		if !ok {
			return nil, groupsStatusError{im.StatusInvalidCommand, fmt.Sprintf("AddGroupIfIdentifyingRequest expected, got %T", fields)}
		}
		return s.addGroupIfIdentifying(ctx, fabric, req.GroupID, req.GroupName)
	}
	return nil, im.UnsupportedCommandf("matter: Groups command 0x%02X not supported", cmdID)
}

// nameLength is the length matter.js checks GroupName against: a JS string
// length, i.e. UTF-16 code units.
func nameLength(name string) int { return len(utf16.Encode([]rune(name))) }

// addGroup mirrors GroupsServer.addGroup: GroupId 0 and a name longer than
// 16 answer ConstraintError in the response (the server relaxes the model
// constraints so it can), a group without a GroupKeyMap entry
// UnsupportedAccess, a fabric at MaxGroupsPerFabric ResourceExhausted.
func (s *Groups) addGroup(ctx context.Context, fabric uint8, groupID uint16, name string) (any, error) {
	if groupID < 1 || nameLength(name) > groupsMaxNameLength {
		return AddGroupResponse{Status: im.StatusConstraintError, GroupID: groupID}, nil
	}
	mapped, err := s.groups.HasKeyMapping(ctx, fabric, groupID)
	if err != nil {
		return nil, fmt.Errorf("matter: Groups.AddGroup: %w", err)
	}
	if !mapped {
		return AddGroupResponse{Status: im.StatusUnsupportedAccess, GroupID: groupID}, nil
	}
	if err := s.groups.AddEndpointForGroup(ctx, fabric, groupID, s.endpoint, name); err != nil {
		if errors.Is(err, groups.ErrResourceExhausted) {
			return AddGroupResponse{Status: im.StatusResourceExhausted, GroupID: groupID}, nil
		}
		return nil, fmt.Errorf("matter: Groups.AddGroup: %w", err)
	}
	return AddGroupResponse{Status: im.StatusSuccess, GroupID: groupID}, nil
}

// viewGroup mirrors GroupsServer.viewGroup.
func (s *Groups) viewGroup(ctx context.Context, fabric uint8, groupID uint16) (any, error) {
	if groupID < 1 {
		return ViewGroupResponse{Status: im.StatusConstraintError, GroupID: groupID}, nil
	}
	table, err := s.groups.GroupTable(ctx, fabric)
	if err != nil {
		return nil, fmt.Errorf("matter: Groups.ViewGroup: %w", err)
	}
	for _, e := range table {
		if e.GroupID == groupID && slices.Contains(e.Endpoints, s.endpoint) {
			return ViewGroupResponse{Status: im.StatusSuccess, GroupID: groupID, GroupName: e.GroupName}, nil
		}
	}
	return ViewGroupResponse{Status: im.StatusNotFound, GroupID: groupID}, nil
}

// getGroupMembership mirrors GroupsServer.getGroupMembership: an empty
// GroupList asks for every group of the endpoint, a non-empty one for the
// listed groups the endpoint is in (in request order); Capacity is 0xFE
// minus the endpoint's group count. A listed group id 0 violates the
// model's "all[min 1]" constraint, which this command keeps.
func (s *Groups) getGroupMembership(ctx context.Context, fabric uint8, list []uint16) (any, error) {
	if slices.Contains(list, 0) {
		return nil, groupsStatusError{im.StatusConstraintError, "GetGroupMembership: GroupList entries must be at least 1"}
	}
	table, err := s.groups.GroupTable(ctx, fabric)
	if err != nil {
		return nil, fmt.Errorf("matter: Groups.GetGroupMembership: %w", err)
	}
	member := make([]uint16, 0, len(table))
	for _, e := range table {
		if slices.Contains(e.Endpoints, s.endpoint) {
			member = append(member, e.GroupID)
		}
	}
	capacity := uint8(groupsCapacityBase - min(len(member), groupsCapacityBase)) //nolint:gosec // bounded to 0..0xFE
	if len(list) == 0 {
		return GetGroupMembershipResponse{Capacity: capacity, GroupList: member}, nil
	}
	filtered := make([]uint16, 0, len(list))
	for _, gid := range list {
		if slices.Contains(member, gid) {
			filtered = append(filtered, gid)
		}
	}
	return GetGroupMembershipResponse{Capacity: capacity, GroupList: filtered}, nil
}

// removeGroup mirrors GroupsServer.removeGroup. Scenes of the group would
// be removed with it (removeScenesForGroupOnFabric); the ScenesManagement
// stub keeps no scenes, so there is nothing to remove.
func (s *Groups) removeGroup(ctx context.Context, fabric uint8, groupID uint16) (any, error) {
	if groupID < 1 {
		return RemoveGroupResponse{Status: im.StatusConstraintError, GroupID: groupID}, nil
	}
	existed, err := s.groups.RemoveEndpoint(ctx, fabric, s.endpoint, groupID, false)
	if err != nil {
		return nil, fmt.Errorf("matter: Groups.RemoveGroup: %w", err)
	}
	if !existed {
		return RemoveGroupResponse{Status: im.StatusNotFound, GroupID: groupID}, nil
	}
	if s.scenes != nil {
		s.scenes.RemoveScenesForGroup(fabric, groupID)
	}
	return RemoveGroupResponse{Status: im.StatusSuccess, GroupID: groupID}, nil
}

// removeAllGroups mirrors GroupsServer.removeAllGroups.
func (s *Groups) removeAllGroups(ctx context.Context, fabric uint8) (any, error) {
	if _, err := s.groups.RemoveEndpoint(ctx, fabric, s.endpoint, 0, true); err != nil {
		return nil, fmt.Errorf("matter: Groups.RemoveAllGroups: %w", err)
	}
	if s.scenes != nil {
		s.scenes.RemoveScenesForFabric(fabric)
	}
	return nil, nil
}

// addGroupIfIdentifying mirrors GroupsServer.addGroupIfIdentifying: while
// the endpoint identifies it adds the group and turns a non-success
// AddGroup status into the command's status; otherwise it does nothing and
// succeeds.
func (s *Groups) addGroupIfIdentifying(ctx context.Context, fabric uint8, groupID uint16, name string) (any, error) {
	if s.identifyTime == nil || s.identifyTime() == 0 {
		return nil, nil
	}
	resp, err := s.addGroup(ctx, fabric, groupID, name)
	if err != nil {
		return nil, err
	}
	if r, ok := resp.(AddGroupResponse); ok && r.Status != im.StatusSuccess {
		return nil, groupsStatusError{r.Status, fmt.Sprintf("failed to add group %d", groupID)}
	}
	return nil, nil
}

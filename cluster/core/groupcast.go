// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/groups"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/schema"
)

// Groupcast cluster (0x0065) identifiers, groupcast.element.ts.
const (
	// GroupcastClusterID is the Groupcast cluster id.
	GroupcastClusterID uint32 = 0x0065

	groupcastAttrMembership         uint32 = 0x0000
	groupcastAttrMaxMembershipCount uint32 = 0x0001
	groupcastAttrMaxMcastAddrCount  uint32 = 0x0002
	groupcastAttrUsedMcastAddrCount uint32 = 0x0003
	groupcastAttrFabricUnderTest    uint32 = 0x0004

	groupcastCmdJoinGroup             uint32 = 0x00
	groupcastCmdLeaveGroup            uint32 = 0x01
	groupcastCmdLeaveGroupResponse    uint32 = 0x02
	groupcastCmdUpdateGroupKey        uint32 = 0x03
	groupcastCmdConfigureAuxiliaryACL uint32 = 0x04
	groupcastCmdGroupcastTesting      uint32 = 0x05

	groupcastEventGroupcastTesting uint32 = 0x00

	// Feature bits: Listener (LN) 0, Sender (SD) 1, PerGroup (PGA) 2.
	groupcastFeatureListener uint32 = 1 << 0
	groupcastFeaturePerGroup uint32 = 1 << 2

	// MaxMembershipCount / MaxMcastAddrCount: matter.js GroupcastServer
	// State (44 = 2 × GroupKeyManagement maxGroupsPerFabric, so the
	// per-fabric quota floor(44/2) equals that cap).
	groupcastMaxMembershipCount uint16 = 44
	groupcastMaxMcastAddrCount  uint16 = 44

	// groupcastMaxGroupID is the highest group id JoinGroup accepts.
	groupcastMaxGroupID uint16 = 0xFFF7
	// groupcastKeyLength is the "16" constraint of the Key fields.
	groupcastKeyLength = 16
	// groupcastLeaveMaxEndpoints is LeaveGroup.Endpoints' "1 to 20".
	groupcastLeaveMaxEndpoints = 20

	// GroupcastTestingEnum values.
	groupcastTestingDisable        uint8 = 0
	groupcastTestingEnableListener uint8 = 1
	groupcastTestingEnableSender   uint8 = 2

	// DurationSeconds: "10 to 1200", default 60 (matter.js
	// DEFAULT_TESTING_DURATION_SECONDS, 452d6f5c).
	groupcastTestingDefaultSeconds uint16 = 60
	groupcastTestingMinSeconds     uint16 = 10
	groupcastTestingMaxSeconds     uint16 = 1200
)

// JoinGroupRequest is Groupcast JoinGroup (core§11.27.7.1). A nil pointer
// or Key is a field the request left out.
type JoinGroupRequest struct {
	GroupID          uint16
	Endpoints        []uint16
	KeySetID         uint16
	Key              []byte
	UseAuxiliaryACL  *bool
	ReplaceEndpoints *bool
	McastAddrPolicy  *uint8
}

// LeaveGroupRequest is Groupcast LeaveGroup (core§11.27.7.2). HasEndpoints
// tells an Endpoints list the request carried from one it left out.
type LeaveGroupRequest struct {
	GroupID      uint16
	Endpoints    []uint16
	HasEndpoints bool
}

// LeaveGroupResponse is Groupcast LeaveGroupResponse (core§11.27.7.3).
type LeaveGroupResponse struct {
	GroupID   uint16
	Endpoints []uint16
}

// UpdateGroupKeyRequest is Groupcast UpdateGroupKey (core§11.27.7.4).
type UpdateGroupKeyRequest struct {
	GroupID  uint16
	KeySetID uint16
	Key      []byte
}

// ConfigureAuxiliaryACLRequest is Groupcast ConfigureAuxiliaryAcl
// (core§11.27.7.5).
type ConfigureAuxiliaryACLRequest struct {
	GroupID         uint16
	UseAuxiliaryACL bool
}

// GroupcastTestingRequest is Groupcast GroupcastTesting (core§11.27.7.6).
type GroupcastTestingRequest struct {
	TestOperation   uint8
	DurationSeconds *uint16
}

// GroupcastMembershipStruct is one Membership entry (MembershipStruct). A
// nil KeySetID is the fabric-sensitive field withheld from a session of
// another fabric.
type GroupcastMembershipStruct struct {
	GroupID         uint16
	Endpoints       []uint16
	KeySetID        *uint16
	HasAuxiliaryACL bool
	McastAddrPolicy uint8
	FabricIndex     uint8
}

// GroupcastTestingEvent is the GroupcastTesting event (access "S A"). Every
// field but GroupcastTestResult and FabricIndex is optional; nil leaves it
// out. The two addresses are 16-byte IPv6 addresses.
type GroupcastTestingEvent struct {
	SourceIPAddress      []byte
	DestinationIPAddress []byte
	GroupID              *uint16
	EndpointID           *uint16
	ClusterID            *uint32
	ElementID            *uint32
	AccessAllowed        *bool
	GroupcastTestResult  uint8
	FabricIndex          uint8
}

// groupcastStatusError carries the status a Groupcast command answers with
// (a StatusResponseError in matter.js).
type groupcastStatusError struct {
	code im.StatusCode
	msg  string
}

func (e groupcastStatusError) Error() string { return "matter: Groupcast: " + e.msg }

// MatterStatusCode implements [im.StatusCodeError].
func (e groupcastStatusError) MatterStatusCode() im.StatusCode { return e.code }

var _ im.StatusCodeError = groupcastStatusError{}

func groupcastErr(code im.StatusCode, format string, args ...any) error {
	return groupcastStatusError{code: code, msg: fmt.Sprintf(format, args...)}
}

// GroupcastConfig drives [NewGroupcast].
type GroupcastConfig struct {
	// Groups is the node's group state — the instance GroupKeyManagement
	// and the Groups servers use. Required.
	Groups *groups.Manager
	// GroupKeyManagement is the root's GroupKeyManagement server,
	// configured with the same Groups. Required: Groupcast creates key
	// sets and GroupKeyMap bindings through it.
	GroupKeyManagement *GroupKeyManagement
	// AccessControl is the root's AccessControl server. Required: the
	// Listener feature needs its Auxiliary feature, which NewGroupcast
	// turns on with the group state as the provider of the auxiliary
	// entries.
	AccessControl *AccessControl
	// TestingTimer schedules the end of a GroupcastTesting period and
	// returns its cancellation; nil uses time.AfterFunc. A seam for tests.
	TestingTimer func(d time.Duration, fire func()) (stop func())
}

// Groupcast is the Groupcast cluster server of the root endpoint, the
// counterpart of matter.js packages/node/src/behaviors/groupcast/
// GroupcastServer.ts as ServerNode.RootEndpoint installs it — with the
// Listener and PerGroup features but without Sender (see
// notes/parity/by_design.md BD-Matter-GroupcastNoSender): this node
// receives group messages and originates none.
//
// It keeps no group state of its own. Membership is derived from the
// shared [groups.Manager] (Groupcast properties, group table, GroupKeyMap),
// key sets and bindings go through GroupKeyManagement, and the auxiliary
// access control entries reach AccessControl through the same manager.
type Groupcast struct {
	groups *groups.Manager
	gkm    *GroupKeyManagement
	acl    *AccessControl
	timer  func(d time.Duration, fire func()) (stop func())

	// cmdMu serialises the commands: each one reads Membership, checks it
	// and then changes several sources, as one matter.js transaction does.
	cmdMu sync.Mutex

	mu              sync.RWMutex
	endpoint        uint16
	emitter         contract.EventEmitter
	fabricUnderTest uint8
	testingGen      uint64
	stopTesting     func()
	unlisten        func()
	notifiers       map[uint64]func()
	nextNotifier    uint64

	dataVersion cluster.DataVersionTracker
}

// Compile-time assertions.
var (
	_ contract.ClusterServer                 = (*Groupcast)(nil)
	_ contract.FabricScopedReader            = (*Groupcast)(nil)
	_ contract.ClusterAttributeLister        = (*Groupcast)(nil)
	_ contract.ClusterCommandLister          = (*Groupcast)(nil)
	_ contract.ClusterEventLister            = (*Groupcast)(nil)
	_ contract.ClusterCommandInvokePrivilege = (*Groupcast)(nil)
	_ contract.ClusterDataVersion            = (*Groupcast)(nil)
	_ contract.EventReceiver                 = (*Groupcast)(nil)
	_ contract.ChangeNotifier                = (*Groupcast)(nil)
)

// NewGroupcast returns the Groupcast server and registers the group state as
// AccessControl's auxiliary-ACL provider — the Listener feature requires
// the Auxiliary feature, which matter.js GroupcastServer #online enforces
// with an ImplementationError and AccessControlServer.registerAuxAclProvider
// wires.
func NewGroupcast(cfg GroupcastConfig) (*Groupcast, error) {
	switch {
	case cfg.Groups == nil:
		return nil, errors.New("matter: Groupcast: group state is required")
	case cfg.GroupKeyManagement == nil:
		return nil, errors.New("matter: Groupcast: GroupKeyManagement is required")
	case cfg.GroupKeyManagement.groups != cfg.Groups:
		return nil, errors.New("matter: Groupcast: GroupKeyManagement must share the Groupcast group state")
	case cfg.AccessControl == nil:
		return nil, errors.New("matter: Groupcast: the Listener feature requires AccessControl with the Auxiliary feature")
	}
	g := &Groupcast{groups: cfg.Groups, gkm: cfg.GroupKeyManagement, acl: cfg.AccessControl, timer: cfg.TestingTimer}
	if g.timer == nil {
		g.timer = func(d time.Duration, fire func()) func() {
			t := time.AfterFunc(d, fire)
			return func() { t.Stop() }
		}
	}
	g.dataVersion.Bump()
	g.acl.registerAuxiliaryProvider(context.Background(), cfg.Groups)
	cfg.Groups.OnGroupcastChanged(func(context.Context, uint8) { g.changed() })
	return g, nil
}

// changed bumps the DataVersion and tells the subscribers that Membership
// or a count may have moved.
func (g *Groupcast) changed() {
	g.dataVersion.Bump()
	g.mu.RLock()
	fns := make([]func(), 0, len(g.notifiers))
	for _, fn := range g.notifiers {
		fns = append(fns, fn)
	}
	g.mu.RUnlock()
	for _, fn := range fns {
		fn()
	}
}

// OnMatterValueChanged implements [contract.ChangeNotifier]: cb fires after
// Membership, UsedMcastAddrCount or FabricUnderTest changed.
func (g *Groupcast) OnMatterValueChanged(cb func()) (unsubscribe func()) {
	if cb == nil {
		return func() {}
	}
	g.mu.Lock()
	if g.notifiers == nil {
		g.notifiers = make(map[uint64]func())
	}
	g.nextNotifier++
	id := g.nextNotifier
	g.notifiers[id] = cb
	g.mu.Unlock()
	return func() {
		g.mu.Lock()
		delete(g.notifiers, id)
		g.mu.Unlock()
	}
}

// MatterClusterID implements [contract.ClusterServer].
func (g *Groupcast) MatterClusterID() uint32 { return GroupcastClusterID }

// MatterDataVersion implements [contract.ClusterDataVersion].
func (g *Groupcast) MatterDataVersion() uint32 { return g.dataVersion.Current() }

// SetMatterEventEmitter implements [contract.EventReceiver].
func (g *Groupcast) SetMatterEventEmitter(emitter contract.EventEmitter) {
	g.mu.Lock()
	g.emitter = emitter
	g.mu.Unlock()
}

// SetEndpoint stamps the endpoint the server is mounted on (the root).
func (g *Groupcast) SetEndpoint(endpoint uint16) {
	g.mu.Lock()
	g.endpoint = endpoint
	g.mu.Unlock()
}

// featureMap is Listener | PerGroup.
func (*Groupcast) featureMap() uint32 { return groupcastFeatureListener | groupcastFeaturePerGroup }

// MatterRead implements [contract.ClusterServer]. A read without an IM
// request behind it is a local one and sees Membership whole.
func (g *Groupcast) MatterRead(attrID uint32) (any, bool) {
	return g.read(context.Background(), attrID, false, 0, true)
}

// MatterReadFiltered implements [contract.FabricScopedReader] for the
// fabric-scoped Membership: a fabric-filtered read sees the accessing
// fabric's entries, an unfiltered one every fabric's with KeySetId (access
// "S") withheld from the others' — matter.js ListManager / StructManager.
func (g *Groupcast) MatterReadFiltered(ctx context.Context, attrID uint32) (any, bool) {
	filtered, fabric := im.FabricFilterFromContext(ctx)
	return g.read(ctx, attrID, filtered, fabric, false)
}

func (g *Groupcast) read(ctx context.Context, attrID uint32, filtered bool, fabric uint8, local bool) (any, bool) {
	switch attrID {
	case groupcastAttrMembership:
		ms, err := g.groups.GroupcastMemberships(ctx)
		if err != nil {
			return nil, false
		}
		out := []GroupcastMembershipStruct{}
		for _, m := range ms {
			if filtered && !local && m.FabricIndex != fabric {
				continue
			}
			e := GroupcastMembershipStruct{
				GroupID: m.GroupID, Endpoints: m.Endpoints, HasAuxiliaryACL: m.HasAuxiliaryACL,
				McastAddrPolicy: m.McastAddrPolicy, FabricIndex: m.FabricIndex,
			}
			if local || m.FabricIndex == fabric {
				ks := m.KeySetID
				e.KeySetID = &ks
			}
			out = append(out, e)
		}
		return out, true
	case groupcastAttrMaxMembershipCount:
		return groupcastMaxMembershipCount, true
	case groupcastAttrMaxMcastAddrCount:
		return groupcastMaxMcastAddrCount, true
	case groupcastAttrUsedMcastAddrCount:
		ms, err := g.groups.GroupcastMemberships(ctx)
		if err != nil {
			return nil, false
		}
		return usedMcastAddrCount(ms), true
	case groupcastAttrFabricUnderTest:
		g.mu.RLock()
		defer g.mu.RUnlock()
		return g.fabricUnderTest, true
	case cluster.AttrGlobalFeatureMap:
		return g.featureMap(), true
	case cluster.AttrGlobalClusterRevision:
		rev, _ := schema.ClusterRevision(GroupcastClusterID)
		return rev, true
	}
	return nil, false
}

// usedMcastAddrCount counts the multicast addresses a membership list
// needs: one per PerGroup group, one shared by every IanaAddr group.
// Mirrors matter.js GroupcastServer #computeUsedMcastAddrCount.
func usedMcastAddrCount(ms []groups.GroupcastMembership) uint16 {
	var perGroup uint16
	iana := false
	for _, m := range ms {
		switch m.McastAddrPolicy {
		case groups.PolicyPerGroup:
			perGroup++
		case groups.PolicyIanaAddr:
			iana = true
		}
	}
	if iana {
		perGroup++
	}
	return perGroup
}

// MatterWrite implements [contract.ClusterServer]: Groupcast has no
// writable attribute.
func (g *Groupcast) MatterWrite(_ context.Context, attrID uint32, _ any) error {
	return fmt.Errorf("matter: Groupcast attribute 0x%04X is read-only", attrID)
}

// MatterReportable implements [contract.ClusterServer].
func (g *Groupcast) MatterReportable() []uint32 {
	return []uint32{
		groupcastAttrMembership, groupcastAttrMaxMembershipCount, groupcastAttrMaxMcastAddrCount,
		groupcastAttrUsedMcastAddrCount, groupcastAttrFabricUnderTest,
	}
}

// MatterAttributes implements [contract.ClusterAttributeLister].
func (g *Groupcast) MatterAttributes() []uint32 { return g.MatterReportable() }

// MatterAcceptedCommands implements [contract.ClusterCommandLister]:
// ConfigureAuxiliaryAcl has conformance LN.
func (g *Groupcast) MatterAcceptedCommands() []uint32 {
	return []uint32{
		groupcastCmdJoinGroup, groupcastCmdLeaveGroup, groupcastCmdUpdateGroupKey,
		groupcastCmdConfigureAuxiliaryACL, groupcastCmdGroupcastTesting,
	}
}

// MatterGeneratedCommands implements [contract.ClusterCommandLister].
func (g *Groupcast) MatterGeneratedCommands() []uint32 {
	return []uint32{groupcastCmdLeaveGroupResponse}
}

// MatterEvents implements [contract.ClusterEventLister].
func (g *Groupcast) MatterEvents() []uint32 { return []uint32{groupcastEventGroupcastTesting} }

// MinInvokePrivilege implements [contract.ClusterCommandInvokePrivilege]:
// JoinGroup, LeaveGroup and UpdateGroupKey need Manage ("F M"),
// ConfigureAuxiliaryAcl and GroupcastTesting Administer ("F A").
func (g *Groupcast) MinInvokePrivilege(cmdID uint32) uint8 {
	switch cmdID {
	case groupcastCmdConfigureAuxiliaryACL, groupcastCmdGroupcastTesting:
		return 5
	default:
		return 4
	}
}

// MatterInvoke implements [contract.ClusterServer]. Every command is
// fabric-scoped; without an accessing fabric it answers UnsupportedAccess.
func (g *Groupcast) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	_, fabric := im.FabricFilterFromContext(ctx)
	if fabric == 0 {
		return nil, groupcastErr(im.StatusUnsupportedAccess, "command requires an accessing fabric")
	}
	g.cmdMu.Lock()
	defer g.cmdMu.Unlock()
	g.acl.beginAuxiliaryBatch()
	defer g.acl.endAuxiliaryBatch(ctx)
	switch cmdID {
	case groupcastCmdJoinGroup:
		req, ok := fields.(JoinGroupRequest)
		if !ok {
			return nil, groupcastErr(im.StatusInvalidCommand, "JoinGroupRequest expected, got %T", fields)
		}
		return nil, g.joinGroup(ctx, fabric, req)
	case groupcastCmdLeaveGroup:
		req, ok := fields.(LeaveGroupRequest)
		if !ok {
			return nil, groupcastErr(im.StatusInvalidCommand, "LeaveGroupRequest expected, got %T", fields)
		}
		return g.leaveGroup(ctx, fabric, req)
	case groupcastCmdUpdateGroupKey:
		req, ok := fields.(UpdateGroupKeyRequest)
		if !ok {
			return nil, groupcastErr(im.StatusInvalidCommand, "UpdateGroupKeyRequest expected, got %T", fields)
		}
		return nil, g.updateGroupKey(ctx, fabric, req)
	case groupcastCmdConfigureAuxiliaryACL:
		req, ok := fields.(ConfigureAuxiliaryACLRequest)
		if !ok {
			return nil, groupcastErr(im.StatusInvalidCommand, "ConfigureAuxiliaryAclRequest expected, got %T", fields)
		}
		return nil, g.configureAuxiliaryACL(ctx, fabric, req)
	case groupcastCmdGroupcastTesting:
		req, ok := fields.(GroupcastTestingRequest)
		if !ok {
			return nil, groupcastErr(im.StatusInvalidCommand, "GroupcastTestingRequest expected, got %T", fields)
		}
		return nil, g.groupcastTesting(fabric, req)
	}
	return nil, im.UnsupportedCommandf("matter: Groupcast command 0x%02X not supported", cmdID)
}

// requireAdmin refuses a key or an auxiliary-ACL field without Administer.
// Mirrors matter.js GroupcastServer #requireAdmin ("Admin privilege
// required for key or ACL operations", UnsupportedAccess).
func (g *Groupcast) requireAdmin(ctx context.Context) error {
	g.mu.RLock()
	ep := g.endpoint
	g.mu.RUnlock()
	if !im.AuthorityAt(ctx, ep, GroupcastClusterID, 5).IsSuccess() {
		return groupcastErr(im.StatusUnsupportedAccess, "Admin privilege required for key or ACL operations")
	}
	return nil
}

// memberOf returns the fabric's Membership entry of groupID.
func memberOf(ms []groups.GroupcastMembership, fabric uint8, groupID uint16) (groups.GroupcastMembership, bool) {
	for _, m := range ms {
		if m.FabricIndex == fabric && m.GroupID == groupID {
			return m, true
		}
	}
	return groups.GroupcastMembership{}, false
}

// joinGroup mirrors matter.js GroupcastServer.joinGroup. The model
// constraints come first (GroupId and KeySetId "min 1", Key "16", the
// MulticastAddrPolicyEnum values), then the server's own checks in its
// order. Everything that can still fail is checked before the first change
// lands, so a refused join leaves nothing behind but — as in matter.js,
// whose key store a failed transaction does not roll back — a key set it
// already created.
func (g *Groupcast) joinGroup(ctx context.Context, fabric uint8, req JoinGroupRequest) error {
	if err := g.validateJoin(ctx, req); err != nil {
		return err
	}
	policy := groups.PolicyIanaAddr
	if req.McastAddrPolicy != nil {
		policy = *req.McastAddrPolicy
	}
	ms, err := g.groups.GroupcastMemberships(ctx)
	if err != nil {
		return fmt.Errorf("matter: Groupcast.JoinGroup: %w", err)
	}
	existing, isMember := memberOf(ms, fabric, req.GroupID)
	if err := checkJoinCapacity(ms, fabric, req.GroupID, isMember, policy); err != nil {
		return err
	}

	if err := g.applyKeySet(ctx, fabric, req.KeySetID, req.Key); err != nil {
		return err
	}

	// The group table must take a new group and GroupKeyMap the binding;
	// both refusals are known before anything changes.
	table, err := g.groups.GroupTable(ctx, fabric)
	if err != nil {
		return fmt.Errorf("matter: Groupcast.JoinGroup: %w", err)
	}
	name, inTable := "", false
	for _, e := range table {
		if e.GroupID == req.GroupID {
			name, inTable = e.GroupName, true
		}
	}
	if !inTable && len(table) >= g.groups.MaxGroupsPerFabric() {
		return groupcastErr(im.StatusResourceExhausted, "Too many groups for fabric %d, maximum is %d", fabric, g.groups.MaxGroupsPerFabric())
	}
	if err := g.gkm.checkGroupKeyMapping(ctx, fabric, req.GroupID); err != nil {
		return err
	}

	if err := g.mirrorEndpoints(ctx, fabric, req, existing.Endpoints, name); err != nil {
		return err
	}
	if err := g.gkm.setGroupKeyMapping(ctx, fabric, req.GroupID, req.KeySetID); err != nil {
		return err
	}
	if err := g.groups.SetGroupProperties(ctx, fabric, req.GroupID, &policy, req.UseAuxiliaryACL); err != nil {
		return g.groupStateErr("JoinGroup", err)
	}
	return nil
}

// validateJoin applies the model constraints and the request checks of
// JoinGroup in matter.js's order: GroupId and KeySetId "min 1", Key "16",
// the MulticastAddrPolicyEnum values, then the GroupId range, Administer
// for a key or UseAuxiliaryAcl, the endpoint ids, and — without the Sender
// feature — a non-empty endpoint list.
func (g *Groupcast) validateJoin(ctx context.Context, req JoinGroupRequest) error {
	if req.GroupID < 1 || req.KeySetID < 1 || (req.Key != nil && len(req.Key) != groupcastKeyLength) {
		return groupcastErr(im.StatusConstraintError, "JoinGroup: GroupId and KeySetId must be at least 1, Key 16 bytes")
	}
	if req.McastAddrPolicy != nil && *req.McastAddrPolicy != groups.PolicyIanaAddr && *req.McastAddrPolicy != groups.PolicyPerGroup {
		return groupcastErr(im.StatusConstraintError, "JoinGroup: McastAddrPolicy %d is no MulticastAddrPolicyEnum value", *req.McastAddrPolicy)
	}
	if req.GroupID > groupcastMaxGroupID {
		return groupcastErr(im.StatusConstraintError, "Invalid group ID")
	}
	if req.Key != nil || req.UseAuxiliaryACL != nil {
		if err := g.requireAdmin(ctx); err != nil {
			return err
		}
	}
	for _, ep := range req.Endpoints {
		if ep == 0 || ep > 0xFFFE {
			return groupcastErr(im.StatusUnsupportedEndpoint, "Endpoint %d is invalid", ep)
		}
	}
	if len(req.Endpoints) == 0 {
		return groupcastErr(im.StatusConstraintError, "Empty endpoint list requires Sender feature")
	}
	return nil
}

// checkJoinCapacity applies the membership limits — per fabric
// floor(MaxMembershipCount/2), in total MaxMembershipCount, for a new group
// only — and MaxMcastAddrCount for the address the join would use.
func checkJoinCapacity(ms []groups.GroupcastMembership, fabric uint8, groupID uint16, isMember bool, policy uint8) error {
	if !isMember {
		perFabric := 0
		for _, m := range ms {
			if m.FabricIndex == fabric {
				perFabric++
			}
		}
		if perFabric >= int(groupcastMaxMembershipCount/2) {
			return groupcastErr(im.StatusResourceExhausted, "Per-fabric membership limit reached")
		}
		if len(ms) >= int(groupcastMaxMembershipCount) {
			return groupcastErr(im.StatusResourceExhausted, "Total membership limit reached")
		}
	}
	projected := slices.DeleteFunc(slices.Clone(ms), func(m groups.GroupcastMembership) bool {
		return m.FabricIndex == fabric && m.GroupID == groupID
	})
	projected = append(projected, groups.GroupcastMembership{McastAddrPolicy: policy})
	if n := usedMcastAddrCount(projected); n > groupcastMaxMcastAddrCount && n > usedMcastAddrCount(ms) {
		return groupcastErr(im.StatusResourceExhausted, "MaxMcastAddrCount limit reached")
	}
	return nil
}

// mirrorEndpoints writes the Listener's endpoints into the group table:
// merged with the current ones, or replacing them with ReplaceEndpoints.
// The new endpoints join before the dropped ones leave, so a replace never
// empties the group on the way (which would prune its properties); the end
// state is the one matter.js's remove-then-add produces.
func (g *Groupcast) mirrorEndpoints(ctx context.Context, fabric uint8, req JoinGroupRequest, current []uint16, name string) error {
	for _, ep := range req.Endpoints {
		if err := g.groups.AddEndpointForGroup(ctx, fabric, req.GroupID, ep, name); err != nil {
			return g.groupStateErr("JoinGroup", err)
		}
	}
	if req.ReplaceEndpoints == nil || !*req.ReplaceEndpoints {
		return nil
	}
	for _, ep := range current {
		if slices.Contains(req.Endpoints, ep) {
			continue
		}
		if _, err := g.groups.RemoveEndpoint(ctx, fabric, ep, req.GroupID, false); err != nil {
			return g.groupStateErr("JoinGroup", err)
		}
	}
	return nil
}

// groupStateErr maps a group-state failure to its status.
func (g *Groupcast) groupStateErr(cmd string, err error) error {
	if errors.Is(err, groups.ErrResourceExhausted) {
		return groupcastErr(im.StatusResourceExhausted, "%s: %v", cmd, err)
	}
	return fmt.Errorf("matter: Groupcast.%s: %w", cmd, err)
}

// applyKeySet validates or creates the key set a command names: with a key
// the set must not exist yet and is created, without one it must exist.
// Mirrors matter.js GroupcastServer #applyKeySet.
func (g *Groupcast) applyKeySet(ctx context.Context, fabric uint8, keySetID uint16, key []byte) error {
	exists, err := g.gkm.validateKeySetID(ctx, fabric, keySetID)
	if err != nil {
		return err
	}
	if key != nil {
		if exists {
			return groupcastErr(im.StatusAlreadyExists, "KeySet %d already exists for fabric", keySetID)
		}
		return g.gkm.createKeySetForGroupcast(ctx, fabric, keySetID, key)
	}
	if !exists {
		return groupcastErr(im.StatusNotFound, "KeySet %d not found for fabric", keySetID)
	}
	return nil
}

// leaveGroup mirrors matter.js GroupcastServer.leaveGroup with 0d30528a:
// GroupID 0 applies the request — its Endpoints included — to every group
// of the fabric (NotFound when it has none); the response names the
// removed endpoints of a single group and none for the wildcard.
func (g *Groupcast) leaveGroup(ctx context.Context, fabric uint8, req LeaveGroupRequest) (any, error) {
	if req.HasEndpoints && (len(req.Endpoints) == 0 || len(req.Endpoints) > groupcastLeaveMaxEndpoints) {
		return nil, groupcastErr(im.StatusConstraintError, "LeaveGroup: Endpoints must list 1 to %d endpoints", groupcastLeaveMaxEndpoints)
	}
	ms, err := g.groups.GroupcastMemberships(ctx)
	if err != nil {
		return nil, fmt.Errorf("matter: Groupcast.LeaveGroup: %w", err)
	}
	var ids []uint16
	if req.GroupID == 0 {
		for _, m := range ms {
			if m.FabricIndex == fabric {
				ids = append(ids, m.GroupID)
			}
		}
		if len(ids) == 0 {
			return nil, groupcastErr(im.StatusNotFound, "No groups to leave")
		}
	} else {
		ids = []uint16{req.GroupID}
	}
	var removed []uint16
	for _, id := range ids {
		entry, ok := memberOf(ms, fabric, id)
		if !ok {
			return nil, groupcastErr(im.StatusNotFound, "Group %d not found", id)
		}
		r, err := g.leave(ctx, fabric, entry, req)
		if err != nil {
			return nil, err
		}
		removed = r
	}
	if req.GroupID == 0 {
		return LeaveGroupResponse{GroupID: 0, Endpoints: []uint16{}}, nil
	}
	if removed == nil {
		removed = []uint16{}
	}
	return LeaveGroupResponse{GroupID: req.GroupID, Endpoints: removed}, nil
}

// leave withdraws the requested endpoints, or all of them, from one group;
// a group left without an endpoint is removed with its binding and its
// properties (no Sender feature to keep it sender-only). Mirrors matter.js
// GroupcastServer #leave.
func (g *Groupcast) leave(ctx context.Context, fabric uint8, entry groups.GroupcastMembership, req LeaveGroupRequest) ([]uint16, error) {
	var (
		removed      []uint16
		entryRemoved bool
	)
	if !req.HasEndpoints {
		removed = slices.Clone(entry.Endpoints)
		entryRemoved = true
	} else {
		for _, ep := range req.Endpoints {
			if slices.Contains(entry.Endpoints, ep) {
				removed = append(removed, ep)
			}
		}
		remaining := slices.DeleteFunc(slices.Clone(entry.Endpoints), func(ep uint16) bool { return slices.Contains(req.Endpoints, ep) })
		entryRemoved = len(remaining) == 0
	}
	for _, ep := range removed {
		if _, err := g.groups.RemoveEndpoint(ctx, fabric, ep, entry.GroupID, false); err != nil {
			return nil, g.groupStateErr("LeaveGroup", err)
		}
	}
	if entryRemoved {
		if err := g.gkm.removeGroupKeyMapping(ctx, fabric, entry.GroupID); err != nil {
			return nil, err
		}
		if err := g.groups.RemoveGroupProperties(ctx, fabric, entry.GroupID); err != nil {
			return nil, g.groupStateErr("LeaveGroup", err)
		}
	}
	return removed, nil
}

// updateGroupKey mirrors matter.js GroupcastServer.updateGroupKey.
func (g *Groupcast) updateGroupKey(ctx context.Context, fabric uint8, req UpdateGroupKeyRequest) error {
	if req.GroupID < 1 || req.KeySetID < 1 || (req.Key != nil && len(req.Key) != groupcastKeyLength) {
		return groupcastErr(im.StatusConstraintError, "UpdateGroupKey: GroupId and KeySetId must be at least 1, Key 16 bytes")
	}
	if req.Key != nil {
		if err := g.requireAdmin(ctx); err != nil {
			return err
		}
	}
	ms, err := g.groups.GroupcastMemberships(ctx)
	if err != nil {
		return fmt.Errorf("matter: Groupcast.UpdateGroupKey: %w", err)
	}
	if _, ok := memberOf(ms, fabric, req.GroupID); !ok {
		return groupcastErr(im.StatusNotFound, "Group %d not found", req.GroupID)
	}
	if err := g.applyKeySet(ctx, fabric, req.KeySetID, req.Key); err != nil {
		return err
	}
	return g.gkm.setGroupKeyMapping(ctx, fabric, req.GroupID, req.KeySetID)
}

// configureAuxiliaryACL mirrors matter.js GroupcastServer
// .configureAuxiliaryAcl.
func (g *Groupcast) configureAuxiliaryACL(ctx context.Context, fabric uint8, req ConfigureAuxiliaryACLRequest) error {
	ms, err := g.groups.GroupcastMemberships(ctx)
	if err != nil {
		return fmt.Errorf("matter: Groupcast.ConfigureAuxiliaryAcl: %w", err)
	}
	if _, ok := memberOf(ms, fabric, req.GroupID); !ok {
		return groupcastErr(im.StatusNotFound, "Group %d not found", req.GroupID)
	}
	aux := req.UseAuxiliaryACL
	if err := g.groups.SetGroupProperties(ctx, fabric, req.GroupID, nil, &aux); err != nil {
		return g.groupStateErr("ConfigureAuxiliaryAcl", err)
	}
	return nil
}

// groupcastTesting mirrors matter.js GroupcastServer.groupcastTesting:
// DisableTesting clears FabricUnderTest; EnableListenerTesting puts the
// accessing fabric under test for DurationSeconds (60 by default, 452d6f5c)
// and reports every received group message as a GroupcastTesting event
// meanwhile. EnableSenderTesting is not a value this server's enum allows
// (conformance SD) and answers ConstraintError, as matter.js's conformance
// validation does.
func (g *Groupcast) groupcastTesting(fabric uint8, req GroupcastTestingRequest) error {
	switch req.TestOperation {
	case groupcastTestingDisable, groupcastTestingEnableListener:
	case groupcastTestingEnableSender:
		return groupcastErr(im.StatusConstraintError, "EnableSenderTesting requires the Sender feature")
	default:
		return groupcastErr(im.StatusConstraintError, "TestOperation %d is no GroupcastTestingEnum value", req.TestOperation)
	}
	seconds := groupcastTestingDefaultSeconds
	if req.DurationSeconds != nil {
		seconds = *req.DurationSeconds
		if seconds < groupcastTestingMinSeconds || seconds > groupcastTestingMaxSeconds {
			return groupcastErr(im.StatusConstraintError, "DurationSeconds %d is outside 10 to 1200", seconds)
		}
	}
	g.mu.Lock()
	if g.stopTesting != nil {
		g.stopTesting()
		g.stopTesting = nil
	}
	g.testingGen++
	gen := g.testingGen
	if req.TestOperation == groupcastTestingDisable {
		g.mu.Unlock()
		g.setFabricUnderTest(0, gen)
		return nil
	}
	g.stopTesting = g.timer(time.Duration(seconds)*time.Second, func() { g.setFabricUnderTest(0, gen) })
	g.mu.Unlock()
	g.setFabricUnderTest(fabric, gen)
	return nil
}

// setFabricUnderTest changes FabricUnderTest unless a later
// GroupcastTesting superseded generation gen, and follows the group
// message reports while a fabric is under test. Mirrors matter.js
// #handleFabricUnderTestChanged / #attachGroupMessageListener.
func (g *Groupcast) setFabricUnderTest(fabric uint8, gen uint64) {
	g.mu.Lock()
	if gen != g.testingGen {
		g.mu.Unlock()
		return
	}
	changed := g.fabricUnderTest != fabric
	g.fabricUnderTest = fabric
	var unlisten func()
	if fabric == 0 && g.unlisten != nil {
		unlisten, g.unlisten = g.unlisten, nil
	}
	listen := fabric != 0 && g.unlisten == nil
	g.mu.Unlock()
	if unlisten != nil {
		unlisten()
	}
	if listen {
		un := g.groups.OnGroupMessage(g.onGroupMessage)
		g.mu.Lock()
		if g.unlisten == nil && g.fabricUnderTest != 0 {
			g.unlisten = un
			un = nil
		}
		g.mu.Unlock()
		if un != nil {
			un()
		}
	}
	if changed {
		g.changed()
	}
}

// onGroupMessage turns a group message outcome into a GroupcastTesting
// event for the fabric under test. A message that names another fabric is
// not reported; one that names none (it failed to authenticate) is
// reported on the fabric under test. The destination address is derived
// from the group id — the datagram's own is not available — as the
// group's address when the fabric knows the group, the shared IANA address
// otherwise. Mirrors matter.js GroupcastServer #onGroupMessage.
func (g *Groupcast) onGroupMessage(ev groups.GroupMessageEvent) {
	g.mu.RLock()
	fut, emitter, endpoint := g.fabricUnderTest, g.emitter, g.endpoint
	g.mu.RUnlock()
	if fut == 0 || emitter == nil {
		return
	}
	if ev.FabricIndex != 0 && ev.FabricIndex != fut {
		return
	}
	ctx := context.Background()
	dest := groups.IANAGroupcastAddress
	groupForAddress, haveGroup := ev.GroupID, ev.HasGroupID
	if !haveGroup {
		groupForAddress, haveGroup = ev.HeaderGroupID, ev.HasHeaderGroupID
	}
	if haveGroup {
		if ms, err := g.groups.GroupcastMemberships(ctx); err == nil {
			if _, known := memberOf(ms, fut, groupForAddress); known {
				if addr, err := g.groups.MulticastAddressFor(ctx, fut, groupForAddress); err == nil {
					dest = addr
				}
			}
		}
	}
	out := GroupcastTestingEvent{
		SourceIPAddress:      ipv6EventAddress(ev.SourceIP),
		DestinationIPAddress: ipv6EventAddress(dest),
		GroupcastTestResult:  ev.Result,
		AccessAllowed:        ev.AccessAllowed,
		FabricIndex:          fut,
	}
	if ev.HasGroupID {
		gid := ev.GroupID
		out.GroupID = &gid
	}
	if ev.HasPath {
		cl, el := ev.ClusterID, ev.ElementID
		out.ClusterID, out.ElementID = &cl, &el
		if ev.HasEndpoint {
			ep := ev.EndpointID
			out.EndpointID = &ep
		}
	}
	emitter.MatterEmitEvent(endpoint, GroupcastClusterID, groupcastEventGroupcastTesting, out, contract.EventPriorityInfo)
}

// ipv6EventAddress is the 16-byte ipv6adr of an IPv6 address; an IPv4
// sender has none. Mirrors matter.js toIpv6EventAddress.
func ipv6EventAddress(ip net.IP) []byte {
	if ip == nil || ip.To4() != nil {
		return nil
	}
	v6 := ip.To16()
	if v6 == nil {
		return nil
	}
	return slices.Clone([]byte(v6))
}

// Close stops a running GroupcastTesting period and its report listener.
// Mirrors matter.js GroupcastServer [Symbol.asyncDispose].
func (g *Groupcast) Close() {
	g.mu.Lock()
	stop, unlisten := g.stopTesting, g.unlisten
	g.stopTesting, g.unlisten = nil, nil
	g.mu.Unlock()
	if stop != nil {
		stop()
	}
	if unlisten != nil {
		unlisten()
	}
}

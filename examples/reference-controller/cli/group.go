// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"strconv"
	"strings"

	"thingfabric.dev/go-fabric/client/interaction"
	"thingfabric.dev/go-fabric/cluster/spec"
	acldef "thingfabric.dev/go-fabric/cluster/spec/accesscontrol"
	descdef "thingfabric.dev/go-fabric/cluster/spec/descriptor"
	groupcastdef "thingfabric.dev/go-fabric/cluster/spec/groupcast"
	gkmdef "thingfabric.dev/go-fabric/cluster/spec/groupkeymanagement"
	groupsdef "thingfabric.dev/go-fabric/cluster/spec/groups"
	"thingfabric.dev/go-fabric/controller/client"
	"thingfabric.dev/go-fabric/groups"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/store"
)

// epochStartTime0 is the key set's EpochStartTime0, the value of
// node/groupsend_live_test.go's KeySetWrite.
const epochStartTime0 = 1_110_000

// groupRecord is what the database keeps of a group "group create" made.
// It carries no key: the epoch key lives once, in the controller's own key
// set (Controller.SetGroupKey, table matter_group_keys), which
// Config.Sealer seals; a copy here, under a setting key the sealed store
// does not seal, would keep the secret in clear beside it
// (R2-P6-store-2603). groupEpochKey reads it from there.
type groupRecord struct {
	GroupID   uint16   `json:"groupId"`
	KeySetID  uint16   `json:"keySetId"`
	Nodes     []uint64 `json:"nodes"`
	Endpoints []uint16 `json:"endpoints"`
}

// legacyEpochKeyField is the field a record written before
// R2-P6-store-2603 carried the epoch key in; loadGroup rewrites such a
// record without it.
const legacyEpochKeyField = "epochKey"

func groupKey(id uint16) string { return groupKeyPref + strconv.Itoa(int(id)) }

func loadGroup(ctx context.Context, s *stack, id uint16) (groupRecord, error) {
	var g groupRecord
	v, ok, err := s.settings.GetSetting(ctx, groupKey(id))
	if err != nil {
		return g, err
	}
	if !ok {
		return g, fmt.Errorf("group %d: create it first (group create)", id)
	}
	if err := json.Unmarshal([]byte(v), &g); err != nil {
		return g, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(v), &fields); err != nil {
		return g, err
	}
	if _, legacy := fields[legacyEpochKeyField]; legacy {
		if err := saveGroup(ctx, s, g); err != nil {
			return g, fmt.Errorf("group %d: drop the record's key copy: %w", id, err)
		}
	}
	return g, nil
}

// saveGroup writes g's record.
func saveGroup(ctx context.Context, s *stack, g groupRecord) error {
	b, err := json.Marshal(g)
	if err != nil {
		return err
	}
	return s.settings.SetSetting(ctx, groupKey(g.GroupID), string(b))
}

// groupEpochKey is the epoch key of g's key set, read from the
// controller's own key set, the one copy (R2-P6-store-2603).
func groupEpochKey(ctx context.Context, s *stack, g groupRecord) ([]byte, error) {
	ks, err := s.settings.GetGroupKeySet(ctx, s.fabric.FabricIndex, g.KeySetID)
	if err != nil {
		return nil, fmt.Errorf("group %d key set %d: %w", g.GroupID, g.KeySetID, err)
	}
	return ks.EpochKey0, nil
}

// keySetWrite is GroupKeyManagement KeySetWrite (§11.2.7.1) of one epoch
// key — node/groupsend_live_test.go's keySetWrite, in the generated
// request type.
func keySetWrite(keySet uint16, key []byte) gkmdef.KeySetWriteRequest {
	return gkmdef.KeySetWriteRequest{GroupKeySet: gkmdef.GroupKeySetStruct{
		GroupKeySetId:          keySet,
		GroupKeySecurityPolicy: gkmdef.GroupKeySecurityPolicyTrustFirst,
		EpochKey0:              spec.ValueOf(key),
		EpochStartTime0:        spec.ValueOf(uint64(epochStartTime0)),
		EpochKey1:              spec.NullOf[[]byte](),
		EpochStartTime1:        spec.NullOf[uint64](),
		EpochKey2:              spec.NullOf[[]byte](),
		EpochStartTime2:        spec.NullOf[uint64](),
	}}
}

// joinGroup is Groupcast JoinGroup: the group on endpoints under the key
// set, with the auxiliary ACL entry for a member — none for a sender-only
// node. node/groupsend_live_test.go's joinGroup, in the generated request
// type.
func joinGroup(group, keySet uint16, endpoints []uint16, aux bool) groupcastdef.JoinGroupRequest {
	req := groupcastdef.JoinGroupRequest{GroupId: group, Endpoints: endpoints, KeySetId: keySet}
	if req.Endpoints == nil {
		req.Endpoints = []uint16{}
	}
	if aux {
		req.UseAuxiliaryAcl = new(true)
	}
	return req
}

// invoker is what a command is invoked through: a node.
type invoker interface {
	Invoke(ctx context.Context, path im.ConcreteCommandPath, fields spec.Encodable, opts interaction.InvokeOptions) (interaction.Result, error)
}

// invokeOK invokes command 0x00 of a root-endpoint cluster (KeySetWrite,
// JoinGroup) and wants Success.
func invokeOK(ctx context.Context, n invoker, cluster uint32, fields spec.Encodable) error {
	res, err := n.Invoke(ctx, im.ConcreteCommandPath{Endpoint: 0, Cluster: cluster, Command: 0x00, HasEndpoint: true, HasCluster: true, HasCommand: true}, fields, interaction.InvokeOptions{})
	if err != nil {
		return err
	}
	if res.Status != im.StatusSuccess {
		return fmt.Errorf("status %s", res.Status)
	}
	return nil
}

// cmdGroup dispatches "group create" and "group send".
func cmdGroup(e *env, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: %s", commands["group"].usage)
	}
	e.ownNode = true
	switch args[0] {
	case "create":
		return cmdGroupCreate(e, args[1:])
	case "send":
		return cmdGroupSend(e, args[1:])
	}
	return fmt.Errorf("usage: %s", commands["group"].usage)
}

func parseList[T uint16 | uint64](s string, bits int) ([]T, error) {
	if s == "" {
		return nil, nil
	}
	var out []T
	for _, part := range strings.Split(s, ",") {
		v, err := parseUint(strings.TrimSpace(part), bits)
		if err != nil {
			return nil, fmt.Errorf("list entry %q", part)
		}
		out = append(out, T(v)) //nolint:gosec // G115: parsed at the type's width
	}
	return out, nil
}

// cmdGroupCreate provisions a group: the key set and the group's
// membership on every node (GroupKeyManagement KeySetWrite, Groupcast
// JoinGroup with the auxiliary ACL entry on the endpoints — or, on a node
// whose ServerList lacks Groupcast, legacyJoin), and the key set in the
// controller's own tables, as the sender (Controller.SetGroupKey) — the
// provisioning of TestLiveGroupSend (node/groupsend_live_test.go).
func cmdGroupCreate(e *env, args []string) error {
	var keyArg, nodesArg, epsArg, keySetArg string
	pos, err := parseArgs(e, "group create", args, func(fs *flag.FlagSet) {
		fs.StringVar(&keyArg, "key", "random", "the 16-byte epoch key in hex, or random")
		fs.StringVar(&nodesArg, "nodes", "", "the member nodes, comma-separated")
		fs.StringVar(&epsArg, "endpoints", "", "the endpoints of each member, comma-separated")
		fs.StringVar(&keySetArg, "keyset", "", "the key set id (default the group id)")
	})
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return fmt.Errorf("usage: %s", commands["group"].usage)
	}
	gid, err := parseU16(pos[0])
	if err != nil || gid == 0 {
		return fmt.Errorf("group id %q", pos[0])
	}
	g := groupRecord{GroupID: gid, KeySetID: gid}
	if keySetArg != "" {
		ks, err := parseU16(keySetArg)
		if err != nil || ks == 0 {
			return fmt.Errorf("--keyset %q (0 is the IPK's)", keySetArg)
		}
		g.KeySetID = ks
	}
	key := make([]byte, 16)
	if keyArg == "random" {
		_, _ = rand.Read(key)
	} else if key, err = hex.DecodeString(strings.TrimPrefix(keyArg, "hex:")); err != nil || len(key) != 16 {
		return errors.New("--key: want 16 bytes in hex or random")
	}
	if g.Nodes, err = parseList[uint64](nodesArg, 64); err != nil {
		return err
	}
	if g.Endpoints, err = parseList[uint16](epsArg, 16); err != nil {
		return err
	}
	return withStack(e, func(s *stack) error {
		ctx, cancel := e.timeoutCtx()
		defer cancel()
		for _, id := range g.Nodes {
			n, _, err := s.node(strconv.FormatUint(id, 10))
			if err != nil {
				return err
			}
			if err := invokeOK(ctx, n, gkmdef.ClusterID, keySetWrite(g.KeySetID, key)); err != nil {
				return fmt.Errorf("node %d KeySetWrite: %w", id, err)
			}
			hasGroupcast, err := servesGroupcast(ctx, n)
			if err != nil {
				return fmt.Errorf("node %d ServerList: %w", id, err)
			}
			how := "Groupcast"
			if hasGroupcast {
				if err := invokeOK(ctx, n, groupcastdef.ClusterID, joinGroup(g.GroupID, g.KeySetID, g.Endpoints, true)); err != nil {
					return fmt.Errorf("node %d JoinGroup: %w", id, err)
				}
			} else {
				how = "Groups"
				if err := legacyJoin(ctx, n, g); err != nil {
					return fmt.Errorf("node %d: %w", id, err)
				}
			}
			e.printf("node %d joined group %d on endpoints %v (%s)\n", id, g.GroupID, g.Endpoints, how)
		}
		// The controller keeps the key set itself: it is the sender.
		if err := s.ctl.SetGroupKey(ctx, store.GroupKeySet{
			GroupKeySetID: g.KeySetID, SecurityPolicy: store.SecurityPolicyTrustFirst, EpochKey0: key, EpochStart0: epochStartTime0,
		}, g.GroupID); err != nil {
			return fmt.Errorf("the controller's key set: %w", err)
		}
		if err := saveGroup(ctx, s, g); err != nil {
			return err
		}
		e.printf("group %d created with key set %d\n", g.GroupID, g.KeySetID)
		return nil
	})
}

// cmdGroupSend sends a group command from the controller, as RootNodeID:
// the controller's groups.Manager.Sender for the group
// (Controller.Groups), its Invoke — TestLiveGroupSend's send
// (node/groupsend_live_test.go).
func cmdGroupSend(e *env, args []string) error {
	pos, err := parseArgs(e, "group send", args, nil)
	if err != nil {
		return err
	}
	if len(pos) < 2 || len(pos) > 3 {
		return fmt.Errorf("usage: %s", commands["group"].usage)
	}
	gid, err := parseU16(pos[0])
	if err != nil || gid == 0 {
		return fmt.Errorf("group id %q", pos[0])
	}
	if strings.Count(pos[1], "/") != 1 {
		return errors.New("a group command is <Cluster>/<Command>")
	}
	p, err := parsePath("*/" + pos[1])
	if err != nil {
		return err
	}
	cp, cmd, err := p.commandPath(true)
	if err != nil {
		return errors.New("a group command is <Cluster>/<Command>")
	}
	fields, err := commandFields(cmd, pos[2:])
	if err != nil {
		return err
	}
	return withStack(e, func(s *stack) error {
		ctx, cancel := e.timeoutCtx()
		defer cancel()
		sender, err := s.ctl.Groups().Sender(ctx, s.ctl.Fabric().FabricIndex, gid)
		if err != nil {
			return fmt.Errorf("group %d: %w (group create keeps the controller's key set)", gid, err)
		}
		if err := sender.Invoke(ctx, cp, fields, groups.RequestOptions{}); err != nil {
			return fmt.Errorf("group send: %w", err)
		}
		e.printf("sent to group %d from node %d\n", gid, sender.NodeID())
		return nil
	})
}

// servesGroupcast reports whether the root's Descriptor ServerList names
// Groupcast (0x0065), the Matter 1.5 group membership cluster.
func servesGroupcast(ctx context.Context, n *client.Node) (bool, error) {
	path := im.ConcreteAttributePath{Endpoint: 0, Cluster: descdef.ClusterID, Attribute: descdef.AttrServerList, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	rep, err := n.Read(ctx, client.ReadRequest{ReadRequest: interaction.ReadRequest{Attributes: []im.ConcreteAttributePath{path}}, IncludeKnownVersions: true})
	if err != nil {
		return false, err
	}
	v, ok := rep.Value(0, descdef.ClusterID, descdef.AttrServerList)
	if !ok {
		return false, errors.New("no ServerList")
	}
	for _, c := range v.Children {
		if c.El.Uint == uint64(groupcastdef.ClusterID) {
			return true, nil
		}
	}
	return false, nil
}

// legacyJoin provisions a node without Groupcast the way chip's
// TestGroupMessaging.yaml does at the harness pin (src/app/tests/suites/
// TestGroupMessaging.yaml:84-211, the IsLegacyGroupsRevision steps): after
// the KeySetWrite (TrustFirst, :84-102) the GroupKeyMap entry group →
// key set (:122-132), Groups AddGroup on each endpoint (:134-168), and an
// ACL entry for the group (:188-211). The ACL entry here is narrower than
// the YAML's "any group can manage": AuthMode Group granting the group
// Operate on the member endpoints. Its subject is the bare group id — the
// wire form chip stores (src/app/server/AclStorage.cpp:162-171 converts
// the internal 0xFFFFFFFFFFFF0000|group node id to GroupIdFromNodeId) and
// the one cluster/core's AccessControl accepts (0x0001..0xFFFF).
// addGroupOK judges an AddGroup answer: an AddGroupResponse whose Status
// (context tag 0) is Success, for the group asked — decoded by tag
// (groupsdef.AddGroupResponse.DecodeTLV), not by the members' position
// (R2-P6-wire-2609); TestGroupMessaging.yaml:145-150 expects Status 0.
func addGroupOK(res interaction.Result, groupID uint16) error {
	if !res.HasFields || res.Command != groupsdef.CmdAddGroupResponse {
		return fmt.Errorf("no AddGroupResponse (status %s, command 0x%02X)", res.Status, res.Command)
	}
	var rsp groupsdef.AddGroupResponse
	if err := rsp.DecodeTLV(res.Fields); err != nil {
		return fmt.Errorf("AddGroupResponse: %w", err)
	}
	if rsp.Status != groupsdef.Status(im.StatusSuccess) {
		return fmt.Errorf("status %s", im.StatusCode(rsp.Status))
	}
	if rsp.GroupId != groupID {
		return fmt.Errorf("AddGroupResponse for group %d, want %d", rsp.GroupId, groupID)
	}
	return nil
}

func legacyJoin(ctx context.Context, n *client.Node, g groupRecord) error {
	gid := uint64(g.GroupID)
	if err := appendEntry(ctx, n, listPath(0, gkmdef.ClusterID),
		gkmdef.GroupKeyMapStruct{GroupId: g.GroupID, GroupKeySetId: g.KeySetID}, mapsGroup(gid)); err != nil {
		return fmt.Errorf("GroupKeyMap: %w", err)
	}
	name := "group " + strconv.Itoa(int(g.GroupID))
	targets := make([]acldef.AccessControlTargetStruct, 0, len(g.Endpoints))
	for _, ep := range g.Endpoints {
		res, err := n.Invoke(ctx, im.ConcreteCommandPath{Endpoint: ep, Cluster: groupsdef.ClusterID, Command: groupsdef.CmdAddGroup, HasEndpoint: true, HasCluster: true, HasCommand: true},
			groupsdef.AddGroupRequest{GroupId: g.GroupID, GroupName: name}, interaction.InvokeOptions{})
		if err != nil {
			return fmt.Errorf("AddGroup on endpoint %d: %w", ep, err)
		}
		if err := addGroupOK(res, g.GroupID); err != nil {
			return fmt.Errorf("AddGroup on endpoint %d: %w", ep, err)
		}
		targets = append(targets, acldef.AccessControlTargetStruct{
			Cluster: spec.NullOf[uint32](), Endpoint: spec.ValueOf(ep), DeviceType: spec.NullOf[uint32](),
		})
	}
	entry := acldef.AccessControlEntryStruct{
		Privilege: acldef.AccessControlEntryPrivilegeOperate, AuthMode: acldef.AccessControlEntryAuthModeGroup,
		Subjects: spec.ValueOf([]uint64{gid}), Targets: spec.ValueOf(targets),
	}
	if err := appendEntry(ctx, n, listPath(0, acldef.ClusterID), entry, nil); err != nil {
		return fmt.Errorf("ACL: %w", err)
	}
	return nil
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package cli

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/spec"
	acldef "github.com/SukramJ/go-fabric/cluster/spec/accesscontrol"
	bindingdef "github.com/SukramJ/go-fabric/cluster/spec/binding"
	gkmdef "github.com/SukramJ/go-fabric/cluster/spec/groupkeymanagement"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// entryNode is v, an entry of a fabric-scoped list in its generated
// type, as the TLV node appendToList adds: its FabricIndex (254) left out,
// which the device fills in from the accessing fabric.
func entryNode(v spec.Encodable) (spec.Node, error) {
	enc := tlv.NewEncoder()
	v.EncodeTLV(enc, tlv.AnonymousTag())
	b, err := enc.Bytes()
	if err != nil {
		return spec.Node{}, err
	}
	n, err := spec.ReadElement(tlv.NewDecoder(b))
	if err != nil {
		return spec.Node{}, err
	}
	n.Children = slices.DeleteFunc(n.Children, func(c spec.Node) bool {
		return c.El.Tag.Kind == tlv.TagKindContext && c.El.Tag.Number == fabricIndexTag
	})
	return n, nil
}

// fabricIndexTag is the field of a fabric-scoped struct the device sets.
const fabricIndexTag = 254

// appendEntry is appendToList of v in its generated type.
func appendEntry(ctx context.Context, n *client.Node, path im.ConcreteAttributePath, v spec.Encodable, present func(spec.Node) bool) error {
	entry, err := entryNode(v)
	if err != nil {
		return err
	}
	return appendToList(ctx, n, path, entry, present)
}

// listPath is the path of a list attribute 0x0000 (Binding, ACL,
// GroupKeyMap — each its cluster's first attribute).
func listPath(ep uint16, cluster uint32) im.ConcreteAttributePath {
	return im.ConcreteAttributePath{Endpoint: ep, Cluster: cluster, Attribute: 0x0000, HasEndpoint: true, HasCluster: true, HasAttribute: true}
}

// appendToList reads a fabric-scoped list attribute (our fabric's entries),
// appends entry and writes the list back — the whole list in one write
// (DisableListChunking, as node/binding_live_test.go's Binding write).
//
// present, when set, reports an existing entry that already is the one
// to add: the list is then left as it is.
func appendToList(ctx context.Context, n *client.Node, path im.ConcreteAttributePath, entry spec.Node, present func(spec.Node) bool) error {
	rep, err := n.Read(ctx, client.ReadRequest{ReadRequest: interaction.ReadRequest{Attributes: []im.ConcreteAttributePath{path}}, IncludeKnownVersions: true})
	if err != nil {
		return err
	}
	list := spec.Node{El: tlv.Element{Tag: tlv.AnonymousTag(), Type: tlv.TypeArray, IsContainer: true}}
	if v, ok := rep.Value(path.Endpoint, path.Cluster, path.Attribute); ok && v.El.IsContainer {
		list.Children = append(list.Children, v.Children...)
	} else if len(rep.Statuses) > 0 {
		return fmt.Errorf("read: %s", rep.Statuses[0].Status)
	}
	if present != nil {
		for _, c := range list.Children {
			if present(c) {
				return nil
			}
		}
	}
	entry.El.Tag = tlv.AnonymousTag()
	list.Children = append(list.Children, entry)
	st, err := n.Write(ctx, []interaction.WriteRequest{{Path: path, Value: interaction.Raw(list)}}, interaction.WriteOptions{DisableListChunking: true})
	if err != nil {
		return err
	}
	return st.Err()
}

// cmdBind writes a Binding entry on the source node's endpoint and what
// the binding needs to work — the steps of TestLiveBinding
// (node/binding_live_test.go): for a node target the ACL entry on the
// target granting the source Operate on the endpoint (step 7), for a group
// the group's key set and GroupKeyMap entry on the source (the group
// entry's provisioning there).
func cmdBind(e *env, args []string) error {
	var to, groupArg string
	pos, err := parseArgs(e, "bind", args, func(fs *flag.FlagSet) {
		fs.StringVar(&to, "to", "", "the target: <node>/<endpoint>[/<Cluster>]")
		fs.StringVar(&groupArg, "group", "", "a group target: the group id")
	})
	if err != nil {
		return err
	}
	if err := wantArgs("bind", pos, 2, 2); err != nil {
		return err
	}
	if (to == "") == (groupArg == "") {
		return fmt.Errorf("usage: %s", commands["bind"].usage)
	}
	srcEP, err := parseU16(pos[1])
	if err != nil {
		return fmt.Errorf("endpoint %q", pos[1])
	}
	return withStack(e, func(s *stack) error {
		src, srcID, err := s.node(pos[0])
		if err != nil {
			return err
		}
		ctx, cancel := e.timeoutCtx()
		defer cancel()
		if groupArg != "" {
			return bindGroup(ctx, e, s, src, srcEP, groupArg)
		}
		parts := strings.Split(to, "/")
		if len(parts) < 2 || len(parts) > 3 {
			return fmt.Errorf("--to %q: want <node>/<endpoint>[/<Cluster>]", to)
		}
		dst, dstID, err := s.node(parts[0])
		if err != nil {
			return err
		}
		dstEP, err := parseU16(parts[1])
		if err != nil {
			return fmt.Errorf("--to endpoint %q", parts[1])
		}
		target := bindingdef.TargetStruct{Node: &dstID, Endpoint: &dstEP}
		aclTarget := acldef.AccessControlTargetStruct{
			Cluster: spec.NullOf[uint32](), Endpoint: spec.ValueOf(dstEP), DeviceType: spec.NullOf[uint32](),
		}
		if len(parts) == 3 {
			cid, _, err := lookupCluster(parts[2])
			if err != nil {
				return err
			}
			target.Cluster, aclTarget.Cluster = &cid, spec.ValueOf(cid)
		}
		if err := appendEntry(ctx, src, listPath(srcEP, bindingdef.ClusterID), target, nil); err != nil {
			return fmt.Errorf("the Binding on node %d: %w", srcID, err)
		}
		// The operator entry of node/binding_live_test.go's aclList: the
		// source node may Operate the target endpoint over CASE.
		entry := acldef.AccessControlEntryStruct{
			Privilege: acldef.AccessControlEntryPrivilegeOperate, AuthMode: acldef.AccessControlEntryAuthModeCase,
			Subjects: spec.ValueOf([]uint64{srcID}), Targets: spec.ValueOf([]acldef.AccessControlTargetStruct{aclTarget}),
		}
		if err := appendEntry(ctx, dst, listPath(0, acldef.ClusterID), entry, nil); err != nil {
			return fmt.Errorf("ACL on node %d: %w", dstID, err)
		}
		e.printf("bound node %d endpoint %d to node %d endpoint %d\n", srcID, srcEP, dstID, dstEP)
		return nil
	})
}

func bindGroup(ctx context.Context, e *env, s *stack, src *client.Node, srcEP uint16, groupArg string) error {
	gid, err := parseU16(groupArg)
	if err != nil || gid == 0 {
		return fmt.Errorf("--group %q", groupArg)
	}
	g, err := loadGroup(ctx, s, gid)
	if err != nil {
		return err
	}
	key, err := hex.DecodeString(g.EpochKey)
	if err != nil {
		return err
	}
	if err := invokeOK(ctx, src, gkmdef.ClusterID, keySetWrite(g.KeySetID, key)); err != nil {
		return fmt.Errorf("KeySetWrite: %w", err)
	}
	if err := appendEntry(ctx, src, listPath(0, gkmdef.ClusterID), gkmdef.GroupKeyMapStruct{GroupId: gid, GroupKeySetId: g.KeySetID}, mapsGroup(uint64(gid))); err != nil {
		return fmt.Errorf("GroupKeyMap: %w", err)
	}
	if err := appendEntry(ctx, src, listPath(srcEP, bindingdef.ClusterID), bindingdef.TargetStruct{Group: &gid}, nil); err != nil {
		return fmt.Errorf("the Binding: %w", err)
	}
	addr, _ := src.PeerAddress()
	e.printf("bound node %d endpoint %d to group %s\n", addr.NodeID, srcEP, strconv.FormatUint(uint64(gid), 10))
	return nil
}

// mapsGroup reports a GroupKeyMap entry of the group: a group maps to one
// key set per fabric, so a second entry is refused (Groupcast JoinGroup
// may have written it already).
func mapsGroup(gid uint64) func(spec.Node) bool {
	return func(n spec.Node) bool {
		for _, c := range n.Children {
			if c.El.Tag.Kind == tlv.TagKindContext && c.El.Tag.Number == 1 && c.El.Uint == gid {
				return true
			}
		}
		return false
	}
}

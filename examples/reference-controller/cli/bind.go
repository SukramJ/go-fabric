// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package cli

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"strconv"
	"strings"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// TLV node builders for the list entries bind appends.
func uintNode(tag uint8, v uint64) spec.Node {
	return spec.Node{El: tlv.Element{Tag: tlv.ContextTag(tag), Type: tlv.TypeUnsignedInt8, Uint: v}}
}

func nullNode(tag uint8) spec.Node {
	return spec.Node{El: tlv.Element{Tag: tlv.ContextTag(tag), Type: tlv.TypeNull, IsNull: true}}
}

func structNode(tag tlv.Tag, fields ...spec.Node) spec.Node {
	return spec.Node{El: tlv.Element{Tag: tag, Type: tlv.TypeStructure, IsContainer: true}, Children: fields}
}

func arrayNode(tag tlv.Tag, entries ...spec.Node) spec.Node {
	return spec.Node{El: tlv.Element{Tag: tag, Type: tlv.TypeArray, IsContainer: true}, Children: entries}
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
	list := arrayNode(tlv.AnonymousTag())
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
		// TargetStruct (binding.element.ts): Node 1, Endpoint 3, Cluster 4.
		target := structNode(tlv.AnonymousTag(), uintNode(1, dstID), uintNode(3, uint64(dstEP)))
		// AccessControlTargetStruct: Cluster 0, Endpoint 1, DeviceType 2.
		aclTarget := structNode(tlv.AnonymousTag(), nullNode(0), uintNode(1, uint64(dstEP)), nullNode(2))
		if len(parts) == 3 {
			cid, _, err := lookupCluster(parts[2])
			if err != nil {
				return err
			}
			target.Children = append(target.Children, uintNode(4, uint64(cid)))
			aclTarget.Children[0] = uintNode(0, uint64(cid))
		}
		if err := appendToList(ctx, src, listPath(srcEP, clusterBinding), target, nil); err != nil {
			return fmt.Errorf("the Binding on node %d: %w", srcID, err)
		}
		// AccessControlEntryStruct: Privilege 1 (Operate 3), AuthMode 2
		// (CASE 2), Subjects 3, Targets 4 — the operator entry of
		// node/binding_live_test.go's aclList.
		entry := structNode(tlv.AnonymousTag(),
			uintNode(1, 3), uintNode(2, 2),
			arrayNode(tlv.ContextTag(3), spec.Node{El: tlv.Element{Tag: tlv.AnonymousTag(), Type: tlv.TypeUnsignedInt8, Uint: srcID}}),
			arrayNode(tlv.ContextTag(4), aclTarget))
		if err := appendToList(ctx, dst, listPath(0, clusterAccessControl), entry, nil); err != nil {
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
	if err := invokeOK(ctx, src, clusterGroupKeyManagement, keySetWrite(g.KeySetID, key)); err != nil {
		return fmt.Errorf("KeySetWrite: %w", err)
	}
	// GroupKeyMapStruct: GroupId 1, GroupKeySetID 2.
	if err := appendToList(ctx, src, listPath(0, clusterGroupKeyManagement), structNode(tlv.AnonymousTag(), uintNode(1, uint64(gid)), uintNode(2, uint64(g.KeySetID))), mapsGroup(uint64(gid))); err != nil {
		return fmt.Errorf("GroupKeyMap: %w", err)
	}
	// TargetStruct: Group 2.
	if err := appendToList(ctx, src, listPath(srcEP, clusterBinding), structNode(tlv.AnonymousTag(), uintNode(2, uint64(gid))), nil); err != nil {
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

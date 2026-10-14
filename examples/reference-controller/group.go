// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

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

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/groups"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// Cluster and command ids the group and binding commands write by hand:
// GroupKeyManagement, AccessControl and Groupcast have no generated
// definition (ADR 0013 skips the core servers).
const (
	clusterGroupKeyManagement uint32 = 0x003F
	clusterGroupcast          uint32 = 0x0065
	clusterAccessControl      uint32 = 0x001F
	clusterBinding            uint32 = 0x001E
)

// epochStartTime0 is the key set's EpochStartTime0, the value of
// node/groupsend_live_test.go's KeySetWrite.
const epochStartTime0 = 1_110_000

// groupRecord is what the database keeps of a group "group create" made.
type groupRecord struct {
	GroupID   uint16   `json:"groupId"`
	KeySetID  uint16   `json:"keySetId"`
	EpochKey  string   `json:"epochKey"`
	Nodes     []uint64 `json:"nodes"`
	Endpoints []uint16 `json:"endpoints"`
}

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
	err = json.Unmarshal([]byte(v), &g)
	return g, err
}

// encodable writes a payload through a function, as the live tests do.
type encodable func(enc *tlv.Encoder, tag tlv.Tag)

func (f encodable) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) { f(enc, tag) }

// keySetWrite is GroupKeyManagement KeySetWrite (§11.2.7.1) of one epoch
// key — node/groupsend_live_test.go's keySetWrite.
func keySetWrite(keySet uint16, key []byte) encodable {
	return func(enc *tlv.Encoder, tag tlv.Tag) {
		enc.StartStruct(tag)
		enc.StartStruct(tlv.ContextTag(0))
		enc.PutUint(tlv.ContextTag(0), uint64(keySet))
		enc.PutUint(tlv.ContextTag(1), 0) // TrustFirst
		enc.PutOctets(tlv.ContextTag(2), key)
		enc.PutUint(tlv.ContextTag(3), epochStartTime0)
		for i := uint8(4); i <= 7; i++ {
			enc.PutNull(tlv.ContextTag(i))
		}
		_ = enc.EndContainer()
		_ = enc.EndContainer()
	}
}

// joinGroup is Groupcast JoinGroup: the group on endpoints under the key
// set, with the auxiliary ACL entry for a member (field 4) — none for a
// sender-only node. node/groupsend_live_test.go's joinGroup.
func joinGroup(group, keySet uint16, endpoints []uint16, aux bool) encodable {
	return func(enc *tlv.Encoder, tag tlv.Tag) {
		enc.StartStruct(tag)
		enc.PutUint(tlv.ContextTag(0), uint64(group))
		enc.StartArray(tlv.ContextTag(1))
		for _, ep := range endpoints {
			enc.PutUint(tlv.AnonymousTag(), uint64(ep))
		}
		_ = enc.EndContainer()
		enc.PutUint(tlv.ContextTag(2), uint64(keySet))
		if aux {
			enc.PutBool(tlv.ContextTag(4), true)
		}
		_ = enc.EndContainer()
	}
}

// invoker is a node or the own node's interaction.
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
// JoinGroup with the auxiliary ACL entry on the endpoints), and the key set
// on the own node as a sender-only member — the provisioning of
// TestLiveGroupSend (node/groupsend_live_test.go).
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
	g.EpochKey = hex.EncodeToString(key)
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
			if err := invokeOK(ctx, n, clusterGroupKeyManagement, keySetWrite(g.KeySetID, key)); err != nil {
				return fmt.Errorf("node %d KeySetWrite: %w", id, err)
			}
			if err := invokeOK(ctx, n, clusterGroupcast, joinGroup(g.GroupID, g.KeySetID, g.Endpoints, true)); err != nil {
				return fmt.Errorf("node %d JoinGroup: %w", id, err)
			}
			e.printf("node %d joined group %d on endpoints %v\n", id, g.GroupID, g.Endpoints)
		}
		self, err := s.ensureSelf(ctx)
		if err != nil {
			return err
		}
		if err := invokeOK(ctx, self.ia, clusterGroupKeyManagement, keySetWrite(g.KeySetID, key)); err != nil {
			return fmt.Errorf("own node KeySetWrite: %w", err)
		}
		if err := invokeOK(ctx, self.ia, clusterGroupcast, joinGroup(g.GroupID, g.KeySetID, nil, false)); err != nil {
			return fmt.Errorf("own node JoinGroup: %w", err)
		}
		b, err := json.Marshal(g)
		if err != nil {
			return err
		}
		if err := s.settings.SetSetting(ctx, groupKey(g.GroupID), string(b)); err != nil {
			return err
		}
		e.printf("group %d created with key set %d\n", g.GroupID, g.KeySetID)
		return nil
	})
}

// cmdGroupSend sends a group command from the own node: the node's
// groups.Manager.Sender for the group, its Invoke — TestLiveGroupSend's
// send (node/groupsend_live_test.go).
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
		self, err := s.ensureSelf(ctx)
		if err != nil {
			return err
		}
		fi, ok, err := self.fabricIndex(ctx, s)
		if err != nil || !ok {
			return fmt.Errorf("the own node is not on the fabric: %w", err)
		}
		sender, err := self.n.Groups().Sender(ctx, fi, gid)
		if err != nil {
			return fmt.Errorf("group %d: %w (group create provisions the own node)", gid, err)
		}
		if err := sender.Invoke(ctx, cp, fields, groups.RequestOptions{}); err != nil {
			return fmt.Errorf("group send: %w", err)
		}
		e.printf("sent to group %d from node %d\n", gid, sender.NodeID())
		return nil
	})
}

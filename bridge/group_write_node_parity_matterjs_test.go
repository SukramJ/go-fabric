// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"bytes"
	"context"
	"testing"

	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/store"
	"thingfabric.dev/go-fabric/tlv"
)

// groupWriteIdentifyTimeAt is groupWriteIdentifyTime with the path naming
// node.
func groupWriteIdentifyTimeAt(t *testing.T, node, seconds uint64) []byte {
	t.Helper()
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutBool(tlv.ContextTag(0), true) // SuppressResponse
	enc.PutBool(tlv.ContextTag(1), false)
	enc.StartArray(tlv.ContextTag(2))
	enc.StartStruct(tlv.AnonymousTag())
	enc.StartList(tlv.ContextTag(1))
	enc.PutUint(tlv.ContextTag(1), node)
	enc.PutUint(tlv.ContextTag(3), 0x0003)
	enc.PutUint(tlv.ContextTag(4), 0x0000)
	_ = enc.EndContainer()
	enc.PutUint(tlv.ContextTag(2), seconds)
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	enc.PutUint(tlv.ContextTag(0xFF), 12)
	_ = enc.EndContainer()
	b, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestGroupWriteNamingAnotherNodeParity: a group write whose path names
// another node writes nothing; one naming this node's id on the group's
// fabric is written. matter.js's group write takes the wildcard branch,
// which returns for a path naming another node
// (packages/protocol/src/action/server/AttributeWriteResponse.ts:118-123
// at db021da0), the node's id being its id on the session's fabric
// (DataResponse.ts:57-62) — for a group session the group's fabric.
func TestGroupWriteNamingAnotherNodeParity(t *testing.T) {
	t.Parallel()
	gh := newGroupsHarness(t, 2)
	member := &fakeMulticastMember{joined: map[string]bool{}}
	gh.bridge.startGroupNetworking(member)
	gh.bridge.AttachGroupMessaging(gh.groups)
	lamps := gh.lampIDs()
	key := bytes.Repeat([]byte{0xA1}, 16)
	gh.provisionGroup(t, 0x01A1, key, 0x0101, "Kitchen", lamps[0], lamps[1])
	if err := gh.store.ReplaceACL(context.Background(), gh.fabric, []store.ACLEntry{
		{FabricIndex: gh.fabric, Privilege: store.PrivilegeAdminister, AuthMode: store.AuthModeCASE, Subjects: []uint64{harnessControllerNodeID}},
		{FabricIndex: gh.fabric, Privilege: store.PrivilegeOperate, AuthMode: store.AuthModeGroup, Subjects: []uint64{0x0101}},
	}); err != nil {
		t.Fatal(err)
	}
	sender := newGroupSender(t, key)

	// The harness fabric's node id is 0x0B0B (addHarnessFabric).
	gh.deliver(sender.seal(0x0101, im.OpcodeWriteRequest, groupWriteIdentifyTimeAt(t, 0x9999, 30)))
	gh.expectSilence(t)
	if a, b := gh.identifyTime(t, lamps[0]), gh.identifyTime(t, lamps[1]); a != 0 || b != 0 {
		t.Fatalf("a group write naming another node set IdentifyTime %d/%d", a, b)
	}
	gh.deliver(sender.seal(0x0101, im.OpcodeWriteRequest, groupWriteIdentifyTimeAt(t, 0x0B0B, 30)))
	gh.expectSilence(t)
	if a, b := gh.identifyTime(t, lamps[0]), gh.identifyTime(t, lamps[1]); a == 0 || b == 0 {
		t.Fatalf("a group write naming this node set IdentifyTime %d/%d, want both identifying", a, b)
	}
}

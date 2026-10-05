// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"bytes"
	"context"
	"testing"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/tlv"
)

// TestGroupKeyMapBeforeKeySetOverTheWire: a controller may write the
// GroupKeyMap before the key set it names. matter.js accepts that write —
// GroupKeyManagementServer.ts #validateGroupKeyMap leaves the key-set check
// commented out because certification tests write the map first — and the
// entry authenticates nothing until the set arrives. Against the real store
// the write used to fail with Failure on a foreign key.
func TestGroupKeyMapBeforeKeySetOverTheWire(t *testing.T) {
	t.Parallel()
	gh := newGroupsHarness(t, 1)
	gh.bridge.AttachGroupMessaging(gh.groups)
	lamp := gh.lampIDs()[0]
	const (
		groupID  uint16 = 0x0101
		keySetID uint16 = 0x01A1
	)
	key := bytes.Repeat([]byte{0xC3}, 16)

	if status := gh.writeAttribute(0, gkmCluster, 0x0000, func(enc *tlv.Encoder, tag tlv.Tag) {
		enc.StartArray(tag)
		enc.StartStruct(tlv.AnonymousTag())
		enc.PutUint(tlv.ContextTag(1), uint64(groupID))
		enc.PutUint(tlv.ContextTag(2), uint64(keySetID))
		_ = enc.EndContainer()
		_ = enc.EndContainer()
	}); status != 0 {
		t.Fatalf("GroupKeyMap write naming a key set not written yet: status 0x%02X, want Success", status)
	}
	// The mapping alone lets AddGroup through (GroupsServer.addGroup checks
	// only groupKeyIdMap.has).
	if _, fields, _, _ := invokeResult(t, gh.invoke(lamp, mattercore.GroupsClusterID, 0x00, func(enc *tlv.Encoder) {
		enc.PutUint(tlv.ContextTag(0), uint64(groupID))
		enc.PutUTF8(tlv.ContextTag(1), "")
	})); fields.mustChild(t, 0).El.Uint != 0 {
		t.Fatal("AddGroup with a mapping to a missing key set failed")
	}
	if err := gh.store.ReplaceACL(context.Background(), gh.fabric, []store.ACLEntry{
		{FabricIndex: gh.fabric, Privilege: store.PrivilegeAdminister, AuthMode: store.AuthModeCASE, Subjects: []uint64{harnessControllerNodeID}},
		{FabricIndex: gh.fabric, Privilege: store.PrivilegeOperate, AuthMode: store.AuthModeGroup},
	}); err != nil {
		t.Fatal(err)
	}

	sender := newGroupSender(t, key)
	toggle := groupInvoke(t, 0x0006, 0x02, nil)
	gh.deliver(sender.seal(groupID, im.OpcodeInvokeRequest, toggle))
	if got := gh.counts()[lamp]; got != 0 {
		t.Fatal("a group message authenticated before its key set existed")
	}

	if _, _, status, _ := invokeResult(t, gh.invoke(0, gkmCluster, gkmCmdKeySetWrite, putKeySet(keySetID, key, 1, 0))); status != 0 {
		t.Fatalf("KeySetWrite: %v", status)
	}
	gh.deliver(sender.seal(groupID, im.OpcodeInvokeRequest, toggle))
	gh.expectSilence(t)
	if got := gh.counts()[lamp]; got != 1 {
		t.Fatalf("once the key set exists the group Toggle reached the lamp %d times, want 1", got)
	}

	// KeySetRemove takes the entries naming the set with it
	// (core§11.2.7.4.1; matter.js keySetRemove filters groupKeyMap).
	if _, _, status, _ := invokeResult(t, gh.invoke(0, gkmCluster, gkmCmdKeySetRemove, putUint16Field(keySetID))); status != 0 {
		t.Fatalf("KeySetRemove: %v", status)
	}
	if got, _ := gh.store.ListGroupKeyMappings(context.Background(), gh.fabric); len(got) != 0 {
		t.Fatalf("GroupKeyMap after KeySetRemove = %+v, want empty", got)
	}
}

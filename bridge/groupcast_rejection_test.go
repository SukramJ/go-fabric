// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"bytes"
	"context"
	"net"
	"slices"
	"testing"

	mattercore "thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/groups"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/store"
	"thingfabric.dev/go-fabric/tlv"
)

// rejectionFabricID / rejectionCompressedID identify the second fabric
// of TestGroupcastTestingReportsRejectionsOnTheFabricUnderTest.
const rejectionFabricID uint64 = 0x0000_0000_0000_0FAC

var rejectionCompressedID = [8]byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88}

// newFabricGroupSender is newGroupSender for a fabric of the given
// compressed fabric id.
func newFabricGroupSender(t *testing.T, epochKey []byte, compressed [8]byte) *groupSender {
	t.Helper()
	op, err := groups.OperationalKey(epochKey, compressed)
	if err != nil {
		t.Fatal(err)
	}
	sid, err := groups.SessionID(op)
	if err != nil {
		t.Fatal(err)
	}
	return &groupSender{t: t, opKey: op, sessionID: sid, source: harnessControllerNodeID, counter: 0x0200_0000, exchange: 0x5000}
}

// TestGroupcastTestingReportsRejectionsOnTheFabricUnderTest holds the
// receive path of a group message that no candidate key passes to
// matter.js at db021da0: the candidates are the keys of mapped key sets
// with the message's session id (packages/protocol/src/session/
// GroupSession.ts:366-376); one that decrypts names the owning group and
// fabric and still fails when GroupKeyMap does not map the group to its
// key (:425-433); the rejection carries the candidate fabrics and that
// group (:454-461) and is reported on the fabric under test — FailedAuth
// when a key of that fabric was a candidate, NoAvailableKey otherwise
// (:74-81) — from the owning fabric's address for the group when its
// table holds endpoints for it, the IANA address otherwise
// (packages/node/src/behaviors/groupcast/GroupcastServer.ts:478-487,
// :514-520). None of these messages reaches a lamp.
func TestGroupcastTestingReportsRejectionsOnTheFabricUnderTest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	gh := newGroupcastHarness(t, 2)
	lamps := gh.lampIDs()
	iana := groups.IANAGroupcastAddress.To16()

	// Fabric 1 (under test): kitchen 0x0101 on the IANA address under key
	// set 0x0042, hall 0x0202 on its own address under key set 0x0043,
	// and 0x0303 on its own address under key set 0x0044.
	kitchenKey := bytes.Repeat([]byte{0x5A}, 16)
	hallKey := bytes.Repeat([]byte{0x6B}, 16)
	for _, j := range []struct {
		group, keySet uint16
		key           []byte
		policy        uint8
	}{
		{0x0101, 0x0042, kitchenKey, groups.PolicyIanaAddr},
		{0x0202, 0x0043, hallKey, groups.PolicyPerGroup},
		{0x0303, 0x0044, bytes.Repeat([]byte{0x7C}, 16), groups.PolicyPerGroup},
	} {
		if st := gh.gcStatus(t, 0x00, joinGroupFields(j.group, []uint16{lamps[0]}, j.keySet, j.key, bptr(true), nil, u8ptr(j.policy))); st != im.StatusSuccess {
			t.Fatalf("fabric 1 JoinGroup 0x%04X: %v", j.group, st)
		}
	}

	// Fabric 2: key set 0x0052 (key Y) mapped to 0x0505 only, 0x0101 on
	// its own address under key set 0x0051, and key set 0x0053 (key Z)
	// that no group is mapped to.
	fabric2 := addHarnessFabric(t, gh.store, rejectionFabricID, rejectionCompressedID)
	admin2 := im.WithAuthority(im.WithSubject(im.WithFabricFilter(ctx, true, fabric2), harnessControllerNodeID, nil),
		func(uint16, uint32, uint8) im.StatusCode { return im.StatusSuccess })
	keyY := bytes.Repeat([]byte{0x9E}, 16)
	keyZ := bytes.Repeat([]byte{0xAF}, 16)
	for _, req := range []mattercore.JoinGroupRequest{
		{GroupID: 0x0505, Endpoints: []uint16{lamps[1]}, KeySetID: 0x0052, Key: keyY, McastAddrPolicy: u8ptr(groups.PolicyIanaAddr)},
		{GroupID: 0x0101, Endpoints: []uint16{lamps[1]}, KeySetID: 0x0051, Key: bytes.Repeat([]byte{0x8D}, 16), McastAddrPolicy: u8ptr(groups.PolicyPerGroup)},
	} {
		if _, err := gh.groupcast.MatterInvoke(admin2, 0x00, req); err != nil {
			t.Fatalf("fabric 2 JoinGroup 0x%04X: %v", req.GroupID, err)
		}
	}
	if err := gh.store.UpsertGroupKeySet(ctx, store.GroupKeySet{FabricIndex: fabric2, GroupKeySetID: 0x0053, EpochKey0: keyZ, EpochStart0: 1}); err != nil {
		t.Fatal(err)
	}
	if err := gh.groups.Reload(ctx, fabric2); err != nil {
		t.Fatal(err)
	}

	if st := gh.gcStatus(t, 0x05, func(enc *tlv.Encoder) { enc.PutUint(tlv.ContextTag(0), 1) }); st != im.StatusSuccess {
		t.Fatalf("GroupcastTesting: %v", st)
	}
	before := gh.counts()
	toggle := groupInvoke(t, 0x0006, 0x02, nil)
	fabric2Kitchen := groups.MulticastAddress(rejectionFabricID, 0x0101).To16()
	fabric1Hall := groups.MulticastAddress(harnessFabricID, 0x0202).To16()

	for _, row := range []struct {
		name     string
		datagram []byte
		result   uint8
		dest     net.IP
	}{
		{
			// A key of fabric 2 opens a message for 0x0101, which fabric 2
			// maps to another key set and holds endpoints for.
			name:     "a key of another fabric authenticates a group it is not mapped to",
			datagram: newFabricGroupSender(t, keyY, rejectionCompressedID).seal(0x0101, im.OpcodeInvokeRequest, toggle),
			result:   groups.TestResultNoAvailableKey,
			dest:     fabric2Kitchen,
		},
		{
			// The same for 0x0303: fabric 1 holds endpoints for it, fabric
			// 2 — which owns the id here — does not.
			name:     "the owning fabric has not joined the group",
			datagram: newFabricGroupSender(t, keyY, rejectionCompressedID).seal(0x0303, im.OpcodeInvokeRequest, toggle),
			result:   groups.TestResultNoAvailableKey,
			dest:     iana,
		},
		{
			// Fabric 1's kitchen key, mapped to 0x0101 only, opens a
			// message for the hall.
			name:     "a key of the fabric under test authenticates another of its groups",
			datagram: newGroupSender(t, kitchenKey).seal(0x0202, im.OpcodeInvokeRequest, toggle),
			result:   groups.TestResultFailedAuth,
			dest:     fabric1Hall,
		},
		{
			name:     "only another fabric held a candidate and it does not decrypt",
			datagram: tamper(newFabricGroupSender(t, keyY, rejectionCompressedID).seal(0x0505, im.OpcodeInvokeRequest, toggle)),
			result:   groups.TestResultNoAvailableKey,
			dest:     iana,
		},
		{
			name:     "only an unmapped key set of another fabric would open it",
			datagram: newFabricGroupSender(t, keyZ, rejectionCompressedID).seal(0x0505, im.OpcodeInvokeRequest, toggle),
			result:   groups.TestResultNoAvailableKey,
			dest:     iana,
		},
	} {
		mark := gh.lastEventNumber()
		gh.deliver(row.datagram)
		evs := gh.groupcastTestingEvents(mark + 1)
		if len(evs) != 1 {
			t.Errorf("%s: GroupcastTesting events %+v, want one", row.name, evs)
			continue
		}
		ev := evs[0]
		if ev.FabricIndex != gh.fabric || ev.GroupcastTestResult != row.result || ev.GroupID != nil ||
			ev.EndpointID != nil || ev.AccessAllowed != nil || !slices.Equal(ev.DestinationIPAddress, row.dest) {
			t.Errorf("%s: event %+v, want result %d on fabric %d from %v without a group id", row.name, ev, row.result, gh.fabric, row.dest)
		}
	}
	gh.expectSilence(t)
	if got := gh.counts(); !mapsEqual(got, before) {
		t.Errorf("lamps ran %v, want %v: no rejected group message is dispatched", got, before)
	}
}

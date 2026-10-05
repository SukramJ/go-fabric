// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

// Groupcast end to end, in process: a CASE controller joins lamps to groups
// with Groupcast JoinGroup — key, binding and auxiliary access control in
// one command, as a Matter 1.6.1 controller provisions them — and then sends
// group messages sealed with that key, the way matter.js GroupSession.encode
// seals them, through the bridge's UDP handler. No stored Group access
// control entry exists: whatever a group may do, the auxiliary entry grants.

import (
	"bytes"
	"context"
	"slices"
	"testing"
	"time"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/groups"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/tlv"
)

// groupcastTestingEvents returns the GroupcastTesting events the bridge
// logged from number from on.
func (gh *groupsHarness) groupcastTestingEvents(from uint64) []mattercore.GroupcastTestingEvent {
	var out []mattercore.GroupcastTestingEvent
	for _, rec := range gh.bridge.EventLog().Query(0, gcCluster, 0, from) {
		if ev, ok := rec.Payload.(mattercore.GroupcastTestingEvent); ok {
			out = append(out, ev)
		}
	}
	return out
}

func (gh *groupsHarness) lastEventNumber() uint64 {
	recs := gh.bridge.EventLog().Query(0xFFFF, 0xFFFFFFFF, 0xFFFFFFFF, 0)
	if len(recs) == 0 {
		return 0
	}
	return recs[len(recs)-1].Number
}

func TestGroupcastEndToEnd(t *testing.T) {
	t.Parallel()
	gh := newGroupcastHarness(t, 3)
	member := &fakeMulticastMember{joined: map[string]bool{}}
	gh.bridge.startGroupNetworking(member)
	lamps := gh.lampIDs()
	ctx := context.Background()
	kitchenKey := bytes.Repeat([]byte{0x5A}, 16)

	// JoinGroup over CASE: the key set is created, GroupKeyMap binds it,
	// both lamps join, the group goes on FF05::FA, and the auxiliary ACL
	// grants it Operate on the two lamps.
	before := gh.lastEventNumber()
	if st := gh.gcStatus(t, 0x00, joinGroupFields(0x0101, []uint16{lamps[0], lamps[1]}, 0x0042, kitchenKey, bptr(true), nil, u8ptr(groups.PolicyIanaAddr))); st != im.StatusSuccess {
		t.Fatalf("JoinGroup: %v", st)
	}
	if got := member.addresses(); !slices.Equal(got, []string{"ff05::fa"}) {
		t.Fatalf("joined %v, want the IANA Groupcast address only", got)
	}
	updates := gh.bridge.EventLog().Query(0, 0x001F, 0x0003, before+1)
	if len(updates) != 1 {
		t.Fatalf("AuxiliaryAccessUpdated events = %d, want 1", len(updates))
	}
	if ev := updates[0].Payload.(mattercore.AuxiliaryAccessUpdatedEvent); ev.FabricIndex != gh.fabric || ev.AdminNodeID == nil || *ev.AdminNodeID != harnessControllerNodeID {
		t.Errorf("AuxiliaryAccessUpdated = %+v", ev)
	}

	sender := newGroupSender(t, kitchenKey)
	toggle := groupInvoke(t, 0x0006, 0x02, nil)
	gh.deliver(sender.seal(0x0101, im.OpcodeInvokeRequest, toggle))
	gh.expectSilence(t)
	if got := gh.counts(); got[lamps[0]] != 1 || got[lamps[1]] != 1 || got[lamps[2]] != 0 {
		t.Fatalf("group Toggle under the auxiliary grant reached %v, want lamps %d and %d once", got, lamps[0], lamps[1])
	}

	// The auxiliary grant is Operate: a Manage command (Groups
	// RemoveAllGroups) on the same group goes nowhere.
	gh.deliver(sender.seal(0x0101, im.OpcodeInvokeRequest, groupInvoke(t, mattercore.GroupsClusterID, 0x04, nil)))
	if table, _ := gh.groups.GroupTable(ctx, gh.fabric); len(table) != 1 || len(table[0].Endpoints) != 2 {
		t.Fatalf("a Manage command passed the Operate grant: %+v", table)
	}

	// Denial: without the auxiliary entry nothing grants the group.
	if st := gh.gcStatus(t, 0x04, func(enc *tlv.Encoder) {
		enc.PutUint(tlv.ContextTag(0), 0x0101)
		enc.PutBool(tlv.ContextTag(1), false)
	}); st != im.StatusSuccess {
		t.Fatalf("ConfigureAuxiliaryAcl: %v", st)
	}
	gh.deliver(sender.seal(0x0101, im.OpcodeInvokeRequest, toggle))
	gh.expectSilence(t)
	if got := gh.counts(); got[lamps[0]] != 1 || got[lamps[1]] != 1 {
		t.Fatalf("a group without an auxiliary or stored grant was executed: %v", got)
	}
	gh.gcStatus(t, 0x04, func(enc *tlv.Encoder) {
		enc.PutUint(tlv.ContextTag(0), 0x0101)
		enc.PutBool(tlv.ContextTag(1), true)
	})

	// A second, PerGroup group for the third lamp joins its own address.
	hallKey := bytes.Repeat([]byte{0x6B}, 16)
	if st := gh.gcStatus(t, 0x00, joinGroupFields(0x0202, []uint16{lamps[2]}, 0x0043, hallKey, bptr(true), nil, u8ptr(groups.PolicyPerGroup))); st != im.StatusSuccess {
		t.Fatalf("JoinGroup hall: %v", st)
	}
	hallAddr := groups.MulticastAddress(harnessFabricID, 0x0202).String()
	want := []string{hallAddr, "ff05::fa"}
	slices.Sort(want)
	if got := member.addresses(); !slices.Equal(got, want) {
		t.Fatalf("joined %v, want the IANA and the hall's per-group address", got)
	}

	// GroupcastTesting: every outcome is reported while the fabric is
	// under test.
	if st := gh.gcStatus(t, 0x05, func(enc *tlv.Encoder) { enc.PutUint(tlv.ContextTag(0), 1) }); st != im.StatusSuccess {
		t.Fatalf("GroupcastTesting: %v", st)
	}
	mark := gh.lastEventNumber()
	hall := newGroupSender(t, hallKey)
	first := hall.seal(0x0202, im.OpcodeInvokeRequest, toggle)
	gh.deliver(first)
	gh.deliver(first) // replay
	gh.deliver(newGroupSender(t, bytes.Repeat([]byte{0x11}, 16)).seal(0x0202, im.OpcodeInvokeRequest, toggle))
	gh.deliver(tamper(hall.seal(0x0202, im.OpcodeInvokeRequest, toggle)))
	gh.expectSilence(t)
	evs := gh.groupcastTestingEvents(mark + 1)
	if len(evs) != 4 {
		t.Fatalf("GroupcastTesting events = %+v, want four", evs)
	}
	success, replay, noKey, failed := evs[0], evs[1], evs[2], evs[3]
	if success.GroupcastTestResult != groups.TestResultSuccess || success.GroupID == nil || *success.GroupID != 0x0202 ||
		success.EndpointID == nil || *success.EndpointID != lamps[2] || *success.ClusterID != 0x0006 || *success.ElementID != 0x02 ||
		success.AccessAllowed == nil || !*success.AccessAllowed || !slices.Equal(success.DestinationIPAddress, groups.MulticastAddress(harnessFabricID, 0x0202).To16()) {
		t.Errorf("Success event = %+v", success)
	}
	if replay.GroupcastTestResult != groups.TestResultMessageReplay || replay.GroupID == nil || *replay.GroupID != 0x0202 {
		t.Errorf("replay event = %+v", replay)
	}
	if noKey.GroupcastTestResult != groups.TestResultNoAvailableKey || noKey.GroupID != nil ||
		!slices.Equal(noKey.DestinationIPAddress, groups.IANAGroupcastAddress.To16()) {
		t.Errorf("unknown-key event = %+v (privacy hides the group, so the IANA address is the arrival)", noKey)
	}
	if failed.GroupcastTestResult != groups.TestResultFailedAuth || failed.GroupID != nil || failed.FabricIndex != gh.fabric {
		t.Errorf("tampered event = %+v", failed)
	}
	if got := gh.counts(); got[lamps[2]] != 1 {
		t.Fatalf("the hall lamp ran the Toggle %d times, want once", got[lamps[2]])
	}
	gh.gcStatus(t, 0x05, func(enc *tlv.Encoder) { enc.PutUint(tlv.ContextTag(0), 0) })

	// LeaveGroup: one lamp leaves the kitchen, then GroupID 0 empties the
	// fabric — the addresses are left.
	gh.gcStatus(t, 0x01, leaveGroupFields(0x0101, []uint16{lamps[1]}))
	gh.deliver(sender.seal(0x0101, im.OpcodeInvokeRequest, toggle))
	if got := gh.counts(); got[lamps[0]] != 2 || got[lamps[1]] != 1 {
		t.Fatalf("after lamp %d left: %v", lamps[1], got)
	}
	gh.gcStatus(t, 0x01, leaveGroupFields(0, nil))
	if got := member.addresses(); len(got) != 0 {
		t.Fatalf("still joined %v after LeaveGroup 0", got)
	}
	gh.deliver(sender.seal(0x0101, im.OpcodeInvokeRequest, toggle))
	gh.deliver(hall.seal(0x0202, im.OpcodeInvokeRequest, toggle))
	if got := gh.counts(); got[lamps[0]] != 2 || got[lamps[2]] != 1 {
		t.Fatalf("a left group still delivered: %v", got)
	}

	// Fabric removal: memberships, auxiliary entries and addresses go.
	gh.gcStatus(t, 0x00, joinGroupFields(0x0101, []uint16{lamps[0]}, 0x0042, nil, bptr(true), nil, nil))
	if len(member.addresses()) != 1 {
		t.Fatalf("rejoin did not join: %v", member.addresses())
	}
	if err := gh.store.RemoveFabric(ctx, gh.fabric); err != nil {
		t.Fatal(err)
	}
	gh.bridge.EmitFabricRemoved(gh.fabric)
	if got := member.addresses(); len(got) != 0 {
		t.Fatalf("still joined %v after fabric removal", got)
	}
	if ms, _ := gh.groups.GroupcastMemberships(ctx); len(ms) != 0 {
		t.Fatalf("memberships after fabric removal: %+v", ms)
	}
	if aux, _ := gh.groups.AuxiliaryACL(ctx, 0); len(aux) != 0 {
		t.Fatalf("auxiliary entries after fabric removal: %+v", aux)
	}
	gh.deliver(sender.seal(0x0101, im.OpcodeInvokeRequest, toggle))
	if got := gh.counts(); got[lamps[0]] != 2 {
		t.Fatal("a removed fabric's group key still delivered")
	}
}

// TestGroupcastWithoutTheAuxiliaryPortFailsClosed: a root with Groupcast
// whose host never attached the auxiliary entries keeps the dispatcher's
// Auxiliary feature off — the grant is listed but not enforced, so the group
// message is dropped, never executed.
func TestGroupcastWithoutTheAuxiliaryPortFailsClosed(t *testing.T) {
	t.Parallel()
	gh := newGroupsHarnessWith(t, 1, true)
	gh.bridge.AttachGroupMessaging(gh.groups)
	lamp := gh.lampIDs()[0]
	key := bytes.Repeat([]byte{0x77}, 16)
	if st := gh.gcStatus(t, 0x00, joinGroupFields(0x0101, []uint16{lamp}, 0x0042, key, bptr(true), nil, nil)); st != im.StatusSuccess {
		t.Fatalf("JoinGroup: %v", st)
	}
	gh.deliver(newGroupSender(t, key).seal(0x0101, im.OpcodeInvokeRequest, groupInvoke(t, 0x0006, 0x02, nil)))
	gh.expectSilence(t)
	if got := gh.counts()[lamp]; got != 0 {
		t.Fatal("an auxiliary grant was honoured without AttachAuxiliaryACL")
	}
}

// TestGroupcastChangeReachesASubscriber: Membership is derived state, so a
// JoinGroup must still mark it dirty for a subscriber — the root notifier
// wiring carries the Groupcast server's change signal to the subscription
// engine, as matter.js's reactive state reports a Membership change.
func TestGroupcastChangeReachesASubscriber(t *testing.T) {
	t.Parallel()
	gh := newGroupcastHarness(t, 1)
	spy := &reachAttrReporterSpy{}
	mgr := subscription.NewManager(subscription.Config{}, spy.report, nil)
	if _, err := mgr.Subscribe(subscription.SubscribeArgs{
		FabricIndex: gh.fabric, PeerNodeID: harnessControllerNodeID, SessionID: 1, MaxIntervalCeiling: 60,
		AttributePaths: []im.ConcreteAttributePath{{HasEndpoint: true, HasCluster: true, Endpoint: 0, Cluster: gcCluster}},
	}); err != nil {
		t.Fatal(err)
	}
	gh.bridge.AttachSubscriptionManager(mgr)
	if st := gh.gcStatus(t, 0x00, joinGroupFields(0x0101, gh.lampIDs(), 0x0042, bytes.Repeat([]byte{1}, 16), nil, nil, nil)); st != im.StatusSuccess {
		t.Fatalf("JoinGroup: %v", st)
	}
	mgr.Tick(context.Background(), time.Now().Add(2*time.Second))
	spy.mu.Lock()
	defer spy.mu.Unlock()
	var dirty []uint32
	for _, call := range spy.calls {
		for _, p := range call {
			if p.Cluster == gcCluster {
				dirty = append(dirty, p.Attribute)
			}
		}
	}
	if !slices.Contains(dirty, 0x0000) || !slices.Contains(dirty, 0x0003) {
		t.Fatalf("dirty Groupcast attributes %v, want Membership and UsedMcastAddrCount", dirty)
	}
}

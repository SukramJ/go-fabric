// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package groups

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/store"
)

func ptr[T any](v T) *T { return &v }

// TestGroupcastMembershipDerivation pins matter.js GroupcastServer
// #deriveMembership: a member is a group with Groupcast properties or a
// group table entry, never one known to GroupKeyMap alone; endpoints come
// from the table, KeySetId from GroupKeyMap (0xFFFF without a binding),
// the flags from the properties or the PerGroup-capable defaults.
func TestGroupcastMembershipDerivation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m, st := newTestManager(t)
	st.mapGroup(1, 0x0202, 0x10) // GroupKeyMap alone: no member
	if err := m.Reload(ctx, 1); err != nil {
		t.Fatal(err)
	}
	// Legacy group: table only.
	if err := m.AddEndpointForGroup(ctx, 1, 0x0101, 3, "Kitchen"); err != nil {
		t.Fatal(err)
	}
	// Groupcast group without a binding.
	if err := m.SetGroupProperties(ctx, 1, 0x0303, ptr(PolicyIanaAddr), ptr(true)); err != nil {
		t.Fatal(err)
	}
	got, err := m.GroupcastMemberships(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []GroupcastMembership{
		{FabricIndex: 1, GroupID: 0x0101, Endpoints: []uint16{3}, KeySetID: 0x10, McastAddrPolicy: PolicyPerGroup},
		{FabricIndex: 1, GroupID: 0x0303, Endpoints: []uint16{}, KeySetID: UnmappedKeySetID, McastAddrPolicy: PolicyIanaAddr, HasAuxiliaryACL: true},
	}
	if len(got) != len(want) {
		t.Fatalf("Membership = %+v, want %+v", got, want)
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.FabricIndex != w.FabricIndex || g.GroupID != w.GroupID || !slices.Equal(g.Endpoints, w.Endpoints) ||
			g.KeySetID != w.KeySetID || g.McastAddrPolicy != w.McastAddrPolicy || g.HasAuxiliaryACL != w.HasAuxiliaryACL {
			t.Errorf("Membership[%d] = %+v, want %+v", i, g, w)
		}
	}
}

// TestGroupPropertiesApplyOnlyGivenFields pins #upsertGroupProperties: a
// plain re-join must not reset HasAuxiliaryAcl ("does not overwrite
// hasAuxiliaryAcl on a plain re-join"), and the properties persist.
func TestGroupPropertiesApplyOnlyGivenFields(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m, st := newTestManager(t)
	if err := m.SetGroupProperties(ctx, 1, 0x0102, ptr(PolicyIanaAddr), ptr(true)); err != nil {
		t.Fatal(err)
	}
	if err := m.SetGroupProperties(ctx, 1, 0x0102, ptr(PolicyIanaAddr), nil); err != nil {
		t.Fatal(err)
	}
	if err := m.SetGroupProperties(ctx, 1, 0x0104, nil, nil); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewManager(st, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := restarted.GroupcastMemberships(ctx)
	if err != nil || len(got) != 2 {
		t.Fatalf("after a restart: %+v, %v", got, err)
	}
	if !got[0].HasAuxiliaryACL || got[0].McastAddrPolicy != PolicyIanaAddr {
		t.Errorf("0x0102 = %+v, want IanaAddr with its auxiliary ACL kept", got[0])
	}
	if got[1].HasAuxiliaryACL || got[1].McastAddrPolicy != DefaultMcastAddrPolicy {
		t.Errorf("0x0104 = %+v, want the defaults", got[1])
	}
}

// TestGroupcastPolicyMovesTheMulticastMembership pins Groups.multicastAddress
// and ServerGroupNetworking's rebind: an IanaAddr group is received on
// FF05::FA, a PerGroup or legacy group on its own address, and a policy
// change of a group with endpoints re-triggers the join reconciliation.
func TestGroupcastPolicyMovesTheMulticastMembership(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m, _ := newTestManager(t)
	var rebinds int
	m.OnMembershipChanged(func() { rebinds++ })
	if err := m.AddEndpointForGroup(ctx, 1, 0x0101, 3, ""); err != nil {
		t.Fatal(err)
	}
	if err := m.AddEndpointForGroup(ctx, 1, 0x0102, 3, ""); err != nil {
		t.Fatal(err)
	}
	rebinds = 0
	for _, gid := range []uint16{0x0101, 0x0102} {
		if err := m.SetGroupProperties(ctx, 1, gid, ptr(PolicyIanaAddr), nil); err != nil {
			t.Fatal(err)
		}
	}
	if rebinds != 2 {
		t.Errorf("policy changes of two member groups re-triggered the join %d times, want 2", rebinds)
	}
	ms, err := m.Memberships(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, mb := range ms {
		if !mb.Address.Equal(IANAGroupcastAddress) {
			t.Errorf("group %#x joins %s, want ff05::fa", mb.GroupID, mb.Address)
		}
	}
	// Unchanged properties are no change.
	rebinds = 0
	if err := m.SetGroupProperties(ctx, 1, 0x0101, ptr(PolicyIanaAddr), nil); err != nil || rebinds != 0 {
		t.Errorf("an unchanged policy rebound (%d, %v)", rebinds, err)
	}
	if err := m.SetGroupProperties(ctx, 1, 0x0102, ptr(PolicyPerGroup), nil); err != nil {
		t.Fatal(err)
	}
	if addr, _ := m.MulticastAddressFor(ctx, 1, 0x0102); !addr.Equal(MulticastAddress(0x0FAB, 0x0102)) {
		t.Errorf("PerGroup address = %s", addr)
	}
	if err := m.RemoveGroupProperties(ctx, 1, 0x0101); err != nil {
		t.Fatal(err)
	}
	if addr, _ := m.MulticastAddressFor(ctx, 1, 0x0101); !addr.Equal(MulticastAddress(0x0FAB, 0x0101)) {
		t.Errorf("a group without properties is on %s, want its per-group address", addr)
	}
	if err := m.RemoveGroupProperties(ctx, 1, 0x0101); err != nil {
		t.Errorf("removing absent properties: %v", err)
	}
	if _, err := m.MulticastAddressFor(ctx, 9, 1); !errors.Is(err, ErrNoFabric) {
		t.Errorf("unknown fabric: %v", err)
	}
}

// TestGroupLeavingTheTableLosesItsProperties pins the kDeleteGroupIfEmpty
// prune of GroupcastServer #applyPendingAndDerive: a Groupcast group whose
// last endpoint leaves the group table (a Groups RemoveGroup, say) stops
// being a member.
func TestGroupLeavingTheTableLosesItsProperties(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m, _ := newTestManager(t)
	if err := m.AddEndpointForGroup(ctx, 1, 0x0101, 3, ""); err != nil {
		t.Fatal(err)
	}
	if err := m.AddEndpointForGroup(ctx, 1, 0x0101, 4, ""); err != nil {
		t.Fatal(err)
	}
	if err := m.SetGroupProperties(ctx, 1, 0x0101, ptr(PolicyIanaAddr), ptr(true)); err != nil {
		t.Fatal(err)
	}
	if _, err := m.RemoveEndpoint(ctx, 1, 3, 0x0101, false); err != nil {
		t.Fatal(err)
	}
	if got, _ := m.GroupcastMemberships(ctx); len(got) != 1 || !got[0].HasAuxiliaryACL {
		t.Fatalf("one endpoint left, membership = %+v", got)
	}
	if _, err := m.RemoveEndpoint(ctx, 1, 4, 0, true); err != nil {
		t.Fatal(err)
	}
	if got, _ := m.GroupcastMemberships(ctx); len(got) != 0 {
		t.Fatalf("the emptied group is still a member: %+v", got)
	}
}

// TestAuxiliaryACLEntries pins GroupcastServer #emitAuxAcl: one Operate
// Group entry per member with HasAuxiliaryAcl and listener endpoints, the
// group id its subject and its endpoints the targets.
func TestAuxiliaryACLEntries(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m, st := newTestManager(t)
	st.addFabric(2, 0x0FAC, testCompressed)
	for _, step := range []struct {
		fabric uint8
		gid    uint16
		eps    []uint16
		aux    bool
	}{
		{1, 0x0101, []uint16{3, 4}, true},
		{1, 0x0102, []uint16{5}, false},
		{1, 0x0103, nil, true}, // no listener endpoint: no entry
		{2, 0x0101, []uint16{7}, true},
	} {
		for _, ep := range step.eps {
			if err := m.AddEndpointForGroup(ctx, step.fabric, step.gid, ep, ""); err != nil {
				t.Fatal(err)
			}
		}
		if err := m.SetGroupProperties(ctx, step.fabric, step.gid, nil, ptr(step.aux)); err != nil {
			t.Fatal(err)
		}
	}
	one, err := m.AuxiliaryACL(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(one) != 1 {
		t.Fatalf("fabric 1 auxiliary ACL = %+v, want one entry", one)
	}
	e := one[0]
	if e.FabricIndex != 1 || e.Privilege != store.PrivilegeOperate || e.AuthMode != store.AuthModeGroup ||
		!slices.Equal(e.Subjects, []uint64{0x0101}) || len(e.Targets) != 2 ||
		*e.Targets[0].Endpoint != 3 || *e.Targets[1].Endpoint != 4 || e.Targets[0].Cluster != nil || e.Targets[0].DeviceType != nil {
		t.Errorf("entry = %+v", e)
	}
	all, err := m.AuxiliaryACL(ctx, 0)
	if err != nil || len(all) != 2 || all[1].FabricIndex != 2 {
		t.Errorf("all fabrics = %+v, %v", all, err)
	}
}

// TestGroupcastChangeSignal: every source of Membership signals a change
// with the context of the request behind it, and a removed fabric's
// properties are forgotten with it.
func TestGroupcastChangeSignal(t *testing.T) {
	t.Parallel()
	type key struct{}
	ctx := context.WithValue(context.Background(), key{}, "request")
	m, st := newTestManager(t)
	var (
		mu   sync.Mutex
		seen []string
	)
	m.OnGroupcastChanged(nil)
	m.OnGroupcastChanged(func(c context.Context, fabric uint8) {
		mu.Lock()
		defer mu.Unlock()
		v, _ := c.Value(key{}).(string)
		seen = append(seen, v)
		if fabric != 1 {
			t.Errorf("signal for fabric %d", fabric)
		}
	})
	if err := m.AddEndpointForGroup(ctx, 1, 0x0101, 3, ""); err != nil {
		t.Fatal(err)
	}
	if err := m.SetGroupProperties(ctx, 1, 0x0101, nil, ptr(true)); err != nil {
		t.Fatal(err)
	}
	if err := m.Reload(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := m.RemoveEndpoint(ctx, 1, 3, 0x0101, false); err != nil {
		t.Fatal(err)
	}
	if err := m.SetGroupProperties(ctx, 1, 0x0101, nil, ptr(true)); err != nil {
		t.Fatal(err)
	}
	if err := m.RemoveGroupProperties(ctx, 1, 0x0101); err != nil {
		t.Fatal(err)
	}
	if err := m.SetGroupProperties(ctx, 1, 0x0102, nil, nil); err != nil {
		t.Fatal(err)
	}
	m.ForgetFabric(1)
	mu.Lock()
	got := slices.Clone(seen)
	mu.Unlock()
	if want := []string{"request", "request", "request", "request", "request", "request", "request", ""}; !slices.Equal(got, want) {
		t.Errorf("signals = %q, want %q", got, want)
	}
	if len(st.gcast[1]) != 1 {
		t.Fatalf("store rows = %+v (the store's cascade, not the manager, drops them)", st.gcast[1])
	}
}

// TestGroupcastStoreFailures surfaces persistence failures.
func TestGroupcastStoreFailures(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m, st := newTestManager(t)
	if err := m.SetGroupProperties(ctx, 1, 0x0101, nil, nil); err != nil {
		t.Fatal(err)
	}
	st.failPut = errors.New("disk full")
	if err := m.SetGroupProperties(ctx, 1, 0x0101, nil, ptr(true)); err == nil {
		t.Error("SetGroupProperties swallowed a store failure")
	}
	if err := m.RemoveGroupProperties(ctx, 1, 0x0101); err == nil {
		t.Error("RemoveGroupProperties swallowed a store failure")
	}
	if err := m.SetGroupProperties(ctx, 9, 1, nil, nil); !errors.Is(err, ErrNoFabric) {
		t.Errorf("unknown fabric: %v", err)
	}
	if err := m.RemoveGroupProperties(ctx, 9, 1); !errors.Is(err, ErrNoFabric) {
		t.Errorf("unknown fabric: %v", err)
	}
}

// TestGroupMessageListeners pins SessionManager.onGroupMessage: reports
// reach every listener until it unsubscribes, and cost nothing without one.
func TestGroupMessageListeners(t *testing.T) {
	t.Parallel()
	m, _ := newTestManager(t)
	m.ReportGroupMessage(GroupMessageEvent{Result: TestResultFailedAuth}) // no listener
	var a, b []GroupMessageEvent
	unA := m.OnGroupMessage(func(ev GroupMessageEvent) { a = append(a, ev) })
	unB := m.OnGroupMessage(func(ev GroupMessageEvent) { b = append(b, ev) })
	m.OnGroupMessage(nil)()
	m.ReportGroupMessage(GroupMessageEvent{Result: TestResultMessageReplay, FabricIndex: 1})
	unA()
	m.ReportGroupMessage(GroupMessageEvent{Result: TestResultSuccess})
	unB()
	m.ReportGroupMessage(GroupMessageEvent{Result: TestResultNoAvailableKey})
	if len(a) != 1 || a[0].Result != TestResultMessageReplay || len(b) != 2 || b[1].Result != TestResultSuccess {
		t.Errorf("a=%+v b=%+v", a, b)
	}
}

// TestDecodeNoKeyNamesAnUnmappedAuthentication pins GroupSession.decode's
// unmappedAuthentication: with no mapped key set at the session id, an
// unmapped one that opens the message names its fabric and group for the
// report, and the message is still refused.
func TestDecodeNoKeyNamesAnUnmappedAuthentication(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m, st := newTestManager(t)
	op, sid := opKeyOf(t, 0xA1)
	st.mapGroup(1, 0x0101, 0x77) // key set 0x10 is now mapped to nothing
	if err := m.Reload(ctx, 1); err != nil {
		t.Fatal(err)
	}
	_, err := m.Decode(ctx, sealGroup(t, op, sid, 0x0101, 1, 5, true, []byte("x")))
	var noKey *NoKeyError
	if !errors.As(err, &noKey) || !errors.Is(err, ErrNoKey) {
		t.Fatalf("Decode = %v, want a NoKeyError", err)
	}
	if !noKey.Authenticated || noKey.FabricIndex != 1 || noKey.GroupID != 0x0101 {
		t.Errorf("NoKeyError = %+v, want fabric 1 group 0x0101", noKey)
	}
	if noKey.Error() == ErrNoKey.Error() {
		t.Error("an authenticated NoKeyError does not say what authenticated")
	}
	// A key nobody holds authenticates nothing.
	other, otherSID := opKeyOf(t, 0x55)
	_, err = m.Decode(ctx, sealGroup(t, other, otherSID, 0x0101, 1, 6, false, []byte("x")))
	if !errors.As(err, &noKey) || noKey.Authenticated || noKey.Error() != ErrNoKey.Error() {
		t.Errorf("unknown key: %v", err)
	}
}

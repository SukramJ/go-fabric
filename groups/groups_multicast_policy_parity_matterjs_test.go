// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package groups

// A port of matter.js packages/protocol/test/groups/GroupsMulticastPolicyTest.ts
// at 862ad75f: the Groupcast multicast address policy of a group — per-group
// by default, the shared IANA address under IanaAddr, per group and
// reversible.
//
// matter.js sets the policy with fabric.groups.setGroupMulticastPolicy and
// removeGroupMulticastPolicy; the Go surface is Manager.SetGroupProperties
// (the policy alone) and RemoveGroupProperties. The cases share one fabric,
// as the matter.js describe does.

import (
	"context"
	"strings"
	"testing"
)

func TestGroupsMulticastPolicy(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := newMemStore()
	st.addFabric(1, 0x456789ABCDEF1234, [8]byte{}) // GroupsMulticastPolicyTest.ts:22
	m, err := NewManager(st, nil)
	if err != nil {
		t.Fatal(err)
	}
	addr := func(t *testing.T, gid uint16) string {
		t.Helper()
		a, err := m.MulticastAddressFor(ctx, 1, gid)
		if err != nil {
			t.Fatal(err)
		}
		return a.String()
	}
	wantPerGroup := func(t *testing.T, gid uint16) {
		t.Helper()
		a := addr(t, gid)
		if !strings.HasPrefix(a, "ff35:") || a == IANAGroupcastAddress.String() {
			t.Fatalf("group %#x on %s, want a per-group ff35: address", gid, a)
		}
	}
	wantIANA := func(t *testing.T, gid uint16) {
		t.Helper()
		if a := addr(t, gid); a != IANAGroupcastAddress.String() {
			t.Fatalf("group %#x on %s, want %s", gid, a, IANAGroupcastAddress)
		}
	}
	setPolicy := func(t *testing.T, gid uint16, policy uint8) {
		t.Helper()
		if err := m.SetGroupProperties(ctx, 1, gid, &policy, nil); err != nil {
			t.Fatal(err)
		}
	}
	removePolicy := func(t *testing.T, gid uint16) {
		t.Helper()
		if err := m.RemoveGroupProperties(ctx, 1, gid); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("derives a per-group IPv6 address by default (PerGroupId policy)", func(t *testing.T) {
		wantPerGroup(t, 0x1234)
	})

	t.Run("returns the shared IANA address when IanaAddr policy is set", func(t *testing.T) {
		setPolicy(t, 0x0001, PolicyIanaAddr)
		wantIANA(t, 0x0001)
	})

	t.Run("reverts to per-group derivation after policy removal", func(t *testing.T) {
		setPolicy(t, 0x0001, PolicyIanaAddr)
		removePolicy(t, 0x0001)
		wantPerGroup(t, 0x0001)
	})

	t.Run("different groups can have different policies independently", func(t *testing.T) {
		setPolicy(t, 0x0001, PolicyIanaAddr)
		setPolicy(t, 0x0002, PolicyPerGroup)
		wantIANA(t, 0x0001)
		wantPerGroup(t, 0x0002)
		removePolicy(t, 0x0001)
		removePolicy(t, 0x0002)
	})
}

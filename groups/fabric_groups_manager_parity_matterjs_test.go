// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package groups

// A port of matter.js packages/protocol/test/groups/FabricGroupsManagerTest.ts
// at 862ad75f: the group IPv6 multicast address of a fabric and group.
//
// matter.js reads fabric.groups.multicastAddressFor; the Go fabric's group
// state is the Manager's, so the case reads Manager.MulticastAddressFor of a
// fabric with the test's fabric id.

import (
	"context"
	"testing"
)

func TestFabricGroupsManagerGroupIPv6AddressCalculation(t *testing.T) {
	t.Parallel()

	t.Run("should calculate group IPv6 address", func(t *testing.T) {
		t.Parallel()
		st := newMemStore()
		st.addFabric(1, 0x456789ABCDEF1234, [8]byte{}) // FabricGroupsManagerTest.ts:23
		m, err := NewManager(st, nil)
		if err != nil {
			t.Fatal(err)
		}
		addr, err := m.MulticastAddressFor(context.Background(), 1, 0x1234)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := addr.String(), "ff35:40:fd45:6789:abcd:ef12:3400:1234"; got != want { // FabricGroupsManagerTest.ts:38
			t.Fatalf("multicastAddressFor(0x1234) = %s, want %s", got, want)
		}
	})
}

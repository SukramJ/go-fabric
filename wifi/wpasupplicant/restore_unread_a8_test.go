// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package wpasupplicant

import (
	"testing"

	"github.com/godbus/dbus/v5"
)

// TestAFailedJoinRestoresTheJoinedNetworkWhenTheCurrentOneIsUnreadable: a
// join that fails after its CurrentNetwork read failed still selects the
// network the driver joined before, while wpa_supplicant lists it, as the
// package doc promises for a failed join (review II, R4-fsm-4354).
func TestAFailedJoinRestoresTheJoinedNetworkWhenTheCurrentOneIsUnreadable(t *testing.T) {
	t.Parallel()
	const first = dbus.ObjectPath("/fi/w1/wpa_supplicant1/Interfaces/0/Networks/1")
	const second = dbus.ObjectPath("/fi/w1/wpa_supplicant1/Interfaces/0/Networks/2")
	b := newFakeBus()
	b.nextNetworks = []dbus.ObjectPath{first, second}
	d, _ := startDriver(t, b, Options{})
	if status, debug, _ := d.ConnectNetwork(t.Context(), []byte("one"), []byte(testCredential)); status != statusSuccess {
		t.Fatalf("first join = %d %q", status, debug)
	}
	b.mu.Lock()
	delete(b.props, "CurrentNetwork") // the second join's read fails
	b.onSelect = func(b *fakeBus, _ int) {
		b.setStateLocked("associating")
		b.setStateLocked(stateDisconnected)
	}
	b.mu.Unlock()
	if status, _, _ := d.ConnectNetwork(t.Context(), []byte("two"), []byte(testCredential)); status == statusSuccess {
		t.Fatal("the second join succeeded")
	}
	var selected []dbus.ObjectPath
	for _, c := range b.callsCopy() {
		if c.method == ifaceInterface+".SelectNetwork" {
			selected = append(selected, c.args[0].(dbus.ObjectPath))
		}
	}
	if len(selected) == 0 || selected[len(selected)-1] != first {
		t.Fatalf("SelectNetwork %v, want the first join's network selected again last", selected)
	}
}

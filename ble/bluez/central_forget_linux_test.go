// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build linux

package bluez

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/SukramJ/go-fabric/controller/ble"
	"github.com/SukramJ/go-fabric/transport/btp"
)

// TestCentral_ForgetsRemovedDevices: the central's per-device state is
// bounded by the devices BlueZ holds — a device object BlueZ removed
// (InterfacesRemoved) leaves no entry in discovered, addresses or
// devices, so its advertisement is not reported to a later discovery
// callback (review II, R2-P3-ble-04).
func TestCentral_ForgetsRemovedDevices(t *testing.T) {
	b := newFakeBus()
	c := startCentral(t, b)
	sentinel := make(chan struct{}, 1)
	c.SetDiscoveryCallback(func(p ble.Peripheral, _ []byte) {
		if p.Address == "AA:AA:AA:AA:AA:AA" {
			select {
			case sentinel <- struct{}{}:
			default:
			}
		}
	})
	if err := c.StartScanning(t.Context()); err != nil {
		t.Fatal(err)
	}
	sd := []byte{0, 0, 0x0f, 0xf1, 0xff, 0x00, 0x80, 0}
	const n = 2000
	for i := range n {
		addr := fmt.Sprintf("C0:%02X:%02X:%02X:%02X:01", byte(i>>24), byte(i>>16), byte(i>>8), byte(i))
		path := testAdapter + dbus.ObjectPath("/dev_"+strings.ReplaceAll(addr, ":", "_"))
		b.addObject(path, map[string]map[string]dbus.Variant{ifaceDevice: {
			"Address":     dbus.MakeVariant(addr),
			"ServiceData": dbus.MakeVariant(map[string]dbus.Variant{btp.ServiceUUID: dbus.MakeVariant(sd)}),
		}})
		// BlueZ drops the temporary device object again.
		b.emit(&dbus.Signal{Path: "/", Name: sigIfacesRemoved, Body: []any{path, []string{ifaceDevice}}})
	}
	b.addObject(testAdapter+"/dev_AA_AA_AA_AA_AA_AA", map[string]map[string]dbus.Variant{ifaceDevice: {
		"Address":     dbus.MakeVariant("AA:AA:AA:AA:AA:AA"),
		"ServiceData": dbus.MakeVariant(map[string]dbus.Variant{btp.ServiceUUID: dbus.MakeVariant(sd)}),
	}})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	select {
	case <-sentinel: // the signals are handled in order: every removal before it
	case <-ctx.Done():
		t.Fatal("the last advertisement was not reported")
	}
	c.mu.Lock()
	disc, addrs, devs := len(c.discovered), len(c.addresses), len(c.devices)
	c.mu.Unlock()
	if disc > 1 || addrs > 1 || devs > 1 {
		t.Fatalf("%d devices added and removed by BlueZ, one live: discovered %d, addresses %d, devices %d entries", n, disc, addrs, devs)
	}
	replayed := 0
	c.SetDiscoveryCallback(func(ble.Peripheral, []byte) { replayed++ })
	if replayed != 1 {
		t.Fatalf("a new discovery callback was handed %d advertisements, want the one live device's", replayed)
	}
}

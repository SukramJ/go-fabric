// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

//go:build linux

package bluez

import (
	"context"
	"slices"
	"testing"

	"github.com/godbus/dbus/v5"

	"thingfabric.dev/go-fabric/controller/ble"
	"thingfabric.dev/go-fabric/controller/ble/bletest"
)

// TestCentralCloseDisconnectsW2B1 holds #610 row 6: bluetoothd keeps a
// link when the bus name that connected it leaves (probe, scenario 5), so
// Close sends Device1.Disconnect for every connection and every path a
// Connect is in flight for, before the bus goes.
func TestCentralCloseDisconnectsW2B1(t *testing.T) {
	b, d, c, _, err := connectTo(t, 247, false)
	if err != nil {
		t.Fatal(err)
	}
	p2 := &bletest.Peripheral{Address: "AA:BB:CC:DD:EE:02", Discriminator: 3841}
	d2 := b.addDevice(p2, 247)
	d2.advertise(p2.ServiceData())
	connect := ifaceDevice + ".Connect"
	release := b.hold(connect)
	t.Cleanup(release)
	before := callsOf(b, connect)
	connected := make(chan error, 1)
	go func() {
		_, err := c.Connect(context.Background(), ble.Peripheral{Address: p2.Address})
		connected <- err
	}()
	waitUntil(t, "the second Connect in flight", func() bool { return callsOf(b, connect) > before })
	b.failWith(string(d2.path)+" "+connect, dbus.Error{Name: "org.bluez.Error.Failed"})

	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	calls := b.calledBeforeClose()
	for _, dev := range []dbus.ObjectPath{d.path, d2.path} {
		if !slices.Contains(calls, string(dev)+" "+ifaceDevice+".Disconnect") {
			t.Fatalf("no Device1.Disconnect for %s before the bus closed: %q", dev, calls)
		}
	}
	release()
	<-connected
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"bytes"
	"errors"
	"slices"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/clock/clocktest"
	netcommdef "thingfabric.dev/go-fabric/cluster/spec/networkcommissioning"
)

// TestWiFiConnectNetworkSwitchesADisabledInterfaceOn (N4-d4wifi-1): a
// ConnectNetwork on a switched-off interface switches it on before the
// join, whatever the network id — as connectedhomeip's Thread driver
// resets InterfaceEnabled to true at the start of its ConnectNetwork
// (GenericNetworkCommissioningThreadDriver.cpp:205-213 at 6170af84) — and
// the attribute reads true with the version advanced; the command's join
// is the only join. A driver that does not switch on fails the command,
// which then joins nothing.
func TestWiFiConnectNetworkSwitchesADisabledInterfaceOn(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	d := &switchDriver{simDriver: &simDriver{}}
	w, err := NewWiFiNetworkCommissioning(WiFiNetworkCommissioningConfig{
		Driver: d, IsFailSafeArmed: func() bool { return true }, SetBreadcrumb: func(uint64) {},
		Clock: clocktest.New(time.Unix(0, 0)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.MatterInvoke(ctx, netcommdef.CmdAddOrUpdateWiFiNetwork, netcommdef.AddOrUpdateWiFiNetworkRequest{Ssid: []byte("lab"), Credentials: []byte("secret12")}); err != nil {
		t.Fatal(err)
	}
	off := func() {
		t.Helper()
		if err := w.MatterWrite(ctx, netcommAttrInterfaceEnabled, false); err != nil {
			t.Fatal(err)
		}
	}
	connect := func(id string) error {
		_, err := w.MatterInvoke(ctx, netcommdef.CmdConnectNetwork, netcommdef.ConnectNetworkRequest{NetworkId: []byte(id)})
		return err
	}
	enabled := func() any { v, _ := w.MatterRead(netcommAttrInterfaceEnabled); return v }

	off()
	before := w.MatterDataVersion()
	if err := connect("lab"); err != nil {
		t.Fatalf("ConnectNetwork: %v", err)
	}
	if got := d.switched(); !slices.Equal(got, []bool{false, true}) {
		t.Errorf("switches %v, want the write's off and the command's on", got)
	}
	if enabled() != true || w.MatterDataVersion() == before {
		t.Errorf("InterfaceEnabled %v, DataVersion %d -> %d; want true and advanced", enabled(), before, w.MatterDataVersion())
	}
	d.mu.Lock()
	joins := slices.Clone(d.connects)
	d.mu.Unlock()
	if len(joins) != 1 || !bytes.Equal(joins[0], []byte("lab")) {
		t.Errorf("joins %q, want the command's one", joins)
	}

	// An id that names no network: switched on all the same, as chip.
	off()
	if err := connect("nowhere"); err != nil {
		t.Fatalf("ConnectNetwork of an unknown id: %v", err)
	}
	if enabled() != true {
		t.Error("an unknown network id left the interface off")
	}

	// A driver that does not switch on: the command fails and joins
	// nothing.
	off()
	d.answer(errors.New("rfkill"))
	if err := connect("lab"); err == nil {
		t.Fatal("ConnectNetwork succeeded on an interface the driver did not switch on")
	}
	if enabled() != false {
		t.Error("InterfaceEnabled reads true after a refused switch")
	}
	d.mu.Lock()
	n := len(d.connects)
	d.mu.Unlock()
	if n != 1 {
		t.Errorf("%d joins, want still the first one", n)
	}
}

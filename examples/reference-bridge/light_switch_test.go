// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/SukramJ/go-fabric/endpoint"
)

// TestLightSwitchFlag: with --light-switch the node bridges an On/Off
// Light Switch after every other device, free of device-type violations,
// with the Binding server and the OnOff client its type mandates; the app
// pipe's SendOnOffToBindings is refused without the flag, for another
// endpoint and for a command that is neither on nor off, and sends to no
// one while the Binding attribute is empty
// (LightSwitchTestInstance.ts:181-210 at 862ad75f).
func TestLightSwitchFlag(t *testing.T) {
	plain := newFleet()
	if err := plain.applyPipeCommand(pipeCommand{Name: "SendOnOffToBindings", Command: "on"}); err == nil {
		t.Error("SendOnOffToBindings accepted without --light-switch")
	}
	f := newFleet()
	sw, err := newDemoLightSwitch(f, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	f.lightSwitch = sw
	n := testNode(t, f, filepath.Join(t.TempDir(), "switch.db"))
	attachLightSwitch(sw, n)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := n.Start(ctx); err != nil {
		t.Fatal(err)
	}
	br := n.Bridge()
	ep := mountedEndpoint(t, br, "demo-lightswitch-1")
	if ep.DeviceType != 0x0103 {
		t.Fatalf("device type 0x%04X, want the On/Off Light Switch 0x0103", ep.DeviceType)
	}
	for _, other := range br.Topology().Endpoints {
		if other != nil && other.ID > ep.ID {
			t.Errorf("endpoint %d (%s) after the light switch %d: the switch must come last", other.ID, other.DeviceAddress, ep.ID)
		}
	}
	for _, v := range endpoint.ValidateDeviceTypes(br.Topology()) {
		if v.Endpoint == ep.ID {
			t.Errorf("device type violation: %s", v)
		}
	}
	readMounted(t, ep, 0x001E, 0xFFFD)
	if err := f.applyPipeCommand(pipeCommand{Name: "SendOnOffToBindings", EndpointID: ep.ID + 1, Command: "on"}); err == nil {
		t.Error("SendOnOffToBindings accepted for another endpoint")
	}
	if err := f.applyPipeCommand(pipeCommand{Name: "SendOnOffToBindings", EndpointID: ep.ID, Command: "toggle"}); err == nil {
		t.Error("SendOnOffToBindings accepted a command that is neither on nor off")
	}
	if err := f.applyPipeCommand(pipeCommand{Name: "SendOnOffToBindings", EndpointID: ep.ID, Command: "on"}); err != nil {
		t.Errorf("SendOnOffToBindings with no binding entry: %v", err)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/fan-control/FanControlTest.ts
// at 862ad75f. matter.js adds the device to a MockServerNode with the state
// it names and starts the node; the Go form is the server built over a
// host fan holding that state (the node validates nothing more of it), and
// the air purifier's OnOff the generated device mounts.
package fan_test

import (
	"context"
	"slices"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/fan"
	"github.com/SukramJ/go-fabric/cluster/onoff"
	godevice "github.com/SukramJ/go-fabric/device"
	"github.com/SukramJ/go-fabric/endpoint"
)

// TestFanControlServerParityMatterJS ports the describe "FanControlServer"
// (FanControlTest.ts:7-37).
func TestFanControlServerParityMatterJS(t *testing.T) {
	t.Parallel()

	// :8-21. FanMode On — deprecated, the comment asks whether a node takes
	// it — on OffLowMedHigh at 50 percent: the server builds and serves the
	// state it was given.
	t.Run("accepts local mode tags", func(t *testing.T) {
		t.Parallel()
		half := uint8(50)
		host := &device{st: fan.State{FanMode: fan.FanModeOn, PercentSetting: &half, PercentCurrent: 50}}
		srv, err := fan.NewServer(fan.Config{Source: host, Sequence: fan.SequenceOffLowMedHigh})
		if err != nil {
			t.Fatalf("building the server: %v", err)
		}
		if v, ok := srv.MatterRead(fan.AttrFanMode); !ok || v != uint8(fan.FanModeOn) {
			t.Errorf("FanMode %v, %v, want On", v, ok)
		}
	})

	// :23-36. An air purifier with OnOff mounted and its fan Off: OnOff's
	// default feature set is not OffOnly, so On is accepted and completes.
	t.Run("has not-off-only commands by default", func(t *testing.T) {
		t.Parallel()
		purifier, err := godevice.NewAirPurifier(godevice.Options{StableKey: endpoint.StringKey("purifier"), Name: "purifier"},
			godevice.WithOptional(onoff.ClusterID))
		if err != nil {
			t.Fatal(err)
		}
		srv, _ := purifier.OnOff().Underlying().(*onoff.Server)
		if srv == nil {
			t.Fatal("the air purifier mounted no OnOff")
		}
		if !slices.Contains(srv.MatterAcceptedCommands(), onoff.CmdOn) {
			t.Errorf("AcceptedCommandList %v lacks On", srv.MatterAcceptedCommands())
		}
		if _, err := srv.MatterInvoke(context.Background(), onoff.CmdOn, nil); err != nil {
			t.Fatalf("On: %v", err)
		}
	})
}

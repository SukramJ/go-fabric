// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fan_test

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/fan"
	fandef "github.com/SukramJ/go-fabric/cluster/spec/fancontrol"
)

// laterFan is a host that applies a write later: its state does not show
// the write when ApplyFanSettings returns. calls counts the setter calls.
type laterFan struct {
	mu    sync.Mutex
	st    fandef.State
	calls int
}

func (l *laterFan) FanState() fandef.State {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.st
}

func (l *laterFan) ApplyFanSettings(context.Context, fandef.Patch) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls++
	return nil
}

// TestFanWriteVerdictIsTheWrittenValueAgainstTheHeldOne pins that a
// write's verdict compares the written value with the one the host holds,
// before the host is called: a value equal to the held one reaches no
// setter and answers unchanged, a different one reaches the setter and
// answers changed — whatever the host's state reads when the setter
// returns. matter.js notes a change, and runs a reactor, only for a value
// that differs from the held one (packages/node/src/behavior/state/
// managed/Datasource.ts:1476-1492 at db021da0, #noteIfChanged), and its
// FanControlServer adds no behaviour to a write
// (packages/node/src/behaviors/fan-control/FanControlServer.ts:13-19).
func TestFanWriteVerdictIsTheWrittenValueAgainstTheHeldOne(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	held := fandef.State{
		FanMode: fan.FanModeLow, PercentSetting: u8(40), SpeedSetting: u8(4),
		RockSetting: fan.RockLeftRight, WindSetting: 0, AirflowDirection: fan.AirflowForward,
	}
	for _, tc := range []struct {
		name          string
		attr          uint32
		held, written any
	}{
		{"FanMode", fan.AttrFanMode, uint64(fan.FanModeLow), uint64(fan.FanModeHigh)},
		{"PercentSetting", fan.AttrPercentSetting, uint64(40), uint64(60)},
		{"SpeedSetting", fan.AttrSpeedSetting, uint64(4), uint64(6)},
		{"RockSetting", fan.AttrRockSetting, uint64(fan.RockLeftRight), uint64(fan.RockUpDown)},
		{"WindSetting", fan.AttrWindSetting, uint64(0), uint64(fan.WindNatural)},
		{"AirflowDirection", fan.AttrAirflowDirection, uint64(fan.AirflowForward), uint64(fan.AirflowReverse)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			host := &laterFan{st: held}
			srv, err := fan.NewServer(fan.Config{
				Source: host, Features: allFeatures, Sequence: fan.SequenceOffLowMedHighAuto,
				SpeedMax: 10, RockSupport: fan.RockLeftRight | fan.RockUpDown, WindSupport: fan.WindNatural,
			})
			if err != nil {
				t.Fatal(err)
			}
			v0 := srv.MatterDataVersion()
			if changed, err := srv.MatterWriteChanged(ctx, tc.attr, tc.held); err != nil || changed || host.calls != 0 || srv.MatterDataVersion() != v0 {
				t.Errorf("the held %v: changed %v, %v, setter calls %d, DataVersion %d → %d; want unchanged, no call, no advance",
					tc.held, changed, err, host.calls, v0, srv.MatterDataVersion())
			}
			if changed, err := srv.MatterWriteChanged(ctx, tc.attr, tc.written); err != nil || !changed || host.calls != 1 || srv.MatterDataVersion() != v0+1 {
				t.Errorf("%v over the held %v: changed %v, %v, setter calls %d, DataVersion %d → %d; want changed, one call, one advance",
					tc.written, tc.held, changed, err, host.calls, v0, srv.MatterDataVersion())
			}
		})
	}
}

// TestFanWriteCommitNamesTheSiblings (F-db021da0-57): a write names the
// attributes it changed — the written one, and each sibling the coupling
// rules hand the host whose value differs from the held one — and advances
// the version once; the held value names nothing. Mirrors matter.js: a
// commit names a property only when its value differs and advances once
// (Datasource.ts:1476-1481, :1456-1457, :1319 at db021da0).
func TestFanWriteCommitNamesTheSiblings(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, tc := range []struct {
		name    string
		held    fandef.State
		attr    uint32
		written any
		want    []uint32
	}{
		{"FanMode Off moves both settings", fandef.State{FanMode: fan.FanModeLow, PercentSetting: u8(40), SpeedSetting: u8(4)},
			fan.AttrFanMode, uint64(fan.FanModeOff), []uint32{fan.AttrFanMode, fan.AttrPercentSetting, fan.AttrSpeedSetting}},
		{"PercentSetting moves SpeedSetting", fandef.State{FanMode: fan.FanModeLow, PercentSetting: u8(40), SpeedSetting: u8(4)},
			fan.AttrPercentSetting, uint64(60), []uint32{fan.AttrPercentSetting, fan.AttrSpeedSetting}},
		{"a sibling already there is not named", fandef.State{FanMode: fan.FanModeLow, PercentSetting: u8(40), SpeedSetting: u8(6)},
			fan.AttrPercentSetting, uint64(60), []uint32{fan.AttrPercentSetting}},
		{"SpeedSetting 0 moves FanMode and PercentSetting", fandef.State{FanMode: fan.FanModeLow, PercentSetting: u8(40), SpeedSetting: u8(4)},
			fan.AttrSpeedSetting, uint64(0), []uint32{fan.AttrSpeedSetting, fan.AttrFanMode, fan.AttrPercentSetting}},
		{"the held value", fandef.State{FanMode: fan.FanModeLow, PercentSetting: u8(40), SpeedSetting: u8(4)},
			fan.AttrPercentSetting, uint64(40), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, err := fan.NewServer(fan.Config{
				Source: &laterFan{st: tc.held}, Features: allFeatures, Sequence: fan.SequenceOffLowMedHighAuto, SpeedMax: 10,
				RockSupport: fan.RockLeftRight | fan.RockUpDown, WindSupport: fan.WindNatural,
			})
			if err != nil {
				t.Fatal(err)
			}
			v0 := srv.MatterDataVersion()
			got, err := srv.MatterWriteCommit(ctx, tc.attr, tc.written)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("named %v, want %v", got, tc.want)
			}
			advance := uint32(0)
			if len(tc.want) > 0 {
				advance = 1
			}
			if srv.MatterDataVersion() != v0+advance {
				t.Errorf("version %d → %d, want +%d", v0, srv.MatterDataVersion(), advance)
			}
		})
	}
}

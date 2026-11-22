// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package pump_test

import (
	"context"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/cluster/pump"
)

// laterPump is a host that applies a write later: its state does not
// show the write when the setter returns. calls counts the setter calls.
type laterPump struct {
	mu    sync.Mutex
	st    pump.State
	calls int
}

func (l *laterPump) PumpState() pump.State {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.st
}

func (l *laterPump) call() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls++
	return nil
}

func (l *laterPump) SetOperationMode(context.Context, pump.OperationMode) error { return l.call() }
func (l *laterPump) SetControlMode(context.Context, pump.ControlMode) error     { return l.call() }
func (l *laterPump) SetLifetimeRunningHours(context.Context, *uint32) error     { return l.call() }
func (l *laterPump) SetLifetimeEnergyConsumed(context.Context, *uint32) error   { return l.call() }

// TestPumpWriteVerdictIsTheWrittenValueAgainstTheHeldOne pins that a
// write's verdict compares the written value with the one the host holds,
// before the host is called: a value equal to the held one reaches no
// setter and answers unchanged, a different one reaches the setter and
// answers changed — whatever the host's state reads when the setter
// returns. matter.js notes a change, and runs a reactor, only for a value
// that differs from the held one (packages/node/src/behavior/state/
// managed/Datasource.ts:1476-1492 at db021da0, #noteIfChanged), and its
// PumpConfigurationAndControlServer adds no behaviour to a write
// (packages/node/src/behaviors/pump-configuration-and-control/
// PumpConfigurationAndControlServer.ts:18).
func TestPumpWriteVerdictIsTheWrittenValueAgainstTheHeldOne(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	held := pump.State{
		OperationMode: pump.OperationNormal, ControlMode: pump.ControlConstantSpeed,
		LifetimeRunningHours: u32(10), LifetimeEnergyConsumed: u32(10),
	}
	for _, tc := range []struct {
		name          string
		attr          uint32
		held, written any
	}{
		{"OperationMode", pump.AttrOperationMode, uint64(pump.OperationNormal), uint64(pump.OperationMaximum)},
		{"ControlMode", pump.AttrControlMode, uint64(pump.ControlConstantSpeed), uint64(pump.ControlConstantPressure)},
		{"LifetimeRunningHours", pump.AttrLifetimeRunningHours, uint64(10), uint64(20)},
		{"LifetimeRunningHours null", pump.AttrLifetimeRunningHours, uint64(10), nil},
		{"LifetimeEnergyConsumed", pump.AttrLifetimeEnergyConsumed, uint64(10), uint64(20)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			host := &laterPump{st: held}
			srv, err := pump.NewServer(pump.Config{
				Source: host,
				Features: pump.FeatureConstantPressure | pump.FeatureConstantSpeed | pump.FeatureAutomatic |
					pump.FeatureLocalOperation,
				Optional: everyOptional,
				Limits: pump.Limits{
					MaxPressure: i16(1000), MaxSpeed: u16(3000),
					MinConstPressure: i16(100), MaxConstPressure: i16(900),
					MinConstSpeed: u16(500), MaxConstSpeed: u16(0xFFFF),
				},
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

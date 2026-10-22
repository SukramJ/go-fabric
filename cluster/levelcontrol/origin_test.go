// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package levelcontrol_test

import (
	"context"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/cluster/levelcontrol"
	lvldef "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"
	"github.com/SukramJ/go-fabric/cluster/spec/spectest"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/contract"
)

// The origin audits of the LevelControl server (gate-2 consumer, defects 1
// and 2, brief BY-1 §1): every path that changes CurrentLevel reaches the
// change listeners — the host's OnChanged — at once, with its origin, as
// every observer of matter.js's currentLevel$Changed sees each committed
// change (packages/node/src/behavior/state/managed/Datasource.ts:1473-1476);
// a controller's write reaches them as the written attribute.

func moveTo(level uint8) spectest.OriginCase {
	return spectest.OriginCase{
		Name: "command", Want: contract.OriginCommand, Attributes: []uint32{levelcontrol.AttrCurrentLevel},
		Act: func(tb testing.TB, srv contract.ClusterServer) {
			tb.Helper()
			ctx := contract.WithOrigin(context.Background(), contract.OriginCommand)
			if _, err := srv.MatterInvoke(ctx, levelcontrol.CmdMoveToLevel, wire.MoveToLevelRequest{Level: level, OptionsMask: levelcontrol.OptionExecuteIfOff, OptionsOverride: levelcontrol.OptionExecuteIfOff}); err != nil {
				tb.Fatal(err)
			}
		},
	}
}

// TestHeldLevelControlOrigins audits the kit's held server: the host's
// Set, a MoveToLevel, the OnOff coupling and the three writable
// attributes.
func TestHeldLevelControlOrigins(t *testing.T) {
	t.Parallel()
	var onOff *typedOnOff
	spectest.CheckOrigins(t, func(tb testing.TB) contract.ClusterServer {
		tb.Helper()
		onOff = &typedOnOff{}
		typed, srv := heldServer(t, onOff)
		if err := typed.Set(lvldef.Patch{CurrentLevel: ptr(uint8(10)), OnLevel: ptr(uint8(77))}); err != nil {
			tb.Fatal(err)
		}
		return srv
	},
		spectest.OriginCase{Name: "host set", Want: contract.OriginHostSet, Attributes: []uint32{levelcontrol.AttrCurrentLevel}, Act: func(tb testing.TB, srv contract.ClusterServer) {
			tb.Helper()
			if err := lvldef.Wrap(srv).Set(lvldef.Patch{CurrentLevel: ptr(uint8(20))}); err != nil {
				tb.Fatal(err)
			}
		}},
		moveTo(200),
		spectest.OriginCase{Name: "OnOff turns on", Want: contract.OriginCommand, Attributes: []uint32{levelcontrol.AttrCurrentLevel}, Act: func(tb testing.TB, _ contract.ClusterServer) {
			tb.Helper()
			// OnLevel is set before the audit listens (the server's own
			// state); OnOff turning on by a command moves the level to it,
			// from the OnOff change's origin (matter.js handleOnOffChange
			// reacts within that session).
			onOff.origin = contract.OriginCommand
			if err := onOff.SetOnOff(context.Background(), true); err != nil {
				tb.Fatal(err)
			}
		}},
		spectest.Write(levelcontrol.AttrOptions, uint64(levelcontrol.OptionExecuteIfOff)),
		spectest.Write(levelcontrol.AttrOnLevel, uint64(30)),
		spectest.Write(levelcontrol.AttrStartUpCurrentLevel, uint64(77)),
	)
}

// TestPortLevelControlOrigins audits the server over a host's LevelSource
// without the engine (the hand-off path): the device's own change through
// its notifier, a forwarded MoveToLevel, and the two writable attributes
// the port holds.
func TestPortLevelControlOrigins(t *testing.T) {
	t.Parallel()
	var d *device
	spectest.CheckOrigins(t, func(tb testing.TB) contract.ClusterServer {
		tb.Helper()
		d = newDevice(10, true)
		return levelcontrol.NewServer(levelcontrol.Config{Source: d, Lighting: true})
	},
		spectest.OriginCase{Name: "the device moved", Want: contract.OriginHostSet, Attributes: []uint32{levelcontrol.AttrCurrentLevel}, Act: func(tb testing.TB, _ contract.ClusterServer) {
			tb.Helper()
			if err := d.MoveToLevel(context.Background(), lvldef.MoveToLevelRequest{Level: 40}); err != nil {
				tb.Fatal(err)
			}
		}},
		moveTo(200),
		spectest.Write(levelcontrol.AttrOptions, uint64(levelcontrol.OptionExecuteIfOff)),
		spectest.Write(levelcontrol.AttrOnLevel, uint64(30)),
	)
}

// TestEngineLevelControlOrigins audits the server over a LevelSource with
// Transitions: the device's own change and a MoveToLevel the engine
// applies.
func TestEngineLevelControlOrigins(t *testing.T) {
	t.Parallel()
	var d *device
	spectest.CheckOrigins(t, func(tb testing.TB) contract.ClusterServer {
		tb.Helper()
		d = newDevice(10, true)
		return engineServer(d, nil)
	},
		spectest.OriginCase{Name: "the device moved", Want: contract.OriginHostSet, Attributes: []uint32{levelcontrol.AttrCurrentLevel}, Act: func(tb testing.TB, _ contract.ClusterServer) {
			tb.Helper()
			if err := d.MoveToLevel(context.Background(), lvldef.MoveToLevelRequest{Level: 40}); err != nil {
				tb.Fatal(err)
			}
		}},
		moveTo(200),
	)
}

// TestHostSeesEveryChangeTheWireThrottles: a host's Sets inside one
// second reach the change listeners each at once, from the host, while
// the wire (contract.AttributeReportNotifier) reports the first at once
// and the last once the quieter's interval has passed — matter.js's
// QuietEvent throttles only its `online` view
// (packages/node/src/behavior/Events.ts:267-297, ServerBehaviorBacking.ts
// :265-273).
func TestHostSeesEveryChangeTheWireThrottles(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		srv, err := levelcontrol.New(levelcontrol.HeldConfig{Features: levelcontrol.FeatureLighting, CurrentLevel: ptr(uint8(10))})
		if err != nil {
			t.Fatal(err)
		}
		var host []contract.Origin
		srv.OnAttributeChange(func(c contract.Change) {
			if slices.Contains(c.Attributes, levelcontrol.AttrCurrentLevel) {
				host = append(host, c.Origin)
			}
		})
		onWire := listen(t, srv)
		for _, l := range []uint8{20, 30, 40} {
			if err := srv.SetAttributes(map[uint32]any{levelcontrol.AttrCurrentLevel: l}); err != nil {
				t.Fatal(err)
			}
		}
		if want := []contract.Origin{contract.OriginHostSet, contract.OriginHostSet, contract.OriginHostSet}; !slices.Equal(host, want) {
			t.Errorf("the host saw %v, want three changes from the host", host)
		}
		if levels, _ := onWire.snapshot(); !slices.Equal(levels, []uint8{20}) {
			t.Errorf("the wire reported %v at once, want [20]", levels)
		}
		sleep(2 * time.Second)
		if levels, _ := onWire.snapshot(); !slices.Equal(levels, []uint8{20, 40}) {
			t.Errorf("the wire reported %v after the interval, want [20 40]", levels)
		}
	})
}

// TestHostSeesEveryStepOfATransition: a managed transition's steps reach
// the change listeners each, from the command, ending on the target once;
// the wire reports at most one a second (TC-LVL-2.3's bound).
func TestHostSeesEveryStepOfATransition(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		d := newDevice(1, true)
		srv := engineServer(d, nil)
		host := observe(t, srv)
		onWire := listen(t, srv)
		if err := invoke(t, srv, levelcontrol.CmdMoveToLevel, wire.MoveToLevelRequest{Level: 254, TransitionTime: u16(50)}); err != nil {
			t.Fatal(err)
		}
		sleep(10 * time.Second)
		steps, _ := host.snapshot()
		reports, _ := onWire.snapshot()
		if len(steps) <= len(reports) || !slices.IsSorted(steps) || steps[len(steps)-1] != 254 || slices.Index(steps, 254) != len(steps)-1 {
			t.Errorf("the host saw %v, want every step rising and the target 254 once, more than the wire's %v", steps, reports)
		}
		if len(reports) == 0 || len(reports) > 7 || reports[len(reports)-1] != 254 {
			t.Errorf("the wire reported %v, want at most one a second, ending on 254", reports)
		}
	})
}

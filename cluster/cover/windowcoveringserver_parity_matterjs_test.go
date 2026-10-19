// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/window-covering/WindowCoveringServerTest.ts
// at 862ad75f: OperationalStatus following the target positions of a
// lift-and-tilt blind, and the Type / EndProductType checks of a shutter.
//
// matter.js runs the cases on a MockEndpoint, calling the server locally;
// the port calls the Go server in its package.
//
//   - TestWindowCoveringServer overrides handleMovement with a no-op so the
//     positions do not jump to the target and the status changes stay
//     visible. The Go form of a movement that has not arrived is a
//     travelling one (Config.MoveStep) whose clock the test never moves.
//     matter.js captures operationalStatus$Changing and $Changed; Go has
//     no pre-commit observer, so the port asserts the committed
//     OperationalStatus around each command — the oldValue and newValue
//     of each $Changed event — as the attribute value a controller reads.
//   - matter.js's shutter servers are WindowCoveringServer.with("Lift") and
//     .with("Tilt"); the Go server serves an axis position-aware only
//     (ServedFeatureMap, ServedTiltFeatureMap), which leaves the enum
//     conformances ("LF & !TL | !LF & TL", "LF & TL", "LF & !TL") the
//     cases test unchanged. matter.js's MatterAggregateError with an
//     EnumValueConformanceError cause is cover.ErrEnumValue from cover.New.
package cover_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/cover"
	wcdef "github.com/SukramJ/go-fabric/cluster/spec/windowcovering"
)

// operationalStatus is the OperationalStatus bitmap of a { global, lift,
// tilt } value (window-covering-cluster.element.ts: global bits 0-1, lift
// 2-3, tilt 4-5).
func operationalStatus(global, lift, tilt uint8) uint8 { return global | lift<<2 | tilt<<4 }

// createTiltBlindLift is WindowCoveringServerTest.ts:13-37: Lift, Tilt,
// PositionAwareLift, PositionAwareTilt, a TiltBlindLift InteriorVenetianBlind
// half open and half tilted.
func createTiltBlindLift(t *testing.T) *cover.WindowCoveringServer {
	t.Helper()
	srv, err := cover.New(cover.Config{
		FeatureMap: cover.ServedLiftTiltFeatureMap,
		Type:       uint8(wcdef.TypeTiltBlindLift), EndProductType: uint8(wcdef.EndProductTypeInteriorVenetianBlind),
		InitialPositionPercent100ths: 5000, InitialTiltPositionPercent100ths: 5000,
		MoveStep: time.Second, Clock: clocktest.New(time.Unix(0, 0)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func TestParityMatterJS_WindowCoveringServerTest(t *testing.T) {
	t.Parallel()
	t.Run("WindowCoveringServer", func(t *testing.T) {
		t.Parallel()
		t.Run("emits operationalStatus change events when target position changes", func(t *testing.T) {
			t.Parallel()
			srv := createTiltBlindLift(t)
			read := func() uint8 {
				v, _ := srv.MatterRead(wcdef.AttrOperationalStatus)
				u, _ := v.(uint8)
				return u
			}
			// The three operationalStatus$Changed events
			// (WindowCoveringServerTest.ts:64-68, :79-83, :94-98).
			for _, step := range []struct {
				name     string
				cmd      uint32
				old, new uint8
			}{
				{"downOrClose", wcdef.CmdDownOrClose, operationalStatus(0, 0, 0), operationalStatus(2, 2, 2)},
				{"upOrOpen", wcdef.CmdUpOrOpen, operationalStatus(2, 2, 2), operationalStatus(1, 1, 1)},
				{"stopMotion", wcdef.CmdStopMotion, operationalStatus(1, 1, 1), operationalStatus(0, 0, 0)},
			} {
				if got := read(); got != step.old {
					t.Fatalf("%s: OperationalStatus before 0x%02X, want 0x%02X", step.name, got, step.old)
				}
				if _, err := srv.MatterInvoke(context.Background(), step.cmd, nil); err != nil {
					t.Fatalf("%s: %v", step.name, err)
				}
				if got := read(); got != step.new {
					t.Fatalf("%s: OperationalStatus after 0x%02X, want 0x%02X", step.name, got, step.new)
				}
			}
		})
	})

	t.Run("WindowCoveringServer type", func(t *testing.T) {
		t.Parallel()
		// shutter, WindowCoveringServerTest.ts:113-116.
		shutter := cover.Config{Type: uint8(wcdef.TypeShutter), EndProductType: uint8(wcdef.EndProductTypeSwingingShutter)}
		with := func(c cover.Config, features uint32) cover.Config {
			c.FeatureMap = features
			return c
		}
		for _, c := range []struct {
			name string
			cfg  cover.Config
			ok   bool
		}{
			{"accepts a shutter that lifts", with(shutter, cover.ServedFeatureMap), true},
			{"accepts a shutter that tilts", with(shutter, cover.ServedTiltFeatureMap), true},
			{"rejects a shutter that lifts and tilts", cover.Config{
				FeatureMap: cover.ServedLiftTiltFeatureMap,
				Type:       uint8(wcdef.TypeShutter), EndProductType: uint8(wcdef.EndProductTypeInteriorVenetianBlind),
			}, false},
			{"rejects an end product that only lifts on a covering that lifts and tilts", cover.Config{
				FeatureMap: cover.ServedLiftTiltFeatureMap,
				Type:       uint8(wcdef.TypeTiltBlindLift), EndProductType: uint8(wcdef.EndProductTypeRollerShade),
			}, false},
		} {
			t.Run(c.name, func(t *testing.T) {
				t.Parallel()
				_, err := cover.New(c.cfg)
				switch {
				case c.ok && err != nil:
					t.Fatalf("refused: %v", err)
				case !c.ok && !errors.Is(err, cover.ErrEnumValue):
					t.Fatalf("New: %v, want ErrEnumValue", err)
				}
			})
		}
	})
}

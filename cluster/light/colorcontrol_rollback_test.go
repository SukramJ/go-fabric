// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package light

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	ccdef "github.com/SukramJ/go-fabric/cluster/spec/colorcontrol"
	"github.com/SukramJ/go-fabric/contract"
)

// rbScenes counts the scene invalidations a server asks for.
type rbScenes struct{ invalidated atomic.Int32 }

func (p *rbScenes) MakeAllFabricSceneInfoEntriesInvalid()    { p.invalidated.Add(1) }
func (p *rbScenes) StoreGlobalScene(context.Context, uint8)  {}
func (p *rbScenes) RecallGlobalScene(context.Context, uint8) {}

// errDeviceRefused is the refusal of rbWriter.
var errDeviceRefused = errors.New("device refused")

// rbWriter refuses every colour while refuse is set.
type rbWriter struct{ refuse atomic.Bool }

func (w *rbWriter) SetColor(context.Context, Color) error {
	if w.refuse.Load() {
		return errDeviceRefused
	}
	return nil
}

// TestFailedColorCommandKeepsTheScene holds R2-P6-err-1404 over every
// command that ends the current scene: a command the device refuses
// leaves the scene valid with the rest of the state it rolls back, and the
// same command succeeding ends it once. The table is the class's guard —
// a command that invalidates the scene joins it.
func TestFailedColorCommandKeepsTheScene(t *testing.T) {
	cases := []struct {
		name   string
		cmd    uint32
		fields any
	}{
		{"MoveToSaturation", ccdef.CmdMoveToSaturation, ccdef.MoveToSaturationRequest{Saturation: 200}},
		{"MoveToHueAndSaturation", ccdef.CmdMoveToHueAndSaturation, ccdef.MoveToHueAndSaturationRequest{Hue: 100, Saturation: 200}},
		{"MoveToColor", ccdef.CmdMoveToColor, ccdef.MoveToColorRequest{ColorX: 20000, ColorY: 20000}},
		{"EnhancedMoveToHueAndSaturation", ccdef.CmdEnhancedMoveToHueAndSaturation, ccdef.EnhancedMoveToHueAndSaturationRequest{EnhancedHue: 1000, Saturation: 200}},
		{"ColorLoopSet", ccdef.CmdColorLoopSet, ccdef.ColorLoopSetRequest{UpdateFlags: ccdef.UpdateFlagsUpdateAction | ccdef.UpdateFlagsUpdateStartHue | ccdef.UpdateFlagsUpdateTime, Action: ccdef.ColorLoopActionActivateFromColorLoopStartEnhancedHue, StartHue: 5000, Time: 10}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hs := ColorModeHueSaturation
			s, err := NewColorControl(ColorControlServerConfig{
				Features:         ColorFeatureHueSaturation | ColorFeatureEnhancedHue | ColorFeatureColorLoop | ColorFeatureXY,
				InitialColorMode: &hs,
			})
			if err != nil {
				t.Fatal(err)
			}
			sc, w := &rbScenes{}, &rbWriter{}
			s.SetScenes(sc)
			s.SetColorWriter(w)
			w.refuse.Store(true)
			if _, err := s.MatterInvoke(context.Background(), tc.cmd, tc.fields); !errors.Is(err, errDeviceRefused) {
				t.Fatalf("err = %v, want the device's refusal", err)
			}
			if n := sc.invalidated.Load(); n != 0 {
				t.Errorf("a failed %s invalidated the scene %d time(s); the rollback keeps it", tc.name, n)
			}
			w.refuse.Store(false)
			if _, err := s.MatterInvoke(context.Background(), tc.cmd, tc.fields); err != nil {
				t.Fatal(err)
			}
			if n := sc.invalidated.Load(); n != 1 {
				t.Errorf("a successful %s invalidated the scene %d time(s), want 1", tc.name, n)
			}
		})
	}
}

// TestColorQuiesceDropsQuieterReports holds R2-P6-fsm-1403: a report the
// quieters hold back when the server is quiesced is dropped with its
// timer, as LevelControl's MatterQuiesce drops its own.
func TestColorQuiesceDropsQuieterReports(t *testing.T) {
	fc := clocktest.New(time.Unix(1000, 0))
	hs := ColorModeHueSaturation
	s, err := NewColorControl(ColorControlServerConfig{Features: ColorFeatureHueSaturation, InitialColorMode: &hs, Clock: fc})
	if err != nil {
		t.Fatal(err)
	}
	var reports atomic.Int32
	s.OnAttributeReport(func(contract.Change) { reports.Add(1) })
	s.Set(Color{Mode: ColorModeHueSaturation, Hue: 10, Saturation: 10})
	fc.Advance(100 * time.Millisecond)
	s.Set(Color{Mode: ColorModeHueSaturation, Hue: 20, Saturation: 20})
	if fc.Pending() == 0 {
		t.Fatal("no report is held back; the test proves nothing")
	}
	s.MatterQuiesce()
	at := reports.Load()
	fc.Advance(2 * time.Second)
	if n := reports.Load() - at; n != 0 {
		t.Errorf("%d report(s) after MatterQuiesce, want 0", n)
	}
	if n := fc.Pending(); n != 0 {
		t.Errorf("%d timer(s) armed after MatterQuiesce, want 0", n)
	}
	// The quieters stay usable.
	s.Set(Color{Mode: ColorModeHueSaturation, Hue: 30, Saturation: 30})
	fc.Advance(2 * time.Second)
	if reports.Load() == at {
		t.Error("no report after quiesce for a later change")
	}
}

// TestHueTransitionRateFromTheEngineRead holds R2-P6-conc-1407's rate
// half: MoveToHue's rate follows the hue the engine reads when it starts
// the transition (RateFrom), not a value the command read before, which a
// step of a running transition may have moved since.
func TestHueTransitionRateFromTheEngineRead(t *testing.T) {
	hs := ColorModeHueSaturation
	s, err := NewColorControl(ColorControlServerConfig{Features: ColorFeatureHueSaturation, InitialColorMode: &hs})
	if err != nil {
		t.Fatal(err)
	}
	tr := s.hueTransition(100, ccdef.DirectionUp, 10, false)
	if tr.RateFrom == nil {
		t.Fatal("hueTransition carries no RateFrom: its rate comes from a read before the engine's")
	}
	if got := tr.RateFrom(60); got != 40 {
		t.Errorf("RateFrom(60) towards 100 over 1 s = %v, want 40", got)
	}
}

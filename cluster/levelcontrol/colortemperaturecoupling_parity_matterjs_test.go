// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/level-control/ColorTemperatureCouplingTest.ts
// at 862ad75f: LevelControl's CoupleColorTempToLevel option moving a colour
// temperature light's ColorTemperatureMireds with the level — through the
// Options attribute and per-command overrides, for MoveToLevel, Step and
// Move, every step of a managed transition, two commands in a row and a
// coupling that waits for a ColorControl command in flight.
//
// The cases run on a MockServerNode in matter.js; the port runs them on a
// fabrictest colour temperature light (parity_matterjs_helpers_test.go)
// with matter.js's state: on, level 254, ColorControl in the colour
// temperature mode at 300 mireds, physical range 153-370,
// CoupleColorTempToLevelMinMireds 200. The earlier local port with the
// physical minimum as coupling floor stays (TestColorTemperatureCoupling).
//
// Three Go forms differ from the matter.js setup, the expectations
// unchanged:
//
//   - the Go LevelControl server under the engine always manages its
//     transitions; a command with a null transition time applies at once
//     as without managedTransitionTimeHandling, and the Move at rate 254
//     takes the second its rate asks for, which the port lets pass before
//     it reads the end state;
//   - two commands in one matter.js transaction are two Go commands in one
//     Device.Act: each commits when it returns;
//   - a transaction that holds the colour temperature is a ColorControl
//     command in flight, which holds the ColorControl server's command
//     lock until its writer returns, as the transaction holds the state's
//     lock until it commits.
//
// "leaves the color temperature where a later command in the transaction
// overrides the coupling off" is the register's missing case: matter.js
// couples once, on the commit of the transaction, with the options of its
// last command; a Go command couples when it commits, so the first command
// of the pair has coupled the colour temperature before the second clears
// the coupling.
package levelcontrol_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/levelcontrol"
	"github.com/SukramJ/go-fabric/cluster/light"
	ccdef "github.com/SukramJ/go-fabric/cluster/spec/colorcontrol"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/node"
)

// The mireds of ColorTemperatureCouplingTest.ts:37-44.
const (
	miredsAtMinLevel = 370 // MIREDS_AT_MIN_LEVEL: the physical maximum
	miredsUncoupled  = 300 // MIREDS_UNCOUPLED: before any coupling
	miredsAtMaxLevel = 200 // MIREDS_AT_MAX_LEVEL: coupleColorTempToLevelMinMireds
)

// coupleOption is { coupleColorTempToLevel: true }.
const coupleOption = levelcontrol.OptionCoupleColorTempToLevel

// coupledColorLight is ColorTemperatureCouplingTest.ts:12-33.
func coupledColorLight(t *testing.T, options uint8) *nodeLight {
	t.Helper()
	ct := light.ColorModeColorTemperature
	return newNodeLight(t, nodeLightOptions{level: 254, on: true, options: options, color: &light.ColorControlServerConfig{
		Features:  light.ColorFeatureColorTemperature,
		MinMireds: 153, MaxMireds: 370, InitialMireds: miredsUncoupled,
		CoupleColorTempToLevelMinMireds: miredsAtMaxLevel,
		InitialColorMode:                &ct,
	}})
}

// request is REQUEST (ColorTemperatureCouplingTest.ts:35): level 1, a null
// transition time, no override; at sets the level, override the
// coupling override (nil: none).
func request(level uint8, override *bool) wire.MoveToLevelRequest {
	r := wire.MoveToLevelRequest{Level: level}
	if override != nil {
		r.OptionsMask = coupleOption
		if *override {
			r.OptionsOverride = coupleOption
		}
	}
	return r
}

func ptrBool(v bool) *bool { return &v }

func (l *nodeLight) wantMireds(t *testing.T, want uint16) {
	t.Helper()
	if got := l.mireds(t); got != want {
		t.Errorf("ColorTemperatureMireds %d, want %d", got, want)
	}
}

func (l *nodeLight) wantLevel(t *testing.T, want uint8) {
	t.Helper()
	if got := level(t, l.srv); got != want {
		t.Errorf("CurrentLevel %d, want %d", got, want)
	}
}

func TestParityMatterJS_ColorTemperatureCouplingTest(t *testing.T) {
	t.Run("couples a WithOnOff level change when the Options attribute asks for it", func(t *testing.T) {
		l := coupledColorLight(t, coupleOption)
		l.invoke(t, levelcontrol.CmdMoveToLevelWithOnOff, request(1, nil))
		l.wantMireds(t, miredsAtMinLevel)
	})

	t.Run("scales the color temperature between the coupling minimum and the physical maximum", func(t *testing.T) {
		l := coupledColorLight(t, coupleOption)
		l.invoke(t, levelcontrol.CmdMoveToLevelWithOnOff, request(128, nil))
		// 370 - floor((370 - 200) * 128 / 254).
		l.wantMireds(t, 285)
	})

	t.Run("couples a WithOnOff step when the Options attribute asks for it", func(t *testing.T) {
		l := coupledColorLight(t, coupleOption)
		l.invoke(t, levelcontrol.CmdStepWithOnOff, wire.StepRequest{StepMode: levelcontrol.StepModeDown, StepSize: 126})
		l.wantLevel(t, 128)
		l.wantMireds(t, 285)
	})

	t.Run("couples a WithOnOff move when the Options attribute asks for it", func(t *testing.T) {
		l := coupledColorLight(t, coupleOption)
		l.invoke(t, levelcontrol.CmdMoveWithOnOff, wire.MoveRequest{MoveMode: levelcontrol.MoveModeDown, Rate: u8(254)})
		l.advance(1, time.Second) // the managed move at 254 per second
		l.wantLevel(t, 1)
		l.wantMireds(t, miredsAtMinLevel)
	})

	t.Run("couples every step of a managed transition", func(t *testing.T) {
		l := coupledColorLight(t, coupleOption)
		l.invoke(t, levelcontrol.CmdMoveWithOnOff, wire.MoveRequest{MoveMode: levelcontrol.MoveModeDown, Rate: u8(100)})
		l.advance(1, time.Second)
		if mid := l.mireds(t); mid <= miredsAtMaxLevel || mid >= miredsAtMinLevel {
			t.Errorf("mid-transition ColorTemperatureMireds %d, want between %d and %d", mid, miredsAtMaxLevel, miredsAtMinLevel)
		}
		l.advance(1, 3*time.Second)
		l.wantLevel(t, 1)
		l.wantMireds(t, miredsAtMinLevel)
	})

	t.Run("couples two level changes that share one transaction", func(t *testing.T) {
		l := coupledColorLight(t, coupleOption)
		l.invokeAll(t, request(128, nil), request(1, nil))
		l.wantLevel(t, 1)
		l.wantMireds(t, miredsAtMinLevel)
	})

	t.Run("couples where a later command in the transaction overrides the coupling on", func(t *testing.T) {
		l := coupledColorLight(t, 0)
		l.invokeAll(t, request(128, nil), request(1, ptrBool(true)))
		l.wantMireds(t, miredsAtMinLevel)
	})

	t.Run("waits for a transaction that holds the color temperature", testWaitsForColorTemperature)

	t.Run("couples a level change when the Options attribute asks for it", func(t *testing.T) {
		l := coupledColorLight(t, coupleOption)
		l.invoke(t, levelcontrol.CmdMoveToLevel, request(1, nil))
		l.wantMireds(t, miredsAtMinLevel)
	})

	t.Run("leaves the color temperature alone where the Options attribute does not ask", func(t *testing.T) {
		l := coupledColorLight(t, 0)
		l.invoke(t, levelcontrol.CmdMoveToLevelWithOnOff, request(1, nil))
		l.wantMireds(t, miredsUncoupled)
	})

	t.Run("honors an override that sets the coupling for one command", func(t *testing.T) {
		l := coupledColorLight(t, 0)
		l.invoke(t, levelcontrol.CmdMoveToLevelWithOnOff, request(1, ptrBool(true)))
		l.wantMireds(t, miredsAtMinLevel)
	})

	t.Run("honors an override that clears the coupling for one command", func(t *testing.T) {
		l := coupledColorLight(t, coupleOption)
		l.invoke(t, levelcontrol.CmdMoveToLevelWithOnOff, request(1, ptrBool(false)))
		l.wantMireds(t, miredsUncoupled)
	})
}

// invokeAll runs MoveToLevelWithOnOff with each request inside one
// Device.Act: two commands of one matter.js transaction.
func (l *nodeLight) invokeAll(t *testing.T, reqs ...wire.MoveToLevelRequest) {
	t.Helper()
	if err := l.dev.Act(func(*node.Node) error {
		for _, r := range reqs {
			if _, err := l.srv.MatterInvoke(context.Background(), levelcontrol.CmdMoveToLevelWithOnOff, r); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// blockingWriter holds the first colour temperature it receives until
// released: the ColorControl command that writes it holds the server's
// command lock meanwhile.
type blockingWriter struct {
	once     sync.Once
	entered  chan struct{}
	release  chan struct{}
	mu       sync.Mutex
	received []uint16
}

func (w *blockingWriter) SetColorTemperatureMireds(_ context.Context, mireds uint16) error {
	w.mu.Lock()
	w.received = append(w.received, mireds)
	w.mu.Unlock()
	first := false
	w.once.Do(func() { first = true })
	if first {
		close(w.entered)
		<-w.release
	}
	return nil
}

// testWaitsForColorTemperature is ColorTemperatureCouplingTest.ts:197-230.
func testWaitsForColorTemperature(t *testing.T) {
	l := coupledColorLight(t, coupleOption)
	w := &blockingWriter{entered: make(chan struct{}), release: make(chan struct{})}
	l.color.SetWriter(w)
	holder := make(chan error, 1)
	go func() {
		_, err := l.color.MatterInvoke(context.Background(), ccdef.CmdMoveToColorTemperature,
			ccdef.MoveToColorTemperatureRequest{ColorTemperatureMireds: 250})
		holder <- err
	}()
	<-w.entered // the holder holds the colour temperature
	coupled := make(chan error, 1)
	go func() {
		_, err := l.srv.MatterInvoke(context.Background(), levelcontrol.CmdMoveToLevelWithOnOff, request(1, nil))
		coupled <- err
	}()
	l.site.Fake().Yield()
	close(w.release)
	if err := <-holder; err != nil {
		t.Fatal(err)
	}
	if err := <-coupled; err != nil {
		t.Fatal(err)
	}
	l.wantMireds(t, miredsAtMinLevel)
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.received) != 2 || w.received[0] != 250 || w.received[1] != miredsAtMinLevel {
		t.Errorf("writes %v, want the holder's 250 and then the coupling's %d", w.received, miredsAtMinLevel)
	}
}

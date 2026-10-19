// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of matter.js packages/node/test/behaviors/color-control/ColorControlServerTest.ts
// at 862ad75f: the case the register listed as missing that has a Go form —
// StopMoveStep leaving a running colour loop and its remaining time alone.
// The cyclic hue transition and the other stop cases are ported locally
// (cluster/transition TestCyclicHueDownwards, TestStopMoveStepReportsZero,
// TestRemainingTimeOnStops).
//
// matter.js runs the case on a MockServerNode; the port runs it on a
// fabrictest extended colour light (an OnOff server and the ColorControl
// server with every feature, colorLightState's state, :162-181), the
// commands local calls inside Device.Act, MockTime.advance the site's fake
// clock. Two cases stay the register's missing ones:
//
//   - "accepts a primary a light supplies without stating the number of
//     primaries": the Go ColorControl server serves no Primary1X …
//     Primary6Intensity attributes, so a light cannot supply one;
//   - "clears the end time the application states on StopMoveStep": the Go
//     server keeps no application-stated transitionEndTime
//     (notes/parity/by_design.md BD-Matter-LevelControl-NativeRamp).
package light_test

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/light"
	"github.com/SukramJ/go-fabric/cluster/onoff"
	ccdef "github.com/SukramJ/go-fabric/cluster/spec/colorcontrol"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
)

// colorLoopActive is ColorLoopActive Active (color-control.element.ts:160).
const colorLoopActive uint8 = 1

// deviceTypeExtendedColorLight is ExtendedColorLight (0x010D).
const deviceTypeExtendedColorLight uint16 = 0x010D

// colorNode is setup() (ColorControlServerTest.ts:109-150): a started
// fabrictest device serving the light, and the RemainingTime reports.
type colorNode struct {
	site *fabrictest.Site
	dev  *fabrictest.Device
	srv  *light.ColorControlServer

	mu      sync.Mutex
	reports []uint16
}

func setupColorNode(t *testing.T) *colorNode {
	t.Helper()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	onOff, err := onoff.New(onoff.Config{Features: onoff.FeatureLT, Initial: map[uint32]any{onoff.AttrOnOff: true}, Bridged: true, TimeSource: site.Clock()})
	if err != nil {
		t.Fatal(err)
	}
	hs := light.ColorModeHueSaturation
	srv, err := light.NewColorControl(light.ColorControlServerConfig{
		Features: light.ColorFeatureHueSaturation | light.ColorFeatureEnhancedHue | light.ColorFeatureColorLoop |
			light.ColorFeatureXY | light.ColorFeatureColorTemperature,
		MinMireds: 153, MaxMireds: 370, CoupleColorTempToLevelMinMireds: 153,
		InitialColorMode: &hs, ManageTransitions: true, OnOff: onOff, Clock: site.Clock(),
	})
	if err != nil {
		t.Fatal(err)
	}
	c := &colorNode{site: site, srv: srv}
	srv.OnMatterAttributesChanged(func(ids []uint32) {
		if !slices.Contains(ids, ccdef.AttrRemainingTime) {
			return
		}
		v, _ := srv.MatterRead(ccdef.AttrRemainingTime)
		rt, _ := v.(uint16)
		c.mu.Lock()
		c.reports = append(c.reports, rt)
		c.mu.Unlock()
	})
	c.dev = site.AddDevice(fabrictest.Topology{Bridged: []node.Endpoint{fabrictest.Endpoint("light:1", deviceTypeExtendedColorLight, onOff, srv)}})
	return c
}

// invoke is setup()'s invoke: one local command.
func (c *colorNode) invoke(t *testing.T, cmd uint32, fields any) {
	t.Helper()
	if err := c.dev.Act(func(*node.Node) error {
		_, err := c.srv.MatterInvoke(context.Background(), cmd, fields)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func (c *colorNode) wantReports(t *testing.T, want ...uint16) {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if !slices.Equal(c.reports, want) {
		t.Errorf("RemainingTime reports %v, want %v", c.reports, want)
	}
}

func (c *colorNode) read(t *testing.T, attr uint32) any {
	t.Helper()
	v, ok := c.srv.MatterRead(attr)
	if !ok {
		t.Fatalf("attribute 0x%04X not served", attr)
	}
	return v
}

func TestParityMatterJS_ColorControlServerTest(t *testing.T) {
	t.Run("ColorControl stop", func(t *testing.T) {
		t.Run("leaves the remaining time alone on StopMoveStep while a color loop runs", func(t *testing.T) {
			c := setupColorNode(t)
			c.invoke(t, ccdef.CmdColorLoopSet, ccdef.ColorLoopSetRequest{
				UpdateFlags: ccdef.UpdateFlagsUpdateAction, Action: ccdef.ColorLoopActionActivateFromEnhancedCurrentHue,
				Direction: ccdef.ColorLoopDirectionIncrement, Time: 25, StartHue: 0,
			})
			c.invoke(t, ccdef.CmdMoveToSaturation, ccdef.MoveToSaturationRequest{Saturation: 254, TransitionTime: 100})
			c.wantReports(t, 100)
			c.invoke(t, ccdef.CmdStopMoveStep, ccdef.StopMoveStepRequest{})
			c.wantReports(t, 100)
			if got := c.read(t, ccdef.AttrColorLoopActive); got != colorLoopActive {
				t.Errorf("ColorLoopActive %v, want Active", got)
			}
			before := c.read(t, ccdef.AttrEnhancedCurrentHue)
			c.site.Fake().Advance(time.Second)
			c.site.Fake().Yield()
			if after := c.read(t, ccdef.AttrEnhancedCurrentHue); after == before {
				t.Errorf("EnhancedCurrentHue still %v a second later: the loop stopped", after)
			}
		})
	})
}

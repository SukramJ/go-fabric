// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package levelcontrol_test

// Shared support of the ports of matter.js packages/node/test/behaviors/
// level-control at 862ad75f that run on a MockServerNode: the Go
// counterpart of `node.add(DimmableLightDevice | ColorTemperatureLightDevice,
// { onOff, levelControl, colorControl })` on fabrictest — a device whose
// light is a bridged endpoint serving an OnOff server, the LevelControl
// server under the transition engine (managedTransitionTimeHandling; with
// a transition time of null every command applies at once, as it does
// without it) and, for a colour temperature light, the ColorControl server
// the level couples to. `node.online({ command: true }, agent => …)` and
// `endpoint.act(…)` are the servers' MatterInvoke inside Device.Act;
// MockTime.advance is the site's fake clock Advance.

import (
	"context"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/cluster/levelcontrol"
	"github.com/SukramJ/go-fabric/cluster/light"
	"github.com/SukramJ/go-fabric/cluster/onoff"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/fabrictest"
	"github.com/SukramJ/go-fabric/node"
)

// The device types of the lights (schema/devicetypes.go).
const (
	deviceTypeDimmableLight         uint16 = 0x0101
	deviceTypeColorTemperatureLight uint16 = 0x010C
)

// lightOn is the On/Off cluster the LevelControl server couples to: the
// host's lamp ([device]) and the endpoint's OnOff server, which stores and
// reports what the lamp switched to.
type lightOn struct {
	*device
	srv *onoff.Server
}

func (l *lightOn) SetOnOff(ctx context.Context, on bool) error {
	if err := l.device.SetOnOff(ctx, on); err != nil {
		return err
	}
	l.srv.Set(on)
	return nil
}

// nodeLightOptions is the initial state of the light.
type nodeLightOptions struct {
	level   uint8
	on      bool
	onLevel *uint8
	options uint8
	// color, when set, adds the ColorControl server of a colour
	// temperature light; its OnOff and Clock are filled in.
	color *light.ColorControlServerConfig
	// clock, when set, paces the LevelControl server instead of the
	// site's clock — a test that counts the server's own timers.
	clock clock.Clock
}

// nodeLight is a started fabrictest device serving one light.
type nodeLight struct {
	site  *fabrictest.Site
	dev   *fabrictest.Device
	lamp  *lightOn
	srv   *levelcontrol.Server
	color *light.ColorControlServer
}

func newNodeLight(t *testing.T, o nodeLightOptions) *nodeLight {
	t.Helper()
	site := fabrictest.NewSite(t, fabrictest.Options{})
	clk := o.clock
	if clk == nil {
		clk = site.Clock()
	}
	d := newDevice(o.level, o.on)
	d.onLevel, d.options = o.onLevel, o.options
	onOffSrv, err := onoff.New(onoff.Config{Features: onoff.FeatureLT, Initial: map[uint32]any{onoff.AttrOnOff: o.on}, Bridged: true, TimeSource: site.Clock()})
	if err != nil {
		t.Fatal(err)
	}
	l := &nodeLight{site: site, lamp: &lightOn{device: d, srv: onOffSrv}}
	tr := &levelcontrol.Transitions{OnOff: l.lamp, Clock: clk}
	servers := []contract.ClusterServer{onOffSrv}
	deviceType := deviceTypeDimmableLight
	if o.color != nil {
		cfg := *o.color
		cfg.OnOff, cfg.Clock = l.lamp, site.Clock()
		color, err := light.NewColorControl(cfg)
		if err != nil {
			t.Fatal(err)
		}
		l.color, tr.ColorTemperature, deviceType = color, color, deviceTypeColorTemperatureLight
	}
	l.srv = levelcontrol.NewServer(levelcontrol.Config{Source: d, Lighting: true, Transitions: tr})
	servers = append(servers, l.srv)
	if l.color != nil {
		servers = append(servers, l.color)
	}
	l.dev = site.AddDevice(fabrictest.Topology{Bridged: []node.Endpoint{fabrictest.Endpoint("light:1", deviceType, servers...)}})
	return l
}

// invoke is one local command on the light's LevelControl server.
func (l *nodeLight) invoke(t *testing.T, cmd uint32, fields any) {
	t.Helper()
	if err := l.dev.Act(func(*node.Node) error {
		_, err := l.srv.MatterInvoke(context.Background(), cmd, fields)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

// advance is n × MockTime.advance(d).
func (l *nodeLight) advance(n int, d time.Duration) {
	for range n {
		l.site.Fake().Advance(d)
	}
}

// mireds reads ColorTemperatureMireds.
func (l *nodeLight) mireds(t *testing.T) uint16 {
	t.Helper()
	v, ok := l.color.MatterRead(colorTemperatureMireds)
	m, isU16 := v.(uint16)
	if !ok || !isU16 {
		t.Fatalf("ColorTemperatureMireds %v (%T)", v, v)
	}
	return m
}

// colorTemperatureMireds is ColorControl's ColorTemperatureMireds (0x0007).
const colorTemperatureMireds uint32 = 0x0007

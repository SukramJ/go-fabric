// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"log/slog"
	"sync"

	"github.com/SukramJ/go-fabric/cluster/levelcontrol"
	"github.com/SukramJ/go-fabric/cluster/light"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/contract"
)

// deviceTypeColorTemperatureLight is ColorTemperatureLight (matter.js
// packages/model/src/standard/elements/color-temperature-light.element.ts).
// Besides Identify and Groups, which the assembler mounts, it requires
// OnOff and LevelControl with the LIGHTING feature, ScenesManagement and
// ColorControl with ColorTemperature.
const deviceTypeColorTemperatureLight uint16 = 0x010C

// lightingSurface is what turns a [dimmer] into a colour-temperature light:
// the OnOff Lighting state, the LevelControl Lighting attributes and the
// colour temperature. Its servers hold state of their own (the ColorControl
// server keeps the current mireds in-process, the LevelControl server
// StartUpCurrentLevel and the running transition), so the device builds
// them once and hands the same instances to every reassembly — a fresh
// server per call would reset the colour temperature to its initial value
// whenever the topology is rebuilt.
type lightingSurface struct {
	once    sync.Once
	servers []contract.ClusterServer
}

// newDemoCeilingLight returns a colour-temperature light that is off at
// half brightness.
func newDemoCeilingLight(name string) *dimmer {
	return &dimmer{
		name:     name,
		kind:     "ceiling",
		minLevel: levelcontrol.LightingLevelMin,
		level:    127,
		lighting: &lightingSurface{},
	}
}

// lightingServers builds the light's cluster surface once.
//
// The lamp cannot ramp, so both servers run the module's transition engine
// (cluster/transition, matter.js behavior/Transitions.ts): LevelControl
// with the Lighting feature steps the level through the dimmer's
// [dimmer.MoveToLevel] every 100 ms and couples it to the dimmer's On/Off
// state and to the colour temperature; ColorControl steps the colour
// temperature through [ceilingColorWriter]. RemainingTime is live, and the
// CHIP cases that read a level or a colour temperature part-way through a
// transition (TC-LVL-3.1 to 6.1, TC-CC-6.2, 6.3) and count its reports
// (TC-LVL-2.3, TC-CC-2.2) run against it. The speaker keeps the hand-off
// path, where the device is assumed to ramp natively.
func (s *dimmer) lightingServers() []contract.ClusterServer {
	s.lighting.once.Do(func() {
		cfg := light.DefaultColorControlServerConfig()
		cfg.ManageTransitions = true
		cfg.OnOff = s
		color := light.NewColorControlServer(cfg)
		color.SetWriter(ceilingColorWriter{name: s.name})
		s.lighting.servers = []contract.ClusterServer{
			&onOffServer{dev: s, logMessage: "ceiling.set", lt: newLightingState()},
			levelcontrol.NewServer(levelcontrol.Config{
				Source:      s,
				DataVersion: &s.version,
				Lighting:    true,
				Transitions: &levelcontrol.Transitions{OnOff: s, ColorTemperature: color},
			}),
			color,
			wire.ScenesManagement{},
		}
	})
	return s.lighting.servers
}

// ceilingColorWriter is where a real host would push the colour
// temperature to the lamp. Here it only logs, which is what the chip-tool
// suite reads back as proof the command reached the device. A transition
// pushes every step; those are logged at debug level, so the log shows one
// line per command.
type ceilingColorWriter struct{ name string }

// SetColorTemperatureMireds implements [light.ColorTemperatureWriter].
func (w ceilingColorWriter) SetColorTemperatureMireds(_ context.Context, mireds uint16) error {
	slog.Debug("ceiling.color", slog.String("device", w.name), slog.Int("mireds", int(mireds)))
	return nil
}

// OnOff implements [levelcontrol.OnOff] and [light.OnOffState]: the On/Off
// state the light's LevelControl and ColorControl gate and couple on.
func (s *dimmer) OnOff() bool { return s.isOn() }

// SetOnOff implements [levelcontrol.OnOff]: a "with On/Off" level command
// switches the light as its On/Off cluster does, OnLevel included
// ([dimmer.setOn]).
func (s *dimmer) SetOnOff(_ context.Context, on bool) error {
	s.setOn(on)
	slog.Info(s.kind+".set", slog.String("device", s.name), slog.Bool("on", on))
	return nil
}

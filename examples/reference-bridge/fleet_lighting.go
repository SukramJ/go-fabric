// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/levelcontrol"
	"github.com/SukramJ/go-fabric/cluster/light"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
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
// server keeps the current mireds in-process), so the device builds them
// once and hands the same instances to every reassembly — a fresh server per
// call would reset the colour temperature to its initial value whenever the
// topology is rebuilt.
type lightingSurface struct {
	once    sync.Once
	servers []contract.ClusterServer

	mu sync.Mutex
	// startUpCurrentLevel is the LT attribute StartUpCurrentLevel (0x4000):
	// null keeps the previous level at start-up. Stored and reported, never
	// applied, for the reason [onOffServer] gives for StartUpOnOff: matter.js
	// skips start-up behaviour on an endpoint owned by an Aggregator.
	startUpCurrentLevel *uint8
}

// newDemoCeilingLight returns a colour-temperature light that is off at
// half brightness.
func newDemoCeilingLight(name string) *dimmer {
	return &dimmer{
		name:     name,
		kind:     "ceiling",
		minLevel: ltMinLevel,
		level:    127,
		lighting: &lightingSurface{},
	}
}

// lightingServers builds the light's cluster surface once.
//
// The LevelControl server is the module's own, wrapped in [ltLevelServer]:
// cluster/levelcontrol serves exactly the conformance-M attributes and
// leaves the Lighting ones to "a host that needs them", which extends the
// port and the FeatureMap together. That is this file.
func (s *dimmer) lightingServers() []contract.ClusterServer {
	s.lighting.once.Do(func() {
		color := light.NewColorControlServer(light.DefaultColorControlServerConfig())
		color.SetWriter(ceilingColorWriter{name: s.name})
		s.lighting.servers = []contract.ClusterServer{
			&onOffServer{dev: s, logMessage: "ceiling.set", lt: newLightingState()},
			&ltLevelServer{
				Server:  levelcontrol.NewServer(levelcontrol.Config{Source: s, DataVersion: &s.version}),
				dev:     s,
				surface: s.lighting,
			},
			color,
			wire.ScenesManagement{},
		}
	})
	return s.lighting.servers
}

// ceilingColorWriter is where a real host would push the colour
// temperature to the lamp. Here it only logs, which is what the chip-tool
// suite reads back as proof the command reached the device.
type ceilingColorWriter struct{ name string }

// SetColorTemperatureMireds implements [light.ColorTemperatureWriter].
func (w ceilingColorWriter) SetColorTemperatureMireds(_ context.Context, mireds uint16) error {
	slog.Info("ceiling.color", slog.String("device", w.name), slog.Int("mireds", int(mireds)))
	return nil
}

// LevelControl Lighting-feature attributes the module's server leaves to the
// host (matter.js packages/model/src/standard/elements/
// level-control.element.ts: RemainingTime :33 "LT", MinLevel / MaxLevel
// :34-41, StartUpCurrentLevel :68-71 "LT").
const (
	ltAttrRemainingTime       uint32 = 0x0001
	ltAttrMinLevel            uint32 = 0x0002
	ltAttrMaxLevel            uint32 = 0x0003
	ltAttrStartUpCurrentLevel uint32 = 0x4000

	// ltMinLevel and ltMaxLevel are the only values the Lighting feature
	// allows (matter.js LevelControlServer.ts:205-220 initializeLighting
	// warns on anything else).
	ltMinLevel uint8 = 1
	ltMaxLevel uint8 = 0xFE

	// ltOptionsMask is ExecuteIfOff plus CoupleColorTempToLevel, the two
	// OptionsBitmap bits whose conformance LT satisfies (element :127-130).
	ltOptionsMask uint8 = levelcontrol.OptionExecuteIfOff | 1<<1
)

// ltLevelServer is the module's LevelControl server with the Lighting
// feature added on top: FeatureMap OO|LT, the four LT attributes, and the
// Options bit LT makes conformant. Everything else — the eight commands,
// CurrentLevel, OnLevel — is the wrapped server's.
//
// The device has no transitions (see [dimmer]), so RemainingTime is always
// 0: nothing is ever in flight.
type ltLevelServer struct {
	*levelcontrol.Server
	dev     *dimmer
	surface *lightingSurface
}

// MatterRead implements [contract.ClusterServer].
func (s *ltLevelServer) MatterRead(attrID uint32) (any, bool) {
	switch attrID {
	case cluster.AttrGlobalFeatureMap:
		return levelcontrol.FeatureOnOff | levelcontrol.FeatureLighting, true
	case ltAttrRemainingTime:
		return uint16(0), true
	case ltAttrMinLevel:
		return ltMinLevel, true
	case ltAttrMaxLevel:
		return ltMaxLevel, true
	case ltAttrStartUpCurrentLevel:
		s.surface.mu.Lock()
		defer s.surface.mu.Unlock()
		if s.surface.startUpCurrentLevel == nil {
			return nil, true
		}
		return *s.surface.startUpCurrentLevel, true
	}
	return s.Server.MatterRead(attrID)
}

// MatterAttributes implements [contract.ClusterAttributeLister].
func (s *ltLevelServer) MatterAttributes() []uint32 {
	// The wrapped server already lists MinLevel / MaxLevel; LT adds
	// RemainingTime and StartUpCurrentLevel.
	ids := append(s.Server.MatterAttributes(), ltAttrRemainingTime, ltAttrStartUpCurrentLevel)
	slices.Sort(ids)
	return ids
}

// MatterWrite implements [contract.ClusterServer]. Options is validated
// against the LT mask here, because the wrapped server checks it against the
// OO-only FeatureMap it advertises on its own.
func (s *ltLevelServer) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	switch attrID {
	case levelcontrol.AttrOptions:
		v, ok := cluster.AsUint8(value)
		if !ok || value == nil {
			return statusErr{im.StatusConstraintError, fmt.Sprintf("Options expects a map8, got %v", value)}
		}
		if v&^ltOptionsMask != 0 {
			return statusErr{im.StatusConstraintError, fmt.Sprintf("Options 0x%02X sets a bit outside 0x%02X", v, ltOptionsMask)}
		}
		if err := s.dev.SetOptions(ctx, v); err != nil {
			return err
		}
		s.dev.version.Bump()
		return nil
	case ltAttrStartUpCurrentLevel:
		var next *uint8
		if value != nil {
			v, ok := cluster.AsUint8(value)
			if !ok || v > ltMaxLevel {
				return statusErr{im.StatusConstraintError, fmt.Sprintf("StartUpCurrentLevel expects 0..254 or null, got %v", value)}
			}
			next = &v
		}
		s.surface.mu.Lock()
		s.surface.startUpCurrentLevel = next
		s.surface.mu.Unlock()
		s.dev.version.Bump()
		return nil
	case ltAttrRemainingTime, ltAttrMinLevel, ltAttrMaxLevel:
		return statusErr{im.StatusUnsupportedWrite, fmt.Sprintf("attribute 0x%04X is read-only", attrID)}
	}
	return s.Server.MatterWrite(ctx, attrID, value)
}

// MinWritePrivilege implements [contract.ClusterAttributeWritePrivilege]:
// StartUpCurrentLevel is "RW VM" (element :68), Options and OnLevel "RW VO".
func (s *ltLevelServer) MinWritePrivilege(attrID uint32) uint8 {
	if attrID == ltAttrStartUpCurrentLevel {
		return privilegeManage
	}
	return privilegeOperate
}

// Access-control privileges (Matter §9.10.5.2, AccessControlEntryPrivilegeEnum).
const (
	privilegeOperate uint8 = 3
	privilegeManage  uint8 = 4
)

// statusErr answers a write or invoke with a specific Interaction Model
// status — the [im.StatusCodeError] shape the dispatcher maps.
type statusErr struct {
	code im.StatusCode
	msg  string
}

func (e statusErr) Error() string                   { return e.msg }
func (e statusErr) MatterStatusCode() im.StatusCode { return e.code }

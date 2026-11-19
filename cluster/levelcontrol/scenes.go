// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package levelcontrol

import (
	"context"

	"github.com/SukramJ/go-fabric/cluster/onoff"
	lvldef "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/contract"
)

// scenesRef boxes the scenes port for the atomic pointer.
type scenesRef struct{ sc onoff.Scenes }

// SetScenes sets the endpoint's ScenesManagement port; nil removes it. The
// endpoint layer calls it with the endpoint's ScenesManagement where it
// mounts one, as it does for the OnOff and ColorControl servers. matter.js
// LevelControlServer reaches ScenesManagementServer through its agent
// (packages/node/src/behaviors/level-control/LevelControlServer.ts:145,
// :760-763 at db021da0).
func (s *Server) SetScenes(sc onoff.Scenes) {
	if sc == nil {
		s.scenes.Store(nil)
		return
	}
	s.scenes.Store(&scenesRef{sc})
}

// MatterInvalidatesScenes implements cluster/core's SceneInvalidator:
// with a scenes port the server invalidates the current scene itself.
// Under the engine it does so from the eight commands after the
// ExecuteIfOff gate, as matter.js does (LevelControlServer.ts:269-276,
// :293, :354-359, :373, :421-426, :442, :482-487, :498 at db021da0): a
// plain command the gate turns into a no-op leaves the scene valid, a
// "with On/Off" one always ends it. On the hand-off path the gate of a
// [NativeSource] is the module's and is honoured the same way; a host's
// port applies its gate where the server cannot see it, so every
// command the port accepts ends the scene, as does every command a
// handler the host registered through [Server.Handle] accepts.
func (s *Server) MatterInvalidatesScenes() bool { return s.scenes.Load() != nil }

// invalidateScenes is matter.js #invalidateScenes
// (LevelControlServer.ts:760-763 at b25ed960):
// ScenesManagementServer.makeAllFabricSceneInfoEntriesInvalid, when the
// endpoint has a ScenesManagement.
func (s *Server) invalidateScenes() {
	if r := s.scenes.Load(); r != nil {
		r.sc.MakeAllFabricSceneInfoEntriesInvalid()
	}
}

// gatedSource is a source that applies the ExecuteIfOff gate of the plain
// commands itself and can say whether it lets a command through: the
// module's [NativeSource].
type gatedSource interface {
	gateAllows(mask, override uint8) bool
}

// gateAllows is the gate [NativeSource] applies to a plain command
// (matter.js LevelControlServer.ts:729-736 #optionsAllowExecution).
func (n *NativeSource) gateAllows(mask, override uint8) bool {
	return n.allowsExecution(n.effective(mask, override))
}

// handOffExecutes reports whether a command forwarded on the hand-off path
// executes, for the scene invalidation: a "with On/Off" command always
// does; a plain one does unless a source that applies the gate itself
// refuses it. A host's port applies its own gate, which the server cannot
// see: its plain commands count as executed.
func (s *Server) handOffExecutes(withOnOff bool, mask, override uint8) bool {
	if withOnOff {
		return true
	}
	if g, ok := s.src.(gatedSource); ok {
		return g.gateAllows(mask, override)
	}
	return true
}

// MatterApplySceneValues implements cluster/core's SceneValuesApplier: the
// recall of a scene's CurrentLevel. Mirrors matter.js
// packages/node/src/behaviors/level-control/LevelControlServer.ts:746-758
// at db021da0 (#applySceneValues, registered through
// ScenesManagementServer.implementScenes, :145): a level that is absent,
// equal to CurrentLevel or outside MinLevel..MaxLevel is ignored;
// otherwise moveToLevelLogic(level, transitionTime / 100, false,
// { executeIfOff: true }) — the plain MoveToLevel's logic past its gate,
// over the scene's transition in fractional tenths, with ExecuteIfOff as
// the only option in effect, so the colour temperature does not follow
// the level (:559-560 couples it only under coupleColorTempToLevel).
// moveToLevelLogic invalidates the current scene (:340); the recall makes
// the recalled scene current afterwards. On the hand-off path the level
// reaches the port as such a MoveToLevel, its TransitionTime in whole
// tenths.
func (s *Server) MatterApplySceneValues(ctx context.Context, values map[uint32]uint64, transitionMs uint32) {
	v, ok := values[AttrCurrentLevel]
	if !ok || s.src == nil || v < uint64(s.minLevel()) || v > uint64(lvldef.LevelMax) {
		return
	}
	level := uint8(v) //nolint:gosec // within MinLevel..MaxLevel, checked above
	if s.engine != nil {
		s.cmd.Lock()
		defer s.cmd.Unlock()
	}
	before := s.levelNow()
	if before.known && before.level == level {
		return
	}
	if s.engine != nil {
		var rateFrom func(current float64) float64
		if transitionMs != 0 {
			tenths := float64(transitionMs) / 100
			rateFrom = func(current float64) float64 { return (float64(level) - current) / tenths * 10 }
		}
		// Each change of CurrentLevel advanced the DataVersion
		// ([Server.observeLevel]).
		if err := s.transition(ctx, float64(level), 0, rateFrom, false, OptionExecuteIfOff); err == nil {
			s.invalidateScenes()
		}
		return
	}
	tenths := uint16(min(transitionMs/100, maxTransitionTenths)) //nolint:gosec // bounded by maxTransitionTenths (native.go)
	req := wire.MoveToLevelRequest{
		Level: level, TransitionTime: &tenths,
		OptionsMask: OptionExecuteIfOff, OptionsOverride: OptionExecuteIfOff,
	}
	s.forwarding.Add(1)
	err := s.src.MoveToLevel(ctx, req)
	s.forwarding.Add(-1)
	if err != nil {
		return
	}
	s.observeLevel(contract.OriginCommand)
	s.invalidateScenes()
	s.bumpIfMoved(before)
}

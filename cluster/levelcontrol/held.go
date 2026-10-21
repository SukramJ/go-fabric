// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package levelcontrol

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/cluster/wire"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/contract"
)

// HeldConfig carries the construction parameters of [New]: a LevelControl
// server that holds its level itself. It is the device layer's default for
// LevelControl. matter.js's server keeps its attributes in its behavior
// state (packages/node/src/behaviors/level-control/LevelControlServer.ts:772-799,
// the State class with managedTransitionTimeHandling).
type HeldConfig struct {
	// Features is the FeatureMap: [FeatureOnOff] and [FeatureLighting],
	// each optional (matter.js's LevelControlServer without features is a
	// device type's LevelControl that couples to no OnOff, a speaker's
	// volume); [FeatureFrequency] is not served ([ErrFeatures]).
	Features uint32
	// CurrentLevel, OnLevel and Options are the values the server starts
	// with; a nil level is null (matter.js's default for both).
	CurrentLevel, OnLevel *uint8
	Options               uint8
	// DataVersion is an optional host-owned tracker, as [Config.DataVersion].
	DataVersion *cluster.DataVersionTracker
	// ManageTransitions runs the commands as gradual transitions, stepping
	// the level every StepInterval and reporting RemainingTime (matter.js
	// managedTransitionTimeHandling). Without it every command applies
	// its target level at once, matter.js's default (:782-790: "The
	// default implementation always set the target level immediately").
	ManageTransitions bool
	// StepInterval is the time between two steps; zero is
	// [transition.DefaultStepInterval].
	StepInterval time.Duration
	// OnOff is the On/Off cluster on the same endpoint, as
	// [Transitions.OnOff]; without [FeatureOnOff] the server couples to
	// none (matter.js `this.features.onOff`, LevelControlServer.ts:141,
	// :558, :729-735). When it also reports its changes
	// ([contract.AttributeChangeNotifier]), the server follows it: OnOff
	// turning on moves CurrentLevel to a non-null OnLevel (matter.js
	// initializeOnOff and handleOnOffChange, :253-255, :658-670).
	OnOff OnOff
	// ColorTemperature is the ColorControl cluster on the same endpoint,
	// as [Transitions.ColorTemperature].
	ColorTemperature ColorTemperatureCoupling
	// Clock paces the steps and the CurrentLevel quieter; nil is
	// [clock.System].
	Clock clock.Clock
}

// New returns a server that holds its own level: the commands run on the
// module's transition engine against the server's state. A host that owns
// the level in its device builds the server over its port with
// [NewServer] instead. matter.js sets the transitions up over its own state
// in initialize (LevelControlServer.ts:113-147).
func New(cfg HeldConfig) (*Server, error) {
	if extra := cfg.Features &^ (FeatureOnOff | FeatureLighting); extra != 0 {
		return nil, fmt.Errorf("%w: 0x%X", ErrFeatures, extra)
	}
	h := &heldLevel{options: cfg.Options}
	h.level, h.levelKnown = levelOf(cfg.CurrentLevel)
	h.onLevel = clonePtr(cfg.OnLevel)
	s := newServer(h, cfg.DataVersion, cfg.Features)
	s.held = h
	onOff := cfg.OnOff
	if cfg.Features&FeatureOnOff == 0 {
		onOff = nil
	}
	s.initEngine(Transitions{
		OnOff: onOff, ColorTemperature: cfg.ColorTemperature,
		StepInterval: cfg.StepInterval, Clock: clock.Or(cfg.Clock),
	}, cfg.ManageTransitions)
	if n, ok := onOff.(contract.AttributeChangeNotifier); ok && n != nil {
		s.followOnOff(n)
	}
	return s, nil
}

// ErrFeatures is the refusal of a [HeldConfig] feature selection the
// server does not serve.
var ErrFeatures = errors.New("levelcontrol: feature selection not served")

// heldLevel is the [LevelSource] a held server keeps its state in: the
// level the engine applies and the two writable attributes; the server
// reports their changes.
type heldLevel struct {
	// mu guards level, levelKnown, onLevel and options.
	mu         sync.Mutex
	level      uint8
	levelKnown bool
	onLevel    *uint8
	options    uint8
}

func levelOf(p *uint8) (uint8, bool) {
	if p == nil {
		return 0, false
	}
	return *p, true
}

func clonePtr(p *uint8) *uint8 {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// setLevel stores a level; nil is null.
func (h *heldLevel) setLevel(p *uint8) {
	level, known := levelOf(p)
	h.mu.Lock()
	h.level, h.levelKnown = level, known
	h.mu.Unlock()
}

// CurrentLevel implements [LevelSource].
func (h *heldLevel) CurrentLevel() (uint8, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.level, h.levelKnown
}

// Options implements [LevelSource].
func (h *heldLevel) Options() uint8 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.options
}

// OnLevel implements [LevelSource].
func (h *heldLevel) OnLevel() (uint8, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return levelOf(h.onLevel)
}

// SetOptions implements [LevelSource].
func (h *heldLevel) SetOptions(_ context.Context, options uint8) error {
	h.mu.Lock()
	h.options = options
	h.mu.Unlock()
	return nil
}

// SetOnLevel implements [LevelSource].
func (h *heldLevel) SetOnLevel(_ context.Context, level *uint8) error {
	h.mu.Lock()
	h.onLevel = clonePtr(level)
	h.mu.Unlock()
	return nil
}

// MoveToLevel implements [LevelSource]: under the engine, the level it
// applies — the only command method a held server calls.
func (h *heldLevel) MoveToLevel(_ context.Context, req wire.MoveToLevelRequest) error {
	h.setLevel(&req.Level)
	return nil
}

// The other seven command methods are never called: the engine runs the
// commands and applies each level through MoveToLevel ([LevelSource]).

func (h *heldLevel) MoveToLevelWithOnOff(ctx context.Context, req wire.MoveToLevelRequest) error {
	return h.MoveToLevel(ctx, req)
}
func (*heldLevel) Move(context.Context, wire.MoveRequest) error          { return nil }
func (*heldLevel) MoveWithOnOff(context.Context, wire.MoveRequest) error { return nil }
func (*heldLevel) Step(context.Context, wire.StepRequest) error          { return nil }
func (*heldLevel) StepWithOnOff(context.Context, wire.StepRequest) error { return nil }
func (*heldLevel) Stop(context.Context, wire.StopRequest) error          { return nil }
func (*heldLevel) StopWithOnOff(context.Context, wire.StopRequest) error { return nil }

var _ source = (*heldLevel)(nil)

// followOnOff is matter.js initializeOnOff (:253-255): a held server
// reacts to the On/Off cluster's OnOff turning on with handleOnOffChange
// (:658-670) — a non-null OnLevel becomes CurrentLevel. The coupling of a
// "with On/Off" command switches the device on itself and moves to
// OnLevel in the same step (#turnOnForLevel, :597-617), which this
// reaction is then, as matter.js's blockOnOffCouplingOnce makes its own,
// the only application of.
func (s *Server) followOnOff(n contract.AttributeChangeNotifier) {
	s.mu.Lock()
	s.wasOn = s.onOff.OnOff()
	s.mu.Unlock()
	n.OnAttributeChange(func(contract.Change) {
		on := s.onOff.OnOff()
		s.mu.Lock()
		turnedOn := on && !s.wasOn
		s.wasOn = on
		s.mu.Unlock()
		if !turnedOn {
			return
		}
		if onLevel, known := s.held.OnLevel(); known {
			s.held.setLevel(&onLevel)
			s.observeLevel(contract.OriginCommand)
		}
	})
}

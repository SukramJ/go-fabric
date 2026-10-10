// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package levelcontrol

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/transition"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// Transitions turns on the module's transition engine for a LevelControl
// server whose device cannot ramp by itself (matter.js
// LevelControlServer with managedTransitionTimeHandling). The server then
// runs MoveToLevel, Move, Step and Stop, with and without On/Off, the way
// matter.js LevelControlServer does, and drives the host through
// [LevelSource.MoveToLevel] with TransitionTime 0 and ExecuteIfOff forced
// for every level it applies — at once, or step by step every
// StepInterval.
type Transitions struct {
	// OnOff is the On/Off cluster on the same endpoint. The server reads
	// it for the ExecuteIfOff gate and drives it for the "with On/Off"
	// commands. Nil means the endpoint has none: every command executes
	// and none couples (matter.js `!this.agent.has(OnOffServer)`).
	OnOff OnOff
	// ColorTemperature is the ColorControl cluster on the same endpoint,
	// which follows the level while the CoupleColorTempToLevel option is
	// in effect (Lighting only). Nil means there is none.
	ColorTemperature ColorTemperatureCoupling
	// StepInterval is the time between two steps; zero is
	// [transition.DefaultStepInterval] (matter.js transitionStepInterval).
	StepInterval time.Duration
}

// OnOff is the On/Off state of the endpoint a LevelControl server under
// the engine couples to.
type OnOff interface {
	// OnOff reports whether the device is on.
	OnOff() bool
	// SetOnOff switches the device as the On/Off cluster's own state
	// change does — including the OnLevel the host applies when the device
	// turns on, which matter.js applies in LevelControlServer
	// handleOnOffChange. The server calls it for the "with On/Off"
	// commands only: on before a transition towards a level above the
	// minimum, off once one ends at the minimum.
	SetOnOff(ctx context.Context, on bool) error
}

// ColorTemperatureCoupling is the ColorControl cluster a LevelControl
// server under the engine moves along with the level
// (light.ColorControlServer implements it).
type ColorTemperatureCoupling interface {
	// SyncColorTemperatureWithLevel moves the colour temperature to the
	// value level maps to (matter.js
	// ColorControlServer.syncColorTemperatureWithLevel).
	SyncColorTemperatureWithLevel(ctx context.Context, level uint8) error
}

// propCurrentLevel names CurrentLevel in the engine.
const propCurrentLevel = "currentLevel"

// coupling is what one transition couples to (matter.js
// LevelControlServer CouplingParticipant).
type coupling struct {
	// turnsOff: a "with On/Off" command whose transition ends at the
	// minimum switches the device off once the level is there.
	turnsOff bool
	// colorTemperature: the colour temperature follows the level.
	colorTemperature bool
}

// initTransitions sets up the engine. matter.js
// LevelControlServer #initializeTransitions: CurrentLevel between MinLevel
// and MaxLevel, RemainingTime reported by hand.
func (s *Server) initTransitions(cfg Transitions) {
	s.onOff, s.ct = cfg.OnOff, cfg.ColorTemperature
	s.quiet = &cluster.Quieter{Report: func() { s.changes.Notify(AttrCurrentLevel) }}
	s.engine = transition.New(transition.Config{
		Manage:       true,
		StepInterval: cfg.StepInterval,
		Properties: map[string]transition.Property{
			propCurrentLevel: {Min: float64(s.minLevel()), Max: float64(LevelMax)},
		},
		Read:  s.readLevel,
		Apply: s.applyLevel,
		RemainingTimeChanged: func() {
			if s.lighting {
				s.changes.Notify(AttrRemainingTime)
			}
		},
		Settled: func(string) { s.quiet.EmitNow() },
	})
	if s.src != nil {
		s.level, s.levelKnown = s.src.CurrentLevel()
	}
}

// remainingTime is RemainingTime: the engine's, 0 on the hand-off path.
func (s *Server) remainingTime() uint16 {
	if s.engine == nil {
		return 0
	}
	return uint16(min(s.engine.RemainingTime(), math.MaxUint16)) //nolint:gosec // bounded by MaxUint16
}

// readLevel is the engine's view of CurrentLevel.
func (s *Server) readLevel(string) (float64, bool) {
	if s.src == nil {
		return 0, false
	}
	level, known := s.src.CurrentLevel()
	return float64(level), known
}

// applyLevel hands the engine's level to the host as an immediate
// MoveToLevel that executes whatever the On/Off state — the engine gated
// the command already — then reports the change and couples.
func (s *Server) applyLevel(changes []transition.Change) error {
	ctx := context.Background()
	for _, c := range changes {
		zero := uint16(0)
		if err := s.src.MoveToLevel(ctx, MoveToLevelRequest{
			Level: uint8(c.Value), TransitionTime: &zero, //nolint:gosec // the engine keeps the value between MinLevel and MaxLevel
			OptionsMask: OptionExecuteIfOff, OptionsOverride: OptionExecuteIfOff,
		}); err != nil {
			return fmt.Errorf("levelcontrol: apply level %v: %w", c.Value, err)
		}
	}
	s.observeLevel()
	s.mu.Lock()
	c := s.coupling
	s.mu.Unlock()
	return s.commit(ctx, c)
}

// observeLevel reports CurrentLevel when it changed: by the quieter rules
// of its "Q" quality (matter.js QuietEvent), at once from or to null and
// otherwise at most once a second, the end of a transition flushing the
// last value. It runs after every level the engine applied and on every
// change notification of the host, so a level the device moved by itself
// is reported the same way.
func (s *Server) observeLevel() {
	level, known := s.src.CurrentLevel()
	s.mu.Lock()
	wasNull := !s.levelKnown
	changed := known != s.levelKnown || (known && level != s.level)
	s.level, s.levelKnown = level, known
	s.mu.Unlock()
	if changed {
		s.quiet.Changed(wasNull, !known)
	}
}

// OnMatterAttributesChanged implements [contract.AttributeChangeNotifier].
// Under the engine it reports CurrentLevel by its quieter rules and
// RemainingTime when matter.js's #updateRemainingTime would; while a
// listener is registered the server follows the host's own change
// notification, so a level the device changed by itself is reported too.
// On the hand-off path nothing is reported here: the host's notifier
// reports CurrentLevel ([Server.OnMatterValueChanged]).
func (s *Server) OnMatterAttributesChanged(cb func(attrIDs []uint32)) (unsubscribe func()) {
	unsub := s.changes.OnMatterAttributesChanged(cb)
	if s.engine == nil {
		return unsub
	}
	s.mu.Lock()
	s.listeners++
	subscribe := s.listeners == 1
	s.mu.Unlock()
	if subscribe {
		if n, ok := s.src.(contract.ChangeNotifier); ok && n != nil {
			stop := n.OnMatterValueChanged(s.observeLevel)
			s.mu.Lock()
			s.unsubSrc = stop
			s.mu.Unlock()
		}
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			unsub()
			s.mu.Lock()
			s.listeners--
			var stop func()
			if s.listeners == 0 {
				stop, s.unsubSrc = s.unsubSrc, nil
			}
			s.mu.Unlock()
			if stop != nil {
				stop()
			}
		})
	}
}

// MatterSelfReportedAttributes implements
// [contract.SelfReportedAttributeLister]: under the engine the server
// reports CurrentLevel and RemainingTime itself.
func (s *Server) MatterSelfReportedAttributes() []uint32 {
	if s.engine == nil {
		return nil
	}
	if s.lighting {
		return []uint32{AttrCurrentLevel, AttrRemainingTime}
	}
	return []uint32{AttrCurrentLevel}
}

// MatterQuiesce implements [contract.ClusterQuiescer]: a transition stops
// where it is, without a report (matter.js LevelControlServer
// [Symbol.asyncDispose] closes its transitions).
func (s *Server) MatterQuiesce() {
	if s.engine != nil {
		s.engine.StopAll()
	}
}

// effectiveOptions is matter.js #calculateEffectiveOptions: per bit, a set
// OptionsMask bit takes OptionsOverride's, a clear one the Options
// attribute's; CoupleColorTempToLevel only under Lighting.
func (s *Server) effectiveOptions(mask, override uint8) uint8 {
	return ((s.src.Options() &^ mask) | (override & mask)) & s.optionsMask()
}

// executes is matter.js #optionsAllowExecution.
func (s *Server) executes(options uint8) bool {
	return options&OptionExecuteIfOff != 0 || s.onOff == nil || s.onOff.OnOff()
}

// currentLevel is matter.js's currentLevel getter: a null CurrentLevel
// cannot be operated on (Status.Failure).
func (s *Server) currentLevel() (uint8, error) {
	level, known := s.src.CurrentLevel()
	if !known {
		return 0, failureErr{"levelcontrol: the CurrentLevel value is null, so we cannot operate on it"}
	}
	return level, nil
}

// runMoveToLevel is matter.js moveToLevel / moveToLevelWithOnOff and
// moveToLevelLogic: the level cropped to MinLevel..MaxLevel, a rate from
// TransitionTime (none for a null or zero TransitionTime — no
// OnOffTransitionTime is served), and the plain command gated by
// ExecuteIfOff.
func (s *Server) runMoveToLevel(ctx context.Context, req MoveToLevelRequest, withOnOff bool) error {
	s.cmd.Lock()
	defer s.cmd.Unlock()
	options := s.effectiveOptions(req.OptionsMask, req.OptionsOverride)
	if !withOnOff && !s.executes(options) {
		return nil
	}
	level := float64(min(max(req.Level, s.minLevel()), LevelMax))
	var rate func(current float64) float64
	if tt := req.TransitionTime; tt != nil && *tt != 0 {
		tenths := float64(*tt)
		rate = func(current float64) float64 { return (level - current) / tenths * 10 }
	}
	return s.transition(ctx, level, rate, withOnOff, options)
}

// runMove is matter.js move / moveWithOnOff and moveLogic: towards the
// bound in the direction of MoveMode at Rate per second, at once for a
// null Rate (no DefaultMoveRate is served). The zero Rate was refused
// before.
func (s *Server) runMove(ctx context.Context, req MoveRequest, withOnOff bool) error {
	s.cmd.Lock()
	defer s.cmd.Unlock()
	options := s.effectiveOptions(req.OptionsMask, req.OptionsOverride)
	if !withOnOff && !s.executes(options) {
		return nil
	}
	target, sign := math.Inf(1), 1.0
	if req.MoveMode == MoveModeDown {
		target, sign = math.Inf(-1), -1
	}
	var rate func(float64) float64
	if req.Rate != nil {
		r := float64(*req.Rate) * sign
		rate = func(float64) float64 { return r }
	}
	return s.transition(ctx, target, rate, withOnOff, options)
}

// runStep is matter.js step / stepWithOnOff and stepLogic: StepSize from
// the current level, over TransitionTime (at once for a null one, or a
// zero one, whose rate is infinite).
func (s *Server) runStep(ctx context.Context, req StepRequest, withOnOff bool) error {
	s.cmd.Lock()
	defer s.cmd.Unlock()
	options := s.effectiveOptions(req.OptionsMask, req.OptionsOverride)
	if !withOnOff && !s.executes(options) {
		return nil
	}
	sign := 1.0
	if req.StepMode == StepModeDown {
		sign = -1
	}
	current, err := s.currentLevel()
	if err != nil {
		return err
	}
	var rate func(float64) float64
	if tt := req.TransitionTime; tt != nil {
		r := float64(req.StepSize) / float64(*tt) * 10 * sign
		rate = func(float64) float64 { return r }
	}
	return s.transition(ctx, float64(current)+float64(req.StepSize)*sign, rate, withOnOff, options)
}

// runStop is matter.js stop / stopWithOnOff and stopLogic: the transition
// ends where it is and RemainingTime reports 0. Only the plain Stop is
// gated by ExecuteIfOff.
func (s *Server) runStop(req StopRequest, withOnOff bool) error {
	s.cmd.Lock()
	defer s.cmd.Unlock()
	if !withOnOff && !s.executes(s.effectiveOptions(req.OptionsMask, req.OptionsOverride)) {
		return nil
	}
	s.engine.CancelAll()
	return nil
}

// transition is matter.js LevelControlServer.transition: couple first —
// a "with On/Off" command towards a level above the minimum switches the
// device on, which may move the level to OnLevel — then derive the rate
// from the level that leaves, start the engine, and couple what the level
// arrived at. Every step of the transition couples again.
func (s *Server) transition(ctx context.Context, target float64, rate func(current float64) float64, withOnOff bool, options uint8) error {
	c, err := s.couple(ctx, withOnOff, options, target)
	if err != nil {
		return err
	}
	if rate != nil {
		// The level is read here only for its error; the rate itself is
		// computed from the value the engine reads under its lock
		// (transition.Transition.RateFrom), as matter.js's moveToLevelLogic
		// reads and starts in one synchronous handler.
		if _, err := s.currentLevel(); err != nil {
			return err
		}
	}
	// The steps run after the command is answered, so they keep its
	// context's values but not its cancellation.
	stepCtx := context.WithoutCancel(ctx)
	if err := s.engine.Start(transition.Transition{
		Name: propCurrentLevel, RateFrom: rate, Target: target,
		OnStep: func(float64) { _, _ = s.couple(stepCtx, withOnOff, options, target) },
	}); err != nil {
		return err
	}
	return s.commit(ctx, c)
}

// couple is matter.js LevelControlServer.couple: it records what the
// transition couples to and switches the device on for a "with On/Off"
// command whose transition does not end at the minimum.
func (s *Server) couple(ctx context.Context, withOnOff bool, options uint8, target float64) (coupling, error) {
	coupleOnOff := withOnOff && s.onOff != nil
	c := coupling{
		turnsOff:         coupleOnOff && min(max(target, float64(s.minLevel())), float64(LevelMax)) <= float64(s.minLevel()),
		colorTemperature: s.lighting && options&OptionCoupleColorTempToLevel != 0 && s.ct != nil,
	}
	s.mu.Lock()
	s.coupling = c
	s.mu.Unlock()
	if coupleOnOff && !c.turnsOff && !s.onOff.OnOff() {
		// Moving towards on takes effect at once, which the CHIP tests
		// require (matter.js #turnOnForLevel).
		if err := s.onOff.SetOnOff(ctx, true); err != nil {
			return c, fmt.Errorf("levelcontrol: switch on: %w", err)
		}
		s.observeLevel()
	}
	return c, nil
}

// commit is matter.js #coupleOnCommit: the device switches off once a
// transition that turns it off has reached the minimum, and the colour
// temperature follows the level.
func (s *Server) commit(ctx context.Context, c coupling) error {
	level, known := s.src.CurrentLevel()
	if !known {
		return nil
	}
	if c.turnsOff && level == s.minLevel() && s.onOff.OnOff() {
		if err := s.onOff.SetOnOff(ctx, false); err != nil {
			return fmt.Errorf("levelcontrol: switch off: %w", err)
		}
	}
	if c.colorTemperature {
		if err := s.ct.SyncColorTemperatureWithLevel(ctx, level); err != nil {
			return fmt.Errorf("levelcontrol: couple colour temperature: %w", err)
		}
	}
	return nil
}

// failureErr maps onto the Matter FAILURE status.
type failureErr struct{ msg string }

func (e failureErr) Error() string                 { return e.msg }
func (failureErr) MatterStatusCode() im.StatusCode { return im.StatusFailure }

var _ im.StatusCodeError = failureErr{}

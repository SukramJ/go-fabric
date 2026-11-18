// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package levelcontrol

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	lvldef "github.com/SukramJ/go-fabric/cluster/spec/levelcontrol"

	"github.com/SukramJ/go-fabric/cluster/wire"

	"github.com/SukramJ/go-fabric/clock"
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
	// and none couples (matter.js `!this.agent.has(OnOffServer)`). A
	// server whose features lack OO does not use it ([NewServer]).
	OnOff OnOff
	// ColorTemperature is the ColorControl cluster on the same endpoint,
	// which follows the level while the CoupleColorTempToLevel option is
	// in effect (Lighting only). Nil means there is none.
	ColorTemperature ColorTemperatureCoupling
	// StepInterval is the time between two steps; zero is
	// [transition.DefaultStepInterval] (matter.js transitionStepInterval).
	StepInterval time.Duration
	// Clock paces the steps and the CurrentLevel quieter; nil is
	// [clock.System].
	Clock clock.Clock
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

// initEngine sets up the engine; manage makes its transitions gradual
// (matter.js managedTransitionTimeHandling — [Config.Transitions] always
// manages, [New] as its configuration says). matter.js
// LevelControlServer #initializeTransitions: CurrentLevel between MinLevel
// and MaxLevel, RemainingTime reported by hand.
func (s *Server) initEngine(cfg Transitions, manage bool) {
	s.onOff, s.ct = cfg.OnOff, cfg.ColorTemperature
	s.quiet = &cluster.Quieter{Report: func() {
		if !s.quiescing.Load() {
			s.changes.ReportFrom(contract.Origin(s.levelOrigin.Load()), AttrCurrentLevel) //nolint:gosec // only an Origin is stored
		}
	}, Clock: cfg.Clock}
	s.engine = transition.New(transition.Config{
		Manage:       manage,
		StepInterval: cfg.StepInterval,
		Clock:        cfg.Clock,
		Properties: map[string]transition.Property{
			propCurrentLevel: {Min: float64(s.minLevel()), Max: float64(lvldef.LevelMax)},
		},
		Read:  s.readLevel,
		Apply: s.applyLevel,
		// Each report of RemainingTime advances the DataVersion once.
		// Mirrors matter.js
		// packages/node/src/behavior/internal/ServerBehaviorBacking.ts:258-261
		// at db021da0 (the quiet observer runs advanceVersionFor before it
		// broadcasts), Datasource.ts:679-688 (advanceVersionFor advances
		// for a dynamic property) and
		// packages/node/src/behaviors/level-control/LevelControlServer.ts:806-822
		// (remainingTime is served from Val.properties, a dynamic property,
		// when the attribute is supported).
		RemainingTimeChanged: func() {
			if s.lighting {
				s.tracker().Bump()
				s.changes.NotifyFrom(contract.OriginCommand, AttrRemainingTime)
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
	s.mu.Lock()
	ended := s.offEndedGen != 0 && s.offEndedGen == s.couplingGen
	if ended {
		s.offEndedGen = 0
	}
	s.mu.Unlock()
	if ended {
		return errOffEndedTransition
	}
	ctx := context.Background()
	// A change the host's notifier reports while the level is handed over
	// is the command's.
	s.forwarding.Add(1)
	for _, c := range changes {
		zero := uint16(0)
		if err := s.src.MoveToLevel(ctx, wire.MoveToLevelRequest{
			Level: uint8(c.Value), TransitionTime: &zero, //nolint:gosec // the engine keeps the value between MinLevel and MaxLevel
			OptionsMask: OptionExecuteIfOff, OptionsOverride: OptionExecuteIfOff,
		}); err != nil {
			s.forwarding.Add(-1)
			return fmt.Errorf("levelcontrol: apply level %v: %w", c.Value, err)
		}
	}
	s.forwarding.Add(-1)
	s.observeLevel(contract.OriginCommand)
	s.mu.Lock()
	c := s.coupling
	s.mu.Unlock()
	return s.commit(ctx, c)
}

// observeLevel is the one place a change of CurrentLevel is reported,
// whoever made it: a command — at once or a transition's step —, the
// host's Set, the OnOff coupling, the device's own change through the
// host's notifier, and on the hand-off path what the host's port moved.
// The change listeners see it at once, from origin, as every observer of
// matter.js's currentLevel$Changed does (#noteIfChanged queues it for a
// state write that changed the value, Datasource.ts:1486-1492, and
// postCommit emits it, :1302-1317, at db021da0; Events.ts:269-296
// QuietEvent is an
// OnlineEvent whose own observers are not throttled). Under the engine
// the wire report follows the quieter rules of the "Q" quality (matter.js
// QuietEvent's QuietObservable, ServerBehaviorBacking.ts:253-261): at
// once from or to null and otherwise at most once a second, the end of a
// transition flushing the last value — a report the quieter defers
// carries the origin of the last change before it. Under the engine each
// change advances the server's DataVersion once, a transition's step as
// much as a command's level: matter.js advances the version on every
// commit that changed a value (Datasource.ts:1456-1457 at db021da0), and
// its quiet emit adds no advance for currentLevel (advanceVersionFor,
// ServerBehaviorBacking.ts:259, advances only for a dynamic property,
// Datasource.ts:679-688). On the hand-off path the host's own notifier is
// the wire's ([Server.OnMatterValueChanged]) and advances the version, a
// command's change advances it in [Server.finish] unless that notifier
// advanced it while the command ran, so the listeners alone are told.
func (s *Server) observeLevel(origin contract.Origin) {
	s.noteLevel(origin, false)
}

// noteLevel is [Server.observeLevel] within a change that advanced the
// DataVersion already when advanced is true: the host's set of
// CurrentLevel with another attribute is one commit, one advance.
func (s *Server) noteLevel(origin contract.Origin, advanced bool) {
	// The read and the compare-and-store are one step, so an older reading
	// is never stored after a newer one (R2-P6-conc-1408): the host's
	// notifier, the engine's step and a command observe concurrently.
	s.observe.Lock()
	level, known := s.src.CurrentLevel()
	s.mu.Lock()
	wasNull := !s.levelKnown
	changed := known != s.levelKnown || (known && level != s.level)
	s.level, s.levelKnown = level, known
	s.mu.Unlock()
	s.observe.Unlock()
	if !changed {
		return
	}
	if s.engine != nil && !advanced {
		s.tracker().Bump()
	}
	s.changes.ChangedFrom(origin, AttrCurrentLevel)
	if s.quiet != nil {
		s.levelOrigin.Store(uint32(origin))
		s.quiet.Changed(wasNull, !known)
	}
}

// sourceOrigin is the origin of a change the host's notifier reports: a
// command's while one is handed to the port — forwarded on the hand-off
// path, or a level the engine applies —, the host's own otherwise: the
// device moved.
func (s *Server) sourceOrigin() contract.Origin {
	if s.forwarding.Load() > 0 {
		return contract.OriginCommand
	}
	return contract.OriginHostSet
}

// OnAttributeChange implements [contract.AttributeChangeNotifier]: every
// change at once — CurrentLevel whoever moved it ([Server.observeLevel]),
// Options, OnLevel and StartUpCurrentLevel, and under the engine
// RemainingTime when matter.js's #updateRemainingTime would report it.
// While a listener is registered the server follows the host's own change
// notification, so a level the device changed by itself is reported too.
func (s *Server) OnAttributeChange(cb func(change contract.Change)) (unsubscribe func()) {
	return s.listen(s.changes.OnAttributeChange(cb))
}

// OnAttributeReport implements [contract.AttributeReportNotifier]: what
// goes on the wire — CurrentLevel under the engine by the quieter rules of
// its "Q" quality, every other change at once. On the hand-off path
// CurrentLevel is not reported here: the host's notifier reports it
// ([Server.OnMatterValueChanged]).
func (s *Server) OnAttributeReport(cb func(change contract.Change)) (unsubscribe func()) {
	return s.listen(s.changes.OnAttributeReport(cb))
}

// listen counts a listener whose unsubscribe is unsub: while there is one,
// the server follows the host's notifier ([Server.observeLevel]).
func (s *Server) listen(unsub func()) (unsubscribe func()) {
	if s.src == nil {
		return unsub
	}
	s.mu.Lock()
	s.listeners++
	s.mu.Unlock()
	s.reconcileHost()
	var once sync.Once
	return func() {
		once.Do(func() {
			unsub()
			s.mu.Lock()
			s.listeners--
			s.mu.Unlock()
			s.reconcileHost()
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
// where it is, and a CurrentLevel report the quieter holds back is dropped
// with its timer, both without a report, so no timer of the server
// outlives it. Mirrors matter.js
// packages/node/src/behaviors/level-control/LevelControlServer.ts:765-770
// ([Symbol.asyncDispose] closes the transitions) and
// packages/general/src/util/Observable.ts:806-815, :1276-1280 (disposing
// the behavior's events disposes each QuietObservable, which stops its
// "delayed emit" timer, #stop :1309-1314). The quieter stays usable: a
// later command reports by its rules again.
func (s *Server) MatterQuiesce() {
	// On the hand-off path the module's own NativeSource owes a switch-off
	// with a deadline of its own; it is dropped as well (R2-P6-fsm-1406).
	if q, ok := s.src.(interface{ quiesce() }); ok {
		q.quiesce()
	}
	if s.engine == nil {
		return
	}
	s.engine.StopAll()
	// EmitNow clears the pending report and stops its timer; the report
	// itself is suppressed.
	s.quiescing.Store(true)
	s.quiet.EmitNow()
	s.quiescing.Store(false)
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
func (s *Server) runMoveToLevel(ctx context.Context, req wire.MoveToLevelRequest, withOnOff bool) error {
	s.cmd.Lock()
	defer s.cmd.Unlock()
	options := s.effectiveOptions(req.OptionsMask, req.OptionsOverride)
	if !withOnOff && !s.executes(options) {
		return nil
	}
	level := float64(min(max(req.Level, s.minLevel()), lvldef.LevelMax))
	var rateFrom func(current float64) float64
	if tt := req.TransitionTime; tt != nil && *tt != 0 {
		tenths := float64(*tt)
		rateFrom = func(current float64) float64 { return (level - current) / tenths * 10 }
	}
	return s.transitionAndInvalidate(ctx, level, 0, rateFrom, withOnOff, options)
}

// runMove is matter.js move / moveWithOnOff and moveLogic: towards the
// bound in the direction of MoveMode at Rate per second, at once for a
// null Rate (no DefaultMoveRate is served). The zero Rate was refused
// before.
func (s *Server) runMove(ctx context.Context, req wire.MoveRequest, withOnOff bool) error {
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
	// A null Rate is the rate 0, which the engine applies at once.
	var rate float64
	if req.Rate != nil {
		rate = float64(*req.Rate) * sign
	}
	return s.transitionAndInvalidate(ctx, target, rate, nil, withOnOff, options)
}

// runStep is matter.js step / stepWithOnOff and stepLogic: StepSize from
// the current level, over TransitionTime (at once for a null one, or a
// zero one, whose rate is infinite).
func (s *Server) runStep(ctx context.Context, req wire.StepRequest, withOnOff bool) error {
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
	var rate float64
	if tt := req.TransitionTime; tt != nil {
		rate = float64(req.StepSize) / float64(*tt) * 10 * sign
	}
	return s.transitionAndInvalidate(ctx, float64(current)+float64(req.StepSize)*sign, rate, nil, withOnOff, options)
}

// runStop is matter.js stop / stopWithOnOff and stopLogic: the transition
// ends where it is and RemainingTime reports 0. Only the plain Stop is
// gated by ExecuteIfOff.
func (s *Server) runStop(req wire.StopRequest, withOnOff bool) error {
	s.cmd.Lock()
	defer s.cmd.Unlock()
	if !withOnOff && !s.executes(s.effectiveOptions(req.OptionsMask, req.OptionsOverride)) {
		return nil
	}
	s.engine.CancelAll()
	s.invalidateScenes()
	return nil
}

// transitionAndInvalidate is [Server.transition] for a command past its
// ExecuteIfOff gate, which then ends the current scene. matter.js
// invalidates before the logic runs (LevelControlServer.ts:276, :293,
// :359, :373, :426, :442 at db021da0), inside the command's transaction,
// which a failing command rolls back
// (packages/protocol/src/action/server/CommandInvokeResponse.ts:509-516):
// the scene ends when the command succeeds.
func (s *Server) transitionAndInvalidate(ctx context.Context, target, rate float64, rateFrom func(current float64) float64, withOnOff bool, options uint8) error {
	if err := s.transition(ctx, target, rate, rateFrom, withOnOff, options); err != nil {
		return err
	}
	s.invalidateScenes()
	return nil
}

// transition is matter.js LevelControlServer.transition: couple first —
// a "with On/Off" command towards a level above the minimum switches the
// device on, which may move the level to OnLevel — then derive the rate
// from the level that leaves, start the engine, and couple what the level
// arrived at. Every step of the transition records its coupling again
// but switches nothing ([Server.stepCouple]). rate is a
// constant rate (Move, Step; 0 applies at once); rateFrom, when set,
// derives it from the current level (MoveToLevel).
func (s *Server) transition(ctx context.Context, target, rate float64, rateFrom func(current float64) float64, withOnOff bool, options uint8) error {
	gen := s.nextCoupling()
	c, err := s.couple(ctx, gen, withOnOff, options, target)
	if err != nil {
		return err
	}
	if rateFrom != nil {
		// The level is read for its error only where the rate is derived
		// from it. Mirrors matter.js
		// packages/node/src/behaviors/level-control/LevelControlServer.ts:536-538
		// at db021da0 (transition: `if (typeof changePerS === "function")
		// changePerS = changePerS(this.currentLevel)`, the getter that
		// fails a null level, :103-111) — a function only moveToLevelLogic
		// passes (:336-338); moveLogic hands a number (:388-404), so a
		// rated Move at a null level is not refused, as the native path
		// does not refuse it (native.go). The rate itself is computed from
		// the value the engine reads under its lock
		// (transition.Transition.RateFrom), as matter.js reads and starts
		// in one synchronous handler.
		if _, err := s.currentLevel(); err != nil {
			return err
		}
	}
	if err := s.engine.Start(transition.Transition{
		Name: propCurrentLevel, Rate: rate, RateFrom: rateFrom, Target: target,
		OnStep: func(float64) { s.stepCouple(gen, withOnOff, options, target) },
	}); err != nil {
		return err
	}
	return s.commit(ctx, c)
}

// nextCoupling starts a command's coupling: the generation its
// transition records its coupling under ([Server.couple]).
func (s *Server) nextCoupling() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.couplingGen++
	return s.couplingGen
}

// errOffEndedTransition is the step apply's refusal of a with-On/Off
// transition the device was switched off during: the engine ends every
// transition as a cancel does (Settled, RemainingTime 0).
var errOffEndedTransition = errors.New("levelcontrol: the device was switched off during a with-On/Off transition")

// stepCouple is a step's coupling: it records what the transition couples
// to, as [Server.couple] does, and does not switch the device on again —
// the command's couple did, once. A step that reads the device off after
// a step of the same transition read it on marks the transition ended by
// the Off: the next step's apply refuses it ([errOffEndedTransition]) and
// the engine ends it as a cancel does. Follows connectedhomeip, which
// switches on once, when the with-On/Off command is taken, never at a
// step, and stops the running transition on an Off
// (src/app/clusters/level-control/LevelControlCluster.cpp:342-345,
// :742-826, :829-889, :351 at 6170af84; R2-P6-fsm-1401). matter.js
// couples on every step (LevelControlServer.ts:545-547) and lets the fade
// run on after an Off (:664 at db021da0). Until a step has read the
// device on, a step reads nothing into an Off: an On/Off port whose
// OnOff() lags its SetOnOff(true) keeps its fade. chip's Off effect itself
// — the fade to MinLevel over OffTransitionTime — is not adopted.
func (s *Server) stepCouple(gen uint64, withOnOff bool, options uint8, target float64) {
	coupleOnOff := withOnOff && s.onOff != nil
	c := coupling{
		turnsOff:         coupleOnOff && min(max(target, float64(s.minLevel())), float64(lvldef.LevelMax)) <= float64(s.minLevel()),
		colorTemperature: s.lighting && options&OptionCoupleColorTempToLevel != 0 && s.ct != nil,
	}
	on := coupleOnOff && s.onOff.OnOff()
	s.mu.Lock()
	defer s.mu.Unlock()
	if gen != s.couplingGen {
		return
	}
	s.coupling = c
	if !coupleOnOff {
		return
	}
	switch {
	case on:
		s.onSeenGen = gen
	case s.onSeenGen == gen:
		s.offEndedGen = gen
	}
}

// couple is matter.js LevelControlServer.couple: it records what the
// transition couples to and switches the device on for a "with On/Off"
// command whose transition does not end at the minimum. gen is the
// command's [Server.nextCoupling]: a step of a transition a newer command
// replaced records nothing, so the coupling the newer command's immediate
// level commits with is its own (R2-P6-conc-1409).
func (s *Server) couple(ctx context.Context, gen uint64, withOnOff bool, options uint8, target float64) (coupling, error) {
	coupleOnOff := withOnOff && s.onOff != nil
	c := coupling{
		turnsOff:         coupleOnOff && min(max(target, float64(s.minLevel())), float64(lvldef.LevelMax)) <= float64(s.minLevel()),
		colorTemperature: s.lighting && options&OptionCoupleColorTempToLevel != 0 && s.ct != nil,
	}
	s.mu.Lock()
	if gen == s.couplingGen {
		s.coupling = c
	}
	s.mu.Unlock()
	if coupleOnOff && !c.turnsOff && !s.onOff.OnOff() {
		// Moving towards on takes effect at once, which the CHIP tests
		// require (matter.js #turnOnForLevel).
		if err := s.onOff.SetOnOff(ctx, true); err != nil {
			return c, fmt.Errorf("levelcontrol: switch on: %w", err)
		}
		s.observeLevel(contract.OriginCommand)
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

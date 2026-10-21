// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package levelcontrol

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/cluster/wire"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/contract"
)

// NativeRamp is a device that ramps its level by itself — a dimmer with a
// ramp time, a player with a volume. It is all a host implements to take
// the hand-off path through [NewNativeSource]: the source keeps the
// Options and OnLevel attributes, applies the ExecuteIfOff gate, turns
// each of the eight commands into one target level and one duration, and
// couples the endpoint's On/Off state for the "with On/Off" commands, as
// matter.js LevelControlServer does on its own transitions.
type NativeRamp interface {
	// Level reports the device's current level; known=false is a level
	// the device has not reported, which reads as null. A level below the
	// server's MinLevel (a dimmer whose 0 means off) is reported as it is:
	// the source counts it as at the minimum for an owed switch-off, so a
	// host need not crop it.
	Level() (level uint8, known bool)
	// Ramp drives the device to level over d; d == 0 is at once. withOnOff
	// marks a "with On/Off" command: the source has already switched the
	// device on before a ramp away from the minimum and switches it off
	// once a ramp to the minimum has arrived, so a host needs it only when
	// its device couples the two itself.
	Ramp(ctx context.Context, level uint8, d time.Duration, withOnOff bool) error
	// Stop halts a running ramp where it is.
	Stop(ctx context.Context) error
}

// NativeMover is an optional interface of a [NativeRamp]: a device that
// implements it replaces the source's Move logic, as an implementation
// overrides matter.js's "default implementation of the Move commands",
// moveLogic (LevelControlServer.ts:378-403). Move and MoveWithOnOff call
// MoveLevel after the ExecuteIfOff gate (which matter.js applies in move
// before moveLogic, :351-361) instead of ramping to the bound; the On/Off
// coupling of MoveWithOnOff is then the host's, as an override of
// moveLogic skips transition and its couple (:529-550). A device for
// which a jump to the bound is no reading of a continuous move — a
// speaker's volume — returns nil and keeps its level. Stop and
// StopWithOnOff are unaffected.
type NativeMover interface {
	MoveLevel(ctx context.Context, req wire.MoveRequest, withOnOff bool) error
}

// NativeSwitcher is an optional interface of a [NativeRamp] whose On/Off
// and level are written together — a player that takes "start at volume
// X" and "stop" as one write each. For a "with On/Off" command the source
// calls SwitchOn(level, d) instead of switching the endpoint on and then
// ramping when the command turns the device on, and SwitchOff(d) instead
// of ramping to the minimum and then switching off when it ends there.
// The source then sets nothing on the OnOff attribute itself: the host's
// OnOff reflects what the device did. matter.js has no counterpart — it
// couples On/Off and the transition in one transaction against local
// state (LevelControlServer.ts:529-550, #turnOnForLevel) — so the
// interface is a bridged host's affordance; a device without it keeps
// matter.js's order. A [NativeMover] still takes Move and MoveWithOnOff.
type NativeSwitcher interface {
	SwitchOn(ctx context.Context, level uint8, d time.Duration) error
	SwitchOff(ctx context.Context, d time.Duration) error
}

// NativeConfig carries the construction parameters of [NewNativeSource].
type NativeConfig struct {
	// Lighting matches [Config.Lighting] of the server the source is
	// handed to: MinLevel is then 1, otherwise 0 (matter.js
	// LevelControlServer.ts:89-91).
	Lighting bool
	// Options and OnLevel are the attribute values the source starts with
	// — the host's persisted ones. A nil OnLevel is null.
	Options uint8
	OnLevel *uint8
	// Persist, when non-nil, receives the Options and OnLevel a write
	// would leave in effect, before they take effect; an error refuses
	// the write and keeps the previous values.
	Persist func(ctx context.Context, options uint8, onLevel *uint8) error
	// Clock pays a ramp's switch-off when the ramp is due to end; nil is
	// [clock.System].
	Clock clock.Clock
}

// NativeSource is the [LevelSource] [NewNativeSource] builds. It holds the
// two writable attributes in memory and drives a [NativeRamp].
type NativeSource struct {
	dev      NativeRamp
	onOff    OnOff
	minLevel uint8
	persist  func(ctx context.Context, options uint8, onLevel *uint8) error

	// wmu serialises the attribute writes, so the values handed to Persist
	// are the ones that take effect in that order.
	wmu sync.Mutex

	// mu guards off, onLevel and options.
	mu      sync.Mutex
	options uint8
	onLevel *uint8
	// off is the switch-off a "with On/Off" ramp to the minimum owes once
	// the level is there; nil when none is owed.
	off   *pendingOff
	clock clock.Clock
}

// pendingOff is a switch-off owed by one ramp: armed until the level
// reaches the minimum or a later command replaces the ramp.
type pendingOff struct {
	ctx context.Context
	// deadline pays the switch-off when the ramp is due to end, for a
	// device that does not notify or report the level it reached.
	deadline clock.Timer
}

// Compile-time assertions.
var (
	_ LevelSource             = (*NativeSource)(nil)
	_ contract.ChangeNotifier = (*NativeSource)(nil)
)

// NewNativeSource returns the [LevelSource] of a device that ramps by
// itself. onOff is the On/Off cluster of the same endpoint — the
// ExecuteIfOff gate reads it and the "with On/Off" commands drive it; nil
// means the endpoint has none: every command executes and none couples
// (matter.js `!this.agent.has(OnOffServer)`, LevelControlServer.ts:733 and
// :558). Its SetOnOff applies OnLevel on switch-on, as [OnOff] says; the
// host reads it from [NativeSource.OnLevel].
//
// The source is meant for the hand-off path ([Config.Transitions] nil):
// under the engine the server runs the commands itself and calls
// MoveToLevel alone, which the source then forwards ungated as an
// immediate ramp.
func NewNativeSource(dev NativeRamp, onOff OnOff, cfg NativeConfig) *NativeSource {
	n := &NativeSource{dev: dev, onOff: onOff, minLevel: LevelMin, persist: cfg.Persist, options: cfg.Options, clock: clock.Or(cfg.Clock)}
	if cfg.Lighting {
		n.minLevel = LightingLevelMin
	}
	if cfg.OnLevel != nil {
		v := *cfg.OnLevel
		n.onLevel = &v
	}
	// A device that notifies settles an owed switch-off the moment its
	// level arrives at the minimum.
	if c, ok := dev.(contract.ChangeNotifier); ok && c != nil {
		c.OnMatterValueChanged(n.settleOff)
	}
	return n
}

// CurrentLevel implements [LevelSource]: the device's level.
func (n *NativeSource) CurrentLevel() (uint8, bool) { return n.dev.Level() }

// optionsWire is the wire form of Options.
func (n *NativeSource) optionsWire() uint8 {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.options
}

// OnLevel implements [LevelSource].
func (n *NativeSource) OnLevel() (uint8, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.onLevel == nil {
		return 0, false
	}
	return *n.onLevel, true
}

// setOptionsWire is the wire form of SetOptions: the server has checked the bitmap
// against the FeatureMap; Persist sees it before it takes effect.
func (n *NativeSource) setOptionsWire(ctx context.Context, options uint8) error {
	n.wmu.Lock()
	defer n.wmu.Unlock()
	n.mu.Lock()
	onLevel := n.onLevel
	n.mu.Unlock()
	if err := n.save(ctx, options, onLevel); err != nil {
		return err
	}
	n.mu.Lock()
	n.options = options
	n.mu.Unlock()
	return nil
}

// SetOnLevel implements [LevelSource]; nil is null.
func (n *NativeSource) SetOnLevel(ctx context.Context, level *uint8) error {
	var v *uint8
	if level != nil {
		c := *level
		v = &c
	}
	n.wmu.Lock()
	defer n.wmu.Unlock()
	n.mu.Lock()
	options := n.options
	n.mu.Unlock()
	if err := n.save(ctx, options, v); err != nil {
		return err
	}
	n.mu.Lock()
	n.onLevel = v
	n.mu.Unlock()
	return nil
}

// save hands the values to Persist, when the host set one.
func (n *NativeSource) save(ctx context.Context, options uint8, onLevel *uint8) error {
	if n.persist == nil {
		return nil
	}
	if err := n.persist(ctx, options, onLevel); err != nil {
		return fmt.Errorf("levelcontrol: persist: %w", err)
	}
	return nil
}

// OnMatterValueChanged implements [contract.ChangeNotifier] by forwarding
// the device's own notifier, when it has one; otherwise it is a no-op.
func (n *NativeSource) OnMatterValueChanged(cb func()) (unsubscribe func()) {
	if c, ok := n.dev.(contract.ChangeNotifier); ok && c != nil {
		return c.OnMatterValueChanged(cb)
	}
	return func() {}
}

// moveToLevelWire is the wire form of MoveToLevel: gated by ExecuteIfOff, the level
// cropped to MinLevel..MaxLevel, over TransitionTime. Mirrors matter.js
// LevelControlServer.ts:263-278 moveToLevel (gate :269-272, crop :274) and
// moveToLevelLogic :326-342 — the rate (level - current) / TransitionTime
// * 10 covers any distance in TransitionTime tenths, which is the duration
// handed to the device; no OnOffTransitionTime is served, so a null
// TransitionTime is at once.
func (n *NativeSource) moveToLevelWire(ctx context.Context, req wire.MoveToLevelRequest) error {
	if !n.allowsExecution(n.effective(req.OptionsMask, req.OptionsOverride)) {
		return nil
	}
	d := TransitionDuration(req.TransitionTime)
	return n.ramp(ctx, CropLevel(req.Level, n.minLevel), func(uint8) time.Duration { return d }, false)
}

// moveToLevelWithOnOffWire is the wire form of MoveToLevelWithOnOff: as MoveToLevel without
// the gate, coupled to On/Off. Mirrors matter.js
// LevelControlServer.ts:285-300 moveToLevelWithOnOff.
func (n *NativeSource) moveToLevelWithOnOffWire(ctx context.Context, req wire.MoveToLevelRequest) error {
	d := TransitionDuration(req.TransitionTime)
	return n.ramp(ctx, CropLevel(req.Level, n.minLevel), func(uint8) time.Duration { return d }, true)
}

// moveWire is the wire form of Move: gated by ExecuteIfOff, towards MaxLevel or
// MinLevel at Rate per second. Mirrors matter.js LevelControlServer.ts
// :351-361 move (gate :354-357) and moveLogic :388-405.
func (n *NativeSource) moveWire(ctx context.Context, req wire.MoveRequest) error {
	if !n.allowsExecution(n.effective(req.OptionsMask, req.OptionsOverride)) {
		return nil
	}
	return n.move(ctx, req, false)
}

// moveWithOnOffWire is the wire form of MoveWithOnOff: as Move without the gate,
// coupled to On/Off. Mirrors matter.js LevelControlServer.ts:370-375
// moveWithOnOff.
func (n *NativeSource) moveWithOnOffWire(ctx context.Context, req wire.MoveRequest) error {
	return n.move(ctx, req, true)
}

// move is matter.js moveLogic: the target is the bound in the direction of
// MoveMode (matter.js ±Infinity, cropped by the transition's min/max,
// LevelControlServer.ts:186-195), reached at Rate per second from the
// level the coupling left (the rate applies after couple,
// LevelControlServer.ts:535-538). A null Rate is at once: matter.js
// falls back to DefaultMoveRate (:309-311), which this cluster does not
// serve. A zero Rate was refused by the server (:305-308).
func (n *NativeSource) move(ctx context.Context, req wire.MoveRequest, withOnOff bool) error {
	if m, ok := n.dev.(NativeMover); ok {
		n.cancelOff()
		return m.MoveLevel(ctx, req, withOnOff)
	}
	target := LevelMax
	if req.MoveMode == MoveModeDown {
		target = n.minLevel
	}
	if req.Rate == nil {
		return n.ramp(ctx, target, func(uint8) time.Duration { return 0 }, withOnOff)
	}
	if _, known := n.dev.Level(); !known {
		// A level the device has not reported cannot time a rated move,
		// and matter.js does not read it for one: moveLogic hands
		// transition ±Infinity (LevelControlServer.ts:388-403), couple
		// crops `targetLevel ?? this.currentLevel` (:529-550), and with
		// managed transitions off — the default, :786-791, and the
		// hardware NativeRamp models (Transitions.ts:44-49) —
		// Transitions.start sets the cropped target at once
		// (Transitions.ts:120-148). Step keeps its Failure: stepLogic
		// reads currentLevel (:478), which throws on null (:103-111).
		return n.ramp(ctx, target, func(uint8) time.Duration { return 0 }, withOnOff)
	}
	rate := *req.Rate
	return n.ramp(ctx, target, func(current uint8) time.Duration {
		return TransitionDuration(ptr(MoveTransition(distance(current, target), rate)))
	}, withOnOff)
}

// stepWire is the wire form of Step: gated by ExecuteIfOff, StepSize from the
// current level, over TransitionTime. Mirrors matter.js
// LevelControlServer.ts:414-428 step (gate :421-424) and stepLogic
// :464-479.
func (n *NativeSource) stepWire(ctx context.Context, req wire.StepRequest) error {
	if !n.allowsExecution(n.effective(req.OptionsMask, req.OptionsOverride)) {
		return nil
	}
	return n.step(ctx, req, false)
}

// stepWithOnOffWire is the wire form of StepWithOnOff: as Step without the gate,
// coupled to On/Off. Mirrors matter.js LevelControlServer.ts:435-450
// stepWithOnOff.
func (n *NativeSource) stepWithOnOffWire(ctx context.Context, req wire.StepRequest) error {
	return n.step(ctx, req, true)
}

// step is matter.js stepLogic (LevelControlServer.ts:471-478): the target
// current ± StepSize, cropped to MinLevel..MaxLevel by the transition's
// bounds, at the rate StepSize / TransitionTime * 10 — so a step the crop
// shortens arrives sooner, and one the coupling moved the level for
// covers the distance it has left. A null TransitionTime, a zero one
// (whose rate is infinite) and a zero StepSize (whose rate is zero) are at
// once (Transitions.ts:133).
func (n *NativeSource) step(ctx context.Context, req wire.StepRequest, withOnOff bool) error {
	current, err := n.currentLevel()
	if err != nil {
		return err
	}
	target := StepLevel(current, req.StepMode, req.StepSize, n.minLevel)
	tt := req.TransitionTime
	return n.ramp(ctx, target, func(from uint8) time.Duration {
		if tt == nil || *tt == 0 || req.StepSize == 0 {
			return 0
		}
		tenths := jsRound(float64(distance(from, target)) * float64(*tt) / float64(req.StepSize))
		return TransitionDuration(ptr(uint16(min(tenths, maxTransitionTenths))))
	}, withOnOff)
}

// stopWire is the wire form of Stop: gated by ExecuteIfOff, the ramp halts
// where it is. Mirrors matter.js LevelControlServer.ts:481-489 stop (gate
// :482-485) and stopLogic :505-506, which cancels the transition.
func (n *NativeSource) stopWire(ctx context.Context, req wire.StopRequest) error {
	if !n.allowsExecution(n.effective(req.OptionsMask, req.OptionsOverride)) {
		return nil
	}
	return n.stop(ctx)
}

// stopWithOnOffWire is the wire form of StopWithOnOff: as Stop, without the gate —
// matter.js LevelControlServer.ts:491-500 stopWithOnOff ("Unlike stop
// this does not consult the ExecuteIfOff option").
func (n *NativeSource) stopWithOnOffWire(ctx context.Context, _ wire.StopRequest) error {
	return n.stop(ctx)
}

func (n *NativeSource) stop(ctx context.Context) error {
	n.cancelOff()
	if err := n.dev.Stop(ctx); err != nil {
		return fmt.Errorf("levelcontrol: stop: %w", err)
	}
	return nil
}

// effective is [EffectiveOptions] over the stored Options attribute
// (matter.js LevelControlServer.ts:714-727 #calculateEffectiveOptions).
func (n *NativeSource) effective(mask, override uint8) uint8 {
	return EffectiveOptions(n.optionsWire(), mask, override)
}

// allowsExecution is matter.js LevelControlServer.ts:729-736
// #optionsAllowExecution: ExecuteIfOff in effect, no On/Off cluster, or
// the device on.
func (n *NativeSource) allowsExecution(options uint8) bool {
	return options&OptionExecuteIfOff != 0 || n.onOff == nil || n.onOff.OnOff()
}

// currentLevel is matter.js's currentLevel getter (LevelControlServer.ts
// :103-111): a null level cannot be operated on.
func (n *NativeSource) currentLevel() (uint8, error) {
	level, known := n.dev.Level()
	if !known {
		return 0, failureErr{"levelcontrol: the CurrentLevel value is null, so we cannot operate on it"}
	}
	return level, nil
}

// ramp is matter.js LevelControlServer.transition (:529-550) on a device
// that ramps by itself, in matter.js's order: couple first — a "with
// On/Off" command whose target, cropped, lies above the minimum switches
// the device on (:574-576, :589-590, #turnOnForLevel :597-603), which may
// move the level to OnLevel — then derive the duration from the level the
// coupling left (:536-538) and start the ramp, unless the device is
// already there (Transitions.ts:128-130); then couple on commit: the
// device switches off once a ramp that turns it off has reached the
// minimum (#coupleOnCommit :624-633).
//
// matter.js commits on every step of its own transition (:545-547), so
// the switch-off happens at the step that reaches the minimum. The
// device's ramp has no steps this side sees: the switch-off is owed from
// the ramp on and settled when the level is at the minimum — at once, on
// the device's change notification, or when the ramp is due to end —
// unless a later command replaces the ramp first, as a new matter.js
// transition stops the running one (Transitions.ts:124).
func (n *NativeSource) ramp(ctx context.Context, target uint8, duration func(current uint8) time.Duration, withOnOff bool) error {
	n.cancelOff()
	coupleOnOff := withOnOff && n.onOff != nil
	turnsOff := coupleOnOff && target <= n.minLevel
	if sw, ok := n.dev.(NativeSwitcher); ok && coupleOnOff {
		current, known := n.dev.Level()
		d := duration(current)
		if !known {
			d = 0
		}
		switch {
		case turnsOff && (n.onOff.OnOff() || !known || current != target):
			if err := sw.SwitchOff(ctx, d); err != nil {
				return fmt.Errorf("levelcontrol: switch off: %w", err)
			}
			return nil
		case !turnsOff && !n.onOff.OnOff():
			if err := sw.SwitchOn(ctx, target, d); err != nil {
				return fmt.Errorf("levelcontrol: switch on at %d: %w", target, err)
			}
			return nil
		}
	}
	if coupleOnOff && !turnsOff && !n.onOff.OnOff() {
		if err := n.onOff.SetOnOff(ctx, true); err != nil {
			return fmt.Errorf("levelcontrol: switch on: %w", err)
		}
	}
	current, known := n.dev.Level()
	var d time.Duration
	if !known || current != target {
		d = duration(current)
		if err := n.dev.Ramp(ctx, target, d, withOnOff); err != nil {
			return fmt.Errorf("levelcontrol: ramp to %d: %w", target, err)
		}
	}
	if !turnsOff {
		return nil
	}
	if d == 0 {
		// At once, the target is the level: matter.js writes it into state
		// on the immediate path (Transitions.ts:147, transitionImmediately
		// :238-241) and switches off in the same transaction's preCommit
		// once currentLevel is the minimum (LevelControlServer.ts:624-627),
		// so OnOff is false when the command returns. The device's later
		// report still corrects the level; it is not waited for.
		if n.onOff.OnOff() {
			if err := n.onOff.SetOnOff(ctx, false); err != nil {
				return fmt.Errorf("levelcontrol: switch off: %w", err)
			}
		}
		return nil
	}
	p := &pendingOff{ctx: context.WithoutCancel(ctx)}
	n.mu.Lock()
	n.off = p
	if d > 0 {
		// When the ramp is due to end the target is the level, reported or
		// not: matter.js's transition steps to targetValue and couples off
		// on that commit (Transitions.ts, LevelControlServer.ts:624-627).
		p.deadline = n.clock.AfterFunc(d, func() { _ = n.pay(p) })
	}
	n.mu.Unlock()
	return n.settle(p)
}

// settleOff settles an owed switch-off, if the level is at the minimum.
// It runs on the device's notification and at a ramp's deadline, where an
// error has no caller to reach.
func (n *NativeSource) settleOff() {
	n.mu.Lock()
	p := n.off
	n.mu.Unlock()
	if p != nil {
		_ = n.settle(p)
	}
}

// settle is matter.js #coupleOnCommit's On/Off half
// (LevelControlServer.ts:627-633): once the level is at the minimum, the
// switch-off p owes is paid — off, if the device is still on.
func (n *NativeSource) settle(p *pendingOff) error {
	// At or below the minimum: matter.js decides the switch-off with
	// cropValueRange(target, minLevel, maxLevel) <= minLevel
	// (LevelControlServer.ts couple), and a dimmer whose only knob is its
	// level reports 0 for "off", below a Lighting server's MinLevel of 1.
	if level, known := n.dev.Level(); !known || level > n.minLevel {
		return nil
	}
	return n.pay(p)
}

// pay pays the switch-off p owes, once: off, if the device is still on.
func (n *NativeSource) pay(p *pendingOff) error {
	n.mu.Lock()
	owed := n.off == p
	if owed {
		n.off = nil
		if p.deadline != nil {
			p.deadline.Stop()
		}
	}
	n.mu.Unlock()
	if !owed || !n.onOff.OnOff() {
		return nil
	}
	if err := n.onOff.SetOnOff(p.ctx, false); err != nil {
		return fmt.Errorf("levelcontrol: switch off: %w", err)
	}
	return nil
}

// cancelOff drops the switch-off an earlier ramp owed.
func (n *NativeSource) cancelOff() {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.off != nil && n.off.deadline != nil {
		n.off.deadline.Stop()
	}
	n.off = nil
}

// maxTransitionTenths caps a derived duration at the largest non-null
// TransitionTime (a nullable uint16 reserves 0xFFFF for null).
const maxTransitionTenths = 0xFFFE

// StepLevel is the level a Step command aims at: current + size for
// [StepModeUp], current - size for any other mode (matter.js `stepMode ===
// StepMode.Up ? 1 : -1`),
// cropped to minLevel..[LevelMax]. Mirrors matter.js LevelControlServer.ts
// :471 and :478 stepLogic (`this.currentLevel + stepSize * direction`)
// with the transition's bounds minLevel..maxLevel (:186-195, applied by
// Transitions.ts:138-143). minLevel is [LightingLevelMin] under the
// Lighting feature and [LevelMin] otherwise (:89-91).
func StepLevel(current, mode, size, minLevel uint8) uint8 {
	v := int(current) - int(size)
	if mode == StepModeUp {
		v = int(current) + int(size)
	}
	return uint8(min(max(v, int(minLevel)), int(LevelMax))) //nolint:gosec // cropped to 0..254
}

// CropLevel crops level to minLevel..[LevelMax]. Mirrors matter.js
// LevelControlServer.ts:274 and :291 `cropValueRange(level, this.minLevel,
// this.maxLevel)` (packages/general/src/util/Number.ts:54-56).
func CropLevel(level, minLevel uint8) uint8 { return min(max(level, minLevel), LevelMax) }

// TransitionDuration turns a TransitionTime in tenths of a second into a
// duration; a null or zero one is at once. Mirrors matter.js
// LevelControlServer.ts:332-338 moveToLevelLogic (a falsy transition time
// sets no rate, which Transitions.ts:133 applies at once) and the tenths
// of Transitions.ts:645-651 (DEFAULT_EXTERNAL_TIME_UNIT 100 ms, :698).
func TransitionDuration(tenths *uint16) time.Duration {
	if tenths == nil {
		return 0
	}
	return time.Duration(*tenths) * 100 * time.Millisecond
}

// MoveTransition is the TransitionTime, in tenths of a second, a move at
// rate units per second takes over distance: Math.round(distance * 10 /
// rate), capped at 0xFFFE; a zero rate is at once (0). Mirrors matter.js
// Transitions.ts:429 (`distanceLeft / changePerMs`) in tenths
// (:645-647 #externalTimeOf, Math.round), and :133 for the zero rate.
func MoveTransition(distance, rate uint8) uint16 {
	if rate == 0 {
		return 0
	}
	tenths := jsRound(float64(distance) * 10 / float64(rate))
	return uint16(min(tenths, maxTransitionTenths))
}

// LevelFromUnit maps a fraction 0..1 to the CurrentLevel range 0..254:
// Math.round(u * 254), cropped; NaN is 0. matter.js has no level
// counterpart: this is the attribute's range arithmetic (CurrentLevel
// "max 254", level-control.element.ts:29-30, :74), rounded as matter.js
// rounds its 0..1 saturation (ColorControlServer.ts:205-211). A light
// crops the result with [CropLevel] to its MinLevel.
func LevelFromUnit(u float64) uint8 {
	if math.IsNaN(u) {
		return 0
	}
	return uint8(min(max(jsRound(u*float64(LevelMax)), 0), float64(LevelMax)))
}

// UnitFromLevel maps a CurrentLevel 0..254 to a fraction 0..1: level /
// 254. matter.js has no level counterpart: this is the attribute's range
// arithmetic, as [LevelFromUnit].
func UnitFromLevel(level uint8) float64 { return float64(level) / float64(LevelMax) }

// jsRound is JavaScript's Math.round: half rounds up.
func jsRound(v float64) float64 { return math.Floor(v + 0.5) }

// distance is |a - b|.
func distance(a, b uint8) uint8 {
	if a > b {
		return a - b
	}
	return b - a
}

func ptr[T any](v T) *T { return &v }

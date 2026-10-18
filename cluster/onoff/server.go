// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package onoff

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/spec"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
)

// Feature is an OnOff FeatureMap bit, the generated type.
type Feature = onoffdef.Feature

// FeatureMap bits as [Feature] values for [Config].Features. LT and DF
// carry "[!OFFONLY]", OFFONLY "[!(LT | DF)]"; a selection breaking either
// is refused by [New].
const (
	FeatureLT      = onoffdef.FeatureLighting
	FeatureDF      = onoffdef.FeatureDeadFrontBehavior
	FeatureOFFONLY = onoffdef.FeatureOffOnly
)

// StartUpOnOff values (StartUpOnOffEnum).
const (
	StartUpOff    = onoffdef.StartUpOnOffOff
	StartUpOn     = onoffdef.StartUpOnOffOn
	StartUpToggle = onoffdef.StartUpOnOffToggle
)

// bootReasonSoftwareUpdateCompleted is GeneralDiagnostics
// BootReasonEnum.SoftwareUpdateCompleted (generaldiagnostics definition,
// value 5); matter.js does not apply StartUpOnOff after it
// (OnOffServer.ts:36).
const bootReasonSoftwareUpdateCompleted = 5

// tickPeriod is the period of both countdown timers: matter.js
// Time.getPeriodicTimer(…, Millis(100), …) (OnOffServer.ts:230-234,
// :258-262). OnTime and OffWaitTime are in tenths of a second.
const tickPeriod = 100 * time.Millisecond

// hold is the OnTime / OffWaitTime value that holds a countdown
// indefinitely (OnOffServer.ts:218, :240, :313).
const hold = 0xFFFF

// Sink is the optional host port: told about every OnOff change the
// server's on and off logic makes — a command, a countdown that ends —
// before the change is stored. An error refuses the change: a command
// answers FAILURE and nothing it would have changed is stored. It is the
// Go form of a matter.js host overriding on() / off(), which is "enough"
// for the timed actions as well (OnOffServer.ts:142-145, :194-197).
// The host's own changes ([Server.Set]) and the StartUpOnOff applied by
// [New] do not call it.
//
// The Sink runs with no lock held that [Server.Set] or a read of the
// server takes: it may confirm the change through [Server.Set] before it
// returns, on its own goroutine or from any other, and it may read the
// server. Commands, writes and the countdown ticks stay serialised behind
// it — the next one waits until it returned. A Set that lands while the
// Sink runs is stored and reported at once; when the Sink consents, the
// command stores its own OnOff over it (see [Server.Set]).
type Sink interface {
	SetOnOff(ctx context.Context, on bool) error
}

// Scenes is the optional port to the endpoint's ScenesManagement: the
// three operations matter.js's OnOffServer calls on
// ScenesManagementServer, named after them. cluster/core's
// ScenesManagement implements it; the endpoint layer hands the endpoint's
// one to the server ([Server.SetScenes]).
type Scenes interface {
	// MakeAllFabricSceneInfoEntriesInvalid ends the current scene's
	// validity: a command changed OnOff (OnOffServer.ts:97-100,
	// :114-117, :119-122).
	MakeAllFabricSceneInfoEntriesInvalid()
	// StoreGlobalScene stores the endpoint's scene-able values as the
	// fabric's global scene (OffWithEffect, :159-165).
	StoreGlobalScene(ctx context.Context, fabricIndex uint8)
	// RecallGlobalScene applies the fabric's global scene, if stored
	// (OnWithRecallGlobalScene, :175-184). The OnOff value comes back to
	// the server through [Server.MatterApplySceneValues] with ctx.
	RecallGlobalScene(ctx context.Context, fabricIndex uint8)
}

// Clock is the timer seam. AfterFunc runs f once after d; stop prevents
// a run that has not begun. The default is time.AfterFunc; tests pass a
// clock they advance by hand.
//
// Deprecated: set [Config.TimeSource] to a [clock.Clock], the module's
// time port; Clock may be removed in v0.10.0.
type Clock interface {
	AfterFunc(d time.Duration, f func()) (stop func() bool)
}

// portClock is a [clock.Clock] as the server's timer seam.
type portClock struct{ c clock.Clock }

func (p portClock) AfterFunc(d time.Duration, f func()) func() bool {
	return p.c.AfterFunc(d, f).Stop
}

// Config carries the construction parameters.
type Config struct {
	// Features is the FeatureMap selection.
	Features Feature
	// DataVersion is an optional host-owned tracker.
	DataVersion *cluster.DataVersionTracker
	// Sink is the optional host port.
	Sink Sink
	// Initial holds the values the server starts with — the persisted
	// OnOff and StartUpOnOff (quality N), say — checked against the
	// model. A served attribute it leaves out starts at false / 0 / null
	// (OnOff, GlobalSceneControl, OnTime and OffWaitTime; StartUpOnOff),
	// the values matter.js's logic reads an unset attribute as
	// (OnOffServer.ts:39, :105, :132, :159).
	Initial map[uint32]any
	// BootReason optionally returns the node's GeneralDiagnostics
	// BootReason; StartUpOnOff is not applied after a software update
	// (OnOffServer.ts:36, :326-331). nil applies it.
	BootReason func() uint8
	// TimeSource paces the OnTime / OffWaitTime countdown and the scene
	// transitions. Set, it wins over Clock; neither set is
	// [clock.System].
	TimeSource clock.Clock
	// Clock is the old timer seam.
	//
	// Deprecated: set TimeSource; Clock may be removed in v0.10.0.
	Clock Clock
	// Bridged marks a server on an endpoint under the Aggregator: matter.js
	// does not apply StartUpOnOff there
	// (OnOffServer.ts:37, `!this.endpoint.ownerOfType(AggregatorEndpoint)`).
	// The device layer sets it for a device added under the Aggregator.
	Bridged bool
	// Scenes is the optional ScenesManagement port; nil serves OnOff
	// without scenes, and the endpoint layer sets it where the endpoint
	// mounts ScenesManagement ([Server.SetScenes]).
	Scenes Scenes
}

// ErrInvalidConfig reports a configuration the cluster's model refuses.
var ErrInvalidConfig = errors.New("onoff: configuration outside the cluster's model")

// Server is the default OnOff server: matter.js OnOffBaseServer
// (packages/node/src/behaviors/on-off/OnOffServer.ts, HEAD 0ceb202d). The
// lists, globals, privileges, codecs and write checks come from the
// generated definition through [spec.Server]; the command logic, the
// StartUpOnOff at construction and the two countdown timers (timed on,
// delayed off) are matter.js's, cited by line.
//
// The DeadFrontBehavior feature has no logic here, as it has none in
// matter.js (:27-28). With a [Scenes] port the scene paths are matter.js's:
// a change of OnOff by a command invalidates the current scene (:97-100,
// :114-117, :119-122), OffWithEffect stores the global scene while
// GlobalSceneControl is true (:159-165), OnWithRecallGlobalScene recalls
// it (:175-184), and a scene's OnOff value is applied by the server itself
// — at once without a transition time, after it otherwise
// ([Server.MatterApplySceneValues], :268-310).
//
// Two locks. Every operation the server starts itself — a command, a
// write, a timer tick, a scene apply — runs in one queue (op), as
// matter.js runs the timer callbacks with `lock: true` (:233, :261), and
// stores its changes at once when it succeeds, as a matter.js command's
// transaction commits. The stored state has its own lock (mu), held only
// while a state is read or stored and never across a host port: the
// host's [Server.Set] takes only mu, so a [Sink] that confirms through it
// — synchronously or from another goroutine — never waits on the command
// that called it (issue #63). The cover server's Mover follows the same
// rule (cluster/cover windowcovering_server.go invokeMover).
type Server struct {
	*spec.Instance

	srv        *spec.Server
	sink       Sink
	clock      Clock
	lighting   bool
	timedOn    ticker
	delayedOff ticker

	// sceneApply is the pending transition-delayed scene apply (matter.js
	// applySceneDelayTimer / applyScenePendingOnOff, :283-288), under mu:
	// [Server.Set] drops it when it changes OnOff.
	sceneApply pendingSceneApply

	// op is the queue: commands, writes, timer ticks, scene applies and
	// the timers' running state. The host never takes it.
	op sync.Mutex
	// mu guards the stored state's read-modify-write ([Server.load],
	// [Server.commit], [Server.Set]) and sceneApply. It is never held
	// while a host port runs.
	mu sync.Mutex

	// scenes is the ScenesManagement port, nil without one. It has its
	// own lock so [Server.SetScenes] never waits for a command (a host
	// may reassemble from inside its sink).
	scenesMu sync.RWMutex
	scenes   Scenes
}

// pendingSceneApply is one scene OnOff value waiting for its transition
// time; gen tells a stale timer run to do nothing.
type pendingSceneApply struct {
	pending bool
	onOff   bool
	gen     uint64
	stop    func() bool
}

// Compile-time assertions.
var (
	_ contract.ClusterServer                  = (*Server)(nil)
	_ contract.ClusterDataVersion             = (*Server)(nil)
	_ contract.ClusterAttributeLister         = (*Server)(nil)
	_ contract.ClusterCommandLister           = (*Server)(nil)
	_ contract.ClusterAttributeWritePrivilege = (*Server)(nil)
	_ contract.ClusterCommandInvokePrivilege  = (*Server)(nil)
	_ contract.AttributeChangeNotifier        = (*Server)(nil)
	_ contract.ClusterQuiescer                = (*Server)(nil)
)

// New builds an OnOff server. With LT, StartUpOnOff is applied to OnOff
// unless the boot reason is SoftwareUpdateCompleted or the server is
// Bridged (OnOffServer.ts:34-50).
func New(cfg Config) (*Server, error) {
	s := &Server{sink: cfg.Sink, clock: cfg.Clock, scenes: cfg.Scenes}
	handlers := map[uint32]spec.CommandHandler{
		CmdOff:                     s.command(func(ctx context.Context, st *state, _ any) error { return s.off(ctx, st) }),
		CmdOn:                      s.command(func(ctx context.Context, st *state, _ any) error { return s.on(ctx, st) }),
		CmdToggle:                  s.command(s.toggle),
		CmdOffWithEffect:           s.command(s.offWithEffect),
		CmdOnWithRecallGlobalScene: s.command(s.onWithRecallGlobalScene),
		CmdOnWithTimedOff:          s.command(s.onWithTimedOff),
	}
	srv, err := spec.NewServer(onoffdef.Definition, spec.Options{Features: uint32(cfg.Features)}, spec.ServerConfig{DataVersion: cfg.DataVersion, Handlers: handlers})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}
	s.Instance, s.srv, s.lighting = srv.Instance, srv, srv.HasFeature("LT")
	switch {
	case cfg.TimeSource != nil:
		s.clock = portClock{cfg.TimeSource}
	case s.clock == nil:
		s.clock = portClock{clock.System{}}
	}
	initial := map[uint32]any{AttrOnOff: false}
	if s.lighting {
		initial[AttrGlobalSceneControl] = false
		initial[AttrOnTime] = uint16(0)
		initial[AttrOffWaitTime] = uint16(0)
		initial[AttrStartUpOnOff] = nil
	}
	for id, v := range cfg.Initial {
		initial[id] = v
	}
	if err := srv.SetAttributes(initial); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}
	// OnOffServer.ts:34-38: lighting, not after a software update, not
	// under an Aggregator.
	if s.lighting && !cfg.Bridged && (cfg.BootReason == nil || cfg.BootReason() != bootReasonSoftwareUpdateCompleted) {
		s.applyStartUp()
	}
	return s, nil
}

// applyStartUp is OnOffServer.ts:39-49: a non-null StartUpOnOff sets
// OnOff — Toggle inverts it, On / Off set it.
func (s *Server) applyStartUp() {
	v, _ := s.srv.Value(AttrStartUpOnOff)
	mode, ok := v.(uint8)
	if !ok { // null
		return
	}
	current := s.OnOff()
	target := mode == uint8(StartUpOn)
	if mode == uint8(StartUpToggle) {
		target = !current
	}
	if target != current {
		_ = s.srv.Set(AttrOnOff, target)
	}
}

// state is the working copy one operation changes; [Server.commit]
// stores it and reconciles the timers.
type state struct {
	onOff, globalSceneControl bool
	onTime, offWaitTime       uint16
	timedOnRunning            bool
	delayedOffRunning         bool
	// stored is OnOff as stored when st was loaded or last brought up to
	// date ([Server.setOnOff]): the baseline that tells a host's
	// [Server.Set] apart from the operation's own, not yet stored, change.
	stored bool
}

// load reads the stored state; the caller holds s.op (the timers' running
// state is op's), load takes s.mu.
func (s *Server) load() state {
	s.mu.Lock()
	defer s.mu.Unlock()
	on := s.OnOff()
	st := state{onOff: on, stored: on, timedOnRunning: s.timedOn.running, delayedOffRunning: s.delayedOff.running}
	if s.lighting {
		if v, ok := s.srv.Value(AttrGlobalSceneControl); ok {
			st.globalSceneControl, _ = v.(bool)
		}
		st.onTime = s.uint16Value(AttrOnTime)
		st.offWaitTime = s.uint16Value(AttrOffWaitTime)
	}
	return st
}

func (s *Server) uint16Value(attrID uint32) uint16 {
	v, _ := s.srv.Value(attrID)
	u, _ := v.(uint16)
	return u
}

// commit stores st and starts or stops the timers to match it; the
// caller holds s.op, commit takes s.mu for the store.
func (s *Server) commit(st state) {
	s.store(st)
	s.setTimer(&s.timedOn, st.timedOnRunning, s.timedOnTick)
	s.setTimer(&s.delayedOff, st.delayedOffRunning, s.delayedOffTick)
}

// store is commit's attribute half, under s.mu.
func (s *Server) store(st state) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// matter.js clears a pending scene apply whenever OnOff changes
	// (:62 reactTo(onOff$Changed, #clearDelayedSceneApplyData)); the
	// event fires after the transaction commits
	// (packages/node/src/behavior/state/managed/Datasource.ts:1163), so a
	// value a command armed and then changed OnOff past is dropped here.
	if st.onOff != s.OnOff() {
		s.clearSceneApply()
	}
	values := map[uint32]any{AttrOnOff: st.onOff}
	if s.lighting {
		values[AttrGlobalSceneControl] = st.globalSceneControl
		values[AttrOnTime] = st.onTime
		values[AttrOffWaitTime] = st.offWaitTime
	}
	_ = s.srv.SetAttributes(values) // every value is one the model admits
}

// command wraps one command's logic in the queue and the
// commit-on-success the handlers share. Only op is held while fn runs, so
// the Sink fn calls runs outside s.mu.
func (s *Server) command(fn func(ctx context.Context, st *state, fields any) error) spec.CommandHandler {
	return func(ctx context.Context, fields any) (any, error) {
		s.op.Lock()
		defer s.op.Unlock()
		st := s.load()
		if err := fn(ctx, &st, fields); err != nil {
			return nil, err
		}
		s.commit(st)
		return nil, nil
	}
}

// setOnOff tells the sink, which may refuse, and then re-loads OnOff: when
// the stored value moved since st was loaded — a [Server.Set] the host
// made while the sink ran, the confirmation of this very change or
// another — st takes it, so the handler compares against what the host
// stored, not what it read before. When it did not move, st keeps its
// own value: an operation's change that is not stored yet (the scene
// recall inside OnWithRecallGlobalScene) is not undone. Only OnOff can
// have moved: the host's setter changes nothing else, and every other
// change waits in the queue (s.op) the caller holds. The caller holds
// neither s.mu nor anything [Server.Set] takes.
func (s *Server) setOnOff(ctx context.Context, st *state, on bool) error {
	if s.sink == nil {
		return nil
	}
	if err := s.sink.SetOnOff(ctx, on); err != nil {
		return spec.Errorf(im.StatusFailure, "OnOff: change to %t refused: %v", on, err)
	}
	if now := s.OnOff(); now != st.stored {
		st.onOff, st.stored = now, now
	}
	return nil
}

// on is OnOffServer.ts:97-112: a change to on invalidates the current
// scene (:98-100); OnOff true; with LT GlobalSceneControl true, and with
// OnTime 0 the delayed-off timer stops and OffWaitTime is cleared
// (OffWaitTime is kept during a timed-on phase, :104). The invalidation
// follows the sink's consent: matter.js would roll it back with the
// refused command's transaction. "A change to on" compares with OnOff as
// stored after the sink returned ([Server.setOnOff]).
func (s *Server) on(ctx context.Context, st *state) error {
	if err := s.setOnOff(ctx, st, true); err != nil {
		return err
	}
	if !st.onOff {
		s.invalidateScenes()
	}
	st.onOff = true
	if s.lighting {
		st.globalSceneControl = true
		if st.onTime == 0 {
			st.delayedOffRunning = false
			st.offWaitTime = 0
		}
	}
	return nil
}

// off is OnOffServer.ts:119-139: a change to off invalidates the current
// scene (:120-122); OnOff false; with LT the timed-on timer stops, OnTime
// is 0, and OffWaitTime in 1..0xFFFE starts the delayed-off timer
// (:129-137). As in on, the comparison reads OnOff as stored after the
// sink returned.
func (s *Server) off(ctx context.Context, st *state) error {
	if err := s.setOnOff(ctx, st, false); err != nil {
		return err
	}
	if st.onOff {
		s.invalidateScenes()
	}
	st.onOff = false
	if s.lighting {
		st.timedOnRunning = false
		st.onTime = 0
		if st.offWaitTime > 0 && st.offWaitTime != hold && !st.delayedOffRunning {
			st.delayedOffRunning = true
		}
	}
	return nil
}

// toggle is OnOffServer.ts:146-152.
func (s *Server) toggle(ctx context.Context, st *state, _ any) error {
	if st.onOff {
		return s.off(ctx, st)
	}
	return s.on(ctx, st)
}

// offWithEffect is OnOffServer.ts:158-169: the effect is ignored; while
// GlobalSceneControl is true the global scene of the accessing fabric is
// stored (:160-165, a remote actor with a fabric) and GlobalSceneControl
// goes false; then off.
func (s *Server) offWithEffect(ctx context.Context, st *state, _ any) error {
	if st.globalSceneControl {
		if sc := s.scenesPort(); sc != nil {
			if fabric := accessingFabric(ctx); fabric != 0 {
				sc.StoreGlobalScene(ctx, fabric)
			}
		}
		st.globalSceneControl = false
	}
	return s.off(ctx, st)
}

// onWithRecallGlobalScene is OnOffServer.ts:171-191: nothing while
// GlobalSceneControl is true; else the global scene of the accessing
// fabric is recalled (:175-184), GlobalSceneControl goes true, OffWaitTime
// is cleared with OnTime 0, then on. The recall's OnOff value is applied to
// this command's state (the same matter.js transaction), through the
// context it carries.
func (s *Server) onWithRecallGlobalScene(ctx context.Context, st *state, _ any) error {
	if st.globalSceneControl {
		return nil
	}
	if sc := s.scenesPort(); sc != nil {
		if fabric := accessingFabric(ctx); fabric != 0 {
			sc.RecallGlobalScene(context.WithValue(ctx, inCommandKey{}, &inCommand{srv: s, st: st}), fabric)
		}
	}
	st.globalSceneControl = true
	if st.onTime == 0 {
		st.offWaitTime = 0
	}
	return s.on(ctx, st)
}

// accessingFabric is the fabric of the session a command arrived on, 0
// for none — matter.js's `hasRemoteActor(this.context) &&
// this.context.session.fabric !== undefined` (:160, :176-177).
func accessingFabric(ctx context.Context) uint8 {
	_, fabric := im.FabricFilterFromContext(ctx)
	return fabric
}

// invalidateScenes is OnOffServer.ts:114-117 #invalidateScenes.
func (s *Server) invalidateScenes() {
	if sc := s.scenesPort(); sc != nil {
		sc.MakeAllFabricSceneInfoEntriesInvalid()
	}
}

func (s *Server) scenesPort() Scenes {
	s.scenesMu.RLock()
	defer s.scenesMu.RUnlock()
	return s.scenes
}

// inCommandKey marks a context that carries the state of a command this
// server is running (under s.op), so a scene value applied from inside it
// — OnWithRecallGlobalScene's recall — changes that state instead of
// waiting for the lock the command holds.
type inCommandKey struct{}

type inCommand struct {
	srv *Server
	st  *state
}

// MatterApplySceneValues implements cluster/core's SceneValuesApplier:
// matter.js #applySceneValues (OnOffServer.ts:267-289), registered with
// ScenesManagementServer.implementScenes (:60-63). A pending delayed apply
// is dropped; a value that is not a boolean (0 / 1) or equals OnOff is
// ignored; with transition time 0 the server turns on or off at once,
// otherwise after transitionMs on the server's clock
// (#applyDelayedSceneOnOffValue, :299-310). A change of OnOff in between
// drops the pending value (:62, :291-297).
func (s *Server) MatterApplySceneValues(ctx context.Context, values map[uint32]uint64, transitionMs uint32) {
	if ic, ok := ctx.Value(inCommandKey{}).(*inCommand); ok && ic.srv == s {
		s.applySceneValues(ctx, ic.st, values, transitionMs)
		return
	}
	s.op.Lock()
	defer s.op.Unlock()
	st := s.load()
	if s.applySceneValues(ctx, &st, values, transitionMs) {
		s.commit(st)
	}
}

// applySceneValues is #applySceneValues on st; it reports whether st
// changed. The caller holds s.op.
func (s *Server) applySceneValues(ctx context.Context, st *state, values map[uint32]uint64, transitionMs uint32) bool {
	s.mu.Lock()
	s.clearSceneApply()
	s.mu.Unlock()
	v, ok := values[AttrOnOff]
	if !ok || v > 1 || (v == 1) == st.onOff {
		return false
	}
	on := v == 1
	if transitionMs == 0 {
		var err error
		if on {
			err = s.on(ctx, st)
		} else {
			err = s.off(ctx, st)
		}
		return err == nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sceneApply.pending, s.sceneApply.onOff = true, on
	s.sceneApply.gen++
	gen := s.sceneApply.gen
	// The timer's run outlives the call that started it, as matter.js
	// runs #applyDelayedSceneOnOffValue as its own callback (:284-288).
	//nolint:contextcheck // the run comes after the call that armed it returned
	s.sceneApply.stop = s.clock.AfterFunc(time.Duration(transitionMs)*time.Millisecond, func() {
		s.applyDelayedSceneOnOffValue(gen)
	})
	return false
}

// clearSceneApply is #clearDelayedSceneApplyData (:291-297). The caller
// holds s.mu.
func (s *Server) clearSceneApply() {
	if !s.sceneApply.pending {
		return
	}
	s.sceneApply.pending = false
	s.sceneApply.gen++
	if s.sceneApply.stop != nil {
		s.sceneApply.stop()
		s.sceneApply.stop = nil
	}
}

// applyDelayedSceneOnOffValue is #applyDelayedSceneOnOffValue (:299-310):
// the pending value, if still pending, turns the server on or off.
func (s *Server) applyDelayedSceneOnOffValue(gen uint64) {
	s.op.Lock()
	defer s.op.Unlock()
	s.mu.Lock()
	if !s.sceneApply.pending || s.sceneApply.gen != gen {
		s.mu.Unlock()
		return
	}
	on := s.sceneApply.onOff
	s.clearSceneApply()
	s.mu.Unlock()
	st := s.load()
	var err error
	if on {
		err = s.on(context.Background(), &st)
	} else {
		err = s.off(context.Background(), &st)
	}
	if err == nil {
		s.commit(st)
	}
}

// SetScenes sets the ScenesManagement port ([Config].Scenes); nil removes
// it. The endpoint layer calls it with the endpoint's ScenesManagement
// whenever it mounts one.
func (s *Server) SetScenes(sc Scenes) {
	s.scenesMu.Lock()
	defer s.scenesMu.Unlock()
	s.scenes = sc
}

// MatterInvalidatesScenes implements cluster/core's SceneInvalidator: with
// a [Scenes] port the server invalidates the current scene itself, only
// when a command changes OnOff (OnOffServer.ts:97-100, :119-122).
func (s *Server) MatterInvalidatesScenes() bool {
	return s.scenesPort() != nil
}

// onWithTimedOff is OnOffServer.ts:198-225.
func (s *Server) onWithTimedOff(ctx context.Context, st *state, fields any) error {
	req, ok := fields.(onoffdef.OnWithTimedOffRequest)
	if !ok {
		return spec.Errorf(im.StatusInvalidCommand, "OnWithTimedOff payload %T", fields)
	}
	if req.OnOffControl&onoffdef.OnOffControlAcceptOnlyWhenOn != 0 && !st.onOff { // :199-201
		return nil
	}
	if st.offWaitTime > 0 && !st.onOff { // :203-214: delayed off — only lower OffWaitTime
		st.offWaitTime = min(req.OffWaitTime, st.offWaitTime)
		if !st.delayedOffRunning && st.offWaitTime > 0 && st.offWaitTime != hold {
			st.delayedOffRunning = true
		}
		return nil
	}
	st.onTime = max(req.OnTime, st.onTime)   // :216
	st.offWaitTime = req.OffWaitTime         // :217
	if st.onTime != 0 && st.onTime != hold { // :219-223
		st.timedOnRunning = true
	} else {
		st.timedOnRunning = false
	}
	return s.on(ctx, st) // :224
}

// timedOnTick is OnOffServer.ts:239-253: 0xFFFF holds (the timer stops);
// otherwise OnTime counts down, and at 0 the timer stops, OffWaitTime is
// cleared and the server turns off. The caller holds s.op.
func (s *Server) timedOnTick() {
	st := s.load()
	if st.onTime == hold {
		st.timedOnRunning = false
		s.commit(st)
		return
	}
	t := int(st.onTime) - 1
	if t <= 0 {
		t = 0
		st.timedOnRunning = false
		st.offWaitTime = 0
		if err := s.off(context.Background(), &st); err != nil {
			// The sink refused: the timer has stopped (matter.js stops it
			// before off(), :248), the state stays as it was.
			s.setTimer(&s.timedOn, false, nil)
			return
		}
	}
	st.onTime = uint16(t) //nolint:gosec // 0 ≤ t < 0xFFFF
	s.commit(st)
}

// delayedOffTick is OnOffServer.ts:310-322: 0xFFFF holds (the timer
// stops); otherwise OffWaitTime counts down and the timer stops at 0.
// The caller holds s.op.
func (s *Server) delayedOffTick() {
	st := s.load()
	if st.offWaitTime == hold {
		st.delayedOffRunning = false
		s.commit(st)
		return
	}
	t := int(st.offWaitTime) - 1
	if t <= 0 {
		t = 0
		st.delayedOffRunning = false
	}
	st.offWaitTime = uint16(t) //nolint:gosec // 0 ≤ t < 0xFFFF
	s.commit(st)
}

// MatterDataVersion implements [contract.ClusterDataVersion].
func (s *Server) MatterDataVersion() uint32 { return s.srv.MatterDataVersion() }

// OnMatterAttributesChanged implements [contract.AttributeChangeNotifier].
func (s *Server) OnMatterAttributesChanged(cb func(attrIDs []uint32)) (unsubscribe func()) {
	return s.srv.OnMatterAttributesChanged(cb)
}

// MatterRead resolves an attribute.
func (s *Server) MatterRead(attrID uint32) (any, bool) { return s.srv.MatterRead(attrID) }

// MatterWrite applies a controller write to OnTime, OffWaitTime or
// StartUpOnOff after the definition's checks. A write never starts a
// countdown; one that parks the running countdown's attribute at 0 or
// 0xFFFF stops it (OnOffServer.ts:52-58, #stopHeldTimer :71-84).
func (s *Server) MatterWrite(ctx context.Context, attrID uint32, value any) error {
	s.op.Lock()
	defer s.op.Unlock()
	if err := s.srv.MatterWrite(ctx, attrID, value); err != nil {
		return err
	}
	if s.lighting && (attrID == AttrOnTime || attrID == AttrOffWaitTime) {
		s.stopHeldTimer()
	}
	return nil
}

// stopHeldTimer is OnOffServer.ts:71-84; the caller holds s.op.
func (s *Server) stopHeldTimer() {
	st := s.load()
	if st.onOff {
		if s.timedOn.running && (st.onTime == 0 || st.onTime == hold) {
			s.setTimer(&s.timedOn, false, nil)
		}
	} else if s.delayedOff.running && (st.offWaitTime == 0 || st.offWaitTime == hold) {
		s.setTimer(&s.delayedOff, false, nil)
	}
}

// MatterInvoke dispatches an accepted command; the generated server
// answers any other with UNSUPPORTED_COMMAND — On and Toggle with
// OFFONLY, the three LT commands without LT, as their conformance
// disables them (OnOffServer.ts:26).
func (s *Server) MatterInvoke(ctx context.Context, cmdID uint32, fields any) (any, error) {
	return s.srv.MatterInvoke(ctx, cmdID, fields)
}

// MatterQuiesce implements [contract.ClusterQuiescer]: both countdown
// timers stop without a report, as OnOffServer.ts's asyncDispose stops
// them (:90-95). A later command starts them again. A pending
// transition-delayed scene apply is dropped as well, which matter.js's
// asyncDispose does not name: a quiesced server changes nothing on its
// own.
func (s *Server) MatterQuiesce() {
	s.op.Lock()
	defer s.op.Unlock()
	s.setTimer(&s.timedOn, false, nil)
	s.setTimer(&s.delayedOff, false, nil)
	s.mu.Lock()
	s.clearSceneApply()
	s.mu.Unlock()
}

// Set records an OnOff change the device made itself: the value is
// stored and reported, the sink is not called and no timer moves — a
// matter.js host setting the state rather than invoking a command.
//
// Set takes only the state lock, never the command queue, so it may be
// called from inside the [Sink] — before SetOnOff returns, on the same
// goroutine — and from any other goroutine while a Sink call is in
// flight, without waiting for the command (issue #63). Its value is
// stored and reported at once. A command whose Sink is in flight then
// compares with it (a scene is invalidated only if the command changes
// what Set stored) and, once the Sink consents, stores its own OnOff over
// it: the command wins its own attribute, and the host's echo of the very
// change carries the same value.
func (s *Server) Set(on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if on != s.OnOff() {
		s.clearSceneApply() // :62, onOff$Changed
	}
	_ = s.srv.Set(AttrOnOff, on)
}

// OnOff returns the current OnOff value.
func (s *Server) OnOff() bool {
	v, _ := s.srv.Value(AttrOnOff)
	b, _ := v.(bool)
	return b
}

// ticker is a periodic timer built on [Clock.AfterFunc]: one pending
// run at a time, re-armed after each tick while running. gen tells a
// stale run (one that was already due when the timer stopped) to do
// nothing.
type ticker struct {
	running bool
	gen     uint64
	stop    func() bool
}

// setTimer starts or stops t; a start of a running timer and a stop of a
// stopped one change nothing. The caller holds s.op, not s.mu.
func (s *Server) setTimer(t *ticker, run bool, tick func()) {
	switch {
	case run && !t.running:
		t.running = true
		t.gen++
		s.arm(t, t.gen, tick)
	case !run && t.running:
		t.running = false
		t.gen++
		if t.stop != nil {
			t.stop()
			t.stop = nil
		}
	}
}

func (s *Server) arm(t *ticker, gen uint64, tick func()) {
	t.stop = s.clock.AfterFunc(tickPeriod, func() {
		s.op.Lock()
		defer s.op.Unlock()
		if !t.running || t.gen != gen {
			return
		}
		tick()
		if t.running && t.gen == gen {
			s.arm(t, gen, tick)
		}
	})
}

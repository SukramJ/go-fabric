// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package onoff

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

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
type Sink interface {
	SetOnOff(ctx context.Context, on bool) error
}

// Clock is the timer seam. AfterFunc runs f once after d; stop prevents
// a run that has not begun. The default is time.AfterFunc; tests pass a
// clock they advance by hand.
type Clock interface {
	AfterFunc(d time.Duration, f func()) (stop func() bool)
}

type wallClock struct{}

func (wallClock) AfterFunc(d time.Duration, f func()) func() bool {
	return time.AfterFunc(d, f).Stop
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
	// Clock is the timer seam; nil is the wall clock.
	Clock Clock
}

// ErrInvalidConfig reports a configuration the cluster's model refuses.
var ErrInvalidConfig = errors.New("onoff: configuration outside the cluster's model")

// Server is the default OnOff server: matter.js OnOffBaseServer
// (packages/node/src/behaviors/on-off/OnOffServer.ts, HEAD 4633bfd7). The
// lists, globals, privileges, codecs and write checks come from the
// generated definition through [spec.Server]; the command logic, the
// StartUpOnOff at construction and the two countdown timers (timed on,
// delayed off) are matter.js's, cited by line.
//
// The DeadFrontBehavior feature has no logic here, as it has none in
// matter.js (:27-28). Scenes are left to ScenesManagement: it recalls an
// OnOff value through the On / Off commands (cluster/core
// scenes_attributes.go applyValues); the transition-delayed apply
// (:268-310), the scene invalidation on a change (:98-100, :114-117,
// :120-122) and the global-scene store / recall (:159-165, :175-184) are
// not carried out by this server.
//
// Every operation — a command, a write, a timer tick, a host change — runs
// under one lock, as matter.js runs the timer callbacks with `lock: true`
// (:233, :261), and stores its changes at once when it succeeds, as a
// matter.js command's transaction commits.
type Server struct {
	*spec.Instance

	srv        *spec.Server
	sink       Sink
	clock      Clock
	lighting   bool
	timedOn    ticker
	delayedOff ticker

	op sync.Mutex
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
// unless the boot reason is SoftwareUpdateCompleted (OnOffServer.ts:34-50).
func New(cfg Config) (*Server, error) {
	srv, err := spec.NewServer(onoffdef.Definition, spec.Options{Features: uint32(cfg.Features)}, spec.ServerConfig{DataVersion: cfg.DataVersion})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}
	s := &Server{Instance: srv.Instance, srv: srv, sink: cfg.Sink, clock: cfg.Clock, lighting: srv.HasFeature("LT")}
	if s.clock == nil {
		s.clock = wallClock{}
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
	if s.lighting && (cfg.BootReason == nil || cfg.BootReason() != bootReasonSoftwareUpdateCompleted) {
		s.applyStartUp()
	}
	srv.Handle(CmdOff, s.command(func(ctx context.Context, st *state, _ any) error { return s.off(ctx, st) }))
	srv.Handle(CmdOn, s.command(func(ctx context.Context, st *state, _ any) error { return s.on(ctx, st) }))
	srv.Handle(CmdToggle, s.command(s.toggle))
	srv.Handle(CmdOffWithEffect, s.command(s.offWithEffect))
	srv.Handle(CmdOnWithRecallGlobalScene, s.command(s.onWithRecallGlobalScene))
	srv.Handle(CmdOnWithTimedOff, s.command(s.onWithTimedOff))
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
}

// load reads the stored state; the caller holds s.op.
func (s *Server) load() state {
	st := state{onOff: s.OnOff(), timedOnRunning: s.timedOn.running, delayedOffRunning: s.delayedOff.running}
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
// caller holds s.op.
func (s *Server) commit(st state) {
	values := map[uint32]any{AttrOnOff: st.onOff}
	if s.lighting {
		values[AttrGlobalSceneControl] = st.globalSceneControl
		values[AttrOnTime] = st.onTime
		values[AttrOffWaitTime] = st.offWaitTime
	}
	_ = s.srv.SetAttributes(values) // every value is one the model admits
	s.setTimer(&s.timedOn, st.timedOnRunning, s.timedOnTick)
	s.setTimer(&s.delayedOff, st.delayedOffRunning, s.delayedOffTick)
}

// command wraps one command's logic in the operation lock and the
// commit-on-success the handlers share.
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

// setOnOff tells the sink, which may refuse.
func (s *Server) setOnOff(ctx context.Context, on bool) error {
	if s.sink == nil {
		return nil
	}
	if err := s.sink.SetOnOff(ctx, on); err != nil {
		return spec.Errorf(im.StatusFailure, "OnOff: change to %t refused: %v", on, err)
	}
	return nil
}

// on is OnOffServer.ts:97-112: OnOff true; with LT GlobalSceneControl
// true, and with OnTime 0 the delayed-off timer stops and OffWaitTime is
// cleared (OffWaitTime is kept during a timed-on phase, :104).
func (s *Server) on(ctx context.Context, st *state) error {
	if err := s.setOnOff(ctx, true); err != nil {
		return err
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

// off is OnOffServer.ts:119-139: OnOff false; with LT the timed-on timer
// stops, OnTime is 0, and OffWaitTime in 1..0xFFFE starts the delayed-off
// timer (:129-137).
func (s *Server) off(ctx context.Context, st *state) error {
	if err := s.setOnOff(ctx, false); err != nil {
		return err
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

// offWithEffect is OnOffServer.ts:158-169: the effect is ignored;
// GlobalSceneControl goes false, then off.
func (s *Server) offWithEffect(ctx context.Context, st *state, _ any) error {
	st.globalSceneControl = false
	return s.off(ctx, st)
}

// onWithRecallGlobalScene is OnOffServer.ts:171-191: nothing while
// GlobalSceneControl is true; else GlobalSceneControl true, OffWaitTime
// cleared with OnTime 0, then on.
func (s *Server) onWithRecallGlobalScene(ctx context.Context, st *state, _ any) error {
	if st.globalSceneControl {
		return nil
	}
	st.globalSceneControl = true
	if st.onTime == 0 {
		st.offWaitTime = 0
	}
	return s.on(ctx, st)
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

// delayedOffTick is OnOffServer.ts:312-324: 0xFFFF holds (the timer
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
// them (:90-95). A later command starts them again.
func (s *Server) MatterQuiesce() {
	s.op.Lock()
	defer s.op.Unlock()
	s.setTimer(&s.timedOn, false, nil)
	s.setTimer(&s.delayedOff, false, nil)
}

// Set records an OnOff change the device made itself: the value is
// stored and reported, the sink is not called and no timer moves — a
// matter.js host setting the state rather than invoking a command.
func (s *Server) Set(on bool) {
	s.op.Lock()
	defer s.op.Unlock()
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
// stopped one change nothing. The caller holds s.op.
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

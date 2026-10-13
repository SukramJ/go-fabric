// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package onoff_test

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/onoff"
	onoffdef "github.com/SukramJ/go-fabric/cluster/spec/onoff"
	"github.com/SukramJ/go-fabric/im"
)

// fakeClock runs AfterFunc callbacks only when the test advances it, on
// the test's goroutine, in due order.
type fakeClock struct {
	mu      sync.Mutex
	now     time.Duration
	pending []*fakeTimer
}

type fakeTimer struct {
	due  time.Duration
	f    func()
	done bool
}

func (c *fakeClock) AfterFunc(d time.Duration, f func()) func() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{due: c.now + d, f: f}
	c.pending = append(c.pending, t)
	return func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		was := !t.done
		t.done = true
		return was
	}
}

// Advance moves the clock by d, running every callback that falls due.
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	end := c.now + d
	c.mu.Unlock()
	for {
		c.mu.Lock()
		sort.SliceStable(c.pending, func(i, j int) bool { return c.pending[i].due < c.pending[j].due })
		var next *fakeTimer
		for len(c.pending) > 0 {
			t := c.pending[0]
			if t.done {
				c.pending = c.pending[1:]
				continue
			}
			if t.due <= end {
				next = t
				c.pending = c.pending[1:]
				t.done = true
				c.now = t.due
			}
			break
		}
		if next == nil {
			c.now = end
			c.mu.Unlock()
			return
		}
		c.mu.Unlock()
		next.f()
	}
}

// Pending counts the callbacks not yet run or stopped.
func (c *fakeClock) Pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, t := range c.pending {
		if !t.done {
			n++
		}
	}
	return n
}

// recSink records the sink calls and refuses while refuse is set.
type recSink struct {
	mu     sync.Mutex
	calls  []bool
	refuse bool
}

func (r *recSink) SetOnOff(_ context.Context, on bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.refuse {
		return errors.New("relay stuck")
	}
	r.calls = append(r.calls, on)
	return nil
}

type fixture struct {
	t     *testing.T
	srv   *onoff.Server
	clock *fakeClock
	sink  *recSink
}

func newFixture(t *testing.T, features onoff.Feature, initial map[uint32]any) *fixture {
	t.Helper()
	f := &fixture{t: t, clock: &fakeClock{}, sink: &recSink{}}
	srv, err := onoff.New(onoff.Config{Features: features, Sink: f.sink, Initial: initial, Clock: f.clock})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	f.srv = srv
	return f
}

func (f *fixture) invoke(cmd uint32, fields any) error {
	f.t.Helper()
	_, err := f.srv.MatterInvoke(context.Background(), cmd, fields)
	return err
}

func (f *fixture) mustInvoke(cmd uint32, fields any) {
	f.t.Helper()
	if err := f.invoke(cmd, fields); err != nil {
		f.t.Fatalf("invoke 0x%02X: %v", cmd, err)
	}
}

func (f *fixture) mustInvokeCtx(ctx context.Context, cmd uint32, fields any) {
	f.t.Helper()
	if _, err := f.srv.MatterInvoke(ctx, cmd, fields); err != nil {
		f.t.Fatalf("invoke 0x%02X: %v", cmd, err)
	}
}

func (f *fixture) read(attr uint32) any {
	f.t.Helper()
	v, ok := f.srv.MatterRead(attr)
	if !ok {
		f.t.Fatalf("attribute 0x%04X absent", attr)
	}
	return v
}

// lt is the observable LT state.
type lt struct {
	OnOff, GSC          bool
	OnTime, OffWaitTime uint16
}

func (f *fixture) state() lt {
	f.t.Helper()
	return lt{
		OnOff:       f.read(onoff.AttrOnOff).(bool),
		GSC:         f.read(onoff.AttrGlobalSceneControl).(bool),
		OnTime:      f.read(onoff.AttrOnTime).(uint16),
		OffWaitTime: f.read(onoff.AttrOffWaitTime).(uint16),
	}
}

func (f *fixture) want(w lt) {
	f.t.Helper()
	if got := f.state(); got != w {
		f.t.Errorf("state %+v, want %+v", got, w)
	}
}

func timedOff(control onoffdef.OnOffControlBitmap, onTime, offWait uint16) onoffdef.OnWithTimedOffRequest {
	return onoffdef.OnWithTimedOffRequest{OnOffControl: control, OnTime: onTime, OffWaitTime: offWait}
}

func statusOf(err error) im.StatusCode {
	var se im.StatusCodeError
	if errors.As(err, &se) {
		return se.MatterStatusCode()
	}
	return im.StatusSuccess
}

// TestStartUpOnOff holds the boot rule (OnOffServer.ts:34-50): with LT a
// non-null StartUpOnOff sets OnOff — Toggle inverts it — unless the boot
// reason is SoftwareUpdateCompleted or the server is Bridged (:37); without
// LT nothing is applied.
func TestStartUpOnOff(t *testing.T) {
	t.Parallel()
	sw := uint8(5) // GeneralDiagnostics BootReason SoftwareUpdateCompleted
	power := uint8(1)
	cases := []struct {
		name    string
		feat    onoff.Feature
		initial bool
		startUp any
		boot    *uint8
		bridged bool
		want    bool
	}{
		{"null keeps off", onoff.FeatureLT, false, nil, nil, false, false},
		{"null keeps on", onoff.FeatureLT, true, nil, nil, false, true},
		{"On", onoff.FeatureLT, false, onoff.StartUpOn, nil, false, true},
		{"Off", onoff.FeatureLT, true, onoff.StartUpOff, nil, false, false},
		{"Toggle off→on", onoff.FeatureLT, false, onoff.StartUpToggle, nil, false, true},
		{"Toggle on→off", onoff.FeatureLT, true, onoff.StartUpToggle, nil, false, false},
		{"power-on reboot applies", onoff.FeatureLT, false, onoff.StartUpOn, &power, false, true},
		{"software update skips", onoff.FeatureLT, false, onoff.StartUpOn, &sw, false, false},
		{"no LT", 0, false, nil, nil, false, false},
		// OnOffServer.ts:37: not under an Aggregator.
		{"bridged skips On", onoff.FeatureLT, false, onoff.StartUpOn, nil, true, false},
		{"bridged skips Toggle", onoff.FeatureLT, true, onoff.StartUpToggle, nil, true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			initial := map[uint32]any{onoff.AttrOnOff: c.initial}
			if c.feat&onoff.FeatureLT != 0 {
				initial[onoff.AttrStartUpOnOff] = c.startUp
			}
			cfg := onoff.Config{Features: c.feat, Initial: initial, Clock: &fakeClock{}, Bridged: c.bridged}
			if c.boot != nil {
				b := *c.boot
				cfg.BootReason = func() uint8 { return b }
			}
			srv, err := onoff.New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if srv.OnOff() != c.want {
				t.Errorf("OnOff %t, want %t", srv.OnOff(), c.want)
			}
		})
	}
}

// TestCommands holds every command rule of OnOffServer.ts on an LT server
// as a table: the starting state, the command, the resulting state and
// whether the timed-on / delayed-off timers run afterwards.
func TestCommands(t *testing.T) {
	t.Parallel()
	type step struct {
		cmd    uint32
		fields any
	}
	cases := []struct {
		name       string
		start      lt
		setup      []step // commands run before the one under test
		cmd        step
		want       lt
		wantTimers int // pending timer callbacks after the command
	}{
		// on (:97-112)
		{"on sets OnOff and GSC", lt{}, nil, step{onoff.CmdOn, nil}, lt{OnOff: true, GSC: true}, 0},
		{"on with OnTime 0 clears OffWaitTime", lt{OffWaitTime: 30}, nil, step{onoff.CmdOn, nil}, lt{OnOff: true, GSC: true}, 0},
		{"on with OnTime > 0 keeps OffWaitTime", lt{OnTime: 50, OffWaitTime: 30}, nil, step{onoff.CmdOn, nil}, lt{OnOff: true, GSC: true, OnTime: 50, OffWaitTime: 30}, 0},
		{
			"on stops the delayed-off timer",
			lt{OnOff: true, OffWaitTime: 30},
			[]step{{onoff.CmdOff, nil}},
			step{onoff.CmdOn, nil},
			lt{OnOff: true, GSC: true},
			0,
		},
		// off (:119-139)
		{"off clears OnOff and OnTime", lt{OnOff: true, GSC: true, OnTime: 50}, nil, step{onoff.CmdOff, nil}, lt{GSC: true}, 0},
		{"off with OffWaitTime starts delayed off", lt{OnOff: true, OffWaitTime: 30}, nil, step{onoff.CmdOff, nil}, lt{OffWaitTime: 30}, 1},
		{"off with OffWaitTime 0xFFFF holds", lt{OnOff: true, OffWaitTime: 0xFFFF}, nil, step{onoff.CmdOff, nil}, lt{OffWaitTime: 0xFFFF}, 0},
		{
			"off stops the timed-on timer",
			lt{},
			[]step{{onoff.CmdOnWithTimedOff, timedOff(0, 50, 0)}},
			step{onoff.CmdOff, nil},
			lt{GSC: true},
			0,
		},
		// toggle (:146-152)
		{"toggle off→on", lt{}, nil, step{onoff.CmdToggle, nil}, lt{OnOff: true, GSC: true}, 0},
		{"toggle on→off", lt{OnOff: true, GSC: true}, nil, step{onoff.CmdToggle, nil}, lt{GSC: true}, 0},
		// offWithEffect (:158-169)
		{"offWithEffect clears GSC", lt{OnOff: true, GSC: true}, nil, step{onoff.CmdOffWithEffect, onoffdef.OffWithEffectRequest{}}, lt{}, 0},
		// onWithRecallGlobalScene (:171-191)
		{"recall with GSC true does nothing", lt{GSC: true, OffWaitTime: 30}, nil, step{onoff.CmdOnWithRecallGlobalScene, nil}, lt{GSC: true, OffWaitTime: 30}, 0},
		{"recall with GSC false turns on", lt{OffWaitTime: 30}, nil, step{onoff.CmdOnWithRecallGlobalScene, nil}, lt{OnOff: true, GSC: true}, 0},
		{"recall keeps OffWaitTime with OnTime", lt{OnTime: 5, OffWaitTime: 30}, nil, step{onoff.CmdOnWithRecallGlobalScene, nil}, lt{OnOff: true, GSC: true, OnTime: 5, OffWaitTime: 30}, 0},
		// onWithTimedOff (:198-225)
		{"timed off: AcceptOnlyWhenOn while off", lt{}, nil, step{onoff.CmdOnWithTimedOff, timedOff(onoffdef.OnOffControlAcceptOnlyWhenOn, 50, 20)}, lt{}, 0},
		{"timed off: AcceptOnlyWhenOn while on", lt{OnOff: true}, nil, step{onoff.CmdOnWithTimedOff, timedOff(onoffdef.OnOffControlAcceptOnlyWhenOn, 50, 20)}, lt{OnOff: true, GSC: true, OnTime: 50, OffWaitTime: 20}, 1},
		{"timed off: delayed off lowers OffWaitTime", lt{OffWaitTime: 30}, nil, step{onoff.CmdOnWithTimedOff, timedOff(0, 50, 10)}, lt{OffWaitTime: 10}, 1},
		{"timed off: delayed off keeps the lower OffWaitTime", lt{OffWaitTime: 30}, nil, step{onoff.CmdOnWithTimedOff, timedOff(0, 50, 40)}, lt{OffWaitTime: 30}, 1},
		{"timed off: delayed off lowered to 0", lt{OffWaitTime: 30}, nil, step{onoff.CmdOnWithTimedOff, timedOff(0, 50, 0)}, lt{}, 0},
		{"timed off: OnTime is the max", lt{OnOff: true, OnTime: 80}, nil, step{onoff.CmdOnWithTimedOff, timedOff(0, 50, 20)}, lt{OnOff: true, GSC: true, OnTime: 80, OffWaitTime: 20}, 1},
		{"timed off: starts the timer", lt{}, nil, step{onoff.CmdOnWithTimedOff, timedOff(0, 50, 20)}, lt{OnOff: true, GSC: true, OnTime: 50, OffWaitTime: 20}, 1},
		{"timed off: OnTime 0 arms nothing", lt{}, nil, step{onoff.CmdOnWithTimedOff, timedOff(0, 0, 20)}, lt{OnOff: true, GSC: true}, 0},
		{"timed off: OnTime 0xFFFF holds", lt{OnOff: true, OnTime: 0xFFFF}, nil, step{onoff.CmdOnWithTimedOff, timedOff(0, 50, 20)}, lt{OnOff: true, GSC: true, OnTime: 0xFFFF, OffWaitTime: 20}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, onoff.FeatureLT, map[uint32]any{
				onoff.AttrOnOff: c.start.OnOff, onoff.AttrGlobalSceneControl: c.start.GSC,
				onoff.AttrOnTime: c.start.OnTime, onoff.AttrOffWaitTime: c.start.OffWaitTime,
			})
			for _, s := range c.setup {
				f.mustInvoke(s.cmd, s.fields)
			}
			f.mustInvoke(c.cmd.cmd, c.cmd.fields)
			f.want(c.want)
			if n := f.clock.Pending(); n != c.wantTimers {
				t.Errorf("%d timers pending, want %d", n, c.wantTimers)
			}
		})
	}
}

// TestTimedOnCountdown: OnTime counts down every 100 ms; at 0 the server
// turns off and OffWaitTime is cleared (OnOffServer.ts:239-253), and the
// off goes through the sink.
func TestTimedOnCountdown(t *testing.T) {
	t.Parallel()
	f := newFixture(t, onoff.FeatureLT, nil)
	f.mustInvoke(onoff.CmdOnWithTimedOff, timedOff(0, 3, 20))
	f.clock.Advance(100 * time.Millisecond)
	f.want(lt{OnOff: true, GSC: true, OnTime: 2, OffWaitTime: 20})
	f.clock.Advance(200 * time.Millisecond)
	f.want(lt{GSC: true})
	if n := f.clock.Pending(); n != 0 {
		t.Errorf("%d timers pending after the countdown", n)
	}
	if got := f.sink.calls; len(got) != 2 || !got[0] || got[1] {
		t.Errorf("sink calls %v, want [true false]", got)
	}
}

// TestDelayedOffCountdown: OffWaitTime counts down every 100 ms and the
// timer stops at 0 (OnOffServer.ts:310-322).
func TestDelayedOffCountdown(t *testing.T) {
	t.Parallel()
	f := newFixture(t, onoff.FeatureLT, map[uint32]any{onoff.AttrOnOff: true, onoff.AttrOffWaitTime: uint16(2)})
	f.mustInvoke(onoff.CmdOff, nil)
	f.clock.Advance(100 * time.Millisecond)
	f.want(lt{OffWaitTime: 1})
	f.clock.Advance(time.Second)
	f.want(lt{})
	if n := f.clock.Pending(); n != 0 {
		t.Errorf("%d timers pending after the countdown", n)
	}
}

// TestHeldTimers: a write that parks the running countdown's attribute at
// 0 or 0xFFFF stops it; another value is picked up on the next tick; a
// write never starts a countdown (OnOffServer.ts:52-58, :71-84).
func TestHeldTimers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, v := range []uint64{0, 0xFFFF} {
		f := newFixture(t, onoff.FeatureLT, nil)
		f.mustInvoke(onoff.CmdOnWithTimedOff, timedOff(0, 50, 0))
		if err := f.srv.MatterWrite(ctx, onoff.AttrOnTime, v); err != nil {
			t.Fatal(err)
		}
		if n := f.clock.Pending(); n != 0 {
			t.Errorf("OnTime %#x: timed-on timer still runs", v)
		}

		f = newFixture(t, onoff.FeatureLT, map[uint32]any{onoff.AttrOnOff: true, onoff.AttrOffWaitTime: uint16(30)})
		f.mustInvoke(onoff.CmdOff, nil)
		if err := f.srv.MatterWrite(ctx, onoff.AttrOffWaitTime, v); err != nil {
			t.Fatal(err)
		}
		if n := f.clock.Pending(); n != 0 {
			t.Errorf("OffWaitTime %#x: delayed-off timer still runs", v)
		}
	}

	f := newFixture(t, onoff.FeatureLT, nil)
	f.mustInvoke(onoff.CmdOnWithTimedOff, timedOff(0, 50, 0))
	if err := f.srv.MatterWrite(ctx, onoff.AttrOnTime, uint64(5)); err != nil {
		t.Fatal(err)
	}
	f.clock.Advance(100 * time.Millisecond)
	if got := f.state().OnTime; got != 4 {
		t.Errorf("OnTime %d after a write of 5 and one tick, want 4", got)
	}

	idle := newFixture(t, onoff.FeatureLT, map[uint32]any{onoff.AttrOnOff: true})
	if err := idle.srv.MatterWrite(ctx, onoff.AttrOnTime, uint64(50)); err != nil {
		t.Fatal(err)
	}
	if err := idle.srv.MatterWrite(ctx, onoff.AttrOffWaitTime, uint64(50)); err != nil {
		t.Fatal(err)
	}
	if n := idle.clock.Pending(); n != 0 {
		t.Errorf("a write started %d timers", n)
	}
}

// TestSinkRefusal: a refused change answers FAILURE and stores nothing.
func TestSinkRefusal(t *testing.T) {
	t.Parallel()
	f := newFixture(t, onoff.FeatureLT, map[uint32]any{onoff.AttrOffWaitTime: uint16(30)})
	f.sink.refuse = true
	err := f.invoke(onoff.CmdOnWithTimedOff, timedOff(0, 0, 0))
	if err != nil {
		t.Fatalf("delayed-off branch does not turn on: %v", err)
	}
	err = f.invoke(onoff.CmdOn, nil)
	if statusOf(err) != im.StatusFailure {
		t.Fatalf("refused On answered %v, want FAILURE", err)
	}
	f.want(lt{OffWaitTime: 0})

	g := newFixture(t, onoff.FeatureLT, nil)
	g.sink.refuse = true
	if err := g.invoke(onoff.CmdOnWithTimedOff, timedOff(0, 50, 20)); statusOf(err) != im.StatusFailure {
		t.Fatalf("refused OnWithTimedOff answered %v", err)
	}
	g.want(lt{})
	if n := g.clock.Pending(); n != 0 {
		t.Errorf("a refused command armed %d timers", n)
	}
}

// TestSinkRefusesTimedOff: the countdown's off is refused — the timer
// stops and the state stays.
func TestSinkRefusesTimedOff(t *testing.T) {
	t.Parallel()
	f := newFixture(t, onoff.FeatureLT, nil)
	f.mustInvoke(onoff.CmdOnWithTimedOff, timedOff(0, 1, 0))
	f.sink.refuse = true
	f.clock.Advance(time.Second)
	if !f.srv.OnOff() {
		t.Error("refused off was stored")
	}
	if n := f.clock.Pending(); n != 0 {
		t.Errorf("%d timers pending", n)
	}
}

// TestFeatureGatedCommands: OFFONLY disables On and Toggle, a server
// without LT the three LT commands (OnOffServer.ts:26, conformance).
func TestFeatureGatedCommands(t *testing.T) {
	t.Parallel()
	off := newFixture(t, onoff.FeatureOFFONLY, map[uint32]any{onoff.AttrOnOff: true})
	for _, cmd := range []uint32{onoff.CmdOn, onoff.CmdToggle, onoff.CmdOnWithTimedOff} {
		if err := off.invoke(cmd, nil); statusOf(err) != im.StatusUnsupportedCommand {
			t.Errorf("OFFONLY command 0x%02X: %v", cmd, err)
		}
	}
	off.mustInvoke(onoff.CmdOff, nil)
	if off.srv.OnOff() {
		t.Error("OFFONLY Off did not turn off")
	}

	plain := newFixture(t, 0, nil)
	for _, cmd := range []uint32{onoff.CmdOffWithEffect, onoff.CmdOnWithRecallGlobalScene, onoff.CmdOnWithTimedOff} {
		if err := plain.invoke(cmd, nil); statusOf(err) != im.StatusUnsupportedCommand {
			t.Errorf("non-LT command 0x%02X: %v", cmd, err)
		}
	}
	plain.mustInvoke(onoff.CmdToggle, nil)
	if !plain.srv.OnOff() {
		t.Error("Toggle did not turn on")
	}
	if _, ok := plain.srv.MatterRead(onoff.AttrGlobalSceneControl); ok {
		t.Error("GlobalSceneControl served without LT")
	}
	if _, err := onoff.New(onoff.Config{Features: onoff.FeatureLT | onoff.FeatureOFFONLY}); !errors.Is(err, onoff.ErrInvalidConfig) {
		t.Errorf("LT | OFFONLY accepted: %v", err)
	}
}

// TestQuiesce stops both timers without a report; a later command starts
// them again.
func TestQuiesce(t *testing.T) {
	t.Parallel()
	f := newFixture(t, onoff.FeatureLT, nil)
	f.mustInvoke(onoff.CmdOnWithTimedOff, timedOff(0, 50, 0))
	v := f.srv.MatterDataVersion()
	f.srv.MatterQuiesce()
	f.clock.Advance(time.Second)
	if f.srv.MatterDataVersion() != v || f.state().OnTime != 50 {
		t.Error("a quiesced timer still ticked")
	}
	f.srv.MatterQuiesce() // idempotent
	f.mustInvoke(onoff.CmdOnWithTimedOff, timedOff(0, 50, 0))
	f.clock.Advance(100 * time.Millisecond)
	if got := f.state().OnTime; got != 49 {
		t.Errorf("OnTime %d after restart and one tick", got)
	}
}

// TestHostSet: Set stores and reports the value without the sink and
// without timer logic.
func TestHostSet(t *testing.T) {
	t.Parallel()
	f := newFixture(t, onoff.FeatureLT, map[uint32]any{onoff.AttrOffWaitTime: uint16(30)})
	var changed []uint32
	f.srv.OnMatterAttributesChanged(func(ids []uint32) { changed = append(changed, ids...) })
	f.srv.Set(true)
	if !f.srv.OnOff() || len(f.sink.calls) != 0 {
		t.Errorf("OnOff %t, sink %v", f.srv.OnOff(), f.sink.calls)
	}
	if len(changed) != 1 || changed[0] != onoff.AttrOnOff {
		t.Errorf("changed %v", changed)
	}
	f.want(lt{OnOff: true, OffWaitTime: 30})
}

// TestInitialRejected: an initial value outside the model is refused.
func TestInitialRejected(t *testing.T) {
	t.Parallel()
	if _, err := onoff.New(onoff.Config{Features: onoff.FeatureLT, Initial: map[uint32]any{onoff.AttrStartUpOnOff: uint8(7)}}); !errors.Is(err, onoff.ErrInvalidConfig) {
		t.Errorf("StartUpOnOff 7 accepted: %v", err)
	}
	if _, err := onoff.New(onoff.Config{Initial: map[uint32]any{onoff.AttrOnTime: uint16(1)}}); !errors.Is(err, onoff.ErrInvalidConfig) {
		t.Errorf("OnTime without LT accepted: %v", err)
	}
}

// TestWallClock exercises the default clock: a one-tick countdown ends.
func TestWallClock(t *testing.T) {
	t.Parallel()
	srv, err := onoff.New(onoff.Config{Features: onoff.FeatureLT})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.MatterInvoke(context.Background(), onoff.CmdOnWithTimedOff, timedOff(0, 1, 0)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for srv.OnOff() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if srv.OnOff() {
		t.Error("wall-clock countdown did not turn off")
	}
}

// TestOnWithTimedOffPayload: a payload of another type is INVALID_COMMAND.
func TestOnWithTimedOffPayload(t *testing.T) {
	t.Parallel()
	f := newFixture(t, onoff.FeatureLT, nil)
	if err := f.invoke(onoff.CmdOnWithTimedOff, nil); statusOf(err) != im.StatusInvalidCommand {
		t.Errorf("nil payload: %v", err)
	}
}

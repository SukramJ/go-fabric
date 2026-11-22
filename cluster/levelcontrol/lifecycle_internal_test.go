// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package levelcontrol

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/clock/clocktest"
	"thingfabric.dev/go-fabric/cluster/spec"
	lvldef "thingfabric.dev/go-fabric/cluster/spec/levelcontrol"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/tlv"
)

// lcRamp is a device that ramps and never reports arriving; it counts its
// notifier's subscriptions and their releases.
type lcRamp struct {
	subs, unsubs atomic.Int32
}

func (*lcRamp) Level() (uint8, bool)                                   { return 100, true }
func (*lcRamp) Ramp(context.Context, uint8, time.Duration, bool) error { return nil }
func (*lcRamp) Stop(context.Context) error                             { return nil }

func (d *lcRamp) OnMatterValueChanged(func()) func() {
	d.subs.Add(1)
	var once sync.Once
	return func() { once.Do(func() { d.unsubs.Add(1) }) }
}

// lcOnOff is an On/Off cluster that counts its switch-offs.
type lcOnOff struct {
	on   atomic.Bool
	offs atomic.Int32
}

func (o *lcOnOff) OnOff() bool { return o.on.Load() }

func (o *lcOnOff) SetOnOff(_ context.Context, on bool) error {
	o.on.Store(on)
	if !on {
		o.offs.Add(1)
	}
	return nil
}

// TestNativeSourceQuiesceDropsTheOwedSwitchOff holds R2-P6-fsm-1406: on
// the hand-off path, a "with On/Off" ramp to the minimum arms a deadline
// that switches the device off; MatterQuiesce drops it, so no timer of the
// server outlives it, and releases the device's notifier the source
// subscribed to. A later ramp subscribes again: the server stays usable.
func TestNativeSourceQuiesceDropsTheOwedSwitchOff(t *testing.T) {
	fc := clocktest.New(time.Unix(0, 0))
	dev, oo := &lcRamp{}, &lcOnOff{}
	oo.on.Store(true)
	src := NewNativeSource(dev, oo, NativeConfig{Clock: fc})
	srv := NewServer(Config{Source: src})
	ramp := func() {
		t.Helper()
		if _, err := srv.MatterInvoke(context.Background(), lvldef.CmdMoveToLevelWithOnOff,
			lvldef.MoveToLevelWithOnOffRequest{Level: 0, TransitionTime: spec.ValueOf(uint16(10))}); err != nil {
			t.Fatal(err)
		}
	}
	ramp()
	if fc.Pending() == 0 {
		t.Fatal("no switch-off deadline armed; the test proves nothing")
	}
	srv.MatterQuiesce()
	fc.Advance(2 * time.Second)
	if n := oo.offs.Load(); n != 0 {
		t.Errorf("the owed switch-off ran %d time(s) after MatterQuiesce, want 0", n)
	}
	if n := fc.Pending(); n != 0 {
		t.Errorf("%d timer(s) armed after MatterQuiesce, want 0", n)
	}
	if s, u := dev.subs.Load(), dev.unsubs.Load(); s != u {
		t.Errorf("after MatterQuiesce %d subscription(s), %d released; the source keeps the device's notifier", s, u)
	}
	ramp()
	fc.Advance(2 * time.Second)
	if n := oo.offs.Load(); n != 1 {
		t.Errorf("a ramp after quiesce switched off %d time(s), want 1", n)
	}
	if dev.subs.Load() == dev.unsubs.Load() {
		t.Error("a ramp after quiesce did not subscribe to the device's notifier again")
	}
}

// lcLevel is a LevelSource whose first CurrentLevel read blocks until
// release is closed and answers the level of that moment's past.
type lcLevel struct {
	lvldef.LevelSource
	calls   atomic.Int32
	entered chan struct{}
	release chan struct{}
	level   atomic.Uint32
}

func (l *lcLevel) CurrentLevel() (uint8, bool) {
	if l.calls.Add(1) == 1 {
		old := uint8(l.level.Load()) //nolint:gosec // a level
		close(l.entered)
		<-l.release
		return old, true
	}
	return uint8(l.level.Load()), true //nolint:gosec // a level
}

// TestObserveLevelKeepsTheNewestReading holds R2-P6-conc-1408: two
// observers of CurrentLevel — the host's notifier and a command — read and
// store as one step, so an older reading is never stored after a newer
// one.
func TestObserveLevelKeepsTheNewestReading(t *testing.T) {
	src := &lcLevel{entered: make(chan struct{}), release: make(chan struct{})}
	src.level.Store(10)
	s := newServer(sourceOf(src), nil, featureMap)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); s.observeLevel(contract.OriginHostSet) }()
	<-src.entered
	src.level.Store(20)
	second := make(chan struct{})
	go func() { defer wg.Done(); close(second); s.observeLevel(contract.OriginHostSet) }()
	<-second
	// Give the second observer the chance to read and store while the
	// first is inside its read; serialised, it cannot get past the first.
	for i := 0; i < 10000 && src.calls.Load() < 2; i++ {
		runtime.Gosched()
	}
	close(src.release)
	wg.Wait()
	s.mu.Lock()
	level := s.level
	s.mu.Unlock()
	if level != 20 {
		t.Errorf("CurrentLevel stored %d after both observers, want the newest 20", level)
	}
}

// TestSupersededTransitionKeepsTheNewCoupling holds R2-P6-conc-1409: the
// coupling a step of a replaced transition records does not overwrite the
// one the newer command recorded.
func TestSupersededTransitionKeepsTheNewCoupling(t *testing.T) {
	oo := &lcOnOff{}
	oo.on.Store(true)
	s := newServer(sourceOf(&lcLevel{entered: make(chan struct{}), release: make(chan struct{})}), nil, featureMap)
	s.onOff = oo
	old := s.nextCoupling()
	if _, err := s.couple(context.Background(), old, true, 0, 0); err != nil {
		t.Fatal(err)
	}
	newer := s.nextCoupling()
	if _, err := s.couple(context.Background(), newer, true, 0, 200); err != nil {
		t.Fatal(err)
	}
	// A step of the replaced transition couples again.
	if _, err := s.couple(context.Background(), old, true, 0, 0); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	c := s.coupling
	s.mu.Unlock()
	if c.turnsOff {
		t.Error("the replaced transition's step restored its coupling (turnsOff) over the newer command's")
	}
}

// lcPort is a LevelSource whose SetOnLevel fails.
type lcPort struct {
	lvldef.LevelSource
	options atomic.Uint32
}

func (p *lcPort) CurrentLevel() (uint8, bool)              { return 10, true }
func (p *lcPort) Options() lvldef.OptionsBitmap            { return lvldef.OptionsBitmap(p.options.Load()) } //nolint:gosec // a map8
func (p *lcPort) OnLevel() (uint8, bool)                   { return 0, false }
func (p *lcPort) SetOnLevel(context.Context, *uint8) error { return errors.New("port refused") }

func (p *lcPort) SetOptions(_ context.Context, o lvldef.OptionsBitmap) error {
	p.options.Store(uint32(o))
	return nil
}

// TestSetAttributesAllOrNothingOverAPort holds R2-P6-api-1410: a set whose
// OnLevel the port refuses leaves Options as it was — the set is all or
// nothing, as its documentation says.
func TestSetAttributesAllOrNothingOverAPort(t *testing.T) {
	port := &lcPort{}
	s := NewServer(Config{Source: port})
	var reports atomic.Int32
	s.OnAttributeChange(func(contract.Change) { reports.Add(1) })
	err := s.SetAttributes(map[uint32]any{AttrOptions: OptionExecuteIfOff, AttrOnLevel: uint8(5)})
	if err == nil {
		t.Fatal("the refused set succeeded")
	}
	if o := port.options.Load(); o != 0 {
		t.Errorf("Options = %d after a refused set, want 0 (all or nothing)", o)
	}
	if n := reports.Load(); n != 0 {
		t.Errorf("%d change report(s) for a refused set, want 0", n)
	}
}

// TestRawPayloadKeepsTheDecoderStatus holds R2-P6-wire-1416 over the four
// raw-payload decoders: a field the decoder refuses with a status of its
// own answers that status (CONSTRAINT_ERROR for a value above the field's
// width), never INVALID_COMMAND in its place.
func TestRawPayloadKeepsTheDecoderStatus(t *testing.T) {
	e := tlv.NewEncoder()
	e.StartStruct(tlv.AnonymousTag())
	e.PutUint(tlv.ContextTag(0), 1000)
	if err := e.EndContainer(); err != nil {
		t.Fatal(err)
	}
	payload, err := e.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	decoders := map[string]func() error{
		"MoveToLevel": func() error { _, err := moveToLevelRequestFrom("MoveToLevel", payload); return err },
		"Move":        func() error { _, err := moveRequestFrom("Move", payload); return err },
		"Step":        func() error { _, err := stepRequestFrom("Step", payload); return err },
		"Stop":        func() error { _, err := stopRequestFrom("Stop", payload); return err },
	}
	for name, decode := range decoders {
		var st im.StatusCodeError
		if err := decode(); !errors.As(err, &st) || st.MatterStatusCode() != im.StatusConstraintError {
			t.Errorf("%s with field 0 = 1000: %v, want CONSTRAINT_ERROR", name, err)
		}
	}
}

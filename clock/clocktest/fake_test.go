// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package clocktest_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/clock/clocktest"
)

var epoch = time.Date(2025, 1, 1, 12, 34, 56, 0, time.UTC)

func TestAdvanceFiresInTimeOrderAndAwaitsEach(t *testing.T) {
	t.Parallel()
	f := clocktest.New(epoch)
	var (
		mu    sync.Mutex
		order []string
		at    []time.Duration
	)
	record := func(name string) func() {
		return func() {
			mu.Lock()
			defer mu.Unlock()
			order = append(order, name)
			at = append(at, f.Now().Sub(epoch))
		}
	}
	f.AfterFunc(3*time.Second, record("c"))
	f.AfterFunc(time.Second, record("a"))
	f.AfterFunc(2*time.Second, record("b1"))
	f.AfterFunc(2*time.Second, record("b2")) // same instant: arming order
	f.AfterFunc(5*time.Second, record("late"))

	f.Advance(3 * time.Second)
	if want := []string{"a", "b1", "b2", "c"}; !slices.Equal(order, want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	if want := []time.Duration{time.Second, 2 * time.Second, 2 * time.Second, 3 * time.Second}; !slices.Equal(at, want) {
		t.Fatalf("callbacks read %v, want each its own deadline %v", at, want)
	}
	if got := f.Now().Sub(epoch); got != 3*time.Second {
		t.Fatalf("Now after Advance = +%v, want +3s", got)
	}
	if got := f.Pending(); got != 1 {
		t.Fatalf("Pending = %d, want 1 (the late timer)", got)
	}
	if got := f.Armed(); !slices.Equal(got, []time.Duration{2 * time.Second}) {
		t.Fatalf("Armed = %v, want [2s] left on the late timer", got)
	}
	if at, ok := f.Next(); !ok || !at.Equal(epoch.Add(5*time.Second)) {
		t.Fatalf("Next = %v %v, want +5s", at, ok)
	}
}

func TestAdvanceRunsTimersArmedByCallbacksInTheInterval(t *testing.T) {
	t.Parallel()
	f := clocktest.New(epoch)
	var fired atomic.Int32
	f.AfterFunc(time.Second, func() {
		fired.Add(1)
		f.AfterFunc(time.Second, func() { fired.Add(1) }) // due at +2s
		f.AfterFunc(5*time.Second, func() { fired.Add(1) })
	})
	f.Advance(2 * time.Second)
	if got := fired.Load(); got != 2 {
		t.Fatalf("fired = %d, want 2 (the nested timer due inside the interval)", got)
	}
	if got := f.Pending(); got != 1 {
		t.Fatalf("Pending = %d, want 1", got)
	}
}

func TestAfterFuncRunsInItsOwnGoroutine(t *testing.T) {
	t.Parallel()
	f := clocktest.New(epoch)
	var mu sync.Mutex
	mu.Lock() // held by the test goroutine; the callback must not run on it
	ran := make(chan struct{})
	f.AfterFunc(time.Millisecond, func() {
		mu.Lock()
		defer mu.Unlock()
		close(ran)
	})
	go func() {
		time.Sleep(10 * time.Millisecond)
		mu.Unlock()
	}()
	f.Advance(time.Millisecond) // returns only once the callback returned
	select {
	case <-ran:
	default:
		t.Fatal("Advance returned before the callback did")
	}
}

func TestTimerStopAndResetLikeTheStandardLibrary(t *testing.T) {
	t.Parallel()
	f := clocktest.New(epoch)
	var fired atomic.Int32
	tm := f.AfterFunc(time.Second, func() { fired.Add(1) })
	if !tm.Stop() {
		t.Fatal("Stop on an armed timer = false, want true")
	}
	if tm.Stop() {
		t.Fatal("second Stop = true, want false")
	}
	f.Advance(2 * time.Second)
	if fired.Load() != 0 {
		t.Fatal("a stopped timer fired")
	}
	if tm.Reset(time.Second) {
		t.Fatal("Reset of a stopped timer = true, want false")
	}
	f.Advance(time.Second)
	if fired.Load() != 1 {
		t.Fatal("a reset timer did not fire")
	}
	if tm.Stop() {
		t.Fatal("Stop after firing = true, want false")
	}
	if tm.Reset(time.Second) {
		t.Fatal("Reset after firing = true, want false")
	}
	if !tm.Reset(3 * time.Second) {
		t.Fatal("Reset of an armed timer = false, want true")
	}
	f.Advance(2 * time.Second)
	if fired.Load() != 1 {
		t.Fatal("Reset did not move the deadline")
	}
	f.Advance(time.Second)
	if fired.Load() != 2 {
		t.Fatal("the re-armed timer did not fire at its new deadline")
	}
	if tm.C() != nil {
		t.Fatal("an AfterFunc timer has a channel")
	}
}

func TestChannelTimerDeliversAndStopDrains(t *testing.T) {
	t.Parallel()
	f := clocktest.New(epoch)
	tm := f.NewTimer(time.Second)
	select {
	case <-tm.C():
		t.Fatal("timer delivered before its deadline")
	default:
	}
	f.Advance(time.Second)
	select {
	case got := <-tm.C():
		if !got.Equal(epoch.Add(time.Second)) {
			t.Fatalf("delivered %v, want the deadline", got)
		}
	default:
		t.Fatal("timer did not deliver at its deadline")
	}

	tm.Reset(time.Second)
	f.Advance(time.Second)
	if tm.Stop() {
		t.Fatal("Stop after firing = true")
	}
	select {
	case <-tm.C():
		t.Fatal("a stale value survived Stop")
	default:
	}
}

func TestTickerTicksEveryPeriodAndDropsForASlowReceiver(t *testing.T) {
	t.Parallel()
	f := clocktest.New(epoch)
	tk := f.NewTicker(time.Second)
	defer tk.Stop()
	f.Advance(time.Second)
	if got := <-tk.C(); !got.Equal(epoch.Add(time.Second)) {
		t.Fatalf("first tick %v, want +1s", got)
	}
	f.Advance(3 * time.Second) // three ticks, one buffered
	if got := <-tk.C(); !got.Equal(epoch.Add(2 * time.Second)) {
		t.Fatalf("buffered tick %v, want +2s (later ones dropped)", got)
	}
	select {
	case <-tk.C():
		t.Fatal("a slow receiver received a queued tick")
	default:
	}
	if f.Pending() != 1 {
		t.Fatalf("Pending = %d, want 1 (the running ticker)", f.Pending())
	}
	tk.Reset(500 * time.Millisecond)
	f.Advance(500 * time.Millisecond)
	if got := <-tk.C(); !got.Equal(epoch.Add(4500 * time.Millisecond)) {
		t.Fatalf("tick after Reset %v, want +4.5s", got)
	}
	tk.Stop()
	f.Advance(time.Hour)
	select {
	case <-tk.C():
		t.Fatal("a stopped ticker ticked")
	default:
	}
	if f.Pending() != 0 {
		t.Fatalf("Pending = %d after Stop, want 0", f.Pending())
	}
}

func TestSetStepsWithoutFiring(t *testing.T) {
	t.Parallel()
	f := clocktest.New(epoch)
	var fired atomic.Bool
	f.AfterFunc(time.Second, func() { fired.Store(true) })
	f.Set(epoch.Add(time.Hour))
	if fired.Load() {
		t.Fatal("Set fired a timer")
	}
	if !f.Now().Equal(epoch.Add(time.Hour)) {
		t.Fatal("Set did not move Now")
	}
	f.Advance(0)
	if !fired.Load() {
		t.Fatal("an overdue timer did not fire on Advance(0)")
	}
	if !f.Now().Equal(epoch.Add(time.Hour)) {
		t.Fatal("firing an overdue timer moved the clock back")
	}
}

func TestResolveAdvancesUntilWaitReturns(t *testing.T) {
	t.Parallel()
	f := clocktest.New(epoch)
	got, err := resolveSleep(t, f, 90*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Fatal("the sleep did not complete")
	}
	if elapsed := f.Now().Sub(epoch); elapsed != 90*time.Second {
		t.Fatalf("Resolve moved %v, want exactly the 90s the work waited", elapsed)
	}
}

func resolveSleep(t *testing.T, f *clocktest.Fake, d time.Duration) (bool, error) {
	t.Helper()
	var full bool
	err := f.Resolve(context.Background(), func() error {
		full = clock.Sleep(f, d, nil)
		return nil
	})
	return full, err
}

func TestResolveReturnsWaitsErrorAndHonoursContext(t *testing.T) {
	t.Parallel()
	f := clocktest.New(epoch)
	want := errors.New("boom")
	if err := f.Resolve(context.Background(), func() error { return want }); !errors.Is(err, want) {
		t.Fatalf("Resolve = %v, want wait's error", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	block := make(chan struct{})
	defer close(block)
	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()
	if err := f.Resolve(ctx, func() error { <-block; return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("Resolve with nothing armed and a blocked wait = %v, want context.Canceled", err)
	}
}

func TestFakeIsGoroutineSafe(t *testing.T) {
	t.Parallel()
	f := clocktest.New(epoch)
	var fired atomic.Int64
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			for j := range 50 {
				tm := f.AfterFunc(time.Duration(i*j)*time.Millisecond, func() { fired.Add(1) })
				if j%3 == 0 {
					tm.Stop()
				}
				_ = f.Now()
				_ = f.Pending()
			}
		})
	}
	wg.Go(func() {
		for range 50 {
			f.Advance(10 * time.Millisecond)
		}
	})
	wg.Wait()
	f.Advance(time.Hour)
	if f.Pending() != 0 {
		t.Fatalf("Pending = %d after advancing past every deadline", f.Pending())
	}
	if fired.Load() == 0 {
		t.Fatal("no timer fired")
	}
}

func TestSystemIsTheStandardLibrary(t *testing.T) {
	t.Parallel()
	var c clock.Clock = clock.System{}
	before := time.Now()
	if now := c.Now(); now.Before(before) {
		t.Fatal("System.Now is behind time.Now")
	}
	done := make(chan struct{})
	c.AfterFunc(time.Millisecond, func() { close(done) })
	<-done
	tm := c.NewTimer(time.Millisecond)
	<-tm.C()
	tk := c.NewTicker(time.Millisecond)
	<-tk.C()
	tk.Stop()
	if clock.Or(nil) != (clock.System{}) {
		t.Fatal("Or(nil) is not System")
	}
	f := clocktest.New(epoch)
	if clock.Or(f) != f {
		t.Fatal("Or(c) is not c")
	}
	if clock.Since(f, epoch.Add(-time.Second)) != time.Second || clock.Until(f, epoch.Add(time.Second)) != time.Second {
		t.Fatal("Since/Until do not read the clock")
	}
}

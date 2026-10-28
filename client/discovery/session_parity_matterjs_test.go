// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package discovery_test

// Ports of matter.js at 862ad75f, by meaning, of
// packages/node/test/behavior/system/controller/discovery/:
// DiscoveryCancelTest.ts, DiscoveryErrorTest.ts, DiscoveryTransportsTest.ts,
// ParallelPaseRaceTest.ts and ContinuousDiscoveryTest.ts, on the helpers of
// discovery-helpers.ts: a fixed transport that reports its devices and
// scans until the discovery stops (FixedScanner), one that fails on demand
// (FailingScanner) and a race whose attempts the test supplies
// (TestDiscovery). The attempts block on channels instead of MockTime; the
// discovery's timeout and stagger run on a fake clock.

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/clock/clocktest"
)

// fixedTransport is FixedScanner wrapped in FailingScanner
// (discovery-helpers.ts:28-96).
type fixedTransport struct {
	kind        discovery.TransportKind
	devices     []discovery.Device
	afterReport func()
	// afterStop runs once the discovery stopped the scan.
	afterStop func()
	// beforeReport runs before the devices are reported.
	beforeReport func()
	ignoresStop  bool
	endsAtOnce   bool
	failCh       chan error
	once         sync.Once
	ids          chan discovery.Identifier
}

func newFixed(devs ...discovery.Device) *fixedTransport {
	return &fixedTransport{kind: discovery.TransportIP, devices: devs, failCh: make(chan error, 1), ids: make(chan discovery.Identifier, 4)}
}

func (f *fixedTransport) fail(err error) { f.once.Do(func() { f.failCh <- err }) }

func (f *fixedTransport) Kind() discovery.TransportKind { return f.kind }

func (f *fixedTransport) FindContinuously(ctx context.Context, id discovery.Identifier, found func(discovery.Device)) error {
	f.ids <- id
	if f.beforeReport != nil {
		f.beforeReport()
	}
	for _, d := range f.devices {
		found(d)
	}
	if f.afterReport != nil {
		f.afterReport()
	}
	if f.endsAtOnce {
		select {
		case err := <-f.failCh:
			return err
		default:
			return nil
		}
	}
	done := ctx.Done()
	if f.ignoresStop {
		done = nil
	}
	select {
	case <-done:
		if f.afterStop != nil {
			f.afterStop()
		}
		return nil
	case err := <-f.failCh:
		return err
	}
}

var (
	deviceA = discovery.Device{InstanceID: "device-a", D: 1000, CM: 1}
	deviceB = discovery.Device{InstanceID: "device-b", D: 1000, CM: 1}
	deviceC = discovery.Device{InstanceID: "device-c", D: 1000, CM: 1}
)

type testRace struct {
	discovered, started atomic.Int32
	session             *discovery.Session[discovery.Candidate]
}

// race is TestDiscovery (:98-146): the winner's result is its candidate.
func race(ctx context.Context, clk *clocktest.Fake, opts discovery.SessionOptions, attempt func(ctx context.Context, win func() bool, index int) error, transports ...discovery.Transport) *testRace {
	r := &testRace{}
	opts.Clock = clk
	var index atomic.Int32
	opts.Prepare = func(context.Context, discovery.Candidate) error { r.discovered.Add(1); return nil }
	r.session = discovery.Race(ctx, transports, opts, func(ctx context.Context, c discovery.Candidate, win func() bool) (discovery.Candidate, error) {
		r.started.Add(1)
		return c, attempt(ctx, win, int(index.Add(1)-1))
	})
	return r
}

func newClock() *clocktest.Fake { return clocktest.New(time.Unix(1_000_000, 0)) }

// outcome waits for the session's outcome, advancing the fake clock by
// step meanwhile so timeouts and staggers pass.
func outcome[T any](t *testing.T, s *discovery.Session[T], clk *clocktest.Fake) (T, error) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		select {
		case <-s.Done():
			return s.Wait(context.Background())
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("the discovery did not end")
		}
		clk.Advance(100 * time.Millisecond)
		time.Sleep(time.Millisecond)
	}
}

// winner is :60-68: wins at once, runs afterWin, then commissions until
// commissioned is closed (nil: at once).
func winner(afterWin func(), commissioned <-chan struct{}) func(context.Context, func() bool, int) error {
	return func(_ context.Context, win func() bool, _ int) error {
		if !win() {
			return errors.New("lost")
		}
		if afterWin != nil {
			afterWin()
		}
		if commissioned != nil {
			<-commissioned
		}
		return nil
	}
}

// lateWinner is :45-58: an attempt that does not notice the abort; its
// PASE completes when pase is closed and it then asks to win.
type lateWinner struct {
	started, pase chan struct{}
	won           chan bool
}

func newLateWinner() *lateWinner {
	return &lateWinner{started: make(chan struct{}), pase: make(chan struct{}), won: make(chan bool, 1)}
}

func (l *lateWinner) attempt(_ context.Context, win func() bool, _ int) error {
	close(l.started)
	<-l.pase
	l.won <- win()
	return nil
}

func untilAborted(ctx context.Context, _ func() bool, _ int) error {
	<-ctx.Done()
	return ctx.Err()
}

func recv[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
		var zero T
		return zero
	}
}

func TestDiscoveryCancelParity(t *testing.T) {
	t.Parallel()
	reason := errors.New("caller gave up")
	t.Run("refuses a PASE win after a cancel that came first, before the scan has ended", func(t *testing.T) {
		t.Parallel()
		clk := newClock()
		tr := newFixed(deviceA)
		tr.ignoresStop = true
		l := newLateWinner()
		r := race(context.Background(), clk, discovery.SessionOptions{Timeout: 30 * time.Second}, l.attempt, tr)
		recv(t, l.started)
		r.session.Cancel(reason)
		close(l.pase)
		if recv(t, l.won) {
			t.Fatal("the late attempt won after the cancel")
		}
		tr.fail(errors.New("scan ends"))
		if _, err := outcome(t, r.session, clk); !errors.Is(err, reason) {
			t.Fatalf("err %v, want the cancel reason", err)
		}
	})
	t.Run("refuses a PASE win after the discovery timeout, before the scan has ended", func(t *testing.T) {
		t.Parallel()
		clk := newClock()
		tr := newFixed(deviceA)
		tr.ignoresStop = true
		l := newLateWinner()
		r := race(context.Background(), clk, discovery.SessionOptions{Timeout: time.Second}, l.attempt, tr)
		recv(t, l.started)
		clk.Advance(time.Second)
		close(l.pase)
		if recv(t, l.won) {
			t.Fatal("the late attempt won after the timeout")
		}
		tr.fail(errors.New("scan ends"))
		if _, err := outcome(t, r.session, clk); err == nil {
			t.Fatal("no error")
		}
	})
	t.Run("honours the caller's abort signal like a cancel", func(t *testing.T) {
		t.Parallel()
		clk := newClock()
		tr := newFixed(deviceA)
		tr.ignoresStop = true
		l := newLateWinner()
		ctx, cancel := context.WithCancel(context.Background())
		r := race(ctx, clk, discovery.SessionOptions{Timeout: 30 * time.Second}, l.attempt, tr)
		recv(t, l.started)
		cancel()
		close(l.pase)
		if recv(t, l.won) {
			t.Fatal("the late attempt won after the abort")
		}
		tr.fail(errors.New("scan ends"))
		if _, err := outcome(t, r.session, clk); !errors.Is(err, context.Canceled) {
			t.Fatalf("err %v, want context.Canceled", err)
		}
	})
	t.Run("cancels at once with a caller abort signal that is already aborted", func(t *testing.T) {
		t.Parallel()
		clk := newClock()
		ctx, cancel := context.WithCancelCause(context.Background())
		cancel(reason)
		r := race(ctx, clk, discovery.SessionOptions{Timeout: 30 * time.Second}, winner(nil, nil), newFixed(deviceA))
		if _, err := outcome(t, r.session, clk); !errors.Is(err, reason) {
			t.Fatalf("err %v, want the abort reason", err)
		}
		if n := r.started.Load(); n != 0 {
			t.Fatalf("%d attempts started", n)
		}
	})
	t.Run("does not start an attempt for a device that is handed over after the cancel", func(t *testing.T) {
		t.Parallel()
		clk := newClock()
		tr := newFixed(deviceA)
		var r *testRace
		ready := make(chan struct{})
		tr.beforeReport = func() {
			<-ready
			r.session.Cancel(reason)
		}
		r = race(context.Background(), clk, discovery.SessionOptions{Timeout: 30 * time.Second}, winner(nil, nil), tr)
		close(ready)
		if _, err := outcome(t, r.session, clk); !errors.Is(err, reason) {
			t.Fatalf("err %v, want the cancel reason", err)
		}
		if d, n := r.discovered.Load(), r.started.Load(); d != 1 || n != 0 {
			t.Fatalf("%d discovered, %d started; want 1 and 0", d, n)
		}
	})
	t.Run("rejects with the cancel reason when an instance discovery is canceled before it finds a node", func(t *testing.T) {
		t.Parallel()
		clk := newClock()
		s := discovery.FindInstance(context.Background(), []discovery.Transport{newFixed()}, discovery.SessionOptions{Timeout: 30 * time.Second, Clock: clk})
		clk.Advance(time.Second)
		s.Cancel(reason)
		if _, err := outcome(t, s, clk); !errors.Is(err, reason) {
			t.Fatalf("err %v, want the cancel reason", err)
		}
	})
	t.Run("rejects with the cancel reason when a continuous discovery is canceled", func(t *testing.T) {
		t.Parallel()
		clk := newClock()
		s := discovery.FindAll(context.Background(), []discovery.Transport{newFixed(deviceA)}, discovery.SessionOptions{Clock: clk}, nil)
		clk.Advance(time.Second)
		s.Cancel(reason)
		if _, err := outcome(t, s, clk); !errors.Is(err, reason) {
			t.Fatalf("err %v, want the cancel reason", err)
		}
	})
	t.Run("resolves with the winner when the cancel comes right after the PASE win", func(t *testing.T) {
		t.Parallel()
		clk := newClock()
		var r *testRace
		ready := make(chan struct{})
		r = race(context.Background(), clk, discovery.SessionOptions{Timeout: 30 * time.Second}, func(ctx context.Context, win func() bool, i int) error {
			<-ready
			return winner(func() { r.session.Cancel(reason) }, nil)(ctx, win, i)
		}, newFixed(deviceA))
		close(ready)
		if c, err := outcome(t, r.session, clk); err != nil || c.Device.InstanceID != "device-a" {
			t.Fatalf("%+v, %v; want the winner", c, err)
		}
	})
	t.Run("resolves with the winner when the cancel comes while the winner commissions", func(t *testing.T) {
		t.Parallel()
		clk := newClock()
		won, commissioned := make(chan struct{}), make(chan struct{})
		r := race(context.Background(), clk, discovery.SessionOptions{Timeout: 30 * time.Second}, winner(func() { close(won) }, commissioned), newFixed(deviceA))
		recv(t, won)
		r.session.Cancel(reason)
		close(commissioned)
		if c, err := outcome(t, r.session, clk); err != nil || c.Device.InstanceID != "device-a" {
			t.Fatalf("%+v, %v; want the winner", c, err)
		}
	})
	t.Run("resolves with the winner when every scanner fails after the PASE win", func(t *testing.T) {
		t.Parallel()
		clk := newClock()
		tr := newFixed(deviceA)
		tr.ignoresStop = true
		r := race(context.Background(), clk, discovery.SessionOptions{Timeout: 30 * time.Second}, winner(func() { tr.fail(errors.New("scanner broke")) }, nil), tr)
		if c, err := outcome(t, r.session, clk); err != nil || c.Device.InstanceID != "device-a" {
			t.Fatalf("%+v, %v; want the winner", c, err)
		}
	})
	t.Run("resolves with the node when an instance discovery is canceled after finding it", func(t *testing.T) {
		t.Parallel()
		clk := newClock()
		var s *discovery.Session[discovery.Candidate]
		ready := make(chan struct{})
		tr := newFixed(deviceA)
		tr.afterStop = func() {
			<-ready
			s.Cancel(reason)
		}
		s = discovery.FindInstance(context.Background(), []discovery.Transport{tr}, discovery.SessionOptions{Timeout: 30 * time.Second, Clock: clk})
		close(ready)
		if c, err := outcome(t, s, clk); err != nil || c.Device.InstanceID != "device-a" {
			t.Fatalf("%+v, %v; want the node", c, err)
		}
	})
	t.Run("keeps the first cancel reason when canceled twice while attempts wind down", func(t *testing.T) {
		t.Parallel()
		clk := newClock()
		r := race(context.Background(), clk, discovery.SessionOptions{Timeout: 5 * time.Second}, untilAborted, newFixed(deviceA))
		for r.started.Load() == 0 {
			time.Sleep(time.Millisecond)
		}
		r.session.Cancel(reason)
		r.session.Cancel(errors.New("caller gave up again"))
		if _, err := outcome(t, r.session, clk); !errors.Is(err, reason) {
			t.Fatalf("err %v, want the first reason", err)
		}
	})
	t.Run("keeps the cancel reason when every scanner fails after the cancel", func(t *testing.T) {
		t.Parallel()
		clk := newClock()
		tr := newFixed(deviceA)
		tr.ignoresStop = true
		r := race(context.Background(), clk, discovery.SessionOptions{Timeout: 30 * time.Second}, untilAborted, tr)
		for r.started.Load() == 0 {
			time.Sleep(time.Millisecond)
		}
		r.session.Cancel(reason)
		tr.fail(errors.New("scanner broke"))
		if _, err := outcome(t, r.session, clk); !errors.Is(err, reason) {
			t.Fatalf("err %v, want the cancel reason", err)
		}
	})
	for name, interrupt := range map[string]func(*testRace, context.CancelCauseFunc, *fixedTransport){
		"a cancel":              func(r *testRace, _ context.CancelCauseFunc, _ *fixedTransport) { r.session.Cancel(reason) },
		"a caller abort":        func(_ *testRace, cancel context.CancelCauseFunc, _ *fixedTransport) { cancel(reason) },
		"every scanner failing": func(_ *testRace, _ context.CancelCauseFunc, tr *fixedTransport) { tr.fail(errors.New("scanner broke")) },
	} {
		t.Run("settles only after the aborted attempts have finished when interrupted by "+name, func(t *testing.T) {
			t.Parallel()
			clk := newClock()
			tr := newFixed(deviceA)
			ctx, cancel := context.WithCancelCause(context.Background())
			defer cancel(nil)
			var finished atomic.Bool
			release := make(chan struct{})
			r := race(ctx, clk, discovery.SessionOptions{Timeout: 30 * time.Second}, func(ctx context.Context, _ func() bool, _ int) error {
				<-ctx.Done()
				<-release // the cleanup outlives the abort
				finished.Store(true)
				return ctx.Err()
			}, tr)
			for r.started.Load() == 0 {
				time.Sleep(time.Millisecond)
			}
			interrupt(r, cancel, tr)
			select {
			case <-r.session.Done():
				t.Fatal("settled before the attempt finished")
			case <-time.After(20 * time.Millisecond):
			}
			close(release)
			_, _ = outcome(t, r.session, clk)
			if !finished.Load() {
				t.Fatal("settled before the attempt finished")
			}
		})
	}
	t.Run("detaches from the caller's abort signal once the discovery resolves", func(t *testing.T) {
		t.Parallel()
		// By meaning: an abort after the outcome changes nothing.
		clk := newClock()
		ctx, cancel := context.WithCancel(context.Background())
		r := race(ctx, clk, discovery.SessionOptions{Timeout: 30 * time.Second}, winner(nil, nil), newFixed(deviceA))
		if _, err := outcome(t, r.session, clk); err != nil {
			t.Fatal(err)
		}
		cancel()
		if _, err := r.session.Wait(context.Background()); err != nil {
			t.Fatalf("the outcome moved to %v", err)
		}
	})
	t.Run("detaches from the caller's abort signal once the discovery rejects", func(t *testing.T) {
		t.Parallel()
		clk := newClock()
		ctx, cancel := context.WithCancel(context.Background())
		s := discovery.FindInstance(ctx, []discovery.Transport{newFixed()}, discovery.SessionOptions{Timeout: 5 * time.Second, Clock: clk})
		_, err := outcome(t, s, clk)
		cancel()
		if _, err2 := s.Wait(context.Background()); !errors.Is(err, discovery.ErrDeviceNotDiscovered) || !errors.Is(err2, discovery.ErrDeviceNotDiscovered) {
			t.Fatalf("%v then %v, want ErrDeviceNotDiscovered", err, err2)
		}
	})
	t.Run("keeps discovering while another scanner still runs", func(t *testing.T) {
		t.Parallel()
		clk := newClock()
		broken := newFixed()
		broken.fail(errors.New("BLE did not start"))
		r := race(context.Background(), clk, discovery.SessionOptions{Timeout: 30 * time.Second}, winner(nil, nil), broken, newFixed(deviceA))
		if c, err := outcome(t, r.session, clk); err != nil || c.Device.InstanceID != "device-a" {
			t.Fatalf("%+v, %v; want the winner", c, err)
		}
	})
	t.Run("logs a failed scanner and resolves a continuous discovery with what it found", func(t *testing.T) {
		t.Parallel()
		clk := newClock()
		broken := newFixed()
		broken.fail(errors.New("BLE did not start"))
		s := discovery.FindAll(context.Background(), []discovery.Transport{broken, newFixed()}, discovery.SessionOptions{Timeout: 5 * time.Second, Clock: clk}, nil)
		if found, err := outcome(t, s, clk); err != nil || len(found) != 0 {
			t.Fatalf("%v, %v; want nothing found and no error", found, err)
		}
	})
	scannerFailure := errors.New("BLE did not start")
	for name, shape := range map[string]func(broken *fixedTransport) []discovery.Transport{
		"one of two scanners failed and the other found nothing": func(b *fixedTransport) []discovery.Transport { return []discovery.Transport{b, newFixed()} },
		"every scanner failed": func(b *fixedTransport) []discovery.Transport { return []discovery.Transport{b} },
		"one scanner failed and the other ended at once without a result": func(b *fixedTransport) []discovery.Transport {
			other := newFixed()
			other.endsAtOnce = true
			return []discovery.Transport{b, other}
		},
	} {
		t.Run("reports the scanner failure as DiscoveryAggregateError where "+name, func(t *testing.T) {
			t.Parallel()
			clk := newClock()
			broken := newFixed()
			broken.fail(scannerFailure)
			s := discovery.FindInstance(context.Background(), shape(broken), discovery.SessionOptions{Timeout: 5 * time.Second, Clock: clk})
			_, err := outcome(t, s, clk)
			var agg *discovery.AggregateError
			if !errors.As(err, &agg) || !errors.Is(err, scannerFailure) {
				t.Fatalf("err %v, want an AggregateError of the scanner's", err)
			}
		})
	}
}

func TestDiscoveryErrorParity(t *testing.T) {
	t.Parallel()
	t.Run("reports DeviceNotDiscoveredError when commissioning discovery finds no device", func(t *testing.T) {
		t.Parallel()
		clk := newClock()
		s := discovery.CommissioningRace(context.Background(), []discovery.Transport{newFixed()}, discovery.SessionOptions{Timeout: 5 * time.Second, Clock: clk}, discovery.Identity{},
			func(context.Context, discovery.Candidate, func() bool) (int, error) { return 0, nil })
		if _, err := outcome(t, s, clk); !errors.Is(err, discovery.ErrDeviceNotDiscovered) {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("reports DeviceNotDiscoveredError when instance discovery finds no node", func(t *testing.T) {
		t.Parallel()
		clk := newClock()
		s := discovery.FindInstance(context.Background(), []discovery.Transport{newFixed()}, discovery.SessionOptions{Timeout: 5 * time.Second, Clock: clk})
		if _, err := outcome(t, s, clk); !errors.Is(err, discovery.ErrDeviceNotDiscovered) {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("reports DiscoveryIncompleteError when the timeout aborts an attempt in flight", func(t *testing.T) {
		t.Parallel()
		clk := newClock()
		r := race(context.Background(), clk, discovery.SessionOptions{Timeout: 5 * time.Second}, untilAborted, newFixed(deviceA))
		if _, err := outcome(t, r.session, clk); !errors.Is(err, discovery.ErrDiscoveryIncomplete) {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("reports DiscoveryAggregateError when one attempt failed and the others were canceled", func(t *testing.T) {
		t.Parallel()
		clk := newClock()
		failure := errors.New("PASE failed")
		r := race(context.Background(), clk, discovery.SessionOptions{Timeout: 5 * time.Second}, func(ctx context.Context, win func() bool, i int) error {
			if i == 0 {
				return failure
			}
			return untilAborted(ctx, win, i)
		}, newFixed(deviceA, deviceB))
		_, err := outcome(t, r.session, clk)
		var agg *discovery.AggregateError
		if !errors.As(err, &agg) || len(agg.Errs) != 1 || !errors.Is(err, failure) {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("rejects with NoDiscoveryTransportError when no scanner is installed for the requested transports", func(t *testing.T) {
		t.Parallel()
		clk := newClock()
		ble := newFixed(deviceA)
		ble.kind = discovery.TransportBLE
		s := discovery.FindInstance(context.Background(), []discovery.Transport{ble}, discovery.SessionOptions{Timeout: 5 * time.Second, Clock: clk, Capabilities: &discovery.Capabilities{OnIPNetwork: true}})
		if _, err := outcome(t, s, clk); !errors.Is(err, discovery.ErrNoDiscoveryTransport) {
			t.Fatalf("err %v", err)
		}
	})
	unusable := errors.New("cannot build the node")
	t.Run("reports the error of a node that could not be built", func(t *testing.T) {
		t.Parallel()
		clk := newClock()
		s := discovery.FindInstance(context.Background(), []discovery.Transport{newFixed(deviceA)}, discovery.SessionOptions{
			Timeout: 5 * time.Second, Clock: clk, Prepare: func(context.Context, discovery.Candidate) error { return unusable },
		})
		if _, err := outcome(t, s, clk); !errors.Is(err, unusable) {
			t.Fatalf("err %v, want the preparation's", err)
		}
	})
	t.Run("keeps the first node found when further devices finish their preparation later", func(t *testing.T) {
		t.Parallel()
		clk := newClock()
		slow := make(chan struct{})
		s := discovery.FindInstance(context.Background(), []discovery.Transport{newFixed(deviceA, deviceB)}, discovery.SessionOptions{
			Timeout: 5 * time.Second, Clock: clk, Prepare: func(_ context.Context, c discovery.Candidate) error {
				if c.Device.InstanceID == "device-b" {
					<-slow
				}
				return nil
			},
		})
		go func() { time.Sleep(20 * time.Millisecond); close(slow) }()
		if c, err := outcome(t, s, clk); err != nil || c.Device.InstanceID != "device-a" {
			t.Fatalf("%+v, %v; want device-a", c, err)
		}
	})
	t.Run("waits for a device still being prepared when the scan ends", func(t *testing.T) {
		t.Parallel()
		clk := newClock()
		slow := make(chan struct{})
		tr := newFixed(deviceA)
		tr.endsAtOnce = true
		s := discovery.FindInstance(context.Background(), []discovery.Transport{tr}, discovery.SessionOptions{
			Clock: clk, Prepare: func(context.Context, discovery.Candidate) error { <-slow; return nil },
		})
		select {
		case <-s.Done():
			t.Fatal("ended before the preparation")
		case <-time.After(20 * time.Millisecond):
		}
		close(slow)
		if c, err := outcome(t, s, clk); err != nil || c.Device.InstanceID != "device-a" {
			t.Fatalf("%+v, %v; want device-a", c, err)
		}
	})
	t.Run("reports the first device's error when several cannot be used", func(t *testing.T) {
		t.Parallel()
		clk := newClock()
		first := errors.New("first")
		var n atomic.Int32
		order := make(chan struct{})
		s := discovery.FindInstance(context.Background(), []discovery.Transport{newFixed(deviceA, deviceB)}, discovery.SessionOptions{
			Timeout: 5 * time.Second, Clock: clk, Prepare: func(_ context.Context, c discovery.Candidate) error {
				if c.Device.InstanceID == "device-b" {
					<-order
					return errors.New("second")
				}
				defer close(order)
				n.Add(1)
				return first
			},
		})
		if _, err := outcome(t, s, clk); !errors.Is(err, first) {
			t.Fatalf("err %v, want the first device's", err)
		}
	})
	t.Run("keeps the attempts' own errors over the unusable device's error", func(t *testing.T) {
		t.Parallel()
		clk := newClock()
		failure := errors.New("PASE failed")
		s := discovery.Race(context.Background(), []discovery.Transport{newFixed(deviceA, deviceB)}, discovery.SessionOptions{
			Timeout: 5 * time.Second, Clock: clk, Prepare: func(_ context.Context, c discovery.Candidate) error {
				if c.Device.InstanceID == "device-b" {
					return unusable
				}
				return nil
			},
		}, func(context.Context, discovery.Candidate, func() bool) (int, error) { return 0, failure })
		_, err := outcome(t, s, clk)
		if !errors.Is(err, failure) || errors.Is(err, unusable) {
			t.Fatalf("err %v, want the attempt's", err)
		}
	})
	t.Run("warns once per device and logs repeats at debug", func(t *testing.T) {
		t.Parallel()
		// By meaning: the same device reported twice and unusable twice
		// is one unusable device whose error is the outcome.
		clk := newClock()
		s := discovery.FindInstance(context.Background(), []discovery.Transport{newFixed(deviceA, deviceA)}, discovery.SessionOptions{
			Timeout: 5 * time.Second, Clock: clk, Prepare: func(context.Context, discovery.Candidate) error { return unusable },
		})
		if _, err := outcome(t, s, clk); !errors.Is(err, unusable) {
			t.Fatalf("err %v", err)
		}
	})
}

func TestDiscoveryTransportsParity(t *testing.T) {
	t.Parallel()
	ip := newFixed()
	ble := newFixed()
	ble.kind = discovery.TransportBLE
	all := []discovery.Transport{ip, ble}
	kinds := func(ts []discovery.Transport) []discovery.TransportKind {
		out := make([]discovery.TransportKind, len(ts))
		for i, t := range ts {
			out[i] = t.Kind()
		}
		return out
	}
	t.Run("discovers on every installed transport where the caller states nothing", func(t *testing.T) {
		t.Parallel()
		if got := kinds(discovery.SelectTransports(all, nil)); len(got) != 2 {
			t.Fatalf("%v", got)
		}
	})
	t.Run("discovers on the IP network alone unless the payload names BLE", func(t *testing.T) {
		t.Parallel()
		got := kinds(discovery.SelectTransports(all, &discovery.Capabilities{SoftAP: true}))
		if len(got) != 1 || got[0] != discovery.TransportIP {
			t.Fatalf("%v", got)
		}
	})
	t.Run("discovers on BLE where the payload names it", func(t *testing.T) {
		t.Parallel()
		caps := discovery.CapabilitiesOf(0b010)
		if got := kinds(discovery.SelectTransports(all, &caps)); len(got) != 2 {
			t.Fatalf("%v", got)
		}
	})
	t.Run("names the device to a scanner without the options that steer discovery itself", func(t *testing.T) {
		t.Parallel()
		clk := newClock()
		tr := newFixed()
		id := discovery.ByLongDiscriminator(3840)
		s := discovery.FindInstance(context.Background(), []discovery.Transport{tr}, discovery.SessionOptions{Identifier: id, Timeout: time.Second, Clock: clk})
		if got := recv(t, tr.ids); got != id {
			t.Fatalf("identifier %+v, want %+v", got, id)
		}
		_, _ = outcome(t, s, clk)
	})
}

func TestParallelPaseRaceParity(t *testing.T) {
	t.Parallel()
	t.Run("resolves with the winner while a losing attempt's rejection stays handled", func(t *testing.T) {
		t.Parallel()
		clk := newClock()
		r := race(context.Background(), clk, discovery.SessionOptions{Timeout: 30 * time.Second}, func(ctx context.Context, win func() bool, i int) error {
			if i == 0 {
				return winner(nil, nil)(ctx, win, i)
			}
			return errors.New("loser")
		}, newFixed(deviceA, deviceB))
		if c, err := outcome(t, r.session, clk); err != nil || c.Device.InstanceID == "" {
			t.Fatalf("%+v, %v", c, err)
		}
	})
	t.Run("rejects with the winner's own error", func(t *testing.T) {
		t.Parallel()
		clk := newClock()
		own := errors.New("commissioning failed")
		r := race(context.Background(), clk, discovery.SessionOptions{Timeout: 30 * time.Second}, func(_ context.Context, win func() bool, _ int) error {
			win()
			return own
		}, newFixed(deviceA))
		if _, err := outcome(t, r.session, clk); !errors.Is(err, own) {
			t.Fatalf("err %v, want the winner's", err)
		}
	})
	t.Run("starts attempts one stagger slot apart", func(t *testing.T) {
		t.Parallel()
		clk := newClock()
		r := race(context.Background(), clk, discovery.SessionOptions{Timeout: 30 * time.Second}, untilAborted, newFixed(deviceA, deviceB, deviceC))
		waitStarted := func(n int32) {
			t.Helper()
			deadline := time.Now().Add(5 * time.Second)
			for r.started.Load() != n {
				if time.Now().After(deadline) {
					t.Fatalf("%d started, want %d", r.started.Load(), n)
				}
				time.Sleep(time.Millisecond)
			}
		}
		waitStarted(1)
		time.Sleep(10 * time.Millisecond)
		if n := r.started.Load(); n != 1 {
			t.Fatalf("%d started before the first slot", n)
		}
		// The discovery's timeout and the two stagger waits.
		for len(clk.Armed()) < 3 {
			time.Sleep(time.Millisecond)
		}
		clk.Advance(discovery.CrossDeviceStagger)
		waitStarted(2)
		clk.Advance(discovery.CrossDeviceStagger)
		waitStarted(3)
		r.session.Cancel(nil)
		_, _ = outcome(t, r.session, clk)
	})
	t.Run("does not start a staggered attempt once a candidate has won", func(t *testing.T) {
		t.Parallel()
		clk := newClock()
		r := race(context.Background(), clk, discovery.SessionOptions{Timeout: 30 * time.Second}, winner(nil, nil), newFixed(deviceA, deviceB))
		if _, err := outcome(t, r.session, clk); err != nil {
			t.Fatal(err)
		}
		if n := r.started.Load(); n != 1 {
			t.Fatalf("%d started, want the winner alone", n)
		}
	})
	t.Run("ends pending stagger waits when discovery ends without a win", func(t *testing.T) {
		t.Parallel()
		clk := newClock()
		r := race(context.Background(), clk, discovery.SessionOptions{Timeout: 30 * time.Second}, untilAborted, newFixed(deviceA, deviceB))
		for r.started.Load() == 0 {
			time.Sleep(time.Millisecond)
		}
		r.session.Stop()
		select {
		case <-r.session.Done():
		case <-time.After(5 * time.Second):
			t.Fatal("the stagger wait kept the discovery open")
		}
		if n := r.started.Load(); n != 1 {
			t.Fatalf("%d started", n)
		}
	})
}

func TestContinuousDiscoveryParity(t *testing.T) {
	t.Parallel()
	t.Run("reports each device found and lists them when a timeout bounds the search", func(t *testing.T) {
		t.Parallel()
		// By meaning of ContinuousDiscoveryTest.ts:63-85: two concurrent
		// discoveries each report the device once.
		clk := newClock()
		var seen atomic.Int32
		opts := discovery.SessionOptions{Timeout: 5 * time.Second, Clock: clk}
		first := discovery.FindAll(context.Background(), []discovery.Transport{newFixed(deviceA)}, opts, func(discovery.Candidate) { seen.Add(1) })
		second := discovery.FindAll(context.Background(), []discovery.Transport{newFixed(deviceA)}, opts, nil)
		for _, s := range []*discovery.Session[[]discovery.Candidate]{first, second} {
			if found, err := outcome(t, s, clk); err != nil || len(found) != 1 || found[0].Device.InstanceID != "device-a" {
				t.Fatalf("%v, %v", found, err)
			}
		}
		if seen.Load() != 1 {
			t.Fatalf("found reported %d times", seen.Load())
		}
	})
	t.Run("an unbounded search reports through found alone", func(t *testing.T) {
		t.Parallel()
		clk := newClock()
		found := make(chan discovery.Candidate, 1)
		s := discovery.FindAll(context.Background(), []discovery.Transport{newFixed(deviceA)}, discovery.SessionOptions{Clock: clk}, func(c discovery.Candidate) { found <- c })
		recv(t, found)
		s.Stop()
		if list, err := outcome(t, s, clk); err != nil || len(list) != 0 {
			t.Fatalf("%v, %v", list, err)
		}
	})
}

// TestPASERaceParity: PaseDiscovery (PaseDiscovery.ts:28-71 at 862ad75f)
// — the race's winner is the attempt's result, and a race without a
// device reports one of the discovery outcomes, ErrDiscovery's kinds
// (DiscoveryError.ts:12-29).
func TestPASERaceParity(t *testing.T) {
	t.Parallel()
	clk := newClock()
	s := discovery.PASERace(context.Background(), []discovery.Transport{newFixed(deviceA)}, discovery.SessionOptions{Clock: clk},
		func(_ context.Context, c discovery.Candidate, win func() bool) (string, error) {
			if !win() {
				return "", errors.New("lost")
			}
			return c.Device.InstanceID, nil
		})
	if got, err := outcome(t, s, clk); err != nil || got != "device-a" {
		t.Fatalf("%q, %v", got, err)
	}
	none := discovery.PASERace(context.Background(), []discovery.Transport{newFixed()}, discovery.SessionOptions{Clock: clk, Timeout: time.Second},
		func(context.Context, discovery.Candidate, func() bool) (string, error) { return "", nil })
	if _, err := outcome(t, none, clk); !errors.Is(err, discovery.ErrDiscovery) || !errors.Is(err, discovery.ErrDeviceNotDiscovered) {
		t.Fatalf("err %v, want ErrDeviceNotDiscovered, an ErrDiscovery", err)
	}
}

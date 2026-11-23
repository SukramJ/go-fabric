// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package discovery_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"thingfabric.dev/go-fabric/client/discovery"
	"thingfabric.dev/go-fabric/clock/clocktest"
)

// lateDevice is a transport that reports the candidates of early first
// and the device after one CrossDeviceStagger of its clock.
type lateDevice struct {
	clk    *clocktest.Fake
	early  []discovery.Device
	device discovery.Device
}

func (l *lateDevice) Kind() discovery.TransportKind { return discovery.TransportBLE }

func (l *lateDevice) FindContinuously(ctx context.Context, _ discovery.Identifier, found func(discovery.Device)) error {
	for i := range l.early {
		found(l.early[i])
	}
	t := l.clk.NewTimer(discovery.CrossDeviceStagger)
	defer t.Stop()
	select {
	case <-t.C():
		found(l.device)
	case <-ctx.Done():
		return nil
	}
	<-ctx.Done()
	return nil
}

// settledOutcome waits for the session's outcome inside a synctest
// bubble, advancing the fake clock by step and letting the bubble settle
// after each step, for at most bound of fake time.
func settledOutcome[T any](t *testing.T, s *discovery.Session[T], clk *clocktest.Fake, step, bound time.Duration) (T, error) {
	t.Helper()
	end := clk.Now().Add(bound)
	for {
		synctest.Wait()
		select {
		case <-s.Done():
			return s.Wait(t.Context())
		default:
		}
		if clk.Now().After(end) {
			t.Fatalf("the race had no outcome within %v", bound)
		}
		clk.Advance(step)
	}
}

// TestCommissioningRace_EndedAttemptsDoNotDelayTheNext: the stagger
// between attempts counts only the attempts still running, so candidates
// whose attempts have already ended do not push back the attempt on a
// device found after them, however many there were (review II,
// R2-P3-ble-01, R2-P3-ctl-lan-06).
func TestCommissioningRace_EndedAttemptsDoNotDelayTheNext(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		clk := newClock()
		begin := clk.Now()
		device := discovery.Device{InstanceID: "device", D: 1000, CM: 1}
		const others = 1000
		tr := &lateDevice{clk: clk, device: device}
		for i := range others {
			tr.early = append(tr.early, discovery.Device{InstanceID: fmt.Sprintf("other-%04d", i), D: 1000, CM: 1})
		}
		failed := errors.New("PASE failed")
		var startedAt time.Duration
		s := discovery.CommissioningRace(t.Context(), []discovery.Transport{tr},
			discovery.SessionOptions{Clock: clk}, discovery.Identity{},
			func(_ context.Context, c discovery.Candidate, win func() bool) (string, error) {
				if c.Device.InstanceID != device.InstanceID {
					return "", failed
				}
				startedAt = clk.Now().Sub(begin)
				if !win() {
					return "", errors.New("lost")
				}
				return c.Device.InstanceID, nil
			})
		got, err := settledOutcome(t, s, clk, 100*time.Millisecond, 2*discovery.DefaultCommissioningTimeout)
		if err != nil || got != device.InstanceID {
			t.Fatalf("%d candidates whose attempts failed at once were found before the device: outcome %q, %v", others, got, err)
		}
		if startedAt > 2*discovery.CrossDeviceStagger {
			t.Fatalf("the device, found %v into the race, was attempted %v into it", discovery.CrossDeviceStagger, startedAt)
		}
	})
}

// TestCommissioningRace_RunningAttemptsKeepTheStagger: while an attempt
// runs, the next starts one CrossDeviceStagger after it; once every
// started attempt has ended, the next starts at once.
func TestCommissioningRace_RunningAttemptsKeepTheStagger(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		clk := newClock()
		release := make(chan struct{})
		started := make(chan string, 3)
		s := discovery.Race(t.Context(), []discovery.Transport{newFixed(deviceA, deviceB, deviceC)},
			discovery.SessionOptions{Clock: clk, Timeout: 30 * time.Second},
			func(ctx context.Context, c discovery.Candidate, _ func() bool) (string, error) {
				started <- c.Device.InstanceID
				if c.Device.InstanceID == deviceA.InstanceID {
					select {
					case <-release:
					case <-ctx.Done():
					}
					return "", errors.New("PASE failed")
				}
				<-ctx.Done()
				return "", ctx.Err()
			})
		count := func() int {
			synctest.Wait()
			return len(started)
		}
		if n := count(); n != 1 {
			t.Fatalf("%d attempts started at once, want 1", n)
		}
		clk.Advance(discovery.CrossDeviceStagger / 2)
		if n := count(); n != 1 {
			t.Fatalf("%d attempts started half a stagger in while the first runs, want 1", n)
		}
		clk.Advance(discovery.CrossDeviceStagger / 2)
		if n := count(); n != 2 {
			t.Fatalf("%d attempts started one stagger in, want 2", n)
		}
		close(release)
		if n := count(); n != 2 {
			t.Fatalf("%d attempts started while the second still runs, want 2", n)
		}
		clk.Advance(discovery.CrossDeviceStagger)
		if n := count(); n != 3 {
			t.Fatalf("%d attempts started one stagger after the second, want 3", n)
		}
		s.Cancel(nil)
		_, _ = settledOutcome(t, s, clk, time.Second, time.Minute)
	})
}

// TestCommissioningRace_DeviceAfterUnansweredCandidatesIsAttempted: a
// device found after any number of candidates whose attempts never get an
// answer is attempted within RaceStaggerCeiling of being found and wins
// the race; every place is filled, and beside the MaxRaceAttempts places at
// most MaxRaceUnwinding attempts ended for later candidates still run
// (review II, R2-P3-ctl-lan-06, R2-P3-ble-01, R3-bound-3501, owner
// decision 559).
func TestCommissioningRace_DeviceAfterUnansweredCandidatesIsAttempted(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		clk := newClock()
		begin := clk.Now()
		device := discovery.Device{InstanceID: "device", D: 1000, CM: 1}
		const others = 200
		tr := &lateDevice{clk: clk, device: device}
		for i := range others {
			tr.early = append(tr.early, discovery.Device{InstanceID: fmt.Sprintf("other-%04d", i), D: 1000, CM: 1})
		}
		var mu sync.Mutex
		running, most := 0, 0
		var startedAt time.Duration
		s := discovery.CommissioningRace(t.Context(), []discovery.Transport{tr},
			discovery.SessionOptions{Clock: clk}, discovery.Identity{},
			func(ctx context.Context, c discovery.Candidate, win func() bool) (string, error) {
				mu.Lock()
				running++
				most = max(most, running)
				mu.Unlock()
				defer func() {
					mu.Lock()
					running--
					mu.Unlock()
				}()
				if c.Device.InstanceID != device.InstanceID {
					<-ctx.Done()
					return "", ctx.Err()
				}
				startedAt = clk.Now().Sub(begin)
				if !win() {
					return "", errors.New("lost")
				}
				return c.Device.InstanceID, nil
			})
		got, err := settledOutcome(t, s, clk, 100*time.Millisecond, 2*discovery.DefaultCommissioningTimeout)
		if err != nil || got != device.InstanceID {
			t.Fatalf("%d candidates whose attempts get no answer were found before the device: outcome %q, %v", others, got, err)
		}
		if waited := startedAt - discovery.CrossDeviceStagger; waited > discovery.RaceStaggerCeiling {
			t.Fatalf("the device, found %v into the race, was attempted %v after it was found, want at most %v",
				discovery.CrossDeviceStagger, waited, discovery.RaceStaggerCeiling)
		}
		if most < discovery.MaxRaceAttempts || most > discovery.MaxRaceAttempts+discovery.MaxRaceUnwinding {
			t.Fatalf("at most %d attempts ran at once, want every place filled (%d) and at most %d beside them still returning",
				most, discovery.MaxRaceAttempts, discovery.MaxRaceUnwinding)
		}
	})
}

// feed is a transport that reports each device sent on it.
type feed chan discovery.Device

func (feed) Kind() discovery.TransportKind { return discovery.TransportIP }

func (f feed) FindContinuously(ctx context.Context, _ discovery.Identifier, found func(discovery.Device)) error {
	for {
		select {
		case d := <-f:
			found(d)
		case <-ctx.Done():
			return nil
		}
	}
}

// placesRace runs a race on a feed whose attempts record their start and
// the end of their context and then wait for it; the attempts on the
// devices of answering call AttemptAnswered first.
type placesRace struct {
	clk     *clocktest.Fake
	feed    feed
	s       *discovery.Session[string]
	mu      sync.Mutex
	started map[string]bool
	ended   map[string]bool
}

func newPlacesRace(t *testing.T, answering func(id string) bool) *placesRace {
	t.Helper()
	r := &placesRace{clk: newClock(), feed: make(feed), started: map[string]bool{}, ended: map[string]bool{}}
	r.s = discovery.Race(t.Context(), []discovery.Transport{r.feed},
		discovery.SessionOptions{Clock: r.clk, Timeout: 10 * time.Minute},
		func(ctx context.Context, c discovery.Candidate, _ func() bool) (string, error) {
			id := c.Device.InstanceID
			if answering(id) {
				discovery.AttemptAnswered(ctx)
			}
			r.mu.Lock()
			r.started[id] = true
			r.mu.Unlock()
			<-ctx.Done()
			r.mu.Lock()
			r.ended[id] = true
			r.mu.Unlock()
			return "", ctx.Err()
		})
	return r
}

// find reports the devices named ids one after the other, letting the
// race settle after each, so the race finds them in this order.
func (r *placesRace) find(ids ...string) {
	for _, id := range ids {
		r.feed <- discovery.Device{InstanceID: id}
		synctest.Wait()
	}
}

func (r *placesRace) advance(d time.Duration) {
	r.clk.Advance(d)
	synctest.Wait()
}

func (r *placesRace) state(id string) (started, ended bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.started[id], r.ended[id]
}

func placeIDs() []string {
	ids := make([]string, discovery.MaxRaceAttempts)
	for i := range ids {
		ids[i] = fmt.Sprintf("held-%02d", i)
	}
	return ids
}

// TestRace_OldestUnansweredAttemptGivesItsPlace: with every place taken, a
// candidate found later starts at its ceiling in the place of the oldest
// attempt that has had no answer, which ends; an answered attempt older
// than that one keeps its place.
func TestRace_OldestUnansweredAttemptGivesItsPlace(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ids := placeIDs()
		r := newPlacesRace(t, func(id string) bool { return id == ids[0] })
		r.find(ids...)
		r.advance(time.Duration(discovery.MaxRaceAttempts) * discovery.CrossDeviceStagger)
		for _, id := range ids {
			if started, ended := r.state(id); !started || ended {
				t.Fatalf("%s: started %v, ended %v once the stagger has passed, want started and running", id, started, ended)
			}
		}
		r.find("late")
		r.advance(discovery.RaceStaggerCeiling - discovery.CrossDeviceStagger)
		if started, _ := r.state("late"); started {
			t.Fatal("a candidate found with every place taken started before its ceiling")
		}
		r.advance(discovery.CrossDeviceStagger)
		if started, _ := r.state("late"); !started {
			t.Fatalf("a candidate found with every place taken has not started %v after it was found", discovery.RaceStaggerCeiling)
		}
		if _, ended := r.state(ids[1]); !ended {
			t.Fatalf("the oldest attempt without an answer (%s) still runs after giving its place", ids[1])
		}
		for _, id := range append([]string{ids[0]}, ids[2:]...) {
			if _, ended := r.state(id); ended {
				t.Fatalf("%s ended for a later candidate; only %s should have", id, ids[1])
			}
		}
		r.s.Cancel(nil)
		_, _ = settledOutcome(t, r.s, r.clk, time.Second, time.Minute)
	})
}

// TestRace_AnsweredAttemptsKeepTheirPlaces: an attempt that has had an
// answer is never ended for a candidate found later; while every place
// holds one, the candidate waits past its ceiling until an attempt ends.
func TestRace_AnsweredAttemptsKeepTheirPlaces(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ids := placeIDs()
		r := newPlacesRace(t, func(id string) bool { return id != "late" })
		r.find(ids...)
		r.advance(time.Duration(discovery.MaxRaceAttempts) * discovery.CrossDeviceStagger)
		r.find("late")
		r.advance(2 * discovery.RaceStaggerCeiling)
		if started, _ := r.state("late"); started {
			t.Fatal("a candidate started while every place holds an answered attempt")
		}
		for _, id := range ids {
			if _, ended := r.state(id); ended {
				t.Fatalf("the answered attempt %s ended for a later candidate", id)
			}
		}
		r.s.Cancel(nil)
		_, _ = settledOutcome(t, r.s, r.clk, time.Second, time.Minute)
	})
}

// TestRace_WaitingCandidatesAreBounded: with every place held by an
// answered attempt, ever-new candidates wait; at most MaxRaceWaiting of
// them do, the ones found longest ago leaving the queue unattempted, so
// the race's entries and goroutines do not grow with what a sender
// announces (review II, R2-C2-bound-2153).
func TestRace_WaitingCandidatesAreBounded(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ids := placeIDs()
		r := newPlacesRace(t, func(string) bool { return true })
		r.find(ids...)
		r.advance(time.Duration(discovery.MaxRaceAttempts) * discovery.CrossDeviceStagger)
		const late = 200
		for i := range late {
			r.find(fmt.Sprintf("late-%03d", i))
		}
		r.advance(2 * discovery.RaceStaggerCeiling)
		if got := discovery.RaceWaiting(r.s); got > discovery.MaxRaceWaiting {
			t.Fatalf("%d candidates wait after %d were found with every place held, want at most %d", got, late, discovery.MaxRaceWaiting)
		}
		r.s.Cancel(nil)
		_, _ = settledOutcome(t, r.s, r.clk, time.Second, time.Minute)
		for i := range late {
			if started, _ := r.state(fmt.Sprintf("late-%03d", i)); started {
				t.Fatalf("late-%03d started while every place was held", i)
			}
		}
	})
}

// TestRace_DeviceFoundBehindAStreamOfCandidatesStartsWithinItsCeiling:
// a device found while every place holds an attempt without an answer
// starts within RaceStaggerCeiling of being found while further
// candidates arrive after it — the overflow of the waiting candidates
// makes the one found longest ago take a place at once, and does not drop
// it while a place holds an unanswered attempt; a burst faster than the
// ended attempts return is TestRace_DeviceInABurstStartsWithinItsCeiling's
// (review II, R3-bound-3501, owner decisions 55, 552 and 559).
func TestRace_DeviceFoundBehindAStreamOfCandidatesStartsWithinItsCeiling(t *testing.T) {
	t.Parallel()
	for _, every := range []time.Duration{100 * time.Millisecond, 500 * time.Millisecond, 900 * time.Millisecond} {
		t.Run(every.String(), func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				ids := placeIDs()
				r := newPlacesRace(t, func(string) bool { return false })
				r.find(ids...)
				r.advance(time.Duration(discovery.MaxRaceAttempts) * discovery.CrossDeviceStagger)
				r.find("device")
				var waited time.Duration
				for waited <= 2*discovery.RaceStaggerCeiling {
					if started, _ := r.state("device"); started {
						break
					}
					r.advance(every)
					waited += every
					r.find(fmt.Sprintf("stream-%04d", int(waited/every)))
					if got := discovery.RaceWaiting(r.s); got > discovery.MaxRaceWaiting {
						t.Fatalf("%d candidates wait, want at most %d", got, discovery.MaxRaceWaiting)
					}
				}
				if started, _ := r.state("device"); !started || waited > discovery.RaceStaggerCeiling {
					t.Fatalf("one candidate every %v after the device: the device started %v (after %v), want within %v of being found",
						every, started, waited, discovery.RaceStaggerCeiling)
				}
				r.s.Cancel(nil)
				_, _ = settledOutcome(t, r.s, r.clk, time.Second, time.Minute)
			})
		})
	}
}

// TestRace_DeviceInABurstStartsWithinItsCeiling: every place holds an
// attempt without an answer, then a burst arrives faster than the ended
// attempts return — MaxRaceWaiting candidates, the device, MaxRaceWaiting
// more. An ended attempt gives its place up when it is cancelled, not when
// it returns, so the device starts within RaceStaggerCeiling of being
// found however long the ended attempts take to unwind; the attempts
// running stay within MaxRaceAttempts + MaxRaceUnwinding (review II,
// R3-bound-3501, owner decision 559; the burst is the Fable diff read's
// probe, fable-diff-g3.md).
func TestRace_DeviceInABurstStartsWithinItsCeiling(t *testing.T) {
	t.Parallel()
	for _, unwind := range []time.Duration{0, 5 * time.Millisecond, 50 * time.Millisecond} {
		t.Run(unwind.String(), func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				clk := newClock()
				f := make(feed)
				var mu sync.Mutex
				started := map[string]bool{}
				running, most := 0, 0
				s := discovery.Race(t.Context(), []discovery.Transport{f},
					discovery.SessionOptions{Clock: clk, Timeout: 10 * time.Minute},
					func(ctx context.Context, c discovery.Candidate, _ func() bool) (string, error) {
						mu.Lock()
						started[c.Device.InstanceID] = true
						running++
						most = max(most, running)
						mu.Unlock()
						defer func() {
							mu.Lock()
							running--
							mu.Unlock()
						}()
						<-ctx.Done()
						time.Sleep(unwind) // the attempt's unwind after its cancel
						return "", ctx.Err()
					})
				find := func(id string) {
					f <- discovery.Device{InstanceID: id}
					synctest.Wait()
				}
				for _, id := range placeIDs() {
					find(id)
				}
				clk.Advance(time.Duration(discovery.MaxRaceAttempts) * discovery.CrossDeviceStagger)
				synctest.Wait()
				for i := range discovery.MaxRaceWaiting {
					find(fmt.Sprintf("before-%02d", i))
				}
				find("device")
				for i := range discovery.MaxRaceWaiting {
					find(fmt.Sprintf("after-%02d", i))
				}
				isStarted := func() bool {
					mu.Lock()
					defer mu.Unlock()
					return started["device"]
				}
				const step = 100 * time.Millisecond
				var waited time.Duration
				for !isStarted() && waited <= 2*discovery.RaceStaggerCeiling {
					time.Sleep(step) // the unwinds run in the bubble's time
					synctest.Wait()
					clk.Advance(step)
					synctest.Wait()
					waited += step
				}
				if !isStarted() || waited > discovery.RaceStaggerCeiling {
					t.Errorf("attempts unwinding %v after their cancel: the device started %v (after %v), want within %v of being found",
						unwind, isStarted(), waited, discovery.RaceStaggerCeiling)
				}
				mu.Lock()
				if most > discovery.MaxRaceAttempts+discovery.MaxRaceUnwinding {
					t.Errorf("%d attempts ran at once, want at most %d", most, discovery.MaxRaceAttempts+discovery.MaxRaceUnwinding)
				}
				mu.Unlock()
				if got := discovery.RaceWaiting(s); got > discovery.MaxRaceWaiting {
					t.Errorf("%d candidates wait once the unwinds are over, want at most %d", got, discovery.MaxRaceWaiting)
				}
				s.Cancel(nil)
				time.Sleep(time.Second)
				_, _ = settledOutcome(t, s, clk, time.Second, time.Minute)
			})
		})
	}
}

// TestRace_OverflowWhileReplacedAttemptsHaveNotReturnedIsBounded: the
// attempts ignore their cancel and never return. An ended attempt gives
// its place up at its cancel while fewer than MaxRaceUnwinding ended ones
// have not returned, so exactly that many late candidates start; past
// them an ended attempt keeps its place until it returns and covers one
// waiting candidate, and past those and MaxRaceWaiting more the one found
// longest ago is dropped. The attempts running stay within
// MaxRaceAttempts + MaxRaceUnwinding and the queue within MaxRaceWaiting +
// MaxRaceAttempts even when no attempt returns (review II, R3-bound-3501,
// R2-C2-bound-2153, owner decision 559).
func TestRace_OverflowWhileReplacedAttemptsHaveNotReturnedIsBounded(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		clk := newClock()
		f := make(feed)
		stuck := make(chan struct{})
		var mu sync.Mutex
		started := map[string]bool{}
		s := discovery.Race(t.Context(), []discovery.Transport{f},
			discovery.SessionOptions{Clock: clk, Timeout: 10 * time.Minute},
			func(ctx context.Context, c discovery.Candidate, _ func() bool) (string, error) {
				mu.Lock()
				started[c.Device.InstanceID] = true
				mu.Unlock()
				<-ctx.Done()
				<-stuck
				return "", ctx.Err()
			})
		ids := placeIDs()
		for _, id := range ids {
			f <- discovery.Device{InstanceID: id}
			synctest.Wait()
		}
		clk.Advance(time.Duration(discovery.MaxRaceAttempts) * discovery.CrossDeviceStagger)
		synctest.Wait()
		const late = 200
		for i := range late {
			f <- discovery.Device{InstanceID: fmt.Sprintf("late-%03d", i)}
			synctest.Wait()
			if got := discovery.RaceWaiting(s); got > discovery.MaxRaceWaiting+discovery.MaxRaceAttempts {
				t.Fatalf("%d candidates wait with no attempt returning, want at most %d", got, discovery.MaxRaceWaiting+discovery.MaxRaceAttempts)
			}
		}
		mu.Lock()
		if len(started) > discovery.MaxRaceAttempts+discovery.MaxRaceUnwinding {
			t.Errorf("%d attempts run with none returning, want at most %d", len(started), discovery.MaxRaceAttempts+discovery.MaxRaceUnwinding)
		}
		lateStarted := 0
		for i := range late {
			if started[fmt.Sprintf("late-%03d", i)] {
				lateStarted++
			}
		}
		if lateStarted != discovery.MaxRaceUnwinding {
			t.Errorf("%d late candidates started with no attempt returning, want %d: an ended attempt gives its place up at its cancel while fewer than that are unwinding",
				lateStarted, discovery.MaxRaceUnwinding)
		}
		mu.Unlock()
		close(stuck)
		s.Cancel(nil)
		_, _ = settledOutcome(t, s, clk, time.Second, time.Minute)
	})
}

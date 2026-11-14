// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package discovery_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/clock/clocktest"
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

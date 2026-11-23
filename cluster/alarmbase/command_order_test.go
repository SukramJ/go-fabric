// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package alarmbase_test

import (
	"context"
	"sync"
	"testing"
	"testing/synctest"

	"thingfabric.dev/go-fabric/cluster/alarmbase"
	dwa "thingfabric.dev/go-fabric/cluster/spec/dishwasheralarm"
)

// gatedDelegate holds the first ModifyEnabledAlarms it hears until release
// closes, and records every mask in the order it heard them.
type gatedDelegate struct {
	mu      sync.Mutex
	heard   []uint32
	entered chan struct{}
	release chan struct{}
}

func (d *gatedDelegate) ModifyEnabledAlarms(_ context.Context, mask uint32) error {
	d.mu.Lock()
	d.heard = append(d.heard, mask)
	first := len(d.heard) == 1
	d.mu.Unlock()
	if first {
		close(d.entered)
		<-d.release
	}
	return nil
}

func (*gatedDelegate) ResetAlarms(context.Context, uint32) error { return nil }

func newGated(t *testing.T) (*alarmbase.Server, *gatedDelegate) {
	t.Helper()
	d := &gatedDelegate{entered: make(chan struct{}), release: make(chan struct{})}
	s, err := alarmbase.NewDishwasherAlarm(alarmbase.Config{Supported: 0x3, ModifyEnabledAlarms: true, Delegate: d})
	if err != nil {
		t.Fatal(err)
	}
	return s, d
}

// TestModifyEnabledAlarmsStoresInTheOrderTheDelegateHeard (R4-conc-4252):
// two ModifyEnabledAlarms invokes that run concurrently leave Mask at the
// mask the Delegate heard last. Before the fix the second invoke ran its
// Delegate call and its store while the first one's Delegate call was
// still running, and the first one's store then landed last: Mask served
// 0x1 while the device was told 0x2.
func TestModifyEnabledAlarmsStoresInTheOrderTheDelegateHeard(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s, d := newGated(t)
		invoke := func(mask uint32, done chan<- error) {
			_, err := s.MatterInvoke(context.Background(), alarmbase.CmdModifyEnabledAlarms, dwa.ModifyEnabledAlarmsRequest{Mask: dwa.AlarmBitmap(mask)})
			done <- err
		}
		doneA, doneB := make(chan error, 1), make(chan error, 1)
		go invoke(0x1, doneA)
		<-d.entered
		go invoke(0x2, doneB)
		synctest.Wait() // B is through, or waits behind A
		close(d.release)
		for _, done := range []chan error{doneA, doneB} {
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		}
		if last := d.heard[len(d.heard)-1]; s.Mask() != last {
			t.Errorf("Mask 0x%X; the Delegate heard 0x%X last (order %v)", s.Mask(), last, d.heard)
		}
	})
}

// TestWaitingAlarmCommandEndsWithItsContext (R4-conc-4252): a command
// waiting behind a running one returns when its request context ends and
// changes nothing; the host setters do not wait for the command lock, so a
// Delegate may call them.
func TestWaitingAlarmCommandEndsWithItsContext(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s, d := newGated(t)
		doneA := make(chan error, 1)
		go func() {
			_, err := s.MatterInvoke(context.Background(), alarmbase.CmdModifyEnabledAlarms, dwa.ModifyEnabledAlarmsRequest{Mask: 0x1})
			doneA <- err
		}()
		<-d.entered
		ctx, cancel := context.WithCancel(context.Background())
		doneB := make(chan error, 1)
		go func() {
			_, err := s.MatterInvoke(ctx, alarmbase.CmdModifyEnabledAlarms, dwa.ModifyEnabledAlarmsRequest{Mask: 0x2})
			doneB <- err
		}()
		synctest.Wait()
		cancel()
		if err := <-doneB; err == nil {
			t.Error("a ModifyEnabledAlarms whose context ended while it waited was carried out")
		}
		if err := s.SetMask(0x3); err != nil {
			t.Errorf("SetMask while a command runs: %v", err)
		}
		close(d.release)
		if err := <-doneA; err != nil {
			t.Fatal(err)
		}
		if got := s.Mask(); got != 0x1 {
			t.Errorf("Mask 0x%X, want 0x1 (the running command's)", got)
		}
		if len(d.heard) != 1 {
			t.Errorf("the Delegate heard %v; the cancelled command reached it", d.heard)
		}
	})
}

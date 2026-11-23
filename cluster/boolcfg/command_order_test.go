// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package boolcfg_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"testing/synctest"

	"thingfabric.dev/go-fabric/cluster/boolcfg"
	def "thingfabric.dev/go-fabric/cluster/spec/booleanstateconfiguration"
	"thingfabric.dev/go-fabric/contract"
)

// gatedEnableDelegate holds the first AlarmsEnabledChanged it hears until
// release closes, and records every value in the order it heard them.
type gatedEnableDelegate struct {
	mu      sync.Mutex
	heard   []boolcfg.AlarmMode
	entered chan struct{}
	release chan struct{}
}

func (*gatedEnableDelegate) CurrentSensitivityLevelChanged(context.Context, uint8) error { return nil }

func (d *gatedEnableDelegate) AlarmsEnabledChanged(_ context.Context, enabled def.AlarmModeBitmap) error {
	d.mu.Lock()
	d.heard = append(d.heard, enabled)
	first := len(d.heard) == 1
	d.mu.Unlock()
	if first {
		close(d.entered)
		<-d.release
	}
	return nil
}

func (*gatedEnableDelegate) AlarmsActiveChanged(context.Context, def.AlarmModeBitmap) error {
	return nil
}

func (*gatedEnableDelegate) AlarmsSuppressedChanged(context.Context, def.AlarmModeBitmap) error {
	return nil
}

func newGatedEnable(t *testing.T) (*boolcfg.Server, *gatedEnableDelegate) {
	t.Helper()
	d := &gatedEnableDelegate{entered: make(chan struct{}), release: make(chan struct{})}
	srv, err := boolcfg.New(boolcfg.Config{
		Features:        boolcfg.FeatureVisual | boolcfg.FeatureAudible,
		Optional:        boolcfg.OptionalAlarmsEnabled,
		AlarmsSupported: boolcfg.AlarmVisual | boolcfg.AlarmAudible,
		AlarmsEnabled:   boolcfg.AlarmVisual | boolcfg.AlarmAudible,
		Delegate:        d,
	})
	if err != nil {
		t.Fatal(err)
	}
	return srv, d
}

func enableDisable(srv *boolcfg.Server, ctx context.Context, alarms boolcfg.AlarmMode, done chan<- error) {
	_, err := srv.MatterInvoke(ctx, def.CmdEnableDisableAlarm, def.EnableDisableAlarmRequest{AlarmsToEnableDisable: alarms})
	done <- err
}

// TestEnableDisableAlarmStoresInTheOrderTheDelegateHeard (R4-conc-4252):
// two EnableDisableAlarm invokes that run concurrently leave AlarmsEnabled
// at the value the Delegate heard last. Before the fix the second invoke
// ran its Delegate call and its store while the first one's Delegate call
// was still running, and the first one's store then landed last.
func TestEnableDisableAlarmStoresInTheOrderTheDelegateHeard(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		srv, d := newGatedEnable(t)
		doneA, doneB := make(chan error, 1), make(chan error, 1)
		go enableDisable(srv, context.Background(), boolcfg.AlarmVisual, doneA)
		<-d.entered
		go enableDisable(srv, context.Background(), boolcfg.AlarmAudible, doneB)
		synctest.Wait() // B is through, or waits behind A
		close(d.release)
		for _, done := range []chan error{doneA, doneB} {
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		}
		last := d.heard[len(d.heard)-1]
		if got := srv.State().AlarmsEnabled; got != last {
			t.Errorf("AlarmsEnabled 0x%02X; the Delegate heard 0x%02X last (order %v)", got, last, d.heard)
		}
		if got, _ := srv.MatterRead(def.AttrAlarmsEnabled); fmt.Sprint(got) != fmt.Sprint(uint8(last)) {
			t.Errorf("AlarmsEnabled serves %v; the Delegate heard 0x%02X last", got, last)
		}
	})
}

// TestWaitingAlarmCommandEndsWithItsContext (R4-conc-4252): a command
// waiting behind a running one returns when its request context ends and
// reaches neither the Delegate nor the state; a change listener that
// invokes a command on the caller's goroutine is not held by the command
// it was told from.
func TestWaitingAlarmCommandEndsWithItsContext(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		srv, d := newGatedEnable(t)
		doneA := make(chan error, 1)
		go enableDisable(srv, context.Background(), boolcfg.AlarmVisual, doneA)
		<-d.entered
		ctx, cancel := context.WithCancel(context.Background())
		doneB := make(chan error, 1)
		go enableDisable(srv, ctx, boolcfg.AlarmAudible, doneB)
		synctest.Wait()
		cancel()
		if err := <-doneB; err == nil {
			t.Error("an EnableDisableAlarm whose context ended while it waited was carried out")
		}
		again := true
		srv.OnAttributeChange(func(contract.Change) {
			if again {
				again = false
				done := make(chan error, 1)
				enableDisable(srv, context.Background(), boolcfg.AlarmVisual|boolcfg.AlarmAudible, done)
				<-done
			}
		})
		close(d.release)
		if err := <-doneA; err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if got := srv.State().AlarmsEnabled; got != boolcfg.AlarmVisual|boolcfg.AlarmAudible {
			t.Errorf("AlarmsEnabled 0x%02X after the listener's EnableDisableAlarm(0x03)", got)
		}
		if len(d.heard) != 2 || d.heard[1] != boolcfg.AlarmVisual|boolcfg.AlarmAudible {
			t.Errorf("the Delegate heard %v, want [0x01 0x03]", d.heard)
		}
	})
}

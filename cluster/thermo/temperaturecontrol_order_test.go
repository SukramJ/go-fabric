// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package thermo_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"testing/synctest"

	tc "thingfabric.dev/go-fabric/cluster/spec/temperaturecontrol"
	"thingfabric.dev/go-fabric/cluster/thermo"
	"thingfabric.dev/go-fabric/contract"
)

// gatedSetter holds the first SetTemperature it hears until release
// closes, and records every target in the order it heard them.
type gatedSetter struct {
	mu      sync.Mutex
	heard   []int16
	entered chan struct{}
	release chan struct{}
}

func (g *gatedSetter) SetTemperature(_ context.Context, target *int16, _ *uint8) error {
	g.mu.Lock()
	g.heard = append(g.heard, *target)
	first := len(g.heard) == 1
	g.mu.Unlock()
	if first {
		close(g.entered)
		<-g.release
	}
	return nil
}

// TestTemperatureControlCommandsStoreInTheOrderTheSetterHeard
// (R3-conc-3252): two SetTemperature invokes that run concurrently leave
// TemperatureSetpoint at the target the Setter heard last. Before the fix
// the second invoke ran its Setter and its store while the first one's
// Setter was still running, and the first one's store then landed last:
// the setpoint served A while the device took B.
func TestTemperatureControlCommandsStoreInTheOrderTheSetterHeard(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		g := &gatedSetter{entered: make(chan struct{}), release: make(chan struct{})}
		srv := newTC(t, thermo.TemperatureControlConfig{
			Features: thermo.TemperatureControlFeatureNumber | thermo.TemperatureControlFeatureStep,
			Setter:   g, MinTemperature: -2000, MaxTemperature: 500, Step: 100, TemperatureSetpoint: -1800,
		})
		invoke := func(target int16, done chan<- error) {
			_, err := srv.MatterInvoke(context.Background(), tc.CmdSetTemperature, tc.SetTemperatureRequest{TargetTemperature: ptr(target)})
			done <- err
		}
		doneA, doneB := make(chan error, 1), make(chan error, 1)
		go invoke(-1000, doneA)
		<-g.entered
		go invoke(-500, doneB)
		synctest.Wait() // B is through, or waits behind A
		close(g.release)
		for _, done := range []chan error{doneA, doneB} {
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		}
		served, _ := srv.MatterRead(tc.AttrTemperatureSetpoint)
		last := g.heard[len(g.heard)-1]
		if fmt.Sprint(served) != fmt.Sprint(last) {
			t.Errorf("TemperatureSetpoint serves %v; the Setter heard %v last (order %v)", served, last, g.heard)
		}
	})
}

// TestTemperatureControlWaitingCommandEndsWithItsContext (R3-conc-3252):
// a SetTemperature waiting behind a running one returns when its request
// context ends, and a change listener that invokes SetTemperature on the
// caller's goroutine is not held by the command it was told from.
func TestTemperatureControlWaitingCommandEndsWithItsContext(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		g := &gatedSetter{entered: make(chan struct{}), release: make(chan struct{})}
		srv := newTC(t, thermo.TemperatureControlConfig{
			Features: thermo.TemperatureControlFeatureNumber,
			Setter:   g, MinTemperature: -2000, MaxTemperature: 500, TemperatureSetpoint: -1800,
		})
		doneA := make(chan error, 1)
		go func() {
			_, err := srv.MatterInvoke(context.Background(), tc.CmdSetTemperature, tc.SetTemperatureRequest{TargetTemperature: ptr[int16](-1000)})
			doneA <- err
		}()
		<-g.entered
		ctx, cancel := context.WithCancel(context.Background())
		doneB := make(chan error, 1)
		go func() {
			_, err := srv.MatterInvoke(ctx, tc.CmdSetTemperature, tc.SetTemperatureRequest{TargetTemperature: ptr[int16](-500)})
			doneB <- err
		}()
		synctest.Wait()
		cancel()
		if err := <-doneB; err == nil {
			t.Error("a SetTemperature whose context ended while it waited was carried out")
		}
		again := true
		srv.OnAttributeChange(func(contract.Change) {
			if again {
				again = false
				_, _ = srv.MatterInvoke(context.Background(), tc.CmdSetTemperature, tc.SetTemperatureRequest{TargetTemperature: ptr[int16](0)})
			}
		})
		close(g.release)
		if err := <-doneA; err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if served, _ := srv.MatterRead(tc.AttrTemperatureSetpoint); fmt.Sprint(served) != "0" {
			t.Errorf("TemperatureSetpoint serves %v after the listener's SetTemperature(0)", served)
		}
	})
}

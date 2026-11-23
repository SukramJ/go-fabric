// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"sync"
	"testing"
	"time"

	ccldef "thingfabric.dev/go-fabric/cluster/spec/closurecontrol"
	padef "thingfabric.dev/go-fabric/cluster/spec/pushavstreamtransport"
	"thingfabric.dev/go-fabric/surface"
)

// handOff runs callback and then rearm, each queued on mu, so that rearm
// holds mu right after callback's first critical section — the window
// between a timer callback taking its generation and acting on it, when
// it acts after releasing the lock it took under (R2-C2-conc-2602).
//
// A goroutine that unlocks and locks again takes a sync.Mutex before a
// woken waiter can in the mutex's normal mode, so handOff puts it in
// starvation mode, where an Unlock hands the mutex to the waiter at the
// front of the queue and a new Lock queues at the tail: callback queues
// first and rearm second, each longer than the 1 ms that lets a failing
// waiter switch the mode, and the Lock right after the first Unlock takes
// the mutex from the woken callback, which then switches it.
func handOff(mu *sync.Mutex, callback, rearm func()) {
	const starve = 3 * time.Millisecond
	var wg sync.WaitGroup
	mu.Lock()
	wg.Go(callback)
	time.Sleep(starve)
	wg.Go(rearm)
	time.Sleep(starve)
	mu.Unlock()
	mu.Lock()
	time.Sleep(starve)
	mu.Unlock()
	wg.Wait()
}

// windowRounds is how often each test runs its interleaving.
const windowRounds = 10

// TestGarageReportsActUnderTheLockThatTookThem holds R2-C2-conc-2602 on
// the garage, through its ClosureControl server: an arrival (a
// calibration's end) that took its generation reports Stopped, and a
// MoveTo (a Stop and a Calibrate) the server is invoking meanwhile must
// still leave the new action's Moving (Calibrating) — whether the report
// lands after the command, over the server's Moving, or while the drive
// has the command, where the server keeps the drive's report.
func TestGarageReportsActUnderTheLockThatTookThem(t *testing.T) {
	t.Parallel()
	var travel, calibration heldTimers
	g := newDemoGarage("garage")
	g.travel.after, g.calibration.after = travel.after, calibration.after
	opts := bridged("garage", g.name)
	opts.Surface = surface.Policies{Extensions: surface.NewExtensions(closureRow)}
	d, err := g.device(opts)
	if err != nil {
		t.Fatal(err)
	}
	srv := d.Server(ccldef.ClusterID)
	ctx := context.Background()
	invoke := func(cmd uint32, req any) {
		if _, err := srv.MatterInvoke(ctx, cmd, req); err != nil {
			t.Error(err)
		}
	}
	stopped := ccldef.MainStateStopped
	rest := func() {
		g.mu.Lock()
		g.travel.stopLocked()
		g.calibration.stopLocked()
		g.mu.Unlock()
		if err := g.server().Set(ccldef.Patch{MainState: &stopped}); err != nil {
			t.Fatal(err)
		}
	}
	open, closed := ccldef.TargetPositionMoveToFullyOpen, ccldef.TargetPositionMoveToFullyClosed
	for i := range windowRounds {
		invoke(ccldef.CmdMoveTo, ccldef.MoveToRequest{Position: &open})
		handOff(&g.mu, travel.fns[len(travel.fns)-1], func() {
			invoke(ccldef.CmdMoveTo, ccldef.MoveToRequest{Position: &closed})
		})
		if st := g.server().Get().MainState; st != ccldef.MainStateMoving {
			t.Fatalf("travel round %d: MainState %v while the second travel runs, want Moving", i, st)
		}
		rest()
	}
	for i := range windowRounds {
		invoke(ccldef.CmdCalibrate, nil)
		// The server answers a Calibrate while Calibrating without a call
		// (invokeCalibrate); a Stop first lets the second run start.
		handOff(&g.mu, calibration.fns[len(calibration.fns)-1], func() {
			invoke(ccldef.CmdStop, nil)
			invoke(ccldef.CmdCalibrate, nil)
		})
		if st := g.server().Get().MainState; st != ccldef.MainStateCalibrating {
			t.Fatalf("calibration round %d: MainState %v while the second run calibrates, want Calibrating", i, st)
		}
		rest()
	}
}

// TestCameraRecordingEndActsUnderTheLockThatTookIt holds R2-C2-conc-2602
// on the push transport: a recording's end that took its generation must
// not end the recording a trigger re-armed meanwhile.
func TestCameraRecordingEndActsUnderTheLockThatTookIt(t *testing.T) {
	t.Parallel()
	c, _, rec := simCamera(t, cameraOptions{}, func(c *demoCamera) { c.restored.Push = simConnections() })
	go func() {
		for range rec.ch { //nolint:revive // drains the events this test does not read
		}
	}()
	port := camPushPort{c}
	ctx := context.Background()
	if err := port.SetTransportStatus(ctx, []uint16{3}, padef.TransportStatusActive); err != nil {
		t.Fatal(err)
	}
	var ends heldTimers
	c.mu.Lock()
	tr := c.transports[3]
	tr.stop.after = ends.after
	c.mu.Unlock()
	reason := padef.TriggerActivationReasonUserInitiated
	for i := range windowRounds {
		if err := port.ManuallyTriggerTransport(ctx, 3, reason, nil); err != nil {
			t.Fatal(err)
		}
		c.mu.Lock()
		end := ends.fns[len(ends.fns)-1]
		c.mu.Unlock()
		// beginTransport is ManuallyTriggerTransport's arming, in one
		// critical section.
		handOff(&c.mu, end, func() { c.beginTransport(3, &reason, time.Second) })
		c.mu.Lock()
		streaming, armed := tr.streaming, tr.stop.armedLocked()
		c.mu.Unlock()
		if !streaming || !armed {
			t.Fatalf("round %d: the recording re-armed after the end took its generation: streaming %v, armed %v", i, streaming, armed)
		}
	}
}

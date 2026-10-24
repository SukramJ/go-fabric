// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package inflight_test

import (
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/internal/inflight"
)

func TestGroupCounts(t *testing.T) {
	t.Parallel()
	g := inflight.New("device 1")
	if g.Busy() || len(g.Snapshot()) != 0 || g.Name() != "device 1" {
		t.Fatal("a new group is not empty")
	}
	var wg sync.WaitGroup
	for range 64 {
		wg.Go(func() {
			g.Enter(inflight.UDPDatagrams)
			g.Enter(inflight.ExchangeDelivered)
			g.Leave(inflight.UDPDatagrams)
		})
	}
	wg.Wait()
	if got := g.Snapshot(); len(got) != 1 || got[inflight.ExchangeDelivered] != 64 {
		t.Fatalf("snapshot %v", got)
	}
	if !g.Busy() {
		t.Fatal("not busy with work in hand")
	}
	before := g.Changes()
	g.Add(inflight.ExchangeDelivered, -64)
	if g.Busy() || g.Changes() != before+1 {
		t.Fatalf("busy %v, changes %d after %d", g.Busy(), g.Changes(), before)
	}
	g.Add(inflight.ExchangeDelivered, 0)
	if g.Changes() != before+1 {
		t.Fatal("an Add of zero counted as a change")
	}
}

func TestNilGroupIsANoop(t *testing.T) {
	t.Parallel()
	var g *inflight.Group
	g.Enter(inflight.NodeStart)
	g.Leave(inflight.NodeStart)
	if g.Busy() || g.Snapshot() != nil || g.Changes() != 0 || g.Name() != "" {
		t.Fatal("a nil group counted")
	}
}

func TestClockCarriesGroup(t *testing.T) {
	t.Parallel()
	f := clocktest.New(time.Unix(0, 0))
	g := inflight.New("controller 2")
	c := inflight.WithGroup(f, g)
	if inflight.Of(c) != g || inflight.Unwrap(c) != clock.Clock(f) {
		t.Fatal("the carrier lost its group or its clock")
	}
	if inflight.Of(f) != nil || inflight.Of(clock.System{}) != nil || inflight.Of(nil) != nil {
		t.Fatal("a plain clock carries a group")
	}
	if inflight.Unwrap(f) != clock.Clock(f) {
		t.Fatal("Unwrap of a plain clock is not the clock")
	}
	fired := make(chan struct{})
	c.AfterFunc(time.Second, func() { close(fired) })
	f.Advance(time.Second)
	<-fired
	if !c.Now().Equal(f.Now()) {
		t.Fatal("the carrier tells another time")
	}
	if inflight.Of(inflight.WithGroup(nil, g)) != g {
		t.Fatal("a carrier of the system clock lost its group")
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package alarm_test

import (
	"slices"
	"sync"
	"testing"
	"testing/synctest"

	"thingfabric.dev/go-fabric/cluster/alarm"
	smokedef "thingfabric.dev/go-fabric/cluster/spec/smokecoalarm"
	"thingfabric.dev/go-fabric/contract"
)

// gatedSource answers SmokeCOState; when armed, the next caller takes the
// state it read and waits for release before it returns it.
type gatedSource struct {
	mu      sync.Mutex
	st      smokedef.State
	armed   bool
	entered chan struct{}
	release chan struct{}
}

func (g *gatedSource) SmokeCOState() smokedef.State {
	g.mu.Lock()
	st, block := g.st, g.armed
	g.armed = false
	g.mu.Unlock()
	if block {
		close(g.entered)
		<-g.release
	}
	return st
}

func (g *gatedSource) set(state smokedef.AlarmStateEnum, arm bool) {
	g.mu.Lock()
	g.st.SmokeState, g.armed = state, arm
	g.mu.Unlock()
}

type orderedEvents struct {
	mu  sync.Mutex
	ids []uint32
}

func (p *orderedEvents) MatterEmitEvent(_ uint16, _, event uint32, _ any, _ contract.EventPriority) {
	p.mu.Lock()
	p.ids = append(p.ids, event)
	p.mu.Unlock()
}

func (p *orderedEvents) seen() []uint32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.ids)
}

// TestOverlappingRefreshesKeepTheNewestBaseline (R2-P6-conc-1503): a
// Refresh that read Warning and a later one that read Normal overlap; the
// later one runs after the first, so the events follow the device —
// SmokeAlarm, then AllClear — and the baseline ends at Normal, where the
// next Refresh finds nothing to report.
func TestOverlappingRefreshesKeepTheNewestBaseline(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		src := &gatedSource{entered: make(chan struct{}), release: make(chan struct{})}
		srv, err := alarm.NewServer(alarm.Config{Source: src, Features: alarm.FeatureSmokeAlarm})
		if err != nil {
			t.Fatal(err)
		}
		ev := &orderedEvents{}
		srv.SetMatterEventEmitter(ev)
		srv.SetEndpoint(1)
		srv.Refresh() // baseline: Normal
		src.set(smokedef.AlarmStateWarning, true)
		first := make(chan struct{})
		go func() { srv.Refresh(); close(first) }() // reads Warning, then stalls
		<-src.entered
		src.set(smokedef.AlarmStateNormal, false) // the alarm cleared
		second := make(chan struct{})
		go func() { srv.Refresh(); close(second) }() // the notifier: reads Normal
		synctest.Wait()
		close(src.release)
		<-first
		<-second
		want := []uint32{alarm.EventSmokeAlarm, alarm.EventAllClear}
		if got := ev.seen(); !slices.Equal(got, want) {
			t.Fatalf("events %v after both refreshes, want %v (SmokeAlarm, then AllClear)", got, want)
		}
		srv.Refresh()
		if got := ev.seen(); !slices.Equal(got, want) {
			t.Errorf("events %v after a further Refresh: the baseline fell back to Warning", got)
		}
	})
}

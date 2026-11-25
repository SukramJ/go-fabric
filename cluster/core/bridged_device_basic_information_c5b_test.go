// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/contract"
)

// reachHoldA2 records the ReachableChanged values and holds the first
// emit until release is closed.
type reachHoldA2 struct {
	mu      sync.Mutex
	values  []bool
	first   bool
	entered chan struct{}
	release chan struct{}
}

func (e *reachHoldA2) MatterEmitEvent(_ uint16, _, _ uint32, data any, _ contract.EventPriority) {
	e.mu.Lock()
	hold := !e.first
	e.first = true
	e.mu.Unlock()
	if hold {
		close(e.entered)
		<-e.release
	}
	e.mu.Lock()
	e.values = append(e.values, data.(core.ReachableChangedEvent).ReachableNewValue)
	e.mu.Unlock()
}

// TestBridgedReachableLastEventIsTheFinalValue (review II, R4-conc-4156):
// two SetReachable calls that interleave — the second runs while the
// first's event is on its way — leave the event log ending on the value
// the attribute holds: the compare, the bump and the emit are one step.
func TestBridgedReachableLastEventIsTheFinalValue(t *testing.T) {
	t.Parallel()
	cfg := validBridgedConfig()
	cfg.Reachable = false
	b, err := core.NewBridgedDeviceBasicInformation(cfg)
	if err != nil {
		t.Fatal(err)
	}
	e := &reachHoldA2{entered: make(chan struct{}), release: make(chan struct{})}
	b.SetMatterEventEmitter(e)

	first := make(chan struct{})
	go func() {
		defer close(first)
		b.SetReachable(true)
	}()
	<-e.entered

	header := make(chan string, 1)
	second := make(chan struct{})
	go func() {
		defer close(second)
		header <- goroutineHeaderA2()
		b.SetReachable(false)
	}()
	waitLockedOrDoneA2(t, <-header, "SetReachable", second)
	close(e.release)
	<-first
	<-second

	got, _ := b.MatterRead(0x0011)
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.values) == 0 || e.values[len(e.values)-1] != got {
		t.Fatalf("events %v end off the attribute's value %v", e.values, got)
	}
}

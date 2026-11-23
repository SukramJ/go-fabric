// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package servicearea_test

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/cluster/servicearea"
	sa "thingfabric.dev/go-fabric/cluster/spec/servicearea"
)

// orderingSelector records the selections in the order the device took
// them, and holds its answer to the first until release closes.
type orderingSelector struct {
	mu       sync.Mutex
	calls    int
	accepted [][]uint32
	first    chan struct{} // closed when the first selection is entered
	second   chan struct{} // closed when the second selection is entered
	release  chan struct{}
}

func (o *orderingSelector) SelectAreas(_ context.Context, areas []uint32) (servicearea.SelectAreasStatus, string, error) {
	o.mu.Lock()
	o.calls++
	n := o.calls
	o.accepted = append(o.accepted, areas)
	o.mu.Unlock()
	switch n {
	case 1: // taken; the device answers once release closes
		close(o.first)
		<-o.release
	case 2:
		close(o.second)
	}
	return servicearea.SelectAreasSuccess, "", nil
}

// TestSelectAreasStoresTheSelectionTheDeviceAcceptedLast
// (R2-C2-conc-1554): two concurrent SelectAreas are carried out one after
// the other, so SelectedAreas is the selection the device accepted last.
// Before the fix the second stored while the first was still with the
// device, and the first then overwrote it.
func TestSelectAreasStoresTheSelectionTheDeviceAcceptedLast(t *testing.T) {
	t.Parallel()
	sel := &orderingSelector{first: make(chan struct{}), second: make(chan struct{}), release: make(chan struct{})}
	cfg := vacuum(&device{})
	cfg.Selector = sel
	srv := newServer(t, cfg)
	invoke := func(areas ...uint32) <-chan error {
		done := make(chan error, 1)
		go func() {
			_, err := srv.MatterInvoke(context.Background(), sa.CmdSelectAreas, sa.SelectAreasRequest{NewAreas: areas})
			done <- err
		}()
		return done
	}
	firstDone := invoke(7)
	<-sel.first
	secondDone := invoke(9)
	select {
	case <-sel.second: // before the fix: the second reaches the device at once
	case <-time.After(200 * time.Millisecond):
	}
	close(sel.release)
	for _, done := range []<-chan error{firstDone, secondDone} {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	sel.mu.Lock()
	last := sel.accepted[len(sel.accepted)-1]
	sel.mu.Unlock()
	v, _ := srv.MatterRead(sa.AttrSelectedAreas)
	if got := v.([]uint32); !slices.Equal(got, last) {
		t.Errorf("SelectedAreas %v, want %v, the selection the device accepted last", got, last)
	}
}

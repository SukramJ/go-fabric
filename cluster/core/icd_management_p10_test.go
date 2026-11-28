// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"sync/atomic"
	"testing"

	"thingfabric.dev/go-fabric/cluster/core"
	"thingfabric.dev/go-fabric/store"
	"thingfabric.dev/go-fabric/store/memstore"
)

// icdListCountP10 counts ListICDRegistrations and holds the first one —
// after it read the rows — until release is closed.
type icdListCountP10 struct {
	*memstore.Store
	lists   atomic.Int32
	held    chan struct{}
	release chan struct{}
}

func (s *icdListCountP10) ListICDRegistrations(ctx context.Context) ([]store.ICDRegistration, error) {
	regs, err := s.Store.ListICDRegistrations(ctx)
	if s.lists.Add(1) == 1 {
		close(s.held)
		<-s.release
	}
	return regs, err
}

// TestICDParallelStartLoadsOnce (review II, P10-conc-6101): a Start that
// waits for the command lock while another Start loads finds the server
// started once it gets the lock, and loads and swaps nothing.
func TestICDParallelStartLoadsOnce(t *testing.T) {
	t.Parallel()
	hs := &icdListCountP10{Store: icdStore(t), held: make(chan struct{}), release: make(chan struct{})}
	s := newICDServer(t, core.ICDManagementConfig{Store: hs})

	first := make(chan error, 1)
	go func() { first <- s.Start(context.Background()) }()
	<-hs.held

	header := make(chan string, 1)
	second := make(chan error, 1)
	secondDone := make(chan struct{})
	go func() {
		defer close(secondDone)
		header <- goroutineHeaderA2()
		second <- s.Start(context.Background())
	}()
	waitLockedOrDoneA2(t, <-header, "(*ICDManagement).Start", secondDone)
	close(hs.release)
	for _, ch := range []chan error{first, second} {
		if err := <-ch; err != nil {
			t.Fatal(err)
		}
	}
	if n := hs.lists.Load(); n != 1 {
		t.Fatalf("ListICDRegistrations ran %d times for two parallel Starts, want once", n)
	}
}

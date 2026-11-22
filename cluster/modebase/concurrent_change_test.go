// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package modebase_test

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	"thingfabric.dev/go-fabric/cluster/modebase"
	"thingfabric.dev/go-fabric/cluster/spec/rvcrunmode"
	clusterwire "thingfabric.dev/go-fabric/cluster/wire"
)

// blockingChanger holds its first change until release is closed.
type blockingChanger struct {
	calls   atomic.Int32
	entered chan struct{}
	release chan struct{}
}

func (c *blockingChanger) ChangeToMode(context.Context, uint8) (modebase.Status, string, error) {
	if c.calls.Add(1) == 1 {
		close(c.entered)
		<-c.release
	}
	return modebase.StatusSuccess, "", nil
}

// TestConcurrentChangeToModeIsOneAtATime holds R2-P6-conc-1414: a
// ChangeToMode reads CurrentMode, checks it, calls the host and stores as
// one step. Two concurrent changes from Idle to two non-idle RvcRunMode
// modes cannot both pass the "only from idle" rule: the second sees the
// first's mode and answers InvalidInMode without reaching the host.
func TestConcurrentChangeToModeIsOneAtATime(t *testing.T) {
	c := &blockingChanger{entered: make(chan struct{}), release: make(chan struct{})}
	srv, err := modebase.NewRvcRunMode(modebase.Config{Changer: c, SupportedModes: runModes})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var second clusterwire.ChangeToModeResponse
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = srv.MatterInvoke(context.Background(), modebase.CmdChangeToMode, rvcrunmode.ChangeToModeRequest{NewMode: 1})
	}()
	<-c.entered
	go func() {
		defer wg.Done()
		resp, err := srv.MatterInvoke(context.Background(), modebase.CmdChangeToMode, rvcrunmode.ChangeToModeRequest{NewMode: 2})
		if err != nil {
			t.Error(err)
			return
		}
		second, _ = resp.(clusterwire.ChangeToModeResponse)
	}()
	for i := 0; i < 10000 && c.calls.Load() < 2; i++ {
		runtime.Gosched()
	}
	close(c.release)
	wg.Wait()
	if n := c.calls.Load(); n != 1 {
		t.Errorf("the host was asked %d times, want 1", n)
	}
	if second.Status != uint8(modebase.StatusInvalidInMode) {
		t.Errorf("the second change answered %+v, want InvalidInMode", second)
	}
}

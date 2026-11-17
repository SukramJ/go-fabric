// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package opstate_test

import (
	"context"
	"sync"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/opstate"
	"github.com/SukramJ/go-fabric/contract"
)

// overlapHost answers Stop at once and holds Pause until release; each
// moves the state through the host setter, as a device's handler does.
type overlapHost struct {
	srv     *opstate.Server
	entered chan struct{}
	release chan struct{}
}

func (h *overlapHost) HandleOperationalCommand(_ context.Context, cmd opstate.Command) (opstate.ErrorState, error) {
	switch cmd {
	case opstate.CommandPause:
		close(h.entered)
		<-h.release
		return opstate.ErrorState{}, h.srv.SetOperationalState(opstate.StatePaused)
	default:
		return opstate.ErrorState{}, h.srv.SetOperationalState(opstate.StateStopped)
	}
}

// TestACommandThatEndsKeepsAnotherCommandsOrigin (R2-P6-conc-1509): the
// changes a command's handler makes are the command's, though another
// command ended while it ran.
func TestACommandThatEndsKeepsAnotherCommandsOrigin(t *testing.T) {
	t.Parallel()
	h := &overlapHost{entered: make(chan struct{}), release: make(chan struct{})}
	srv, err := opstate.NewServer(opstate.Config{
		Handler: h, Commands: opstate.CommandPause | opstate.CommandResume | opstate.CommandStop,
		States: []opstate.StateEntry{{ID: opstate.StateStopped}, {ID: opstate.StateRunning}, {ID: opstate.StatePaused}, {ID: opstate.StateError}},
		State:  opstate.StateRunning,
	})
	if err != nil {
		t.Fatal(err)
	}
	h.srv = srv
	var mu sync.Mutex
	var origins []contract.Origin
	srv.OnAttributeChange(func(c contract.Change) {
		mu.Lock()
		origins = append(origins, c.Origin)
		mu.Unlock()
	})
	paused := make(chan error, 1)
	go func() {
		_, err := srv.MatterInvoke(context.Background(), opstate.CmdPause, nil)
		paused <- err
	}()
	<-h.entered
	if _, err := srv.MatterInvoke(context.Background(), opstate.CmdStop, nil); err != nil {
		t.Fatal(err)
	}
	close(h.release)
	if err := <-paused; err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(origins) != 2 {
		t.Fatalf("%d changes, want Stop's and Pause's", len(origins))
	}
	for i, o := range origins {
		if o != contract.OriginCommand {
			t.Errorf("change %d has origin %v, want the command's", i, o)
		}
	}
}

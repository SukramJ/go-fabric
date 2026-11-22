// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package closure_test

import (
	"context"
	"sync"
	"testing"
	"testing/synctest"

	"thingfabric.dev/go-fabric/cluster/closure"
	closuredef "thingfabric.dev/go-fabric/cluster/spec/closurecontrol"
	"thingfabric.dev/go-fabric/cluster/wire"
)

// blockingMover holds the first MoveTo until release and counts the
// calls inside it at once.
type blockingMover struct {
	mu      sync.Mutex
	inside  int
	most    int
	calls   int
	entered chan struct{}
	release chan struct{}
	targets []closuredef.TargetPositionEnum
}

func (m *blockingMover) MoveTo(_ context.Context, p closuredef.TargetPositionEnum) error {
	m.mu.Lock()
	m.inside++
	m.most = max(m.most, m.inside)
	m.calls++
	first := m.calls == 1
	m.targets = append(m.targets, p)
	m.mu.Unlock()
	if first {
		close(m.entered)
		<-m.release
	}
	m.mu.Lock()
	m.inside--
	m.mu.Unlock()
	return nil
}

func (*blockingMover) Stop(context.Context) error { return nil }

// TestClosureControlCommandsRunOneAtATime (R2-P6-conc-1511): a MoveTo
// that arrives while another holds the mover waits for it; the mover is
// never driven by two commands at once, and the target recorded last is
// the command that ran last.
func TestClosureControlCommandsRunOneAtATime(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		m := &blockingMover{entered: make(chan struct{}), release: make(chan struct{})}
		srv, err := closure.New(closure.Config{FeatureMap: wire.ClosureControlFeaturePositioning, Mover: m})
		if err != nil {
			t.Fatal(err)
		}
		srv.SetMainState(wire.ClosureMainStateStopped)
		open, closed := closuredef.TargetPositionMoveToFullyOpen, closuredef.TargetPositionMoveToFullyClosed
		first := make(chan error, 1)
		go func() {
			_, err := srv.MatterInvoke(context.Background(), closuredef.CmdMoveTo, closuredef.MoveToRequest{Position: &open})
			first <- err
		}()
		<-m.entered
		second := make(chan error, 1)
		go func() {
			_, err := srv.MatterInvoke(context.Background(), closuredef.CmdMoveTo, closuredef.MoveToRequest{Position: &closed})
			second <- err
		}()
		synctest.Wait()
		m.mu.Lock()
		calls := m.calls
		m.mu.Unlock()
		if calls != 1 {
			t.Errorf("the second MoveTo reached the mover while the first held it (%d calls)", calls)
		}
		close(m.release)
		if err := <-first; err != nil {
			t.Fatal(err)
		}
		if err := <-second; err != nil {
			t.Fatal(err)
		}
		if m.most != 1 {
			t.Errorf("%d MoveTo calls inside the mover at once, want 1", m.most)
		}
		v, _ := srv.MatterRead(wire.ClosureControlAttrOverallTargetState)
		if got := v.(*wire.ClosureOverallTargetState).Position; got == nil || *got != wire.ClosureTargetPositionMoveToFullyClosed {
			t.Errorf("OverallTargetState.Position %v, want the second MoveTo's FullyClosed", got)
		}
	})
}

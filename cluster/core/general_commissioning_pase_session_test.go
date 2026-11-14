// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core_test

import (
	"context"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/core"
)

// TestGencomm_PASESessionIsCoveredOrClosed is R2-P3-pase-03: a PASE
// session is never left open without a fail-safe — the rollback in flight
// closes it, or its arm arms, or the arm's refusal reaches the caller,
// which closes it (connectedhomeip: "Don't allow a PASE session to hang
// around without a fail-safe", src/app/server/CommissioningWindowManager.cpp:240-241
// at 6170af84).
//
// The rolled-back hook plays the bridge's PASE close
// (bridge/pase_close.go paseSessionsCloser): it names the PASE sessions
// open when it starts and closes exactly those. Session 2 is registered
// after that, as a Pake3 completing on another dispatch goroutine is, and
// the caller closes it when its arm is refused, as the node does.
func TestGencomm_PASESessionIsCoveredOrClosed(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		clk := clocktest.New(time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC))
		gc := newGencomm(t, core.GeneralCommissioningConfig{Clock: clk})
		var (
			mu   sync.Mutex
			open = map[int]bool{1: true}
		)
		closeSession := func(id int) {
			mu.Lock()
			delete(open, id)
			mu.Unlock()
		}
		named := make(chan struct{})
		release := make(chan struct{})
		gc.SetOnFailSafeRolledBack(func(context.Context) {
			mu.Lock()
			ids := make([]int, 0, len(open))
			for id := range open {
				ids = append(ids, id)
			}
			mu.Unlock()
			close(named)
			<-release
			for _, id := range ids {
				closeSession(id)
			}
		})
		if err := gc.ArmOnPaseEstablished(context.Background()); err != nil {
			t.Fatalf("session 1's arm: %v", err)
		}
		// The fail-safe ends (RevokeCommissioning's ExpireFailSafe; the
		// timer and ArmFailSafe(0) run the same hooks).
		expired := make(chan struct{})
		go func() { gc.ExpireFailSafe(context.Background()); close(expired) }()
		<-named
		mu.Lock()
		open[2] = true
		mu.Unlock()
		armed := make(chan error, 1)
		go func() { armed <- gc.ArmOnPaseEstablished(context.Background()) }()
		synctest.Wait() // the arm waits for the rollback in flight
		close(release)
		<-expired
		if err := <-armed; err != nil {
			closeSession(2)
		}
		mu.Lock()
		stillOpen := open[2]
		mu.Unlock()
		if _, ok := gc.FailSafeFabric(); stillOpen && !ok {
			t.Fatal("PASE session 2 is open without a fail-safe: the rollback's close did not name it, and its arm armed nothing yet reported no refusal")
		}
		_ = gc.ArmFailSafeFor(context.Background(), 0, 0)
	})
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package core

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// waitClosed returns once Close has set closed.
func waitClosed(t *testing.T, g *GeneralCommissioning) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		g.mu.Lock()
		c := g.closed
		g.mu.Unlock()
		if c {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("Close did not set closed")
		}
		time.Sleep(100 * time.Microsecond)
	}
}

// TestGeneralCommissioningArmWaitingForARollbackRefusedAfterClose
// (R2-C2-conc-1002): an arm that passed the closed check and waits for a
// rollback in flight is refused with errGencommClosed when Close set
// closed meanwhile, and the server is not armed after it.
func TestGeneralCommissioningArmWaitingForARollbackRefusedAfterClose(t *testing.T) {
	t.Parallel()
	g, err := NewGeneralCommissioning(GeneralCommissioningConfig{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	g.SetFailSafeSessionCloser(func(context.Context) []uint16 {
		once.Do(func() { close(entered); <-release })
		return nil
	})
	if err := g.ArmFailSafeFor(ctx, 60, 1); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); g.ExpireFailSafe(ctx) }() // the rollback in flight
	<-entered
	armErr := make(chan error, 1)
	go func() { armErr <- g.ArmFailSafeFor(ctx, 60, 1) }() // waits for the rollback
	time.Sleep(2 * time.Millisecond)
	wg.Add(1)
	go func() { defer wg.Done(); _ = g.Close(ctx) }()
	waitClosed(t, g)
	close(release)
	if err := <-armErr; !errors.Is(err, errGencommClosed) {
		t.Errorf("arm across Close: %v, want errGencommClosed", err)
	}
	wg.Wait()
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.failSafeArmed {
		t.Error("the fail-safe is armed after Close")
	}
}

// TestGeneralCommissioningArmRunningTheStartHookRefusedAfterClose
// (R2-C2-conc-1002): an arm whose fresh-start hook runs when Close sets
// closed is refused with errGencommClosed and arms nothing.
func TestGeneralCommissioningArmRunningTheStartHookRefusedAfterClose(t *testing.T) {
	t.Parallel()
	g, err := NewGeneralCommissioning(GeneralCommissioningConfig{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	g.SetOnFailSafeStarting(func(context.Context) []uint8 {
		once.Do(func() { close(entered); <-release })
		return nil
	})
	armErr := make(chan error, 1)
	go func() { armErr <- g.ArmFailSafeFor(ctx, 60, 1) }()
	<-entered
	closeDone := make(chan struct{})
	go func() { defer close(closeDone); _ = g.Close(ctx) }()
	waitClosed(t, g)
	close(release)
	if err := <-armErr; !errors.Is(err, errGencommClosed) {
		t.Errorf("arm across Close: %v, want errGencommClosed", err)
	}
	<-closeDone
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.failSafeArmed {
		t.Error("the fail-safe is armed after Close")
	}
}

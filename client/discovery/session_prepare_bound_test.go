// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package discovery_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"thingfabric.dev/go-fabric/client/discovery"
)

// TestSession_ConcurrentPreparationsAreBounded: a transport that reports
// far more devices than MaxConcurrentPreparations while every Prepare
// waits sees its found callback wait once that many run — the
// preparations are not started one goroutine per device — and once they
// may finish, every device reported is prepared and handed to the
// discovery: none is dropped (review II, R3-bound-3552, owner decision
// 563).
func TestSession_ConcurrentPreparationsAreBounded(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		const devices = 200
		f := make(feed)
		gate := make(chan struct{})
		var mu sync.Mutex
		running, most, prepared := 0, 0, 0
		found := map[string]bool{}
		s := discovery.FindAll(t.Context(), []discovery.Transport{f}, discovery.SessionOptions{
			Prepare: func(context.Context, discovery.Candidate) error {
				mu.Lock()
				running++
				most = max(most, running)
				mu.Unlock()
				<-gate
				mu.Lock()
				running--
				prepared++
				mu.Unlock()
				return nil
			},
		}, func(c discovery.Candidate) {
			mu.Lock()
			found[c.Device.InstanceID] = true
			mu.Unlock()
		})
		sent := 0
		sender := make(chan struct{})
		go func() {
			defer close(sender)
			for i := range devices {
				f <- discovery.Device{InstanceID: fmt.Sprintf("dev-%03d", i)}
				mu.Lock()
				sent++
				mu.Unlock()
			}
		}()
		synctest.Wait()
		mu.Lock()
		if most > discovery.MaxConcurrentPreparations {
			t.Errorf("%d preparations ran at once, want at most %d", most, discovery.MaxConcurrentPreparations)
		}
		if sent >= devices {
			t.Errorf("the transport reported all %d devices while every preparation waits, want its found callback to wait", devices)
		}
		mu.Unlock()
		close(gate)
		<-sender
		synctest.Wait()
		s.Stop()
		if _, err := s.Wait(t.Context()); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		defer mu.Unlock()
		if prepared != devices || len(found) != devices {
			t.Fatalf("%d devices prepared and %d found of %d reported, want every one", prepared, len(found), devices)
		}
		if most > discovery.MaxConcurrentPreparations {
			t.Fatalf("%d preparations ran at once, want at most %d", most, discovery.MaxConcurrentPreparations)
		}
	})
}

// backlog is a transport that hands its devices to found one after
// another without reading ctx between them — as Scanner.find's deliver
// hands a drained batch — then waits for the end of the search.
type backlog []discovery.Device

func (backlog) Kind() discovery.TransportKind { return discovery.TransportIP }

func (b backlog) FindContinuously(ctx context.Context, _ discovery.Identifier, found func(discovery.Device)) error {
	for _, d := range b {
		found(d)
	}
	<-ctx.Done()
	return nil
}

// TestSession_PreparationBacklogEndsWithTheSearch: a transport hands 200
// devices at once to a discovery whose Prepare takes 1 s; the search's
// 2 s Timeout still ends the discovery within one more Prepare time. A
// device still waiting for a preparation slot when the search stops is
// not prepared, so the backlog does not hold the discovery past its end
// (review II, R3-bound-3552, Fable diff read of decision 563: at
// 68323f566 the discovery ended after 13 s).
func TestSession_PreparationBacklogEndsWithTheSearch(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		const (
			devices = 200
			timeout = 2 * time.Second
			prepare = time.Second
		)
		b := make(backlog, devices)
		for i := range b {
			b[i] = discovery.Device{InstanceID: fmt.Sprintf("dev-%03d", i)}
		}
		start := time.Now()
		var mu sync.Mutex
		prepared, found := 0, 0
		s := discovery.FindAll(t.Context(), []discovery.Transport{b}, discovery.SessionOptions{
			Timeout: timeout,
			Prepare: func(context.Context, discovery.Candidate) error {
				time.Sleep(prepare)
				mu.Lock()
				prepared++
				mu.Unlock()
				return nil
			},
		}, func(discovery.Candidate) {
			mu.Lock()
			found++
			mu.Unlock()
		})
		<-s.Done()
		if took := time.Since(start); took > timeout+prepare {
			t.Fatalf("the discovery ended after %v, want within its %v Timeout and one %v Prepare", took, timeout, prepare)
		}
		mu.Lock()
		defer mu.Unlock()
		// The search ran for two Prepare times: two rounds of
		// MaxConcurrentPreparations devices were prepared and handed over,
		// and a third when the slots freed at 2 s are taken before the
		// Timeout stops the search at the same instant.
		lo, hi := 2*discovery.MaxConcurrentPreparations, 3*discovery.MaxConcurrentPreparations
		if prepared != found || prepared < lo || prepared > hi {
			t.Fatalf("%d devices prepared and %d found, want the same number, %d to %d", prepared, found, lo, hi)
		}
	})
}

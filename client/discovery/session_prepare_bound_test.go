// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package discovery_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"testing/synctest"

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

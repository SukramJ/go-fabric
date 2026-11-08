// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package brdiscovery

import (
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
)

// TestRegistryNilReceiver: a nil *Registry — the controller's when its
// registry is disabled — lists nothing, finds nothing, never calls an
// observer, and unsubscribes and closes without effect.
func TestRegistryNilReceiver(t *testing.T) {
	t.Parallel()
	var r *Registry
	if got := r.List(); len(got) != 0 {
		t.Fatalf("List = %v, want none", got)
	}
	if e, ok := r.Get(brXA); ok {
		t.Fatalf("Get = %v, true; want not found", e)
	}
	unsubscribe := r.OnChange(func(Event) { t.Error("observer called on a nil registry") })
	if unsubscribe == nil {
		t.Fatal("OnChange returned a nil unsubscribe")
	}
	unsubscribe()
	r.Close()
	r.Close()
}

// TestRegistryCloseStopsDelivery: an event is being delivered — the first
// observer is still running — when Close is called on another goroutine;
// once Close returned, the second observer is not called with it.
func TestRegistryCloseStopsDelivery(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		stub := &stubNames{}
		r := newRegistry(stub, Config{Clock: clocktest.New(time.Unix(1000, 0))})
		entered, release := make(chan struct{}), make(chan struct{})
		r.OnChange(func(Event) {
			close(entered)
			<-release
		})
		var late atomic.Int32
		r.OnChange(func(Event) { late.Add(1) })

		go stub.discover(t, brMeshcop, txt{"xa": brXA, "nn": "Home"}, "otbr.local")
		<-entered
		r.Close()
		close(release)
		synctest.Wait()
		if n := late.Load(); n != 0 {
			t.Fatalf("an observer was called %d time(s) after Close returned", n)
		}
	})
}

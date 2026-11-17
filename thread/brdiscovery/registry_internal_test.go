// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package brdiscovery

import (
	"fmt"
	"strings"
	"sync"
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

		st := instanceState(t, brMeshcop, txt{"xa": brXA, "nn": "Home"}, "otbr.local")
		go stub.fn(st)
		<-entered
		r.Close()
		close(release)
		synctest.Wait()
		if n := late.Load(); n != 0 {
			t.Fatalf("an observer was called %d time(s) after Close returned", n)
		}
	})
}

// TestRegistryInstanceCap: the instances the registry watches are capped
// at maxInstances; a new instance past it evicts the instance without a
// valid xa watched first, then the oldest entry with its instances, and
// the tombstones of evicted instances stay within the cap too (review II,
// R2-P6-bound-2026).
func TestRegistryInstanceCap(t *testing.T) {
	t.Parallel()
	r, stub, _ := newTestRegistry(t)
	stub.discover(t, brMeshcop, txt{"xa": brXA, "nn": "Home"}, "otbr.local")
	for i := range 1000 {
		stub.discover(t, fmt.Sprintf("noise-%04d._meshcop._udp.local", i), txt{"nn": "x"}, "")
	}
	r.mu.Lock()
	n := len(r.instances)
	_, first := r.instances["noise-0000._meshcop._udp.local"]
	_, last := r.instances["noise-0999._meshcop._udp.local"]
	_, own := r.instances[strings.ToLower(brMeshcop)]
	r.mu.Unlock()
	if n > maxInstances || first || !last || !own {
		t.Fatalf("%d instances (cap %d); the first xa-less kept %v, the last %v, the border router's %v", n, maxInstances, first, last, own)
	}
	if _, ok := r.Get(brXA); !ok {
		t.Fatal("the border router's entry went with the xa-less instances")
	}

	// A flood of valid xa: entries, instances and tombstones stay bounded.
	r2, stub2, _ := newTestRegistry(t)
	for i := range 3 * maxInstances {
		stub2.discover(t, fmt.Sprintf("br-%04d._meshcop._udp.local", i), txt{"xa": makeXA(i)}, "")
	}
	r2.mu.Lock()
	entries, instances, evicted := len(r2.entries), len(r2.instances), len(r2.evicted)
	r2.mu.Unlock()
	if entries > maxEntries || instances > maxInstances || evicted > maxInstances {
		t.Fatalf("entries %d, instances %d, tombstones %d; caps %d, %d, %d", entries, instances, evicted, maxEntries, maxInstances, maxInstances)
	}
}

// TestRegistryCoalescesUpdates: while an observer is called, the
// observations of a known instance queue one update with the latest
// entry, not one each (review II, R2-P6-bound-2027).
func TestRegistryCoalescesUpdates(t *testing.T) {
	t.Parallel()
	r, stub, _ := newTestRegistry(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var got []Event
	var mu sync.Mutex
	r.OnChange(func(e Event) {
		mu.Lock()
		got = append(got, e)
		n := len(got)
		mu.Unlock()
		if n == 1 {
			close(entered)
			<-release
		}
	})
	done := make(chan struct{})
	go func() {
		defer close(done)
		stub.discover(t, brMeshcop, txt{"xa": brXA, "nn": "Home"}, "otbr.local")
	}()
	<-entered
	for i := range 10000 {
		stub.discover(t, brMeshcop, txt{"xa": brXA, "nn": fmt.Sprintf("Home-%d", i)}, "otbr.local")
	}
	r.mu.Lock()
	queued := len(r.pending)
	r.mu.Unlock()
	if queued > 1 {
		t.Fatalf("%d events queued for one entry", queued)
	}
	close(release)
	<-done
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 || got[0].Kind != EventAdded || got[1].Kind != EventUpdated || got[1].Entry.NetworkName != "Home-9999" {
		t.Fatalf("events %+v, want Added and one Updated with the latest entry", got)
	}
}

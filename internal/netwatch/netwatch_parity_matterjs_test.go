// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package netwatch_test

import (
	"maps"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/internal/netwatch"
)

// The cases of matter.js packages/general/test/net/
// NetworkInterfaceChangesTest.ts at db021da0 that a Go watcher has, ported
// by meaning: a fake host whose interfaces the test edits, the fake clock
// for the periodic check.

type details struct {
	mac      string
	v4, v6   []string
	noDetail bool
}

type fakeHost struct {
	mu       sync.Mutex
	ifaces   map[string]details
	listings int
	failNext bool
	emitted  [][]string
}

func newFakeHost() *fakeHost {
	return &fakeHost{ifaces: map[string]details{
		"eth0":    {mac: "aa:bb:cc:dd:ee:01", v4: []string{"10.0.0.2"}, v6: []string{"2001:db8:1::10", "fe80::1"}},
		"docker0": {mac: "aa:bb:cc:dd:ee:02", v4: []string{"172.17.0.1"}},
	}}
}

func (h *fakeHost) snapshot() map[string]string {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.listings++
	if h.failNext {
		h.failNext = false
		return nil
	}
	out := map[string]string{}
	for name, d := range h.ifaces {
		if d.noDetail {
			continue
		}
		out[name] = netwatch.Describe(d.mac, d.v4, d.v6)
	}
	return out
}

func (h *fakeHost) record(names []string) {
	h.mu.Lock()
	h.emitted = append(h.emitted, slices.Clone(names))
	h.mu.Unlock()
}

func (h *fakeHost) set(name string, d details) {
	h.mu.Lock()
	h.ifaces[name] = d
	h.mu.Unlock()
}

func (h *fakeHost) remove(name string) {
	h.mu.Lock()
	delete(h.ifaces, name)
	h.mu.Unlock()
}

func (h *fakeHost) state() (int, [][]string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.listings, slices.Clone(h.emitted)
}

func newWatcher(t *testing.T) (*netwatch.Watcher, *fakeHost, *clocktest.Fake) {
	t.Helper()
	clk := clocktest.New(time.Unix(1_000_000, 0))
	h := newFakeHost()
	w := netwatch.New(func() clock.Clock { return clk }, h.snapshot, h.record)
	t.Cleanup(w.Stop)
	return w, h, clk
}

func expectEmitted(t *testing.T, h *fakeHost, want ...[]string) {
	t.Helper()
	_, got := h.state()
	if len(got) != len(want) {
		t.Fatalf("emitted %v, want %v", got, want)
	}
	for i := range want {
		if !slices.Equal(got[i], want[i]) {
			t.Fatalf("emitted %v, want %v", got, want)
		}
	}
}

func TestNetworkInterfaceChanges(t *testing.T) {
	t.Parallel()

	t.Run("does not emit for the first snapshot", func(t *testing.T) {
		t.Parallel()
		w, h, _ := newWatcher(t)
		w.Start()
		w.Check()
		expectEmitted(t, h)
	})

	t.Run("emits the names of changed interfaces", func(t *testing.T) {
		t.Parallel()
		w, h, _ := newWatcher(t)
		w.Start()
		h.set("eth0", details{mac: "aa:bb:cc:dd:ee:01", v4: []string{"10.0.0.2"}, v6: []string{"2001:db8:2::10", "fe80::1"}})
		h.set("wlan0", details{mac: "aa:bb:cc:dd:ee:03", v6: []string{"fe80::3"}})
		h.remove("docker0")
		if got := w.Check(); !slices.Equal(got, []string{"docker0", "eth0", "wlan0"}) {
			t.Fatalf("Check = %v", got)
		}
		expectEmitted(t, h, []string{"docker0", "eth0", "wlan0"})
	})

	t.Run("treats reordering and duplicates as no change", func(t *testing.T) {
		t.Parallel()
		w, h, _ := newWatcher(t)
		w.Start()
		h.set("eth0", details{mac: "aa:bb:cc:dd:ee:01", v4: []string{"10.0.0.2"}, v6: []string{"fe80::1", "2001:db8:1::10", "fe80::1"}})
		w.Check()
		expectEmitted(t, h)
	})

	t.Run("treats a MAC change as a change", func(t *testing.T) {
		t.Parallel()
		w, h, _ := newWatcher(t)
		w.Start()
		h.set("docker0", details{mac: "aa:bb:cc:dd:ee:09", v4: []string{"172.17.0.1"}})
		w.Check()
		expectEmitted(t, h, []string{"docker0"})
	})

	t.Run("polls only while observed", func(t *testing.T) {
		t.Parallel()
		w, h, clk := newWatcher(t)
		clk.Advance(netwatch.DefaultInterval)
		if n, _ := h.state(); n != 0 {
			t.Fatalf("%d listings before Start", n)
		}
		w.Start()
		if n, _ := h.state(); n != 1 {
			t.Fatalf("%d listings after Start, want 1", n)
		}
		h.set("eth0", details{mac: "aa:bb:cc:dd:ee:01", v4: []string{"10.0.0.5"}, v6: []string{"2001:db8:1::10", "fe80::1"}})
		clk.Advance(netwatch.DefaultInterval)
		if n, _ := h.state(); n != 2 {
			t.Fatalf("%d listings after one interval, want 2", n)
		}
		expectEmitted(t, h, []string{"eth0"})
		w.Stop()
		clk.Advance(netwatch.DefaultInterval)
		if n, _ := h.state(); n != 2 {
			t.Fatalf("%d listings after Stop, want 2", n)
		}
	})

	t.Run("keeps the previous snapshot when a check fails", func(t *testing.T) {
		t.Parallel()
		w, h, _ := newWatcher(t)
		w.Start()
		h.mu.Lock()
		h.failNext = true
		h.mu.Unlock()
		w.Check()
		h.set("docker0", details{mac: "aa:bb:cc:dd:ee:02"})
		w.Check()
		expectEmitted(t, h, []string{"docker0"})
	})

	t.Run("starts from the current state when observed again after a running check", func(t *testing.T) {
		t.Parallel()
		w, h, _ := newWatcher(t)
		w.Start()
		w.Stop()
		h.set("eth0", details{mac: "aa:bb:cc:dd:ee:01", v4: []string{"10.0.0.9"}})
		w.Start()
		w.Check()
		expectEmitted(t, h)
	})

	t.Run("starts from the current state when observed after an unobserved check", func(t *testing.T) {
		t.Parallel()
		w, h, _ := newWatcher(t)
		if got := w.Check(); got != nil {
			t.Fatalf("a stopped watcher reported %v", got)
		}
		h.set("eth0", details{mac: "aa:bb:cc:dd:ee:01", v4: []string{"10.0.0.9"}})
		w.Start()
		w.Check()
		expectEmitted(t, h)
	})

	t.Run("treats an interface without details as absent", func(t *testing.T) {
		t.Parallel()
		w, h, _ := newWatcher(t)
		w.Start()
		h.set("eth0", details{noDetail: true})
		w.Check()
		expectEmitted(t, h, []string{"eth0"})
	})

	t.Run("keeps polling after a failed periodic check", func(t *testing.T) {
		t.Parallel()
		w, h, clk := newWatcher(t)
		w.Start()
		h.mu.Lock()
		h.failNext = true
		h.mu.Unlock()
		clk.Advance(netwatch.DefaultInterval)
		h.set("docker0", details{mac: "aa:bb:cc:dd:ee:02"})
		clk.Advance(netwatch.DefaultInterval)
		expectEmitted(t, h, []string{"docker0"})
	})

	t.Run("stops polling when closed", func(t *testing.T) {
		t.Parallel()
		w, h, clk := newWatcher(t)
		w.Start()
		w.Stop()
		clk.Advance(netwatch.DefaultInterval)
		if n, _ := h.state(); n != 1 {
			t.Fatalf("%d listings, want 1", n)
		}
	})
}

// TestWatcherInterval: a disabled interval starts no periodic check (the
// host's switch, issue #432) while Check still compares, and a running
// watcher ticks at a new interval and keeps its snapshot.
func TestWatcherInterval(t *testing.T) {
	t.Parallel()
	w, h, clk := newWatcher(t)
	w.SetInterval(0)
	w.Start()
	if !w.Running() {
		t.Fatal("a watcher with a disabled interval is not observing")
	}
	h.set("docker0", details{mac: "aa:bb:cc:dd:ee:02"})
	clk.Advance(netwatch.DefaultInterval)
	expectEmitted(t, h)
	if got := w.Check(); !slices.Equal(got, []string{"docker0"}) {
		t.Fatalf("Check with a disabled interval = %v", got)
	}
	w.SetInterval(10 * time.Second)
	h.set("docker0", details{mac: "aa:bb:cc:dd:ee:03"})
	clk.Advance(10 * time.Second)
	expectEmitted(t, h, []string{"docker0"}, []string{"docker0"})
	w.SetInterval(time.Minute)
	h.set("docker0", details{mac: "aa:bb:cc:dd:ee:04"})
	clk.Advance(10 * time.Second)
	expectEmitted(t, h, []string{"docker0"}, []string{"docker0"})
	clk.Advance(50 * time.Second)
	expectEmitted(t, h, []string{"docker0"}, []string{"docker0"}, []string{"docker0"})
}

func TestDiffAndDescribe(t *testing.T) {
	t.Parallel()
	prev := map[string]string{"a": "1", "b": "2"}
	next := maps.Clone(prev)
	if got := netwatch.Diff(prev, next); got != nil {
		t.Fatalf("Diff of equal maps = %v", got)
	}
	if d := netwatch.Describe("m", []string{"b", "a", "a"}, nil); d != "m|a,b|" {
		t.Fatalf("Describe = %q", d)
	}
}

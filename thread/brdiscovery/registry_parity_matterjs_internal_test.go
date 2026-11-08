// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package brdiscovery

import (
	"encoding/hex"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/discovery"
	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/mdns"
)

// stubNames is the DNS-SD cache of the registry tests, as matter.js's
// StubDnssdNames is BorderRouterRegistryTest.ts's: a test hands the
// registry an instance's state directly.
type stubNames struct {
	suffixes []string
	fn       func(discovery.NameState)
	stopped  bool
}

func (s *stubNames) Watch(suffixes []string, fn func(discovery.NameState)) func() {
	s.suffixes, s.fn = suffixes, fn
	return func() { s.stopped = true }
}

// txt is an instance's TXT: hex for xa and xp (the stub of the matter.js
// test writes them so), text for the rest.
type txt map[string]string

func (s *stubNames) discover(t *testing.T, qname string, entries txt, srvTarget string) {
	t.Helper()
	s.fn(instanceState(t, qname, entries, srvTarget))
}

// instanceState is a discovered instance's state as discover hands it on.
func instanceState(t *testing.T, qname string, entries txt, srvTarget string) discovery.NameState {
	t.Helper()
	p := discovery.Parameters{}
	for k, v := range entries {
		if k == "xa" || k == "xp" {
			b, err := hex.DecodeString(v)
			if err != nil {
				t.Fatal(err)
			}
			p[k] = b
			continue
		}
		p[k] = []byte(v)
	}
	st := discovery.NameState{QName: qname, Parameters: p, Discovered: true}
	if srvTarget != "" {
		st.Records = append(st.Records, discovery.Record{Name: qname, Type: discovery.TypeSRV, SRV: mdns.DNSSRV{Target: srvTarget, Port: 49154}})
	}
	return st
}

func (s *stubNames) gone(qname string) { s.fn(discovery.NameState{QName: qname}) }

const (
	brXA      = "AABBCCDDEEFF0011"
	brMeshcop = "Kuche._meshcop._udp.local"
	brTrel    = "aabbccddeeff0011._trel._udp.local"
)

func newTestRegistry(t *testing.T) (*Registry, *stubNames, *clocktest.Fake) {
	t.Helper()
	stub := &stubNames{}
	clk := clocktest.New(time.Unix(1000, 0))
	r := newRegistry(stub, Config{Clock: clk})
	t.Cleanup(r.Close)
	return r, stub, clk
}

func (r *Registry) sourcesOf(xa string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if rec, ok := r.entries[xa]; ok {
		return slices.Clone(rec.sources)
	}
	return nil
}

func makeXA(i int) string { return fmt.Sprintf("%016X", 0xAA00000000000000+uint64(i)) }

// TestBorderRouterRegistryParityMatterJS ports matter.js
// packages/thread-br-client/test/BorderRouterRegistryTest.ts at db021da0 by
// meaning, for the fields this registry keeps: TXT parsing, the TREL
// fallback, merging, the meshcop precedence, source loss, expiry,
// eviction and the events. The address, port and TXT-version cases have
// no subject here (the package doc's "Left out").
func TestBorderRouterRegistryParityMatterJS(t *testing.T) {
	t.Parallel()

	// :23-25, :56-62.
	t.Run("starts empty; get misses an unknown xa and folds case", func(t *testing.T) {
		t.Parallel()
		r, stub, _ := newTestRegistry(t)
		if len(r.List()) != 0 {
			t.Fatal("not empty")
		}
		if !slices.Equal(stub.suffixes, []string{"._meshcop._udp.local", "._trel._udp.local"}) {
			t.Fatalf("watched %v", stub.suffixes)
		}
		if _, ok := r.Get(brXA); ok {
			t.Fatal("unknown xa found")
		}
		stub.discover(t, brMeshcop, txt{"xa": "aabbccddeeff0011"}, "")
		if _, ok := r.Get("aabbccddeeff0011"); !ok {
			t.Fatal("lower-case xa not folded")
		}
	})

	// :77-118 for the fields kept.
	t.Run("creates a meshcop entry with parsed fields", func(t *testing.T) {
		t.Parallel()
		r, stub, clk := newTestRegistry(t)
		stub.discover(t, brMeshcop, txt{"xa": "aabbccddeeff0011", "xp": "1122334455667788", "nn": "MyNetwork", "vn": "ACME", "mn": "BR-100", "tv": "1.3.0"}, "Kuche.local")
		list := r.List()
		want := Entry{ExtAddress: brXA, ExtPanID: "1122334455667788", NetworkName: "MyNetwork", VendorName: "ACME", ModelName: "BR-100", HostName: "Kuche.local", LastSeen: clk.Now()}
		if len(list) != 1 || list[0] != want {
			t.Fatalf("list %+v, want %+v", list, want)
		}
		if got := r.sourcesOf(brXA); !slices.Equal(got, []string{"meshcop"}) {
			t.Fatalf("sources %v", got)
		}
	})

	// :120-137.
	t.Run("creates a trel-only entry without naming fields", func(t *testing.T) {
		t.Parallel()
		r, stub, _ := newTestRegistry(t)
		stub.discover(t, brTrel, txt{"xa": "aabbccddeeff0011", "nn": "Ignored"}, "br.local")
		e, ok := r.Get(brXA)
		if !ok || e.NetworkName != "" || e.HostName != "br.local" {
			t.Fatalf("entry %+v (%v)", e, ok)
		}
		if got := r.sourcesOf(brXA); !slices.Equal(got, []string{"trel"}) {
			t.Fatalf("sources %v", got)
		}
	})

	// BorderRouterRegistry.ts:335-346: a TREL instance without a valid
	// xa takes its extended address from the instance name.
	t.Run("derives a TREL instance's xa from its name", func(t *testing.T) {
		t.Parallel()
		r, stub, _ := newTestRegistry(t)
		stub.discover(t, brTrel, txt{"xa": "aabb"}, "br.local")
		if _, ok := r.Get(brXA); !ok {
			t.Fatal("no entry from the TREL instance name")
		}
		stub.discover(t, "not-a-mac._trel._udp.local", txt{}, "x.local")
		if len(r.List()) != 1 {
			t.Fatalf("list %+v", r.List())
		}
	})

	// :139-164.
	t.Run("merges meshcop and trel for the same xa", func(t *testing.T) {
		t.Parallel()
		r, stub, _ := newTestRegistry(t)
		stub.discover(t, brMeshcop, txt{"xa": "aabbccddeeff0011", "xp": "1111111111111111", "nn": "MyNet", "vn": "ACME"}, "Kuche.local")
		stub.discover(t, brTrel, txt{"xa": "aabbccddeeff0011", "xp": "1111111111111111"}, "br.local")
		e, _ := r.Get(brXA)
		if e.NetworkName != "MyNet" || e.VendorName != "ACME" || e.HostName != "Kuche.local" || len(r.List()) != 1 {
			t.Fatalf("entry %+v", e)
		}
		if got := r.sourcesOf(brXA); !slices.Equal(got, []string{"meshcop", "trel"}) {
			t.Fatalf("sources %v", got)
		}
	})

	// :166-185, and :430-453 for the host name.
	t.Run("meshcop wins over trel for xp and the host", func(t *testing.T) {
		t.Parallel()
		r, stub, _ := newTestRegistry(t)
		stub.discover(t, brMeshcop, txt{"xa": "aabbccddeeff0011", "xp": "AAAAAAAAAAAAAAAA", "nn": "MyNet"}, "brA.local")
		stub.discover(t, brTrel, txt{"xa": "aabbccddeeff0011", "xp": "BBBBBBBBBBBBBBBB"}, "brC.local")
		e, _ := r.Get(brXA)
		if e.ExtPanID != "AAAAAAAAAAAAAAAA" || e.HostName != "brA.local" {
			t.Fatalf("entry %+v", e)
		}
	})

	// :187-226.
	t.Run("retains the entry across single- and full-source loss", func(t *testing.T) {
		t.Parallel()
		r, stub, _ := newTestRegistry(t)
		stub.discover(t, brMeshcop, txt{"xa": "aabbccddeeff0011", "nn": "MyNet"}, "Kuche.local")
		stub.discover(t, brTrel, txt{"xa": "aabbccddeeff0011"}, "br.local")
		stub.gone(brTrel)
		if got := r.sourcesOf(brXA); !slices.Equal(got, []string{"meshcop"}) {
			t.Fatalf("sources after the trel loss %v", got)
		}
		stub.gone(brMeshcop)
		e, ok := r.Get(brXA)
		if !ok || e.NetworkName != "MyNet" || len(r.sourcesOf(brXA)) != 0 {
			t.Fatalf("stale entry %+v (%v), sources %v", e, ok, r.sourcesOf(brXA))
		}
	})

	// :287-297.
	t.Run("skips an instance without an xa", func(t *testing.T) {
		t.Parallel()
		r, stub, _ := newTestRegistry(t)
		stub.discover(t, brMeshcop, txt{"nn": "OrphanNet"}, "Kuche.local")
		stub.discover(t, "Other._meshcop._udp.local", txt{"xa": "aabbccdd"}, "o.local") // not 8 bytes
		if len(r.List()) != 0 {
			t.Fatalf("list %+v", r.List())
		}
	})

	// :299-336.
	t.Run("evicts the oldest entry past 256", func(t *testing.T) {
		t.Parallel()
		r, stub, clk := newTestRegistry(t)
		for i := range 256 {
			stub.discover(t, makeXA(i)+"._meshcop._udp.local", txt{"xa": makeXA(i)}, "")
			clk.StepWall(time.Millisecond)
		}
		stub.discover(t, makeXA(257)+"._meshcop._udp.local", txt{"xa": makeXA(257)}, "")
		if n := len(r.List()); n != 256 {
			t.Fatalf("%d entries", n)
		}
		if _, ok := r.Get(makeXA(0)); ok {
			t.Fatal("the oldest entry survived")
		}
		if _, ok := r.Get(makeXA(257)); !ok {
			t.Fatal("the new entry is missing")
		}
		// :345-402: the evicted instance is ignored until it goes and
		// comes back.
		evicted := makeXA(0) + "._meshcop._udp.local"
		stub.discover(t, evicted, txt{"xa": makeXA(0)}, "")
		if _, ok := r.Get(makeXA(0)); ok {
			t.Fatal("an evicted instance's change re-added it")
		}
		stub.gone(evicted)
		stub.discover(t, evicted, txt{"xa": makeXA(0)}, "")
		if _, ok := r.Get(makeXA(0)); !ok {
			t.Fatal("the rediscovered instance was not added")
		}
	})

	// :602-654 and :685-753.
	t.Run("eviction prefers a stale entry, even a newer one", func(t *testing.T) {
		t.Parallel()
		r, stub, clk := newTestRegistry(t)
		for i := 1; i <= 2; i++ {
			stub.discover(t, makeXA(i)+"._meshcop._udp.local", txt{"xa": makeXA(i)}, "")
			clk.StepWall(time.Millisecond)
		}
		clk.StepWall(time.Second)
		stale := makeXA(100) + "._meshcop._udp.local"
		stub.discover(t, stale, txt{"xa": makeXA(100)}, "")
		stub.gone(stale)
		clk.StepWall(time.Second)
		for i := 200; i < 200+253; i++ {
			stub.discover(t, makeXA(i)+"._meshcop._udp.local", txt{"xa": makeXA(i)}, "")
			clk.StepWall(time.Millisecond)
		}
		stub.discover(t, makeXA(900)+"._meshcop._udp.local", txt{"xa": makeXA(900)}, "")
		if _, ok := r.Get(makeXA(100)); ok {
			t.Fatal("the stale entry survived")
		}
		for _, i := range []int{1, 2, 900} {
			if _, ok := r.Get(makeXA(i)); !ok {
				t.Fatalf("entry %d evicted", i)
			}
		}
	})

	// :539-566, :656-683.
	t.Run("prunes a stale entry 24 h after it was last seen, its LastSeen frozen", func(t *testing.T) {
		t.Parallel()
		r, stub, clk := newTestRegistry(t)
		stub.discover(t, brMeshcop, txt{"xa": "aabbccddeeff0011"}, "")
		seen := clk.Now()
		clk.StepWall(time.Minute)
		stub.gone(brMeshcop)
		if e, _ := r.Get(brXA); !e.LastSeen.Equal(seen) {
			t.Fatalf("LastSeen %v, want %v", e.LastSeen, seen)
		}
		clk.StepWall(StaleRetention - time.Minute) // now: seen + 24 h
		if _, ok := r.Get(brXA); !ok {
			t.Fatal("pruned at the cutoff")
		}
		clk.StepWall(time.Millisecond)
		if _, ok := r.Get(brXA); ok || len(r.List()) != 0 {
			t.Fatal("not pruned past the cutoff")
		}
	})

	// :568-600.
	t.Run("resets the retention clock when a stale entry is rediscovered", func(t *testing.T) {
		t.Parallel()
		r, stub, clk := newTestRegistry(t)
		stub.discover(t, brMeshcop, txt{"xa": "aabbccddeeff0011"}, "")
		stub.gone(brMeshcop)
		staleAt := clk.Now()
		clk.StepWall(12 * time.Hour)
		stub.discover(t, brMeshcop, txt{"xa": "aabbccddeeff0011"}, "")
		if e, _ := r.Get(brXA); !e.LastSeen.Equal(clk.Now()) {
			t.Fatalf("LastSeen %v", e.LastSeen)
		}
		clk.StepWall(12*time.Hour + time.Millisecond)
		if !clk.Now().After(staleAt.Add(StaleRetention)) {
			t.Fatal("the clock is not past the first cutoff")
		}
		if _, ok := r.Get(brXA); !ok {
			t.Fatal("a live entry was pruned")
		}
	})

	// :755-782.
	t.Run("rediscovers a stale entry from the other source", func(t *testing.T) {
		t.Parallel()
		r, stub, _ := newTestRegistry(t)
		stub.discover(t, brMeshcop, txt{"xa": "aabbccddeeff0011", "nn": "MyNet"}, "Kuche.local")
		stub.gone(brMeshcop)
		stub.discover(t, brTrel, txt{"xa": "aabbccddeeff0011"}, "br.local")
		e, _ := r.Get(brXA)
		if e.NetworkName != "MyNet" || !slices.Equal(r.sourcesOf(brXA), []string{"trel"}) {
			t.Fatalf("entry %+v, sources %v", e, r.sourcesOf(brXA))
		}
		// The meshcop instance gone, trel may set the host (:391-401).
		if e.HostName != "br.local" {
			t.Fatalf("host %q", e.HostName)
		}
	})

	// :515-537, :784-800.
	t.Run("Close clears the registry and stops watching", func(t *testing.T) {
		t.Parallel()
		r, stub, _ := newTestRegistry(t)
		stub.discover(t, brMeshcop, txt{"xa": "aabbccddeeff0011"}, "")
		stub.gone(brMeshcop)
		r.Close()
		r.Close()
		if !stub.stopped || len(r.List()) != 0 {
			t.Fatal("Close kept the registry")
		}
		stub.discover(t, brMeshcop, txt{"xa": "aabbccddeeff0011"}, "")
		if _, ok := r.Get(brXA); ok {
			t.Fatal("a closed registry took an instance")
		}
	})

	// BorderRouterRegistry.ts:73-77: added, updated, removed.
	t.Run("tells added, updated and removed", func(t *testing.T) {
		t.Parallel()
		r, stub, clk := newTestRegistry(t)
		var got []EventKind
		var listed int
		unsubscribe := r.OnChange(func(e Event) {
			got = append(got, e.Kind)
			listed = len(r.List()) // an observer may read the registry
		})
		stub.discover(t, brMeshcop, txt{"xa": "aabbccddeeff0011"}, "")
		stub.discover(t, brMeshcop, txt{"xa": "aabbccddeeff0011", "nn": "Net"}, "")
		stub.gone(brMeshcop)
		clk.StepWall(StaleRetention + time.Second)
		r.List()
		want := []EventKind{EventAdded, EventUpdated, EventRemoved}
		if !slices.Equal(got, want) || listed != 0 {
			t.Fatalf("events %v (listed %d), want %v", got, listed, want)
		}
		unsubscribe()
		unsubscribe()
		stub.discover(t, brMeshcop, txt{"xa": "aabbccddeeff0011"}, "")
		if len(got) != 3 {
			t.Fatalf("told after unsubscribing: %v", got)
		}
	})
}

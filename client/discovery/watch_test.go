// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package discovery

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"
)

const (
	brInst = "Kitchen BR._meshcop._udp.local"
	brHost = "kitchen-br.local"
)

// TestIngress_BorderRouterInstancesEnterTheCache: a _meshcop instance's
// records enter the cache without a browse or a filter of its own
// (BorderRouterRegistry.ts:34-37, :121-126 of matter.js at db021da0), and a
// browse of Matter's operational type does not return it.
func TestIngress_BorderRouterInstancesEnterTheCache(t *testing.T) {
	t.Parallel()
	n, _, _ := testNames(t)
	deliver(t, n, 0,
		[]dns.RR{srvRR(brInst, brHost, 49154, 120, true), txtRR(brInst, 4500, true, "nn=Home", "vn=ACME")},
		aaaaRR(brHost, "fd00::1", 120, true),
		srvRR("aabbccddeeff0011._trel._udp.local", brHost, 12345, 120, true),
	)
	if !n.Has(brInst) || !n.Has("aabbccddeeff0011._trel._udp.local") {
		t.Fatal("the border router's instances are not cached")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got, err := n.Browse(ctx, "_matter._tcp.local")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("a _matter browse returned %+v", got)
	}
}

// TestWatch reports a matching name when it is discovered (one cached
// already, at once), after each change and once it is gone, nothing for
// another type, and nothing after stop.
func TestWatch(t *testing.T) {
	t.Parallel()
	n, _, clk := testNames(t)
	deliver(t, n, 0, []dns.RR{srvRR(brInst, brHost, 49154, 120, true)})

	var mu sync.Mutex
	var states []NameState
	stop := n.Watch([]string{"._MESHCOP._udp.local"}, func(st NameState) {
		mu.Lock()
		states = append(states, st)
		mu.Unlock()
	})
	snapshot := func() []NameState {
		mu.Lock()
		defer mu.Unlock()
		return append([]NameState(nil), states...)
	}
	if s := snapshot(); len(s) != 1 || !s[0].Discovered || s[0].QName != brInst || s[0].SRVTarget() != brHost {
		t.Fatalf("the cached instance: %+v", s)
	}
	deliver(t, n, 0, []dns.RR{txtRR(brInst, 4500, true, "nn=Home")})
	s := snapshot()
	if len(s) != 2 {
		t.Fatalf("after the TXT: %+v", s)
	}
	if nn, ok := s[1].Parameters.Get("nn"); !ok || nn != "Home" {
		t.Fatalf("parameters %v", s[1].Parameters)
	}
	if raw, ok := s[1].Parameters.Raw("NN"); !ok || string(raw) != "Home" {
		t.Fatalf("raw %q %v", raw, ok)
	}
	deliver(t, n, 0, []dns.RR{srvRR("node._matter._tcp.local", "node.local", 5540, 120, true)})
	if len(snapshot()) != 2 {
		t.Fatal("a _matter name was reported")
	}
	clk.advance(n, 3*time.Hour)
	s = snapshot()
	if last := s[len(s)-1]; last.Discovered || last.QName != brInst || last.SRVTarget() != "" {
		t.Fatalf("after expiry: %+v", last)
	}
	before := len(s)
	deliver(t, n, 0, []dns.RR{srvRR(brInst, brHost, 49154, 120, true)})
	if s = snapshot(); len(s) != before+1 || !s[len(s)-1].Discovered {
		t.Fatalf("the rediscovered instance: %+v", s)
	}
	stop()
	stop()
	deliver(t, n, 0, []dns.RR{txtRR(brInst, 4500, true, "nn=Other")})
	if len(snapshot()) != before+1 {
		t.Fatal("reported after stop")
	}
}

// TestWatch_OnAClosedCache returns a stop that does nothing.
func TestWatch_OnAClosedCache(t *testing.T) {
	t.Parallel()
	n, _, _ := testNames(t)
	_ = n.Close()
	n.Watch([]string{"._meshcop._udp.local"}, func(NameState) { t.Error("called") })()
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package discovery

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// recordedLog keeps the records a logger handled, for the log assertions.
type recordedLog struct {
	mu      sync.Mutex
	records []slog.Record
}

func (l *recordedLog) Enabled(context.Context, slog.Level) bool { return true }

func (l *recordedLog) Handle(_ context.Context, r slog.Record) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.records = append(l.records, r.Clone())
	return nil
}

func (l *recordedLog) WithAttrs([]slog.Attr) slog.Handler { return l }
func (l *recordedLog) WithGroup(string) slog.Handler      { return l }

// messages returns the attributes of every record logged as msg.
func (l *recordedLog) messages(msg string) []map[string]string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []map[string]string
	for _, r := range l.records {
		if r.Message != msg {
			continue
		}
		attrs := map[string]string{}
		r.Attrs(func(a slog.Attr) bool {
			attrs[a.Key] = a.Value.String()
			return true
		})
		out = append(out, attrs)
	}
	return out
}

// TestService_TwoRespondersFlipTheSRVTarget is issue 478's capture as a
// fixture: the node's own responder (its SRP host, a Thread ULA) and a
// border router answering the same operational instance with an SRV to
// its own host (fe80 on the receiving interface, an infrastructure ULA, a
// GUA and IPv4), both with cache-flush, alternating. Every address names
// the host that owns it, and the instance's SRV target follows the last
// answer once the eviction window has passed — each change logged as
// matter.discovery.srv_target_changed.
func TestService_TwoRespondersFlipTheSRVTarget(t *testing.T) {
	t.Parallel()
	const (
		inst   = "B1AF46ED144D5E6C-13CA9279820F9E75._matter._tcp.local"
		node   = "B25F4DCB247973D5.local"
		router = "Schlafzimmer.local"
	)
	log := &recordedLog{}
	clk := newFakeClock()
	n := newNames(newFakeConn(), slog.New(log), namesConfig{
		clock:         clk,
		random:        func() uint32 { return 0 },
		interfaceName: func(i int) string { return map[int]string{0: "", 7: "eth7"}[i] },
	})
	t.Cleanup(func() { _ = n.Close() })
	s := NewService(n, inst)
	defer s.Close()

	nodeAnswer := func() {
		deliver(t, n, 7, []dns.RR{srvRR(inst, node, 5540, 120, true)},
			aaaaRR(node, "fd75:c2d:62d3:0:c20:86a3:cb92:55ba", 120, true))
	}
	routerAnswer := func() {
		deliver(t, n, 7, []dns.RR{srvRR(inst, router, 5540, 120, true)},
			aRR(router, "172.18.4.81", 120, true),
			aaaaRR(router, "fe80::86f:c70a:452c:f0b5", 120, true),
			aaaaRR(router, "2003:c8:8711:e802:c51:9bdb:1e3c:9b86", 120, true),
			aaaaRR(router, "fdd8:a08f:13c7:5772:146b:ec9:78f2:98dd", 120, true))
	}
	hosts := func() map[string]string {
		out := map[string]string{}
		for _, a := range s.Addresses() {
			out[a.String()] = a.Host
		}
		return out
	}
	const (
		nodeHost   = "b25f4dcb247973d5.local"
		routerHost = "schlafzimmer.local"
	)
	nodeAddrs := map[string]string{"[fd75:c2d:62d3:0:c20:86a3:cb92:55ba]:5540": nodeHost}
	routerAddrs := map[string]string{
		"[fe80::86f:c70a:452c:f0b5%eth7]:5540":          routerHost,
		"[fdd8:a08f:13c7:5772:146b:ec9:78f2:98dd]:5540": routerHost,
		"[2003:c8:8711:e802:c51:9bdb:1e3c:9b86]:5540":   routerHost,
		"172.18.4.81:5540":                              routerHost,
	}
	union := func(ms ...map[string]string) map[string]string {
		out := map[string]string{}
		for _, m := range ms {
			for k, v := range m {
				out[k] = v
			}
		}
		return out
	}
	check := func(step string, want map[string]string) {
		t.Helper()
		got := hosts()
		if len(got) != len(want) {
			t.Fatalf("%s: addresses = %v, want %v", step, got, want)
		}
		for k, v := range want {
			if got[k] != v {
				t.Fatalf("%s: host of %s = %q, want %q (all %v)", step, k, got[k], v, got)
			}
		}
	}

	nodeAnswer()
	check("node answers", nodeAddrs)

	clk.advance(n, 2*time.Second)
	routerAnswer()
	// The node's SRV is retired, not gone: both targets coexist for the
	// eviction window.
	check("router answers", union(nodeAddrs, routerAddrs))
	clk.advance(n, DefaultEvictionDelay+time.Millisecond)
	check("node's SRV evicted", routerAddrs)

	clk.advance(n, 2*time.Second)
	nodeAnswer()
	check("node answers again", union(nodeAddrs, routerAddrs))
	clk.advance(n, DefaultEvictionDelay+time.Millisecond)
	check("router's SRV evicted", nodeAddrs)

	type change struct{ from, to string }
	var got []change
	for _, a := range log.messages("matter.discovery.srv_target_changed") {
		if a["service"] != inst {
			t.Fatalf("srv_target_changed service = %q, want %q", a["service"], inst)
		}
		got = append(got, change{a["from"], a["to"]})
	}
	// The first answer reaches a service that already exists: a change
	// from no target.
	both := nodeHost + "," + routerHost
	want := []change{{"", nodeHost}, {nodeHost, both}, {both, routerHost}, {routerHost, both}, {both, nodeHost}}
	if !slices.Equal(got, want) {
		t.Fatalf("srv_target_changed = %v, want %v", got, want)
	}
}

// TestService_AddressFromTwoTargetsKeepsTheFirstOwner: an address two SRV
// targets name is one address, owned by the target that named it first.
func TestService_AddressFromTwoTargetsKeepsTheFirstOwner(t *testing.T) {
	t.Parallel()
	n, _, _ := testNames(t)
	s := NewService(n, instQ)
	defer s.Close()
	deliver(t, n, 0, []dns.RR{srvRR(instQ, "first.local", 5540, 120, false)}, aaaaRR("first.local", "fd00::1", 120, true))
	deliver(t, n, 0, []dns.RR{srvRR(instQ, "Second.local.", 5540, 120, false)}, aaaaRR("second.local", "fd00::1", 120, true), aaaaRR("second.local", "fd00::2", 120, true))
	got := map[string]string{}
	for _, a := range s.Addresses() {
		got[a.String()] = a.Host
	}
	if len(got) != 2 || got["[fd00::1]:5540"] != "first.local" || got["[fd00::2]:5540"] != "second.local" {
		t.Fatalf("hosts = %v", got)
	}
	var targets string
	onLoop(t, n, func() { targets = s.targets() })
	if targets != "first.local,second.local" {
		t.Fatalf("targets = %q", targets)
	}
}

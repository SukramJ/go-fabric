// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package discovery

import (
	"errors"
	"testing"
	"time"

	"github.com/miekg/dns"
)

const (
	instQ = "ABCDEF0123456789._matterc._udp.local"
	hostQ = "node1.local"
)

func TestNames_SRVTargetAddressesEnterWithTheSRV(t *testing.T) {
	t.Parallel()
	n, _, _ := testNames(t)
	deliver(t, n, 0,
		[]dns.RR{srvRR(instQ, hostQ, 5540, 120, true)},
		aRR(hostQ, "192.0.2.1", 120, true), aaaaRR(hostQ, "fd00::1", 120, true),
	)
	if !n.Has(instQ) || !n.Has(hostQ) {
		t.Fatal("instance and SRV target must be cached")
	}
	host := n.Get(hostQ)
	if got := len(host.Records()); got != 2 {
		t.Fatalf("host records = %d, want 2 (A and AAAA)", got)
	}
	if !host.IsDiscovered() {
		t.Fatal("host must be discovered")
	}
}

func TestNames_UnrelatedRecordsStayOut(t *testing.T) {
	t.Parallel()
	n, _, _ := testNames(t)
	deliver(t, n, 0, []dns.RR{srvRR("printer._ipp._tcp.local", "printer.local", 631, 120, false)}, aRR("printer.local", "192.0.2.9", 120, false))
	if n.Has("printer._ipp._tcp.local") || n.Has("printer.local") {
		t.Fatal("a non-Matter service must not enter the cache")
	}
	if got := len(n.DiscoveredNames()); got != 0 {
		t.Fatalf("discovered = %d", got)
	}
}

func TestNames_QueryKnownAnswersAreNotCached(t *testing.T) {
	t.Parallel()
	n, _, _ := testNames(t)
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(instQ), dns.TypeSRV)
	m.Answer = []dns.RR{srvRR(instQ, hostQ, 5540, 120, false)}
	b, _ := m.Pack()
	onLoop(t, n, func() { n.handleMessage(mdnsResponse(b, 0)) })
	if n.Has(instQ) {
		t.Fatal("a query's known answer must not be cached")
	}
	onLoop(t, n, func() { n.handleMessage(mdnsResponse([]byte{1, 2, 3}, 0)) })
}

func TestNames_StagedAddressReplayedWhenAnSRVNamesTheHost(t *testing.T) {
	t.Parallel()
	n, _, clk := testNames(t)
	// The TXT makes the packet relevant; the A for a host nobody named yet
	// is staged.
	deliver(t, n, 0, []dns.RR{txtRR(instQ, 120, true, "D=3840")}, aRR(hostQ, "192.0.2.1", 120, true))
	if n.Has(hostQ) {
		t.Fatal("staged host must not be a name yet")
	}
	stagedAt := clk.Now()
	clk.advance(n, 2*time.Second)
	deliver(t, n, 0, []dns.RR{srvRR(instQ, hostQ, 5540, 120, true)})
	recs := n.Get(hostQ).Records()
	if len(recs) != 1 || recs[0].IP.String() != "192.0.2.1" {
		t.Fatalf("host records = %+v", recs)
	}
	if !recs[0].InstalledAt.Equal(stagedAt) {
		t.Fatalf("replayed record installed at %v, want the original receive time %v", recs[0].InstalledAt, stagedAt)
	}
}

func TestNames_IrrelevantPacketIsNotStaged(t *testing.T) {
	t.Parallel()
	n, _, _ := testNames(t)
	deliver(t, n, 0, nil, aRR(hostQ, "192.0.2.1", 120, false))
	deliver(t, n, 0, []dns.RR{srvRR(instQ, hostQ, 5540, 120, true)})
	if got := len(n.Get(hostQ).Records()); got != 0 {
		t.Fatalf("host records = %d, want 0: an address from an irrelevant packet must not be staged", got)
	}
}

func TestNames_StagedGoodbyeAndFlushAndPrune(t *testing.T) {
	t.Parallel()
	n, _, clk := testNames(t)
	deliver(t, n, 0, []dns.RR{txtRR(instQ, 120, true, "D=1")}, aRR(hostQ, "192.0.2.1", 120, true), aRR(hostQ, "192.0.2.2", 120, false))
	// A goodbye for one staged address removes it from the stage.
	deliver(t, n, 0, nil, aRR(hostQ, "192.0.2.2", 0, false))
	onLoop(t, n, func() {
		if s, _ := n.stagedIPs.get(hostQ); len(s) != 1 {
			t.Errorf("staged = %d, want 1 after the goodbye", len(s))
		}
	})
	// A cache-flush A two seconds later supersedes the older staged A.
	clk.advance(n, 2*time.Second)
	deliver(t, n, 0, []dns.RR{txtRR(instQ, 120, true, "D=1")}, aRR(hostQ, "192.0.2.3", 120, true))
	onLoop(t, n, func() {
		s, _ := n.stagedIPs.get(hostQ)
		if len(s) != 1 || s[0].record.IP.String() != "192.0.2.3" {
			t.Errorf("staged after flush = %+v", s)
		}
	})
	// A goodbye for the last one empties the host.
	deliver(t, n, 0, nil, aRR(hostQ, "192.0.2.3", 0, false))
	onLoop(t, n, func() {
		if n.stagedIPs.has(hostQ) {
			t.Error("host must leave the stage with its last address")
		}
	})
	// Pruning drops a staged address once its graced TTL passed.
	deliver(t, n, 0, []dns.RR{txtRR(instQ, 120, true, "D=1")}, aRR("other.local", "192.0.2.4", 20, false))
	clk.advance(n, 2*time.Minute)
	onLoop(t, n, func() {
		if n.stagedIPs.has("other.local") {
			t.Error("prune must drop an expired staged address")
		}
	})
}

func TestNames_PruneCapsStagedHosts(t *testing.T) {
	t.Parallel()
	n, _, _ := testNames(t)
	onLoop(t, n, func() {
		now := n.clock.Now()
		for i := range stagedIPMaxHosts + 1 {
			rec := &Record{Name: "h", Type: TypeA, TTL: time.Hour}
			n.stagedIPs.set(string(rune('a'+i%26))+"-"+time.Duration(i).String(), []stagedIP{{record: rec, receivedAt: now}})
		}
		n.pruneStagedIPs()
		if got := n.stagedIPs.len(); got != stagedIPMinHosts {
			t.Errorf("staged hosts = %d, want %d", got, stagedIPMinHosts)
		}
	})
}

func TestNames_ExpiryRemovesRecordsAndTheName(t *testing.T) {
	t.Parallel()
	n, _, clk := testNames(t)
	deliver(t, n, 0, []dns.RR{txtRR(instQ, 20, false, "D=1")})
	if !n.Has(instQ) {
		t.Fatal("cached")
	}
	clk.advance(n, 20*time.Second)
	if !n.Has(instQ) {
		t.Fatal("the grace factor keeps the record past its TTL")
	}
	clk.advance(n, 2*time.Second)
	if n.Has(instQ) {
		t.Fatal("an expired record's unobserved name must go")
	}
}

func TestNames_ChangesAreCoalescedPerTask(t *testing.T) {
	t.Parallel()
	n, _, _ := testNames(t)
	var got []Changes
	onLoop(t, n, func() {
		nm := n.get(instQ)
		nm.on(&nameObserver{fn: func(c Changes) { got = append(got, c) }})
	})
	deliver(t, n, 0, []dns.RR{srvRR(instQ, hostQ, 5540, 120, true), txtRR(instQ, 120, true, "D=1")})
	onLoop(t, n, func() {
		if len(got) != 1 || len(got[0].Updated) != 2 || got[0].Name.QName() != instQ {
			t.Errorf("changes = %+v, want one batch with two updates", got)
		}
	})
}

func TestNames_SourceInterfaceKeptForAAAAOnly(t *testing.T) {
	t.Parallel()
	n, _, _ := testNames(t)
	deliver(t, n, 7, []dns.RR{srvRR(instQ, hostQ, 5540, 120, true)}, aRR(hostQ, "192.0.2.1", 120, true), aaaaRR(hostQ, "fe80::1", 120, true))
	for _, r := range n.Get(hostQ).Records() {
		want := ""
		if r.Type == TypeAAAA {
			want = "eth7"
		}
		if r.SourceIntf != want {
			t.Errorf("%v SourceIntf = %q, want %q", r.Type, r.SourceIntf, want)
		}
	}
}

func TestNames_DependencyReleasedWithTheSRV(t *testing.T) {
	t.Parallel()
	n, _, clk := testNames(t)
	deliver(t, n, 0, []dns.RR{srvRR(instQ, hostQ, 5540, 120, true)})
	if !n.Has(hostQ) {
		t.Fatal("the SRV keeps its target name")
	}
	deliver(t, n, 0, []dns.RR{srvRR(instQ, hostQ, 5540, 0, false)})
	clk.advance(n, 2*time.Second)
	if n.Has(instQ) || n.Has(hostQ) {
		t.Fatal("instance and target must go once the SRV is gone")
	}
}

func TestNames_ClosedCacheRefusesWork(t *testing.T) {
	t.Parallel()
	n, conn, _ := testNames(t)
	_ = n.Close()
	_ = n.Close()
	if !conn.closed {
		t.Fatal("Close must close the connection")
	}
	if n.Has(instQ) || n.DiscoveredNames() != nil {
		t.Fatal("a closed cache answers empty")
	}
	if nm := n.Get(instQ); nm.QName() != instQ || nm.IsDiscovered() || nm.Records() != nil || nm.Parameters() != nil {
		t.Fatal("a closed cache hands out a detached, empty name")
	}
	if err := n.loop.call(func() {}); !errors.Is(err, ErrClosed) {
		t.Fatalf("call after close = %v", err)
	}
	if n.loop.post(func() {}) {
		t.Fatal("post after close must fail")
	}
}

func TestNames_InterfaceNameOf(t *testing.T) {
	t.Parallel()
	if interfaceNameOf(0) != "" || interfaceNameOf(1<<30) != "" {
		t.Fatal("unknown interface indices have no name")
	}
}

func TestNames_Constructors(t *testing.T) {
	t.Parallel()
	conn := newFakeConn()
	n := NewNames(conn, nil)
	if n.Has(instQ) {
		t.Fatal("empty cache")
	}
	_ = n.Close()
	if !conn.closed {
		t.Fatal("NewNames owns the connection")
	}
	live, err := Open(nil)
	if err != nil {
		t.Skipf("no multicast here: %v", err)
	}
	_ = live.Close()
}

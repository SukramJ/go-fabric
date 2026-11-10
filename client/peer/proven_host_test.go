// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

import (
	"net/netip"
	"testing"
	"testing/synctest"

	"github.com/SukramJ/go-fabric/client/discovery"
)

// The two hosts of issue 478: the node's own (its SRP host) and a border
// router that answers the node's instance with an SRV to its own host.
const (
	routerHostQ = "router.local"
	routerLL    = "[fe80::1]:5540"
)

// announceTwoHosts publishes the test node's instance with two SRV
// targets in one packet, both kept: the node's host with fd00::1 and the
// router's with fe80::1, which the parity order ranks first.
func (h *harness) announceTwoHosts(t *testing.T) {
	t.Helper()
	h.mdns.announce(t,
		srvRR(h.qname, h.hostQ, 5540, 120), srvRR(h.qname, routerHostQ, 5540, 120),
		txtRR(h.qname, 120, "SII=800", "SAI=300"),
		aaaaRR(h.hostQ, "fd00::1", 120), aaaaRR(routerHostQ, "fe80::1", 120))
}

// TestProvenHostRanksFirst: the proven host's address is attempted at
// once even when another target's ranks first in the parity order — first,
// or, when the other target's address was delivered and attempted before
// it, preempting the stagger. The rule lapses when no discovered address
// carries the host, and without a proven host the parity order and the
// stagger stand.
func TestProvenHostRanksFirst(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, host string
		proven     bool
	}{
		{"proven host first", "node1234.local", true},
		{"no proven host: parity order", "", false},
		// Parity only without an operational address: with one, the
		// absent proven host queues it first (issue 488,
		// TestProvenHostAbsentTriesTheOperationalAddressFirst).
		{"host no longer advertised, no operational address: parity order", "gone.local", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				h := newHarness(t, nil)
				p, err := h.set.AddKnownPeer(Descriptor{Address: testAddr, OperationalHost: c.host})
				if err != nil {
					t.Fatal(err)
				}
				h.announceTwoHosts(t)
				eventually(t, "the addresses", func() bool { return len(p.Service().Addresses()) == 2 })
				began := h.clock.Now()
				connectAsync(p, ConnectOptions{ConnectionTimeout: -1})
				if c.proven {
					h.sigma1(t, addr1)
					if d := h.clock.Now().Sub(began); d != 0 {
						t.Fatalf("the proven host's address began after %v", d)
					}
					return
				}
				if d := h.udp.next(t); d.to.String() != routerLL {
					t.Fatalf("first attempt to %v, want %s", d.to, routerLL)
				}
				h.noSigma1(t, addr1)
			})
		})
	}
}

// TestProvenHostPreemptsTheStagger: the router's link-local address is
// attempted first because the proven host's address is not yet known;
// when it arrives, its attempt starts at once instead of
// DelayBeforeNextAddress later, and the router's attempt goes on.
func TestProvenHostPreemptsTheStagger(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, nil)
		p, err := h.set.AddKnownPeer(Descriptor{Address: testAddr, OperationalHost: "node1234.local"})
		if err != nil {
			t.Fatal(err)
		}
		h.mdns.announce(t, srvRR(h.qname, routerHostQ, 5540, 120), aaaaRR(routerHostQ, "fe80::1", 120))
		eventually(t, "the router's address", func() bool { return len(p.Service().Addresses()) == 1 })
		began := h.clock.Now()
		connectAsync(p, ConnectOptions{ConnectionTimeout: -1})
		h.sigma1(t, routerLL)
		h.mdns.announce(t, srvRR(h.qname, h.hostQ, 5540, 120), aaaaRR(h.hostQ, "fd00::1", 120))
		h.sigma1(t, addr1)
		if d := h.clock.Now().Sub(began); d != 0 {
			t.Fatalf("the proven host's address began after %v", d)
		}
		c := process(p)
		c.mu.Lock()
		running := len(c.attempts)
		c.mu.Unlock()
		if running != 2 {
			t.Fatalf("%d attempts running, want the router's and the proven host's", running)
		}
	})
}

// TestCompareForHost: the proven host's addresses before every other,
// each group in the desirability order; an empty host is the desirability
// order alone.
func TestCompareForHost(t *testing.T) {
	t.Parallel()
	mk := func(ip, host string) discovery.Address {
		return discovery.Address{IP: netip.MustParseAddr(ip), Port: 5540, Host: host}
	}
	ll, ula, gua := mk("fe80::1", "router.local"), mk("fd00::1", "node.local"), mk("2001:db8::1", "node.local")
	if compareForHost("node.local", ula, ll) >= 0 || compareForHost("node.local", gua, ll) >= 0 {
		t.Fatal("the proven host's addresses rank first")
	}
	if compareForHost("node.local", ula, gua) >= 0 {
		t.Fatal("within the proven host: desirability")
	}
	if compareForHost("", ll, ula) >= 0 {
		t.Fatal("without a host: desirability")
	}
	if got := provenHost("node.local", []discovery.Address{ll}); got != "" {
		t.Fatalf("a host no address carries is no proven host: %q", got)
	}
	if got := provenHost("node.local", []discovery.Address{ll, gua}); got != "node.local" {
		t.Fatalf("provenHost = %q", got)
	}
}

// TestOperationalHostLearned: the session's address names the proven
// host — the discovered SRV target that owns it — saved with the
// descriptor; an address no target owns leaves the host as it was; a
// move to another target's address proves that one.
func TestOperationalHostLearned(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, nil)
		p, _ := h.set.For(testAddr)
		h.announceTwoHosts(t)
		eventually(t, "the addresses", func() bool { return len(p.Service().Addresses()) == 2 })
		s := h.secureSession(t, 0x51, udpAddr(addr1))
		if h.set.Adopt(s) != p {
			t.Fatal("the session's peer")
		}
		if got := p.Descriptor().OperationalHost; got != h.hostQ {
			t.Fatalf("OperationalHost = %q, want %q", got, h.hostQ)
		}
		eventually(t, "the saved host", func() bool {
			ds, _ := h.store.LoadPeers(testAddr.FabricIndex)
			return len(ds) == 1 && ds[0].OperationalHost == h.hostQ
		})
		s.SetAddr(udpAddr(addr9))
		if d := p.Descriptor(); d.OperationalHost != h.hostQ || d.OperationalAddress.String() != addr9 {
			t.Fatalf("an address no target owns: %+v", d)
		}
		s.SetAddr(udpAddr(routerLL))
		if got := p.Descriptor().OperationalHost; got != routerHostQ {
			t.Fatalf("OperationalHost = %q, want %q", got, routerHostQ)
		}
	})
}

// TestOrderForPeer: the monitor's alternates, the proven host's first,
// each group in the order given.
func TestOrderForPeer(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, nil)
		p, _ := h.set.AddKnownPeer(Descriptor{Address: testAddr, OperationalHost: "node1234.local"})
		h.announceTwoHosts(t)
		eventually(t, "the addresses", func() bool { return len(p.Service().Addresses()) == 2 })
		discovered := p.Service().Addresses()
		if discovered[0].String() != routerLL {
			t.Fatalf("parity order %v", discovered)
		}
		got := orderForPeer(p, discovered)
		if got[0].String() != addr1 || got[1].String() != routerLL || discovered[0].String() != routerLL {
			t.Fatalf("orderForPeer = %v (input %v)", got, discovered)
		}
	})
}

// TestPendingAddAdoptsTheHost: the fallback queued from the operational
// address carries no host; when discovery names the same address, the
// queued entry takes its host, so the proven host ranks it.
func TestPendingAddAdoptsTheHost(t *testing.T) {
	t.Parallel()
	c := &connection{wake: make(chan struct{}, 1)}
	fallback := addressOf(udpAddr(addr1))
	if !c.pendingAddLocked(target{addr: fallback}) {
		t.Fatal("the fallback was not queued")
	}
	discovered := fallback
	discovered.Host = "node1234.local"
	if !c.pendingAddLocked(target{addr: discovered}) {
		t.Fatal("adopting the host does not count")
	}
	if len(c.pending) != 1 || c.pending[0].addr.Host != "node1234.local" {
		t.Fatalf("pending %+v", c.pending)
	}
	if c.pendingAddLocked(target{addr: discovered}) {
		t.Fatal("a second add of the same entry counts")
	}
}

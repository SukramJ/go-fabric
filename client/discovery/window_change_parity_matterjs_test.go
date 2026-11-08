// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package discovery

// A port of matter.js packages/protocol/test/mdns/
// CommissionableMdnsScannerWindowChangeTest.ts at db021da0 by meaning: a
// device ends one commissioning window and opens the next with the same
// discriminator about 30 ms apart, while the controller has the previous
// window cached. matter.js drives its own responder over a simulated
// network; the port injects the packets that responder sends — the window's
// records in one announcement, the goodbyes of a withdrawal (InterfaceResponder.ts
// #goodbyesFor at db021da0: the host addresses only when no other window
// publishes them, the cache-flush bit only on a unique type whose set is
// left empty) — into the cache's fake connection, and answers its address
// queries as the device would.

import (
	"context"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/SukramJ/go-fabric/mdns"
)

// The constants of CommissionableMdnsScannerWindowChangeTest.ts:37-50.
const (
	wcDeviceIPv4     = "10.10.10.1"
	wcDeviceIPv6     = "abcd::1"
	wcHostname       = "0011223344550000.local"
	wcPort           = 5540
	wcDiscriminator  = 3840
	wcPreviousWindow = "F24C0677BA7AF549"
	wcNextWindow     = "6B7F25408AAD3DB6"
)

var wcDeviceAddresses = []string{wcDeviceIPv4, wcDeviceIPv6}

func wcQname(instance string) string { return instance + "._matterc._udp.local" }

// wcWindowRecords is windowRecords (:58-68): the record set of one window,
// every unique record with the cache-flush bit, the host addresses in every
// window's set.
func wcWindowRecords(instance string, ttl uint32) []dns.RR {
	q := wcQname(instance)
	return []dns.RR{
		ptrRR("_matterc._udp.local", q, ttl),
		ptrRR("_L3840._sub._matterc._udp.local", q, ttl),
		srvRR(q, wcHostname, wcPort, ttl, true),
		txtRR(q, ttl, true, "D=3840", "CM=2"),
		aaaaRR(wcHostname, wcDeviceIPv6, ttl, true),
		aRR(wcHostname, wcDeviceIPv4, ttl, true),
	}
}

// wcGoodbye is the device's goodbye for a window: its own records with TTL
// 0, the host addresses too when withHost — the last window, or a
// responder that does not check — and the cache-flush bit kept on the
// unique types whose sets the device leaves empty.
func wcGoodbye(instance string, withHost bool) []dns.RR {
	out := []dns.RR{}
	for _, rr := range wcWindowRecords(instance, 0) {
		if t := rr.Header().Rrtype; (t == dns.TypeA || t == dns.TypeAAAA) && !withHost {
			continue
		}
		out = append(out, rr)
	}
	return out
}

// wcHarness is withHarness (:106-131): the cache, the scanner, the clock
// and the device's connection.
type wcHarness struct {
	names *Names
	conn  *fakeConn
	clk   *fakeClock
	s     *Scanner
}

func newWCHarness(t *testing.T) *wcHarness {
	t.Helper()
	n, conn, clk := testNames(t)
	s := NewScanner(n)
	t.Cleanup(s.Close)
	return &wcHarness{names: n, conn: conn, clk: clk, s: s}
}

// announce is Device.announce (:83-89): the window's records in one
// announcement.
func (h *wcHarness) announce(t *testing.T, instance string) {
	t.Helper()
	deliver(t, h.names, 0, wcWindowRecords(instance, 120))
}

func (h *wcHarness) elapse(d time.Duration) { h.clk.advance(h.names, d) }

// openPreviousWindow is :188-196: the previous window announced and
// re-announced over three minutes, resolved by the scanner.
func (h *wcHarness) openPreviousWindow(t *testing.T) {
	t.Helper()
	h.announce(t, wcPreviousWindow)
	for range 3 {
		h.elapse(60 * time.Second)
		h.announce(t, wcPreviousWindow)
	}
	h.elapse(10 * time.Millisecond)
	if got := h.state(wcPreviousWindow).scannerAddresses; !slices.Equal(got, wcDeviceAddresses) {
		t.Fatalf("previous window resolved to %v, want %v", got, wcDeviceAddresses)
	}
}

// wcState is stateOf (:153-168).
type wcState struct {
	instanceDiscovered bool
	hostAddresses      []string
	scannerAddresses   []string
}

func (h *wcHarness) state(instance string) wcState {
	var st wcState
	_ = h.names.loop.call(func() {
		if nm := h.names.maybeGet(wcQname(instance)); nm != nil {
			st.instanceDiscovered = nm.isDiscovered()
		}
		st.hostAddresses = []string{}
		if nm := h.names.maybeGet(wcHostname); nm != nil {
			for _, r := range nm.records.values() {
				if r.Type == mdns.DNSTypeA || r.Type == mdns.DNSTypeAAAA {
					st.hostAddresses = append(st.hostAddresses, r.IP.String())
				}
			}
		}
	})
	sort.Strings(st.hostAddresses)
	st.scannerAddresses = []string{}
	devs := h.s.Discovered(ByInstanceID(instance))
	for i := range devs {
		st.scannerAddresses = append(st.scannerAddresses, ipsOf(devs[i].Addresses)...)
	}
	return st
}

func ipsOf(addresses []Address) []string {
	out := make([]string, 0, len(addresses))
	for _, a := range addresses {
		out = append(out, a.IP.String())
	}
	sort.Strings(out)
	return out
}

type wcFound struct {
	instance  string
	addresses []string
}

// discover is :173-185: a discovery for the shared discriminator that runs
// for d and reports what it delivered.
func (h *wcHarness) discover(t *testing.T, d time.Duration) []wcFound {
	t.Helper()
	id := ByLongDiscriminator(wcDiscriminator)
	r := cmsFind(context.Background(), t, h.names, h.s, id)
	h.elapse(d)
	h.s.Cancel(id)
	r.result(t)
	devs := r.found.snapshot()
	out := make([]wcFound, 0, len(devs))
	for i := range devs {
		out = append(out, wcFound{devs[i].InstanceID, ipsOf(devs[i].Addresses)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].instance < out[j].instance })
	return out
}

// answerAddresses makes the device answer a query for its host addresses
// with them, as its responder does.
func (h *wcHarness) answerAddresses(t *testing.T) {
	t.Helper()
	h.conn.mu.Lock()
	h.conn.onSend = func(msg []byte) {
		m, err := mdns.ParseDNSMessage(msg)
		if err != nil {
			return
		}
		for _, q := range m.Questions {
			if q.Name == wcHostname && (q.Type == mdns.DNSTypeA || q.Type == mdns.DNSTypeAAAA) {
				inject(t, h.conn, 0, []dns.RR{aaaaRR(wcHostname, wcDeviceIPv6, 120, true), aRR(wcHostname, wcDeviceIPv4, 120, true)})
				return
			}
		}
	}
	h.conn.mu.Unlock()
}

func TestCommissionableMdnsScannerWindowChange(t *testing.T) {
	t.Parallel()
	wantBoth := func(t *testing.T, found []wcFound) {
		t.Helper()
		if len(found) != 2 || found[0].instance != wcNextWindow || found[1].instance != wcPreviousWindow {
			t.Fatalf("found %+v, want both windows", found)
		}
		for _, f := range found {
			if !slices.Equal(f.addresses, wcDeviceAddresses) {
				t.Fatalf("%s delivered with %v, want %v", f.instance, f.addresses, wcDeviceAddresses)
			}
		}
	}
	wantNextResolved := func(t *testing.T, h *wcHarness) {
		t.Helper()
		st := h.state(wcNextWindow)
		if !st.instanceDiscovered || !slices.Equal(st.hostAddresses, wcDeviceAddresses) || !slices.Equal(st.scannerAddresses, wcDeviceAddresses) {
			t.Fatalf("next window %+v, want discovered with %v", st, wcDeviceAddresses)
		}
	}

	// "goodbye of the previous window, then the next window" (:198-235):
	// the previous window is the device's last service, so its goodbye
	// names the host addresses as well.
	goodbyeFirst := func(t *testing.T) *wcHarness {
		t.Helper()
		h := newWCHarness(t)
		h.openPreviousWindow(t)
		deliver(t, h.names, 0, wcGoodbye(wcPreviousWindow, true))
		h.elapse(30 * time.Millisecond)
		h.announce(t, wcNextWindow)
		return h
	}
	t.Run("goodbye first: delivers both windows to a discovery that starts 6 ms later", func(t *testing.T) {
		t.Parallel()
		h := goodbyeFirst(t)
		h.elapse(6 * time.Millisecond)
		wantBoth(t, h.discover(t, 100*time.Millisecond))
	})
	t.Run("goodbye first: keeps the host addresses of the next window once the goodbye takes effect", func(t *testing.T) {
		t.Parallel()
		h := goodbyeFirst(t)
		h.elapse(2 * time.Second)
		wantNextResolved(t, h)
		if h.state(wcPreviousWindow).instanceDiscovered {
			t.Fatal("the previous window is still discovered")
		}
	})

	// "the next window, then goodbye of the previous window" (:237-275):
	// the next window publishes the host addresses, so the goodbye leaves
	// them out.
	nextFirst := func(t *testing.T) *wcHarness {
		t.Helper()
		h := newWCHarness(t)
		h.openPreviousWindow(t)
		h.announce(t, wcNextWindow)
		h.elapse(30 * time.Millisecond)
		deliver(t, h.names, 0, wcGoodbye(wcPreviousWindow, false))
		return h
	}
	t.Run("next first: delivers both windows to a discovery that starts 6 ms later", func(t *testing.T) {
		t.Parallel()
		h := nextFirst(t)
		h.elapse(6 * time.Millisecond)
		wantBoth(t, h.discover(t, 100*time.Millisecond))
	})
	t.Run("next first: keeps the host addresses of the next window once the goodbye takes effect", func(t *testing.T) {
		t.Parallel()
		h := nextFirst(t)
		h.elapse(2 * time.Second)
		wantNextResolved(t, h)
	})

	// "a responder that says goodbye to host addresses its next window still
	// uses" (:277-308): the controller honours the goodbye and finds the
	// addresses again with a query.
	hostGoodbye := func(t *testing.T) *wcHarness {
		t.Helper()
		h := newWCHarness(t)
		h.openPreviousWindow(t)
		h.announce(t, wcNextWindow)
		h.elapse(1100 * time.Millisecond)
		deliver(t, h.names, 0, wcGoodbye(wcPreviousWindow, false))
		deliver(t, h.names, 0, wcGoodbye(wcPreviousWindow, true))
		h.elapse(2 * time.Second)
		return h
	}
	t.Run("host goodbye: drops the host addresses once the goodbye takes effect", func(t *testing.T) {
		t.Parallel()
		h := hostGoodbye(t)
		st := h.state(wcNextWindow)
		if !st.instanceDiscovered || len(st.hostAddresses) != 0 || len(st.scannerAddresses) != 0 {
			t.Fatalf("next window %+v, want discovered without addresses", st)
		}
	})
	t.Run("host goodbye: delivers the next window once the device answers the address query", func(t *testing.T) {
		t.Parallel()
		h := hostGoodbye(t)
		h.answerAddresses(t)
		found := h.discover(t, 5*time.Second)
		if len(found) != 1 || found[0].instance != wcNextWindow || !slices.Equal(found[0].addresses, wcDeviceAddresses) {
			t.Fatalf("found %+v, want the next window with %v", found, wcDeviceAddresses)
		}
	})
}

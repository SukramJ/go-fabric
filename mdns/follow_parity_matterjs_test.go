// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns

import (
	"context"
	"slices"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/SukramJ/go-fabric/internal/netwatch"
)

// The cases of matter.js packages/protocol/test/mdns/
// MdnsServerAddressChangeTest.ts at db021da0 (#4842), ported by meaning:
// the fake wire's addresses are the test network's interface details, and
// CheckInterfaces is Network.checkInterfaces. The service's broadcast run
// is ended after its first announcement (ServiceConnected), as the matter.js
// fixture registers without a schedule, so only the change publications
// are on the wire.

// newChangeFixture is the beforeEach: eth0 (1) with 10.0.0.2 and
// 2001:db8:1::10, one operational service announced.
func newChangeFixture(t *testing.T, links map[int][]string) (*Responder, *fakeWire, *fakeClock) {
	t.Helper()
	r, w, clk := newWiredResponder(links)
	svc := operationalFixture(port1)
	if err := r.Publish(context.Background(), svc); err != nil {
		t.Fatal(err)
	}
	r.ServiceConnected(svc.InstanceName, svc.ServiceType)
	clk.Advance(2 * time.Second)
	w.take()
	t.Cleanup(func() { _ = r.Close() })
	return r, w, clk
}

// addressRecords is the test's addressRecords: "interface value ttl" of
// every address record sent, in order.
func sentAddresses(sent []sentPacket) []string {
	var out []string
	for _, p := range sent {
		for _, rr := range append(slices.Clone(p.msg.Answer), p.msg.Extra...) {
			var v string
			switch a := rr.(type) {
			case *dns.A:
				v = a.A.String()
			case *dns.AAAA:
				v = a.AAAA.String()
			default:
				continue
			}
			out = append(out, strconv.Itoa(p.ifIndex)+" "+v+" "+strconv.FormatUint(uint64(rr.Header().Ttl), 10))
		}
	}
	return out
}

func sortedStrings(in []string) []string {
	out := slices.Clone(in)
	sort.Strings(out)
	return out
}

func TestMdnsServerAddressChange(t *testing.T) {
	t.Parallel()
	eth0 := func() map[int][]string { return map[int][]string{1: {"10.0.0.2", "2001:db8:1::10"}} }

	t.Run("says goodbye to a lost address and announces the new one, again one second later", func(t *testing.T) {
		t.Parallel()
		r, w, clk := newChangeFixture(t, eth0())
		w.setAddrs(1, "10.0.0.2", "2001:db8:2::10")
		r.CheckInterfaces()
		got := sentAddresses(w.take())
		if len(got) == 0 || got[0] != "1 2001:db8:1::10 0" {
			t.Fatalf("the publication does not open with the lost address's goodbye: %q", got)
		}
		expectStrings(t, "change", sortedStrings(got), []string{"1 10.0.0.2 120", "1 2001:db8:1::10 0", "1 2001:db8:2::10 120"})
		clk.Advance(time.Second)
		expectStrings(t, "repeat", sortedStrings(sentAddresses(w.take())), []string{"1 10.0.0.2 120", "1 2001:db8:2::10 120"})
	})

	t.Run("announces an added address", func(t *testing.T) {
		t.Parallel()
		r, w, _ := newChangeFixture(t, eth0())
		w.setAddrs(1, "10.0.0.2", "10.0.0.3", "2001:db8:1::10")
		r.CheckInterfaces()
		expectStrings(t, "change", sortedStrings(sentAddresses(w.take())),
			[]string{"1 10.0.0.2 120", "1 10.0.0.3 120", "1 2001:db8:1::10 120"})
	})

	t.Run("answers with the new addresses after a change", func(t *testing.T) {
		t.Parallel()
		r, w, clk := newChangeFixture(t, eth0())
		w.setAddrs(1, "10.0.0.2", "2001:db8:2::10")
		r.CheckInterfaces()
		clk.Advance(2 * time.Second)
		w.take()
		deliver(t, r, queryMsg([]dns.Question{question(serverHost+".local", dns.TypeAAAA, false)}), 1, dummySource)
		expectStrings(t, "answer", sentAddresses(w.take()), []string{"1 2001:db8:2::10 120"})
	})

	t.Run("publishes only on the interface that changed", func(t *testing.T) {
		t.Parallel()
		links := eth0()
		links[2] = []string{"10.1.0.2"}
		r, w, clk := newChangeFixture(t, links)
		w.setAddrs(2, "10.1.0.3")
		r.CheckInterfaces()
		clk.Advance(time.Second)
		for _, p := range w.take() {
			if p.ifIndex != 2 {
				t.Errorf("published on interface %d", p.ifIndex)
			}
		}
	})

	t.Run("observes interface changes only while services are registered", func(t *testing.T) {
		t.Parallel()
		r, _, _ := newChangeFixture(t, eth0())
		if !r.watch.Running() {
			t.Fatal("not observing with a service published")
		}
		svc := operationalFixture(port1)
		if err := r.Withdraw(context.Background(), svc.InstanceName, svc.ServiceType); err != nil {
			t.Fatal(err)
		}
		if r.watch.Running() {
			t.Error("observing without a service")
		}
	})

	t.Run("sends nothing after close, also not the repeat of an earlier change", func(t *testing.T) {
		t.Parallel()
		r, w, clk := newChangeFixture(t, eth0())
		w.setAddrs(1, "10.0.0.2", "2001:db8:2::10")
		r.CheckInterfaces()
		if err := r.Close(); err != nil {
			t.Fatal(err)
		}
		w.take()
		if r.watch.Running() {
			t.Error("observing after close")
		}
		clk.Advance(2 * time.Second)
		expectSent(t, w.take(), 0)
	})

	t.Run("notices a change at the periodic interface check", func(t *testing.T) {
		t.Parallel()
		_, w, clk := newChangeFixture(t, eth0())
		w.setAddrs(1, "10.0.0.2", "2001:db8:2::10")
		clk.Advance(netwatch.DefaultInterval)
		if got := sentAddresses(w.take()); !slices.Contains(got, "1 2001:db8:1::10 0") {
			t.Errorf("no goodbye for the lost address after one interval: %q", got)
		}
	})

	t.Run("the host disables the periodic interface check", func(t *testing.T) {
		t.Parallel()
		r, w, clk := newChangeFixture(t, eth0())
		r.SetInterfaceCheckInterval(0)
		w.setAddrs(1, "10.0.0.2", "2001:db8:2::10")
		// Past one interval, within the old address's TTL.
		clk.Advance(netwatch.DefaultInterval + time.Second)
		expectSent(t, w.take(), 0)
		r.CheckInterfaces()
		if got := sentAddresses(w.take()); !slices.Contains(got, "1 2001:db8:1::10 0") {
			t.Errorf("CheckInterfaces sent no goodbye: %q", got)
		}
	})
}

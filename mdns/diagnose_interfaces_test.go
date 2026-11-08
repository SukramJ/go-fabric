// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns

import (
	"net"
	"slices"
	"testing"

	"github.com/miekg/dns"
)

// testIfaces are a dual-homed host's interfaces: two LAN links, a
// container bridge, a link that is down and the loopback.
func testIfaces() []hostIface {
	up := func(name string, ips ...string) hostIface {
		h := hostIface{name: name, up: true, multicast: true}
		for _, ip := range ips {
			h.ips = append(h.ips, net.ParseIP(ip))
		}
		return h
	}
	down := up("eth2", "192.168.40.5")
	down.up = false
	lo := up("lo", "127.0.0.1", "::1")
	lo.loopback = true
	return []hostIface{
		up("br10", "192.168.10.116", "fd10::116"),
		up("br30", "192.168.30.116", "fd30::116"),
		up("docker0", "172.17.0.1"),
		down,
		lo,
	}
}

// allowOnly is the filter node.MDNS.Interfaces composes: the default
// filter, and every interface not named.
func allowOnly(names ...string) func(string) bool {
	return func(name string) bool { return DefaultInterfaceFilter(name) || !slices.Contains(names, name) }
}

// TestDiagnoseInterfaces: a name the host lacks, a named container bridge
// and a named interface without an advertised address are findings,
// errors when no named interface advertises, warnings beside one that
// does; an empty list and good names report nothing.
func TestDiagnoseInterfaces(t *testing.T) {
	t.Parallel()
	ifaces := testIfaces()
	codes := func(fs []Finding) []string {
		out := make([]string, 0, len(fs))
		for _, f := range fs {
			out = append(out, string(f.Severity)+" "+f.Code)
		}
		return out
	}
	for _, row := range []struct {
		allow []string
		want  []string
	}{
		{nil, []string{}},
		{[]string{"br30"}, []string{}},
		{[]string{"eth9"}, []string{"error interface_not_found"}},
		{[]string{"br30", "eth9"}, []string{"warning interface_not_found"}},
		{[]string{"docker0"}, []string{"error interface_filtered"}},
		{[]string{"eth2", "lo"}, []string{"error interface_no_addresses", "error interface_no_addresses"}},
	} {
		if got := codes(diagnoseInterfaces(row.allow, ifaces)); !slices.Equal(got, row.want) {
			t.Errorf("%q: %q, want %q", row.allow, got, row.want)
		}
	}
	if DiagnoseInterfaces(nil) != nil {
		t.Error("an empty allow-list reports findings")
	}
}

// TestAllowListAdvertisesOnNamedInterfaceOnly: with an allow-list the
// advertise policy publishes the named interface's addresses alone — the
// br10 address the reflector relayed stays out — and the responder on the
// fake wire announces on that interface only and answers no query on the
// other (issue 450, BD-matterjs-mdns-interface-allowlist).
func TestAllowListAdvertisesOnNamedInterfaceOnly(t *testing.T) {
	t.Parallel()
	ifaces := testIfaces()
	filter := allowOnly("br30", "docker0")
	if got, want := filterPrimaryHostIPs(ifaces, filter), []string{"192.168.30.116", "fd30::116"}; !slices.Equal(got, want) {
		t.Fatalf("published addresses %q, want %q", got, want)
	}
	if got, want := filterPrimaryHostIPs(ifaces, DefaultInterfaceFilter), []string{"192.168.10.116", "192.168.30.116", "fd10::116", "fd30::116"}; !slices.Equal(got, want) {
		t.Fatalf("without an allow-list: %q, want %q", got, want)
	}

	// Links 1 (br10) and 2 (br30), each with what the policy publishes there.
	perLink := map[int][]string{}
	for i, name := range []string{"br10", "br30"} {
		var h hostIface
		for _, c := range ifaces {
			if c.name == name {
				h = c
			}
		}
		perLink[i+1] = filterPrimaryHostIPs([]hostIface{h}, filter)
	}
	const br10, br30 = 1, 2
	r, w, _ := newWiredResponder(map[int][]string{br10: {"192.0.2.1"}, br30: perLink[br30]})
	t.Cleanup(func() { _ = r.Close() })
	w.mu.Lock()
	w.addrsByLink[br10] = nil // excluded by the allow-list: listed, no addresses
	w.mu.Unlock()
	srv := &dns.SRV{Hdr: dns.RR_Header{Name: "ABCD._matterc._udp.local.", Rrtype: dns.TypeSRV, Class: dns.ClassINET, Ttl: 120}, Port: 5540, Target: "host.local."}
	setRecords(r, "svc", srv)
	broadcastKey(r, "svc")
	announced := 0
	for _, p := range w.take() {
		if p.ifIndex != br30 {
			t.Fatalf("announced on interface %d, want only br30 (%d)", p.ifIndex, br30)
		}
		announced++
	}
	if announced == 0 {
		t.Fatal("nothing announced on br30")
	}
	src := &net.UDPAddr{IP: net.ParseIP("192.168.10.9"), Port: 5353}
	deliver(t, r, queryMsg([]dns.Question{question("ABCD._matterc._udp.local.", dns.TypeSRV, false)}), br10, src)
	if sent := w.take(); len(sent) != 0 {
		t.Fatalf("a query on br10 was answered with %d packet(s), want none", len(sent))
	}
}

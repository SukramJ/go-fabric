// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns

// A port of matter.js packages/protocol/test/mdns/MdnsTest.ts at 862ad75f:
// what the advertiser puts on the wire for the operational, commissionable
// and commissioner services — the announcement, its record set, the
// cache-flush bits, the TTL-0 expiry and the split of an oversized
// announcement. The records come from this module's builders, so the TXT
// values are theirs (the operational SAT is always emitted,
// notes/parity/by_design.md BD-mDNS-SAT-AlwaysEmitted); everything else is
// matter.js's expected message. Like matter.js's expectMessage
// (:1075-1110) the comparison is by set, not by order.

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// The MdnsTest fixtures (:33-57).
const (
	serverIPv4 = "192.168.200.1"
	serverIPv6 = "fe80::e777:4f5e:c61e:7314"
	serverHost = "00B0D063C2260000"
	port1      = 5540
	port2      = 5541
	port3      = 5542
)

// mockInstance is the instance id MockCrypto yields (0x80 bytes).
var mockInstance = [8]byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80}

func operationalFixture(port uint16) Service {
	return BuildOperationalService(OperationalServiceConfig{
		CompressedFabricID: [8]byte{0, 0, 0, 0, 0, 0, 0, 0x18},
		NodeID:             1,
		Port:               port,
		HostName:           serverHost,
	})
}

func commissionableFixture(port uint16) Service {
	return BuildCommissionableService(CommissionableServiceConfig{
		InstanceID:        mockInstance,
		DeviceName:        "Test Device",
		CommissioningMode: 1,
		DeviceTypeID:      1,
		VendorID:          1,
		ProductID:         0x8000,
		Discriminator:     1234,
		Port:              port,
		HostName:          serverHost,
	})
}

func commissionerFixture(port uint16, masked bool) Service {
	cfg := testCommissionerConfig(masked)
	cfg.Port = port
	return BuildCommissionerService(cfg)
}

// testCommissionerConfig is MdnsTest.ts:549-554.
func testCommissionerConfig(masked bool) CommissionerServiceConfig {
	return CommissionerServiceConfig{
		InstanceID:    mockInstance,
		DeviceName:    "Test Commissioner",
		DeviceTypeID:  1,
		VendorID:      1,
		ProductID:     0x8000,
		PrivacyMasked: masked,
		HostName:      serverHost,
	}
}

// ipRecords is IPDnsRecords (:66-86): the AAAA, and the A when the server
// has an IPv4 address, both cache-flush.
func ipRecords(withIPv4 bool) []dns.RR {
	out := []dns.RR{flushed(addrRR(serverHost+".local", serverIPv6))}
	if withIPv4 {
		out = append(out, flushed(addrRR(serverHost+".local", serverIPv4)))
	}
	return out
}

// expectedRecords is the record set matter.js expects for svc: the PTRs
// given as (name, target) pairs without the ".local", the SRV and TXT of
// the instance, the address records.
func expectedRecords(svc Service, withIPv4 bool, ptrs ...[2]string) []dns.RR {
	out := make([]dns.RR, 0, len(ptrs)+4)
	for _, p := range ptrs {
		out = append(out, ptrRR(p[0]+".local", p[1]+".local"))
	}
	instance := svc.InstanceName + "." + svc.ServiceType + ".local"
	out = append(out,
		flushed(srvRec(instance, svc.Port, serverHost+".local")),
		flushed(txtRR(instance, svc.MarshalTXT()...)))
	return append(out, ipRecords(withIPv4)...)
}

func serverAddrs(withIPv4 bool) map[int][]string {
	if withIPv4 {
		return map[int][]string{1: {serverIPv4, serverIPv6}}
	}
	return map[int][]string{1: {serverIPv6}}
}

// expectAnnouncement checks one message: transaction id 0, response with
// AA (0x8400), no queries, authorities or additionals, the record set in
// the answer section.
func expectAnnouncement(t *testing.T, p sentPacket, want []dns.RR) {
	t.Helper()
	m := p.msg
	if m.Id != 0 || !m.Response || !m.Authoritative || len(m.Question) != 0 || len(m.Ns) != 0 || len(m.Extra) != 0 {
		t.Errorf("announcement header: id=%d qr=%v aa=%v questions=%d authorities=%d additionals=%d",
			m.Id, m.Response, m.Authoritative, len(m.Question), len(m.Ns), len(m.Extra))
	}
	if p.dst != nil {
		t.Errorf("announcement sent by unicast to %v", p.dst)
	}
	if g, w := sortedRRStrings(m.Answer), sortedRRStrings(want); strings.Join(g, "\n") != strings.Join(w, "\n") {
		t.Errorf("answers:\n got  %s\n want %s", strings.Join(g, "\n      "), strings.Join(w, "\n      "))
	}
}

func TestMdnsAdvertisementParity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, withIPv4 := range []bool{true, false} {
		t.Run(fmt.Sprintf("server IPv4 %v", withIPv4), func(t *testing.T) {
			t.Parallel()
			t.Run("it broadcasts the device fabric on one port and expires", func(t *testing.T) {
				t.Parallel()
				r, w, _ := newWiredResponder(serverAddrs(withIPv4))
				svc := operationalFixture(port1)
				if err := r.Publish(ctx, svc); err != nil {
					t.Fatal(err)
				}
				fabric := "_I0000000000000018._sub._matter._tcp"
				instance := "0000000000000018-0000000000000001._matter._tcp"
				want := expectedRecords(svc, withIPv4,
					[2]string{"_services._dns-sd._udp", "_matter._tcp"},
					[2]string{"_services._dns-sd._udp", fabric},
					[2]string{"_matter._tcp", instance},
					[2]string{fabric, instance},
				)
				got := w.take()
				expectSent(t, got, 1)
				expectAnnouncement(t, got[0], want)
				// And expire: the same set with TTL 0, the cache-flush
				// bits kept (:354-410).
				if err := r.Withdraw(ctx, svc.InstanceName, svc.ServiceType); err != nil {
					t.Fatal(err)
				}
				got = w.take()
				expectSent(t, got, 1)
				expectAnnouncement(t, got[0], goodbyeRecords(want))
			})
			t.Run("it broadcasts the device commissionable info on one port", func(t *testing.T) {
				t.Parallel()
				r, w, _ := newWiredResponder(serverAddrs(withIPv4))
				svc := commissionableFixture(port1)
				if err := r.Publish(ctx, svc); err != nil {
					t.Fatal(err)
				}
				instance := "8080808080808080._matterc._udp"
				ptrs := make([][2]string, 0, 12)
				for _, s := range []string{
					"_matterc._udp", "_V1._sub._matterc._udp", "_T1._sub._matterc._udp",
					"_S4._sub._matterc._udp", "_L1234._sub._matterc._udp", "_CM._sub._matterc._udp",
				} {
					ptrs = append(ptrs, [2]string{"_services._dns-sd._udp", s}, [2]string{s, instance})
				}
				got := w.take()
				expectSent(t, got, 1)
				expectAnnouncement(t, got[0], expectedRecords(svc, withIPv4, ptrs...))
				if err := r.Close(); err != nil {
					t.Fatal(err)
				}
				got = w.take()
				expectSent(t, got, 1)
				expectAnnouncement(t, got[0], goodbyeRecords(expectedRecords(svc, withIPv4, ptrs...)))
			})
			for _, masked := range []bool{false, true} {
				name := "it broadcasts the controller commissioner on one port"
				if masked {
					name = "it omits vendor details from commissioner broadcasts when privacy masked"
				}
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					r, w, _ := newWiredResponder(serverAddrs(withIPv4))
					svc := commissionerFixture(port1, masked)
					if err := r.Publish(ctx, svc); err != nil {
						t.Fatal(err)
					}
					instance := "8080808080808080._matterd._udp"
					subs := []string{"_matterd._udp", "_T1._sub._matterd._udp"}
					if !masked {
						subs = append(subs, "_V1._sub._matterd._udp")
					}
					ptrs := make([][2]string, 0, 2*len(subs))
					for _, s := range subs {
						ptrs = append(ptrs, [2]string{"_services._dns-sd._udp", s}, [2]string{s, instance})
					}
					got := w.take()
					expectSent(t, got, 1)
					expectAnnouncement(t, got[0], expectedRecords(svc, withIPv4, ptrs...))
				})
			}
			t.Run("it announces devices that start together in one announcement with each shared record once", func(t *testing.T) {
				t.Parallel()
				// Three advertisers on three ports started together
				// (MdnsTest.ts:720-748 at db021da0): one publication,
				// split into datagrams by size, every SRV, the host
				// addresses once.
				r, w, _ := newWiredResponder(serverAddrs(withIPv4))
				svcs := []Service{operationalFixture(port1), commissionableFixture(port2), commissionerFixture(port3, false)}
				for _, svc := range svcs {
					if err := r.Publish(ctx, svc); err != nil {
						t.Fatal(err)
					}
				}
				got := w.take()
				var ports []int
				var addrs []string
				for _, p := range got {
					if p.dst != nil || p.ifIndex != 1 {
						t.Fatalf("announcement to %v on %d", p.dst, p.ifIndex)
					}
					for _, rr := range p.msg.Answer {
						switch v := rr.(type) {
						case *dns.SRV:
							ports = append(ports, int(v.Port))
						case *dns.A:
							addrs = append(addrs, v.A.String())
						case *dns.AAAA:
							addrs = append(addrs, v.AAAA.String())
						}
					}
				}
				slices.Sort(ports)
				if want := []int{int(port1), int(port2), int(port3)}; !slices.Equal(ports, slices.Sorted(slices.Values(want))) {
					t.Errorf("SRV ports %v, want %v", ports, want)
				}
				if len(got) > 2 {
					t.Errorf("%d datagrams, want the one announcement split by size", len(got))
				}
				announced := map[string]bool{}
				for _, a := range addrs {
					if announced[a] {
						t.Errorf("host address %s announced twice", a)
					}
					announced[a] = true
				}
				// The operational record with an empty TXT carries one
				// zero byte (RFC 6763 §6.1, :782-783).
				empty := operationalFixture(port1)
				empty.TXT = nil
				r2, w2, _ := newWiredResponder(serverAddrs(withIPv4))
				if err := r2.Publish(ctx, empty); err != nil {
					t.Fatal(err)
				}
				for _, rr := range w2.take()[0].msg.Answer {
					if txt, ok := rr.(*dns.TXT); ok && (len(txt.Txt) != 1 || txt.Txt[0] != "") {
						t.Errorf("empty TXT = %q, want one empty string", txt.Txt)
					}
				}
				// Expiry for all: one publication with each shared record
				// once, split into datagrams by size
				// (InterfaceResponder.ts:445-490 at db021da0).
				if err := r.Close(); err != nil {
					t.Fatal(err)
				}
				got = w.take()
				if len(got) == 0 {
					t.Fatal("no expiry sent")
				}
				seen := map[string]bool{}
				for i, p := range got {
					for _, rr := range p.msg.Answer {
						if rr.Header().Ttl != 0 {
							t.Errorf("expiry %d carries %v with TTL %d", i+1, rr, rr.Header().Ttl)
						}
						if id := recordIdentity(rr); seen[id] {
							t.Errorf("expiry repeats %v", rr)
						} else {
							seen[id] = true
						}
					}
				}
			})
		})
	}
}

func TestMdnsAnnouncementOverflow(t *testing.T) {
	t.Parallel()
	t.Run("splits oversized announcements into multiple packets with all records in the answer section", func(t *testing.T) {
		t.Parallel()
		// Enough AAAA records to push the announcement past the
		// maximum message size (:1002-1006).
		ula := make([]string, 0, 16)
		for i := range 16 {
			ula = append(ula, fmt.Sprintf("fd54:23a1:c6de:1::%d", i+1))
		}
		r, w, _ := newWiredResponder(map[int][]string{1: append(append(append([]string{}, ula...), serverIPv4), serverIPv6)})
		if err := r.Publish(context.Background(), commissionableFixture(port1)); err != nil {
			t.Fatal(err)
		}
		got := w.take()
		if len(got) < 2 {
			t.Fatalf("%d packets, want the full record set to exceed one", len(got))
		}
		var addrs []string
		for _, p := range got {
			// Everything in the answer section (RFC 6762 §8.3); the size
			// limit is checked by the fake wire.
			if len(p.msg.Extra) != 0 || len(p.msg.Answer) == 0 {
				t.Errorf("packet with %d answers and %d additionals", len(p.msg.Answer), len(p.msg.Extra))
			}
			for _, rr := range p.msg.Answer {
				switch a := rr.(type) {
				case *dns.A:
					addrs = append(addrs, a.A.String())
				case *dns.AAAA:
					addrs = append(addrs, a.AAAA.String())
				}
			}
		}
		// SelectionPreference order: link-local, ULA, IPv4 (:1060-1064).
		want := append(append([]string{serverIPv6}, ula...), serverIPv4)
		if strings.Join(addrs, ",") != strings.Join(want, ",") {
			t.Errorf("addresses %v, want %v", addrs, want)
		}
	})
}

// TestMdnsAdvertisementHostOwnedByOS is the module's own rule
// (notes/parity/by_design.md L7-D03): a host name the OS responder owns —
// the HostName override — gets no address records from this responder;
// the SRV names it.
func TestMdnsAdvertisementHostOwnedByOS(t *testing.T) {
	t.Parallel()
	r, w, _ := newWiredResponder(serverAddrs(true))
	r.HostName = "os-host"
	svc := operationalFixture(port1)
	if err := r.Publish(context.Background(), svc); err != nil {
		t.Fatal(err)
	}
	for _, rr := range w.take()[0].msg.Answer {
		switch rr := rr.(type) {
		case *dns.A, *dns.AAAA:
			t.Errorf("address record %v for a host the OS owns", rr)
		case *dns.SRV:
			if rr.Target != "os-host.local." {
				t.Errorf("SRV target %q, want os-host.local.", rr.Target)
			}
		}
	}
	// A bare host question is left to the OS responder.
	deliver(t, r, queryMsg([]dns.Question{question("os-host.local", dns.TypeAAAA, false)}), 1, dummySource)
	expectSent(t, w.take(), 0)
}

// TestMdnsAdvertisementOSHostNameVerdict is the macOS verdict of
// notes/parity/by_design.md L7-D03 (issue 465) on the published record
// set, the Responder's for a Service whose host name the builders
// defaulted: an OS host name the system resolver does not answer for this
// host gives way to the MAC-derived name, whose address records this
// responder publishes and answers (ownsHost), as matter.js does on every
// platform (MdnsServer.ts:80, :608-610 at db021da0); a served one is kept
// and its address records left to the OS responder. The verdict is taken
// again on an interface change, and a moved host name is published on
// every interface (MdnsServer.ts:40-43, :321-339).
func TestMdnsAdvertisementOSHostNameVerdict(t *testing.T) {
	t.Parallel()
	osName := osHostName()
	if osName == "" {
		t.Skip("no OS host name")
	}
	// defaulted is a Service as the builders return it with HostName left
	// empty: the MAC-derived name, marked.
	defaulted := func() Service {
		svc := operationalFixture(port1)
		svc.HostName, svc.hostNameDefaulted = eth0HostName, true
		return svc
	}
	// published is the SRV target and the address records of one packet.
	published := func(p sentPacket) (target string, addrs int) {
		for _, rr := range append(slices.Clone(p.msg.Answer), p.msg.Extra...) {
			switch rr := rr.(type) {
			case *dns.A, *dns.AAAA:
				if rr.Header().Ttl != 0 {
					addrs++
				}
			case *dns.SRV:
				if rr.Header().Ttl != 0 {
					target = rr.Target
				}
			}
		}
		return target, addrs
	}

	for _, served := range []bool{false, true} {
		t.Run(fmt.Sprintf("defaulted, served=%v", served), func(t *testing.T) {
			t.Parallel()
			r, w, _ := newWiredResponder(serverAddrs(true))
			res := newFakeResolver()
			if served {
				res.set(localTestAddr)
			}
			r.policy = testPolicy(t, "darwin", res, true)
			if err := r.Publish(context.Background(), defaulted()); err != nil {
				t.Fatal(err)
			}
			host := eth0HostName
			if served {
				host = osName
			}
			target, addrs := published(w.take()[0])
			if target != host+".local." {
				t.Errorf("SRV target %q, want %s.local.", target, host)
			}
			deliver(t, r, queryMsg([]dns.Question{question(host+".local", dns.TypeAAAA, false)}), 1, dummySource)
			answered := len(w.take())
			if served {
				if addrs != 0 || answered != 0 {
					t.Errorf("served OS host name: %d address records announced, %d answers, want none (left to the OS)", addrs, answered)
				}
				return
			}
			if addrs == 0 || answered == 0 {
				t.Errorf("fallback host name: %d address records announced, %d answers, want both (ownsHost)", addrs, answered)
			}
		})
	}

	t.Run("override: nothing looked up, nothing logged", func(t *testing.T) {
		t.Parallel()
		r, w, _ := newWiredResponder(serverAddrs(true))
		log, buf := testLog()
		r.SetLogger(log)
		res := newFakeResolver()
		r.policy = testPolicy(t, "darwin", res, true)
		r.HostName = "os-host"
		if err := r.Publish(context.Background(), defaulted()); err != nil {
			t.Fatal(err)
		}
		if target, _ := published(w.take()[0]); target != "os-host.local." {
			t.Errorf("SRV target %q, want os-host.local.", target)
		}
		if n := res.calls.Load(); n != 0 {
			t.Errorf("resolver asked %d times with an override, want 0", n)
		}
		if out := buf.String(); strings.Contains(out, "mdns.hostname") {
			t.Errorf("a host-name verdict logged with an override:\n%s", out)
		}
	})

	t.Run("a host name set by hand is never looked up", func(t *testing.T) {
		t.Parallel()
		r, w, _ := newWiredResponder(serverAddrs(true))
		res := newFakeResolver(localTestAddr)
		r.policy = testPolicy(t, "darwin", res, true)
		if err := r.Publish(context.Background(), operationalFixture(port1)); err != nil {
			t.Fatal(err)
		}
		if target, _ := published(w.take()[0]); target != serverHost+".local." {
			t.Errorf("SRV target %q, want %s.local.", target, serverHost)
		}
		if n := res.calls.Load(); n != 0 {
			t.Errorf("resolver asked %d times for a Service built by hand, want 0", n)
		}
	})

	t.Run("an interface change takes the verdict again and moves the host on every interface", func(t *testing.T) {
		t.Parallel()
		r, w, clk := newWiredResponder(map[int][]string{1: {serverIPv4, serverIPv6}, 2: {"10.1.0.2"}})
		t.Cleanup(func() { _ = r.Close() })
		res := newFakeResolver(localTestAddr)
		r.policy = testPolicy(t, "darwin", res, true)
		svc := defaulted()
		if err := r.Publish(context.Background(), svc); err != nil {
			t.Fatal(err)
		}
		r.ServiceConnected(svc.InstanceName, svc.ServiceType)
		clk.Advance(2 * time.Second)
		w.take()

		// change publishes after an address change on interface 2 and
		// reports, per interface, the SRV targets announced and said
		// goodbye to and the address records announced and said goodbye to.
		type view struct {
			srv, srvGoodbye    []string
			addrs, addrGoodbye int
		}
		change := func(addr string) map[int]view {
			w.setAddrs(2, addr)
			r.CheckInterfaces()
			out := map[int]view{}
			for _, p := range w.take() {
				v := out[p.ifIndex]
				for _, rr := range p.msg.Answer {
					gone := rr.Header().Ttl == 0
					switch rr := rr.(type) {
					case *dns.SRV:
						if gone {
							v.srvGoodbye = append(v.srvGoodbye, rr.Target)
						} else {
							v.srv = append(v.srv, rr.Target)
						}
					case *dns.A, *dns.AAAA:
						if gone {
							v.addrGoodbye++
						} else {
							v.addrs++
						}
					}
				}
				out[p.ifIndex] = v
			}
			return out
		}

		// Served → unanswered: the SRV target moves to the MAC-derived
		// name on both interfaces, the old target gets its goodbye, the
		// new name's address records are announced.
		res.set()
		got := change("10.1.0.3")
		for _, i := range []int{1, 2} {
			v := got[i]
			if !slices.Equal(v.srv, []string{eth0HostName + ".local."}) || !slices.Equal(v.srvGoodbye, []string{osName + ".local."}) || v.addrs == 0 {
				t.Errorf("interface %d after the move to the MAC name: %+v, want the new SRV target, the old one's goodbye and address records", i, v)
			}
		}
		if n := res.calls.Load(); n != 2 {
			t.Errorf("resolver asked %d times, want 2 (Publish, interface change)", n)
		}
		clk.Advance(time.Second)
		repeated := map[int]bool{}
		for _, p := range w.take() {
			repeated[p.ifIndex] = true
		}
		if !repeated[1] || !repeated[2] {
			t.Errorf("the change repeat went out on %v, want both interfaces", repeated)
		}

		// Unanswered → served: back to the OS host name, whose address
		// records are the OS responder's — the MAC name's get their
		// goodbye.
		res.set(localTestAddr)
		got = change("10.1.0.4")
		for _, i := range []int{1, 2} {
			v := got[i]
			if !slices.Equal(v.srv, []string{osName + ".local."}) || !slices.Equal(v.srvGoodbye, []string{eth0HostName + ".local."}) || v.addrs != 0 || v.addrGoodbye == 0 {
				t.Errorf("interface %d after the move to the OS name: %+v, want the OS name, the MAC name's goodbyes and no address records", i, v)
			}
		}

		// An unchanged verdict publishes only on the changed interface.
		got = change("10.1.0.5")
		if _, ok := got[1]; ok {
			t.Errorf("published on interface 1 for a change on 2 without a host move: %+v", got)
		}
	})
}

// TestMdnsAdvertisementInterfaceScope: an interface the advertise policy
// excludes carries no records — nothing is announced or answered there —
// and each interface announces its own addresses (matter.js builds the
// record set per interface, MdnsServer.ts:62-87).
func TestMdnsAdvertisementInterfaceScope(t *testing.T) {
	t.Parallel()
	r, w, _ := newWiredResponder(map[int][]string{1: {serverIPv6}, 2: {"fd00::2"}, 3: nil})
	svc := operationalFixture(port1)
	if err := r.Publish(context.Background(), svc); err != nil {
		t.Fatal(err)
	}
	got := w.take()
	expectSent(t, got, 2)
	for _, p := range got {
		want := map[int]string{1: serverIPv6, 2: "fd00::2"}[p.ifIndex]
		var addrs []string
		for _, rr := range p.msg.Answer {
			if a, ok := rr.(*dns.AAAA); ok {
				addrs = append(addrs, a.AAAA.String())
			}
		}
		if len(addrs) != 1 || addrs[0] != want {
			t.Errorf("interface %d announces %v, want [%s]", p.ifIndex, addrs, want)
		}
	}
	deliver(t, r, queryMsg([]dns.Question{question(svc.InstanceName+"._matter._tcp.local", dns.TypeANY, false)}), 3, dummySource)
	expectSent(t, w.take(), 0)
}

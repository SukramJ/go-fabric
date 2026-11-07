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
	"strings"
	"testing"

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
			t.Run("it allows announcements of multiple devices on different ports", func(t *testing.T) {
				t.Parallel()
				// Three advertisers on three ports (:714-990): one
				// announcement message per service, never merged.
				r, w, _ := newWiredResponder(serverAddrs(withIPv4))
				svcs := []Service{operationalFixture(port1), commissionableFixture(port2), commissionerFixture(port3, false)}
				for _, svc := range svcs {
					if err := r.Publish(ctx, svc); err != nil {
						t.Fatal(err)
					}
				}
				got := w.take()
				expectSent(t, got, 3)
				for i, svc := range svcs {
					instance := svc.InstanceName + "." + svc.ServiceType + ".local."
					for _, rr := range got[i].msg.Answer {
						if srv, ok := rr.(*dns.SRV); ok && (srv.Hdr.Name != instance || srv.Port != svc.Port) {
							t.Errorf("message %d carries SRV %v, want %s port %d", i+1, srv, instance, svc.Port)
						}
					}
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

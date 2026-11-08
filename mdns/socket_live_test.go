// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Live tests of the responder's sockets: they bind UDP/5353, join the mDNS
// groups and exchange packets over the host's multicast interfaces. They
// skip where the environment has no multicast (a sandbox, a CI runner
// without it) and under -short.

package mdns

import (
	"context"
	"net"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

// probeMcastV4 reports whether the kernel accepts a multicast join on at
// least one multicast-capable interface.
func probeMcastV4() bool {
	ifaces := listMulticastInterfaces()
	for i := range ifaces {
		mc, err := net.ListenMulticastUDP("udp4", &ifaces[i], &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251)})
		if err == nil {
			_ = mc.Close()
			return true
		}
	}
	return false
}

func skipUnlessMulticastV4(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("live multicast test: skipped under -short")
	}
	if !probeMcastV4() {
		t.Skip("no multicast-capable IPv4 interface: skipping live socket tests")
	}
}

func TestJoinMcast4_Success(t *testing.T) {
	skipUnlessMulticastV4(t)
	pc, err := joinMcast4()
	if err != nil {
		t.Skipf("no IPv4 multicast join on this host (a UDP 5353 socket joined to the mDNS group 224.0.0.251): %v", err)
	}
	if err := pc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// TestJoinMcast6_ProbeAndClose: joinMcast6 either succeeds and closes, or
// fails with an error — never (nil, nil).
func TestJoinMcast6_ProbeAndClose(t *testing.T) {
	if testing.Short() {
		t.Skip("live multicast test: skipped under -short")
	}
	pc, err := joinMcast6()
	if err != nil {
		t.Logf("joinMcast6 failed (acceptable): %v", err)
		return
	}
	if pc == nil {
		t.Fatal("joinMcast6 returned (nil, nil)")
	}
	if err := pc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// TestSockets_Lifecycle opens the sockets, sends on every link, and closes
// them without hanging; a send after close is a no-op.
func TestSockets_Lifecycle(t *testing.T) {
	skipUnlessMulticastV4(t)
	r := NewResponder()
	s, err := openSockets(DefaultInterfaceFilter, r.logger(), &r.clock, func([]byte, int, *net.UDPAddr) {})
	if err != nil {
		t.Skipf("the responder opened no multicast socket on any interface of this host: %v", err)
	}
	pkt, _ := newResponse(0).Pack()
	for _, l := range s.links() {
		_, _ = s.multicast(pkt, l)
		_ = s.link(l.index)
	}
	_, _ = s.unicast(pkt, 1, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 5353})
	_, _ = s.unicast(pkt, 1, &net.UDPAddr{IP: net.IPv6loopback, Port: 5353})
	_, _ = s.unicast(pkt, 1, nil)
	_, _ = s.multicast(pkt, s.link(1<<30))
	done := make(chan error, 1)
	go func() { done <- s.close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("close: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("close hung")
	}
	if err := s.close(); err != nil {
		t.Errorf("second close: %v", err)
	}
	for _, i := range s.links() {
		s.multicast(pkt, i)
	}
}

// liveHost is the SRV target of the live tests' service, a name nothing
// else on the link owns.
const liveHost = "GOFABRICLIVE0000"

// livePublish publishes an operational service on a real responder and
// returns it with the address it advertises on ifi.
func livePublish(t *testing.T, instance string) *Responder {
	t.Helper()
	r := NewResponder()
	t.Cleanup(func() { _ = r.Close() })
	svc := BuildOperationalService(OperationalServiceConfig{
		CompressedFabricID: [8]byte{0x47, 0x46, 0, 0, 0, 0, 0, 1},
		NodeID:             0x362,
		Port:               5540,
		HostName:           liveHost,
	})
	svc.InstanceName = instance
	if err := r.Publish(context.Background(), svc); err != nil {
		t.Skipf("the responder could not publish on this host's multicast interfaces: %v", err)
	}
	return r
}

// awaitAnswer reads the group until a response carries a record match
// accepts, or the deadline passes.
func awaitAnswer(t *testing.T, read func([]byte) (int, error), match func(dns.RR) bool) bool {
	t.Helper()
	buf := make([]byte, 9000)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		n, err := read(buf)
		if err != nil {
			continue
		}
		m := new(dns.Msg)
		if m.Unpack(buf[:n]) != nil || !m.Response {
			continue
		}
		for _, rr := range append(slices.Clone(m.Answer), m.Extra...) {
			if match(rr) {
				return true
			}
		}
	}
	return false
}

// TestResponder_LiveHostAnswerV4 / V6: a bare host-name question sent to
// the group on an advertised interface is answered there with the host's
// address records, TTL 120 s, cache-flush — what TC-SC-4.1 and TC-SC-4.3
// ask for.
func TestResponder_LiveHostAnswerV4(t *testing.T) {
	skipUnlessMulticastV4(t)
	var ifi *net.Interface
	for _, cand := range listMulticastInterfaces() {
		if isPrimaryV4(&cand) && len(hostAddrsFor(nil)(cand.Index)) > 0 {
			ifi = &cand
			break
		}
	}
	if ifi == nil {
		t.Skip("no multicast interface of this host carries a primary IPv4 address the responder announces")
	}
	livePublish(t, "GOFABRICLIVE-V4")
	conn, err := net.ListenMulticastUDP("udp4", ifi, mdnsGroup4)
	if err != nil {
		t.Skipf("this host lets no second socket join the IPv4 mDNS group 224.0.0.251:5353 on %s: %v", ifi.Name, err)
	}
	defer conn.Close()
	pc := ipv4.NewPacketConn(conn)
	_ = pc.SetMulticastLoopback(true)
	q := new(dns.Msg)
	q.SetQuestion(liveHost+".local.", dns.TypeANY)
	q.Id, q.RecursionDesired = 0, false
	out, _ := q.Pack()
	if _, err := pc.WriteTo(out, &ipv4.ControlMessage{IfIndex: ifi.Index}, mdnsGroup4); err != nil {
		t.Skipf("this host refused a datagram to the IPv4 mDNS group on %s: %v", ifi.Name, err)
	}
	ok := awaitAnswer(t, func(b []byte) (int, error) {
		_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		n, _, err := conn.ReadFromUDP(b)
		return n, err
	}, func(rr dns.RR) bool {
		h := rr.Header()
		return strings.EqualFold(h.Name, liveHost+".local.") && (h.Rrtype == dns.TypeA || h.Rrtype == dns.TypeAAAA) &&
			h.Ttl == recordTTL && h.Class&classCacheFlush != 0
	})
	if !ok {
		t.Fatal("no address record for the host name arrived on the group")
	}
}

func TestResponder_LiveHostAnswerV6(t *testing.T) {
	if testing.Short() {
		t.Skip("live multicast test: skipped under -short")
	}
	var ifi *net.Interface
	for _, cand := range listMulticastInterfaces() {
		if isPrimaryV6(&cand) && slices.ContainsFunc(hostAddrsFor(nil)(cand.Index), func(ip net.IP) bool { return ip.To4() == nil }) {
			ifi = &cand
			break
		}
	}
	if ifi == nil {
		t.Skip("no multicast interface of this host carries a primary IPv6 address the responder announces")
	}
	livePublish(t, "GOFABRICLIVE-V6")
	conn, err := net.ListenMulticastUDP("udp6", ifi, mdnsGroup6)
	if err != nil {
		t.Skipf("this host lets no second socket join the IPv6 mDNS group [ff02::fb]:5353 on %s: %v", ifi.Name, err)
	}
	defer conn.Close()
	pc := ipv6.NewPacketConn(conn)
	_ = pc.SetMulticastLoopback(true)
	q := new(dns.Msg)
	q.SetQuestion(liveHost+".local.", dns.TypeAAAA)
	q.Id, q.RecursionDesired = 0, false
	out, _ := q.Pack()
	if _, err := pc.WriteTo(out, &ipv6.ControlMessage{IfIndex: ifi.Index}, &net.UDPAddr{IP: mdnsGroup6.IP, Port: 5353, Zone: ifi.Name}); err != nil {
		t.Skipf("this host refused a datagram to the IPv6 mDNS group on %s: %v", ifi.Name, err)
	}
	ok := awaitAnswer(t, func(b []byte) (int, error) {
		_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		n, _, err := conn.ReadFromUDP(b)
		return n, err
	}, func(rr dns.RR) bool {
		h := rr.Header()
		return strings.EqualFold(h.Name, liveHost+".local.") && h.Rrtype == dns.TypeAAAA && h.Ttl == recordTTL
	})
	if !ok {
		t.Fatal("no AAAA answer for the host name arrived on the group")
	}
}

// TestResponder_LiveOperationalRoundTrip is the wire-level smoke test:
// publish an operational service, then resolve it over the real multicast
// sockets with the module's own querier — PTR to the instance, SRV and TXT
// for it, the host's A/AAAA — every record 120 s.
func TestResponder_LiveOperationalRoundTrip(t *testing.T) {
	skipUnlessMulticastV4(t)
	const instance = "GOFABRICSMOKE0362"
	livePublish(t, instance)
	conn, err := OpenQueryConn()
	if err != nil {
		t.Skipf("no mDNS query socket on this host: joining the mDNS multicast groups failed (%v)", err)
	}
	defer conn.Close()

	ask := func(name string, qtype DNSType) {
		q, err := PackDNSQuery([]DNSQuestion{{Name: name, Type: qtype}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := conn.Send(q); err != nil {
			t.Skipf("this host refused the query to the mDNS multicast groups: %v", err)
		}
	}
	// await collects the records of responses until want holds.
	await := func(want func(DNSRecord) bool) (DNSRecord, bool) {
		deadline := time.After(3 * time.Second)
		for {
			select {
			case <-deadline:
				return DNSRecord{}, false
			case resp := <-conn.Responses():
				m, err := ParseDNSMessage(resp.Msg)
				if err != nil || !m.Response {
					continue
				}
				for _, rec := range append(slices.Clone(m.Answers), m.Additional...) {
					if want(rec) {
						return rec, true
					}
				}
			}
		}
	}
	fqdn := instance + "._matter._tcp.local"
	ask("_matter._tcp.local", DNSTypePTR)
	ptr, ok := await(func(r DNSRecord) bool { return r.Type == DNSTypePTR && strings.EqualFold(r.PTR, fqdn) })
	if !ok {
		t.Fatal("no PTR to the instance")
	}
	ask(fqdn, DNSTypeSRV)
	srv, ok := await(func(r DNSRecord) bool { return r.Type == DNSTypeSRV && strings.EqualFold(r.Name, fqdn) })
	if !ok {
		t.Fatal("no SRV for the instance")
	}
	ask(fqdn, DNSTypeTXT)
	txt, ok := await(func(r DNSRecord) bool { return r.Type == DNSTypeTXT && strings.EqualFold(r.Name, fqdn) })
	if !ok {
		t.Fatal("no TXT for the instance")
	}
	ask(srv.SRV.Target, DNSTypeA)
	addr, ok := await(func(r DNSRecord) bool {
		return (r.Type == DNSTypeA || r.Type == DNSTypeAAAA) && strings.EqualFold(r.Name, srv.SRV.Target)
	})
	if !ok {
		t.Fatal("no address record for the SRV target")
	}
	if srv.SRV.Port != 5540 || !strings.EqualFold(srv.SRV.Target, liveHost+".local") {
		t.Errorf("SRV %+v, want port 5540 target %s.local", srv.SRV, liveHost)
	}
	for _, rec := range []DNSRecord{ptr, srv, txt, addr} {
		if rec.TTL != recordTTL {
			t.Errorf("%v %s TTL %d, want %d", rec.Type, rec.Name, rec.TTL, recordTTL)
		}
	}
}

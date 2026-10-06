// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package mdns

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

// Live multicast: a real responder on 5353 answers a bare-host-name AAAA
// question sent to the mDNS group — the query TC-SC-4.1 / TC-SC-4.3 send —
// and the answer reaches a listener on the group. Skips where the host has
// no usable multicast interface.

const liveHost = "0A0B0C0D0E0F0000.local."

func liveResponder(t *testing.T, ifIndex int) *SubtypeResponder {
	t.Helper()
	r, err := NewSubtypeResponder(nil)
	if err != nil {
		t.Skipf("no multicast responder possible here: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	r.Start(t.Context())
	r.SetHost(liveHost, func(idx int) []net.IP {
		if idx == ifIndex {
			return []net.IP{net.ParseIP("192.0.2.7"), net.ParseIP("2001:db8::7")}
		}
		return nil
	})
	return r
}

// awaitHostAnswer reads responses until one answers liveHost with an AAAA.
func awaitHostAnswer(t *testing.T, read func([]byte) (int, error)) {
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
		for _, rr := range m.Answer {
			if aaaa, ok := rr.(*dns.AAAA); ok && strings.EqualFold(aaaa.Hdr.Name, liveHost) {
				if !aaaa.AAAA.Equal(net.ParseIP("2001:db8::7")) || aaaa.Hdr.Ttl != hostRecordTTL || aaaa.Hdr.Class&classCacheFlush == 0 {
					t.Fatalf("answer %v: want 2001:db8::7, TTL %d, cache-flush", aaaa, hostRecordTTL)
				}
				return
			}
		}
	}
	t.Fatal("no AAAA answer for the host name arrived on the group")
}

func TestHostResponder_LiveMulticastV4(t *testing.T) {
	skipUnlessMulticastV4(t)
	var ifi *net.Interface
	for _, cand := range listMulticastInterfaces() {
		if isPrimaryV4(&cand) {
			ifi = &cand
			break
		}
	}
	if ifi == nil {
		t.Skip("no primary IPv4 multicast interface")
	}
	liveResponder(t, ifi.Index)

	group := &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}
	conn, err := net.ListenMulticastUDP("udp4", ifi, group)
	if err != nil {
		t.Skipf("listen on the group: %v", err)
	}
	defer conn.Close()
	pc := ipv4.NewPacketConn(conn)
	_ = pc.SetMulticastLoopback(true)
	q := new(dns.Msg)
	q.SetQuestion(liveHost, dns.TypeAAAA)
	q.Id, q.RecursionDesired = 0, false
	out, _ := q.Pack()
	if _, err := pc.WriteTo(out, &ipv4.ControlMessage{IfIndex: ifi.Index}, group); err != nil {
		t.Skipf("send to the group: %v", err)
	}
	awaitHostAnswer(t, func(b []byte) (int, error) {
		_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		n, _, err := conn.ReadFromUDP(b)
		return n, err
	})
}

func TestHostResponder_LiveMulticastV6(t *testing.T) {
	var ifi *net.Interface
	for _, cand := range listMulticastInterfaces() {
		if isPrimaryV6(&cand) {
			ifi = &cand
			break
		}
	}
	if ifi == nil {
		t.Skip("no IPv6 multicast interface")
	}
	liveResponder(t, ifi.Index)

	group := &net.UDPAddr{IP: net.ParseIP("ff02::fb"), Port: 5353}
	conn, err := net.ListenMulticastUDP("udp6", ifi, group)
	if err != nil {
		t.Skipf("listen on the group: %v", err)
	}
	defer conn.Close()
	pc := ipv6.NewPacketConn(conn)
	_ = pc.SetMulticastLoopback(true)
	q := new(dns.Msg)
	q.SetQuestion(liveHost, dns.TypeAAAA)
	q.Id, q.RecursionDesired = 0, false
	out, _ := q.Pack()
	if _, err := pc.WriteTo(out, &ipv6.ControlMessage{IfIndex: ifi.Index}, &net.UDPAddr{IP: group.IP, Port: 5353, Zone: ifi.Name}); err != nil {
		t.Skipf("send to the group: %v", err)
	}
	awaitHostAnswer(t, func(b []byte) (int, error) {
		_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		n, _, err := conn.ReadFromUDP(b)
		return n, err
	})
}

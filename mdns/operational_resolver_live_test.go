// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package mdns

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// TestOperationalResolver_LiveMulticastRoundTrip runs the production
// transport: a responder on the mDNS group answers the resolver's
// SRV/TXT query for the operational instance with a multicast response,
// and the resolver returns the advertised address. Skipped where the
// sandbox has no multicast.
func TestOperationalResolver_LiveMulticastRoundTrip(t *testing.T) {
	skipUnlessMulticastV4(t)
	instance := OperationalInstanceQName(testCFID, testNodeID)
	responder, err := joinMcast4()
	if err != nil {
		t.Skipf("join: %v", err)
	}
	t.Cleanup(func() { _ = responder.Close() })
	go func() {
		buf := make([]byte, 9000)
		for {
			n, _, _, err := responder.ReadFrom(buf)
			if err != nil {
				return
			}
			q := new(dns.Msg)
			if q.Unpack(buf[:n]) != nil || q.Response || len(q.Question) == 0 || !strings.EqualFold(q.Question[0].Name, instance) {
				continue
			}
			resp := new(dns.Msg)
			resp.Response = true
			resp.Answer = []dns.RR{srvRR(instance, "live-ctrl.local.", 5540)}
			resp.Extra = []dns.RR{aRR("live-ctrl.local.", "127.0.0.1")}
			out, _ := resp.Pack()
			_, _ = responder.WriteTo(out, nil, &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353})
		}
	}()

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	addrs, err := NewOperationalResolver(nil).ResolveOperational(ctx, testCFID, testNodeID)
	if err != nil {
		t.Skipf("multicast loopback unavailable here: %v", err)
	}
	if len(addrs) == 0 || addrs[0].String() != "127.0.0.1:5540" {
		t.Fatalf("addresses = %v", addrs)
	}
}

// TestOperationalResolver_LiveTimesOut covers the production transport's
// lifecycle when nobody answers.
func TestOperationalResolver_LiveTimesOut(t *testing.T) {
	skipUnlessMulticastV4(t)
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
	defer cancel()
	_, err := NewOperationalResolver(nil).ResolveOperational(ctx, [8]byte{0xEE}, 0xEEEE)
	if !errors.Is(err, ErrOperationalNotResolved) && !strings.Contains(err.Error(), "join") {
		t.Fatalf("err = %v", err)
	}
}

// TestResolution_ZonesLinkLocalByInterface: a link-local address keeps the
// interface it arrived on as its zone, so it stays routable.
func TestResolution_ZonesLinkLocalByInterface(t *testing.T) {
	t.Parallel()
	ifaces, err := net.Interfaces()
	if err != nil || len(ifaces) == 0 {
		t.Skip("no interfaces")
	}
	ifi := ifaces[0]
	instance := OperationalInstanceQName(testCFID, testNodeID)
	r := newResolution(instance)
	stale := aaaaRR("ctrl.local.", "fd00::99")
	stale.Header().Ttl = 0
	r.absorb(received{msg: packResponse(t, srvRR(instance, "ctrl.local.", 5540), aaaaRR("ctrl.local.", "fe80::7"), stale), ifIndex: ifi.Index})
	addrs := r.addresses()
	if len(addrs) != 1 || addrs[0].Zone != ifi.Name || addrs[0].IP.String() != "fe80::7" {
		t.Fatalf("addresses = %v, want fe80::7%%%s only (TTL-0 record ignored)", addrs, ifi.Name)
	}
	if r.absorb(received{msg: []byte{1, 2, 3}}) {
		t.Fatal("garbage must not count as a response")
	}
}

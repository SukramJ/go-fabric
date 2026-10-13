// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridgetest

// The controller's DNS-SD cache without multicast: the commissioner's
// query socket sends nowhere, and the device's operational record —
// read from Options.Advertiser, or the dialled address — is written into
// the cache as an answer, as controller/interaction's live test injects
// it. The peer set resolves the commissioned node through that cache.

import (
	"github.com/miekg/dns"

	"github.com/SukramJ/go-fabric/controller/discovery"
	"github.com/SukramJ/go-fabric/mdns"
)

// deviceHost is the host name the injected records give the device.
const deviceHost = "bridgetest-device.local"

// queryConn is an mdns.QueryConn that sends nowhere; Responses delivers
// what the commissioner writes into it. Every query the cache sends runs
// onQuery first, so a record that appeared since the last one (the
// operational record the device publishes at AddNOC) answers it.
type queryConn struct {
	out     chan mdns.QueryResponse
	onQuery func()
}

func (q *queryConn) Send([]byte) error {
	if q.onQuery != nil {
		q.onQuery()
	}
	return nil
}

func (q *queryConn) Responses() <-chan mdns.QueryResponse { return q.out }

func (q *queryConn) Close() error { return nil }

// answer writes one response with rrs into the cache; a full queue drops
// it, as a socket would, and the cache's next query asks again.
func (q *queryConn) answer(rrs []dns.RR) {
	m := new(dns.Msg)
	m.Response = true
	m.Answer = rrs
	b, err := m.Pack()
	mustNot(err)
	select {
	case q.out <- mdns.QueryResponse{Msg: b}:
	default:
	}
}

// feedOperational writes the device's operational record into the
// cache when it resolves: SRV, TXT (SII=500, SAI=300) and the address
// record under the operational instance name of NodeID on the fabric.
func (c *Commissioner) feedOperational() {
	addr, err := c.operationalAddr()
	if err != nil {
		return
	}
	op := discovery.OperationalQName(c.cfid, c.opts.NodeID)
	hdr := func(name string, typ uint16) dns.RR_Header {
		return dns.RR_Header{Name: dns.Fqdn(name), Rrtype: typ, Class: dns.ClassINET | 0x8000, Ttl: 120}
	}
	rrs := []dns.RR{
		&dns.SRV{Hdr: hdr(op, dns.TypeSRV), Target: dns.Fqdn(deviceHost), Port: uint16(addr.Port)}, //nolint:gosec // a UDP port
		&dns.TXT{Hdr: hdr(op, dns.TypeTXT), Txt: []string{"SII=500", "SAI=300"}},
	}
	if ip4 := addr.IP.To4(); ip4 != nil {
		rrs = append(rrs, &dns.A{Hdr: hdr(deviceHost, dns.TypeA), A: ip4})
	} else {
		rrs = append(rrs, &dns.AAAA{Hdr: hdr(deviceHost, dns.TypeAAAA), AAAA: addr.IP.To16()})
	}
	c.conn.answer(rrs)
}

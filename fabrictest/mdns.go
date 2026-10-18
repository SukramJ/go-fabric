// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package fabrictest

import (
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/miekg/dns"

	"github.com/SukramJ/go-fabric/mdns"
)

// The site's DNS-SD: a controller's discovery cache sends its queries to
// the site, and the site answers them from what every device publishes —
// the records each device's advertiser holds (an mdns.Noop the site gives
// it), its commissionable record while a window is open and its
// operational record per fabric, with the device's address on the
// network. It replaces the DNS answers controller/interaction's and
// controller/client's live tests injected by hand, and multicast with it.

// TTLs of the answers: the host and SRV records 120 s, the PTR and TXT
// records 4500 s, as controller/commissioner's node test announces them.
const (
	ttlHost    uint32 = 120
	ttlService uint32 = 4500
)

// queryConn is a controller's mdns.QueryConn on the site: Send answers at
// once on Responses.
type queryConn struct {
	site *Site
	out  chan mdns.QueryResponse
	once sync.Once
	done chan struct{}
}

func newQueryConn(s *Site) *queryConn {
	return &queryConn{site: s, out: make(chan mdns.QueryResponse, 256), done: make(chan struct{})}
}

func (q *queryConn) Send(msg []byte) error {
	select {
	case <-q.done:
		return nil
	default:
	}
	query := new(dns.Msg)
	if err := query.Unpack(msg); err != nil || query.Response {
		return nil //nolint:nilerr // a socket sends what it is given; an unreadable query is answered by nobody
	}
	rrs := q.site.answer(query.Question)
	if len(rrs) == 0 {
		return nil
	}
	reply := new(dns.Msg)
	reply.Response = true
	reply.Authoritative = true
	reply.Answer = rrs
	b, err := reply.Pack()
	if err != nil {
		return nil //nolint:nilerr // as above: nothing the querier could act on
	}
	select {
	case q.out <- mdns.QueryResponse{Msg: b}:
	default: // a full queue drops, as a socket's would; the querier asks again
	}
	return nil
}

func (q *queryConn) Responses() <-chan mdns.QueryResponse { return q.out }

func (q *queryConn) Close() error {
	q.once.Do(func() { close(q.done) })
	return nil
}

// answer is every record of every device's services a question names: a
// service instance's PTR, SRV, TXT and host address records when a
// question names its service type, one of its subtypes, the instance or
// its host.
func (s *Site) answer(questions []dns.Question) []dns.RR {
	if len(questions) == 0 {
		return nil
	}
	asked := make(map[string]bool, len(questions))
	for _, q := range questions {
		asked[strings.ToLower(dns.Fqdn(q.Name))] = true
	}
	var out []dns.RR
	for _, d := range s.devicesSnapshot() {
		adv, ip, host := d.advertised()
		if adv == nil || ip == nil {
			continue
		}
		for _, svc := range adv.Active() {
			rrs, names := serviceRecords(svc, host, ip)
			for _, name := range names {
				if asked[name] {
					out = append(out, rrs...)
					break
				}
			}
		}
	}
	return out
}

// serviceRecords are svc's records on host at ip, and the lower-case names
// a question may ask them by.
func serviceRecords(svc mdns.Service, host string, ip net.IP) ([]dns.RR, []string) {
	domain := svc.Domain
	if domain == "" {
		domain = "local"
	}
	typeName := svc.ServiceType + "." + domain + "."
	instance := svc.FQDN()
	hdr := func(name string, typ uint16, ttl uint32, unique bool) dns.RR_Header {
		class := uint16(dns.ClassINET)
		if unique {
			class |= 0x8000 // cache-flush: a unique record (RFC 6762 §10.2)
		}
		return dns.RR_Header{Name: name, Rrtype: typ, Class: class, Ttl: ttl}
	}
	rrs := []dns.RR{&dns.PTR{Hdr: hdr(typeName, dns.TypePTR, ttlService, false), Ptr: instance}}
	names := []string{strings.ToLower(typeName), strings.ToLower(instance), strings.ToLower(host)}
	for _, sub := range svc.Subtypes {
		subName := sub + "._sub." + typeName
		rrs = append(rrs, &dns.PTR{Hdr: hdr(subName, dns.TypePTR, ttlService, false), Ptr: instance})
		names = append(names, strings.ToLower(subName))
	}
	rrs = append(rrs,
		&dns.SRV{Hdr: hdr(instance, dns.TypeSRV, ttlHost, true), Target: host, Port: svc.Port},
		&dns.TXT{Hdr: hdr(instance, dns.TypeTXT, ttlService, true), Txt: svc.MarshalTXT()},
	)
	if ip4 := ip.To4(); ip4 != nil {
		rrs = append(rrs, &dns.A{Hdr: hdr(host, dns.TypeA, ttlHost, true), A: ip4})
	} else {
		rrs = append(rrs, &dns.AAAA{Hdr: hdr(host, dns.TypeAAAA, ttlHost, true), AAAA: ip.To16()})
	}
	return rrs, names
}

// hostName is the DNS-SD host name of host index i.
func hostName(i int) string { return "fabrictest-" + strconv.Itoa(i) + ".local." }

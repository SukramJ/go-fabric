// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns

import (
	"net"
	"net/netip"
	"slices"
	"strings"

	"github.com/miekg/dns"
)

const (
	// recordTTL is the TTL of every record the responder publishes, in
	// seconds: matter.js builds each one with DEFAULT_MDNS_TTL (matter.js
	// packages/general/src/codec/DnsCodec.ts:20 at 862ad75f) — PTR, SRV,
	// TXT, A and AAAA alike, the TXT record included where chip keeps 4500 s
	// (concept v12 §D.2: matter.js before chip). A node that vanishes without
	// its goodbyes leaves a commissioner's cache within two minutes.
	recordTTL = 120
	// classCacheFlush is the top bit of the class field: cache-flush in a
	// response record (RFC 6762 §10.2), unicast-response in a question
	// (RFC 6762 §5.4).
	classCacheFlush = 1 << 15
	// serviceDiscoveryName is the DNS-SD service-type enumeration name
	// (RFC 6763 §9), matter.js SERVICE_DISCOVERY_QNAME.
	serviceDiscoveryName = "_services._dns-sd._udp"
)

// serviceNames are the DNS names one published [Service] owns, in
// presentation form with a trailing dot.
type serviceNames struct {
	// domain is the service's domain without dots ("local").
	domain string
	// serviceType is "<service-type>.<domain>.".
	serviceType string
	// instance is "<instance>.<service-type>.<domain>.".
	instance string
	// subtypes are "<subtype>._sub.<service-type>.<domain>.", in the
	// Service's order, empty labels dropped.
	subtypes []string
	// host is the SRV target, "<host>.<domain>.".
	host string
	// enumeration is "_services._dns-sd._udp.<domain>.".
	enumeration string
}

// namesOf derives the names of svc with host as the SRV target (the
// advertiser's override or the Service's own host name).
func namesOf(svc Service, host string) serviceNames {
	d := strings.Trim(svc.Domain, ".")
	if d == "" {
		d = "local"
	}
	n := serviceNames{
		domain:      d,
		serviceType: svc.ServiceType + "." + d + ".",
		instance:    svc.InstanceName + "." + svc.ServiceType + "." + d + ".",
		enumeration: serviceDiscoveryName + "." + d + ".",
	}
	for _, sub := range svc.Subtypes {
		if sub == "" {
			continue
		}
		n.subtypes = append(n.subtypes, sub+"._sub."+svc.ServiceType+"."+d+".")
	}
	h := strings.Trim(host, ".")
	if !strings.HasSuffix(strings.ToLower(h), "."+strings.ToLower(d)) {
		h += "." + d
	}
	n.host = h + "."
	return n
}

// recordSet is the full record set of one published service on one
// interface — what an announcement carries and what a query is answered
// from. Mirrors matter.js
// packages/protocol/src/advertisement/mdns/MdnsAdvertisement.ts:154-184
// (#recordsFor) at 862ad75f: the PTR records, the SRV record (priority 0,
// weight 0, the port, the host as target), the TXT record and one A or
// AAAA record per address of the interface, IPv6 before IPv4 and then in
// selection-preference order (:166-177). The unique types — SRV, TXT, A,
// AAAA — carry the cache-flush bit, the shared PTRs do not (:181-183,
// DnsCodec.ts:198-208 isUnique). Every record carries [recordTTL].
//
// The PTR records follow OperationalMdnsAdvertisement.ts:33-41,
// CommissionableMdnsAdvertisement.ts:43-71 and
// CommissionerMdnsAdvertisement.ts:33-55: the enumeration name points at
// the service type and at each subtype, the service type and each subtype
// point at the instance. The Service's Subtypes are the subtypes the
// builders derived (the fabric subtype, `_L`, `_S`, `_CM`, `_T`, `_V`), so
// one generic list covers the three advertisement kinds; matter.js's order
// differs per kind and its tests compare the set, not the order
// (packages/protocol/test/mdns/MdnsTest.ts:1075-1110 expectMessage).
//
// addrs nil leaves the address records out: the host name belongs to the
// OS responder ([Responder.HostName], the macOS host name), which
// publishes them itself.
func recordSet(svc Service, names serviceNames, txt []string, addrs []net.IP) []dns.RR {
	ptr := func(name, target string) dns.RR {
		return &dns.PTR{Hdr: dns.RR_Header{Name: name, Rrtype: dns.TypePTR, Class: dns.ClassINET, Ttl: recordTTL}, Ptr: target}
	}
	unique := func(name string, t uint16) dns.RR_Header {
		return dns.RR_Header{Name: name, Rrtype: t, Class: dns.ClassINET | classCacheFlush, Ttl: recordTTL}
	}
	out := make([]dns.RR, 0, 4+2*len(names.subtypes)+len(addrs))
	out = append(out, ptr(names.enumeration, names.serviceType))
	for _, sub := range names.subtypes {
		out = append(out, ptr(names.enumeration, sub))
	}
	out = append(out, ptr(names.serviceType, names.instance))
	for _, sub := range names.subtypes {
		out = append(out, ptr(sub, names.instance))
	}
	out = append(out, &dns.SRV{Hdr: unique(names.instance, dns.TypeSRV), Priority: 0, Weight: 0, Port: svc.Port, Target: names.host})
	if len(txt) == 0 {
		// RFC 6763 §6.1: an empty TXT record is a single zero byte
		// (matter.js MdnsTest.ts:782-789 expects [""]).
		txt = []string{""}
	}
	out = append(out, &dns.TXT{Hdr: unique(names.instance, dns.TypeTXT), Txt: txt})
	return append(out, addressRecords(names.host, addrs)...)
}

// addressRecords are the A/AAAA records of host for addrs: IPv6 first,
// then IPv4, stably sorted by [SelectionPreference]
// (MdnsAdvertisement.ts:166-177 at 862ad75f), each with the cache-flush bit.
func addressRecords(host string, addrs []net.IP) []dns.RR {
	v6 := make([]netip.Addr, 0, len(addrs))
	var v4 []netip.Addr
	for _, ip := range addrs {
		a, ok := netip.AddrFromSlice(ip)
		if !ok {
			continue
		}
		if a = a.Unmap(); a.Is4() {
			v4 = append(v4, a)
		} else {
			v6 = append(v6, a)
		}
	}
	all := append(v6, v4...)
	slices.SortStableFunc(all, func(a, b netip.Addr) int { return SelectionPreference(a) - SelectionPreference(b) })
	out := make([]dns.RR, 0, len(all))
	for _, a := range all {
		hdr := dns.RR_Header{Name: host, Class: dns.ClassINET | classCacheFlush, Ttl: recordTTL}
		if a.Is4() {
			hdr.Rrtype = dns.TypeA
			out = append(out, &dns.A{Hdr: hdr, A: net.IP(a.AsSlice())})
		} else {
			hdr.Rrtype = dns.TypeAAAA
			out = append(out, &dns.AAAA{Hdr: hdr, AAAA: net.IP(a.AsSlice())})
		}
	}
	return out
}

// withTTL returns copies of rrs carrying ttl — a goodbye is the record set
// with TTL 0, the cache-flush bits kept (matter.js
// packages/protocol/src/mdns/MdnsServer.ts:253-256 at 862ad75f;
// MdnsTest.ts:354-410 expects the bits on the expiry).
func withTTL(rrs []dns.RR, ttl uint32) []dns.RR {
	out := make([]dns.RR, len(rrs))
	for i, rr := range rrs {
		c := dns.Copy(rr)
		c.Header().Ttl = ttl
		out[i] = c
	}
	return out
}

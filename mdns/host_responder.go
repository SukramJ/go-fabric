// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package mdns

import (
	"log/slog"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

// The bare-host-name questions (`<MAC>0000.local. AAAA`) are answered here,
// not by grandcat/zeroconf: the library's Server.handleQuestion answers only
// the service-type, service-name and instance-name questions and emits the
// host's A/AAAA records solely as additionals of an SRV answer. A resolver
// that follows an SRV target with its own address query — the CHIP Python
// harness does (TC-SC-4.1, TC-SC-4.3: "No AAAA addresses were resolved for
// hostname") and so does any querier whose cache lost the additionals — got
// no answer at all. matter.js answers it: its MdnsServer registers every
// advertised name, the host name included, as a name it responds for
// (packages/protocol/src/mdns/MdnsServer.ts #registerResponderNames) and
// answers an A/AAAA/ANY question for it from the interface's address
// records (MdnsAdvertisement.ts #recordsFor).

const (
	// hostRecordTTL is the TTL of the A/AAAA records: RFC 6762 §10's 120 s
	// for records that name a host, matter.js DEFAULT_MDNS_TTL
	// (packages/general/src/codec/DnsCodec.ts) and grandcat's appendAddrs.
	hostRecordTTL = 120
	// questionSuppressionWindow is RFC 6762 §7.3's duplicate-question window,
	// matter.js QUESTION_SUPPRESSION_WINDOW (999 ms, after python-zeroconf).
	questionSuppressionWindow = 999 * time.Millisecond
	// multicastRateLimit is the minimum gap between two multicasts of one
	// record on one interface (RFC 6762 §6: "at most once per second";
	// matter.js MdnsServer #handleMessage uses 900 ms).
	multicastRateLimit = 900 * time.Millisecond
	// classCacheFlush is the top bit of the class field: cache-flush in a
	// response record (RFC 6762 §10.2), unicast-response in a question
	// (RFC 6762 §5.4).
	classCacheFlush = 1 << 15
)

// hostAnswerer answers A, AAAA and ANY questions for the advertiser's own
// host name. It is the record-and-policy half of the host-name responder:
// the [SubtypeResponder] feeds it the queries its sockets receive and sends
// what it returns. Its decisions mirror matter.js MdnsServer #handleMessage
// for one record set:
//
//   - only the records of the interface the query arrived on are answered
//     (matter.js generates the record set per interface); an interface the
//     advertise policy excludes has none, and its queries go unanswered;
//   - known-answer suppression (RFC 6762 §7.1): a record the querier lists
//     with more than half its TTL left is not sent;
//   - a question with the unicast-response bit is answered by unicast only
//     when the records went out by multicast on that interface within a
//     quarter of their TTL (RFC 6762 §5.4), otherwise by multicast;
//   - a multicast answer repeats no record sent within 900 ms on that
//     interface, and a question another responder answered with the same
//     records within 999 ms is not answered again (RFC 6762 §7.3);
//   - the cache-flush bit is set on the unique A/AAAA records and cleared
//     where it would claim more than is sent: in a unicast response, and on
//     a record set that suppression left incomplete (matter.js `sendable`).
type hostAnswerer struct {
	mu sync.Mutex
	// name is the host's FQDN with a trailing dot; "" answers nothing.
	name string
	// addrsFor returns the addresses the host advertises on an interface
	// (by index); nil or empty when the interface is not advertised on.
	addrsFor func(ifIndex int) []net.IP
	// lastMulticast is when a record set (recordKey) last went out by
	// multicast; matter.js #recordLastSentAsMulticastAnswer.
	lastMulticast map[string]time.Time
	// recentlyAnswered is matter.js #recentlyAnsweredQueries: per question
	// signature and interface, the records known to the link.
	recentlyAnswered map[string]recentAnswer
	now              func() time.Time
}

type recentAnswer struct {
	known map[string]bool
	at    time.Time
}

// hostReply is one response the answerer decided to send.
type hostReply struct {
	msg *dns.Msg
	// unicast sends msg to dst (the querier) instead of the multicast group.
	unicast bool
	dst     *net.UDPAddr
	ifIndex int
}

func newHostAnswerer() *hostAnswerer {
	return &hostAnswerer{
		lastMulticast:    map[string]time.Time{},
		recentlyAnswered: map[string]recentAnswer{},
		now:              time.Now,
	}
}

// set replaces the host name and its address source. An empty name stops
// answering. The rate-limit and suppression state is reset, as matter.js
// #resetServices does when the records change.
func (h *hostAnswerer) set(name string, addrsFor func(ifIndex int) []net.IP) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if name == "" || addrsFor == nil {
		h.name, h.addrsFor = "", nil
	} else {
		h.name, h.addrsFor = ensureTrailingDot(name), addrsFor
	}
	clear(h.lastMulticast)
	clear(h.recentlyAnswered)
}

// hostName returns the name answered for, "" when none.
func (h *hostAnswerer) hostName() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.name
}

// records is the interface's A/AAAA record set, IPv6 first in matter.js
// selection-preference order (MdnsAdvertisement.ts #recordsFor sorts by
// ServerAddress.selectionPreferenceOfIp), each flagged cache-flush.
func (h *hostAnswerer) records(ifIndex int) []dns.RR {
	ips := h.addrsFor(ifIndex)
	addrs := make([]netip.Addr, 0, len(ips))
	for _, ip := range ips {
		if a, ok := netip.AddrFromSlice(ip); ok {
			addrs = append(addrs, a.Unmap())
		}
	}
	slices.SortStableFunc(addrs, func(a, b netip.Addr) int { return SelectionPreference(a) - SelectionPreference(b) })
	out := make([]dns.RR, 0, len(addrs))
	for _, a := range addrs {
		hdr := dns.RR_Header{Name: h.name, Class: dns.ClassINET | classCacheFlush, Ttl: hostRecordTTL}
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

// answer decides the response to a query received on ifIndex from src.
// ok is false when nothing is sent.
func (h *hostAnswerer) answer(query *dns.Msg, ifIndex int, src *net.UDPAddr) (reply hostReply, ok bool) {
	if query == nil || query.Response || len(query.Question) == 0 {
		return hostReply{}, false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.name == "" {
		return hostReply{}, false
	}
	owned := false
	for _, q := range query.Question {
		if strings.EqualFold(q.Name, h.name) {
			owned = true
			break
		}
	}
	if !owned {
		return hostReply{}, false
	}
	all := h.records(ifIndex)
	if len(all) == 0 {
		// Not an interface the host advertises on: its records are not
		// this link's (matter.js: no records for the interface, no answer).
		return hostReply{}, false
	}

	answers, additional := h.collect(query, all)
	if len(answers) == 0 {
		return hostReply{}, false
	}
	if len(query.Answer) > 0 {
		answers = slices.DeleteFunc(answers, func(rr dns.RR) bool { return knownTo(query.Answer, rr) })
		if len(answers) == 0 {
			return hostReply{}, false
		}
		additional = slices.DeleteFunc(additional, func(rr dns.RR) bool { return knownTo(query.Answer, rr) })
	}

	now := h.now()
	sinceMulticast := func(rr dns.RR) time.Duration {
		last, seen := h.lastMulticast[recordKey(rr, ifIndex)]
		if !seen {
			return time.Duration(1<<63 - 1)
		}
		return now.Sub(last)
	}
	unicast := src != nil && unicastAllowed(query, answers, sinceMulticast)
	if !unicast {
		answers = slices.DeleteFunc(answers, func(rr dns.RR) bool { return sinceMulticast(rr) < multicastRateLimit })
		if len(answers) == 0 {
			return hostReply{}, false
		}
		if h.suppressDuplicate(query, ifIndex, answers, now) {
			return hostReply{}, false
		}
		for _, rr := range answers {
			h.lastMulticast[recordKey(rr, ifIndex)] = now
		}
	}

	resp := new(dns.Msg)
	resp.Response = true
	resp.Authoritative = true
	// RFC 6762 §6: no questions. The ID echoes the query's, as matter.js
	// does (transactionId) — 0 from a conforming mDNS querier (§18.1).
	resp.Id = query.Id
	resp.Answer = sendable(answers, all, unicast)
	resp.Extra = sendable(additional, all, unicast)
	return hostReply{msg: resp, unicast: unicast, dst: src, ifIndex: ifIndex}, true
}

// collect matches the questions against the interface's record set: the
// answers, and — for a query that is not purely A/AAAA — the rest of the set
// as additionals (matter.js #handleMessage).
func (h *hostAnswerer) collect(query *dns.Msg, all []dns.RR) (answers, additional []dns.RR) {
	onlyAddressQuestions := true
	for _, q := range query.Question {
		if q.Qtype != dns.TypeA && q.Qtype != dns.TypeAAAA {
			onlyAddressQuestions = false
		}
		if !strings.EqualFold(q.Name, h.name) {
			continue
		}
		for _, rr := range all {
			if q.Qtype == dns.TypeANY || q.Qtype == rr.Header().Rrtype {
				answers = append(answers, rr)
			}
		}
	}
	if !onlyAddressQuestions {
		for _, rr := range all {
			if !slices.Contains(answers, rr) {
				additional = append(additional, rr)
			}
		}
	}
	return answers, additional
}

// unicastAllowed is the QU rule (RFC 6762 §5.4, matter.js #handleMessage):
// every question asks for a unicast response, and each answer went out by
// multicast on this link within a quarter of its TTL.
func unicastAllowed(query *dns.Msg, answers []dns.RR, sinceMulticast func(dns.RR) time.Duration) bool {
	for _, q := range query.Question {
		if q.Qclass&classCacheFlush == 0 {
			return false
		}
	}
	for _, rr := range answers {
		if sinceMulticast(rr) > time.Duration(rr.Header().Ttl)*time.Second/4 {
			// Not multicast on this link within a quarter of the TTL:
			// the link's caches need it, answer by multicast.
			return false
		}
	}
	return true
}

// suppressDuplicate is matter.js #shouldSuppressResponse (RFC 6762 §7.3):
// the same question answered on the interface within the window, with every
// record we would send already known to the link, is not answered again.
// Otherwise the answer is recorded for the next duplicate. Caller holds mu.
func (h *hostAnswerer) suppressDuplicate(query *dns.Msg, ifIndex int, answers []dns.RR, now time.Time) bool {
	for k, e := range h.recentlyAnswered {
		if now.Sub(e.at) >= questionSuppressionWindow {
			delete(h.recentlyAnswered, k)
		}
	}
	sig := make([]string, 0, len(query.Question))
	for _, q := range query.Question {
		sig = append(sig, strings.ToLower(q.Name)+"-"+dns.TypeToString[q.Qtype])
	}
	slices.Sort(sig)
	key := strings.Join(sig, "|") + "-" + strconv.Itoa(ifIndex)
	if e, ok := h.recentlyAnswered[key]; ok && now.Sub(e.at) < questionSuppressionWindow {
		if !slices.ContainsFunc(answers, func(rr dns.RR) bool { return !e.known[recordKey(rr, ifIndex)] }) {
			return true
		}
	}
	known := map[string]bool{}
	for _, rr := range query.Answer {
		known[recordKey(rr, ifIndex)] = true
	}
	for _, rr := range answers {
		known[recordKey(rr, ifIndex)] = true
	}
	h.recentlyAnswered[key] = recentAnswer{known: known, at: now}
	return false
}

// recordKey identifies a record set on an interface as matter.js
// buildDnsRecordKey does: name, class, type and interface — not the value.
func recordKey(rr dns.RR, ifIndex int) string {
	hdr := rr.Header()
	return strings.ToLower(hdr.Name) + "-" + strconv.Itoa(int(hdr.Class&^classCacheFlush)) + "-" +
		strconv.Itoa(int(hdr.Rrtype)) + "-" + strconv.Itoa(ifIndex)
}

// knownTo reports whether a known answer of the query carries rr (RFC 6762
// §7.1, matter.js #suppressedByKnownAnswer): same type, class (the
// cache-flush bit is framing), name and value, with more than half of rr's
// TTL left.
func knownTo(known []dns.RR, rr dns.RR) bool {
	hdr := rr.Header()
	for _, k := range known {
		kh := k.Header()
		if kh.Rrtype != hdr.Rrtype || kh.Class&^classCacheFlush != hdr.Class&^classCacheFlush ||
			uint64(kh.Ttl)*2 <= uint64(hdr.Ttl) || !strings.EqualFold(kh.Name, hdr.Name) {
			continue
		}
		switch r := rr.(type) {
		case *dns.A:
			if ka, ok := k.(*dns.A); ok && ka.A.Equal(r.A) {
				return true
			}
		case *dns.AAAA:
			if ka, ok := k.(*dns.AAAA); ok && ka.AAAA.Equal(r.AAAA) {
				return true
			}
		}
	}
	return false
}

// sendable clears the cache-flush bit wherever it would claim more than the
// response carries (matter.js MdnsServer.ts `sendable`, RFC 6762 §10.2 and
// §6.7): on every record of a unicast response, and on a record whose set
// (name, type, class) is sent incompletely.
func sendable(sent, complete []dns.RR, unicast bool) []dns.RR {
	if len(sent) == 0 {
		return nil
	}
	setKey := func(rr dns.RR) string {
		hdr := rr.Header()
		return strconv.Itoa(int(hdr.Rrtype)) + "-" + strconv.Itoa(int(hdr.Class&^classCacheFlush)) + "-" + strings.ToLower(hdr.Name)
	}
	sizes := func(rrs []dns.RR) map[string]int {
		m := map[string]int{}
		for _, rr := range rrs {
			m[setKey(rr)]++
		}
		return m
	}
	sentSizes, completeSizes := sizes(sent), sizes(complete)
	out := make([]dns.RR, 0, len(sent))
	for _, rr := range sent {
		if rr.Header().Class&classCacheFlush != 0 && (unicast || sentSizes[setKey(rr)] < completeSizes[setKey(rr)]) {
			rr = dns.Copy(rr)
			rr.Header().Class &^= classCacheFlush
		}
		out = append(out, rr)
	}
	return out
}

// hostSender is the transport a host-name answer goes out on.
type hostSender interface {
	// multicast sends pkt to the mDNS groups on the interface: the IPv4
	// group when the interface has an IPv4 address, the IPv6 group when it
	// has an IPv6 one (matter.js UdpMulticastServer.send per interface).
	multicast(pkt []byte, ifIndex int)
	// unicast sends pkt to the querier through the interface.
	unicast(pkt []byte, ifIndex int, dst *net.UDPAddr)
}

// SetHost makes the responder answer A, AAAA and ANY questions for name
// (the SRV target the advertiser publishes) with the addresses addrsFor
// returns for the interface a query arrived on — nil or empty for an
// interface the host does not advertise on, whose queries then go
// unanswered. An empty name stops answering. [Zeroconf] calls it on every
// Publish with its own host name and address policy; a name the OS
// responder owns (the OS host name) must not be set, or two responders
// answer for it.
func (r *SubtypeResponder) SetHost(name string, addrsFor func(ifIndex int) []net.IP) {
	if r == nil || r.host == nil {
		return
	}
	if r.host.hostName() == ensureTrailingDot(name) && name != "" {
		return
	}
	r.host.set(name, addrsFor)
}

// answerHost answers a received packet's host-name questions, if any.
func (r *SubtypeResponder) answerHost(buf []byte, ifIndex int, src net.Addr) {
	if r.host == nil || r.hostSend == nil || r.host.hostName() == "" {
		return
	}
	msg := new(dns.Msg)
	if err := msg.Unpack(buf); err != nil {
		return
	}
	from, _ := src.(*net.UDPAddr)
	reply, ok := r.host.answer(msg, ifIndex, from)
	if !ok {
		return
	}
	out, err := reply.msg.Pack()
	if err != nil {
		r.logger.Debug("matter.mdns.host.pack_err", slog.String("err", err.Error()))
		return
	}
	if reply.unicast {
		r.hostSend.unicast(out, reply.ifIndex, reply.dst)
	} else {
		r.hostSend.multicast(out, reply.ifIndex)
	}
	r.logger.Debug("matter.mdns.host.answered",
		slog.String("host", r.host.hostName()),
		slog.Int("ifindex", reply.ifIndex),
		slog.Bool("unicast", reply.unicast),
		slog.Int("records", len(reply.msg.Answer)+len(reply.msg.Extra)))
}

// socketHostSender sends through the responder's own multicast sockets.
type socketHostSender struct{ r *SubtypeResponder }

func (s socketHostSender) multicast(pkt []byte, ifIndex int) {
	ifi, err := net.InterfaceByIndex(ifIndex)
	if err != nil {
		return
	}
	has4, has6 := false, false
	if addrs, err := ifi.Addrs(); err == nil {
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok {
				if ipn.IP.To4() != nil {
					has4 = true
				} else {
					has6 = true
				}
			}
		}
	}
	if has4 && s.r.pc4 != nil {
		_, _ = s.r.pc4.WriteTo(pkt, &ipv4.ControlMessage{IfIndex: ifIndex}, &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353})
	}
	if has6 && s.r.pc6 != nil {
		_, _ = s.r.pc6.WriteTo(pkt, &ipv6.ControlMessage{IfIndex: ifIndex}, &net.UDPAddr{IP: net.ParseIP("ff02::fb"), Port: 5353})
	}
}

// unicast answers the querier at its source address and port: 5353 for an
// mDNS querier, where matter.js sends (it cannot see the source port), and
// the legacy querier's own port otherwise (RFC 6762 §6.7).
func (s socketHostSender) unicast(pkt []byte, ifIndex int, dst *net.UDPAddr) {
	if dst == nil {
		return
	}
	if dst.IP.To4() != nil {
		if s.r.pc4 != nil {
			_, _ = s.r.pc4.WriteTo(pkt, &ipv4.ControlMessage{IfIndex: ifIndex}, dst)
		}
		return
	}
	if s.r.pc6 != nil {
		_, _ = s.r.pc6.WriteTo(pkt, &ipv6.ControlMessage{IfIndex: ifIndex}, dst)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns

import (
	"errors"
	"log/slog"
	"math/rand/v2"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/miekg/dns"

	"github.com/SukramJ/go-fabric/clock"
)

// The answer engine: one inbound query against the record sets of the
// published services on the interface it arrived on. It mirrors matter.js
// packages/protocol/src/mdns/InterfaceResponder.ts answer and #sendAnswer
// (:247-396 at db021da0) and MdnsServer.ts #handleMessage and
// #prepareMessage (:103-121, :463-503):
//
//   - a query for no name the responder owns is dropped before anything
//     else, and an interface without records is not answered on;
//   - each service's record set is matched on its own (queryRecords: the
//     name case-insensitively, ANY or the exact type) and answered in its
//     own packet, 20–120 ms after the previous one, its content decided
//     when it is sent;
//   - the rest of the set except its PTRs goes along as additionals,
//     unless every question asks for A or AAAA (additionalRecordsFor,
//     :910-916);
//   - a record the querier lists as a known answer with more than half its
//     TTL left is not sent;
//   - a query whose every question carries the unicast-response bit is
//     answered by unicast, unless an answer has not gone out by multicast
//     on the interface within a quarter of its TTL while peers cache it;
//   - a multicast answer obeys the one-second rule with its deferral
//     (hold.go);
//   - a truncated query waits 400–500 ms for its continuation packets and
//     is answered as one;
//   - the cache-flush bit is cleared where it would claim more than the
//     response carries (sendable, :934-962);
//   - a response that outgrows 1232 bytes is split, answers first and the
//     additionals spilled after them (packages/general/src/net/dns-sd/
//     MdnsSocket.ts:131-201, MAX_MDNS_MESSAGE_SIZE DnsCodec.ts:26).

const (
	// maxMessageSize is the largest message the responder sends,
	// matter.js MAX_MDNS_MESSAGE_SIZE (packages/general/src/codec/
	// DnsCodec.ts:26 at 862ad75f): the IPv6 minimum MTU less the IPv6 and
	// UDP headers.
	maxMessageSize = 1232
	// dnsHeaderSize is the fixed DNS header.
	dnsHeaderSize = 12
	// recordCacheLifetime is how long the per-interface record sets are
	// reused before the interface's addresses are read again, and how long
	// an interface whose peers cache nothing stays unused before its state
	// goes — matter.js INTERFACE_LIFETIME (InterfaceResponder.ts:37-40 at
	// db021da0, Minutes(15)).
	recordCacheLifetime = 15 * time.Minute

	// truncatedWait and truncatedWaitJitter are the wait for the
	// continuation packets of a truncated query: 400–500 ms, RFC 6762 §7.2,
	// counted from the query's first packet.
	truncatedWait       = 400 * time.Millisecond
	truncatedWaitJitter = 100
	// maxTruncatedPackets is how many packets of one truncated query are
	// gathered before it is answered, the wait cut short — the module's own
	// choice: a querier fills a packet of the usual 1500-byte MTU with about
	// 29 PTR known answers of a Matter instance name (48 bytes each
	// compressed), so 32 packets carry some 900 — more than the instances of
	// one service type a single link holds.
	maxTruncatedPackets = 32
	// maxTruncatedQuestions is how many distinct questions a held
	// truncated query keeps; the packet that brings more completes it — the
	// module's own choice. RFC 6762 §7.2 has the continuation packets carry
	// no questions, so the questions of the first packet are what a querier
	// needs, and a querier asks a few per packet.
	maxTruncatedQuestions = 64
	// maxTruncatedQueries is how many truncated queries wait at once,
	// across every source, interface and transaction id; a truncated query
	// beyond it is answered at once with its first packet — the module's
	// own choice: each waits at most 500 ms, and a link has a few queriers
	// with a known-answer list too long for one packet.
	maxTruncatedQueries = 64
)

// answerState is the engine's memory between queries: the truncated
// queries waiting for their continuation, by transaction id, interface and
// source, at most [maxTruncatedQueries]. The Responder's mutex guards it.
type answerState struct {
	truncated map[string]*truncatedQuery
}

// truncatedQuery is a truncated query gathered so far: its transaction id,
// its distinct questions and the known answers that suppress a record of
// the interface — no more than the interface publishes, whatever the
// querier lists. Its timer runs out 400–500 ms after the first packet.
type truncatedQuery struct {
	msg       *dns.Msg
	questions map[string]bool
	packets   int
	timer     clock.Timer
}

// absorb adds one packet of the query: its questions not held yet, and each
// known answer that suppresses a record of records (the interface's, each
// once) no held known answer suppresses yet. It reports whether the query
// is complete by its bounds: [maxTruncatedPackets] packets, or more than
// [maxTruncatedQuestions] questions.
func (t *truncatedQuery) absorb(msg *dns.Msg, records []dns.RR) bool {
	t.packets++
	for _, q := range msg.Question {
		k := strings.ToLower(q.Name) + "-" + strconv.Itoa(int(q.Qtype)) + "-" + strconv.Itoa(int(q.Qclass))
		if !t.questions[k] {
			t.questions[k] = true
			t.msg.Question = append(t.msg.Question, q)
		}
	}
	if len(msg.Answer) > 0 {
		open := slices.DeleteFunc(slices.Clone(records), func(rr dns.RR) bool { return knownTo(t.msg.Answer, rr) })
		for _, k := range msg.Answer {
			suppressed := func(rr dns.RR) bool { return knownTo([]dns.RR{k}, rr) }
			if slices.ContainsFunc(open, suppressed) {
				t.msg.Answer = append(t.msg.Answer, k)
				open = slices.DeleteFunc(open, suppressed)
			}
		}
	}
	return t.packets >= maxTruncatedPackets || len(t.msg.Question) > maxTruncatedQuestions
}

func newAnswerState() answerState {
	return answerState{truncated: map[string]*truncatedQuery{}}
}

// linkRecords are the record sets of every published service on one
// interface, by service key, and the names they own.
type linkRecords struct {
	byService map[string][]dns.RR
	owned     map[string]bool
	// byKey holds every record by its identity (recordIdentity), setKeys
	// the set (setKeyOf) of every record — matter.js's View.byKey and
	// View.setKeys (InterfaceResponder.ts:880-892 at db021da0); complete
	// is every record once, in order.
	byKey    map[string]dns.RR
	setKeys  map[string]bool
	complete []dns.RR
	at       time.Time
	// nets are the interface's prefixes, read at the first query the
	// record sets answer (netsRead), so they go with them when the
	// interface's addresses change.
	nets     []*net.IPNet
	netsRead bool
}

// recordsForLocked returns the record sets on ifIndex, from the cache while
// it is fresh, else built from the addresses addrs reads — the listing a
// publication round took, or the one interface a query arrived on. Caller
// holds r.mu.
func (r *Responder) recordsForLocked(ifIndex int, addrs func() []net.IP) *linkRecords {
	now := r.clock.Load().Now()
	if lr, ok := r.links[ifIndex]; ok && now.Sub(lr.at) < recordCacheLifetime {
		return lr
	}
	lr := &linkRecords{
		byService: map[string][]dns.RR{}, owned: map[string]bool{},
		byKey: map[string]dns.RR{}, setKeys: map[string]bool{}, at: now,
	}
	// An interface without advertised addresses carries no service records,
	// listed or not. matter.js's generator yields PTR, SRV and TXT for
	// details with no addresses (MdnsAdvertisement.ts:154-177 at db021da0),
	// but its details are every address of the interface, so that case is
	// an interface that has none; here the advertise policy can leave a
	// carrier-less container bridge with addresses it does not publish, and
	// a multicast sent there loops back to the host. A controller on the
	// host — chip's resolver keyed by interface — then holds an SRV without
	// an A or AAAA and times out (connectedhomeip
	// src/lib/address_resolve/AddressResolve_DefaultImpl.cpp:115-125 at
	// 6170af84, CHIP_ERROR_TIMEOUT once no address arrived). A goodbye for
	// an interface left without addresses therefore names the service
	// records too (BD-Matter-MdnsFollowsAdvertisedAddresses).
	if addrs := addrs(); len(addrs) > 0 {
		for _, key := range r.order {
			e := r.services[key]
			var hostAddrs []net.IP
			if e.ownsHost {
				hostAddrs = addrs
			}
			var rrs []dns.RR
			if e.gen != nil {
				rrs = e.gen()
			} else {
				rrs = recordSet(e.svc, e.names, e.txt, hostAddrs)
			}
			lr.byService[key] = rrs
			for _, rr := range rrs {
				lr.owned[strings.ToLower(rr.Header().Name)] = true
				if key := recordIdentity(rr); lr.byKey[key] == nil {
					lr.byKey[key] = rr
					lr.complete = append(lr.complete, rr)
				}
				lr.setKeys[setKeyOf(rr)] = true
			}
		}
	}
	r.links[ifIndex] = lr
	return lr
}

// receiveLocked handles one inbound packet. Caller holds r.mu.
func (r *Responder) receiveLocked(msg *dns.Msg, ifIndex int, src *net.UDPAddr) {
	if r.closed || len(r.order) == 0 || r.wire == nil {
		return
	}
	// Ignore an interface without records.
	lr := r.recordsForLocked(ifIndex, r.addrsOf(ifIndex))
	if len(lr.byService) == 0 {
		return
	}
	if !lr.netsRead {
		lr.nets, lr.netsRead = r.wire.link(ifIndex).nets, true
	}
	// The sockets hand no packet without a source; a nil one is the
	// engine's own call.
	if src != nil && !onLink(src.IP, lr.nets) {
		return
	}
	query := r.prepareLocked(msg, ifIndex, src)
	if query == nil {
		return
	}
	r.answerLocked(query, ifIndex, src)
}

// onLink reports whether a query's source is on the link of the interface
// it arrived on, whose prefixes are nets: an IPv6 link-local address, or an
// address inside one of the prefixes (RFC 6762 §11, the source address
// check). An IPv4 link-local source (169.254/16) is one of the prefixes'
// like any other: refused unless the interface holds a 169.254 address, as
// matter.js finds it no interface (below; review II, R2-P1-dr-rest-07). The sockets bind the wildcard address, so a datagram sent by
// unicast to the host's port 5353 from anywhere reaches the engine; a query
// from off the link is not answered, by multicast or unicast, and not held
// as a truncated query (review II, R2-P1-net-02).
//
// matter.js gets the same filter from how it finds a packet's interface
// when its socket has none configured (packages/nodejs/src/net/
// NodeJsUdpSocket.ts:227 at db021da0): the source's zone, else the
// interface whose address and netmask contain the source
// (NodeJsNetwork.ts:144-166, onSameNetwork packages/general/src/util/
// Ip.ts:108-130), else "" for an IPv6 ULA source and undefined otherwise.
// A packet without an
// interface is dropped (packages/general/src/net/udp/UdpMulticastServer.ts:
// 142-155), and the "" interface has no details (NodeJsNetwork.ts:207-209
// getIpMac; InterfaceResponder.ts:206-213 #setDetails), so its query is not
// answered (InterfaceResponder.ts:254-259 answer, :796-800 #currentView).
// matter.js matches the source against every interface of the host and
// answers on the one that contains it; this engine answers on the
// interface the query arrived on (the control message's, socket.go), so
// it checks that interface's prefixes.
func onLink(ip net.IP, nets []*net.IPNet) bool {
	if ip.To4() == nil && ip.IsLinkLocalUnicast() {
		return true
	}
	return slices.ContainsFunc(nets, func(n *net.IPNet) bool { return n.Contains(ip) })
}

// truncatedKey is matter.js's `${transactionId}-${sourceIntf}-${sourceIp}`
// (MdnsServer.ts:471 at 862ad75f).
func truncatedKey(msg *dns.Msg, ifIndex int, src *net.UDPAddr) string {
	s := ""
	if src != nil {
		s = src.IP.String()
	}
	return strconv.Itoa(int(msg.Id)) + "-" + strconv.Itoa(ifIndex) + "-" + s
}

// prepareLocked is matter.js #prepareMessage (MdnsServer.ts:463-503 at
// 862ad75f): only queries are handled; a truncated query is held for
// 400–500 ms (RFC 6762 §7.2) and merged with its continuation packets; nil
// when there is nothing to answer yet. Caller holds r.mu.
//
// Every dimension of the merge an unauthenticated sender controls is
// bounded, where matter.js bounds none — its #prepareMessage spreads the
// held queries, answers and additionals with the new ones and re-arms the
// 400–500 ms timer on every truncated continuation, and its
// #truncatedQueryCache has no size limit (MdnsServer.ts:506-546, :61 at
// db021da0); a sender that kept the TC bit set held one ever-growing query
// alive, copied whole on each packet (review II, R2-P1-net-01):
//
//   - the wait runs from the first packet and a continuation does not move
//     it, so a query is answered at most 500 ms after it began;
//   - a held query keeps its distinct questions and only the known answers
//     that suppress a record of the interface, each record once; the
//     additionals, which the answer does not read, are not kept;
//   - [maxTruncatedPackets] packets, or a packet that takes the questions
//     past [maxTruncatedQuestions], complete the query;
//   - past [maxTruncatedQueries] held queries a new truncated query is not
//     held.
//
// At every bound the query is answered at once with what has arrived; no
// question that arrived is dropped, and a known answer that came too late
// only costs a record the querier already had.
func (r *Responder) prepareLocked(msg *dns.Msg, ifIndex int, src *net.UDPAddr) *dns.Msg {
	if msg.Response {
		return nil
	}
	key := truncatedKey(msg, ifIndex, src)
	held, ok := r.answers.truncated[key]
	if !ok && (!msg.Truncated || len(r.answers.truncated) >= maxTruncatedQueries) {
		if len(msg.Question) == 0 {
			return nil
		}
		return msg
	}
	if !ok {
		held = &truncatedQuery{msg: &dns.Msg{MsgHdr: dns.MsgHdr{Id: msg.Id}}, questions: map[string]bool{}}
		wait := truncatedWait + time.Duration(rand.IntN(truncatedWaitJitter))*time.Millisecond //nolint:gosec // G404: a timing jitter, not a secret
		held.timer = r.clock.Load().AfterFunc(wait, func() { r.processTruncated(key, ifIndex, src) })
		r.answers.truncated[key] = held
	}
	full := held.absorb(msg, r.recordsForLocked(ifIndex, r.addrsOf(ifIndex)).complete)
	if msg.Truncated && !full {
		return nil
	}
	delete(r.answers.truncated, key)
	held.timer.Stop()
	if len(held.msg.Question) == 0 {
		return nil
	}
	return held.msg
}

// processTruncated answers a truncated query whose wait ran out
// (#processTruncatedQuery, MdnsServer.ts:446-458 at 862ad75f).
func (r *Responder) processTruncated(key string, ifIndex int, src *net.UDPAddr) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.answers.truncated[key]
	if !ok {
		return
	}
	delete(r.answers.truncated, key)
	if len(t.msg.Question) == 0 || r.closed || r.wire == nil || len(r.order) == 0 {
		return
	}
	t.msg.Truncated = false
	if len(r.recordsForLocked(ifIndex, r.addrsOf(ifIndex)).byService) == 0 {
		return
	}
	r.answerLocked(t.msg, ifIndex, src)
}

// addrsOf reads the addresses of the one interface ifIndex.
func (r *Responder) addrsOf(ifIndex int) func() []net.IP {
	return func() []net.IP { return r.wire.link(ifIndex).addrs }
}

// answerLocked answers a complete query — InterfaceResponder.answer
// (:247-268 at db021da0): one packet per service with answers, in
// registration order, 20–120 ms apart. Caller holds r.mu.
func (r *Responder) answerLocked(query *dns.Msg, ifIndex int, src *net.UDPAddr) {
	now := r.clock.Load().Now()
	st := r.linkStateLocked(ifIndex)
	st.usedAt = now
	link := r.recordsForLocked(ifIndex, r.addrsOf(ifIndex))
	// Skip unrelated LAN traffic.
	if !slices.ContainsFunc(query.Question, func(q dns.Question) bool { return link.owned[strings.ToLower(q.Name)] }) {
		return
	}
	r.answerNextLocked(query, ifIndex, src, map[string]bool{}, now)
}

// answerNextLocked is #sendAnswer (:323-396 at db021da0): the packet for
// the first service in registration order not visited yet that has
// answers, decided now; when services remain, the next packet follows
// 20–120 ms later. Caller holds r.mu.
func (r *Responder) answerNextLocked(query *dns.Msg, ifIndex int, src *net.UDPAddr, visited map[string]bool, receivedAt time.Time) {
	if r.closed || r.wire == nil {
		return
	}
	link := r.recordsForLocked(ifIndex, r.addrsOf(ifIndex))
	st := r.linkStateLocked(ifIndex)
	onlyAddressQuestions := !slices.ContainsFunc(query.Question, func(q dns.Question) bool {
		return q.Qtype != dns.TypeA && q.Qtype != dns.TypeAAAA
	})
	allUnicast := !slices.ContainsFunc(query.Question, func(q dns.Question) bool { return q.Qclass&classCacheFlush == 0 })
	for _, key := range r.order {
		if visited[key] {
			continue
		}
		visited[key] = true
		all := link.byService[key]
		var answers []dns.RR
		for _, q := range query.Question {
			answers = append(answers, queryRecords(q, all)...)
		}
		answers = slices.DeleteFunc(uniqueRecords(answers), func(rr dns.RR) bool { return knownTo(query.Answer, rr) })
		if len(answers) == 0 {
			continue
		}
		var additional []dns.RR
		if !onlyAddressQuestions {
			for _, rr := range all {
				if rr.Header().Rrtype != dns.TypePTR && !slices.Contains(answers, rr) && !knownTo(query.Answer, rr) {
					additional = append(additional, rr)
				}
			}
		}

		now := r.clock.Load().Now()
		unicast := allUnicast && src != nil && !slices.ContainsFunc(answers, func(rr dns.RR) bool {
			return st.sinceCachedMulticast(rr, now) > time.Duration(rr.Header().Ttl)*time.Second/4
		})
		sentAnswers, sentAdditional := answers, additional
		if !unicast {
			packet := append(slices.Clone(answers), additional...)
			since := func(dns.RR) time.Time { return receivedAt }
			sentAnswers = r.holdBackLocked(ifIndex, st, answers, now, since, true, func(dns.RR) []dns.RR { return additional }, packet)
			sentAdditional = r.holdBackLocked(ifIndex, st, additional, now, since, false, func(dns.RR) []dns.RR { return nil }, packet)
			if len(sentAnswers) == 0 {
				continue
			}
		}
		var dst *net.UDPAddr
		if unicast {
			dst = src
		}
		l := r.wire.link(ifIndex)
		r.sendLocked(splitResponse(query.Id, sendable(sentAnswers, all, unicast), sendable(sentAdditional, all, unicast)), l, dst, sentAnswers)
		if !slices.ContainsFunc(r.order, func(k string) bool { return !visited[k] }) {
			return
		}
		// RFC 6762 §6: 20–120 ms between the packets of one answer
		// (packetDelay, InterfaceResponder.ts:920-922).
		delay := 20*time.Millisecond + time.Duration(rand.IntN(100))*time.Millisecond //nolint:gosec // G404: a timing jitter
		r.clock.Load().AfterFunc(delay, func() {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.answerNextLocked(query, ifIndex, src, visited, receivedAt)
		})
		return
	}
}

// queryRecords is matter.js #queryRecords (MdnsServer.ts:377-387 at
// 862ad75f): the records whose name matches the question's
// case-insensitively (RFC 6762 §16) and whose type is the question's, or
// any type for ANY.
func queryRecords(q dns.Question, all []dns.RR) []dns.RR {
	var out []dns.RR
	for _, rr := range all {
		hdr := rr.Header()
		if strings.EqualFold(hdr.Name, q.Name) && (q.Qtype == dns.TypeANY || q.Qtype == hdr.Rrtype) {
			out = append(out, rr)
		}
	}
	return out
}

// knownTo reports whether a known answer of the query carries rr (RFC 6762
// §7.1, matter.js #suppressedByKnownAnswer, MdnsServer.ts:366-375 at
// 862ad75f): same type, class (the cache-flush bit is framing), name and
// value, with more than half of rr's TTL left.
func knownTo(known []dns.RR, rr dns.RR) bool {
	hdr := rr.Header()
	for _, k := range known {
		kh := k.Header()
		if kh.Rrtype != hdr.Rrtype || kh.Class&^classCacheFlush != hdr.Class&^classCacheFlush ||
			uint64(kh.Ttl)*2 <= uint64(hdr.Ttl) || !strings.EqualFold(kh.Name, hdr.Name) {
			continue
		}
		a, b := dns.Copy(rr), dns.Copy(k)
		a.Header().Class &^= classCacheFlush
		b.Header().Class &^= classCacheFlush
		if dns.IsDuplicate(a, b) {
			return true
		}
	}
	return false
}

// sendable clears the cache-flush bit wherever it would claim more than the
// response carries (RFC 6762 §10.2 and §6.7): on every record of a unicast
// response, and on a record whose set (name, type, class) is sent
// incompletely, the complete set counted with each record once (matter.js
// InterfaceResponder.ts `sendable`, :934-962 at db021da0).
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
	sentSizes, completeSizes := sizes(sent), sizes(uniqueRecords(complete))
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

// newResponse is an mDNS response header: QR and AA set, no question
// (RFC 6762 §6), the query's transaction id (0 from a conforming querier,
// §18.1, and in an announcement). Names are not compressed, as matter.js
// encodes them.
func newResponse(id uint16) *dns.Msg {
	m := new(dns.Msg)
	m.Id = id
	m.Response = true
	m.Authoritative = true
	return m
}

// splitResponse packs answers and additionals into responses of at most
// [maxMessageSize] bytes, as matter.js MdnsSocket.send does
// (packages/general/src/net/dns-sd/MdnsSocket.ts:131-201 at 862ad75f):
// answers fill a packet and spill into the next with no additionals; the
// additionals follow the last answers and spill into packets of their own.
// The size is counted uncompressed, as matter.js encodes records one by one.
func splitResponse(id uint16, answers, additional []dns.RR) []*dns.Msg {
	var out []*dns.Msg
	chunk := newResponse(id)
	size := dnsHeaderSize
	flush := func() {
		if len(chunk.Answer) > 0 || len(chunk.Extra) > 0 {
			out = append(out, chunk)
		}
		chunk = newResponse(id)
	}
	for _, rr := range answers {
		n := dns.Len(rr)
		if size+n > maxMessageSize {
			flush()
			size = dnsHeaderSize
		}
		size += n
		chunk.Answer = append(chunk.Answer, rr)
	}
	for _, rr := range additional {
		n := dns.Len(rr)
		if size+n > maxMessageSize {
			flush()
			size = dnsHeaderSize
		}
		size += n
		chunk.Extra = append(chunk.Extra, rr)
	}
	flush()
	return out
}

// sendLocked packs and sends msgs on the interface l: to dst by unicast
// when set, to the mDNS groups otherwise. It notes what the peers may now
// cache (noteSentLocked; answered are the answers to a query, which count
// as the records' last multicast), logs a miss, and reports how far the
// message reached (InterfaceResponder.ts #transmit, #sendFailed and
// #multicastMissed, :694-789 at db021da0). Caller holds r.mu.
func (r *Responder) sendLocked(msgs []*dns.Msg, l link, dst *net.UDPAddr, answered []dns.RR) reach {
	if len(msgs) == 0 {
		return reachAll
	}
	now := r.clock.Load().Now()
	rc, first := reachNone, true
	var errs []error
	for _, m := range msgs {
		pkt, err := m.Pack()
		var one reach
		switch {
		case err != nil:
			one = reachFailed
		case dst != nil:
			one, err = r.wire.unicast(pkt, l.index, dst)
		default:
			one, err = r.wire.multicast(pkt, l)
		}
		if err != nil {
			errs = append(errs, err)
		}
		if first {
			rc, first = one, false
		} else {
			rc = rc.combine(one)
		}
	}
	r.noteSentLocked(l.index, msgs, rc, now, dst == nil, answered)
	err := errors.Join(errs...)
	if dst != nil {
		switch rc {
		case reachFailed:
			r.logger().Debug("mdns.responder.response_not_sent", slog.String("to", dst.String()), slog.Any("err", err))
		case reachSome:
			r.logger().Debug("mdns.responder.response_part_not_sent", slog.String("to", dst.String()), slog.Any("err", err))
		case reachNone, reachAll:
		}
		return rc
	}
	st := r.linkStateLocked(l.index)
	switch rc {
	case reachAll:
		st.outage = false
	case reachSome:
		r.multicastMissedLocked(st, l, "mdns.responder.multicast_some_families", err)
	case reachFailed:
		r.multicastMissedLocked(st, l, "mdns.responder.multicast_not_sent", err)
	case reachNone:
		// No usable family: nothing was tried, no outage (#transmit, :752-754).
	}
	return rc
}

// multicastMissedLocked logs a multicast that failed or reached only some
// address families. Both are one outage of the interface, which only a
// multicast that reached every family ends: the first miss logs at Info,
// later ones — and every one while closing or on an interface the listing
// no longer has — at Debug (#multicastMissed, :671-692 at db021da0). A
// peer that makes the reach change between some and none cannot repeat
// the line. Caller holds r.mu.
func (r *Responder) multicastMissedLocked(st *linkState, l link, msg string, err error) {
	attrs := []any{slog.String("iface", l.name), slog.Any("err", err)}
	if st.outage || r.closed || !l.listed {
		r.logger().Debug(msg, attrs...)
		return
	}
	st.outage = true
	r.logger().Info(msg, attrs...)
}

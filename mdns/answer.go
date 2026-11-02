// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns

import (
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
// packages/protocol/src/mdns/MdnsServer.ts #handleMessage (:103-203 at
// 862ad75f) rule for rule:
//
//   - a query for no name the responder owns is dropped before anything
//     else (:120-122), and an interface without records is not answered on
//     (:106-111);
//   - each service's record set is matched on its own (#queryRecords,
//     :377-387: the name case-insensitively, ANY or the exact type) and
//     answered in its own response;
//   - the rest of the set except its PTRs goes along as additionals,
//     unless every question asks for A or AAAA (:130-136);
//   - a record the querier lists as a known answer with more than half its
//     TTL left is not sent (#suppressedByKnownAnswer, :366-375, :137-150);
//   - a query whose every question carries the unicast-response bit is
//     answered by unicast, unless an answer has not gone out by multicast
//     on the interface within a quarter of its TTL (:152-166);
//   - a multicast answer repeats no record set sent on the interface within
//     900 ms (:167-173), and a question answered on the interface within
//     999 ms with every record already known to the link is not answered
//     again (#shouldSuppressResponse, :394-444, QUESTION_SUPPRESSION_WINDOW
//     :35);
//   - a truncated query waits 400–500 ms for its continuation packets and
//     is answered as one (#prepareMessage, :463-503;
//     #processTruncatedQuery, :446-458);
//   - the cache-flush bit is cleared where it would claim more than the
//     response carries (sendable, :534-551);
//   - a response that outgrows 1232 bytes is split, answers first and the
//     additionals spilled after them (packages/general/src/net/dns-sd/
//     MdnsSocket.ts:131-201, MAX_MDNS_MESSAGE_SIZE DnsCodec.ts:26).

const (
	// questionSuppressionWindow is RFC 6762 §7.3's duplicate-question
	// window, matter.js QUESTION_SUPPRESSION_WINDOW (MdnsServer.ts:35 at
	// 862ad75f, after python-zeroconf).
	questionSuppressionWindow = 999 * time.Millisecond
	// multicastRateLimit is the minimum gap between two multicasts of one
	// record set on one interface (RFC 6762 §6; matter.js
	// MdnsServer.ts:167-173 at 862ad75f).
	multicastRateLimit = 900 * time.Millisecond
	// maxMessageSize is the largest message the responder sends,
	// matter.js MAX_MDNS_MESSAGE_SIZE (packages/general/src/codec/
	// DnsCodec.ts:26 at 862ad75f): the IPv6 minimum MTU less the IPv6 and
	// UDP headers.
	maxMessageSize = 1232
	// dnsHeaderSize is the fixed DNS header.
	dnsHeaderSize = 12
	// recordCacheLifetime is how long the per-interface record sets are
	// reused before the interface's addresses are read again — matter.js's
	// record cache (MdnsServer.ts:62-87 at 862ad75f, Minutes(15)).
	recordCacheLifetime = 15 * time.Minute
)

// answerState is the engine's memory between queries. The Responder's
// mutex guards it.
type answerState struct {
	// lastMulticast is when a record set (recordKey) last went out by
	// multicast on an interface — matter.js
	// #recordLastSentAsMulticastAnswer.
	lastMulticast map[string]time.Time
	// recentlyAnswered is matter.js #recentlyAnsweredQueries: per question
	// signature and interface, the records known to the link.
	recentlyAnswered map[string]recentAnswer
	// truncated holds the truncated queries waiting for their
	// continuation, by transaction id, interface and source.
	truncated map[string]*truncatedQuery
}

type recentAnswer struct {
	known map[string]bool
	at    time.Time
}

type truncatedQuery struct {
	msg   *dns.Msg
	timer clock.Timer
}

func newAnswerState() answerState {
	return answerState{
		lastMulticast:    map[string]time.Time{},
		recentlyAnswered: map[string]recentAnswer{},
		truncated:        map[string]*truncatedQuery{},
	}
}

// reset forgets the rate-limit and suppression state, as matter.js
// #resetServices does whenever the records change (MdnsServer.ts:282-291
// at 862ad75f). Pending truncated queries are kept.
func (s *answerState) reset() {
	clear(s.lastMulticast)
	clear(s.recentlyAnswered)
}

// linkRecords are the record sets of every published service on one
// interface, by service key, and the names they own.
type linkRecords struct {
	byService map[string][]dns.RR
	owned     map[string]bool
	at        time.Time
}

// recordsForLocked returns the record sets on ifIndex, from the cache while
// it is fresh. Caller holds r.mu.
func (r *Responder) recordsForLocked(ifIndex int) *linkRecords {
	now := r.clock.Load().Now()
	if lr, ok := r.links[ifIndex]; ok && now.Sub(lr.at) < recordCacheLifetime {
		return lr
	}
	lr := &linkRecords{byService: map[string][]dns.RR{}, owned: map[string]bool{}, at: now}
	addrs := r.wire.addrs(ifIndex)
	if len(addrs) > 0 {
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
	// Ignore an interface without records (MdnsServer.ts:106-111).
	if len(r.recordsForLocked(ifIndex).byService) == 0 {
		return
	}
	query := r.prepareLocked(msg, ifIndex, src)
	if query == nil {
		return
	}
	r.answerLocked(query, ifIndex, src)
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
func (r *Responder) prepareLocked(msg *dns.Msg, ifIndex int, src *net.UDPAddr) *dns.Msg {
	if msg.Response {
		return nil
	}
	key := truncatedKey(msg, ifIndex, src)
	query := msg
	if prev, ok := r.answers.truncated[key]; ok {
		delete(r.answers.truncated, key)
		prev.timer.Stop()
		merged := prev.msg.Copy()
		merged.Question = append(merged.Question, msg.Question...)
		merged.Answer = append(merged.Answer, msg.Answer...)
		merged.Extra = append(merged.Extra, msg.Extra...)
		merged.Truncated = msg.Truncated
		query = merged
	}
	if !query.Truncated {
		if len(query.Question) == 0 {
			return nil
		}
		return query
	}
	wait := 400*time.Millisecond + time.Duration(rand.IntN(100))*time.Millisecond //nolint:gosec // G404: a timing jitter, not a secret
	r.answers.truncated[key] = &truncatedQuery{
		msg:   query,
		timer: r.clock.Load().AfterFunc(wait, func() { r.processTruncated(key, ifIndex, src) }),
	}
	return nil
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
	if len(r.recordsForLocked(ifIndex).byService) == 0 {
		return
	}
	r.answerLocked(t.msg, ifIndex, src)
}

// answerLocked answers a complete query (#handleMessage after
// #prepareMessage, MdnsServer.ts:118-202 at 862ad75f). Caller holds r.mu.
func (r *Responder) answerLocked(query *dns.Msg, ifIndex int, src *net.UDPAddr) {
	link := r.recordsForLocked(ifIndex)
	// Skip unrelated LAN traffic (:120-122).
	if !slices.ContainsFunc(query.Question, func(q dns.Question) bool { return link.owned[strings.ToLower(q.Name)] }) {
		return
	}
	onlyAddressQuestions := !slices.ContainsFunc(query.Question, func(q dns.Question) bool {
		return q.Qtype != dns.TypeA && q.Qtype != dns.TypeAAAA
	})
	allUnicast := !slices.ContainsFunc(query.Question, func(q dns.Question) bool { return q.Qclass&classCacheFlush == 0 })

	var delay time.Duration
	sentPrevious := false
	for _, key := range r.order {
		all := link.byService[key]
		var answers []dns.RR
		for _, q := range query.Question {
			answers = append(answers, queryRecords(q, all)...)
		}
		if len(answers) == 0 {
			continue
		}
		var additional []dns.RR
		if !onlyAddressQuestions {
			for _, rr := range all {
				if rr.Header().Rrtype != dns.TypePTR && !slices.Contains(answers, rr) {
					additional = append(additional, rr)
				}
			}
		}
		if len(query.Answer) > 0 {
			answers = slices.DeleteFunc(answers, func(rr dns.RR) bool { return knownTo(query.Answer, rr) })
			if len(answers) == 0 {
				continue
			}
			additional = slices.DeleteFunc(additional, func(rr dns.RR) bool { return knownTo(query.Answer, rr) })
		}

		now := r.clock.Load().Now()
		sinceMulticast := func(rr dns.RR) time.Duration {
			last, seen := r.answers.lastMulticast[recordKey(rr, ifIndex)]
			if !seen {
				return time.Duration(1<<63 - 1)
			}
			return now.Sub(last)
		}
		unicast := allUnicast && src != nil
		if unicast && slices.ContainsFunc(answers, func(rr dns.RR) bool {
			return sinceMulticast(rr) > time.Duration(rr.Header().Ttl)*time.Second/4
		}) {
			// Not multicast on this link within a quarter of the TTL: the
			// link's caches need it, answer by multicast (:160-166).
			unicast = false
		}
		if !unicast {
			answers = slices.DeleteFunc(answers, func(rr dns.RR) bool { return sinceMulticast(rr) < multicastRateLimit })
			if len(answers) == 0 {
				continue
			}
			if r.suppressDuplicateLocked(query, ifIndex, answers, now) {
				continue
			}
			for _, rr := range answers {
				r.answers.lastMulticast[recordKey(rr, ifIndex)] = now
			}
		}

		msgs := splitResponse(query.Id, sendable(answers, all, unicast), sendable(additional, all, unicast))
		if sentPrevious {
			// RFC 6762 §6: 20–120 ms between the responses of the
			// services (:184-186).
			delay += 20*time.Millisecond + time.Duration(rand.IntN(100))*time.Millisecond //nolint:gosec // G404: a timing jitter
		}
		sentPrevious = true
		var dst *net.UDPAddr
		if unicast {
			dst = src
		}
		if delay == 0 {
			r.sendLocked(msgs, ifIndex, dst)
			continue
		}
		r.clock.Load().AfterFunc(delay, func() {
			r.mu.Lock()
			defer r.mu.Unlock()
			if !r.closed {
				r.sendLocked(msgs, ifIndex, dst)
			}
		})
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

// suppressDuplicateLocked is matter.js #shouldSuppressResponse
// (MdnsServer.ts:394-444 at 862ad75f, RFC 6762 §7.3): the same question
// answered on the interface within the window, with every record we would
// send already known to the link, is not answered again. Otherwise the
// answer is recorded for the next duplicate. Caller holds r.mu.
func (r *Responder) suppressDuplicateLocked(query *dns.Msg, ifIndex int, answers []dns.RR, now time.Time) bool {
	for k, e := range r.answers.recentlyAnswered {
		if now.Sub(e.at) >= questionSuppressionWindow {
			delete(r.answers.recentlyAnswered, k)
		}
	}
	sig := make([]string, 0, len(query.Question))
	for _, q := range query.Question {
		sig = append(sig, strings.ToLower(q.Name)+"-"+strconv.Itoa(int(q.Qtype)))
	}
	slices.Sort(sig)
	key := strings.Join(sig, "|") + "-" + strconv.Itoa(ifIndex)
	if e, ok := r.answers.recentlyAnswered[key]; ok && now.Sub(e.at) < questionSuppressionWindow {
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
	r.answers.recentlyAnswered[key] = recentAnswer{known: known, at: now}
	return false
}

// recordKey identifies a record set on an interface as matter.js
// buildDnsRecordKey does (MdnsServer.ts:99-101 at 862ad75f): name, class,
// type and interface — not the value.
func recordKey(rr dns.RR, ifIndex int) string {
	hdr := rr.Header()
	return strings.ToLower(hdr.Name) + "-" + strconv.Itoa(int(hdr.Class&^classCacheFlush)) + "-" +
		strconv.Itoa(int(hdr.Rrtype)) + "-" + strconv.Itoa(ifIndex)
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
// response carries (matter.js MdnsServer.ts:534-551 `sendable` at
// 862ad75f, RFC 6762 §10.2 and §6.7): on every record of a unicast
// response, and on a record whose set (name, type, class) is sent
// incompletely.
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

// sendLocked packs and sends msgs on ifIndex: to dst by unicast when set,
// to the mDNS groups otherwise. Caller holds r.mu.
func (r *Responder) sendLocked(msgs []*dns.Msg, ifIndex int, dst *net.UDPAddr) {
	for _, m := range msgs {
		pkt, err := m.Pack()
		if err != nil {
			r.logger().Debug("mdns.responder.pack_failed", slog.Any("err", err))
			continue
		}
		if dst != nil {
			r.wire.unicast(pkt, ifIndex, dst)
		} else {
			r.wire.multicast(pkt, ifIndex)
		}
	}
}

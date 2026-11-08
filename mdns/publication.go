// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns

import (
	"cmp"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/miekg/dns"

	"github.com/SukramJ/go-fabric/clock"
)

// The publication path: what peers on an interface may cache, and the one
// message that announces services and says goodbye to the records that
// left. It mirrors matter.js packages/protocol/src/mdns/
// InterfaceResponder.ts at db021da0:
//
//   - every record sent on an interface is noted with the time its TTL
//     ends, as far as the message reached (#transmit, :723-789);
//   - a goodbye (TTL 0) goes only to a noted record that no published
//     service holds any more, so a host address or a shared pointer another
//     service still publishes is never withdrawn (#goodbyesFor, :492-510);
//     its cache-flush bit is set only for a unique type whose set the
//     current records leave empty (RFC 6762 §10.2, :503-507);
//   - the goodbyes and the announcement go out in one message, goodbyes
//     first, and a service whose unique record replaces a record that gets
//     a goodbye is announced with them (#sendPublication, :445-490;
//     publish, :271-302);
//   - a goodbye that did not reach every address family is sent again, a
//     second later and then at twice the interval up to a minute, until
//     it leaves or peers dropped the record at the end of its TTL
//     (#retryGoodbyes and #settleGoodbyes, :512-547; GOODBYE_RETRY_MAXIMUM_
//     INTERVAL, :46-47).

const (
	// goodbyeRetryFirst is the first wait before a goodbye that did not
	// leave is sent again: matter.js doubles MULTICAST_INTERVAL / 2
	// (InterfaceResponder.ts:34-35, :523-526 at db021da0).
	goodbyeRetryFirst = time.Second
	// goodbyeRetryMax is the longest wait between two attempts,
	// GOODBYE_RETRY_MAXIMUM_INTERVAL (InterfaceResponder.ts:46-47 at
	// db021da0).
	goodbyeRetryMax = time.Minute
)

// reach is how far a send reached — the combined result of matter.js's
// MdnsSocket.send (packages/general/src/net/dns-sd/MdnsSocket.ts:102-127 at
// db021da0).
type reach int

const (
	// reachNone: no address family was usable — the interface has no
	// address of a family a socket is bound for; nothing was tried.
	reachNone reach = iota
	// reachFailed: every family it was tried on refused it, or it could
	// not be encoded (matter.js's rejected send).
	reachFailed
	// reachSome: it left on some of the interface's address families.
	reachSome
	// reachAll: it left on every address family it was sent on.
	reachAll
)

// reachOf is the reach of a send tried on tried families, ok of which
// took it.
func reachOf(tried, ok int) reach {
	switch {
	case tried == 0:
		return reachNone
	case ok == 0:
		return reachFailed
	case ok < tried:
		return reachSome
	default:
		return reachAll
	}
}

// combine is the reach of a message from the reach of its datagrams: every
// one all is all, every one none is none, anything else some — a message
// that reached some families counts as reaching each of its records
// (InterfaceResponder.ts:734-737 at db021da0).
func (a reach) combine(b reach) reach {
	if a == b {
		return a
	}
	return reachSome
}

// peerEntry is one record peers on an interface may cache.
type peerEntry struct {
	rr dns.RR
	// until is when the record's TTL ends in the peers' caches.
	until time.Time
	// seq orders the entries as first noted: matter.js's peer cache is a
	// Map, which keeps a key's position when its value is replaced.
	seq uint64
	// lastMulticast is when a multicast last carried the record as an
	// answer to a query or as its goodbye, zero when none did;
	// lastIsGoodbye says it was the goodbye (InterfaceResponder.ts
	// PeerCacheEntry lastMulticastAt, lastMulticastIsGoodbye at db021da0).
	lastMulticast time.Time
	lastIsGoodbye bool
}

// linkState is the responder's memory of one interface — matter.js's
// InterfaceResponder state that outlives a record rebuild: what peers may
// cache there, and the goodbye retry.
type linkState struct {
	peers map[string]peerEntry
	seq   uint64
	// retry is the pending goodbye retry, nil while none is needed.
	retry *goodbyeRetry
	// deferred are the records the one-second rule held back, by
	// recordIdentity (hold.go); deferredTimer sends them when the
	// earliest is due, deferredGen tells a fired timer from the current
	// one, deferSeq orders them as first deferred (matter.js's Map).
	deferred      map[string]*deferral
	deferredTimer clock.Timer
	deferredGen   uint64
	deferSeq      uint64
	// usedAt is when the interface last answered or published: its state
	// goes only after recordCacheLifetime unused (InterfaceResponder.ts
	// #usedAt, :100, :142-160 at db021da0).
	usedAt time.Time
	// outage is set by a multicast that failed or missed an address
	// family and cleared by one that reached every family: only the first
	// miss of an outage logs at Info (#isMulticastOutage, :105-106).
	outage bool
}

// goodbyeRetry is one interface's retry schedule (InterfaceResponder.ts
// #goodbyeRetry, :99 at db021da0).
type goodbyeRetry struct {
	timer    clock.Timer
	interval time.Duration
	// waiting is true from arming until the timer fired.
	waiting bool
}

// linkStateLocked returns ifIndex's state, creating it. Caller holds r.mu.
func (r *Responder) linkStateLocked(ifIndex int) *linkState {
	st, ok := r.linkStates[ifIndex]
	if !ok {
		st = &linkState{peers: map[string]peerEntry{}, usedAt: r.clock.Load().Now()}
		r.linkStates[ifIndex] = st
	}
	return st
}

// noteSentLocked records what a message that reached as far as rc carried
// (InterfaceResponder.ts #transmit, :700-789 at db021da0): a record with
// a TTL is cached by the peers until the TTL ends; a goodbye that reached
// every family ends the record at once, one that reached only some counts
// as its last multicast and leaves it to be said again; a send that
// reached nothing changes nothing. Of a multicast, only goodbyes and the
// records in answered — the answers to a query — count as the record's
// last multicast; announcements and additional records do not. Caller
// holds r.mu.
func (r *Responder) noteSentLocked(ifIndex int, msgs []*dns.Msg, rc reach, now time.Time, multicast bool, answered []dns.RR) {
	if rc == reachNone || rc == reachFailed {
		return
	}
	answeredKeys := make(map[string]bool, len(answered))
	for _, rr := range answered {
		answeredKeys[recordIdentity(rr)] = true
	}
	st := r.linkStateLocked(ifIndex)
	for _, m := range msgs {
		for _, section := range [][]dns.RR{m.Answer, m.Extra} {
			for _, rr := range section {
				key := recordIdentity(rr)
				ttl := rr.Header().Ttl
				p, ok := st.peers[key]
				if ttl == 0 && rc == reachSome {
					if ok {
						p.lastMulticast, p.lastIsGoodbye = now, true
						st.peers[key] = p
					}
					continue
				}
				if !ok {
					st.seq++
					p.seq = st.seq
				}
				p.rr, p.until = rr, now.Add(time.Duration(ttl)*time.Second)
				if multicast && (ttl == 0 || answeredKeys[key]) {
					p.lastMulticast, p.lastIsGoodbye = now, ttl == 0
				}
				st.peers[key] = p
			}
		}
	}
}

// goodbyesLocked returns the goodbyes for the records peers may still cache
// on the interface that lr no longer holds: TTL 0, the cache-flush bit only
// on a unique type whose set lr leaves empty (InterfaceResponder.ts
// #goodbyesFor, :497-510 at db021da0). Caller holds r.mu.
func (r *Responder) goodbyesLocked(st *linkState, lr *linkRecords, now time.Time) []dns.RR {
	keys := make([]string, 0, len(st.peers))
	for key, p := range st.peers {
		if p.until.After(now) && lr.byKey[key] == nil {
			keys = append(keys, key)
		}
	}
	slices.SortFunc(keys, func(a, b string) int { return cmp.Compare(st.peers[a].seq, st.peers[b].seq) })
	out := make([]dns.RR, 0, len(keys))
	for _, key := range keys {
		p := st.peers[key]
		g := dns.Copy(p.rr)
		g.Header().Ttl = 0
		g.Header().Class &^= classCacheFlush
		if isUniqueType(g.Header().Rrtype) && !lr.setKeys[setKeyOf(g)] {
			g.Header().Class |= classCacheFlush
		}
		out = append(out, g)
	}
	return out
}

// publishLocked is one publication on ifIndex — matter.js
// InterfaceResponder.publish (:271-302 and #sendPublication :445-490 at
// db021da0): in one message, the goodbyes for the records that left and
// the announcement of the services named (all of them when all is set),
// plus every service whose unique record replaces a record that gets a
// goodbye while the interface is listed. Nothing is sent when there is
// nothing to say. A goodbye that did not reach every family is retried.
// Caller holds r.mu.
func (r *Responder) publishLocked(l link, services map[string]bool, all bool) {
	if r.wire == nil {
		return
	}
	now := r.clock.Load().Now()
	lr := r.recordsForLocked(l.index, l.addrsFunc())
	st := r.linkStateLocked(l.index)
	st.usedAt = now
	goodbye := r.goodbyesLocked(st, lr, now)

	replaced := map[string]bool{}
	if l.listed {
		for _, g := range goodbye {
			if isUniqueType(g.Header().Rrtype) && lr.setKeys[setKeyOf(g)] {
				replaced[setKeyOf(g)] = true
			}
		}
	}
	var complete []dns.RR
	if !r.closed {
		for _, key := range r.order {
			rrs := lr.byService[key]
			if all || services[key] || anyReplaces(rrs, replaced) {
				complete = append(complete, rrs...)
			}
		}
	}
	announced := uniqueRecords(complete)
	if len(goodbye) == 0 && len(announced) == 0 {
		r.stopGoodbyeRetryLocked(st)
		return
	}
	// RFC 6762 §8.3: every record in the answer section. Goodbyes first,
	// so a resolver that applies the TXT records of a packet in order ends
	// on the new one (InterfaceResponder.ts:478-483 at db021da0).
	answers := make([]dns.RR, 0, len(goodbye)+len(announced))
	answers = append(answers, goodbye...)
	answers = append(answers, sendable(announced, complete, false)...)
	if rc := r.sendLocked(splitResponse(0, answers, nil), l, nil, nil); rc != reachAll && len(goodbye) > 0 {
		r.logger().Debug("mdns.responder.goodbye_not_sent", slog.Int("iface", l.index))
	}
	r.settleGoodbyesLocked(l, st)
}

// settleGoodbyesLocked retries the goodbyes peers may still need on the
// interface, or ends the retry when none remain (InterfaceResponder.ts
// #settleGoodbyes, :534-542 at db021da0). Caller holds r.mu.
func (r *Responder) settleGoodbyesLocked(l link, st *linkState) {
	if len(r.goodbyesLocked(st, r.recordsForLocked(l.index, l.addrsFunc()), r.clock.Load().Now())) > 0 {
		r.retryGoodbyesLocked(l.index, st)
		return
	}
	r.stopGoodbyeRetryLocked(st)
}

// retryGoodbyesLocked arms the next goodbye retry unless one waits: first
// a second, then twice the last interval up to a minute; a retry sends
// only the goodbyes and the services that replace them
// (InterfaceResponder.ts #retryGoodbyes, :512-532 at db021da0). Caller
// holds r.mu.
func (r *Responder) retryGoodbyesLocked(ifIndex int, st *linkState) {
	if r.closed || (st.retry != nil && st.retry.waiting) {
		return
	}
	interval := goodbyeRetryFirst
	if st.retry != nil {
		interval = min(st.retry.interval*2, goodbyeRetryMax)
	}
	retry := &goodbyeRetry{interval: interval, waiting: true}
	st.retry = retry
	retry.timer = r.clock.Load().AfterFunc(interval, func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.closed || st.retry != retry {
			return
		}
		retry.waiting = false
		r.publishLocked(r.wire.link(ifIndex), nil, false)
	})
}

// stopGoodbyeRetryLocked ends the interface's retry; the next one starts
// at a second again. Caller holds r.mu.
func (r *Responder) stopGoodbyeRetryLocked(st *linkState) {
	if st.retry != nil && st.retry.timer != nil {
		st.retry.timer.Stop()
	}
	st.retry = nil
}

// publishEverywhereLocked publishes on every listed interface and on every
// interface peers may still cache records on — matter.js's withdrawal,
// which publishes nothing but the goodbyes on each responder
// (MdnsServer.ts #withdraw, :165-178 at db021da0). Caller holds r.mu.
func (r *Responder) publishEverywhereLocked(services map[string]bool, all bool) {
	if r.wire == nil {
		return
	}
	for _, l := range r.knownLinksLocked(r.listLocked()) {
		r.publishLocked(l, services, all)
	}
	r.pruneLinkStatesLocked()
}

// listLocked lists the interfaces once and prunes the interface states,
// as matter.js prunes its responders on every interface listing
// (MdnsServer.ts #listInterfaces and #currentResponders, :405-421 at
// db021da0), so a record whose TTL ended without a goodbye leaves the
// cache on a long-running node. Caller holds r.mu.
func (r *Responder) listLocked() []link {
	ls := r.wire.links()
	for i := range ls {
		ls[i].listed = true
	}
	r.pruneLinkStatesLocked()
	return ls
}

// knownLinksLocked is the listed interfaces ls and those with state the
// listing misses, in index order. Caller holds r.mu.
func (r *Responder) knownLinksLocked(ls []link) []link {
	seen := map[int]bool{}
	out := make([]link, 0, len(ls)+len(r.linkStates))
	for _, l := range ls {
		if !seen[l.index] {
			seen[l.index] = true
			out = append(out, l)
		}
	}
	for i := range r.linkStates {
		if !seen[i] {
			seen[i] = true
			l := r.wire.link(i)
			l.listed = false
			out = append(out, l)
		}
	}
	slices.SortFunc(out, func(a, b link) int { return cmp.Compare(a.index, b.index) })
	return out
}

// pruneLinkStatesLocked forgets the records whose TTL ended and whose last
// multicast is more than a second ago, and drops the state of an
// interface peers cache nothing on, with nothing deferred, no goodbye
// retry waiting and unused for recordCacheLifetime
// (InterfaceResponder.prune, :142-160 at db021da0). Caller holds r.mu.
func (r *Responder) pruneLinkStatesLocked() {
	now := r.clock.Load().Now()
	for i, st := range r.linkStates {
		for key, p := range st.peers {
			if !p.until.After(now) && (p.lastMulticast.IsZero() || now.Sub(p.lastMulticast) >= multicastInterval) {
				delete(st.peers, key)
			}
		}
		if len(st.peers) == 0 && len(st.deferred) == 0 && (st.retry == nil || !st.retry.waiting) &&
			now.Sub(st.usedAt) > recordCacheLifetime {
			r.stopGoodbyeRetryLocked(st)
			delete(r.linkStates, i)
		}
	}
}

// stopAllGoodbyeRetriesLocked ends every interface's retry (close).
// Caller holds r.mu.
func (r *Responder) stopAllGoodbyeRetriesLocked() {
	for _, st := range r.linkStates {
		r.stopGoodbyeRetryLocked(st)
		st.stopDeferred()
	}
}

// isUniqueType reports a record type whose set the cache-flush bit may
// claim: SRV, TXT, A and AAAA (matter.js DnsRecordType.isUnique, used at
// InterfaceResponder.ts:456, :506 at db021da0).
func isUniqueType(t uint16) bool {
	switch t {
	case dns.TypeSRV, dns.TypeTXT, dns.TypeA, dns.TypeAAAA:
		return true
	}
	return false
}

// setKeyOf identifies the record set rr belongs to: name, type and class,
// the scope of the cache-flush bit (InterfaceResponder.ts setKeyOf,
// :964-967 at db021da0).
func setKeyOf(rr dns.RR) string {
	hdr := rr.Header()
	return strconv.Itoa(int(hdr.Rrtype)) + "-" + strconv.Itoa(int(hdr.Class&^classCacheFlush)) + "-" + strings.ToLower(hdr.Name)
}

// recordIdentity identifies a resource record by name, type, class and
// rdata — RFC 6762 §10.1's goodbye identity: names, in the record and in
// PTR and SRV rdata, without case; TTL and the cache-flush bit do not
// count (InterfaceResponder.ts recordKeyOf, :991-1027 at db021da0).
func recordIdentity(rr dns.RR) string {
	c := dns.Copy(rr)
	hdr := c.Header()
	hdr.Ttl = 0
	hdr.Class &^= classCacheFlush
	hdr.Name = strings.ToLower(hdr.Name)
	switch v := c.(type) {
	case *dns.PTR:
		v.Ptr = strings.ToLower(v.Ptr)
	case *dns.SRV:
		v.Target = strings.ToLower(v.Target)
	}
	return c.String()
}

// uniqueRecords returns rrs with each resource record once, in first-seen
// order (InterfaceResponder.ts uniqueRecords, :979-989 at db021da0).
func uniqueRecords(rrs []dns.RR) []dns.RR {
	seen := map[string]bool{}
	out := make([]dns.RR, 0, len(rrs))
	for _, rr := range rrs {
		key := recordIdentity(rr)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, rr)
	}
	return out
}

// anyReplaces reports whether one of rrs belongs to a replaced set.
func anyReplaces(rrs []dns.RR, replaced map[string]bool) bool {
	if len(replaced) == 0 {
		return false
	}
	for _, rr := range rrs {
		if replaced[setKeyOf(rr)] {
			return true
		}
	}
	return false
}

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

// reach is how far a send reached — matter.js UdpMulticastServer.Reach
// (packages/general/src/net/dns-sd/MdnsSocket.ts:102-127 at db021da0).
type reach int

const (
	// reachNone: the send left on no address family, or was refused.
	reachNone reach = iota
	// reachSome: it left on some of the interface's address families.
	reachSome
	// reachAll: it left on every address family it was sent on.
	reachAll
)

// reachOf is the reach of a send tried on tried families, ok of which
// took it.
func reachOf(tried, ok int) reach {
	switch {
	case ok == 0:
		return reachNone
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
}

// linkState is the responder's memory of one interface — matter.js's
// InterfaceResponder state that outlives a record rebuild: what peers may
// cache there, and the goodbye retry.
type linkState struct {
	peers map[string]peerEntry
	seq   uint64
	// retry is the pending goodbye retry, nil while none is needed.
	retry *goodbyeRetry
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
		st = &linkState{peers: map[string]peerEntry{}}
		r.linkStates[ifIndex] = st
	}
	return st
}

// noteSentLocked records what a message that reached as far as rc carried:
// a record with a TTL is cached by the peers until the TTL ends; a goodbye
// that reached every family ends the record, one that reached only some
// leaves it to be said again; a send that reached nothing changes nothing
// (InterfaceResponder.ts #transmit, :727-772 at db021da0). Caller holds
// r.mu.
func (r *Responder) noteSentLocked(ifIndex int, msgs []*dns.Msg, rc reach, now time.Time) {
	if rc == reachNone {
		return
	}
	st := r.linkStateLocked(ifIndex)
	for _, m := range msgs {
		for _, section := range [][]dns.RR{m.Answer, m.Extra} {
			for _, rr := range section {
				key := recordIdentity(rr)
				ttl := rr.Header().Ttl
				switch {
				case ttl == 0 && rc == reachSome:
					continue
				case ttl == 0:
					delete(st.peers, key)
				default:
					p, ok := st.peers[key]
					if !ok {
						st.seq++
						p.seq = st.seq
					}
					p.rr, p.until = rr, now.Add(time.Duration(ttl)*time.Second)
					st.peers[key] = p
				}
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
		if p.until.After(now) && !lr.byKey[key] {
			keys = append(keys, key)
		}
	}
	slices.SortFunc(keys, func(a, b string) int { return cmp.Compare(st.peers[a].seq, st.peers[b].seq) })
	var out []dns.RR
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
func (r *Responder) publishLocked(ifIndex int, services map[string]bool, all bool) {
	if r.wire == nil {
		return
	}
	now := r.clock.Load().Now()
	lr := r.recordsForLocked(ifIndex)
	st := r.linkStateLocked(ifIndex)
	goodbye := r.goodbyesLocked(st, lr, now)

	replaced := map[string]bool{}
	if r.isListedLocked(ifIndex) {
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
	answers := append(goodbye, sendable(announced, complete, false)...)
	if rc := r.sendLocked(splitResponse(0, answers, nil), ifIndex, nil); rc != reachAll && len(goodbye) > 0 {
		r.logger().Debug("mdns.responder.goodbye_not_sent", slog.Int("iface", ifIndex), slog.Int("reach", int(rc)))
	}
	r.settleGoodbyesLocked(ifIndex, st)
}

// settleGoodbyesLocked retries the goodbyes peers may still need on the
// interface, or ends the retry when none remain (InterfaceResponder.ts
// #settleGoodbyes, :534-542 at db021da0). Caller holds r.mu.
func (r *Responder) settleGoodbyesLocked(ifIndex int, st *linkState) {
	if len(r.goodbyesLocked(st, r.recordsForLocked(ifIndex), r.clock.Load().Now())) > 0 {
		r.retryGoodbyesLocked(ifIndex, st)
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
		r.publishLocked(ifIndex, nil, false)
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
	for _, ifIndex := range r.knownLinksLocked() {
		r.publishLocked(ifIndex, services, all)
	}
	r.pruneLinkStatesLocked()
}

// knownLinksLocked lists the listed interfaces and those with state, in
// index order. Caller holds r.mu.
func (r *Responder) knownLinksLocked() []int {
	seen := map[int]bool{}
	var out []int
	for _, i := range r.wire.links() {
		if !seen[i] {
			seen[i] = true
			out = append(out, i)
		}
	}
	for i := range r.linkStates {
		if !seen[i] {
			seen[i] = true
			out = append(out, i)
		}
	}
	slices.Sort(out)
	return out
}

// isListedLocked reports whether the interface is among the wire's links.
// Caller holds r.mu.
func (r *Responder) isListedLocked(ifIndex int) bool {
	return slices.Contains(r.wire.links(), ifIndex)
}

// pruneLinkStatesLocked forgets the records whose TTL ended and drops the
// state of an interface peers cache nothing on and no retry waits for
// (InterfaceResponder.prune, :142-160 at db021da0). Caller holds r.mu.
func (r *Responder) pruneLinkStatesLocked() {
	now := r.clock.Load().Now()
	for i, st := range r.linkStates {
		for key, p := range st.peers {
			if !p.until.After(now) {
				delete(st.peers, key)
			}
		}
		if len(st.peers) == 0 && st.retry == nil {
			delete(r.linkStates, i)
		}
	}
}

// stopAllGoodbyeRetriesLocked ends every interface's retry (close).
// Caller holds r.mu.
func (r *Responder) stopAllGoodbyeRetriesLocked() {
	for _, st := range r.linkStates {
		r.stopGoodbyeRetryLocked(st)
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

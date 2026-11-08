// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"github.com/miekg/dns"
)

// The RFC 6762 §6 one-second rule as matter.js keeps it per interface
// (packages/protocol/src/mdns/InterfaceResponder.ts at db021da0, the class
// description :48-84): a record goes out by multicast at most once a
// second on an interface, counting the multicast answers to a query and
// the goodbyes — not the announcements, which are never held, nor the
// additional records; a record the rule holds back is deferred to the
// moment its second has passed, and every record due then goes out in one
// deferred message under the same rule. The address records of one name
// are held together (§6.2). A held record is not sent when a multicast
// answer that carried it left at or after the query arrived; a deferred
// record its service no longer publishes is dropped.

// multicastInterval is MULTICAST_INTERVAL (InterfaceResponder.ts:34-35 at
// db021da0): one multicast of a record per second and interface.
const multicastInterval = time.Second

// deferral is one record the rule held back (InterfaceResponder.ts
// Deferral): the section it was meant for, when its second has passed,
// the earliest time a querier asked for it, and, for an answer, the
// additional records its packet would have carried.
type deferral struct {
	answer     bool
	dueAt      time.Time
	since      time.Time
	additional []string
	// seq orders the deferrals as first deferred (matter.js's Map).
	seq uint64
}

// heldUntil is #heldUntil (:629-632): when the rule stops holding rr
// back; a goodbye counts as a multicast. Caller holds r.mu.
func (st *linkState) heldUntil(rr dns.RR) time.Time {
	p, ok := st.peers[recordIdentity(rr)]
	if !ok || p.lastMulticast.IsZero() {
		return time.Time{}
	}
	return p.lastMulticast.Add(multicastInterval)
}

// deliveredSince is #isDeliveredSince (:634-638): a multicast answer that
// carried rr, not its goodbye, left at or after since.
func (st *linkState) deliveredSince(rr dns.RR, since time.Time) bool {
	p, ok := st.peers[recordIdentity(rr)]
	return ok && !p.lastIsGoodbye && !p.lastMulticast.IsZero() && !p.lastMulticast.Before(since)
}

// sinceCachedMulticast is #sinceCachedMulticast (:640-652): the time since
// the last multicast answer that carried rr, or the longest duration when
// peers may not cache it.
func (st *linkState) sinceCachedMulticast(rr dns.RR, now time.Time) time.Duration {
	p, ok := st.peers[recordIdentity(rr)]
	if !ok || p.lastMulticast.IsZero() || p.lastIsGoodbye || !p.until.After(now) {
		return time.Duration(1<<63 - 1)
	}
	return now.Sub(p.lastMulticast)
}

// addressSetsOf is addressSetsOf (:898-908): the A and AAAA records of rrs
// grouped by name, the unit of the hold.
func addressSetsOf(rrs []dns.RR) [][]dns.RR {
	var order []string
	sets := map[string][]dns.RR{}
	for _, rr := range rrs {
		if t := rr.Header().Rrtype; t != dns.TypeA && t != dns.TypeAAAA {
			continue
		}
		name := strings.ToLower(rr.Header().Name)
		if _, ok := sets[name]; !ok {
			order = append(order, name)
		}
		sets[name] = append(sets[name], rr)
	}
	out := make([][]dns.RR, 0, len(order))
	for _, n := range order {
		out = append(out, sets[n])
	}
	return out
}

// holdBackLocked is #holdBack (:398-443): rrs without the records the rule
// holds back; the address records of one name across packet wait for the
// latest one's second; a held record is deferred unless a multicast answer
// carried it at or after its since time. A deferred answer remembers its
// additional records. Caller holds r.mu.
func (r *Responder) holdBackLocked(ifIndex int, st *linkState, rrs []dns.RR, now time.Time, sinceOf func(dns.RR) time.Time,
	answer bool, additionalOf func(dns.RR) []dns.RR, packet []dns.RR,
) []dns.RR {
	held := map[dns.RR]time.Time{}
	for _, rr := range rrs {
		if until := st.heldUntil(rr); now.Before(until) {
			held[rr] = until
		}
	}
	for _, set := range addressSetsOf(packet) {
		var until time.Time
		for _, rr := range set {
			if u := st.heldUntil(rr); u.After(until) {
				until = u
			}
		}
		if !now.Before(until) {
			continue
		}
		for _, rr := range set {
			for _, x := range rrs {
				if x == rr {
					held[rr] = until
				}
			}
		}
	}
	out := make([]dns.RR, 0, len(rrs))
	for _, rr := range rrs {
		until, ok := held[rr]
		if !ok {
			out = append(out, rr)
			continue
		}
		since := sinceOf(rr)
		if !st.deliveredSince(rr, since) {
			r.deferLocked(ifIndex, st, rr, answer, since, additionalOf(rr), until)
		}
	}
	return out
}

// deferLocked is #defer (:654-681). Caller holds r.mu.
func (r *Responder) deferLocked(ifIndex int, st *linkState, rr dns.RR, answer bool, since time.Time, additional []dns.RR, dueAt time.Time) {
	if st.deferred == nil {
		st.deferred = map[string]*deferral{}
	}
	key := recordIdentity(rr)
	d, ok := st.deferred[key]
	if !ok {
		st.deferSeq++
		d = &deferral{answer: answer, dueAt: dueAt, since: since, seq: st.deferSeq}
		st.deferred[key] = d
	} else {
		d.answer = d.answer || answer
		if dueAt.After(d.dueAt) {
			d.dueAt = dueAt
		}
		if since.Before(d.since) {
			d.since = since
		}
	}
	for _, a := range additional {
		if k := recordIdentity(a); !slices.Contains(d.additional, k) {
			d.additional = append(d.additional, k)
		}
	}
	r.scheduleDeferredLocked(ifIndex, st)
}

// scheduleDeferredLocked is #scheduleDeferred (:683-692): the timer of the
// earliest due deferral. Caller holds r.mu.
func (r *Responder) scheduleDeferredLocked(ifIndex int, st *linkState) {
	if st.deferredTimer != nil {
		st.deferredTimer.Stop()
		st.deferredTimer = nil
	}
	st.deferredGen++
	if len(st.deferred) == 0 {
		return
	}
	var due time.Time
	for _, d := range st.deferred {
		if due.IsZero() || d.dueAt.Before(due) {
			due = d.dueAt
		}
	}
	gen := st.deferredGen
	wait := max(due.Sub(r.clock.Load().Now()), 0)
	st.deferredTimer = r.clock.Load().AfterFunc(wait, func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.closed || r.wire == nil || r.linkStates[ifIndex] != st || st.deferredGen != gen {
			return
		}
		st.deferredTimer = nil
		r.sendDeferredLocked(ifIndex, st)
	})
}

// stopDeferred drops the interface's deferred records (close).
func (st *linkState) stopDeferred() {
	if st.deferredTimer != nil {
		st.deferredTimer.Stop()
		st.deferredTimer = nil
	}
	st.deferredGen++
	clear(st.deferred)
}

// sendDeferredLocked is #sendDeferred (:548-627): the deferred records
// whose second has passed, as one packet, under the same hold decision; a
// record held again is deferred again, one a multicast answer carried
// since its deferral is not sent, one its service no longer publishes is
// dropped, and a deferred answer brings its additional records. Caller
// holds r.mu.
func (r *Responder) sendDeferredLocked(ifIndex int, st *linkState) {
	now := r.clock.Load().Now()
	l := r.wire.link(ifIndex)
	lr := r.recordsForLocked(ifIndex, l.addrsFunc())
	deferrals := st.deferred
	st.deferred = map[string]*deferral{}

	var answers, additional []dns.RR
	sinceOf := map[dns.RR]time.Time{}
	additionalOf := map[dns.RR][]dns.RR{}
	keys := make([]string, 0, len(deferrals))
	for k := range deferrals {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b string) int { return cmp.Compare(deferrals[a].seq, deferrals[b].seq) })
	for _, key := range keys {
		d := deferrals[key]
		rr, ok := lr.byKey[key]
		if !ok {
			continue
		}
		sinceOf[rr] = d.since
		if !d.answer {
			additional = append(additional, rr)
			continue
		}
		answers = append(answers, rr)
		var add []dns.RR
		for _, k := range d.additional {
			if a, ok := lr.byKey[k]; ok {
				add = append(add, a)
			}
		}
		additionalOf[rr] = add
	}
	timeOf := func(rr dns.RR) time.Time {
		if s, ok := sinceOf[rr]; ok {
			return s
		}
		return now
	}
	var packet []dns.RR
	packet = append(packet, answers...)
	packet = append(packet, additional...)
	for _, rr := range answers {
		packet = append(packet, additionalOf[rr]...)
	}
	packet = uniqueRecords(packet)
	sentAnswers := r.holdBackLocked(ifIndex, st, answers, now, timeOf, true, func(rr dns.RR) []dns.RR { return additionalOf[rr] }, packet)
	answerKeys := map[string]bool{}
	for _, rr := range sentAnswers {
		answerKeys[recordIdentity(rr)] = true
	}
	for _, rr := range sentAnswers {
		for _, a := range additionalOf[rr] {
			if _, ok := sinceOf[a]; !ok {
				sinceOf[a] = timeOf(rr)
			}
			additional = append(additional, a)
		}
	}
	var candidates []dns.RR
	for _, rr := range uniqueRecords(additional) {
		if !answerKeys[recordIdentity(rr)] {
			candidates = append(candidates, rr)
		}
	}
	sentAdditional := r.holdBackLocked(ifIndex, st, candidates, now, timeOf, false, func(dns.RR) []dns.RR { return nil },
		uniqueRecords(append(append([]dns.RR{}, sentAnswers...), additional...)))
	r.scheduleDeferredLocked(ifIndex, st)
	if len(sentAnswers) == 0 && len(sentAdditional) == 0 {
		return
	}
	complete := lr.complete
	section := sentAnswers
	var extra []dns.RR
	if len(sentAnswers) > 0 {
		extra = sendable(sentAdditional, complete, false)
	} else {
		section = sentAdditional
	}
	r.sendLocked(splitResponse(0, sendable(section, complete, false), extra), l, nil, section)
}

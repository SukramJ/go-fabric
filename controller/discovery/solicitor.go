// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package discovery

import (
	"context"
	"log/slog"
	"math"
	"slices"
	"time"

	"github.com/SukramJ/go-fabric/mdns"
)

// Retries configures a retry schedule, matter.js RetrySchedule.Configuration
// (packages/general/src/net/RetrySchedule.ts). A zero field is unset: in a
// [Discovery] it takes the solicitor's default, in the schedule itself it
// means "none" (no timeout, no count limit, no maximum, no jitter) or the
// schedule's own default (initial interval 1 s, backoff ×2).
type Retries struct {
	// Timeout bounds the schedule's total time.
	Timeout time.Duration
	// MaximumCount bounds the number of intervals.
	MaximumCount int
	// InitialInterval is the first interval.
	InitialInterval time.Duration
	// BackoffFactor multiplies the interval after each step.
	BackoffFactor float64
	// MaximumInterval caps an interval, jitter included.
	MaximumInterval time.Duration
	// JitterFactor adds up to this fraction of the interval, at random.
	JitterFactor float64
}

// DefaultRetries is the solicitor's schedule: 1 s, doubling, jitter up to
// 20 %, at most one hour. Mirrors matter.js 0ceb202d
// packages/general/src/net/dns-sd/DnssdSolicitor.ts:94-99
// (DnssdSolicitor.DefaultRetries).
var DefaultRetries = Retries{
	InitialInterval: time.Second,
	JitterFactor:    0.2,
	BackoffFactor:   2,
	MaximumInterval: time.Hour,
}

// merge overlays the set fields of o on r (matter.js
// RetrySchedule.Configuration(defaults, config), RetrySchedule.ts:114-119).
func (r Retries) merge(o *Retries) Retries {
	if o == nil {
		return r
	}
	if o.Timeout != 0 {
		r.Timeout = o.Timeout
	}
	if o.MaximumCount != 0 {
		r.MaximumCount = o.MaximumCount
	}
	if o.InitialInterval != 0 {
		r.InitialInterval = o.InitialInterval
	}
	if o.BackoffFactor != 0 {
		r.BackoffFactor = o.BackoffFactor
	}
	if o.MaximumInterval != 0 {
		r.MaximumInterval = o.MaximumInterval
	}
	if o.JitterFactor != 0 {
		r.JitterFactor = o.JitterFactor
	}
	return r
}

// retrySchedule yields the intervals of one schedule. Mirrors matter.js
// 0ceb202d packages/general/src/net/RetrySchedule.ts:28-60: the jitter
// (floor of jitterFactor × interval × random/2³², in ms) is added first,
// then the interval is cut to what is left of the timeout and clamped to
// the maximum.
type retrySchedule struct {
	cfg    Retries
	random func() uint32
	count  int
	base   time.Duration
	soFar  time.Duration
}

func newRetrySchedule(cfg Retries, random func() uint32) *retrySchedule {
	if cfg.InitialInterval == 0 {
		cfg.InitialInterval = time.Second
	}
	if cfg.BackoffFactor == 0 {
		cfg.BackoffFactor = 2
	}
	return &retrySchedule{cfg: cfg, random: random, base: cfg.InitialInterval}
}

func (r *retrySchedule) next() (time.Duration, bool) {
	if (r.cfg.Timeout != 0 && r.soFar >= r.cfg.Timeout) || (r.cfg.MaximumCount != 0 && r.count >= r.cfg.MaximumCount) {
		return 0, false
	}
	r.count++
	maxJitterMs := r.cfg.JitterFactor * float64(r.base) / float64(time.Millisecond)
	jitter := saturate(math.Floor(maxJitterMs*float64(r.random())/(1<<32)) * float64(time.Millisecond))
	interval := saturate(float64(r.base) + float64(jitter))
	if r.cfg.Timeout != 0 && r.soFar+interval > r.cfg.Timeout {
		interval = r.cfg.Timeout - r.soFar
	}
	if r.cfg.MaximumInterval != 0 && interval > r.cfg.MaximumInterval {
		interval = r.cfg.MaximumInterval
	}
	r.soFar += interval
	r.base = saturate(float64(r.base) * r.cfg.BackoffFactor)
	return interval, true
}

// saturate converts to a Duration, holding at the largest one: matter.js
// computes intervals in floating point, where the doubling base never
// wraps, and the maximum interval clamps it.
func saturate(ns float64) time.Duration {
	if ns >= math.MaxInt64 {
		return math.MaxInt64
	}
	return time.Duration(ns)
}

// Solicitation asks for record types of one name, matter.js
// DnssdSolicitor.Solicitation.
type Solicitation struct {
	// Name is the name to query.
	Name *Name
	// RecordTypes are the record types to ask for.
	RecordTypes []mdns.DNSType
	// AssociatedNames, when set, returns further names whose records go
	// into the query as known answers. It is read when the query is built,
	// so it may change between retries.
	AssociatedNames func() []*Name
}

// Discovery is a [Solicitation] repeated on a retry schedule, matter.js
// DnssdSolicitor.Discovery.
type Discovery struct {
	Solicitation
	// Retries overrides fields of [DefaultRetries] for this discovery;
	// nil keeps the default.
	Retries *Retries
}

type associated struct{ fn func() []*Name }

type solicitation struct {
	name       *Name
	types      []mdns.DNSType
	associated *associated
}

type pendingSolicitation struct {
	name       *Name
	types      []mdns.DNSType
	associated []*associated
}

type activeDiscovery struct {
	waiting  map[*discoveryHandle]struct{}
	timer    stopper
	aborted  bool
	finished chan struct{}
}

// discoveryHandle identifies one caller of a discovery. It is not
// zero-sized: pointers to zero-sized values may compare equal.
type discoveryHandle struct{ _ byte }

// Solicitor sends the queries of a [Names] cache, matter.js
// QueryMulticaster (packages/general/src/net/dns-sd/DnssdSolicitor.ts):
// solicitations made in one loop task go out as one packet, and repeated
// discovery of one name runs one schedule however many callers want it.
type Solicitor struct {
	names *Names

	// Loop-owned state below.
	toSolicit   *omap[*Name, *pendingSolicitation]
	discovering map[*Name]*activeDiscovery
	scheduled   bool
	closed      bool
}

func newSolicitor(n *Names) *Solicitor {
	return &Solicitor{
		names:       n,
		toSolicit:   newOmap[*Name, *pendingSolicitation](),
		discovering: make(map[*Name]*activeDiscovery),
	}
}

// Solicitor returns the cache's shared solicitor.
func (n *Names) Solicitor() *Solicitor { return n.solicitor }

// Solicit sends one query for the solicitation, coalesced with the other
// solicitations of the same loop task. Mirrors matter.js 0ceb202d
// DnssdSolicitor.ts:131-152.
func (s *Solicitor) Solicit(sol Solicitation) {
	so := solicitationOf(sol)
	_ = s.names.loop.call(func() { s.solicit(so) })
}

// Discover queries for the discovery's name now and again on its retry
// schedule until ctx ends or the schedule runs out. While one discovery of
// a name runs, a second one for the same name joins it rather than
// querying twice, and the first caller's fields drive both. It returns nil
// when ctx ends or the schedule runs out, [ErrClosed] when the cache
// closes. Mirrors matter.js 0ceb202d DnssdSolicitor.ts:154-205.
func (s *Solicitor) Discover(ctx context.Context, d Discovery) error {
	var cancel func()
	var finished chan struct{}
	so := solicitationOf(d.Solicitation)
	if err := s.names.loop.call(func() { cancel, finished = s.discover(so, d.Retries) }); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
	case <-finished:
	case <-s.names.loop.done:
		return ErrClosed
	}
	return s.names.loop.call(cancel)
}

func solicitationOf(sol Solicitation) *solicitation {
	so := &solicitation{name: sol.Name, types: slices.Clone(sol.RecordTypes)}
	if sol.AssociatedNames != nil {
		so.associated = &associated{fn: sol.AssociatedNames}
	}
	return so
}

// --- loop-side ---

func (s *Solicitor) solicit(so *solicitation) {
	if s.closed {
		return
	}
	p, ok := s.toSolicit.get(so.name)
	if !ok {
		p = &pendingSolicitation{name: so.name}
		s.toSolicit.set(so.name, p)
	}
	for _, t := range so.types {
		if !slices.Contains(p.types, t) {
			p.types = append(p.types, t)
		}
	}
	if so.associated != nil && !slices.Contains(p.associated, so.associated) {
		p.associated = append(p.associated, so.associated)
	}
	if !s.scheduled {
		s.scheduled = true
		s.names.loop.macrotask(s.emit)
	}
}

// discover registers one caller of a discovery and returns the function
// that ends its participation, and the channel closed when the shared
// discovery finishes. Mirrors matter.js 0ceb202d DnssdSolicitor.ts:154-179.
func (s *Solicitor) discover(so *solicitation, retries *Retries) (cancel func(), finished chan struct{}) {
	h := &discoveryHandle{}
	active, ok := s.discovering[so.name]
	if ok {
		active.waiting[h] = struct{}{}
	} else {
		active = &activeDiscovery{waiting: map[*discoveryHandle]struct{}{h: {}}, finished: make(chan struct{})}
		s.discovering[so.name] = active
		s.run(so, retries, active)
	}
	return func() {
		delete(active.waiting, h)
		if len(active.waiting) == 0 {
			s.abort(active)
			if s.discovering[so.name] == active {
				delete(s.discovering, so.name)
			}
		}
	}, active.finished
}

// run sends the first query at once — RFC 6762 §5.2's 20–120 ms initial
// delay is skipped, as matter.js skips it (:189-193) — and the rest on the
// schedule. Mirrors matter.js 0ceb202d DnssdSolicitor.ts:181-205.
func (s *Solicitor) run(so *solicitation, retries *Retries, active *activeDiscovery) {
	schedule := newRetrySchedule(DefaultRetries.merge(retries), s.names.random)
	s.solicit(so)
	var step func()
	step = func() {
		interval, ok := schedule.next()
		if !ok {
			s.finish(active)
			return
		}
		active.timer = s.names.clock.AfterFunc(interval, func() {
			s.names.loop.post(func() {
				if active.aborted {
					return
				}
				s.solicit(so)
				step()
			})
		})
	}
	step()
}

func (s *Solicitor) abort(active *activeDiscovery) {
	if active.aborted {
		return
	}
	active.aborted = true
	if active.timer != nil {
		active.timer.Stop()
	}
	s.finish(active)
}

func (s *Solicitor) finish(active *activeDiscovery) {
	select {
	case <-active.finished:
	default:
		close(active.finished)
	}
}

// emit sends the pending solicitations as one query whose known answers
// are the solicited and associated names' records. Mirrors matter.js
// 0ceb202d DnssdSolicitor.ts:212-266.
func (s *Solicitor) emit() {
	s.scheduled = false
	if s.closed {
		return
	}
	entries := s.toSolicit.values()
	s.toSolicit.clear()
	var questions []mdns.DNSQuestion
	var answers []mdns.DNSRecord
	for _, e := range entries {
		for _, t := range e.types {
			questions = append(questions, mdns.DNSQuestion{Name: e.name.qname, Type: t})
		}
		answers = append(answers, knownAnswersOf(e.name)...)
		for _, a := range e.associated {
			for _, nm := range a.fn() {
				answers = append(answers, knownAnswersOf(nm)...)
			}
		}
	}
	if len(questions) == 0 {
		return
	}
	msg, err := mdns.PackDNSQuery(questions, answers)
	if err == nil && s.names.conn != nil {
		err = s.names.conn.Send(msg)
	}
	if err != nil {
		s.names.logger.Warn("matter.discovery.solicit", slog.String("err", err.Error()))
	}
}

// knownAnswersOf gives a name's records as known answers, without the
// cache-flush bit, which means nothing outside a response (RFC 6762
// §18.12). Mirrors matter.js 0ceb202d DnssdSolicitor.ts:274-276.
func knownAnswersOf(nm *Name) []mdns.DNSRecord {
	out := make([]mdns.DNSRecord, 0, nm.records.len())
	for _, r := range nm.records.values() {
		d := r.dnsRecord()
		d.CacheFlush = false
		out = append(out, d)
	}
	return out
}

// close aborts every discovery. Mirrors matter.js 0ceb202d
// DnssdSolicitor.ts:207-210.
func (s *Solicitor) close() {
	s.closed = true
	for name, active := range s.discovering {
		s.abort(active)
		delete(s.discovering, name)
	}
	s.toSolicit.clear()
}

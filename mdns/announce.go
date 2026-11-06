// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package mdns

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/SukramJ/go-fabric/clock"
)

// Announcements, the broadcast schedule and goodbyes. matter.js claims the
// names without probing first (MdnsAdvertisement.ts:179-180 at 862ad75f),
// and chip does not probe either; the responder announces at once.

// broadcastEvent is what started a broadcast run, matter.js
// Advertiser.BroadcastEvent (packages/protocol/src/advertisement/
// Advertiser.ts:93 at 862ad75f).
type broadcastEvent int

const (
	// eventStartup is a new or changed publish: the full schedule.
	eventStartup broadcastEvent = iota
	// eventReconnect follows the loss of the last session of a peer
	// (MdnsAdvertisement.ts:132-134 serviceDisconnected): the full
	// schedule again.
	eventReconnect
	// eventRetransmit follows an MRP retransmission: one broadcast
	// (MdnsAdvertiser.ts:237-242 RetransmissionBroadcastSchedule).
	eventRetransmit
)

// broadcastSchedule is matter.js RetrySchedule.Configuration as the mDNS
// advertiser fills it. A zero field is "unset".
type broadcastSchedule struct {
	initialInterval time.Duration
	timeout         time.Duration
	backoffFactor   float64
	maximumInterval time.Duration
	jitterFactor    float64
	maximumCount    int
}

// defaultBroadcastSchedule is matter.js MdnsAdvertiser.DefaultBroadcastSchedule
// (packages/protocol/src/advertisement/mdns/MdnsAdvertiser.ts:213-232 at
// 862ad75f): 1 s, doubling, at most 90 s apart, 25 % jitter, for the
// maximum commissioning timeout of two days
// (packages/types/src/commissioning/CommissioningConstants.ts:11). Its
// broadcastAfterConnection is Instant: an operational connection ends the
// run ([Responder.ServiceConnected]).
var defaultBroadcastSchedule = broadcastSchedule{
	initialInterval: time.Second,
	timeout:         48 * time.Hour,
	backoffFactor:   2,
	maximumInterval: 90 * time.Second,
	jitterFactor:    0.25,
}

// scheduleFor is matter.js MdnsAdvertiser #scheduleFor (MdnsAdvertiser.ts:129-149
// at 862ad75f) over its built-in list: the retransmit schedule for a
// retransmission — the default merged with maximumCount 1 (:109-119,
// RetrySchedule.Configuration) — and the default for every other event.
func scheduleFor(event broadcastEvent) broadcastSchedule {
	s := defaultBroadcastSchedule
	if event == eventRetransmit {
		s.maximumCount = 1
	}
	return s
}

// retrySchedule yields the intervals of a schedule, matter.js RetrySchedule
// (packages/general/src/net/RetrySchedule.ts:28-60 at 862ad75f).
type retrySchedule struct {
	cfg      broadcastSchedule
	count    int
	base     time.Duration
	soFar    time.Duration
	random32 func() uint32
}

func newRetrySchedule(cfg broadcastSchedule) *retrySchedule {
	initial := cfg.initialInterval
	if initial == 0 {
		initial = time.Second
	}
	if cfg.backoffFactor == 0 {
		cfg.backoffFactor = 2
	}
	return &retrySchedule{cfg: cfg, base: initial, random32: rand.Uint32} //nolint:gosec // G404: a timing jitter
}

// next is the next interval; false when the schedule is exhausted.
func (s *retrySchedule) next() (time.Duration, bool) {
	c := s.cfg
	if (c.timeout != 0 && s.soFar >= c.timeout) || (c.maximumCount != 0 && s.count >= c.maximumCount) {
		return 0, false
	}
	s.count++
	maxJitter := c.jitterFactor * float64(s.base.Milliseconds())
	jitter := time.Duration(maxJitter*float64(s.random32())/(1<<32)) * time.Millisecond
	interval := s.base + jitter
	if c.timeout != 0 && s.soFar+interval > c.timeout {
		interval = c.timeout - s.soFar
	}
	if c.maximumInterval != 0 && interval > c.maximumInterval {
		interval = c.maximumInterval
	}
	s.soFar += interval
	// matter.js grows the base without bound (a float of milliseconds);
	// once it reaches the cap every interval is the cap, so the base
	// saturates there instead of overflowing a Duration.
	if c.maximumInterval == 0 || s.base < c.maximumInterval {
		if next := float64(s.base) * c.backoffFactor; next < float64(math.MaxInt64/2) {
			s.base = time.Duration(next)
		}
	}
	return interval, true
}

// broadcastRun is one running schedule of a service — matter.js
// MdnsAdvertisement.run (MdnsAdvertisement.ts:64-80 at 862ad75f) over its
// activity.
type broadcastRun struct {
	sched *retrySchedule
	timer clock.Timer
	// stopAt ends the run at the first step at or past it; zero is none
	// (#stopAt, :65, :68-70).
	stopAt time.Time
}

// startLocked begins a broadcast run of e for event, unless one runs:
// matter.js Advertisement.start (packages/protocol/src/advertisement/
// Advertisement.ts:78-91 at 862ad75f) does not reset a running schedule.
// The first broadcast goes out now. Caller holds r.mu.
func (r *Responder) startLocked(e *entry, event broadcastEvent) {
	if r.closed || e.run != nil {
		return
	}
	run := &broadcastRun{sched: newRetrySchedule(scheduleFor(event))}
	if r.random32 != nil {
		run.sched.random32 = r.random32
	}
	e.run = run
	r.stepLocked(e, run)
}

// stepLocked is one turn of the run loop (MdnsAdvertisement.ts:67-79):
// the next interval, the stop check, the broadcast, the sleep. A run whose
// schedule is exhausted ends after its last sleep, as matter.js keeps the
// run's broadcasting flag set through it (Advertisement.ts:278-290). Caller
// holds r.mu.
func (r *Responder) stepLocked(e *entry, run *broadcastRun) {
	if r.closed || e.run != run {
		return
	}
	interval, ok := run.sched.next()
	now := r.clock.Load().Now()
	if !ok || (!run.stopAt.IsZero() && !now.Before(run.stopAt)) {
		e.run = nil
		return
	}
	r.broadcastLocked(e)
	run.timer = r.clock.Load().AfterFunc(interval, func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.stepLocked(e, run)
	})
}

// stopLocked ends e's run (Advertisement.stop, Advertisement.ts:96-99).
// Caller holds r.mu.
func (r *Responder) stopLocked(e *entry) {
	if e.run == nil {
		return
	}
	if e.run.timer != nil {
		e.run.timer.Stop()
	}
	e.run = nil
}

// broadcastLocked announces e on every interface: one publication per
// interface that carries e's records with the goodbyes of the records that
// left, every record in the answer section (RFC 6762 §8.3; matter.js
// MdnsServer.broadcast and InterfaceResponder.publish,
// MdnsServer.ts:210-223 and InterfaceResponder.ts:271-302 at db021da0),
// split where it outgrows a packet with every part in the answer section
// (MdnsTest.ts:757-829 at db021da0). Caller holds r.mu.
func (r *Responder) broadcastLocked(e *entry) {
	if r.wire == nil {
		return
	}
	for _, ifIndex := range r.wire.links() {
		r.publishLocked(ifIndex, map[string]bool{e.key: true}, false)
	}
}

// broadcastAllLocked announces every published service now: one message
// per interface with each shared record once (MdnsServer.broadcast with no
// service named, MdnsServer.ts:210-223 at db021da0; MdnsServerTest.ts
// "announces all services in one packet with each shared record once").
// Caller holds r.mu.
func (r *Responder) broadcastAllLocked() {
	if r.wire == nil {
		return
	}
	for _, ifIndex := range r.wire.links() {
		r.publishLocked(ifIndex, nil, true)
	}
}

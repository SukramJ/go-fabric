// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"bytes"
	"context"
	"log/slog"
	"math"
	"sync"
	"time"

	"thingfabric.dev/go-fabric/cluster/core"
	msgdef "thingfabric.dev/go-fabric/cluster/spec/messages"
)

// The --media player's Messages (cluster/core Messages): a simulation of
// connectedhomeip's tv-app MessagesManager at the harness pin 6170af84
// (examples/tv-app/tv-common/clusters/messages/MessagesManager.cpp), the
// manager the MESS cases were written against. A message is presented at
// its StartTime — at once when it has none or it has passed
// (ScheduleOrPresentMessage) — and completes when its Duration is over;
// nothing is shown.

// matterEpoch is the Matter epoch, 2000-01-01T00:00:00Z (epoch-s).
var matterEpoch = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

// presentRetry is how often a presentation waits for the server to queue
// the message the presenter accepted (PresentMessage runs before it).
const presentRetry = 10 * time.Millisecond

// tvMessages is the presenter.
type tvMessages struct {
	logger *slog.Logger
	// presented and complete are the server's Presented and Complete; a
	// test sets fakes.
	presented func(id []byte) bool
	complete  func(id []byte)

	mu sync.Mutex
	// timers is each message's one pending timer, its presentation or
	// its completion; after arms them (nil: time.AfterFunc).
	timers map[string]*latestTimer
	after  func(d time.Duration, f func()) (stop func() bool)
}

// newTVMessages returns the Messages server over the presenter.
func newTVMessages(logger *slog.Logger) (*core.Messages, error) {
	p := &tvMessages{logger: logger, timers: map[string]*latestTimer{}}
	srv, err := core.NewMessages(core.MessagesConfig{Presenter: p})
	if err != nil {
		return nil, err
	}
	// The host builds Messages, so the host hands it its logger (decision
	// 635): the take-down after a fabric's removal logs a refusal there.
	srv.SetLogger(logger)
	p.presented = srv.Presented
	p.complete = func(id []byte) { srv.Complete(id, core.MessageOutcome{}) }
	return srv, nil
}

// PresentMessage schedules the message's presentation and completion.
// Each acts only while its arming is the message's latest (latestTimer,
// R2-C2-conc-2603): a presentation or completion that fired before
// CancelMessage or a newer PresentMessage of the id replaced it does
// nothing.
func (p *tvMessages) PresentMessage(_ context.Context, msg msgdef.MessageStruct) error {
	id := bytes.Clone(msg.MessageId)
	delay := time.Duration(0)
	if !msg.StartTime.Null {
		if at := matterEpoch.Add(time.Duration(msg.StartTime.Value) * time.Second); at.After(time.Now()) {
			delay = time.Until(at)
		}
	}
	p.schedule(id, delay, func(t *latestTimer, gen uint64) {
		p.mu.Lock()
		taken := t.takeLocked(gen)
		p.mu.Unlock()
		if !taken {
			return
		}
		for range 100 {
			if p.presented(id) {
				break
			}
			time.Sleep(presentRetry)
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		// The wait released p.mu: a cancel or a newer presentation of
		// the id in it replaced this one.
		if !t.latestLocked(gen) {
			return
		}
		if msg.Duration.Null {
			p.forgetLocked(id, t)
			return
		}
		d := time.Duration(min(msg.Duration.Value, uint64(math.MaxInt64)/uint64(time.Millisecond))) * time.Millisecond //nolint:gosec // bounded above
		t.armLocked(d, func(gen uint64) {
			p.mu.Lock()
			taken := t.takeLocked(gen)
			if taken {
				p.forgetLocked(id, t)
			}
			p.mu.Unlock()
			if taken {
				p.complete(id)
			}
		})
	})
	return nil
}

// CancelMessage stops the message's timers (CancelMessageTimers).
func (p *tvMessages) CancelMessage(_ context.Context, id []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if t, ok := p.timers[string(id)]; ok {
		t.stopLocked()
		delete(p.timers, string(id))
	}
	return nil
}

// schedule arms the message's timer with fn, which receives the timer and
// the arming's generation. Caller does not hold p.mu.
func (p *tvMessages) schedule(id []byte, after time.Duration, fn func(t *latestTimer, gen uint64)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	t, ok := p.timers[string(id)]
	if !ok {
		t = &latestTimer{after: p.after}
		p.timers[string(id)] = t
	}
	t.armLocked(after, func(gen uint64) { fn(t, gen) })
}

// forgetLocked drops the message's timer once it is done, unless a newer
// one took its place. Caller holds p.mu.
func (p *tvMessages) forgetLocked(id []byte, t *latestTimer) {
	if p.timers[string(id)] == t {
		delete(p.timers, string(id))
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/cluster/core"
	msgdef "github.com/SukramJ/go-fabric/cluster/spec/messages"
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
	srv    *core.Messages

	mu     sync.Mutex
	timers map[string]*time.Timer
}

// newTVMessages returns the Messages server over the presenter.
func newTVMessages(logger *slog.Logger) (*core.Messages, error) {
	p := &tvMessages{logger: logger, timers: map[string]*time.Timer{}}
	srv, err := core.NewMessages(core.MessagesConfig{Presenter: p})
	if err != nil {
		return nil, err
	}
	p.srv = srv
	return srv, nil
}

// PresentMessage schedules the message's presentation and completion.
func (p *tvMessages) PresentMessage(_ context.Context, msg msgdef.MessageStruct) error {
	id := bytes.Clone(msg.MessageId)
	delay := time.Duration(0)
	if !msg.StartTime.Null {
		if at := matterEpoch.Add(time.Duration(msg.StartTime.Value) * time.Second); at.After(time.Now()) {
			delay = time.Until(at)
		}
	}
	p.schedule(id, delay, func() {
		for range 100 {
			if p.srv.Presented(id) {
				break
			}
			time.Sleep(presentRetry)
		}
		if !msg.Duration.Null {
			p.schedule(id, time.Duration(msg.Duration.Value)*time.Millisecond, func() {
				p.srv.Complete(id, core.MessageOutcome{})
			})
		}
	})
	return nil
}

// CancelMessage stops the message's timers (CancelMessageTimers).
func (p *tvMessages) CancelMessage(_ context.Context, id []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if t, ok := p.timers[string(id)]; ok {
		t.Stop()
		delete(p.timers, string(id))
	}
	return nil
}

func (p *tvMessages) schedule(id []byte, after time.Duration, fn func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if t, ok := p.timers[string(id)]; ok {
		t.Stop()
	}
	p.timers[string(id)] = time.AfterFunc(after, fn)
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/secure/sigma"
)

// nowHookClock is the system clock whose Now counts its calls and runs
// hook once, on call number at: the seam that places a session's close
// inside sessionEvents.opened, after its table lookup. info reads the
// clock last, after the table's Sessions released its lock (which reads
// the clock too), so at is calibrated by a dry run of info.
type nowHookClock struct {
	clock.System
	calls atomic.Int64
	at    atomic.Int64
	hook  atomic.Pointer[func()]
}

func (c *nowHookClock) Now() time.Time {
	if c.calls.Add(1) == c.at.Load() {
		if h := c.hook.Swap(nil); h != nil {
			(*h)()
		}
	}
	return c.System.Now()
}

// armAtInfosLastRead arms hook on the last clock read of the next
// info(id) — opened's lookup — counted by a dry run of info.
func (c *nowHookClock) armAtInfosLastRead(t *testing.T, e *sessionEvents, id uint16, hook func()) {
	t.Helper()
	c.calls.Store(0)
	if _, ok := e.info(id); !ok {
		t.Fatal("the session is not in the table")
	}
	c.at.Store(c.calls.Load())
	c.calls.Store(0)
	c.hook.Store(&hook)
}

// sessionEventCounts records the session hooks in the order they ran.
type sessionEventCounts struct {
	mu     sync.Mutex
	events []string
}

func (c *sessionEventCounts) add(e string) {
	c.mu.Lock()
	c.events = append(c.events, e)
	c.mu.Unlock()
}

func (c *sessionEventCounts) list() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.events...)
}

// sessionEventsNode is an unstarted node — New starts no goroutine, so
// only the test reads its clock — with one CASE session in its table.
func sessionEventsNode(t *testing.T) (*Node, *nowHookClock, uint16) {
	t.Helper()
	cfg := mdnsTestConfig(t)
	cfg.Network.MDNS.Off = true
	clk := &nowHookClock{}
	cfg.Clock = clk
	n := newMDNSTestNode(t, cfg)
	entry, err := n.sec.sessions.OpenFromSigma(1, 2, 3, sigma.SessionKeys{})
	if err != nil {
		t.Fatal(err)
	}
	return n, clk, entry.SessionID
}

// TestSessionClosedWhileOpenedRecordsItLeavesNoOpenSession: a session that
// closes after opened() looked it up in the table and before opened()
// recorded it is neither left as announced nor reported as opened without
// its close.
func TestSessionClosedWhileOpenedRecordsItLeavesNoOpenSession(t *testing.T) {
	t.Parallel()
	n, clk, id := sessionEventsNode(t)
	var got sessionEventCounts
	n.Observe(Observers{
		OnSessionOpened: func(SessionInfo) { got.add("opened") },
		OnSessionClosed: func(SessionInfo) { got.add("closed") },
	})
	closeIt := func() {
		if err := n.sec.sessions.Close(id); err != nil {
			t.Error(err)
		}
	}
	clk.armAtInfosLastRead(t, &n.sessionEvents, id, closeIt)
	n.sessionEvents.opened(id)
	if clk.hook.Load() != nil {
		t.Fatal("opened read no clock: the close did not run inside it")
	}
	n.sessionEvents.mu.Lock()
	_, stale := n.sessionEvents.known[id]
	n.sessionEvents.mu.Unlock()
	if stale {
		t.Error("the closed session stays recorded as announced")
	}
	if events := got.list(); len(events) == 1 {
		t.Errorf("hooks %v: an open without its close", events)
	}
}

// TestSessionClosedDuringItsOpenHookReportsOpenBeforeClose: a session an
// OnSessionOpened hook closes is reported to every observer as opened,
// then closed — never closed before it was opened.
func TestSessionClosedDuringItsOpenHookReportsOpenBeforeClose(t *testing.T) {
	t.Parallel()
	n, _, id := sessionEventsNode(t)
	var second sessionEventCounts
	n.Observe(Observers{OnSessionOpened: func(SessionInfo) {
		if err := n.sec.sessions.Close(id); err != nil {
			t.Error(err)
		}
	}})
	n.Observe(Observers{
		OnSessionOpened: func(SessionInfo) { second.add("opened") },
		OnSessionClosed: func(SessionInfo) { second.add("closed") },
	})
	n.sessionEvents.opened(id)
	if events := second.list(); len(events) != 2 || events[0] != "opened" || events[1] != "closed" {
		t.Errorf("the second observer's hooks %v, want [opened closed]", events)
	}
}

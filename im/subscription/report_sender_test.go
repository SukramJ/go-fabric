// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package subscription_test

// The single report hook (review II, R2-P6-api-0508, W2-conc-04, owner
// decision 138): one [subscription.ReportSender] for every report of a
// manager, and a manager without one that fails visibly instead of
// draining what its subscriptions owe into nothing.

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/im/subscription"
)

// sentReport is one call of a recording [subscription.ReportSender].
type sentReport struct {
	at     time.Duration
	report subscription.Report
}

// recordingSender records every report it is handed, at now's offset from
// t0, and answers with outcome(report).
type recordingSender struct {
	mu      sync.Mutex
	t0, now time.Time
	got     []sentReport
	outcome func(subscription.Report) subscription.ReportOutcome
}

func (r *recordingSender) send(_ context.Context, _ *subscription.Subscription, report subscription.Report) subscription.ReportOutcome {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, sentReport{at: r.now.Sub(r.t0), report: report})
	if r.outcome == nil {
		return subscription.Sent
	}
	return r.outcome(report)
}

func (r *recordingSender) tick(m *subscription.Manager, at time.Duration) {
	r.mu.Lock()
	r.now = r.t0.Add(at)
	r.mu.Unlock()
	m.Tick(context.Background(), r.t0.Add(at))
}

func (r *recordingSender) take() []sentReport {
	r.mu.Lock()
	defer r.mu.Unlock()
	got := r.got
	r.got = nil
	return got
}

func TestManagerWithoutAReportSenderRefusesSubscriptions(t *testing.T) {
	t.Parallel()
	m := subscription.NewManager(subscription.Config{}, nil, nil)
	if _, err := m.Subscribe(defaultArgs()); !errors.Is(err, subscription.ErrNoReportSender) {
		t.Fatalf("Subscribe without a sender: err = %v, want ErrNoReportSender", err)
	}
	if m.Active() != 0 {
		t.Fatalf("Active() = %d after a refused Subscribe", m.Active())
	}

	wired := newManager(subscription.Config{}, nil)
	sub, err := wired.Subscribe(defaultArgs())
	if err != nil {
		t.Fatalf("Subscribe with a sender: %v", err)
	}
	if _, err := m.Restore(sub.PeerSubscription(false), 7); !errors.Is(err, subscription.ErrNoReportSender) {
		t.Fatalf("Restore without a sender: err = %v, want ErrNoReportSender", err)
	}

	m.SetReportSender(subscription.DiscardReports)
	if _, err := m.Subscribe(defaultArgs()); err != nil {
		t.Fatalf("Subscribe once a sender is set: %v", err)
	}
}

func TestReportSenderCarriesEventsAndPathsInOneReport(t *testing.T) {
	t.Parallel()
	m, fire := newEventManager(subscription.Config{})
	rec := &recordingSender{t0: time.Now()}
	m.SetReportSender(rec.send)
	args := defaultEventArgs()
	attr := mkPath(1, 0x0006, 0x0000)
	args.AttributePaths = []im.ConcreteAttributePath{attr}
	sub, err := m.Subscribe(args)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	sub.TouchLastReport(rec.t0)

	fire(mkEventFiring(1, 0x003B, 0x01, im.EventPriorityInfo))
	m.OnAttributeChanged(attr)
	rec.tick(m, 2*time.Second)
	got := rec.take()
	if len(got) != 1 {
		t.Fatalf("reports = %+v, want one", got)
	}
	r := got[0].report
	if len(r.Events) != 1 || len(r.Paths) != 1 || r.Paths[0] != attr || r.KeepAlive || r.Flush {
		t.Fatalf("report = %+v, want the event and the changed path together, neither keep-alive nor flush", r)
	}
	rec.tick(m, 4*time.Second)
	if got := rec.take(); len(got) != 0 {
		t.Fatalf("a second report followed: %+v", got)
	}
}

func TestReportSenderNothingForSubscriber(t *testing.T) {
	t.Parallel()
	// A 1 s floor and a 60 s ceiling: send interval 48 s.
	filterEvents := func(r subscription.Report) subscription.ReportOutcome {
		if len(r.Events) > 0 {
			return subscription.NothingForSubscriber
		}
		return subscription.Sent
	}

	t.Run("a filtered urgent event's pass is no report", func(t *testing.T) {
		t.Parallel()
		m, fire := newEventManager(subscription.Config{})
		rec := &recordingSender{t0: time.Now(), outcome: filterEvents}
		m.SetReportSender(rec.send)
		sub, err := m.Subscribe(defaultEventArgs())
		if err != nil {
			t.Fatalf("Subscribe: %v", err)
		}
		sub.TouchLastReport(rec.t0)
		fire(mkEventFiring(1, 0x003B, 0x01, im.EventPriorityInfo))
		rec.tick(m, time.Second)
		rec.tick(m, 47*time.Second)
		rec.tick(m, 48*time.Second)
		got := rec.take()
		// Counted, the pass at 1 s put the keep-alive at 49 s.
		if len(got) != 2 || got[0].at != time.Second || len(got[0].report.Events) != 1 ||
			got[1].at != 48*time.Second || !got[1].report.KeepAlive || len(got[1].report.Events) != 0 {
			t.Fatalf("reports = %+v, want the filtered event at 1s and the keep-alive at 48s", got)
		}
	})

	t.Run("the keep-alive a filtered pass carried goes out alone", func(t *testing.T) {
		t.Parallel()
		m, fire := newEventManager(subscription.Config{})
		rec := &recordingSender{t0: time.Now(), outcome: filterEvents}
		m.SetReportSender(rec.send)
		args := defaultEventArgs()
		args.EventPaths = []im.ConcreteEventPath{mkEventPath(1, 0x003B, 0x01)} // not urgent
		sub, err := m.Subscribe(args)
		if err != nil {
			t.Fatalf("Subscribe: %v", err)
		}
		sub.TouchLastReport(rec.t0)
		fire(mkEventFiring(1, 0x003B, 0x01, im.EventPriorityCritical))
		rec.tick(m, 48*time.Second)
		rec.tick(m, 95*time.Second)
		rec.tick(m, 96*time.Second)
		got := rec.take()
		if len(got) != 3 || got[0].at != 48*time.Second || len(got[0].report.Events) != 1 || !got[0].report.KeepAlive ||
			got[1].at != 48*time.Second || len(got[1].report.Events) != 0 || !got[1].report.KeepAlive ||
			got[2].at != 96*time.Second || !got[2].report.KeepAlive {
			t.Fatalf("reports = %+v, want the filtered pass and its keep-alive at 48s, the next keep-alive at 96s", got)
		}
	})
}

func TestReportSenderFlush(t *testing.T) {
	t.Parallel()
	m, fire := newEventManager(subscription.Config{})
	rec := &recordingSender{t0: time.Now()}
	m.SetReportSender(rec.send)
	args := defaultEventArgs()
	attr := mkPath(1, 0x0006, 0x0000)
	args.AttributePaths = []im.ConcreteAttributePath{attr}
	if _, err := m.Subscribe(args); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	fire(mkEventFiring(1, 0x003B, 0x01, im.EventPriorityInfo))
	m.OnAttributeChanged(attr)
	m.FlushFabric(context.Background(), args.FabricIndex, 0)
	got := rec.take()
	if len(got) != 1 {
		t.Fatalf("flush reports = %+v, want one", got)
	}
	if r := got[0].report; !r.Flush || r.KeepAlive || len(r.Events) != 1 || len(r.Paths) != 1 {
		t.Fatalf("flush report = %+v, want Flush with the event and the path", r)
	}
}

func TestDeprecatedHooksWinOverTheReportSender(t *testing.T) {
	t.Parallel()
	var legacy []im.ConcreteAttributePath
	var mu sync.Mutex
	m := subscription.NewManager(subscription.Config{}, func(_ context.Context, _ *subscription.Subscription, paths []im.ConcreteAttributePath) {
		mu.Lock()
		legacy = append(legacy, paths...)
		mu.Unlock()
	}, nil)
	rec := &recordingSender{t0: time.Now()}
	m.SetReportSender(rec.send)
	sub, err := m.Subscribe(defaultArgs())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	sub.TouchLastReport(rec.t0)
	m.OnAttributeChanged(mkPath(1, 0x0006, 0x0000))
	rec.tick(m, 2*time.Second)
	if got := rec.take(); len(got) != 0 {
		t.Fatalf("the sender got %+v; a manager with a deprecated hook reports through it", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(legacy) != 1 {
		t.Fatalf("deprecated Reporter got %v, want the changed path", legacy)
	}
}

func TestDetachedSenderDrainsNothing(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelError}))
	m := subscription.NewManager(subscription.Config{}, nil, logger)
	rec := &recordingSender{t0: time.Now()}
	m.SetReportSender(rec.send)
	sub, err := m.Subscribe(defaultArgs())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	sub.TouchLastReport(rec.t0)
	attr := mkPath(1, 0x0006, 0x0000)

	m.SetReportSender(nil)
	m.OnAttributeChanged(attr)
	rec.tick(m, 2*time.Second)
	rec.tick(m, 3*time.Second)
	m.FlushFabric(context.Background(), 1, 0)
	if n := strings.Count(logs.String(), "no ReportSender"); n != 1 {
		t.Fatalf("the missing sender was logged %d times, want once:\n%s", n, logs.String())
	}

	m.SetReportSender(rec.send)
	rec.tick(m, 4*time.Second)
	got := rec.take()
	if len(got) != 1 || len(got[0].report.Paths) != 1 || got[0].report.Paths[0] != attr {
		t.Fatalf("reports = %+v, want the change held while no sender was set", got)
	}
}

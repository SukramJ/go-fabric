// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package subscription

import (
	"context"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/im"
)

// slowReports is a manager whose reporter records the engine time of each
// report and holds the reports whose index is in held until released —
// matter.js ServerSubscriptionTest.ts's subscribeWithSlowReports
// (:732-842 at 862ad75f), whose mocked report exchanges are acknowledged
// when the test releases them. The engine is driven by tick with explicit
// times, which returns while a held report is still in flight.
type slowReports struct {
	m     *Manager
	sub   *Subscription
	path  im.ConcreteAttributePath
	start time.Time

	mu       sync.Mutex
	now      time.Time
	sends    []time.Time
	keepAliv []bool
	release  map[int]chan struct{}
	done     map[int]chan struct{}
}

func newSlowReports(t *testing.T, sendInterval time.Duration, held ...int) *slowReports {
	t.Helper()
	r := &slowReports{
		path:    im.ConcreteAttributePath{Endpoint: 0, Cluster: 0x0028, Attribute: 0x0000, HasEndpoint: true, HasCluster: true, HasAttribute: true},
		start:   time.Unix(1_000_000, 0),
		release: map[int]chan struct{}{},
		done:    map[int]chan struct{}{},
	}
	for _, i := range held {
		r.release[i] = make(chan struct{})
	}
	r.m = NewManager(Config{}, func(_ context.Context, _ *Subscription, paths []im.ConcreteAttributePath) {
		r.mu.Lock()
		i := len(r.sends)
		r.sends = append(r.sends, r.now)
		r.keepAliv = append(r.keepAliv, paths == nil)
		rel, d := r.release[i], make(chan struct{})
		r.done[i] = d
		r.mu.Unlock()
		if rel != nil {
			<-rel
		}
		close(d)
	}, nil)
	sub, err := r.m.Subscribe(SubscribeArgs{FabricIndex: 1, PeerNodeID: 1, SessionID: 1, MaxIntervalCeiling: 10, AttributePaths: []im.ConcreteAttributePath{r.path}})
	if err != nil {
		t.Fatal(err)
	}
	sub.mu.Lock()
	sub.sendIntervalOverride = sendInterval
	sub.mu.Unlock()
	sub.EndPriming(r.start)
	r.sub = sub
	t.Cleanup(func() {
		// A failed case leaves held reports in flight: release them so
		// Stop does not wait on them.
		r.mu.Lock()
		for i, ch := range r.release {
			select {
			case <-ch:
			default:
				close(ch)
			}
			delete(r.release, i)
		}
		r.mu.Unlock()
		r.m.Stop()
	})
	return r
}

// at ticks the engine at start+ms.
func (r *slowReports) at(ms int) {
	now := r.start.Add(time.Duration(ms) * time.Millisecond)
	r.mu.Lock()
	r.now = now
	r.mu.Unlock()
	if r.sub.reporting.Load() {
		r.m.tick(context.Background(), now) // skips the subscription in flight
		return
	}
	r.m.tick(context.Background(), now)
	// A report the tick started reaches the reporter on its own goroutine:
	// once the case's synctest bubble settles it did (a held one stays in
	// flight) or finished.
	synctest.Wait()
}

// change reports a change of the selected attribute; unselected one of an
// attribute the subscription does not select.
func (r *slowReports) change()     { r.m.OnAttributeChanged(r.path) }
func (r *slowReports) unselected() { p := r.path; p.Attribute = 0x0005; r.m.OnAttributeChanged(p) }

// releaseReport lets report i complete and waits for it.
func (r *slowReports) releaseReport(t *testing.T, i int) {
	t.Helper()
	r.mu.Lock()
	close(r.release[i])
	delete(r.release, i)
	d := r.done[i]
	r.mu.Unlock()
	if d == nil {
		t.Fatalf("report %d was never sent", i)
	}
	<-d
	// The reporting flag drops after the reporter returned: settle the
	// bubble.
	synctest.Wait()
}

func (r *slowReports) sent() []time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]time.Time(nil), r.sends...)
}

// TestSlowReportParityMatterJS ports the cases of matter.js
// packages/node/test/node/ServerSubscriptionTest.ts at 862ad75f in which a
// report is still being sent when the next falls due.
func TestSlowReportParityMatterJS(t *testing.T) {
	t.Parallel()
	const sendInterval = 8 * time.Second

	// ServerSubscriptionTest.ts:557-637: report A in flight, a change
	// queues B; a deferral of 5 s requested meanwhile holds B until it
	// ends, and nothing follows it within a second.
	t.Run("holds a report queued behind a report being sent until its deferral ends", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			r := newSlowReports(t, sendInterval, 0)
			r.change()
			r.at(0)
			r.change()
			r.at(100)
			r.at(200)
			if n := len(r.sent()); n != 1 {
				t.Fatalf("%d reports with A in flight, want 1", n)
			}
			deferredAt := r.start.Add(200 * time.Millisecond)
			r.sub.DeferReports(deferredAt, 5*time.Second)
			r.releaseReport(t, 0)
			r.at(300)
			if n := len(r.sent()); n != 1 {
				t.Fatalf("%d reports during the deferral, want 1", n)
			}
			r.at(5300)
			sends := r.sent()
			if len(sends) != 2 || sends[1].Sub(deferredAt) < 5*time.Second {
				t.Fatalf("reports at %v, want B once the deferral ended", sends)
			}
			r.at(6300)
			if n := len(r.sent()); n != 2 {
				t.Fatalf("%d reports a second later, want 2", n)
			}
		})
	})

	// reportQueuedBehindSlowReport (:851-863): A in flight from 0 to 5 s,
	// a change at 1 s queues B, which goes out at 5 s.
	queuedBehindSlow := func(t *testing.T, held ...int) *slowReports {
		t.Helper()
		r := newSlowReports(t, sendInterval, held...)
		r.change()
		r.at(0)
		r.at(1000)
		r.change()
		r.at(1100)
		if n := len(r.sent()); n != 1 {
			t.Fatalf("%d reports with A in flight, want 1", n)
		}
		r.at(5000)
		r.releaseReport(t, 0)
		r.at(5000)
		if n := len(r.sent()); n != 2 {
			t.Fatalf("%d reports after A completed, want 2", n)
		}
		return r
	}

	// :639-725: the keep-alive that falls due behind B goes out within the
	// send interval after B although a deferral was requested.
	t.Run("sends the keep-alive queued behind a slow report within the send interval despite a deferral", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			r := queuedBehindSlow(t, 0, 1)
			bSentAt := r.sent()[1]
			r.at(9200)
			r.sub.DeferReports(r.start.Add(9200*time.Millisecond), 10*time.Second)
			r.at(9500)
			r.releaseReport(t, 1)
			r.at(int(bSentAt.Add(sendInterval).Sub(r.start)/time.Millisecond) + 1)
			sends := r.sent()
			if len(sends) != 3 || sends[2].Sub(bSentAt) > sendInterval+time.Millisecond {
				t.Fatalf("reports at %v, want the keep-alive within %v of B", sends, sendInterval)
			}
		})
	})

	// :865-878: the keep-alive that fell due while B was in flight goes
	// out once B completes, within the send interval after B.
	t.Run("sends the keep-alive that fell due while a report was in flight when that report completes", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			r := queuedBehindSlow(t, 0, 1)
			bSentAt := r.sent()[1]
			r.at(9500)
			if n := len(r.sent()); n != 2 {
				t.Fatalf("%d reports with B in flight, want 2", n)
			}
			r.releaseReport(t, 1)
			for ms := 9600; ms <= 20_000; ms += 100 {
				r.at(ms)
			}
			sends := r.sent()
			if len(sends) < 3 || sends[2].Sub(bSentAt) > sendInterval {
				t.Fatalf("reports at %v, want a third within %v of B", sends, sendInterval)
			}
		})
	})

	// :882-898: a pass queued behind A with only a change the subscription
	// does not select sends nothing.
	t.Run("sends nothing for a queued pass with only unselected changes when no keep-alive is owed", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			r := newSlowReports(t, sendInterval, 0)
			r.change()
			r.at(0)
			r.at(1000)
			r.unselected()
			r.at(5000)
			r.releaseReport(t, 0)
			r.at(5500)
			if n := len(r.sent()); n != 1 {
				t.Fatalf("%d reports, want 1", n)
			}
		})
	})

	// :900-923: once the owed keep-alive went out, a later pass with only
	// an unselected change behind report D sends nothing either.
	t.Run("sends nothing for a later queued pass with only unselected changes once the owed keep-alive went out", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			r := queuedBehindSlow(t, 0, 1, 3)
			r.at(9500)
			r.releaseReport(t, 1)
			r.at(int(r.sent()[1].Add(sendInterval).Sub(r.start) / time.Millisecond))
			if n := len(r.sent()); n != 3 {
				t.Fatalf("%d reports after the keep-alive, want 3", n)
			}
			r.change()
			r.at(int(r.sent()[2].Sub(r.start)/time.Millisecond) + 200)
			if n := len(r.sent()); n != 4 {
				t.Fatalf("%d reports after the change, want 4", n)
			}
			r.unselected()
			r.at(int(r.sent()[3].Sub(r.start)/time.Millisecond) + 1000)
			r.releaseReport(t, 3)
			r.at(int(r.sent()[3].Sub(r.start)/time.Millisecond) + 1500)
			if n := len(r.sent()); n != 4 {
				t.Fatalf("%d reports, want 4", n)
			}
		})
	})

	// :926-938: a closed subscription does not send the keep-alive that
	// fell due while B was in flight.
	t.Run("does not send the keep-alive that fell due while a report was in flight once closed", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			r := queuedBehindSlow(t, 0, 1)
			r.at(9200)
			if err := r.m.Close(r.sub.ID); err != nil {
				t.Fatal(err)
			}
			r.releaseReport(t, 1)
			for ms := 9300; ms <= 30_000; ms += 1000 {
				r.at(ms)
			}
			if n := len(r.sent()); n != 2 {
				t.Fatalf("%d reports, want 2", n)
			}
		})
	})
}

// TestSessionShutdownParityMatterJS ports ServerSubscriptionTest.ts:247-263
// at 862ad75f ("keeps a subscription that joins a session closing for
// shutdown"): a subscription closed with its session for shutdown is not
// terminated, and one that joins the session afterwards is not terminated
// by the session's later close — Go never terminates on a session close
// (Manager.CloseSession), matter.js remembers the shutdown on the session.
func TestSessionShutdownParityMatterJS(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		m := NewManager(Config{}, nil, nil)
		terminated := map[uint32]bool{}
		var mu sync.Mutex
		m.SetOnSubscriptionTerminated(func(id uint32, _ uint8) { mu.Lock(); terminated[id] = true; mu.Unlock() })
		args := SubscribeArgs{FabricIndex: 1, PeerNodeID: 1, SessionID: 7, MaxIntervalCeiling: 60, KeepSubscriptions: true}
		first, err := m.Subscribe(args)
		if err != nil {
			t.Fatal(err)
		}
		m.closeSession(args.SessionID, false) // closeSubscriptionsForShutdown
		late, err := m.Subscribe(args)
		if err != nil {
			t.Fatal(err)
		}
		m.CloseSession(args.SessionID)
		mu.Lock()
		defer mu.Unlock()
		if terminated[first.ID] || terminated[late.ID] {
			t.Fatalf("terminated %v, want neither", terminated)
		}
	})
}

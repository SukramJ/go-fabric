// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package softwareupdate

import (
	"errors"
	"sync"
	"testing"
	"time"

	mattercore "github.com/SukramJ/go-fabric/cluster/core"
)

// eventLog collects what Manager.Observe delivers.
type eventLog struct {
	mu     sync.Mutex
	events []UpdateEvent
}

func (l *eventLog) add(ev UpdateEvent) {
	l.mu.Lock()
	l.events = append(l.events, ev)
	l.mu.Unlock()
}

// await waits until n events arrived and returns them.
func (l *eventLog) await(t *testing.T, n int) []UpdateEvent {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		l.mu.Lock()
		got := append([]UpdateEvent(nil), l.events...)
		l.mu.Unlock()
		if len(got) >= n || time.Now().After(deadline) {
			if len(got) != n {
				t.Fatalf("%d events %v, want %d", len(got), kinds(got), n)
			}
			return got
		}
		time.Sleep(time.Millisecond)
	}
}

func kinds(evs []UpdateEvent) []string {
	out := make([]string, len(evs))
	for i := range evs {
		out[i] = evs[i].Kind.String()
	}
	return out
}

func observe(t *testing.T, m *Manager) *eventLog {
	t.Helper()
	l := &eventLog{}
	t.Cleanup(m.Observe(l.add))
	return l
}

// TestObserveAppliedUpdate: an update the provider reports Applying and
// then Done is reported applying once — a retransmitted ApplyUpdateRequest
// reports Applying again (SoftwareUpdateManager.ts:1312-1318 at 862ad75f)
// — and then done, both with the queued target.
func TestObserveAppliedUpdate(t *testing.T) {
	t.Parallel()
	r := newManagerRig(t, nil)
	l := observe(t, r.m)
	r.queue(2, nil)
	v := uint32(2)
	r.m.OnStatusChange(testPeer, mattercore.OTAUpdateApplying, &v, 0)
	r.m.OnStatusChange(testPeer, mattercore.OTAUpdateApplying, &v, 0)
	r.m.OnStatusChange(testPeer, mattercore.OTAUpdateDone, &v, 0)
	evs := l.await(t, 2)
	if evs[0].Kind != UpdateApplying || evs[1].Kind != UpdateDone {
		t.Fatalf("events %v, want applying, done", kinds(evs))
	}
	for _, e := range evs {
		if e.Peer != testPeer || e.Target.TargetSoftwareVersion != 2 || e.Err != nil {
			t.Errorf("event %+v", e)
		}
	}
}

// TestObserveRemovedConsent: removing the consent of a queued update
// reports the consent gone, then the update failed as cancelled
// (removeConsent → #cancelUpdate, :1217-1232, :1091-1123).
func TestObserveRemovedConsent(t *testing.T) {
	t.Parallel()
	r := newManagerRig(t, nil)
	l := observe(t, r.m)
	r.queue(2, nil)
	r.m.RemoveConsent(testPeer, 0)
	evs := l.await(t, 2)
	if evs[0].Kind != UpdateConsentChanged || evs[0].Consented || evs[0].Target.TargetSoftwareVersion != 2 {
		t.Errorf("first event %+v, want the consent removed", evs[0])
	}
	if evs[1].Kind != UpdateFailed || !errors.Is(evs[1].Err, ErrUpdateCancelled) || evs[1].Peer != testPeer {
		t.Errorf("second event %+v, want failed, cancelled", evs[1])
	}
	// No consent left: a second removal moves nothing.
	r.m.RemoveConsent(testPeer, 0)
	time.Sleep(10 * time.Millisecond)
	l.await(t, 2)
}

// TestObserveFailedApply: a node that restarts below the target after
// Applying fails the update (#failAppliedUpdate, :875-880).
func TestObserveFailedApply(t *testing.T) {
	t.Parallel()
	r := newManagerRig(t, nil)
	l := observe(t, r.m)
	r.queue(2, statusPtr(mattercore.OTAUpdateApplying))
	r.m.onSoftwareVersionChanged(testPeer, 1, true)
	evs := l.await(t, 1)
	if evs[0].Kind != UpdateFailed || !errors.Is(evs[0].Err, ErrNotApplied) || evs[0].Target.TargetSoftwareVersion != 2 {
		t.Fatalf("event %+v, want failed, not applied", evs[0])
	}
}

// TestObserveCheckFinished: a check ends with check_finished, the nodes
// it found an update for (none here) and no error.
func TestObserveCheckFinished(t *testing.T) {
	t.Parallel()
	r := newManagerRig(t, nil)
	l := observe(t, r.m)
	if _, err := r.m.QueryUpdates(t.Context(), nil, true); err != nil {
		t.Fatal(err)
	}
	evs := l.await(t, 1)
	if evs[0].Kind != UpdateCheckFinished || len(evs[0].Updates) != 0 || evs[0].Err != nil || evs[0].Peer != (Peer{}) {
		t.Fatalf("event %+v", evs[0])
	}
}

// TestObserveUnsubscribe: unsubscribe is idempotent and stops delivery;
// an observer may call back into the manager; Close delivers nothing
// after it.
func TestObserveUnsubscribe(t *testing.T) {
	t.Parallel()
	r := newManagerRig(t, nil)
	kept := &eventLog{}
	var queued int
	stopKept := r.m.Observe(func(ev UpdateEvent) {
		queued = len(r.m.QueuedUpdates()) // no deadlock: delivery holds no manager lock
		kept.add(ev)
	})
	gone := &eventLog{}
	unsubscribe := r.m.Observe(gone.add)
	unsubscribe()
	unsubscribe()
	r.queue(2, nil)
	r.m.RemoveConsent(testPeer, 0)
	kept.await(t, 2)
	if queued != 0 {
		t.Errorf("observer saw %d queued updates after the cancel", queued)
	}
	gone.await(t, 0)
	stopKept()
	r.m.Close()
	r.m.emit(UpdateEvent{Kind: UpdateDone})
	time.Sleep(10 * time.Millisecond)
	kept.await(t, 2)
}

// TestUpdateEventKindNames: the kinds' names, as a UI shows them.
func TestUpdateEventKindNames(t *testing.T) {
	t.Parallel()
	want := []string{"unknown", "available", "consent_changed", "check_finished", "applying", "done", "failed", "unknown"}
	for k := range want {
		if got := UpdateEventKind(k).String(); got != want[k] {
			t.Errorf("kind %d: %q, want %q", k, got, want[k])
		}
	}
}

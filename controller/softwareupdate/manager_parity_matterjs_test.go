// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package softwareupdate

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bdx/otaimage"
	"github.com/SukramJ/go-fabric/clock/clocktest"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/controller/client"
)

// The manager's queue cases of matter.js's OtaTest
// (packages/node/test/behaviors/ota/OtaTest.ts at 862ad75f) that set the
// queue entry's state directly (su.internal.updateQueue[0]) and feed the
// manager its inputs (onOtaStatusChange, the startUp, the resubscription)
// rather than drive a device to them: the same here, on the manager's
// own fields. The cases that run a device are the controller's
// (controller/ota_parity_matterjs_test.go).

var testPeer = Peer{FabricIndex: 1, NodeID: 0x11}

type managerRig struct {
	m     *Manager
	clock *clocktest.Fake

	mu     sync.Mutex
	failed []Peer
	done   []Peer
}

func newManagerRig(t *testing.T, edit func(*Config)) *managerRig {
	t.Helper()
	r := &managerRig{clock: clocktest.New(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))}
	cfg := Config{
		Registry: &client.Registry{}, Images: NewImages(ImagesConfig{}), Clock: r.clock,
		Logger:         slog.New(slog.DiscardHandler),
		OnUpdateFailed: func(p Peer) { r.mu.Lock(); r.failed = append(r.failed, p); r.mu.Unlock() },
		OnUpdateDone:   func(p Peer) { r.mu.Lock(); r.done = append(r.done, p); r.mu.Unlock() },
	}
	if edit != nil {
		edit(&cfg)
	}
	m, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	// The manager online without its timers (#nodeOnline's suppressUpdates
	// lifted); no node to announce to.
	m.mu.Lock()
	m.suppress = false
	m.mu.Unlock()
	t.Cleanup(m.Close)
	r.m = m
	return r
}

// queue adds the consent and the queued entry addUpdateConsent leaves for
// an applicable node (:1153-1196), in status at the current time.
func (r *managerRig) queue(target uint32, status *Status) *queueEntry {
	t := Target{VendorID: 0xFFF1, ProductID: 0x8001, TargetSoftwareVersion: target}
	e := &queueEntry{consent: consent{Target: t, peer: testPeer}, endpoint: 0}
	if status != nil {
		s := *status
		e.status = &s
		e.started = r.clock.Now()
	}
	r.m.mu.Lock()
	r.m.consents = append(r.m.consents, consent{Target: t, peer: testPeer})
	r.m.queue = append(r.m.queue, e)
	r.m.mu.Unlock()
	return e
}

func (r *managerRig) counts() (failed, done int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.failed), len(r.done)
}

func statusPtr(s Status) *Status { return &s }

// TestManagerCancelledResetsForRetryParity: a Cancelled status (a BDX
// interruption) resets the entry for a retry and reports no failure —
// OtaTest.ts:1349-1401, onOtaStatusChange (SoftwareUpdateManager.ts:1253-1321).
func TestManagerCancelledResetsForRetryParity(t *testing.T) {
	t.Parallel()
	r := newManagerRig(t, nil)
	r.queue(2, statusPtr(mattercore.OTAUpdateDownloading))
	r.m.OnStatusChange(testPeer, mattercore.OTAUpdateCancelled, nil, 0)
	q := r.m.QueuedUpdates()
	if len(q) != 1 || q[0].Status != PendingQueued || q[0].LastProgressStatus == nil || *q[0].LastProgressStatus != mattercore.OTAUpdateUnknown {
		t.Fatalf("queue %+v, want one queued entry with status Unknown", q)
	}
	if failed, _ := r.counts(); failed != 0 {
		t.Errorf("%d updateFailed, want 0", failed)
	}
}

// TestManagerStalledAfterApplyDelayParity: an Applying update counts as
// in progress while the device waits out its apply delay and is given up
// only past the delay and the progress timeout — OtaTest.ts:1660-1691,
// queuedUpdates (:293-320).
func TestManagerStalledAfterApplyDelayParity(t *testing.T) {
	t.Parallel()
	r := newManagerRig(t, nil)
	r.queue(2, nil)
	v := uint32(2)
	r.m.OnStatusChange(testPeer, mattercore.OTAUpdateApplying, &v, 30*time.Minute)
	statusOf := func() PendingStatus {
		q := r.m.QueuedUpdates()
		if len(q) == 0 {
			return ""
		}
		return q[0].Status
	}
	r.clock.Advance(40 * time.Minute)
	if s := statusOf(); s != PendingInProgress {
		t.Fatalf("after 40 min %q, want in-progress", s)
	}
	r.clock.Advance(10 * time.Minute)
	if s := statusOf(); s != PendingStalled && s != PendingQueued {
		t.Fatalf("after 50 min %q, want stalled or queued", s)
	}
}

// TestManagerApplyFailureOnStartUpParity: a startUp with the previous
// version after Applying fails the update and drops its entry —
// OtaTest.ts:1693-1752, #onSoftwareVersionChanged (:778-842).
func TestManagerApplyFailureOnStartUpParity(t *testing.T) {
	t.Parallel()
	r := newManagerRig(t, nil)
	r.queue(2, statusPtr(mattercore.OTAUpdateApplying))
	r.m.onSoftwareVersionChanged(testPeer, 1, true)
	if failed, _ := r.counts(); failed != 1 {
		t.Fatalf("%d updateFailed, want 1", failed)
	}
	if q := r.m.QueuedUpdates(); len(q) != 0 {
		t.Errorf("queue %+v, want empty", q)
	}
}

// TestManagerSuppressNextStartUpParity: the startUp on the session the
// provider took the restart's QueryImage on is suppressed; one on another
// session is not; the suppression is consumed either way —
// OtaTest.ts:2593-2682 and :2684-2770, suppressNextStartUp (:1239) and
// #onSoftwareVersionChanged (:778-800).
func TestManagerSuppressNextStartUpParity(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name              string
		status            Status
		session, suppress uint16
		wantFailed        int
	}{
		{"matching session suppresses", mattercore.OTAUpdateWaitForApply, 42, 42, 0},
		{"different session does not suppress", mattercore.OTAUpdateApplying, 77, 999, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newManagerRig(t, func(c *Config) {
				c.SessionID = func(p Peer) (uint16, bool) { return tc.session, p == testPeer }
			})
			r.queue(2, statusPtr(tc.status))
			r.m.SuppressNextStartUp(testPeer, tc.suppress)
			r.m.onSoftwareVersionChanged(testPeer, 1, true)
			if failed, _ := r.counts(); failed != tc.wantFailed {
				t.Errorf("%d updateFailed, want %d", failed, tc.wantFailed)
			}
			r.m.mu.Lock()
			_, pending := r.m.startUpSuppress[testPeer]
			r.m.mu.Unlock()
			if pending {
				t.Error("the suppression was not consumed")
			}
		})
	}
}

// TestManagerAppliedUpdateAfterResubscriptionParity: an Applying update
// whose node reports, over a subscription established after its restart
// deadline, a version below the target failed; one at or above it, one
// resubscribed before the deadline or during its apply delay, and one
// waiting for its apply are kept — OtaTest.ts:1754-1900 (the outcome
// half), #onSubscribedAfterApply (:848-870). The armer's half of these
// cases (that the resubscription is forced past the deadline) is
// RebootResubscribeArmerTest's (client/peer).
func TestManagerAppliedUpdateAfterResubscriptionParity(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		status     Status
		target     uint32
		applyDelay time.Duration
		after      time.Duration
		wantFailed int
	}{
		{"fails with the previous version (apply delay none)", mattercore.OTAUpdateApplying, 2, 0, 4 * time.Minute, 1},
		{"fails with the previous version (apply delay 2 min)", mattercore.OTAUpdateApplying, 2, 2 * time.Minute, 6 * time.Minute, 1},
		{"keeps at or above the target version", mattercore.OTAUpdateApplying, 0, 0, 4 * time.Minute, 0},
		{"keeps before its restart deadline", mattercore.OTAUpdateApplying, 2, 0, 0, 0},
		{"keeps during its apply delay", mattercore.OTAUpdateApplying, 2, 30 * time.Minute, 4 * time.Minute, 0},
		{"keeps an update that waits for its apply", mattercore.OTAUpdateWaitForApply, 2, 0, 6 * time.Minute, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newManagerRig(t, nil)
			r.queue(tc.target, nil)
			v := tc.target
			r.m.OnStatusChange(testPeer, tc.status, &v, tc.applyDelay)
			r.clock.Advance(tc.after)
			r.m.subscribedAfterApply(testPeer, 1)
			failed, _ := r.counts()
			if failed != tc.wantFailed {
				t.Errorf("%d updateFailed, want %d", failed, tc.wantFailed)
			}
			if q := r.m.QueuedUpdates(); len(q) != 1-tc.wantFailed {
				t.Errorf("queue %+v, want %d entries", q, 1-tc.wantFailed)
			}
		})
	}
}

// TestManagerCleanupPurgesTestImagesParity: with test images not allowed
// the update check purges the stored test-mode image and keeps the prod
// and local ones — OtaTest.ts:2772-2817, #cleanupObsoleteUpdates
// (:483-556).
func TestManagerCleanupPurgesTestImagesParity(t *testing.T) {
	t.Parallel()
	r := newManagerRig(t, nil)
	image, err := otaimage.Create(otaimage.CreateOptions{
		VendorID: 0xFFF1, ProductID: 0x8001, SoftwareVersion: 2, SoftwareVersionString: "v2.0.0",
		Payload: bytes.Repeat([]byte{0x5A}, 1024),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, mode := range []Mode{ModeTest, ModeProd, ModeLocal} {
		if _, err := r.m.cfg.Images.Store(ctx, image, mode, nil); err != nil {
			t.Fatal(err)
		}
	}
	modes := func() map[Mode]int {
		out := map[Mode]int{}
		for _, st := range r.m.cfg.Images.Find(ctx, FindOptions{}) {
			out[st.Mode]++
		}
		return out
	}
	if got := modes(); got[ModeTest] != 1 || got[ModeProd] != 1 || got[ModeLocal] != 1 {
		t.Fatalf("stored %v, want one of each mode", got)
	}
	if err := r.m.CheckForUpdates(ctx); err != nil {
		t.Fatal(err)
	}
	if got := modes(); got[ModeTest] != 0 || got[ModeProd] != 1 || got[ModeLocal] != 1 {
		t.Errorf("after the check %v, want the test image purged", got)
	}
}

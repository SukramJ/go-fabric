// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package softwareupdate

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/bdx"
	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/peer"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
)

// The BDX-liveness and reboot-armer cases of matter.js's OtaTest
// (packages/node/test/behaviors/ota/OtaTest.ts at 862ad75f) on the
// manager rig. matter.js replaces BdxProtocol.sessionFor with a session of
// its own whose transferredBytes and dataLength the case moves
// (:1932-1942, :2000-2007); the manager's bdxSession seam is that
// replacement here. The rig queues its entry directly, so each case runs
// the queue check addUpdateConsent would trigger (#triggerQueuedUpdate)
// itself, which starts the periodic check.

// fakeTransfer is the cases' { peerAddress, transferredBytes, dataLength }.
type fakeTransfer struct {
	mu          sync.Mutex
	transferred uint64
	length      *uint64
}

func (f *fakeTransfer) Transferred() uint64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.transferred
}

func (f *fakeTransfer) Negotiated() (bdx.Result, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return bdx.Result{Transferred: f.transferred, Length: f.length}, true
}

func (f *fakeTransfer) set(v uint64) {
	f.mu.Lock()
	f.transferred = v
	f.mu.Unlock()
}

func (f *fakeTransfer) add(v uint64) {
	f.mu.Lock()
	f.transferred += v
	f.mu.Unlock()
}

// withTransfer makes f the running transfer of testPeer.
func (r *managerRig) withTransfer(f *fakeTransfer) {
	r.m.bdxSession = func(p Peer) bdxTransfer {
		if p == testPeer {
			return f
		}
		return nil
	}
}

// downloading queues an update, runs the queue check addUpdateConsent
// triggers and reports Downloading (onOtaStatusChange).
func (r *managerRig) downloading() {
	r.queue(2, nil)
	r.m.triggerQueuedUpdate()
	r.m.OnStatusChange(testPeer, mattercore.OTAUpdateDownloading, nil, 0)
}

// advance moves the clock by d in steps of the queue check's interval.
func (r *managerRig) advance(d time.Duration) {
	for ; d > 0; d -= queueCheckInterval {
		r.clock.Advance(min(d, queueCheckInterval))
	}
}

func (r *managerRig) only(t *testing.T) PendingUpdate {
	t.Helper()
	q := r.m.QueuedUpdates()
	if len(q) != 1 {
		t.Fatalf("queue %+v, want one entry", q)
	}
	return q[0]
}

func wantStatus(t *testing.T, u PendingUpdate, status PendingStatus, last Status) {
	t.Helper()
	if u.Status != status || u.LastProgressStatus == nil || *u.LastProgressStatus != last {
		t.Fatalf("entry %+v, want %s with last status %v", u, status, last)
	}
}

// messageRecorder counts the records with each message.
type messageRecorder struct {
	mu   sync.Mutex
	msgs []string
}

func (h *messageRecorder) Enabled(context.Context, slog.Level) bool { return true }
func (h *messageRecorder) WithAttrs([]slog.Attr) slog.Handler       { return h }
func (h *messageRecorder) WithGroup(string) slog.Handler            { return h }
func (h *messageRecorder) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	h.msgs = append(h.msgs, r.Message)
	h.mu.Unlock()
	return nil
}

func (h *messageRecorder) has(msg string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Contains(h.msgs, msg)
}

// TestManagerBDXLivenessParity ports OtaTest.ts:1902-2286 (the manager's
// half): a running transfer is the update's progress
// (#triggerQueuedUpdate, #isTransferring, queuedUpdates;
// SoftwareUpdateManager.ts:293-336, :931-1025 at 862ad75f).
func TestManagerBDXLivenessParity(t *testing.T) {
	t.Parallel()
	length := uint64(6_000_000)

	// :1902-1978
	t.Run("triggerUpdateOnNode skips announcement when BDX session is active", func(t *testing.T) {
		t.Parallel()
		rec := &messageRecorder{}
		r := newManagerRig(t, func(c *Config) { c.Logger = slog.New(rec) })
		r.withTransfer(&fakeTransfer{})
		r.m.mu.Lock()
		r.m.announcements = newAnnouncements(r.m)
		r.m.mu.Unlock()
		r.queue(2, nil)
		r.m.triggerQueuedUpdate()
		r.m.work.Wait()
		if !rec.has("ota.manager.bdx_active_skip") || rec.has("ota.manager.announcing") {
			t.Fatalf("log %v, want the announcement skipped for the running transfer", rec.msgs)
		}
		if u := r.only(t); u.Status != PendingQueued {
			t.Fatalf("entry %+v, want queued", u)
		}
	})

	// :1980-2042
	t.Run("A running BDX transfer keeps a long download out of the stalled state", func(t *testing.T) {
		t.Parallel()
		r := newManagerRig(t, nil)
		f := &fakeTransfer{length: &length}
		r.withTransfer(f)
		r.downloading()
		for range 6 {
			f.add(200_000)
			r.advance(5 * time.Minute)
		}
		u := r.only(t)
		wantStatus(t, u, PendingInProgress, mattercore.OTAUpdateDownloading)
		if u.BDXTransferred == nil || *u.BDXTransferred != 1_200_000 || u.BDXDataLength == nil || *u.BDXDataLength != length {
			t.Fatalf("transfer %v of %v, want 1200000 of 6000000", u.BDXTransferred, u.BDXDataLength)
		}
		// Blocks between two queue checks count for the reported status.
		r.m.mu.Lock()
		r.m.queue[0].started = r.clock.Now().Add(-20 * time.Minute)
		r.m.mu.Unlock()
		f.add(200_000)
		wantStatus(t, r.only(t), PendingInProgress, mattercore.OTAUpdateDownloading)
	})

	// :2044-2098
	t.Run("A retried BDX transfer counts as progress although its byte counter restarts", func(t *testing.T) {
		t.Parallel()
		r := newManagerRig(t, nil)
		f := &fakeTransfer{transferred: 900_000, length: &length}
		r.withTransfer(f)
		r.downloading()
		r.advance(5 * time.Minute)
		f.set(0)
		for range 5 {
			f.add(20_000)
			r.advance(5 * time.Minute)
		}
		wantStatus(t, r.only(t), PendingInProgress, mattercore.OTAUpdateDownloading)
	})

	// :2187-2235
	t.Run("An open BDX session that never transfers a block does not count as progress", func(t *testing.T) {
		t.Parallel()
		r := newManagerRig(t, nil)
		r.withTransfer(&fakeTransfer{})
		r.downloading()
		r.advance(22 * time.Minute)
		wantStatus(t, r.only(t), PendingQueued, mattercore.OTAUpdateUnknown)
	})

	// :2239-2286
	t.Run("A BDX transfer that stops moving data is reset for retry", func(t *testing.T) {
		t.Parallel()
		r := newManagerRig(t, nil)
		r.withTransfer(&fakeTransfer{transferred: 512_000, length: &length})
		r.downloading()
		r.advance(30 * time.Minute)
		wantStatus(t, r.only(t), PendingQueued, mattercore.OTAUpdateUnknown)
	})
}

// armerCalls counts what a real reboot armer does to testPeer.
type armerCalls struct {
	mu                sync.Mutex
	lost, unsubscribe int
}

func (a *armerCalls) context(clk *Config) peer.RebootArmerContext {
	return peer.RebootArmerContext{
		Clock: clk.Clock, Logger: slog.New(slog.DiscardHandler),
		ClosePeerSessionsBefore: func(exchange.PeerAddress, time.Time) {},
		PeerLost: func(p exchange.PeerAddress) {
			if p == testPeer {
				a.mu.Lock()
				a.lost++
				a.mu.Unlock()
			}
		},
		CloseSubscriptions: func(p exchange.PeerAddress) {
			if p == testPeer {
				a.mu.Lock()
				a.unsubscribe++
				a.mu.Unlock()
			}
		},
	}
}

func (a *armerCalls) counts() (lost, unsubscribe int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lost, a.unsubscribe
}

// TestManagerFailedApplyKeepsArmedParity ports OtaTest.ts:954-1047: the
// failed-apply detection (a startUp with the previous version while
// Applying, #onSoftwareVersionChanged) fails the update and leaves the
// reboot armer armed, so its return-timeout backstop still recovers the
// peer — matter.js's case spies on disarm; here the backstop firing after
// the detection is the proof no disarm ran (RebootResubscribeArmer.ts:
// 102-104, 174-204 at 862ad75f).
func TestManagerFailedApplyKeepsArmedParity(t *testing.T) {
	t.Parallel()
	calls := &armerCalls{}
	var armer *peer.RebootResubscribeArmer
	r := newManagerRig(t, func(c *Config) {
		armer = peer.NewRebootResubscribeArmer(calls.context(c))
		c.Armer = armer
	})
	t.Cleanup(armer.Close)
	r.queue(2, nil)
	v := uint32(2)
	r.m.OnStatusChange(testPeer, mattercore.OTAUpdateApplying, &v, 0)
	r.m.onSoftwareVersionChanged(testPeer, 1, true)
	if failed, _ := r.counts(); failed != 1 {
		t.Fatalf("%d updateFailed, want 1", failed)
	}
	r.clock.Advance(peer.RebootReturnTimeout(0) + time.Second)
	if lost, unsubscribe := calls.counts(); lost != 1 || unsubscribe != 1 {
		t.Fatalf("armer gave the peer up %d times and closed its subscriptions %d times, want 1 and 1: the detection disarmed it", lost, unsubscribe)
	}
}

// TestManagerAppliedUpdateNodeGoneParity ports OtaTest.ts:1874-1885: an
// applied update whose node goes away as its restart deadline passes —
// the armer's forced resubscription finds no node, so no subscription
// after the apply ever reports a version — is kept: neither failed nor
// dropped (#onSubscribedAfterApply runs only on a resubscription,
// SoftwareUpdateManager.ts:848-870 at 862ad75f).
func TestManagerAppliedUpdateNodeGoneParity(t *testing.T) {
	t.Parallel()
	r := newManagerRig(t, nil)
	r.queue(2, nil)
	v := uint32(2)
	r.m.OnStatusChange(testPeer, mattercore.OTAUpdateApplying, &v, 0)
	r.clock.Advance(170 * time.Second)
	r.clock.Advance(2 * time.Minute)
	if failed, _ := r.counts(); failed != 0 {
		t.Fatalf("%d updateFailed, want 0", failed)
	}
	if q := r.m.QueuedUpdates(); len(q) != 1 {
		t.Fatalf("queue %+v, want the update kept", q)
	}
}

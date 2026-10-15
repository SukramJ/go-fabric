// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package operational

// Parity tests for graceful secure-session teardown against matter.js
// HEAD:
//
//   - packages/protocol/src/session/Session.ts:248 initiateClose —
//     graceful close begins while the session keys are still live.
//   - packages/protocol/src/session/NodeSession.ts:368 close() emits
//     gracefulClose (unless the peer is lost) BEFORE state is dropped.
//   - packages/protocol/src/protocol/ExchangeManager.ts:693/:716-726 —
//     gracefulClose observer ships the CloseSession StatusReport.
//   - packages/protocol/src/session/NodeSession.ts:305 handlePeerClose
//     marks the peer lost, so a peer-initiated close never echoes a
//     CloseSession back.
//   - packages/protocol/src/protocol/DeviceAdvertiser.ts:132-149 —
//     a deleted session resumes mDNS broadcast only when the peer has
//     no remaining session (`fabric.hasSessionForPeer` guard).
//
// White-box (package operational) so the reaper sweep can be driven
// directly via reapIdle.

import (
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/secure/channel"
	"github.com/SukramJ/go-fabric/secure/sigma"
	"github.com/SukramJ/go-fabric/transport/message"
)

// closeRecorder captures graceful-close notifier, close-hook, and
// reannounce-trigger invocations for assertion.
type closeRecorder struct {
	mu         sync.Mutex
	graceful   []uint16 // sessionIDs passed to the graceful-close notifier
	encryptOK  []bool   // whether the session could still encrypt inside the notifier
	closed     []uint16 // sessionIDs passed to the onSessionClose hook
	reannounce int
}

// wire installs all three hooks on m.
func (r *closeRecorder) wire(m *Manager) {
	m.SetGracefulCloseNotifier(func(sessionID uint16, sess *channel.Session) {
		_, err := sess.Encrypt(&message.Header{}, 0, []byte{0x01})
		r.mu.Lock()
		r.graceful = append(r.graceful, sessionID)
		r.encryptOK = append(r.encryptOK, err == nil)
		r.mu.Unlock()
	})
	m.SetOnSessionClose(func(sessionID uint16) {
		r.mu.Lock()
		r.closed = append(r.closed, sessionID)
		r.mu.Unlock()
	})
	m.SetReannounceTrigger(func() {
		r.mu.Lock()
		r.reannounce++
		r.mu.Unlock()
	})
}

func (r *closeRecorder) snapshot() (graceful, closed []uint16, encryptOK []bool, reannounce int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]uint16(nil), r.graceful...),
		append([]uint16(nil), r.closed...),
		append([]bool(nil), r.encryptOK...),
		r.reannounce
}

func closeParityKeys(seed byte) sigma.SessionKeys {
	var keys sigma.SessionKeys
	for i := range keys.I2RKey {
		keys.I2RKey[i] = seed + byte(i)
	}
	for i := range keys.R2IKey {
		keys.R2IKey[i] = seed + byte(i+16)
	}
	return keys
}

// TestReapIdle_NotifiesPeerBeforeKeyZeroise verifies the reaper fires
// the graceful-close notifier while the session can still encrypt
// (matter.js NodeSession.ts:368 emits gracefulClose before state is
// dropped; ExchangeManager.ts:716-726 seals the CloseSession report under
// the live keys) and only then zeroises + cascades the close hook.
func TestReapIdle_NotifiesPeerBeforeKeyZeroise(t *testing.T) {
	t.Parallel()
	m := NewManager(newMinimalFakeStore())
	rec := &closeRecorder{}
	rec.wire(m)

	entry, err := m.OpenFromPase(0xB0B, 0xA11, 0x2222, []byte("shared-secret"))
	if err != nil {
		t.Fatalf("OpenFromPase: %v", err)
	}
	entry.MarkActiveRx()
	time.Sleep(2 * time.Millisecond)

	m.reapIdle(time.Millisecond)

	graceful, closed, encryptOK, reannounce := rec.snapshot()
	if len(graceful) != 1 || graceful[0] != entry.SessionID {
		t.Fatalf("graceful notifier calls = %v, want [%d]", graceful, entry.SessionID)
	}
	if len(encryptOK) != 1 || !encryptOK[0] {
		t.Errorf("session could not encrypt inside the notifier — keys zeroised too early")
	}
	if len(closed) != 1 || closed[0] != entry.SessionID {
		t.Errorf("onSessionClose calls = %v, want [%d]", closed, entry.SessionID)
	}
	if reannounce != 1 {
		t.Errorf("reannounce trigger fired %d times, want 1 (peer has no remaining session)", reannounce)
	}
	if n := m.Active(); n != 0 {
		t.Errorf("Active() = %d, want 0 after reap", n)
	}
	if _, err := entry.Session.Encrypt(&message.Header{}, 0, []byte{0x01}); err == nil {
		t.Error("Encrypt after reap succeeded — session keys were not zeroised")
	}
}

// TestReapIdle_ReannounceSkippedWhilePeerKeepsAnotherSession verifies
// the matter.js `hasSessionForPeer` guard (DeviceAdvertiser.ts:138):
// reaping one of two sessions to the same peer must not resume mDNS
// broadcast.
func TestReapIdle_ReannounceSkippedWhilePeerKeepsAnotherSession(t *testing.T) {
	t.Parallel()
	m := NewManager(newMinimalFakeStore())
	rec := &closeRecorder{}
	rec.wire(m)

	idle, err := m.OpenFromPase(0xB0B, 0xA11, 0x2222, []byte("shared-secret"))
	if err != nil {
		t.Fatalf("OpenFromPase (idle): %v", err)
	}
	if _, err := m.OpenFromPase(0xB0B, 0xA11, 0x2223, []byte("shared-secret")); err != nil {
		t.Fatalf("OpenFromPase (survivor): %v", err)
	}
	// Only the first session ever saw traffic; the survivor's zero
	// LastActivity exempts it from the reaper.
	idle.MarkActiveRx()
	time.Sleep(2 * time.Millisecond)

	m.reapIdle(time.Millisecond)

	graceful, _, _, reannounce := rec.snapshot()
	if len(graceful) != 1 || graceful[0] != idle.SessionID {
		t.Fatalf("graceful notifier calls = %v, want [%d]", graceful, idle.SessionID)
	}
	if reannounce != 0 {
		t.Errorf("reannounce fired %d times, want 0 — peer still holds a live session", reannounce)
	}
	if n := m.Active(); n != 1 {
		t.Errorf("Active() = %d, want 1 (survivor)", n)
	}
}

// TestPeerCapEviction_ForceClosesWithoutCloseSessionOrReannounce
// verifies the per-peer cap in OpenFromSigmaWithID: the evicted
// session is force-closed — no CloseSession to its peer, because
// matter.js skips the graceful path for the cap
// (SessionManager.ts:676-678: the target is typically the peer it can
// no longer reach, and a graceful close would hold the mutex for a
// full retransmission window) — the onSessionClose hook still fires,
// and the reannounce trigger stays silent because the peer still has
// sessions, exactly the `hasSessionForPeer` skip in
// DeviceAdvertiser.ts:138.
func TestPeerCapEviction_ForceClosesWithoutCloseSessionOrReannounce(t *testing.T) {
	t.Parallel()
	m := NewManager(newMinimalFakeStore())
	rec := &closeRecorder{}
	rec.wire(m)

	const (
		fabricIndex uint8  = 1
		localNodeID uint64 = 0xB0B
		peerNodeID  uint64 = 0xA11
	)
	firstID, err := m.AllocateID()
	if err != nil {
		t.Fatalf("AllocateID (first): %v", err)
	}
	if _, err := m.OpenFromSigmaWithID(firstID, fabricIndex, localNodeID, peerNodeID, 0x1000, nil, closeParityKeys(0)); err != nil {
		t.Fatalf("OpenFromSigmaWithID (first): %v", err)
	}
	for i := 1; i < MaxSessionsPerPeer; i++ {
		id, err := m.AllocateID()
		if err != nil {
			t.Fatalf("AllocateID (#%d): %v", i+1, err)
		}
		e, err := m.OpenFromSigmaWithID(id, fabricIndex, localNodeID, peerNodeID, 0x1000+uint16(i), nil, closeParityKeys(byte(i)))
		if err != nil {
			t.Fatalf("OpenFromSigmaWithID (#%d): %v", i+1, err)
		}
		e.MarkActiveRx()
	}
	if graceful, closed, _, _ := rec.snapshot(); len(graceful) != 0 || len(closed) != 0 {
		t.Fatalf("within the cap nothing closes: graceful=%v closed=%v", graceful, closed)
	}

	lastID, err := m.AllocateID()
	if err != nil {
		t.Fatalf("AllocateID (beyond cap): %v", err)
	}
	if _, err := m.OpenFromSigmaWithID(lastID, fabricIndex, localNodeID, peerNodeID, 0x1FFF, nil, closeParityKeys(32)); err != nil {
		t.Fatalf("OpenFromSigmaWithID (beyond cap): %v", err)
	}

	graceful, closed, _, reannounce := rec.snapshot()
	if len(graceful) != 0 {
		t.Errorf("graceful notifier calls = %v, want none — the cap force-closes without CloseSession", graceful)
	}
	if len(closed) != 1 || closed[0] != firstID {
		t.Errorf("onSessionClose calls = %v, want [%d] (least recently active)", closed, firstID)
	}
	if reannounce != 0 {
		t.Errorf("reannounce fired %d times, want 0 — the peer still holds sessions", reannounce)
	}
	if _, err := m.Get(lastID); err != nil {
		t.Errorf("session %d that prompted the check missing after eviction: %v", lastID, err)
	}
}

// TestClosePeer_NotifiesAndResumesBroadcast verifies the on-demand
// per-peer invalidation path ships the graceful close and, with the
// peer fully disconnected afterwards, resumes mDNS broadcast
// (DeviceAdvertiser.ts:132-149).
func TestClosePeer_NotifiesAndResumesBroadcast(t *testing.T) {
	t.Parallel()
	m := NewManager(newMinimalFakeStore())
	rec := &closeRecorder{}
	rec.wire(m)

	entry, err := m.OpenFromSigma(3, 0xB0B, 0xA11, closeParityKeys(7))
	if err != nil {
		t.Fatalf("OpenFromSigma: %v", err)
	}
	if n := m.ClosePeer(3, 0xA11); n != 1 {
		t.Fatalf("ClosePeer = %d, want 1", n)
	}

	graceful, closed, _, reannounce := rec.snapshot()
	if len(graceful) != 1 || graceful[0] != entry.SessionID {
		t.Fatalf("graceful notifier calls = %v, want [%d]", graceful, entry.SessionID)
	}
	if len(closed) != 1 || closed[0] != entry.SessionID {
		t.Errorf("onSessionClose calls = %v, want [%d]", closed, entry.SessionID)
	}
	if reannounce != 1 {
		t.Errorf("reannounce fired %d times, want 1", reannounce)
	}
}

// TestClose_PeerInitiated_DoesNotEchoGracefulClose verifies that
// [Manager.Close] — the inbound-CloseSession teardown path — never
// fires the graceful-close notifier: matter.js marks the peer lost in
// handlePeerClose (NodeSession.ts:305-309) so close() skips the
// gracefulClose emit, and no CloseSession is echoed back. The
// subscription cascade and the conditional broadcast resume still run.
func TestClose_PeerInitiated_DoesNotEchoGracefulClose(t *testing.T) {
	t.Parallel()
	m := NewManager(newMinimalFakeStore())
	rec := &closeRecorder{}
	rec.wire(m)

	entry, err := m.OpenFromPase(0xB0B, 0xA11, 0x2222, []byte("shared-secret"))
	if err != nil {
		t.Fatalf("OpenFromPase: %v", err)
	}
	if err := m.Close(entry.SessionID); err != nil {
		t.Fatalf("Close: %v", err)
	}

	graceful, closed, _, reannounce := rec.snapshot()
	if len(graceful) != 0 {
		t.Errorf("graceful notifier fired %v — a peer-initiated close must not echo a CloseSession", graceful)
	}
	if len(closed) != 1 || closed[0] != entry.SessionID {
		t.Errorf("onSessionClose calls = %v, want [%d]", closed, entry.SessionID)
	}
	if reannounce != 1 {
		t.Errorf("reannounce fired %d times, want 1 (peer fully gone)", reannounce)
	}
}

// TestCloseAllGraceful_NotifiesEveryPeerWithoutReannounce verifies the
// shutdown drain: every live session is notified before its keys are
// zeroised (matter.js shutdown chain Session.ts:248 →
// ExchangeManager.ts:716-726), placeholders are dropped silently, and the
// reannounce trigger stays silent — matter.js shuts the advertiser
// down before removing sessions to prevent re-announces during
// teardown (ServerNetworkRuntime.ts:427).
func TestCloseAllGraceful_NotifiesEveryPeerWithoutReannounce(t *testing.T) {
	t.Parallel()
	m := NewManager(newMinimalFakeStore())
	rec := &closeRecorder{}
	rec.wire(m)

	first, err := m.OpenFromPase(0xB0B, 0xA11, 0x2222, []byte("shared-secret"))
	if err != nil {
		t.Fatalf("OpenFromPase (first): %v", err)
	}
	second, err := m.OpenFromSigma(2, 0xB0B, 0xC0C, closeParityKeys(3))
	if err != nil {
		t.Fatalf("OpenFromSigma (second): %v", err)
	}
	// A pre-allocated placeholder must neither be notified nor counted.
	if _, err := m.AllocateID(); err != nil {
		t.Fatalf("AllocateID: %v", err)
	}

	n := m.CloseAllGraceful(time.Now().Add(time.Second))
	if n != 2 {
		t.Fatalf("CloseAllGraceful = %d, want 2 (placeholder excluded)", n)
	}

	graceful, closed, encryptOK, reannounce := rec.snapshot()
	if len(graceful) != 2 {
		t.Fatalf("graceful notifier calls = %v, want both sessions", graceful)
	}
	want := map[uint16]bool{first.SessionID: true, second.SessionID: true}
	for _, id := range graceful {
		if !want[id] {
			t.Errorf("graceful notifier saw unexpected session %d", id)
		}
	}
	for i, ok := range encryptOK {
		if !ok {
			t.Errorf("session (call %d) could not encrypt inside the notifier", i)
		}
	}
	if len(closed) != 2 {
		t.Errorf("onSessionClose calls = %v, want 2", closed)
	}
	if reannounce != 0 {
		t.Errorf("reannounce fired %d times during shutdown drain, want 0", reannounce)
	}
	if active := m.Active(); active != 0 {
		t.Errorf("Active() = %d, want 0", active)
	}
}

// TestCloseAllGraceful_DeadlinePastSkipsNotifications verifies the
// shutdown cap: an already-expired deadline suppresses every wire
// notification while local teardown still completes, so a wedged wire
// path can never stall daemon shutdown.
func TestCloseAllGraceful_DeadlinePastSkipsNotifications(t *testing.T) {
	t.Parallel()
	m := NewManager(newMinimalFakeStore())
	rec := &closeRecorder{}
	rec.wire(m)

	entry, err := m.OpenFromPase(0xB0B, 0xA11, 0x2222, []byte("shared-secret"))
	if err != nil {
		t.Fatalf("OpenFromPase: %v", err)
	}

	n := m.CloseAllGraceful(time.Now().Add(-time.Second))
	if n != 1 {
		t.Fatalf("CloseAllGraceful = %d, want 1", n)
	}

	graceful, closed, _, _ := rec.snapshot()
	if len(graceful) != 0 {
		t.Errorf("graceful notifier fired %v past the deadline, want none", graceful)
	}
	if len(closed) != 1 {
		t.Errorf("onSessionClose calls = %v, want 1 — local teardown must complete", closed)
	}
	if _, err := entry.Session.Encrypt(&message.Header{}, 0, []byte{0x01}); err == nil {
		t.Error("Encrypt after CloseAllGraceful succeeded — keys were not zeroised")
	}
}

// TestClosePeerLost_ForceClosesWithoutCloseSession verifies the peer-loss
// sweep: every session of the lost peer created before asOf is closed
// without a CloseSession on the wire (matter.js NodeSession.ts:325-328
// handlePeerLoss → initiateForceClose), the onSessionClose hook still
// fires per session, a session established after the loss was observed
// survives (SessionManager.ts:688-690), and the reannounce trigger stays
// silent while that survivor keeps the peer connected
// (DeviceAdvertiser.ts:138 hasSessionForPeer).
func TestClosePeerLost_ForceClosesWithoutCloseSession(t *testing.T) {
	t.Parallel()
	m := NewManager(newMinimalFakeStore())
	rec := &closeRecorder{}
	rec.wire(m)

	const (
		fabricIndex uint8  = 2
		localNodeID uint64 = 0xB0B
		peerNodeID  uint64 = 0xA11
	)
	first, err := m.OpenFromSigma(fabricIndex, localNodeID, peerNodeID, closeParityKeys(1))
	if err != nil {
		t.Fatalf("OpenFromSigma (first): %v", err)
	}
	second, err := m.OpenFromSigma(fabricIndex, localNodeID, peerNodeID, closeParityKeys(2))
	if err != nil {
		t.Fatalf("OpenFromSigma (second): %v", err)
	}
	other, err := m.OpenFromSigma(fabricIndex, localNodeID, 0xBEEF, closeParityKeys(3))
	if err != nil {
		t.Fatalf("OpenFromSigma (other peer): %v", err)
	}
	asOf := time.Now()
	time.Sleep(time.Millisecond)
	later, err := m.OpenFromSigma(fabricIndex, localNodeID, peerNodeID, closeParityKeys(4))
	if err != nil {
		t.Fatalf("OpenFromSigma (later): %v", err)
	}

	if n := m.ClosePeerLost(fabricIndex, peerNodeID, asOf); n != 2 {
		t.Fatalf("ClosePeerLost = %d, want 2 (the sessions held when the loss was observed)", n)
	}
	graceful, closed, _, reannounce := rec.snapshot()
	if len(graceful) != 0 {
		t.Errorf("graceful notifier calls = %v, want none — a lost peer gets no CloseSession", graceful)
	}
	if len(closed) != 2 {
		t.Errorf("onSessionClose calls = %v, want the two lost sessions", closed)
	}
	if reannounce != 0 {
		t.Errorf("reannounce fired %d times, want 0 — the later session keeps the peer connected", reannounce)
	}
	for _, id := range []uint16{first.SessionID, second.SessionID} {
		if _, err := m.Get(id); err == nil {
			t.Errorf("session %d still live after the peer was lost", id)
		}
	}
	for _, id := range []uint16{later.SessionID, other.SessionID} {
		if _, err := m.Get(id); err != nil {
			t.Errorf("session %d must survive: %v", id, err)
		}
	}

	// A zero asOf sweeps the rest; with no session left the broadcast resumes.
	if n := m.ClosePeerLost(fabricIndex, peerNodeID, time.Time{}); n != 1 {
		t.Fatalf("ClosePeerLost(zero asOf) = %d, want 1", n)
	}
	if _, _, _, reannounce := rec.snapshot(); reannounce != 1 {
		t.Errorf("reannounce fired %d times after the last session, want 1", reannounce)
	}
}

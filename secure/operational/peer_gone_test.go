// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package operational

import (
	"slices"
	"sync"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/clock/clocktest"
)

// peerGoneCall is one call of the peer-gone hook.
type peerGoneCall struct {
	fabricIndex uint8
	peerNodeID  uint64
	cause       PeerGoneCause
}

// peerGoneRecorder records the peer-gone hook's calls.
type peerGoneRecorder struct {
	mu    sync.Mutex
	calls []peerGoneCall
}

func (r *peerGoneRecorder) wire(m *Manager) {
	m.SetOnPeerGone(func(fabricIndex uint8, peerNodeID uint64, cause PeerGoneCause) {
		r.mu.Lock()
		r.calls = append(r.calls, peerGoneCall{fabricIndex, peerNodeID, cause})
		r.mu.Unlock()
	})
}

func (r *peerGoneRecorder) snapshot() []peerGoneCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]peerGoneCall(nil), r.calls...)
}

const (
	peerGoneA uint64 = 0xA0A0
	peerGoneB uint64 = 0xB0B0
)

// TestPeerGoneHookCarriesTheCause pins SetOnPeerGone (#590 item 2, owner
// decisions 611, 612): the hook fires once per (fabric, peer) pair a
// close sweep leaves without a live session, with the cause of the close
// — PeerGoneLost only for the peer-loss sweep (closePeerLost, an exchange
// this node opened went unanswered); the idle reaper, the peer's
// CloseSession and a TCP eviction ([Manager.Close]) and ClosePeer are
// PeerGoneClosed, and a fabric's removal fires nothing.
func TestPeerGoneHookCarriesTheCause(t *testing.T) {
	t.Parallel()
	t.Run("peer loss leaving no session is PeerGoneLost", func(t *testing.T) {
		t.Parallel()
		m := NewManager(nil)
		rec := &peerGoneRecorder{}
		rec.wire(m)
		sessionManagerOpen(t, m, 0x0100, peerGoneA, 0x0100, false)
		sessionManagerOpen(t, m, 0x0101, peerGoneA, 0x0101, false)
		sessionManagerOpen(t, m, 0x0200, peerGoneB, 0x0200, false)
		if n := m.ClosePeerLostBefore(sessionManagerFabric, peerGoneA, m.RegistrationCount()); n != 2 {
			t.Fatalf("closed %d, want 2", n)
		}
		want := []peerGoneCall{{sessionManagerFabric, peerGoneA, PeerGoneLost}}
		if got := rec.snapshot(); !slices.Equal(got, want) {
			t.Fatalf("hook calls %v, want %v", got, want)
		}
	})
	t.Run("peer loss that keeps a later session fires nothing", func(t *testing.T) {
		t.Parallel()
		m := NewManager(nil)
		rec := &peerGoneRecorder{}
		rec.wire(m)
		sessionManagerOpen(t, m, 0x0100, peerGoneA, 0x0100, false)
		before := m.RegistrationCount()
		sessionManagerOpen(t, m, 0x0101, peerGoneA, 0x0101, false)
		if n := m.ClosePeerLostBefore(sessionManagerFabric, peerGoneA, before); n != 1 {
			t.Fatalf("closed %d, want 1", n)
		}
		if got := rec.snapshot(); len(got) != 0 {
			t.Fatalf("hook calls %v, want none: a session registered after the loss is live", got)
		}
	})
	t.Run("Close (the peer's CloseSession, a TCP eviction) is PeerGoneClosed", func(t *testing.T) {
		t.Parallel()
		m := NewManager(nil)
		rec := &peerGoneRecorder{}
		rec.wire(m)
		e := sessionManagerOpen(t, m, 0x0100, peerGoneA, 0x0100, false)
		if err := m.Close(e.SessionID); err != nil {
			t.Fatal(err)
		}
		want := []peerGoneCall{{sessionManagerFabric, peerGoneA, PeerGoneClosed}}
		if got := rec.snapshot(); !slices.Equal(got, want) {
			t.Fatalf("hook calls %v, want %v", got, want)
		}
	})
	t.Run("ClosePeer is PeerGoneClosed", func(t *testing.T) {
		t.Parallel()
		m := NewManager(nil)
		rec := &peerGoneRecorder{}
		rec.wire(m)
		sessionManagerOpen(t, m, 0x0100, peerGoneA, 0x0100, false)
		m.ClosePeer(sessionManagerFabric, peerGoneA)
		want := []peerGoneCall{{sessionManagerFabric, peerGoneA, PeerGoneClosed}}
		if got := rec.snapshot(); !slices.Equal(got, want) {
			t.Fatalf("hook calls %v, want %v", got, want)
		}
	})
	t.Run("the idle reaper is PeerGoneClosed", func(t *testing.T) {
		t.Parallel()
		clk := clocktest.New(time.UnixMilli(1_700_000_000_000))
		m := NewManager(newMinimalFakeStore())
		m.SetClock(clk)
		rec := &peerGoneRecorder{}
		rec.wire(m)
		sessionManagerOpen(t, m, 0x0100, peerGoneA, 0x0100, false)
		clk.Advance(reaperTestIdle + time.Second)
		m.reapIdle(reaperTestIdle)
		want := []peerGoneCall{{sessionManagerFabric, peerGoneA, PeerGoneClosed}}
		if got := rec.snapshot(); !slices.Equal(got, want) {
			t.Fatalf("hook calls %v, want %v", got, want)
		}
	})
	t.Run("a fabric's removal fires nothing", func(t *testing.T) {
		t.Parallel()
		m := NewManager(nil)
		rec := &peerGoneRecorder{}
		rec.wire(m)
		sessionManagerOpen(t, m, 0x0100, peerGoneA, 0x0100, false)
		m.CloseFabric(sessionManagerFabric)
		if got := rec.snapshot(); len(got) != 0 {
			t.Fatalf("hook calls %v, want none", got)
		}
	})
	t.Run("a PASE session has no peer to report", func(t *testing.T) {
		t.Parallel()
		m := NewManager(nil)
		rec := &peerGoneRecorder{}
		rec.wire(m)
		e, err := m.OpenFromPase(0xB0B, 0, 0x2222, []byte("shared-secret"))
		if err != nil {
			t.Fatal(err)
		}
		if err := m.Close(e.SessionID); err != nil {
			t.Fatal(err)
		}
		if got := rec.snapshot(); len(got) != 0 {
			t.Fatalf("hook calls %v, want none", got)
		}
	})
}

// TestPeerGoneHookRunsOutsideTheLock: the hook may call back into the
// table — here a Close, which takes the manager's write lock — so it runs
// after the sweep released it.
func TestPeerGoneHookRunsOutsideTheLock(t *testing.T) {
	t.Parallel()
	m := NewManager(nil)
	other := sessionManagerOpen(t, m, 0x0200, peerGoneB, 0x0200, false)
	sessionManagerOpen(t, m, 0x0100, peerGoneA, 0x0100, false)
	var reentered []PeerGoneCause
	m.SetOnPeerGone(func(_ uint8, peerNodeID uint64, cause PeerGoneCause) {
		reentered = append(reentered, cause)
		if peerNodeID == peerGoneA {
			_ = m.Close(other.SessionID)
		}
	})
	m.ClosePeerLostBefore(sessionManagerFabric, peerGoneA, m.RegistrationCount())
	if len(reentered) != 2 || reentered[0] != PeerGoneLost || reentered[1] != PeerGoneClosed {
		t.Fatalf("causes %v, want [lost closed]", reentered)
	}
}

// TestPeerGoneCauseNamesItself pins the log names of the two causes and
// the fallback of a value outside them.
func TestPeerGoneCauseNamesItself(t *testing.T) {
	for _, tc := range []struct {
		cause PeerGoneCause
		want  string
	}{
		{PeerGoneClosed, "closed"},
		{PeerGoneLost, "lost"},
		{PeerGoneCause(7), "PeerGoneCause(7)"},
	} {
		if got := tc.cause.String(); got != tc.want {
			t.Errorf("PeerGoneCause(%d).String() = %q, want %q", uint8(tc.cause), got, tc.want)
		}
	}
}

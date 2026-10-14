// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package exchange

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/secure/channel"
)

// newFabricSession registers a CASE-like session with peer node 0x2222 on
// fabric index 1.
func newFabricSession(t *testing.T, m *Manager, id uint16) *Session {
	t.Helper()
	s, err := m.NewSecureSession(SecureSessionConfig{
		ID:   id,
		Addr: peerAddr,
		Channel: channel.Config{
			EncryptKey: keyI2R, DecryptKey: keyR2I,
			LocalNodeID: 0x1111, PeerNodeID: 0x2222, PeerSessionID: 0x7000 + id,
		},
		PeerParameters: PeerParameters(nil),
		FabricIndex:    1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// lossRecorder counts PeerLost calls per session.
type lossRecorder struct {
	mu   sync.Mutex
	seen map[*Session]int
}

func (r *lossRecorder) hook(s *Session, _ error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.seen == nil {
		r.seen = map[*Session]int{}
	}
	r.seen[s]++
}

func (r *lossRecorder) count(s *Session) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.seen[s]
}

// TestSessionHandlePeerLoss: the session is marked lost, the hook runs
// once, every exchange ends with the cause, and the session closes without
// a CloseSession (NodeSession.ts:311-314, :389-395).
func TestSessionHandlePeerLoss(t *testing.T) {
	t.Parallel()
	var rec lossRecorder
	m, conn, _ := newTestManager(t, Config{PeerLost: rec.hook})
	s := newFabricSession(t, m, 0x0331)
	if s.FabricIndex() != 1 {
		t.Fatalf("FabricIndex = %d", s.FabricIndex())
	}
	x, _ := m.Initiate(s, im.InteractionModelProtocolID, Options{})
	cause := errors.New("gone")
	next := make(chan error, 1)
	go func() {
		_, err := x.Next(context.Background(), ReceiveOptions{Timeout: time.Hour})
		next <- err
	}()
	s.HandlePeerLoss(cause)
	s.HandlePeerLoss(cause)
	if err := waitErr(t, next); !errors.Is(err, cause) {
		t.Fatalf("Next = %v, want the cause", err)
	}
	select {
	case <-s.Closing():
	default:
		t.Fatal("Closing not closed")
	}
	if !s.IsPeerLost() || !s.IsClosed() || rec.count(s) != 1 || m.Session(0x0331) != nil {
		t.Fatalf("lost %v closed %v hook %d", s.IsPeerLost(), s.IsClosed(), rec.count(s))
	}
	m.CloseSession(context.Background(), s)
	conn.none(t)
}

// TestManagerHandlePeerLossAsOf: only the peer's sessions created before
// asOf are lost; a zero asOf takes all (SessionManager.ts:677-696).
func TestManagerHandlePeerLossAsOf(t *testing.T) {
	t.Parallel()
	m, _, clock := newTestManager(t, Config{})
	old := newFabricSession(t, m, 0x0341)
	clock.Advance(time.Second)
	asOf := clock.Now()
	fresh := newFabricSession(t, m, 0x0342)
	other, _ := newSecurePair(t, m, 0x0343, false)
	if got := m.SessionsFor(PeerAddress{FabricIndex: 1, NodeID: 0x2222}); len(got) != 2 || got[0] != old || got[1] != fresh {
		t.Fatalf("SessionsFor = %v", got)
	}
	if !fresh.CreatedAt().Equal(asOf) || !old.CreatedAt().Before(asOf) {
		t.Fatalf("CreatedAt: old %v fresh %v asOf %v", old.CreatedAt(), fresh.CreatedAt(), asOf)
	}
	m.HandlePeerLoss(PeerAddress{FabricIndex: 1, NodeID: 0x2222}, errors.New("x"), fresh.CreatedAt())
	if !old.IsPeerLost() || fresh.IsPeerLost() || other.IsPeerLost() {
		t.Fatalf("lost: old %v fresh %v other %v", old.IsPeerLost(), fresh.IsPeerLost(), other.IsPeerLost())
	}
	m.HandlePeerLoss(PeerAddress{FabricIndex: 1, NodeID: 0x2222}, errors.New("x"), time.Time{})
	if !fresh.IsPeerLost() || other.IsPeerLost() {
		t.Fatal("a zero asOf did not take the fresh session, or took a foreign one")
	}
}

// TestExchangePeerLossClosesThePeersSessions: a send that shows the peer
// gone loses every session of the peer created before the exchange; the
// session of the failing exchange closes once that exchange does
// (ExchangeManager.ts:556-579, Session.ts:324-327, NodeSession.ts:354-362).
func TestExchangePeerLossClosesThePeersSessions(t *testing.T) {
	t.Parallel()
	var rec lossRecorder
	m, conn, clock := newTestManager(t, Config{PeerLost: rec.hook})
	sibling := newFabricSession(t, m, 0x0351)
	s := newFabricSession(t, m, 0x0352)
	clock.Advance(time.Second)
	x, _ := m.Initiate(s, im.InteractionModelProtocolID, Options{})
	clock.Advance(time.Second)
	later := newFabricSession(t, m, 0x0353)
	done := sendAsync(x, im.OpcodeReadRequest, nil, SendOptions{})
	conn.nextOn(t, x)
	clock.Advance(time.Hour)
	clock.Advance(time.Hour)
	if err := waitErr(t, done); !errors.Is(err, ErrTransient) {
		t.Fatal(err)
	}
	if !sibling.IsPeerLost() || !sibling.IsClosed() || !s.IsPeerLost() || later.IsPeerLost() {
		t.Fatalf("sibling lost %v closed %v, own lost %v, later lost %v", sibling.IsPeerLost(), sibling.IsClosed(), s.IsPeerLost(), later.IsPeerLost())
	}
	if s.IsClosed() {
		t.Fatal("the session closed before its failing exchange did")
	}
	x.Close() // the unacknowledged message keeps it open for its retransmission window
	clock.Advance(time.Hour)
	<-x.Done()
	if !s.IsClosed() || rec.count(s) != 1 || rec.count(sibling) != 1 {
		t.Fatalf("closed %v, hooks %d/%d", s.IsClosed(), rec.count(s), rec.count(sibling))
	}
}

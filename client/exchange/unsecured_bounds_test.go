// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package exchange

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/SukramJ/go-fabric/secure/sessionparams"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// TestUnauthenticatedTimingIsClamped: the intervals a peer states before
// it has authenticated — in its DNS-SD record or in a handshake message —
// time the unsecured session at most one hour each (review II, owner
// decision 45, R2-P2-alloc-02).
func TestUnauthenticatedTimingIsClamped(t *testing.T) {
	t.Parallel()
	m, _, _ := newTestManager(t, Config{})
	huge := sessionparams.Parameters{SessionIdleInterval: 0xFFFFFFFF, SessionActiveInterval: 0xFFFFFFFF, SessionActiveThreshold: 0xFFFF}
	s, err := m.NewUnsecuredSession(peerAddr, &huge)
	if err != nil {
		t.Fatal(err)
	}
	check := func(stage string) {
		t.Helper()
		p := s.Parameters()
		if p.SessionIdleInterval != sessionparams.MaxUnauthenticatedInterval || p.SessionActiveInterval != sessionparams.MaxUnauthenticatedInterval || p.SessionActiveThreshold != 0xFFFF {
			t.Fatalf("%s: intervals %d / %d / %d", stage, p.SessionIdleInterval, p.SessionActiveInterval, p.SessionActiveThreshold)
		}
	}
	check("from the DNS-SD record")
	s.SetTimingParameters(sessionparams.Parameters{SessionIdleInterval: 500})
	s.SetTimingParameters(huge)
	check("from the handshake")
}

// TestInboundUnsecuredAckWaitsAreBounded: at most 16 messages on the
// sessions peers open without authenticating wait for an acknowledgement;
// the next one ends the oldest wait, whose exchange and session then close
// (review II, R2-P2-alloc-02).
func TestInboundUnsecuredAckWaitsAreBounded(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		m, _, _ := newTestManager(t, Config{})
		for src := uint64(1); src <= inboundUnsecuredAckWaitLimit+1; src++ {
			unsecuredRequest(m, src, 1, 0x6300, mrp.SecureChannelProtocolID, mrp.SCOpcodePBKDFParamRequest, []byte{0x15, 0x18})
			synctest.Wait()
		}
		if n := countInbound(m); n != inboundUnsecuredAckWaitLimit {
			t.Fatalf("%d inbound sessions wait, want %d", n, inboundUnsecuredAckWaitLimit)
		}
		m.mu.Lock()
		waits, oldest := len(m.ackWaits), m.unsecured[1]
		m.mu.Unlock()
		if waits != inboundUnsecuredAckWaitLimit || oldest != nil {
			t.Fatalf("%d waits held, the oldest kept: %v", waits, oldest != nil)
		}
	})
}

// TestInboundUnsecuredSessionLifetime: a session a peer opened without
// authenticating closes 60 s after it was opened, whatever its handshake
// waits for (review II, R2-P2-alloc-02).
func TestInboundUnsecuredSessionLifetime(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		m, _, clock := newTestManager(t, Config{})
		release := make(chan struct{})
		defer close(release)
		h, calls := recordingHandler(release)
		m.SetProtocolHandler(mrp.SecureChannelProtocolID, h)
		unsecuredRequest(m, 0xAAAA, 1, 0x6300, mrp.SecureChannelProtocolID, mrp.SCOpcodeSigma1, []byte{0x15, 0x18})
		c := waitHandled(t, calls)
		synctest.Wait()
		clock.Advance(inboundUnsecuredLifetime - time.Millisecond)
		synctest.Wait()
		if c.x.Session().IsClosed() || countInbound(m) != 1 {
			t.Fatal("closed before its lifetime")
		}
		clock.Advance(time.Millisecond)
		synctest.Wait()
		if !c.x.Session().IsClosed() || countInbound(m) != 0 {
			t.Fatal("open after its lifetime")
		}
	})
}

// TestInboundUnsecuredSessionsAreBounded: at most 256 sessions peers
// opened without authenticating are held; the next one closes the oldest
// (review II, R2-P2-alloc-02).
func TestInboundUnsecuredSessionsAreBounded(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		m, _, _ := newTestManager(t, Config{})
		release := make(chan struct{})
		defer close(release)
		calls := make(chan *Exchange, maxInboundUnsecuredSessions+1)
		m.SetProtocolHandler(mrp.SecureChannelProtocolID, func(x *Exchange, _ *Message) {
			calls <- x
			<-release
		})
		for src := uint64(1); src <= maxInboundUnsecuredSessions+1; src++ {
			unsecuredRequest(m, src, 1, 0x6300, mrp.SecureChannelProtocolID, mrp.SCOpcodeSigma1, []byte{0x15, 0x18})
		}
		synctest.Wait()
		if n := countInbound(m); n != maxInboundUnsecuredSessions {
			t.Fatalf("%d inbound sessions, want %d", n, maxInboundUnsecuredSessions)
		}
		m.mu.Lock()
		first := m.unsecured[1]
		m.mu.Unlock()
		closed := 0
		for range maxInboundUnsecuredSessions + 1 {
			x := <-calls
			if x.Session().IsClosed() {
				closed++
				if x.Session().nodeID != 1 {
					t.Fatalf("closed the session of %#x, not the oldest", x.Session().nodeID)
				}
			}
		}
		if first != nil || closed != 1 {
			t.Fatalf("the oldest session was kept (closed %d)", closed)
		}
	})
}

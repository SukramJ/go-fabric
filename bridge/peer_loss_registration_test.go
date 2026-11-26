// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"thingfabric.dev/go-fabric/bdx"
	"thingfabric.dev/go-fabric/clock"
	"thingfabric.dev/go-fabric/clock/clocktest"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/internal/sessionseam"
	"thingfabric.dev/go-fabric/secure/channel"
	"thingfabric.dev/go-fabric/secure/operational"
	"thingfabric.dev/go-fabric/transport/mrp"
)

// frozenClock reads one instant, on the wall clock and on its monotonic
// reading; its timers are the system's. Every session and exchange stamp
// is equal, so only the registration order can tell them apart.
type frozenClock struct {
	clock.System
	at time.Time
}

func (c frozenClock) Now() time.Time { return c.at }

func (c frozenClock) Monotonic() time.Duration { return time.Hour }

// TestInitiatedPeerLossCutsByRegistrationOrder: the exchange's own session
// and one the peer established after the exchange opened carry the same
// creation instant. The loss closes the own session, registered before the
// exchange took its count, and keeps the later one — the creation time
// decides nothing. Mirrors matter.js
// packages/protocol/src/protocol/ExchangeManager.ts:623 at b25ed960 and
// packages/protocol/src/session/SessionManager.ts:812-838 at b25ed960.
func TestInitiatedPeerLossCutsByRegistrationOrder(t *testing.T) {
	t.Parallel()
	frozen := frozenClock{at: time.UnixMilli(1_700_000_000_000)}
	dev := startResumeDevice(t, newMemSubscriptionStore(), func(br *Bridge, sessions *operational.Manager) {
		br.clock = frozen
		sessions.SetClock(frozen)
		br.AttachAckTracker(mrp.NewAckTracker(0))
	})
	silent, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = silent.Close() })
	const (
		ownSID   = uint16(0x0E01)
		laterSID = uint16(0x0E02)
	)
	if _, err := sessionseam.OpenAt(dev.sessions, ownSID, true, resumeFabricIndex, resumeDeviceNode, resumeCtrlNode, 0x0F01, nil, peerLossKeys(t)); err != nil {
		t.Fatal(err)
	}
	dev.br.sessionPeerAddrs.Store(ownSID, silent.LocalAddr())
	link, ok := dev.br.existingPeerSession(resumeFabricIndex, resumeCtrlNode)
	if !ok {
		t.Fatal("no session to reuse")
	}
	ex, err := dev.br.openInitiatedExchange(link)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ex.Close() }()
	if !ex.counted {
		t.Fatal("the exchange took no registration count from the operational manager")
	}
	if _, err := sessionseam.OpenAt(dev.sessions, laterSID, false, resumeFabricIndex, resumeDeviceNode, resumeCtrlNode, 0x0F02, nil, peerLossKeys(t)); err != nil {
		t.Fatal(err)
	}
	own, _ := dev.sessions.Get(ownSID)
	later, _ := dev.sessions.Get(laterSID)
	if own.CreatedAtMonotonic() != ex.createdAtMono || later.CreatedAtMonotonic() != ex.createdAtMono {
		t.Fatal("the stamps differ")
	}

	dev.br.handleInitiatedPeerLoss(ownSID, ex.exchangeID)

	if _, err := dev.sessions.Get(ownSID); !errors.Is(err, operational.ErrSessionNotFound) {
		t.Errorf("the exchange's own session after the loss: %v, want ErrSessionNotFound", err)
	}
	if _, err := dev.sessions.Get(laterSID); err != nil {
		t.Errorf("a session registered after the exchange opened must survive: %v", err)
	}
}

// orderedRegistry is a [SessionRegistry] with both peer-loss capabilities;
// it records which one a loss used.
type orderedRegistry struct {
	lostPeerRegistry
	count  uint64
	before []uint64
}

func (r *orderedRegistry) RegistrationCount() uint64 { return r.count }

func (r *orderedRegistry) ClosePeerLostBefore(_ uint8, _, registeredBefore uint64) int {
	r.before = append(r.before, registeredBefore)
	return 1
}

// closeOnlyRegistry has neither capability.
type closeOnlyRegistry struct{ closed []uint16 }

func (r *closeOnlyRegistry) Close(id uint16) error { r.closed = append(r.closed, id); return nil }

func (r *closeOnlyRegistry) CloseAllGraceful(time.Time) int { return 0 }

// TestPeerLossCapabilityOrder: an exchange takes the registration count of
// a registry that gives one; the loss then cuts by it. A registry with only
// the deprecated time form still gets it, through the window, and one with
// neither closes the session alone (coordinator decision C-2).
func TestPeerLossCapabilityOrder(t *testing.T) {
	t.Parallel()
	const sid = uint16(0x0E10)
	key := make([]byte, 16)
	sess, err := channel.New(channel.Config{EncryptKey: key, DecryptKey: key, PeerNodeID: 0x1234, InitialCounter: 1})
	if err != nil {
		t.Fatal(err)
	}
	open := func(t *testing.T, reg SessionRegistry) (*Bridge, *initiatedSecureExchange) {
		t.Helper()
		b := &Bridge{}
		b.sessions = lostPeerLookup{sid: sid, sess: sess, fabric: 1}
		b.sessionRegistry = reg
		ex, err := b.openInitiatedExchange(peerLink{sessionID: sid})
		if err != nil {
			t.Fatal(err)
		}
		return b, ex
	}
	t.Run("registration order first", func(t *testing.T) {
		t.Parallel()
		reg := &orderedRegistry{count: 7}
		b, ex := open(t, reg)
		reg.count = 9 // a session registered since
		b.closeLostPeer(ex, "a test exchange")
		if len(reg.before) != 1 || reg.before[0] != 7 || len(reg.perPeer) != 0 || len(reg.closed) != 0 {
			t.Fatalf("before %v, since %v, closed %v; want one cut at 7", reg.before, reg.perPeer, reg.closed)
		}
	})
	t.Run("the deprecated time form for a registry without the order", func(t *testing.T) {
		t.Parallel()
		reg := &lostPeerRegistry{}
		b, ex := open(t, reg)
		if ex.counted {
			t.Fatal("counted without the capability")
		}
		b.closeLostPeer(ex, "a test exchange")
		if len(reg.perPeer) != 1 || len(reg.closed) != 0 {
			t.Fatalf("since %v, closed %v; want one per-peer close", reg.perPeer, reg.closed)
		}
	})
	t.Run("the session alone for a registry with neither", func(t *testing.T) {
		t.Parallel()
		reg := &closeOnlyRegistry{}
		b, ex := open(t, reg)
		b.closeLostPeer(ex, "a test exchange")
		if len(reg.closed) != 1 || reg.closed[0] != sid {
			t.Fatalf("closed %v, want %#x alone", reg.closed, sid)
		}
	})
}

// TestInitiatedExchangeReceivePeerLoss ports matter.js
// packages/protocol/test/protocol/MessageExchangeTest.ts:126-149 at
// b25ed960 (describe "peer loss declaration" / "nextMessage()") onto
// initiatedSecureExchange.Receive: a receive whose deadline passed reports
// the peer lost only while the peer sent nothing on the exchange. The
// suppressing-session cases (:151-170) are not ported: no session of the
// device side suppresses peer loss (coordinator decision C-4). An expired
// deadline is matter.js's `timeout: Millis(0)`; the cases below the two
// add what the bridge distinguishes: a cancellation is no timeout, and an
// exchange the peer opened never reports.
func TestInitiatedExchangeReceivePeerLoss(t *testing.T) {
	t.Parallel()
	const sid = uint16(0x0E20)
	key := make([]byte, 16)
	sess, err := channel.New(channel.Config{EncryptKey: key, DecryptKey: key, PeerNodeID: 0x1234, InitialCounter: 1})
	if err != nil {
		t.Fatal(err)
	}
	open := func(t *testing.T) (*orderedRegistry, *initiatedSecureExchange) {
		t.Helper()
		reg := &orderedRegistry{count: 1}
		b := &Bridge{}
		b.sessions = lostPeerLookup{sid: sid, sess: sess, fabric: 1}
		b.sessionRegistry = reg
		ex, err := b.openInitiatedExchange(peerLink{sessionID: sid})
		if err != nil {
			t.Fatal(err)
		}
		return reg, ex
	}
	expired := func(t *testing.T) context.Context {
		t.Helper()
		ctx, cancel := context.WithDeadline(context.Background(), time.Unix(0, 0))
		t.Cleanup(cancel)
		return ctx
	}
	lost := func(reg *orderedRegistry) bool { return len(reg.before)+len(reg.perPeer)+len(reg.closed) > 0 }

	t.Run("nextMessage()/declares peer lost when exchange has received no messages", func(t *testing.T) {
		t.Parallel()
		reg, ex := open(t)
		if _, err := ex.Receive(expired(t)); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Receive = %v, want DeadlineExceeded", err)
		}
		if !lost(reg) {
			t.Fatal("peer not declared lost")
		}
	})
	t.Run("nextMessage()/does not declare peer lost when exchange already received at least one message", func(t *testing.T) {
		t.Parallel()
		reg, ex := open(t)
		ex.received.Add(1) // as noteInitiatedReceived counts it
		if !ex.enqueue(bdxTestMessage()) {
			t.Fatal("not queued")
		}
		if _, err := ex.Receive(context.Background()); err != nil {
			t.Fatal(err)
		}
		if _, err := ex.Receive(expired(t)); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Receive = %v, want DeadlineExceeded", err)
		}
		if lost(reg) {
			t.Fatal("peer declared lost")
		}
	})
	t.Run("a deadline on the injected clock declares peer lost", func(t *testing.T) {
		t.Parallel()
		// PeerSession.Invoke's wait: clock.WithTimeout on the bridge's
		// clock, here a fake one, expires with DeadlineExceeded.
		reg, ex := open(t)
		fake := clocktest.New(time.UnixMilli(1_700_000_000_000))
		ctx, cancel := clock.WithTimeout(context.Background(), fake, 3*time.Second)
		defer cancel()
		fake.Advance(3 * time.Second)
		if _, err := ex.Receive(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Receive = %v, want DeadlineExceeded", err)
		}
		if !lost(reg) {
			t.Fatal("peer not declared lost")
		}
	})
	t.Run("a cancelled receive declares nothing", func(t *testing.T) {
		t.Parallel()
		reg, ex := open(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := ex.Receive(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("Receive = %v, want Canceled", err)
		}
		if lost(reg) {
			t.Fatal("peer declared lost on a cancellation")
		}
	})
	t.Run("an exchange the peer opened declares nothing", func(t *testing.T) {
		t.Parallel()
		reg, ex := open(t)
		ex.responder = true
		if _, err := ex.Receive(expired(t)); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Receive = %v, want DeadlineExceeded", err)
		}
		if lost(reg) {
			t.Fatal("peer declared lost on a responder exchange")
		}
	})
	t.Run("a closed exchange declares nothing", func(t *testing.T) {
		t.Parallel()
		reg, ex := open(t)
		_ = ex.Close()
		// Receive on the closed exchange with an expired deadline reaches
		// receiveTimedOut only when its select picks the deadline over
		// the closed exchange; the direct call reaches it every run.
		_, _ = ex.Receive(expired(t))
		ex.b.receiveTimedOut(ex)
		if lost(reg) {
			t.Fatal("peer declared lost on a closed exchange")
		}
	})
}

// bdxTestMessage is one message of the peer on an exchange.
func bdxTestMessage() bdx.Message {
	return bdx.Message{ProtocolID: im.InteractionModelProtocolID, Opcode: im.OpcodeStatusResponse}
}

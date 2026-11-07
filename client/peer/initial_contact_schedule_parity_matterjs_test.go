// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package peer

// A port of matter.js packages/protocol/test/peer/InitialContactScheduleTest.ts
// at db021da0 (matter.js #4794): the Sigma1 transmission schedule of one
// address. matter.js sends a never-acknowledged Sigma1 on a MessageExchange
// over a mock channel with MockTime; here an unsecured session of an
// exchange.Manager on the harness's fake clock and silent socket carries
// it, the send options mapped from the attempt as handshake.CASE maps them
// for Sigma1. matter.js's PeerAckMissingError is exchange's
// *PeerUnresponsiveError, its PeerMessageMissingError the Go one of the
// same name.

import (
	"bytes"
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/handshake"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// sigma1Send is one attempt's Sigma1 on its own exchange.
type sigma1Send struct {
	attempt *contactAttempt
	x       *exchange.Exchange
	mu      sync.Mutex
	at      []time.Time
	outcome chan error
}

func (s *sigma1Send) transmissions() []time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Time(nil), s.at...)
}

// scheduleRig is a manager on a fake clock whose socket answers nothing.
type scheduleRig struct {
	mgr   *exchange.Manager
	udp   *fakeUDP
	clock *fakeClock
}

func newScheduleRig(t *testing.T) *scheduleRig {
	t.Helper()
	r := &scheduleRig{udp: newFakeUDP(), clock: newFakeClock()}
	mgr, err := exchange.NewManager(r.udp, exchange.Config{Clock: r.clock, Rand: func() float64 { return 0.5 }})
	if err != nil {
		t.Fatal(err)
	}
	r.mgr = mgr
	t.Cleanup(mgr.Close)
	return r
}

// sendSigma1 begins the next attempt of s and sends a Sigma1 nobody
// acknowledges on it (InitialContactScheduleTest.ts:45-88).
func (r *scheduleRig) sendSigma1(t *testing.T, s *initialContactSchedule, isDelayedFallback bool) *sigma1Send {
	t.Helper()
	a, err := s.begin(isDelayedFallback)
	if err != nil {
		t.Fatal(err)
	}
	us, err := r.mgr.NewUnsecuredSession(&net.UDPAddr{IP: net.ParseIP("fd00::1"), Port: 5540}, nil)
	if err != nil {
		t.Fatal(err)
	}
	send := &sigma1Send{attempt: a, outcome: make(chan error, 1)}
	x, err := r.mgr.Initiate(us, mrp.SecureChannelProtocolID, exchange.Options{
		OnSend: func(n int) {
			send.mu.Lock()
			send.at = append(send.at, r.clock.Now())
			send.mu.Unlock()
			a.onSend(n)
		},
		OnReceive: a.onReceive,
	})
	if err != nil {
		t.Fatal(err)
	}
	send.x = x
	go func() {
		send.outcome <- x.Send(context.Background(), mrp.SCOpcodeSigma1, []byte{1}, exchange.SendOptions{
			ExpectedProcessingTime:    handshake.ExpectedCryptoProcessingTime,
			MaxRetransmissions:        a.maxTransmissions,
			MaxRetransmissionTime:     a.maxRetransmissionTime,
			InitialRetransmissionTime: a.initialRetransmissionTime,
			RetransmissionTimeLimit:   a.retransmissionTimeLimit,
			BackoffOffset:             a.backoffOffset,
		})
	}()
	r.transmitted(t, send, 1)
	return send
}

// settle advances the clock until the send failed (:93-107).
func (r *scheduleRig) settle(t *testing.T, s *sigma1Send) error {
	t.Helper()
	for range 7200 {
		select {
		case err := <-s.outcome:
			return err
		default:
		}
		r.clock.Advance(time.Second)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	select {
	case err := <-s.outcome:
		return err
	case <-ctx.Done():
		t.Fatal("the Sigma1 send did not settle")
		return nil
	}
}

// transmitted advances the clock until s transmitted count times
// (:112-123).
func (r *scheduleRig) transmitted(t *testing.T, s *sigma1Send, count int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	// The first transmission is complete — its retransmission timer armed,
	// under the exchange's lock — once the exchange reports the message
	// unacknowledged; only then does the clock move.
	for len(s.transmissions()) == 0 || !s.x.HasUnackedMessage() {
		if ctx.Err() != nil {
			t.Fatal("Sigma1 was never sent")
		}
		select {
		case <-ctx.Done():
		case <-time.NewTimer(time.Millisecond).C:
		}
	}
	for i := 0; i < 3600 && len(s.transmissions()) < count; i++ {
		r.clock.Advance(time.Second)
	}
	if n := len(s.transmissions()); n < count {
		t.Fatalf("%d transmissions, want %d", n, count)
	}
}

func intervalsOf(at []time.Time) []time.Duration {
	out := make([]time.Duration, 0, len(at))
	for i := 1; i < len(at); i++ {
		out = append(out, at[i].Sub(at[i-1]))
	}
	return out
}

func isAckMissing(err error) bool {
	var ackMissing *exchange.PeerUnresponsiveError
	return errors.As(err, &ackMissing)
}

func TestInitialContactScheduleParity(t *testing.T) {
	t.Parallel()
	timing := DefaultTiming()

	t.Run("keeps one Sigma1 for the MRP budget and the retry delay, then continues the backoff with fresh ones", func(t *testing.T) {
		t.Parallel()
		r := newScheduleRig(t)
		unresponsive := 0
		s := newInitialContactSchedule(timing, func() { unresponsive++ })

		identical := r.sendSigma1(t, s, false)
		if identical.attempt.isRefresh {
			t.Fatal("the first attempt refreshes")
		}
		failure := r.settle(t, identical)
		if !isAckMissing(failure) {
			t.Fatalf("failure %v, want an unacknowledged Sigma1", failure)
		}
		at := identical.transmissions()
		if len(at) <= exchange.MaxTransmissions {
			t.Fatalf("%d transmissions, want more than the MRP budget %d", len(at), exchange.MaxTransmissions)
		}
		for _, a := range at {
			if a.Sub(at[0]) >= timing.MaxDelayBetweenInitialContactRetries {
				t.Fatalf("a transmission %v after the first, the retry delay is %v", a.Sub(at[0]), timing.MaxDelayBetweenInitialContactRetries)
			}
		}
		// One message: every transmission is the same datagram.
		first := r.udp.next(t).bytes
		for range len(at) - 1 {
			if d := r.udp.next(t).bytes; !bytes.Equal(d, first) {
				t.Fatal("the identical phase sent a second Sigma1")
			}
		}
		if unresponsive != 1 {
			t.Fatalf("unresponsive signalled %d times, want 1", unresponsive)
		}
		identical.x.Close()
		if got := identical.attempt.fail(failure); got != contactSwitched {
			t.Fatalf("outcome %q, want switched", got)
		}
		identical.attempt.dispose()

		ivs := intervalsOf(at)
		lastIdentical := ivs[len(ivs)-1]
		transmissions := len(at)
		for range 2 {
			fresh := r.sendSigma1(t, s, false)
			if !fresh.attempt.isRefresh || fresh.attempt.backoffOffset != transmissions {
				t.Fatalf("refresh %v with offset %d, want a refresh at %d", fresh.attempt.isRefresh, fresh.attempt.backoffOffset, transmissions)
			}
			freshFailure := r.settle(t, fresh)
			if !isAckMissing(freshFailure) {
				t.Fatalf("failure %v", freshFailure)
			}
			fat := fresh.transmissions()
			if len(fat) != transmissionsPerRefresh {
				t.Fatalf("%d transmissions of a fresh Sigma1, want %d", len(fat), transmissionsPerRefresh)
			}
			for _, iv := range intervalsOf(fat) {
				if iv < lastIdentical*8/10 {
					t.Fatalf("interval %v fell back below the identical phase's %v", iv, lastIdentical)
				}
			}
			fresh.x.Close()
			if got := fresh.attempt.fail(freshFailure); got != contactRefreshed {
				t.Fatalf("outcome %q, want refreshed", got)
			}
			transmissions += len(fat)
		}
		if unresponsive != 1 {
			t.Fatalf("unresponsive signalled %d times across attempts, want 1", unresponsive)
		}
	})

	t.Run("keeps the MRP budget for a delayed fallback and counts retransmissions across attempts", func(t *testing.T) {
		t.Parallel()
		r := newScheduleRig(t)
		unresponsive := 0
		s := newInitialContactSchedule(timing, func() { unresponsive++ })
		fallback := r.sendSigma1(t, s, true)
		if fallback.attempt.initialRetransmissionTime != timing.MaxDelayBetweenInitialContactRetries {
			t.Fatalf("initial retransmission time %v", fallback.attempt.initialRetransmissionTime)
		}
		failure := r.settle(t, fallback)
		if !isAckMissing(failure) || len(fallback.transmissions()) != exchange.MaxTransmissions {
			t.Fatalf("failure %v after %d transmissions, want the MRP budget %d", failure, len(fallback.transmissions()), exchange.MaxTransmissions)
		}
		fallback.x.Close()
		if unresponsive != 0 {
			t.Fatal("the MRP budget alone signalled unresponsive")
		}
		fallback.attempt.fail(failure)
		fresh := r.sendSigma1(t, s, true)
		r.settle(t, fresh)
		fresh.x.Close()
		if unresponsive != 1 {
			t.Fatalf("unresponsive signalled %d times, want 1", unresponsive)
		}
	})

	// The ends of an attempt other than an unacknowledged Sigma1 (:198-262).
	switched := func(t *testing.T) *initialContactSchedule {
		t.Helper()
		s := newInitialContactSchedule(timing, func() {})
		first, err := s.begin(false)
		if err != nil {
			t.Fatal(err)
		}
		if got := first.fail(&exchange.PeerUnresponsiveError{}); got != contactSwitched {
			t.Fatalf("outcome %q, want switched", got)
		}
		first.dispose()
		return s
	}
	identicalPhase := func(t *testing.T, s *initialContactSchedule) {
		t.Helper()
		next, err := s.begin(false)
		if err != nil {
			t.Fatal(err)
		}
		defer next.dispose()
		if next.isRefresh || next.maxTransmissions != -1 || next.backoffOffset != 0 {
			t.Fatalf("attempt %+v, want the identical phase", next)
		}
	}
	t.Run("returns to the identical phase when the peer answered", func(t *testing.T) {
		t.Parallel()
		s := switched(t)
		a, _ := s.begin(false)
		a.onReceive()
		if got := a.fail(&exchange.PeerUnresponsiveError{}); got != "" {
			t.Fatalf("outcome %q", got)
		}
		a.dispose()
		identicalPhase(t, s)
	})
	t.Run("returns to the identical phase on any other failure", func(t *testing.T) {
		t.Parallel()
		s := switched(t)
		a, _ := s.begin(false)
		if got := a.fail(&exchange.PeerMessageMissingError{}); got != "" {
			t.Fatalf("outcome %q", got)
		}
		a.dispose()
		identicalPhase(t, s)
	})
	t.Run("returns to the identical phase when the current attempt is disposed without a failure", func(t *testing.T) {
		t.Parallel()
		s := switched(t)
		a, _ := s.begin(false)
		a.dispose()
		identicalPhase(t, s)
	})
	t.Run("keeps the phase when an attempt that already failed is disposed", func(t *testing.T) {
		t.Parallel()
		s := newInitialContactSchedule(timing, func() {})
		first, _ := s.begin(false)
		first.fail(&exchange.PeerUnresponsiveError{})
		first.dispose()
		next, _ := s.begin(false)
		defer next.dispose()
		if !next.isRefresh {
			t.Fatal("the disposal of a failed attempt reset the phase")
		}
	})
	t.Run("refuses to begin an attempt while the previous one is open", func(t *testing.T) {
		t.Parallel()
		s := switched(t)
		open, _ := s.begin(false)
		defer open.dispose()
		if _, err := s.begin(false); !errors.Is(err, errAttemptOpen) {
			t.Fatalf("begin: %v, want errAttemptOpen", err)
		}
	})
	t.Run("reports a restart for an unacknowledged Sigma1 that a kick restarted while it was failing", func(t *testing.T) {
		t.Parallel()
		s := switched(t)
		a, _ := s.begin(false)
		a.restart()
		if got := a.fail(&exchange.PeerUnresponsiveError{}); got != contactRestarted {
			t.Fatalf("outcome %q, want restarted", got)
		}
		a.dispose()
		identicalPhase(t, s)
	})
	t.Run("leaves any other failure of a restarted attempt to the usual handling", func(t *testing.T) {
		t.Parallel()
		s := switched(t)
		a, _ := s.begin(false)
		a.restart()
		if got := a.fail(&exchange.PeerMessageMissingError{}); got != "" {
			t.Fatalf("outcome %q", got)
		}
		a.dispose()
		identicalPhase(t, s)
	})

	t.Run("signals unresponsiveness again after a restart, but not for transmissions of an ended attempt", func(t *testing.T) {
		t.Parallel()
		r := newScheduleRig(t)
		unresponsive := 0
		s := newInitialContactSchedule(timing, func() { unresponsive++ })
		first := r.sendSigma1(t, s, false)
		r.settle(t, first)
		first.x.Close()
		if unresponsive != 1 {
			t.Fatalf("unresponsive %d, want 1", unresponsive)
		}
		first.attempt.restart()
		first.attempt.onSend(exchange.MaxTransmissions + 10)
		if unresponsive != 1 {
			t.Fatal("a transmission of an ended attempt signalled")
		}
		second := r.sendSigma1(t, s, false)
		r.settle(t, second)
		second.x.Close()
		if unresponsive != 2 {
			t.Fatalf("unresponsive %d after the restart, want 2", unresponsive)
		}
	})

	// The kick blocker (:285-343).
	t.Run("blocks until Sigma1 was retransmitted often enough", func(t *testing.T) {
		t.Parallel()
		r := newScheduleRig(t)
		s := newInitialContactSchedule(timing, func() {})
		send := r.sendSigma1(t, s, false)
		if got := send.attempt.kickBlocker(send.x.RetransmissionRestartSaving(), false); got != kickBlockedRetransmissions {
			t.Fatalf("blocker %q, want retransmissions", got)
		}
		send.x.Close()
		r.settle(t, send)
	})
	t.Run("blocks once the peer answered", func(t *testing.T) {
		t.Parallel()
		r := newScheduleRig(t)
		s := newInitialContactSchedule(timing, func() {})
		send := r.sendSigma1(t, s, true)
		r.transmitted(t, send, timing.KickMinRetransmissions+1)
		if got := send.attempt.kickBlocker(send.x.RetransmissionRestartSaving(), false); got != "" {
			t.Fatalf("blocker %q, want none", got)
		}
		send.attempt.onReceive()
		if got := send.attempt.kickBlocker(send.x.RetransmissionRestartSaving(), false); got != kickBlockedAnswered {
			t.Fatalf("blocker %q, want answered", got)
		}
		send.x.Close()
		r.settle(t, send)
	})
	t.Run("measures the saving against the schedule the restart would use", func(t *testing.T) {
		t.Parallel()
		r := newScheduleRig(t)
		s := newInitialContactSchedule(timing, func() {})
		send := r.sendSigma1(t, s, true)
		r.transmitted(t, send, timing.KickMinRetransmissions+1)
		// A promoted fallback restarts on the fast schedule, a fallback
		// restarts just as slowly as before.
		if got := send.attempt.kickBlocker(send.x.RetransmissionRestartSaving(), false); got != "" {
			t.Fatalf("blocker %q for a fast restart, want none", got)
		}
		if got := send.attempt.kickBlocker(send.x.RetransmissionRestartSaving(), true); got != kickBlockedSaving {
			t.Fatalf("blocker %q for a delayed restart, want saving", got)
		}
		send.x.Close()
		r.settle(t, send)
	})
	t.Run("counts retransmissions across attempts and measures a refreshed exchange against the base schedule", func(t *testing.T) {
		t.Parallel()
		r := newScheduleRig(t)
		s := newInitialContactSchedule(timing, func() {})
		identical := r.sendSigma1(t, s, true)
		identical.attempt.fail(r.settle(t, identical))
		identical.x.Close()
		fresh := r.sendSigma1(t, s, true)
		if got := fresh.attempt.kickBlocker(fresh.x.RetransmissionRestartSaving(), false); got != "" {
			t.Fatalf("blocker %q, want none", got)
		}
		fresh.x.Close()
		r.settle(t, fresh)
	})
}

// TestInitialContactRefreshKickBeforeSend: a kick between the start of a
// refresh attempt and its first transmission is blocked. The attempt's
// count passes the retransmission threshold (it counts the earlier
// attempts), so the saving decides: the try's exchange is cleared at the
// start of a try, and a fresh exchange reports no saving; matter.js
// registers the kick only after createExchange (PeerConnection.ts:544,
// :579 at db021da0).
func TestInitialContactRefreshKickBeforeSend(t *testing.T) {
	t.Parallel()
	t.Run("a fresh exchange reports no saving", func(t *testing.T) {
		t.Parallel()
		r := newScheduleRig(t)
		s := newInitialContactSchedule(DefaultTiming(), func() {})
		identical := r.sendSigma1(t, s, false)
		identical.attempt.fail(r.settle(t, identical))
		identical.x.Close()
		a, err := s.begin(false)
		if err != nil {
			t.Fatal(err)
		}
		defer a.dispose()
		us, err := r.mgr.NewUnsecuredSession(&net.UDPAddr{IP: net.ParseIP("fd00::1"), Port: 5540}, nil)
		if err != nil {
			t.Fatal(err)
		}
		x, err := r.mgr.Initiate(us, mrp.SecureChannelProtocolID, exchange.Options{})
		if err != nil {
			t.Fatal(err)
		}
		defer x.Close()
		if !a.isRefresh || a.earlier < DefaultTiming().KickMinRetransmissions {
			t.Fatalf("attempt %+v, want a refresh past the retransmission threshold", a)
		}
		if got := a.kickBlocker(x.RetransmissionRestartSaving(), false); got != kickBlockedSaving {
			t.Fatalf("blocker %q before the first transmission, want saving", got)
		}
	})
	t.Run("attemptOnce clears the previous try's exchange", func(t *testing.T) {
		t.Parallel()
		r := newScheduleRig(t)
		us, err := r.mgr.NewUnsecuredSession(&net.UDPAddr{IP: net.ParseIP("fd00::1"), Port: 5540}, nil)
		if err != nil {
			t.Fatal(err)
		}
		stale, err := r.mgr.Initiate(us, mrp.SecureChannelProtocolID, exchange.Options{})
		if err != nil {
			t.Fatal(err)
		}
		stale.Close()
		// A disconnected set: the try ends before its handshake.
		h := newHarness(t, nil)
		p, err := h.set.For(testAddr)
		if err != nil {
			t.Fatal(err)
		}
		h.set.Disconnect(t.Context())
		c := &connection{p: p, timing: DefaultTiming()}
		att := &attempt{ctx: t.Context()}
		att.exchange.Store(stale)
		if err := c.attemptOnce(att, newInitialContactSchedule(DefaultTiming(), func() {}), 1); !errors.Is(err, ErrNotConnectable) {
			t.Fatalf("attemptOnce: %v, want ErrNotConnectable", err)
		}
		if att.exchange.Load() != nil {
			t.Fatal("the previous try's exchange is still the kick's")
		}
	})
}

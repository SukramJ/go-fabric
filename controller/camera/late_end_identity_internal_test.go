// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
	wrdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportrequestor"
)

// routedIO is the manager's device I/O with the establishing offer routed
// through the service's provider sessions, as clientIO routes it
// (client_io.go): the requestor takes the session and the offer is
// recorded.
type routedIO struct {
	*fakeIO
	s *Service
}

func (r routedIO) invoke(ctx context.Context, call deviceCall) (any, error) {
	if call.command == callProvideOffer {
		return r.s.ps.establishOwned(ctx, call.target, call.fields, call.establishing)
	}
	return r.fakeIO.invoke(ctx, call)
}

// reuseFixture is a service whose camera hands out the ids of ids in
// turn, wired as New wires it, with its observer's session ends recorded.
type reuseFixture struct {
	f   *psFixture
	s   *Service
	mu  sync.Mutex
	end []SessionEnd
}

func newReuseFixture(t *testing.T, ids ...uint16) *reuseFixture {
	t.Helper()
	x := &reuseFixture{f: newPSFixture()}
	x.s = x.f.svc
	x.s.life = t.Context()
	numbering := sliceBNumbering()
	var next int
	x.f.respond = func(command string) (any, error) {
		x.f.mu.Lock()
		defer x.f.mu.Unlock()
		id := ids[next]
		next++
		return wpdef.ProvideOfferResponse{WebRtcSessionId: id}, nil
	}
	clk := x.s.m.clk
	x.s.m = newManager(routedIO{fakeIO: &fakeIO{state: fixtureState(), respond: numbering}, s: x.s}, clk, x.s.log)
	x.s.ps.cleanup = x.s.m.cleanupContext
	x.s.wire()
	return x
}

func (x *reuseFixture) start(t *testing.T, key string) *Session {
	t.Helper()
	offer := psOffer
	sess, err := x.s.StartStream(t.Context(), StartRequest{Node: cmdErrNode(t), Endpoint: tEndpoint, Usage: csdef.StreamUsageLiveView, Offer: &offer, DeclineAudio: true, Owner: "conn-1", Key: key})
	if err != nil {
		t.Fatalf("start %s: %v", key, err)
	}
	return sess
}

func (x *reuseFixture) ended(e SessionEnd) {
	x.mu.Lock()
	x.end = append(x.end, e)
	x.mu.Unlock()
}

func (x *reuseFixture) endsOf(id uint16) []EndCause {
	x.mu.Lock()
	defer x.mu.Unlock()
	var out []EndCause
	for _, e := range x.end {
		if e.SessionID == id {
			out = append(out, e.Cause)
		}
	}
	return out
}

// intact fails unless the session under id 42 is the given one, with its
// Signals open, its offer recorded and its entry in the requestor.
func (x *reuseFixture) intact(t *testing.T, sess *Session) {
	t.Helper()
	k := sessionKey{tNode, tEndpoint, 42}
	x.s.mu.Lock()
	held := x.s.sessions[k]
	x.s.mu.Unlock()
	if held != sess {
		t.Error("the newer session under id 42 left the service's sessions")
	}
	sess.box.mu.Lock()
	closed := sess.box.closed
	sess.box.mu.Unlock()
	if closed {
		t.Error("the newer session's Signals were closed")
	}
	if _, ok := x.s.ps.offerOf(x.s.ps.key(tTarget, 42)); !ok {
		t.Error("the newer session's recorded offer is gone")
	}
	if !slices.ContainsFunc(x.f.sessions(), func(s wrdef.WebRTCSessionStruct) bool { return s.Id == 42 }) {
		t.Error("the newer session's entry left the requestor")
	}
}

// TestLateEndSparesTheSessionThatReusedItsID (review II, R2-P1-cam-b-02):
// the end of a session the camera lost is applied to that session alone.
// A newer session the camera gave the same id keeps its Signals, its
// recorded offer and its requestor entry, whether the old end is
// delivered late by another goroutine's delivery or a host restarts the
// stream from inside SessionEnded before the vanished callback runs.
// Before, both were applied by (node, endpoint, id) and tore the newer
// session down.
func TestLateEndSparesTheSessionThatReusedItsID(t *testing.T) {
	t.Parallel()
	t.Run("an end announced late by another goroutine's delivery", func(t *testing.T) {
		t.Parallel()
		x := newReuseFixture(t, 42, 43, 42)
		entered, release := make(chan struct{}), make(chan struct{})
		x.s.cfg.Observer.SessionEnded = func(e SessionEnd) {
			x.ended(e)
			if e.SessionID == 43 {
				close(entered)
				<-release
			}
		}
		x.start(t, "a")
		x.start(t, "b")
		delivered := make(chan struct{})
		go func() {
			defer close(delivered)
			// The camera ends 43: this goroutine delivers its end and
			// holds the delivery in the observer.
			x.s.onSignal(Signal{Kind: SignalEnd, Session: wrdef.WebRTCSessionStruct{Id: 43, PeerNodeId: tNode, PeerEndpointId: tEndpoint}})
		}()
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal("the end of 43 was not delivered")
		}
		// The camera rebooted and hands 42 out again: the old 42's end is
		// queued behind the held delivery.
		c := x.start(t, "c")
		close(release)
		select {
		case <-delivered:
		case <-ctx.Done():
			t.Fatal("the held delivery did not finish")
		}
		if got := x.endsOf(42); !slices.Equal(got, []EndCause{EndCause(endCameraRestarted)}) {
			t.Fatalf("ends of 42 = %v, want the old session's camera_restarted", got)
		}
		x.intact(t, c)
	})
	t.Run("a stream restarted from inside SessionEnded", func(t *testing.T) {
		t.Parallel()
		x := newReuseFixture(t, 42, 42)
		var restarted *Session
		x.s.cfg.Observer.SessionEnded = func(e SessionEnd) {
			x.ended(e)
			if e.SessionID == 42 && restarted == nil {
				restarted = x.start(t, "c")
			}
		}
		x.start(t, "a")
		sliceBReport(x.s.m, false, 42)
		// The camera dropped 42: its end is announced, the host restarts
		// the stream and the camera hands 42 out again, then the vanished
		// callback of the old 42 runs.
		sliceBReport(x.s.m, false)
		if restarted == nil {
			t.Fatal("the dropped session's end was not announced")
		}
		x.intact(t, restarted)
	})
}

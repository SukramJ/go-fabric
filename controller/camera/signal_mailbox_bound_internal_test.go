// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
	wrdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportrequestor"
)

// boundFixture is a service holding session 42 for conn-1, its ends
// handed to a channel.
type boundFixture struct {
	x     *reuseFixture
	sess  *Session
	ended chan SessionEnd
}

func newBoundFixture(t *testing.T) *boundFixture {
	t.Helper()
	b := &boundFixture{x: newReuseFixture(t, 42), ended: make(chan SessionEnd, 4)}
	b.x.s.cfg.Observer.SessionEnded = func(e SessionEnd) { b.ended <- e }
	b.sess = b.x.start(t, "a")
	return b
}

// signal delivers one camera command for session 42.
func (b *boundFixture) signal(sig Signal) {
	sig.Session = wrdef.WebRTCSessionStruct{Id: 42, PeerNodeId: tNode, PeerEndpointId: tEndpoint}
	b.x.s.onSignal(sig)
}

func candidateSignal(i int) Signal {
	return Signal{Kind: SignalICECandidates, Candidates: []wrdef.ICECandidateStruct{{Candidate: "candidate:" + strconv.Itoa(i)}}}
}

// endSessions are the EndSessions the camera received for session 42.
func (b *boundFixture) endSessions() []wpdef.EndSessionRequest {
	var out []wpdef.EndSessionRequest
	for _, r := range b.x.s.m.io.(routedIO).recorded() {
		if f, ok := r.fields.(wpdef.EndSessionRequest); ok && r.command == callEndSession && f.WebRtcSessionId == 42 {
			out = append(out, f)
		}
	}
	return out
}

// overflowed waits for the overflow's end and checks that nothing of the
// session is left: the end announced as EndSignalOverflow after one
// EndSession, the pump and the end's goroutine finished, Signals closed
// without a signal, no entry in the registry or the service.
func (b *boundFixture) overflowed(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	select {
	case e := <-b.ended:
		if e.Cause != EndSignalOverflow || e.SessionID != 42 || e.Owner != "conn-1" {
			t.Fatalf("end = %+v, want session 42 of conn-1 ended as %s", e, EndSignalOverflow)
		}
	case <-ctx.Done():
		t.Fatal("the flooded session did not end")
	}
	finished := make(chan struct{})
	go func() {
		b.x.s.ending.Wait()
		close(finished)
	}()
	for name, done := range map[string]<-chan struct{}{"the session's pump": b.sess.box.done, "the overflow's end": finished} {
		select {
		case <-done:
		case <-ctx.Done():
			t.Fatalf("%s is still running", name)
		}
	}
	if n := len(b.sess.box.out); n != 0 {
		t.Fatalf("%d signals buffered", n)
	}
	for sig := range b.sess.Signals() {
		t.Fatalf("an overflowed session's Signals delivered %+v", sig)
	}
	// More of the flood after the end: dropped, no second end.
	for i := range 10 {
		b.signal(candidateSignal(i))
	}
	b.x.s.ending.Wait()
	if got := b.endSessions(); len(got) != 1 || got[0].Reason != endReasonUserHangup {
		t.Fatalf("EndSessions = %+v, want one with UserHangup", got)
	}
	if _, ok := b.x.s.m.sessions.get(tNode, tEndpoint, 42); ok {
		t.Error("the registry still tracks the overflowed session")
	}
	b.x.s.mu.Lock()
	n := len(b.x.s.sessions)
	b.x.s.mu.Unlock()
	if n != 0 {
		t.Errorf("the service still holds %d sessions", n)
	}
}

// TestSignalMailboxIsBounded (review II, R2-P1-cam-b-06): a camera that
// keeps signalling a session whose host does not read its Signals ends
// the session at the mailbox's bound — EndSession to the camera, the end
// announced as EndSignalOverflow, Signals closed — and leaves no goroutine
// and no registry entry. A burst at the bound is delivered in order.
// Before, the mailbox grew with every signal for as long as the camera
// kept the session.
func TestSignalMailboxIsBounded(t *testing.T) {
	t.Parallel()
	t.Run("a flood of candidates ends the session at the count bound", func(t *testing.T) {
		t.Parallel()
		b := newBoundFixture(t)
		for i := range signalMailboxLimit + 1 {
			b.signal(candidateSignal(i))
		}
		b.overflowed(t)
	})
	t.Run("a flood of large SDPs ends the session at the byte bound", func(t *testing.T) {
		t.Parallel()
		b := newBoundFixture(t)
		sdp := strings.Repeat("x", 64000)
		for range signalMailboxBytes/len(sdp) + 1 {
			b.signal(Signal{Kind: SignalAnswer, SDP: sdp})
		}
		b.overflowed(t)
	})
	t.Run("an EndSession that fails ends the session here and abandons it", func(t *testing.T) {
		t.Parallel()
		b := newBoundFixture(t)
		io := b.x.s.m.io.(routedIO).fakeIO
		io.mu.Lock()
		numbering := io.respond
		io.respond = func(r *recordedInvoke) (any, error) {
			if r.command == callEndSession {
				return nil, errors.New("the camera is unreachable")
			}
			return numbering(r)
		}
		io.mu.Unlock()
		for i := range signalMailboxLimit + 1 {
			b.signal(candidateSignal(i))
		}
		b.overflowed(t)
		io.mu.Lock()
		abandoned := slices.Clone(io.abandoned)
		io.mu.Unlock()
		if !slices.Equal(abandoned, []uint16{42}) {
			t.Errorf("abandoned = %v, want session 42 handed to the leftover sweep", abandoned)
		}
	})
	t.Run("a burst at the bound is delivered in order", func(t *testing.T) {
		t.Parallel()
		b := newBoundFixture(t)
		b.signal(Signal{Kind: SignalAnswer, SDP: "answer"})
		for i := range signalMailboxLimit - 1 {
			b.signal(candidateSignal(i))
		}
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		for i := range signalMailboxLimit {
			select {
			case sig := <-b.sess.Signals():
				switch {
				case i == 0 && (sig.Kind != SignalAnswer || sig.SDP != "answer"):
					t.Fatalf("signal 0 = %+v, want the answer", sig)
				case i > 0 && (sig.Kind != SignalICECandidates || sig.Candidates[0].Candidate != "candidate:"+strconv.Itoa(i-1)):
					t.Fatalf("signal %d = %+v, want candidate %d", i, sig, i-1)
				}
			case <-ctx.Done():
				t.Fatalf("signal %d was not delivered", i)
			}
		}
		select {
		case e := <-b.ended:
			t.Fatalf("a session within the bound ended: %+v", e)
		default:
		}
		if got := b.endSessions(); len(got) != 0 {
			t.Fatalf("EndSessions = %+v, want none", got)
		}
		// Read, the box takes the next burst.
		b.signal(candidateSignal(0))
		select {
		case <-b.sess.Signals():
		case <-ctx.Done():
			t.Fatal("a signal after the burst was not delivered")
		}
	})
}

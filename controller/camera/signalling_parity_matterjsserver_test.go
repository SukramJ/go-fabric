// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
	wrdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportrequestor"
	"github.com/SukramJ/go-fabric/controller"
)

// The cases of matterjs-server's
// packages/ws-controller/test/StartingSessionSignallingTest.ts:64-460 at
// 0bd06f2 the module carries: a session's signals are its own and keep
// their order until the host reads them (the Session's Signals), and
// candidates go in sends that fit one Matter message. The queue of a
// connection's candidates by client_session_key is go-fabric-server's.

// sigCandidateOfBytes is a candidate whose estimated size in a send is
// exactly bytes (StartingSessionSignallingTest.ts:26-29, matterjs-server
// at 0bd06f2).
func sigCandidateOfBytes(bytes int, label string) wpdef.ICECandidateStruct {
	pad := max(bytes-iceCandidateFramingBytes-len(label), 0)
	return wpdef.ICECandidateStruct{Candidate: label + strings.Repeat("x", pad), SdpMid: spec.Nullable[string]{Null: true}}
}

func sigLabels(sends [][]wpdef.ICECandidateStruct) [][]string {
	out := make([][]string, len(sends))
	for i, send := range sends {
		for _, c := range send {
			out[i] = append(out[i], c.Candidate[:2])
		}
	}
	return out
}

// sigService is a service that routes signals to the sessions it holds.
func sigService() *Service {
	s := &Service{sessions: map[sessionKey]*Session{}, m: newManager(&fakeIO{}, nil, nil)}
	s.ps = &providerSessions{offers: map[providerKey]string{}}
	return s
}

// TestSignallingHoldParityMatterjsServer is describe("hold")
// (StartingSessionSignallingTest.ts:65-94, matterjs-server at 0bd06f2).
func TestSignallingHoldParityMatterjsServer(t *testing.T) {
	life := t.Context()
	t.Run("holds an event of the started session only for its node, endpoint and id", func(t *testing.T) {
		s := sigService()
		sess := &Session{box: newMailbox(life)}
		s.sessions[sessionKey{5, 1, 7}] = sess
		var elsewhere []wrdef.WebRTCSessionStruct
		s.cfg.Observer.Signal = func(sig Signal) { elsewhere = append(elsewhere, sig.Session) }
		for _, other := range []wrdef.WebRTCSessionStruct{{Id: 7, PeerNodeId: 6, PeerEndpointId: 1}, {Id: 7, PeerNodeId: 5, PeerEndpointId: 2}, {Id: 8, PeerNodeId: 5, PeerEndpointId: 1}} {
			s.onSignal(Signal{Kind: controller.CameraSignalICECandidates, Session: other})
		}
		if len(elsewhere) != 3 {
			t.Fatalf("signals of other sessions reached %d times elsewhere", len(elsewhere))
		}
		s.onSignal(Signal{Kind: controller.CameraSignalICECandidates, Session: wrdef.WebRTCSessionStruct{Id: 7, PeerNodeId: 5, PeerEndpointId: 1}})
		if sig := <-sess.Signals(); sig.Session.Id != 7 || len(elsewhere) != 3 {
			t.Errorf("the session's own signal: %+v", sig)
		}
	})
	t.Run("sends held events at release, in order, before the queued candidates", func(t *testing.T) {
		// The signals delivered before the host reads are kept and come out
		// in order; the queued candidates are the connection's.
		s := sigService()
		sess := &Session{box: newMailbox(life)}
		s.sessions[sessionKey{5, 1, 7}] = sess
		on := wrdef.WebRTCSessionStruct{Id: 7, PeerNodeId: 5, PeerEndpointId: 1}
		s.onSignal(Signal{Kind: controller.CameraSignalAnswer, Session: on, SDP: "event-1"})
		s.onSignal(Signal{Kind: controller.CameraSignalAnswer, Session: on, SDP: "event-2"})
		got := make([]string, 0, 2)
		for range 2 {
			got = append(got, (<-sess.Signals()).SDP)
		}
		if !slices.Equal(got, []string{"event-1", "event-2"}) {
			t.Errorf("order %q", got)
		}
	})
}

// TestSignallingSendsParityMatterjsServer is the budget cases of
// describe("sending the queue") (StartingSessionSignallingTest.ts:262-373,
// matterjs-server at 0bd06f2), for one call's candidates.
func TestSignallingSendsParityMatterjsServer(t *testing.T) {
	half := iceCandidatesByteBudget / 2
	tooBigForTwo := half + 1
	t.Run("keeps candidates in one invoke up to the byte budget and splits one byte over it", func(t *testing.T) {
		fits := iceCandidateSends([]wpdef.ICECandidateStruct{sigCandidateOfBytes(iceCandidatesByteBudget-100, "a0"), sigCandidateOfBytes(100, "b0")})
		if got := sigLabels(fits); !slices.EqualFunc(got, [][]string{{"a0", "b0"}}, slices.Equal) {
			t.Errorf("fitting %v", got)
		}
		over := iceCandidateSends([]wpdef.ICECandidateStruct{sigCandidateOfBytes(iceCandidatesByteBudget-100, "a0"), sigCandidateOfBytes(101, "b0"), sigCandidateOfBytes(10, "b1")})
		if got := sigLabels(over); !slices.EqualFunc(got, [][]string{{"a0"}, {"b0", "b1"}}, slices.Equal) {
			t.Errorf("over %v", got)
		}
	})
	t.Run("sends a candidate larger than the budget alone instead of dropping it", func(t *testing.T) {
		sends := iceCandidateSends([]wpdef.ICECandidateStruct{sigCandidateOfBytes(iceCandidatesByteBudget*2, "a0"), sigCandidateOfBytes(20, "b0")})
		if got := sigLabels(sends); !slices.EqualFunc(got, [][]string{{"a0"}, {"b0"}}, slices.Equal) {
			t.Errorf("sends %v", got)
		}
	})
	t.Run("answers a command split across invokes with the first failure, else success", func(t *testing.T) {
		first, second := errors.New("first"), errors.New("second")
		for _, c := range []struct {
			failures []error
			want     error
		}{{[]error{first, second}, first}, {[]error{nil, second}, second}, {[]error{first, nil}, first}, {[]error{nil, nil}, nil}} {
			var provided [][]wpdef.ICECandidateStruct
			err := provideCandidates(context.Background(), []wpdef.ICECandidateStruct{sigCandidateOfBytes(tooBigForTwo, "a0"), sigCandidateOfBytes(tooBigForTwo, "a1")},
				func(_ context.Context, send []wpdef.ICECandidateStruct) error {
					provided = append(provided, send)
					return c.failures[len(provided)-1]
				})
			if !errors.Is(err, c.want) || (c.want == nil && err != nil) {
				t.Errorf("failures %v: err = %v, want %v", c.failures, err, c.want)
			}
			if got := sigLabels(provided); !slices.EqualFunc(got, [][]string{{"a0"}, {"a1"}}, slices.Equal) {
				t.Errorf("provided %v", got)
			}
		}
	})
}

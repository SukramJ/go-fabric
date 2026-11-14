// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"testing"
	"time"

	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	wrdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportrequestor"
	"github.com/SukramJ/go-fabric/controller"
)

// TestFailedStartReleasesItsSignals (review II, R2-P1-cam-b-01): a start
// that fails after the camera already signalled its session — the owner
// stopped it once the offer was answered, and the camera's candidates
// arrived while the start's EndSession was in flight — drops the queued
// signals and ends the session's pump at once: nobody ever receives that
// Session. Before, the mailbox was closed with the signals queued, and the
// pump stayed parked on a send no one would take until the Service closed.
func TestFailedStartReleasesItsSignals(t *testing.T) {
	t.Parallel()
	s := sigService()
	s.life = t.Context()
	var box *mailbox
	numbering := sliceBNumbering()
	s.m, _, _ = managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
		switch r.command {
		case callProvideOffer:
			// The owner stops the start before the answer is learned: track
			// refuses it, and the start gives the session up.
			ks, ok := s.m.sessions.byClientKey(tNode, tEndpoint, "conn-1", "k")
			if !ok || ks.pending == nil {
				t.Error("the start is not registered under its key")
			} else {
				_ = s.m.sessions.cancel(ks.pending)
			}
		case callEndSession:
			// The camera's candidates arrive while EndSession is in flight:
			// the session is registered, so they are queued for it.
			s.mu.Lock()
			if sess := s.sessions[sessionKey{tNode, tEndpoint, 42}]; sess != nil {
				box = sess.box
			}
			s.mu.Unlock()
			s.onSignal(Signal{Kind: controller.CameraSignalICECandidates, Session: wrdef.WebRTCSessionStruct{Id: 42, PeerNodeId: tNode, PeerEndpointId: tEndpoint}})
		}
		return numbering(r)
	})
	offer := videoOffer
	_, err := s.StartStream(t.Context(), StartRequest{Node: cmdErrNode(t), Endpoint: tEndpoint, Usage: csdef.StreamUsageLiveView, Offer: &offer, DeclineAudio: true, Owner: "conn-1", Key: "k"})
	if err == nil {
		t.Fatal("the stopped start succeeded")
	}
	if box == nil {
		t.Fatal("the session was not registered while its EndSession was in flight")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	select {
	case <-box.done:
	case <-ctx.Done():
		t.Fatal("the failed start's pump is still parked with a queued signal and no reader")
	}
}

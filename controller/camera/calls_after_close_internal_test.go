// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	wpdef "thingfabric.dev/go-fabric/cluster/spec/webrtctransportprovider"
)

// TestCallsAfterCloseAreRefused (review II, R2-P1-cam-b-04,
// R2-P6-api-2207): after Close every call that would act on the camera
// through the detached service — Provider.Send, Provider.EndSession,
// Snapshot, StopSession, Capabilities, ReleaseStream, StopByKey and a
// Session's Stop, ProvideAnswer and ProvideICECandidates — is refused with
// ErrClosed and sends nothing. Before, a raw ProvideOffer opened a session
// and registered it with the requestor after Close, one nothing would end,
// StopSession sent an EndSession and announced its end to the observer of
// the closed service, and the six later ones still reached the camera.
// Release stays a no-op once Close ended its owner's sessions.
func TestCallsAfterCloseAreRefused(t *testing.T) {
	t.Parallel()
	x := newReuseFixture(t, 42, 43)
	var ends []SessionEnd
	x.s.cfg.Observer.SessionEnded = func(e SessionEnd) { ends = append(ends, e) }
	sess := x.start(t, "a")
	if err := x.s.Close(t.Context()); err != nil {
		t.Fatalf("close: %v", err)
	}
	// The raw route's own commands reach the camera through the service's
	// client I/O.
	var raw []uint32
	x.s.io = &clientIO{svc: x.s, send: func(_ context.Context, _ target, _, command uint32, _ spec.Encodable) (any, error) {
		raw = append(raw, command)
		return nil, nil
	}}
	io := x.s.m.io.(routedIO).fakeIO
	sentBefore, callsBefore, endsBefore, heldBefore := len(io.recorded()), len(x.f.callsNow()), len(ends), len(x.f.sessions())
	node := cmdErrNode(t)
	p := &Provider{s: x.s, t: tTarget}
	payload, _ := json.Marshal(map[string]any{"webRtcSessionId": nil, "sdp": psOffer, "streamUsage": 3, "videoStreams": []int{9}})
	if _, _, err := p.Send(t.Context(), "ProvideOffer", payload); !errors.Is(err, ErrClosed) {
		t.Errorf("Provider.Send after Close = %v, want ErrClosed", err)
	}
	if err := p.EndSession(t.Context(), wpdef.EndSessionRequest{WebRtcSessionId: 42}, "conn-2"); !errors.Is(err, ErrClosed) {
		t.Errorf("Provider.EndSession after Close = %v, want ErrClosed", err)
	}
	if _, err := x.s.Snapshot(t.Context(), SnapshotRequest{Node: node, Endpoint: tEndpoint}); !errors.Is(err, ErrClosed) {
		t.Errorf("Snapshot after Close = %v, want ErrClosed", err)
	}
	if _, err := x.s.StopSession(t.Context(), node, tEndpoint, 42, "conn-2"); !errors.Is(err, ErrClosed) {
		t.Errorf("StopSession after Close = %v, want ErrClosed", err)
	}
	if _, err := x.s.Capabilities(t.Context(), node, tEndpoint); !errors.Is(err, ErrClosed) {
		t.Errorf("Capabilities after Close = %v, want ErrClosed", err)
	}
	if err := x.s.ReleaseStream(t.Context(), node, tEndpoint, StreamVideo, 1); !errors.Is(err, ErrClosed) {
		t.Errorf("ReleaseStream after Close = %v, want ErrClosed", err)
	}
	if _, err := x.s.StopByKey(t.Context(), node, tEndpoint, "conn-1", "a"); !errors.Is(err, ErrClosed) {
		t.Errorf("StopByKey after Close = %v, want ErrClosed", err)
	}
	if _, err := sess.Stop(t.Context()); !errors.Is(err, ErrClosed) {
		t.Errorf("Session.Stop after Close = %v, want ErrClosed", err)
	}
	if err := sess.ProvideAnswer(t.Context(), psAnswer); !errors.Is(err, ErrClosed) {
		t.Errorf("Session.ProvideAnswer after Close = %v, want ErrClosed", err)
	}
	if err := sess.ProvideICECandidates(t.Context(), []wpdef.ICECandidateStruct{{Candidate: "candidate:1 1 UDP 1 192.0.2.1 9 typ host"}}); !errors.Is(err, ErrClosed) {
		t.Errorf("Session.ProvideICECandidates after Close = %v, want ErrClosed", err)
	}
	x.s.Release(t.Context(), "conn-1")
	if got := io.recorded()[sentBefore:]; len(got) != 0 {
		t.Errorf("sent after Close: %+v", got)
	}
	if len(raw) != 0 {
		t.Errorf("raw commands after Close: %v", raw)
	}
	if got := x.f.callsNow()[callsBefore:]; len(got) != 0 {
		t.Errorf("provider calls after Close: %v", got)
	}
	// (The fixture's EndSession bypasses the requestor, so Close's own end
	// leaves 42 there; nothing may be added.)
	if got := x.f.sessions(); len(got) != heldBefore {
		t.Errorf("requestor sessions after the calls: %+v, want %d", got, heldBefore)
	}
	if got := ends[endsBefore:]; len(got) != 0 {
		t.Errorf("ends announced after Close: %+v", got)
	}
}

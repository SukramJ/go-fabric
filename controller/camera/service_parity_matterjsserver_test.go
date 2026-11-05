// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"errors"
	"slices"
	"testing"

	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
	wrdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportrequestor"
	"github.com/SukramJ/go-fabric/controller"
)

// The cases of matterjs-server's
// packages/ws-controller/test/MatterControllerCameraStreamsTest.ts:31-216 at
// 0bd06f2 the module carries, ported onto the service's own wiring: the
// peer's End drops the registry entry, a report's vanished session leaves
// the requestor, a removed node's sessions end, and Close ends the
// sessions before it lets go of the controller.

// svcTracked is a service over the psFixture with session 7 of node 5
// endpoint 1 started through it and held by the requestor.
func svcTracked(t *testing.T, endSession func(*recordedInvoke) (any, error)) (*psFixture, *fakeIO) {
	t.Helper()
	f := newPSFixture()
	io := &fakeIO{state: fixtureState(), respond: func(r *recordedInvoke) (any, error) {
		switch r.command {
		case callProvideOffer:
			return wpdef.ProvideOfferResponse{WebRtcSessionId: 7}, nil
		case callEndSession:
			if endSession != nil {
				return endSession(r)
			}
		case cmdVideoStreamAllocate:
			return csdef.VideoStreamAllocateResponse{VideoStreamId: 2}, nil
		}
		return nil, nil
	}}
	f.svc.m = newManager(io, nil, nil)
	f.svc.wire()
	sdp := videoOffer
	if _, err := f.svc.m.startStream(context.Background(), startArgs{t: target{psCamera, psEndpoint}, connectionID: "conn-1", usage: csdef.StreamUsageLiveView, sdp: &sdp, allowEviction: true}); err != nil {
		t.Fatal(err)
	}
	_ = f.upsert(trackedSession(func(s *wrdef.WebRTCSessionStruct) { s.Id = 7 }))
	return f, io
}

// TestServiceWiringParityMatterjsServer is describe("MatterController.cameraStreamsIfCreated")
// (MatterControllerCameraStreamsTest.ts:31-216, matterjs-server at 0bd06f2).
func TestServiceWiringParityMatterjsServer(t *testing.T) {
	t.Run("the cameraStreams getter itself still fails typed once stopped", func(t *testing.T) {
		f := newPSFixture()
		if err := f.svc.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
		if _, err := f.svc.StartStream(context.Background(), StartRequest{}); !errors.Is(err, ErrClosed) {
			t.Errorf("StartStream after Close: %v", err)
		}
	})
	t.Run("drops the camera registry entry when the peer ends a session", func(t *testing.T) {
		f, _ := svcTracked(t, nil)
		on := wrdef.WebRTCSessionStruct{Id: 7, PeerNodeId: psCamera, PeerEndpointId: psEndpoint}
		f.svc.onSignal(Signal{Kind: controller.CameraSignalAnswer, Session: on, SDP: "v=0"})
		if _, ok := f.svc.m.sessions.get(psCamera, psEndpoint, 7); !ok {
			t.Fatal("an answer dropped the entry")
		}
		f.svc.onSignal(Signal{Kind: controller.CameraSignalEnd, Session: on, Reason: 9})
		if _, ok := f.svc.m.sessions.get(psCamera, psEndpoint, 7); ok {
			t.Error("the peer's End left the entry")
		}
	})
	t.Run("applies a camera's session report and drops the requestor tracking of each session that vanished", func(t *testing.T) {
		f, _ := svcTracked(t, nil)
		f.svc.reported = f.svc.reconcile
		f.setCamera(psEndpoint, []sessionPeer{{7, psServer}})
		f.batch()
		f.setCamera(psEndpoint, []sessionPeer{{3, psServer}})
		f.svc.w.startedUp(psCamera)
		f.batch()
		if got := f.sessions(); len(got) != 0 {
			t.Errorf("requestor %+v", got)
		}
	})
	t.Run("drops the camera sessions of a node removed from the fabric", func(t *testing.T) {
		f, _ := svcTracked(t, nil)
		var causes []sessionEndCause
		onEnded := f.svc.m.onEnded
		f.svc.m.onEnded = func(e cameraSessionEnded) { causes = append(causes, e.cause); onEnded(e) }
		f.svc.nodeRemoved(psCamera)
		if !slices.Equal(causes, []sessionEndCause{endNodeRemoved}) {
			t.Errorf("ends %v", causes)
		}
	})
	t.Run("stop() waits for every open camera session to be released before closing connections", func(t *testing.T) {
		gate := make(chan struct{})
		f, io := svcTracked(t, func(*recordedInvoke) (any, error) { <-gate; return nil, nil })
		closed := make(chan error, 1)
		go func() { closed <- f.svc.Close(context.Background()) }()
		for !slices.Contains(io.commands(), callEndSession) {
			select {
			case <-closed:
				t.Fatal("Close returned before the session ended")
			default:
			}
		}
		select {
		case <-closed:
			t.Fatal("Close returned while EndSession was in flight")
		default:
		}
		close(gate)
		if err := <-closed; err != nil {
			t.Fatal(err)
		}
	})
}

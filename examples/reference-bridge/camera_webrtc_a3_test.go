// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"thingfabric.dev/go-fabric/cluster/camera"
	"thingfabric.dev/go-fabric/cluster/spec"
	csdef "thingfabric.dev/go-fabric/cluster/spec/cameraavstreammanagement"
	wpdef "thingfabric.dev/go-fabric/cluster/spec/webrtctransportprovider"
	"thingfabric.dev/go-fabric/im"
)

// TestCameraSimSolicitOfferCountsStreamsA3: a session SolicitOffer opened
// counts the streams it names, as ProvideOffer's does — a stream the
// solicited session names is refused deallocation (INVALID_IN_STATE)
// while the session lives, and EndSession releases it (issue #596 C;
// chip's HandleSolicitOffer, webrtc-provider-manager.cpp:176 at 6170af84,
// acquires the session's streams at :301).
func TestCameraSimSolicitOfferCountsStreamsA3(t *testing.T) {
	t.Parallel()
	c, _, _ := simCamera(t, cameraOptions{})
	provider := c.camera.WebRtcTransportProvider().Underlying()
	provider.(*camera.Provider).AttachPeerConnector(&simSignals{})
	video, audio := simStreams(t, c)
	v0, a0 := simRefCounts(t, c)

	nv, na := spec.Nullable[uint16]{Value: video}, spec.Nullable[uint16]{Value: audio}
	resp, status := simInvoke(provider, wpdef.CmdSolicitOffer, wpdef.SolicitOfferRequest{
		StreamUsage: wpdef.StreamUsageLiveView, OriginatingEndpointId: 1, VideoStreamId: &nv, AudioStreamId: &na,
	})
	if status != im.StatusSuccess {
		t.Fatalf("SolicitOffer: %v", status)
	}
	id := resp.(wpdef.SolicitOfferResponse).WebRtcSessionId
	if v, a := simRefCounts(t, c); v != v0+1 || a != a0+1 {
		t.Fatalf("reference counts after SolicitOffer %d/%d, want %d/%d", v, a, v0+1, a0+1)
	}
	avsm := c.camera.CameraAvStreamManagement().Underlying()
	if _, status := simInvoke(avsm, csdef.CmdVideoStreamDeallocate, csdef.VideoStreamDeallocateRequest{VideoStreamId: video}); status != im.StatusInvalidInState {
		t.Fatalf("VideoStreamDeallocate of a stream a solicited session names: %v, want INVALID_IN_STATE", status)
	}
	if _, status := simInvoke(provider, wpdef.CmdEndSession, wpdef.EndSessionRequest{WebRtcSessionId: id, Reason: uint8(wpdef.WebRTCEndReasonUserHangup)}); status != im.StatusSuccess {
		t.Fatalf("EndSession: %v", status)
	}
	if v, a := simRefCounts(t, c); v != v0 || a != a0 {
		t.Fatalf("reference counts after EndSession %d/%d, want %d/%d", v, a, v0, a0)
	}
	if _, status := simInvoke(avsm, csdef.CmdVideoStreamDeallocate, csdef.VideoStreamDeallocateRequest{VideoStreamId: video}); status != im.StatusSuccess {
		t.Fatalf("VideoStreamDeallocate after EndSession: %v", status)
	}
}

// lockedBufferA3 is a log sink the Answer goroutine and the test share.
type lockedBufferA3 struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBufferA3) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBufferA3) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestCameraSimAnswerLogsEachStepA3: at Debug the Answer path logs the
// session seen, the attempt and its outcome, in that order, so a run in
// which no Answer reached the requestor says where it stopped (issue
// #596 B step 1).
func TestCameraSimAnswerLogsEachStepA3(t *testing.T) {
	t.Parallel()
	logs := &lockedBufferA3{}
	c, _, _ := simCamera(t, cameraOptions{}, func(c *demoCamera) {
		c.logger = slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	})
	c.camera.WebRtcTransportProvider().Underlying().(*camera.Provider).AttachPeerConnector(&simSignals{})
	video, audio := simStreams(t, c)
	simProvideOffer(t, c, video, audio)
	waitFor(t, "the sent line", func() bool { return strings.Contains(logs.String(), "camerasim.answer.sent") })
	out := logs.String()
	at := -1
	for _, msg := range []string{"camerasim.answer.session_seen", "camerasim.answer.attempt", "camerasim.answer.sent"} {
		i := strings.Index(out, msg)
		if i <= at {
			t.Fatalf("%s missing or out of order in\n%s", msg, out)
		}
		at = i
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"runtime"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/spec"
	avdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavsettingsuserlevelmanagement"
	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	chimedef "github.com/SukramJ/go-fabric/cluster/spec/chime"
	padef "github.com/SukramJ/go-fabric/cluster/spec/pushavstreamtransport"
	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
	zmdef "github.com/SukramJ/go-fabric/cluster/spec/zonemanagement"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/surface"
)

// The camera simulation's behaviours (camera.go) against connectedhomeip's
// camera-app at the harness pin 6170af84, driven through the cluster
// servers' commands, the ports and the app pipe; the timers run on a fake
// clock.

// simEvent is an event a camera server emitted.
type simEvent struct {
	cluster, event uint32
	data           any
}

// simEvents records the events of the servers it is the emitter of.
type simEvents struct{ ch chan simEvent }

func (r *simEvents) MatterEmitEvent(_ uint16, cluster, event uint32, data any, _ contract.EventPriority) {
	r.ch <- simEvent{cluster, event, data}
}

// next waits for the next event; the push transports' events are emitted
// from their own goroutine (camera.go beginTransport, endTransport).
func (r *simEvents) next(t *testing.T) simEvent {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	select {
	case e := <-r.ch:
		return e
	case <-ctx.Done():
		t.Fatal("no event")
		return simEvent{}
	}
}

// none asserts that no event is pending once the goroutines the last step
// started had their chance to run.
func (r *simEvents) none(t *testing.T) {
	t.Helper()
	for range 64 {
		runtime.Gosched()
	}
	select {
	case e := <-r.ch:
		t.Fatalf("unexpected event 0x%02X of cluster 0x%04X: %+v", e.event, e.cluster, e.data)
	default:
	}
}

// simCamera is a built camera simulation on a fake clock whose servers
// emit into the returned recorder.
func simCamera(t *testing.T, opts cameraOptions, edit ...func(*demoCamera)) (*demoCamera, *clocktest.Fake, *simEvents) {
	t.Helper()
	c := newDemoCamera(surface.Policies{}, opts, slog.New(slog.DiscardHandler))
	fake := clocktest.New(time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC))
	c.clk = fake
	for _, e := range edit {
		e(c)
	}
	if _, err := c.devices(); err != nil {
		t.Fatal(err)
	}
	rec := &simEvents{ch: make(chan simEvent, 64)}
	for _, srv := range []contract.ClusterServer{c.camera.ZoneManagement().Underlying(), c.camera.PushAvStreamTransport().Underlying()} {
		srv.(contract.EventReceiver).SetMatterEventEmitter(rec)
	}
	return c, fake, rec
}

// simPeer is an invoke from node 0x55 on fabric 1.
func simPeer() context.Context {
	return im.WithSubject(im.WithFabricFilter(context.Background(), false, 1), 0x55, nil)
}

// simInvoke invokes a command on a camera server and returns its response
// and status.
func simInvoke(srv contract.ClusterServer, cmdID uint32, req any) (any, im.StatusCode) {
	resp, err := srv.MatterInvoke(simPeer(), cmdID, req)
	return resp, spec.StatusOf(err)
}

func wantStatus(t *testing.T, what string, err error, want im.StatusCode) {
	t.Helper()
	if got := spec.StatusOf(err); got != want {
		t.Fatalf("%s: status %v (%v), want %v", what, got, err, want)
	}
}

// simVideoRequest asks for a stream inside the camera-app's stream 1
// (640x480 to 720p, 15 to 60 fps; camera-device.cpp InitializeVideoStreams).
func simVideoRequest() csdef.VideoStreamAllocateRequest {
	off := false
	return csdef.VideoStreamAllocateRequest{
		WatermarkEnabled: &off, OsdEnabled: &off,
		StreamUsage: csdef.StreamUsageLiveView, VideoCodec: csdef.VideoCodecH264, MinFrameRate: 30, MaxFrameRate: 30,
		MinResolution: csdef.VideoResolutionStruct{Width: 640, Height: 480}, MaxResolution: csdef.VideoResolutionStruct{Width: 1280, Height: 720},
		MinBitRate: camMinBitRate, MaxBitRate: camMaxBitRate, KeyFrameInterval: camKeyFrameInterval,
	}
}

// TestCameraSimVideoStreams: the stream management's video allocation
// through the server.
func TestCameraSimVideoStreams(t *testing.T) {
	t.Parallel()
	c, _, _ := simCamera(t, cameraOptions{})
	avsm := c.camera.CameraAvStreamManagement().Underlying()
	ctx := context.Background()

	// Mirrors chip examples/camera-app/linux/src/clusters/camera-avstream-mgmt/camera-av-stream-manager.cpp:379-405 at 6170af84:
	// an unused compatible stream is allocated, its viewport the camera's
	// (:395-396), and DPTZ learns of it (:398-399).
	resp, status := simInvoke(avsm, csdef.CmdVideoStreamAllocate, simVideoRequest())
	if status != im.StatusSuccess {
		t.Fatalf("VideoStreamAllocate: %v", status)
	}
	if id := resp.(csdef.VideoStreamAllocateResponse).VideoStreamId; id != 1 {
		t.Fatalf("VideoStreamAllocate = stream %d, want the camera-app's stream 1", id)
	}
	if got := c.videos[0].viewport; got != (avdef.ViewportStruct{X2: camMaxWidth, Y2: camMaxHeight}) {
		t.Fatalf("the new stream's viewport = %+v, want the camera's", got)
	}
	if dptz := c.camera.CameraAvSettingsUserLevelManagement().Get().DptzStreams; len(dptz) != 1 || dptz[0].VideoStreamId != 1 {
		t.Fatalf("DptzStreams after the allocation = %+v, want stream 1", dptz)
	}

	// Mirrors chip examples/camera-app/linux/src/clusters/camera-avstream-mgmt/camera-av-stream-manager.cpp:368-377 at 6170af84:
	// the same request reuses the allocated stream.
	if resp, status := simInvoke(avsm, csdef.CmdVideoStreamAllocate, simVideoRequest()); status != im.StatusSuccess || resp.(csdef.VideoStreamAllocateResponse).VideoStreamId != 1 {
		t.Fatalf("a second VideoStreamAllocate = %v, %v; want stream 1 reused", resp, status)
	}

	// Mirrors chip examples/camera-app/linux/src/clusters/camera-avstream-mgmt/camera-av-stream-manager.cpp:384-391 at 6170af84:
	// a 1080p stream needs a second encoder, the camera has one
	// (camera-device.h kMaxConcurrentEncoders).
	hd := camVideo(0, cam60fps, camMaxVideoFrameRate, cam720Width, cam720Height, camMaxWidth, camMaxHeight)
	_, err := camVideoPort{c}.VideoStreamAllocate(ctx, hd)
	wantStatus(t, "a second encoder", err, im.StatusResourceExhausted)

	// Mirrors chip examples/camera-app/linux/src/clusters/camera-avstream-mgmt/camera-av-stream-manager.cpp:363-366 at 6170af84.
	av1 := hd
	av1.VideoCodec = csdef.VideoCodecAv1
	_, err = camVideoPort{c}.VideoStreamAllocate(ctx, av1)
	wantStatus(t, "an AV1 stream", err, im.StatusDynamicConstraintError)

	// Mirrors chip examples/camera-app/linux/src/clusters/camera-avstream-mgmt/camera-av-stream-manager.cpp:447-472 at 6170af84:
	// only an allocated stream is modified.
	wantStatus(t, "VideoStreamModify of stream 1", camVideoPort{c}.VideoStreamModify(ctx, 1, nil, nil), im.StatusSuccess)
	wantStatus(t, "VideoStreamModify of stream 2", camVideoPort{c}.VideoStreamModify(ctx, 2, nil, nil), im.StatusNotFound)

	// Mirrors chip examples/camera-app/linux/src/clusters/camera-avstream-mgmt/camera-av-stream-manager.cpp:474-494 at 6170af84:
	// the deallocation frees the stream and leaves DPTZ (:485); a second
	// one finds nothing.
	if _, status := simInvoke(avsm, csdef.CmdVideoStreamDeallocate, csdef.VideoStreamDeallocateRequest{VideoStreamId: 1}); status != im.StatusSuccess {
		t.Fatalf("VideoStreamDeallocate: %v", status)
	}
	if c.videos[0].allocated {
		t.Fatal("stream 1 is still allocated")
	}
	if dptz := c.camera.CameraAvSettingsUserLevelManagement().Get().DptzStreams; len(dptz) != 0 {
		t.Fatalf("DptzStreams after the deallocation = %+v, want none", dptz)
	}
	wantStatus(t, "a second deallocation", camVideoPort{c}.VideoStreamDeallocate(ctx, 1), im.StatusNotFound)
}

// TestCameraSimAudioAndSnapshotStreams: the audio and snapshot allocation.
func TestCameraSimAudioAndSnapshotStreams(t *testing.T) {
	t.Parallel()
	c, _, _ := simCamera(t, cameraOptions{})
	avsm := c.camera.CameraAvStreamManagement().Underlying()
	ctx := context.Background()

	// Mirrors chip examples/camera-app/linux/src/clusters/camera-avstream-mgmt/camera-av-stream-manager.cpp:496-524 at 6170af84:
	// the first compatible stream, allocated or not, else
	// DYNAMIC_CONSTRAINT_ERROR.
	mono := csdef.AudioStreamAllocateRequest{StreamUsage: csdef.StreamUsageLiveView, AudioCodec: csdef.AudioCodecOpus, ChannelCount: 1, SampleRate: 48000, BitRate: 20000, BitDepth: 24}
	resp, status := simInvoke(avsm, csdef.CmdAudioStreamAllocate, mono)
	if status != im.StatusSuccess || resp.(csdef.AudioStreamAllocateResponse).AudioStreamId != 1 {
		t.Fatalf("AudioStreamAllocate = %v, %v; want stream 1", resp, status)
	}
	if id, err := (camAudioPort{c}).AudioStreamAllocate(ctx, csdef.AudioStreamStruct{AudioCodec: csdef.AudioCodecOpus, ChannelCount: 2, SampleRate: 48000, BitDepth: 24}); err != nil || id != 2 {
		t.Fatalf("a stereo stream = %d, %v; want stream 2", id, err)
	}
	_, err := camAudioPort{c}.AudioStreamAllocate(ctx, csdef.AudioStreamStruct{AudioCodec: csdef.AudioCodecAacLc, ChannelCount: 1, SampleRate: 48000, BitDepth: 24})
	wantStatus(t, "an AAC stream", err, im.StatusDynamicConstraintError)
	// Mirrors chip examples/camera-app/linux/src/clusters/camera-avstream-mgmt/camera-av-stream-manager.cpp:526-544 at 6170af84.
	wantStatus(t, "AudioStreamDeallocate", camAudioPort{c}.AudioStreamDeallocate(ctx, 2), im.StatusSuccess)
	wantStatus(t, "a second AudioStreamDeallocate", camAudioPort{c}.AudioStreamDeallocate(ctx, 2), im.StatusNotFound)

	// Mirrors chip examples/camera-app/linux/src/clusters/camera-avstream-mgmt/camera-av-stream-manager.cpp:546-600 at 6170af84:
	// a compatible request reuses or makes a stream (AllocateSnapshotStream:
	// the next id after the camera-app's stream 0), an incompatible one is
	// DYNAMIC_CONSTRAINT_ERROR.
	off := false
	snap := csdef.SnapshotStreamAllocateRequest{
		WatermarkEnabled: &off, OsdEnabled: &off,
		ImageCodec: csdef.ImageCodecJpeg, MaxFrameRate: camSnapshotFrameRate, Quality: 90,
		MinResolution: csdef.VideoResolutionStruct{Width: 640, Height: 480}, MaxResolution: csdef.VideoResolutionStruct{Width: 640, Height: 480},
	}
	resp, status = simInvoke(avsm, csdef.CmdSnapshotStreamAllocate, snap)
	if status != im.StatusSuccess || resp.(csdef.SnapshotStreamAllocateResponse).SnapshotStreamId != 1 {
		t.Fatalf("SnapshotStreamAllocate = %v, %v; want stream 1", resp, status)
	}
	if resp, status := simInvoke(avsm, csdef.CmdSnapshotStreamAllocate, snap); status != im.StatusSuccess || resp.(csdef.SnapshotStreamAllocateResponse).SnapshotStreamId != 1 {
		t.Fatalf("the same SnapshotStreamAllocate = %v, %v; want stream 1 reused", resp, status)
	}
	slow := csdef.SnapshotStreamStruct{ImageCodec: csdef.ImageCodecJpeg, FrameRate: 10, MinResolution: snap.MinResolution, MaxResolution: snap.MaxResolution}
	_, err = camSnapshotPort{c}.SnapshotStreamAllocate(ctx, slow)
	wantStatus(t, "a 10 fps snapshot stream", err, im.StatusDynamicConstraintError)
	// Mirrors chip examples/camera-app/linux/src/clusters/camera-avstream-mgmt/camera-av-stream-manager.cpp:578-593 at 6170af84:
	// encoded pixels from a hardware encoder count against the camera's
	// one encoder.
	encoded := csdef.SnapshotStreamStruct{
		ImageCodec: csdef.ImageCodecJpeg, FrameRate: camSnapshotFrameRate, EncodedPixels: true, HardwareEncoder: true,
		MinResolution: csdef.VideoResolutionStruct{Width: 1280, Height: 720}, MaxResolution: csdef.VideoResolutionStruct{Width: 1920, Height: 1080},
	}
	if id, err := (camSnapshotPort{c}).SnapshotStreamAllocate(ctx, encoded); err != nil || id != 2 {
		t.Fatalf("an encoded snapshot stream = %d, %v; want stream 2", id, err)
	}

	// Mirrors chip examples/camera-app/linux/src/clusters/camera-avstream-mgmt/camera-av-stream-manager.cpp:627-677 at 6170af84:
	// modify and deallocate find only an allocated stream.
	wantStatus(t, "SnapshotStreamModify", camSnapshotPort{c}.SnapshotStreamModify(ctx, 1, nil, nil), im.StatusSuccess)
	wantStatus(t, "SnapshotStreamModify of stream 0", camSnapshotPort{c}.SnapshotStreamModify(ctx, 0, nil, nil), im.StatusNotFound)
	wantStatus(t, "SnapshotStreamDeallocate", camSnapshotPort{c}.SnapshotStreamDeallocate(ctx, 1), im.StatusSuccess)
	wantStatus(t, "a second SnapshotStreamDeallocate", camSnapshotPort{c}.SnapshotStreamDeallocate(ctx, 1), im.StatusNotFound)
}

// TestCameraSimRestoresItsAllocations: a kept allocation is the available
// stream's again after a restart (camera.go build).
func TestCameraSimRestoresItsAllocations(t *testing.T) {
	t.Parallel()
	kept := csdef.Patch{
		AllocatedVideoStreams:    &[]csdef.VideoStreamStruct{camVideo(2, cam60fps, camMaxVideoFrameRate, cam720Width, cam720Height, camMaxWidth, camMaxHeight)},
		AllocatedAudioStreams:    &[]csdef.AudioStreamStruct{{AudioStreamId: 3, StreamUsage: csdef.StreamUsageLiveView, AudioCodec: csdef.AudioCodecOpus, ChannelCount: 8, SampleRate: 48000, BitRate: 64000, BitDepth: 24}},
		AllocatedSnapshotStreams: &[]csdef.SnapshotStreamStruct{{SnapshotStreamId: 4, ImageCodec: csdef.ImageCodecJpeg, FrameRate: 30, MinResolution: csdef.VideoResolutionStruct{Width: 640, Height: 480}, MaxResolution: csdef.VideoResolutionStruct{Width: 640, Height: 480}, Quality: 90}},
	}
	raw, err := json.Marshal(kept)
	if err != nil {
		t.Fatal(err)
	}
	c, _, _ := simCamera(t, cameraOptions{}, func(c *demoCamera) { c.restored.AvStream = raw })
	if !c.videos[1].allocated || c.videos[0].allocated || !c.audios[2].allocated || !c.snapshots[4] {
		t.Fatalf("restored allocations: videos %v %v, audio 3 %v, snapshot 4 %v", c.videos[0].allocated, c.videos[1].allocated, c.audios[2].allocated, c.snapshots[4])
	}
	// The next snapshot stream takes the id after the kept one.
	if id, err := (camSnapshotPort{c}).SnapshotStreamAllocate(context.Background(), csdef.SnapshotStreamStruct{
		ImageCodec: csdef.ImageCodecJpeg, FrameRate: 30, MinResolution: csdef.VideoResolutionStruct{Width: 1280, Height: 720}, MaxResolution: csdef.VideoResolutionStruct{Width: 1280, Height: 720},
	}); err != nil || id != 5 {
		t.Fatalf("a new snapshot stream = %d, %v; want 5", id, err)
	}
}

// simSessions is how many WebRTC sessions the provider manager holds.
func simSessions(c *demoCamera) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.sessions)
}

// TestCameraSimWebRTCProvider: the provider manager's sessions and their
// capacity.
func TestCameraSimWebRTCProvider(t *testing.T) {
	t.Parallel()
	c, _, _ := simCamera(t, cameraOptions{deferredOffer: true})
	port := camProviderPort{c}
	ctx := context.Background()
	on := true
	args := func(id uint16) wpdef.OfferArgs {
		return wpdef.OfferArgs{
			SessionID: id, PeerNodeID: 0x55, OriginatingEndpointID: 2, StreamUsage: wpdef.StreamUsageLiveView, FabricIndex: 1,
			VideoStreams: &[]uint16{1, 2}, AudioStreams: &[]uint16{1}, MetadataEnabled: &on,
		}
	}

	// Mirrors chip examples/camera-app/linux/src/clusters/webrtc-provider/webrtc-provider-manager.cpp:220 at 6170af84:
	// DeferredOffer is the --camera-deferred-offer switch; the session
	// carries the request's peer and streams (:230, :246-259).
	s, deferred, err := port.SolicitOffer(ctx, args(1))
	if err != nil || !deferred {
		t.Fatalf("SolicitOffer = deferred %v, %v; want a deferred offer", deferred, err)
	}
	if s.Id != 1 || s.PeerNodeId != 0x55 || s.PeerEndpointId != 2 || s.FabricIndex != 1 || !s.MetadataEnabled ||
		s.VideoStreamId == nil || s.VideoStreamId.Value != 1 || s.AudioStreamId == nil || s.AudioStreamId.Value != 1 ||
		s.VideoStreams == nil || len(*s.VideoStreams) != 2 || s.AudioStreams == nil || len(*s.AudioStreams) != 1 {
		t.Fatalf("the solicited session = %+v", s)
	}
	bare, _, err := port.SolicitOffer(ctx, wpdef.OfferArgs{SessionID: 2, StreamUsage: wpdef.StreamUsageLiveView})
	if err != nil || bare.VideoStreamId == nil || !bare.VideoStreamId.Null || bare.VideoStreams != nil {
		t.Fatalf("a session without streams = %+v, %v; want null stream ids", bare, err)
	}
	for id := uint16(3); id <= camMaxWebRTCSessions; id++ {
		if _, err := port.ProvideOffer(ctx, args(id)); err != nil {
			t.Fatalf("ProvideOffer %d: %v", id, err)
		}
	}

	// Mirrors chip examples/camera-app/linux/src/clusters/webrtc-provider/webrtc-provider-manager.cpp:470-477 at 6170af84:
	// a sixth session through ProvideOffer is RESOURCE_EXHAUSTED.
	_, err = port.ProvideOffer(ctx, args(6))
	wantStatus(t, "a sixth ProvideOffer", err, im.StatusResourceExhausted)
	if n := simSessions(c); n != camMaxWebRTCSessions {
		t.Fatalf("sessions after the refused offer = %d, want %d", n, camMaxWebRTCSessions)
	}

	// Mirrors chip examples/camera-app/linux/src/clusters/webrtc-provider/webrtc-provider-manager.cpp:279-292 at 6170af84:
	// a sixth SolicitOffer still makes its session, then ends it
	// OutOfResources.
	if s, _, err := port.SolicitOffer(ctx, args(7)); err != nil || s.Id != 7 {
		t.Fatalf("a sixth SolicitOffer = %+v, %v; want the session made", s, err)
	}
	waitFor(t, "the over-capacity session ended", func() bool { return simSessions(c) == camMaxWebRTCSessions })

	if err := port.ProvideAnswer(ctx, 1, "v=0"); err != nil {
		t.Fatal(err)
	}
	if err := port.ProvideIceCandidates(ctx, 1, nil); err != nil {
		t.Fatal(err)
	}
	if err := port.EndSession(ctx, 1, 2); err != nil || simSessions(c) != camMaxWebRTCSessions-1 {
		t.Fatalf("EndSession = %v, %d sessions left", err, simSessions(c))
	}
}

// waitFor waits for cond, which goroutines the step started make true.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for !cond() {
		if ctx.Err() != nil {
			t.Fatalf("%s: timed out", what)
		}
		runtime.Gosched()
	}
}

// simConnections are three kept push transports of fabric 1: 1 continuous,
// 2 motion-triggered, 3 command-triggered, all CMAF and Inactive.
func simConnections() json.RawMessage {
	conn := func(id uint16, trigger padef.TransportTriggerOptionsStruct) padef.TransportConfigurationStruct {
		return padef.TransportConfigurationStruct{
			ConnectionId: id, TransportStatus: padef.TransportStatusInactive, FabricIndex: 1,
			TransportOptions: &padef.TransportOptionsStruct{
				StreamUsage: padef.StreamUsageRecording, TlsEndpointId: 1, Url: "https://camera.example/push/",
				TriggerOptions: trigger, IngestMethod: padef.IngestMethodsCmafIngest,
				ContainerOptions: padef.ContainerOptionsStruct{ContainerType: padef.ContainerFormatCmaf},
			},
		}
	}
	motion := padef.TransportTriggerOptionsStruct{
		TriggerType:       padef.TransportTriggerTypeMotion,
		MotionTimeControl: &padef.TransportMotionTriggerTimeControlStruct{InitialDuration: 4, AugmentationDuration: 4, MaxDuration: 7, BlindDuration: 3},
	}
	raw, _ := json.Marshal(padef.Patch{CurrentConnections: &[]padef.TransportConfigurationStruct{
		conn(1, padef.TransportTriggerOptionsStruct{TriggerType: padef.TransportTriggerTypeContinuous}),
		conn(2, motion),
		conn(3, padef.TransportTriggerOptionsStruct{TriggerType: padef.TransportTriggerTypeCommand}),
	}})
	return raw
}

// wantPush asserts the next event is the push transport's begin (with its
// CMAF session) or end of connection id.
func wantPush(t *testing.T, rec *simEvents, event uint32, id uint16, session uint64) simEvent {
	t.Helper()
	e := rec.next(t)
	if e.cluster != padef.ClusterID || e.event != event {
		t.Fatalf("event 0x%02X of cluster 0x%04X, want 0x%02X of PushAvStreamTransport", e.event, e.cluster, event)
	}
	var gotID uint16
	var number *uint64
	switch d := e.data.(type) {
	case padef.PushTransportBeginEvent:
		gotID, number = d.ConnectionId, d.CmafSessionNumber
	case padef.PushTransportEndEvent:
		gotID, number = d.ConnectionId, d.CmafSessionNumber
	}
	if gotID != id || number == nil || *number != session {
		t.Fatalf("event %+v, want connection %d in CMAF session %d", e.data, id, session)
	}
	return e
}

// TestCameraSimPushTransportLifecycle: a continuous transport records while
// Active, a command-triggered one for its InitialDuration, and no Active
// while a privacy mode is on.
func TestCameraSimPushTransportLifecycle(t *testing.T) {
	t.Parallel()
	c, fake, rec := simCamera(t, cameraOptions{}, func(c *demoCamera) { c.restored.Push = simConnections() })
	port := camPushPort{c}
	ctx := context.Background()

	// Mirrors chip examples/camera-app/linux/src/pushav-transport/pushav-transport.cpp:610-615 at 6170af84:
	// a continuous transport set Active starts recording at once
	// (StartRecordingAndStreaming, :410-427: PushTransportBegin), each
	// begin a new CMAF session.
	if err := port.SetTransportStatus(ctx, []uint16{1, 9}, padef.TransportStatusActive); err != nil {
		t.Fatal(err)
	}
	if b := wantPush(t, rec, padef.EventPushTransportBegin, 1, 1).data.(padef.PushTransportBeginEvent); b.TriggerType != padef.TransportTriggerTypeContinuous || b.ActivationReason != nil {
		t.Fatalf("the continuous begin = %+v", b)
	}
	if err := port.SetTransportStatus(ctx, []uint16{1}, padef.TransportStatusActive); err != nil {
		t.Fatal(err)
	}
	rec.none(t)
	if err := port.SetTransportStatus(ctx, []uint16{1}, padef.TransportStatusInactive); err != nil {
		t.Fatal(err)
	}
	wantPush(t, rec, padef.EventPushTransportEnd, 1, 1)
	if err := port.SetTransportStatus(ctx, []uint16{1}, padef.TransportStatusActive); err != nil {
		t.Fatal(err)
	}
	wantPush(t, rec, padef.EventPushTransportBegin, 1, 2)
	if n, ok := port.CMAFSessionNumber(1); !ok || n != 2 {
		t.Fatalf("CMAFSessionNumber(1) = %d, %v; want 2", n, ok)
	}
	if _, ok := port.CMAFSessionNumber(9); ok {
		t.Fatal("an unknown connection has a CMAF session")
	}

	// Mirrors chip examples/camera-app/linux/src/clusters/push-av-stream-transport/push-av-stream-manager.cpp:255-272 at 6170af84:
	// no Active while a privacy mode is on (INVALID_IN_STATE).
	f := &fleet{camera: c}
	if err := f.applyCameraPipeCommand(`{"Name":"SetHardPrivacyModeOn","Value":true}`); err != nil {
		t.Fatal(err)
	}
	if !c.camera.CameraAvStreamManagement().Get().HardPrivacyModeOn {
		t.Fatal("HardPrivacyModeOn is off after the app-pipe command")
	}
	wantStatus(t, "Active under hard privacy", port.SetTransportStatus(ctx, []uint16{3}, padef.TransportStatusActive), im.StatusInvalidInState)
	if err := f.applyCameraPipeCommand(`{"Name":"SetHardPrivacyModeOn","Value":false}`); err != nil {
		t.Fatal(err)
	}

	// A command-triggered transport records only on ManuallyTriggerTransport
	// while Active (push-av-stream-manager.cpp:287-315 at 6170af84), for
	// its InitialDuration.
	userInitiated := padef.TriggerActivationReasonUserInitiated
	if err := port.ManuallyTriggerTransport(ctx, 3, userInitiated, nil); err != nil {
		t.Fatal(err)
	}
	rec.none(t)
	if err := port.SetTransportStatus(ctx, []uint16{3}, padef.TransportStatusActive); err != nil {
		t.Fatal(err)
	}
	rec.none(t)
	if err := port.ManuallyTriggerTransport(ctx, 3, userInitiated, &padef.TransportMotionTriggerTimeControlStruct{InitialDuration: 5, MaxDuration: 8}); err != nil {
		t.Fatal(err)
	}
	if b := wantPush(t, rec, padef.EventPushTransportBegin, 3, 1).data.(padef.PushTransportBeginEvent); b.ActivationReason == nil || *b.ActivationReason != userInitiated {
		t.Fatalf("the manual begin = %+v, want reason UserInitiated", b)
	}
	fake.Advance(4 * time.Second)
	rec.none(t)
	fake.Advance(time.Second)
	wantPush(t, rec, padef.EventPushTransportEnd, 3, 1)
	wantStatus(t, "ManuallyTriggerTransport of an unknown connection", port.ManuallyTriggerTransport(ctx, 9, userInitiated, nil), im.StatusNotFound)

	// The rest of the port: nothing uploads, nothing is limited.
	if port.TransportBusy(1) {
		t.Fatal("a transport is busy")
	}
	if err := port.ValidateBandwidthLimit(ctx, padef.StreamUsageRecording, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := port.AllocatePushTransport(ctx, 4, 1, padef.TransportOptionsStruct{TriggerOptions: padef.TransportTriggerOptionsStruct{TriggerType: padef.TransportTriggerTypeCommand}}, padef.TLSCredentials{}); err != nil {
		t.Fatal(err)
	}
	if err := port.ModifyPushTransport(ctx, 4, padef.TransportOptionsStruct{TriggerOptions: padef.TransportTriggerOptionsStruct{TriggerType: padef.TransportTriggerTypeContinuous}}); err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	got := c.transports[4].trigger.TriggerType
	c.mu.Unlock()
	if got != padef.TransportTriggerTypeContinuous {
		t.Fatalf("the modified trigger = %v", got)
	}
	if err := port.DeallocatePushTransport(ctx, 4); err != nil {
		t.Fatal(err)
	}
	if err := port.DeallocatePushTransport(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if _, ok := port.CMAFSessionNumber(4); ok {
		t.Fatal("a deallocated transport keeps its CMAF session")
	}
}

// TestCameraSimMotionTransport: a ZoneTriggered motion starts the Active
// motion transports' recording for the InitialDuration, augmented up to the
// MaxDuration, then a blind period.
func TestCameraSimMotionTransport(t *testing.T) {
	t.Parallel()
	c, fake, rec := simCamera(t, cameraOptions{}, func(c *demoCamera) { c.restored.Push = simConnections() })
	port := camPushPort{c}
	ctx := context.Background()
	f := &fleet{camera: c}
	automation := padef.TriggerActivationReasonAutomation

	// Mirrors chip examples/camera-app/linux/src/pushav-transport/pushav-transport.cpp:616-641 at 6170af84:
	// a motion before the transport went Active starts its recording for
	// the rest of the detected duration.
	c.motionTriggered()
	rec.none(t)
	fake.Advance(time.Second)
	if err := port.SetTransportStatus(ctx, []uint16{2}, padef.TransportStatusActive); err != nil {
		t.Fatal(err)
	}
	if b := wantPush(t, rec, padef.EventPushTransportBegin, 2, 1).data.(padef.PushTransportBeginEvent); b.ActivationReason == nil || *b.ActivationReason != automation {
		t.Fatalf("the motion begin = %+v, want reason Automation", b)
	}
	fake.Advance(2 * time.Second)
	rec.none(t)
	fake.Advance(time.Second)
	wantPush(t, rec, padef.EventPushTransportEnd, 2, 1)

	// Mirrors chip examples/camera-app/linux/src/pushav-transport/pushav-transport.cpp:330-354 at 6170af84:
	// a motion in the blind period after the recording is ignored.
	fake.Advance(time.Second / 2)
	if err := f.applyCameraPipeCommand(`{"Name":"ZoneTriggered","ZoneId":[]}`); err != nil {
		t.Fatal(err)
	}
	rec.none(t)

	// Mirrors chip examples/camera-app/linux/src/pushav-transport/pushav-transport.cpp:356-392 at 6170af84:
	// after the blind period a motion starts a new recording, a second
	// one within it augments the recording, bounded by the maximum clip
	// duration (4 s + 4 s, at most 7 s).
	fake.Advance(3 * time.Second)
	c.motionTriggered()
	wantPush(t, rec, padef.EventPushTransportBegin, 2, 2)
	fake.Advance(time.Second)
	c.motionTriggered()
	fake.Advance(5 * time.Second)
	rec.none(t)
	fake.Advance(time.Second)
	wantPush(t, rec, padef.EventPushTransportEnd, 2, 2)

	// Set Inactive with an expired motion: nothing records once it is
	// Active again (pushav-transport.cpp:629-633).
	if err := port.SetTransportStatus(ctx, []uint16{2}, padef.TransportStatusInactive); err != nil {
		t.Fatal(err)
	}
	fake.Advance(time.Minute)
	if err := port.SetTransportStatus(ctx, []uint16{2}, padef.TransportStatusActive); err != nil {
		t.Fatal(err)
	}
	rec.none(t)
}

// TestCameraSimMechanicalPTZ: a mechanical move completes two seconds
// after it starts.
func TestCameraSimMechanicalPTZ(t *testing.T) {
	t.Parallel()
	c, fake, _ := simCamera(t, cameraOptions{})
	avsum := c.camera.CameraAvSettingsUserLevelManagement()
	ctx := context.Background()
	pan, tilt, zoom := int16(10), int16(-20), uint8(5)

	// Mirrors chip examples/camera-app/linux/src/clusters/camera-avsettingsuserlevel-mgmt/camera-avsettingsuserlevel-manager.cpp:70-76 at 6170af84:
	// the move starts, a two-second timer mimics the movement, and its
	// expiry completes it (onTimerExpiry, :298-306).
	if _, status := simInvoke(avsum.Underlying(), avdef.CmdMptzSetPosition, avdef.MptzSetPositionRequest{Pan: &pan, Tilt: &tilt, Zoom: &zoom}); status != im.StatusSuccess {
		t.Fatalf("MPTZSetPosition: %v", status)
	}
	if got := avsum.Get().MovementState; got != avdef.PhysicalMovementMoving {
		t.Fatalf("MovementState after MPTZSetPosition = %v, want Moving", got)
	}
	fake.Advance(camMoveDuration - time.Millisecond)
	if got := avsum.Get().MovementState; got != avdef.PhysicalMovementMoving {
		t.Fatalf("MovementState before two seconds = %v, want Moving", got)
	}
	fake.Advance(time.Millisecond)
	st := avsum.Get()
	if st.MovementState != avdef.PhysicalMovementIdle || st.MptzPosition.Pan == nil || *st.MptzPosition.Pan != pan ||
		*st.MptzPosition.Tilt != tilt || *st.MptzPosition.Zoom != zoom {
		t.Fatalf("after two seconds: MovementState %v, MPTZPosition %+v", st.MovementState, st.MptzPosition)
	}

	// Mirrors chip examples/camera-app/linux/src/clusters/camera-avsettingsuserlevel-mgmt/camera-avsettingsuserlevel-manager.cpp:79-129 at 6170af84:
	// the relative move and the move to a preset run the same timer; a
	// preset save and removal take effect at once.
	ptz := camPTZ{c}
	if err := ptz.MptzRelativeMove(ctx, avdef.MPTZStruct{Pan: &pan}); err != nil {
		t.Fatal(err)
	}
	if err := ptz.MptzMoveToPreset(ctx, 1, avdef.MPTZStruct{Pan: &pan}); err != nil {
		t.Fatal(err)
	}
	if n := fake.Pending(); n != 2 {
		t.Fatalf("%d movement timers pending, want 2", n)
	}
	fake.Advance(camMoveDuration)
	if err := ptz.MptzSavePreset(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if err := ptz.MptzRemovePreset(ctx, 1); err != nil {
		t.Fatal(err)
	}
}

// TestCameraSimDigitalPTZ: the viewport of an allocated stream.
func TestCameraSimDigitalPTZ(t *testing.T) {
	t.Parallel()
	c, _, _ := simCamera(t, cameraOptions{})
	ctx := context.Background()
	if _, status := simInvoke(c.camera.CameraAvStreamManagement().Underlying(), csdef.CmdVideoStreamAllocate, simVideoRequest()); status != im.StatusSuccess {
		t.Fatalf("VideoStreamAllocate: %v", status)
	}
	ptz := camPTZ{c}

	// Mirrors chip examples/camera-app/linux/src/clusters/camera-avsettingsuserlevel-mgmt/camera-avsettingsuserlevel-manager.cpp:131-180 at 6170af84:
	// no smaller than the minimum viewport, no larger than the sensor, at
	// the stream's aspect ratio to two places; an unallocated stream is
	// NOT_FOUND.
	wantStatus(t, "a 100x100 viewport", ptz.DptzSetViewport(ctx, 1, avdef.ViewportStruct{X2: 100, Y2: 100}), im.StatusConstraintError)
	wantStatus(t, "a 2000x1200 viewport", ptz.DptzSetViewport(ctx, 1, avdef.ViewportStruct{X2: 2000, Y2: 1200}), im.StatusConstraintError)
	wantStatus(t, "a 4:3 viewport on a 16:9 stream", ptz.DptzSetViewport(ctx, 1, avdef.ViewportStruct{X2: 640, Y2: 480}), im.StatusConstraintError)
	wantStatus(t, "the viewport of stream 2", ptz.DptzSetViewport(ctx, 2, avdef.ViewportStruct{X2: 1280, Y2: 720}), im.StatusNotFound)
	if _, status := simInvoke(c.camera.CameraAvSettingsUserLevelManagement().Underlying(), avdef.CmdDptzSetViewport,
		avdef.DptzSetViewportRequest{VideoStreamId: 1, Viewport: avdef.ViewportStruct{X2: 1280, Y2: 720}}); status != im.StatusSuccess {
		t.Fatalf("DPTZSetViewport 1280x720: %v", status)
	}

	// Mirrors chip examples/camera-app/linux/src/clusters/camera-avsettingsuserlevel-mgmt/camera-avsettingsuserlevel-manager.cpp:182-290 at 6170af84,
	// in its uint16 arithmetic: a move, each edge clamped at the left and
	// top of the sensor (:198-229); a zoom out that scales the area at the
	// viewport's aspect ratio to two places and is then raised to the
	// minimum viewport from x1, y1 (:231-271); a zoom in past the sensor
	// that resets the viewport to the sensor (:273-281).
	i16 := func(v int16) *int16 { return &v }
	i8 := func(v int8) *int8 { return &v }
	steps := []struct {
		name string
		req  avdef.DptzRelativeMoveRequest
		want avdef.ViewportStruct
	}{
		{"a move right and down", avdef.DptzRelativeMoveRequest{DeltaX: i16(100), DeltaY: i16(50)}, avdef.ViewportStruct{X1: 100, Y1: 50, X2: 1380, Y2: 770}},
		{"a move past the left edge", avdef.DptzRelativeMoveRequest{DeltaX: i16(-200), DeltaY: i16(0)}, avdef.ViewportStruct{X1: 0, Y1: 50, X2: 1180, Y2: 770}},
		{"a 50 % zoom out", avdef.DptzRelativeMoveRequest{ZoomDelta: i8(-50)}, avdef.ViewportStruct{X1: 348, Y1: 260, X2: 988, Y2: 740}},
		{"a 100 % zoom in", avdef.DptzRelativeMoveRequest{ZoomDelta: i8(100)}, avdef.ViewportStruct{X1: 84, Y1: 61, X2: 1252, Y2: 939}},
	}
	for _, s := range steps {
		s.req.VideoStreamId = 1
		got, err := ptz.DptzRelativeMove(ctx, s.req)
		if err != nil || got != s.want {
			t.Fatalf("%s: %+v, %v; want %+v", s.name, got, err, s.want)
		}
	}
	if err := ptz.DptzSetViewport(ctx, 1, avdef.ViewportStruct{X1: 600, Y1: 400, X2: 1880, Y2: 1120}); err != nil {
		t.Fatal(err)
	}
	if got, err := ptz.DptzRelativeMove(ctx, avdef.DptzRelativeMoveRequest{VideoStreamId: 1, ZoomDelta: i8(100)}); err != nil ||
		got != (avdef.ViewportStruct{X2: camMaxWidth - 1, Y2: camMaxHeight - 1}) {
		t.Fatalf("a zoom in past the sensor: %+v, %v; want the sensor", got, err)
	}
	_, err := ptz.DptzRelativeMove(ctx, avdef.DptzRelativeMoveRequest{VideoStreamId: 2, DeltaX: i16(1)})
	wantStatus(t, "a relative move of stream 2", err, im.StatusNotFound)
}

// simZone creates a triangle zone through the server.
func simZone(t *testing.T, c *demoCamera, offset uint16) uint16 {
	t.Helper()
	zone := zmdef.TwoDCartesianZoneStruct{Name: "zone", Use: zmdef.ZoneUseMotion, Vertices: []zmdef.TwoDCartesianVertexStruct{
		{X: 10 + offset, Y: 20 + offset}, {X: 30 + offset, Y: 20 + offset}, {X: 20 + offset, Y: 40 + offset},
	}}
	resp, status := simInvoke(c.camera.ZoneManagement().Underlying(), zmdef.CmdCreateTwoDCartesianZone, zmdef.CreateTwoDCartesianZoneRequest{Zone: zone})
	if status != im.StatusSuccess {
		t.Fatalf("CreateTwoDCartesianZone: %v", status)
	}
	return resp.(zmdef.CreateTwoDCartesianZoneResponse).ZoneId
}

// wantZone asserts the next event is the zone management's event for
// zone with reason.
func wantZone(t *testing.T, rec *simEvents, event uint32, zone uint16, reason uint8) {
	t.Helper()
	e := rec.next(t)
	if e.cluster != zmdef.ClusterID || e.event != event {
		t.Fatalf("event 0x%02X of cluster 0x%04X, want 0x%02X of ZoneManagement", e.event, e.cluster, event)
	}
	switch d := e.data.(type) {
	case zmdef.ZoneTriggeredEvent:
		if d.Zone == zone && uint8(d.Reason) == reason {
			return
		}
	case zmdef.ZoneStoppedEvent:
		if d.Zone == zone && uint8(d.Reason) == reason {
			return
		}
	}
	t.Fatalf("event %+v, want zone %d with reason %d", e.data, zone, reason)
}

// TestCameraSimZoneTriggers: the zone manager's ids and its trigger
// timing through the app pipe's ZoneTriggered.
func TestCameraSimZoneTriggers(t *testing.T) {
	t.Parallel()
	c, fake, rec := simCamera(t, cameraOptions{})
	zones := c.camera.ZoneManagement().Underlying()
	f := &fleet{camera: c}
	ctx := context.Background()

	// Mirrors chip examples/camera-app/linux/include/clusters/zone-mgmt/zone-manager.h:105-125 at 6170af84:
	// a new zone takes the lowest free id from 1.
	one, two := simZone(t, c, 0), simZone(t, c, 100)
	if one != 1 || two != 2 {
		t.Fatalf("the first zones = %d, %d; want 1, 2", one, two)
	}
	if _, status := simInvoke(zones, zmdef.CmdRemoveZone, zmdef.RemoveZoneRequest{ZoneId: 1}); status != im.StatusSuccess {
		t.Fatalf("RemoveZone: %v", status)
	}
	if again := simZone(t, c, 200); again != 1 {
		t.Fatalf("the zone after removing 1 = %d, want 1 again", again)
	}
	// Mirrors chip examples/camera-app/linux/src/clusters/zone-mgmt/zone-manager.cpp:56-72 at 6170af84.
	wantStatus(t, "UpdateTwoDCartesianZone of zone 9", camZonePort{c}.UpdateTwoDCartesianZone(ctx, 9, zmdef.TwoDCartesianZoneStruct{}), im.StatusNotFound)
	if err := (camZonePort{c}).UpdateTwoDCartesianZone(ctx, 2, zmdef.TwoDCartesianZoneStruct{}); err != nil {
		t.Fatal(err)
	}

	sens := uint8(5)
	for _, trig := range []zmdef.ZoneTriggerControlStruct{
		{ZoneId: 1, InitialDuration: 3, AugmentationDuration: 3, MaxDuration: 4, BlindDuration: 2, Sensitivity: &sens},
		{ZoneId: 2, InitialDuration: 2, AugmentationDuration: 0, MaxDuration: 10, BlindDuration: 1, Sensitivity: &sens},
	} {
		if _, status := simInvoke(zones, zmdef.CmdCreateOrUpdateTrigger, zmdef.CreateOrUpdateTriggerRequest{Trigger: trig}); status != im.StatusSuccess {
			t.Fatalf("CreateOrUpdateTrigger %d: %v", trig.ZoneId, status)
		}
	}
	motion, actionStopped, timeout := uint8(zmdef.ZoneEventTriggeredReasonMotion), uint8(zmdef.ZoneEventStoppedReasonActionStopped), uint8(zmdef.ZoneEventStoppedReasonTimeout)

	// Mirrors chip examples/camera-app/linux/src/clusters/zone-mgmt/zone-manager.cpp:241-261 at 6170af84:
	// a zone's first trigger is its ZoneTriggered event; a second one
	// advances the detected duration by the augmentation (:268-282) and
	// emits nothing.
	if err := f.applyCameraPipeCommand(`{"Name":"ZoneTriggered","ZoneId":[1,2]}`); err != nil {
		t.Fatal(err)
	}
	wantZone(t, rec, zmdef.EventZoneTriggered, 1, motion)
	wantZone(t, rec, zmdef.EventZoneTriggered, 2, motion)
	if err := f.applyCameraPipeCommand(`{"Name":"ZoneTriggered","ZoneId":1}`); err != nil {
		t.Fatal(err)
	}
	rec.none(t)

	// Mirrors chip examples/camera-app/linux/src/clusters/zone-mgmt/zone-manager.cpp:194-206 at 6170af84:
	// a zone stops ActionStopped once its detected duration has elapsed —
	// here at TriggerDetectedDuration, a second before the camera-app's
	// `>` (BD-chip-CameraApp-Simulation).
	fake.Advance(time.Second)
	rec.none(t)
	fake.Advance(time.Second)
	wantZone(t, rec, zmdef.EventZoneStopped, 2, actionStopped)
	// Mirrors chip examples/camera-app/linux/src/clusters/zone-mgmt/zone-manager.cpp:264-266 at 6170af84:
	// a trigger in the blind duration is ignored.
	if err := f.applyCameraPipeCommand(`{"Name":"ZoneTriggered","ZoneId":2}`); err != nil {
		t.Fatal(err)
	}
	rec.none(t)
	// Mirrors chip examples/camera-app/linux/src/clusters/zone-mgmt/zone-manager.cpp:207-216 at 6170af84:
	// zone 1, augmented to six seconds, stops Timeout past its four-second
	// MaxDuration.
	fake.Advance(2 * time.Second)
	rec.none(t)
	fake.Advance(time.Second)
	wantZone(t, rec, zmdef.EventZoneStopped, 1, timeout)

	// Mirrors chip examples/camera-app/linux/src/clusters/zone-mgmt/zone-manager.cpp:176-191, :222-227 at 6170af84:
	// a context is dropped when its blind duration has run out, and the
	// timer stops with the last one.
	fake.Advance(2 * time.Second)
	c.mu.Lock()
	contexts, timer := len(c.triggers), c.timer
	c.mu.Unlock()
	if contexts != 0 || timer != nil || fake.Pending() != 0 {
		t.Fatalf("after the blind durations: %d contexts, timer %v, %d pending", contexts, timer, fake.Pending())
	}
	if err := f.applyCameraPipeCommand(`{"Name":"ZoneTriggered","ZoneId":[2]}`); err != nil {
		t.Fatal(err)
	}
	wantZone(t, rec, zmdef.EventZoneTriggered, 2, motion)

	// Mirrors chip examples/camera-app/linux/src/clusters/zone-mgmt/zone-manager.cpp:233-235 at 6170af84:
	// a zone without a trigger is not triggered.
	if _, status := simInvoke(zones, zmdef.CmdRemoveTrigger, zmdef.RemoveTriggerRequest{ZoneId: 2}); status != im.StatusSuccess {
		t.Fatalf("RemoveTrigger: %v", status)
	}
	c.mu.Lock()
	contexts = len(c.triggers)
	c.mu.Unlock()
	if contexts != 0 {
		t.Fatalf("%d trigger contexts after RemoveTrigger, want none", contexts)
	}
	if err := f.applyCameraPipeCommand(`{"Name":"ZoneTriggered","ZoneId":2}`); err == nil {
		t.Fatal("a zone without a trigger was triggered")
	}
	rec.none(t)
	if err := (camZonePort{c}).UpdateTrigger(ctx, zmdef.ZoneTriggerControlStruct{ZoneId: 1}); err != nil {
		t.Fatal(err)
	}
}

// TestCameraSimChime: PlayChimeSound fails while the chime is disabled.
func TestCameraSimChime(t *testing.T) {
	t.Parallel()
	c, _, _ := simCamera(t, cameraOptions{})
	chime := c.chime.Chime()
	// Mirrors chip examples/camera-app/linux/src/clusters/chime/chime-manager.cpp:52-63 at 6170af84.
	if _, status := simInvoke(chime.Underlying(), chimedef.CmdPlayChimeSound, chimedef.PlayChimeSoundRequest{}); status != im.StatusSuccess {
		t.Fatalf("PlayChimeSound on an enabled chime: %v", status)
	}
	// A disabled chime's PlayChimeSound succeeds in the cluster
	// (ChimeCluster.cpp:263-266) and never reaches the manager, whose own
	// check answers FAILURE.
	off := false
	if err := chime.Set(chimedef.Patch{Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	if _, status := simInvoke(chime.Underlying(), chimedef.CmdPlayChimeSound, chimedef.PlayChimeSoundRequest{}); status != im.StatusSuccess {
		t.Fatalf("PlayChimeSound on a disabled chime: %v, want SUCCESS from the cluster", status)
	}
	wantStatus(t, "the manager's PlayChimeSound while disabled", camChimePort{c}.PlayChimeSound(context.Background(), 0), im.StatusFailure)
	// Mirrors chip examples/camera-app/linux/include/clusters/chime/chime-manager.h:41-45 at 6170af84.
	if got := chime.Get().InstalledChimeSounds; len(got) != 3 || got[2].Name != "Star Wars Door Chime" {
		t.Fatalf("InstalledChimeSounds = %+v", got)
	}
}

// TestCameraSimAppPipeRefusals: the app-pipe commands the camera refuses
// or drops.
func TestCameraSimAppPipeRefusals(t *testing.T) {
	t.Parallel()
	if err := (&fleet{}).applyCameraPipeCommand(`{"Name":"ZoneTriggered","ZoneId":1}`); err == nil {
		t.Fatal("a camera command without --camera-sim was applied")
	}
	c, _, _ := simCamera(t, cameraOptions{})
	f := &fleet{camera: c}
	for _, line := range []string{
		`{"Name":"SetHardPrivacyModeOn"}`,
		`{"Name":"ZoneTriggered","ZoneId":"one"}`,
		`{"Name":"ZoneTriggered"`,
	} {
		if err := f.applyCameraPipeCommand(line); err == nil {
			t.Fatalf("%s was applied", line)
		}
	}
	// The AvAnalysis commands are logged and dropped (no AvAnalysis
	// cluster in the matter.js snapshot; BD-chip-CameraApp-Simulation).
	for _, name := range []string{"AmbientContextTriggered", "AvAnalysisSessionStart", "AvAnalysisPerceivedContext", "AvAnalysisSessionEnd"} {
		if !isCameraPipeCommand(name) {
			t.Fatalf("%s is not a camera command", name)
		}
		if err := f.applyCameraPipeCommand(`{"Name":"` + name + `"}`); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if isCameraPipeCommand("SetOnOff") {
		t.Fatal("SetOnOff is a camera command")
	}
}

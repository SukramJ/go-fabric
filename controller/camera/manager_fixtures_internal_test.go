// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/clock/clocktest"
	"github.com/SukramJ/go-fabric/cluster/spec"
	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
	"github.com/SukramJ/go-fabric/im"
)

// The fixtures of matterjs-server's CameraStreamManagerTest.ts:52-337 and
// cameraFixtures.ts:1-43 at 0bd06f2, ported by meaning: the manager runs
// over a fake deviceIO that records every invoke, a mutable state holder,
// and a fake clock for the lease grace window.

const (
	tNode     = uint64(5)
	tEndpoint = uint16(1)
	h265      = csdef.VideoCodecHevc
	liveView  = csdef.StreamUsageLiveView
)

var tTarget = target{tNode, tEndpoint}

func boolp(b bool) *bool { return &b }

func intp(v int) *int { return &v }

// fixtureState is the fixture's STATE (CameraStreamManagerTest.ts:57-93,
// matterjs-server at 0bd06f2).
func fixtureState() *cameraState {
	hdr := 15
	talk := csdef.TwoWayTalkSupportTypeNotSupported
	return &cameraState{
		features:              csdef.FeatureAudio | csdef.FeatureVideo | csdef.FeatureSnapshot | csdef.FeatureHighDynamicRange,
		maxConcurrentEncoders: stated(1),
		maxEncodedPixelRate:   stated(248832000),
		sensor:                &sensorParams{width: 2560, height: 1440, maxFPS: 30, maxHDRFPS: &hdr, hdrCapable: true},
		minViewport:           &resolution{640, 360},
		rateDistortionPoints:  []rateDistortionPoint{{codec: h265, resolution: resolution{1920, 1080}, minBitRate: 800000}},
		snapshotCapabilities: []snapshotCapability{
			{resolution: resolution{640, 480}, maxFrameRate: 1, imageCodec: csdef.ImageCodecJpeg},
			{resolution: resolution{1920, 1080}, maxFrameRate: 1, imageCodec: csdef.ImageCodecJpeg, requiresEncodedPixels: true, requiresHardwareEncoder: true},
		},
		supportedStreamUsages: []csdef.StreamUsageEnum{liveView, csdef.StreamUsageRecording, csdef.StreamUsageAnalysis},
		streamUsagePriorities: []csdef.StreamUsageEnum{liveView, csdef.StreamUsageRecording, csdef.StreamUsageAnalysis},
		microphone:            &audioCapabilities{supportedCodecs: []csdef.AudioCodecEnum{csdef.AudioCodecOpus}, maxNumberOfChannels: 1, supportedSampleRates: []int{48000}, supportedBitDepths: []int{16}},
		twoWayTalkSupport:     &talk,
	}
}

// with is a copy of s changed by edit.
func (s *cameraState) with(edit func(*cameraState)) *cameraState {
	c := *s
	edit(&c)
	return &c
}

// The offers of CameraStreamManagerTest.ts:102-190, matterjs-server at
// 0bd06f2.
var (
	videoOffer         = "v=0\r\no=- 0 0 IN IP4 127.0.0.1\r\ns=-\r\nt=0 0\r\nm=video 9 UDP/TLS/RTP/SAVPF 96\r\na=recvonly"
	videoAndAudioOffer = videoOffer + "\r\nm=audio 9 UDP/TLS/RTP/SAVPF 111\r\na=recvonly"
	talkbackOffer      = "v=0\r\no=- 0 0 IN IP4 127.0.0.1\r\ns=-\r\nt=0 0\r\nm=video 9 UDP/TLS/RTP/SAVPF 96\r\na=recvonly\r\nm=audio 9 UDP/TLS/RTP/SAVPF 111\r\na=rtpmap:111 opus/48000/2\r\na=sendrecv"
	audioOnlyOffer     = "v=0\r\no=- 0 0 IN IP4 127.0.0.1\r\ns=-\r\nt=0 0\r\nm=audio 9 UDP/TLS/RTP/SAVPF 111\r\na=recvonly"
	videoRefusedOffer  = "v=0\r\no=- 0 0 IN IP4 127.0.0.1\r\ns=-\r\nt=0 0\r\nm=video 0 UDP/TLS/RTP/SAVPF 96\r\na=rtpmap:96 H265/90000\r\nm=audio 9 UDP/TLS/RTP/SAVPF 111\r\na=rtpmap:111 opus/48000/2\r\na=recvonly"
	audioRefusedOffer  = "v=0\r\no=- 0 0 IN IP4 127.0.0.1\r\ns=-\r\nt=0 0\r\nm=video 9 UDP/TLS/RTP/SAVPF 96\r\na=rtpmap:96 H265/90000\r\na=recvonly\r\nm=audio 0 UDP/TLS/RTP/SAVPF 111\r\na=rtpmap:111 opus/48000/2"
	videoInactiveOffer = "v=0\r\no=- 0 0 IN IP4 127.0.0.1\r\ns=-\r\nt=0 0\r\nm=video 9 UDP/TLS/RTP/SAVPF 96\r\na=rtpmap:96 H265/90000\r\na=inactive\r\nm=audio 9 UDP/TLS/RTP/SAVPF 111\r\na=rtpmap:111 opus/48000/2\r\na=recvonly"
	audioSendonlyOffer = "v=0\r\no=- 0 0 IN IP4 127.0.0.1\r\ns=-\r\nt=0 0\r\nm=video 9 UDP/TLS/RTP/SAVPF 96\r\na=rtpmap:96 H265/90000\r\na=recvonly\r\nm=audio 9 UDP/TLS/RTP/SAVPF 111\r\na=rtpmap:111 opus/48000/2\r\na=sendonly"
	_                  = []string{videoAndAudioOffer, talkbackOffer, audioOnlyOffer, videoRefusedOffer, audioRefusedOffer, videoInactiveOffer, audioSendonlyOffer}
)

// statusErr is a device rejection carrying a Matter status
// (CameraStreamManagerTest.ts:95-100, matterjs-server at 0bd06f2).
func statusErr(s im.StatusCode) error { return &interaction.StatusError{Status: s} }

// recordedInvoke is one invoke the fake camera saw.
type recordedInvoke struct {
	command      string
	fields       spec.Encodable
	t            target
	establishing func(uint16)
}

// fakeIO is the managerWith camera (CameraStreamManagerTest.ts:192-269,
// matterjs-server at 0bd06f2): readCameraState answers the holder's state,
// invoke records and asks respond, and an establishing command's id goes
// to the manager after the response, as the real provider path does.
type fakeIO struct {
	mu        sync.Mutex
	state     *cameraState
	sessions  []DeviceSession
	abandoned []uint16
	invokes   []recordedInvoke
	missing   []uint32
	hasMiss   bool
	respond   func(*recordedInvoke) (any, error)
}

func (f *fakeIO) setState(s *cameraState) {
	f.mu.Lock()
	f.state = s
	f.mu.Unlock()
}

func (f *fakeIO) recorded() []recordedInvoke {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recordedInvoke(nil), f.invokes...)
}

// commands is the command names invoked, in order.
func (f *fakeIO) commands() []string {
	rs := f.recorded()
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.command)
	}
	return out
}

func (f *fakeIO) readCameraState(context.Context, target) (*cameraState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state, nil
}

func (f *fakeIO) missingCameraClusters(context.Context, target) []uint32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.hasMiss {
		return f.missing
	}
	if f.state == nil {
		return []uint32{csdef.ClusterID, wpdef.ClusterID}
	}
	return nil
}

func (f *fakeIO) readWebRTCSessions(context.Context, target) ([]DeviceSession, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]DeviceSession{}, f.sessions...), true
}

func (f *fakeIO) invoke(_ context.Context, call deviceCall) (any, error) {
	r := &recordedInvoke{command: call.command, fields: call.fields, t: call.target, establishing: call.establishing}
	f.mu.Lock()
	f.invokes = append(f.invokes, *r)
	respond := f.respond
	f.mu.Unlock()
	var resp any
	var err error
	if respond != nil {
		resp, err = respond(r)
	}
	if err == nil && r.establishing != nil {
		if id, _, _, ok := sessionIDOf(resp); ok {
			r.establishing(id)
		}
	}
	return resp, err
}

func (f *fakeIO) abandonSession(_ context.Context, _ target, id uint16) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.abandoned = append(f.abandoned, id)
	return nil
}

// managerWith is the fixture's managerWith: a manager over the state with
// respond answering every invoke, and its fake clock.
func managerWith(state *cameraState, respond func(*recordedInvoke) (any, error)) (*manager, *fakeIO, *clocktest.Fake) {
	io := &fakeIO{state: state, respond: respond}
	clk := clocktest.New(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	return newManager(io, clk, nil), io, clk
}

// endingsOf records every session end the manager announces
// (CameraStreamManagerTest.ts:271-278, matterjs-server at 0bd06f2).
func endingsOf(m *manager) func() []cameraSessionEnded {
	var mu sync.Mutex
	var seen []cameraSessionEnded
	m.onEnded = func(e cameraSessionEnded) { mu.Lock(); seen = append(seen, e); mu.Unlock() }
	return func() []cameraSessionEnded {
		mu.Lock()
		defer mu.Unlock()
		return append([]cameraSessionEnded(nil), seen...)
	}
}

// vanishedOf records the ids reported vanished
// (CameraStreamManagerTest.ts:280-287, matterjs-server at 0bd06f2).
func vanishedOf(m *manager) func() []uint16 {
	var mu sync.Mutex
	var seen []uint16
	m.onVanished = func(s managedSession, _ sessionEndCause) {
		mu.Lock()
		seen = append(seen, s.webRTCSessionID)
		mu.Unlock()
	}
	return func() []uint16 {
		mu.Lock()
		defer mu.Unlock()
		return append([]uint16(nil), seen...)
	}
}

// allocateVideo answers a VideoStreamAllocate with id.
func allocateVideo(id uint16) func(*recordedInvoke) (any, error) {
	return func(r *recordedInvoke) (any, error) {
		if r.command == cmdVideoStreamAllocate {
			return csdef.VideoStreamAllocateResponse{VideoStreamId: id}, nil
		}
		return nil, nil
	}
}

// liveStream is a reported H.265 live-view stream, 640x360 to 1920x1080,
// 1 to 30 fps, 800 kbit/s to 4 Mbit/s.
func liveStream(id uint16, refs int) allocatedVideoStream {
	return allocatedVideoStream{
		videoStreamID: id, overlays: noOverlays, streamUsage: liveView, videoCodec: h265,
		minResolution: resolution{640, 360}, maxResolution: resolution{1920, 1080},
		minFrameRate: 1, maxFrameRate: 30, minBitRate: 800000, maxBitRate: 4000000,
		keyFrameInterval: 4000, referenceCount: refs,
	}
}

// startDefaults is a ProvideOffer start of a live-view session.
func startDefaults(sdp string) startArgs {
	return startArgs{t: tTarget, connectionID: "conn-1", usage: liveView, sdp: &sdp, allowEviction: true}
}

var _ = intp

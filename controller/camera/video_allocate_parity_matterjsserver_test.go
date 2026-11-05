// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	"github.com/SukramJ/go-fabric/im"
)

// The cases of matterjs-server's
// packages/ws-controller/test/CameraVideoAllocateTest.ts:191-611 at
// 0bd06f2: the video allocate ladder against a fake camera with the fixed
// stream profiles a tested camera showed.

const vaH264 = csdef.VideoCodecH264

// vaFixedState is FIXED_PROFILE_STATE (CameraVideoAllocateTest.ts:30-40,
// matterjs-server at 0bd06f2).
func vaFixedState() *cameraState {
	return fixtureState().with(func(s *cameraState) {
		s.maxConcurrentEncoders, s.maxEncodedPixelRate, s.maxNetworkBandwidth = stated(1), stated(248832000), stated(128000000)
		s.sensor = &sensorParams{width: 1920, height: 1080, maxFPS: 120}
		s.minViewport = &resolution{640, 480}
		s.rateDistortionPoints = []rateDistortionPoint{{codec: vaH264, resolution: resolution{640, 480}, minBitRate: 10000}}
		s.video = nil
	})
}

type vaProfile struct {
	id                       uint16
	minRate, maxRate         int
	minRes, maxRes           resolution
	minBitRate, maxBitRateBR int
}

// vaProfiles is FIXED_PROFILES (CameraVideoAllocateTest.ts:54-73,
// matterjs-server at 0bd06f2).
var vaProfiles = []vaProfile{
	{1, 30, 60, resolution{640, 480}, resolution{1280, 720}, 10000, 2000000},
	{2, 60, 120, resolution{1280, 720}, resolution{1920, 1080}, 10000, 2000000},
	{3, 30, 120, resolution{640, 480}, resolution{1920, 1080}, 10000, 2000000},
}

func vaCompatible(r csdef.VideoStreamAllocateRequest) []vaProfile {
	if r.VideoCodec != vaH264 || r.KeyFrameInterval != 4000 {
		return nil
	}
	var out []vaProfile
	for _, p := range vaProfiles {
		if int(r.MinFrameRate) >= p.minRate && int(r.MaxFrameRate) <= p.maxRate &&
			int(r.MinResolution.Width) >= p.minRes.width && int(r.MinResolution.Height) >= p.minRes.height &&
			int(r.MaxResolution.Width) <= p.maxRes.width && int(r.MaxResolution.Height) <= p.maxRes.height &&
			int(r.MinBitRate) >= p.minBitRate && int(r.MaxBitRate) <= p.maxBitRateBR {
			out = append(out, p)
		}
	}
	return out
}

// vaCamera is fixedProfileCamera (CameraVideoAllocateTest.ts:99-122,
// matterjs-server at 0bd06f2): outside every profile is
// DynamicConstraintError, then a taken encoder or no free profile is
// ResourceExhausted.
func vaCamera(allocated ...uint16) func(*recordedInvoke) (any, error) {
	held := map[uint16]bool{}
	for _, id := range allocated {
		held[id] = true
	}
	return func(r *recordedInvoke) (any, error) {
		switch f := r.fields.(type) {
		case csdef.VideoStreamDeallocateRequest:
			delete(held, f.VideoStreamId)
			return nil, nil
		case csdef.VideoStreamAllocateRequest:
			compatible := vaCompatible(f)
			if len(compatible) == 0 {
				return nil, statusErr(im.StatusDynamicConstraintError)
			}
			i := slices.IndexFunc(compatible, func(p vaProfile) bool { return !held[p.id] })
			if len(held) >= 1 || i < 0 {
				return nil, statusErr(im.StatusResourceExhausted)
			}
			held[compatible[i].id] = true
			return csdef.VideoStreamAllocateResponse{VideoStreamId: compatible[i].id}, nil
		}
		return nil, nil
	}
}

// vaRecording is recordingStream (CameraVideoAllocateTest.ts:129-149,
// matterjs-server at 0bd06f2).
func vaRecording(refs int, spendsBudget bool) allocatedVideoStream {
	maxRes := resolution{640, 480}
	if spendsBudget {
		maxRes = resolution{1920, 1080}
	}
	return allocatedVideoStream{
		videoStreamID: 3, overlays: noOverlays, streamUsage: csdef.StreamUsageRecording, videoCodec: vaH264,
		minResolution: resolution{640, 480}, maxResolution: maxRes, minFrameRate: 30, maxFrameRate: 120, minBitRate: 10000, maxBitRate: 2000000, keyFrameInterval: 4000, referenceCount: refs,
	}
}

const vaFirefoxOffer = "v=0\r\no=- 0 0 IN IP4 127.0.0.1\r\ns=-\r\nt=0 0\r\nm=video 9 UDP/TLS/RTP/SAVPF 126\r\na=recvonly\r\na=rtpmap:126 H264/90000\r\na=fmtp:126 profile-level-id=42e01f;level-asymmetry-allowed=1;packetization-mode=1"

func vaFirefoxLimits() selectedVideoCodecLimits {
	sdp := parseSdpVideoConstraints(vaFirefoxOffer)
	return videoCodecLimitsOf(&sdp, vaH264)
}

func vaAllocates(rs []recordedInvoke) []csdef.VideoStreamAllocateRequest {
	out := make([]csdef.VideoStreamAllocateRequest, 0, len(rs))
	for _, r := range rs {
		if f, ok := r.fields.(csdef.VideoStreamAllocateRequest); ok {
			out = append(out, f)
		}
	}
	return out
}

func vaSummary(f csdef.VideoStreamAllocateRequest) string {
	return fmt.Sprintf("%dx%d@%d-%d %d", f.MaxResolution.Width, f.MaxResolution.Height, f.MinFrameRate, f.MaxFrameRate, f.MaxBitRate)
}

func vaSummaries(rs []recordedInvoke) []string {
	sent := vaAllocates(rs)
	out := make([]string, 0, len(sent))
	for _, f := range sent {
		out = append(out, vaSummary(f))
	}
	return out
}

func vaCommands(rs []recordedInvoke) []string {
	var out []string
	for _, r := range rs {
		if f, ok := r.fields.(csdef.VideoStreamAllocateRequest); ok {
			out = append(out, vaSummary(f))
		} else {
			out = append(out, r.command)
		}
	}
	return out
}

// vaResolve is resolveOn (CameraVideoAllocateTest.ts:175-195,
// matterjs-server at 0bd06f2).
func vaResolve(state *cameraState, respond func(*recordedInvoke) (any, error), hints *videoHints, limits *selectedVideoCodecLimits) ([]recordedInvoke, *resolvedStream, error) {
	l := vaFirefoxLimits()
	if limits != nil {
		l = *limits
	}
	m, io, _ := managerWith(state, respond)
	r, err := m.resolveVideoStream(context.Background(), videoRequest{t: tTarget, usage: liveView, limits: l, hints: hints, allowEviction: true})
	return io.recorded(), r, err
}

func vaHints(edit func(*videoRangeBounds)) *videoHints {
	h := &videoHints{}
	edit(&h.videoRangeBounds)
	return h
}

func vaWantSummaries(t *testing.T, got []string, want ...string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("sent\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// TestVideoAllocateFakeCameraParityMatterjsServer is describe("the fake
// camera") (CameraVideoAllocateTest.ts:198-266, matterjs-server at
// 0bd06f2): the fake gives the answers the raw probes against the real
// camera gave.
func TestVideoAllocateFakeCameraParityMatterjsServer(t *testing.T) {
	base := csdef.VideoStreamAllocateRequest{
		StreamUsage: liveView, VideoCodec: vaH264, MinFrameRate: 30, MaxFrameRate: 120,
		MinResolution: csdef.VideoResolutionStruct{Width: 640, Height: 480}, MaxResolution: csdef.VideoResolutionStruct{Width: 1920, Height: 1080},
		MinBitRate: 10000, MaxBitRate: 10000, KeyFrameInterval: 4000,
	}
	probes := []struct {
		name string
		edit func(*csdef.VideoStreamAllocateRequest)
		id   uint16
	}{
		{`refuses {"minFrameRate":1,"maxFrameRate":30,"maxBitRate":14000000,"keyFrameInterval":2000}`, func(r *csdef.VideoStreamAllocateRequest) {
			r.MinFrameRate, r.MaxFrameRate, r.MaxBitRate, r.KeyFrameInterval = 1, 30, 14000000, 2000
		}, 0},
		{`accepts {"minFrameRate":30,"maxFrameRate":120}`, func(r *csdef.VideoStreamAllocateRequest) { r.MinFrameRate, r.MaxFrameRate = 30, 120 }, 3},
		{`accepts {"minFrameRate":30,"maxFrameRate":30}`, func(r *csdef.VideoStreamAllocateRequest) { r.MinFrameRate, r.MaxFrameRate = 30, 30 }, 3},
		{`refuses {"minFrameRate":15,"maxFrameRate":30}`, func(r *csdef.VideoStreamAllocateRequest) { r.MinFrameRate, r.MaxFrameRate = 15, 30 }, 0},
		{`refuses {"minFrameRate":29,"maxFrameRate":30}`, func(r *csdef.VideoStreamAllocateRequest) { r.MinFrameRate, r.MaxFrameRate = 29, 30 }, 0},
		{`refuses {"minFrameRate":30,"maxFrameRate":121}`, func(r *csdef.VideoStreamAllocateRequest) { r.MinFrameRate, r.MaxFrameRate = 30, 121 }, 0},
		{`accepts {"maxBitRate":2000000}`, func(r *csdef.VideoStreamAllocateRequest) { r.MaxBitRate = 2000000 }, 3},
		{`refuses {"maxBitRate":2000001}`, func(r *csdef.VideoStreamAllocateRequest) { r.MaxBitRate = 2000001 }, 0},
		{`refuses {"minBitRate":9999}`, func(r *csdef.VideoStreamAllocateRequest) { r.MinBitRate = 9999 }, 0},
		{`refuses {"minResolution":{"width":320,"height":240}}`, func(r *csdef.VideoStreamAllocateRequest) {
			r.MinResolution = csdef.VideoResolutionStruct{Width: 320, Height: 240}
		}, 0},
		{`refuses {"keyFrameInterval":3999}`, func(r *csdef.VideoStreamAllocateRequest) { r.KeyFrameInterval = 3999 }, 0},
		{`refuses {"keyFrameInterval":5000}`, func(r *csdef.VideoStreamAllocateRequest) { r.KeyFrameInterval = 5000 }, 0},
		{`accepts {"minFrameRate":30,"maxFrameRate":30,"minResolution":{"width":1280,"height":720},"maxResolution":{"width":1280,"height":720},"maxBitRate":2000000}`, func(r *csdef.VideoStreamAllocateRequest) {
			r.MinFrameRate, r.MaxFrameRate, r.MaxBitRate = 30, 30, 2000000
			r.MinResolution, r.MaxResolution = csdef.VideoResolutionStruct{Width: 1280, Height: 720}, csdef.VideoResolutionStruct{Width: 1280, Height: 720}
		}, 1},
	}
	for _, p := range probes {
		t.Run("${probe.streamId === undefined ? \"refuses\" : \"accepts\"} ${JSON.stringify(probe.fields)}: "+p.name, func(t *testing.T) {
			req := base
			p.edit(&req)
			resp, err := vaCamera()(&recordedInvoke{command: cmdVideoStreamAllocate, fields: req})
			if p.id == 0 {
				if err == nil {
					t.Errorf("accepted with %+v", resp)
				}
				return
			}
			if r, ok := resp.(csdef.VideoStreamAllocateResponse); err != nil || !ok || r.VideoStreamId != p.id {
				t.Errorf("answer %+v %v, want stream %d", resp, err, p.id)
			}
		})
	}
	t.Run("answers a servable window with ResourceExhausted while its encoder is taken", func(t *testing.T) {
		if _, err := vaCamera(3)(&recordedInvoke{command: cmdVideoStreamAllocate, fields: base}); !statusIs(err, im.StatusResourceExhausted) {
			t.Errorf("err = %v", err)
		}
	})
}

// TestVideoAllocateFixedProfilesParityMatterjsServer is describe("VideoStreamAllocate
// against a camera with fixed stream profiles")
// (CameraVideoAllocateTest.ts:197-611, matterjs-server at 0bd06f2).
func TestVideoAllocateFixedProfilesParityMatterjsServer(t *testing.T) {
	h264 := &selectedVideoCodecLimits{codec: vaH264}
	refuseAll := func(status im.StatusCode) func(*recordedInvoke) (any, error) {
		return func(r *recordedInvoke) (any, error) {
			if r.command == cmdVideoStreamAllocate {
				return nil, statusErr(status)
			}
			return nil, nil
		}
	}
	t.Run("finds a window the camera accepts with no caller hints, lowering only the bit rate", func(t *testing.T) {
		rs, r, err := vaResolve(vaFixedState(), vaCamera(), nil, nil)
		if err != nil || r.streamID != 1 || r.provenance != ProvenanceAllocated {
			t.Fatalf("result %+v %v", r, err)
		}
		first := vaAllocates(rs)[0]
		want := csdef.VideoStreamAllocateRequest{
			StreamUsage: liveView, VideoCodec: vaH264, MinFrameRate: 30, MaxFrameRate: 30,
			MinResolution: csdef.VideoResolutionStruct{Width: 640, Height: 480}, MaxResolution: csdef.VideoResolutionStruct{Width: 1280, Height: 720},
			MinBitRate: 10000, MaxBitRate: 8000000, KeyFrameInterval: 4000,
		}
		if first != want {
			t.Errorf("first %+v", first)
		}
		vaWantSummaries(t, vaSummaries(rs), "1280x720@30-30 8000000", "1280x720@30-30 4000000", "1280x720@30-30 2000000")
	})
	vga := resolution{640, 480}
	firstRate := func(edit func(*videoRangeBounds)) [2]uint16 {
		rs, _, _ := vaResolve(vaFixedState(), vaCamera(), vaHints(edit), nil)
		f := vaAllocates(rs)[0]
		return [2]uint16{f.MinFrameRate, f.MaxFrameRate}
	}
	t.Run("asks for 30 fps without a caller frame rate, though the offer decodes 640x480 at 90", func(t *testing.T) {
		if got := firstRate(func(b *videoRangeBounds) { b.maxResolution = vga }); got != [2]uint16{30, 30} {
			t.Errorf("rate %v", got)
		}
	})
	t.Run("raises the default to the caller's min_frame_rate", func(t *testing.T) {
		if got := firstRate(func(b *videoRangeBounds) { b.maxResolution, b.minFrameRate = vga, 60 }); got != [2]uint16{60, 60} {
			t.Errorf("rate %v", got)
		}
	})
	t.Run("follows the caller's max_frame_rate above the default, up to the offer's decode ceiling", func(t *testing.T) {
		if got := firstRate(func(b *videoRangeBounds) { b.maxResolution, b.maxFrameRate = vga, 120 }); got != [2]uint16{90, 90} {
			t.Errorf("rate %v", got)
		}
	})
	t.Run("finds a window without an offer, starting at the default bit rate, not the network bandwidth", func(t *testing.T) {
		rs, r, err := vaResolve(vaFixedState(), vaCamera(), nil, h264)
		if err != nil || r.streamID != 3 {
			t.Fatalf("result %+v %v", r, err)
		}
		vaWantSummaries(t, vaSummaries(rs), "1920x1080@30-30 8000000", "1920x1080@30-30 4000000", "1920x1080@30-30 2000000")
		if last := vaAllocates(rs)[len(vaAllocates(rs))-1]; last.MinResolution != (csdef.VideoResolutionStruct{Width: 640, Height: 480}) {
			t.Errorf("last min resolution %+v", last.MinResolution)
		}
	})
	t.Run("asks the certification window last, and a camera accepting only that window succeeds", func(t *testing.T) {
		cert := csdef.VideoStreamAllocateRequest{
			StreamUsage: liveView, VideoCodec: vaH264, MinFrameRate: 30, MaxFrameRate: 120,
			MinResolution: csdef.VideoResolutionStruct{Width: 640, Height: 480}, MaxResolution: csdef.VideoResolutionStruct{Width: 1920, Height: 1080},
			MinBitRate: 10000, MaxBitRate: 10000, KeyFrameInterval: 4000,
		}
		rs, r, err := vaResolve(vaFixedState(), func(rec *recordedInvoke) (any, error) {
			if f, ok := rec.fields.(csdef.VideoStreamAllocateRequest); ok {
				if f != cert {
					return nil, statusErr(im.StatusDynamicConstraintError)
				}
				return csdef.VideoStreamAllocateResponse{VideoStreamId: 7}, nil
			}
			return nil, nil
		}, nil, h264)
		if err != nil || r.streamID != 7 || r.provenance != ProvenanceAllocated {
			t.Fatalf("result %+v %v", r, err)
		}
		sent := vaAllocates(rs)
		if len(sent) >= 32 || sent[len(sent)-1] != cert {
			t.Errorf("%d sent, last %+v", len(sent), sent[len(sent)-1])
		}
		for _, f := range sent[:len(sent)-1] {
			if f.MinFrameRate != f.MaxFrameRate {
				t.Errorf("a window before the last spans frame rates: %+v", f)
			}
		}
	})
	t.Run("asks the first trade-off point's bit rate last when the points are listed smallest first", func(t *testing.T) {
		st := vaFixedState().with(func(s *cameraState) {
			s.rateDistortionPoints = []rateDistortionPoint{{vaH264, resolution{640, 480}, 10000}, {vaH264, resolution{1920, 1080}, 2000000}}
		})
		rs, r, err := vaResolve(st, func(rec *recordedInvoke) (any, error) {
			if f, ok := rec.fields.(csdef.VideoStreamAllocateRequest); ok {
				if f.MinFrameRate == f.MaxFrameRate {
					return nil, statusErr(im.StatusDynamicConstraintError)
				}
				return csdef.VideoStreamAllocateResponse{VideoStreamId: 7}, nil
			}
			return nil, nil
		}, nil, h264)
		if err != nil || r.streamID != 7 {
			t.Fatalf("result %+v %v", r, err)
		}
		if last := vaAllocates(rs)[len(vaAllocates(rs))-1]; last.MinBitRate != 10000 || last.MaxBitRate != 10000 {
			t.Errorf("last %+v", last)
		}
	})
	t.Run("skips the certification window when the caller's bounds exclude it", func(t *testing.T) {
		rs, _, _ := vaResolve(vaFixedState(), refuseAll(im.StatusDynamicConstraintError), vaHints(func(b *videoRangeBounds) { b.maxFrameRate = 25 }), h264)
		for _, f := range vaAllocates(rs) {
			if f.MinFrameRate != f.MaxFrameRate {
				t.Errorf("a window spans frame rates: %+v", f)
			}
		}
	})
	t.Run("reaches a hidden bit rate ceiling far below the default", func(t *testing.T) {
		rs, r, err := vaResolve(vaFixedState(), func(rec *recordedInvoke) (any, error) {
			if f, ok := rec.fields.(csdef.VideoStreamAllocateRequest); ok {
				if f.MaxBitRate > 500000 {
					return nil, statusErr(im.StatusDynamicConstraintError)
				}
				return csdef.VideoStreamAllocateResponse{VideoStreamId: 6}, nil
			}
			return nil, nil
		}, nil, nil)
		if err != nil || r.streamID != 6 {
			t.Fatalf("result %+v %v", r, err)
		}
		sent := vaAllocates(rs)
		rates := make([]uint32, 0, len(sent))
		for _, f := range sent {
			rates = append(rates, f.MaxBitRate)
		}
		if !slices.Equal(rates, []uint32{8000000, 4000000, 2000000, 1000000, 500000}) {
			t.Errorf("bit rates %v", rates)
		}
	})
	t.Run("asks for one frame rate inside a caller's range the camera's hidden floor lies in", func(t *testing.T) {
		rs, r, err := vaResolve(vaFixedState(), vaCamera(), vaHints(func(b *videoRangeBounds) { b.minFrameRate, b.maxFrameRate = 15, 60 }), nil)
		if err != nil || r.streamID != 1 {
			t.Fatalf("result %+v %v", r, err)
		}
		if last := vaAllocates(rs)[len(vaAllocates(rs))-1]; last.MinFrameRate != 30 || last.MaxFrameRate != 30 {
			t.Errorf("last %+v", last)
		}
	})
	t.Run("keeps every retry inside the bounds the caller stated", func(t *testing.T) {
		hints := vaHints(func(b *videoRangeBounds) {
			b.minFrameRate, b.maxFrameRate, b.minBitRate, b.maxBitRate = 10, 25, 50000, 4000000
			b.minResolution, b.maxResolution = resolution{640, 480}, resolution{1280, 720}
		})
		rs, _, err := vaResolve(vaFixedState(), vaCamera(), hints, nil)
		var inc *IncompatibleError
		if !errors.As(err, &inc) {
			t.Fatalf("err = %v", err)
		}
		sent := vaAllocates(rs)
		if len(sent) <= 5 {
			t.Errorf("%d sent", len(sent))
		}
		for _, f := range sent {
			if f.MinFrameRate != f.MaxFrameRate || f.MaxFrameRate < 10 || f.MaxFrameRate > 25 || f.MinBitRate != 50000 ||
				f.MaxBitRate < 50000 || f.MaxBitRate > 4000000 || f.MaxResolution.Width < 640 || f.MaxResolution.Width > 1280 ||
				f.MaxResolution.Height < 480 || f.MaxResolution.Height > 720 || f.KeyFrameInterval != 4000 {
				t.Errorf("outside the caller's bounds: %+v", f)
			}
		}
	})
	t.Run("gives up bit rate, then frame rate, then frame size, at the size's own highest rate", func(t *testing.T) {
		rs, _, _ := vaResolve(vaFixedState(), refuseAll(im.StatusDynamicConstraintError), vaHints(func(b *videoRangeBounds) { b.maxFrameRate = 120 }), nil)
		vaWantSummaries(t, vaSummaries(rs),
			"1280x720@30-30 8000000", "1280x720@30-30 4000000", "1280x720@30-30 2000000", "1280x720@30-30 1000000",
			"1280x720@30-30 500000", "1280x720@30-30 250000", "1280x720@30-30 125000", "1280x720@30-30 62500",
			"1280x720@30-30 31250", "1280x720@30-30 15625", "1280x720@30-30 10000", "1280x720@15-15 10000",
			"1280x720@7-7 10000", "640x480@90-90 10000", "640x480@45-45 10000", "640x480@22-22 10000")
	})
	t.Run("reaches a camera that serves only 7 fps", func(t *testing.T) {
		rs, r, err := vaResolve(vaFixedState(), func(rec *recordedInvoke) (any, error) {
			if f, ok := rec.fields.(csdef.VideoStreamAllocateRequest); ok {
				if f.MaxFrameRate > 7 {
					return nil, statusErr(im.StatusDynamicConstraintError)
				}
				return csdef.VideoStreamAllocateResponse{VideoStreamId: 5}, nil
			}
			return nil, nil
		}, nil, nil)
		if err != nil || r.streamID != 5 {
			t.Fatalf("result %+v %v", r, err)
		}
		if last := vaAllocates(rs)[len(vaAllocates(rs))-1]; last.MinFrameRate != 7 || last.MaxFrameRate != 7 {
			t.Errorf("last %+v", last)
		}
	})
	t.Run("evicts an idle stream that holds the encoder budget once the camera answers ResourceExhausted, and keeps 30 fps", func(t *testing.T) {
		st := vaFixedState().with(func(s *cameraState) { s.video = []allocatedVideoStream{vaRecording(0, true)} })
		rs, r, err := vaResolve(st, vaCamera(3), nil, nil)
		if err != nil || r.streamID != 1 || !slices.Equal(r.evicted, []uint16{3}) {
			t.Fatalf("result %+v %v", r, err)
		}
		vaWantSummaries(t, vaCommands(rs), "640x480@30-30 8000000", "640x480@30-30 4000000", "640x480@30-30 2000000", cmdVideoStreamDeallocate, "1280x720@30-30 2000000")
	})
	t.Run("takes no stream while the camera accepts a window the budget predicted it could not carry", func(t *testing.T) {
		st := vaFixedState().with(func(s *cameraState) { s.video = []allocatedVideoStream{vaRecording(0, true)} })
		rs, r, err := vaResolve(st, allocateVideo(9), nil, nil)
		if err != nil || r.streamID != 9 {
			t.Fatalf("result %+v %v", r, err)
		}
		vaWantSummaries(t, vaCommands(rs)[:0:0], vaCommands(rs)[:0:0]...)
		if len(rs) != 1 || rs[0].command != cmdVideoStreamAllocate {
			t.Errorf("commands %v", vaCommands(rs))
		}
	})
	t.Run("reuses a slower stream when no window fits the encoder budget", func(t *testing.T) {
		slow := vaRecording(1, false)
		slow.videoStreamID, slow.streamUsage, slow.minFrameRate, slow.maxFrameRate, slow.maxBitRate = 4, liveView, 15, 15, 1000000
		st := vaFixedState().with(func(s *cameraState) { s.video = []allocatedVideoStream{vaRecording(1, true), slow} })
		rs, r, err := vaResolve(st, vaCamera(3, 1), nil, nil)
		if err != nil || r.streamID != 4 || len(rs) != 0 {
			t.Fatalf("result %+v %v, commands %v", r, err, vaCommands(rs))
		}
	})
	t.Run("does not offer a stream the camera refused to deallocate for eviction again", func(t *testing.T) {
		st := vaFixedState().with(func(s *cameraState) { s.video = []allocatedVideoStream{vaRecording(0, false)} })
		camera := vaCamera(3)
		rs, _, err := vaResolve(st, func(rec *recordedInvoke) (any, error) {
			if rec.command == cmdVideoStreamDeallocate {
				return nil, statusErr(im.StatusFailure)
			}
			return camera(rec)
		}, nil, nil)
		var exh *ExhaustedError
		if !errors.As(err, &exh) {
			t.Fatalf("err = %v", err)
		}
		n := 0
		for _, r := range rs {
			if r.command == cmdVideoStreamDeallocate {
				n++
			}
		}
		if n != 1 {
			t.Errorf("%d deallocates", n)
		}
	})
	t.Run("evicts an idle stream after a capacity refusal and keeps the frame rate", func(t *testing.T) {
		st := vaFixedState().with(func(s *cameraState) { s.video = []allocatedVideoStream{vaRecording(0, false)} })
		rs, r, err := vaResolve(st, vaCamera(3), nil, nil)
		if err != nil || r.streamID != 1 || !slices.Equal(r.evicted, []uint16{3}) {
			t.Fatalf("result %+v %v", r, err)
		}
		vaWantSummaries(t, vaCommands(rs), "1280x720@30-30 8000000", "1280x720@30-30 4000000", "1280x720@30-30 2000000", "640x480@30-30 2000000", cmdVideoStreamDeallocate, "1280x720@30-30 2000000")
	})
	t.Run("never turns a capacity refusal into an incompatibility when nothing can be taken", func(t *testing.T) {
		st := vaFixedState().with(func(s *cameraState) { s.video = []allocatedVideoStream{vaRecording(1, false)} })
		rs, _, err := vaResolve(st, vaCamera(3), nil, nil)
		var exh *ExhaustedError
		if !errors.As(err, &exh) {
			t.Fatalf("err = %v", err)
		}
		var below []uint16
		for _, f := range vaAllocates(rs) {
			if f.MaxFrameRate < 30 {
				below = append(below, f.MaxFrameRate)
			}
		}
		if !slices.Equal(below, []uint16{15}) {
			t.Errorf("rates below 30: %v", below)
		}
	})
	t.Run("steps only the resolution, then the frame rate, while the camera answers ResourceExhausted", func(t *testing.T) {
		rs, _, _ := vaResolve(vaFixedState(), refuseAll(im.StatusResourceExhausted), nil, nil)
		vaWantSummaries(t, vaSummaries(rs), "1280x720@30-30 8000000", "640x480@30-30 8000000", "640x480@15-15 8000000", "640x480@7-7 8000000")
	})
	t.Run("still reaches the smallest size when the bit rate walks down to 1 bit/s first", func(t *testing.T) {
		st := fixtureState().with(func(s *cameraState) { s.rateDistortionPoints = nil })
		rs, _, _ := vaResolve(st, refuseAll(im.StatusDynamicConstraintError), nil, &selectedVideoCodecLimits{codec: h265})
		sent := vaAllocates(rs)
		if len(sent) != 31 {
			t.Fatalf("%d sent", len(sent))
		}
		if last := sent[30]; last.MaxResolution != (csdef.VideoResolutionStruct{Width: 640, Height: 360}) || last.MaxBitRate != 1 {
			t.Errorf("last %+v", last)
		}
	})
	t.Run("gives up within a bounded number of attempts on a camera that refuses every window", func(t *testing.T) {
		rs, _, err := vaResolve(fixtureState(), refuseAll(im.StatusDynamicConstraintError), nil, &selectedVideoCodecLimits{codec: h265})
		var inc *IncompatibleError
		if !errors.As(err, &inc) {
			t.Fatalf("err = %v", err)
		}
		// Four bit rate steps and two frame rate steps at each of the three
		// sizes.
		if n := len(vaAllocates(rs)); n != 13 {
			t.Errorf("%d sent", n)
		}
	})
}

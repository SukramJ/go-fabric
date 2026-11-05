// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"

	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	"github.com/SukramJ/go-fabric/im"
)

// The getCapabilities, resolveVideoStream, withEndpointLock and
// resolveAudioStream cases of matterjs-server's CameraStreamManagerTest.ts
// at 0bd06f2, ported by meaning over the managerWith fixture. A TS
// `undefined` result field is the Go zero value; codec 2 (the source's
// "H264" constant) is kept as the number it is on the wire.

const sliceAOtherCodec = csdef.VideoCodecEnum(2)

// sliceANoErr fails the test on err.
func sliceANoErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// sliceAIncompatible is err as error 102.
func sliceAIncompatible(t *testing.T, err error) *IncompatibleError {
	t.Helper()
	var e *IncompatibleError
	if !errors.As(err, &e) {
		t.Fatalf("err = %v, want *IncompatibleError", err)
	}
	return e
}

// sliceAExhausted is err as error 103.
func sliceAExhausted(t *testing.T, err error) *ExhaustedError {
	t.Helper()
	var e *ExhaustedError
	if !errors.As(err, &e) {
		t.Fatalf("err = %v, want *ExhaustedError", err)
	}
	return e
}

// sliceAContained is the describe's CONTAINED_STREAM
// (CameraStreamManagerTest.ts:486-499, matterjs-server at 0bd06f2).
func sliceAContained() allocatedVideoStream {
	return allocatedVideoStream{
		videoStreamID: 7, overlays: noOverlays, streamUsage: liveView, videoCodec: h265,
		minResolution: resolution{1920, 1080}, maxResolution: resolution{1920, 1080},
		minFrameRate: 1, maxFrameRate: 30, minBitRate: 800000, maxBitRate: 4000000,
		keyFrameInterval: 3000, referenceCount: 1,
	}
}

// sliceAStream is CONTAINED_STREAM changed by edit.
func sliceAStream(edit func(*allocatedVideoStream)) allocatedVideoStream {
	s := sliceAContained()
	edit(&s)
	return s
}

func sliceAWithStreams(streams ...allocatedVideoStream) *cameraState {
	return fixtureState().with(func(s *cameraState) { s.video = streams })
}

func sliceAHints(b videoRangeBounds) *videoHints { return &videoHints{videoRangeBounds: b} }

// sliceAPinned1080p is PINNED_1080P.
func sliceAPinned1080p() *videoHints {
	return sliceAHints(videoRangeBounds{minResolution: resolution{1920, 1080}, maxResolution: resolution{1920, 1080}})
}

func sliceAMax720p() *videoHints {
	return sliceAHints(videoRangeBounds{maxResolution: resolution{1280, 720}})
}

// sliceARequest is a live-view H.265 request; eviction is allowed, the
// source's default.
func sliceARequest(hints *videoHints) videoRequest {
	return videoRequest{t: tTarget, usage: liveView, limits: selectedVideoCodecLimits{codec: h265}, hints: hints, allowEviction: true}
}

func sliceAVideoAllocates(io *fakeIO) []csdef.VideoStreamAllocateRequest {
	var out []csdef.VideoStreamAllocateRequest
	for _, r := range io.recorded() {
		if f, ok := r.fields.(csdef.VideoStreamAllocateRequest); ok && r.command == cmdVideoStreamAllocate {
			out = append(out, f)
		}
	}
	return out
}

func sliceASnapshotAllocates(io *fakeIO) []csdef.SnapshotStreamAllocateRequest {
	var out []csdef.SnapshotStreamAllocateRequest
	for _, r := range io.recorded() {
		if f, ok := r.fields.(csdef.SnapshotStreamAllocateRequest); ok && r.command == cmdSnapshotStreamAllocate {
			out = append(out, f)
		}
	}
	return out
}

func sliceAVideoDeallocated(io *fakeIO) []uint16 {
	var out []uint16
	for _, r := range io.recorded() {
		if f, ok := r.fields.(csdef.VideoStreamDeallocateRequest); ok && r.command == cmdVideoStreamDeallocate {
			out = append(out, f.VideoStreamId)
		}
	}
	return out
}

func sliceASnapshotDeallocated(io *fakeIO) []uint16 {
	var out []uint16
	for _, r := range io.recorded() {
		if f, ok := r.fields.(csdef.SnapshotStreamDeallocateRequest); ok && r.command == cmdSnapshotStreamDeallocate {
			out = append(out, f.SnapshotStreamId)
		}
	}
	return out
}

// sliceAAlways answers every invoke with err.
func sliceAAlways(err error) func(*recordedInvoke) (any, error) {
	return func(*recordedInvoke) (any, error) { return nil, err }
}

// sliceAEvictions records every stream the manager announces as evicted.
func sliceAEvictions(m *manager) func() []StreamEvicted {
	var mu sync.Mutex
	var seen []StreamEvicted
	m.onEvicted = func(e StreamEvicted) { mu.Lock(); seen = append(seen, e); mu.Unlock() }
	return func() []StreamEvicted {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(seen)
	}
}

// sliceACapture is the fixture camera's still frame.
var sliceACapture = csdef.CaptureSnapshotResponse{Data: []byte{1}, ImageCodec: csdef.ImageCodecJpeg, Resolution: csdef.VideoResolutionStruct{Width: 1920, Height: 1080}}

// sliceASnapshotHolder is SNAPSHOT_HOLDER, a snapshot stream of our own as
// the camera reports it (CameraStreamManagerTest.ts:911-923,
// matterjs-server at 0bd06f2).
func sliceASnapshotHolder() allocatedSnapshotStream {
	return allocatedSnapshotStream{
		snapshotStreamID: 5, overlays: noOverlays, imageCodec: csdef.ImageCodecJpeg,
		minResolution: resolution{1920, 1080}, maxResolution: resolution{1920, 1080},
		quality: 75, frameRate: 1, encodedPixels: true, hardwareEncoder: true,
	}
}

// sliceAShortOfEncoders is cameraShortOfEncoders: a camera that answers a
// snapshot allocate and capture and refuses every video allocate while the
// snapshot stream exists; override answers first when it handles the
// invoke (CameraStreamManagerTest.ts:925-952, matterjs-server at 0bd06f2).
func sliceAShortOfEncoders(state *cameraState, override func(*recordedInvoke) (any, bool, error)) (*manager, *fakeIO) {
	snapshotStreamLive := false
	m, io, _ := managerWith(state, func(r *recordedInvoke) (any, error) {
		if override != nil {
			if resp, ok, err := override(r); ok {
				return resp, err
			}
		}
		switch r.command {
		case cmdSnapshotStreamAllocate:
			snapshotStreamLive = true
			return csdef.SnapshotStreamAllocateResponse{SnapshotStreamId: 5}, nil
		case cmdCaptureSnapshot:
			return sliceACapture, nil
		case cmdSnapshotStreamDeallocate:
			snapshotStreamLive = false
			return nil, nil
		case cmdVideoStreamAllocate:
			if snapshotStreamLive {
				return nil, statusErr(im.StatusResourceExhausted)
			}
			return csdef.VideoStreamAllocateResponse{VideoStreamId: 11}, nil
		}
		return nil, nil
	})
	return m, io
}

// sliceAVideoExhausted overrides every video allocate with ResourceExhausted.
func sliceAVideoExhausted(r *recordedInvoke) (resp any, handled bool, err error) {
	if r.command == cmdVideoStreamAllocate {
		return nil, true, statusErr(im.StatusResourceExhausted)
	}
	return nil, false, nil
}

// sliceAEncoderBudgeted is encoderBudgetedCamera: it refuses any allocate
// whose maxResolution times maxFrameRate exceeds budget
// (CameraStreamManagerTest.ts:1552-1580, matterjs-server at 0bd06f2).
func sliceAEncoderBudgeted(budget int) func(*recordedInvoke) (any, error) {
	return func(r *recordedInvoke) (any, error) {
		f, ok := r.fields.(csdef.VideoStreamAllocateRequest)
		if r.command != cmdVideoStreamAllocate || !ok {
			return nil, nil
		}
		if int(f.MaxResolution.Width)*int(f.MaxResolution.Height)*int(f.MaxFrameRate) > budget {
			return nil, statusErr(im.StatusResourceExhausted)
		}
		return csdef.VideoStreamAllocateResponse{VideoStreamId: 40}, nil
	}
}

// sliceAHalfSensorBudget is half of the sensor's 2560x1440 at 30 fps.
const sliceAHalfSensorBudget = 55296000

// The getCapabilities cases (CameraStreamManagerTest.ts:340-479,
// matterjs-server at 0bd06f2).
func TestSliceAGetCapabilitiesParityMatterjsServer(t *testing.T) {
	t.Run("reports the camera's own facts", func(t *testing.T) {
		m, _, _ := managerWith(fixtureState(), nil)
		c, err := m.capabilities(t.Context(), tTarget)
		sliceANoErr(t, err)
		if c.Video.Sensor == nil || *c.Video.Sensor != (csdef.VideoResolutionStruct{Width: 2560, Height: 1440}) {
			t.Errorf("sensor = %v", c.Video.Sensor)
		}
		if c.Video.MinViewport == nil || *c.Video.MinViewport != (csdef.VideoResolutionStruct{Width: 640, Height: 360}) {
			t.Errorf("minViewport = %v", c.Video.MinViewport)
		}
		if c.Video.MaxFPS == nil || *c.Video.MaxFPS != 30 {
			t.Errorf("maxFps = %v", c.Video.MaxFPS)
		}
		if c.Limits.MaxConcurrentEncoders == nil || *c.Limits.MaxConcurrentEncoders != 1 {
			t.Errorf("maxConcurrentEncoders = %v", c.Limits.MaxConcurrentEncoders)
		}
		if c.Limits.MaxEncodedPixelRate == nil || *c.Limits.MaxEncodedPixelRate != 248832000 {
			t.Errorf("maxEncodedPixelRate = %v", c.Limits.MaxEncodedPixelRate)
		}
	})

	t.Run("reports the sessions the camera itself holds, whoever established them", func(t *testing.T) {
		m, io, _ := managerWith(fixtureState(), nil)
		io.sessions = []DeviceSession{
			{ID: 7, PeerNodeID: 1, PeerEndpoint: tEndpoint, StreamUsage: uint8(liveView), VideoStreams: []uint16{9}, AudioStreams: []uint16{}, EstablishedByUs: true},
			{ID: 8, PeerNodeID: 2, PeerEndpoint: tEndpoint, StreamUsage: uint8(liveView), VideoStreams: []uint16{9}, AudioStreams: []uint16{}, EstablishedByUs: false},
		}
		c, err := m.capabilities(t.Context(), tTarget)
		sliceANoErr(t, err)
		ids := make([]uint16, 0, len(c.Sessions))
		ours := make([]bool, 0, len(c.Sessions))
		for _, s := range c.Sessions {
			ids = append(ids, s.ID)
			ours = append(ours, s.EstablishedByUs)
		}
		if !slices.Equal(ids, []uint16{7, 8}) || !slices.Equal(ours, []bool{true, false}) {
			t.Errorf("sessions = %v %v, want [7 8] [true false]", ids, ours)
		}
	})

	t.Run("derives the codec list from the trade-off points", func(t *testing.T) {
		m, _, _ := managerWith(fixtureState(), nil)
		c, err := m.capabilities(t.Context(), tTarget)
		sliceANoErr(t, err)
		if !slices.Equal(c.Video.Codecs, []csdef.VideoCodecEnum{h265}) {
			t.Errorf("codecs = %v, want [H265]", c.Video.Codecs)
		}
	})

	t.Run("reports an absent capability as absent rather than substituting a default", func(t *testing.T) {
		bare := fixtureState().with(func(s *cameraState) { s.rateDistortionPoints = nil; s.minViewport = nil })
		m, _, _ := managerWith(bare, nil)
		c, err := m.capabilities(t.Context(), tTarget)
		sliceANoErr(t, err)
		if c.Video.RateDistortionPoints == nil || len(c.Video.RateDistortionPoints) != 0 {
			t.Errorf("rateDistortionPoints = %v, want []", c.Video.RateDistortionPoints)
		}
		if c.Video.MinViewport != nil {
			t.Errorf("minViewport = %v, want absent", c.Video.MinViewport)
		}
		if c.Video.Codecs == nil || len(c.Video.Codecs) != 0 {
			t.Errorf("codecs = %v, want []", c.Video.Codecs)
		}
	})

	t.Run("reports every snapshot capability, including whether it needs an encoder", func(t *testing.T) {
		m, _, _ := managerWith(fixtureState(), nil)
		c, err := m.capabilities(t.Context(), tTarget)
		sliceANoErr(t, err)
		if len(c.Snapshot) != 2 || !c.Snapshot[1].RequiresEncodedPixels {
			t.Errorf("snapshot = %+v, want two with [1] requiring encoded pixels", c.Snapshot)
		}
	})

	t.Run("fails typed when the endpoint has no camera behaviour", func(t *testing.T) {
		m, _, _ := managerWith(nil, nil)
		_, err := m.capabilities(t.Context(), tTarget)
		var ns *NotSupportedError
		if !errors.As(err, &ns) {
			t.Fatalf("err = %v, want *NotSupportedError", err)
		}
	})

	t.Run("reports a stream we did not allocate as not owned by the server", func(t *testing.T) {
		m, _, _ := managerWith(fixtureState().with(func(s *cameraState) {
			s.video = []allocatedVideoStream{liveStream(1, 2)}
		}), nil)
		c, err := m.capabilities(t.Context(), tTarget)
		sliceANoErr(t, err)
		v := c.Allocated.Video[0]
		if v.ReferenceCount != 2 || v.AllocatedByUs || v.MinBitRate != 800000 || v.MaxBitRate != 4000000 {
			t.Errorf("allocated video = %+v", v)
		}
	})

	t.Run("reports a stream the server allocated itself as owned by the server", func(t *testing.T) {
		m, io, _ := managerWith(fixtureState(), allocateVideo(9))
		_, err := m.resolveVideoStream(t.Context(), sliceARequest(nil))
		sliceANoErr(t, err)
		io.setState(fixtureState().with(func(s *cameraState) {
			s.video = []allocatedVideoStream{sliceAStream(func(v *allocatedVideoStream) {
				v.videoStreamID, v.keyFrameInterval, v.referenceCount = 9, 4000, 0
			})}
		}))
		c, err := m.capabilities(t.Context(), tTarget)
		sliceANoErr(t, err)
		if !c.Allocated.Video[0].AllocatedByUs {
			t.Error("allocatedByServer = false, want true")
		}
	})

	t.Run("reads state without invoking anything on the device", func(t *testing.T) {
		m, io, _ := managerWith(fixtureState(), nil)
		_, err := m.capabilities(t.Context(), tTarget)
		sliceANoErr(t, err)
		if n := len(io.recorded()); n != 0 {
			t.Errorf("invokes = %d, want 0", n)
		}
	})
}

// The resolveVideoStream cases (CameraStreamManagerTest.ts:481-2148,
// matterjs-server at 0bd06f2).
func TestSliceAResolveVideoStreamParityMatterjsServer(t *testing.T) {
	t.Run("reuses a contained stream without invoking anything", func(t *testing.T) {
		m, io, _ := managerWith(sliceAWithStreams(sliceAContained()), nil)
		r, err := m.resolveVideoStream(t.Context(), sliceARequest(sliceAPinned1080p()))
		sliceANoErr(t, err)
		if r.streamID != 7 || r.provenance != ProvenanceAdopted || len(io.recorded()) != 0 {
			t.Errorf("got %d %s with %v, want 7 adopted and no invoke", r.streamID, r.provenance, io.commands())
		}
	})

	t.Run("reports the reused stream's own bounds, not the wider envelope the request computed", func(t *testing.T) {
		m, _, _ := managerWith(sliceAWithStreams(sliceAContained()), nil)
		r, err := m.resolveVideoStream(t.Context(), sliceARequest(nil))
		sliceANoErr(t, err)
		if r.provenance == ProvenanceAllocated {
			t.Error("provenance = allocated")
		}
		c := sliceAContained()
		if r.video.minResolution != c.minResolution || r.video.maxResolution != c.maxResolution || r.video.keyFrameInterval != c.keyFrameInterval {
			t.Errorf("envelope = %+v, want the stream's own bounds", *r.video)
		}
	})

	t.Run("does not reuse a stream whose floor is below the requested floor", func(t *testing.T) {
		wider := sliceAStream(func(s *allocatedVideoStream) { s.minResolution = resolution{1280, 720} })
		m, io, _ := managerWith(sliceAWithStreams(wider), allocateVideo(9))
		r, err := m.resolveVideoStream(t.Context(), sliceARequest(sliceAPinned1080p()))
		sliceANoErr(t, err)
		if r.streamID != 9 || r.provenance != ProvenanceAllocated || !slices.Equal(io.commands(), []string{cmdVideoStreamAllocate}) {
			t.Errorf("got %d %s with %v", r.streamID, r.provenance, io.commands())
		}
	})

	t.Run("records a newly allocated stream as owned by the server", func(t *testing.T) {
		m, _, _ := managerWith(fixtureState(), allocateVideo(9))
		r, err := m.resolveVideoStream(t.Context(), sliceARequest(nil))
		sliceANoErr(t, err)
		if r.provenance == ProvenanceAdopted {
			t.Error("provenance = adopted")
		}
	})

	t.Run("fails typed when the caller's resolution floor exceeds what the camera can deliver", func(t *testing.T) {
		m, io, _ := managerWith(fixtureState(), allocateVideo(9))
		_, err := m.resolveVideoStream(t.Context(), sliceARequest(sliceAHints(videoRangeBounds{minResolution: resolution{3840, 2160}})))
		e := sliceAIncompatible(t, err)
		want := IncompatibleBound{Field: BoundMinResolution, Requested: "3840x2160", Limit: "2560x1440"}
		if e.Reason != ReasonBounds || e.Bound == nil || *e.Bound != want || !slices.Equal(e.Device, []string{"H265"}) {
			t.Errorf("err = %+v (bound %+v)", e, e.Bound)
		}
		if len(io.recorded()) != 0 {
			t.Errorf("invokes = %v, want none", io.commands())
		}
	})

	t.Run("takes the bit-rate ceiling from the camera's MaxNetworkBandwidth", func(t *testing.T) {
		m, io, _ := managerWith(fixtureState().with(func(s *cameraState) { s.maxNetworkBandwidth = stated(2000000) }), allocateVideo(9))
		_, err := m.resolveVideoStream(t.Context(), sliceARequest(nil))
		sliceANoErr(t, err)
		if a := sliceAVideoAllocates(io); len(a) == 0 || a[0].MaxBitRate != 2000000 {
			t.Errorf("allocates = %+v, want maxBitRate 2000000", a)
		}
	})

	t.Run("narrows the envelope and retries when the device rejects the parameters", func(t *testing.T) {
		attempt := 0
		m, io, _ := managerWith(fixtureState(), func(*recordedInvoke) (any, error) {
			attempt++
			if attempt == 1 {
				return nil, statusErr(im.StatusDynamicConstraintError)
			}
			return csdef.VideoStreamAllocateResponse{VideoStreamId: 9}, nil
		})
		r, err := m.resolveVideoStream(t.Context(), sliceARequest(nil))
		sliceANoErr(t, err)
		a := sliceAVideoAllocates(io)
		if r.streamID != 9 || len(io.recorded()) != 2 || len(a) != 2 {
			t.Fatalf("got %d with %v", r.streamID, io.commands())
		}
		if a[1].MaxBitRate != a[0].MaxBitRate/2 || a[1].MaxResolution != a[0].MaxResolution || a[1].MaxFrameRate != a[0].MaxFrameRate {
			t.Errorf("retry = %+v after %+v, want only the bit rate halved", a[1], a[0])
		}
	})

	t.Run("fails immediately when the device calls the request structurally invalid", func(t *testing.T) {
		m, io, _ := managerWith(fixtureState(), sliceAAlways(statusErr(im.StatusConstraintError)))
		_, err := m.resolveVideoStream(t.Context(), sliceARequest(nil))
		e := sliceAIncompatible(t, err)
		if e.DeviceStatus == nil || *e.DeviceStatus != im.StatusConstraintError || !slices.Equal(e.Device, []string{"H265"}) {
			t.Errorf("err = %+v", e)
		}
		if n := len(io.recorded()); n != 1 {
			t.Errorf("invokes = %d, want 1", n)
		}
	})

	t.Run("fails typed once narrowing is exhausted and the device still cannot serve the range", func(t *testing.T) {
		m, io, _ := managerWith(fixtureState(), sliceAAlways(statusErr(im.StatusDynamicConstraintError)))
		_, err := m.resolveVideoStream(t.Context(), sliceARequest(nil))
		e := sliceAIncompatible(t, err)
		if e.Reason != ReasonBounds || e.DeviceStatus == nil || *e.DeviceStatus != im.StatusDynamicConstraintError || !slices.Equal(e.Device, []string{"H265"}) {
			t.Errorf("err = %+v", e)
		}
		if n := len(io.recorded()); n <= 1 {
			t.Errorf("invokes = %d, want more than 1", n)
		}
	})

	t.Run("propagates a device rejection the ladder does not know how to react to", func(t *testing.T) {
		const unsupported = im.StatusCode(0x81)
		m, io, _ := managerWith(fixtureState(), sliceAAlways(statusErr(unsupported)))
		_, err := m.resolveVideoStream(t.Context(), sliceARequest(nil))
		if s, ok := deviceStatusOf(err); !ok || s != unsupported || isTyped(err) {
			t.Errorf("err = %v, want the device's 0x81", err)
		}
		if n := len(io.recorded()); n != 1 {
			t.Errorf("invokes = %d, want 1", n)
		}
	})

	t.Run("hands out a stream of another usage when it meets the caller's bounds", func(t *testing.T) {
		other := sliceAStream(func(s *allocatedVideoStream) { s.streamUsage, s.referenceCount = csdef.StreamUsageRecording, 1 })
		m, io, _ := managerWith(sliceAWithStreams(other), sliceAAlways(statusErr(im.StatusResourceExhausted)))
		r, err := m.resolveVideoStream(t.Context(), sliceARequest(sliceAPinned1080p()))
		sliceANoErr(t, err)
		if r.streamID != 7 || len(io.recorded()) != 0 {
			t.Errorf("got %d with %v, want 7 and no invoke", r.streamID, io.commands())
		}
	})

	t.Run("hands out an idle stream of another usage as degraded instead of evicting it", func(t *testing.T) {
		idle := sliceAStream(func(s *allocatedVideoStream) {
			s.streamUsage, s.referenceCount, s.minBitRate, s.maxBitRate = csdef.StreamUsageRecording, 0, 100000, 200000
		})
		m, io, _ := managerWith(sliceAWithStreams(idle), sliceAAlways(statusErr(im.StatusResourceExhausted)))
		r, err := m.resolveVideoStream(t.Context(), sliceARequest(nil))
		sliceANoErr(t, err)
		if r.streamID != 7 || !r.degraded || slices.Contains(io.commands(), cmdVideoStreamDeallocate) {
			t.Errorf("got %d degraded=%v with %v", r.streamID, r.degraded, io.commands())
		}
	})

	t.Run("hands out a stream outside the computed bit-rate range as degraded, not as a clean reuse", func(t *testing.T) {
		starved := sliceAStream(func(s *allocatedVideoStream) { s.minBitRate, s.maxBitRate = 100000, 200000 })
		m, _, _ := managerWith(sliceAWithStreams(starved), sliceAAlways(statusErr(im.StatusResourceExhausted)))
		r, err := m.resolveVideoStream(t.Context(), sliceARequest(nil))
		sliceANoErr(t, err)
		if r.streamID != 7 || !r.degraded {
			t.Errorf("got %d degraded=%v, want 7 degraded", r.streamID, r.degraded)
		}
	})

	t.Run("refuses a stream whose bit-rate ceiling is above the one the caller stated", func(t *testing.T) {
		loud := sliceAStream(func(s *allocatedVideoStream) { s.maxBitRate = 8000000 })
		m, io, _ := managerWith(sliceAWithStreams(loud), allocateVideo(9))
		hints := sliceAPinned1080p()
		hints.maxBitRate = 500000
		r, err := m.resolveVideoStream(t.Context(), sliceARequest(hints))
		sliceANoErr(t, err)
		if r.streamID != 9 || r.provenance != ProvenanceAllocated || !slices.Equal(io.commands(), []string{cmdVideoStreamAllocate}) {
			t.Errorf("got %d %s with %v", r.streamID, r.provenance, io.commands())
		}
	})

	t.Run("deallocates an unreferenced stream and retries rather than failing", func(t *testing.T) {
		idle := sliceAStream(func(s *allocatedVideoStream) { s.referenceCount = 0 })
		freed := 0
		m, io, _ := managerWith(sliceAWithStreams(idle), func(r *recordedInvoke) (any, error) {
			switch r.command {
			case cmdVideoStreamDeallocate:
				freed++
			case cmdVideoStreamAllocate:
				if freed == 0 {
					return nil, statusErr(im.StatusResourceExhausted)
				}
				return csdef.VideoStreamAllocateResponse{VideoStreamId: 11}, nil
			}
			return nil, nil
		})
		r, err := m.resolveVideoStream(t.Context(), sliceARequest(sliceAMax720p()))
		sliceANoErr(t, err)
		cmds := io.commands()
		i := slices.Index(cmds, cmdVideoStreamDeallocate)
		if r.streamID != 11 || i < 0 || !slices.Equal(cmds[i+1:], []string{cmdVideoStreamAllocate}) {
			t.Errorf("got %d with %v", r.streamID, cmds)
		}
	})

	t.Run("frees a snapshot stream it allocated itself when that is the only capacity left", func(t *testing.T) {
		encoderHolder := sliceASnapshotHolder()
		encoderHolder.quality = 90
		m, io := sliceAShortOfEncoders(fixtureState(), nil)
		evicted := sliceAEvictions(m)
		_, err := m.snapshot(t.Context(), snapshotArgs{t: tTarget})
		sliceANoErr(t, err)
		io.setState(fixtureState().with(func(s *cameraState) { s.snapshot = []allocatedSnapshotStream{encoderHolder} }))
		r, err := m.resolveVideoStream(t.Context(), sliceARequest(nil))
		sliceANoErr(t, err)
		if r.streamID != 11 {
			t.Errorf("streamID = %d, want 11", r.streamID)
		}
		if d := sliceASnapshotDeallocated(io); !slices.Equal(d, []uint16{5}) {
			t.Errorf("snapshot deallocates = %v, want [5]", d)
		}
		if n := len(sliceASnapshotAllocates(io)); n != 1 {
			t.Errorf("snapshot allocates = %d, want 1 (no replacement)", n)
		}
		if e := evicted(); !slices.Equal(e, []StreamEvicted{{NodeID: tNode, Endpoint: tEndpoint, Kind: StreamSnapshot, StreamID: 5}}) {
			t.Errorf("evicted = %+v", e)
		}
		if r.evicted != nil {
			t.Errorf("result evicted = %v, want none", r.evicted)
		}
	})

	t.Run("frees its own snapshot stream before it gives up frame rate for the encoder budget", func(t *testing.T) {
		const budget = 110592000
		holder := sliceASnapshotHolder()
		holder.quality, holder.frameRate, holder.hardwareEncoder = 90, 30, false
		m, io := sliceAShortOfEncoders(fixtureState().with(func(s *cameraState) { s.maxEncodedPixelRate = stated(budget) }), nil)
		_, err := m.snapshot(t.Context(), snapshotArgs{t: tTarget})
		sliceANoErr(t, err)
		io.setState(fixtureState().with(func(s *cameraState) {
			s.maxEncodedPixelRate = stated(budget)
			s.snapshot = []allocatedSnapshotStream{holder}
		}))
		r, err := m.resolveVideoStream(t.Context(), sliceARequest(sliceAHints(videoRangeBounds{maxFrameRate: 30, minResolution: resolution{2560, 1440}})))
		sliceANoErr(t, err)
		cmds := io.commands()
		if r.streamID != 11 || r.video.maxFrameRate != 30 {
			t.Errorf("got %d at %d fps, want 11 at 30", r.streamID, r.video.maxFrameRate)
		}
		if len(cmds) < 2 || !slices.Equal(cmds[len(cmds)-2:], []string{cmdSnapshotStreamDeallocate, cmdVideoStreamAllocate}) {
			t.Errorf("commands = %v, want the deallocate then the first allocate", cmds)
		}
	})

	t.Run("takes its own snapshot stream before a video stream anyone else allocated", func(t *testing.T) {
		foreignIdle := sliceAStream(func(s *allocatedVideoStream) {
			s.videoStreamID, s.videoCodec, s.referenceCount = 21, sliceAOtherCodec, 0
		})
		m, io := sliceAShortOfEncoders(fixtureState(), nil)
		_, err := m.snapshot(t.Context(), snapshotArgs{t: tTarget})
		sliceANoErr(t, err)
		io.setState(fixtureState().with(func(s *cameraState) {
			s.video = []allocatedVideoStream{foreignIdle}
			s.snapshot = []allocatedSnapshotStream{sliceASnapshotHolder()}
		}))
		r, err := m.resolveVideoStream(t.Context(), sliceARequest(nil))
		sliceANoErr(t, err)
		freed := slices.DeleteFunc(io.commands(), func(c string) bool {
			return c != cmdSnapshotStreamDeallocate && c != cmdVideoStreamDeallocate
		})
		if r.streamID != 11 || !slices.Equal(freed, []string{cmdSnapshotStreamDeallocate}) {
			t.Errorf("got %d, freed %v", r.streamID, freed)
		}
	})

	t.Run("takes a snapshot stream of its own the camera has not reported yet", func(t *testing.T) {
		m, io := sliceAShortOfEncoders(fixtureState(), nil)
		_, err := m.snapshot(t.Context(), snapshotArgs{t: tTarget})
		sliceANoErr(t, err)
		r, err := m.resolveVideoStream(t.Context(), sliceARequest(nil))
		sliceANoErr(t, err)
		if d := sliceASnapshotDeallocated(io); r.streamID != 11 || !slices.Equal(d, []uint16{5}) {
			t.Errorf("got %d, snapshot deallocates %v", r.streamID, d)
		}
	})

	t.Run("leases no replacement snapshot stream when the camera answers the restore with one it already held", func(t *testing.T) {
		foreign := sliceASnapshotHolder()
		foreign.snapshotStreamID, foreign.minResolution, foreign.maxResolution = 8, resolution{640, 480}, resolution{640, 480}
		foreign.encodedPixels, foreign.hardwareEncoder = false, false
		restored := false
		m, io := sliceAShortOfEncoders(fixtureState(), func(r *recordedInvoke) (any, bool, error) {
			if r.command == cmdVideoStreamAllocate {
				return nil, true, statusErr(im.StatusResourceExhausted)
			}
			if r.command == cmdSnapshotStreamAllocate && restored {
				return csdef.SnapshotStreamAllocateResponse{SnapshotStreamId: 8}, true, nil
			}
			return nil, false, nil
		})
		_, err := m.snapshot(t.Context(), snapshotArgs{t: tTarget})
		sliceANoErr(t, err)
		restored = true
		io.setState(fixtureState().with(func(s *cameraState) {
			s.snapshot = []allocatedSnapshotStream{sliceASnapshotHolder(), foreign}
		}))
		_, _ = m.resolveVideoStream(t.Context(), sliceARequest(nil))
		io.setState(fixtureState().with(func(s *cameraState) { s.snapshot = []allocatedSnapshotStream{foreign} }))
		c, err := m.capabilities(t.Context(), tTarget)
		sliceANoErr(t, err)
		if len(c.Allocated.Snapshot) != 1 || c.Allocated.Snapshot[0].AllocatedByUs {
			t.Errorf("allocated snapshot = %+v, want one not ours", c.Allocated.Snapshot)
		}
	})

	t.Run("sends one deallocate for a snapshot stream the camera refuses to free, and moves on", func(t *testing.T) {
		foreignIdle := sliceAStream(func(s *allocatedVideoStream) {
			s.videoStreamID, s.videoCodec, s.referenceCount = 21, sliceAOtherCodec, 0
		})
		m, io := sliceAShortOfEncoders(fixtureState(), func(r *recordedInvoke) (any, bool, error) {
			if r.command == cmdSnapshotStreamDeallocate {
				return nil, true, statusErr(im.StatusBusy)
			}
			return nil, false, nil
		})
		_, err := m.snapshot(t.Context(), snapshotArgs{t: tTarget})
		sliceANoErr(t, err)
		io.setState(fixtureState().with(func(s *cameraState) {
			s.video = []allocatedVideoStream{foreignIdle}
			s.snapshot = []allocatedSnapshotStream{sliceASnapshotHolder()}
		}))
		_, _ = m.resolveVideoStream(t.Context(), sliceARequest(nil))
		if d := sliceASnapshotDeallocated(io); len(d) != 1 || !slices.Contains(io.commands(), cmdVideoStreamDeallocate) {
			t.Errorf("snapshot deallocates %v, commands %v", d, io.commands())
		}
	})

	t.Run("does not name a snapshot stream it destroyed in the capacity it reports as taken", func(t *testing.T) {
		m, io := sliceAShortOfEncoders(fixtureState(), sliceAVideoExhausted)
		_, err := m.snapshot(t.Context(), snapshotArgs{t: tTarget})
		sliceANoErr(t, err)
		io.setState(fixtureState().with(func(s *cameraState) { s.snapshot = []allocatedSnapshotStream{sliceASnapshotHolder()} }))
		_, err = m.resolveVideoStream(t.Context(), sliceARequest(nil))
		if e := sliceAExhausted(t, err); len(e.Allocated) != 0 {
			t.Errorf("allocated = %+v, want []", e.Allocated)
		}
	})

	t.Run("puts a snapshot stream back when the request that freed it never used the capacity", func(t *testing.T) {
		m, io := sliceAShortOfEncoders(fixtureState(), sliceAVideoExhausted)
		_, err := m.snapshot(t.Context(), snapshotArgs{t: tTarget})
		sliceANoErr(t, err)
		io.setState(fixtureState().with(func(s *cameraState) { s.snapshot = []allocatedSnapshotStream{sliceASnapshotHolder()} }))
		_, _ = m.resolveVideoStream(t.Context(), sliceARequest(nil))
		a := sliceASnapshotAllocates(io)
		if len(a) != 2 {
			t.Fatalf("snapshot allocates = %d, want 2", len(a))
		}
		if a[1].MinResolution != (csdef.VideoResolutionStruct{Width: 1920, Height: 1080}) || a[1].MaxFrameRate != 1 || int(a[1].Quality) != sliceASnapshotHolder().quality {
			t.Errorf("restore = %+v", a[1])
		}
	})

	t.Run("puts a snapshot stream back when the degraded rung served the caller instead", func(t *testing.T) {
		inUse := sliceAStream(func(s *allocatedVideoStream) { s.maxResolution = resolution{3840, 2160} })
		m, io := sliceAShortOfEncoders(fixtureState(), sliceAVideoExhausted)
		_, err := m.snapshot(t.Context(), snapshotArgs{t: tTarget})
		sliceANoErr(t, err)
		io.setState(fixtureState().with(func(s *cameraState) {
			s.video = []allocatedVideoStream{inUse}
			s.snapshot = []allocatedSnapshotStream{sliceASnapshotHolder()}
		}))
		r, err := m.resolveVideoStream(t.Context(), sliceARequest(nil))
		sliceANoErr(t, err)
		if r.streamID != 7 || !r.degraded || len(sliceASnapshotAllocates(io)) != 2 {
			t.Errorf("got %d degraded=%v with %d snapshot allocates", r.streamID, r.degraded, len(sliceASnapshotAllocates(io)))
		}
	})

	t.Run("frees its own snapshot stream before another controller's", func(t *testing.T) {
		foreign := sliceASnapshotHolder()
		foreign.snapshotStreamID = 2
		m, io := sliceAShortOfEncoders(fixtureState(), nil)
		_, err := m.snapshot(t.Context(), snapshotArgs{t: tTarget})
		sliceANoErr(t, err)
		io.setState(fixtureState().with(func(s *cameraState) {
			s.snapshot = []allocatedSnapshotStream{foreign, sliceASnapshotHolder()}
		}))
		_, _ = m.resolveVideoStream(t.Context(), sliceARequest(nil))
		if d := sliceASnapshotDeallocated(io); !slices.Equal(d, []uint16{5}) {
			t.Errorf("snapshot deallocates = %v, want [5]", d)
		}
	})

	t.Run("counts a stream reported with parameters this server never asked for as another controller's", func(t *testing.T) {
		reissued := sliceASnapshotHolder()
		reissued.minResolution, reissued.maxResolution = resolution{640, 480}, resolution{640, 480}
		foreign := sliceASnapshotHolder()
		foreign.snapshotStreamID = 2
		m, io := sliceAShortOfEncoders(fixtureState(), sliceAVideoExhausted)
		_, err := m.snapshot(t.Context(), snapshotArgs{t: tTarget})
		sliceANoErr(t, err)
		io.setState(fixtureState().with(func(s *cameraState) { s.snapshot = []allocatedSnapshotStream{foreign, reissued} }))
		_, _ = m.resolveVideoStream(t.Context(), sliceARequest(nil))
		if d := sliceASnapshotDeallocated(io); len(d) == 0 || d[0] != 2 {
			t.Errorf("snapshot deallocates = %v, want 2 first", d)
		}
	})

	t.Run("stops claiming a snapshot stream the camera answers NotFound for", func(t *testing.T) {
		m, io := sliceAShortOfEncoders(fixtureState(), func(r *recordedInvoke) (any, bool, error) {
			if r.command == cmdSnapshotStreamDeallocate {
				return nil, true, statusErr(im.StatusNotFound)
			}
			return sliceAVideoExhausted(r)
		})
		_, err := m.snapshot(t.Context(), snapshotArgs{t: tTarget})
		sliceANoErr(t, err)
		io.setState(fixtureState().with(func(s *cameraState) { s.snapshot = []allocatedSnapshotStream{sliceASnapshotHolder()} }))
		_, _ = m.resolveVideoStream(t.Context(), sliceARequest(nil))
		if d := sliceASnapshotDeallocated(io); len(d) != 1 {
			t.Errorf("snapshot deallocates = %v, want one", d)
		}
		c, err := m.capabilities(t.Context(), tTarget)
		sliceANoErr(t, err)
		if len(c.Allocated.Snapshot) != 1 || c.Allocated.Snapshot[0].AllocatedByUs {
			t.Errorf("allocated snapshot = %+v, want one not ours", c.Allocated.Snapshot)
		}
	})

	t.Run("frees an unreferenced snapshot stream another controller allocated, and announces it", func(t *testing.T) {
		foreign := sliceASnapshotHolder()
		foreign.snapshotStreamID, foreign.quality = 6, 90
		freed := false
		m, io, _ := managerWith(fixtureState().with(func(s *cameraState) { s.snapshot = []allocatedSnapshotStream{foreign} }), func(r *recordedInvoke) (any, error) {
			switch r.command {
			case cmdSnapshotStreamDeallocate:
				freed = true
			case cmdVideoStreamAllocate:
				if !freed {
					return nil, statusErr(im.StatusResourceExhausted)
				}
				return csdef.VideoStreamAllocateResponse{VideoStreamId: 11}, nil
			}
			return nil, nil
		})
		evictions := sliceAEvictions(m)
		r, err := m.resolveVideoStream(t.Context(), sliceARequest(nil))
		sliceANoErr(t, err)
		if d := sliceASnapshotDeallocated(io); r.streamID != 11 || !slices.Equal(d, []uint16{6}) {
			t.Errorf("got %d, snapshot deallocates %v", r.streamID, d)
		}
		if e := evictions(); !slices.Equal(e, []StreamEvicted{{NodeID: tNode, Endpoint: tEndpoint, Kind: StreamSnapshot, StreamID: 6}}) {
			t.Errorf("evicted = %+v", e)
		}
	})

	t.Run("takes no snapshot stream of its own when the caller forbade eviction", func(t *testing.T) {
		m, io, _ := managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
			switch r.command {
			case cmdSnapshotStreamAllocate:
				return csdef.SnapshotStreamAllocateResponse{SnapshotStreamId: 5}, nil
			case cmdCaptureSnapshot:
				return sliceACapture, nil
			case cmdVideoStreamAllocate:
				return nil, statusErr(im.StatusResourceExhausted)
			}
			return nil, nil
		})
		_, err := m.snapshot(t.Context(), snapshotArgs{t: tTarget})
		sliceANoErr(t, err)
		holder := sliceASnapshotHolder()
		holder.quality = 90
		io.setState(fixtureState().with(func(s *cameraState) { s.snapshot = []allocatedSnapshotStream{holder} }))
		req := sliceARequest(nil)
		req.allowEviction = false
		_, err = m.resolveVideoStream(t.Context(), req)
		sliceAExhausted(t, err)
		if slices.Contains(io.commands(), cmdSnapshotStreamDeallocate) {
			t.Errorf("commands = %v, want no snapshot deallocate", io.commands())
		}
	})

	t.Run("announces the stream the make-room rung took", func(t *testing.T) {
		idle := sliceAStream(func(s *allocatedVideoStream) { s.referenceCount = 0 })
		freed := 0
		m, _, _ := managerWith(sliceAWithStreams(idle), func(r *recordedInvoke) (any, error) {
			switch r.command {
			case cmdVideoStreamDeallocate:
				freed++
			case cmdVideoStreamAllocate:
				if freed == 0 {
					return nil, statusErr(im.StatusResourceExhausted)
				}
				return csdef.VideoStreamAllocateResponse{VideoStreamId: 11}, nil
			}
			return nil, nil
		})
		evicted := sliceAEvictions(m)
		_, err := m.resolveVideoStream(t.Context(), sliceARequest(sliceAMax720p()))
		sliceANoErr(t, err)
		if e := evicted(); !slices.Equal(e, []StreamEvicted{{NodeID: tNode, Endpoint: tEndpoint, Kind: StreamVideo, StreamID: 7}}) {
			t.Errorf("evicted = %+v", e)
		}
	})

	// sliceAWrapped is the eviction camera whose capacity refusal arrives
	// wrapped by wrap.
	sliceAWrapped := func(t *testing.T, wrap func(error) error) {
		t.Helper()
		idle := sliceAStream(func(s *allocatedVideoStream) { s.referenceCount = 0 })
		freed := 0
		m, io, _ := managerWith(sliceAWithStreams(idle), func(r *recordedInvoke) (any, error) {
			switch r.command {
			case cmdVideoStreamDeallocate:
				freed++
			case cmdVideoStreamAllocate:
				if freed == 0 {
					return nil, wrap(statusErr(im.StatusResourceExhausted))
				}
				return csdef.VideoStreamAllocateResponse{VideoStreamId: 11}, nil
			}
			return nil, nil
		})
		r, err := m.resolveVideoStream(t.Context(), sliceARequest(sliceAMax720p()))
		sliceANoErr(t, err)
		if r.streamID != 11 || !slices.Contains(io.commands(), cmdVideoStreamDeallocate) {
			t.Errorf("got %d with %v", r.streamID, io.commands())
		}
	}

	t.Run("reacts to a device status matter.js wrapped in a cause chain", func(t *testing.T) {
		sliceAWrapped(t, func(err error) error { return fmt.Errorf("invoke failed: %w", err) })
	})

	t.Run("reacts to a device status matter.js wrapped in an AggregateError", func(t *testing.T) {
		sliceAWrapped(t, func(err error) error { return errors.Join(err) })
	})

	t.Run("frees its own unreferenced stream before touching a foreign one", func(t *testing.T) {
		attempts, freed := 0, 0
		m, io, _ := managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
			if r.command == cmdVideoStreamDeallocate {
				freed++
			}
			if r.command != cmdVideoStreamAllocate {
				return nil, nil
			}
			attempts++
			switch {
			case attempts == 1:
				return csdef.VideoStreamAllocateResponse{VideoStreamId: 20}, nil
			case freed == 0:
				return nil, statusErr(im.StatusResourceExhausted)
			}
			return csdef.VideoStreamAllocateResponse{VideoStreamId: 30}, nil
		})
		_, err := m.resolveVideoStream(t.Context(), sliceARequest(nil))
		sliceANoErr(t, err)
		foreign := sliceAStream(func(s *allocatedVideoStream) {
			s.videoStreamID, s.videoCodec, s.referenceCount = 21, sliceAOtherCodec, 0
		})
		ours := sliceAStream(func(s *allocatedVideoStream) {
			s.videoStreamID, s.videoCodec, s.referenceCount = 20, sliceAOtherCodec, 0
		})
		st := sliceAWithStreams(foreign, ours)
		io.setState(st)
		r, err := m.resolveVideoStream(t.Context(), sliceARequest(nil))
		sliceANoErr(t, err)
		if d := sliceAVideoDeallocated(io); r.streamID != 30 || !slices.Equal(d, []uint16{20}) {
			t.Errorf("got %d, deallocates %v", r.streamID, d)
		}
		if st.video[0].videoStreamID != 21 || st.video[1].videoStreamID != 20 || len(st.video) != 2 {
			t.Errorf("camera state written to: %+v", st.video)
		}
	})

	t.Run("does not retry deallocating a stream it already freed", func(t *testing.T) {
		first := sliceAStream(func(s *allocatedVideoStream) {
			s.videoStreamID, s.videoCodec, s.referenceCount = 20, sliceAOtherCodec, 0
		})
		second := sliceAStream(func(s *allocatedVideoStream) {
			s.videoStreamID, s.videoCodec, s.referenceCount = 21, sliceAOtherCodec, 0
		})
		st := sliceAWithStreams(first, second)
		m, io, _ := managerWith(st, func(r *recordedInvoke) (any, error) {
			if r.command == cmdVideoStreamAllocate {
				return nil, statusErr(im.StatusResourceExhausted)
			}
			return nil, nil
		})
		_, err := m.resolveVideoStream(t.Context(), sliceARequest(sliceAMax720p()))
		e := sliceAExhausted(t, err)
		if d := sliceAVideoDeallocated(io); !slices.Equal(d, []uint16{20, 21}) {
			t.Errorf("deallocates = %v, want [20 21]", d)
		}
		if len(e.Allocated) != 0 {
			t.Errorf("allocated = %+v, want []", e.Allocated)
		}
		if len(st.video) != 2 || st.video[0].videoStreamID != 20 || st.video[1].videoStreamID != 21 {
			t.Errorf("camera state written to: %+v", st.video)
		}
	})

	t.Run("takes the lowest-priority unreferenced stream, not the first one it finds", func(t *testing.T) {
		recording := sliceAStream(func(s *allocatedVideoStream) {
			s.videoStreamID, s.videoCodec, s.streamUsage, s.referenceCount = 20, sliceAOtherCodec, csdef.StreamUsageRecording, 0
		})
		analysis := sliceAStream(func(s *allocatedVideoStream) {
			s.videoStreamID, s.videoCodec, s.streamUsage, s.referenceCount = 21, sliceAOtherCodec, csdef.StreamUsageAnalysis, 0
		})
		freed := 0
		m, io, _ := managerWith(sliceAWithStreams(recording, analysis), sliceAFreeThenAllocate(&freed, 1, 30))
		r, err := m.resolveVideoStream(t.Context(), sliceARequest(nil))
		sliceANoErr(t, err)
		if d := sliceAVideoDeallocated(io); r.streamID != 30 || !slices.Equal(d, []uint16{21}) {
			t.Errorf("got %d, deallocates %v", r.streamID, d)
		}
	})

	t.Run("still frees a stream when the camera reports no stream-usage ranking at all", func(t *testing.T) {
		idle := sliceAStream(func(s *allocatedVideoStream) {
			s.videoStreamID, s.videoCodec, s.referenceCount = 20, sliceAOtherCodec, 0
		})
		unranked := fixtureState().with(func(s *cameraState) {
			s.streamUsagePriorities = []csdef.StreamUsageEnum{}
			s.video = []allocatedVideoStream{idle}
		})
		freed := 0
		m, io, _ := managerWith(unranked, sliceAFreeThenAllocate(&freed, 1, 30))
		r, err := m.resolveVideoStream(t.Context(), sliceARequest(nil))
		sliceANoErr(t, err)
		if d := sliceAVideoDeallocated(io); r.streamID != 30 || !slices.Equal(d, []uint16{20}) {
			t.Errorf("got %d, deallocates %v", r.streamID, d)
		}
	})

	t.Run("never takes an Internal stream to make room, however idle it is", func(t *testing.T) {
		internal := sliceAStream(func(s *allocatedVideoStream) {
			s.videoStreamID, s.videoCodec, s.streamUsage, s.referenceCount = 20, sliceAOtherCodec, csdef.StreamUsageInternal, 0
		})
		m, io, _ := managerWith(sliceAWithStreams(internal), sliceAVideoOnlyExhausted)
		_, err := m.resolveVideoStream(t.Context(), sliceARequest(nil))
		sliceAExhausted(t, err)
		if slices.Contains(io.commands(), cmdVideoStreamDeallocate) {
			t.Errorf("commands = %v, want no deallocate", io.commands())
		}
	})

	t.Run("hands out an in-use stream as degraded when the caller stated no bounds", func(t *testing.T) {
		busy := sliceAStream(func(s *allocatedVideoStream) { s.maxResolution = resolution{3840, 2160} })
		m, _, _ := managerWith(sliceAWithStreams(busy), sliceAAlways(statusErr(im.StatusResourceExhausted)))
		r, err := m.resolveVideoStream(t.Context(), sliceARequest(nil))
		sliceANoErr(t, err)
		if r.streamID != 7 || !r.degraded || r.video.minResolution != busy.minResolution || r.video.maxResolution != busy.maxResolution {
			t.Errorf("got %d degraded=%v envelope %+v", r.streamID, r.degraded, r.video)
		}
	})

	t.Run("fails rather than degrading when the caller pinned a resolution the stream cannot guarantee", func(t *testing.T) {
		busy := sliceAStream(func(s *allocatedVideoStream) { s.minResolution = resolution{1280, 720} })
		m, _, _ := managerWith(sliceAWithStreams(busy), sliceAAlways(statusErr(im.StatusResourceExhausted)))
		_, err := m.resolveVideoStream(t.Context(), sliceARequest(sliceAPinned1080p()))
		sliceAExhausted(t, err)
	})

	t.Run("hands a pinned caller a stream that meets its pins exactly", func(t *testing.T) {
		busy := sliceAStream(func(s *allocatedVideoStream) { s.minBitRate, s.maxBitRate = 100000, 200000 })
		m, _, _ := managerWith(sliceAWithStreams(busy), sliceAAlways(statusErr(im.StatusResourceExhausted)))
		r, err := m.resolveVideoStream(t.Context(), sliceARequest(sliceAPinned1080p()))
		sliceANoErr(t, err)
		pin := resolution{1920, 1080}
		if r.streamID != 7 || r.provenance == ProvenanceAllocated || !r.degraded || r.video.minResolution != pin || r.video.maxResolution != pin {
			t.Errorf("got %d %s degraded=%v envelope %+v", r.streamID, r.provenance, r.degraded, r.video)
		}
	})

	t.Run("reuses a stream the camera already produces although the budget is spent", func(t *testing.T) {
		st := sliceAWithStreams(sliceAContained()).with(func(s *cameraState) { s.maxEncodedPixelRate = stated(1920*1080*30 + 1000) })
		m, io, _ := managerWith(st, sliceAAlways(errors.New("nothing should be invoked")))
		r, err := m.resolveVideoStream(t.Context(), sliceARequest(sliceAPinned1080p()))
		sliceANoErr(t, err)
		if r.streamID != 7 || r.provenance == ProvenanceAllocated || len(io.recorded()) != 0 {
			t.Errorf("got %d %s with %v", r.streamID, r.provenance, io.commands())
		}
	})

	halfBudget := func() *cameraState {
		return fixtureState().with(func(s *cameraState) { s.maxEncodedPixelRate = stated(sliceAHalfSensorBudget) })
	}

	t.Run("asks for a frame rate the camera's stated encoder budget can carry", func(t *testing.T) {
		m, io, _ := managerWith(halfBudget(), sliceAEncoderBudgeted(sliceAHalfSensorBudget))
		r, err := m.resolveVideoStream(t.Context(), sliceARequest(nil))
		sliceANoErr(t, err)
		if r.streamID != 40 || r.video.maxResolution != (resolution{1280, 720}) || r.video.maxFrameRate != 30 {
			t.Errorf("got %d envelope %+v", r.streamID, r.video)
		}
		if n := len(sliceAVideoAllocates(io)); n != 1 {
			t.Errorf("allocates = %d, want 1", n)
		}
	})

	t.Run("reports the ceiling the budget lowered, so the caller is not left guessing", func(t *testing.T) {
		m, _, _ := managerWith(halfBudget(), sliceAEncoderBudgeted(sliceAHalfSensorBudget))
		r, err := m.resolveVideoStream(t.Context(), sliceARequest(nil))
		sliceANoErr(t, err)
		if r.video.maxFrameRate != 30 {
			t.Errorf("maxFrameRate = %d, want 30", r.video.maxFrameRate)
		}
		if r.budgetNarrowed == nil || *r.budgetNarrowed != (videoBudgetNarrowing{maxResolution: resolution{2560, 1440}}) {
			t.Errorf("budgetNarrowed = %+v, want maxResolution 2560x1440 only", r.budgetNarrowed)
		}
		if r.degraded {
			t.Error("degraded = true")
		}
	})

	t.Run("reports no budget narrowing for a stream it reused", func(t *testing.T) {
		st := sliceAWithStreams(sliceAContained()).with(func(s *cameraState) { s.maxEncodedPixelRate = stated(sliceAHalfSensorBudget) })
		m, _, _ := managerWith(st, sliceAEncoderBudgeted(sliceAHalfSensorBudget))
		r, err := m.resolveVideoStream(t.Context(), sliceARequest(sliceAPinned1080p()))
		sliceANoErr(t, err)
		if r.provenance == ProvenanceAllocated || r.budgetNarrowed != nil {
			t.Errorf("got %s budgetNarrowed %+v", r.provenance, r.budgetNarrowed)
		}
	})

	t.Run("reuses a stream it allocated at a frame rate the encoder budget lowered", func(t *testing.T) {
		pinned := sliceAHints(videoRangeBounds{minResolution: resolution{2560, 1440}, maxResolution: resolution{2560, 1440}})
		m, io, _ := managerWith(halfBudget(), sliceAEncoderBudgeted(sliceAHalfSensorBudget))
		r, err := m.resolveVideoStream(t.Context(), sliceARequest(pinned))
		sliceANoErr(t, err)
		first := *r.video
		if first.maxFrameRate != 15 {
			t.Fatalf("first maxFrameRate = %d, want 15", first.maxFrameRate)
		}
		io.setState(halfBudget().with(func(s *cameraState) {
			s.video = []allocatedVideoStream{{
				videoStreamID: 40, overlays: noOverlays, streamUsage: liveView, videoCodec: h265,
				minResolution: first.minResolution, maxResolution: first.maxResolution,
				minFrameRate: first.minFrameRate, maxFrameRate: first.maxFrameRate,
				minBitRate: first.minBitRate, maxBitRate: first.maxBitRate,
				keyFrameInterval: first.keyFrameInterval, referenceCount: 1,
			}}
		}))
		before := len(sliceAVideoAllocates(io))
		second, err := m.resolveVideoStream(t.Context(), sliceARequest(pinned))
		sliceANoErr(t, err)
		if second.streamID != 40 || second.provenance == ProvenanceAllocated || len(sliceAVideoAllocates(io)) != before {
			t.Errorf("got %d %s with %d allocates (before %d)", second.streamID, second.provenance, len(sliceAVideoAllocates(io)), before)
		}
	})

	t.Run("would have been refused by the same camera without the budget", func(t *testing.T) {
		m, io, _ := managerWith(fixtureState().with(func(s *cameraState) { s.maxEncodedPixelRate = ceiling{} }), sliceAEncoderBudgeted(sliceAHalfSensorBudget))
		_, err := m.resolveVideoStream(t.Context(), sliceARequest(nil))
		sliceANoErr(t, err)
		a := sliceAVideoAllocates(io)
		if len(a) <= 1 || a[0].MaxFrameRate != 30 {
			t.Errorf("allocates = %+v, want a 30 fps first ask and a retry", a)
		}
	})

	t.Run("counts what the camera's other streams reserve, not just its ceiling", func(t *testing.T) {
		const budget = 110592000
		st := sliceAWithStreams(sliceAContained()).with(func(s *cameraState) { s.maxEncodedPixelRate = stated(budget) })
		m, io, _ := managerWith(st, sliceAEncoderBudgeted(budget-1920*1080*30))
		r, err := m.resolveVideoStream(t.Context(), sliceARequest(sliceAHints(videoRangeBounds{maxFrameRate: 30, minResolution: resolution{2560, 1440}})))
		sliceANoErr(t, err)
		allocs := sliceAVideoAllocates(io)
		rates := make([]uint16, 0, len(allocs))
		for _, a := range allocs {
			rates = append(rates, a.MaxFrameRate)
		}
		if r.video.maxFrameRate != 7 || !slices.Equal(rates, []uint16{7}) {
			t.Errorf("got %d fps, asks %v, want 7 and [7]", r.video.maxFrameRate, rates)
		}
	})

	t.Run("sends a floor the caller stated to the device although the budget cannot carry it", func(t *testing.T) {
		m, io, _ := managerWith(halfBudget(), allocateVideo(41))
		r, err := m.resolveVideoStream(t.Context(), sliceARequest(sliceAHints(videoRangeBounds{minFrameRate: 30})))
		sliceANoErr(t, err)
		a := sliceAVideoAllocates(io)
		if r.streamID != 41 || len(a) == 0 || a[0].MinFrameRate != 30 || a[0].MaxFrameRate != 30 {
			t.Errorf("got %d, allocates %+v", r.streamID, a)
		}
	})

	t.Run("narrows its own envelope before taking a stream that is not being used", func(t *testing.T) {
		idle := sliceAStream(func(s *allocatedVideoStream) { s.referenceCount = 0 })
		attempts := 0
		m, io, _ := managerWith(sliceAWithStreams(idle), func(r *recordedInvoke) (any, error) {
			if r.command != cmdVideoStreamAllocate {
				return nil, nil
			}
			attempts++
			if attempts == 1 {
				return nil, statusErr(im.StatusResourceExhausted)
			}
			return csdef.VideoStreamAllocateResponse{VideoStreamId: 42}, nil
		})
		r, err := m.resolveVideoStream(t.Context(), sliceARequest(sliceAMax720p()))
		sliceANoErr(t, err)
		if r.streamID != 42 || r.evicted != nil || slices.Contains(io.commands(), cmdVideoStreamDeallocate) {
			t.Errorf("got %d evicted %v with %v", r.streamID, r.evicted, io.commands())
		}
	})

	t.Run("names the stream it took in the result", func(t *testing.T) {
		idle := sliceAStream(func(s *allocatedVideoStream) { s.referenceCount = 0 })
		freed := 0
		m, _, _ := managerWith(sliceAWithStreams(idle), sliceAFreeThenAllocate(&freed, 1, 43))
		r, err := m.resolveVideoStream(t.Context(), sliceARequest(sliceAMax720p()))
		sliceANoErr(t, err)
		if r.streamID != 43 || !slices.Equal(r.evicted, []uint16{7}) || r.video.maxResolution != (resolution{1280, 720}) {
			t.Errorf("got %d evicted %v envelope %+v", r.streamID, r.evicted, r.video)
		}
	})

	t.Run("fails rather than taking a stream when the caller forbade eviction", func(t *testing.T) {
		idle := sliceAStream(func(s *allocatedVideoStream) { s.referenceCount = 0 })
		m, io, _ := managerWith(sliceAWithStreams(idle), sliceAVideoOnlyExhausted)
		req := sliceARequest(sliceAMax720p())
		req.allowEviction = false
		_, err := m.resolveVideoStream(t.Context(), req)
		sliceAExhausted(t, err)
		if slices.Contains(io.commands(), cmdVideoStreamDeallocate) {
			t.Errorf("commands = %v, want no deallocate", io.commands())
		}
	})

	t.Run("counts an encoded-pixels snapshot stream against the video budget", func(t *testing.T) {
		snapshot := allocatedSnapshotStream{
			snapshotStreamID: 4, overlays: noOverlays, imageCodec: csdef.ImageCodecJpeg,
			minResolution: resolution{2560, 1440}, maxResolution: resolution{2560, 1440},
			quality: 90, frameRate: 15, encodedPixels: true,
		}
		const budget = 2560 * 1440 * 30
		st := fixtureState().with(func(s *cameraState) {
			s.maxEncodedPixelRate = stated(budget)
			s.snapshot = []allocatedSnapshotStream{snapshot}
		})
		m, io, _ := managerWith(st, sliceAEncoderBudgeted(budget-2560*1440*15))
		r, err := m.resolveVideoStream(t.Context(), sliceARequest(nil))
		sliceANoErr(t, err)
		if r.video.maxResolution != (resolution{1280, 720}) || len(sliceAVideoAllocates(io)) != 1 {
			t.Errorf("envelope %+v with %d allocates", r.video, len(sliceAVideoAllocates(io)))
		}
	})

	t.Run("never takes another controller's stream the degraded rung could have handed out", func(t *testing.T) {
		usable := sliceAStream(func(s *allocatedVideoStream) { s.referenceCount, s.minBitRate, s.maxBitRate = 0, 100000, 200000 })
		m, io, _ := managerWith(sliceAWithStreams(usable), sliceAVideoOnlyExhausted)
		r, err := m.resolveVideoStream(t.Context(), sliceARequest(sliceAPinned1080p()))
		sliceANoErr(t, err)
		if r.streamID != 7 || !r.degraded || r.evicted != nil || slices.Contains(io.commands(), cmdVideoStreamDeallocate) {
			t.Errorf("got %d degraded=%v evicted %v with %v", r.streamID, r.degraded, r.evicted, io.commands())
		}
	})

	t.Run("reports a stream it took even when the degraded rung is what served the caller", func(t *testing.T) {
		victim := sliceAStream(func(s *allocatedVideoStream) {
			s.videoStreamID, s.videoCodec, s.referenceCount = 20, sliceAOtherCodec, 0
		})
		busy := sliceAStream(func(s *allocatedVideoStream) { s.maxResolution = resolution{3840, 2160} })
		m, io, _ := managerWith(sliceAWithStreams(victim, busy), sliceAVideoOnlyExhausted)
		r, err := m.resolveVideoStream(t.Context(), sliceARequest(nil))
		sliceANoErr(t, err)
		if d := sliceAVideoDeallocated(io); r.streamID != 7 || !r.degraded || !slices.Equal(r.evicted, []uint16{20}) || !slices.Equal(d, []uint16{20}) {
			t.Errorf("got %d degraded=%v evicted %v, deallocates %v", r.streamID, r.degraded, r.evicted, d)
		}
	})

	t.Run("takes a stream it allocated itself before a foreign one the camera ranks lower", func(t *testing.T) {
		idle := func(id uint16, usage csdef.StreamUsageEnum) allocatedVideoStream {
			return sliceAStream(func(s *allocatedVideoStream) { s.videoStreamID, s.streamUsage, s.referenceCount = id, usage, 0 })
		}
		allocates, freed := 0, 0
		m, io, _ := managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
			if r.command == cmdVideoStreamDeallocate {
				freed++
			}
			if r.command != cmdVideoStreamAllocate {
				return nil, nil
			}
			allocates++
			switch {
			case allocates == 1:
				return csdef.VideoStreamAllocateResponse{VideoStreamId: 30}, nil
			case freed < 2:
				return nil, statusErr(im.StatusResourceExhausted)
			}
			return csdef.VideoStreamAllocateResponse{VideoStreamId: 44}, nil
		})
		recording := sliceARequest(nil)
		recording.usage = csdef.StreamUsageRecording
		_, err := m.resolveVideoStream(t.Context(), recording)
		sliceANoErr(t, err)
		io.setState(sliceAWithStreams(idle(31, csdef.StreamUsageAnalysis), idle(30, csdef.StreamUsageRecording)))
		r, err := m.resolveVideoStream(t.Context(), sliceARequest(sliceAMax720p()))
		sliceANoErr(t, err)
		if r.streamID != 44 || !slices.Equal(r.evicted, []uint16{30, 31}) {
			t.Errorf("got %d evicted %v, want 44 [30 31]", r.streamID, r.evicted)
		}
	})

	t.Run("names every stream it took, not only the first", func(t *testing.T) {
		idle := func(id uint16) allocatedVideoStream {
			return sliceAStream(func(s *allocatedVideoStream) {
				s.videoStreamID, s.videoCodec, s.streamUsage, s.referenceCount = id, sliceAOtherCodec, csdef.StreamUsageRecording, 0
			})
		}
		freed := 0
		m, _, _ := managerWith(sliceAWithStreams(idle(20), idle(21)), sliceAFreeThenAllocate(&freed, 2, 44))
		r, err := m.resolveVideoStream(t.Context(), sliceARequest(sliceAMax720p()))
		sliceANoErr(t, err)
		if r.streamID != 44 || !slices.Equal(r.evicted, []uint16{20, 21}) {
			t.Errorf("got %d evicted %v, want 44 [20 21]", r.streamID, r.evicted)
		}
	})

	t.Run("stops taking streams when no allocate attempt is left to use the capacity", func(t *testing.T) {
		idles := make([]allocatedVideoStream, 0, 40)
		for i := range 40 {
			idles = append(idles, sliceAStream(func(s *allocatedVideoStream) {
				s.videoStreamID, s.videoCodec, s.streamUsage = uint16(20+i), sliceAOtherCodec, csdef.StreamUsageRecording
				s.minResolution, s.maxResolution, s.maxFrameRate, s.referenceCount = resolution{320, 240}, resolution{320, 240}, 5, 0
			}))
		}
		m, io, _ := managerWith(sliceAWithStreams(idles...), sliceAVideoOnlyExhausted)
		_, err := m.resolveVideoStream(t.Context(), sliceARequest(sliceAMax720p()))
		sliceAExhausted(t, err)
		attempts := 0
		for _, a := range sliceAVideoAllocates(io) {
			if a.StreamUsage == liveView {
				attempts++
			}
		}
		const windowsBeforeEviction = 2
		if d := len(sliceAVideoDeallocated(io)); attempts != maxVideoAsks || d != (maxVideoAsks-windowsBeforeEviction)/windowsBeforeEviction {
			t.Errorf("attempts = %d, deallocates = %d, want %d and %d", attempts, d, maxVideoAsks, (maxVideoAsks-windowsBeforeEviction)/windowsBeforeEviction)
		}
	})

	t.Run("still reaches the degraded rung when the caller forbade eviction", func(t *testing.T) {
		idle := sliceAStream(func(s *allocatedVideoStream) {
			s.videoStreamID, s.videoCodec, s.referenceCount = 20, sliceAOtherCodec, 0
		})
		busy := sliceAStream(func(s *allocatedVideoStream) { s.maxResolution = resolution{3840, 2160} })
		m, io, _ := managerWith(sliceAWithStreams(idle, busy), sliceAVideoOnlyExhausted)
		req := sliceARequest(nil)
		req.allowEviction = false
		r, err := m.resolveVideoStream(t.Context(), req)
		sliceANoErr(t, err)
		if r.streamID != 7 || !r.degraded || r.evicted != nil || slices.Contains(io.commands(), cmdVideoStreamDeallocate) {
			t.Errorf("got %d degraded=%v evicted %v with %v", r.streamID, r.degraded, r.evicted, io.commands())
		}
	})

	t.Run("fails typed with the allocated list once the ladder is exhausted", func(t *testing.T) {
		m, io, _ := managerWith(sliceAWithStreams(sliceAContained()), sliceAAlways(statusErr(im.StatusResourceExhausted)))
		_, err := m.resolveVideoStream(t.Context(), sliceARequest(sliceAMax720p()))
		e := sliceAExhausted(t, err)
		if !slices.Equal(e.Allocated, []OccupyingStream{{Kind: StreamVideo, StreamID: 7, ReferenceCount: 1}}) {
			t.Errorf("allocated = %+v", e.Allocated)
		}
		if n := len(io.recorded()); n != 4 {
			t.Errorf("invokes = %d (%v), want 4", n, io.commands())
		}
	})

	t.Run("fails typed when no codec suits both the camera and the offer", func(t *testing.T) {
		m, _, _ := managerWith(fixtureState(), nil)
		req := sliceARequest(nil)
		req.limits.codec = 99
		_, err := m.resolveVideoStream(t.Context(), req)
		e := sliceAIncompatible(t, err)
		if !slices.Equal(e.Device, []string{"H265"}) || !slices.Equal(e.Requested, []string{"99"}) {
			t.Errorf("err = %+v", e)
		}
	})

	t.Run("names a codec the enum knows when the camera does not support it", func(t *testing.T) {
		m, _, _ := managerWith(fixtureState(), nil)
		req := sliceARequest(nil)
		req.limits.codec = csdef.VideoCodecH264
		_, err := m.resolveVideoStream(t.Context(), req)
		e := sliceAIncompatible(t, err)
		if !slices.Equal(e.Requested, []string{"H264"}) || !slices.Equal(e.Device, []string{"H265"}) {
			t.Errorf("err = %+v", e)
		}
	})

	t.Run("allocates once when two callers race for the same endpoint", func(t *testing.T) {
		var mu sync.Mutex
		ids := []uint16{9, 10}
		m, io, _ := managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
			if r.command != cmdVideoStreamAllocate {
				return nil, nil
			}
			mu.Lock()
			defer mu.Unlock()
			id := ids[0]
			ids = ids[1:]
			return csdef.VideoStreamAllocateResponse{VideoStreamId: id}, nil
		})
		results := make([]*resolvedStream, 2)
		errs := make([]error, 2)
		var wg sync.WaitGroup
		for i := range results {
			wg.Go(func() { results[i], errs[i] = m.resolveVideoStream(t.Context(), sliceARequest(nil)) })
		}
		wg.Wait()
		sliceANoErr(t, errors.Join(errs...))
		if n := len(sliceAVideoAllocates(io)); n != 1 {
			t.Errorf("allocates = %d, want 1", n)
		}
		allocated := 0
		for _, r := range results {
			if r.streamID != 9 {
				t.Errorf("streamID = %d, want 9", r.streamID)
			}
			if r.provenance == ProvenanceAllocated {
				allocated++
			}
		}
		if allocated != 1 {
			t.Errorf("%d results allocated, want exactly the first caller's", allocated)
		}
	})
}

// sliceAVideoOnlyExhausted refuses every video allocate for capacity and
// answers anything else with nothing.
func sliceAVideoOnlyExhausted(r *recordedInvoke) (any, error) {
	if r.command == cmdVideoStreamAllocate {
		return nil, statusErr(im.StatusResourceExhausted)
	}
	return nil, nil
}

// sliceAFreeThenAllocate refuses video allocates for capacity until need
// deallocates have been sent, then answers with id.
func sliceAFreeThenAllocate(freed *int, need int, id uint16) func(*recordedInvoke) (any, error) {
	return func(r *recordedInvoke) (any, error) {
		if r.command == cmdVideoStreamDeallocate {
			*freed++
		}
		if r.command != cmdVideoStreamAllocate {
			return nil, nil
		}
		if *freed < need {
			return nil, statusErr(im.StatusResourceExhausted)
		}
		return csdef.VideoStreamAllocateResponse{VideoStreamId: id}, nil
	}
}

// The withEndpointLock case (CameraStreamManagerTest.ts:2150-2171,
// matterjs-server at 0bd06f2); the pendingLockCount subclass hook is
// m.locks.pending().
func TestSliceAWithEndpointLockParityMatterjsServer(t *testing.T) {
	t.Run("clears the per-endpoint lock once the work it guards has settled", func(t *testing.T) {
		m, _, _ := managerWith(fixtureState(), func(*recordedInvoke) (any, error) {
			return csdef.VideoStreamAllocateResponse{VideoStreamId: 9}, nil
		})
		for range 2 {
			_, err := m.resolveVideoStream(t.Context(), sliceARequest(nil))
			sliceANoErr(t, err)
		}
		if n := m.locks.pending(); n != 0 {
			t.Errorf("pending locks = %d, want 0", n)
		}
	})
}

// sliceAAudioStream is the describe's EXISTING_AUDIO_STREAM
// (CameraStreamManagerTest.ts:2178-2187, matterjs-server at 0bd06f2).
func sliceAAudioStream() allocatedAudioStream {
	return allocatedAudioStream{
		audioStreamID: 4, streamUsage: liveView, audioCodec: csdef.AudioCodecOpus,
		channelCount: 1, sampleRate: 48000, bitRate: 64000, bitDepth: 16, referenceCount: 1,
	}
}

func sliceAWithAudio(streams ...allocatedAudioStream) *cameraState {
	return fixtureState().with(func(s *cameraState) { s.audio = streams })
}

// sliceAResolveAudio is resolveAudioStream as the source's caller states
// it: an audio value demands the track, none defers it.
func sliceAResolveAudio(ctx context.Context, m *manager, sdp *sdpVideoConstraints, hints *audioHints) (*resolvedStream, error) {
	state := trackDeferred
	if hints != nil {
		state = trackDemanded
	}
	return m.resolveAudioStream(ctx, audioRequest{t: tTarget, usage: liveView, sdp: sdp, hints: hints}, state)
}

// sliceAAACOnly is an offer whose only video is absent and whose audio
// receives AAC alone.
func sliceAAACOnly(codecs []string) *sdpVideoConstraints {
	return &sdpVideoConstraints{
		video: mediaDisposition{state: mediaAbsent},
		audio: mediaDisposition{state: mediaReceiving, codecs: codecs},
	}
}

func sliceAAllocateAudio(id uint16) func(*recordedInvoke) (any, error) {
	return func(*recordedInvoke) (any, error) { return csdef.AudioStreamAllocateResponse{AudioStreamId: id}, nil }
}

// The resolveAudioStream cases (CameraStreamManagerTest.ts:2173-2597,
// matterjs-server at 0bd06f2).
func TestSliceAResolveAudioStreamParityMatterjsServer(t *testing.T) {
	t.Run("reuses a matching audio stream without invoking anything", func(t *testing.T) {
		m, io, _ := managerWith(sliceAWithAudio(sliceAAudioStream()), nil)
		r, err := sliceAResolveAudio(t.Context(), m, nil, nil)
		sliceANoErr(t, err)
		if r == nil || r.streamID != 4 || r.provenance != ProvenanceAdopted || len(io.recorded()) != 0 {
			t.Errorf("got %+v with %v, want 4 adopted and no invoke", r, io.commands())
		}
	})

	t.Run("reuses an audio stream allocated for another usage without invoking anything", func(t *testing.T) {
		recording := sliceAAudioStream()
		recording.streamUsage = csdef.StreamUsageRecording
		m, io, _ := managerWith(sliceAWithAudio(recording), nil)
		r, err := sliceAResolveAudio(t.Context(), m, nil, nil)
		sliceANoErr(t, err)
		if r == nil || r.streamID != 4 || len(io.recorded()) != 0 {
			t.Errorf("got %+v with %v, want 4 and no invoke", r, io.commands())
		}
	})

	t.Run("reuses the audio stream of the session's usage when another usage fits as well", func(t *testing.T) {
		recording := sliceAAudioStream()
		recording.streamUsage = csdef.StreamUsageRecording
		live := sliceAAudioStream()
		live.audioStreamID = 5
		m, _, _ := managerWith(sliceAWithAudio(recording, live), nil)
		r, err := sliceAResolveAudio(t.Context(), m, nil, nil)
		sliceANoErr(t, err)
		if r == nil || r.streamID != 5 {
			t.Errorf("got %+v, want 5", r)
		}
	})

	t.Run("reports the reused audio stream's own bitRate, not the freshly computed default", func(t *testing.T) {
		custom := sliceAAudioStream()
		custom.bitRate = 32000
		m, _, _ := managerWith(sliceAWithAudio(custom), nil)
		r, err := sliceAResolveAudio(t.Context(), m, nil, nil)
		sliceANoErr(t, err)
		if r == nil || r.provenance == ProvenanceAllocated || r.audio.bitRate != 32000 {
			t.Errorf("got %+v, want the stream's own 32000", r)
		}
	})

	t.Run("allocates rather than reporting another stream's bit rate as the caller's", func(t *testing.T) {
		m, io, _ := managerWith(sliceAWithAudio(sliceAAudioStream()), sliceAAllocateAudio(9))
		r, err := sliceAResolveAudio(t.Context(), m, nil, &audioHints{bitRate: 32000})
		sliceANoErr(t, err)
		if r == nil || r.streamID != 9 || r.provenance != ProvenanceAllocated {
			t.Fatalf("got %+v, want 9 allocated", r)
		}
		recorded := io.recorded()
		if f, ok := recorded[0].fields.(csdef.AudioStreamAllocateRequest); !ok || f.BitRate != 32000 {
			t.Errorf("allocate = %+v, want bitRate 32000", recorded[0].fields)
		}
	})

	t.Run("reuses a stream that already carries the bit rate the caller asked for", func(t *testing.T) {
		m, io, _ := managerWith(sliceAWithAudio(sliceAAudioStream()), nil)
		r, err := sliceAResolveAudio(t.Context(), m, nil, &audioHints{bitRate: 64000})
		sliceANoErr(t, err)
		if r == nil || r.streamID != 4 || len(io.recorded()) != 0 {
			t.Errorf("got %+v with %v, want 4 and no invoke", r, io.commands())
		}
	})

	t.Run("fails typed when the camera lists no such sample rate, rather than picking its own", func(t *testing.T) {
		m, io, _ := managerWith(fixtureState(), sliceAAllocateAudio(9))
		_, err := sliceAResolveAudio(t.Context(), m, nil, &audioHints{sampleRate: 44100})
		e := sliceAIncompatible(t, err)
		want := IncompatibleBound{Field: BoundSampleRate, Requested: "44100", Limit: "48000"}
		if e.Bound == nil || *e.Bound != want {
			t.Errorf("bound = %+v, want %+v", e.Bound, want)
		}
		if len(io.recorded()) != 0 {
			t.Errorf("invokes = %v, want none", io.commands())
		}
	})

	t.Run("fails typed when the caller asks for more channels than the camera has", func(t *testing.T) {
		m, _, _ := managerWith(fixtureState(), sliceAAllocateAudio(9))
		_, err := sliceAResolveAudio(t.Context(), m, nil, &audioHints{channelCount: 2})
		e := sliceAIncompatible(t, err)
		want := IncompatibleBound{Field: BoundChannelCount, Requested: "2", Limit: "1"}
		if e.Bound == nil || *e.Bound != want {
			t.Errorf("bound = %+v, want %+v", e.Bound, want)
		}
	})

	t.Run("allocates a new audio stream and records it as owned by the server", func(t *testing.T) {
		m, io, _ := managerWith(fixtureState(), sliceAAllocateAudio(9))
		r, err := sliceAResolveAudio(t.Context(), m, nil, nil)
		sliceANoErr(t, err)
		if r == nil || r.streamID != 9 || r.provenance != ProvenanceAllocated || !slices.Equal(io.commands(), []string{cmdAudioStreamAllocate}) {
			t.Errorf("got %+v with %v", r, io.commands())
		}
	})

	t.Run("fails typed when a caller that asked for audio gets none from the codec narrowing", func(t *testing.T) {
		m, _, _ := managerWith(fixtureState(), sliceAAllocateAudio(9))
		_, err := sliceAResolveAudio(t.Context(), m, sliceAAACOnly([]string{"AAC"}), &audioHints{codecs: []string{"OPUS"}})
		e := sliceAIncompatible(t, err)
		if e.Reason != ReasonCodec || !slices.Equal(e.Device, []string{"OPUS"}) || !slices.Equal(e.Requested, []string{"OPUS"}) {
			t.Errorf("err = %+v", e)
		}
	})

	t.Run("goes video-only for the same narrowing when the caller stated no audio value", func(t *testing.T) {
		m, _, _ := managerWith(fixtureState(), sliceAAllocateAudio(9))
		r, err := sliceAResolveAudio(t.Context(), m, sliceAAACOnly([]string{"AAC"}), nil)
		sliceANoErr(t, err)
		if r != nil {
			t.Errorf("got %+v, want none", r)
		}
	})

	t.Run("keeps the camera's audio codecs when the offered audio section states none", func(t *testing.T) {
		m, _, _ := managerWith(fixtureState(), sliceAAllocateAudio(9))
		r, err := sliceAResolveAudio(t.Context(), m, sliceAAACOnly(nil), &audioHints{bitRate: 32000})
		sliceANoErr(t, err)
		if r == nil || r.streamID != 9 {
			t.Errorf("got %+v, want 9", r)
		}
	})

	noMicrophone := func() *cameraState {
		return fixtureState().with(func(s *cameraState) { s.microphone = nil })
	}

	t.Run("reads an empty audio object as asking for audio", func(t *testing.T) {
		m, _, _ := managerWith(noMicrophone(), nil)
		_, err := sliceAResolveAudio(t.Context(), m, nil, &audioHints{})
		if e := sliceAIncompatible(t, err); e.Track != TrackAudio {
			t.Errorf("track = %q, want audio", e.Track)
		}
	})

	t.Run("fails typed when a caller that asked for audio meets a camera with no microphone", func(t *testing.T) {
		m, _, _ := managerWith(noMicrophone(), nil)
		_, err := sliceAResolveAudio(t.Context(), m, nil, &audioHints{bitRate: 32000})
		if e := sliceAIncompatible(t, err); e.Reason != ReasonCapability {
			t.Errorf("reason = %q, want capability", e.Reason)
		}
	})

	t.Run("fails when a caller that asked for audio gets an allocate response with no id", func(t *testing.T) {
		m, _, _ := managerWith(fixtureState(), nil)
		_, err := sliceAResolveAudio(t.Context(), m, nil, &audioHints{bitRate: 32000})
		var le *StackError
		if !errors.As(err, &le) {
			t.Errorf("err = %v, want *StackError", err)
		}
	})

	t.Run("fails typed when a caller that asked for audio meets a device that refuses to allocate", func(t *testing.T) {
		occupied := fixtureState().with(func(s *cameraState) {
			s.video = []allocatedVideoStream{sliceAStream(func(v *allocatedVideoStream) { v.videoStreamID, v.keyFrameInterval = 8, 4000 })}
			s.audio = []allocatedAudioStream{sliceAAudioStream()}
		})
		m, _, _ := managerWith(occupied, sliceAAlways(statusErr(im.StatusResourceExhausted)))
		_, err := sliceAResolveAudio(t.Context(), m, nil, &audioHints{bitRate: 32000})
		e := sliceAExhausted(t, err)
		if e.Error() != "Camera has no capacity for this stream" ||
			!slices.Equal(e.Allocated, []OccupyingStream{{Kind: StreamAudio, StreamID: 4, ReferenceCount: 1}}) ||
			e.MaxEncoders == nil || *e.MaxEncoders != 1 || e.MaxPixelRate == nil || *e.MaxPixelRate != 248832000 {
			t.Errorf("err = %+v", e)
		}
	})

	t.Run("raises the device's own error when a caller that asked for audio meets a status the ladder does not know", func(t *testing.T) {
		m, _, _ := managerWith(fixtureState(), sliceAAlways(statusErr(im.StatusBusy)))
		_, err := sliceAResolveAudio(t.Context(), m, nil, &audioHints{bitRate: 32000})
		if s, ok := deviceStatusOf(err); isTyped(err) || !ok || s != im.StatusBusy {
			t.Errorf("err = %v, want the device's Busy", err)
		}
	})

	t.Run("fails typed when a caller that asked for audio meets a device that rejects the range", func(t *testing.T) {
		m, _, _ := managerWith(fixtureState(), sliceAAlways(statusErr(im.StatusDynamicConstraintError)))
		_, err := sliceAResolveAudio(t.Context(), m, nil, &audioHints{bitRate: 32000})
		if e := sliceAIncompatible(t, err); e.DeviceStatus == nil || *e.DeviceStatus != im.StatusDynamicConstraintError {
			t.Errorf("deviceStatus = %v, want DynamicConstraintError", e.DeviceStatus)
		}
	})

	t.Run("returns undefined rather than failing when the device has no microphone", func(t *testing.T) {
		m, _, _ := managerWith(noMicrophone(), nil)
		r, err := sliceAResolveAudio(t.Context(), m, nil, nil)
		if r != nil || err != nil {
			t.Errorf("got %+v, %v, want none", r, err)
		}
	})

	t.Run("returns undefined rather than computing -Infinity when a capability list is empty", func(t *testing.T) {
		st := fixtureState().with(func(s *cameraState) {
			s.microphone = &audioCapabilities{supportedCodecs: []csdef.AudioCodecEnum{csdef.AudioCodecOpus}, maxNumberOfChannels: 1, supportedSampleRates: []int{}, supportedBitDepths: []int{16}}
		})
		m, io, _ := managerWith(st, nil)
		r, err := sliceAResolveAudio(t.Context(), m, nil, nil)
		if r != nil || err != nil || len(io.recorded()) != 0 {
			t.Errorf("got %+v, %v with %v, want none", r, err, io.commands())
		}
	})

	t.Run("returns undefined rather than failing when the device rejects audio allocation", func(t *testing.T) {
		m, _, _ := managerWith(fixtureState(), sliceAAlways(errors.New("device refused audio allocation")))
		r, err := sliceAResolveAudio(t.Context(), m, nil, nil)
		if r != nil || err != nil {
			t.Errorf("got %+v, %v, want none", r, err)
		}
	})

	t.Run("returns undefined rather than failing when the response carries no AudioStreamID", func(t *testing.T) {
		m, _, _ := managerWith(fixtureState(), nil)
		r, err := sliceAResolveAudio(t.Context(), m, nil, nil)
		if r != nil || err != nil {
			t.Errorf("got %+v, %v, want none", r, err)
		}
	})

	t.Run("rethrows a ServerError from the device rather than treating it as a missing microphone", func(t *testing.T) {
		m, _, _ := managerWith(fixtureState(), sliceAAlways(&NotSupportedError{Missing: []uint32{}}))
		_, err := sliceAResolveAudio(t.Context(), m, nil, nil)
		var ns *NotSupportedError
		if !errors.As(err, &ns) {
			t.Errorf("err = %v, want *NotSupportedError", err)
		}
	})
}

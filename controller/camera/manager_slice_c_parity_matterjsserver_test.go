// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
	"github.com/SukramJ/go-fabric/im"
)

// The cases of matterjs-server's CameraStreamManagerTest.ts:5602-7815 at
// 0bd06f2 (snapshot, camera_not_supported, releaseStream, leases and the
// reuse before the device has reported), ported by meaning over the
// managerWith fixture. probeWith's LeaseProbe is leasedEndpoints, and
// MockTime is the fixture's fake clock.

func sliceCRes(w, h uint16) csdef.VideoResolutionStruct {
	return csdef.VideoResolutionStruct{Width: w, Height: h}
}

// sliceCFrame is a one-byte JPEG of the size.
func sliceCFrame(w, h uint16) csdef.CaptureSnapshotResponse {
	return csdef.CaptureSnapshotResponse{Data: []byte{1}, ImageCodec: csdef.ImageCodecJpeg, Resolution: sliceCRes(w, h)}
}

// sliceCSnapshotCamera answers SnapshotStreamAllocate with id and
// CaptureSnapshot with frame.
func sliceCSnapshotCamera(id uint16, frame csdef.CaptureSnapshotResponse) func(*recordedInvoke) (any, error) {
	return func(r *recordedInvoke) (any, error) {
		switch r.command {
		case cmdSnapshotStreamAllocate:
			return csdef.SnapshotStreamAllocateResponse{SnapshotStreamId: id}, nil
		case cmdCaptureSnapshot:
			return frame, nil
		}
		return nil, nil
	}
}

// sliceCFieldsOf is the request of every invoke of command, in order.
func sliceCFieldsOf[T any](io *fakeIO, command string) []T {
	var out []T
	for _, r := range io.recorded() {
		if r.command == command {
			out = append(out, r.fields.(T))
		}
	}
	return out
}

func sliceCCount(io *fakeIO, command string) int {
	n := 0
	for _, c := range io.commands() {
		if c == command {
			n++
		}
	}
	return n
}

// sliceCRequested is the RequestedResolution of the first CaptureSnapshot.
func sliceCRequested(t *testing.T, io *fakeIO) csdef.VideoResolutionStruct {
	t.Helper()
	captures := sliceCFieldsOf[csdef.CaptureSnapshotRequest](io, cmdCaptureSnapshot)
	if len(captures) == 0 {
		t.Fatal("no captureSnapshot invoked")
	}
	return captures[0].RequestedResolution
}

// sliceCCapturedFrom is the stream id of the first CaptureSnapshot.
func sliceCCapturedFrom(t *testing.T, io *fakeIO) uint16 {
	t.Helper()
	captures := sliceCFieldsOf[csdef.CaptureSnapshotRequest](io, cmdCaptureSnapshot)
	if len(captures) == 0 {
		t.Fatal("no captureSnapshot invoked")
	}
	return captures[0].SnapshotStreamId.Value
}

// sliceCMap is pick of every request of command, in order.
func sliceCMap[T, V any](io *fakeIO, command string, pick func(T) V) []V {
	fields := sliceCFieldsOf[T](io, command)
	out := make([]V, 0, len(fields))
	for _, f := range fields {
		out = append(out, pick(f))
	}
	return out
}

func sliceCSnapshotDeallocs(io *fakeIO) []uint16 {
	return sliceCMap(io, cmdSnapshotStreamDeallocate, func(f csdef.SnapshotStreamDeallocateRequest) uint16 { return f.SnapshotStreamId })
}

func sliceCVideoDeallocs(io *fakeIO) []uint16 {
	return sliceCMap(io, cmdVideoStreamDeallocate, func(f csdef.VideoStreamDeallocateRequest) uint16 { return f.VideoStreamId })
}

// sliceCAllocatedMax and sliceCAllocatedMin are the resolutions every
// SnapshotStreamAllocate asked for.
func sliceCAllocatedMax(io *fakeIO) []csdef.VideoResolutionStruct {
	return sliceCMap(io, cmdSnapshotStreamAllocate, func(f csdef.SnapshotStreamAllocateRequest) csdef.VideoResolutionStruct { return f.MaxResolution })
}

func sliceCAllocatedMin(io *fakeIO) []csdef.VideoResolutionStruct {
	return sliceCMap(io, cmdSnapshotStreamAllocate, func(f csdef.SnapshotStreamAllocateRequest) csdef.VideoResolutionStruct { return f.MinResolution })
}

// sliceCRestore is the first VideoStreamAllocate of usage Recording: the
// restore of the freed foreign stream.
func sliceCRestore(t *testing.T, io *fakeIO) csdef.VideoStreamAllocateRequest {
	t.Helper()
	for _, f := range sliceCFieldsOf[csdef.VideoStreamAllocateRequest](io, cmdVideoStreamAllocate) {
		if f.StreamUsage == csdef.StreamUsageRecording {
			return f
		}
	}
	t.Fatal("no restoring videoStreamAllocate")
	return csdef.VideoStreamAllocateRequest{}
}

func sliceCSnapshot(t *testing.T, m *manager, args snapshotArgs) *snapshotResult {
	t.Helper()
	args.t = tTarget
	r, err := m.snapshot(t.Context(), args)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	return r
}

func sliceCSnapshotErr(t *testing.T, m *manager, args snapshotArgs) error {
	t.Helper()
	args.t = tTarget
	r, err := m.snapshot(t.Context(), args)
	if err == nil {
		t.Fatalf("snapshot = %+v, want an error", r)
	}
	return err
}

// sliceCAs is err as E, failing the test otherwise.
func sliceCAs[E error](t *testing.T, err error) E {
	t.Helper()
	var e E
	if !errors.As(err, &e) {
		t.Fatalf("error %v (%T), want %T", err, err, e)
	}
	return e
}

func sliceCNoErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func sliceCEq[T comparable](t *testing.T, what string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

func sliceCSlice[T comparable](t *testing.T, what string, got, want []T) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

// sliceCHeldStream is a snapshot stream the device lists, 1920x1080 at
// id 8 unless edit changes it.
func sliceCHeldStream(edit func(*allocatedSnapshotStream)) allocatedSnapshotStream {
	s := allocatedSnapshotStream{
		snapshotStreamID: 8, overlays: noOverlays, imageCodec: csdef.ImageCodecJpeg,
		minResolution: resolution{1920, 1080}, maxResolution: resolution{1920, 1080}, quality: 90, frameRate: 1,
	}
	if edit != nil {
		edit(&s)
	}
	return s
}

// sliceCWithSnapshots is STATE listing the snapshot streams.
func sliceCWithSnapshots(streams ...allocatedSnapshotStream) *cameraState {
	return fixtureState().with(func(s *cameraState) { s.snapshot = streams })
}

// sliceCStreaming is STATE with live stream 1 referenced once.
func sliceCStreaming() *cameraState {
	return fixtureState().with(func(s *cameraState) { s.video = []allocatedVideoStream{liveStream(1, 1)} })
}

// sliceCForeign is the leases describe's FOREIGN_STREAM.
func sliceCForeign() allocatedVideoStream {
	return allocatedVideoStream{
		videoStreamID: 7, overlays: noOverlays, streamUsage: csdef.StreamUsageRecording, videoCodec: csdef.VideoCodecH264,
		minResolution: resolution{320, 240}, maxResolution: resolution{320, 240}, minFrameRate: 1, maxFrameRate: 5,
		minBitRate: 100000, maxBitRate: 200000, keyFrameInterval: 2000,
	}
}

// sliceCContained is s with CONTAINED_FOREIGN's bounds, reusable for an
// unhinted LiveView request.
func sliceCContained(s allocatedVideoStream) allocatedVideoStream {
	s.streamUsage, s.videoCodec = liveView, h265
	s.minResolution, s.maxResolution = resolution{1920, 1080}, resolution{1920, 1080}
	s.minFrameRate, s.maxFrameRate, s.minBitRate, s.maxBitRate = 1, 30, 800000, 4000000
	return s
}

func sliceCWithVideo(streams ...allocatedVideoStream) *cameraState {
	return fixtureState().with(func(s *cameraState) { s.video = streams })
}

// sliceCResolve resolves an H.265 video track of the usage.
func sliceCResolve(t *testing.T, m *manager, usage csdef.StreamUsageEnum, hints *videoHints) (*resolvedStream, error) {
	t.Helper()
	return m.resolveVideoStream(t.Context(), videoRequest{t: tTarget, usage: usage, limits: selectedVideoCodecLimits{codec: h265}, hints: hints, allowEviction: true})
}

// sliceCLiveView is the reuse describe's liveView.
func sliceCLiveView(t *testing.T, m *manager) *resolvedStream {
	t.Helper()
	s, err := sliceCResolve(t, m, liveView, nil)
	sliceCNoErr(t, err)
	return s
}

func sliceCAudio(t *testing.T, m *manager) *resolvedStream {
	t.Helper()
	s, err := m.resolveAudioStream(t.Context(), audioRequest{t: tTarget, usage: liveView}, trackDeferred)
	sliceCNoErr(t, err)
	if s == nil {
		t.Fatal("expected an audio stream")
	}
	return s
}

func sliceCRelease(t *testing.T, m *manager, kind StreamKind, id uint16) error {
	t.Helper()
	return m.releaseStream(t.Context(), tTarget, kind, id)
}

func sliceCCapabilities(t *testing.T, m *manager) *Capabilities {
	t.Helper()
	c, err := m.capabilities(t.Context(), tTarget)
	sliceCNoErr(t, err)
	return c
}

// sliceCReportedVideo is the device's report of stream id as the envelope
// asked for it.
func sliceCReportedVideo(id uint16, e *videoEnvelope, refs int) allocatedVideoStream {
	return allocatedVideoStream{
		videoStreamID: id, overlays: noOverlays, streamUsage: liveView, videoCodec: e.codec,
		minResolution: e.minResolution, maxResolution: e.maxResolution, minFrameRate: e.minFrameRate, maxFrameRate: e.maxFrameRate,
		minBitRate: e.minBitRate, maxBitRate: e.maxBitRate, keyFrameInterval: 4000, referenceCount: refs,
	}
}

// sliceCReportedAudio is the device's report of audio stream id as the
// envelope asked for it.
func sliceCReportedAudio(id uint16, e *audioEnvelope, refs int) allocatedAudioStream {
	return allocatedAudioStream{
		audioStreamID: id, streamUsage: liveView, audioCodec: e.codec, channelCount: e.channelCount,
		sampleRate: e.sampleRate, bitRate: e.bitRate, bitDepth: e.bitDepth, referenceCount: refs,
	}
}

// sliceCAllocating is the reuse describe's allocatingManager: video
// allocations answer ids in order, then ResourceExhausted.
func sliceCAllocating(state *cameraState, ids ...uint16) (*manager, *fakeIO, *clocktest.Fake) {
	var mu sync.Mutex
	remaining := slices.Clone(ids)
	return managerWith(state, func(r *recordedInvoke) (any, error) {
		if r.command != cmdVideoStreamAllocate {
			return nil, nil
		}
		mu.Lock()
		defer mu.Unlock()
		if len(remaining) == 0 {
			return nil, statusErr(im.StatusResourceExhausted)
		}
		next := remaining[0]
		remaining = remaining[1:]
		return csdef.VideoStreamAllocateResponse{VideoStreamId: next}, nil
	})
}

// sliceCAudioAllocating answers audio allocations with ids in order.
func sliceCAudioAllocating(state *cameraState, ids ...uint16) (*manager, *fakeIO, *clocktest.Fake) {
	var mu sync.Mutex
	remaining := slices.Clone(ids)
	return managerWith(state, func(r *recordedInvoke) (any, error) {
		if r.command != cmdAudioStreamAllocate {
			return nil, nil
		}
		mu.Lock()
		defer mu.Unlock()
		next := remaining[0]
		remaining = remaining[1:]
		return csdef.AudioStreamAllocateResponse{AudioStreamId: next}, nil
	})
}

// TestSliceCSnapshotWithAResolutionTargetParityMatterjsServer is
// describe("snapshot with a resolution target")
// (CameraStreamManagerTest.ts:5602-5757, matterjs-server at 0bd06f2).
func TestSliceCSnapshotWithAResolutionTargetParityMatterjsServer(t *testing.T) {
	t.Run("allocates the smallest capability that reaches the target and asks for the fitted size", func(t *testing.T) {
		m, io, _ := managerWith(fixtureState(), sliceCSnapshotCamera(3, sliceCFrame(320, 240)))
		r := sliceCSnapshot(t, m, snapshotArgs{target: &resolutionTarget{width: 320}})
		allocs := sliceCFieldsOf[csdef.SnapshotStreamAllocateRequest](io, cmdSnapshotStreamAllocate)
		sliceCEq(t, "allocate maxResolution", allocs[0].MaxResolution, sliceCRes(640, 480))
		sliceCEq(t, "requestedResolution", sliceCRequested(t, io), sliceCRes(320, 240))
		sliceCEq(t, "degraded", r.degraded, false)
	})

	t.Run("asks for the largest allowed capability when none reaches the target", func(t *testing.T) {
		m, io, _ := managerWith(fixtureState(), sliceCSnapshotCamera(3, sliceCFrame(640, 480)))
		r := sliceCSnapshot(t, m, snapshotArgs{maxResolution: resolution{1280, 720}, target: &resolutionTarget{width: 4000}})
		allocs := sliceCFieldsOf[csdef.SnapshotStreamAllocateRequest](io, cmdSnapshotStreamAllocate)
		sliceCEq(t, "allocate maxResolution", allocs[0].MaxResolution, sliceCRes(640, 480))
		sliceCEq(t, "requestedResolution", sliceCRequested(t, io), sliceCRes(640, 480))
		sliceCEq(t, "degraded", r.degraded, false)
	})

	t.Run("reports a frame below the requested size as degraded", func(t *testing.T) {
		m, _, _ := managerWith(fixtureState(), sliceCSnapshotCamera(3, sliceCFrame(160, 120)))
		r := sliceCSnapshot(t, m, snapshotArgs{target: &resolutionTarget{width: 320}})
		sliceCEq(t, "degraded", r.degraded, true)
	})

	t.Run("adopts a larger held stream for a small target and requests the fitted size from it", func(t *testing.T) {
		m, io, _ := managerWith(sliceCWithSnapshots(sliceCHeldStream(nil)), sliceCSnapshotCamera(3, sliceCFrame(320, 180)))
		r := sliceCSnapshot(t, m, snapshotArgs{target: &resolutionTarget{width: 320}})
		sliceCSlice(t, "commands", io.commands(), []string{cmdCaptureSnapshot})
		sliceCEq(t, "requestedResolution", sliceCRequested(t, io), sliceCRes(320, 180))
		sliceCEq(t, "provenance", r.provenance, ProvenanceAdopted)
		sliceCEq(t, "degraded", r.degraded, false)
	})

	t.Run("adopts the smallest held stream that reaches the target", func(t *testing.T) {
		held := sliceCWithSnapshots(sliceCHeldStream(nil), sliceCHeldStream(func(s *allocatedSnapshotStream) {
			s.snapshotStreamID, s.minResolution, s.maxResolution = 9, resolution{1280, 720}, resolution{1280, 720}
		}))
		m, io, _ := managerWith(held, func(*recordedInvoke) (any, error) { return sliceCFrame(320, 180), nil })
		sliceCSnapshot(t, m, snapshotArgs{target: &resolutionTarget{width: 1000}})
		sliceCEq(t, "captured stream", sliceCCapturedFrom(t, io), 9)
	})

	t.Run("fits the target into the stream the camera answered an allocate with", func(t *testing.T) {
		ranged := sliceCHeldStream(func(s *allocatedSnapshotStream) {
			s.snapshotStreamID, s.minResolution, s.maxResolution = 5, resolution{640, 480}, resolution{1920, 1080}
		})
		m, io, _ := managerWith(sliceCWithSnapshots(ranged), sliceCSnapshotCamera(5, sliceCFrame(1000, 563)))
		r := sliceCSnapshot(t, m, snapshotArgs{target: &resolutionTarget{width: 1000}})
		sliceCSlice(t, "commands", io.commands(), []string{cmdSnapshotStreamAllocate, cmdCaptureSnapshot})
		sliceCEq(t, "requestedResolution", sliceCRequested(t, io), sliceCRes(1000, 563))
		sliceCEq(t, "degraded", r.degraded, false)
	})

	t.Run("keeps the encoder-free capability first while a video stream holds the only encoder", func(t *testing.T) {
		m, io, _ := managerWith(sliceCStreaming(), sliceCSnapshotCamera(3, sliceCFrame(640, 480)))
		r := sliceCSnapshot(t, m, snapshotArgs{target: &resolutionTarget{width: 1000}})
		asked := sliceCAllocatedMax(io)
		sliceCSlice(t, "allocate maxResolutions", asked, []csdef.VideoResolutionStruct{sliceCRes(640, 480)})
		sliceCEq(t, "requestedResolution", sliceCRequested(t, io), sliceCRes(640, 480))
		sliceCEq(t, "degraded", r.degraded, true)
	})
}

// TestSliceCSnapshotParityMatterjsServer is describe("snapshot")
// (CameraStreamManagerTest.ts:5759-6577, matterjs-server at 0bd06f2).
func TestSliceCSnapshotParityMatterjsServer(t *testing.T) { //nolint:gocognit,gocyclo,cyclop,maintidx,funlen // one subtest per it, as the source has them
	full := sliceCFrame(1920, 1080)
	small := sliceCFrame(640, 480)

	t.Run("uses an encoder-free capability while a video stream is referenced", func(t *testing.T) {
		frame := small
		frame.Data = []byte{1, 2, 3}
		m, io, _ := managerWith(sliceCStreaming(), sliceCSnapshotCamera(3, frame))
		r := sliceCSnapshot(t, m, snapshotArgs{maxResolution: resolution{1920, 1080}})
		sliceCEq(t, "resolution", r.resolution, resolution{640, 480})
		sliceCEq(t, "degraded", r.degraded, true)
		sliceCEq(t, "provenance", r.provenance, ProvenanceAllocated)
		a := sliceCFieldsOf[csdef.SnapshotStreamAllocateRequest](io, cmdSnapshotStreamAllocate)[0]
		sliceCEq(t, "minResolution", a.MinResolution, sliceCRes(640, 480))
		sliceCEq(t, "maxResolution", a.MaxResolution, sliceCRes(640, 480))
		sliceCEq(t, "maxFrameRate", a.MaxFrameRate, 1)
		sliceCEq(t, "quality", a.Quality, 90)
	})

	t.Run("keeps the best capability while a viewer streams on a camera with encoders to spare", func(t *testing.T) {
		live := liveStream(1, 1)
		live.minResolution = resolution{1920, 1080}
		spare := sliceCWithVideo(live).with(func(s *cameraState) { s.maxConcurrentEncoders = stated(4) })
		m, io, _ := managerWith(spare, sliceCSnapshotCamera(3, full))
		r := sliceCSnapshot(t, m, snapshotArgs{})
		sliceCEq(t, "resolution", r.resolution, resolution{1920, 1080})
		sliceCEq(t, "degraded", r.degraded, false)
		a := sliceCFieldsOf[csdef.SnapshotStreamAllocateRequest](io, cmdSnapshotStreamAllocate)[0]
		sliceCEq(t, "minResolution", a.MinResolution, sliceCRes(1920, 1080))
		sliceCEq(t, "maxResolution", a.MaxResolution, sliceCRes(1920, 1080))
	})

	t.Run("counts the encoder a snapshot stream of its own holds before the camera reports it", func(t *testing.T) {
		var mu sync.Mutex
		allocated := uint16(0)
		m, io, _ := managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
			switch r.command {
			case cmdSnapshotStreamAllocate:
				mu.Lock()
				defer mu.Unlock()
				allocated++
				return csdef.SnapshotStreamAllocateResponse{SnapshotStreamId: allocated}, nil
			case cmdCaptureSnapshot:
				return small, nil
			}
			return nil, nil
		})
		sliceCSnapshot(t, m, snapshotArgs{})
		sliceCSnapshot(t, m, snapshotArgs{})
		asked := sliceCAllocatedMax(io)
		sliceCSlice(t, "allocate maxResolutions", asked, []csdef.VideoResolutionStruct{sliceCRes(1920, 1080), sliceCRes(640, 480)})
	})

	t.Run("keeps the best capability although the device still lists a snapshot stream", func(t *testing.T) {
		stale := sliceCWithSnapshots(sliceCHeldStream(func(s *allocatedSnapshotStream) { s.minResolution = resolution{640, 480} }))
		m, io, _ := managerWith(stale, sliceCSnapshotCamera(3, full))
		r := sliceCSnapshot(t, m, snapshotArgs{})
		sliceCEq(t, "resolution", r.resolution, resolution{1920, 1080})
		sliceCEq(t, "degraded", r.degraded, false)
		a := sliceCFieldsOf[csdef.SnapshotStreamAllocateRequest](io, cmdSnapshotStreamAllocate)[0]
		sliceCEq(t, "minResolution", a.MinResolution, sliceCRes(1920, 1080))
		sliceCEq(t, "maxResolution", a.MaxResolution, sliceCRes(1920, 1080))
	})

	t.Run("uses the highest capability when nothing holds the encoder", func(t *testing.T) {
		m, io, _ := managerWith(fixtureState(), sliceCSnapshotCamera(3, full))
		r := sliceCSnapshot(t, m, snapshotArgs{})
		sliceCEq(t, "resolution", r.resolution, resolution{1920, 1080})
		sliceCEq(t, "degraded", r.degraded, false)
		a := sliceCFieldsOf[csdef.SnapshotStreamAllocateRequest](io, cmdSnapshotStreamAllocate)[0]
		sliceCEq(t, "minResolution", a.MinResolution, sliceCRes(1920, 1080))
		sliceCEq(t, "maxResolution", a.MaxResolution, sliceCRes(1920, 1080))
	})

	t.Run("fails typed when the caller's resolution ceiling excludes every capability", func(t *testing.T) {
		m, io, _ := managerWith(fixtureState(), nil)
		e := sliceCAs[*IncompatibleError](t, sliceCSnapshotErr(t, m, snapshotArgs{maxResolution: resolution{100, 100}}))
		sliceCEq(t, "reason", e.Reason, ReasonBounds)
		sliceCEq(t, "invokes", len(io.recorded()), 0)
	})

	t.Run("says the camera has no snapshot capability rather than blaming the caller's bounds", func(t *testing.T) {
		m, io, _ := managerWith(fixtureState().with(func(s *cameraState) { s.snapshotCapabilities = nil }), nil)
		e := sliceCAs[*IncompatibleError](t, sliceCSnapshotErr(t, m, snapshotArgs{}))
		sliceCEq(t, "reason", e.Reason, ReasonCapability)
		sliceCEq(t, "invokes", len(io.recorded()), 0)
	})

	t.Run("names the image codecs it reports in a snapshot failure, once each", func(t *testing.T) {
		m, _, _ := managerWith(fixtureState(), nil)
		heic := csdef.ImageCodecHeic
		e := sliceCAs[*IncompatibleError](t, sliceCSnapshotErr(t, m, snapshotArgs{codec: &heic}))
		sliceCSlice(t, "device", e.Device, []string{"JPEG"})
		sliceCSlice(t, "requested", e.Requested, []string{"HEIC"})
	})

	t.Run("reports a codec failure when no capability uses the requested codec", func(t *testing.T) {
		m, _, _ := managerWith(fixtureState(), nil)
		codec := csdef.ImageCodecEnum(7)
		e := sliceCAs[*IncompatibleError](t, sliceCSnapshotErr(t, m, snapshotArgs{codec: &codec}))
		sliceCEq(t, "reason", e.Reason, ReasonCodec)
	})

	t.Run("reports a bounds failure when a ceiling excludes every capability of the requested codec", func(t *testing.T) {
		m, _, _ := managerWith(fixtureState(), nil)
		jpeg := csdef.ImageCodecJpeg
		e := sliceCAs[*IncompatibleError](t, sliceCSnapshotErr(t, m, snapshotArgs{codec: &jpeg, maxResolution: resolution{100, 100}}))
		sliceCEq(t, "reason", e.Reason, ReasonBounds)
	})

	t.Run("tries the next capability when the device matches none against the one it was offered", func(t *testing.T) {
		var mu sync.Mutex
		attempts := 0
		m, io, _ := managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
			switch r.command {
			case cmdSnapshotStreamAllocate:
				mu.Lock()
				defer mu.Unlock()
				attempts++
				if attempts == 1 {
					return nil, statusErr(im.StatusDynamicConstraintError)
				}
				return csdef.SnapshotStreamAllocateResponse{SnapshotStreamId: 3}, nil
			case cmdCaptureSnapshot:
				return small, nil
			}
			return nil, nil
		})
		sliceCSnapshot(t, m, snapshotArgs{})
		tried := sliceCAllocatedMin(io)
		sliceCSlice(t, "allocate minResolutions", tried, []csdef.VideoResolutionStruct{sliceCRes(1920, 1080), sliceCRes(640, 480)})
	})

	t.Run("fails typed without retrying when the device calls the snapshot request invalid", func(t *testing.T) {
		m, io, _ := managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
			if r.command != cmdSnapshotStreamAllocate {
				return nil, nil
			}
			return nil, statusErr(im.StatusConstraintError)
		})
		e := sliceCAs[*IncompatibleError](t, sliceCSnapshotErr(t, m, snapshotArgs{}))
		if e.DeviceStatus == nil || *e.DeviceStatus != im.StatusConstraintError {
			t.Errorf("device status = %v, want ConstraintError", e.DeviceStatus)
		}
		sliceCEq(t, "allocate attempts", sliceCCount(io, cmdSnapshotStreamAllocate), 1)
	})

	t.Run("fails typed rather than raw when no capability survives the device's capacity", func(t *testing.T) {
		m, _, _ := managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
			if r.command != cmdSnapshotStreamAllocate {
				return nil, nil
			}
			return nil, statusErr(im.StatusResourceExhausted)
		})
		sliceCAs[*ExhaustedError](t, sliceCSnapshotErr(t, m, snapshotArgs{}))
	})

	t.Run("fails typed when the device refuses the capture itself", func(t *testing.T) {
		m, _, _ := managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
			switch r.command {
			case cmdSnapshotStreamAllocate:
				return csdef.SnapshotStreamAllocateResponse{SnapshotStreamId: 3}, nil
			case cmdCaptureSnapshot:
				return nil, statusErr(im.StatusResourceExhausted)
			}
			return nil, nil
		})
		sliceCAs[*ExhaustedError](t, sliceCSnapshotErr(t, m, snapshotArgs{}))
	})

	t.Run("propagates a snapshot rejection the ladder does not know how to react to", func(t *testing.T) {
		const unsupported = im.StatusCode(0x81)
		m, _, _ := managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
			if r.command != cmdSnapshotStreamAllocate {
				return nil, nil
			}
			return nil, statusErr(unsupported)
		})
		err := sliceCSnapshotErr(t, m, snapshotArgs{})
		status, ok := deviceStatusOf(err)
		if !ok || status != unsupported {
			t.Errorf("error %v, want the device's status 0x81", err)
		}
	})

	t.Run("tries an encoder-using capability once the device refuses every encoder-free one", func(t *testing.T) {
		var mu sync.Mutex
		attempts := 0
		m, io, _ := managerWith(sliceCStreaming(), func(r *recordedInvoke) (any, error) {
			switch r.command {
			case cmdSnapshotStreamAllocate:
				mu.Lock()
				defer mu.Unlock()
				attempts++
				if attempts == 1 {
					return nil, statusErr(im.StatusDynamicConstraintError)
				}
				return csdef.SnapshotStreamAllocateResponse{SnapshotStreamId: 3}, nil
			case cmdCaptureSnapshot:
				return full, nil
			}
			return nil, nil
		})
		r := sliceCSnapshot(t, m, snapshotArgs{})
		tried := sliceCAllocatedMin(io)
		sliceCSlice(t, "allocate minResolutions", tried, []csdef.VideoResolutionStruct{sliceCRes(640, 480), sliceCRes(1920, 1080)})
		sliceCEq(t, "resolution", r.resolution, resolution{1920, 1080})
		sliceCEq(t, "stream", r.streamID, 3)
		sliceCEq(t, "degraded", r.degraded, false)
	})

	t.Run("keeps a capability that needs no hardware encoder while a video stream is live", func(t *testing.T) {
		softwareEncoded := sliceCStreaming().with(func(s *cameraState) {
			c := fixtureState().snapshotCapabilities
			hd := c[1]
			hd.requiresHardwareEncoder = false
			s.snapshotCapabilities = []snapshotCapability{hd, c[0]}
		})
		m, _, _ := managerWith(softwareEncoded, sliceCSnapshotCamera(3, full))
		r := sliceCSnapshot(t, m, snapshotArgs{})
		sliceCEq(t, "resolution", r.resolution, resolution{1920, 1080})
		sliceCEq(t, "degraded", r.degraded, false)
	})

	t.Run("reports a degradation when the device refuses the best capability and the next one is smaller", func(t *testing.T) {
		var mu sync.Mutex
		attempts := 0
		m, _, _ := managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
			switch r.command {
			case cmdSnapshotStreamAllocate:
				mu.Lock()
				defer mu.Unlock()
				attempts++
				if attempts == 1 {
					return nil, statusErr(im.StatusDynamicConstraintError)
				}
				return csdef.SnapshotStreamAllocateResponse{SnapshotStreamId: 3}, nil
			case cmdCaptureSnapshot:
				return small, nil
			}
			return nil, nil
		})
		r := sliceCSnapshot(t, m, snapshotArgs{})
		sliceCEq(t, "resolution", r.resolution, resolution{640, 480})
		sliceCEq(t, "degraded", r.degraded, true)
	})

	t.Run("gives the snapshot stream back when the capture fails", func(t *testing.T) {
		m, io, _ := managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
			switch r.command {
			case cmdSnapshotStreamAllocate:
				return csdef.SnapshotStreamAllocateResponse{SnapshotStreamId: 3}, nil
			case cmdCaptureSnapshot:
				return nil, statusErr(im.StatusResourceExhausted)
			}
			return nil, nil
		})
		sliceCAs[*ExhaustedError](t, sliceCSnapshotErr(t, m, snapshotArgs{}))
		sliceCSlice(t, "deallocated", sliceCSnapshotDeallocs(io), []uint16{3})
	})

	t.Run("names the stream it allocated at a capability that holds the hardware encoder", func(t *testing.T) {
		m, io, _ := managerWith(fixtureState(), sliceCSnapshotCamera(3, full))
		r := sliceCSnapshot(t, m, snapshotArgs{})
		sliceCEq(t, "stream", r.streamID, 3)
		sliceCEq(t, "deallocates", sliceCCount(io, cmdSnapshotStreamDeallocate), 0)
	})

	t.Run("adopts the encoder-holding stream it left behind on the next call", func(t *testing.T) {
		m, io, _ := managerWith(sliceCWithSnapshots(), sliceCSnapshotCamera(3, full))
		first := sliceCSnapshot(t, m, snapshotArgs{})
		io.setState(sliceCWithSnapshots(sliceCHeldStream(func(s *allocatedSnapshotStream) { s.snapshotStreamID = first.streamID })))
		second := sliceCSnapshot(t, m, snapshotArgs{})
		sliceCEq(t, "second stream", second.streamID, first.streamID)
		sliceCEq(t, "allocates", sliceCCount(io, cmdSnapshotStreamAllocate), 1)
		sliceCEq(t, "deallocates", sliceCCount(io, cmdSnapshotStreamDeallocate), 0)
	})

	t.Run("counts a snapshot stream the camera says holds the encoder before choosing a capability", func(t *testing.T) {
		encoderTaken := sliceCWithSnapshots(sliceCHeldStream(func(s *allocatedSnapshotStream) {
			s.minResolution, s.hardwareEncoder = resolution{640, 480}, true
		}))
		m, io, _ := managerWith(encoderTaken, func(r *recordedInvoke) (any, error) {
			switch r.command {
			case cmdSnapshotStreamAllocate:
				return nil, statusErr(im.StatusResourceExhausted)
			case cmdCaptureSnapshot:
				return small, nil
			}
			return nil, nil
		})
		r := sliceCSnapshot(t, m, snapshotArgs{})
		sliceCSlice(t, "commands", io.commands(), []string{cmdCaptureSnapshot})
		sliceCEq(t, "stream", r.streamID, 8)
		sliceCEq(t, "degraded", r.degraded, true)
	})

	t.Run("names an adopted stream although the camera's best capability needs the encoder", func(t *testing.T) {
		m, io, _ := managerWith(sliceCWithSnapshots(sliceCHeldStream(nil)), sliceCSnapshotCamera(3, full))
		r := sliceCSnapshot(t, m, snapshotArgs{})
		sliceCEq(t, "stream", r.streamID, 8)
		sliceCSlice(t, "commands", io.commands(), []string{cmdCaptureSnapshot})
	})

	softwareOnly := func() *cameraState {
		return fixtureState().with(func(s *cameraState) { s.snapshotCapabilities = s.snapshotCapabilities[:1] })
	}

	t.Run("gives a kept-capability stream back when the capture fails", func(t *testing.T) {
		m, io, _ := managerWith(softwareOnly(), func(r *recordedInvoke) (any, error) {
			switch r.command {
			case cmdSnapshotStreamAllocate:
				return csdef.SnapshotStreamAllocateResponse{SnapshotStreamId: 3}, nil
			case cmdCaptureSnapshot:
				return nil, errors.New("capture refused")
			}
			return nil, nil
		})
		sliceCSnapshotErr(t, m, snapshotArgs{})
		sliceCSlice(t, "deallocated", sliceCSnapshotDeallocs(io), []uint16{3})
	})

	t.Run("names the stream it kept, and releases that same id on request", func(t *testing.T) {
		m, io, _ := managerWith(softwareOnly(), sliceCSnapshotCamera(3, small))
		r := sliceCSnapshot(t, m, snapshotArgs{})
		sliceCEq(t, "stream", r.streamID, 3)
		sliceCSlice(t, "commands", io.commands(), []string{cmdSnapshotStreamAllocate, cmdCaptureSnapshot})
		sliceCNoErr(t, sliceCRelease(t, m, StreamSnapshot, r.streamID))
		sliceCSlice(t, "deallocated", sliceCSnapshotDeallocs(io), []uint16{3})
	})

	t.Run("captures from a snapshot stream the device already holds rather than allocating one", func(t *testing.T) {
		m, io, _ := managerWith(sliceCWithSnapshots(sliceCHeldStream(nil)), sliceCSnapshotCamera(3, full))
		r := sliceCSnapshot(t, m, snapshotArgs{})
		sliceCSlice(t, "commands", io.commands(), []string{cmdCaptureSnapshot})
		sliceCEq(t, "captured stream", sliceCCapturedFrom(t, io), 8)
		sliceCEq(t, "degraded", r.degraded, false)
		sliceCEq(t, "stream", r.streamID, 8)
		sliceCEq(t, "provenance", r.provenance, ProvenanceAdopted)
	})

	t.Run("reports a snapshot stream it allocated in an earlier call as reused", func(t *testing.T) {
		m, io, _ := managerWith(sliceCWithSnapshots(), sliceCSnapshotCamera(8, full))
		first := sliceCSnapshot(t, m, snapshotArgs{})
		sliceCEq(t, "first provenance", first.provenance, ProvenanceAllocated)
		io.setState(sliceCWithSnapshots(sliceCHeldStream(nil)))
		before := len(io.commands())
		second := sliceCSnapshot(t, m, snapshotArgs{})
		sliceCSlice(t, "commands", io.commands()[before:], []string{cmdCaptureSnapshot})
		sliceCEq(t, "stream", second.streamID, 8)
		sliceCEq(t, "provenance", second.provenance, ProvenanceReused)
	})

	t.Run("does not adopt a stream whose range reaches below the capability it would allocate", func(t *testing.T) {
		ranged := sliceCWithSnapshots(sliceCHeldStream(func(s *allocatedSnapshotStream) { s.minResolution = resolution{640, 480} }))
		m, io, _ := managerWith(ranged, sliceCSnapshotCamera(3, full))
		sliceCSnapshot(t, m, snapshotArgs{})
		sliceCEq(t, "allocates", sliceCCount(io, cmdSnapshotStreamAllocate) > 0, true)
		sliceCEq(t, "captured stream", sliceCCapturedFrom(t, io), 3)
	})

	t.Run("reports the degradation from the frame the device delivered, not from the stream it used", func(t *testing.T) {
		m, _, _ := managerWith(sliceCWithSnapshots(sliceCHeldStream(nil)), sliceCSnapshotCamera(3, small))
		r := sliceCSnapshot(t, m, snapshotArgs{})
		sliceCEq(t, "resolution", r.resolution, resolution{640, 480})
		sliceCEq(t, "degraded", r.degraded, true)
	})

	t.Run("allocates rather than adopting a stream smaller than the capability it would have used", func(t *testing.T) {
		smaller := sliceCWithSnapshots(sliceCHeldStream(func(s *allocatedSnapshotStream) {
			s.minResolution, s.maxResolution = resolution{640, 480}, resolution{640, 480}
		}))
		m, io, _ := managerWith(smaller, sliceCSnapshotCamera(3, full))
		sliceCSnapshot(t, m, snapshotArgs{})
		sliceCSlice(t, "commands", io.commands(), []string{cmdSnapshotStreamAllocate, cmdCaptureSnapshot})
	})

	t.Run("refuses to adopt a stream above the resolution ceiling the caller stated", func(t *testing.T) {
		m, io, _ := managerWith(sliceCWithSnapshots(sliceCHeldStream(nil)), sliceCSnapshotCamera(3, small))
		sliceCSnapshot(t, m, snapshotArgs{maxResolution: resolution{640, 480}})
		sliceCEq(t, "allocates", sliceCCount(io, cmdSnapshotStreamAllocate) > 0, true)
		sliceCEq(t, "captured stream", sliceCCapturedFrom(t, io), 3)
	})

	t.Run("allocates when the device answers NotFound for the stream its reported state still lists", func(t *testing.T) {
		m, io, _ := managerWith(sliceCWithSnapshots(sliceCHeldStream(nil)), func(r *recordedInvoke) (any, error) {
			switch r.command {
			case cmdSnapshotStreamAllocate:
				return csdef.SnapshotStreamAllocateResponse{SnapshotStreamId: 3}, nil
			case cmdCaptureSnapshot:
				if r.fields.(csdef.CaptureSnapshotRequest).SnapshotStreamId.Value == 8 {
					return nil, statusErr(im.StatusNotFound)
				}
				return full, nil
			}
			return nil, nil
		})
		r := sliceCSnapshot(t, m, snapshotArgs{})
		sliceCSlice(t, "commands", io.commands(), []string{cmdCaptureSnapshot, cmdSnapshotStreamAllocate, cmdCaptureSnapshot})
		sliceCSlice(t, "data", r.data, []byte{1})
	})

	t.Run("leaves a snapshot stream in place when its capability needs no hardware encoder", func(t *testing.T) {
		m, io, _ := managerWith(softwareOnly(), sliceCSnapshotCamera(3, small))
		sliceCSnapshot(t, m, snapshotArgs{})
		sliceCSlice(t, "commands", io.commands(), []string{cmdSnapshotStreamAllocate, cmdCaptureSnapshot})
	})

	t.Run("keeps the snapshot stream releasable by the id the answer named", func(t *testing.T) {
		m, _, _ := managerWith(sliceCWithSnapshots(), sliceCSnapshotCamera(3, full))
		r := sliceCSnapshot(t, m, snapshotArgs{})
		sliceCEq(t, "leased endpoints", m.leasedEndpoints(), 1)
		sliceCNoErr(t, sliceCRelease(t, m, StreamSnapshot, r.streamID))
		sliceCEq(t, "leased endpoints after release", m.leasedEndpoints(), 0)
	})

	t.Run("gives the snapshot stream back when the capture response is unusable", func(t *testing.T) {
		m, io, _ := managerWith(fixtureState(), sliceCSnapshotCamera(3, csdef.CaptureSnapshotResponse{ImageCodec: csdef.ImageCodecJpeg}))
		sliceCAs[*StackError](t, sliceCSnapshotErr(t, m, snapshotArgs{}))
		sliceCSlice(t, "deallocated", sliceCSnapshotDeallocs(io), []uint16{3})
	})
}

// TestSliceCCameraNotSupportedParityMatterjsServer is
// describe("camera_not_supported") (CameraStreamManagerTest.ts:6579-6617,
// matterjs-server at 0bd06f2).
func TestSliceCCameraNotSupportedParityMatterjsServer(t *testing.T) {
	t.Run("reports a missing WebRTC provider cluster rather than allocating a stream first", func(t *testing.T) {
		m, io, _ := managerWith(fixtureState(), allocateVideo(9))
		io.missing, io.hasMiss = []uint32{wpdef.ClusterID}, true
		args := startDefaults(videoOffer)
		args.videoState, args.video, args.audioState = trackDemanded, &videoHints{}, trackDeclined
		_, err := m.startStream(t.Context(), args)
		e := sliceCAs[*NotSupportedError](t, err)
		sliceCSlice(t, "missing", e.Missing, []uint32{wpdef.ClusterID})
		sliceCEq(t, "invokes", len(io.recorded()), 0)
	})

	t.Run("reports both clusters when the endpoint exposes neither", func(t *testing.T) {
		m, _, _ := managerWith(nil, nil)
		_, err := m.capabilities(t.Context(), tTarget)
		e := sliceCAs[*NotSupportedError](t, err)
		sliceCSlice(t, "missing", e.Missing, []uint32{csdef.ClusterID, wpdef.ClusterID})
	})
}

// TestSliceCReleaseStreamParityMatterjsServer is describe("releaseStream")
// (CameraStreamManagerTest.ts:6619-6783, matterjs-server at 0bd06f2). The
// error-104 message text is the host's wire; the fields are asserted.
func TestSliceCReleaseStreamParityMatterjsServer(t *testing.T) {
	refuse := func(s im.StatusCode) func(*recordedInvoke) (any, error) {
		return func(*recordedInvoke) (any, error) { return nil, statusErr(s) }
	}

	t.Run("refuses to release a stream the device still references, naming the count it read", func(t *testing.T) {
		m, io, _ := managerWith(sliceCWithVideo(liveStream(1, 1)), refuse(im.StatusInvalidInState))
		err := sliceCRelease(t, m, StreamVideo, 1)
		if !errors.Is(err, ErrStreamInUse) {
			t.Fatalf("error %v, want ErrStreamInUse", err)
		}
		e := sliceCAs[*InUseError](t, err)
		sliceCEq(t, "stream", e.StreamID, 1)
		sliceCEq(t, "reference count", e.ReferenceCount, 1)
		sliceCSlice(t, "commands", io.commands(), []string{cmdVideoStreamDeallocate})
	})

	t.Run("releases a stream the server reads as referenced when the camera accepts it", func(t *testing.T) {
		m, io, _ := managerWith(sliceCWithVideo(liveStream(1, 2)), nil)
		sliceCNoErr(t, sliceCRelease(t, m, StreamVideo, 1))
		sliceCSlice(t, "commands", io.commands(), []string{cmdVideoStreamDeallocate})
	})

	t.Run("releases a stream the server did not allocate, because the camera allows it", func(t *testing.T) {
		m, io, _ := managerWith(sliceCWithVideo(liveStream(1, 0)), nil)
		sliceCNoErr(t, sliceCRelease(t, m, StreamVideo, 1))
		sliceCSlice(t, "commands", io.commands(), []string{cmdVideoStreamDeallocate})
		sliceCEq(t, "fields", io.recorded()[0].fields.(csdef.VideoStreamDeallocateRequest), csdef.VideoStreamDeallocateRequest{VideoStreamId: 1})
	})

	t.Run("answers the camera's own INVALID_IN_STATE with the in-use error, not the generic one", func(t *testing.T) {
		m, _, _ := managerWith(sliceCWithVideo(liveStream(1, 0)), refuse(im.StatusInvalidInState))
		e := sliceCAs[*InUseError](t, sliceCRelease(t, m, StreamVideo, 1))
		sliceCEq(t, "stream", e.StreamID, 1)
		sliceCEq(t, "reference count (none stated)", e.ReferenceCount, 0)
		status, ok := deviceStatusOf(e.Unwrap())
		if !ok || status != im.StatusInvalidInState {
			t.Errorf("cause %v, want the device's INVALID_IN_STATE", e.Unwrap())
		}
	})

	t.Run("forwards the camera's own refusal rather than deciding for it", func(t *testing.T) {
		foreign := liveStream(1, 0)
		foreign.streamUsage = csdef.StreamUsageInternal
		m, _, _ := managerWith(sliceCWithVideo(foreign), refuse(im.StatusDynamicConstraintError))
		err := sliceCRelease(t, m, StreamVideo, 1)
		status, ok := deviceStatusOf(err)
		if !ok || status != im.StatusDynamicConstraintError {
			t.Errorf("error %v, want the device's DYNAMIC_CONSTRAINT_ERROR", err)
		}
	})
}

// TestSliceCLeasesParityMatterjsServer is describe("leases")
// (CameraStreamManagerTest.ts:6785-7334, matterjs-server at 0bd06f2).
func TestSliceCLeasesParityMatterjsServer(t *testing.T) { //nolint:gocognit,gocyclo,cyclop,maintidx,funlen // one subtest per it, as the source has them
	reusable := sliceCContained(sliceCForeign())
	exhaustedLiveView := func(id uint16) func(*recordedInvoke) (any, error) {
		return func(r *recordedInvoke) (any, error) {
			if r.command != cmdVideoStreamAllocate {
				return nil, nil
			}
			if r.fields.(csdef.VideoStreamAllocateRequest).StreamUsage == liveView {
				return nil, statusErr(im.StatusResourceExhausted)
			}
			return csdef.VideoStreamAllocateResponse{VideoStreamId: id}, nil
		}
	}

	t.Run("holds no entry for an endpoint whose last lease is gone", func(t *testing.T) {
		m, _, _ := managerWith(sliceCWithVideo(), allocateVideo(9))
		sliceCLiveView(t, m)
		sliceCEq(t, "leased endpoints", m.leasedEndpoints(), 1)
		sliceCNoErr(t, sliceCRelease(t, m, StreamVideo, 9))
		sliceCEq(t, "leased endpoints after release", m.leasedEndpoints(), 0)
	})

	t.Run("leases a foreign stream it hands out, without claiming to own it", func(t *testing.T) {
		m, io, _ := managerWith(sliceCWithVideo(reusable), nil)
		r := sliceCLiveView(t, m)
		sliceCEq(t, "provenance", r.provenance, ProvenanceAdopted)
		sliceCEq(t, "leased endpoints", m.leasedEndpoints(), 1)
		sliceCEq(t, "invokes", len(io.recorded()), 0)
	})

	t.Run("releases a foreign stream it leased for reuse, without ever having owned it", func(t *testing.T) {
		m, io, _ := managerWith(sliceCWithVideo(reusable), nil)
		r := sliceCLiveView(t, m)
		sliceCEq(t, "provenance", r.provenance, ProvenanceAdopted)
		sliceCNoErr(t, sliceCRelease(t, m, StreamVideo, 7))
		sliceCEq(t, "deallocates", sliceCCount(io, cmdVideoStreamDeallocate) > 0, true)
	})

	t.Run("leases a foreign audio stream it hands out", func(t *testing.T) {
		foreignAudio := allocatedAudioStream{audioStreamID: 4, streamUsage: liveView, audioCodec: csdef.AudioCodecOpus, channelCount: 1, sampleRate: 48000, bitRate: 64000, bitDepth: 16, referenceCount: 1}
		m, _, _ := managerWith(fixtureState().with(func(s *cameraState) { s.audio = []allocatedAudioStream{foreignAudio} }), nil)
		r := sliceCAudio(t, m)
		sliceCEq(t, "provenance", r.provenance, ProvenanceAdopted)
		sliceCEq(t, "leased endpoints", m.leasedEndpoints(), 1)
	})

	t.Run("leases nothing when the only stream left fails the caller's bounds", func(t *testing.T) {
		otherCodec := reusable
		otherCodec.videoCodec, otherCodec.referenceCount = csdef.VideoCodecVvc, 1
		m, _, _ := managerWith(sliceCWithVideo(otherCodec), func(*recordedInvoke) (any, error) { return nil, statusErr(im.StatusResourceExhausted) })
		_, err := sliceCResolve(t, m, liveView, nil)
		sliceCAs[*ExhaustedError](t, err)
		sliceCEq(t, "leased endpoints", m.leasedEndpoints(), 0)
	})

	t.Run("leases a foreign stream the degraded rung hands out", func(t *testing.T) {
		busy := reusable
		busy.referenceCount, busy.maxResolution = 1, resolution{3840, 2160}
		m, _, _ := managerWith(sliceCWithVideo(busy), func(*recordedInvoke) (any, error) { return nil, statusErr(im.StatusResourceExhausted) })
		r := sliceCLiveView(t, m)
		sliceCEq(t, "degraded", r.degraded, true)
		sliceCEq(t, "provenance", r.provenance, ProvenanceAdopted)
		sliceCEq(t, "leased endpoints", m.leasedEndpoints(), 1)
	})

	t.Run("treats a stream id reissued to a fresh allocation as its own", func(t *testing.T) {
		m, io, _ := managerWith(sliceCWithVideo(reusable), allocateVideo(7))
		sliceCLiveView(t, m)
		io.setState(sliceCWithVideo())
		fresh := sliceCLiveView(t, m)
		sliceCEq(t, "stream", fresh.streamID, 7)
		if fresh.provenance == ProvenanceAdopted {
			t.Errorf("provenance = %v, want not adopted", fresh.provenance)
		}
		sliceCNoErr(t, sliceCRelease(t, m, StreamVideo, 7))
		sliceCEq(t, "leased endpoints", m.leasedEndpoints(), 0)
	})

	t.Run("still calls a stream its own when a later request reuses it rather than allocating", func(t *testing.T) {
		m, io, _ := managerWith(sliceCWithVideo(), allocateVideo(9))
		allocated := sliceCLiveView(t, m)
		sliceCEq(t, "first provenance", allocated.provenance, ProvenanceAllocated)
		own := reusable
		own.videoStreamID = 9
		io.setState(sliceCWithVideo(own))
		reused := sliceCLiveView(t, m)
		sliceCEq(t, "stream", reused.streamID, 9)
		sliceCEq(t, "provenance", reused.provenance, ProvenanceReused)
		sliceCSlice(t, "commands", io.commands(), []string{cmdVideoStreamAllocate})
	})

	t.Run("does not stand in for a device report of a stream it only reused", func(t *testing.T) {
		m, io, _ := managerWith(sliceCWithVideo(reusable), allocateVideo(11))
		first := sliceCLiveView(t, m)
		sliceCEq(t, "first stream", first.streamID, 7)
		io.setState(sliceCWithVideo())
		second := sliceCLiveView(t, m)
		sliceCEq(t, "second stream", second.streamID, 11)
		sliceCEq(t, "provenance", second.provenance, ProvenanceAllocated)
	})

	t.Run("drops the lease for a foreign stream once the device stops naming it", func(t *testing.T) {
		m, io, _ := managerWith(sliceCWithVideo(reusable), nil)
		sliceCLiveView(t, m)
		sliceCEq(t, "leased endpoints", m.leasedEndpoints(), 1)
		io.setState(sliceCWithVideo())
		sliceCCapabilities(t, m)
		sliceCEq(t, "leased endpoints after the report", m.leasedEndpoints(), 0)
	})

	t.Run("drops a video lease once the device has named the stream and then stops", func(t *testing.T) {
		m, io, _ := managerWith(sliceCWithVideo(), allocateVideo(9))
		allocated := sliceCLiveView(t, m)
		io.setState(sliceCWithVideo(sliceCReportedVideo(9, allocated.video, 0)))
		sliceCCapabilities(t, m)
		sliceCEq(t, "leased endpoints while named", m.leasedEndpoints(), 1)
		io.setState(sliceCWithVideo())
		sliceCCapabilities(t, m)
		sliceCEq(t, "leased endpoints after", m.leasedEndpoints(), 0)
	})

	t.Run("drops an audio lease once the device has named the stream and then stops", func(t *testing.T) {
		m, io, _ := sliceCAudioAllocating(fixtureState(), 4)
		allocated := sliceCAudio(t, m)
		io.setState(fixtureState().with(func(s *cameraState) {
			s.audio = []allocatedAudioStream{sliceCReportedAudio(4, allocated.audio, 0)}
		}))
		sliceCCapabilities(t, m)
		sliceCEq(t, "leased endpoints while named", m.leasedEndpoints(), 1)
		io.setState(fixtureState())
		sliceCCapabilities(t, m)
		sliceCEq(t, "leased endpoints after", m.leasedEndpoints(), 0)
	})

	t.Run("leases the snapshot stream it allocated at a capability that holds the hardware encoder", func(t *testing.T) {
		m, _, _ := managerWith(sliceCWithSnapshots(), sliceCSnapshotCamera(3, sliceCFrame(640, 480)))
		sliceCSnapshot(t, m, snapshotArgs{})
		sliceCEq(t, "leased endpoints", m.leasedEndpoints(), 1)
	})

	t.Run("holds no entry for an endpoint that only ever gave up a foreign stream", func(t *testing.T) {
		m, io, _ := managerWith(sliceCWithVideo(sliceCForeign()), func(r *recordedInvoke) (any, error) {
			if r.command == cmdVideoStreamAllocate {
				return nil, statusErr(im.StatusResourceExhausted)
			}
			return nil, nil
		})
		_, err := sliceCResolve(t, m, liveView, nil)
		sliceCAs[*ExhaustedError](t, err)
		sliceCEq(t, "deallocates", sliceCCount(io, cmdVideoStreamDeallocate) > 0, true)
		sliceCEq(t, "leased endpoints", m.leasedEndpoints(), 0)
	})

	t.Run("puts back a stream it freed to make room when the request succeeds without the capacity", func(t *testing.T) {
		inUse := reusable
		inUse.videoStreamID, inUse.referenceCount, inUse.maxResolution = 11, 1, resolution{3840, 2160}
		m, io, _ := managerWith(sliceCWithVideo(sliceCForeign(), inUse), exhaustedLiveView(20))
		r := sliceCLiveView(t, m)
		sliceCEq(t, "stream", r.streamID, 11)
		sliceCEq(t, "degraded", r.degraded, true)
		sliceCSlice(t, "deallocated", sliceCVideoDeallocs(io), []uint16{7})
		restore := sliceCRestore(t, io)
		sliceCEq(t, "restore maxResolution", restore.MaxResolution, sliceCRes(320, 240))
		sliceCEq(t, "restore keyFrameInterval", restore.KeyFrameInterval, 2000)
	})

	t.Run("puts back a stream it freed to make room when the request fails after the allocate", func(t *testing.T) {
		var mu sync.Mutex
		freed := 0
		m, io, _ := managerWith(sliceCWithVideo(sliceCForeign()), func(r *recordedInvoke) (any, error) {
			mu.Lock()
			defer mu.Unlock()
			switch r.command {
			case cmdVideoStreamDeallocate:
				freed++
			case cmdVideoStreamAllocate:
				if r.fields.(csdef.VideoStreamAllocateRequest).StreamUsage != liveView {
					return csdef.VideoStreamAllocateResponse{VideoStreamId: 21}, nil
				}
				if freed == 0 {
					return nil, statusErr(im.StatusResourceExhausted)
				}
				return csdef.VideoStreamAllocateResponse{VideoStreamId: 20}, nil
			case cmdAudioStreamAllocate:
				return nil, statusErr(im.StatusConstraintError)
			}
			return nil, nil
		})
		args := startDefaults(videoAndAudioOffer)
		args.videoState, args.video = trackDemanded, &videoHints{}
		args.audioState, args.audio = trackDemanded, &audioHints{bitRate: 32000}
		_, err := m.startStream(t.Context(), args)
		sliceCAs[*IncompatibleError](t, err)
		sliceCSlice(t, "deallocated", sliceCVideoDeallocs(io), []uint16{7, 20})
		restore := sliceCRestore(t, io)
		sliceCEq(t, "restore maxResolution", restore.MaxResolution, sliceCRes(320, 240))
		sliceCEq(t, "restore keyFrameInterval", restore.KeyFrameInterval, 2000)
	})

	t.Run("puts back a stream it freed to make room when the request fails anyway", func(t *testing.T) {
		m, io, _ := managerWith(sliceCWithVideo(sliceCForeign()), exhaustedLiveView(20))
		_, err := sliceCResolve(t, m, liveView, nil)
		sliceCAs[*ExhaustedError](t, err)
		restore := sliceCRestore(t, io)
		sliceCEq(t, "restore maxResolution", restore.MaxResolution, sliceCRes(320, 240))
		sliceCEq(t, "restore keyFrameInterval", restore.KeyFrameInterval, 2000)
		sliceCEq(t, "restore videoCodec", restore.VideoCodec, csdef.VideoCodecH264)
		sliceCNoErr(t, sliceCRelease(t, m, StreamVideo, 20))
		sliceCEq(t, "leased endpoints", m.leasedEndpoints(), 0)
	})

	t.Run("puts back a stream it freed to make room when the camera answers with a stream it already held", func(t *testing.T) {
		held := reusable
		held.videoStreamID, held.referenceCount, held.minResolution = 11, 1, resolution{1280, 720}
		var mu sync.Mutex
		freed := 0
		var io *fakeIO
		m, io, _ := managerWith(sliceCWithVideo(sliceCForeign(), held), func(r *recordedInvoke) (any, error) {
			mu.Lock()
			defer mu.Unlock()
			if r.command == cmdVideoStreamDeallocate {
				freed++
			}
			if r.command != cmdVideoStreamAllocate {
				return nil, nil
			}
			if r.fields.(csdef.VideoStreamAllocateRequest).StreamUsage != liveView {
				return csdef.VideoStreamAllocateResponse{VideoStreamId: 20}, nil
			}
			if freed == 0 {
				return nil, statusErr(im.StatusResourceExhausted)
			}
			narrowed := held
			narrowed.minResolution = resolution{1920, 1080}
			io.setState(sliceCWithVideo(narrowed))
			return csdef.VideoStreamAllocateResponse{VideoStreamId: 11}, nil
		})
		hints := &videoHints{videoRangeBounds: videoRangeBounds{minResolution: resolution{1920, 1080}, maxResolution: resolution{1920, 1080}}}
		r, err := sliceCResolve(t, m, liveView, hints)
		sliceCNoErr(t, err)
		sliceCEq(t, "stream", r.streamID, 11)
		sliceCEq(t, "provenance", r.provenance, ProvenanceAdopted)
		sliceCSlice(t, "evicted", r.evicted, []uint16{7})
		sliceCEq(t, "restore maxResolution", sliceCRestore(t, io).MaxResolution, sliceCRes(320, 240))
	})

	t.Run("leases no replacement when the camera answers the restore with a stream it already held", func(t *testing.T) {
		held := sliceCForeign()
		held.videoStreamID, held.videoCodec, held.referenceCount = 11, csdef.VideoCodecVvc, 1
		m, _, _ := managerWith(sliceCWithVideo(sliceCForeign(), held), exhaustedLiveView(11))
		_, err := sliceCResolve(t, m, liveView, nil)
		if err == nil || !isTyped(err) {
			t.Fatalf("error %v, want a typed error", err)
		}
		sliceCEq(t, "leased endpoints", m.leasedEndpoints(), 0)
	})
}

// TestSliceCReuseBeforeTheDeviceHasReportedParityMatterjsServer is
// describe("CameraStreamManager reuse before the device has reported")
// (CameraStreamManagerTest.ts:7337-7815, matterjs-server at 0bd06f2).
func TestSliceCReuseBeforeTheDeviceHasReportedParityMatterjsServer(t *testing.T) { //nolint:gocognit,gocyclo,cyclop,maintidx,funlen // one subtest per it, as the source has them
	empty := sliceCWithVideo
	overlayState := func() *cameraState {
		return sliceCWithVideo().with(func(s *cameraState) {
			s.features = csdef.FeatureVideo | csdef.FeatureSnapshot | csdef.FeatureWatermark | csdef.FeatureOnScreenDisplay
		})
	}
	watermark := func(on bool) *videoHints { return &videoHints{overlays: overlayBounds{watermark: flagOf(on)}} }
	leasedProbe := func(deallocate im.StatusCode) *manager {
		m, _, _ := managerWith(empty(), func(r *recordedInvoke) (any, error) {
			switch r.command {
			case cmdVideoStreamAllocate:
				return csdef.VideoStreamAllocateResponse{VideoStreamId: 9}, nil
			case cmdVideoStreamDeallocate:
				return nil, statusErr(deallocate)
			}
			return nil, nil
		})
		return m
	}

	t.Run("reuses the stream it has just allocated instead of allocating a twin", func(t *testing.T) {
		m, io, _ := sliceCAllocating(empty(), 9, 10)
		first := sliceCLiveView(t, m)
		second := sliceCLiveView(t, m)
		sliceCEq(t, "stream", second.streamID, 9)
		if second.provenance == ProvenanceAllocated {
			t.Error("second provenance = allocated")
		}
		sliceCEq(t, "allocates", sliceCCount(io, cmdVideoStreamAllocate), 1)
		if first.video == nil || second.video == nil || *first.video != *second.video {
			t.Errorf("second envelope %+v, want the first's %+v", second.video, first.video)
		}
	})

	t.Run("refuses an unreported stream whose overlays the caller did not ask for", func(t *testing.T) {
		m, io, _ := sliceCAllocating(overlayState(), 9, 10)
		first, err := sliceCResolve(t, m, liveView, watermark(true))
		sliceCNoErr(t, err)
		sliceCEq(t, "first stream", first.streamID, 9)
		second, err := sliceCResolve(t, m, liveView, watermark(false))
		sliceCNoErr(t, err)
		sliceCEq(t, "second stream", second.streamID, 10)
		sliceCEq(t, "provenance", second.provenance, ProvenanceAllocated)
		sliceCEq(t, "allocates", sliceCCount(io, cmdVideoStreamAllocate), 2)
	})

	t.Run("reuses an unreported stream whose overlays are the ones asked for again", func(t *testing.T) {
		m, io, _ := sliceCAllocating(overlayState(), 9, 10)
		_, err := sliceCResolve(t, m, liveView, watermark(true))
		sliceCNoErr(t, err)
		second, err := sliceCResolve(t, m, liveView, watermark(true))
		sliceCNoErr(t, err)
		sliceCEq(t, "stream", second.streamID, 9)
		if second.provenance == ProvenanceAllocated {
			t.Error("second provenance = allocated")
		}
		sliceCEq(t, "allocates", sliceCCount(io, cmdVideoStreamAllocate), 1)
	})

	t.Run("reuses an audio stream it has just allocated instead of allocating a twin", func(t *testing.T) {
		m, io, _ := managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
			if r.command == cmdAudioStreamAllocate {
				return csdef.AudioStreamAllocateResponse{AudioStreamId: 4}, nil
			}
			return nil, nil
		})
		first := sliceCAudio(t, m)
		second := sliceCAudio(t, m)
		sliceCEq(t, "stream", second.streamID, 4)
		if second.provenance == ProvenanceAllocated {
			t.Error("second provenance = allocated")
		}
		sliceCEq(t, "allocates", sliceCCount(io, cmdAudioStreamAllocate), 1)
		if first.audio == nil || second.audio == nil || *first.audio != *second.audio {
			t.Errorf("second envelope %+v, want the first's %+v", second.audio, first.audio)
		}
	})

	t.Run("reuses an unreported stream allocated for another usage", func(t *testing.T) {
		m, io, _ := sliceCAllocating(empty(), 9)
		sliceCLiveView(t, m)
		second, err := sliceCResolve(t, m, csdef.StreamUsageRecording, nil)
		sliceCNoErr(t, err)
		sliceCEq(t, "stream", second.streamID, 9)
		if second.provenance == ProvenanceAllocated {
			t.Error("second provenance = allocated")
		}
		sliceCEq(t, "allocates", sliceCCount(io, cmdVideoStreamAllocate), 1)
	})

	t.Run("hands out an unreported stream on the degraded rung when nothing else is left", func(t *testing.T) {
		m, io, _ := sliceCAllocating(empty(), 9)
		sliceCLiveView(t, m)
		io.setState(empty().with(func(s *cameraState) { s.maxNetworkBandwidth = stated(2000000) }))
		second := sliceCLiveView(t, m)
		sliceCEq(t, "stream", second.streamID, 9)
		sliceCEq(t, "degraded", second.degraded, true)
	})

	t.Run("refuses to degrade to a stream the offer says the peer cannot decode", func(t *testing.T) {
		m, _, _ := sliceCAllocating(empty(), 9)
		sliceCLiveView(t, m)
		sdp := &sdpVideoConstraints{
			video:                   mediaDisposition{state: mediaReceiving},
			audio:                   mediaDisposition{state: mediaAbsent},
			limitsByCodec:           map[string]videoCodecLimits{VideoCodecName(h265): {maxPixels: stated(1280 * 720)}},
			unreadableCeilingCodecs: map[string]bool{},
		}
		_, err := m.resolveVideoStream(t.Context(), videoRequest{t: tTarget, usage: liveView, limits: videoCodecLimitsOf(sdp, h265), allowEviction: true})
		sliceCAs[*ExhaustedError](t, err)
	})

	t.Run("lets the device's own report replace its record of a stream it allocated", func(t *testing.T) {
		m, io, _ := sliceCAllocating(empty(), 9)
		first := sliceCLiveView(t, m)
		wider := sliceCReportedVideo(9, first.video, 1)
		wider.maxResolution = resolution{3840, 2160}
		io.setState(sliceCWithVideo(wider))
		second := sliceCLiveView(t, m)
		sliceCEq(t, "stream", second.streamID, 9)
		sliceCEq(t, "degraded", second.degraded, true)
		sliceCEq(t, "envelope maxResolution", second.video.maxResolution, resolution{3840, 2160})
	})

	t.Run("lets the device's own report replace its record of an audio stream it allocated", func(t *testing.T) {
		m, io, _ := sliceCAudioAllocating(fixtureState(), 4, 5)
		first := sliceCAudio(t, m)
		settled := sliceCReportedAudio(4, first.audio, 1)
		settled.sampleRate = first.audio.sampleRate / 2
		io.setState(fixtureState().with(func(s *cameraState) { s.audio = []allocatedAudioStream{settled} }))
		second := sliceCAudio(t, m)
		sliceCEq(t, "stream", second.streamID, 5)
		sliceCEq(t, "provenance", second.provenance, ProvenanceAllocated)
	})

	t.Run("keeps reusing an unreported stream up to the last millisecond of the grace window", func(t *testing.T) {
		m, io, clk := sliceCAllocating(empty(), 9, 10)
		sliceCLiveView(t, m)
		clk.Advance(time.Millisecond)
		sliceCLiveView(t, m)
		clk.Advance(unreportedLeaseGrace - 2*time.Millisecond)
		third := sliceCLiveView(t, m)
		sliceCEq(t, "stream", third.streamID, 9)
		sliceCEq(t, "allocates", sliceCCount(io, cmdVideoStreamAllocate), 1)
	})

	t.Run("allocates again once the grace window passes and the device still names no stream", func(t *testing.T) {
		m, io, clk := sliceCAllocating(empty(), 9, 10)
		sliceCLiveView(t, m)
		clk.Advance(unreportedLeaseGrace)
		second := sliceCLiveView(t, m)
		sliceCEq(t, "stream", second.streamID, 10)
		sliceCEq(t, "provenance", second.provenance, ProvenanceAllocated)
		sliceCEq(t, "allocates", sliceCCount(io, cmdVideoStreamAllocate), 2)
	})

	t.Run("keeps a stream it allocated releasable although the device never names it", func(t *testing.T) {
		m, io, clk := managerWith(empty(), allocateVideo(9))
		sliceCLiveView(t, m)
		clk.Advance(unreportedLeaseGrace * 6)
		sliceCCapabilities(t, m)
		sliceCEq(t, "leased endpoints", m.leasedEndpoints(), 1)
		sliceCNoErr(t, sliceCRelease(t, m, StreamVideo, 9))
		sliceCEq(t, "deallocates", sliceCCount(io, cmdVideoStreamDeallocate) > 0, true)
		sliceCEq(t, "leased endpoints after release", m.leasedEndpoints(), 0)
	})

	t.Run("keeps the lease when the camera refuses the release as in use", func(t *testing.T) {
		m := leasedProbe(im.StatusInvalidInState)
		sliceCLiveView(t, m)
		sliceCEq(t, "leased endpoints", m.leasedEndpoints(), 1)
		if err := sliceCRelease(t, m, StreamVideo, 9); !errors.Is(err, ErrStreamInUse) {
			t.Fatalf("error %v, want ErrStreamInUse", err)
		}
		sliceCEq(t, "leased endpoints after the refusal", m.leasedEndpoints(), 1)
	})

	t.Run("drops the lease when the camera states it has no such stream", func(t *testing.T) {
		m := leasedProbe(im.StatusNotFound)
		sliceCLiveView(t, m)
		sliceCEq(t, "leased endpoints", m.leasedEndpoints(), 1)
		err := sliceCRelease(t, m, StreamVideo, 9)
		if !statusIs(err, im.StatusNotFound) {
			t.Fatalf("error %v, want the device's NOT_FOUND", err)
		}
		sliceCEq(t, "leased endpoints after", m.leasedEndpoints(), 0)
	})

	t.Run("allocates a second audio stream once the grace window passes", func(t *testing.T) {
		m, _, clk := sliceCAudioAllocating(fixtureState(), 4, 5)
		sliceCAudio(t, m)
		clk.Advance(unreportedLeaseGrace)
		second := sliceCAudio(t, m)
		sliceCEq(t, "stream", second.streamID, 5)
		sliceCEq(t, "provenance", second.provenance, ProvenanceAllocated)
	})

	t.Run("keeps ownership of an unreported stream it has handed out again", func(t *testing.T) {
		m, io, clk := managerWith(empty(), allocateVideo(9))
		sliceCLiveView(t, m)
		sliceCLiveView(t, m)
		clk.Advance(unreportedLeaseGrace * 6)
		sliceCCapabilities(t, m)
		sliceCNoErr(t, sliceCRelease(t, m, StreamVideo, 9))
		sliceCEq(t, "deallocates", sliceCCount(io, cmdVideoStreamDeallocate) > 0, true)
		sliceCEq(t, "leased endpoints", m.leasedEndpoints(), 0)
	})

	t.Run("keeps a stream it allocated its own to give back, however long the device never names it", func(t *testing.T) {
		m, io, clk := managerWith(empty(), allocateVideo(9))
		sliceCLiveView(t, m)
		clk.Advance(unreportedLeaseGrace * 1000)
		sliceCCapabilities(t, m)
		sliceCEq(t, "leased endpoints", m.leasedEndpoints(), 1)
		owned := sliceCCapabilities(t, m)
		sliceCEq(t, "reported video streams", len(owned.Allocated.Video), 0)
		sliceCNoErr(t, sliceCRelease(t, m, StreamVideo, 9))
		sliceCEq(t, "deallocates", sliceCCount(io, cmdVideoStreamDeallocate) > 0, true)
	})

	t.Run("keeps a lease the device has reported, however long the endpoint then stays quiet", func(t *testing.T) {
		m, io, clk := managerWith(empty(), allocateVideo(9))
		allocated := sliceCLiveView(t, m)
		io.setState(sliceCWithVideo(sliceCReportedVideo(9, allocated.video, 0)))
		sliceCCapabilities(t, m)
		clk.Advance(unreportedLeaseGrace * 1000)
		sliceCCapabilities(t, m)
		sliceCEq(t, "leased endpoints", m.leasedEndpoints(), 1)
		sliceCNoErr(t, sliceCRelease(t, m, StreamVideo, 9))
		sliceCEq(t, "deallocates", sliceCCount(io, cmdVideoStreamDeallocate) > 0, true)
	})

	t.Run("does not extend the window of a stream it hands out again", func(t *testing.T) {
		m, io, clk := sliceCAllocating(empty(), 9, 10)
		sliceCLiveView(t, m)
		clk.Advance(unreportedLeaseGrace - time.Millisecond)
		sliceCEq(t, "second stream", sliceCLiveView(t, m).streamID, 9)
		clk.Advance(time.Millisecond)
		third := sliceCLiveView(t, m)
		sliceCEq(t, "third stream", third.streamID, 10)
		sliceCEq(t, "allocates", sliceCCount(io, cmdVideoStreamAllocate), 2)
	})
}

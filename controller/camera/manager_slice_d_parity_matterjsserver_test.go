// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/clock/clocktest"
	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
	"github.com/SukramJ/go-fabric/im"
)

// sliceDIO is the managerWith camera with an invoke that honours its
// context, as clientIO's does, so a camera that never answers is given up
// when the cleanup budget ends; readHook, when set, replaces the state
// read (the TS tests that build their own CameraDeviceIo).
type sliceDIO struct {
	*fakeIO
	readHook func() (*cameraState, error)
}

func (s *sliceDIO) readCameraState(ctx context.Context, t target) (*cameraState, error) {
	if s.readHook != nil {
		return s.readHook()
	}
	return s.fakeIO.readCameraState(ctx, t)
}

func (s *sliceDIO) invoke(ctx context.Context, call deviceCall) (any, error) {
	type answer struct {
		resp any
		err  error
	}
	done := make(chan answer, 1)
	go func() {
		resp, err := s.fakeIO.invoke(ctx, call)
		done <- answer{resp, err}
	}()
	select {
	case a := <-done:
		return a.resp, a.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// sliceDManager is managerWith over sliceDIO; respond also gets the holder,
// as the TS respond closures reach holder.state.
func sliceDManager(state *cameraState, respond func(*fakeIO, *recordedInvoke) (any, error)) (*manager, *sliceDIO, *clocktest.Fake) {
	io := &sliceDIO{fakeIO: &fakeIO{state: state}}
	if respond != nil {
		io.respond = func(r *recordedInvoke) (any, error) { return respond(io.fakeIO, r) }
	}
	clk := clocktest.New(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	return newManager(io, clk, nil), io, clk
}

// sliceDGate is a promise resolved once.
type sliceDGate struct {
	once sync.Once
	ch   chan struct{}
}

func sliceDNewGate() *sliceDGate { return &sliceDGate{ch: make(chan struct{})} }

func (g *sliceDGate) open() { g.once.Do(func() { close(g.ch) }) }

// sliceDNever is a channel closed only when the test ends: the answer of a
// camera that has stopped answering.
func sliceDNever(t *testing.T) chan struct{} {
	t.Helper()
	never := make(chan struct{})
	t.Cleanup(func() { close(never) })
	return never
}

// sliceDAsync runs f on its own goroutine, as an awaited promise.
func sliceDAsync(f func()) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		f()
	}()
	return done
}

// sliceDAdvanceBudget advances clk by the cleanup budget once the budget's
// timer is armed: settle arms it after it started the give-backs.
func sliceDAdvanceBudget(t *testing.T, clk *clocktest.Fake) {
	t.Helper()
	sliceDEventually(t, func() bool { return clk.Pending() > 0 })
	clk.Advance(deviceCleanupBudget)
}

// sliceDEventually waits until cond holds, as a TS test awaits a settled
// promise; it fails the test after a generous wall-clock bound.
func sliceDEventually(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached")
		}
		runtime.Gosched()
	}
}

// sliceDAs is the typed error err carries, or a test failure.
func sliceDAs[E error](t *testing.T, err error) E {
	t.Helper()
	var e E
	if !errors.As(err, &e) {
		t.Fatalf("err = %v (%T), want %T", err, err, e)
	}
	return e
}

func sliceDVideoAlloc(id uint16) (any, error) {
	return csdef.VideoStreamAllocateResponse{VideoStreamId: id}, nil
}

func sliceDAudioAlloc(id uint16) (any, error) {
	return csdef.AudioStreamAllocateResponse{AudioStreamId: id}, nil
}

func sliceDSnapshotAlloc(id uint16) (any, error) {
	return csdef.SnapshotStreamAllocateResponse{SnapshotStreamId: id}, nil
}

func sliceDOffer(id uint16) (any, error) {
	return wpdef.ProvideOfferResponse{WebRtcSessionId: id}, nil
}

func sliceDFrame(w, h uint16) csdef.CaptureSnapshotResponse {
	return csdef.CaptureSnapshotResponse{Data: []byte{1}, ImageCodec: csdef.ImageCodecJpeg, Resolution: csdef.VideoResolutionStruct{Width: w, Height: h}}
}

// sliceDLive is a resolveVideoStream request for a live view in H.265.
func sliceDLive(hints *videoHints) videoRequest {
	return videoRequest{t: tTarget, usage: liveView, limits: selectedVideoCodecLimits{codec: h265}, hints: hints, allowEviction: true}
}

// sliceDPinned1080p is PINNED_1080P_REQUEST's hints.
func sliceDPinned1080p() *videoHints {
	return &videoHints{videoRangeBounds: videoRangeBounds{minResolution: resolution{1920, 1080}, maxResolution: resolution{1920, 1080}}}
}

// sliceDStart is a start of a live-view session over sdp ("" solicits).
func sliceDStart(sdp string, videoState trackState, video *videoHints, audioState trackState, audio *audioHints) startArgs {
	a := startArgs{t: tTarget, connectionID: "conn-1", usage: liveView, allowEviction: true, videoState: videoState, video: video, audioState: audioState, audio: audio}
	if sdp != "" {
		a.sdp = &sdp
	}
	return a
}

// sliceDInvokesOf is the recorded invokes of command.
func sliceDInvokesOf(io *sliceDIO, command string) []recordedInvoke {
	return slices.DeleteFunc(io.recorded(), func(r recordedInvoke) bool { return r.command != command })
}

// sliceDVideoAllocates is the VideoStreamAllocate requests, in order.
func sliceDVideoAllocates(io *sliceDIO) []csdef.VideoStreamAllocateRequest {
	allocates := sliceDInvokesOf(io, cmdVideoStreamAllocate)
	out := make([]csdef.VideoStreamAllocateRequest, 0, len(allocates))
	for _, r := range allocates {
		out = append(out, r.fields.(csdef.VideoStreamAllocateRequest))
	}
	return out
}

func sliceDBoolIs(p *bool, want bool) bool { return p != nil && *p == want }

func sliceDFeatures(fs ...csdef.Feature) csdef.Feature {
	var out csdef.Feature
	for _, f := range fs {
		out |= f
	}
	return out
}

// sliceDCodecs is TS preferredVideoCodec's H264 and H265 constants.
const (
	sliceDH264 = csdef.VideoCodecH264
	sliceDH265 = csdef.VideoCodecHevc
)

// sliceDOfferOf is an offer receiving codecs, unreadable naming the ones
// whose level it cannot read; codecs nil is a video m-line with no rtpmap.
func sliceDOfferOf(codecs []string, unreadable ...string) *sdpVideoConstraints {
	o := &sdpVideoConstraints{video: mediaDisposition{state: mediaReceiving, codecs: codecs}, unreadableCeilingCodecs: map[string]bool{}}
	for _, c := range unreadable {
		o.unreadableCeilingCodecs[c] = true
	}
	return o
}

// The preferredVideoCodec cases (CameraStreamManagerTest.ts:7817-7956,
// matterjs-server at 0bd06f2).
func TestSliceDPreferredVideoCodecParityMatterjsServer(t *testing.T) {
	incompatible := func(t *testing.T, err error) *IncompatibleError {
		t.Helper()
		return sliceDAs[*IncompatibleError](t, err)
	}
	t.Run("honors the caller's stated preference order over the device's own ordering", func(t *testing.T) {
		if c, err := preferredVideoCodec([]csdef.VideoCodecEnum{sliceDH264, sliceDH265}, nil, []string{"H265", "H264"}); err != nil || c != sliceDH265 {
			t.Fatalf("= %v, %v; want H265", c, err)
		}
	})
	t.Run("falls back to the device's order when the caller states no preference", func(t *testing.T) {
		if c, err := preferredVideoCodec([]csdef.VideoCodecEnum{sliceDH264, sliceDH265}, nil, nil); err != nil || c != sliceDH264 {
			t.Fatalf("= %v, %v; want H264", c, err)
		}
	})
	t.Run("ignores a hint codec the device does not support", func(t *testing.T) {
		if c, err := preferredVideoCodec([]csdef.VideoCodecEnum{sliceDH264}, nil, []string{"H265", "H264"}); err != nil || c != sliceDH264 {
			t.Fatalf("= %v, %v; want H264", c, err)
		}
	})
	t.Run("fails typed when no hint codec matches, instead of handing back another one", func(t *testing.T) {
		_, err := preferredVideoCodec([]csdef.VideoCodecEnum{sliceDH264, sliceDH265}, nil, []string{"AV1"})
		e := incompatible(t, err)
		if e.Reason != ReasonCodec || !slices.Equal(e.Requested, []string{"AV1"}) {
			t.Fatalf("= %+v", e)
		}
	})
	t.Run("fails typed when the offer names no codec the camera supports", func(t *testing.T) {
		_, err := preferredVideoCodec([]csdef.VideoCodecEnum{sliceDH265}, sliceDOfferOf([]string{"H264"}), nil)
		e := incompatible(t, err)
		if e.Reason != ReasonCodec || !slices.Equal(e.Requested, []string{"H264"}) {
			t.Fatalf("= %+v", e)
		}
	})
	t.Run("refuses a codec whose level it cannot read rather than treating it as unconstrained", func(t *testing.T) {
		_, err := preferredVideoCodec([]csdef.VideoCodecEnum{sliceDH264}, sliceDOfferOf([]string{"H264"}, "H264"), nil)
		e := incompatible(t, err)
		if e.Reason != ReasonLevel || !slices.Equal(e.Requested, []string{"H264"}) || !slices.Equal(e.Device, []string{"H264"}) {
			t.Fatalf("= %+v", e)
		}
	})
	t.Run("selects the codec whose level it can read over the one it cannot", func(t *testing.T) {
		c, err := preferredVideoCodec([]csdef.VideoCodecEnum{sliceDH264, sliceDH265}, sliceDOfferOf([]string{"H264", "H265"}, "H264"), nil)
		if err != nil || c != sliceDH265 {
			t.Fatalf("= %v, %v; want H265", c, err)
		}
	})
	t.Run("reports a codec mismatch, not a level failure, when the unreadable codec was never shared", func(t *testing.T) {
		_, err := preferredVideoCodec([]csdef.VideoCodecEnum{sliceDH265}, sliceDOfferOf([]string{"H264"}, "H264"), nil)
		e := incompatible(t, err)
		if e.Reason != ReasonCodec || !slices.Equal(e.Requested, []string{"H264"}) {
			t.Fatalf("= %+v", e)
		}
	})
	t.Run("reports the set the failing step narrowed, not the camera's full list", func(t *testing.T) {
		_, err := preferredVideoCodec([]csdef.VideoCodecEnum{sliceDH264, sliceDH265}, sliceDOfferOf([]string{"H264"}), []string{"H265"})
		if e := incompatible(t, err); !slices.Equal(e.Device, []string{"H264"}) {
			t.Fatalf("device = %v, want [H264]", e.Device)
		}
	})
	t.Run("keeps the offer's narrowing when a later hint agrees with it", func(t *testing.T) {
		c, err := preferredVideoCodec([]csdef.VideoCodecEnum{sliceDH264, sliceDH265}, sliceDOfferOf([]string{"H264"}), []string{"H264"})
		if err != nil || c != sliceDH264 {
			t.Fatalf("= %v, %v; want H264", c, err)
		}
	})
	t.Run("honours the caller's codec when the camera advertises no trade-off point", func(t *testing.T) {
		if c, err := preferredVideoCodec(nil, nil, []string{"H265"}); err != nil || c != sliceDH265 {
			t.Fatalf("= %v, %v; want H265", c, err)
		}
	})
	t.Run("defaults to H.264 when neither the camera, the offer nor the caller names a codec", func(t *testing.T) {
		if c, err := preferredVideoCodec(nil, nil, nil); err != nil || c != sliceDH264 {
			t.Fatalf("= %v, %v; want H264", c, err)
		}
	})
	t.Run("keeps a video m-line with no rtpmap from failing the request", func(t *testing.T) {
		if c, err := preferredVideoCodec([]csdef.VideoCodecEnum{sliceDH265}, sliceDOfferOf(nil), nil); err != nil || c != sliceDH265 {
			t.Fatalf("= %v, %v; want H265", c, err)
		}
	})
}

// sliceDBudgetStart is the cleanup-budget describe's START.
func sliceDBudgetStart() startArgs {
	return sliceDStart(videoOffer, trackDemanded, &videoHints{}, trackDeclined, nil)
}

// sliceDStream9 is the stream 9 the camera reports in the deallocate
// cases of the cleanup budget.
func sliceDStream9() allocatedVideoStream {
	s := liveStream(9, 0)
	s.minResolution = resolution{1920, 1080}
	return s
}

// The device cleanup budget cases (CameraStreamManagerTest.ts:7958-8255,
// matterjs-server at 0bd06f2). A camera that never answers is a respond
// blocked until the test ends; sliceDIO gives the invoke up when the
// budget's context ends, as clientIO does.
func TestSliceDDeviceCleanupBudgetParityMatterjsServer(t *testing.T) {
	t.Run("stops waiting for an EndSession the camera never answers, and keeps the session tracked", func(t *testing.T) {
		ctx := t.Context()
		entered, never := sliceDNewGate(), sliceDNever(t)
		m, _, clk := sliceDManager(fixtureState(), func(_ *fakeIO, r *recordedInvoke) (any, error) {
			switch r.command {
			case cmdVideoStreamAllocate:
				return sliceDVideoAlloc(9)
			case callProvideOffer:
				return sliceDOffer(42)
			case callEndSession:
				entered.open()
				<-never
			}
			return nil, nil
		})
		if _, err := m.startStream(ctx, sliceDBudgetStart()); err != nil {
			t.Fatal(err)
		}
		stopping := sliceDAsync(func() { m.stopAll(ctx) })
		<-entered.ch
		sliceDAdvanceBudget(t, clk)
		<-stopping
		if !m.sessions.peerEnded(tNode, tEndpoint, 42, 0, false) {
			t.Fatal("session 42 no longer tracked after the abandoned EndSession")
		}
	})
	t.Run("ends the other cameras' sessions although one camera never answers", func(t *testing.T) {
		ctx := t.Context()
		const other = uint64(6)
		entered, never := sliceDNewGate(), sliceDNever(t)
		var mu sync.Mutex
		next := uint16(42)
		m, io, clk := sliceDManager(fixtureState(), func(_ *fakeIO, r *recordedInvoke) (any, error) {
			switch r.command {
			case cmdVideoStreamAllocate:
				return sliceDVideoAlloc(9)
			case callProvideOffer:
				mu.Lock()
				defer mu.Unlock()
				next++
				return sliceDOffer(next - 1)
			case callEndSession:
				if r.t.nodeID == tNode {
					entered.open()
					<-never
				}
			}
			return nil, nil
		})
		if _, err := m.startStream(ctx, sliceDBudgetStart()); err != nil {
			t.Fatal(err)
		}
		otherStart := sliceDBudgetStart()
		otherStart.t, otherStart.connectionID = target{other, tEndpoint}, "conn-2"
		if _, err := m.startStream(ctx, otherStart); err != nil {
			t.Fatal(err)
		}
		otherEnded := sliceDNewGate()
		m.onEnded = func(e cameraSessionEnded) {
			if e.nodeID == other {
				otherEnded.open()
			}
		}
		stopping := sliceDAsync(func() { m.stopAll(ctx) })
		<-entered.ch
		<-otherEnded.ch
		sliceDAdvanceBudget(t, clk)
		<-stopping
		ends := sliceDInvokesOf(io, callEndSession)
		ended := make([]uint64, 0, len(ends))
		for _, r := range ends {
			ended = append(ended, r.t.nodeID)
		}
		if len(ended) != 2 {
			t.Fatalf("EndSession nodes = %v, want two", ended)
		}
		if m.sessions.peerEnded(other, tEndpoint, 43, 0, false) {
			t.Fatal("the answered camera's session is still tracked")
		}
	})
	t.Run("lets an abandoned EndSession that lands late keep its hands off a session established since", func(t *testing.T) {
		ctx := t.Context()
		entered, answer := sliceDNewGate(), sliceDNewGate()
		t.Cleanup(answer.open)
		var mu sync.Mutex
		endSessions := 0
		m, _, clk := sliceDManager(fixtureState(), func(_ *fakeIO, r *recordedInvoke) (any, error) {
			switch r.command {
			case cmdVideoStreamAllocate:
				return sliceDVideoAlloc(9)
			case callProvideOffer:
				return sliceDOffer(42)
			case callEndSession:
				mu.Lock()
				endSessions++
				first := endSessions == 1
				mu.Unlock()
				if first {
					entered.open()
					<-answer.ch
				}
			}
			return nil, nil
		})
		if _, err := m.startStream(ctx, sliceDBudgetStart()); err != nil {
			t.Fatal(err)
		}
		stopping := sliceDAsync(func() { m.stopAll(ctx) })
		<-entered.ch
		sliceDAdvanceBudget(t, clk)
		<-stopping
		second := sliceDBudgetStart()
		second.connectionID = "conn-2"
		if _, err := m.startStream(ctx, second); err != nil {
			t.Fatal(err)
		}
		answer.open()
		if !m.sessions.peerEnded(tNode, tEndpoint, 42, 0, false) {
			t.Fatal("the late EndSession took the session established since")
		}
	})
	t.Run("lets an abandoned deallocate that lands late keep its hands off a lease taken since", func(t *testing.T) {
		ctx := t.Context()
		entered, answer := sliceDNewGate(), sliceDNewGate()
		t.Cleanup(answer.open)
		var mu sync.Mutex
		deallocates := 0
		m, io, clk := sliceDManager(fixtureState(), func(_ *fakeIO, r *recordedInvoke) (any, error) {
			switch r.command {
			case cmdVideoStreamAllocate:
				return sliceDVideoAlloc(9)
			case callProvideOffer:
				return nil, statusErr(im.StatusFailure)
			case cmdVideoStreamDeallocate:
				mu.Lock()
				deallocates++
				first := deallocates == 1
				mu.Unlock()
				if first {
					entered.open()
					<-answer.ch
				}
			}
			return nil, nil
		})
		starting := sliceDAsync(func() { _, _ = m.startStream(ctx, sliceDBudgetStart()) })
		<-entered.ch
		sliceDAdvanceBudget(t, clk)
		<-starting
		retaken, err := m.resolveVideoStream(ctx, sliceDLive(nil))
		if err != nil {
			t.Fatal(err)
		}
		if retaken.provenance != ProvenanceAllocated {
			t.Fatalf("provenance = %q, want allocated", retaken.provenance)
		}
		answer.open()
		clk.Yield() // the late answer lands, as after setImmediate
		io.setState(fixtureState().with(func(s *cameraState) { s.video = []allocatedVideoStream{sliceDStream9()} }))
		caps, err := m.capabilities(ctx, tTarget)
		if err != nil {
			t.Fatal(err)
		}
		if len(caps.Allocated.Video) != 1 || !caps.Allocated.Video[0].AllocatedByUs {
			t.Fatalf("allocated video = %+v, want stream 9 allocated by this server", caps.Allocated.Video)
		}
	})
	t.Run("lets an abandoned deallocate drop a lease that was only reused since, not allocated again", func(t *testing.T) {
		ctx := t.Context()
		entered, answer := sliceDNewGate(), sliceDNewGate()
		t.Cleanup(answer.open)
		m, io, clk := sliceDManager(fixtureState(), func(_ *fakeIO, r *recordedInvoke) (any, error) {
			switch r.command {
			case cmdVideoStreamAllocate:
				return sliceDVideoAlloc(9)
			case callProvideOffer:
				return nil, statusErr(im.StatusFailure)
			case cmdVideoStreamDeallocate:
				entered.open()
				<-answer.ch
			}
			return nil, nil
		})
		starting := sliceDAsync(func() { _, _ = m.startStream(ctx, sliceDBudgetStart()) })
		<-entered.ch
		sliceDAdvanceBudget(t, clk)
		<-starting
		// The camera has not processed the give-back yet, so it still
		// reports the stream.
		io.setState(fixtureState().with(func(s *cameraState) { s.video = []allocatedVideoStream{sliceDStream9()} }))
		reused, err := m.resolveVideoStream(ctx, sliceDLive(nil))
		if err != nil {
			t.Fatal(err)
		}
		if reused.provenance == ProvenanceAllocated {
			t.Fatal("provenance = allocated, want a reuse")
		}
		answer.open()
		// Only the give-back can drop the lease: the camera still reports
		// the stream.
		sliceDEventually(t, func() bool { return !m.ownsStream(tTarget, StreamVideo, 9) })
		caps, err := m.capabilities(ctx, tTarget)
		if err != nil {
			t.Fatal(err)
		}
		if len(caps.Allocated.Video) != 1 || caps.Allocated.Video[0].AllocatedByUs {
			t.Fatalf("allocated video = %+v, want stream 9 not allocated by this server", caps.Allocated.Video)
		}
	})
	t.Run("fails a request rather than waiting for a give-back the camera never answers", func(t *testing.T) {
		ctx := t.Context()
		entered, never := sliceDNewGate(), sliceDNever(t)
		m, _, clk := sliceDManager(fixtureState(), func(_ *fakeIO, r *recordedInvoke) (any, error) {
			switch r.command {
			case cmdVideoStreamAllocate:
				return sliceDVideoAlloc(9)
			case callProvideOffer:
				return nil, statusErr(im.StatusFailure)
			case cmdVideoStreamDeallocate:
				entered.open()
				<-never
			}
			return nil, nil
		})
		var outcome error
		starting := sliceDAsync(func() { _, outcome = m.startStream(ctx, sliceDBudgetStart()) })
		<-entered.ch
		sliceDAdvanceBudget(t, clk)
		<-starting
		if outcome == nil {
			t.Fatal("the start succeeded")
		}
		if s, ok := deviceStatusOf(outcome); !ok || s != im.StatusFailure {
			t.Fatalf("device status = %v %v, want FAILURE", s, ok)
		}
	})
}

// sliceDAudioOnlyState is AUDIO_ONLY_STATE: Audio and nothing else.
func sliceDAudioOnlyState() *cameraState {
	return fixtureState().with(func(s *cameraState) {
		s.features, s.sensor, s.rateDistortionPoints, s.snapshotCapabilities = csdef.FeatureAudio, nil, nil, nil
	})
}

// sliceDRefusing is refusingManager: the establishing offer refused with
// status, as a privacy switch makes the camera refuse it.
func sliceDRefusing(state *cameraState, status im.StatusCode) (*manager, *sliceDIO) {
	m, io, _ := sliceDManager(state, func(_ *fakeIO, r *recordedInvoke) (any, error) {
		switch r.command {
		case cmdVideoStreamAllocate:
			return sliceDVideoAlloc(9)
		case cmdAudioStreamAllocate:
			return sliceDAudioAlloc(4)
		case callProvideOffer, callSolicitOffer:
			return nil, statusErr(status)
		}
		return nil, nil
	})
	return m, io
}

// sliceDPrivacyState is STATE with Audio, Video and Privacy and the
// switches given.
func sliceDPrivacyState(p privacyState) *cameraState {
	return fixtureState().with(func(s *cameraState) {
		s.features, s.privacy = sliceDFeatures(csdef.FeatureAudio, csdef.FeatureVideo, csdef.FeaturePrivacy), p
	})
}

// The device features and privacy cases (CameraStreamManagerTest.ts:8257-8794,
// matterjs-server at 0bd06f2).
func TestSliceDDeviceFeaturesAndPrivacyParityMatterjsServer(t *testing.T) {
	audioOnlyManager := func() (*manager, *sliceDIO) {
		m, io, _ := sliceDManager(sliceDAudioOnlyState(), func(_ *fakeIO, r *recordedInvoke) (any, error) {
			switch r.command {
			case cmdAudioStreamAllocate:
				return sliceDAudioAlloc(4)
			case callProvideOffer:
				return sliceDOffer(42)
			}
			return nil, nil
		})
		return m, io
	}
	videoAndOffer := func(_ *fakeIO, r *recordedInvoke) (any, error) {
		switch r.command {
		case cmdVideoStreamAllocate:
			return sliceDVideoAlloc(9)
		case callProvideOffer:
			return sliceDOffer(42)
		}
		return nil, nil
	}
	liveStart := func(usage csdef.StreamUsageEnum) startArgs {
		a := sliceDStart(videoOffer, trackDemanded, &videoHints{}, trackDeferred, nil)
		a.usage = usage
		return a
	}
	t.Run("reports the advertised features in the spec's bit order", func(t *testing.T) {
		state := fixtureState().with(func(s *cameraState) {
			s.features = sliceDFeatures(csdef.FeatureNightVision, csdef.FeatureOnScreenDisplay, csdef.FeaturePrivacy, csdef.FeatureAudio)
		})
		m, _, _ := sliceDManager(state, nil)
		caps, err := m.capabilities(t.Context(), tTarget)
		if err != nil {
			t.Fatal(err)
		}
		if want := []string{"Audio", "Privacy", "OnScreenDisplay", "NightVision"}; !slices.Equal(caps.Features, want) {
			t.Fatalf("features = %v, want %v", caps.Features, want)
		}
	})
	t.Run("reports each privacy switch the camera states, and omits one it does not", func(t *testing.T) {
		state := fixtureState().with(func(s *cameraState) {
			s.features = sliceDFeatures(csdef.FeatureVideo, csdef.FeaturePrivacy)
			s.privacy = privacyState{softRecording: boolp(false), softLivestream: boolp(true)}
		})
		m, _, _ := sliceDManager(state, nil)
		caps, err := m.capabilities(t.Context(), tTarget)
		if err != nil {
			t.Fatal(err)
		}
		if !sliceDBoolIs(caps.Privacy.SoftLivestream, true) || !sliceDBoolIs(caps.Privacy.SoftRecording, false) || caps.Privacy.Hard != nil {
			t.Fatalf("privacy = %+v", caps.Privacy)
		}
	})
	t.Run("serves capabilities while every switch is on, because that is how a client learns it", func(t *testing.T) {
		state := fixtureState().with(func(s *cameraState) {
			s.features = sliceDFeatures(csdef.FeatureVideo, csdef.FeaturePrivacy)
			s.privacy = privacyState{hard: boolp(true), softLivestream: boolp(true), softRecording: boolp(true)}
		})
		m, _, _ := sliceDManager(state, nil)
		caps, err := m.capabilities(t.Context(), tTarget)
		if err != nil {
			t.Fatal(err)
		}
		if !sliceDBoolIs(caps.Privacy.Hard, true) || !slices.Contains(caps.Features, "Privacy") {
			t.Fatalf("capabilities = %+v", caps)
		}
	})
	t.Run("opens an audio-only session on a camera with no Video feature when video was left to it", func(t *testing.T) {
		m, io := audioOnlyManager()
		session, err := m.startStream(t.Context(), sliceDStart(videoAndAudioOffer, trackDeferred, nil, trackDeferred, nil))
		if err != nil {
			t.Fatal(err)
		}
		if session.video != nil || session.audio == nil || session.audio.streamID != 4 {
			t.Fatalf("session = %+v", session)
		}
		if slices.Contains(io.commands(), cmdVideoStreamAllocate) {
			t.Fatal("VideoStreamAllocate sent")
		}
		offer := sliceDInvokesOf(io, callProvideOffer)[0].fields.(wpdef.ProvideOfferRequest)
		if offer.VideoStreams != nil || offer.AudioStreams == nil || !slices.Equal(*offer.AudioStreams, []uint16{4}) {
			t.Fatalf("offer streams = %v %v", offer.VideoStreams, offer.AudioStreams)
		}
	})
	t.Run("names the missing feature when the caller demanded video from such a camera", func(t *testing.T) {
		m, io := audioOnlyManager()
		_, err := m.startStream(t.Context(), sliceDStart(videoAndAudioOffer, trackDemanded, &videoHints{}, trackDeferred, nil))
		e := sliceDAs[*IncompatibleError](t, err)
		if e.Reason != ReasonFeature || e.Track != TrackVideo || e.Feature != "Video" {
			t.Fatalf("= %+v", e)
		}
		if c := io.commands(); slices.Contains(c, cmdVideoStreamAllocate) || slices.Contains(c, callProvideOffer) {
			t.Fatalf("commands = %v", c)
		}
	})
	t.Run("names the missing feature when the caller demanded audio from a camera with no Audio feature", func(t *testing.T) {
		state := fixtureState().with(func(s *cameraState) { s.features, s.microphone = csdef.FeatureVideo, nil })
		m, _, _ := sliceDManager(state, videoAndOffer)
		_, err := m.startStream(t.Context(), sliceDStart(videoAndAudioOffer, trackDeferred, nil, trackDemanded, &audioHints{}))
		e := sliceDAs[*IncompatibleError](t, err)
		if e.Reason != ReasonFeature || e.Track != TrackAudio || e.Feature != "Audio" {
			t.Fatalf("= %+v", e)
		}
	})
	t.Run("refuses audio on a camera that reports a microphone without advertising the feature", func(t *testing.T) {
		state := fixtureState().with(func(s *cameraState) { s.features = csdef.FeatureVideo })
		m, io, _ := sliceDManager(state, videoAndOffer)
		_, err := m.startStream(t.Context(), sliceDStart(videoAndAudioOffer, trackDeferred, nil, trackDemanded, &audioHints{codecs: []string{"OPUS"}}))
		e := sliceDAs[*IncompatibleError](t, err)
		if e.Track != TrackAudio || e.Feature != "Audio" {
			t.Fatalf("= %+v", e)
		}
		if slices.Contains(io.commands(), cmdAudioStreamAllocate) {
			t.Fatal("AudioStreamAllocate sent")
		}
	})
	t.Run("names the missing feature when a camera with no Snapshot feature is asked for a frame", func(t *testing.T) {
		state := fixtureState().with(func(s *cameraState) { s.features, s.snapshotCapabilities = csdef.FeatureVideo, nil })
		m, _, _ := sliceDManager(state, nil)
		_, err := m.snapshot(t.Context(), snapshotArgs{t: tTarget})
		if e := sliceDAs[*IncompatibleError](t, err); e.Reason != ReasonFeature || e.Feature != "Snapshot" {
			t.Fatalf("= %+v", e)
		}
	})
	t.Run("refuses a snapshot on a camera that lists capabilities without advertising the feature", func(t *testing.T) {
		state := fixtureState().with(func(s *cameraState) { s.features = csdef.FeatureVideo })
		m, io, _ := sliceDManager(state, func(_ *fakeIO, r *recordedInvoke) (any, error) {
			if r.command == cmdSnapshotStreamAllocate {
				return sliceDSnapshotAlloc(3)
			}
			return nil, nil
		})
		_, err := m.snapshot(t.Context(), snapshotArgs{t: tTarget})
		if e := sliceDAs[*IncompatibleError](t, err); e.Feature != "Snapshot" {
			t.Fatalf("= %+v", e)
		}
		if slices.Contains(io.commands(), cmdSnapshotStreamAllocate) {
			t.Fatal("SnapshotStreamAllocate sent")
		}
	})
	t.Run("does not gate a track on a feature map the camera has not stated", func(t *testing.T) {
		state := fixtureState().with(func(s *cameraState) { s.features = csdef.FeatureHighDynamicRange })
		m, io, _ := sliceDManager(state, func(_ *fakeIO, r *recordedInvoke) (any, error) {
			switch r.command {
			case cmdVideoStreamAllocate:
				return sliceDVideoAlloc(9)
			case cmdAudioStreamAllocate:
				return sliceDAudioAlloc(4)
			case callProvideOffer:
				return sliceDOffer(42)
			}
			return nil, nil
		})
		session, err := m.startStream(t.Context(), sliceDStart(videoAndAudioOffer, trackDemanded, &videoHints{}, trackDemanded, &audioHints{}))
		if err != nil {
			t.Fatal(err)
		}
		if session.video == nil || session.video.streamID != 9 || session.audio == nil || session.audio.streamID != 4 {
			t.Fatalf("session = %+v", session)
		}
		if !slices.Contains(io.commands(), cmdVideoStreamAllocate) {
			t.Fatal("no VideoStreamAllocate")
		}
	})
	t.Run("reports no feature list at all when the camera has not stated its feature map", func(t *testing.T) {
		m, _, _ := sliceDManager(fixtureState().with(func(s *cameraState) { s.features = csdef.FeatureHighDynamicRange }), nil)
		caps, err := m.capabilities(t.Context(), tTarget)
		if err != nil {
			t.Fatal(err)
		}
		if caps.Features != nil {
			t.Fatalf("features = %v, want none", caps.Features)
		}
	})
	t.Run("reports the hard privacy switch as the reason the camera refused the session", func(t *testing.T) {
		m, io := sliceDRefusing(sliceDPrivacyState(privacyState{hard: boolp(true), softLivestream: boolp(false), softRecording: boolp(false)}), im.StatusInvalidInState)
		_, err := m.startStream(t.Context(), liveStart(liveView))
		e := sliceDAs[*PrivacyError](t, err)
		if !slices.Equal(e.Modes, []PrivacyMode{PrivacyHard}) || e.DeviceStatus != im.StatusInvalidInState {
			t.Fatalf("= %+v", e)
		}
		if !slices.Contains(io.commands(), cmdVideoStreamDeallocate) {
			t.Fatal("the refused session's stream was not given back")
		}
	})
	t.Run("reports the soft livestream switch for a LiveView session", func(t *testing.T) {
		m, _ := sliceDRefusing(sliceDPrivacyState(privacyState{softLivestream: boolp(true), softRecording: boolp(false), hard: boolp(false)}), im.StatusInvalidInState)
		_, err := m.startStream(t.Context(), liveStart(liveView))
		if e := sliceDAs[*PrivacyError](t, err); !slices.Equal(e.Modes, []PrivacyMode{PrivacySoftLivestream}) {
			t.Fatalf("= %+v", e)
		}
	})
	t.Run("does not blame the livestream switch for a Recording session it does not cover", func(t *testing.T) {
		m, _ := sliceDRefusing(sliceDPrivacyState(privacyState{softLivestream: boolp(true), softRecording: boolp(false), hard: boolp(false)}), im.StatusInvalidInState)
		_, err := m.startStream(t.Context(), liveStart(csdef.StreamUsageRecording))
		if isTyped(err) {
			t.Fatalf("err = %v, want the device's own", err)
		}
		if s, ok := deviceStatusOf(err); !ok || s != im.StatusInvalidInState {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("reports the soft recording switch for a Recording session", func(t *testing.T) {
		m, _ := sliceDRefusing(sliceDPrivacyState(privacyState{softRecording: boolp(true), softLivestream: boolp(false), hard: boolp(false)}), im.StatusInvalidInState)
		_, err := m.startStream(t.Context(), liveStart(csdef.StreamUsageRecording))
		if e := sliceDAs[*PrivacyError](t, err); !slices.Equal(e.Modes, []PrivacyMode{PrivacySoftRecording}) {
			t.Fatalf("= %+v", e)
		}
	})
	t.Run("does not blame the recording switch for a LiveView session it does not cover", func(t *testing.T) {
		m, _ := sliceDRefusing(sliceDPrivacyState(privacyState{softRecording: boolp(true), softLivestream: boolp(false), hard: boolp(false)}), im.StatusInvalidInState)
		_, err := m.startStream(t.Context(), liveStart(liveView))
		if isTyped(err) {
			t.Fatalf("err = %v, want the device's own", err)
		}
		if s, ok := deviceStatusOf(err); !ok || s != im.StatusInvalidInState {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("leaves an INVALID_IN_STATE no switch explains as the device's own error", func(t *testing.T) {
		m, _ := sliceDRefusing(sliceDPrivacyState(privacyState{}), im.StatusInvalidInState)
		_, err := m.startStream(t.Context(), liveStart(liveView))
		if err == nil || isTyped(err) {
			t.Fatalf("err = %v, want the device's own", err)
		}
	})
	t.Run("keeps a typed failure raised under the offer, even one wrapping the device's own status", func(t *testing.T) {
		state := sliceDPrivacyState(privacyState{hard: boolp(true)})
		m, _, _ := sliceDManager(state, func(_ *fakeIO, r *recordedInvoke) (any, error) {
			switch r.command {
			case cmdVideoStreamAllocate:
				return sliceDVideoAlloc(9)
			case callProvideOffer:
				return nil, fmt.Errorf("%w: %w", &localError{msg: "provider relay failed"}, statusErr(im.StatusInvalidInState))
			}
			return nil, nil
		})
		_, err := m.startStream(t.Context(), liveStart(liveView))
		sliceDAs[*localError](t, err)
		var pe *PrivacyError
		if errors.As(err, &pe) {
			t.Fatalf("err = %v, replaced by a privacy explanation", err)
		}
	})
	t.Run("keeps the device's refusal when the endpoint no longer reports the camera behaviour", func(t *testing.T) {
		m, _, _ := sliceDManager(sliceDPrivacyState(privacyState{hard: boolp(true)}), func(io *fakeIO, r *recordedInvoke) (any, error) {
			switch r.command {
			case cmdVideoStreamAllocate:
				return sliceDVideoAlloc(9)
			case callProvideOffer:
				io.setState(nil)
				return nil, statusErr(im.StatusInvalidInState)
			}
			return nil, nil
		})
		_, err := m.startStream(t.Context(), liveStart(liveView))
		if isTyped(err) {
			t.Fatalf("err = %v, want the device's own", err)
		}
		if s, ok := deviceStatusOf(err); !ok || s != im.StatusInvalidInState {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("keeps the device's refusal when the state read that would explain it fails", func(t *testing.T) {
		state := sliceDPrivacyState(privacyState{hard: boolp(true)})
		var mu sync.Mutex
		refused := false
		m, io, _ := sliceDManager(state, func(_ *fakeIO, r *recordedInvoke) (any, error) {
			switch r.command {
			case cmdVideoStreamAllocate:
				return sliceDVideoAlloc(9)
			case callProvideOffer:
				mu.Lock()
				refused = true
				mu.Unlock()
				return nil, statusErr(im.StatusInvalidInState)
			}
			return nil, nil
		})
		io.readHook = func() (*cameraState, error) {
			mu.Lock()
			defer mu.Unlock()
			if refused {
				return nil, errors.New("node is gone")
			}
			return state, nil
		}
		_, err := m.startStream(t.Context(), liveStart(liveView))
		if s, ok := deviceStatusOf(err); !ok || s != im.StatusInvalidInState {
			t.Fatalf("err = %v", err)
		}
		if strings.Contains(err.Error(), "node is gone") {
			t.Fatalf("err = %v names the failed read", err)
		}
	})
	snapshotRefused := func(_ *fakeIO, r *recordedInvoke) (any, error) {
		switch r.command {
		case cmdSnapshotStreamAllocate:
			return sliceDSnapshotAlloc(3)
		case cmdCaptureSnapshot:
			return nil, statusErr(im.StatusInvalidInState)
		}
		return nil, nil
	}
	snapshotPrivacy := func(p privacyState) *cameraState {
		return fixtureState().with(func(s *cameraState) {
			s.features, s.privacy = sliceDFeatures(csdef.FeatureSnapshot, csdef.FeaturePrivacy), p
		})
	}
	t.Run("reports a privacy switch as the reason a snapshot was refused", func(t *testing.T) {
		m, _, _ := sliceDManager(snapshotPrivacy(privacyState{hard: boolp(false), softLivestream: boolp(true), softRecording: boolp(false)}), snapshotRefused)
		_, err := m.snapshot(t.Context(), snapshotArgs{t: tTarget})
		e := sliceDAs[*PrivacyError](t, err)
		if !slices.Equal(e.Modes, []PrivacyMode{PrivacySoftLivestream}) || e.DeviceStatus != im.StatusInvalidInState {
			t.Fatalf("= %+v", e)
		}
	})
	t.Run("does not blame a snapshot on the recording switch, which carries no snapshot", func(t *testing.T) {
		m, _, _ := sliceDManager(snapshotPrivacy(privacyState{hard: boolp(false), softLivestream: boolp(false), softRecording: boolp(true)}), snapshotRefused)
		_, err := m.snapshot(t.Context(), snapshotArgs{t: tTarget})
		if isTyped(err) {
			t.Fatalf("err = %v, want the device's own", err)
		}
		if s, ok := deviceStatusOf(err); !ok || s != im.StatusInvalidInState {
			t.Fatalf("err = %v", err)
		}
	})
}

// sliceDOverlayState is OVERLAY_STATE: a camera that draws both overlays.
func sliceDOverlayState() *cameraState {
	return fixtureState().with(func(s *cameraState) {
		s.features = sliceDFeatures(csdef.FeatureVideo, csdef.FeatureSnapshot, csdef.FeatureWatermark, csdef.FeatureOnScreenDisplay)
	})
}

// sliceDAllocating is allocatingManager.
func sliceDAllocating(state *cameraState) (*manager, *sliceDIO) {
	m, io, _ := sliceDManager(state, func(_ *fakeIO, r *recordedInvoke) (any, error) {
		switch r.command {
		case cmdVideoStreamAllocate:
			return sliceDVideoAlloc(9)
		case cmdSnapshotStreamAllocate:
			return sliceDSnapshotAlloc(3)
		case cmdCaptureSnapshot:
			return sliceDFrame(640, 480), nil
		}
		return nil, nil
	})
	return m, io
}

// sliceDOverlayVideo is the overlays describe's videoStream.
func sliceDOverlayVideo(o overlayBounds) allocatedVideoStream {
	return allocatedVideoStream{
		videoStreamID: 7, overlays: o, streamUsage: liveView, videoCodec: h265,
		minResolution: resolution{640, 360}, maxResolution: resolution{2560, 1440},
		minFrameRate: 1, maxFrameRate: 30, minBitRate: 800000, maxBitRate: 8000000, keyFrameInterval: 4000,
	}
}

// sliceDOverlaySnapshot is the overlays describe's snapshotStream.
func sliceDOverlaySnapshot(o overlayBounds) allocatedSnapshotStream {
	return allocatedSnapshotStream{
		snapshotStreamID: 8, overlays: o, imageCodec: csdef.ImageCodecJpeg,
		minResolution: resolution{1920, 1080}, maxResolution: resolution{1920, 1080}, quality: 90, frameRate: 1,
	}
}

func sliceDOv(wm, osd overlayFlag) overlayBounds { return overlayBounds{watermark: wm, osd: osd} }

// The overlays cases (CameraStreamManagerTest.ts:8796-9208, matterjs-server
// at 0bd06f2).
func TestSliceDOverlaysParityMatterjsServer(t *testing.T) {
	resolve := func(t *testing.T, state *cameraState, hints *videoHints) (*resolvedStream, *sliceDIO, error) {
		t.Helper()
		m, io := sliceDAllocating(state)
		s, err := m.resolveVideoStream(t.Context(), sliceDLive(hints))
		return s, io, err
	}
	firstAllocate := func(t *testing.T, io *sliceDIO) csdef.VideoStreamAllocateRequest {
		t.Helper()
		all := sliceDVideoAllocates(io)
		if len(all) == 0 {
			t.Fatal("no VideoStreamAllocate")
		}
		return all[0]
	}
	lastAllocate := func(t *testing.T, io *sliceDIO) csdef.VideoStreamAllocateRequest {
		t.Helper()
		all := sliceDVideoAllocates(io)
		if len(all) == 0 {
			t.Fatal("no VideoStreamAllocate")
		}
		return all[len(all)-1]
	}
	overlaysHint := func(o overlayBounds) *videoHints { return &videoHints{overlays: o} }
	withVideo := func(base *cameraState, streams ...allocatedVideoStream) *cameraState {
		return base.with(func(s *cameraState) { s.video = streams })
	}
	exhaustedManager := func(state *cameraState) *manager {
		m, _, _ := sliceDManager(state, func(_ *fakeIO, r *recordedInvoke) (any, error) {
			if r.command == cmdVideoStreamAllocate {
				return nil, statusErr(im.StatusResourceExhausted)
			}
			return nil, nil
		})
		return m
	}
	// restoringManager refuses every ask for capacity, so the scope
	// restores the victim, and that allocate succeeds.
	restoringManager := func(state *cameraState) (*manager, *sliceDIO) {
		allocates := 0
		m, io, _ := sliceDManager(state, func(_ *fakeIO, r *recordedInvoke) (any, error) {
			if r.command == cmdVideoStreamAllocate {
				allocates++
				if allocates <= maxVideoAsks {
					return nil, statusErr(im.StatusResourceExhausted)
				}
				return sliceDVideoAlloc(12)
			}
			return nil, nil
		})
		return m, io
	}
	t.Run("asks a camera that draws overlays for neither when the caller stated neither", func(t *testing.T) {
		_, io, err := resolve(t, sliceDOverlayState(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if a := firstAllocate(t, io); !sliceDBoolIs(a.WatermarkEnabled, false) || !sliceDBoolIs(a.OsdEnabled, false) {
			t.Fatalf("allocate overlays = %v %v", a.WatermarkEnabled, a.OsdEnabled)
		}
	})
	t.Run("sends what the caller stated", func(t *testing.T) {
		s, io, err := resolve(t, sliceDOverlayState(), overlaysHint(sliceDOv(flagOn, flagUnstated)))
		if err != nil {
			t.Fatal(err)
		}
		if a := firstAllocate(t, io); !sliceDBoolIs(a.WatermarkEnabled, true) || !sliceDBoolIs(a.OsdEnabled, false) {
			t.Fatalf("allocate overlays = %v %v", a.WatermarkEnabled, a.OsdEnabled)
		}
		if s.video.overlays != sliceDOv(flagOn, flagOff) {
			t.Fatalf("envelope overlays = %+v", s.video.overlays)
		}
	})
	t.Run("sends neither field to a camera that advertises neither feature", func(t *testing.T) {
		_, io, err := resolve(t, fixtureState(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if a := firstAllocate(t, io); a.WatermarkEnabled != nil || a.OsdEnabled != nil {
			t.Fatalf("allocate overlays = %v %v", a.WatermarkEnabled, a.OsdEnabled)
		}
	})
	t.Run("accepts a caller that declined an overlay the camera cannot draw anyway", func(t *testing.T) {
		_, io, err := resolve(t, fixtureState(), overlaysHint(sliceDOv(flagOff, flagOff)))
		if err != nil {
			t.Fatal(err)
		}
		if a := firstAllocate(t, io); a.WatermarkEnabled != nil || a.OsdEnabled != nil {
			t.Fatalf("allocate overlays = %v %v", a.WatermarkEnabled, a.OsdEnabled)
		}
	})
	t.Run("names the missing feature when the caller demanded an overlay the camera cannot draw", func(t *testing.T) {
		_, _, err := resolve(t, fixtureState(), overlaysHint(sliceDOv(flagOn, flagUnstated)))
		if e := sliceDAs[*IncompatibleError](t, err); e.Reason != ReasonFeature || e.Feature != "Watermark" || e.Track != TrackVideo {
			t.Fatalf("= %+v", e)
		}
	})
	t.Run("names OnScreenDisplay for a demanded OSD the camera cannot draw", func(t *testing.T) {
		_, _, err := resolve(t, fixtureState(), overlaysHint(sliceDOv(flagUnstated, flagOn)))
		if e := sliceDAs[*IncompatibleError](t, err); e.Feature != "OnScreenDisplay" {
			t.Fatalf("= %+v", e)
		}
	})
	t.Run("does not reuse a stream without a watermark for a caller that asked for one", func(t *testing.T) {
		s, io, err := resolve(t, withVideo(sliceDOverlayState(), sliceDOverlayVideo(sliceDOv(flagOff, flagOff))), overlaysHint(sliceDOv(flagOn, flagUnstated)))
		if err != nil {
			t.Fatal(err)
		}
		if s.provenance != ProvenanceAllocated || s.streamID != 9 || !slices.Contains(io.commands(), cmdVideoStreamAllocate) {
			t.Fatalf("stream = %+v", s)
		}
	})
	t.Run("does not reuse a watermarked stream for a caller that asked for no watermark", func(t *testing.T) {
		s, _, err := resolve(t, withVideo(sliceDOverlayState(), sliceDOverlayVideo(sliceDOv(flagOn, flagOff))), overlaysHint(sliceDOv(flagOff, flagUnstated)))
		if err != nil || s.provenance != ProvenanceAllocated {
			t.Fatalf("= %+v, %v", s, err)
		}
	})
	t.Run("does not reuse a watermarked stream for a caller that stated nothing either", func(t *testing.T) {
		s, _, err := resolve(t, withVideo(sliceDOverlayState(), sliceDOverlayVideo(sliceDOv(flagOn, flagOff))), nil)
		if err != nil || s.provenance != ProvenanceAllocated {
			t.Fatalf("= %+v, %v", s, err)
		}
	})
	t.Run("reuses a stream whose overlays are the ones the request resolved to", func(t *testing.T) {
		s, _, err := resolve(t, withVideo(sliceDOverlayState(), sliceDOverlayVideo(sliceDOv(flagOn, flagOff))), overlaysHint(sliceDOv(flagOn, flagOff)))
		if err != nil || s.provenance == ProvenanceAllocated || s.streamID != 7 {
			t.Fatalf("= %+v, %v", s, err)
		}
	})
	t.Run("reports a reused stream's own overlays, not the ones the request resolved to", func(t *testing.T) {
		state := withVideo(fixtureState().with(func(s *cameraState) { s.features = 0 }), sliceDOverlayVideo(sliceDOv(flagOn, flagOff)))
		s, _, err := resolve(t, state, overlaysHint(sliceDOv(flagOn, flagUnstated)))
		if err != nil || s.provenance == ProvenanceAllocated {
			t.Fatalf("= %+v, %v", s, err)
		}
		if s.video.overlays != sliceDOv(flagOn, flagOff) {
			t.Fatalf("envelope overlays = %+v", s.video.overlays)
		}
	})
	t.Run("reports a degraded stream's own overlays", func(t *testing.T) {
		held := sliceDOverlayVideo(sliceDOv(flagOn, flagOff))
		held.referenceCount = 1
		s, err := exhaustedManager(withVideo(sliceDOverlayState(), held)).resolveVideoStream(t.Context(), sliceDLive(nil))
		if err != nil {
			t.Fatal(err)
		}
		if !s.degraded || s.video.overlays != sliceDOv(flagOn, flagOff) {
			t.Fatalf("stream = %+v, overlays %+v", s, s.video.overlays)
		}
	})
	t.Run("puts a taken stream's overlays back although the camera has not stated its feature map", func(t *testing.T) {
		victim := sliceDOverlayVideo(sliceDOv(flagOn, flagUnstated))
		victim.videoStreamID = 11
		m, io := restoringManager(withVideo(fixtureState().with(func(s *cameraState) { s.features = 0 }), victim))
		_, err := m.resolveVideoStream(t.Context(), sliceDLive(overlaysHint(sliceDOv(flagOff, flagUnstated))))
		if !isTyped(err) {
			t.Fatalf("err = %v, want a typed failure", err)
		}
		if r := lastAllocate(t, io); !sliceDBoolIs(r.WatermarkEnabled, true) || r.OsdEnabled != nil {
			t.Fatalf("restore overlays = %v %v", r.WatermarkEnabled, r.OsdEnabled)
		}
	})
	t.Run("hands out a stream with an unasked-for overlay only from the degraded rung, flagged", func(t *testing.T) {
		held := sliceDOverlayVideo(sliceDOv(flagOn, flagOff))
		held.referenceCount = 1
		s, err := exhaustedManager(withVideo(sliceDOverlayState(), held)).resolveVideoStream(t.Context(), sliceDLive(nil))
		if err != nil || s.streamID != 7 || !s.degraded {
			t.Fatalf("= %+v, %v", s, err)
		}
	})
	t.Run("refuses rather than degrading onto a stream whose overlays the caller stated against", func(t *testing.T) {
		held := sliceDOverlayVideo(sliceDOv(flagOn, flagOff))
		held.referenceCount = 1
		_, err := exhaustedManager(withVideo(sliceDOverlayState(), held)).resolveVideoStream(t.Context(), sliceDLive(overlaysHint(sliceDOv(flagOff, flagUnstated))))
		sliceDAs[*ExhaustedError](t, err)
	})
	t.Run("puts a taken stream's own overlays back on the replacement", func(t *testing.T) {
		victim := sliceDOverlayVideo(sliceDOv(flagOn, flagOn))
		victim.videoStreamID = 11
		m, io := restoringManager(withVideo(sliceDOverlayState(), victim))
		_, err := m.resolveVideoStream(t.Context(), sliceDLive(overlaysHint(sliceDOv(flagOff, flagUnstated))))
		if !isTyped(err) {
			t.Fatalf("err = %v, want a typed failure", err)
		}
		if r := lastAllocate(t, io); !sliceDBoolIs(r.WatermarkEnabled, true) || !sliceDBoolIs(r.OsdEnabled, true) {
			t.Fatalf("restore overlays = %v %v", r.WatermarkEnabled, r.OsdEnabled)
		}
	})
	snapshotAllocate := func(t *testing.T, io *sliceDIO) csdef.SnapshotStreamAllocateRequest {
		t.Helper()
		all := sliceDInvokesOf(io, cmdSnapshotStreamAllocate)
		if len(all) == 0 {
			t.Fatal("no SnapshotStreamAllocate")
		}
		return all[0].fields.(csdef.SnapshotStreamAllocateRequest)
	}
	t.Run("asks a snapshot camera that draws overlays for neither when the caller stated neither", func(t *testing.T) {
		m, io := sliceDAllocating(sliceDOverlayState())
		if _, err := m.snapshot(t.Context(), snapshotArgs{t: tTarget}); err != nil {
			t.Fatal(err)
		}
		if a := snapshotAllocate(t, io); !sliceDBoolIs(a.WatermarkEnabled, false) || !sliceDBoolIs(a.OsdEnabled, false) {
			t.Fatalf("allocate overlays = %v %v", a.WatermarkEnabled, a.OsdEnabled)
		}
	})
	t.Run("sends a snapshot's stated overlays", func(t *testing.T) {
		m, io := sliceDAllocating(sliceDOverlayState())
		if _, err := m.snapshot(t.Context(), snapshotArgs{t: tTarget, overlays: sliceDOv(flagUnstated, flagOn)}); err != nil {
			t.Fatal(err)
		}
		if a := snapshotAllocate(t, io); !sliceDBoolIs(a.WatermarkEnabled, false) || !sliceDBoolIs(a.OsdEnabled, true) {
			t.Fatalf("allocate overlays = %v %v", a.WatermarkEnabled, a.OsdEnabled)
		}
	})
	t.Run("sends neither snapshot field to a camera that advertises neither feature", func(t *testing.T) {
		m, io := sliceDAllocating(fixtureState())
		if _, err := m.snapshot(t.Context(), snapshotArgs{t: tTarget}); err != nil {
			t.Fatal(err)
		}
		if a := snapshotAllocate(t, io); a.WatermarkEnabled != nil || a.OsdEnabled != nil {
			t.Fatalf("allocate overlays = %v %v", a.WatermarkEnabled, a.OsdEnabled)
		}
	})
	t.Run("names the missing feature when a snapshot demanded an overlay the camera cannot draw", func(t *testing.T) {
		m, _ := sliceDAllocating(fixtureState())
		_, err := m.snapshot(t.Context(), snapshotArgs{t: tTarget, overlays: sliceDOv(flagOn, flagUnstated)})
		if e := sliceDAs[*IncompatibleError](t, err); e.Reason != ReasonFeature || e.Feature != "Watermark" {
			t.Fatalf("= %+v", e)
		}
	})
	withSnapshot := func(o overlayBounds) *cameraState {
		return sliceDOverlayState().with(func(s *cameraState) { s.snapshot = []allocatedSnapshotStream{sliceDOverlaySnapshot(o)} })
	}
	t.Run("does not adopt a snapshot stream without the watermark the caller asked for", func(t *testing.T) {
		m, io := sliceDAllocating(withSnapshot(sliceDOv(flagOff, flagOff)))
		r, err := m.snapshot(t.Context(), snapshotArgs{t: tTarget, overlays: sliceDOv(flagOn, flagUnstated)})
		if err != nil || r.streamID != 3 || !slices.Contains(io.commands(), cmdSnapshotStreamAllocate) {
			t.Fatalf("= %+v, %v", r, err)
		}
	})
	t.Run("does not adopt a watermarked snapshot stream for a caller that stated nothing", func(t *testing.T) {
		m, _ := sliceDAllocating(withSnapshot(sliceDOv(flagOn, flagOff)))
		r, err := m.snapshot(t.Context(), snapshotArgs{t: tTarget})
		if err != nil || r.streamID != 3 {
			t.Fatalf("= %+v, %v", r, err)
		}
	})
	t.Run("adopts a snapshot stream whose overlays are the ones the request resolved to", func(t *testing.T) {
		m, io := sliceDAllocating(withSnapshot(sliceDOv(flagOn, flagOff)))
		r, err := m.snapshot(t.Context(), snapshotArgs{t: tTarget, overlays: sliceDOv(flagOn, flagOff)})
		if err != nil || r.streamID != 8 || slices.Contains(io.commands(), cmdSnapshotStreamAllocate) {
			t.Fatalf("= %+v, %v, commands %v", r, err, io.commands())
		}
	})
	t.Run("reports each allocated stream's overlays", func(t *testing.T) {
		state := sliceDOverlayState().with(func(s *cameraState) {
			s.video = []allocatedVideoStream{sliceDOverlayVideo(sliceDOv(flagOn, flagOff))}
			s.snapshot = []allocatedSnapshotStream{sliceDOverlaySnapshot(sliceDOv(flagOff, flagOn))}
		})
		m, _, _ := sliceDManager(state, nil)
		caps, err := m.capabilities(t.Context(), tTarget)
		if err != nil {
			t.Fatal(err)
		}
		v, s := caps.Allocated.Video[0], caps.Allocated.Snapshot[0]
		if !sliceDBoolIs(v.WatermarkEnabled, true) || !sliceDBoolIs(v.OsdEnabled, false) || !sliceDBoolIs(s.WatermarkEnabled, false) || !sliceDBoolIs(s.OsdEnabled, true) {
			t.Fatalf("video %v %v, snapshot %v %v", v.WatermarkEnabled, v.OsdEnabled, s.WatermarkEnabled, s.OsdEnabled)
		}
	})
	t.Run("sends only what the caller stated while the camera has not stated its feature map", func(t *testing.T) {
		_, io, err := resolve(t, fixtureState().with(func(s *cameraState) { s.features = 0 }), overlaysHint(sliceDOv(flagUnstated, flagOn)))
		if err != nil {
			t.Fatal(err)
		}
		if a := firstAllocate(t, io); !sliceDBoolIs(a.OsdEnabled, true) || a.WatermarkEnabled != nil {
			t.Fatalf("allocate overlays = %v %v", a.WatermarkEnabled, a.OsdEnabled)
		}
	})
}

// sliceDWider is WIDER: 720p..1080p, held by another controller.
func sliceDWider() allocatedVideoStream {
	return allocatedVideoStream{
		videoStreamID: 7, overlays: noOverlays, streamUsage: liveView, videoCodec: h265,
		minResolution: resolution{1280, 720}, maxResolution: resolution{1920, 1080},
		minFrameRate: 1, maxFrameRate: 30, minBitRate: 800000, maxBitRate: 4000000, keyFrameInterval: 3000, referenceCount: 1,
	}
}

// sliceDAudio64k is AUDIO_64K.
func sliceDAudio64k() allocatedAudioStream {
	return allocatedAudioStream{audioStreamID: 4, streamUsage: liveView, audioCodec: csdef.AudioCodecOpus, channelCount: 1, sampleRate: 48000, bitRate: 64000, bitDepth: 16, referenceCount: 1}
}

// sliceDJpeg640 is JPEG_640.
func sliceDJpeg640() allocatedSnapshotStream {
	return allocatedSnapshotStream{
		snapshotStreamID: 5, overlays: noOverlays, imageCodec: csdef.ImageCodecJpeg,
		minResolution: resolution{640, 480}, maxResolution: resolution{640, 480}, quality: 75, frameRate: 1,
	}
}

// sliceDAnswerSnapshotWith5 refuses the 1080p snapshot capability and
// answers the 640x480 one with stream 5.
func sliceDAnswerSnapshotWith5(r *recordedInvoke) (any, error) {
	switch r.command {
	case cmdCaptureSnapshot:
		return sliceDFrame(640, 480), nil
	case cmdSnapshotStreamAllocate:
		if r.fields.(csdef.SnapshotStreamAllocateRequest).MaxResolution.Width == 1920 {
			return nil, statusErr(im.StatusDynamicConstraintError)
		}
		return sliceDSnapshotAlloc(5)
	}
	return nil, nil
}

// The allocate-answered-with-an-existing-stream cases
// (CameraStreamManagerTest.ts:9210-9516, matterjs-server at 0bd06f2).
func TestSliceDAllocateAnsweredWithExistingStreamParityMatterjsServer(t *testing.T) {
	t.Run("reports a video stream another controller holds as adopted, with the range the camera narrowed it to", func(t *testing.T) {
		wider := sliceDWider()
		narrowed := wider
		narrowed.minResolution = resolution{1920, 1080}
		m, io, _ := sliceDManager(fixtureState().with(func(s *cameraState) { s.video = []allocatedVideoStream{wider} }), func(io *fakeIO, _ *recordedInvoke) (any, error) {
			io.setState(fixtureState().with(func(s *cameraState) { s.video = []allocatedVideoStream{narrowed} }))
			return sliceDVideoAlloc(7)
		})
		r, err := m.resolveVideoStream(t.Context(), sliceDLive(sliceDPinned1080p()))
		if err != nil {
			t.Fatal(err)
		}
		if c := io.commands(); !slices.Equal(c, []string{cmdVideoStreamAllocate}) {
			t.Fatalf("commands = %v", c)
		}
		if r.streamID != 7 || r.provenance != ProvenanceAdopted || r.video.minResolution != narrowed.minResolution {
			t.Fatalf("= %+v, envelope %+v", r, r.video)
		}
	})
	t.Run("adopts an audio stream another controller holds with more channels than the request", func(t *testing.T) {
		existing := allocatedAudioStream{audioStreamID: 4, streamUsage: liveView, channelCount: 2, sampleRate: 48000, bitRate: 64000, bitDepth: 16, referenceCount: 1}
		m, io, _ := sliceDManager(fixtureState().with(func(s *cameraState) { s.audio = []allocatedAudioStream{existing} }), func(*fakeIO, *recordedInvoke) (any, error) {
			return sliceDAudioAlloc(4)
		})
		r, err := m.resolveAudioStream(t.Context(), audioRequest{t: tTarget, usage: csdef.StreamUsageRecording}, trackDeferred)
		if err != nil {
			t.Fatal(err)
		}
		if c := io.commands(); !slices.Equal(c, []string{cmdAudioStreamAllocate}) {
			t.Fatalf("commands = %v", c)
		}
		if r == nil || r.streamID != 4 || r.provenance != ProvenanceAdopted || r.audio.bitRate != 64000 {
			t.Fatalf("= %+v", r)
		}
	})
	t.Run("reports its own video stream the camera has not reported yet as reused", func(t *testing.T) {
		allocates := 0
		m, io, _ := sliceDManager(fixtureState(), func(io *fakeIO, r *recordedInvoke) (any, error) {
			allocates++
			if allocates == 2 {
				f := r.fields.(csdef.VideoStreamAllocateRequest)
				io.setState(fixtureState().with(func(s *cameraState) {
					s.video = []allocatedVideoStream{{
						videoStreamID: 9, overlays: noOverlays, streamUsage: liveView, videoCodec: h265,
						minResolution: resolution{1920, 1080}, maxResolution: resolution{1920, 1080},
						minFrameRate: int(f.MinFrameRate), maxFrameRate: int(f.MaxFrameRate),
						minBitRate: int(f.MinBitRate), maxBitRate: int(f.MaxBitRate), keyFrameInterval: int(f.KeyFrameInterval),
					}}
				}))
			}
			return sliceDVideoAlloc(9)
		})
		first, err := m.resolveVideoStream(t.Context(), sliceDLive(nil))
		if err != nil {
			t.Fatal(err)
		}
		second, err := m.resolveVideoStream(t.Context(), sliceDLive(sliceDPinned1080p()))
		if err != nil {
			t.Fatal(err)
		}
		if c := io.commands(); !slices.Equal(c, []string{cmdVideoStreamAllocate, cmdVideoStreamAllocate}) {
			t.Fatalf("commands = %v", c)
		}
		if first.provenance != ProvenanceAllocated || second.streamID != 9 || second.provenance != ProvenanceReused {
			t.Fatalf("first %+v, second %+v", first, second)
		}
	})
	t.Run("reports its own audio stream the camera has not reported yet as reused", func(t *testing.T) {
		stereo := fixtureState().with(func(s *cameraState) {
			s.microphone = &audioCapabilities{supportedCodecs: []csdef.AudioCodecEnum{csdef.AudioCodecOpus}, maxNumberOfChannels: 2, supportedSampleRates: []int{48000}, supportedBitDepths: []int{16}}
		})
		m, io, _ := sliceDManager(stereo, func(*fakeIO, *recordedInvoke) (any, error) { return sliceDAudioAlloc(9) })
		first, err := m.resolveAudioStream(t.Context(), audioRequest{t: tTarget, usage: liveView, hints: &audioHints{channelCount: 1}}, trackDemanded)
		if err != nil {
			t.Fatal(err)
		}
		second, err := m.resolveAudioStream(t.Context(), audioRequest{t: tTarget, usage: liveView}, trackDeferred)
		if err != nil {
			t.Fatal(err)
		}
		if c := io.commands(); !slices.Equal(c, []string{cmdAudioStreamAllocate, cmdAudioStreamAllocate}) {
			t.Fatalf("commands = %v", c)
		}
		if first == nil || first.provenance != ProvenanceAllocated || second == nil || second.streamID != 9 || second.provenance != ProvenanceReused {
			t.Fatalf("first %+v, second %+v", first, second)
		}
	})
	t.Run("reports a snapshot stream another controller holds as adopted", func(t *testing.T) {
		m, _, _ := sliceDManager(fixtureState().with(func(s *cameraState) { s.snapshot = []allocatedSnapshotStream{sliceDJpeg640()} }), func(_ *fakeIO, r *recordedInvoke) (any, error) {
			return sliceDAnswerSnapshotWith5(r)
		})
		r, err := m.snapshot(t.Context(), snapshotArgs{t: tTarget})
		if err != nil || r.streamID != 5 || r.provenance != ProvenanceAdopted {
			t.Fatalf("= %+v, %v", r, err)
		}
	})
	t.Run("leaves a video stream another controller holds on the camera when the session fails", func(t *testing.T) {
		m, io, _ := sliceDManager(fixtureState().with(func(s *cameraState) { s.video = []allocatedVideoStream{sliceDWider()} }), func(_ *fakeIO, r *recordedInvoke) (any, error) {
			switch r.command {
			case cmdVideoStreamAllocate:
				return sliceDVideoAlloc(7)
			case callProvideOffer:
				return nil, statusErr(im.StatusFailure)
			}
			return nil, nil
		})
		_, err := m.startStream(t.Context(), sliceDStart(videoOffer, trackDemanded, sliceDPinned1080p(), trackDeclined, nil))
		if err == nil {
			t.Fatal("the start succeeded")
		}
		if c := io.commands(); !slices.Contains(c, cmdVideoStreamAllocate) || slices.Contains(c, cmdVideoStreamDeallocate) {
			t.Fatalf("commands = %v", c)
		}
	})
	t.Run("leaves an audio stream another controller holds on the camera when the session fails", func(t *testing.T) {
		m, io, _ := sliceDManager(fixtureState().with(func(s *cameraState) { s.audio = []allocatedAudioStream{sliceDAudio64k()} }), func(_ *fakeIO, r *recordedInvoke) (any, error) {
			switch r.command {
			case cmdVideoStreamAllocate:
				return sliceDVideoAlloc(9)
			case cmdAudioStreamAllocate:
				return sliceDAudioAlloc(4)
			case callProvideOffer:
				return nil, statusErr(im.StatusFailure)
			}
			return nil, nil
		})
		_, err := m.startStream(t.Context(), sliceDStart(videoAndAudioOffer, trackDemanded, &videoHints{}, trackDemanded, &audioHints{bitRate: 32000}))
		if err == nil {
			t.Fatal("the start succeeded")
		}
		c := io.commands()
		if !slices.Contains(c, cmdAudioStreamAllocate) || slices.Contains(c, cmdAudioStreamDeallocate) || !slices.Contains(c, cmdVideoStreamDeallocate) {
			t.Fatalf("commands = %v", c)
		}
	})
	t.Run("leaves a snapshot stream another controller holds on the camera when the capture fails", func(t *testing.T) {
		m, io, _ := sliceDManager(fixtureState().with(func(s *cameraState) { s.snapshot = []allocatedSnapshotStream{sliceDJpeg640()} }), func(_ *fakeIO, r *recordedInvoke) (any, error) {
			if r.command == cmdCaptureSnapshot {
				return nil, statusErr(im.StatusFailure)
			}
			return sliceDAnswerSnapshotWith5(r)
		})
		if _, err := m.snapshot(t.Context(), snapshotArgs{t: tTarget}); err == nil {
			t.Fatal("the snapshot succeeded")
		}
		if slices.Contains(io.commands(), cmdSnapshotStreamDeallocate) {
			t.Fatalf("commands = %v", io.commands())
		}
	})
	t.Run("reports its own snapshot stream the camera has not reported yet as reused", func(t *testing.T) {
		m, _, _ := sliceDManager(fixtureState(), func(_ *fakeIO, r *recordedInvoke) (any, error) {
			switch r.command {
			case cmdSnapshotStreamAllocate:
				return sliceDSnapshotAlloc(1)
			case cmdCaptureSnapshot:
				return sliceDFrame(640, 480), nil
			}
			return nil, nil
		})
		first, err := m.snapshot(t.Context(), snapshotArgs{t: tTarget})
		if err != nil {
			t.Fatal(err)
		}
		second, err := m.snapshot(t.Context(), snapshotArgs{t: tTarget})
		if err != nil {
			t.Fatal(err)
		}
		if first.provenance != ProvenanceAllocated || second.streamID != 1 || second.provenance != ProvenanceReused {
			t.Fatalf("first %+v, second %+v", first, second)
		}
	})
	t.Run("reports an id the camera reissued after it lost the listed stream as allocated", func(t *testing.T) {
		listed := sliceDJpeg640()
		listed.minResolution, listed.maxResolution = resolution{1920, 1080}, resolution{1920, 1080}
		listed.encodedPixels, listed.hardwareEncoder = true, true
		captures := 0
		m, _, _ := sliceDManager(fixtureState().with(func(s *cameraState) { s.snapshot = []allocatedSnapshotStream{listed} }), func(_ *fakeIO, r *recordedInvoke) (any, error) {
			switch r.command {
			case cmdSnapshotStreamAllocate:
				return sliceDSnapshotAlloc(5)
			case cmdCaptureSnapshot:
				captures++
				if captures == 1 {
					return nil, statusErr(im.StatusNotFound)
				}
				return sliceDFrame(640, 480), nil
			}
			return nil, nil
		})
		r, err := m.snapshot(t.Context(), snapshotArgs{t: tTarget})
		if err != nil {
			t.Fatal(err)
		}
		if captures != 2 || r.streamID != 5 || r.provenance != ProvenanceAllocated {
			t.Fatalf("captures %d, result %+v", captures, r)
		}
	})
}

// sliceDNarrowedTo is narrowedTo: s with the range an allocate asked for.
func sliceDNarrowedTo(s allocatedVideoStream, f csdef.VideoStreamAllocateRequest) allocatedVideoStream {
	s.minResolution, s.maxResolution = resolutionOf(f.MinResolution), resolutionOf(f.MaxResolution)
	s.minFrameRate, s.maxFrameRate = int(f.MinFrameRate), int(f.MaxFrameRate)
	s.minBitRate, s.maxBitRate = int(f.MinBitRate), int(f.MaxBitRate)
	s.keyFrameInterval = int(f.KeyFrameInterval)
	return s
}

// The allocate-answered-outside-the-caller's-bounds cases
// (CameraStreamManagerTest.ts:9557-9897, matterjs-server at 0bd06f2).
func TestSliceDAllocateAnsweredOutsideBoundsParityMatterjsServer(t *testing.T) {
	videoState := func(streams ...allocatedVideoStream) *cameraState {
		return fixtureState().with(func(s *cameraState) { s.video = streams })
	}
	answer := func(id uint16) func(*fakeIO, *recordedInvoke) (any, error) {
		return func(_ *fakeIO, r *recordedInvoke) (any, error) {
			switch r.command {
			case cmdVideoStreamAllocate:
				return sliceDVideoAlloc(id)
			case cmdAudioStreamAllocate:
				return sliceDAudioAlloc(id)
			}
			return nil, nil
		}
	}
	slow := func() allocatedVideoStream {
		s := sliceDWider()
		s.minResolution, s.maxFrameRate = resolution{1920, 1080}, 10
		return s
	}
	t.Run("refuses a video stream the camera did not narrow to the stated bounds, and leaves it alone", func(t *testing.T) {
		m, io, _ := sliceDManager(videoState(sliceDWider()), answer(7))
		_, err := m.resolveVideoStream(t.Context(), sliceDLive(sliceDPinned1080p()))
		if e := sliceDAs[*IncompatibleError](t, err); e.Reason != ReasonBounds || e.Track != TrackVideo {
			t.Fatalf("= %+v", e)
		}
		if c := io.commands(); !slices.Equal(c, []string{cmdVideoStreamAllocate}) {
			t.Fatalf("commands = %v", c)
		}
		if n := m.leasedEndpoints(); n != 0 {
			t.Fatalf("leased endpoints = %d", n)
		}
	})
	t.Run("adopts a video stream another controller holds below the computed bit rate range", func(t *testing.T) {
		recording := sliceDWider()
		recording.streamUsage, recording.minResolution, recording.minBitRate, recording.maxBitRate = csdef.StreamUsageRecording, resolution{1920, 1080}, 100000, 200000
		m, io, _ := sliceDManager(videoState(recording), answer(7))
		r, err := m.resolveVideoStream(t.Context(), sliceDLive(sliceDPinned1080p()))
		if err != nil {
			t.Fatal(err)
		}
		if c := io.commands(); !slices.Equal(c, []string{cmdVideoStreamAllocate}) {
			t.Fatalf("commands = %v", c)
		}
		if r.streamID != 7 || r.provenance != ProvenanceAdopted || !r.degraded {
			t.Fatalf("= %+v", r)
		}
	})
	t.Run("fails without a lease when the camera no longer lists the video stream it answered with", func(t *testing.T) {
		m, _, _ := sliceDManager(videoState(sliceDWider()), func(io *fakeIO, _ *recordedInvoke) (any, error) {
			io.setState(fixtureState())
			return sliceDVideoAlloc(7)
		})
		_, err := m.resolveVideoStream(t.Context(), sliceDLive(sliceDPinned1080p()))
		sliceDAs[*localError](t, err)
		if n := m.leasedEndpoints(); n != 0 {
			t.Fatalf("leased endpoints = %d", n)
		}
	})
	t.Run("puts back a stream it freed to make room when the stream the camera answers with is outside the bounds", func(t *testing.T) {
		idle := sliceDWider()
		idle.videoStreamID, idle.streamUsage, idle.videoCodec, idle.referenceCount = 3, csdef.StreamUsageRecording, csdef.VideoCodecH264, 0
		var mu sync.Mutex
		freed := false
		m, io, _ := sliceDManager(videoState(idle, sliceDWider()), func(_ *fakeIO, r *recordedInvoke) (any, error) {
			mu.Lock()
			defer mu.Unlock()
			if r.command == cmdVideoStreamDeallocate {
				freed = true
			}
			if r.command != cmdVideoStreamAllocate {
				return nil, nil
			}
			if r.fields.(csdef.VideoStreamAllocateRequest).StreamUsage != liveView {
				return sliceDVideoAlloc(20)
			}
			if !freed {
				return nil, statusErr(im.StatusResourceExhausted)
			}
			return sliceDVideoAlloc(7)
		})
		_, err := m.resolveVideoStream(t.Context(), sliceDLive(sliceDPinned1080p()))
		sliceDAs[*IncompatibleError](t, err)
		i := slices.IndexFunc(sliceDVideoAllocates(io), func(f csdef.VideoStreamAllocateRequest) bool { return f.StreamUsage == csdef.StreamUsageRecording })
		if i < 0 || sliceDVideoAllocates(io)[i].VideoCodec != csdef.VideoCodecH264 {
			t.Fatalf("restore = %d in %+v", i, sliceDVideoAllocates(io))
		}
		deallocates := sliceDInvokesOf(io, cmdVideoStreamDeallocate)
		freedIDs := make([]uint16, 0, len(deallocates))
		for _, r := range deallocates {
			freedIDs = append(freedIDs, r.fields.(csdef.VideoStreamDeallocateRequest).VideoStreamId)
		}
		if !slices.Equal(freedIDs, []uint16{3}) {
			t.Fatalf("deallocated = %v, want [3]", freedIDs)
		}
	})
	audioState := func(streams ...allocatedAudioStream) *cameraState {
		return fixtureState().with(func(s *cameraState) { s.audio = streams })
	}
	t.Run("refuses an audio stream at a bit rate other than the one stated, and leaves it alone", func(t *testing.T) {
		m, io, _ := sliceDManager(audioState(sliceDAudio64k()), answer(4))
		_, err := m.resolveAudioStream(t.Context(), audioRequest{t: tTarget, usage: liveView, hints: &audioHints{bitRate: 32000}}, trackDemanded)
		if e := sliceDAs[*IncompatibleError](t, err); e.Reason != ReasonBounds || e.Track != TrackAudio {
			t.Fatalf("= %+v", e)
		}
		if c := io.commands(); !slices.Equal(c, []string{cmdAudioStreamAllocate}) {
			t.Fatalf("commands = %v", c)
		}
		if n := m.leasedEndpoints(); n != 0 {
			t.Fatalf("leased endpoints = %d", n)
		}
	})
	t.Run("refuses an audio stream in another codec than the one it asked for", func(t *testing.T) {
		aac := sliceDAudio64k()
		aac.audioCodec = csdef.AudioCodecAacLc
		m, _, _ := sliceDManager(audioState(aac), answer(4))
		_, err := m.resolveAudioStream(t.Context(), audioRequest{t: tTarget, usage: liveView, hints: &audioHints{}}, trackDemanded)
		sliceDAs[*IncompatibleError](t, err)
		if n := m.leasedEndpoints(); n != 0 {
			t.Fatalf("leased endpoints = %d", n)
		}
	})
	t.Run("fails without a lease when the camera no longer lists the audio stream it answered with", func(t *testing.T) {
		stereo := sliceDAudio64k()
		stereo.channelCount = 2
		m, _, _ := sliceDManager(audioState(stereo), func(io *fakeIO, _ *recordedInvoke) (any, error) {
			io.setState(fixtureState())
			return sliceDAudioAlloc(4)
		})
		_, err := m.resolveAudioStream(t.Context(), audioRequest{t: tTarget, usage: liveView, hints: &audioHints{}}, trackDemanded)
		sliceDAs[*localError](t, err)
		if n := m.leasedEndpoints(); n != 0 {
			t.Fatalf("leased endpoints = %d", n)
		}
	})
	snapshotState := func(features csdef.Feature, s allocatedSnapshotStream) *cameraState {
		return fixtureState().with(func(st *cameraState) {
			if features != 0 {
				st.features = features
			}
			st.snapshot = []allocatedSnapshotStream{s}
		})
	}
	withWatermark := sliceDFeatures(csdef.FeatureAudio, csdef.FeatureVideo, csdef.FeatureSnapshot, csdef.FeatureWatermark)
	answer5 := func(_ *fakeIO, r *recordedInvoke) (any, error) { return sliceDAnswerSnapshotWith5(r) }
	t.Run("refuses a snapshot stream whose overlays differ from the ones stated, without capturing", func(t *testing.T) {
		watermarked := sliceDJpeg640()
		watermarked.overlays = sliceDOv(flagOn, flagUnstated)
		m, io, _ := sliceDManager(snapshotState(withWatermark, watermarked), answer5)
		_, err := m.snapshot(t.Context(), snapshotArgs{t: tTarget, overlays: sliceDOv(flagOff, flagUnstated)})
		sliceDAs[*IncompatibleError](t, err)
		if c := io.commands(); slices.Contains(c, cmdCaptureSnapshot) || slices.Contains(c, cmdSnapshotStreamDeallocate) {
			t.Fatalf("commands = %v", c)
		}
	})
	t.Run("refuses a snapshot stream in another codec than the one stated", func(t *testing.T) {
		png := sliceDJpeg640()
		png.imageCodec = csdef.ImageCodecHeic
		m, _, _ := sliceDManager(snapshotState(0, png), answer5)
		jpeg := csdef.ImageCodecJpeg
		_, err := m.snapshot(t.Context(), snapshotArgs{t: tTarget, codec: &jpeg})
		sliceDAs[*IncompatibleError](t, err)
	})
	large := func() allocatedSnapshotStream {
		s := sliceDJpeg640()
		s.maxResolution = resolution{800, 600}
		return s
	}
	t.Run("captures from a snapshot stream whose range exceeds the stated ceiling when the frame stays within it", func(t *testing.T) {
		m, io, _ := sliceDManager(snapshotState(0, large()), answer5)
		r, err := m.snapshot(t.Context(), snapshotArgs{t: tTarget, maxResolution: resolution{640, 480}})
		if err != nil {
			t.Fatal(err)
		}
		if r.streamID != 5 || r.resolution != (resolution{640, 480}) {
			t.Fatalf("= %+v", r)
		}
		capture := sliceDInvokesOf(io, cmdCaptureSnapshot)[0].fields.(csdef.CaptureSnapshotRequest)
		if capture.RequestedResolution != (csdef.VideoResolutionStruct{Width: 640, Height: 480}) {
			t.Fatalf("requested = %+v", capture.RequestedResolution)
		}
	})
	t.Run("refuses a frame above the stated ceiling from the snapshot stream the camera answered with, and leaves it alone", func(t *testing.T) {
		m, io, _ := sliceDManager(snapshotState(0, large()), func(_ *fakeIO, r *recordedInvoke) (any, error) {
			if r.command == cmdCaptureSnapshot {
				return sliceDFrame(800, 600), nil
			}
			return sliceDAnswerSnapshotWith5(r)
		})
		_, err := m.snapshot(t.Context(), snapshotArgs{t: tTarget, maxResolution: resolution{640, 480}})
		if e := sliceDAs[*IncompatibleError](t, err); e.Reason != ReasonBounds {
			t.Fatalf("= %+v", e)
		}
		if slices.Contains(io.commands(), cmdSnapshotStreamDeallocate) {
			t.Fatalf("commands = %v", io.commands())
		}
	})
	t.Run("captures from a snapshot stream with overlays the caller did not state", func(t *testing.T) {
		watermarked := sliceDJpeg640()
		watermarked.overlays = sliceDOv(flagOn, flagUnstated)
		m, io, _ := sliceDManager(snapshotState(withWatermark, watermarked), answer5)
		r, err := m.snapshot(t.Context(), snapshotArgs{t: tTarget})
		if err != nil {
			t.Fatal(err)
		}
		if r.streamID != 5 || r.provenance != ProvenanceAdopted {
			t.Fatalf("= %+v", r)
		}
		if a := sliceDInvokesOf(io, cmdSnapshotStreamAllocate)[0].fields.(csdef.SnapshotStreamAllocateRequest); !sliceDBoolIs(a.WatermarkEnabled, false) {
			t.Fatalf("allocate watermark = %v", a.WatermarkEnabled)
		}
	})
	t.Run("hands out another stream within the bounds when the one the camera answers with is outside them", func(t *testing.T) {
		other := slow()
		other.videoStreamID = 8
		m, io, _ := sliceDManager(videoState(sliceDWider(), other), answer(7))
		r, err := m.resolveVideoStream(t.Context(), sliceDLive(sliceDPinned1080p()))
		if err != nil {
			t.Fatal(err)
		}
		if r.streamID != 8 || !r.degraded {
			t.Fatalf("= %+v", r)
		}
		if c := io.commands(); !slices.Equal(c, []string{cmdVideoStreamAllocate}) {
			t.Fatalf("commands = %v", c)
		}
	})
	t.Run("does not hand out the stream the camera answered with at its range from before the allocate", func(t *testing.T) {
		m, _, _ := sliceDManager(videoState(slow()), func(io *fakeIO, _ *recordedInvoke) (any, error) {
			widened := slow()
			widened.maxResolution = resolution{2560, 1440}
			io.setState(videoState(widened))
			return sliceDVideoAlloc(7)
		})
		_, err := m.resolveVideoStream(t.Context(), sliceDLive(sliceDPinned1080p()))
		sliceDAs[*IncompatibleError](t, err)
		if n := m.leasedEndpoints(); n != 0 {
			t.Fatalf("leased endpoints = %d", n)
		}
	})
	t.Run("names no device status when its own check refuses the stream the camera answered with", func(t *testing.T) {
		asked := 0
		m, _, _ := sliceDManager(videoState(sliceDWider()), func(*fakeIO, *recordedInvoke) (any, error) {
			asked++
			if asked == 1 {
				return nil, statusErr(im.StatusDynamicConstraintError)
			}
			return sliceDVideoAlloc(7)
		})
		_, err := m.resolveVideoStream(t.Context(), sliceDLive(sliceDPinned1080p()))
		e := sliceDAs[*IncompatibleError](t, err)
		if asked != 2 || e.DeviceStatus != nil {
			t.Fatalf("asked %d, device status %v", asked, e.DeviceStatus)
		}
	})
	t.Run("flags a stream the camera answers with as degraded when it is within the bounds but not the computed envelope", func(t *testing.T) {
		m, _, _ := sliceDManager(videoState(slow()), answer(7))
		r, err := m.resolveVideoStream(t.Context(), sliceDLive(sliceDPinned1080p()))
		if err != nil || r.streamID != 7 || !r.degraded {
			t.Fatalf("= %+v, %v", r, err)
		}
	})
	t.Run("does not flag a stream the camera narrowed to the window it was asked for", func(t *testing.T) {
		m, _, _ := sliceDManager(videoState(slow()), func(io *fakeIO, r *recordedInvoke) (any, error) {
			io.setState(videoState(sliceDNarrowedTo(slow(), r.fields.(csdef.VideoStreamAllocateRequest))))
			return sliceDVideoAlloc(7)
		})
		r, err := m.resolveVideoStream(t.Context(), sliceDLive(sliceDPinned1080p()))
		if err != nil || r.streamID != 7 || r.degraded {
			t.Fatalf("= %+v, %v", r, err)
		}
	})
	t.Run("fails without a lease when the stream list cannot be read after the camera answered with an existing video stream", func(t *testing.T) {
		m, io, _ := sliceDManager(nil, answer(7))
		reads := 0
		io.readHook = func() (*cameraState, error) {
			reads++
			if reads > 1 {
				return nil, errors.New("read failed")
			}
			return videoState(sliceDWider()), nil
		}
		_, err := m.resolveVideoStream(t.Context(), sliceDLive(sliceDPinned1080p()))
		if err == nil || err.Error() != "read failed" {
			t.Fatalf("err = %v, want the failed read", err)
		}
		if n := m.leasedEndpoints(); n != 0 {
			t.Fatalf("leased endpoints = %d", n)
		}
	})
	t.Run("fails when the camera no longer lists the snapshot stream it answered with", func(t *testing.T) {
		m, _, _ := sliceDManager(snapshotState(0, sliceDJpeg640()), func(io *fakeIO, r *recordedInvoke) (any, error) {
			if r.command == cmdSnapshotStreamAllocate {
				io.setState(fixtureState())
			}
			return sliceDAnswerSnapshotWith5(r)
		})
		_, err := m.snapshot(t.Context(), snapshotArgs{t: tTarget})
		sliceDAs[*localError](t, err)
	})
}

// sliceDSnapshot5 is SNAPSHOT_5: 1920x1080 on the hardware encoder.
func sliceDSnapshot5(id uint16) allocatedSnapshotStream {
	return allocatedSnapshotStream{
		snapshotStreamID: id, overlays: noOverlays, imageCodec: csdef.ImageCodecJpeg,
		minResolution: resolution{1920, 1080}, maxResolution: resolution{1920, 1080}, quality: 75, frameRate: 1,
		encodedPixels: true, hardwareEncoder: true,
	}
}

// sliceDRecordingStream is RECORDING_STREAM: an idle recording stream.
func sliceDRecordingStream(id uint16) allocatedVideoStream {
	s := liveStream(id, 0)
	s.streamUsage = csdef.StreamUsageRecording
	return s
}

// sliceDEvictionLive is the eviction describe's liveView request.
func sliceDEvictionLive() videoRequest {
	return sliceDLive(&videoHints{videoRangeBounds: videoRangeBounds{maxResolution: resolution{1280, 720}}})
}

// sliceDRecording is the eviction describe's recording request without
// hints.
func sliceDRecording() videoRequest {
	r := sliceDLive(nil)
	r.usage = csdef.StreamUsageRecording
	return r
}

// sliceDWithoutRoom is cameraWithoutRoom: this server's snapshot (5) and
// recording allocations answered (recordingIDs in turn, the last
// repeated), the live view refused until a stream is freed.
func sliceDWithoutRoom(recordingIDs ...uint16) (*manager, *sliceDIO, func() string) {
	if len(recordingIDs) == 0 {
		recordingIDs = []uint16{9}
	}
	freed := false
	m, io, _ := sliceDManager(fixtureState(), func(_ *fakeIO, r *recordedInvoke) (any, error) {
		switch r.command {
		case cmdSnapshotStreamAllocate:
			return sliceDSnapshotAlloc(5)
		case cmdCaptureSnapshot:
			return sliceDFrame(1920, 1080), nil
		case cmdVideoStreamDeallocate, cmdSnapshotStreamDeallocate:
			freed = true
			return nil, nil
		case cmdVideoStreamAllocate:
			if r.fields.(csdef.VideoStreamAllocateRequest).StreamUsage != liveView {
				id := recordingIDs[0]
				if len(recordingIDs) > 1 {
					recordingIDs = recordingIDs[1:]
				}
				return sliceDVideoAlloc(id)
			}
			if !freed {
				return nil, statusErr(im.StatusResourceExhausted)
			}
			return sliceDVideoAlloc(11)
		}
		return nil, nil
	})
	firstFreed := func() string {
		for _, r := range io.recorded() {
			switch f := r.fields.(type) {
			case csdef.VideoStreamDeallocateRequest:
				return fmt.Sprintf("%s %d", r.command, f.VideoStreamId)
			case csdef.SnapshotStreamDeallocateRequest:
				return fmt.Sprintf("%s %d", r.command, f.SnapshotStreamId)
			}
		}
		return ""
	}
	return m, io, firstFreed
}

// sliceDStaysFull is cameraThatStaysFull: every live view refused for
// capacity, this server's recording allocates answered with answers in
// turn (a uint16 id or an error).
func sliceDStaysFull(answers ...any) (*manager, *sliceDIO, func() int) {
	m, io, _ := sliceDManager(fixtureState(), func(_ *fakeIO, r *recordedInvoke) (any, error) {
		if r.command != cmdVideoStreamAllocate {
			return nil, nil
		}
		if r.fields.(csdef.VideoStreamAllocateRequest).StreamUsage == liveView {
			return nil, statusErr(im.StatusResourceExhausted)
		}
		var a any
		if len(answers) > 0 {
			a, answers = answers[0], answers[1:]
		}
		switch v := a.(type) {
		case error:
			return nil, v
		case uint16:
			return sliceDVideoAlloc(v)
		}
		return nil, nil
	})
	recordingAllocates := func() int {
		n := 0
		for _, f := range sliceDVideoAllocates(io) {
			if f.StreamUsage != liveView {
				n++
			}
		}
		return n
	}
	return m, io, recordingAllocates
}

// sliceDServable is SERVABLE_STREAM: within every bound of a live view
// without hints, kept out of reuse by its bit rate range.
func sliceDServable() allocatedVideoStream {
	s := sliceDRecordingStream(9)
	s.minBitRate, s.maxBitRate = 100000, 200000
	return s
}

// sliceDOwnServable is ownServableStream.
func sliceDOwnServable(t *testing.T, m *manager, io *sliceDIO) {
	t.Helper()
	if _, err := m.resolveVideoStream(t.Context(), sliceDRecording()); err != nil {
		t.Fatal(err)
	}
	io.setState(fixtureState().with(func(s *cameraState) { s.video = []allocatedVideoStream{sliceDServable()} }))
}

// The eviction order cases (CameraStreamManagerTest.ts:9899-10144,
// matterjs-server at 0bd06f2).
func TestSliceDEvictionOrderParityMatterjsServer(t *testing.T) {
	t.Run("frees its own snapshot stream before its own idle video stream", func(t *testing.T) {
		m, io, firstFreed := sliceDWithoutRoom()
		if _, err := m.snapshot(t.Context(), snapshotArgs{t: tTarget}); err != nil {
			t.Fatal(err)
		}
		if _, err := m.resolveVideoStream(t.Context(), sliceDRecording()); err != nil {
			t.Fatal(err)
		}
		io.setState(fixtureState().with(func(s *cameraState) {
			s.video, s.snapshot = []allocatedVideoStream{sliceDRecordingStream(9)}, []allocatedSnapshotStream{sliceDSnapshot5(5)}
		}))
		if _, err := m.resolveVideoStream(t.Context(), sliceDEvictionLive()); err != nil {
			t.Fatal(err)
		}
		if got := firstFreed(); got != "snapshotStreamDeallocate 5" {
			t.Fatalf("first freed = %q", got)
		}
	})
	t.Run("frees its own idle video stream before another controller's snapshot stream", func(t *testing.T) {
		m, io, firstFreed := sliceDWithoutRoom()
		if _, err := m.resolveVideoStream(t.Context(), sliceDRecording()); err != nil {
			t.Fatal(err)
		}
		io.setState(fixtureState().with(func(s *cameraState) {
			s.video, s.snapshot = []allocatedVideoStream{sliceDRecordingStream(9)}, []allocatedSnapshotStream{sliceDSnapshot5(6)}
		}))
		if _, err := m.resolveVideoStream(t.Context(), sliceDEvictionLive()); err != nil {
			t.Fatal(err)
		}
		if got := firstFreed(); got != "videoStreamDeallocate 9" {
			t.Fatalf("first freed = %q", got)
		}
	})
	t.Run("frees another controller's snapshot stream before its idle video stream", func(t *testing.T) {
		m, io, firstFreed := sliceDWithoutRoom()
		io.setState(fixtureState().with(func(s *cameraState) {
			s.video, s.snapshot = []allocatedVideoStream{sliceDRecordingStream(21)}, []allocatedSnapshotStream{sliceDSnapshot5(6)}
		}))
		if _, err := m.resolveVideoStream(t.Context(), sliceDEvictionLive()); err != nil {
			t.Fatal(err)
		}
		if got := firstFreed(); got != "snapshotStreamDeallocate 6" {
			t.Fatalf("first freed = %q", got)
		}
	})
	t.Run("frees its own idle stream the degraded rung could hand out, and allocates a fresh one", func(t *testing.T) {
		m, io, firstFreed := sliceDWithoutRoom()
		if _, err := m.resolveVideoStream(t.Context(), sliceDRecording()); err != nil {
			t.Fatal(err)
		}
		io.setState(fixtureState().with(func(s *cameraState) { s.video = []allocatedVideoStream{sliceDServable()} }))
		r, err := m.resolveVideoStream(t.Context(), sliceDLive(nil))
		if err != nil {
			t.Fatal(err)
		}
		if got := firstFreed(); got != "videoStreamDeallocate 9" {
			t.Fatalf("first freed = %q", got)
		}
		if r.streamID != 11 || r.degraded {
			t.Fatalf("= %+v", r)
		}
	})
	t.Run("frees its own stream the degraded rung could not hand out before one it could", func(t *testing.T) {
		m, io, firstFreed := sliceDWithoutRoom(9, 10)
		servable := sliceDServable()
		servable.maxResolution = resolution{1280, 720}
		if _, err := m.resolveVideoStream(t.Context(), sliceDRecording()); err != nil {
			t.Fatal(err)
		}
		io.setState(fixtureState().with(func(s *cameraState) { s.video = []allocatedVideoStream{servable} }))
		small := sliceDRecording()
		small.hints = &videoHints{videoRangeBounds: videoRangeBounds{maxResolution: resolution{640, 360}}}
		if _, err := m.resolveVideoStream(t.Context(), small); err != nil {
			t.Fatal(err)
		}
		io.setState(fixtureState().with(func(s *cameraState) { s.video = []allocatedVideoStream{servable, sliceDRecordingStream(10)} }))
		if _, err := m.resolveVideoStream(t.Context(), sliceDEvictionLive()); err != nil {
			t.Fatal(err)
		}
		if got := firstFreed(); got != "videoStreamDeallocate 10" {
			t.Fatalf("first freed = %q", got)
		}
	})
	t.Run("hands out its freed stream allocated again, degraded, when the request still fails", func(t *testing.T) {
		m, io, recordingAllocates := sliceDStaysFull(uint16(9), uint16(12))
		sliceDOwnServable(t, m, io)
		r, err := m.resolveVideoStream(t.Context(), sliceDLive(nil))
		if err != nil {
			t.Fatal(err)
		}
		if r.streamID != 12 || !r.degraded || r.provenance != ProvenanceAllocated || !slices.Equal(r.evicted, []uint16{9}) {
			t.Fatalf("= %+v", r)
		}
		if n := recordingAllocates(); n != 2 {
			t.Fatalf("recording allocates = %d", n)
		}
	})
	t.Run("keeps its freed stream allocated again when the session then fails", func(t *testing.T) {
		m, io, recordingAllocates := sliceDStaysFull(uint16(9), uint16(12))
		sliceDOwnServable(t, m, io)
		_, err := m.startStream(t.Context(), sliceDStart("", trackDeferred, nil, trackDeclined, nil))
		sliceDAs[*localError](t, err)
		deallocates := sliceDInvokesOf(io, cmdVideoStreamDeallocate)
		freed := make([]uint16, 0, len(deallocates))
		for _, r := range deallocates {
			freed = append(freed, r.fields.(csdef.VideoStreamDeallocateRequest).VideoStreamId)
		}
		if !slices.Equal(freed, []uint16{9}) {
			t.Fatalf("deallocated = %v, want [9]", freed)
		}
		if n := recordingAllocates(); n != 2 {
			t.Fatalf("recording allocates = %d", n)
		}
	})
	for _, c := range []struct {
		name   string
		answer any
	}{
		{"fails", statusErr(im.StatusResourceExhausted)},
		{"is answered with a stream the camera already held", uint16(9)},
	} {
		t.Run("fails with 103 when allocating its freed stream again "+c.name, func(t *testing.T) {
			m, io, recordingAllocates := sliceDStaysFull(uint16(9), c.answer)
			sliceDOwnServable(t, m, io)
			_, err := m.resolveVideoStream(t.Context(), sliceDLive(nil))
			sliceDAs[*ExhaustedError](t, err)
			if n := recordingAllocates(); n != 2 {
				t.Fatalf("recording allocates = %d", n)
			}
		})
	}
	t.Run("hands out its own stream degraded without freeing it when eviction is not allowed", func(t *testing.T) {
		m, io, _ := sliceDStaysFull(uint16(9))
		sliceDOwnServable(t, m, io)
		req := sliceDLive(nil)
		req.allowEviction = false
		r, err := m.resolveVideoStream(t.Context(), req)
		if err != nil {
			t.Fatal(err)
		}
		if r.streamID != 9 || !r.degraded || slices.Contains(io.commands(), cmdVideoStreamDeallocate) {
			t.Fatalf("= %+v, commands %v", r, io.commands())
		}
	})
}

// The stream-list-read case (CameraStreamManagerTest.ts:10146-10178,
// matterjs-server at 0bd06f2).
func TestSliceDStreamListReadAfterExistingStreamParityMatterjsServer(t *testing.T) {
	t.Run("continues without audio when the camera's stream list cannot be read after it answered with an existing stream", func(t *testing.T) {
		existing := allocatedAudioStream{audioStreamID: 4, streamUsage: liveView, channelCount: 2, sampleRate: 48000, bitRate: 64000, bitDepth: 16, referenceCount: 1}
		m, io, _ := sliceDManager(nil, func(*fakeIO, *recordedInvoke) (any, error) { return sliceDAudioAlloc(4) })
		reads := 0
		io.readHook = func() (*cameraState, error) {
			reads++
			if reads > 1 {
				return nil, errors.New("read failed")
			}
			return fixtureState().with(func(s *cameraState) { s.audio = []allocatedAudioStream{existing} }), nil
		}
		r, err := m.resolveAudioStream(t.Context(), audioRequest{t: tTarget, usage: csdef.StreamUsageRecording}, trackDeferred)
		if err != nil || r != nil {
			t.Fatalf("= %+v, %v; want no audio", r, err)
		}
	})
}

// The deallocate-answered-NOT_FOUND cases (CameraStreamManagerTest.ts:10180-10284,
// matterjs-server at 0bd06f2).
func TestSliceDDeallocateNotFoundWhileMakingRoomParityMatterjsServer(t *testing.T) {
	withGone := func(state *cameraState) (*manager, *sliceDIO, func() []StreamEvicted, func() bool) {
		gone := false
		m, io, _ := sliceDManager(state, func(_ *fakeIO, r *recordedInvoke) (any, error) {
			switch r.command {
			case cmdVideoStreamDeallocate, cmdSnapshotStreamDeallocate:
				gone = true
				return nil, statusErr(im.StatusNotFound)
			case cmdVideoStreamAllocate:
				if r.fields.(csdef.VideoStreamAllocateRequest).StreamUsage != liveView {
					return sliceDVideoAlloc(30)
				}
				if !gone {
					return nil, statusErr(im.StatusResourceExhausted)
				}
				return sliceDVideoAlloc(11)
			case cmdSnapshotStreamAllocate:
				return sliceDSnapshotAlloc(31)
			}
			return nil, nil
		})
		var evictions []StreamEvicted
		m.onEvicted = func(e StreamEvicted) { evictions = append(evictions, e) }
		retriedAsked := func() bool {
			invokes := io.recorded()
			freeing := slices.IndexFunc(invokes, func(r recordedInvoke) bool { return strings.HasSuffix(r.command, "StreamDeallocate") })
			first := slices.IndexFunc(invokes, func(r recordedInvoke) bool { return r.command == cmdVideoStreamAllocate })
			if freeing < 0 || first < 0 || freeing+1 >= len(invokes) {
				return false
			}
			retry := invokes[freeing+1]
			return retry.command == cmdVideoStreamAllocate && reflect.DeepEqual(retry.fields, invokes[first].fields)
		}
		return m, io, func() []StreamEvicted { return evictions }, retriedAsked
	}
	t.Run("retries the allocate after a snapshot stream turns out to be gone, and neither announces nor restores it", func(t *testing.T) {
		m, io, evictions, retriedAsked := withGone(fixtureState().with(func(s *cameraState) { s.snapshot = []allocatedSnapshotStream{sliceDSnapshot5(6)} }))
		r, err := m.resolveVideoStream(t.Context(), sliceDEvictionLive())
		if err != nil {
			t.Fatal(err)
		}
		if r.streamID != 11 || !retriedAsked() || r.evicted != nil || len(evictions()) != 0 {
			t.Fatalf("= %+v, retried %v, evictions %v", r, retriedAsked(), evictions())
		}
		if slices.Contains(io.commands(), cmdSnapshotStreamAllocate) {
			t.Fatalf("commands = %v", io.commands())
		}
	})
	t.Run("retries the allocate after a video stream turns out to be gone, and neither announces nor restores it", func(t *testing.T) {
		m, io, evictions, retriedAsked := withGone(fixtureState().with(func(s *cameraState) { s.video = []allocatedVideoStream{sliceDRecordingStream(21)} }))
		r, err := m.resolveVideoStream(t.Context(), sliceDEvictionLive())
		if err != nil {
			t.Fatal(err)
		}
		if r.streamID != 11 || !retriedAsked() || r.evicted != nil || len(evictions()) != 0 {
			t.Fatalf("= %+v, retried %v, evictions %v", r, retriedAsked(), evictions())
		}
		for _, f := range sliceDVideoAllocates(io) {
			if f.StreamUsage == csdef.StreamUsageRecording {
				t.Fatalf("restored: %+v", f)
			}
		}
	})
}

// The untracked session end (CameraStreamManagerTest.ts:10360-10369,
// matterjs-server at 0bd06f2).
func TestSliceDUntrackedSessionEndsParityMatterjsServer(t *testing.T) {
	t.Run("announces a session it ended for no connection as released, naming no requester", func(t *testing.T) {
		m, _, _ := sliceDManager(fixtureState(), nil)
		endings := endingsOf(m)
		ended, err := m.stopStream(t.Context(), tTarget, 77, "")
		if err != nil || !ended {
			t.Fatalf("= %v, %v; want ended", ended, err)
		}
		want := []cameraSessionEnded{{sessionEnd: sessionEnd{cause: endReleased}, nodeID: tNode, endpointID: tEndpoint, webRTCSessionID: 77}}
		if got := endings(); !slices.Equal(got, want) {
			t.Fatalf("endings = %+v, want %+v", got, want)
		}
	})
}

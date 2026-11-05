// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// The cases of the describe "sessions" (CameraStreamManagerTest.ts:2599-5600,
// matterjs-server at 0bd06f2), ported by meaning.

var sliceBCtx = context.Background()

// sliceBVideo is a request's `video: {}`: demanded, nothing stated.
func sliceBVideo() *videoHints { return &videoHints{overlays: overlayBoundsOf(nil, nil)} }

// sliceBArgs is a live-view start of conn-1 on the fixture camera; sdp ""
// solicits the offer.
func sliceBArgs(sdp string, edit func(*startArgs)) startArgs {
	a := startArgs{t: tTarget, connectionID: "conn-1", usage: liveView, allowEviction: true}
	if sdp != "" {
		a.sdp = &sdp
	}
	if edit != nil {
		edit(&a)
	}
	return a
}

// sliceBVideoOnly is the source's `video: {}, audio: false`.
func sliceBVideoOnly(a *startArgs) {
	a.videoState, a.video, a.audioState = trackDemanded, sliceBVideo(), trackDeclined
}

// sliceBStart is the source's start(): VIDEO_OFFER, video {}, audio false.
func sliceBStart(m *manager, conn string, node uint64, ep uint16) (*startResult, error) {
	return m.startStream(sliceBCtx, sliceBArgs(videoOffer, func(a *startArgs) {
		a.connectionID, a.t = conn, target{node, ep}
		sliceBVideoOnly(a)
	}))
}

// sliceBStartKeyed is the source's startKeyed.
func sliceBStartKeyed(ctx context.Context, m *manager, conn, key string) (*startResult, error) {
	return m.startStream(ctx, sliceBArgs(videoOffer, func(a *startArgs) {
		a.connectionID, a.clientSessionKey = conn, key
		sliceBVideoOnly(a)
	}))
}

func sliceBMustStart(t *testing.T, m *manager, conn string) *startResult {
	t.Helper()
	r, err := sliceBStart(m, conn, tNode, tEndpoint)
	if err != nil {
		t.Fatalf("start %s: %v", conn, err)
	}
	return r
}

// sliceBAnswers answers allocations and offers with fixed ids; 0 answers
// nothing.
type sliceBAnswers struct{ video, audio, offer, solicit uint16 }

func (a sliceBAnswers) respond(r *recordedInvoke) (any, error) {
	switch {
	case r.command == cmdVideoStreamAllocate && a.video != 0:
		return csdef.VideoStreamAllocateResponse{VideoStreamId: a.video}, nil
	case r.command == cmdAudioStreamAllocate && a.audio != 0:
		return csdef.AudioStreamAllocateResponse{AudioStreamId: a.audio}, nil
	case r.command == callProvideOffer && a.offer != 0:
		return wpdef.ProvideOfferResponse{WebRtcSessionId: a.offer}, nil
	case r.command == callSolicitOffer && a.solicit != 0:
		return wpdef.SolicitOfferResponse{WebRtcSessionId: a.solicit}, nil
	}
	return nil, nil
}

// sliceBAllocating is the source's allocatingManager.
func sliceBAllocating() (*manager, *fakeIO) {
	m, io, _ := managerWith(fixtureState(), sliceBAnswers{video: 9, offer: 42}.respond)
	return m, io
}

// sliceBTracked is a manager whose conn-1 holds tracked session 42.
func sliceBTracked(t *testing.T) (*manager, *fakeIO) {
	t.Helper()
	m, io := sliceBAllocating()
	sliceBMustStart(t, m, "conn-1")
	return m, io
}

// sliceBNumbering is the source's sessionNumbering: video 9, sessions from
// 42 on.
func sliceBNumbering() func(*recordedInvoke) (any, error) {
	var mu sync.Mutex
	next := uint16(42)
	return func(r *recordedInvoke) (any, error) {
		switch r.command {
		case cmdVideoStreamAllocate:
			return csdef.VideoStreamAllocateResponse{VideoStreamId: 9}, nil
		case callProvideOffer:
			mu.Lock()
			defer mu.Unlock()
			id := next
			next++
			return wpdef.ProvideOfferResponse{WebRtcSessionId: id}, nil
		}
		return nil, nil
	}
}

// sliceBOffer is the ProvideOffer the camera saw.
func sliceBOffer(t *testing.T, io *fakeIO) wpdef.ProvideOfferRequest {
	t.Helper()
	for _, r := range io.recorded() {
		if f, ok := r.fields.(wpdef.ProvideOfferRequest); ok && r.command == callProvideOffer {
			return f
		}
	}
	t.Fatal("no provideOffer was sent")
	return wpdef.ProvideOfferRequest{}
}

// sliceBSentSdp is the sdp of the establishing offer, nil for a
// SolicitOffer.
func sliceBSentSdp(io *fakeIO) *string {
	for _, r := range io.recorded() {
		if f, ok := r.fields.(wpdef.ProvideOfferRequest); ok {
			return &f.Sdp
		}
	}
	return nil
}

func sliceBCount(io *fakeIO, command string) int {
	n := 0
	for _, c := range io.commands() {
		if c == command {
			n++
		}
	}
	return n
}

// sliceBDeallocatedVideo is the video stream ids deallocated, in order.
func sliceBDeallocatedVideo(io *fakeIO) []uint16 {
	var ids []uint16
	for _, r := range io.recorded() {
		if f, ok := r.fields.(csdef.VideoStreamDeallocateRequest); ok {
			ids = append(ids, f.VideoStreamId)
		}
	}
	return ids
}

type sliceBEnd struct {
	node uint64
	id   uint16
}

// sliceBEndedSessions is the source's endedSessions.
func sliceBEndedSessions(io *fakeIO) []sliceBEnd {
	out := []sliceBEnd{}
	for _, r := range io.recorded() {
		if f, ok := r.fields.(wpdef.EndSessionRequest); ok {
			out = append(out, sliceBEnd{r.t.nodeID, f.WebRtcSessionId})
		}
	}
	return out
}

func sliceBEnding(id uint16, owner string, end sessionEnd) cameraSessionEnded {
	return cameraSessionEnded{sessionEnd: end, nodeID: tNode, endpointID: tEndpoint, webRTCSessionID: id, ownerID: owner}
}

func sliceBSameEndings(t *testing.T, got, want []cameraSessionEnded) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("endings = %+v, want %+v", got, want)
	}
}

// sliceBCauseBy is the source's `${cause} ${requestedBy}`.
func sliceBCauseBy(e cameraSessionEnded) string {
	by := e.requestedBy
	if by == "" {
		by = "undefined"
	}
	return string(e.cause) + " " + by
}

func sliceBCausesBy(es []cameraSessionEnded) []string {
	out := make([]string, 0, len(es))
	for _, e := range es {
		out = append(out, sliceBCauseBy(e))
	}
	return out
}

func sliceBSameStrings(t *testing.T, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func sliceBReport(m *manager, restarted bool, ids ...uint16) {
	sliceBReportOn(m, tNode, tEndpoint, restarted, ids...)
}

func sliceBReportOn(m *manager, node uint64, ep uint16, restarted bool, ids ...uint16) {
	present := map[uint16]bool{}
	for _, id := range ids {
		present[id] = true
	}
	cause := endCameraDropped
	if restarted {
		cause = endCameraRestarted
	}
	m.sessions.reconcile(node, ep, present, cause)
}

func sliceBOwner(m *manager, id uint16) (string, bool) {
	o, ok := m.sessions.signallingOwner(tNode, tEndpoint, id)
	return o.connectionID, ok
}

func sliceBNoOwner(t *testing.T, m *manager, id uint16) {
	t.Helper()
	if o, ok := m.sessions.signallingOwner(tNode, tEndpoint, id); ok {
		t.Fatalf("signallingOwner(%d) = %+v, want none", id, o)
	}
}

func sliceBIncompatible(t *testing.T, err error) *IncompatibleError {
	t.Helper()
	var inc *IncompatibleError
	if !errors.As(err, &inc) {
		t.Fatalf("err = %v, want *IncompatibleError", err)
	}
	return inc
}

func sliceBLocal(t *testing.T, err error) *StackError {
	t.Helper()
	var le *StackError
	if !errors.As(err, &le) {
		t.Fatalf("err = %v, want *StackError (SDKStackError)", err)
	}
	return le
}

func sliceBStatus(t *testing.T, err error, want im.StatusCode) {
	t.Helper()
	if s, ok := deviceStatusOf(err); !ok || s != want {
		t.Fatalf("err = %v, want device status %v", err, want)
	}
}

// sliceBLog records what the manager logs.
type sliceBLog struct {
	mu   sync.Mutex
	recs []slog.Record
}

func (*sliceBLog) Enabled(context.Context, slog.Level) bool { return true }

func (h *sliceBLog) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	h.recs = append(h.recs, r.Clone())
	h.mu.Unlock()
	return nil
}

func (h *sliceBLog) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h *sliceBLog) WithGroup(string) slog.Handler { return h }

// has reports a record of msg whose attrs satisfy match (nil: any).
func (h *sliceBLog) has(msg string, match func(map[string]slog.Value) bool) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := range h.recs {
		r := &h.recs[i]
		if r.Message != msg {
			continue
		}
		attrs := map[string]slog.Value{}
		r.Attrs(func(a slog.Attr) bool { attrs[a.Key] = a.Value; return true })
		if match == nil || match(attrs) {
			return true
		}
	}
	return false
}

func sliceBLogged(m *manager) *sliceBLog {
	h := &sliceBLog{}
	m.log = slog.New(h)
	return h
}

// sliceBProbe is a context that tells when the call under test first waits
// on it: past every synchronous step before that wait.
type sliceBProbe struct {
	context.Context
	once    sync.Once
	reached chan struct{}
}

func sliceBNewProbe() *sliceBProbe {
	return &sliceBProbe{Context: sliceBCtx, reached: make(chan struct{})}
}

func (p *sliceBProbe) Done() <-chan struct{} {
	p.once.Do(func() { close(p.reached) })
	return p.Context.Done()
}

func sliceBAwait(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	select {
	case <-ch:
	case <-ctx.Done():
		t.Fatalf("timed out waiting for %s", what)
	}
}

// sliceBEventually waits until cond holds, yielding to the goroutines under
// test.
func sliceBEventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		runtime.Gosched()
	}
}

// sliceBClaimed is the number of registrations a release or a stop by key
// has claimed.
func sliceBClaimed(m *manager) int {
	m.sessions.mu.Lock()
	defer m.sessions.mu.Unlock()
	n := 0
	for _, r := range m.sessions.pending {
		if r.claimed != unclaimed {
			n++
		}
	}
	return n
}

// sliceBJoined is the number of goroutines waiting on a shared EndSession
// outcome; a test counts the ones it adds, so a goroutine another test
// left behind does not count.
func sliceBJoined() int {
	buf := make([]byte, 1<<22)
	n := runtime.Stack(buf, true)
	return strings.Count(string(buf[:n]), "camera.(*outcome[...]).wait(")
}

// sliceBAwaitJoined waits until one more goroutine than before waits on a
// shared EndSession outcome.
func sliceBAwaitJoined(t *testing.T, what string, before int) {
	t.Helper()
	sliceBEventually(t, what, func() bool { return sliceBJoined() > before })
}

type sliceBResult[T any] struct {
	v   T
	err error
}

// sliceBGo runs f in its own goroutine; the channel carries its result.
func sliceBGo[T any](f func() (T, error)) <-chan sliceBResult[T] {
	ch := make(chan sliceBResult[T], 1)
	go func() {
		v, err := f()
		ch <- sliceBResult[T]{v, err}
	}()
	return ch
}

func sliceBRecv[T any](t *testing.T, ch <-chan sliceBResult[T], what string) sliceBResult[T] {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	select {
	case r := <-ch:
		return r
	case <-ctx.Done():
		t.Fatalf("timed out waiting for %s", what)
		return sliceBResult[T]{}
	}
}

// sliceBSignal is a channel closed once.
type sliceBSignal struct {
	once sync.Once
	ch   chan struct{}
}

func sliceBNewSignal() *sliceBSignal { return &sliceBSignal{ch: make(chan struct{})} }

func (s *sliceBSignal) fire() { s.once.Do(func() { close(s.ch) }) }

// sliceBCtxIO is the camera honouring the call's context, as the real
// client does: a cleanup budget that runs out ends the wait.
type sliceBCtxIO struct{ *fakeIO }

func (w sliceBCtxIO) invoke(ctx context.Context, call deviceCall) (any, error) {
	ch := sliceBGo(func() (any, error) { return w.fakeIO.invoke(ctx, call) })
	select {
	case r := <-ch:
		return r.v, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// sliceBCountingIO counts the reads of the camera's state, as the source's
// holder getter does.
type sliceBCountingIO struct {
	*fakeIO
	reads *atomic.Int32
}

func (w sliceBCountingIO) readCameraState(ctx context.Context, t target) (*cameraState, error) {
	w.reads.Add(1)
	return w.fakeIO.readCameraState(ctx, t)
}

func (w sliceBCountingIO) missingCameraClusters(ctx context.Context, t target) []uint32 {
	w.reads.Add(1)
	return w.fakeIO.missingCameraClusters(ctx, t)
}

// sliceBContained is the source's CONTAINED_STREAM: 1920x1080 only.
func sliceBContained(id uint16, refs int) allocatedVideoStream {
	s := liveStream(id, refs)
	s.minResolution = resolution{1920, 1080}
	return s
}

func sliceBTalk(v csdef.TwoWayTalkSupportTypeEnum) *csdef.TwoWayTalkSupportTypeEnum { return &v }

// sliceBRoundTrip encodes an offer command as the provider path sends it
// (with its OriginatingEndpointID) and decodes it as a camera would.
func sliceBRoundTrip(fields spec.Encodable) error {
	origin := uint16(1)
	var decode func(spec.Node) error
	switch f := fields.(type) {
	case wpdef.ProvideOfferRequest:
		f.OriginatingEndpointId = &origin
		fields = f
		decode = func(n spec.Node) error { var v wpdef.ProvideOfferRequest; return v.DecodeTLV(n) }
	case wpdef.SolicitOfferRequest:
		f.OriginatingEndpointId = origin
		fields = f
		decode = func(n spec.Node) error { var v wpdef.SolicitOfferRequest; return v.DecodeTLV(n) }
	default:
		return fmt.Errorf("not an offer command: %T", fields)
	}
	enc := tlv.NewEncoder()
	fields.EncodeTLV(enc, tlv.AnonymousTag())
	b, err := enc.Bytes()
	if err != nil {
		return err
	}
	n, err := spec.ReadElement(tlv.NewDecoder(b))
	if err != nil {
		return err
	}
	return decode(n)
}

// The tracks an offer leaves a session, the offer commands and the
// signalling owner (CameraStreamManagerTest.ts:2629-3089, matterjs-server
// at 0bd06f2).
func TestSliceBSessionsTracksParityMatterjsServer(t *testing.T) {
	audioAndOffer := sliceBAnswers{audio: 4, offer: 42}.respond

	t.Run("puts no video track in a session whose offer rejects the video section", func(t *testing.T) {
		m, io, _ := managerWith(fixtureState(), audioAndOffer)
		res, err := m.startStream(sliceBCtx, sliceBArgs(videoRefusedOffer, nil))
		if err != nil {
			t.Fatal(err)
		}
		if res.video != nil || sliceBCount(io, cmdVideoStreamAllocate) != 0 {
			t.Fatalf("video = %+v, allocates %d; want none", res.video, sliceBCount(io, cmdVideoStreamAllocate))
		}
		offer := sliceBOffer(t, io)
		if offer.VideoStreams != nil || offer.AudioStreams == nil || !slices.Equal(*offer.AudioStreams, []uint16{4}) {
			t.Fatalf("offer streams = %v / %v, want none / [4]", offer.VideoStreams, offer.AudioStreams)
		}
	})

	t.Run("asks for a new session with a null WebRTCSessionID on ProvideOffer", func(t *testing.T) {
		m, io := sliceBAllocating()
		if _, err := m.startStream(sliceBCtx, sliceBArgs(videoOffer, func(a *startArgs) { a.audioState = trackDeclined })); err != nil {
			t.Fatal(err)
		}
		if !sliceBOffer(t, io).WebRtcSessionId.Null {
			t.Fatal("WebRTCSessionID is not null")
		}
	})

	t.Run("builds offer commands matter.js accepts", func(t *testing.T) {
		for _, sdp := range []string{videoOffer, ""} {
			m, io, _ := managerWith(fixtureState(), sliceBAnswers{video: 9, offer: 42, solicit: 42}.respond)
			if _, err := m.startStream(sliceBCtx, sliceBArgs(sdp, func(a *startArgs) { a.audioState = trackDeclined })); err != nil {
				t.Fatal(err)
			}
			var offer spec.Encodable
			for _, r := range io.recorded() {
				if r.command == callProvideOffer || r.command == callSolicitOffer {
					offer = r.fields
				}
			}
			if offer == nil {
				t.Fatal("no offer was sent")
			}
			if err := sliceBRoundTrip(offer); err != nil {
				t.Fatalf("%T does not round-trip: %v", offer, err)
			}
		}
	})

	t.Run("tells a caller that asked for video why a rejected video section left it none", func(t *testing.T) {
		m, io, _ := managerWith(fixtureState(), audioAndOffer)
		_, err := m.startStream(sliceBCtx, sliceBArgs(videoRefusedOffer, func(a *startArgs) {
			a.videoState, a.videoCodecs = trackDemanded, []string{"H265"}
			a.video = sliceBVideo()
			a.video.minResolution = resolution{1280, 720}
		}))
		inc := sliceBIncompatible(t, err)
		if inc.Reason != ReasonOffer || inc.Track != TrackVideo || !slices.Equal(inc.Requested, []string{"H265"}) {
			t.Fatalf("err = %+v", inc)
		}
		if sliceBCount(io, callProvideOffer) != 0 {
			t.Fatal("an offer was sent")
		}
	})

	t.Run("puts no audio track in a session whose offer rejects the audio section", func(t *testing.T) {
		m, io := sliceBAllocating()
		res, err := m.startStream(sliceBCtx, sliceBArgs(audioRefusedOffer, func(a *startArgs) { a.videoState, a.video = trackDemanded, sliceBVideo() }))
		if err != nil {
			t.Fatal(err)
		}
		if res.audio != nil || sliceBCount(io, cmdAudioStreamAllocate) != 0 {
			t.Fatalf("audio = %+v, want none allocated", res.audio)
		}
	})

	t.Run("tells a caller that asked for audio why a rejected audio section left it none", func(t *testing.T) {
		m, _ := sliceBAllocating()
		_, err := m.startStream(sliceBCtx, sliceBArgs(audioRefusedOffer, func(a *startArgs) {
			a.videoState, a.video = trackDemanded, sliceBVideo()
			a.audioState, a.audio = trackDemanded, &audioHints{codecs: []string{"OPUS"}}
		}))
		inc := sliceBIncompatible(t, err)
		if inc.Reason != ReasonOffer || len(inc.Device) != 0 || inc.Device == nil || inc.Track != TrackAudio {
			t.Fatalf("err = %+v", inc)
		}
	})

	t.Run("names the track when the audio value the caller stated is not a codec list", func(t *testing.T) {
		m, _ := sliceBAllocating()
		_, err := m.startStream(sliceBCtx, sliceBArgs(audioRefusedOffer, func(a *startArgs) {
			a.videoState, a.video = trackDemanded, sliceBVideo()
			a.audioState, a.audio = trackDemanded, &audioHints{channelCount: 2}
		}))
		inc := sliceBIncompatible(t, err)
		if inc.Reason != ReasonOffer || inc.Requested == nil || len(inc.Requested) != 0 || inc.Track != TrackAudio {
			t.Fatalf("err = %+v", inc)
		}
	})

	t.Run("puts no video track in a session whose offer will not receive video", func(t *testing.T) {
		m, io, _ := managerWith(fixtureState(), audioAndOffer)
		res, err := m.startStream(sliceBCtx, sliceBArgs(videoInactiveOffer, nil))
		if err != nil {
			t.Fatal(err)
		}
		if res.video != nil || sliceBCount(io, cmdVideoStreamAllocate) != 0 || sliceBOffer(t, io).VideoStreams != nil {
			t.Fatal("a video stream was allocated or offered")
		}
	})

	t.Run("tells a caller that asked for video why a section it will not receive left it none", func(t *testing.T) {
		m, io, _ := managerWith(fixtureState(), audioAndOffer)
		_, err := m.startStream(sliceBCtx, sliceBArgs(videoInactiveOffer, func(a *startArgs) {
			a.videoState, a.video, a.videoCodecs = trackDemanded, sliceBVideo(), []string{"H265"}
		}))
		inc := sliceBIncompatible(t, err)
		if inc.Reason != ReasonOffer || inc.Track != TrackVideo || !slices.Equal(inc.Requested, []string{"H265"}) || sliceBCount(io, callProvideOffer) != 0 {
			t.Fatalf("err = %+v", inc)
		}
	})

	t.Run("puts no audio track in a session whose audio section only asks to send", func(t *testing.T) {
		m, io := sliceBAllocating()
		res, err := m.startStream(sliceBCtx, sliceBArgs(audioSendonlyOffer, func(a *startArgs) { a.videoState, a.video = trackDemanded, sliceBVideo() }))
		if err != nil {
			t.Fatal(err)
		}
		if res.audio != nil || sliceBCount(io, cmdAudioStreamAllocate) != 0 || sliceBOffer(t, io).AudioStreams != nil {
			t.Fatal("an audio stream was allocated or offered")
		}
	})

	t.Run("tells a caller that asked for audio why a sendonly audio section left it none", func(t *testing.T) {
		m, _ := sliceBAllocating()
		_, err := m.startStream(sliceBCtx, sliceBArgs(audioSendonlyOffer, func(a *startArgs) {
			a.videoState, a.video = trackDemanded, sliceBVideo()
			a.audioState, a.audio = trackDemanded, &audioHints{codecs: []string{"OPUS"}}
		}))
		if inc := sliceBIncompatible(t, err); inc.Reason != ReasonOffer || inc.Track != TrackAudio {
			t.Fatalf("err = %+v", inc)
		}
	})

	t.Run("puts no video track in a session whose offer carries no video section", func(t *testing.T) {
		m, io, _ := managerWith(fixtureState(), audioAndOffer)
		res, err := m.startStream(sliceBCtx, sliceBArgs(audioOnlyOffer, nil))
		if err != nil {
			t.Fatal(err)
		}
		offer := sliceBOffer(t, io)
		if res.video != nil || sliceBCount(io, cmdVideoStreamAllocate) != 0 || offer.VideoStreams != nil || offer.AudioStreams == nil || !slices.Equal(*offer.AudioStreams, []uint16{4}) {
			t.Fatalf("video = %+v, offer = %+v", res.video, offer)
		}
	})

	t.Run("names the missing section in the log when a deferred track is left out for it", func(t *testing.T) {
		m, _, _ := managerWith(fixtureState(), audioAndOffer)
		logs := sliceBLogged(m)
		if _, err := m.startStream(sliceBCtx, sliceBArgs(audioOnlyOffer, nil)); err != nil {
			t.Fatal(err)
		}
		// The Go log is structured: the left-out track and the offer's refusal.
		if !logs.has("camera.track.unavailable", func(a map[string]slog.Value) bool {
			var inc *IncompatibleError
			err, _ := a["err"].Any().(error)
			return a["track"].String() == "video" && errors.As(err, &inc) && inc.Reason == ReasonOffer
		}) {
			t.Fatal("no log names the video track the offer left out")
		}
	})

	t.Run("tells a caller that asked for video why an offer with no video section left it none", func(t *testing.T) {
		m, io, _ := managerWith(fixtureState(), audioAndOffer)
		_, err := m.startStream(sliceBCtx, sliceBArgs(audioOnlyOffer, func(a *startArgs) {
			a.videoState, a.video, a.videoCodecs = trackDemanded, sliceBVideo(), []string{"H265"}
		}))
		inc := sliceBIncompatible(t, err)
		if inc.Reason != ReasonOffer || inc.Track != TrackVideo || !slices.Equal(inc.Requested, []string{"H265"}) || sliceBCount(io, callProvideOffer) != 0 {
			t.Fatalf("err = %+v", inc)
		}
	})

	t.Run("puts no audio track in a session whose offer carries no audio section", func(t *testing.T) {
		m, io := sliceBAllocating()
		res, err := m.startStream(sliceBCtx, sliceBArgs(videoOffer, func(a *startArgs) { a.videoState, a.video = trackDemanded, sliceBVideo() }))
		if err != nil {
			t.Fatal(err)
		}
		if res.audio != nil || sliceBCount(io, cmdAudioStreamAllocate) != 0 {
			t.Fatal("an audio stream was allocated")
		}
	})

	t.Run("tells a caller that asked for audio why an offer with no audio section left it none", func(t *testing.T) {
		m, _ := sliceBAllocating()
		_, err := m.startStream(sliceBCtx, sliceBArgs(videoOffer, func(a *startArgs) {
			a.videoState, a.video = trackDemanded, sliceBVideo()
			a.audioState, a.audio = trackDemanded, &audioHints{codecs: []string{"OPUS"}}
		}))
		if inc := sliceBIncompatible(t, err); inc.Reason != ReasonOffer || inc.Track != TrackAudio {
			t.Fatalf("err = %+v", inc)
		}
	})

	t.Run("allocates both tracks when there is no offer to answer", func(t *testing.T) {
		m, io, _ := managerWith(fixtureState(), sliceBAnswers{video: 9, audio: 4, solicit: 42}.respond)
		res, err := m.startStream(sliceBCtx, sliceBArgs("", func(a *startArgs) {
			a.videoState, a.video, a.videoCodecs = trackDemanded, sliceBVideo(), []string{"H265"}
			a.audioState, a.audio = trackDemanded, &audioHints{codecs: []string{"OPUS"}}
		}))
		if err != nil {
			t.Fatal(err)
		}
		if res.mode != ModeSolicitOffer || res.video == nil || res.video.streamID != 9 || res.audio == nil || res.audio.streamID != 4 || sliceBCount(io, callSolicitOffer) != 1 {
			t.Fatalf("result = %+v", res)
		}
	})

	t.Run("names the establishing connection as the signalling owner of a tracked session", func(t *testing.T) {
		m, _ := sliceBAllocating()
		res, err := m.startStream(sliceBCtx, sliceBArgs(videoOffer, sliceBVideoOnly))
		if err != nil {
			t.Fatal(err)
		}
		if owner, ok := sliceBOwner(m, res.sessionID); !ok || owner != "conn-1" {
			t.Fatalf("owner = %q, %v", owner, ok)
		}
		sliceBNoOwner(t, m, res.sessionID+1)
	})

	t.Run("names no owner while a session is being established on that camera", func(t *testing.T) {
		var m *manager
		var whileEstablishing bool
		m, _, _ = managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
			if r.command == callSolicitOffer {
				_, whileEstablishing = sliceBOwner(m, 42)
				return wpdef.SolicitOfferResponse{WebRtcSessionId: 42}, nil
			}
			return sliceBAnswers{video: 9}.respond(r)
		})
		res, err := m.startStream(sliceBCtx, sliceBArgs("", func(a *startArgs) { a.connectionID = "conn-7"; sliceBVideoOnly(a) }))
		if err != nil {
			t.Fatal(err)
		}
		if whileEstablishing {
			t.Fatal("an owner was named before the camera's answer")
		}
		if owner, _ := sliceBOwner(m, res.sessionID); owner != "conn-7" {
			t.Fatalf("owner = %q, want conn-7", owner)
		}
	})

	t.Run("names the establishing connection once the camera's answer gave the session id", func(t *testing.T) {
		var m *manager
		var onceKnown string
		m, _, _ = managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
			if r.command == callSolicitOffer {
				r.establishing(42)
				onceKnown, _ = sliceBOwner(m, 42)
				return wpdef.SolicitOfferResponse{WebRtcSessionId: 42}, nil
			}
			return sliceBAnswers{video: 9}.respond(r)
		})
		if _, err := m.startStream(sliceBCtx, sliceBArgs("", func(a *startArgs) { a.connectionID = "conn-8"; sliceBVideoOnly(a) })); err != nil {
			t.Fatal(err)
		}
		if onceKnown != "conn-8" {
			t.Fatalf("owner once the id was known = %q, want conn-8", onceKnown)
		}
		sliceBNoOwner(t, m, 43)
	})

	t.Run("references the resolved stream ids in the provider offer", func(t *testing.T) {
		m, io := sliceBAllocating()
		res, err := m.startStream(sliceBCtx, sliceBArgs(videoOffer, sliceBVideoOnly))
		if err != nil {
			t.Fatal(err)
		}
		offer := sliceBOffer(t, io)
		if res.sessionID != 42 || offer.VideoStreams == nil || !slices.Equal(*offer.VideoStreams, []uint16{9}) {
			t.Fatalf("session %d, video streams %v", res.sessionID, offer.VideoStreams)
		}
	})
}

// sliceBTalkbackLogged starts a session on a camera stating talk, edit
// shaping the request, and returns what was logged.
func sliceBTalkbackLogged(t *testing.T, talk *csdef.TwoWayTalkSupportTypeEnum, answers sliceBAnswers, sdp string, edit func(*startArgs)) *sliceBLog {
	t.Helper()
	m, _, _ := managerWith(fixtureState().with(func(s *cameraState) { s.twoWayTalkSupport = talk }), answers.respond)
	logs := sliceBLogged(m)
	if _, err := m.startStream(sliceBCtx, sliceBArgs(sdp, edit)); err != nil {
		t.Fatal(err)
	}
	return logs
}

// The talkback notice of an offer the camera cannot talk back to
// (CameraStreamManagerTest.ts:3110-3248, matterjs-server at 0bd06f2). The
// source's "TwoWayTalkSupport" line is the camera.talkback.unsupported
// record.
func TestSliceBSessionsTalkbackNoticeParityMatterjsServer(t *testing.T) {
	notSupported := sliceBTalk(csdef.TwoWayTalkSupportTypeNotSupported)
	videoAndOffer := sliceBAnswers{video: 9, offer: 42}
	talkVideoOnly := func(a *startArgs) { a.talkback = true; sliceBVideoOnly(a) }
	const unsupported = "camera.talkback.unsupported"

	t.Run("reports an offer asking for talkback on a camera that does not support it", func(t *testing.T) {
		if !sliceBTalkbackLogged(t, notSupported, videoAndOffer, talkbackOffer, talkVideoOnly).has(unsupported, nil) {
			t.Fatal("no talkback notice")
		}
	})

	t.Run("reports an offer asking for talkback on a camera stating an unknown TwoWayTalkSupport", func(t *testing.T) {
		if !sliceBTalkbackLogged(t, sliceBTalk(3), videoAndOffer, talkbackOffer, talkVideoOnly).has(unsupported, nil) {
			t.Fatal("no talkback notice")
		}
	})

	t.Run("reports the talkback mismatch on a session that does negotiate audio", func(t *testing.T) {
		logs := sliceBTalkbackLogged(t, notSupported, sliceBAnswers{video: 9, audio: 4, offer: 42}, talkbackOffer, func(a *startArgs) {
			a.talkback, a.videoState, a.video = true, trackDemanded, sliceBVideo()
		})
		if !logs.has(unsupported, nil) {
			t.Fatal("no talkback notice")
		}
	})

	t.Run("says nothing about talkback when the offer does not ask for it", func(t *testing.T) {
		if sliceBTalkbackLogged(t, notSupported, videoAndOffer, videoOffer, sliceBVideoOnly).has(unsupported, nil) {
			t.Fatal("a talkback notice was logged")
		}
	})

	t.Run("says nothing about talkback when the camera supports one direction at a time", func(t *testing.T) {
		if sliceBTalkbackLogged(t, sliceBTalk(csdef.TwoWayTalkSupportTypeHalfDuplex), videoAndOffer, talkbackOffer, talkVideoOnly).has(unsupported, nil) {
			t.Fatal("a talkback notice was logged")
		}
	})

	t.Run("says nothing about talkback when the camera supports it", func(t *testing.T) {
		if sliceBTalkbackLogged(t, sliceBTalk(csdef.TwoWayTalkSupportTypeFullDuplex), videoAndOffer, talkbackOffer, talkVideoOnly).has(unsupported, nil) {
			t.Fatal("a talkback notice was logged")
		}
	})

	t.Run("says nothing about talkback when the offer can send but talkback is not asked for", func(t *testing.T) {
		if sliceBTalkbackLogged(t, notSupported, videoAndOffer, talkbackOffer, sliceBVideoOnly).has(unsupported, nil) {
			t.Fatal("a talkback notice was logged")
		}
	})
}

// sliceBTalkStart is the talkback describe's start(): video {}, audio {}
// unless declined, on a camera stating talk.
func sliceBTalkStart(t *testing.T, talk *csdef.TwoWayTalkSupportTypeEnum, sdp string, talkback, declineAudio bool) (*startResult, *string, *sliceBLog) {
	t.Helper()
	m, io, _ := managerWith(fixtureState().with(func(s *cameraState) { s.twoWayTalkSupport = talk }),
		sliceBAnswers{video: 9, audio: 4, offer: 42, solicit: 43}.respond)
	logs := sliceBLogged(m)
	res, err := m.startStream(sliceBCtx, sliceBArgs(sdp, func(a *startArgs) {
		a.talkback, a.videoState, a.video = talkback, trackDemanded, sliceBVideo()
		a.audioState, a.audio = trackDemanded, &audioHints{}
		if declineAudio {
			a.audioState, a.audio = trackDeclined, nil
		}
	}))
	if err != nil {
		t.Fatal(err)
	}
	return res, sliceBSentSdp(io), logs
}

func sliceBSent(t *testing.T, sent *string, want string) {
	t.Helper()
	if sent == nil || *sent != want {
		t.Fatalf("sent sdp = %v, want %q", sent, want)
	}
}

// The talkback describe (CameraStreamManagerTest.ts:3250-3413,
// matterjs-server at 0bd06f2).
func TestSliceBSessionsTalkbackParityMatterjsServer(t *testing.T) {
	full := sliceBTalk(csdef.TwoWayTalkSupportTypeFullDuplex)

	t.Run("sends the offer with its sending audio section made recvonly when talkback is not asked for", func(t *testing.T) {
		res, sent, _ := sliceBTalkStart(t, full, talkbackOffer, false, false)
		sliceBSent(t, sent, strings.Replace(talkbackOffer, "a=sendrecv", "a=recvonly", 1))
		if res.talkback {
			t.Fatal("talkback reported")
		}
	})

	t.Run("reports no talkback when talkback is false, even for a camera and offer that could talk", func(t *testing.T) {
		res, sent, _ := sliceBTalkStart(t, full, talkbackOffer, false, false)
		sliceBSent(t, sent, strings.Replace(talkbackOffer, "a=sendrecv", "a=recvonly", 1))
		if res.audio == nil || res.audio.streamID != 4 || res.talkback {
			t.Fatalf("result = %+v", res)
		}
	})

	t.Run("sends the offer with a sendonly audio section made inactive when talkback is false", func(t *testing.T) {
		_, sent, _ := sliceBTalkStart(t, nil, audioSendonlyOffer, false, true)
		sliceBSent(t, sent, strings.Replace(audioSendonlyOffer, "a=sendonly", "a=inactive", 1))
	})

	t.Run("forwards the offer unchanged when talkback is asked for", func(t *testing.T) {
		res, sent, _ := sliceBTalkStart(t, full, talkbackOffer, true, false)
		sliceBSent(t, sent, talkbackOffer)
		if !res.talkback {
			t.Fatal("no talkback reported")
		}
	})

	t.Run("forwards an offer whose audio cannot send unchanged", func(t *testing.T) {
		_, sent, _ := sliceBTalkStart(t, nil, videoAndAudioOffer, false, false)
		sliceBSent(t, sent, videoAndAudioOffer)
	})

	for _, c := range []struct {
		support csdef.TwoWayTalkSupportTypeEnum
		name    string
	}{{csdef.TwoWayTalkSupportTypeFullDuplex, "FullDuplex"}, {csdef.TwoWayTalkSupportTypeHalfDuplex, "HalfDuplex"}} {
		t.Run(fmt.Sprintf("reports talkback for a %s camera asked with an offer that can send", c.name), func(t *testing.T) {
			if res, _, _ := sliceBTalkStart(t, sliceBTalk(c.support), talkbackOffer, true, false); !res.talkback {
				t.Fatal("no talkback reported")
			}
		})
	}

	for _, c := range []struct {
		support *csdef.TwoWayTalkSupportTypeEnum
		name    string
	}{{sliceBTalk(csdef.TwoWayTalkSupportTypeNotSupported), "NotSupported"}, {nil, "no TwoWayTalkSupport"}, {sliceBTalk(3), "an unknown TwoWayTalkSupport"}} {
		t.Run("reports no talkback and still opens the session for a camera stating "+c.name, func(t *testing.T) {
			res, sent, _ := sliceBTalkStart(t, c.support, talkbackOffer, true, false)
			sliceBSent(t, sent, talkbackOffer)
			if res.sessionID != 42 || res.talkback {
				t.Fatalf("result = %+v", res)
			}
		})
	}

	t.Run("reports talkback beside the audio stream the session carries", func(t *testing.T) {
		m, io, _ := managerWith(fixtureState().with(func(s *cameraState) { s.twoWayTalkSupport = full }), sliceBAnswers{video: 9, audio: 4, offer: 42}.respond)
		res, err := m.startStream(sliceBCtx, sliceBArgs(talkbackOffer, func(a *startArgs) {
			a.talkback, a.videoState, a.video, a.audioState, a.audio = true, trackDemanded, sliceBVideo(), trackDemanded, &audioHints{}
		}))
		if err != nil {
			t.Fatal(err)
		}
		offer := sliceBOffer(t, io)
		if res.audio == nil || res.audio.streamID != 4 || !res.talkback || offer.Sdp != talkbackOffer || offer.AudioStreams == nil || !slices.Equal(*offer.AudioStreams, []uint16{4}) {
			t.Fatalf("result = %+v, offer = %+v", res, offer)
		}
	})

	t.Run("reports and logs no talkback when the session carries no audio stream", func(t *testing.T) {
		res, sent, logs := sliceBTalkStart(t, full, talkbackOffer, true, true)
		sliceBSent(t, sent, talkbackOffer)
		if res.audio != nil || res.talkback {
			t.Fatalf("result = %+v", res)
		}
		if !logs.has("camera.talkback.no_audio", nil) {
			t.Fatal("no log of the missing audio stream")
		}
	})

	t.Run("does not log a missing audio stream when talkback is not asked for", func(t *testing.T) {
		if _, _, logs := sliceBTalkStart(t, full, talkbackOffer, false, true); logs.has("camera.talkback.no_audio", nil) {
			t.Fatal("a missing audio stream was logged")
		}
	})

	t.Run("reports no talkback when the offer's audio cannot send", func(t *testing.T) {
		res, _, _ := sliceBTalkStart(t, full, videoAndAudioOffer, true, false)
		if res.sessionID != 42 || res.talkback {
			t.Fatalf("result = %+v", res)
		}
	})

	t.Run("reports no talkback for a solicited offer", func(t *testing.T) {
		res, sent, _ := sliceBTalkStart(t, full, "", false, false)
		if sent != nil || res.mode != ModeSolicitOffer || res.talkback {
			t.Fatalf("result = %+v, sent = %v", res, sent)
		}
	})
}

// Stops, releases and the endings they announce
// (CameraStreamManagerTest.ts:3415-3746, matterjs-server at 0bd06f2).
func TestSliceBSessionsStopsParityMatterjsServer(t *testing.T) {
	t.Run("ends the session without deallocating the stream", func(t *testing.T) {
		m, io := sliceBTracked(t)
		if ended, err := m.stopStream(sliceBCtx, tTarget, 42, ""); err != nil || !ended {
			t.Fatalf("stopStream = %v, %v", ended, err)
		}
		if sliceBCount(io, callEndSession) == 0 || sliceBCount(io, cmdVideoStreamDeallocate) != 0 {
			t.Fatalf("commands = %v", io.commands())
		}
	})

	t.Run("tells the owning connection that its session was stopped, whoever stopped it", func(t *testing.T) {
		m, _ := sliceBAllocating()
		endings := endingsOf(m)
		sliceBMustStart(t, m, "conn-1")
		if _, err := m.stopStream(sliceBCtx, tTarget, 42, "conn-2"); err != nil {
			t.Fatal(err)
		}
		sliceBSameEndings(t, endings(), []cameraSessionEnded{sliceBEnding(42, "conn-1", stoppedBy("conn-2"))})
	})

	t.Run("announces the sessions a closing connection and a shutdown ended, asked for by nobody", func(t *testing.T) {
		m, _, _ := managerWith(fixtureState(), sliceBNumbering())
		endings := endingsOf(m)
		sliceBMustStart(t, m, "conn-1")
		sliceBMustStart(t, m, "conn-2")
		m.releaseConnection(sliceBCtx, "conn-1")
		m.stopAll(sliceBCtx)
		es := endings()
		got := make([]string, 0, len(es))
		for _, e := range es {
			got = append(got, fmt.Sprintf("%d %s %q", e.webRTCSessionID, e.ownerID, e.requestedBy))
		}
		sliceBSameStrings(t, got, []string{`42 conn-1 ""`, `43 conn-2 ""`})
	})

	t.Run("announces a session it holds no record of with no owner, so every client hears it", func(t *testing.T) {
		m, _, _ := managerWith(fixtureState(), nil)
		endings := endingsOf(m)
		if ended, err := m.stopStream(sliceBCtx, tTarget, 999, "conn-9"); err != nil || !ended {
			t.Fatalf("stopStream = %v, %v", ended, err)
		}
		sliceBSameEndings(t, endings(), []cameraSessionEnded{sliceBEnding(999, "", stoppedBy("conn-9"))})
	})

	t.Run("announces nothing for a session the camera says it does not hold", func(t *testing.T) {
		m, _, _ := managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
			if r.command == callEndSession {
				return nil, statusErr(im.StatusNotFound)
			}
			return nil, nil
		})
		endings := endingsOf(m)
		if ended, err := m.stopStream(sliceBCtx, tTarget, 999, ""); err != nil || ended {
			t.Fatalf("stopStream = %v, %v", ended, err)
		}
		sliceBSameEndings(t, endings(), nil)
	})

	t.Run("announces nothing while a session is still open because EndSession failed", func(t *testing.T) {
		m, _, _ := managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
			if r.command == callEndSession {
				return nil, statusErr(im.StatusBusy)
			}
			return sliceBAnswers{video: 9, offer: 42}.respond(r)
		})
		endings := endingsOf(m)
		sliceBMustStart(t, m, "conn-1")
		_, err := m.stopStream(sliceBCtx, tTarget, 42, "")
		sliceBStatus(t, err, im.StatusBusy)
		sliceBSameEndings(t, endings(), nil)
	})

	t.Run("announces the peer's End to the owner with the camera's reason", func(t *testing.T) {
		m, _ := sliceBAllocating()
		endings := endingsOf(m)
		sliceBMustStart(t, m, "conn-1")
		if !m.sessions.peerEnded(tNode, tEndpoint, 42, 9, true) {
			t.Fatal("endedByPeer = false")
		}
		sliceBSameEndings(t, endings(), []cameraSessionEnded{sliceBEnding(42, "conn-1", sessionEnd{cause: endPeer, reason: 9, hasReason: true})})
	})

	t.Run("ends a session the camera holds that this process run never tracked", func(t *testing.T) {
		m, io, _ := managerWith(fixtureState(), nil)
		if ended, err := m.stopStream(sliceBCtx, tTarget, 999, ""); err != nil || !ended {
			t.Fatalf("stopStream = %v, %v", ended, err)
		}
		if got := sliceBEndedSessions(io); !slices.Equal(got, []sliceBEnd{{tNode, 999}}) {
			t.Fatalf("EndSession = %v", got)
		}
	})

	t.Run("reports false for an untracked id the camera answers NotFound for", func(t *testing.T) {
		m, _, _ := managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
			if r.command == callEndSession {
				return nil, statusErr(im.StatusNotFound)
			}
			return nil, nil
		})
		if ended, err := m.stopStream(sliceBCtx, tTarget, 999, ""); err != nil || ended {
			t.Fatalf("stopStream = %v, %v", ended, err)
		}
	})

	t.Run("raises any other refusal of an untracked session rather than reporting a stop", func(t *testing.T) {
		m, _, _ := managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
			if r.command == callEndSession {
				return nil, statusErr(im.StatusBusy)
			}
			return nil, nil
		})
		_, err := m.stopStream(sliceBCtx, tTarget, 999, "")
		sliceBStatus(t, err, im.StatusBusy)
	})

	t.Run("leaves a session tracked for another node alone and asks the named node instead", func(t *testing.T) {
		m, io := sliceBTracked(t)
		if _, err := m.stopStream(sliceBCtx, target{999, tEndpoint}, 42, ""); err != nil {
			t.Fatal(err)
		}
		if got := sliceBEndedSessions(io); !slices.Equal(got, []sliceBEnd{{999, 42}}) {
			t.Fatalf("EndSession = %v", got)
		}
		if ended, err := m.stopStream(sliceBCtx, tTarget, 42, ""); err != nil || !ended {
			t.Fatalf("stopStream = %v, %v", ended, err)
		}
	})

	t.Run("leaves a session tracked for another endpoint alone and asks the named endpoint instead", func(t *testing.T) {
		m, io := sliceBTracked(t)
		if _, err := m.stopStream(sliceBCtx, target{tNode, 99}, 42, ""); err != nil {
			t.Fatal(err)
		}
		var endpoints []uint16
		for _, r := range io.recorded() {
			if r.command == callEndSession {
				endpoints = append(endpoints, r.t.endpointID)
			}
		}
		if !slices.Equal(endpoints, []uint16{99}) {
			t.Fatalf("EndSession endpoints = %v", endpoints)
		}
		if ended, err := m.stopStream(sliceBCtx, tTarget, 42, ""); err != nil || !ended {
			t.Fatalf("stopStream = %v, %v", ended, err)
		}
	})

	t.Run("ends only the sessions of the connection that closed", func(t *testing.T) {
		m, io := sliceBTracked(t)
		m.releaseConnection(sliceBCtx, "conn-2")
		if sliceBCount(io, callEndSession) != 0 {
			t.Fatal("conn-2's release ended a session")
		}
		m.releaseConnection(sliceBCtx, "conn-1")
		if sliceBCount(io, callEndSession) != 1 {
			t.Fatal("conn-1's release ended nothing")
		}
	})

	t.Run("stopAll ends every tracked session regardless of owning connection", func(t *testing.T) {
		m, io, _ := managerWith(fixtureState(), sliceBNumbering())
		sliceBMustStart(t, m, "conn-1")
		sliceBMustStart(t, m, "conn-2")
		m.stopAll(sliceBCtx)
		ended := sliceBEndedSessions(io)
		ids := make([]uint16, 0, len(ended))
		for _, e := range ended {
			ids = append(ids, e.id)
		}
		slices.Sort(ids)
		if !slices.Equal(ids, []uint16{42, 43}) {
			t.Fatalf("ended = %v", ids)
		}
	})
}

// Starts racing on one endpoint and on two (CameraStreamManagerTest.ts:3748-3878,
// matterjs-server at 0bd06f2).
func TestSliceBSessionsEndpointLockParityMatterjsServer(t *testing.T) {
	t.Run("does not let a concurrent startStream evict a session still being established", func(t *testing.T) {
		var (
			mu       sync.Mutex
			allocs   int
			sessions uint16
			io       *fakeIO
			m        *manager
		)
		state := fixtureState()
		gate, atOffer := make(chan struct{}), sliceBNewSignal()
		m, io, _ = managerWith(state, func(r *recordedInvoke) (any, error) {
			switch r.command {
			case cmdVideoStreamAllocate:
				mu.Lock()
				defer mu.Unlock()
				allocs++
				if allocs == 1 {
					state = state.with(func(s *cameraState) { s.video = append(slices.Clone(s.video), sliceBContained(20, 0)) })
					io.setState(state)
					return csdef.VideoStreamAllocateResponse{VideoStreamId: 20}, nil
				}
				// The second call needs a size stream 20 cannot cover.
				if allocs < 4 {
					return nil, statusErr(im.StatusResourceExhausted)
				}
				return csdef.VideoStreamAllocateResponse{VideoStreamId: 30}, nil
			case callProvideOffer:
				atOffer.fire()
				<-gate
				mu.Lock()
				defer mu.Unlock()
				sessions++
				state = state.with(func(s *cameraState) {
					s.video = slices.Clone(s.video)
					for i := range s.video {
						if s.video[i].videoStreamID == 20 {
							s.video[i].referenceCount++
						}
					}
				})
				io.setState(state)
				return wpdef.ProvideOfferResponse{WebRtcSessionId: sessions}, nil
			}
			return nil, nil
		})
		first := sliceBGo(func() (*startResult, error) { return sliceBStart(m, "conn-1", tNode, tEndpoint) })
		sliceBAwait(t, atOffer.ch, "the first offer")
		queued := sliceBNewProbe()
		second := sliceBGo(func() (*startResult, error) {
			return m.startStream(queued, sliceBArgs(videoOffer, func(a *startArgs) {
				a.connectionID = "conn-2"
				sliceBVideoOnly(a)
				a.video.minResolution, a.video.maxResolution = resolution{2560, 1440}, resolution{2560, 1440}
			}))
		})
		sliceBAwait(t, queued.reached, "the second start at the endpoint lock")
		mu.Lock()
		allocated := allocs
		mu.Unlock()
		if allocated != 1 || sliceBCount(io, cmdVideoStreamDeallocate) != 0 {
			t.Fatalf("while queued: %d allocates, commands %v", allocated, io.commands())
		}
		close(gate)
		for _, r := range []sliceBResult[*startResult]{sliceBRecv(t, first, "first"), sliceBRecv(t, second, "second")} {
			if r.err != nil {
				t.Fatal(r.err)
			}
		}
		if sliceBCount(io, cmdVideoStreamDeallocate) != 0 {
			t.Fatalf("commands = %v", io.commands())
		}
	})

	t.Run("does not block startStream on a different endpoint while one is mid-establishment", func(t *testing.T) {
		gate, atOffer := make(chan struct{}), sliceBNewSignal()
		m, _, _ := managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
			switch r.command {
			case cmdVideoStreamAllocate:
				return csdef.VideoStreamAllocateResponse{VideoStreamId: 20}, nil
			case callProvideOffer:
				if r.t.endpointID == tEndpoint {
					atOffer.fire()
					<-gate
					return wpdef.ProvideOfferResponse{WebRtcSessionId: 1}, nil
				}
				return wpdef.ProvideOfferResponse{WebRtcSessionId: 2}, nil
			}
			return nil, nil
		})
		first := sliceBGo(func() (*startResult, error) { return sliceBStart(m, "conn-1", tNode, tEndpoint) })
		sliceBAwait(t, atOffer.ch, "the first offer")
		second := sliceBRecv(t, sliceBGo(func() (*startResult, error) { return sliceBStart(m, "conn-2", tNode, 2) }), "the other endpoint's start")
		if second.err != nil || second.v.sessionID != 2 {
			t.Fatalf("second = %+v, %v", second.v, second.err)
		}
		close(gate)
		if r := sliceBRecv(t, first, "first"); r.err != nil || r.v.sessionID != 1 {
			t.Fatalf("first = %+v, %v", r.v, r.err)
		}
	})
}

// sliceBEndAsClient is the source's endSessionAsClient: a client's own
// EndSession, send standing in for the camera.
func sliceBEndAsClient(m *manager, id uint16, requestedBy string, send func(context.Context) error) string {
	if send == nil {
		send = func(context.Context) error { return nil }
	}
	err := m.endSessionForClient(sliceBCtx, tTarget, id, requestedBy, send)
	switch {
	case err == nil:
		return "ended"
	case statusIs(err, im.StatusNotFound):
		return "not_found"
	default:
		return "failed"
	}
}

func sliceBNotFound(context.Context) error { return statusErr(im.StatusNotFound) }

// Session ids, failed and peer-ended stops, a client's own EndSession,
// registrations in flight and reissued ids (CameraStreamManagerTest.ts:3903-4269,
// matterjs-server at 0bd06f2).
func TestSliceBSessionsRegistryParityMatterjsServer(t *testing.T) {
	t.Run("tracks two cameras that both issue session id 1 as two sessions", func(t *testing.T) {
		m, io, _ := managerWith(fixtureState(), sliceBAnswers{video: 9, offer: 1}.respond)
		for _, s := range []struct {
			conn string
			node uint64
		}{{"conn-1", tNode}, {"conn-2", 6}} {
			if _, err := sliceBStart(m, s.conn, s.node, tEndpoint); err != nil {
				t.Fatal(err)
			}
		}
		if ended, err := m.stopStream(sliceBCtx, target{6, tEndpoint}, 1, ""); err != nil || !ended {
			t.Fatalf("stopStream = %v, %v", ended, err)
		}
		if got := sliceBEndedSessions(io); !slices.Equal(got, []sliceBEnd{{6, 1}}) {
			t.Fatalf("EndSession = %v", got)
		}
		if ended, err := m.stopStream(sliceBCtx, tTarget, 1, ""); err != nil || !ended {
			t.Fatalf("stopStream = %v, %v", ended, err)
		}
		if got := sliceBEndedSessions(io); !slices.Equal(got, []sliceBEnd{{6, 1}, {tNode, 1}}) {
			t.Fatalf("EndSession = %v", got)
		}
	})

	t.Run("keeps the session tracked when EndSession fails, so a later attempt still reaches it", func(t *testing.T) {
		var attempts atomic.Int32
		m, _, _ := managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
			if r.command == callEndSession && attempts.Add(1) == 1 {
				return nil, errors.New("device unreachable")
			}
			return sliceBAnswers{video: 9, offer: 42}.respond(r)
		})
		sliceBMustStart(t, m, "conn-1")
		if _, err := m.stopStream(sliceBCtx, tTarget, 42, ""); err == nil || err.Error() != "device unreachable" {
			t.Fatalf("err = %v", err)
		}
		if ended, err := m.stopStream(sliceBCtx, tTarget, 42, ""); err != nil || !ended || attempts.Load() != 2 {
			t.Fatalf("retry = %v, %v after %d attempts", ended, err, attempts.Load())
		}
	})

	t.Run("reports no session ended, and drops tracking, when the device answers NotFound", func(t *testing.T) {
		m, io, _ := managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
			if r.command == callEndSession {
				return nil, statusErr(im.StatusNotFound)
			}
			return sliceBAnswers{video: 9, offer: 42}.respond(r)
		})
		sliceBMustStart(t, m, "conn-1")
		if ended, err := m.stopStream(sliceBCtx, tTarget, 42, ""); err != nil || ended {
			t.Fatalf("stopStream = %v, %v", ended, err)
		}
		m.stopAll(sliceBCtx)
		if n := len(sliceBEndedSessions(io)); n != 1 {
			t.Fatalf("%d EndSessions, want 1", n)
		}
	})

	t.Run("raises on a stop that waits for a failing EndSession the closing connection started", func(t *testing.T) {
		gate, entered := make(chan struct{}), make(chan struct{}, 8)
		m, io, _ := managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
			if r.command == callEndSession {
				entered <- struct{}{}
				<-gate
				return nil, errors.New("device unreachable")
			}
			return sliceBAnswers{video: 9, offer: 42}.respond(r)
		})
		sliceBMustStart(t, m, "conn-1")
		releasing := sliceBGo(func() (struct{}, error) { m.releaseConnection(sliceBCtx, "conn-1"); return struct{}{}, nil })
		sliceBAwait(t, entered, "the release's EndSession")
		joined := sliceBJoined()
		stopping := sliceBGo(func() (bool, error) { return m.stopStream(sliceBCtx, tTarget, 42, "") })
		sliceBAwaitJoined(t, "the stop to join the release's EndSession", joined)
		close(gate)
		if r := sliceBRecv(t, stopping, "the stop"); r.err == nil || r.err.Error() != "device unreachable" {
			t.Fatalf("stop = %v, %v", r.v, r.err)
		}
		sliceBRecv(t, releasing, "the release")
		if n := sliceBCount(io, callEndSession); n != 1 {
			t.Fatalf("%d EndSessions, want 1", n)
		}
		m.stopAll(sliceBCtx)
		if n := sliceBCount(io, callEndSession); n != 2 {
			t.Fatalf("%d EndSessions after shutdown, want 2", n)
		}
	})

	t.Run("forgets a session the peer ended, so shutdown sends no EndSession for it", func(t *testing.T) {
		m, io, _ := managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
			if r.command == callEndSession {
				return nil, statusErr(im.StatusNotFound)
			}
			return sliceBAnswers{video: 9, offer: 42}.respond(r)
		})
		sliceBMustStart(t, m, "conn-1")
		if !m.sessions.peerEnded(tNode, tEndpoint, 42, 0, false) {
			t.Fatal("endedByPeer = false")
		}
		if ended, err := m.stopStream(sliceBCtx, tTarget, 42, ""); err != nil || ended {
			t.Fatalf("stopStream = %v, %v", ended, err)
		}
		after := len(sliceBEndedSessions(io))
		m.stopAll(sliceBCtx)
		if n := len(sliceBEndedSessions(io)); n != after {
			t.Fatalf("shutdown sent %d EndSessions", n-after)
		}
	})

	t.Run("announces a client's own EndSession to the session's owner, naming the connection that asked", func(t *testing.T) {
		m, _ := sliceBTracked(t)
		endings := endingsOf(m)
		if got := sliceBEndAsClient(m, 42, "conn-2", nil); got != "ended" {
			t.Fatalf("endSessionForClient = %s", got)
		}
		sliceBSameEndings(t, endings(), []cameraSessionEnded{sliceBEnding(42, "conn-1", stoppedBy("conn-2"))})
	})

	t.Run("announces to the owner even when the camera denies the session, because it is gone either way", func(t *testing.T) {
		m, _ := sliceBTracked(t)
		endings := endingsOf(m)
		if got := sliceBEndAsClient(m, 42, "conn-2", sliceBNotFound); got != "not_found" {
			t.Fatalf("endSessionForClient = %s", got)
		}
		if got := endings(); len(got) != 1 || got[0].ownerID != "conn-1" {
			t.Fatalf("endings = %+v", got)
		}
	})

	t.Run("announces the peer's End once, and nothing for an End no entry names", func(t *testing.T) {
		m, _ := sliceBTracked(t)
		endings := endingsOf(m)
		if !m.sessions.peerEnded(tNode, tEndpoint, 42, 0, false) || m.sessions.peerEnded(tNode, tEndpoint, 42, 0, false) || m.sessions.peerEnded(tNode, tEndpoint, 7, 0, false) {
			t.Fatal("endedByPeer answered other than true, false, false")
		}
		sliceBSameEndings(t, endings(), []cameraSessionEnded{sliceBEnding(42, "conn-1", sessionEnd{cause: endPeer})})
	})

	t.Run("announces a client's EndSession for an untracked session with no owner, so everyone is told", func(t *testing.T) {
		m, _, _ := managerWith(fixtureState(), nil)
		endings := endingsOf(m)
		if got := sliceBEndAsClient(m, 42, "conn-2", nil); got != "ended" {
			t.Fatalf("endSessionForClient = %s", got)
		}
		sliceBSameEndings(t, endings(), []cameraSessionEnded{sliceBEnding(42, "", stoppedBy("conn-2"))})
	})

	t.Run("announces nothing for an untracked id the camera denies, which named no session at all", func(t *testing.T) {
		m, _, _ := managerWith(fixtureState(), nil)
		endings := endingsOf(m)
		if got := sliceBEndAsClient(m, 42, "conn-2", sliceBNotFound); got != "not_found" {
			t.Fatalf("endSessionForClient = %s", got)
		}
		sliceBSameEndings(t, endings(), nil)
	})

	t.Run("refuses to track a session the peer ended while the registration was still in flight", func(t *testing.T) {
		var m *manager
		var foundNoEntry []bool
		m, io, _ := managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
			switch r.command {
			case callProvideOffer:
				r.establishing(42)
				foundNoEntry = append(foundNoEntry, !m.sessions.peerEnded(tNode, tEndpoint, 42, 0, false))
				return wpdef.ProvideOfferResponse{WebRtcSessionId: 42}, nil
			case callEndSession:
				return nil, statusErr(im.StatusNotFound)
			}
			return sliceBAnswers{video: 9}.respond(r)
		})
		_, err := sliceBStart(m, "conn-1", tNode, tEndpoint)
		if !slices.Equal(foundNoEntry, []bool{true}) {
			t.Fatalf("peer End found an entry: %v", foundNoEntry)
		}
		if le := sliceBLocal(t, err); !strings.Contains(le.msg, "was ended before this controller could track it") {
			t.Fatalf("err = %v", err)
		}
		if m.sessions.peerEnded(tNode, tEndpoint, 42, 0, false) || sliceBCount(io, cmdVideoStreamDeallocate) != 1 {
			t.Fatalf("still named, or commands %v", io.commands())
		}
	})

	t.Run("refuses to track a session another connection stopped while the registration was in flight", func(t *testing.T) {
		var m *manager
		var stopped []bool
		m, io, _ := managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
			if r.command == callProvideOffer {
				r.establishing(42)
				ended, _ := m.stopStream(sliceBCtx, tTarget, 42, "conn-2")
				stopped = append(stopped, ended)
				return wpdef.ProvideOfferResponse{WebRtcSessionId: 42}, nil
			}
			return sliceBAnswers{video: 9}.respond(r)
		})
		_, err := sliceBStart(m, "conn-1", tNode, tEndpoint)
		if !slices.Equal(stopped, []bool{true}) {
			t.Fatalf("stopped = %v", stopped)
		}
		sliceBLocal(t, err)
		if m.sessions.peerEnded(tNode, tEndpoint, 42, 0, false) || sliceBCount(io, cmdVideoStreamDeallocate) != 1 {
			t.Fatalf("still named, or commands %v", io.commands())
		}
	})

	t.Run("tracks a session whose camera ended a different session while the registration was in flight", func(t *testing.T) {
		var m *manager
		m, _, _ = managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
			if r.command == callProvideOffer {
				r.establishing(42)
				m.sessions.peerEnded(tNode, tEndpoint, 41, 0, false)
				return wpdef.ProvideOfferResponse{WebRtcSessionId: 42}, nil
			}
			return sliceBAnswers{video: 9}.respond(r)
		})
		if res := sliceBMustStart(t, m, "conn-1"); res.sessionID != 42 {
			t.Fatalf("session = %d", res.sessionID)
		}
		if !m.sessions.peerEnded(tNode, tEndpoint, 42, 0, false) {
			t.Fatal("session 42 is not tracked")
		}
	})

	t.Run("hands the caller the session id while the offer call is still in flight", func(t *testing.T) {
		var seen []string
		m, _, _ := managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
			if r.command == callProvideOffer {
				r.establishing(42)
				seen = append(seen, "offer answered")
				return wpdef.ProvideOfferResponse{WebRtcSessionId: 42}, nil
			}
			return sliceBAnswers{video: 9}.respond(r)
		})
		if _, err := m.startStream(sliceBCtx, sliceBArgs(videoOffer, func(a *startArgs) {
			sliceBVideoOnly(a)
			a.establishing = func(id uint16) { seen = append(seen, fmt.Sprintf("establishing %d", id)) }
		})); err != nil {
			t.Fatal(err)
		}
		if len(seen) < 2 || !slices.Equal(seen[:2], []string{"establishing 42", "offer answered"}) {
			t.Fatalf("seen = %q", seen)
		}
	})

	t.Run("drops and announces a session whose id the camera hands to a new session", func(t *testing.T) {
		m, io := sliceBTracked(t)
		endings := endingsOf(m)
		if res := sliceBMustStart(t, m, "conn-2"); res.sessionID != 42 {
			t.Fatalf("session = %d", res.sessionID)
		}
		sliceBSameEndings(t, endings(), []cameraSessionEnded{sliceBEnding(42, "conn-1", sessionEnd{cause: endCameraRestarted})})
		m.releaseConnection(sliceBCtx, "conn-1")
		if got := sliceBEndedSessions(io); len(got) != 0 {
			t.Fatalf("EndSession = %v", got)
		}
		if owner, _ := sliceBOwner(m, 42); owner != "conn-2" {
			t.Fatalf("owner = %q", owner)
		}
	})

	t.Run("routes a reissued id to the new owner as soon as the camera hands it out", func(t *testing.T) {
		var m *manager
		var secondInFlight atomic.Bool
		var onceReissued string
		m, _, _ = managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
			if r.command == callProvideOffer {
				if secondInFlight.Load() {
					r.establishing(42)
					onceReissued, _ = sliceBOwner(m, 42)
				}
				return wpdef.ProvideOfferResponse{WebRtcSessionId: 42}, nil
			}
			return sliceBAnswers{video: 9}.respond(r)
		})
		sliceBMustStart(t, m, "conn-1")
		secondInFlight.Store(true)
		sliceBMustStart(t, m, "conn-2")
		if onceReissued != "conn-2" {
			t.Fatalf("owner once reissued = %q", onceReissued)
		}
	})

	t.Run("treats an End for a reissued id as the new session's, once the camera handed the id out", func(t *testing.T) {
		var m *manager
		var secondInFlight atomic.Bool
		m, _, _ = managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
			if r.command == callProvideOffer {
				if secondInFlight.Load() {
					r.establishing(42)
					m.sessions.peerEnded(tNode, tEndpoint, 42, 0, false)
				}
				return wpdef.ProvideOfferResponse{WebRtcSessionId: 42}, nil
			}
			return sliceBAnswers{video: 9}.respond(r)
		})
		sliceBMustStart(t, m, "conn-1")
		secondInFlight.Store(true)
		_, err := sliceBStart(m, "conn-2", tNode, tEndpoint)
		if le := sliceBLocal(t, err); !strings.Contains(le.msg, "was ended before this controller could track it") {
			t.Fatalf("err = %v", err)
		}
		sliceBNoOwner(t, m, 42)
	})
}

// sliceBGatedEnd is the source's listedSessionWithGatedEnd: a tracked
// session 42 of conn-1 a report listed, whose next EndSession waits for an
// answer.
type sliceBGatedEnd struct {
	m       *manager
	io      *fakeIO
	entered chan struct{}
	answers chan string
}

// camera is the gated EndSession: "ok", "busy" or "not_found".
func (g *sliceBGatedEnd) camera(context.Context) error {
	g.entered <- struct{}{}
	switch <-g.answers {
	case "busy":
		return statusErr(im.StatusBusy)
	case "not_found":
		return statusErr(im.StatusNotFound)
	}
	return nil
}

func sliceBListedSessionWithGatedEnd(t *testing.T) *sliceBGatedEnd {
	t.Helper()
	g := &sliceBGatedEnd{entered: make(chan struct{}, 8), answers: make(chan string, 8)}
	number := sliceBNumbering()
	g.m, g.io, _ = managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
		if r.command == callEndSession {
			return nil, g.camera(sliceBCtx)
		}
		return number(r)
	})
	sliceBMustStart(t, g.m, "conn-1")
	sliceBReport(g.m, false, 42)
	return g
}

// CurrentSessions reports against stops in flight and a client's own
// EndSession (CameraStreamManagerTest.ts:4280-4423, matterjs-server at
// 0bd06f2).
func TestSliceBSessionsReportsInFlightParityMatterjsServer(t *testing.T) {
	t.Run("ends a session a CurrentSessions report listed and a later one does not", func(t *testing.T) {
		m, io, _ := managerWith(fixtureState(), sliceBNumbering())
		sliceBMustStart(t, m, "conn-1")
		endings, vanished := endingsOf(m), vanishedOf(m)
		sliceBReport(m, false, 42)
		if len(vanished()) != 0 {
			t.Fatal("a listed session vanished")
		}
		sliceBReport(m, false)
		if !slices.Equal(vanished(), []uint16{42}) {
			t.Fatalf("vanished = %v", vanished())
		}
		sliceBSameEndings(t, endings(), []cameraSessionEnded{sliceBEnding(42, "conn-1", sessionEnd{cause: endCameraDropped})})
		sliceBNoOwner(t, m, 42)
		m.releaseConnection(sliceBCtx, "conn-1")
		if got := sliceBEndedSessions(io); len(got) != 0 {
			t.Fatalf("EndSession = %v", got)
		}
	})

	t.Run("leaves a session whose stop is in flight to the stop, which names who asked", func(t *testing.T) {
		gate, entered := make(chan struct{}), make(chan struct{}, 8)
		number := sliceBNumbering()
		m, _, _ := managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
			if r.command == callEndSession {
				entered <- struct{}{}
				<-gate
				return nil, nil
			}
			return number(r)
		})
		sliceBMustStart(t, m, "conn-1")
		endings := endingsOf(m)
		sliceBReport(m, false, 42)
		stopping := sliceBGo(func() (bool, error) { return m.stopStream(sliceBCtx, tTarget, 42, "conn-2") })
		sliceBAwait(t, entered, "the EndSession")
		sliceBReport(m, false)
		if len(endings()) != 0 {
			t.Fatalf("endings = %+v", endings())
		}
		close(gate)
		sliceBRecv(t, stopping, "the stop")
		sliceBSameStrings(t, sliceBCausesBy(endings()), []string{"other_connection conn-2"})
	})

	t.Run("ends a session a report dropped while its stop was in flight, once, when the stop fails", func(t *testing.T) {
		g := sliceBListedSessionWithGatedEnd(t)
		endings, vanished := endingsOf(g.m), vanishedOf(g.m)
		stopping := sliceBGo(func() (bool, error) { return g.m.stopStream(sliceBCtx, tTarget, 42, "conn-2") })
		sliceBAwait(t, g.entered, "the EndSession")
		sliceBReport(g.m, false)
		sliceBReport(g.m, false)
		if len(endings()) != 0 {
			t.Fatalf("endings = %+v", endings())
		}
		g.answers <- "busy"
		if r := sliceBRecv(t, stopping, "the stop"); r.err == nil {
			t.Fatal("the stop did not fail")
		}
		sliceBSameEndings(t, endings(), []cameraSessionEnded{sliceBEnding(42, "conn-1", sessionEnd{cause: endCameraDropped})})
		if !slices.Equal(vanished(), []uint16{42}) {
			t.Fatalf("vanished = %v", vanished())
		}
		sliceBNoOwner(t, g.m, 42)
		g.m.releaseConnection(sliceBCtx, "conn-1")
		if n := sliceBCount(g.io, callEndSession); n != 1 {
			t.Fatalf("%d EndSessions, want 1", n)
		}
	})

	t.Run("names the camera's end, not the stop, when a stop the report overtook finds no session", func(t *testing.T) {
		g := sliceBListedSessionWithGatedEnd(t)
		endings := endingsOf(g.m)
		stopping := sliceBGo(func() (bool, error) { return g.m.stopStream(sliceBCtx, tTarget, 42, "conn-2") })
		sliceBAwait(t, g.entered, "the EndSession")
		sliceBReport(g.m, true)
		g.answers <- "not_found"
		if r := sliceBRecv(t, stopping, "the stop"); r.err != nil || r.v {
			t.Fatalf("stop = %v, %v", r.v, r.err)
		}
		sliceBSameStrings(t, sliceBCausesBy(endings()), []string{"camera_restarted undefined"})
	})

	t.Run("announces a device_command EndSession a removal report overtook once, to the owner, naming the requester", func(t *testing.T) {
		g := sliceBListedSessionWithGatedEnd(t)
		endings, vanished := endingsOf(g.m), vanishedOf(g.m)
		ending := sliceBGo(func() (string, error) { return sliceBEndAsClient(g.m, 42, "conn-2", g.camera), nil })
		sliceBAwait(t, g.entered, "the EndSession")
		sliceBReport(g.m, false)
		if len(endings()) != 0 {
			t.Fatalf("endings = %+v", endings())
		}
		g.answers <- "ok"
		if r := sliceBRecv(t, ending, "the EndSession"); r.v != "ended" {
			t.Fatalf("endSessionForClient = %s", r.v)
		}
		sliceBSameEndings(t, endings(), []cameraSessionEnded{sliceBEnding(42, "conn-1", stoppedBy("conn-2"))})
		if len(vanished()) != 0 {
			t.Fatalf("vanished = %v", vanished())
		}
	})

	t.Run("announces the owner's own device_command EndSession a removal report overtook with the owner as requester", func(t *testing.T) {
		g := sliceBListedSessionWithGatedEnd(t)
		endings := endingsOf(g.m)
		ending := sliceBGo(func() (string, error) { return sliceBEndAsClient(g.m, 42, "conn-1", g.camera), nil })
		sliceBAwait(t, g.entered, "the EndSession")
		sliceBReport(g.m, false)
		g.answers <- "ok"
		if r := sliceBRecv(t, ending, "the EndSession"); r.v != "ended" {
			t.Fatalf("endSessionForClient = %s", r.v)
		}
		es := endings()
		got := make([]string, 0, len(es))
		for _, e := range es {
			got = append(got, e.ownerID+" "+e.requestedBy)
		}
		sliceBSameStrings(t, got, []string{"conn-1 conn-1"})
	})
}

// The describe "announces exactly once whatever order a report, the peer's
// End and the camera's answer arrive in" (CameraStreamManagerTest.ts:4425-4475,
// matterjs-server at 0bd06f2): every route, answer and order.
func TestSliceBSessionsAnnouncesExactlyOnceParityMatterjsServer(t *testing.T) {
	orders := [][]string{
		{"report", "peer", "answer"},
		{"report", "answer", "peer"},
		{"peer", "report", "answer"},
		{"peer", "answer", "report"},
		{"answer", "report", "peer"},
		{"answer", "peer", "report"},
	}
	causeOf := func(step string) string {
		if step == "report" {
			return "camera_dropped"
		}
		return "peer"
	}
	for _, route := range []string{"camera_stop_stream", "device_command"} {
		for _, outcome := range []string{"ok", "busy", "not_found"} {
			for _, order := range orders {
				evidence := slices.DeleteFunc(slices.Clone(order), func(s string) bool { return s == "answer" })
				expected := causeOf(evidence[0]) + " undefined"
				if outcome == "ok" || (outcome == "not_found" && order[0] == "answer") {
					expected = "other_connection conn-2"
				}
				name := fmt.Sprintf("%s, camera answers %s, order %s: %s", route, outcome, strings.Join(order, " > "), expected)
				t.Run(name, func(t *testing.T) {
					g := sliceBListedSessionWithGatedEnd(t)
					endings, vanished := endingsOf(g.m), vanishedOf(g.m)
					ending := sliceBGo(func() (struct{}, error) {
						if route == "camera_stop_stream" {
							_, _ = g.m.stopStream(sliceBCtx, tTarget, 42, "conn-2")
						} else {
							sliceBEndAsClient(g.m, 42, "conn-2", g.camera)
						}
						return struct{}{}, nil
					})
					sliceBAwait(t, g.entered, "the EndSession")
					for _, step := range order {
						switch step {
						case "report":
							sliceBReport(g.m, false)
						case "peer":
							g.m.sessions.peerEnded(tNode, tEndpoint, 42, 0, false)
						default:
							g.answers <- outcome
							sliceBRecv(t, ending, "the EndSession")
						}
					}
					sliceBSameStrings(t, sliceBCausesBy(endings()), []string{expected})
					if owner := endings()[0].ownerID; owner != "conn-1" {
						t.Fatalf("owner = %q", owner)
					}
					want := []uint16{}
					if outcome == "busy" && strings.HasPrefix(expected, "camera_") {
						want = []uint16{42}
					}
					if got := vanished(); !slices.Equal(append([]uint16{}, got...), want) {
						t.Fatalf("vanished = %v, want %v", got, want)
					}
				})
			}
		}
	}
}

// Reports, node removal and ids another route was given
// (CameraStreamManagerTest.ts:4477-4688, matterjs-server at 0bd06f2).
func TestSliceBSessionsReportsParityMatterjsServer(t *testing.T) {
	t.Run("names the restart as the cause when the node reported one", func(t *testing.T) {
		m, _, _ := managerWith(fixtureState(), sliceBNumbering())
		sliceBMustStart(t, m, "conn-1")
		endings := endingsOf(m)
		sliceBReport(m, false, 42)
		sliceBReport(m, true)
		if got := endings(); len(got) != 1 || got[0].cause != endCameraRestarted {
			t.Fatalf("endings = %+v", got)
		}
	})

	t.Run("keeps a session no report has listed yet, since the report may predate its offer", func(t *testing.T) {
		m, _, _ := managerWith(fixtureState(), sliceBNumbering())
		sliceBMustStart(t, m, "conn-1")
		endings := endingsOf(m)
		sliceBReport(m, true)
		sliceBSameEndings(t, endings(), nil)
		if owner, _ := sliceBOwner(m, 42); owner != "conn-1" {
			t.Fatalf("owner = %q", owner)
		}
	})

	t.Run("applies a report only to its own endpoint", func(t *testing.T) {
		m, _, _ := managerWith(fixtureState(), sliceBNumbering())
		sliceBMustStart(t, m, "conn-1")
		sliceBReport(m, false, 42)
		sliceBReportOn(m, tNode, 2, false)
		sliceBReportOn(m, 6, tEndpoint, false)
		if owner, _ := sliceBOwner(m, 42); owner != "conn-1" {
			t.Fatalf("owner = %q", owner)
		}
	})

	t.Run("announces a session once whether the peer's End or the report comes first", func(t *testing.T) {
		m, _, _ := managerWith(fixtureState(), sliceBNumbering())
		sliceBMustStart(t, m, "conn-1")
		sliceBMustStart(t, m, "conn-2")
		endings := endingsOf(m)
		sliceBReport(m, false, 42, 43)
		m.sessions.peerEnded(tNode, tEndpoint, 42, 2, true)
		sliceBReport(m, false)
		m.sessions.peerEnded(tNode, tEndpoint, 43, 2, true)
		es := endings()
		got := make([]string, 0, len(es))
		for _, e := range es {
			got = append(got, fmt.Sprintf("%d %s", e.webRTCSessionID, e.cause))
		}
		sliceBSameStrings(t, got, []string{"42 peer", "43 camera_dropped"})
	})

	t.Run("drops and announces every session of a removed node, and only that node's", func(t *testing.T) {
		m, io, _ := managerWith(fixtureState(), sliceBNumbering())
		sliceBMustStart(t, m, "conn-1")
		if _, err := sliceBStart(m, "conn-1", 6, tEndpoint); err != nil {
			t.Fatal(err)
		}
		endings := endingsOf(m)
		m.sessions.forgetNode(tNode)
		sliceBSameEndings(t, endings(), []cameraSessionEnded{sliceBEnding(42, "conn-1", sessionEnd{cause: endNodeRemoved})})
		m.releaseConnection(sliceBCtx, "conn-1")
		if got := sliceBEndedSessions(io); !slices.Equal(got, []sliceBEnd{{6, 43}}) {
			t.Fatalf("EndSession = %v", got)
		}
	})

	t.Run("refuses a session that was being established on a node removed meanwhile", func(t *testing.T) {
		var m *manager
		m, _, _ = managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
			if r.command == callProvideOffer {
				m.sessions.forgetNode(tNode)
				return wpdef.ProvideOfferResponse{WebRtcSessionId: 42}, nil
			}
			return sliceBAnswers{video: 9}.respond(r)
		})
		_, err := sliceBStart(m, "conn-1", tNode, tEndpoint)
		sliceBLocal(t, err)
		sliceBNoOwner(t, m, 42)
	})

	t.Run("drops a managed session whose id the camera gives to a session of another route", func(t *testing.T) {
		m, io, _ := managerWith(fixtureState(), sliceBNumbering())
		sliceBMustStart(t, m, "conn-1")
		endings := endingsOf(m)
		m.sessions.issued(tNode, tEndpoint, 42)
		sliceBSameEndings(t, endings(), []cameraSessionEnded{sliceBEnding(42, "conn-1", sessionEnd{cause: endCameraRestarted})})
		sliceBNoOwner(t, m, 42)
		m.releaseConnection(sliceBCtx, "conn-1")
		if got := sliceBEndedSessions(io); len(got) != 0 {
			t.Fatalf("EndSession = %v", got)
		}
	})

	t.Run("refuses a registration whose id the camera gave to a session of another route meanwhile, leaving that session alone", func(t *testing.T) {
		var m *manager
		ownerOnceIssued := true
		m, io, _ := managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
			if r.command == callProvideOffer {
				r.establishing(42)
				m.sessions.issued(tNode, tEndpoint, 42)
				_, ownerOnceIssued = sliceBOwner(m, 42)
				return wpdef.ProvideOfferResponse{WebRtcSessionId: 42}, nil
			}
			return sliceBAnswers{video: 9}.respond(r)
		})
		_, err := sliceBStart(m, "conn-1", tNode, tEndpoint)
		sliceBLocal(t, err)
		if ownerOnceIssued {
			t.Fatal("the superseded registration was named the owner")
		}
		if got := sliceBEndedSessions(io); len(got) != 0 {
			t.Fatalf("EndSession = %v", got)
		}
	})

	t.Run("counts a CurrentSessions report that lists the session while it is being established", func(t *testing.T) {
		var m *manager
		m, _, _ = managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
			if r.command == callProvideOffer {
				r.establishing(42)
				sliceBReport(m, false, 42)
				return wpdef.ProvideOfferResponse{WebRtcSessionId: 42}, nil
			}
			return sliceBAnswers{video: 9}.respond(r)
		})
		sliceBMustStart(t, m, "conn-1")
		endings, vanished := endingsOf(m), vanishedOf(m)
		sliceBReport(m, false)
		if !slices.Equal(vanished(), []uint16{42}) {
			t.Fatalf("vanished = %v", vanished())
		}
		if got := endings(); len(got) != 1 || got[0].cause != endCameraDropped {
			t.Fatalf("endings = %+v", got)
		}
	})

	t.Run("joins a stop by id already in flight when a closing connection's start gives its session up", func(t *testing.T) {
		var m *manager
		answered, entered := make(chan struct{}), make(chan struct{}, 8)
		var stopping <-chan sliceBResult[bool]
		var releasing <-chan sliceBResult[struct{}]
		m, io, _ := managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
			switch r.command {
			case callProvideOffer:
				r.establishing(42)
				stopping = sliceBGo(func() (bool, error) { return m.stopStream(sliceBCtx, tTarget, 42, "conn-2") })
				<-entered
				releasing = sliceBGo(func() (struct{}, error) { m.releaseConnection(sliceBCtx, "conn-1"); return struct{}{}, nil })
				for sliceBClaimed(m) == 0 {
					runtime.Gosched()
				}
				return wpdef.ProvideOfferResponse{WebRtcSessionId: 42}, nil
			case callEndSession:
				entered <- struct{}{}
				<-answered
				return nil, nil
			}
			return sliceBAnswers{video: 9}.respond(r)
		})
		endings := endingsOf(m)
		joined := sliceBJoined()
		starting := sliceBGo(func() (*startResult, error) { return sliceBStart(m, "conn-1", tNode, tEndpoint) })
		sliceBAwaitJoined(t, "the start to join the stop's EndSession", joined)
		close(answered)
		if r := sliceBRecv(t, starting, "the start"); r.err == nil {
			t.Fatal("the start succeeded")
		}
		if r := sliceBRecv(t, stopping, "the stop"); r.err != nil || !r.v {
			t.Fatalf("stop = %v, %v", r.v, r.err)
		}
		sliceBRecv(t, releasing, "the release")
		if n := len(sliceBEndedSessions(io)); n != 1 {
			t.Fatalf("%d EndSessions, want 1", n)
		}
		es := endings()
		got := make([]string, 0, len(es))
		for _, e := range es {
			owner := e.ownerID
			if owner == "" {
				owner = "undefined"
			}
			got = append(got, sliceBCauseBy(e)+" "+owner)
		}
		sliceBSameStrings(t, got, []string{"other_connection conn-2 undefined"})
	})

	t.Run("refuses a start whose session a report listed and a later report no longer did, before it was tracked", func(t *testing.T) {
		var m *manager
		m, _, _ = managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
			if r.command == callProvideOffer {
				r.establishing(42)
				sliceBReport(m, false, 42)
				sliceBReport(m, false)
				return wpdef.ProvideOfferResponse{WebRtcSessionId: 42}, nil
			}
			return sliceBAnswers{video: 9}.respond(r)
		})
		endings := endingsOf(m)
		_, err := sliceBStart(m, "conn-1", tNode, tEndpoint)
		if le := sliceBLocal(t, err); !strings.Contains(le.msg, "was ended before this controller could track it") {
			t.Fatalf("err = %v", err)
		}
		sliceBNoOwner(t, m, 42)
		sliceBSameEndings(t, endings(), nil)
	})
}

// Failed starts and what they give back (CameraStreamManagerTest.ts:4690-4924,
// matterjs-server at 0bd06f2).
func TestSliceBSessionsFailedStartsParityMatterjsServer(t *testing.T) {
	refused := errors.New("provider refused")

	t.Run("gives back the streams it allocated when the provider call fails", func(t *testing.T) {
		m, io, _ := managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
			if r.command == callProvideOffer {
				return nil, refused
			}
			return sliceBAnswers{video: 9, audio: 4}.respond(r)
		})
		_, err := m.startStream(sliceBCtx, sliceBArgs(videoAndAudioOffer, func(a *startArgs) {
			a.videoState, a.video, a.audioState, a.audio = trackDemanded, sliceBVideo(), trackDemanded, &audioHints{}
		}))
		if err == nil || err.Error() != "provider refused" {
			t.Fatalf("err = %v", err)
		}
		if sliceBCount(io, cmdVideoStreamDeallocate) != 1 || sliceBCount(io, cmdAudioStreamDeallocate) != 1 {
			t.Fatalf("commands = %v", io.commands())
		}
	})

	t.Run("gives the video stream back when an audio codec the caller stated cannot be served", func(t *testing.T) {
		m, io, _ := managerWith(fixtureState(), sliceBAnswers{video: 9}.respond)
		_, err := m.startStream(sliceBCtx, sliceBArgs(videoAndAudioOffer, func(a *startArgs) {
			a.videoState, a.video, a.audioState, a.audio = trackDemanded, sliceBVideo(), trackDemanded, &audioHints{codecs: []string{"AAC"}}
		}))
		if inc := sliceBIncompatible(t, err); inc.Reason != ReasonCodec || !slices.Equal(inc.Device, []string{"OPUS"}) {
			t.Fatalf("err = %+v", inc)
		}
		if sliceBCount(io, cmdVideoStreamDeallocate) != 1 {
			t.Fatalf("commands = %v", io.commands())
		}
	})

	t.Run("fails the whole call when the offer names no video codec the camera supports", func(t *testing.T) {
		m, io, _ := managerWith(fixtureState(), nil)
		_, err := m.startStream(sliceBCtx, sliceBArgs("v=0\r\nm=video 9 UDP/TLS/RTP/SAVPF 96\r\na=rtpmap:96 H264/90000\r\n", sliceBVideoOnly))
		if inc := sliceBIncompatible(t, err); inc.Reason != ReasonCodec || !slices.Equal(inc.Requested, []string{"H264"}) {
			t.Fatalf("err = %+v", inc)
		}
		if sliceBCount(io, cmdVideoStreamAllocate) != 0 {
			t.Fatalf("commands = %v", io.commands())
		}
	})

	t.Run("fails typed rather than invoking the provider with no tracks, when video is declined and the camera has no microphone", func(t *testing.T) {
		m, io, _ := managerWith(fixtureState().with(func(s *cameraState) { s.microphone = nil }), nil)
		_, err := m.startStream(sliceBCtx, sliceBArgs("", func(a *startArgs) { a.videoState = trackDeclined }))
		if inc := sliceBIncompatible(t, err); inc.Reason != ReasonNoMedia {
			t.Fatalf("err = %+v", inc)
		}
		if sliceBCount(io, callSolicitOffer) != 0 {
			t.Fatal("an offer was solicited")
		}
	})

	t.Run("fails typed rather than invoking the provider with no tracks, when both video and audio are declined", func(t *testing.T) {
		m, io, _ := managerWith(fixtureState(), nil)
		_, err := m.startStream(sliceBCtx, sliceBArgs("", func(a *startArgs) { a.videoState, a.audioState = trackDeclined, trackDeclined }))
		sliceBIncompatible(t, err)
		if sliceBCount(io, callSolicitOffer) != 0 {
			t.Fatal("an offer was solicited")
		}
	})

	t.Run("fails typed for an audio hint a mic-less camera cannot serve, rather than treating it as no audio", func(t *testing.T) {
		m, _, _ := managerWith(fixtureState().with(func(s *cameraState) { s.microphone = nil }), sliceBAnswers{video: 9}.respond)
		_, err := m.startStream(sliceBCtx, sliceBArgs("", func(a *startArgs) {
			a.videoState, a.video, a.audioState, a.audio = trackDemanded, sliceBVideo(), trackDemanded, &audioHints{bitRate: 32000}
		}))
		if inc := sliceBIncompatible(t, err); inc.Reason != ReasonCapability {
			t.Fatalf("err = %+v", inc)
		}
	})

	t.Run("succeeds audio-only when video is declined and the camera has a microphone", func(t *testing.T) {
		m, _, _ := managerWith(fixtureState(), sliceBAnswers{audio: 4, solicit: 42}.respond)
		res, err := m.startStream(sliceBCtx, sliceBArgs("", func(a *startArgs) { a.videoState = trackDeclined }))
		if err != nil {
			t.Fatal(err)
		}
		if res.video != nil || res.audio == nil || res.audio.streamID != 4 {
			t.Fatalf("result = %+v", res)
		}
	})

	t.Run("gives back the stream it allocated when the provider returns no session id", func(t *testing.T) {
		m, io, _ := managerWith(fixtureState(), sliceBAnswers{video: 9}.respond)
		_, err := sliceBStart(m, "conn-1", tNode, tEndpoint)
		sliceBLocal(t, err)
		if ids := sliceBDeallocatedVideo(io); !slices.Equal(ids, []uint16{9}) {
			t.Fatalf("deallocated = %v", ids)
		}
	})

	t.Run("leaves a reused stream alone when the provider call fails", func(t *testing.T) {
		state := fixtureState().with(func(s *cameraState) { s.video = []allocatedVideoStream{sliceBContained(7, 0)} })
		m, io, _ := managerWith(state, func(r *recordedInvoke) (any, error) {
			if r.command == callProvideOffer {
				return nil, refused
			}
			return nil, nil
		})
		_, err := m.startStream(sliceBCtx, sliceBArgs(videoOffer, func(a *startArgs) {
			sliceBVideoOnly(a)
			a.video.minResolution, a.video.maxResolution = resolution{1920, 1080}, resolution{1920, 1080}
		}))
		if err == nil || err.Error() != "provider refused" {
			t.Fatalf("err = %v", err)
		}
		if sliceBCount(io, cmdVideoStreamDeallocate) != 0 {
			t.Fatalf("commands = %v", io.commands())
		}
	})

	t.Run("reports the provider failure even when giving the stream back fails too", func(t *testing.T) {
		m, _, _ := managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
			switch r.command {
			case callProvideOffer:
				return nil, refused
			case cmdVideoStreamDeallocate:
				return nil, errors.New("deallocate refused")
			}
			return sliceBAnswers{video: 9}.respond(r)
		})
		if _, err := sliceBStart(m, "conn-1", tNode, tEndpoint); err == nil || err.Error() != "provider refused" {
			t.Fatalf("err = %v", err)
		}
		if err := m.releaseStream(sliceBCtx, tTarget, StreamVideo, 9); err == nil || err.Error() != "deallocate refused" {
			t.Fatalf("releaseStream = %v", err)
		}
	})
}

// Releases and shutdown racing starts and stops (CameraStreamManagerTest.ts:4926-5112,
// matterjs-server at 0bd06f2).
func TestSliceBSessionsReleasesParityMatterjsServer(t *testing.T) {
	t.Run("ends a session that finished establishing after its connection had closed", func(t *testing.T) {
		offerGate, endGate := make(chan struct{}), make(chan struct{})
		atOffer, entered := sliceBNewSignal(), make(chan struct{}, 8)
		m, io, _ := managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
			switch r.command {
			case callProvideOffer:
				atOffer.fire()
				<-offerGate
				return wpdef.ProvideOfferResponse{WebRtcSessionId: 42}, nil
			case callEndSession:
				entered <- struct{}{}
				<-endGate
				return nil, nil
			}
			return sliceBAnswers{video: 9}.respond(r)
		})
		endings := endingsOf(m)
		starting := sliceBGo(func() (*startResult, error) { return sliceBStart(m, "conn-1", tNode, tEndpoint) })
		sliceBAwait(t, atOffer.ch, "the offer")
		releasing := sliceBGo(func() (struct{}, error) { m.releaseConnection(sliceBCtx, "conn-1"); return struct{}{}, nil })
		sliceBEventually(t, "the release to claim the start", func() bool { return sliceBClaimed(m) == 1 })
		close(offerGate)
		sliceBAwait(t, entered, "the start's EndSession")
		if got := sliceBEndedSessions(io); !slices.Equal(got, []sliceBEnd{{tNode, 42}}) {
			t.Fatalf("EndSession = %v", got)
		}
		select {
		case <-releasing:
			t.Fatal("the release returned while its EndSession ran")
		default:
		}
		close(endGate)
		start := sliceBRecv(t, starting, "the start")
		sliceBRecv(t, releasing, "the release")
		sliceBLocal(t, start.err)
		var order []string
		for _, r := range io.recorded() {
			if r.command == callEndSession || r.command == cmdVideoStreamDeallocate {
				order = append(order, r.command)
			}
		}
		sliceBSameStrings(t, order, []string{callEndSession, cmdVideoStreamDeallocate})
		if ids := sliceBDeallocatedVideo(io); !slices.Equal(ids, []uint16{9}) {
			t.Fatalf("deallocated = %v", ids)
		}
		sliceBSameEndings(t, endings(), nil)
	})

	t.Run("sends no offer for a call whose connection closed while it was still queued for the endpoint", func(t *testing.T) {
		offerGate, atOffer := make(chan struct{}), sliceBNewSignal()
		var mu sync.Mutex
		next := uint16(42)
		m, io, _ := managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
			if r.command == callProvideOffer {
				mu.Lock()
				id := next
				next++
				mu.Unlock()
				if id == 42 {
					atOffer.fire()
					<-offerGate
				}
				return wpdef.ProvideOfferResponse{WebRtcSessionId: id}, nil
			}
			return sliceBAnswers{video: 9}.respond(r)
		})
		first := sliceBGo(func() (*startResult, error) { return sliceBStart(m, "conn-1", tNode, tEndpoint) })
		sliceBAwait(t, atOffer.ch, "the first offer")
		atLock := sliceBNewProbe()
		queued := sliceBGo(func() (*startResult, error) {
			return m.startStream(atLock, sliceBArgs(videoOffer, func(a *startArgs) { a.connectionID = "conn-2"; sliceBVideoOnly(a) }))
		})
		sliceBAwait(t, atLock.reached, "the queued start at the endpoint lock")
		releasing := sliceBGo(func() (struct{}, error) { m.releaseConnection(sliceBCtx, "conn-2"); return struct{}{}, nil })
		sliceBEventually(t, "the release to claim the queued start", func() bool { return sliceBClaimed(m) == 1 })
		close(offerGate)
		if r := sliceBRecv(t, first, "the first start"); r.err != nil {
			t.Fatal(r.err)
		}
		sliceBLocal(t, sliceBRecv(t, queued, "the queued start").err)
		sliceBRecv(t, releasing, "the release")
		if sliceBCount(io, callProvideOffer) != 1 || len(sliceBEndedSessions(io)) != 0 {
			t.Fatalf("commands = %v", io.commands())
		}
	})

	t.Run("sends one EndSession when a connection release and shutdown claim the same session", func(t *testing.T) {
		endGate, entered := make(chan struct{}), make(chan struct{}, 8)
		m, io, _ := managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
			if r.command == callEndSession {
				entered <- struct{}{}
				<-endGate
				return nil, nil
			}
			return sliceBAnswers{video: 9, offer: 42}.respond(r)
		})
		sliceBMustStart(t, m, "conn-1")
		releasing := sliceBGo(func() (struct{}, error) { m.releaseConnection(sliceBCtx, "conn-1"); return struct{}{}, nil })
		sliceBAwait(t, entered, "the release's EndSession")
		joined := sliceBJoined()
		stopping := sliceBGo(func() (struct{}, error) { m.stopAll(sliceBCtx); return struct{}{}, nil })
		sliceBAwaitJoined(t, "the shutdown to join the release's EndSession", joined)
		if n := sliceBCount(io, callEndSession); n != 1 {
			t.Fatalf("%d EndSessions, want 1", n)
		}
		select {
		case <-stopping:
			t.Fatal("the shutdown returned while the EndSession ran")
		default:
		}
		close(endGate)
		sliceBRecv(t, releasing, "the release")
		sliceBRecv(t, stopping, "the shutdown")
		if n := sliceBCount(io, callEndSession); n != 1 {
			t.Fatalf("%d EndSessions, want 1", n)
		}
	})

	t.Run("sends one EndSession when camera_stop_stream races the connection's own release", func(t *testing.T) {
		endGate, entered := make(chan struct{}), make(chan struct{}, 8)
		m, io, _ := managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
			if r.command == callEndSession {
				entered <- struct{}{}
				<-endGate
				return nil, nil
			}
			return sliceBAnswers{video: 9, offer: 42}.respond(r)
		})
		sliceBMustStart(t, m, "conn-1")
		stopping := sliceBGo(func() (bool, error) { return m.stopStream(sliceBCtx, tTarget, 42, "") })
		sliceBAwait(t, entered, "the stop's EndSession")
		joined := sliceBJoined()
		releasing := sliceBGo(func() (struct{}, error) { m.releaseConnection(sliceBCtx, "conn-1"); return struct{}{}, nil })
		sliceBAwaitJoined(t, "the release to join the stop's EndSession", joined)
		if n := sliceBCount(io, callEndSession); n != 1 {
			t.Fatalf("%d EndSessions, want 1", n)
		}
		close(endGate)
		if r := sliceBRecv(t, stopping, "the stop"); r.err != nil || !r.v {
			t.Fatalf("stop = %v, %v", r.v, r.err)
		}
		sliceBRecv(t, releasing, "the release")
		if n := sliceBCount(io, callEndSession); n != 1 {
			t.Fatalf("%d EndSessions, want 1", n)
		}
	})

	t.Run("ends the remaining sessions at shutdown when one camera refuses EndSession", func(t *testing.T) {
		m, io, _ := managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
			if r.command == callEndSession && r.t.nodeID == tNode {
				return nil, errors.New("device unreachable")
			}
			return sliceBAnswers{video: 9, offer: 42}.respond(r)
		})
		sliceBMustStart(t, m, "conn-1")
		if _, err := sliceBStart(m, "conn-2", 6, tEndpoint); err != nil {
			t.Fatal(err)
		}
		m.stopAll(sliceBCtx)
		// The ends run concurrently, so their order is not the source's.
		got := sliceBEndedSessions(io)
		slices.SortFunc(got, func(a, b sliceBEnd) int { return int(a.node) - int(b.node) })
		if !slices.Equal(got, []sliceBEnd{{tNode, 42}, {6, 42}}) {
			t.Fatalf("EndSession = %v", got)
		}
	})
}

// sliceBGated is the source's gatedOffer: every offer names its id before
// the answer, and the first (42) waits for the gate.
type sliceBGated struct {
	m       *manager
	io      *fakeIO
	atOffer *sliceBSignal
	gate    chan struct{}
}

func sliceBGatedOffer() *sliceBGated {
	g := &sliceBGated{atOffer: sliceBNewSignal(), gate: make(chan struct{})}
	var mu sync.Mutex
	next := uint16(42)
	g.m, g.io, _ = managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
		if r.command == callProvideOffer {
			mu.Lock()
			id := next
			next++
			mu.Unlock()
			r.establishing(id)
			if id == 42 {
				g.atOffer.fire()
				<-g.gate
			}
			return wpdef.ProvideOfferResponse{WebRtcSessionId: id}, nil
		}
		return sliceBAnswers{video: 9}.respond(r)
	})
	return g
}

// sliceBCancelled is the source's cancelledAfterOffer: a start cancelled by
// its key once the camera made session 42, whose EndSession runs end.
type sliceBCancelled struct {
	m        *manager
	io       *fakeIO
	clk      interface{ Advance(time.Duration) }
	started  <-chan sliceBResult[*startResult]
	stopping <-chan sliceBResult[bool]
	gate     chan struct{}
}

func sliceBCancelAfterOffer(t *testing.T, end func() error) *sliceBCancelled {
	t.Helper()
	c := &sliceBCancelled{gate: make(chan struct{})}
	atOffer := sliceBNewSignal()
	m, io, clk := managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
		switch r.command {
		case callProvideOffer:
			r.establishing(42)
			atOffer.fire()
			<-c.gate
			return wpdef.ProvideOfferResponse{WebRtcSessionId: 42}, nil
		case callEndSession:
			return nil, end()
		}
		return sliceBAnswers{video: 9}.respond(r)
	})
	// The real client gives up a call whose context ended.
	m.io = sliceBCtxIO{io}
	c.m, c.io, c.clk = m, io, clk
	c.started = sliceBGo(func() (*startResult, error) { return sliceBStartKeyed(sliceBCtx, m, "conn-1", "key-a") })
	sliceBAwait(t, atOffer.ch, "the offer")
	cancelled := sliceBNewProbe()
	c.stopping = sliceBGo(func() (bool, error) { return m.stopStreamByKey(cancelled, tTarget, "conn-1", "key-a") })
	sliceBAwait(t, cancelled.reached, "the stop by key to cancel the start")
	close(c.gate)
	return c
}

// The describe "client session keys" (CameraStreamManagerTest.ts:5114-5599,
// matterjs-server at 0bd06f2).
func TestSliceBSessionsClientSessionKeysParityMatterjsServer(t *testing.T) {
	t.Run("names the key as the signalling owner's, from the offer on", func(t *testing.T) {
		g := sliceBGatedOffer()
		started := sliceBGo(func() (*startResult, error) { return sliceBStartKeyed(sliceBCtx, g.m, "conn-1", "key-a") })
		sliceBAwait(t, g.atOffer.ch, "the offer")
		want := sessionOwner{connectionID: "conn-1", clientSessionKey: "key-a"}
		if o, _ := g.m.sessions.signallingOwner(tNode, tEndpoint, 42); o != want {
			t.Fatalf("owner at the offer = %+v", o)
		}
		close(g.gate)
		if r := sliceBRecv(t, started, "the start"); r.err != nil {
			t.Fatal(r.err)
		}
		if o, _ := g.m.sessions.signallingOwner(tNode, tEndpoint, 42); o != want {
			t.Fatalf("owner once tracked = %+v", o)
		}
	})

	t.Run("names no key for a session started without one", func(t *testing.T) {
		m, _ := sliceBAllocating()
		endings := endingsOf(m)
		sliceBMustStart(t, m, "conn-1")
		if o, ok := m.sessions.signallingOwner(tNode, tEndpoint, 42); !ok || o != (sessionOwner{connectionID: "conn-1"}) {
			t.Fatalf("owner = %+v", o)
		}
		m.sessions.peerEnded(tNode, tEndpoint, 42, 0, false)
		if got := endings(); len(got) != 1 || got[0].clientSessionKey != "" {
			t.Fatalf("endings = %+v", got)
		}
	})

	t.Run("announces the end of a keyed session with its key", func(t *testing.T) {
		m, _ := sliceBAllocating()
		endings := endingsOf(m)
		if _, err := sliceBStartKeyed(sliceBCtx, m, "conn-1", "key-a"); err != nil {
			t.Fatal(err)
		}
		if _, err := m.stopStream(sliceBCtx, tTarget, 42, "conn-2"); err != nil {
			t.Fatal(err)
		}
		want := sliceBEnding(42, "conn-1", stoppedBy("conn-2"))
		want.clientSessionKey = "key-a"
		sliceBSameEndings(t, endings(), []cameraSessionEnded{want})
	})

	t.Run("refuses a key the connection already uses for a starting or open session, on any camera", func(t *testing.T) {
		g := sliceBGatedOffer()
		first := sliceBGo(func() (*startResult, error) { return sliceBStartKeyed(sliceBCtx, g.m, "conn-1", "key-a") })
		sliceBAwait(t, g.atOffer.ch, "the offer")
		if _, err := sliceBStartKeyed(sliceBCtx, g.m, "conn-1", "key-a"); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("while starting: err = %v", err)
		}
		close(g.gate)
		if r := sliceBRecv(t, first, "the start"); r.err != nil {
			t.Fatal(r.err)
		}
		_, err := g.m.startStream(sliceBCtx, sliceBArgs(videoOffer, func(a *startArgs) {
			a.t, a.clientSessionKey = target{6, tEndpoint}, "key-a"
			sliceBVideoOnly(a)
		}))
		if !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("other camera: err = %v", err)
		}
	})

	t.Run("lets another connection use the same key, and frees a key once its session ended", func(t *testing.T) {
		var mu sync.Mutex
		next := uint16(7)
		m, _, _ := managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
			if r.command == callProvideOffer {
				mu.Lock()
				defer mu.Unlock()
				id := next
				next++
				return wpdef.ProvideOfferResponse{WebRtcSessionId: id}, nil
			}
			return sliceBAnswers{video: 9}.respond(r)
		})
		for _, c := range []struct {
			conn string
			want uint16
		}{{"conn-1", 7}, {"conn-2", 8}} {
			if res, err := sliceBStartKeyed(sliceBCtx, m, c.conn, "key-a"); err != nil || res.sessionID != c.want {
				t.Fatalf("%s: %+v, %v", c.conn, res, err)
			}
		}
		if id, ok := m.clientSessionID(tTarget, "conn-1", "key-a"); !ok || id != 7 {
			t.Fatalf("conn-1 key-a = %d, %v", id, ok)
		}
		if id, ok := m.clientSessionID(tTarget, "conn-2", "key-a"); !ok || id != 8 {
			t.Fatalf("conn-2 key-a = %d, %v", id, ok)
		}
		if _, err := m.stopStream(sliceBCtx, tTarget, 7, "conn-1"); err != nil {
			t.Fatal(err)
		}
		if res, err := sliceBStartKeyed(sliceBCtx, m, "conn-1", "key-a"); err != nil || res.sessionID != 9 {
			t.Fatalf("restart: %+v, %v", res, err)
		}
	})

	t.Run("resolves a key to the session id only once the session is tracked, and only for its own connection", func(t *testing.T) {
		g := sliceBGatedOffer()
		started := sliceBGo(func() (*startResult, error) { return sliceBStartKeyed(sliceBCtx, g.m, "conn-1", "key-a") })
		sliceBAwait(t, g.atOffer.ch, "the offer")
		if id, ok := g.m.clientSessionID(tTarget, "conn-1", "key-a"); ok {
			t.Fatalf("resolved %d before it was tracked", id)
		}
		close(g.gate)
		if r := sliceBRecv(t, started, "the start"); r.err != nil {
			t.Fatal(r.err)
		}
		if id, ok := g.m.clientSessionID(tTarget, "conn-1", "key-a"); !ok || id != 42 {
			t.Fatalf("conn-1 key-a = %d, %v", id, ok)
		}
		if _, ok := g.m.clientSessionID(tTarget, "conn-2", "key-a"); ok {
			t.Fatal("another connection resolved the key")
		}
		if _, ok := g.m.clientSessionID(target{tNode, 2}, "conn-1", "key-a"); ok {
			t.Fatal("another endpoint resolved the key")
		}
	})

	t.Run("stops a tracked session by its key, only for the connection that named it", func(t *testing.T) {
		m, io := sliceBAllocating()
		endings := endingsOf(m)
		if _, err := sliceBStartKeyed(sliceBCtx, m, "conn-1", "key-a"); err != nil {
			t.Fatal(err)
		}
		for _, c := range [][2]string{{"conn-2", "key-a"}, {"conn-1", "key-b"}} {
			if ok, err := m.stopStreamByKey(sliceBCtx, tTarget, c[0], c[1]); err != nil || ok {
				t.Fatalf("%s %s: %v, %v", c[0], c[1], ok, err)
			}
		}
		if got := sliceBEndedSessions(io); len(got) != 0 {
			t.Fatalf("EndSession = %v", got)
		}
		if ok, err := m.stopStreamByKey(sliceBCtx, tTarget, "conn-1", "key-a"); err != nil || !ok {
			t.Fatalf("own key: %v, %v", ok, err)
		}
		if got := sliceBEndedSessions(io); !slices.Equal(got, []sliceBEnd{{tNode, 42}}) {
			t.Fatalf("EndSession = %v", got)
		}
		if got := endings(); len(got) != 1 || got[0].requestedBy != "conn-1" {
			t.Fatalf("endings = %+v", got)
		}
		if ok, err := m.stopStreamByKey(sliceBCtx, tTarget, "conn-1", "key-a"); err != nil || ok {
			t.Fatalf("second stop: %v, %v", ok, err)
		}
	})

	t.Run("cancels a start whose offer is in flight and ends the session once the camera answers", func(t *testing.T) {
		g := sliceBGatedOffer()
		started := sliceBGo(func() (*startResult, error) { return sliceBStartKeyed(sliceBCtx, g.m, "conn-1", "key-a") })
		sliceBAwait(t, g.atOffer.ch, "the offer")
		cancelled := sliceBNewProbe()
		stopping := sliceBGo(func() (bool, error) { return g.m.stopStreamByKey(cancelled, tTarget, "conn-1", "key-a") })
		sliceBAwait(t, cancelled.reached, "the stop by key to cancel the start")
		close(g.gate)
		le := sliceBLocal(t, sliceBRecv(t, started, "the start").err)
		if !strings.Contains(le.msg, refusalReason(refusalStopped)) {
			t.Fatalf("err = %v", le)
		}
		if r := sliceBRecv(t, stopping, "the stop"); r.err != nil || !r.v {
			t.Fatalf("stop = %v, %v", r.v, r.err)
		}
		if got := sliceBEndedSessions(g.io); !slices.Equal(got, []sliceBEnd{{tNode, 42}}) {
			t.Fatalf("EndSession = %v", got)
		}
		sliceBNoOwner(t, g.m, 42)
		if res, err := sliceBStartKeyed(sliceBCtx, g.m, "conn-1", "key-a"); err != nil || res.sessionID != 43 {
			t.Fatalf("restart: %+v, %v", res, err)
		}
	})

	t.Run("cancels a start still waiting for the endpoint before it sends an offer", func(t *testing.T) {
		g := sliceBGatedOffer()
		first := sliceBGo(func() (*startResult, error) { return sliceBStartKeyed(sliceBCtx, g.m, "conn-1", "key-a") })
		sliceBAwait(t, g.atOffer.ch, "the offer")
		atLock := sliceBNewProbe()
		queued := sliceBGo(func() (*startResult, error) { return sliceBStartKeyed(atLock, g.m, "conn-1", "key-b") })
		sliceBAwait(t, atLock.reached, "the queued start at the endpoint lock")
		before := len(g.io.recorded())
		cancelled := sliceBNewProbe()
		stopping := sliceBGo(func() (bool, error) { return g.m.stopStreamByKey(cancelled, tTarget, "conn-1", "key-b") })
		sliceBAwait(t, cancelled.reached, "the stop by key to cancel the queued start")
		close(g.gate)
		if r := sliceBRecv(t, first, "the first start"); r.err != nil {
			t.Fatal(r.err)
		}
		if le := sliceBLocal(t, sliceBRecv(t, queued, "the queued start").err); !strings.Contains(le.msg, "before the offer") {
			t.Fatalf("err = %v", le)
		}
		if r := sliceBRecv(t, stopping, "the stop"); r.err != nil || r.v {
			t.Fatalf("stop = %v, %v", r.v, r.err)
		}
		if after := g.io.recorded()[before:]; len(after) != 0 {
			t.Fatalf("the cancelled start invoked %v", g.io.commands()[before:])
		}
		if sliceBCount(g.io, callProvideOffer) != 1 || len(sliceBEndedSessions(g.io)) != 0 {
			t.Fatalf("commands = %v", g.io.commands())
		}
	})

	t.Run("reads nothing from the camera for a start cancelled while it waits for the endpoint", func(t *testing.T) {
		gate, atOffer := make(chan struct{}), sliceBNewSignal()
		m, io, _ := managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
			if r.command == callProvideOffer {
				atOffer.fire()
				<-gate
				return wpdef.ProvideOfferResponse{WebRtcSessionId: 42}, nil
			}
			return sliceBAnswers{video: 9}.respond(r)
		})
		reads := &atomic.Int32{}
		m.io = sliceBCountingIO{io, reads}
		first := sliceBGo(func() (*startResult, error) { return sliceBStartKeyed(sliceBCtx, m, "conn-1", "key-a") })
		sliceBAwait(t, atOffer.ch, "the offer")
		atLock := sliceBNewProbe()
		queued := sliceBGo(func() (*startResult, error) { return sliceBStartKeyed(atLock, m, "conn-1", "key-b") })
		sliceBAwait(t, atLock.reached, "the queued start at the endpoint lock")
		before := reads.Load()
		cancelled := sliceBNewProbe()
		stopping := sliceBGo(func() (bool, error) { return m.stopStreamByKey(cancelled, tTarget, "conn-1", "key-b") })
		sliceBAwait(t, cancelled.reached, "the stop by key to cancel the queued start")
		close(gate)
		if r := sliceBRecv(t, first, "the first start"); r.err != nil {
			t.Fatal(r.err)
		}
		sliceBLocal(t, sliceBRecv(t, queued, "the queued start").err)
		sliceBRecv(t, stopping, "the stop")
		if after := reads.Load(); after != before {
			t.Fatalf("the cancelled start read the camera %d times", after-before)
		}
	})

	t.Run("sends no offer for a start cancelled while it allocates", func(t *testing.T) {
		gate, atAllocate := make(chan struct{}), sliceBNewSignal()
		m, io, _ := managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
			if r.command == cmdVideoStreamAllocate {
				atAllocate.fire()
				<-gate
			}
			return sliceBAnswers{video: 9, offer: 42}.respond(r)
		})
		started := sliceBGo(func() (*startResult, error) { return sliceBStartKeyed(sliceBCtx, m, "conn-1", "key-a") })
		sliceBAwait(t, atAllocate.ch, "the allocate")
		cancelled := sliceBNewProbe()
		stopping := sliceBGo(func() (bool, error) { return m.stopStreamByKey(cancelled, tTarget, "conn-1", "key-a") })
		sliceBAwait(t, cancelled.reached, "the stop by key to cancel the start")
		close(gate)
		if le := sliceBLocal(t, sliceBRecv(t, started, "the start").err); !strings.Contains(le.msg, "before the offer") {
			t.Fatalf("err = %v", le)
		}
		if r := sliceBRecv(t, stopping, "the stop"); r.err != nil || r.v {
			t.Fatalf("stop = %v, %v", r.v, r.err)
		}
		if n := sliceBCount(io, callProvideOffer); n != 0 {
			t.Fatalf("%d offers sent", n)
		}
	})

	t.Run("allocates no audio stream for a start cancelled while it allocates video, and gives the video back", func(t *testing.T) {
		gate, atAllocate := make(chan struct{}), sliceBNewSignal()
		m, io, _ := managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
			if r.command == cmdVideoStreamAllocate {
				atAllocate.fire()
				<-gate
			}
			return sliceBAnswers{video: 9, audio: 4, offer: 42}.respond(r)
		})
		started := sliceBGo(func() (*startResult, error) {
			return m.startStream(sliceBCtx, sliceBArgs(videoAndAudioOffer, func(a *startArgs) {
				a.clientSessionKey = "key-a"
				a.videoState, a.video, a.audioState, a.audio = trackDemanded, sliceBVideo(), trackDemanded, &audioHints{}
			}))
		})
		sliceBAwait(t, atAllocate.ch, "the allocate")
		cancelled := sliceBNewProbe()
		stopping := sliceBGo(func() (bool, error) { return m.stopStreamByKey(cancelled, tTarget, "conn-1", "key-a") })
		sliceBAwait(t, cancelled.reached, "the stop by key to cancel the start")
		close(gate)
		if le := sliceBLocal(t, sliceBRecv(t, started, "the start").err); !strings.Contains(le.msg, "before the offer") {
			t.Fatalf("err = %v", le)
		}
		sliceBRecv(t, stopping, "the stop")
		sliceBSameStrings(t, io.commands(), []string{cmdVideoStreamAllocate, cmdVideoStreamDeallocate})
	})

	t.Run("rejects the stop when the cancelled start's EndSession fails", func(t *testing.T) {
		c := sliceBCancelAfterOffer(t, func() error { return errors.New("device unreachable") })
		sliceBLocal(t, sliceBRecv(t, c.started, "the start").err)
		if r := sliceBRecv(t, c.stopping, "the stop"); r.err == nil || r.err.Error() != "device unreachable" {
			t.Fatalf("stop = %v, %v", r.v, r.err)
		}
	})

	t.Run("tells the client a cancelled start's session could not be ended and hands it to the sweep", func(t *testing.T) {
		c := sliceBCancelAfterOffer(t, func() error { return errors.New("device unreachable") })
		le := sliceBLocal(t, sliceBRecv(t, c.started, "the start").err)
		sliceBRecv(t, c.stopping, "the stop")
		if !strings.Contains(le.msg, "but EndSession failed (device unreachable)") || strings.Contains(le.msg, "was ended") {
			t.Fatalf("err = %v", le)
		}
		if !slices.Equal(c.io.abandoned, []uint16{42}) {
			t.Fatalf("abandoned = %v", c.io.abandoned)
		}
	})

	t.Run("says a cancelled start's session was ended, and hands nothing to the sweep, when EndSession succeeds", func(t *testing.T) {
		c := sliceBCancelAfterOffer(t, func() error { return nil })
		le := sliceBLocal(t, sliceBRecv(t, c.started, "the start").err)
		sliceBRecv(t, c.stopping, "the stop")
		if !strings.Contains(le.msg, "was ended: "+refusalReason(refusalStopped)) || len(c.io.abandoned) != 0 {
			t.Fatalf("err = %v, abandoned = %v", le, c.io.abandoned)
		}
	})

	t.Run("says a cancelled start's session was gone, not ended, when the camera answers EndSession NOT_FOUND", func(t *testing.T) {
		c := sliceBCancelAfterOffer(t, func() error { return statusErr(im.StatusNotFound) })
		le := sliceBLocal(t, sliceBRecv(t, c.started, "the start").err)
		sliceBRecv(t, c.stopping, "the stop")
		if !strings.Contains(le.msg, "the camera no longer held it (EndSession answered NOT_FOUND)") || strings.Contains(le.msg, "was ended") || len(c.io.abandoned) != 0 {
			t.Fatalf("err = %v, abandoned = %v", le, c.io.abandoned)
		}
	})

	t.Run("hands a cancelled start's session to the sweep when EndSession runs past the cleanup budget", func(t *testing.T) {
		entered, never := sliceBNewSignal(), make(chan struct{})
		defer close(never)
		c := sliceBCancelAfterOffer(t, func() error { entered.fire(); <-never; return nil })
		sliceBAwait(t, entered.ch, "the EndSession")
		c.clk.Advance(deviceCleanupBudget)
		le := sliceBLocal(t, sliceBRecv(t, c.started, "the start").err)
		if !strings.Contains(le.msg, "the camera did not answer EndSession within the cleanup budget") {
			t.Fatalf("err = %v", le)
		}
		if !slices.Equal(c.io.abandoned, []uint16{42}) {
			t.Fatalf("abandoned = %v", c.io.abandoned)
		}
		stop := sliceBRecv(t, c.stopping, "the stop")
		if le := sliceBLocal(t, stop.err); !strings.Contains(le.msg, "did not answer EndSession") {
			t.Fatalf("stop = %v", stop.err)
		}
	})

	t.Run("keeps the old key off a session that got the old session's id", func(t *testing.T) {
		m, _, _ := managerWith(fixtureState(), func(r *recordedInvoke) (any, error) {
			if r.command == callProvideOffer {
				r.establishing(42)
				return wpdef.ProvideOfferResponse{WebRtcSessionId: 42}, nil
			}
			return sliceBAnswers{video: 9}.respond(r)
		})
		if _, err := sliceBStartKeyed(sliceBCtx, m, "conn-1", "key-old"); err != nil {
			t.Fatal(err)
		}
		endings := endingsOf(m)
		if _, err := sliceBStartKeyed(sliceBCtx, m, "conn-1", "key-new"); err != nil {
			t.Fatal(err)
		}
		keys := func() []string {
			es := endings()
			out := make([]string, 0, len(es))
			for _, e := range es {
				out = append(out, string(e.cause)+" "+e.clientSessionKey)
			}
			return out
		}
		sliceBSameStrings(t, keys(), []string{"camera_restarted key-old"})
		if o, _ := m.sessions.signallingOwner(tNode, tEndpoint, 42); o.clientSessionKey != "key-new" {
			t.Fatalf("owner = %+v", o)
		}
		if _, ok := m.clientSessionID(tTarget, "conn-1", "key-old"); ok {
			t.Fatal("the old key still resolves")
		}
		if id, ok := m.clientSessionID(tTarget, "conn-1", "key-new"); !ok || id != 42 {
			t.Fatalf("key-new = %d, %v", id, ok)
		}
		m.sessions.peerEnded(tNode, tEndpoint, 42, 0, false)
		sliceBSameStrings(t, keys(), []string{"camera_restarted key-old", "peer key-new"})
	})

	t.Run("keeps a session's key when its entry is reported by the camera again", func(t *testing.T) {
		m, _ := sliceBAllocating()
		endings := endingsOf(m)
		if _, err := sliceBStartKeyed(sliceBCtx, m, "conn-1", "key-a"); err != nil {
			t.Fatal(err)
		}
		sliceBReport(m, false, 42)
		sliceBReport(m, false)
		if got := endings(); len(got) != 1 || got[0].cause != endCameraDropped || got[0].clientSessionKey != "key-a" {
			t.Fatalf("endings = %+v", got)
		}
	})
}

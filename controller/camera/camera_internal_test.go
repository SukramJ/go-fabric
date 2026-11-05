// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/im"
)

// The module's own cases for what the matterjs-server parity cases do not
// reach: the quirk constants, the ranges read from the generated model, the
// typed errors, and the registry's EndSession paths, whose matterjs-server
// cases are CameraStreamManagerTest's (phase 37 wave C).

func TestCameraQuirkConstants(t *testing.T) {
	if keyFrameIntervalMS != 4000 || defaultMaxFrameRate != 30 || snapshotQuality != 90 {
		t.Errorf("key frame %d ms, default rate %d, snapshot quality %d", keyFrameIntervalMS, defaultMaxFrameRate, snapshotQuality)
	}
	if currentSessionsStreamIDsTrusted || !answerDirectionsNarrowed {
		t.Errorf("CurrentSessions stream ids trusted %v, answers narrowed %v", currentSessionsStreamIDsTrusted, answerDirectionsNarrowed)
	}
}

func TestCameraFieldRangesFromModel(t *testing.T) {
	u16, u32 := fieldRange{0, 65535}, fieldRange{1, 4294967295}
	want := cameraRanges{
		resolutionWidth: fieldRange{1, 65535}, resolutionHeight: fieldRange{1, 65535},
		minFrameRate: fieldRange{1, 65535}, maxFrameRate: fieldRange{1, 65535},
		minBitRate: u32, maxBitRate: u32, channelCount: fieldRange{1, 8}, sampleRate: u32, audioBitRate: u32,
		videoStreamID: u16, audioStreamID: u16, snapshotStreamID: u16, webRTCSessionID: u16,
	}
	if cameraFieldRanges != want {
		t.Errorf("ranges = %+v, want %+v", cameraFieldRanges, want)
	}
	if got := (iceLimits{maxServers: 10, maxTransportPolicyLength: 16, maxUrls: 10, maxURLLength: 2000, maxUsernameLength: 508, maxCredentialLength: 512, caid: fieldRange{0, 65534}}); iceServerLimits != got {
		t.Errorf("ICE limits = %+v, want %+v", iceServerLimits, got)
	}
}

func TestCameraErrors(t *testing.T) {
	status := im.StatusConstraintError
	errs := []struct {
		err  error
		text string
	}{
		{&IncompatibleError{Reason: ReasonCodec, Track: TrackVideo, Device: []string{"H264"}, Requested: []string{"AV1"}}, "No codec supported by both the camera and the caller"},
		{&IncompatibleError{Reason: ReasonBounds, Track: TrackAudio, Bound: &IncompatibleBound{Field: BoundSampleRate, Requested: "44100", Limit: "48000"}}, "Camera cannot serve the requested stream parameters"},
		{&IncompatibleError{Reason: ReasonFeature, Feature: "Snapshot"}, "Camera does not advertise the feature this request needs"},
		{&IncompatibleError{Reason: ReasonCapability}, "Camera states no capability this request can use"},
		{&IncompatibleError{Reason: ReasonOffer, Track: TrackVideo}, "The offer does not carry this track's media to the peer"},
		{&IncompatibleError{Reason: ReasonNoMedia, DeviceStatus: &status}, "The request leaves no media for the session to carry"},
		{&IncompatibleError{Reason: ReasonLevel}, "Offer states a codec level this server cannot bound a stream by"},
		{&IncompatibleError{Reason: "unknown"}, "camera stream incompatible: unknown"},
		{&ExhaustedError{Allocated: []OccupyingStream{{Kind: StreamVideo, StreamID: 3, ReferenceCount: 1}, {Kind: StreamSnapshot, StreamID: 4}, {Kind: StreamAudio, StreamID: 5}}, MaxEncoders: ptr(1), MaxPixelRate: ptr(248832000)}, "Camera has no capacity for this stream"},
		{&NotSupportedError{Missing: []uint32{0x0551}}, "Endpoint does not support camera streaming"},
		{&PrivacyError{DeviceStatus: im.StatusInvalidInState}, "Camera privacy mode is enabled"},
		{&PrivacyError{Modes: []PrivacyMode{PrivacyHard, PrivacySoftLivestream, PrivacySoftRecording}}, "Camera privacy mode is enabled: hard_mode_on, soft_livestream_mode_enabled, soft_recording_mode_enabled"},
	}
	for _, e := range errs {
		if got := e.err.Error(); got != e.text {
			t.Errorf("%T: %q, want %q", e.err, got, e.text)
		}
	}
	wrapped := fmt.Errorf("release stream 3: %w", ErrStreamInUse)
	if !errors.Is(wrapped, ErrStreamInUse) {
		t.Error("ErrStreamInUse is not found through a wrap")
	}
	var incompatible *IncompatibleError
	if !errors.As(fmt.Errorf("start: %w", errs[0].err), &incompatible) || incompatible.Reason != ReasonCodec {
		t.Error("an IncompatibleError is not found through a wrap")
	}
}

func TestProviderCommandNames(t *testing.T) {
	for _, name := range []string{"ProvideOffer", "SolicitOffer", "ProvideAnswer", "ProvideIceCandidates"} {
		if !isProviderCommandName(name) {
			t.Errorf("%s is not a provider command name", name)
		}
	}
	// EndSession belongs to stopping a stream, so the session records end
	// with it.
	if isProviderCommandName("EndSession") {
		t.Error("EndSession is a provider command name")
	}
}

func notFound() error { return &interaction.StatusError{Status: im.StatusNotFound} }

// trackedRegistry is a registry tracking session 42 of conn-1, recording
// what it announces and what it reports vanished.
func trackedRegistry(t *testing.T) (*sessionRegistry, *[]cameraSessionEnded, *[]sessionEndCause) {
	t.Helper()
	var mu sync.Mutex
	var announced []cameraSessionEnded
	var vanished []sessionEndCause
	r := newSessionRegistry(func(e cameraSessionEnded) {
		mu.Lock()
		defer mu.Unlock()
		announced = append(announced, e)
	}, func(_ managedSession, cause sessionEndCause) {
		mu.Lock()
		defer mu.Unlock()
		vanished = append(vanished, cause)
	})
	p := r.begin(regNode, regEndpoint, "conn-1", "key-a")
	r.establishing(p, 42)
	if got := r.track(p, sessionOf(42, "conn-1", "key-a", regNode, regEndpoint)); got != trackAccepted {
		t.Fatalf("track = %v", got)
	}
	r.finish(p)
	return r, &announced, &vanished
}

func TestSessionRegistryStopSharesOneEndSession(t *testing.T) {
	r, announced, _ := trackedRegistry(t)
	started, release := make(chan struct{}), make(chan struct{})
	var sends atomic.Int32
	send := func(context.Context) error {
		if sends.Add(1) == 1 {
			close(started)
		}
		<-release
		return nil
	}
	end := sessionEnd{cause: endOtherConnection, requestedBy: "conn-2"}
	type answer struct {
		ended bool
		err   error
	}
	first := make(chan answer, 1)
	go func() {
		ended, err := r.stop(context.Background(), regNode, regEndpoint, 42, end, send)
		first <- answer{ended, err}
	}()
	<-started
	// A second stop while the first EndSession is in flight joins it: it
	// sends nothing and waits for the shared answer, here until its own
	// context ends.
	gone, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.stop(gone, regNode, regEndpoint, 42, end, send); !errors.Is(err, context.Canceled) {
		t.Errorf("joined stop = %v, want the waiter's context error", err)
	}
	close(release)
	if a := <-first; !a.ended || a.err != nil {
		t.Errorf("stop = %v, %v; want true", a.ended, a.err)
	}
	if n := sends.Load(); n != 1 {
		t.Errorf("%d EndSessions sent, want 1", n)
	}
	if len(*announced) != 1 || (*announced)[0].cause != endOtherConnection || (*announced)[0].requestedBy != "conn-2" ||
		(*announced)[0].ownerID != "conn-1" || (*announced)[0].clientSessionKey != "key-a" {
		t.Errorf("announced = %+v", *announced)
	}
	if _, ok := r.get(regNode, regEndpoint, 42); ok {
		t.Error("the stopped session is still tracked")
	}
}

func TestSessionRegistryStopNotFoundAnnouncesTheCameraEnd(t *testing.T) {
	r, announced, vanished := trackedRegistry(t)
	ended, err := r.stop(context.Background(), regNode, regEndpoint, 42, sessionEnd{cause: endOtherConnection}, func(context.Context) error {
		// The camera dropped the session while the EndSession was in
		// flight; its end applies once the EndSession settled.
		r.reconcile(regNode, regEndpoint, ids(42), endCameraDropped)
		r.reconcile(regNode, regEndpoint, ids(), endCameraDropped)
		return notFound()
	})
	if ended || err != nil {
		t.Errorf("stop = %v, %v; want false, nil", ended, err)
	}
	if len(*announced) != 1 || (*announced)[0].cause != endCameraDropped {
		t.Errorf("announced = %+v, want one camera_dropped", *announced)
	}
	if len(*vanished) != 0 {
		t.Errorf("vanished = %v; the EndSession's answer closed the session", *vanished)
	}
}

func TestSessionRegistryStopFailureKeepsTheSession(t *testing.T) {
	r, announced, _ := trackedRegistry(t)
	failure := errors.New("camera: timeout")
	if ended, err := r.stop(context.Background(), regNode, regEndpoint, 42, sessionEnd{cause: endReleased}, func(context.Context) error { return failure }); ended || !errors.Is(err, failure) {
		t.Errorf("stop = %v, %v; want the failure", ended, err)
	}
	if _, ok := r.get(regNode, regEndpoint, 42); !ok || len(*announced) != 0 {
		t.Errorf("the session is gone after a failed EndSession (announced %+v)", *announced)
	}
	// A peer End during a later, successful stop is announced with the
	// stop's requester.
	if ended, err := r.stop(context.Background(), regNode, regEndpoint, 42, sessionEnd{cause: endOtherConnection, requestedBy: "conn-9"}, func(context.Context) error {
		if r.peerEnded(regNode, regEndpoint, 42, 3, true) != true {
			t.Error("peerEnded did not find the tracked session")
		}
		return nil
	}); !ended || err != nil {
		t.Errorf("stop = %v, %v", ended, err)
	}
	if len(*announced) != 1 || (*announced)[0].requestedBy != "conn-9" {
		t.Errorf("announced = %+v", *announced)
	}
}

func TestSessionRegistryEndSessionOfAnUnknownID(t *testing.T) {
	var announced []cameraSessionEnded
	r := newSessionRegistry(func(e cameraSessionEnded) { announced = append(announced, e) }, func(managedSession, sessionEndCause) {})
	// An id nothing names is sent all the same, and its end told to every
	// connection (no owner).
	if err := r.endSession(context.Background(), regNode, regEndpoint, 7, sessionEnd{cause: endOtherConnection}, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if len(announced) != 1 || announced[0].ownerID != "" || announced[0].webRTCSessionID != 7 {
		t.Errorf("announced = %+v", announced)
	}
	if ended, err := r.stop(context.Background(), regNode, regEndpoint, 8, sessionEnd{cause: endReleased}, func(context.Context) error { return notFound() }); ended || err != nil {
		t.Errorf("stop of a forgotten id = %v, %v", ended, err)
	}
	if len(announced) != 1 {
		t.Errorf("announced = %+v; a NotFound announces nothing", announced)
	}
}

func TestSessionRegistryEstablishingSessionEndedByEndSession(t *testing.T) {
	var announced []cameraSessionEnded
	r := newSessionRegistry(func(e cameraSessionEnded) { announced = append(announced, e) }, func(managedSession, sessionEndCause) {})
	p := r.begin(regNode, regEndpoint, "conn-1", "")
	r.establishing(p, 42)
	if owner, ok := r.signallingOwner(regNode, regEndpoint, 42); !ok || owner.connectionID != "conn-1" {
		t.Errorf("signallingOwner = %+v, %v", owner, ok)
	}
	// Another connection ends the session the start is still establishing:
	// the owner's start fails instead, everyone else is told.
	if err := r.endSession(context.Background(), regNode, regEndpoint, 42, sessionEnd{cause: endOtherConnection, requestedBy: "conn-2"}, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if got := r.track(p, sessionOf(42, "conn-1", "", regNode, regEndpoint)); got != refusalAlreadyEnded {
		t.Errorf("track = %v, want already_ended", got)
	}
	if len(announced) != 1 || announced[0].ownerID != "" {
		t.Errorf("announced = %+v", announced)
	}
	r.finish(p)
}

func TestSessionRegistryClaimAndForget(t *testing.T) {
	r, announced, _ := trackedRegistry(t)
	other := r.begin(regNode, regEndpoint, "conn-2", "key-a")
	// Keys are scoped to the connection: conn-2's key-a is not conn-1's.
	if got, ok := r.byClientKey(regNode, regEndpoint, "conn-2", "key-a"); !ok || got.pending != other {
		t.Errorf("byClientKey(conn-2) = %+v, %v; want conn-2's registration", got, ok)
	}
	claimed := r.claim(func(s sessionScope) bool { return s.connectionID == "conn-1" })
	if len(claimed.tracked) != 1 || claimed.tracked[0].webRTCSessionID != 42 || len(claimed.starting) != 0 {
		t.Errorf("claimed = %+v", claimed)
	}
	if got := r.claimRefusal(other); got != trackAccepted {
		t.Errorf("conn-2's start is claimed: %v", got)
	}
	r.finish(other)
	r.forgetNode(regNode)
	if len(*announced) != 1 || (*announced)[0].cause != endNodeRemoved {
		t.Errorf("announced = %+v", *announced)
	}
	if r.peerEnded(regNode, regEndpoint, 42, 0, false) {
		t.Error("a forgotten session is still named")
	}
}

func TestSessionRegistryVanishedAfterAReportDroppedIt(t *testing.T) {
	r, announced, vanished := trackedRegistry(t)
	r.reconcile(regNode, regEndpoint, ids(42), endCameraRestarted)
	r.reconcile(regNode, regEndpoint, ids(), endCameraRestarted)
	if !slices.Equal(*vanished, []sessionEndCause{endCameraRestarted}) || len(*announced) != 1 || (*announced)[0].cause != endCameraRestarted {
		t.Errorf("vanished = %v, announced = %+v", *vanished, *announced)
	}
}

func TestOutcomeWaitEndsWithItsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := newOutcome[bool]().wait(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("wait = %v, want canceled", err)
	}
	r := quietRegistry()
	p := r.begin(regNode, regEndpoint, "conn-1", "")
	if _, err := r.cancel(p)(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("cancel wait = %v, want canceled", err)
	}
	r.finish(p)
	if got, err := r.cancel(p)(context.Background()); got || err != nil {
		t.Errorf("cancel of a finished start = %v, %v", got, err)
	}
}

func TestSessionRegistryScopesByNodeAndKey(t *testing.T) {
	r, announced, _ := trackedRegistry(t)
	if s, ok := r.get(regNode, regEndpoint, 42); !ok || s.connectionID != "conn-1" || s.clientSessionKey != "key-a" {
		t.Errorf("get = %+v, %v", s, ok)
	}
	if _, ok := r.byClientKey(regNode, regEndpoint, "conn-1", "key-b"); ok {
		t.Error("found a key the connection never used")
	}
	// The same id issued on another node names another session.
	r.issued(regOtherNode, regEndpoint, 42)
	if _, ok := r.get(regNode, regEndpoint, 42); !ok || len(*announced) != 0 {
		t.Errorf("an id issued on another node ended the session (announced %+v)", *announced)
	}
	// A stop on another node's id is sent all the same: the camera ended a
	// session nothing here names, which every connection hears of.
	if ended, err := r.stop(context.Background(), regOtherNode, regEndpoint, 42, sessionEnd{cause: endReleased}, func(context.Context) error { return nil }); !ended || err != nil {
		t.Errorf("stop = %v, %v", ended, err)
	}
	if _, ok := r.get(regNode, regEndpoint, 42); !ok {
		t.Error("a stop on another node ended the session")
	}
	if len(*announced) != 1 || (*announced)[0].nodeID != regOtherNode || (*announced)[0].ownerID != "" {
		t.Errorf("announced = %+v, want one ownerless end on node 6", *announced)
	}
	if _, ok := r.get(regOtherNode, regEndpoint, 42); ok {
		t.Error("the session is tracked on another node")
	}
	// And on another endpoint of the same node, or under another id.
	r.issued(regNode, regOtherEndpoint, 42)
	r.issued(regNode, regEndpoint, 43)
	if ended, err := r.stop(context.Background(), regNode, regOtherEndpoint, 42, sessionEnd{cause: endReleased}, func(context.Context) error { return notFound() }); ended || err != nil {
		t.Errorf("stop = %v, %v", ended, err)
	}
	if _, ok := r.get(regNode, regOtherEndpoint, 42); ok {
		t.Error("the session is tracked on another endpoint")
	}
	if _, ok := r.get(regNode, regEndpoint, 43); ok {
		t.Error("the session is tracked under another id")
	}
	if _, ok := r.get(regNode, regEndpoint, 42); !ok || len(*announced) != 1 {
		t.Errorf("another endpoint's id ended the session (announced %+v)", *announced)
	}
}

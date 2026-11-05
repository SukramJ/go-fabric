// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
	"github.com/SukramJ/go-fabric/im"
)

// unreportedLeaseGrace is how long a stream this controller allocated may
// be reused before the camera reports it in Allocated*Streams, so a second
// request in that gap does not allocate a twin. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:339-343 at
// 0bd06f2.
const unreportedLeaseGrace = 10 * time.Second

// deviceCleanupBudget bounds every give-back and end a request leaves
// behind, so one silent camera cannot hold its caller. Mirrors
// matterjs-server packages/ws-controller/src/util/deviceCleanupBudget.ts:13
// at 0bd06f2.
const deviceCleanupBudget = 10 * time.Second

// The WebRTCEndReasonEnum values the controller sends (§11.4.5.2):
// UserHangup when a client stops watching, OutOfResources for a session it
// cannot track, UnknownReason for a leftover. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:234-235 at
// 0bd06f2.
const (
	endReasonUserHangup     uint8 = 2
	endReasonOutOfResources uint8 = 8
	endReasonUnknown        uint8 = 12
)

// Provenance says how a stream came to serve a request.
type Provenance string

// The three provenances, spelled as matterjs-server's wire spells them.
const (
	// ProvenanceAllocated: this request allocated the stream.
	ProvenanceAllocated Provenance = "allocated"
	// ProvenanceReused: a stream this controller allocated earlier.
	ProvenanceReused Provenance = "reused"
	// ProvenanceAdopted: a stream another controller, or an earlier run of
	// this one, allocated.
	ProvenanceAdopted Provenance = "adopted"
)

// existingStreamProvenance is the provenance of a stream the camera
// already had. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:220-222 at
// 0bd06f2.
func existingStreamProvenance(allocatedByUs bool) Provenance {
	if allocatedByUs {
		return ProvenanceReused
	}
	return ProvenanceAdopted
}

// streamLease is the record of a stream this controller handed out: the
// camera's report of it, or the allocation standing in for the report
// until it arrives. Mirrors matterjs-server
// packages/ws-controller/src/camera/cameraTypes.ts:96-121 at 0bd06f2.
type streamLease struct {
	kind     StreamKind
	streamID uint16
	// allocatedByUs: this process run allocated the stream, so it may
	// deallocate it unasked.
	allocatedByUs bool
	video         *allocatedVideoStream
	audio         *allocatedAudioStream
	snapshot      *allocatedSnapshotStream
	// shadowUntil is the instant until which the lease may stand in for a
	// device report; zero for a stream this controller did not allocate.
	shadowUntil      time.Time
	reportedByDevice bool
	// generation tells a reissued id from the lease a late give-back
	// drops.
	generation uint64
}

// StreamEvicted is a stream a request deallocated to make room. Mirrors
// matterjs-server packages/ws-controller/src/camera/cameraTypes.ts:158-164
// at 0bd06f2.
type StreamEvicted struct {
	NodeID   uint64
	Endpoint uint16
	Kind     StreamKind
	StreamID uint16
}

// manager is matterjs-server's CameraStreamManager over a deviceIO: the
// leases, the per-endpoint lock, the allocation ladders, the session
// registry. Its events are callbacks, called outside its locks. Mirrors
// matterjs-server packages/ws-controller/src/camera/CameraStreamManager.ts:618-2737
// at 0bd06f2.
type manager struct {
	io  deviceIO
	clk clock.Clock
	log *slog.Logger

	locks endpointLocks

	mu      sync.Mutex
	leases  map[target][]*streamLease
	nextGen uint64

	sessions *sessionRegistry

	// onEnded, onEvicted and onVanished are the manager's events; nil is
	// none. They are set before the manager is used.
	onEnded    func(cameraSessionEnded)
	onEvicted  func(StreamEvicted)
	onVanished func(managedSession, sessionEndCause)
}

// newManager is a manager over io.
func newManager(io deviceIO, clk clock.Clock, log *slog.Logger) *manager {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	m := &manager{io: io, clk: clock.Or(clk), log: log, leases: map[target][]*streamLease{}}
	m.locks.m = map[target]*endpointLock{}
	m.sessions = newSessionRegistry(
		func(e cameraSessionEnded) {
			if m.onEnded != nil {
				m.onEnded(e)
			}
		},
		func(s managedSession, cause sessionEndCause) {
			m.log.Info("camera.session.vanished", slog.Uint64("node_id", s.nodeID), slog.Int("endpoint", int(s.endpointID)),
				slog.Int("webrtc_session_id", int(s.webRTCSessionID)), slog.String("reason", string(cause)))
			if m.onVanished != nil {
				m.onVanished(s, cause)
			}
		},
	)
	return m
}

// endpointLock serialises the work on one endpoint; users counts the
// holders and waiters, so the map entry goes with the last of them.
type endpointLock struct {
	ch    chan struct{}
	users int
}

// endpointLocks serialises work per endpoint, so concurrent starts cannot
// both miss a reusable stream and allocate twins. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:652-671 at
// 0bd06f2.
type endpointLocks struct {
	mu sync.Mutex
	m  map[target]*endpointLock
}

// lock waits for the endpoint, or for ctx.
func (l *endpointLocks) lock(ctx context.Context, t target) (unlock func(), err error) {
	l.mu.Lock()
	el := l.m[t]
	if el == nil {
		el = &endpointLock{ch: make(chan struct{}, 1)}
		l.m[t] = el
	}
	el.users++
	l.mu.Unlock()
	release := func() {
		l.mu.Lock()
		el.users--
		if el.users == 0 {
			delete(l.m, t)
		}
		l.mu.Unlock()
	}
	select {
	case el.ch <- struct{}{}:
		return func() { <-el.ch; release() }, nil
	case <-ctx.Done():
		release()
		return nil, ctx.Err()
	}
}

// pending is the number of endpoints with work held or waiting: matterjs-server's
// pendingLockCount test hook.
func (l *endpointLocks) pending() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.m)
}

// withEndpointLock runs work under t's lock.
func withEndpointLock[T any](ctx context.Context, m *manager, t target, work func() (T, error)) (T, error) {
	unlock, err := m.locks.lock(ctx, t)
	if err != nil {
		var zero T
		return zero, err
	}
	defer unlock()
	return work()
}

// cleanupContext is the context a give-back runs under: detached from the
// request, bounded by deviceCleanupBudget on the manager's clock. Mirrors
// matterjs-server packages/ws-controller/src/util/deviceCleanupBudget.ts:15-24
// at 0bd06f2.
func (m *manager) cleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return clock.WithTimeout(context.WithoutCancel(ctx), m.clk, deviceCleanupBudget)
}

// scopedReturn is one device effect of a request and its way back.
type scopedReturn struct {
	give func(context.Context) error
	due  func(succeeded bool) bool
}

// allocationScope is the device effects of one request, each with its way
// back; settle runs what is due, newest first, so a session ends before its
// streams are deallocated (the camera refuses at ReferenceCount > 0).
// Mirrors matterjs-server packages/ws-controller/src/camera/CameraStreamManager.ts:345-399
// at 0bd06f2.
type allocationScope struct {
	mu      sync.Mutex
	returns []scopedReturn
}

// returnOnFailure gives this back unless the request succeeds.
func (s *allocationScope) returnOnFailure(give func(context.Context) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.returns = append(s.returns, scopedReturn{give: give, due: func(ok bool) bool { return !ok }})
}

// returnUnlessSpent gives this back unless the request both spends it and
// succeeds; spend records the spending.
func (s *allocationScope) returnUnlessSpent(give func(context.Context) error) (spend func()) {
	var spent bool
	var mu sync.Mutex
	s.mu.Lock()
	s.returns = append(s.returns, scopedReturn{give: give, due: func(ok bool) bool {
		mu.Lock()
		defer mu.Unlock()
		return !spent || !ok
	}})
	s.mu.Unlock()
	return func() {
		mu.Lock()
		spent = true
		mu.Unlock()
	}
}

// settle runs what is due, newest first, and waits for it at most one
// deviceCleanupBudget on the manager's clock. A chain the budget abandons
// keeps running on a context detached from the request, as an abandoned
// promise does, so a give-back the camera answers late still drops its
// lease, unless the id was leased again since (dropLeaseIfCurrent). A
// failed give-back is logged, and a stream left behind stays leased.
// Mirrors matterjs-server packages/ws-controller/src/camera/CameraStreamManager.ts:380-398
// at 0bd06f2.
func (m *manager) settle(ctx context.Context, s *allocationScope, succeeded bool) {
	s.mu.Lock()
	returns := slices.Clone(s.returns)
	s.mu.Unlock()
	var due []scopedReturn
	for i := len(returns) - 1; i >= 0; i-- {
		if returns[i].due(succeeded) {
			due = append(due, returns[i])
		}
	}
	if len(due) == 0 {
		return
	}
	detached := context.WithoutCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, r := range due {
			if err := r.give(detached); err != nil {
				m.log.Warn("camera.cleanup.failed", slog.Any("err", err))
			}
		}
	}()
	budget := m.clk.NewTimer(deviceCleanupBudget)
	defer budget.Stop()
	select {
	case <-done:
	case <-budget.C():
		m.log.Warn("camera.cleanup.abandoned", slog.Duration("budget", deviceCleanupBudget))
	}
}

// withAllocationScope runs work and, on every exit, gives back what it
// caused but may not keep. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:1073-1083 at
// 0bd06f2.
func withAllocationScope[T any](ctx context.Context, m *manager, work func(*allocationScope) (T, error)) (T, error) {
	s := &allocationScope{}
	v, err := work(s)
	m.settle(ctx, s, err == nil)
	return v, err
}

// leasesOf is t's leases, a copy.
func (m *manager) leasesOf(t target) []*streamLease {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.leases[t])
}

func (m *manager) leaseForLocked(t target, kind StreamKind, id uint16) *streamLease {
	for _, l := range m.leases[t] {
		if l.kind == kind && l.streamID == id {
			return l
		}
	}
	return nil
}

func (m *manager) putLeaseLocked(t target, lease *streamLease) {
	others := slices.DeleteFunc(slices.Clone(m.leases[t]), func(l *streamLease) bool { return l.kind == lease.kind && l.streamID == lease.streamID })
	m.leases[t] = append(others, lease)
}

// recordAllocation records a stream this controller just allocated,
// replacing any earlier lease for the id: the camera reissues freed ids.
// Mirrors matterjs-server packages/ws-controller/src/camera/CameraStreamManager.ts:716-730
// at 0bd06f2.
func (m *manager) recordAllocation(t target, lease streamLease) *streamLease {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextGen++
	lease.shadowUntil = m.clk.Now().Add(unreportedLeaseGrace)
	lease.reportedByDevice = false
	lease.generation = m.nextGen
	l := &lease
	m.putLeaseLocked(t, l)
	return l
}

// recordReuse records a stream handed out without allocating it now; an
// existing lease keeps its grace window and reported flag. Mirrors
// matterjs-server packages/ws-controller/src/camera/CameraStreamManager.ts:732-746
// at 0bd06f2.
func (m *manager) recordReuse(t target, lease streamLease) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if prev := m.leaseForLocked(t, lease.kind, lease.streamID); prev != nil {
		lease.shadowUntil, lease.reportedByDevice, lease.generation = prev.shadowUntil, prev.reportedByDevice, prev.generation
	} else {
		m.nextGen++
		lease.reportedByDevice, lease.generation = true, m.nextGen
	}
	m.putLeaseLocked(t, &lease)
	return lease.allocatedByUs
}

// deviceReports reports whether the camera's state names the stream.
func deviceReports(st *cameraState, kind StreamKind, id uint16) bool {
	switch kind {
	case StreamVideo:
		return slices.ContainsFunc(st.video, func(s allocatedVideoStream) bool { return s.videoStreamID == id })
	case StreamAudio:
		return slices.ContainsFunc(st.audio, func(s allocatedAudioStream) bool { return s.audioStreamID == id })
	default:
		return slices.ContainsFunc(st.snapshot, func(s allocatedSnapshotStream) bool { return s.snapshotStreamID == id })
	}
}

// referenceCountOf is the camera's ReferenceCount of the stream, 0 when it
// does not list it. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:412-423 at
// 0bd06f2.
func referenceCountOf(st *cameraState, kind StreamKind, id uint16) int {
	switch kind {
	case StreamVideo:
		if i := slices.IndexFunc(st.video, func(s allocatedVideoStream) bool { return s.videoStreamID == id }); i >= 0 {
			return st.video[i].referenceCount
		}
	case StreamAudio:
		if i := slices.IndexFunc(st.audio, func(s allocatedAudioStream) bool { return s.audioStreamID == id }); i >= 0 {
			return st.audio[i].referenceCount
		}
	default:
		if i := slices.IndexFunc(st.snapshot, func(s allocatedSnapshotStream) bool { return s.snapshotStreamID == id }); i >= 0 {
			return st.snapshot[i].referenceCount
		}
	}
	return 0
}

// reconcileLeases lines the leases up with the camera's report: a lease
// the camera named and then stopped naming is dropped; one it never named
// lives for the process run, since silence is no evidence the stream is
// gone. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:748-771 at
// 0bd06f2.
func (m *manager) reconcileLeases(t target, st *cameraState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.leases[t]
	if !ok {
		return
	}
	var kept []*streamLease
	for _, l := range existing {
		if deviceReports(st, l.kind, l.streamID) {
			if !l.reportedByDevice {
				c := *l
				c.reportedByDevice = true
				l = &c
			}
			kept = append(kept, l)
			continue
		}
		if l.reportedByDevice {
			continue
		}
		kept = append(kept, l)
	}
	if len(kept) == 0 {
		delete(m.leases, t)
		return
	}
	m.leases[t] = kept
}

// unreportedVideoStreams are the video streams this controller allocated
// that reported does not name yet, within unreportedLeaseGrace. Mirrors
// matterjs-server packages/ws-controller/src/camera/CameraStreamManager.ts:773-786
// at 0bd06f2.
func (m *manager) unreportedVideoStreams(t target, reported []allocatedVideoStream) []allocatedVideoStream {
	now := m.clk.Now()
	var out []allocatedVideoStream
	for _, l := range m.leasesOf(t) {
		if l.kind != StreamVideo || !now.Before(l.shadowUntil) {
			continue
		}
		if slices.ContainsFunc(reported, func(s allocatedVideoStream) bool { return s.videoStreamID == l.streamID }) {
			continue
		}
		out = append(out, *l.video)
	}
	return out
}

// unreportedSnapshotStreams is unreportedVideoStreams for snapshot
// streams. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:788-801 at
// 0bd06f2.
func (m *manager) unreportedSnapshotStreams(t target, reported []allocatedSnapshotStream) []allocatedSnapshotStream {
	now := m.clk.Now()
	var out []allocatedSnapshotStream
	for _, l := range m.leasesOf(t) {
		if l.kind != StreamSnapshot || !now.Before(l.shadowUntil) {
			continue
		}
		if slices.ContainsFunc(reported, func(s allocatedSnapshotStream) bool { return s.snapshotStreamID == l.streamID }) {
			continue
		}
		out = append(out, *l.snapshot)
	}
	return out
}

// unreportedAudioStreams is unreportedVideoStreams for audio streams.
// Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:803-816 at
// 0bd06f2.
func (m *manager) unreportedAudioStreams(t target, reported []allocatedAudioStream) []allocatedAudioStream {
	now := m.clk.Now()
	var out []allocatedAudioStream
	for _, l := range m.leasesOf(t) {
		if l.kind != StreamAudio || !now.Before(l.shadowUntil) {
			continue
		}
		if slices.ContainsFunc(reported, func(s allocatedAudioStream) bool { return s.audioStreamID == l.streamID }) {
			continue
		}
		out = append(out, *l.audio)
	}
	return out
}

// dropLease forgets the lease of the stream.
func (m *manager) dropLease(t target, kind StreamKind, id uint16) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dropLeaseLocked(t, kind, id)
}

func (m *manager) dropLeaseLocked(t target, kind StreamKind, id uint16) {
	existing, ok := m.leases[t]
	if !ok {
		return
	}
	remaining := slices.DeleteFunc(slices.Clone(existing), func(l *streamLease) bool { return l.kind == kind && l.streamID == id })
	if len(remaining) == 0 {
		delete(m.leases, t)
		return
	}
	m.leases[t] = remaining
}

// dropLeaseIfCurrent drops this lease unless the id was re-leased since: a
// give-back abandoned by the cleanup budget can land after the reissued id
// went to a later request. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:860-868 at
// 0bd06f2.
func (m *manager) dropLeaseIfCurrent(t target, lease *streamLease) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cur := m.leaseForLocked(t, lease.kind, lease.streamID); cur == nil || cur.generation != lease.generation {
		return
	}
	m.dropLeaseLocked(t, lease.kind, lease.streamID)
}

// ownsStream reports whether this controller allocated the stream.
// Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:909-914 at
// 0bd06f2.
func (m *manager) ownsStream(t target, kind StreamKind, id uint16) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	l := m.leaseForLocked(t, kind, id)
	return l != nil && l.allocatedByUs
}

// leasedEndpoints is the number of endpoints with leases: matterjs-server's
// leasedEndpointCount test hook.
func (m *manager) leasedEndpoints() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.leases)
}

// leaseReusedVideoStream leases a stream handed out as it is and reports
// whether this controller allocated it. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:883-894 at
// 0bd06f2.
func (m *manager) leaseReusedVideoStream(t target, s allocatedVideoStream) bool {
	return m.recordReuse(t, streamLease{kind: StreamVideo, streamID: s.videoStreamID, allocatedByUs: m.ownsStream(t, StreamVideo, s.videoStreamID), video: &s})
}

// leaseReusedAudioStream is leaseReusedVideoStream for audio. Mirrors
// matterjs-server packages/ws-controller/src/camera/CameraStreamManager.ts:896-907
// at 0bd06f2.
func (m *manager) leaseReusedAudioStream(t target, s allocatedAudioStream) bool {
	return m.recordReuse(t, streamLease{kind: StreamAudio, streamID: s.audioStreamID, allocatedByUs: m.ownsStream(t, StreamAudio, s.audioStreamID), audio: &s})
}

// requireState is the camera's state, or a typed failure when the endpoint
// cannot stream; it reconciles the leases. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:916-929 at
// 0bd06f2.
func (m *manager) requireState(ctx context.Context, t target) (*cameraState, error) {
	st, err := m.io.readCameraState(ctx, t)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, &NotSupportedError{Missing: m.io.missingCameraClusters(ctx, t)}
	}
	m.reconcileLeases(t, st)
	return st, nil
}

// requireStreamingState is requireState for a call that establishes a
// WebRTC session: the clusters are checked first, so a provider-less
// endpoint never gets a stream allocated. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:931-942 at
// 0bd06f2.
func (m *manager) requireStreamingState(ctx context.Context, t target) (*cameraState, error) {
	if missing := m.io.missingCameraClusters(ctx, t); len(missing) > 0 {
		return nil, &NotSupportedError{Missing: missing}
	}
	return m.requireState(ctx, t)
}

// isTyped reports whether err is one of this package's own errors, which
// the ladders pass on as they are (matterjs-server's ServerError).
func isTyped(err error) bool {
	var (
		inc *IncompatibleError
		exh *ExhaustedError
		ns  *NotSupportedError
		pr  *PrivacyError
		ae  *argumentError
		le  *localError
	)
	return errors.As(err, &inc) || errors.As(err, &exh) || errors.As(err, &ns) || errors.As(err, &pr) || errors.As(err, &ae) || errors.As(err, &le)
}

// localError is a failure of the controller's own (matterjs-server's
// error 7, SDKStackError): a broken device answer, a session given up.
type localError struct{ msg string }

func (e *localError) Error() string { return "camera: " + e.msg }

func stackError(format string, args ...any) error {
	return &localError{msg: fmt.Sprintf(format, args...)}
}

// privacyFailure is the typed privacy refusal behind a device
// INVALID_IN_STATE, or err unchanged. That status has other causes too, so
// the switches, read after the refusal, decide; a failed read keeps the
// device's refusal. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:944-967 at
// 0bd06f2.
func (m *manager) privacyFailure(ctx context.Context, t target, err error, modesOf func(privacyState) []PrivacyMode) error {
	if isTyped(err) {
		return err
	}
	status, ok := deviceStatusOf(err)
	if !ok || status != im.StatusInvalidInState {
		return err
	}
	st, rerr := m.io.readCameraState(ctx, t)
	if rerr != nil {
		return err
	}
	var modes []PrivacyMode
	if st != nil {
		modes = modesOf(st.privacy)
	}
	if len(modes) == 0 {
		return err
	}
	return &PrivacyError{Modes: modes, DeviceStatus: status}
}

// ladderReaction is the single place a Matter status becomes a ladder
// decision. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:157-172 at
// 0bd06f2.
type ladderReaction uint8

// The reactions: the device cannot serve the window, has no room, or the
// request is malformed; any other answer is passed on.
const (
	reactRethrow ladderReaction = iota
	reactUnservable
	reactCapacity
	reactIncompatible
)

func ladderReactionOf(status im.StatusCode, ok bool) ladderReaction {
	if !ok {
		return reactRethrow
	}
	switch status {
	case im.StatusDynamicConstraintError:
		return reactUnservable
	case im.StatusResourceExhausted:
		return reactCapacity
	case im.StatusConstraintError:
		return reactIncompatible
	default:
		return reactRethrow
	}
}

// videoCodecNames is the names of codecs, in order.
func videoCodecNamesOf(codecs []csdef.VideoCodecEnum) []string {
	out := make([]string, len(codecs))
	for i, c := range codecs {
		out[i] = videoCodecName(c)
	}
	return out
}

// occupying is one stream that holds capacity.
func occupying(kind StreamKind, id uint16, refs int) OccupyingStream {
	return OccupyingStream{Kind: kind, StreamID: id, ReferenceCount: refs}
}

// encoderHoldingSnapshotStreams are the snapshot streams holding a
// hardware encoder, which a capacity refusal names. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:323-331 at
// 0bd06f2.
func encoderHoldingSnapshotStreams(streams []allocatedSnapshotStream) []OccupyingStream {
	var out []OccupyingStream
	for _, s := range streams {
		if s.hardwareEncoder {
			out = append(out, occupying(StreamSnapshot, s.snapshotStreamID, s.referenceCount))
		}
	}
	return out
}

// exhausted is error 103 naming the streams in the way.
func exhausted(st *cameraState, allocated []OccupyingStream) *ExhaustedError {
	e := &ExhaustedError{Allocated: allocated}
	if st.maxConcurrentEncoders.set {
		v := int(st.maxConcurrentEncoders.value)
		e.MaxEncoders = &v
	}
	if st.maxEncodedPixelRate.set {
		v := int(st.maxEncodedPixelRate.value)
		e.MaxPixelRate = &v
	}
	return e
}

// deallocateCall is the AVSM command freeing one stream. Mirrors
// matterjs-server packages/ws-controller/src/camera/CameraStreamManager.ts:425-434
// at 0bd06f2.
func deallocateCall(t target, kind StreamKind, id uint16) deviceCall {
	call := deviceCall{target: t, cluster: clusterAVSM}
	switch kind {
	case StreamVideo:
		call.command, call.fields = cmdVideoStreamDeallocate, csdef.VideoStreamDeallocateRequest{VideoStreamId: id}
	case StreamAudio:
		call.command, call.fields = cmdAudioStreamDeallocate, csdef.AudioStreamDeallocateRequest{AudioStreamId: id}
	default:
		call.command, call.fields = cmdSnapshotStreamDeallocate, csdef.SnapshotStreamDeallocateRequest{SnapshotStreamId: id}
	}
	return call
}

// deallocate gives a stream back on a failure path: the lease goes on
// success or NOT_FOUND; any other failure is logged and keeps it, so a
// release can still reach the stream. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:2187-2200 at
// 0bd06f2.
func (m *manager) deallocate(ctx context.Context, t target, lease *streamLease) error {
	if _, err := m.io.invoke(ctx, deallocateCall(t, lease.kind, lease.streamID)); err != nil && !statusIs(err, im.StatusNotFound) {
		m.log.Warn("camera.giveback.failed", slog.String("kind", string(lease.kind)), slog.Int("stream_id", int(lease.streamID)), slog.Any("err", err))
		return nil
	}
	m.dropLeaseIfCurrent(t, lease)
	return nil
}

// sendEndSession ends a session with UserHangup; only through the
// registry's stop, which counts it as an end of the session. Mirrors
// matterjs-server packages/ws-controller/src/camera/CameraStreamManager.ts:2202-2211
// at 0bd06f2.
func (m *manager) sendEndSession(t target, id uint16) func(context.Context) error {
	return func(ctx context.Context) error {
		_, err := m.io.invoke(ctx, deviceCall{target: t, cluster: clusterProvider, command: callEndSession, fields: wpdef.EndSessionRequest{WebRtcSessionId: id, Reason: endReasonUserHangup}})
		return err
	}
}

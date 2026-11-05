// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"time"

	csdef "github.com/SukramJ/go-fabric/cluster/spec/cameraavstreammanagement"
	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
	"github.com/SukramJ/go-fabric/controller"
	"github.com/SukramJ/go-fabric/controller/client"
)

// matterEpoch is the Matter epoch, 2000-01-01T00:00:00Z (Matter §7.19.2).
var matterEpoch = time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)

// The refusals of [New].
var (
	// ErrTCPRequired: the controller opens no outgoing TCP
	// (controller.Config.TCP.Outgoing). ProvideOffer and ProvideAnswer
	// carry the Large Message quality, so their exchange requires TCP.
	ErrTCPRequired = errors.New("camera: the controller opens no outgoing TCP (controller.Config.TCP.Outgoing)")
	// ErrNoRequestor: the controller serves no WebRtcTransportRequestor
	// (controller.Config.Camera).
	ErrNoRequestor = errors.New("camera: the controller serves no camera requestor (controller.Config.Camera)")
	// ErrReportsNotInstalled: Config.Reports is nil, or was not installed
	// on the controller's configuration before controller.New.
	ErrReportsNotInstalled = errors.New("camera: Config.Reports is not installed on the controller's configuration")
	// ErrClosed: the service was closed.
	ErrClosed = errors.New("camera: the service is closed")
)

// Signal is one command a camera invoked on the controller's requestor:
// its Offer, Answer, ICECandidates or End. An Answer's SDP is narrowed to
// the directions of the offer the controller sent.
type Signal = controller.CameraSignal

// The signal kinds.
const (
	SignalOffer         = controller.CameraSignalOffer
	SignalAnswer        = controller.CameraSignalAnswer
	SignalICECandidates = controller.CameraSignalICECandidates
	SignalEnd           = controller.CameraSignalEnd
)

// Config configures a [Service].
type Config struct {
	// Reports brings the cameras' reports to the service: installed with
	// [Reports.Install] on the controller's configuration before
	// controller.New. Required.
	Reports *Reports
	// Observer receives what no call's own result reports.
	Observer Observer
	// SyncTime pushes the controller's time to a camera that refused an
	// offer for its missing clock; nil sends SetUTCTime on the camera's
	// root endpoint.
	SyncTime func(ctx context.Context, node *client.Node) error
	// Logger is the service's logger, nil discards.
	Logger *slog.Logger
}

// Observer is the camera effects no caller's own result reports; a nil
// field is not called. Each runs outside the service's locks, on the
// goroutine that decided it; it must not block.
type Observer struct {
	// SessionEnded is every end of a session this controller holds.
	SessionEnded func(SessionEnd)
	// StreamEvicted is a stream a request deallocated to make room.
	StreamEvicted func(StreamEvicted)
	// Signal is a signal of a session no [Session] of this service holds:
	// one the raw provider route ([Provider]) opened.
	Signal func(Signal)
}

// EndCause says why a session ended.
type EndCause string

// The causes, spelled as matterjs-server's camera_session_ended spells
// them (packages/ws-client/src/models/model.ts:816-836 of matterjs-server
// at 0bd06f2).
const (
	// EndPeer: the camera sent End; SessionEnd.Reason is its reason.
	EndPeer EndCause = EndCause(endPeer)
	// EndOtherOwner: another owner, or a raw EndSession, stopped it;
	// SessionEnd.RequestedBy names it.
	EndOtherOwner EndCause = EndCause(endOtherConnection)
	// EndCameraRestarted: the camera restarted, or gave the id to a new
	// session.
	EndCameraRestarted EndCause = EndCause(endCameraRestarted)
	// EndCameraDropped: the session is gone from CurrentSessions without an
	// End.
	EndCameraDropped EndCause = EndCause(endCameraDropped)
	// EndNodeRemoved: the node left the fabric.
	EndNodeRemoved EndCause = EndCause(endNodeRemoved)
	// EndReleased: this controller ended it for a released owner or at
	// Close.
	EndReleased EndCause = EndCause(endReleased)
)

// SessionEnd is the end of a session. Owner and Key are the session's
// owner and its key ("" for a session without an owner, which every
// owner hears of).
type SessionEnd struct {
	NodeID      uint64
	Endpoint    uint16
	SessionID   uint16
	Cause       EndCause
	Reason      *uint8
	RequestedBy string
	Owner       string
	Key         string
}

// Service is the controller side of Matter camera streaming over one
// controller: it picks, allocates, reuses and frees a camera's streams,
// opens the WebRTC sessions on them as the controller's one operational
// identity, keeps the controller's WebRtcTransportRequestor table in step,
// routes each camera signal to its [Session], and ends the sessions a
// camera holds for this controller that nothing here does. It is safe for
// concurrent use; [Service.Close] ends it. Mirrors matterjs-server's
// camera stream management at 0bd06f2 (packages/ws-controller/src/camera,
// packages/ws-controller/src/controller/WebRtcProviderSessions.ts).
type Service struct {
	ctrl *controller.Controller
	cfg  Config
	log  *slog.Logger
	io   *clientIO
	m    *manager
	ps   *providerSessions
	w    *sessionWatch

	life   context.Context //nolint:containedctx // the service's lifetime, cancelled by Close
	cancel context.CancelFunc
	unsub  func()

	mu       sync.Mutex
	closed   bool
	sessions map[sessionKey]*Session
	lastSeen map[watchKey][]sessionPeer
}

// New starts a service over ctrl, attaching to its requestor
// (ctrl.CameraRequestor) and its signals (ctrl.OnCameraSignal). It
// refuses a controller without a requestor ([ErrNoRequestor]), without
// outgoing TCP ([ErrTCPRequired]) and a Config whose Reports were not
// installed ([ErrReportsNotInstalled]).
func New(ctx context.Context, ctrl *controller.Controller, cfg Config) (*Service, error) {
	switch {
	case ctrl.CameraRequestor() == nil:
		return nil, ErrNoRequestor
	case !ctrl.TCP().Outgoing:
		return nil, ErrTCPRequired
	case cfg.Reports == nil || !cfg.Reports.installed():
		return nil, ErrReportsNotInstalled
	}
	log := cfg.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	life, cancel := context.WithCancel(context.WithoutCancel(ctx))
	s := &Service{ctrl: ctrl, cfg: cfg, log: log, life: life, cancel: cancel, sessions: map[sessionKey]*Session{}, lastSeen: map[watchKey][]sessionPeer{}}
	s.io = &clientIO{ctrl: ctrl, svc: s, syncTime: cfg.SyncTime, log: log}
	s.m = newManager(s.io, ctrl.Clock(), log)
	s.w = newSessionWatch(life, s.io, ctrl.Clock(), log)
	s.ps = &providerSessions{nodes: clientProviderNodes{s.io}, requestor: requestor{ctrl.CameraRequestor()}, fabricIndex: ctrl.Fabric().FabricIndex, watch: s.w, log: log, cleanup: s.m.cleanupContext, offers: map[providerKey]string{}}
	s.m.onEnded = s.sessionEnded
	s.m.onEvicted = func(e StreamEvicted) {
		if cfg.Observer.StreamEvicted != nil {
			cfg.Observer.StreamEvicted(e)
		}
	}
	s.m.onVanished = func(ms managedSession, _ sessionEndCause) {
		s.ps.removeTracked(target{ms.nodeID, ms.endpointID}, ms.webRTCSessionID)
	}
	s.unsub = ctrl.OnCameraSignal(s.onSignal)
	cfg.Reports.attach(s)
	return s, nil
}

// Close ends every session this service holds, stops its sweeps and
// detaches it from the controller.
func (s *Service) Close(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()
	s.cfg.Reports.detach(s)
	s.m.stopAll(ctx)
	s.unsub()
	s.cancel()
	s.w.wait()
	s.mu.Lock()
	open := s.sessions
	s.sessions = map[sessionKey]*Session{}
	s.mu.Unlock()
	for _, sess := range open {
		sess.box.close()
	}
	return nil
}

func (s *Service) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// targetOf is the camera endpoint a node and endpoint name.
func targetOf(node *client.Node, endpoint uint16) (target, error) {
	if node == nil {
		return target{}, invalidArguments("no node")
	}
	addr, ok := node.PeerAddress()
	if !ok {
		return target{}, invalidArguments("node %s is not commissioned", node.ID())
	}
	if endpoint > 0xFFFE {
		return target{}, invalidArguments("endpoint %d is not an endpoint number", endpoint)
	}
	return target{nodeID: addr.NodeID, endpointID: endpoint}, nil
}

// Capabilities reads what the camera endpoint states and holds. Mirrors
// matterjs-server packages/ws-controller/src/camera/CameraStreamManager.ts:969-1034
// at 0bd06f2.
func (s *Service) Capabilities(ctx context.Context, node *client.Node, endpoint uint16) (*Capabilities, error) {
	t, err := targetOf(node, endpoint)
	if err != nil {
		return nil, err
	}
	return s.m.capabilities(ctx, t)
}

// ReleaseStream deallocates one stream, passing the camera's answer on;
// a stream the camera still references is [ErrStreamInUse] (an
// [*InUseError]). Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:2702-2736 at
// 0bd06f2.
func (s *Service) ReleaseStream(ctx context.Context, node *client.Node, endpoint uint16, kind StreamKind, id uint16) error {
	t, err := targetOf(node, endpoint)
	if err != nil {
		return err
	}
	if kind != StreamVideo && kind != StreamAudio && kind != StreamSnapshot {
		return invalidArguments(`kind must be "video", "audio", or "snapshot"`)
	}
	if err := validateStreamID(kind, id); err != nil {
		return err
	}
	return s.m.releaseStream(ctx, t, kind, id)
}

// StopSession ends a session by its id, whoever opened it: false when the
// camera answered NOT_FOUND. requestedBy is the owner asking ("" for
// none); the end is announced to the session's owner naming it. Mirrors
// matterjs-server packages/ws-controller/src/camera/CameraStreamManager.ts:2243-2269
// at 0bd06f2.
func (s *Service) StopSession(ctx context.Context, node *client.Node, endpoint, id uint16, requestedBy string) (bool, error) {
	t, err := targetOf(node, endpoint)
	if err != nil {
		return false, err
	}
	return s.m.stopStream(ctx, t, id, requestedBy)
}

// StopByKey stops the session owner started with key on the node and
// endpoint; a start still running is cancelled (its StartStream fails)
// and this answers once it gave up. false when the key names no session
// of the owner, or the camera held none. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:2213-2230 at
// 0bd06f2.
func (s *Service) StopByKey(ctx context.Context, node *client.Node, endpoint uint16, owner, key string) (bool, error) {
	t, err := targetOf(node, endpoint)
	if err != nil {
		return false, err
	}
	return s.m.stopStreamByKey(ctx, t, owner, key)
}

// Release ends every session owner holds or is starting, as a host does
// when the owner goes away; only EndSession lowers a stream's
// ReferenceCount. Mirrors matterjs-server
// packages/ws-controller/src/camera/CameraStreamManager.ts:2327-2331 at
// 0bd06f2.
func (s *Service) Release(ctx context.Context, owner string) {
	s.m.releaseConnection(ctx, owner)
}

// sessionEnded announces an end: to the session's Signals, closed after
// the signals before it, and to the observer.
func (s *Service) sessionEnded(e cameraSessionEnded) {
	k := sessionKey{e.nodeID, e.endpointID, e.webRTCSessionID}
	s.ps.sessionEnded(s.ps.key(target{e.nodeID, e.endpointID}, e.webRTCSessionID))
	s.mu.Lock()
	sess := s.sessions[k]
	delete(s.sessions, k)
	s.mu.Unlock()
	if sess != nil {
		sess.box.close()
	}
	if s.cfg.Observer.SessionEnded == nil {
		return
	}
	out := SessionEnd{NodeID: e.nodeID, Endpoint: e.endpointID, SessionID: e.webRTCSessionID, Cause: EndCause(e.cause), RequestedBy: e.requestedBy, Owner: e.ownerID, Key: e.clientSessionKey}
	if e.hasReason {
		r := e.reason
		out.Reason = &r
	}
	s.cfg.Observer.SessionEnded(out)
}

// onSignal routes one requestor signal: an Answer narrowed to the offer
// sent, an End recorded as the peer's, then to the session's Signals or
// the observer.
func (s *Service) onSignal(sig Signal) {
	t := target{sig.Session.PeerNodeId, sig.Session.PeerEndpointId}
	k := sessionKey{t.nodeID, t.endpointID, sig.Session.Id}
	if sig.Kind == SignalAnswer && answerDirectionsNarrowed {
		if offer, ok := s.ps.offerOf(s.ps.key(t, sig.Session.Id)); ok {
			sig.SDP, _ = answerWithinOfferedDirections(sig.SDP, offer)
		}
	}
	s.mu.Lock()
	sess := s.sessions[k]
	s.mu.Unlock()
	if sess != nil {
		sess.box.put(sig)
	} else if s.cfg.Observer.Signal != nil {
		s.cfg.Observer.Signal(sig)
	}
	if sig.Kind == SignalEnd {
		s.ps.sessionEnded(s.ps.key(t, sig.Session.Id))
		s.m.sessions.peerEnded(t.nodeID, t.endpointID, sig.Session.Id, sig.Reason, true)
	}
}

// nodeRemoved forgets a node that left the fabric.
func (s *Service) nodeRemoved(nodeID uint64) {
	s.ps.nodeRemoved(nodeID)
	s.m.sessions.forgetNode(nodeID)
	s.mu.Lock()
	for k := range s.lastSeen {
		if k.nodeID == nodeID {
			delete(s.lastSeen, k)
		}
	}
	s.mu.Unlock()
}

// reportCompleted is a report batch of the node applied: each provider
// endpoint whose CurrentSessions differs from the last batch's is a
// change, then the watch reports and sweeps.
func (s *Service) reportCompleted(nodeID uint64) {
	for _, ep := range s.io.providerEndpoints(nodeID) {
		now, ok := s.io.currentSessions(nodeID, ep)
		if !ok {
			continue
		}
		k := watchKey{nodeID, ep}
		s.mu.Lock()
		before, seen := s.lastSeen[k]
		s.lastSeen[k] = now
		s.mu.Unlock()
		if !seen || !slices.Equal(before, now) {
			s.w.currentSessionsChanged(nodeID, ep)
		}
	}
	s.w.reportCompleted(nodeID)
}

// validateStreamID refuses a stream id outside its kind's range. Mirrors
// matterjs-server packages/ws-controller/src/camera/cameraCommands.ts:61-65
// at 0bd06f2.
func validateStreamID(kind StreamKind, id uint16) error {
	r := cameraFieldRanges.videoStreamID
	switch kind {
	case StreamVideo:
	case StreamAudio:
		r = cameraFieldRanges.audioStreamID
	case StreamSnapshot:
		r = cameraFieldRanges.snapshotStreamID
	}
	if int64(id) < r.min || int64(id) > r.max {
		return invalidArguments("stream_id must be %s", rangeText(r))
	}
	return nil
}

// usagesAllowed are the stream usages a session may ask for: Internal is
// device-only. Mirrors matterjs-server
// packages/ws-controller/src/camera/cameraCommands.ts:519-523 at 0bd06f2.
func usageAllowed(u csdef.StreamUsageEnum) bool {
	return u == csdef.StreamUsageRecording || u == csdef.StreamUsageAnalysis || u == csdef.StreamUsageLiveView
}

var _ = wpdef.ClusterID

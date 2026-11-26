// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"thingfabric.dev/go-fabric/clock"
	"thingfabric.dev/go-fabric/cluster/spec"
	wpdef "thingfabric.dev/go-fabric/cluster/spec/webrtctransportprovider"
	wrdef "thingfabric.dev/go-fabric/cluster/spec/webrtctransportrequestor"
	"thingfabric.dev/go-fabric/contract"
)

// The camera's side of the signalling: the provider invokes Offer, Answer,
// IceCandidates and End on the requestor that opened the session, at the
// endpoint the requestor named. matter.js's WebRtcTransportProviderServer
// is the empty subclass, so the route follows connectedhomeip's camera
// app, examples/camera-app/linux/src/clusters/webrtc-provider/
// webrtc-provider-manager.cpp at 6170af84: a CASE session to the
// session's peer (ScheduleOfferSend … ScheduleICECandidatesSend,
// :839-944, FindOrEstablishSession on the ScopedNodeId the request
// carried) and on it SendOfferCommand (:1093), SendAnswerCommand (:1189),
// SendICECandidatesCommand (:1221) and SendEndCommand (:1301), each an
// InvokeCommandRequest at requestArgs.originatingEndpointId.

// ProviderICECandidate is one ICE candidate the camera gathered, in the
// provider's generated type (the requestor's is [ICECandidate]).
type ProviderICECandidate = wpdef.ICECandidateStruct

// PeerConnector reaches the requestor of a session over a secure session
// (chip FindOrEstablishSession, webrtc-provider-manager.cpp:860, :887,
// :914, :941). The node attaches its peer path to every [Provider] it
// assembles: a live TCP session with the requestor, whichever side opened
// it, else a new one; [ErrNoLargeMessageTransport] when there is neither.
// The provider sets no deadline on a send; the connector's own response
// timeouts are its bound (decision 620).
type PeerConnector interface {
	ConnectPeer(ctx context.Context, fabricIndex uint8, peerNodeID uint64) (PeerSession, error)
}

// PeerSession is a secure session with the requestor's node: Invoke sends
// one command and returns the fields of the response command, nil for a
// status-only Success; another status is an error carrying it
// (im.StatusCodeError).
type PeerSession interface {
	Invoke(ctx context.Context, endpoint uint16, clusterID, commandID uint32, fields spec.Encodable) (*spec.Node, error)
}

// Errors of the provider's signals.
var (
	// ErrPayloadTooLarge is a signal whose command does not fit one message
	// of the session it would go over, refused before it is sent: over a
	// TCP connection the connection's largest message, bounded by the
	// MaxTcpMessageSize the peer stated; over UDP 1178 bytes of payload
	// (matter.js MAX_UDP_MESSAGE_SIZE less the message overhead,
	// MessageExchange.ts:435-439). Every signal is a Large Message command,
	// which a node sends over TCP only ([ErrNoLargeMessageTransport]), so
	// UDP's bound applies only to a [PeerConnector] that does not hold the
	// signals to the quality — bridge.Bridge.ConnectPeer, deprecated, whose
	// bridge.ErrPayloadTooLarge is this value: a camera host recognises it
	// with this package alone.
	ErrPayloadTooLarge = errors.New("camera: the command does not fit one message of the session to the peer")
	// ErrNoLargeMessageTransport is a signal that cannot reach the
	// requestor over a transport that carries large messages: every signal
	// is a command with the Large Message quality, which goes over TCP only
	// — no TCP session with the requestor is live and none could be
	// established (this node dials no TCP, or the requestor accepts none).
	// Nothing was sent. A requestor answers such a command on any other
	// session with InvalidTransportType (connectedhomeip CheckCommandFlags,
	// src/app/InteractionModelEngine.cpp:1875-1884 at 6170af84); its camera
	// app sends the signals with TransportPayloadCapability::kLargePayload
	// (webrtc-provider-manager.cpp:860-861, :887-888, :914-915, :941-942).
	ErrNoLargeMessageTransport = errors.New("camera: the requestor cannot be reached over a transport that carries large messages")
	// ErrNoPeerConnector is a signal on a Provider no [PeerConnector] was
	// attached to: the server runs outside a node, or before the node
	// assembled it.
	ErrNoPeerConnector = errors.New("camera: WebRtcTransportProvider has no PeerConnector")
	// ErrUnknownSession is a signal for a session id CurrentSessions does
	// not hold (chip: "WebTransport not found for sessionId",
	// CHIP_ERROR_INTERNAL).
	ErrUnknownSession = errors.New("camera: no WebRTC session of the id")
	// ErrNoICECandidates is an IceCandidates signal without a candidate
	// (chip SendICECandidatesCommand :1245-1249, CHIP_ERROR_INCORRECT_STATE).
	ErrNoICECandidates = errors.New("camera: no ICE candidates to send")
)

// AttachPeerConnector sets the connector the signals reach the requestor
// through; nil detaches it. The node attaches its own at Start and after
// each assembly; a host that runs the server outside a node attaches one
// itself.
func (p *Provider) AttachPeerConnector(c PeerConnector) {
	p.mu.Lock()
	p.peers = c
	p.mu.Unlock()
}

// Offer sends the camera's SDP offer for a session SolicitOffer opened —
// the deferred offer of a camera that was in standby, or the offer that
// follows SolicitOffer at once — as the requestor's Offer command (chip
// SendOfferCommand :1093-1121: the session id and the local description).
func (p *Provider) Offer(ctx context.Context, sessionID uint16, sdp string) error {
	return p.signal(ctx, sessionID, wrdef.CmdOffer, wrdef.OfferRequest{WebRtcSessionId: sessionID, Sdp: sdp})
}

// Answer sends the camera's SDP answer to a ProvideOffer as the
// requestor's Answer command (chip SendAnswerCommand :1189-1218).
func (p *Provider) Answer(ctx context.Context, sessionID uint16, sdp string) error {
	return p.signal(ctx, sessionID, wrdef.CmdAnswer, wrdef.AnswerRequest{WebRtcSessionId: sessionID, Sdp: sdp})
}

// IceCandidates sends the ICE candidates the camera gathered as the
// requestor's IceCandidates command (chip SendICECandidatesCommand
// :1221-1298); an empty list is [ErrNoICECandidates] and is not sent. An
// empty SDPMid goes as null (chip :1261-1268: SetNonNull for a mid, SetNull
// without one) — the requestor's schema admits no empty one.
func (p *Provider) IceCandidates(ctx context.Context, sessionID uint16, candidates []ProviderICECandidate) error {
	if len(candidates) == 0 {
		return ErrNoICECandidates
	}
	out := make([]wrdef.ICECandidateStruct, len(candidates))
	for i, c := range candidates {
		mid := c.SdpMid
		if !mid.Null && mid.Value == "" {
			mid = spec.NullOf[string]()
		}
		out[i] = wrdef.ICECandidateStruct{Candidate: c.Candidate, SdpMid: mid, SdpmLineIndex: c.SdpmLineIndex}
	}
	return p.signal(ctx, sessionID, wrdef.CmdIceCandidates, wrdef.IceCandidatesRequest{WebRtcSessionId: sessionID, IceCandidates: out})
}

// End tells the requestor the camera ended the session, with the reason
// (a WebRTCEndReasonEnum value), as the requestor's End command. The
// session leaves CurrentSessions first, before the command is sent, so
// the removal waits neither for the requestor's response nor for a
// session to the requestor, and holds when the requestor cannot be
// reached at all (decisions 654, 663) — also on a provider no
// [PeerConnector] was attached to, which sends nothing and returns
// [ErrNoPeerConnector] (decision 686). The command's error is returned,
// joined with the removal's when the table refused the shorter one. A
// session another removal takes first — the requestor's EndSession,
// RemoveSession, an eviction at a bound ([ProviderConfig.EvictIdle]) —
// is that removal's to end: End then sends nothing and returns
// [ErrUnknownSession], so the requestor is told once.
//
// chip's camera app sends End without waiting for its response
// (SendEndCommand :1301-1329, InvokeCommandRequest with logging
// callbacks) and cleans the session up at once, whatever the send
// returned (:999-1000); its CurrentSessions entry goes when the peer
// connection reports Failed or Closed (webrtc-libdatachannel.cpp:342-348
// at 6170af84 → OnConnectionStateChanged :1157-1187, RemoveSession
// :1183), not through the End's response. That
// libdatachannel's close() raises the Closed callback is inferred, not
// read. Where the requestor cannot be reached, chip's
// OnDeviceConnectionFailure (:1014-1019) only logs and keeps the session;
// this one removes it — a divergence from chip (decision 663).
func (p *Provider) End(ctx context.Context, sessionID uint16, reason uint8) error {
	peers, session, found := p.target(sessionID)
	if !found {
		return targetErr(peers, sessionID)
	}
	removed, rerr := p.removeSessionReport(contract.OriginHostSet, sessionID)
	if rerr == nil && !removed {
		// A racing removal — an eviction, RemoveSession, the requestor's
		// EndSession — took the session between the lookup and here: its
		// remover owns the end, so no second End goes out (issue #634).
		return targetErr(peers, sessionID)
	}
	var serr error
	if peers == nil {
		serr = ErrNoPeerConnector
	} else {
		serr = p.send(ctx, peers, session, wrdef.CmdEnd, wrdef.EndRequest{WebRtcSessionId: sessionID, Reason: reason})
	}
	if rerr == nil {
		return serr
	}
	return errors.Join(serr, rerr)
}

// signal invokes one requestor command for the session of the id on the
// session's peer node and fabric at its PeerEndpointId.
func (p *Provider) signal(ctx context.Context, sessionID uint16, commandID uint32, fields spec.Encodable) error {
	peers, session, err := p.signalTarget(sessionID)
	if err != nil {
		return err
	}
	// The camera's signalling is the session's activity at the call
	// (decision 704), whatever the requestor answers.
	p.touch(sessionID)
	return p.send(ctx, peers, session, commandID, fields)
}

// signalTarget returns the attached connector and the CurrentSessions
// entry of the id, both read under one lock: [ErrNoPeerConnector]
// without a connector, [ErrUnknownSession] without the entry.
func (p *Provider) signalTarget(sessionID uint16) (PeerConnector, ProviderSession, error) {
	peers, session, found := p.target(sessionID)
	if peers == nil || !found {
		return nil, ProviderSession{}, targetErr(peers, sessionID)
	}
	return peers, session, nil
}

// targetErr is a signal's refusal when the connector or the session is
// missing: [ErrNoPeerConnector] first, then [ErrUnknownSession].
func targetErr(peers PeerConnector, sessionID uint16) error {
	if peers == nil {
		return ErrNoPeerConnector
	}
	return fmt.Errorf("%w: %d", ErrUnknownSession, sessionID)
}

// target is the attached connector, nil without one, and the session of
// the id with whether CurrentSessions holds it, both read under one lock.
func (p *Provider) target(sessionID uint16) (PeerConnector, ProviderSession, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, s := range p.sessions {
		if s.Id == sessionID {
			return p.peers, s, true
		}
	}
	return p.peers, ProviderSession{}, false
}

// send reaches the session's requestor through peers and invokes the
// command at the session's PeerEndpointId. Each step logs a Debug line
// with the session, fabric, node, command and duration, so a send that
// never returns is told apart from one that failed: the line before a
// step says it began, the line after says how it ended (issue #596 B).
// The lines go to [ProviderConfig.Logger], slog.Default when nil.
func (p *Provider) send(ctx context.Context, peers PeerConnector, session ProviderSession, commandID uint32, fields spec.Encodable) error {
	log := p.logger()
	attrs := []any{
		slog.Int("webrtc_session_id", int(session.Id)), slog.Int("fabric_index", int(session.FabricIndex)),
		slog.Uint64("node_id", session.PeerNodeId), slog.Uint64("command", uint64(commandID)),
	}
	started := p.clock.Now()
	log.Debug("camera.provider.signal.connecting", attrs...)
	sess, err := peers.ConnectPeer(ctx, session.FabricIndex, session.PeerNodeId)
	log.Debug("camera.provider.signal.connected", append(attrs, slog.Duration("took", clock.Since(p.clock, started)), slog.Any("err", err))...)
	if err != nil {
		return fmt.Errorf("camera: WebRTC session %d: reach the requestor: %w", session.Id, err)
	}
	started = p.clock.Now()
	log.Debug("camera.provider.signal.invoking", attrs...)
	_, err = sess.Invoke(ctx, session.PeerEndpointId, wrdef.ClusterID, commandID, fields)
	log.Debug("camera.provider.signal.invoked", append(attrs, slog.Duration("took", clock.Since(p.clock, started)), slog.Any("err", err))...)
	if err != nil {
		return fmt.Errorf("camera: WebRTC session %d: %w", session.Id, err)
	}
	return nil
}

// logger is [ProviderConfig.Logger], slog.Default when nil.
func (p *Provider) logger() *slog.Logger {
	if p.cfg.Logger == nil {
		return slog.Default()
	}
	return p.cfg.Logger
}

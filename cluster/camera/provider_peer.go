// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"errors"
	"fmt"

	"github.com/SukramJ/go-fabric/cluster/spec"
	wpdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportprovider"
	wrdef "github.com/SukramJ/go-fabric/cluster/spec/webrtctransportrequestor"
	"github.com/SukramJ/go-fabric/contract"
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
// this node initiates (chip FindOrEstablishSession, webrtc-provider-manager
// .cpp:860, :887, :914, :941). The node attaches its bridge's
// (bridge.Bridge.ConnectPeer) to every [Provider] it assembles.
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
	// of the session it would go over, refused before it is sent: over UDP
	// 1178 bytes of payload (matter.js MAX_UDP_MESSAGE_SIZE less the message
	// overhead, MessageExchange.ts:435-439), over a TCP connection the
	// connection's largest message. connectedhomeip sends its signals with
	// TransportPayloadCapability::kLargePayload, over TCP
	// (webrtc-provider-manager.cpp:860-942 at 6170af84); this node
	// initiates CASE over UDP only and reuses the newest live session of
	// the peer, so a large signal reaches the peer when the peer came in
	// over TCP. bridge.ErrPayloadTooLarge, which the node's [PeerSession]
	// raises, is this value: a camera host recognises it with this package
	// alone.
	ErrPayloadTooLarge = errors.New("camera: the command does not fit one message of the session to the peer")
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
// (a WebRTCEndReasonEnum value), as the requestor's End command, and
// removes the session from CurrentSessions whether or not the command
// reached the peer (chip SendEndCommand :1301-1328, then CleanupSession
// :999-1000 regardless of the result). The command's error is returned.
func (p *Provider) End(ctx context.Context, sessionID uint16, reason uint8) error {
	err := p.signal(ctx, sessionID, wrdef.CmdEnd, wrdef.EndRequest{WebRtcSessionId: sessionID, Reason: reason})
	if errors.Is(err, ErrNoPeerConnector) || errors.Is(err, ErrUnknownSession) {
		return err
	}
	p.removeSession(contract.OriginHostSet, sessionID)
	return err
}

// signal invokes one requestor command for the session of the id on the
// session's peer node and fabric at its PeerEndpointId.
func (p *Provider) signal(ctx context.Context, sessionID uint16, commandID uint32, fields spec.Encodable) error {
	p.mu.Lock()
	peers := p.peers
	var session ProviderSession
	found := false
	for _, s := range p.sessions {
		if s.Id == sessionID {
			session, found = s, true
			break
		}
	}
	p.mu.Unlock()
	if peers == nil {
		return ErrNoPeerConnector
	}
	if !found {
		return fmt.Errorf("%w: %d", ErrUnknownSession, sessionID)
	}
	sess, err := peers.ConnectPeer(ctx, session.FabricIndex, session.PeerNodeId)
	if err != nil {
		return fmt.Errorf("camera: WebRTC session %d: reach the requestor: %w", sessionID, err)
	}
	if _, err := sess.Invoke(ctx, session.PeerEndpointId, wrdef.ClusterID, commandID, fields); err != nil {
		return fmt.Errorf("camera: WebRTC session %d: %w", sessionID, err)
	}
	return nil
}

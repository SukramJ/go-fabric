// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/SukramJ/go-fabric/bdx"
	"github.com/SukramJ/go-fabric/bridge"
	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/client/handshake"
	"github.com/SukramJ/go-fabric/client/interaction"
	"github.com/SukramJ/go-fabric/client/peer"
	"github.com/SukramJ/go-fabric/cluster/camera"
	mattercore "github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/schema"
)

// peerPath is the node's one path to a peer a device-side server reaches:
// the OTA requestor's provider and the camera's WebRTC requestor
// (docs/adr/0032, Shape 1). The node holds a peer's sessions in two
// stacks — the bridge's, which serves, and the client stack, which
// establishes — and the path composes them:
//
//  1. A command with the Large Message quality ([schema.CommandLargeMessage])
//     requires a session that carries large messages, a TCP one; any other
//     takes a session of any transport.
//  2. Of the live sessions with the peer that meet the requirement, in
//     either stack and whoever opened them, the one the peer sent on last
//     is used ([bridge.Bridge.LivePeerSession] and the client stack's
//     peer), never a session of another transport.
//  3. Without one, the client stack establishes a session with the
//     requirement as its required transport (peer.ConnectOptions); one
//     connection process per peer serves concurrent callers.
//
// A command goes through the stack that holds the chosen session: on the
// client stack through client/interaction, on the bridge's through
// [bridge.PeerSession]. Connecting is deferred to the first command: the
// session is chosen per command, for that command's requirement.
type peerPath struct{ n *Node }

var (
	_ mattercore.OtaProviderConnector = peerPath{}
	_ camera.PeerConnector            = peerPath{}
)

// ConnectOtaProvider implements [mattercore.OtaProviderConnector].
func (p peerPath) ConnectOtaProvider(_ context.Context, fabricIndex uint8, providerNodeID uint64) (mattercore.OtaProviderSession, error) {
	return p.to(fabricIndex, providerNodeID), nil
}

// ConnectPeer implements [camera.PeerConnector].
func (p peerPath) ConnectPeer(_ context.Context, fabricIndex uint8, peerNodeID uint64) (camera.PeerSession, error) {
	return p.to(fabricIndex, peerNodeID), nil
}

func (p peerPath) to(fabricIndex uint8, nodeID uint64) *peerPathSession {
	return &peerPathSession{n: p.n, addr: handshake.PeerAddress{FabricIndex: fabricIndex, NodeID: nodeID}}
}

// peerPathSession is the peer a [peerPath] reaches; it implements
// [mattercore.OtaProviderSession] and [camera.PeerSession].
type peerPathSession struct {
	n    *Node
	addr handshake.PeerAddress
}

// serverSession returns the bridge's live session with the peer that meets
// req when it is newer — the peer sent on it last — than every client-stack
// session that meets req; nil when the client stack's is to be used.
func (s *peerPathSession) serverSession(req bridge.PeerSessionRequirement) *bridge.PeerSession {
	srv, ok := s.n.br.LivePeerSession(s.addr.FabricIndex, s.addr.NodeID, req)
	if !ok {
		return nil
	}
	cs := s.n.existingPeerStack()
	if cs == nil {
		return srv
	}
	pr := cs.peers.Get(s.addr)
	if pr == nil {
		return srv
	}
	var cl *exchange.Session
	if req.LargeMessage {
		cl = pr.NewestSessionOn(exchange.TransportTCP)
	} else {
		cl = pr.NewestSession()
	}
	if cl == nil {
		return srv
	}
	// Both stacks stamp the peer's last message on the node's clock's
	// monotonic reading, which a wall-clock step does not move: the
	// readings compare across the stacks as each stack compares its own
	// (review II, Fable P-2).
	clMono, clHeard := cl.ActiveMonotonic()
	srvMono, srvHeard := srv.LastActivityMonotonic()
	if !srvHeard || (clHeard && srvMono <= clMono) {
		return nil
	}
	return srv
}

// clientPeer is the client stack's peer, connected for req: a live session
// that meets it, else a new one with the required transport.
//
// A Large Message command needs TCP, and the client stack dials TCP only
// when the node takes the TCP client role (Network.TCP.Outgoing,
// [Node.newClientStack]); without it the stack holds no TCP session and
// can establish none, so the signal is refused here, before a stack is
// built or anything is sent (docs/adr/0032, decision 5). Handed to the
// connection process instead, every attempt ended in [peer.ErrNoTCP] and
// was retried until the connection timeout.
func (s *peerPathSession) clientPeer(ctx context.Context, req bridge.PeerSessionRequirement) (*peer.Peer, error) {
	if req.LargeMessage && !s.n.cfg.Network.TCP.Outgoing {
		return nil, fmt.Errorf("%w: peer %d:%#x: %w", camera.ErrNoLargeMessageTransport, s.addr.FabricIndex, s.addr.NodeID, peer.ErrNoTCP)
	}
	cs, err := s.n.peerStack() //nolint:contextcheck // the stack runs for the node's lifetime, not the call's
	if err != nil {
		return nil, err
	}
	pr, err := cs.peers.For(s.addr) //nolint:contextcheck // the peer runs on the peer set's context, not the caller's
	if err != nil {
		return nil, fmt.Errorf("node: peer %d:%#x: %w", s.addr.FabricIndex, s.addr.NodeID, err)
	}
	opts := peer.ConnectOptions{}
	if req.LargeMessage {
		tcp := exchange.TransportTCP
		opts.RequiredTransport = &tcp
	}
	if _, err := pr.Connect(ctx, opts); err != nil {
		if req.LargeMessage {
			return nil, fmt.Errorf("%w: peer %d:%#x: %w", camera.ErrNoLargeMessageTransport, s.addr.FabricIndex, s.addr.NodeID, err)
		}
		return nil, fmt.Errorf("node: peer %d:%#x: %w", s.addr.FabricIndex, s.addr.NodeID, err)
	}
	return pr, nil
}

// Invoke implements [mattercore.OtaProviderSession] and
// [camera.PeerSession]: one command on the session the path chooses for
// it. A command answered with data returns its fields, one answered with
// Success nil, any other status an error carrying it
// ([im.StatusCodeError]).
func (s *peerPathSession) Invoke(ctx context.Context, endpoint uint16, clusterID, commandID uint32, fields spec.Encodable) (*spec.Node, error) {
	req := bridge.PeerSessionRequirement{LargeMessage: schema.CommandLargeMessage(clusterID, commandID)}
	if srv := s.serverSession(req); srv != nil {
		return srv.Invoke(ctx, endpoint, clusterID, commandID, fields)
	}
	pr, err := s.clientPeer(ctx, req)
	if err != nil {
		return nil, err
	}
	ia := interaction.ForPeer(pr, interaction.Config{Logger: s.n.logger, Clock: s.n.cfg.Clock})
	path := im.ConcreteCommandPath{Endpoint: endpoint, Cluster: clusterID, Command: commandID}
	res, err := ia.Invoke(ctx, path, fields, interaction.InvokeOptions{})
	if se, ok := errors.AsType[*interaction.StatusError](err); ok {
		return nil, peerStatusError{se}
	}
	if err != nil {
		return nil, err
	}
	if !res.HasFields {
		return nil, nil
	}
	n := res.Fields
	return &n, nil
}

// OpenExchange implements [mattercore.OtaProviderSession]: a new exchange
// for a BDX transfer on the newest live session with the peer, else on one
// the client stack establishes. On a client-stack session the exchange is
// an [exchange.BDXExchange], reliable over TCP.
func (s *peerPathSession) OpenExchange(ctx context.Context) (bdx.Exchange, error) {
	if srv := s.serverSession(bridge.PeerSessionRequirement{}); srv != nil {
		return srv.OpenExchange(ctx)
	}
	pr, err := s.clientPeer(ctx, bridge.PeerSessionRequirement{})
	if err != nil {
		return nil, err
	}
	sess := pr.NewestSession()
	if sess == nil {
		return nil, fmt.Errorf("node: peer %d:%#x: %w", s.addr.FabricIndex, s.addr.NodeID, exchange.ErrSessionClosed)
	}
	x, err := pr.Exchanges().Initiate(sess, bdx.ProtocolID, exchange.Options{})
	if err != nil {
		return nil, err
	}
	return exchange.NewBDXExchange(x), nil
}

// peerStatusError is a status the peer answered a command with, through
// the client stack: an [im.StatusCodeError] as the bridge's answers are.
type peerStatusError struct{ *interaction.StatusError }

// MatterStatusCode implements [im.StatusCodeError].
func (e peerStatusError) MatterStatusCode() im.StatusCode { return e.Status }

func (e peerStatusError) Unwrap() error { return e.StatusError }

// peerActivity is the server stack's session activity, for the OTA
// provider's check of a transfer's session against a query's
// ([mattercore.OTAProviderConfig].SessionActivity).
func (n *Node) peerActivity(sessionID uint16) (time.Time, bool) {
	e, err := n.sec.sessions.Get(sessionID)
	if err != nil || e == nil {
		return time.Time{}, false
	}
	return e.LastPeerActivity(), true
}

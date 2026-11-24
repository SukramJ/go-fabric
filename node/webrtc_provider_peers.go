// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"

	"thingfabric.dev/go-fabric/client/exchange"
	"thingfabric.dev/go-fabric/cluster/camera"
	"thingfabric.dev/go-fabric/endpoint"
	"thingfabric.dev/go-fabric/secure/operational"
)

// camerasOnline is what Start does for the camera surface: the requestor's
// ACL entry (webrtc_requestor_acl.go), the providers' route to their
// requestors and the end of a lost requestor's sessions
// ([Node.wireCameraPeerGone]).
func (n *Node) camerasOnline(ctx context.Context) {
	n.ensureRequestorACL(ctx)
	n.attachCameraPeers()
	n.wireCameraPeerGone()
}

// attachCameraPeers gives every WebRtcTransportProvider the kit assembled
// ([camera.Provider]) the node's peer path ([peerPath]) as its
// PeerConnector, so the camera's Offer, Answer, IceCandidates and End —
// Large Message commands — reach the requestor over a TCP session, the
// requestor's own or one the client stack establishes. Called at Start and
// after each assembly: a
// camera added to a running node gets its connector with the assembly
// that mounts it, before any controller can offer it a session.
func (n *Node) attachCameraPeers() {
	topo := n.br.Topology()
	if topo == nil {
		return
	}
	connector := peerPath{n}
	for _, ep := range topo.Endpoints {
		for _, srv := range endpoint.ClusterServers(ep) {
			if p, ok := srv.(*camera.Provider); ok {
				p.AttachPeerConnector(connector)
			}
		}
	}
}

// cameraPeerGone is the node's queue of requestors proven lost whose
// WebRTC sessions the cameras end (#590 item 2, owner decisions 611, 612):
// the pairs wait here, off the session table's goroutine, and one keeper
// goroutine ([Node.goKeeper]) works them off in order. A pair already
// waiting is not queued twice, so the queue holds at most one entry per
// lost peer.
type cameraPeerGone struct {
	mu      sync.Mutex
	pending []exchange.PeerAddress
	running bool
	// testBeforeEnd, set by tests only, runs on the queue's goroutine
	// before each provider's check of the pair.
	testBeforeEnd func(addr exchange.PeerAddress)
	// testDone, set by tests only, runs on the queue's goroutine after a
	// pair was worked off: ended is the number of providers it ended
	// sessions on, cancelled whether a live session stopped it.
	testDone func(addr exchange.PeerAddress, ended int, cancelled bool)
}

// wireCameraPeerGone has the session table report a lost peer to the
// camera queue. Called at Start.
func (n *Node) wireCameraPeerGone() {
	n.sec.sessions.SetOnPeerGone(n.bridgePeerGone)
}

// bridgePeerGone is the session table's peer-gone hook: a pair the
// peer-loss sweep left without a session — an exchange this node opened
// went unanswered (bridge closeLostPeer) — is queued; every other cause
// (the idle reaper, the peer's CloseSession, a closed TCP connection,
// ClosePeer) is no evidence that the viewer is gone and ends nothing.
func (n *Node) bridgePeerGone(fabricIndex uint8, peerNodeID uint64, cause operational.PeerGoneCause) {
	if cause != operational.PeerGoneLost {
		return
	}
	n.cameraPeerLost(exchange.PeerAddress{FabricIndex: fabricIndex, NodeID: peerNodeID})
}

// clientPeerLost is the client stack's PeerLost hook: a session given up
// because an exchange this stack opened went unanswered — the peer never
// acknowledged ([exchange.PeerUnresponsiveError]) or the response did not
// arrive ([exchange.PeerMessageMissingError]) — queues its pair. The
// peer's own CloseSession, a send the network refused and a local
// disconnect are no such evidence.
func (n *Node) clientPeerLost(s *exchange.Session, cause error) {
	var unresponsive *exchange.PeerUnresponsiveError
	var missing *exchange.PeerMessageMissingError
	if !errors.As(cause, &unresponsive) && !errors.As(cause, &missing) {
		return
	}
	if s.FabricIndex() == 0 || s.PeerNodeID() == 0 {
		return
	}
	n.cameraPeerLost(exchange.PeerAddress{FabricIndex: s.FabricIndex(), NodeID: s.PeerNodeID()})
}

// cameraPeerLost queues addr for the cameras and starts the queue's
// goroutine when none runs. It never blocks: the session table's hook and
// the client stack's call it.
func (n *Node) cameraPeerLost(addr exchange.PeerAddress) {
	if n.sec == nil {
		return
	}
	q := &n.sec.peerGone
	q.mu.Lock()
	defer q.mu.Unlock()
	if !slices.Contains(q.pending, addr) {
		q.pending = append(q.pending, addr)
	}
	if q.running {
		return
	}
	q.running = true
	if !n.goKeeper(n.drainCameraPeerGone) {
		// Stop began: nothing ends any more.
		q.running, q.pending = false, nil
	}
}

// drainCameraPeerGone works the queue off until it is empty or the node
// stops.
func (n *Node) drainCameraPeerGone() {
	q := &n.sec.peerGone
	for {
		q.mu.Lock()
		if len(q.pending) == 0 || n.life.Err() != nil {
			q.running, q.pending = false, nil
			q.mu.Unlock()
			return
		}
		addr := q.pending[0]
		q.pending = q.pending[1:]
		q.mu.Unlock()
		n.endLostPeer(addr)
	}
}

// endLostPeer ends addr's WebRTC sessions on every camera provider of the
// topology (the walk of [Node.attachCameraPeers]) — [camera.Provider.PeerGone]
// with UnknownReason — while the node holds no session with the peer in
// either stack. The check runs again immediately before each provider:
// a session the peer opened since the loss — a new CASE session — cancels
// the end for that provider and every later one; a provider already
// called keeps its result, its end is final.
func (n *Node) endLostPeer(addr exchange.PeerAddress) {
	q := &n.sec.peerGone
	ended, cancelled := 0, false
	defer func() {
		if q.testDone != nil {
			q.testDone(addr, ended, cancelled)
		}
	}()
	topo := n.br.Topology()
	if topo == nil {
		return
	}
	for _, ep := range topo.Endpoints {
		for _, srv := range endpoint.ClusterServers(ep) {
			p, ok := srv.(*camera.Provider)
			if !ok {
				continue
			}
			if q.testBeforeEnd != nil {
				q.testBeforeEnd(addr)
			}
			if n.holdsPeerSession(addr) {
				cancelled = true
				n.logger.Debug("matter.camera.peer_gone.cancelled",
					slog.Int("fabric_index", int(addr.FabricIndex)), slog.Uint64("node_id", addr.NodeID),
					slog.String("hint", "a session with the peer is live again; its WebRTC sessions stay"))
				return
			}
			if err := p.PeerGone(n.life, addr.FabricIndex, addr.NodeID, camera.EndReasonUnknown); err != nil {
				n.logger.Warn("matter.camera.peer_gone.end_failed",
					slog.Int("fabric_index", int(addr.FabricIndex)), slog.Uint64("node_id", addr.NodeID), slog.Any("err", err))
			}
			ended++
		}
	}
	if ended > 0 {
		n.logger.Info("matter.camera.peer_gone",
			slog.Int("fabric_index", int(addr.FabricIndex)), slog.Uint64("node_id", addr.NodeID),
			slog.String("hint", "the requestor is lost and no session with it is left; its WebRTC sessions ended"))
	}
}

// holdsPeerSession reports whether the node holds a live session with addr
// in either stack: an established one in the bridge's session table, or
// one of a client stack's that is not closing.
func (n *Node) holdsPeerSession(addr exchange.PeerAddress) bool {
	if len(n.sec.sessions.SessionIDsForPeer(addr.FabricIndex, addr.NodeID)) > 0 {
		return true
	}
	for _, cs := range n.clientStacks() {
		if cs.exchanges == nil {
			continue
		}
		for _, s := range cs.exchanges.SessionsFor(addr) {
			if !s.IsClosing() {
				return true
			}
		}
	}
	return false
}

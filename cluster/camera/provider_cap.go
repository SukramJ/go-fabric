// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"sync"

	"thingfabric.dev/go-fabric/cluster/spec"
	wpdef "thingfabric.dev/go-fabric/cluster/spec/webrtctransportprovider"
	wrdef "thingfabric.dev/go-fabric/cluster/spec/webrtctransportrequestor"
	"thingfabric.dev/go-fabric/im"
)

// The session bounds ([ProviderConfig.MaxSessions],
// [ProviderConfig.MaxSessionsPerFabric]; issue #590, decisions 606-609).
// The camera app's delegate holds kMaxConcurrentWebRTCSessions
// (connectedhomeip examples/camera-app/linux/src/clusters/webrtc-provider/
// webrtc-provider-manager.cpp:39 at 6170af84) and, over it, answers
// ProvideOffer RESOURCE_EXHAUSTED (:472-477) and SolicitOffer with the
// session it then ends with OutOfResources (:281-293, the End :990-999).
// Here the bound is counted by the server, directly before the session id
// and the port, so the host is not asked for a session it cannot have; a
// refused SolicitOffer's session is not entered in CurrentSessions; the
// host's rows count and are never refused (decision 608); nothing is
// evicted (decision 609).

// endReasonOutOfResources is WebRTCEndReasonEnum OutOfResources, the
// generated value (decision 633).
const endReasonOutOfResources = uint8(wrdef.WebRTCEndReasonOutOfResources)

// atSessionBound reports whether a new session of the accessing fabric
// would pass a bound: the provider's over every CurrentSessions entry, the
// fabric's over the entries of the fabric.
func (p *Provider) atSessionBound(fabric uint8) bool {
	total, perFabric := p.cfg.MaxSessions, p.cfg.MaxSessionsPerFabric
	if total == 0 && perFabric == 0 {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if total > 0 && len(p.sessions) >= total {
		return true
	}
	if perFabric == 0 {
		return false
	}
	n := 0
	for _, s := range p.sessions {
		if s.FabricIndex == fabric {
			n++
		}
	}
	return n >= perFabric
}

// errSessionBound is a new session refused at a bound.
var errSessionBound = spec.Errorf(im.StatusResourceExhausted, "WebRtcTransportProvider: at the session bound")

// endQueueBound is how many Ends of refused SolicitOffers may wait: the
// provider's bound, else the fabric's.
func (p *Provider) endQueueBound() int {
	if p.cfg.MaxSessions > 0 {
		return p.cfg.MaxSessions
	}
	return p.cfg.MaxSessionsPerFabric
}

// refuseSolicitOffer answers a SolicitOffer at a bound: a session id is
// taken and answered without a deferred offer, the port is not asked and
// CurrentSessions does not take the session, and once the response is out
// the requestor's End with OutOfResources goes to the invoker's node and
// fabric at its OriginatingEndpointID. Without a connector, or with the
// End queue full, the command is RESOURCE_EXHAUSTED instead — a flood of
// offers costs neither a goroutine nor a session to the peer per offer.
func (p *Provider) refuseSolicitOffer(ctx context.Context, req wpdef.SolicitOfferRequest, node uint64, fabric uint8, videos, audios *[]uint16) (any, error) {
	p.mu.Lock()
	peers := p.peers
	p.mu.Unlock()
	if peers == nil || !p.ends.reserve(p.endQueueBound()) {
		return nil, errSessionBound
	}
	id, err := p.generateSessionID()
	if err != nil {
		p.ends.release()
		return nil, err
	}
	end := pendingEnd{peers: peers, fabric: fabric, node: node, endpoint: req.OriginatingEndpointId, id: id}
	// The End outlives the SolicitOffer's interaction, as the camera
	// app's ScheduleEndSend does.
	detached := context.WithoutCancel(ctx)
	deliver := func() { p.ends.deliver(detached, end) }
	if !im.DeferAfterResponse(ctx, deliver) {
		deliver()
	}
	resp := wpdef.SolicitOfferResponse{WebRtcSessionId: id}
	if req.VideoStreamId != nil {
		resp.VideoStreamId = firstStream(videos)
	}
	if req.AudioStreamId != nil {
		resp.AudioStreamId = firstStream(audios)
	}
	return resp, nil
}

// pendingEnd is the End of a refused SolicitOffer.
type pendingEnd struct {
	peers    PeerConnector
	fabric   uint8
	node     uint64
	endpoint uint16
	id       uint16
}

// send invokes the requestor's End. Its outcome has no caller to go to:
// the camera app logs it (SendEndCommand :1301-1328).
func (e pendingEnd) send(ctx context.Context) {
	sess, err := e.peers.ConnectPeer(ctx, e.fabric, e.node)
	if err != nil {
		return
	}
	_, _ = sess.Invoke(ctx, e.endpoint, wrdef.ClusterID, wrdef.CmdEnd, wrdef.EndRequest{WebRtcSessionId: e.id, Reason: endReasonOutOfResources})
}

// endQueue sends the Ends of refused SolicitOffers one at a time, from at
// most one goroutine, which ends when the queue drains. held counts the
// Ends reserved and not yet sent — awaiting their response, queued or in
// flight — and is bounded by the caller.
type endQueue struct {
	mu      sync.Mutex
	held    int
	queue   []queuedEnd
	running bool
}

type queuedEnd struct {
	ctx context.Context //nolint:containedctx // the End's own context, detached from its SolicitOffer
	end pendingEnd
}

// reserve takes a slot when fewer than bound are held.
func (q *endQueue) reserve(bound int) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.held >= bound {
		return false
	}
	q.held++
	return true
}

// release gives back a slot whose End will not be sent.
func (q *endQueue) release() {
	q.mu.Lock()
	q.held--
	q.mu.Unlock()
}

// deliver queues a reserved End and starts the sender when none runs.
func (q *endQueue) deliver(ctx context.Context, end pendingEnd) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.queue = append(q.queue, queuedEnd{ctx: ctx, end: end})
	if !q.running {
		q.running = true
		go q.run()
	}
}

func (q *endQueue) run() {
	for {
		q.mu.Lock()
		if len(q.queue) == 0 {
			q.running = false
			q.mu.Unlock()
			return
		}
		next := q.queue[0]
		q.queue = q.queue[1:]
		q.mu.Unlock()
		next.end.send(next.ctx)
		q.release()
	}
}

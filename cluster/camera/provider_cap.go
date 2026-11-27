// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package camera

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"thingfabric.dev/go-fabric/cluster/spec"
	wpdef "thingfabric.dev/go-fabric/cluster/spec/webrtctransportprovider"
	wrdef "thingfabric.dev/go-fabric/cluster/spec/webrtctransportrequestor"
	"thingfabric.dev/go-fabric/contract"
	"thingfabric.dev/go-fabric/im"
)

// The session bounds ([ProviderConfig.MaxSessions],
// [ProviderConfig.MaxSessionsPerFabric]; issue #590, decisions 606-608)
// and the eviction at them ([ProviderConfig.EvictIdle],
// [ProviderConfig.EvictAcrossFabrics]; issue #634, decisions 702-711,
// which reopened 609's "nothing is evicted").
// The camera app's delegate holds kMaxConcurrentWebRTCSessions
// (connectedhomeip examples/camera-app/linux/src/clusters/webrtc-provider/
// webrtc-provider-manager.cpp:39 at 6170af84) and, over it, answers
// ProvideOffer RESOURCE_EXHAUSTED (:472-477) and SolicitOffer with the
// session it then ends with OutOfResources (:281-293, the End :990-999).
// Here the bound is counted by the server, directly before the session id
// and the port, so the host is not asked for a session it cannot have; a
// refused SolicitOffer's session is not entered in CurrentSessions; the
// host's rows count and are never refused (decision 608). The camera app
// evicts nothing; with EvictIdle set a new session at a bound takes the
// place of the accessing fabric's entry whose signalling is oldest, once
// that is EvictIdle old, and with EvictAcrossFabrics also set a fabric
// under its share takes the place of a borrowed entry of a fabric over
// its share — a divergence from the camera app, this module's own
// (notes/parity/by_design.md, BD-chip-WebRtcTransportProvider-RulesInServer).
// The evicted entry leaves the table first, under the command
// serialisation only, so a host's racing End or RemoveSession ends it
// once; then the port's EndSession and the requestor's End, both
// OutOfResources (decision 703), the End through the queue below and
// left out when it cannot be sent (decision 707). The offer then goes on
// to the port; the eviction stands whatever the port answers, and a
// fabric whose offer fails after a cross-fabric eviction has its claim on
// other fabrics' entries spent for EvictIdle (decision 713).

// endReasonOutOfResources is WebRTCEndReasonEnum OutOfResources, the
// generated value (decision 633).
const endReasonOutOfResources = uint8(wrdef.WebRTCEndReasonOutOfResources)

// evicting reports whether eviction is on: EvictIdle set beside a bound.
func (p *Provider) evicting() bool {
	return p.cfg.EvictIdle > 0 && (p.cfg.MaxSessions > 0 || p.cfg.MaxSessionsPerFabric > 0)
}

// stampLocked starts the activity of every entry new to the table at now
// and forgets the entries that left it, so an id used again starts fresh.
// The caller holds mu.
func (p *Provider) stampLocked(now time.Time) {
	if p.activity == nil {
		return
	}
	held := make(map[uint16]bool, len(p.sessions))
	for _, s := range p.sessions {
		held[s.Id] = true
		if _, ok := p.activity[s.Id]; !ok {
			p.activity[s.Id] = now
		}
	}
	for id := range p.activity {
		if !held[id] {
			delete(p.activity, id)
		}
	}
}

// touch records accepted signalling of the session of the id (decision
// 704); an id the table does not hold is left alone.
func (p *Provider) touch(id uint16) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.activity[id]; ok {
		p.activity[id] = p.clock.Now()
	}
}

// admission judges a new session of the accessing fabric against the
// bounds: admitted (false, nil); refused (true, nil); or admitted once the
// victim is evicted (false, victim). The victim, with eviction on, is
// chosen in this order: at the provider's bound with EvictAcrossFabrics, a
// fabric under its share takes the longest-idle entry of the fabrics over
// their share (decisions 708, 710); else the fabric's own longest-idle
// entry (702). An entry is taken only when its signalling is EvictIdle old
// and when its going makes room — a table over the bound through the
// host's rows is refused (706). Ties go to the entry first in the table.
func (p *Provider) admission(fabric uint8) (refuse bool, victim *ProviderSession, crossed bool) {
	total, perFabric := p.cfg.MaxSessions, p.cfg.MaxSessionsPerFabric
	if total == 0 && perFabric == 0 {
		return false, nil, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	held := map[uint8]int{}
	for _, s := range p.sessions {
		held[s.FabricIndex]++
	}
	atTotal := total > 0 && len(p.sessions) >= total
	if !atTotal && (perFabric == 0 || held[fabric] < perFabric) {
		return false, nil, false
	}
	if p.activity == nil {
		return true, nil, false
	}
	room := func(v ProviderSession) bool {
		if total > 0 && len(p.sessions)-1 >= total {
			return false
		}
		n := held[fabric]
		if v.FabricIndex == fabric {
			n--
		}
		return perFabric == 0 || n < perFabric
	}
	now := p.clock.Now()
	if atTotal && p.cfg.EvictAcrossFabrics && !p.spentLocked(fabric, now) {
		if share := p.share(held, fabric); held[fabric] < share {
			borrowed := func(s ProviderSession) bool { return s.FabricIndex != fabric && held[s.FabricIndex] > share }
			if v, ok := p.idlestLocked(now, borrowed); ok && room(v) {
				return false, &v, true
			}
		}
	}
	own := func(s ProviderSession) bool { return s.FabricIndex == fabric }
	if v, ok := p.idlestLocked(now, own); ok && room(v) {
		return false, &v, false
	}
	return true, nil, false
}

// spentLocked reports whether the fabric's claim on other fabrics'
// entries is spent (decision 713): its offer failed after it took another
// fabric's entry, less than EvictIdle ago, and no offer of it has
// succeeded since. An expired mark is dropped. The caller holds mu.
func (p *Provider) spentLocked(fabric uint8, now time.Time) bool {
	at, ok := p.spent[fabric]
	if !ok {
		return false
	}
	if now.Sub(at) >= p.cfg.EvictIdle {
		delete(p.spent, fabric)
		return false
	}
	return true
}

// settleClaim records the outcome of a new session's offer for the
// fabric's claim (decision 713): stored clears the mark; a failure — the
// port's error or the table's refusal — after a cross-fabric eviction
// (crossed) marks the claim spent, so a camera that keeps refusing the
// fabric costs other fabrics at most one entry per EvictIdle.
func (p *Provider) settleClaim(fabric uint8, crossed, stored bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch {
	case p.spent == nil:
	case stored:
		delete(p.spent, fabric)
	case crossed:
		p.spent[fabric] = p.clock.Now()
	}
}

// share is a fabric's fair share of MaxSessions (decisions 708, 709): the
// bound divided by the fabrics holding an entry, the accessing one
// included when it holds none, at least 1 and at most
// MaxSessionsPerFabric when that is set.
func (p *Provider) share(held map[uint8]int, fabric uint8) int {
	n := len(held)
	if held[fabric] == 0 {
		n++
	}
	share := max(1, p.cfg.MaxSessions/n)
	if p.cfg.MaxSessionsPerFabric > 0 {
		share = min(share, p.cfg.MaxSessionsPerFabric)
	}
	return share
}

// idlestLocked is the entry of which the oldest activity among those
// match admits, first in the table on a tie, when that activity is at
// least EvictIdle old. The caller holds mu.
func (p *Provider) idlestLocked(now time.Time, match func(ProviderSession) bool) (ProviderSession, bool) {
	var (
		victim ProviderSession
		oldest time.Time
		found  bool
	)
	for _, s := range p.sessions {
		at, ok := p.activity[s.Id]
		if !ok || !match(s) || (found && !at.Before(oldest)) {
			continue
		}
		victim, oldest, found = s, at, true
	}
	if !found || now.Sub(oldest) < p.cfg.EvictIdle {
		return ProviderSession{}, false
	}
	return victim, true
}

// admitNew admits a new session of the accessing fabric, evicting the
// victim [Provider.admission] names; refused reports a refusal at the
// bound, crossed an eviction of another fabric's entry. It runs under the command serialisation. A victim a host's End
// or RemoveSession removed meanwhile is not ended again, and the bound is
// judged once more; a second such race refuses.
func (p *Provider) admitNew(ctx context.Context, fabric uint8) (refused, crossed bool, err error) {
	for range 2 {
		refuse, victim, cross := p.admission(fabric)
		if refuse || victim == nil {
			return refuse, false, nil
		}
		if p.beforeEvictForTests != nil {
			p.beforeEvictForTests(*victim)
		}
		removed, err := p.evict(ctx, *victim)
		if err != nil || removed {
			return false, cross && removed, err
		}
	}
	return true, false, nil
}

// evict ends the victim to make room for a new session: it leaves
// CurrentSessions first — removed false when a racing removal took it, and
// then nothing else happens — then the port's EndSession with
// OutOfResources, whose error is logged and does not stop the offer (the
// entry is gone whatever the port answers, as EndSession's), then the
// requestor's End with OutOfResources through the End queue, detached from
// the offer's context; without a PeerConnector, or with the queue full,
// the eviction stands without an End (decision 707). Never under mu or pub
// across a call out.
func (p *Provider) evict(ctx context.Context, victim ProviderSession) (removed bool, err error) {
	removed, err = p.removeSessionReport(contract.OriginCommand, victim.Id)
	if err != nil {
		return false, spec.Errorf(im.StatusFailure, "%v", err)
	}
	if !removed {
		return false, nil
	}
	log := p.logger()
	attrs := []any{
		slog.Int("webrtc_session_id", int(victim.Id)), slog.Int("fabric_index", int(victim.FabricIndex)),
		slog.Uint64("node_id", victim.PeerNodeId),
	}
	if perr := p.port.EndSession(ctx, victim.Id, endReasonOutOfResources); perr != nil {
		log.Warn("camera.provider.evict.port_failed", append(attrs, slog.Any("err", perr))...)
	}
	p.mu.Lock()
	peers := p.peers
	p.mu.Unlock()
	if peers == nil || !p.ends.reserve(p.endQueueBound()) {
		log.Debug("camera.provider.evict.no_end", attrs...)
		return true, nil
	}
	log.Debug("camera.provider.evicted", attrs...)
	p.ends.deliver(context.WithoutCancel(ctx), pendingEnd{
		peers: peers, fabric: victim.FabricIndex, node: victim.PeerNodeId, endpoint: victim.PeerEndpointId, id: victim.Id,
	})
	return true, nil
}

// errSessionBound is a new session refused at a bound.
var errSessionBound = spec.Errorf(im.StatusResourceExhausted, "WebRtcTransportProvider: at the session bound")

// endQueueBound is how many Ends of refused SolicitOffers and evicted
// sessions may wait: the provider's bound, else the fabric's.
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

// pendingEnd is the End of a refused SolicitOffer or an evicted session.
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

// endQueue sends the Ends of refused SolicitOffers and evicted sessions
// one at a time, from at most one goroutine, which ends when the queue
// drains. held counts the Ends reserved and not yet sent — awaiting
// their response, queued or in flight — and is bounded by the caller.
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

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"slices"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/transport/message"
)

// buildInitialReport assembles the initial ReportData for a Subscribe-Initial
// whole: the drained form of [Bridge.newPrimingSource] and
// [Bridge.primingEvents]. Returns the assembled report with
// HasSubscription=false plus the count of matched attribute/event paths —
// taken BEFORE DataVersionFilter suppression, so a caller can reject a
// zero-matching Subscribe (ServerSubscription.ts:610-614) without
// misfiring on an all-cached re-subscribe. The Subscribe and the
// re-establishment stream the same reports chunk by chunk instead
// (review II, decision 44).
func (b *Bridge) buildInitialReport(
	subCtx context.Context,
	dispatcher im.Dispatcher,
	req im.SubscribeRequest,
) (report im.ReportData, matchedPaths int) {
	src := b.newPrimingSource(subCtx, dispatcher, req)
	for rep, ok := src.next(); ok; rep, ok = src.next() {
		report.Reports = append(report.Reports, rep)
	}
	matched := src.matched
	if len(req.EventRequests) > 0 {
		matched += b.primingEventPaths(subCtx, dispatcher, req)
		report.EventReports = b.primingEvents(subCtx, dispatcher, req)
	}
	return report, matched
}

// primingSource generates the attribute reports of a priming report one
// at a time, for the subject subCtx carries: each requested path in
// request order, expanded by an [im.AttributeExpansion]; each result the
// subject may not read dropped (the gate of
// [Bridge.readAuthorizedResults]: without it the Subscribe-Initial would
// leak fabric-sensitive attributes — ACL, NOCs — to a View-only or
// ACE-less subject, the same bypass reportSubscription closes for ongoing
// reports); each cluster the controller holds at its current DataVersion
// left out.
//
// Reports go out in ascending (endpoint, cluster, attribute) order per
// path. Apple Home's MTRDevice processes the wildcard Subscribe-Initial in
// stream order and the HAP-Service-Mapper's topology builder needs
// Descriptor (0x001D) cached BEFORE the cluster surfaces it references —
// otherwise `_attributeValueDictionaryForAttributePath` reports "PartsList
// absent from cache" and HAP-Build aborts with HAPErrorDomain Code=14 ~5s
// after Subscribe-Initial. matter.js emits its reports in the same
// ascending order (`packages/protocol/src/interaction/InteractionServer.ts:
// generateAttributeListReport` walks endpoints sorted then clusters sorted
// then attributes sorted), and the request's paths one after the other, a
// producer each (packages/protocol/src/action/server/
// AttributeReadResponse.ts:88-100 at db021da0) — so two paths are no longer
// merged into one sorted run: the second path's reports follow the first's.
//
// Mirrors matter.js InteractionServer.ts:startReadInteraction for the
// Subscribe path (attribute reading + event merging), generated as
// ServerSubscription.ts #processAttributesAndEventsReport pulls
// AttributeReadResponse.process.
type primingSource struct {
	b       *Bridge
	exp     *im.AttributeExpansion
	allow   func(im.ReadResult) bool
	filters []im.DataVersionFilter
	// suppressStatuses leaves every status report out: a re-established
	// subscription reports no per-path error again.
	suppressStatuses bool

	// matched counts the authorized results of attributes that exist
	// (matter.js AttributeReadResponse.counts.existent,
	// packages/protocol/src/action/server/AttributeReadResponse.ts:
	// 125-131, the values and the version-filtered ones, read by
	// ServerSubscription.ts:674, :707-711): a status (unsupported
	// endpoint, cluster or attribute) does not count.
	matched int
	// reports counts the reports handed out.
	reports int
}

// newPrimingSource returns the priming reports of req's attribute paths
// against dispatcher for the subject subCtx carries (fabric, subject,
// auth mode, fabric filter).
func (b *Bridge) newPrimingSource(subCtx context.Context, dispatcher im.Dispatcher, req im.SubscribeRequest) *primingSource {
	allow := readResultAuthorizer(subCtx, dispatcher)
	if allow == nil {
		allow = func(im.ReadResult) bool { return true }
	}
	return &primingSource{
		b: b,
		// A wildcard path's WildcardPathFlags skip their paths as in a
		// read (AttributeReadResponse.ts:149-197).
		exp:     im.NewAttributeExpansion(im.WithElementQualities(subCtx, specElementQualities{}), dispatcher, req.AttributeRequests).Ascending(),
		allow:   allow,
		filters: req.DataVersionFilters,
	}
}

// result returns the next result the subject may read.
func (p *primingSource) result() (im.ReadResult, bool) {
	for {
		_, rr, ok := p.exp.Next()
		if !ok {
			return im.ReadResult{}, false
		}
		// A path naming another node is refused before any access check
		// (AttributeReadResponse.ts:205-208 at db021da0).
		if rr.Status != im.StatusUnsupportedNode && !p.allow(rr) {
			continue
		}
		if rr.Status == im.StatusSuccess {
			p.matched++
		}
		return rr, true
	}
}

// matches reports whether any requested attribute path resolves to an
// attribute that exists and the subject may read; it stops at the first.
// It consumes the source: a Subscribe asks a source of its own.
func (p *primingSource) matches() bool {
	for {
		rr, ok := p.result()
		if !ok {
			return false
		}
		if rr.Status == im.StatusSuccess {
			return true
		}
	}
}

// next returns the next priming report; ok is false once the paths are
// exhausted.
func (p *primingSource) next() (im.AttributeReport, bool) {
	for {
		rr, ok := p.result()
		if !ok {
			return im.AttributeReport{}, false
		}
		// DataVersionFilter evaluation: skip attributes whose cluster
		// DataVersion matches the controller's cached version.
		// Matter §10.6.5 — the controller infers "no change since
		// cached" from the absence of reports for that cluster.
		// Mirrors matter.js InteractionServer.ts:startReadInteraction
		// DataVersionFilter evaluation.
		//
		// Sentinel guard: clusters without per-instance tracking
		// return rr.DataVersion=0 and the wire encoder substitutes
		// the §10.6.1.4 floor of 1. A controller that cached the
		// sentinel-1 and replays it on re-pair must NOT cause the
		// entire cluster to be omitted from the Subscribe-Initial —
		// the bridged-endpoint clusters would silently disappear
		// from Apple's MTRDevice ClusterStateCache and
		// `endpointDeviceTypes` would collapse to `{0=(22)}`.
		// Guard at >1 keeps the sentinel from ever matching.
		if len(p.filters) > 0 && rr.Status == im.StatusSuccess {
			if cached, ok := im.MatchDataVersionFilter(p.filters, rr.Path.Endpoint, rr.Path.Cluster); ok {
				if cached == rr.DataVersion && rr.DataVersion > 1 {
					continue
				}
			}
		}
		rep := im.AttributeReport{Path: rr.Path, DataVersion: rr.DataVersion}
		if rr.Status != im.StatusSuccess {
			if p.suppressStatuses {
				continue
			}
			rep.IsStatus = true
			rep.Status = im.StatusIB{Status: rr.Status}
		} else {
			rep.Value = rr.Value
		}
		p.log(rep)
		p.reports++
		return rep, true
	}
}

// log is the priming report's diagnostic: each AttributeReport's
// (endpoint, cluster, attribute, value_type), so a failing pair (Apple's
// IM-decoder silently dropping a chunk) can be traced to a single
// attribute without re-hexing the wire bytes. Apple's `<private>` log
// mask hides the offending path on its side; this emit is the only
// bridge-side window into what the Subscribe-Initial actually carried for
// a given cluster.
func (p *primingSource) log(r im.AttributeReport) {
	valueType := "<status>"
	var valuePreview any
	if !r.IsStatus {
		valueType = fmt.Sprintf("%T", r.Value.Value)
		valuePreview = r.Value.Value
	}
	p.b.logger.Debug("matter.tx.subscribe.initial_report",
		slog.Int("path_index", p.reports),
		slog.Any("endpoint", r.Path.Endpoint),
		slog.Any("cluster", r.Path.Cluster),
		slog.Any("attribute", r.Path.Attribute),
		slog.Bool("status", r.IsStatus),
		slog.Any("status_code", r.Status.Status),
		slog.String("value_type", valueType),
		slog.Any("value", valuePreview))
}

// primingEventAuthorizer authorizes the priming events for the subject
// subCtx carries.
func (b *Bridge) primingEventAuthorizer(subCtx context.Context, dispatcher im.Dispatcher) im.EventReadAuthorizer {
	_, subFabricIndex := im.FabricFilterFromContext(subCtx)
	subSubjectNodeID, subSubjectCATs := im.SubjectFromContext(subCtx)
	return b.eventReadAuthorizer(dispatcher, subFabricIndex, im.IsPASEFromContext(subCtx), subSubjectNodeID, subSubjectCATs)
}

// primingEventPaths counts req's event paths that establish a
// subscription. The establish decision (matched > 0) counts the requested
// event paths that resolve to a cluster the subject may read events of,
// not the priming-log instances: a momentary/event-only subscribe
// (GenericSwitch button) is placed BEFORE the event fires, so its event
// log is empty at establish time — counting log records would reject it
// with no_match and it could never receive the future press. matter.js
// establishes an event subscription regardless of the priming log
// (ServerSubscription.ts emits 0+ EventReports on Subscribe-Initial).
func (b *Bridge) primingEventPaths(subCtx context.Context, dispatcher im.Dispatcher, req im.SubscribeRequest) int {
	return b.allowedEventPaths(subCtx, dispatcher, b.primingEventAuthorizer(subCtx, dispatcher), req.EventRequests)
}

// primingEvents returns the EventReports of a priming report.
// EventRequests handling — matter.js's `InteractionServer.ts:
// startReadInteraction` (mirrored here for Subscribe) walks
// req.EventRequests and emits any cached events that match. Apple
// Home's MTRDevice (`MTRDevice_Concrete`) transitions from
// `Subscribing` to `InitialSubscriptionEstablished` only AFTER
// the report-end frame; an Apple Subscribe with `events: *.*.*`
// that comes back attributes-only leaves the controller's state
// machine permanently stuck in `Subscribing`, which surfaces as
// `Storing cluster information count: 3` and the bridge as
// "added but not supported". matter.js Sample byte-dump
// emitted 0+ EventReports per Subscribe-Initial and
// Apple flipped to count: 21 + InitialSubscriptionEstablished.
//
// The raw records seed the report; authorization filtering only affects
// which events are disclosed, not whether the subscription establishes
// ([Bridge.primingEventPaths]) — the priming events are authorized and
// fabric-projected so a wildcard event subscribe from fabric B does not
// receive fabric A's AccessControl-change events, and a non-Administer
// subject sees no AccessControl events (Matter §8.4.3.2 / §9.10.7.1).
// Mirrors matter.js EventReadResponse.ts #readAllowedEvents; subCtx
// carries the requesting fabric + subject.
func (b *Bridge) primingEvents(subCtx context.Context, dispatcher im.Dispatcher, req im.SubscribeRequest) []im.EventReport {
	raw := im.BuildEventReports(req.EventRequests, b.eventLog, req.EventFilters)
	auth := b.primingEventAuthorizer(subCtx, dispatcher)
	return append(im.DeniedEventPathStatuses(subCtx, auth, req.EventRequests),
		im.AuthorizeEventReports(subCtx, auth, raw)...)
}

// primingMatches reports whether req establishes a subscription: one of
// its event paths or attribute paths matches what the subject may read
// (matter.js ServerSubscription.ts:610-614 rejects "no attributes or
// events are matching the query"). The attribute half runs an expansion
// of its own that stops at the first match and keeps nothing.
func (b *Bridge) primingMatches(subCtx context.Context, dispatcher im.Dispatcher, req im.SubscribeRequest) bool {
	if len(req.EventRequests) > 0 && b.primingEventPaths(subCtx, dispatcher, req) > 0 {
		return true
	}
	return b.newPrimingSource(subCtx, dispatcher, req).matches()
}

// registerSubscription registers the subscribe request in the subscription
// manager (the KeepSubscriptions=false teardown ran before, in
// [Bridge.teardownForSubscribe]), captures the routing
// subTarget, and stamps HasSubscription + SubscriptionID on initialReport in
// place. Returns the allocated subID, or 0 with a nil error when no manager
// is wired (test fixtures — the orchestrator then answers with a synthetic
// subID=0 reply).
//
// A Subscribe the manager REJECTS returns the manager's error so the caller
// answers with a StatusResponse. Answering a rejected Subscribe with a normal
// SubscribeResponse tells the controller the subscription exists when it does
// not: no report ever arrives, and the device goes stale with nothing on the
// wire to explain it.
//
// Mirrors matter.js
// packages/protocol/src/action/server/EventReadResponse.ts for the
// EventPaths wiring.
func (b *Bridge) registerSubscription(
	src net.Addr,
	requestHdr *message.Header,
	proto message.ProtocolHeader,
	req im.SubscribeRequest,
	initialReport *im.ReportData,
) (uint32, error) {
	// Subscribe in the manager so quota + cadence bookkeeping is
	// centralised there. The report pump is wired in the daemon bring-up
	// (SetEventReporter / SubscriptionReporter in daemon_matter.go); the
	// manager's engine tick drives delivery through the per-subscription
	// subTarget.
	var subID uint32
	if m := b.subscriptionManagerLocked(); m != nil {
		fabricIndex := b.resolveSessionFabric(requestHdr.SessionID)
		subjectNodeID, _ := b.resolveSessionSubject(requestHdr.SessionID)
		peerNodeID := subscriptionPeerNodeID(requestHdr, subjectNodeID)
		sub, err := m.Subscribe(subscription.SubscribeArgs{
			FabricIndex:        fabricIndex,
			PeerNodeID:         peerNodeID,
			SessionID:          requestHdr.SessionID,
			MinIntervalFloor:   req.MinIntervalFloor,
			MaxIntervalCeiling: req.MaxIntervalCeiling,
			KeepSubscriptions:  req.KeepSubscriptions,
			// A path naming this node is kept as one naming no node: a
			// change notification names none, and the subscription
			// reports this node's changes for it as its priming report
			// did (review II, R2-P2-alloc-07). A path naming another
			// node matches no change, as it selects nothing in the
			// priming report.
			AttributePaths: pathsForThisNode(req.AttributeRequests, b.localNodeIDFor(requestHdr.SessionID, fabricIndex)),
			// EventPaths wiring was missing — without this every
			// cluster-emitted event (GenericSwitch press, BDBI
			// ReachableChanged, OperationalCredentials NOCsChanged,
			// …) queued via [Manager.OnEventFired] failed to match
			// any active subscription and was silently dropped.
			// matter.js
			// packages/protocol/src/action/server/EventReadResponse.ts
			// always passes both path arrays.
			EventPaths: req.EventRequests,
			// Nothing is reported on it until the priming report and the
			// SubscribeResponse have gone out (EndPriming below).
			Priming: true,
			// Replace any stale subscription that arrived on the same
			// CASE session earlier (parallel-reconnect race). Two
			// SubscribeRequests on the same session can only arise when
			// the peer's first attempt is racing a reconnect; keeping
			// the older entry drains quota and causes duplicate reports.
			// Applied only for CASE sessions (fabricIndex != 0) because
			// PASE re-subscribe cleanup is handled by the
			// KeepSubscriptions=false / CloseSession path above; the
			// header's source node id has no say (R2-P1-req2-02).
			//
			// Gated on !req.KeepSubscriptions: a peer that sets
			// KeepSubscriptions=true is explicitly asking for its existing
			// subscriptions on this session to survive — chip and any
			// client built on matter.js's InteractionClient (default
			// keepSubscriptions=true) routinely hold two subscriptions
			// with different path sets on one CASE session. Without this
			// gate every second SubscribeRequest on a session silently
			// tore down the first one regardless of the flag, contradicting
			// the KeepSubscriptions=false teardown matter.js performs at
			// packages/node/src/node/server/InteractionServer.ts:647 at db021da0
			// (`if (fabric !== undefined && !keepSubscriptions)`).
			ReplaceSessionDuplicate: !req.KeepSubscriptions && fabricIndex != 0,
		})
		if err != nil {
			b.logger.Warn("matter.rx.im.subscribe.manager",
				slog.String("src", srcString(src)),
				slog.Any("err", err))
			return 0, err
		}
		subID = sub.ID
		initialReport.HasSubscription = true
		initialReport.SubscriptionID = subID
		b.captureSubTarget(subID, src, requestHdr, proto, req.FabricFiltered)
	}
	return subID, nil
}

// teardownForSubscribe runs the KeepSubscriptions=false teardown of a
// SubscribeRequest: before the request is checked any further, so a
// request refused afterwards (no path, an illegal path) has cancelled the
// peer's subscriptions too, as matter.js InteractionServer.ts:647-665 at
// db021da0 runs it before those refusals (F-db021da0-14, review II
// R2-P2-sess-04). A no-op when the request keeps its subscriptions or no
// manager is attached.
func (b *Bridge) teardownForSubscribe(src net.Addr, requestHdr *message.Header, req im.SubscribeRequest) {
	m := b.subscriptionManagerLocked()
	if m == nil || req.KeepSubscriptions {
		return
	}
	fabricIndex := b.resolveSessionFabric(requestHdr.SessionID)
	subjectNodeID, _ := b.resolveSessionSubject(requestHdr.SessionID)
	peerNodeID := subscriptionPeerNodeID(requestHdr, subjectNodeID)
	// KeepSubscriptions=false teardown — Matter §10.6.5: when the
	// commissioner clears `KeepSubscriptions`, the bridge MUST
	// cancel every existing subscription owned by the same peer
	// before admitting the new one.
	//
	// Mirrors matter.js packages/node/src/node/server/
	// InteractionServer.ts:647-665 at db021da0 for a session with a
	// fabric: it matches on the peer address, (fabric, peerNodeID).
	// On a session without one (PASE) matter.js tears nothing down
	// (`fabric !== undefined`, :647); the same-session teardown
	// below is this module's own
	// (notes/parity/matter_behaviour_findings.md F-db021da0-12). The
	// previous guard
	// `fabricIndex != 0 && SourceNodeID != 0` was too narrow — it
	// silently skipped PASE re-subscriptions (fabricIndex=0) so a
	// PASE subscriber that re-subscribes with KeepSubscriptions=false
	// would accumulate stale subscriptions.
	if fabricIndex != 0 && peerNodeID != 0 {
		// CASE session — tear down by (fabric, peer) tuple. The
		// peer is the session's subject, whatever source node id
		// the frame's header claims (subscriptionPeerNodeID), as
		// matter.js matches on session.peerAddress.
		if cleared := m.ClosePeer(fabricIndex, peerNodeID); cleared > 0 {
			b.logger.Info("matter.rx.im.subscribe.peer_teardown",
				slog.String("src", srcString(src)),
				slog.Int("fabric_index", int(fabricIndex)),
				slog.Uint64("node_id", peerNodeID),
				slog.Int("cleared", cleared))
		}
	} else {
		// PASE session (fabricIndex=0) — tear down by SessionID so
		// a re-subscribe on the same PASE exchange replaces, rather
		// than accumulates, the prior subscription.
		// This module's own: matter.js tears nothing down on a
		// session without a fabric (InteractionServer.ts:647 at
		// db021da0; findings F-db021da0-12).
		m.CloseSession(requestHdr.SessionID)
	}
}

// pathsForThisNode returns paths with the node cleared from each path
// that names localNodeID — this node on the session's fabric, which is
// what a path naming no node selects. Returns paths when none does.
func pathsForThisNode(paths []im.ConcreteAttributePath, localNodeID uint64) []im.ConcreteAttributePath {
	var out []im.ConcreteAttributePath
	for i, p := range paths {
		if !p.HasNode || p.Node != localNodeID {
			continue
		}
		if out == nil {
			out = slices.Clone(paths)
		}
		out[i].HasNode, out[i].Node = false, 0
	}
	if out == nil {
		return paths
	}
	return out
}

// latestEventNumber is the highest number of the events reports carries
// with their data; false when it carries none.
func latestEventNumber(reports []im.EventReport) (uint64, bool) {
	var latest uint64
	found := false
	for _, r := range reports {
		if !r.IsStatus && (!found || r.Number > latest) {
			latest, found = r.Number, true
		}
	}
	return latest, found
}

// subscriptionPeerNodeID names the peer a subscribe came from: the
// subject node id of the secure session it rode on. The source node id a
// secure frame's header may carry is chosen by the sender and not bound to
// the session (the nonce is built from the session's peer), so it never
// names the peer there; only a frame of the unsecured session, which has
// no subject, is attributed to its header's source node id. matter.js
// takes the session's identity, never the header: InteractionServer.ts:647,
// :651 (PeerAddress.is(sess.peerAddress, session.peerAddress)) and :723 at
// db021da0. Review II, R2-P1-req2-02.
func subscriptionPeerNodeID(requestHdr *message.Header, subjectNodeID uint64) uint64 {
	if requestHdr.SessionID == 0 && requestHdr.HasSourceNodeID {
		return requestHdr.SourceNodeID
	}
	return subjectNodeID
}

// subscribeRejectStatus maps a subscription-manager rejection to the IM
// status the controller gets back.
//
// Cadence violations are InvalidAction: matter.js
// packages/node/src/node/server/InteractionServer.ts:665-682 raises
// StatusResponseError(..., Status.InvalidAction) for every
// minIntervalFloor / maxIntervalCeiling constraint it checks. A quota
// rejection is ResourceExhausted — the request is well-formed, the
// publisher has no room for it (Matter §8.10 ResourceExhausted, the code
// chip's ReadHandler returns once the subscription table is full).
func subscribeRejectStatus(err error) im.StatusCode {
	switch {
	case errors.Is(err, subscription.ErrFabricQuotaExceeded):
		return im.StatusResourceExhausted
	case errors.Is(err, subscription.ErrCadenceOutOfRange),
		errors.Is(err, subscription.ErrCadenceInvertedAfterClamp):
		return im.StatusInvalidAction
	default:
		return im.StatusFailure
	}
}

// streamInitialReportChunks encodes initialReport into per-datagram chunks
// and sends them one at a time, blocking for a peer IM:StatusResponse
// between each chunk (Matter §10.6.6).
//
// Subscribe-Initial chunk-streaming pattern mirrors matter.js
// `InteractionMessenger.ts:sendDataReportMessage(_, waitForAck=true)`:
// send one ReportData chunk, block on the peer's IM:StatusResponse for
// this exchange, then send the next.
//
// Apple's ReadClient (`connectedhomeip/src/app/ReadClient.cpp:541`
// → `OnMessageReceived`) emits exactly one StatusResponse per
// inbound ReportData and stays in the per-chunk read state until
// that response is on the wire.
func (b *Bridge) streamInitialReportChunks(
	ctx context.Context,
	abort <-chan struct{},
	src net.Addr,
	requestHdr *message.Header,
	proto message.ProtocolHeader,
	subID uint32,
	initialReport im.ReportData,
) error {
	limits := b.chunkLimitsFor(src, requestHdr.SessionID)
	chunker := newReportChunker(initialReport, limits.budget, limits.hardCap,
		sliceAttributeReports(initialReport.Reports), func() []im.EventReport { return initialReport.EventReports })
	return b.streamPrimingChunks(ctx, abort, src, requestHdr, proto, subID, chunker)
}

// streamPrimingChunks is [Bridge.streamInitialReportChunks] over a
// chunker: chunk N+1 is generated only once the peer answered chunk N
// (review II, decision 44).
func (b *Bridge) streamPrimingChunks( //nolint:gocognit // per-chunk ack synchronisation loop mirrors matter.js sendDataReportMessage; splitting would obscure the atomic arm→send→wait sequence
	ctx context.Context,
	abort <-chan struct{},
	src net.Addr,
	requestHdr *message.Header,
	proto message.ProtocolHeader,
	subID uint32,
	chunker *reportChunker,
) error {
	// Send initial ReportData (opcode 0x05) chunked. Apple Home's
	// initial subscribe over CASE expands to ~1100 attribute reports
	// totalling 60+ KB — well past the AES-CCM 64KB plaintext cap
	// AND the per-datagram budget. The chunker splits on
	// per-attribute boundaries; each chunk except the last carries
	// MoreChunkedMessages=true (Matter §10.6.6).
	chunk, have, err := chunker.next()
	if err != nil {
		debugReplyError(b.logger, "chunk_initial_report", src, err)
		return err
	}
	// Subscribe-Initial chunk-streaming pattern (Matter §10.6.6, mirror
	// of matter.js `InteractionMessenger.ts:sendDataReportMessage(_,
	// waitForAck=true)`): send one ReportData chunk, block on the
	// peer's IM:StatusResponse for this exchange, then send the next.
	//
	// Apple's ReadClient (`connectedhomeip/src/app/ReadClient.cpp:541`
	// → `OnMessageReceived`) emits exactly one StatusResponse per
	// inbound ReportData and stays in the per-chunk read state until
	// that response is on the wire. matter.js's successful Apple-pair
	// byte-trace shows ~1 ms ping-pong between every
	// chunk and Apple's StatusResponse. Earlier go-fabric burst-
	// fired every chunk without ever syncing on the StatusResponse —
	// Apple's state-machine collapsed into a path where
	// `ProcessSubscribeResponse` never triggered (Run 19 of the Apple-pair-diagnose cycle).
	//
	// The per-chunk wait is bounded by the exchange timeout
	// ([Bridge.chunkStatusResponseTimeout]); matter.js's observed round-
	// trip is sub-millisecond. A chunk that is never answered aborts the
	// Subscribe instead of releasing the next chunk to a peer that is not
	// listening.
	for i := 0; have; i++ {
		body, err := EncodeReportData(chunk)
		if err != nil {
			debugReplyError(b.logger, "encode_initial_report", src, err)
			return err
		}
		// Diagnose hook: one debug line per Subscribe-initial chunk. A byte
		// dump of the chunk belongs in a test, not in the library.
		b.logger.Debug("matter.tx.im.subscribe.chunk",
			slog.Int("subscription_id", int(subID)),
			slog.Int("chunk", i),
			slog.Int("bytes", len(body)),
			slog.Bool("final", !chunk.MoreChunkedMessages))

		// Arm the per-exchange StatusResponse waiter BEFORE the send
		// to avoid a missed-wakeup race: Apple can reply faster than
		// our scheduler returns from sendReplyReliable.
		waitCh := b.armStatusResponseWait(requestHdr.SessionID, proto.ExchangeID, !proto.Initiator)
		// Piggyback the latest peer-sent counter on this chunk's
		// AckCounter. Without this rewrite every chunk carries the
		// stale SubscribeRequest counter, and python-matter-server's
		// ReliableMessaging drops chunk N+1 after the peer has
		// StatusResponse-acked chunk N — "Dropping message without
		// piggyback ack when we are waiting for an ack".
		chunkHdr := *requestHdr
		b.refreshAckCounter(&chunkHdr, proto.ExchangeID, !proto.Initiator)
		if err := b.sendReplyReliable(src, &chunkHdr, proto, im.OpcodeReportData, body); err != nil {
			b.disarmStatusResponseWait(requestHdr.SessionID, proto.ExchangeID, !proto.Initiator)
			debugReplyError(b.logger, "send_initial_report", src, err)
			return err
		}
		// Block until Apple StatusResponse-acks this chunk on the IM
		// layer, or the timeout expires.
		//
		// chip's ReadHandler.cpp:241-271 sends `SendSubscribeResponse`
		// ONLY AFTER `OnStatusResponse` for the FINAL chunk. Verified
		// empirically: with the final-chunk-wait, Apple's MTRDevice reached
		// the `1 => 2` transition (`Subscription established in 14090ms`,
		// `InitialSubscriptionEstablished`) for the first time; without the
		// final-wait (Run 22) the cache reverted to 0 sensors and Apple
		// remained in `Subscribing`. Keep the wait unconditional on all
		// chunks including final — the `final` flag in the timeout log is
		// diagnostic only.
		//
		// An error status or a missing answer aborts the Subscribe: the
		// peer is not consuming the priming report, and matter.js's
		// waitForSuccess throws in both cases (InteractionMessenger.ts:721,
		// MessageExchange.ts:855-863).
		if err := b.awaitChunkStatusResponse(ctx, abort, waitCh, "subscribe", src, requestHdr.SessionID, proto.ExchangeID, !proto.Initiator, i, chunk); err != nil {
			return err
		}

		b.logger.Debug("matter.rx.im.subscribe.chunk",
			slog.String("src", srcString(src)),
			slog.Int("chunk", i),
			slog.Int("bytes", len(body)),
			slog.Bool("more", chunk.MoreChunkedMessages))
		if chunk, have, err = chunker.next(); err != nil {
			debugReplyError(b.logger, "chunk_initial_report", src, err)
			return err
		}
	}
	return nil
}

// sendSubscribeResponse sends the SubscribeResponse (opcode 0x04),
// piggybacks the latest inbound counter, stamps lastReport on the
// freshly-created subscription, and emits the final subscribe diagnostic
// log line.
//
// Reliable path — Apple ACKs the SubscribeResponse via MRP; if
// the response drops Apple times the exchange out and tears the
// fabric down, so MRP retransmit is mandatory here.
//
// The piggyback ack counter refresh mirrors Matter §4.12.4.2
// (cumulative ack): the chip-tool commissioner sends an IM:StatusResponse
// acking the initial-report chunk before the bridge fires the
// SubscribeResponse, so acking only the original SubscribeRequest counter
// leaves chip-tool's ReliableMessaging still waiting on the StatusResponse
// counter.
func (b *Bridge) sendSubscribeResponse(
	src net.Addr,
	requestHdr *message.Header,
	proto message.ProtocolHeader,
	req im.SubscribeRequest,
	subID uint32,
	initialReport im.ReportData,
) error {
	// Send SubscribeResponse (opcode 0x04). Caps MaxInterval at the
	// manager's effective ceiling (stored on the Subscription
	// record); when no manager is wired we echo the requested max.
	// Reliable path — Apple ACKs the SubscribeResponse via MRP; if
	// the response drops Apple times the exchange out and tears the
	// fabric down, so MRP retransmit is mandatory here.
	maxInt := req.MaxIntervalCeiling
	if m := b.subscriptionManagerLocked(); m != nil && subID != 0 {
		if sub, err := m.Get(subID); err == nil {
			maxInt = sub.MaxIntervalCeiling
		}
	}
	respBody, err := EncodeSubscribeResponse(im.SubscribeResponse{
		SubscriptionID: subID,
		MaxInterval:    maxInt,
	})
	if err != nil {
		debugReplyError(b.logger, "encode_subscribe_response", src, err)
		return err
	}
	// Piggyback the LATEST inbound counter on this exchange — not just
	// the SubscribeRequest's counter that triggered this reply. The
	// chip-tool commissioner sends an IM:StatusResponse acking the
	// initial-report chunk before the bridge fires the
	// SubscribeResponse; with the UDP-per-datagram goroutine model that
	// StatusResponse hits the receive pipeline a few ms after the chunk
	// send, BEFORE the chunk-ack
	// wait unblocks. Acking only the original SubscribeRequest
	// counter (`requestHdr.MessageCounter`) leaves chip-tool's
	// ReliableMessaging layer still waiting on the StatusResponse
	// counter, so it drops the SubscribeResponse with
	// `Dropping message without piggyback ack when we are waiting
	// for an ack` and the subscription times out. Atomically
	// fetch+clear the latest pending ack-counter and rewrite
	// requestHdr.MessageCounter to that value so sendReplyReliable's
	// `AckCounter: requestHdr.MessageCounter` piggybacks the freshest
	// observed counter (cumulative ack per Matter §4.12.4.2).
	subRespHdr := *requestHdr
	if counter, ok := b.takeOwedAck(requestHdr.SessionID, proto.ExchangeID, !proto.Initiator); ok {
		subRespHdr.MessageCounter = counter
	}
	b.load.subscriptionsSucceeded.Add(1)
	b.recordSubscriptionsActive(b.subscriptionManagerLocked())
	if err := b.sendReplyReliable(src, &subRespHdr, proto, im.OpcodeSubscribeResponse, respBody); err != nil {
		debugReplyError(b.logger, "send_subscribe_response", src, err)
		return err
	}
	// Do NOT call dischargeOwedAck here. The piggyback ack on the
	// SubscribeResponse itself fulfils the obligation for the original
	// SubscribeRequest counter, but the tracker is exchange-keyed:
	// `tracker.Discharge(exchangeID)` clears ALL pending obligations on
	// the exchange, including any registered between sendReplyReliable
	// constructing the reply and this line — most notably chip-tool's
	// IM:StatusResponse acking the initial-report chunk (Matter §8.6.2)
	// which arrives a millisecond after the initial chunk's wait
	// unblocks. Discharging that StatusResponse obligation here means
	// the ack pump never emits a StandaloneAck for it, chip-tool's
	// ReliableMessaging retransmits 4x and gives up, and the Subscribe
	// times out with `CHIP Error 0x32 Timeout`. Leaving the
	// obligation in the tracker lets the 50 ms pump fire the
	// StandaloneAck on its own cadence; the piggyback we already
	// shipped is benign-redundant for the SubscribeRequest counter,
	// and chip-tool's cumulative-ack semantics (Matter §4.12.4.2)
	// absorb a StandaloneAck that lands after the piggyback.

	// IMPORTANT: NO primer ReportData after SubscribeResponse.
	//
	// Earlier code shipped an empty `ReportData{HasSubscription:true,
	// SubscriptionID:subID}` here under the assumption that Apple's
	// MTRDevice expected a follow-up "subscription primed" signal —
	// that hypothesis is now falsified. matter.js's bridge sample (Apple-
	// pair-success byte-dump) sends *only* the chunked
	// ReportData burst followed by exactly one SubscribeResponse and
	// stops; Apple's ReadClient (`connectedhomeip/src/app/ReadClient.cpp:
	// ProcessSubscribeResponse:1124`) MoveToState(SubscriptionActive)
	// + OnSubscriptionEstablished synchronously on the SubscribeResponse
	// itself, which is what flips MTRDevice from `Subscribing` to
	// `InitialSubscriptionEstablished`. Sending an extra ReportData
	// AFTER SubscribeResponse arrives at a ReadClient already in
	// SubscriptionActive — Apple's IM machinery routes it as a
	// regular subscription update rather than a primer, and (per
	// Apple-pair Run 18 byte-trace) appears to interfere with the
	// HAP-Service-Mapper's cache-flush step that gates the
	// `1 => 2` MTRDevice state transition.

	// Stamp lastReport=now on the freshly-created subscription so the
	// engine treats the initial-report flush + primer as the first
	// report. Without this lastReport stays zero, maxIntervalElapsed
	// fires on the very first 250 ms tick, and the engine immediately
	// emits an empty keep-alive — extra wire chatter that races the
	// primer above.
	if m := b.subscriptionManagerLocked(); m != nil && subID != 0 {
		if sub, err := m.Get(subID); err == nil {
			// The events the priming report carried are not owed again
			// (review II, R2-P1-req2-03).
			if latest, ok := latestEventNumber(initialReport.EventReports); ok {
				sub.PrimedEvents(latest)
			}
			sub.EndPriming(b.clk().Now())
		}
	}
	b.logger.Debug("matter.rx.im.subscribe",
		slog.String("src", srcString(src)),
		slog.Int("subscription_id", int(subID)),
		slog.Int("paths", len(req.AttributeRequests)),
		slog.Int("event_paths", len(req.EventRequests)),
		slog.Int("initial_events", len(initialReport.EventReports)),
		slog.Int("min_interval", int(req.MinIntervalFloor)),
		slog.Int("max_interval", int(req.MaxIntervalCeiling)))
	return nil
}

// allowedEventPaths counts the requested event paths that resolve to at least
// one (endpoint, cluster) of the topology whose events the subject may read.
// A subscription whose attribute and event paths all fail that test is
// rejected with INVALID_ACTION: matter.js counts an event path as existent
// only after its View pass, its existence checks and its read-privilege pass
// (packages/protocol/src/action/server/EventReadResponse.ts #addConcrete /
// counts.existent) and ServerSubscription.ts rejects "no attributes or events
// are matching" when both counts are zero. TC-ACE-1.2 step 14 subscribes to
// AccessControlEntryChanged without Administer and expects exactly that.
func (b *Bridge) allowedEventPaths(ctx context.Context, dispatcher im.Dispatcher, auth im.EventReadAuthorizer, paths []im.ConcreteEventPath) int {
	n := 0
	for _, p := range paths {
		if eventPathAllowed(ctx, dispatcher, auth, p) {
			n++
		}
	}
	return n
}

// eventPathAllowed resolves p's endpoint and cluster through the data model
// (a read of ClusterRevision, which every cluster instance has, expands the
// wildcards) and reports whether any resolved cluster's events are readable
// by the subject.
func eventPathAllowed(ctx context.Context, dispatcher im.Dispatcher, auth im.EventReadAuthorizer, p im.ConcreteEventPath) bool {
	if dispatcher == nil {
		return true
	}
	probe := im.ConcreteAttributePath{
		Endpoint: p.Endpoint, HasEndpoint: p.HasEndpoint,
		Cluster: p.Cluster, HasCluster: p.HasCluster,
		Attribute: clusterRevisionAttributeID, HasAttribute: true,
	}
	for _, rr := range dispatcher.Read(ctx, probe) {
		if rr.Status == im.StatusSuccess && auth.Allows(ctx, rr.Path.Endpoint, rr.Path.Cluster) {
			return true
		}
	}
	return false
}

// clusterRevisionAttributeID is the global ClusterRevision attribute.
const clusterRevisionAttributeID = 0xFFFD

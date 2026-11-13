// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"reflect"
	"slices"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/clock"
	"github.com/SukramJ/go-fabric/cluster/measurement"
	occdef "github.com/SukramJ/go-fabric/cluster/spec/occupancysensing"
	"github.com/SukramJ/go-fabric/contract"
	endpointpkg "github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// Errors surfaced by the unsolicited-IM send path. They are logged
// and dropped — best-effort delivery is the v1.1 mode.
var (
	// ErrUnsolicitedListenerMissing is returned when the bridge has no
	// UDP listener bound (Stop racing with a tick).
	ErrUnsolicitedListenerMissing = errors.New("bridge: unsolicited send: listener missing")
	// ErrUnsolicitedSessionMissing is returned when the operational
	// session backing a subscription is gone (peer disconnected; the
	// next manager tick reaps the subscription).
	ErrUnsolicitedSessionMissing = errors.New("bridge: unsolicited send: session missing")
	// ErrUnsolicitedEncrypt wraps an AES-CCM seal failure.
	ErrUnsolicitedEncrypt = errors.New("bridge: unsolicited send: encrypt")
)

// chunkStatusResponseExpectedProcessing and chunkStatusResponseBuffer
// are the two fixed terms of matter.js's maximum peer response time
// (packages/protocol/src/protocol/MRP.ts:55 DEFAULT_EXPECTED_PROCESSING_TIME,
// :61 PEER_RESPONSE_TIME_BUFFER); the third term is the summed MRP
// retransmission backoff, see [Bridge.chunkStatusResponseTimeout].
const (
	chunkStatusResponseExpectedProcessing = 2 * time.Second
	chunkStatusResponseBuffer             = 5 * time.Second
)

// chunkStatusResponseTimeout bounds how long a chunk loop (Subscribe-
// Initial and Read-response) waits for the peer's IM StatusResponse on
// one ReportData chunk before abandoning the interaction. Mirrors
// matter.js MRP.maxPeerResponseTimeOf for a UDP/MRP session
// (packages/protocol/src/protocol/MRP.ts:79-120): the worst-case time
// until the peer received the last MRP retransmission — every backoff
// interval summed with maximum jitter (:198-224 maxResponseTimeOf) —
// plus the default expected processing time and the response buffer.
// The base interval is the peer's advertised one when the session
// lookup can resolve it, else the spec idle default.
//
// A test may pin the bound through chunkStatusResponseTimeoutOverride;
// zero means derive it.
func (b *Bridge) chunkStatusResponseTimeout(sessionID uint16) time.Duration {
	if b.chunkStatusResponseTimeoutOverride > 0 {
		return b.chunkStatusResponseTimeoutOverride
	}
	base := b.outboundBaseInterval(sessionID, b.clk().Now())
	if base <= 0 {
		base = mrp.SessionIdleIntervalDefault
	}
	var backoff time.Duration
	for n := 0; n <= mrp.MaxRetransmissions; n++ {
		backoff += mrp.BackoffDuration(base, n, func() float64 { return 1 }) + b.LocalFixedMRPBackoff()
	}
	return backoff + chunkStatusResponseExpectedProcessing + chunkStatusResponseBuffer
}

// AttachSubscriptionManager wires the subscription manager that
// powers IM Subscribe. When set, inbound SubscribeRequest datagrams
// produce:
//
//  1. Initial ReportData (synchronous, with HasSubscription=true and
//     the freshly-allocated SubscriptionID).
//  2. SubscribeResponse carrying SubscriptionID + MaxInterval.
//
// The manager's engine ticks ongoing reports through the
// [Bridge.SubscriptionReporter] callback the daemon plumbs into
// [subscription.NewManager]; that callback re-reads the cached
// values and ships a fresh ReportData over the original commissioner
// session, looked up via [exchangeRouting.subTargets] (populated here on
// successful Subscribe).
//
// Pass nil to revert to noop. The initial ReportData is still built
// and streamed in full — buildInitialReport runs off the dispatcher and
// never consults the manager — but it carries HasSubscription=false and
// SubscriptionID=0, and no report ever follows. The controller reads a
// successful subscribe and a device that never changes, so treat an
// unattached manager as a wiring defect rather than a degraded mode.
func (b *Bridge) AttachSubscriptionManager(m *subscription.Manager) {
	b.mu.Lock()
	b.subManager = m
	b.mu.Unlock()
	b.rewireMeasurementListeners()
	if m != nil {
		// The manager owns subscription lifetime; the bridge owns the
		// routing state keyed by subscription ID. Without this hook the
		// routing entry outlives every cleanly-closed subscription —
		// re-subscribe, endpoint removal, fabric teardown and session
		// reap all terminate subscriptions the bridge never hears about.
		m.SetOnSubscriptionClosed(func(subID uint32) {
			b.releaseSubscriptionRouting(subID)
			b.recordSubscriptionsActive(m)
		})
		// A terminated subscription is forgotten; one closed with its
		// session stays recorded for re-establishment after a restart
		// (matter.js SubscriptionsServer.#subscriptionCancelled). It
		// also starts its fabric's "reconnect" broadcast so the
		// controller finds the bridge again: matter.js
		// packages/protocol/src/protocol/DeviceAdvertiser.ts:174-179 at
		// 862ad75f (subscriptionsChanged: operational, isTerminated and a
		// fabric → #advertiseFabric(session.fabric, "reconnect"), whose
		// Advertiser.advertise starts the fabric's advertisement,
		// Advertiser.ts:32-43, without resetting a running schedule,
		// Advertisement.ts:78-91). The session-teardown path carries it,
		// so a stopping bridge starts nothing; a fabric being removed
		// has no announced service left and starts nothing either.
		m.SetOnSubscriptionTerminated(func(subID uint32, fabricIndex uint8) {
			b.forgetPersistedSubscription(subID)
			b.triggerSessionReannounce(fabricIndex)
		})
		// A subscription owes events as a number; its report reads
		// them from the bridge's log (matter.js ServerSubscription.ts
		// :541-542 reads the occurrence store at send time).
		m.SetEventSource(b.eventLog)
	}
}

// releaseSubscriptionRouting drops every bridge-side table entry that
// belongs to subID. Called by the subscription manager's close
// observer, so it must not take b.mu (the manager can close a
// subscription from within a bridge call).
func (b *Bridge) releaseSubscriptionRouting(subID uint32) {
	b.routing.subTargets.Delete(subID)
	b.subSendErrorCount.Delete(subID)
	b.forgetReportExchange(subID)
	// reportCounterOwner is keyed by (session, counter), so the owning
	// subscription is only visible in the value — a scan is the only
	// way to reclaim the in-flight report counters of a subscription
	// whose peer never ACKs them.
	b.reportCounterOwner.Range(func(key, value any) bool {
		if owner, ok := value.(uint32); ok && owner == subID {
			b.reportCounterOwner.Delete(key)
		}
		return true
	})
}

// subTarget captures the per-subscription routing metadata needed to
// ship ongoing ReportData datagrams. Populated by
// handleSubscribeRequest when Subscribe succeeds; consumed by
// [Bridge.reportSubscription] from the manager's tick.
//
// peerSourceNodeID is the value the commissioner stamped into
// SourceNodeID on the original SubscribeRequest header — the bridge
// echoes it as DestNodeID in every outbound report (Matter §4.4.1.2).
// sessionID is the operational session under which the reports
// travel; SessionID==0 means PASE (unencrypted), >0 means CASE
// (AES-CCM-sealed via [channel.Session]).
type subTarget struct {
	src                 net.Addr
	hasPeerSourceNodeID bool
	peerSourceNodeID    uint64
	exchangeID          uint16
	sessionID           uint16
	peerInitiator       bool // true if peer opened the exchange (always true for Subscribe inbound)

	// Authorization context captured at Subscribe time, so ongoing
	// reports enforce the SAME ACL + fabric projection as the initial
	// read (Matter §8.5 / §9.10; matter.js ServerSubscription re-applies
	// the read-response authority on every update). Without these the
	// engine tick would ship attribute/event data the subject is no
	// longer (or never was) permitted to see. fabricIndex==0 means PASE.
	fabricIndex    uint8
	subjectNodeID  uint64
	subjectCATs    []uint32
	fabricFiltered bool
	// pase records that the subscription was established over a PASE
	// session, so ongoing reports keep the commissioning channel's
	// implicit Administer grant even after AddNOC adopted the session
	// onto a fabric and fabricIndex stopped being 0.
	pase bool

	// hasAck / ackCounter piggyback an acknowledgement on the next
	// message sent for this target. Set only per chunk by the priming
	// report of a re-established subscription, which answers the peer's
	// StatusResponse to the previous chunk on the same exchange.
	hasAck     bool
	ackCounter uint32

	// flush marks the report a subscription sends as its fabric leaves
	// ([Bridge.reportSubscriptionFlush]): the peer's answer to each of
	// its chunks is awaited [flushStatusResponseTimeout], not the chunk
	// StatusResponse bound, and each chunk is sent once — it asks for an
	// ack but is not handed to the outbound reliable tracker
	// ([Bridge.sendUnsolicitedIM]).
	flush bool
	// abort, when it closes, ends the wait for the peer's answer to a
	// chunk of the report being sent ([errChunkWaitAborted]). The ongoing
	// reporters set it to their context's Done on the copy they send
	// with — the subscription engine's stop — and it is never stored in
	// the routing table. Nil: the wait ends with its answer or its timer.
	abort <-chan struct{}
}

// SubscriptionReporter returns the [subscription.Reporter] closure the
// daemon plugs into [subscription.NewManager]. The closure looks up
// the per-subscription [subTarget], reads the dirty paths through the
// dispatcher, and ships a fresh ReportData over the same session.
//
// Reliability: when the bridge has an outbound MRP tracker wired
// (the standard daemon path), each ReportData is shipped with
// NeedsAck=true and registered with the tracker — the ACK pump
// retransmits until the peer ACKs or the retransmit cap fires.
// On cap exhaustion the [Bridge.releaseReportCounter] callback reaps
// the dead subscription so the manager stops emitting against a
// vanished peer. Replay after a peer reconnect relies on the
// manager's dirty-flag re-emission rather than a side replay buffer
// — every attribute change re-marks the path dirty, so the next
// healthy tick ships a fresh ReportData.
func (b *Bridge) SubscriptionReporter() subscription.Reporter {
	return b.reportSubscription
}

// SubscriptionEventReporter is the symmetrical hook for ongoing
// EVENT reports. The daemon wires this via
// [subscription.Manager.SetEventReporter] so a cluster-server-fired
// event lands on every subscription whose EventPaths cover the
// (endpoint, cluster, event) triple.
//
// The closure encodes the events under the same per-subscription
// [subTarget] used for attribute reports, so a single peer session
// receives both report types interleaved.
func (b *Bridge) SubscriptionEventReporter() subscription.EventReporter {
	return b.reportSubscriptionEvents
}

// SubscriptionFlushReporter is the hook for the flush before a fabric
// leaves. The daemon wires it via [subscription.Manager.SetFlushReporter]
// beside the other two; [subscription.Manager.FlushFabric] hands it what
// one subscription owes — events and attributes together — and it ships
// them as one ReportData and waits for the peer's answer
// ([Bridge.reportSubscriptionFlush]).
func (b *Bridge) SubscriptionFlushReporter() subscription.FlushReporter {
	return b.reportSubscriptionFlush
}

// subscriptionTargetOf returns the routing target captured for sub at
// Subscribe time; false when sub has none (never routed, or reaped).
func (b *Bridge) subscriptionTargetOf(sub *subscription.Subscription) (subTarget, bool) {
	raw, ok := b.routing.subTargets.Load(sub.ID)
	if !ok {
		return subTarget{}, false
	}
	target, ok := raw.(subTarget)
	if !ok || isNilRoute(target.src) {
		return subTarget{}, false
	}
	return target, true
}

// eventReadAuthorizer builds the ACL + fabric-sensitive gate for event reads
// and subscriptions from the requesting session's identity, using dispatcher's
// optional [im.ACLChecker]. A PASE session (fabricIndex==0 before AddNOC,
// pase==true after it adopted the session onto the new fabric) or a
// dispatcher without an ACLChecker fails open inside
// [im.AuthorizeEventReports]. Shared by
// the plain event read (receive_dispatch), the Subscribe-Initial priming report
// (subscribe_dispatch), and the ongoing event fan-out (authorizedEventReports)
// so all three enforce identical event authorization.
func (b *Bridge) eventReadAuthorizer(dispatcher im.Dispatcher, fabricIndex uint8, pase bool, subjectNodeID uint64, subjectCATs []uint32) im.EventReadAuthorizer {
	checker, _ := dispatcher.(im.ACLChecker)
	paths, _ := dispatcher.(im.EventPathChecker)
	return im.EventReadAuthorizer{
		Checker:       checker,
		Paths:         paths,
		FabricIndex:   fabricIndex,
		PASE:          pase,
		SubjectNodeID: subjectNodeID,
		SubjectCATs:   subjectCATs,
	}
}

// authorizedEventReports filters events down to those the subscribing subject
// (captured on target) is permitted to read AND drops fabric-sensitive records
// owned by another fabric. A PASE session (fabricIndex==0) or a dispatcher
// without an [im.ACLChecker] returns the events unchanged. Each event is gated
// at View — the Matter default event read privilege — except AccessControl
// (0x001F) events, which require Administer AND are dropped when the record's
// FabricIndex differs from the subscribing fabric (Matter §8.4.3.2 /
// §9.10.7.1): path-ACL alone does not stop cross-fabric disclosure of a
// fabric-sensitive record, so the fabric-sensitive drop is applied here too.
// Closes the event half of the subscribe-path ACL bypass.
func (b *Bridge) authorizedEventReports(ctx context.Context, target subTarget, events []im.EventReport) []im.EventReport {
	auth := b.eventReadAuthorizer(b.Dispatcher(), target.fabricIndex, target.pase, target.subjectNodeID, target.subjectCATs)
	return im.AuthorizeEventReports(ctx, auth, events)
}

// reportSubscriptionEvents assembles + ships an event-only ReportData
// for sub. Called by the manager's engine tick.
func (b *Bridge) reportSubscriptionEvents(ctx context.Context, sub *subscription.Subscription, events []im.EventReport) {
	if sub == nil || len(events) == 0 {
		return
	}
	target, ok := b.subscriptionTargetOf(sub)
	if !ok {
		return
	}

	// Authorize each event against the subscribing subject: a CASE
	// subject only receives events on (endpoint, cluster) it holds at
	// least View privilege for (Matter §9.10 — events carry a read
	// privilege, default View; AccessControlEntryChanged is Administer).
	// Without this an ACE-less subject would stream every fired event.
	// A PASE subscriber bypasses (the auth mode, not fabric index 0,
	// which alone is refused); a dispatcher without an ACLChecker keeps
	// the pre-ACL behaviour.
	authorizedEvents := b.authorizedEventReports(ctx, target, events)
	if len(authorizedEvents) == 0 {
		return
	}

	// The engine's stop ends the wait for a chunk's answer (W2-conc-06).
	target.abort = ctx.Done()

	report := im.ReportData{
		HasSubscription: true,
		SubscriptionID:  sub.ID,
		EventReports:    authorizedEvents,
	}

	// Same bridge-initiated exchange as the attribute path: the
	// commissioner closed its Subscribe exchange when the
	// SubscribeResponse landed, and a report sent there with
	// Initiator=false is an unsolicited message on an unknown exchange —
	// matter.js answers it with a standalone ack and drops it
	// (ExchangeManager.ts:441-452), which the MRP layer here would then
	// mistake for delivery. Ongoing server reports are always fresh
	// server-initiated exchanges (ServerSubscription.ts:823).
	counters, freshExch, err := b.sendInitiatedReport(target, report)
	if err != nil {
		// Owe the same events again, so the next report retries them;
		// the third consecutive failure reaps the subscription. Mirrors
		// matter.js packages/node/src/node/server/ServerSubscription.ts
		// :566-581 (restores #outstandingEventsMinNumber while
		// #sendUpdateErrorCounter <= 2).
		sub.RequeueEvents(events[0].Number)
		if errors.Is(err, errChunkWaitAborted) {
			return // the engine stopped; the peer did nothing wrong
		}
		b.noteSubscriptionSendError(sub.ID, "event_report", err)
		return
	}
	b.subSendErrorCount.Delete(sub.ID)
	for _, counter := range counters {
		b.reportCounterOwner.Store(reportCounterKey(target.sessionID, counter), sub.ID)
	}
	b.rememberReportExchange(sub.ID, target.sessionID, freshExch)
	b.releaseReportOfClosed(sub)
	b.logger.Debug("matter.tx.subscribe.event_report",
		slog.Int("subscription_id", int(sub.ID)),
		slog.Int("events", len(events)),
		slog.Int("exchange_id", int(freshExch)))
}

// EmitEvent is the bridge-side [contract.EventEmitter]
// implementation. Cluster servers (or model-side cluster servers
// that implement [contract.EventReceiver]) call this when
// the underlying DP fires an event that should surface to
// subscribers. Non-blocking — the manager records what each subscription
// owes; the engine sends it with the subscription's next report. The
// priority does not decide when: an event that an urgent path (isUrgent)
// of the subscriber covers asks for a report, which honours the
// subscription's MinIntervalFloor; any other event leaves with the next
// report — an attribute change's, an urgent event's or the keep-alive's
// ([subscription.Manager.OnEventFired]).
//
// In addition to the live subscription fan-out, EmitEvent appends
// the event to the bridge's persistent [im.EventLog] so controllers
// that send a ReadRequest with EventRequests (e.g. chip-tool's
// `read-event-by-id`, Apple MTRDevice liveness checks) can retrieve
// historical events — Matter §10.6.6 requires Critical events to be
// buffered and answerable out-of-band from live subscriptions.
//
// `endpoint` is the Matter endpoint ID hosting the cluster
// (assigned by the topology assembler). `cluster` is the cluster
// ID (e.g. 0x003B Switch). `event` is the event ID per the cluster
// spec (e.g. 0x00 InitialPress).
func (b *Bridge) EmitEvent(endpoint uint16, cluster, event uint32, data any, priority contract.EventPriority) {
	// Append to the persistent log first (always, even with no active
	// subscriptions) so retrospective Read-Event requests see the event.
	// The log allocates its own monotonic number internally; we read it
	// back to use the same number in the live fan-out so the
	// EventNumber is consistent across both delivery paths.
	rec := im.EventRecord{
		Priority: im.EventPriority(priority),
		Endpoint: endpoint,
		Cluster:  cluster,
		EventID:  event,
		Payload:  data,
	}
	// Append and fan-out as one step: subscriptions see numbers in
	// ascending order, so a report never skips an event that lost the
	// race to a later one. The manager is read before emitMu is taken,
	// so emitMu is never held while waiting for b.mu.
	m := b.subscriptionManagerLocked()
	rec = b.recordAndFanOut(m, rec)
	if hook := b.onEvent.Load(); hook != nil {
		(*hook)(rec)
	}
}

// recordAndFanOut appends rec to the event log and tells the subscriptions
// under emitMu, and returns the record as the log stamped it (its number
// and timestamp).
func (b *Bridge) recordAndFanOut(m *subscription.Manager, rec im.EventRecord) im.EventRecord {
	b.emitMu.Lock()
	defer b.emitMu.Unlock()
	rec = b.eventLog.AppendRecord(rec)
	if m == nil {
		return rec
	}
	m.OnEventFired(subscription.EventFiring{
		Path: im.ConcreteEventPath{
			HasEndpoint: true, HasCluster: true, HasEvent: true,
			Endpoint: rec.Endpoint, Cluster: rec.Cluster, Event: rec.EventID,
		},
		Number:   rec.Number,
		Priority: rec.Priority,
	})
	return rec
}

// MatterEmitEvent is an alias of [Bridge.EmitEvent] satisfying
// [contract.EventEmitter] — the cluster-side hook signature
// the bridge passes to model-side receivers via SetMatterEventEmitter.
func (b *Bridge) MatterEmitEvent(endpoint uint16, cluster, event uint32, data any, priority contract.EventPriority) {
	b.EmitEvent(endpoint, cluster, event, data, priority)
}

// readAuthorizedResults runs dispatcher.Read for path and returns only
// the results the requesting subject (carried in ctx via
// [im.WithFabricFilter] / [im.WithSubject] / [im.WithAuthModePASE]) is
// authorized to read, mirroring the per-result ACL gate in
// [im.HandleReadRequest]. A PASE session — fabricIndex==0 before AddNOC,
// [im.IsPASEFromContext] after it adopted the session onto the new fabric
// — or a dispatcher without an [im.ACLChecker] returns every result
// unchanged.
//
// This closes the subscribe-path ACL bypass: unlike HandleReadRequest,
// the subscription read paths (initial + ongoing) call dispatcher.Read
// directly, so without this gate a View-only or ACE-less subject could
// subscribe to fabric-sensitive attributes (AccessControl.ACL,
// OperationalCredentials.NOCs) and have the engine tick stream them.
// matter.js gates every subscription report through the same read
// authorization (ServerSubscription re-applies the read-response
// authority check). Unauthorized attributes are dropped from the report
// — a subscription simply does not cover paths the subject cannot read.
func (b *Bridge) readAuthorizedResults(ctx context.Context, dispatcher im.Dispatcher, path im.ConcreteAttributePath) []im.ReadResult {
	// A wildcard path's WildcardPathFlags skip their paths in the
	// subscription's reports as in a read (AttributeReadResponse.ts:149-197).
	results := im.ApplyWildcardPathFlags(im.WithElementQualities(ctx, specElementQualities{}), dispatcher, path, dispatcher.Read(ctx, path))
	_, fabricIndex := im.FabricFilterFromContext(ctx)
	if im.IsPASEFromContext(ctx) {
		return results // PASE — commissioning channel, implicit Administer
	}
	aclChecker, hasACL := dispatcher.(im.ACLChecker)
	if !hasACL {
		return results
	}
	if fabricIndex == 0 {
		// No accessing fabric and not PASE: no subject the ACL could grant
		// anything to (matter.js FabricAccessControl.ts:189-191 keys the
		// implicit grant on the PASE auth mode only; review 2026-11
		// W2-sec-03).
		return nil
	}
	subjectNodeID, subjectCATs := im.SubjectFromContext(ctx)
	privProvider, hasPriv := dispatcher.(im.AttributeReadPrivilegeProvider)
	authorized := make([]im.ReadResult, 0, len(results))
	for _, rr := range results {
		var priv uint8 = 1 // View default
		if hasPriv {
			priv = privProvider.MinReadPrivilege(rr.Path.Endpoint, rr.Path.Cluster, rr.Path.Attribute)
		}
		if status := aclChecker.CheckACL(ctx, fabricIndex, subjectNodeID, subjectCATs, rr.Path.Endpoint, rr.Path.Cluster, priv); !status.IsSuccess() {
			continue // subject not permitted to read this attribute — drop
		}
		authorized = append(authorized, rr)
	}
	return authorized
}

// reportSubscription assembles + ships a ReportData for sub. Called
// by the manager's engine tick.
func (b *Bridge) reportSubscription(ctx context.Context, sub *subscription.Subscription, paths []im.ConcreteAttributePath) {
	dispatcher := b.Dispatcher()
	if dispatcher == nil || sub == nil {
		return
	}
	target, ok := b.subscriptionTargetOf(sub)
	if !ok {
		return
	}

	// The engine's stop ends the wait for a chunk's answer (W2-conc-06).
	target.abort = ctx.Done()

	report := im.ReportData{
		HasSubscription: true,
		SubscriptionID:  sub.ID,
		Reports:         b.subscriptionAttributeReports(ctx, dispatcher, target, sub, paths),
	}

	// An empty report is a max-interval keepalive (nothing changed, or every
	// dirty path was dropped by the ACL gate). matter.js ships empty
	// DataReports with SuppressResponse=true (ServerSubscription.ts:782) so
	// the controller does not owe an IM StatusResponse for a no-op heartbeat.
	if len(report.Reports) == 0 {
		report.SuppressResponse = true
	}

	counters, freshExch, err := b.sendInitiatedReport(target, report)
	if err != nil {
		// Mark the drained paths dirty again, so the next report
		// retries them; the third consecutive failure reaps the
		// subscription. Mirrors matter.js
		// packages/node/src/node/server/ServerSubscription.ts:566-577
		// (#addOutstandingAttributes while #sendUpdateErrorCounter <= 2).
		sub.RequeueAttributes(paths)
		if errors.Is(err, errChunkWaitAborted) {
			return // the engine stopped; the peer did nothing wrong
		}
		b.noteSubscriptionSendError(sub.ID, "report", err)
		return
	}
	// Successful send — reset the per-subscription error counter so a
	// later transient hiccup starts at 1 again.
	b.subSendErrorCount.Delete(sub.ID)
	// Record the counter→subID mapping so the ACK pump can close
	// the subscription if the peer never ACKs. Best-effort sends
	// (NeedsAck=false / no tracker) yield no counters; every chunk of a
	// split report gets its own entry because any one of them can be
	// the datagram the peer stops acking.
	for _, counter := range counters {
		b.reportCounterOwner.Store(reportCounterKey(target.sessionID, counter), sub.ID)
	}
	b.rememberReportExchange(sub.ID, target.sessionID, freshExch)
	b.releaseReportOfClosed(sub)
	b.logger.Debug("matter.tx.subscribe.report",
		slog.Int("subscription_id", int(sub.ID)),
		slog.Int("paths", len(paths)),
		slog.Int("reports", len(report.Reports)),
		slog.Int("exchange_id", int(freshExch)))
}

// subscriptionAttributeReports reads paths for sub — the changed ones its
// wildcard flags do not skip — and returns the attribute reports the
// subscribing subject may read, under the identity captured on target.
// Shared by the ongoing report and the flush.
func (b *Bridge) subscriptionAttributeReports(ctx context.Context, dispatcher im.Dispatcher, target subTarget, sub *subscription.Subscription, paths []im.ConcreteAttributePath) []im.AttributeReport {
	// Authorize + fabric-project every ongoing report against the
	// identity captured at Subscribe time, so a CASE subject cannot
	// keep receiving attributes it is not permitted to read (or another
	// fabric's fabric-scoped rows). Mirrors the subCtx stamping the
	// initial read applies in handleSubscribeRequest.
	readCtx := im.WithFabricFilter(ctx, target.fabricFiltered, target.fabricIndex)
	readCtx = im.WithSubject(readCtx, target.subjectNodeID, target.subjectCATs)
	if target.pase {
		readCtx = im.WithAuthModePASE(readCtx)
	}
	var reports []im.AttributeReport
	for _, path := range b.unskippedChangedPaths(readCtx, dispatcher, sub, paths) {
		for _, rr := range b.readAuthorizedResults(readCtx, dispatcher, path) {
			rep := im.AttributeReport{Path: rr.Path, DataVersion: rr.DataVersion}
			if rr.Status != im.StatusSuccess {
				rep.IsStatus = true
				rep.Status = im.StatusIB{Status: rr.Status}
			} else {
				rep.Value = rr.Value
			}
			reports = append(reports, rep)
		}
	}
	return reports
}

// reportSubscriptionFlush ships what sub owes as its fabric leaves — the
// drained events and dirty attributes, authorized as the ongoing reports
// are — in one ReportData on a fresh exchange, and waits for the peer's
// answer ([Bridge.awaitFlushedReport]) so the CloseSession that follows
// cannot overtake it. Mirrors matter.js
// packages/node/src/node/server/ServerSubscription.ts:753-763 at db021da0
// (#flush: one #triggerSendUpdate), :534-556 (#sendUpdate takes the
// attribute filter and the events' minimum number together) and :926-936
// (#sendUpdateMessage: one DataReport carrying both, suppressed when
// empty, :935 suppressEmptyReport), reached from Fabric.ts:447-457
// (leave) through NodeSession.ts:312-323 (closeSubscriptions(true)). A
// failed send owes the events and attributes again, as the ongoing paths
// do; the subscription closes with its session right after.
func (b *Bridge) reportSubscriptionFlush(ctx context.Context, sub *subscription.Subscription, events []im.EventReport, paths []im.ConcreteAttributePath) {
	if sub == nil {
		return
	}
	target, ok := b.subscriptionTargetOf(sub)
	if !ok {
		return
	}
	report := im.ReportData{
		HasSubscription: true,
		SubscriptionID:  sub.ID,
	}
	if dispatcher := b.Dispatcher(); dispatcher != nil && len(paths) > 0 {
		report.Reports = b.subscriptionAttributeReports(ctx, dispatcher, target, sub, paths)
	}
	if len(events) > 0 {
		report.EventReports = b.authorizedEventReports(ctx, target, events)
	}
	if len(report.Reports) == 0 && len(report.EventReports) == 0 {
		return
	}
	target.flush = true
	_, freshExch, wait, err := b.sendInitiatedReportArmed(target, report, true)
	if err != nil {
		if len(events) > 0 {
			sub.RequeueEvents(events[0].Number)
		}
		if len(paths) > 0 {
			sub.RequeueAttributes(paths)
		}
		b.noteSubscriptionSendError(sub.ID, "flush", err)
		return
	}
	// The flushed chunks are untracked, so no counter comes back and no
	// counter→subscription entry is owed: nothing retransmits them, and
	// their acks find no entry (outboundReliableTracker.Ack is a no-op).
	b.subSendErrorCount.Delete(sub.ID)
	b.rememberReportExchange(sub.ID, target.sessionID, freshExch)
	b.releaseReportOfClosed(sub)
	b.logger.Debug("matter.tx.subscribe.flush",
		slog.Int("subscription_id", int(sub.ID)),
		slog.Int("events", len(report.EventReports)),
		slog.Int("reports", len(report.Reports)),
		slog.Int("exchange_id", int(freshExch)))
	b.awaitFlushedReport(ctx, "subscribe.flush", target.sessionID, freshExch, wait)
}

// sendErrorRetryLimit is how many consecutive send failures an ongoing
// report path tolerates before the subscription is reaped. matter.js's
// ServerSubscription.ts retries an ongoing report up to 2 times before
// cancelling; only on the 3rd consecutive failure do we evict.
const sendErrorRetryLimit = 2

// noteSubscriptionSendError records one failed ongoing report for subID
// and reaps the subscription once [sendErrorRetryLimit] consecutive
// failures have accumulated. Both ongoing report paths (attributes and
// events) funnel through here: a transient socket-write hiccup, a
// fabric-reload race or a session being re-adopted must not unroute an
// otherwise-healthy subscription, because the routing entry is written
// only by captureSubTarget on a fresh SubscribeRequest — once dropped,
// nothing recreates it and every later report silently returns at the
// subTargets lookup, keep-alives included. A peer that truly vanished
// is still reaped. The MRP tracker path (closeSubscriptionByCounter)
// covers the ACK-timeout case; this covers the immediate send error
// where no MRP tracker is wired.
//
// op names the reporter for the log line ("report" / "event_report").
func (b *Bridge) noteSubscriptionSendError(subID uint32, op string, err error) {
	if _, live := b.routing.subTargets.Load(subID); !live {
		// Reaped while the report was being sent — its first chunk's
		// loss closed the peer: nothing is left to count against.
		return
	}
	var failures int
	if raw, ok := b.subSendErrorCount.Load(subID); ok {
		if n, ok := raw.(int); ok {
			failures = n
		}
	}
	failures++
	if failures <= sendErrorRetryLimit {
		b.subSendErrorCount.Store(subID, failures)
		if b.logger != nil {
			b.logger.Debug("matter.tx.subscribe."+op,
				slog.Int("subscription_id", int(subID)),
				slog.Int("send_failures", failures),
				slog.Int("retry_limit", sendErrorRetryLimit),
				slog.Any("err", err))
		}
		return
	}
	// Cap reached — drop the routing target and evict the subscription
	// from the manager so the engine stops ticking a dead subscription.
	b.routing.subTargets.Delete(subID)
	b.subSendErrorCount.Delete(subID)
	b.forgetReportExchange(subID)
	if m := b.subscriptionManagerLocked(); m != nil {
		_ = m.Close(subID) // ErrNotFound is fine — racing with peer or ACK-pump Close
	}
	if b.logger != nil {
		b.logger.Info("matter.subscribe.peer_unreachable",
			slog.Int("subscription_id", int(subID)),
			slog.Int("send_failures", failures),
			slog.String("op", op),
			slog.String("hint", "consecutive send-error cap reached; subscription reaped"))
	}
}

// closeSubscriptionByCounter is called by the ACK pump when an
// outbound report's counter hit the retransmit cap. The subscription
// it belonged to is therefore unreachable; close it in the manager
// (so quotas free up and the engine stops ticking it) and drop the
// target.
func (b *Bridge) closeSubscriptionByCounter(sessionID uint16, counter uint32) {
	raw, ok := b.reportCounterOwner.LoadAndDelete(reportCounterKey(sessionID, counter))
	if !ok {
		return
	}
	subID, ok := raw.(uint32)
	if !ok || subID == 0 {
		return
	}
	b.routing.subTargets.Delete(subID)
	b.subSendErrorCount.Delete(subID)
	b.forgetReportExchange(subID)
	if m := b.subscriptionManagerLocked(); m != nil {
		_ = m.Close(subID) // ErrNotFound is fine — racing with peer Close
	}
	if b.logger != nil {
		b.logger.Info("matter.subscribe.peer_unreachable",
			slog.Int("subscription_id", int(subID)),
			slog.String("hint", "max retransmissions reached; subscription reaped"))
	}
}

// rememberReportExchange records that subID's latest ongoing report
// rides the bridge-initiated exchange (sessionID, exchangeID), replacing
// the record of the previous report so the table holds one entry per
// live subscription. Consulted by [Bridge.closeSubscriptionByExchange]
// when the peer answers on that exchange.
func (b *Bridge) rememberReportExchange(subID uint32, sessionID, exchangeID uint16) {
	key := mrp.ExchangeKey{SessionID: sessionID, ExchangeID: exchangeID, Initiator: true}
	if prev, loaded := b.subReportExchange.Swap(subID, key); loaded {
		if prevKey, ok := prev.(mrp.ExchangeKey); ok && prevKey != key {
			b.reportExchangeOwner.Delete(prevKey)
			b.reportExchangeOrigins.Delete(prevKey)
		}
	}
	b.reportExchangeOwner.Store(key, subID)
}

// releaseReportOfClosed drops what a reporter just recorded for sub —
// the report's exchange and counters — when sub closed while the report
// was on its way. The reporter loaded sub's routing target before the
// send; a close landing after that has run
// [Bridge.releaseSubscriptionRouting] already, and nothing would forget
// what the reporter stored afterwards (review II, W2-conc-11). A close
// after this check finds the records and releases them itself.
func (b *Bridge) releaseReportOfClosed(sub *subscription.Subscription) {
	if sub.IsClosed() {
		b.releaseSubscriptionRouting(sub.ID)
	}
}

// forgetReportExchange drops both halves of subID's report-exchange
// record. Every reap path calls it so a reused exchange id can never be
// attributed to a subscription that is already gone.
func (b *Bridge) forgetReportExchange(subID uint32) {
	if prev, loaded := b.subReportExchange.LoadAndDelete(subID); loaded {
		if prevKey, ok := prev.(mrp.ExchangeKey); ok {
			b.reportExchangeOwner.Delete(prevKey)
			b.reportExchangeOrigins.Delete(prevKey)
		}
	}
}

// reportExchangeOrigin is when an ongoing report's exchange was opened
// and the counter of the first message sent on it.
type reportExchangeOrigin struct {
	// createdAtMono is the clock's monotonic reading when the report's
	// exchange was opened — the cutoff of the peer-loss sweep it may
	// trigger (matter.js's asOf, Time.nowUs).
	createdAtMono time.Duration
	firstCounter  uint32
}

// closeSubscriptionByExchange ends the subscription whose latest ongoing
// report rode the bridge-initiated exchange (sessionID, exchangeID),
// because the peer answered that report with the error status given.
// A report exchange nobody remembers (an initial-report chunk, a
// timed-request reply, a subscription already reaped) is ignored.
// Mirrors matter.js ServerSubscription.ts:866-876: InvalidSubscription
// and Failure mark the subscription terminated, and #closeFromUpdate
// cancels it for any error status.
func (b *Bridge) closeSubscriptionByExchange(sessionID, exchangeID uint16, status im.StatusCode) {
	raw, ok := b.reportExchangeOwner.Load(mrp.ExchangeKey{SessionID: sessionID, ExchangeID: exchangeID, Initiator: true})
	if !ok {
		return
	}
	subID, ok := raw.(uint32)
	if !ok || subID == 0 {
		return
	}
	b.routing.subTargets.Delete(subID)
	b.subSendErrorCount.Delete(subID)
	b.forgetReportExchange(subID)
	if m := b.subscriptionManagerLocked(); m != nil {
		_ = m.Close(subID) // ErrNotFound is fine — racing with peer Close
	}
	if b.logger != nil {
		b.logger.Info("matter.subscribe.peer_rejected",
			slog.Int("subscription_id", int(subID)),
			slog.Int("exchange_id", int(exchangeID)),
			slog.String("status", status.String()),
			slog.String("hint", "peer answered an ongoing report with an error status; subscription closed"))
	}
}

// releaseReportCounter clears the (session, counter)→subID mapping
// after a successful peer ACK. Caller is the inbound HasAck pipeline,
// which supplies the session from the received message header.
func (b *Bridge) releaseReportCounter(sessionID uint16, counter uint32) {
	if counter == 0 {
		return
	}
	b.reportCounterOwner.Delete(reportCounterKey(sessionID, counter))
}

// reportCounterKey composes (sessionID, counter) into the
// [Bridge.reportCounterOwner] map key. Two concurrent sessions can
// legitimately reuse the same 32-bit MRP counter (each is seeded from
// an independent random per Matter §4.5.4), so the subscription-owner
// lookup must be session-scoped — otherwise session A's ACK could
// release / reap the subscription session B owns.
func reportCounterKey(sessionID uint16, counter uint32) uint64 {
	return uint64(sessionID)<<32 | uint64(counter)
}

// captureSubTarget records the routing metadata for sub on the bridge
// so reportSubscription can find it later. Overwrites any prior entry
// with the same subID — recycled IDs are rare in practice but the
// engine guarantees ordering of Subscribe-then-tick on the same ID.
func (b *Bridge) captureSubTarget(subID uint32, src net.Addr, requestHdr *message.Header, proto message.ProtocolHeader, fabricFiltered bool) {
	if subID == 0 || isNilRoute(src) || requestHdr == nil {
		return
	}
	// Resolve the requesting fabric + subject the same way the initial
	// read did (handleSubscribeRequest), so ongoing reports authorize
	// against the exact identity that opened the subscription.
	fabricIndex := b.resolveSessionFabric(requestHdr.SessionID)
	subjectNodeID, subjectCATs := b.resolveSessionSubject(requestHdr.SessionID)
	b.routing.subTargets.Store(subID, subTarget{
		src:                 src,
		hasPeerSourceNodeID: requestHdr.HasSourceNodeID,
		peerSourceNodeID:    requestHdr.SourceNodeID,
		exchangeID:          proto.ExchangeID,
		sessionID:           requestHdr.SessionID,
		peerInitiator:       proto.Initiator,
		fabricIndex:         fabricIndex,
		subjectNodeID:       subjectNodeID,
		subjectCATs:         subjectCATs,
		fabricFiltered:      fabricFiltered,
		pase:                b.resolveSessionPASE(requestHdr.SessionID),
	})
}

// nextOutboundExchangeID allocates a fresh 15-bit exchange identifier
// for a server-initiated exchange. Mirrors matter.js's
// `ExchangeManager.#getNextExchangeId` (packages/protocol/src/
// protocol/ExchangeManager.ts). The high bit (0x8000) is reserved by
// Matter §4.13 as the Initiator flag and is set in the protocol
// header on the wire, not embedded in the exchange ID itself.
//
// Counter wraps via modulo 0x7FFF + 1; the bridge re-uses an ID only
// after the entire 15-bit space has been consumed, which keeps the
// risk of an ID collision with a still-live commissioner exchange
// negligible (the longest-running subscription is bounded by Apple's
// MaxIntervalCeiling ~600 s, far below the wrap time at any realistic
// outbound rate).
func (b *Bridge) nextOutboundExchangeID() uint16 {
	for {
		next := b.outboundExchangeID.Add(1)
		exch := uint16((next & 0x7FFF) & 0xFFFF)
		if exch != 0 {
			return exch
		}
	}
}

// sendReportChunks splits report into per-datagram chunks and ships
// them in order on target's exchange, returning the MRP counter of
// every chunk that was tracked (empty when the bridge sends
// best-effort). All chunks but the last carry MoreChunkedMessages
// (Matter §10.6.6), which [chunkReportData] stamps.
//
// Chunking is not optional on any ReportData path: the UDP listener
// rejects a datagram above its size cap outright, and the subscription
// engine has already drained and cleared the dirty set / event queue
// by the time a reporter runs — an oversized report therefore loses
// every change it carried, with no re-queue behind it. matter.js runs
// ongoing updates through the same chunking messenger as the initial
// report
// (packages/protocol/src/interaction/InteractionMessenger.ts:347
// sendDataReport).
//
// Like [Bridge.streamInitialReportChunks] it waits for the peer's IM
// StatusResponse to each chunk but the last before sending the next — the
// Matter chunking handshake (§10.6.6), and what matter.js does for an
// ongoing report too (InteractionMessenger.ts sendDataReport →
// waitForSuccess). It used to send every chunk back to back to keep the
// engine's tick goroutine free; a controller that is still waiting for
// the ack of its StatusResponse to chunk N then drops chunk N+1 ("Dropping
// message without piggyback ack when we are waiting for an ack"), and with
// it every change that chunk carried. The CHIP Python harness lost a dozen
// attribute reports to it (TC-IDM-4.3 step 10). A single-chunk report — the
// common case — still does not wait: its StatusResponse is consumed by the
// subscription's own report bookkeeping.
func (b *Bridge) sendReportChunks(target subTarget, report im.ReportData) ([]uint32, error) {
	counters, _, err := b.sendReportChunksNotify(target, report, nil, false)
	return counters, err
}

// sendReportChunksNotify is [Bridge.sendReportChunks] that hands the MRP
// counter of the first chunk to firstSent (nil: no one) once it is sent
// and tracked — before the wait for the peer's StatusResponse to it, so
// the owner can register the counter while the exchange has heard nothing
// from the peer yet. With armFinal it also arms the wait for the peer's
// StatusResponse to the last chunk just before that chunk goes out and
// returns it as finalWait — the waits for the chunks before it are this
// loop's own, on the same key, so arming the final wait up front would
// have the first of them replace and close it. A failed send disarms it.
func (b *Bridge) sendReportChunksNotify(target subTarget, report im.ReportData, firstSent func(counter uint32), armFinal bool) (counters []uint32, finalWait <-chan im.StatusCode, err error) {
	limits := b.chunkLimitsFor(target.src, target.sessionID)
	chunks, err := chunkReportData(report, limits.budget, limits.hardCap)
	if err != nil {
		debugReplyError(b.logger, "chunk_ongoing_report", target.src, err)
		return nil, nil, err
	}
	counters = make([]uint32, 0, len(chunks))
	ourRole := !target.peerInitiator
	op := "subscribe.ongoing"
	if target.flush {
		op = "subscribe.flush"
	}
	for i, chunk := range chunks {
		body, err := EncodeReportData(chunk)
		if err != nil {
			debugReplyError(b.logger, "encode_ongoing_report", target.src, err)
			return nil, nil, err
		}
		var waitCh <-chan im.StatusCode
		switch {
		case chunk.MoreChunkedMessages:
			waitCh = b.armStatusResponseWait(target.sessionID, target.exchangeID, ourRole)
		case armFinal:
			// The last chunk's answer is awaited too, by the caller:
			// matter.js's sendDataReportMessage waits for the Success to
			// every chunk it sends, the last included — Millis(500) for a
			// closed subscription's report (packages/protocol/src/
			// interaction/InteractionMessenger.ts:799-844, :844 at
			// db021da0). Armed before the send, so the answer cannot land
			// before the waiter exists.
			finalWait = b.armStatusResponseWait(target.sessionID, target.exchangeID, ourRole)
		}
		if i > 0 {
			// Chunk N+1 piggybacks the ack of the peer's StatusResponse
			// to chunk N. chip's ReliableMessageMgr drops it otherwise
			// ("Dropping message without piggyback ack when we are
			// waiting for an ack") — the same rule
			// [Bridge.streamInitialReportChunks] honours through
			// refreshAckCounter. matter.js gets it for free: its
			// MessageExchange piggybacks the pending ack on the next send
			// (packages/protocol/src/protocol/MessageExchange.ts:679 send).
			target = b.piggybackOwedAck(target, ourRole)
		}
		counter, err := b.sendUnsolicitedIM(target, im.OpcodeReportData, body)
		if err != nil {
			if waitCh != nil || finalWait != nil {
				b.disarmStatusResponseWait(target.sessionID, target.exchangeID, ourRole)
			}
			return nil, nil, err
		}
		if counter != 0 {
			counters = append(counters, counter)
			if i == 0 && firstSent != nil {
				firstSent(counter)
			}
		}
		if waitCh != nil {
			timeout := b.chunkStatusResponseTimeout(target.sessionID)
			if target.flush {
				timeout = flushStatusResponseTimeout
			}
			if err := b.awaitChunkStatusResponseWithin(timeout, target.abort, waitCh, op, target.src, target.sessionID, target.exchangeID, ourRole, i, chunk); err != nil {
				return counters, nil, err
			}
		}
	}
	return counters, finalWait, nil
}

// sendInitiatedReport ships report on a **fresh bridge-initiated
// exchange** instead of reusing the commissioner's Subscribe exchange.
// Use this for ongoing subscribe reports (matter.js's
// ServerSubscription.#sendUpdateMessage pattern,
// packages/node/src/node/server/ServerSubscription.ts:764). Every
// chunk of one report rides the same exchange — a chunk sequence is a
// single IM transaction. Returns the allocated exchange ID so the
// caller can correlate the signalStatusResponseRX call back into the
// per-subscription state machine.
//
// Background: ongoing reports on the original Subscribe exchange are
// MRP-ACKed by Apple at the SecureChannel layer but never elicit the
// IM StatusResponse the spec mandates for non-empty ReportData
// (§10.7.5). Empirically Apple's HMOutlet projection trusts ongoing
// state-change reports only after the StatusResponse handshake
// completes, so reusing the Subscribe exchange leaves external state
// changes invisible in the UI. Opening a fresh exchange triggers the
// full IM handshake on Apple's side. matter.js handles the parallel
// "subscription stays alive across exchanges" expectation via the
// negotiated SubscriptionID — which matches the commissioner's
// expectation regardless of which exchange carries the report.
func (b *Bridge) sendInitiatedReport(target subTarget, report im.ReportData) (counters []uint32, exchangeID uint16, err error) {
	counters, exchangeID, _, err = b.sendInitiatedReportArmed(target, report, false)
	return counters, exchangeID, err
}

// sendInitiatedReportArmed is [Bridge.sendInitiatedReport] that, with
// arm, also arms the wait for the peer's StatusResponse to the report's
// last chunk on the fresh exchange, just before that chunk goes out
// ([Bridge.sendReportChunksNotify] armFinal), so the answer cannot land
// before the waiter exists; the caller receives on wait through
// [Bridge.awaitFlushedReport], which disarms it. A failed send disarms
// it here.
func (b *Bridge) sendInitiatedReportArmed(target subTarget, report im.ReportData, arm bool) (counters []uint32, exchangeID uint16, wait <-chan im.StatusCode, err error) {
	freshExchangeID := b.nextOutboundExchangeID()
	freshTarget := target
	freshTarget.exchangeID = freshExchangeID
	// Set peerInitiator=false so sendUnsolicitedIM's
	// `Initiator: !target.peerInitiator` evaluates to true — we are the
	// exchange initiator on this fresh exchange.
	freshTarget.peerInitiator = false
	// The exchange context's creation time bounds a peer-loss sweep this
	// report may trigger (matter.js ExchangeManager.ts:549, :576 asOf).
	createdAtMono := clock.MonotonicOf(b.clk())
	key := mrp.ExchangeKey{SessionID: target.sessionID, ExchangeID: freshExchangeID, Initiator: true}
	// The first message's counter belongs to the report's exchange from
	// the moment it is sent, not from when the whole report is out: a
	// chunked report waits for the peer's StatusResponse to its first
	// chunk, and the first chunk going unacknowledged there is peer loss
	// like any first message of an exchange this node opened — matter.js
	// MessageExchange.ts:605-624 `send` reports `peerLost` on a failed
	// send when `#reportsPeerLoss` (:626-633: nothing received on the
	// exchange yet, `#messageReceivedCounter === 0`). Later chunks are
	// registered by the caller once the report is out, as before.
	var firstCounter uint32
	counters, wait, err = b.sendReportChunksNotify(freshTarget, report, func(counter uint32) {
		firstCounter = counter
		b.reportExchangeOrigins.Store(key, reportExchangeOrigin{createdAtMono: createdAtMono, firstCounter: counter})
		if report.SubscriptionID != 0 {
			b.reportCounterOwner.Store(reportCounterKey(target.sessionID, counter), report.SubscriptionID)
		}
	}, arm)
	if err != nil {
		if arm {
			b.disarmStatusResponseWait(target.sessionID, freshExchangeID, true)
		}
		if firstCounter != 0 {
			b.reportExchangeOrigins.Delete(key)
			b.reportCounterOwner.Delete(reportCounterKey(target.sessionID, firstCounter))
		}
		return nil, 0, nil, err
	}
	return counters, freshExchangeID, wait, nil
}

// flushStatusResponseTimeout bounds the wait for the peer's answer to a
// report flushed while its fabric leaves — each chunk and the report as a
// whole. matter.js's flushing subscription is already closed
// (ServerSubscription.ts:776-788 #close: :800-809 #markClosed sets
// #isClosed, then :816-825 #cancel flushes), so its DataReport goes out
// with `waitForAck: !this.#isClosed` = false (:919, :934 at db021da0),
// and sendDataReportMessage then waits `Millis(500)` for the Success
// instead of the MRP-derived bound (InteractionMessenger.ts:799-844, the
// timeout at :844). The bound does not depend on the transport, as
// matter.js's does not.
const flushStatusResponseTimeout = 500 * time.Millisecond

// awaitFlushedReport waits for the peer's StatusResponse to a flushed
// report — one sent while its fabric leaves, before the fabric's
// sessions close — so the peer has taken the report before the
// CloseSession follows it: a CloseSession overtaking the report leaves
// the report on a session the peer no longer knows, and the Leave in it
// never arrives. matter.js's flush awaits the update it triggered
// (ServerSubscription.ts:753-763 at db021da0, #flush awaits
// #currentUpdatePromise) and the update awaits the answer to each
// DataReport (InteractionMessenger.ts:799-844, sendDataReportMessage
// ends in waitForSuccess). The wait ends with the answer, with ctx, or
// after [flushStatusResponseTimeout]; the answer's standalone ack goes
// out at once, as for a StatusResponse no chunk loop waited for
// (receive_dispatch's absorbStatusResponse).
func (b *Bridge) awaitFlushedReport(ctx context.Context, op string, sessionID, exchangeID uint16, wait <-chan im.StatusCode) {
	timeout := flushStatusResponseTimeout
	timer := b.clk().NewTimer(timeout)
	defer timer.Stop()
	defer b.disarmStatusResponseWait(sessionID, exchangeID, true)
	select {
	case status := <-wait:
		b.expediteDuplicateAck(sessionID, exchangeID, true)
		b.RunAckPumpOnce(b.clk().Now())
		b.logger.Debug("matter.tx."+op+".flush_answered",
			slog.Int("exchange_id", int(exchangeID)),
			slog.String("status", status.String()))
	case <-ctx.Done():
		b.logger.Debug("matter.tx."+op+".flush_abandoned",
			slog.Int("exchange_id", int(exchangeID)),
			slog.Any("err", ctx.Err()))
	case <-timer.C():
		b.logger.Debug("matter.tx."+op+".flush_unanswered",
			slog.Int("exchange_id", int(exchangeID)),
			slog.String("timeout", timeout.String()))
	}
}

// piggybackOwedAck returns target with the ack the bridge owes the peer on
// target's exchange attached, discharging the obligation so the ack pump
// does not also send it standalone. target is returned unchanged when
// nothing is owed or no tracker is wired.
func (b *Bridge) piggybackOwedAck(target subTarget, ourRole bool) subTarget {
	if counter, ok := b.takeOwedAck(target.sessionID, target.exchangeID, ourRole); ok {
		target.hasAck = true
		target.ackCounter = counter
	}
	return target
}

func (b *Bridge) sendUnsolicitedIM(target subTarget, opcode uint8, payload []byte) (uint32, error) {
	b.load.imSent.Add(1)
	b.count(contract.MetricIMSent)
	b.mu.RLock()
	listener := b.listener
	sessions := b.sessions
	tracker := b.outboundReliable
	b.mu.RUnlock()
	// Route to the address the peer most recently used on this session,
	// not the one frozen into the target at Subscribe time: a controller
	// whose source address rotates mid-session keeps authenticating from
	// the new one, and matter.js adopts it ("the new message wins",
	// ExchangeManager.ts:425-431) before routing reports through the
	// session's current channel (ServerSubscription.ts:823).
	dest := b.currentPeerAddr(target.sessionID, target.src)
	if listener == nil && !isConnRoute(dest) {
		return 0, ErrUnsolicitedListenerMissing
	}
	// Over a reliable connection MRP is off: no R flag, no piggybacked
	// acknowledgement (matter.js MessageExchange.ts:665, :685-691).
	usesMRP := routeUsesMRP(dest)

	// sendUnsolicitedIM is the low-level primitive that ships an IM
	// datagram on whichever exchange the caller has prepared in `target`.
	// sendReportChunks is the caller; both ongoing reporters (attributes
	// and events) and the flush reach it through sendInitiatedReport or
	// sendInitiatedReportArmed, which forge a fresh bridge-opened exchange
	// (target.peerInitiator=false → Initiator=true). A target with peerInitiator=true would answer on
	// the peer-opened exchange instead — only meaningful while that
	// exchange is still open on the peer's side.
	respProto := message.ProtocolHeader{
		Initiator:  !target.peerInitiator, // caller picks via target.peerInitiator
		Opcode:     opcode,
		ExchangeID: target.exchangeID,
		ProtocolID: im.InteractionModelProtocolID,
		// NeedsAck only when the bridge can track the obligation;
		// without a tracker the peer would block on a reply that
		// nothing rebroadcasts on loss.
		NeedsAck: tracker != nil && usesMRP,
	}
	if target.hasAck && usesMRP {
		respProto.HasAck = true
		respProto.AckCounter = target.ackCounter
	}
	body := append(respProto.Marshal(), payload...) //nolint:gocritic // single-allocation join

	respHdr := message.Header{
		SessionID: target.sessionID,
	}
	if target.hasPeerSourceNodeID {
		respHdr.DestSize = message.DestNodeID
		respHdr.DestNodeID = target.peerSourceNodeID
	}

	var datagram []byte
	var counter uint32
	if target.sessionID == 0 {
		counter = b.nextUnsecuredCounter()
		respHdr.MessageCounter = counter
		datagram = append(respHdr.Marshal(), body...) //nolint:gocritic // single-allocation join
	} else {
		if sessions == nil {
			return 0, ErrUnsolicitedSessionMissing
		}
		sess, ok := sessions.Lookup(target.sessionID)
		if !ok {
			return 0, ErrUnsolicitedSessionMissing
		}
		// Stamp the peer's SessionID so their inbound table resolves;
		// see [Bridge.sendReply] for the parallel rationale.
		if peerID := sess.PeerSessionID(); peerID != 0 {
			respHdr.SessionID = peerID
		}
		enc, err := b.encryptSecureOutbound(sess, target.sessionID, &respHdr, body)
		if err != nil {
			return 0, fmt.Errorf("%w: %w", ErrUnsolicitedEncrypt, err)
		}
		// Encrypt allocates the message counter from the session's
		// Tx counter — read it back from the encoded header so the
		// retransmit tracker stores the canonical value.
		counter = respHdr.MessageCounter
		datagram = append(respHdr.Marshal(), enc.Ciphertext...)
	}

	// Tracked before it is sent, so an ack handled before Send returns is
	// not lost (bridge/reply.go sendReplyReliable). A flushed report
	// ([subTarget.flush]) still asks for the ack but is sent once: no
	// retransmission, no ack wait — matter.js's `disableMrpLogic`
	// (packages/protocol/src/protocol/MessageExchange.ts:785-798 at
	// db021da0: no retransmission timer and no ack promise for it), which
	// sendDataReportMessage sets for a closed subscription's report
	// (InteractionMessenger.ts:799-844, `disableMrpLogic: !waitForAck`).
	// The peer's ack then finds no
	// entry, a no-op ([outboundReliableTracker.Ack]).
	track := tracker != nil && respProto.NeedsAck && !target.flush
	if track {
		tracker.Track(counter, target.sessionID, target.exchangeID, datagram, dest, b.clk().Now())
	}
	if err := sendVia(listener, dest, datagram); err != nil {
		if track {
			tracker.Untrack(target.sessionID, counter)
		}
		return 0, err
	}
	if track {
		return counter, nil
	}
	return 0, nil
}

// currentPeerAddr returns the last authenticated source address seen on
// sessionID, falling back to fallback when none was recorded (session 0
// has no authenticated peer, and a test may plant a target without any
// inbound traffic). The record is written by the receive pipeline for
// every datagram that decrypts under the session.
func (b *Bridge) currentPeerAddr(sessionID uint16, fallback net.Addr) net.Addr {
	if sessionID == 0 {
		return fallback
	}
	if raw, ok := b.sessionPeerAddrs.Load(sessionID); ok {
		if addr, ok := raw.(net.Addr); ok && !isNilRoute(addr) {
			return addr
		}
	}
	return fallback
}

// handleSubscribeRequest is the Subscribe-opcode branch of the IM
// dispatcher. Exposed as a method so the receive-pipeline test can
// drive it without spinning up a real UDP listener.
//
// Orchestrates the Subscribe flow in four steps — each extracted into
// its own method in subscribe_dispatch.go (mirrors the ADR 0031 pattern
// for handleIMOpcode → receive_dispatch.go):
//  1. buildInitialReport — read all requested paths + events, sort.
//  2. registerSubscription — manager Subscribe + KeepSubscriptions teardown.
//  3. streamInitialReportChunks — chunked ReportData send with per-chunk ack wait.
//  4. sendSubscribeResponse — SubscribeResponse with piggyback ack + TouchLastReport.
//
// abandonPrimingSubscription closes a subscription whose priming report
// or SubscribeResponse did not go out: the controller never saw it
// established, and a priming subscription would otherwise sit silent in
// the manager until its session closed.
func (b *Bridge) abandonPrimingSubscription(subID uint32) {
	if m := b.subscriptionManagerLocked(); m != nil && subID != 0 {
		_ = m.Close(subID)
	}
}

func (b *Bridge) handleSubscribeRequest(
	ctx context.Context,
	src net.Addr,
	requestHdr *message.Header,
	proto message.ProtocolHeader,
	req im.SubscribeRequest,
) error {
	dispatcher := b.Dispatcher()
	if dispatcher == nil {
		// Topology not assembled — drop with debug log; commissioner
		// retries via MRP.
		b.logger.Debug("matter.rx.im.subscribe.no_dispatcher",
			slog.String("src", srcString(src)))
		return nil
	}

	// Reject illegal paths up front with a top-level InvalidAction
	// StatusResponse (wildcard cluster + concrete non-global attribute, or
	// wildcard cluster + concrete event), and a request beyond the path
	// ceiling with PathsExhausted, before building any report. Mirrors
	// matter.js InteractionServer.ts validateReadPaths (#3926, Matter
	// §8.4.3.2), which gates Read and Subscribe through the same check,
	// and the MAX_SUBSCRIBE_PATHS check that follows it (:600-604).
	if status := im.ValidateReadPaths(req.AttributeRequests, req.EventRequests); status != im.StatusSuccess {
		return b.rejectSubscribeStatus(src, requestHdr, proto, "path", status)
	}

	// A Subscribe naming no attribute and no event paths at all is
	// InvalidAction — matter.js InteractionServer.ts:628-633 ("No
	// attributes or events requested").
	if len(req.AttributeRequests) == 0 && len(req.EventRequests) == 0 {
		return b.rejectSubscribeInvalidAction(src, requestHdr, proto, "empty")
	}

	// Stamp the FabricFiltered flag + requesting FabricIndex into the
	// context for the initial ReportData pass. Subscribe requests carry
	// FabricFiltered the same way ReadRequests do (Matter §10.6.3);
	// fabric-scoped cluster servers (OperationalCredentials.Fabrics,
	// AccessControl.ACL) must project their lists accordingly.
	// Mirrors matter.js InteractionServer.ts:startReadInteraction →
	// OnlineContext.forFabricFilteredRead.
	subFabricIndex := b.resolveSessionFabric(requestHdr.SessionID)
	subSubjectNodeID, subSubjectCATs := b.resolveSessionSubject(requestHdr.SessionID)
	subCtx := im.WithFabricFilter(ctx, req.FabricFiltered, subFabricIndex)
	subCtx = im.WithSubject(subCtx, subSubjectNodeID, subSubjectCATs)
	if b.resolveSessionPASE(requestHdr.SessionID) {
		subCtx = im.WithAuthModePASE(subCtx)
	}

	// A normal subscribe from this peer has begun: a re-establishment of
	// its former subscriptions still in flight must leave it alone.
	// Mirrors matter.js InteractionServer.handleSubscribeRequest emitting
	// subscriptionEstablishmentStarted(session.peerAddress) once the
	// request has passed its validation.
	if !b.resolveSessionPASE(requestHdr.SessionID) {
		b.noteSubscriptionEstablishmentStarted(subFabricIndex, subscriptionPeerNodeID(requestHdr, subSubjectNodeID))
	}

	// The subscription is admitted, priming, before its initial data is
	// read: a change made while the data is collected marks it dirty and
	// goes out after the priming report, instead of falling between the
	// read and the admission unseen. matter.js attaches the subscription's
	// change handlers before it reads the initial report
	// (ServerSubscription.ts:731-739 sendInitialReport at 862ad75f) and
	// clears the peer's other subscriptions before that
	// (InteractionServer.ts:647-662); the re-establishment after a restart
	// (subscription_reestablish.go) already admits first.
	var stamp im.ReportData
	subID, regErr := b.registerSubscription(src, requestHdr, proto, req, &stamp)
	if regErr != nil {
		// The manager refused the subscription (per-fabric quota, cadence
		// out of range, cadence inverted after clamp). The controller must
		// hear that as a StatusResponse: a SubscribeResponse would tell it
		// a subscription exists, and it would then wait forever for
		// reports the bridge never sends.
		return b.rejectSubscribeStatus(src, requestHdr, proto, "manager", subscribeRejectStatus(regErr))
	}
	// One EventFilterIB applies to the priming report: the first for this
	// node (matter.js EventReadResponse.ts:68-77; review 2026-11
	// W2-wire-05).
	primingReq := req
	primingReq.EventFilters = im.EventFiltersFor(req.EventFilters, b.localNodeIDFor(requestHdr.SessionID, subFabricIndex))
	initialReport, matchedPaths := b.buildInitialReport(subCtx, dispatcher, primingReq)
	// A Subscribe whose (possibly wildcard) paths match zero attributes
	// AND zero events cannot be established — matter.js
	// ServerSubscription.ts:610-614 rejects it with InvalidAction, after
	// the admission above as there. The matched count is taken BEFORE
	// DataVersionFilter suppression, so a legitimately quiet re-subscribe
	// (all clusters cached) still establishes.
	if matchedPaths == 0 {
		b.abandonPrimingSubscription(subID)
		return b.rejectSubscribeInvalidAction(src, requestHdr, proto, "no_match")
	}
	initialReport.HasSubscription, initialReport.SubscriptionID = stamp.HasSubscription, stamp.SubscriptionID
	// The priming report waits for the peer's StatusResponse per chunk,
	// which arrives on this session: hand its turn on.
	leaveSessionOrder(ctx)
	if err := b.streamInitialReportChunks(src, requestHdr, proto, subID, initialReport); err != nil {
		b.abandonPrimingSubscription(subID)
		return err
	}
	if err := b.sendSubscribeResponse(src, requestHdr, proto, req, subID, initialReport); err != nil {
		b.abandonPrimingSubscription(subID)
		return err
	}
	// The subscription is active: record it for re-establishment after a
	// restart (matter.js SubscriptionsServer.#addSubscription, fired when
	// InteractionServer activates the subscription after its
	// SubscribeResponse).
	b.persistSubscription(ctx, subID)
	return nil
}

// rejectSubscribeInvalidAction ships a top-level
// StatusResponse(InvalidAction) for a Subscribe that cannot be
// established (illegal paths, no paths, or zero matching paths) and
// discharges the owed MRP ack. stage tags the debug log line.
func (b *Bridge) rejectSubscribeInvalidAction(src net.Addr, requestHdr *message.Header, proto message.ProtocolHeader, stage string) error {
	return b.rejectSubscribeStatus(src, requestHdr, proto, stage, im.StatusInvalidAction)
}

// rejectSubscribeStatus ships a top-level StatusResponse carrying status
// for a Subscribe that cannot be established and discharges the owed MRP
// ack. stage tags the debug log line.
func (b *Bridge) rejectSubscribeStatus(src net.Addr, requestHdr *message.Header, proto message.ProtocolHeader, stage string, status im.StatusCode) error {
	body, err := EncodeStatusResponse(im.StatusResponse{Status: status})
	if err != nil {
		debugReplyError(b.logger, "encode_subscribe_reject_"+stage, src, err)
		return err
	}
	if err := b.sendReply(src, requestHdr, proto, im.OpcodeStatusResponse, body); err != nil {
		debugReplyError(b.logger, "send_subscribe_reject_"+stage, src, err)
		return err
	}
	b.dischargeOwedAck(requestHdr.SessionID, proto.ExchangeID, !proto.Initiator)
	b.logger.Debug("matter.rx.im.subscribe.reject",
		slog.String("src", srcString(src)),
		slog.String("stage", stage),
		slog.String("status", status.String()))
	return nil
}

// subscriptionManagerLocked is a small helper to read b.subManager
// under the bridge's RLock. Returns nil when no manager is wired.
func (b *Bridge) subscriptionManagerLocked() *subscription.Manager {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.subManager
}

// wireMeasurementListeners binds every bridged endpoint whose
// Source or Measurement implements [contract.ChangeNotifier] — and then
// every mounted cluster server of the endpoint that does, whether or not
// the source notifies — to the subscription manager's OnAttributeChanged
// hook. On a value push
// from the CCU the notifier fires, the bridge marks every
// (endpoint, cluster, attribute) path advertised by the cluster
// server's MatterReportable() set dirty, and the manager's next tick
// ships a fresh ReportData via [Bridge.reportSubscription]. Without
// this hop the Subscribe pipeline emits only empty heartbeats and
// Apple Home flags bridged sensors as "not responding" once the
// initial ReportData expires.
//
// Mirrors matter.js's reactor wiring (e.g. ThermostatServer.ts:451's
// `reactTo(...measuredValue$Changed, handler)`) translated to Go:
// the cluster server is a struct, not a behavior with attached
// observables, so the source notifies and the bridge looks up the
// reportable paths via [endpoint.ReportablePaths].
//
// The bridge runs this through [Bridge.rewireMeasurementListeners] on
// every reassemble (topology may add or remove bridged endpoints) and
// whenever a fresh subscription manager is attached. It calls the host's
// notifiers, so the caller holds no bridge lock; it returns the
// unsubscribe closures of what it registered.
func (b *Bridge) wireMeasurementListeners(mgr *subscription.Manager, topology *endpointpkg.Topology) []func() {
	set := &listenerSet{}
	b.wireRootNotifiers(set, mgr, topology)
	b.wireAttributeNotifiers(set, mgr, topology)
	var (
		examined, notifierOK, withPaths int
		sourceSeen, measurementSeen     int
	)
	for _, ep := range topology.Endpoints {
		if ep == nil || ep.IsRoot() || ep.IsAggregator() {
			continue
		}
		examined++
		if ep.Source != nil {
			sourceSeen++
		}
		if ep.Measurement != nil {
			measurementSeen++
		}
		// Custom-DP-backed endpoints (Source set, e.g. Switch wrapping
		// generic.Switch via custom/switch) carry their notifier on
		// the Source. Measurement-only endpoints (TempSensor /
		// HumiditySensor / Contact / …) carry it on Measurement. Try
		// Source first because custom DPs subsume the measurement
		// surface (e.g. a HmIP-PSM Switch also has POWER / ENERGY
		// sub-sensors attached) and the bridge prefers the richer
		// notifier when both are present.
		var notifier contract.ChangeNotifier
		var ok bool
		if ep.Source != nil {
			notifier, ok = ep.Source.(contract.ChangeNotifier)
		}
		if !ok && ep.Measurement != nil {
			notifier, ok = ep.Measurement.(contract.ChangeNotifier)
		}
		paths := endpointpkg.ReportablePaths(ep)
		if len(paths) == 0 {
			continue
		}
		primaryCluster := uint32(0)
		if ok {
			notifierOK++
			// Narrow the dirty-mark set to the paths that BELONG to the
			// firing source's own cluster. The notifier reports "my value
			// changed" — it does not say "every cluster on this endpoint
			// changed". Marking the full reportable-path set dirty makes
			// every STATE flip emit a 5-attribute report (OnOff +
			// Identify.IdentifyTime + Descriptor.PartsList +
			// BridgedDeviceBasicInformation.{Reachable,NodeLabel}) where
			// only OnOff actually moved. Apple Home's HMOutlet treats
			// such bursty same-DataVersion reports as noisy and
			// suppresses the UI refresh. matter.js ships one attribute
			// report per real cluster-attribute change; mirror that.
			if pathSet := filterPathsByNotifierCluster(notifier, paths); len(pathSet) > 0 {
				withPaths++
				b.wireMeasurementNotifier(set, mgr, ep, notifier, pathSet)
			}
			if srv, isSrv := notifier.(contract.ClusterServer); isSrv {
				primaryCluster = srv.MatterClusterID()
			}
		}

		// Then every mounted cluster server that notifies its own changes
		// — whether or not the source notifies: matter.js reports a
		// change of any behavior's state through that behavior's own
		// Datasource (packages/node/src/behavior/state/managed/
		// Datasource.ts:776-796, onChange then the $Changed events), so a
		// server need not be relayed by its source to be heard. A metering
		// switch hosts sub-cluster servers (ElectricalPower /
		// ElectricalEnergy) backed by sibling meter-channel sensors in
		// addition to its primary OnOff; a device-layer device mounts
		// host servers that forward their port's notifier. Each is wired
		// to its own cluster's reportable paths. A server that is the
		// source's notifier itself, or serves the source's own cluster, is
		// subscribed once, above.
		for _, srv := range endpointpkg.ClusterServers(ep) {
			if srv == nil || srv.MatterClusterID() == primaryCluster {
				continue
			}
			subNotifier, isNotifier := srv.(contract.ChangeNotifier)
			if !isNotifier || (ok && sameNotifier(subNotifier, notifier)) {
				continue
			}
			subPaths := filterPathsByNotifierCluster(subNotifier, paths)
			if len(subPaths) == 0 {
				continue
			}
			withPaths++
			b.wireMeasurementNotifier(set, mgr, ep, subNotifier, subPaths)
		}
	}
	if b.logger != nil {
		b.logger.Info("matter.bridge.measurement_listeners_wired",
			slog.Int("examined", examined),
			slog.Int("measurement_seen", measurementSeen),
			slog.Int("notifier_ok", notifierOK),
			slog.Int("with_paths", withPaths),
			slog.Int("registered", len(set.unsubs)))
	}
	return set.unsubs
}

// listenerSet collects the unsubscribe closures of one wiring of the
// host's notifiers.
type listenerSet struct{ unsubs []func() }

// add keeps unsub; a notifier that returns none has nothing to detach.
func (l *listenerSet) add(unsub func()) {
	if unsub == nil {
		unsub = func() {}
	}
	l.unsubs = append(l.unsubs, unsub)
}

// rewireMeasurementListeners detaches the listeners of the last wiring
// and registers the bridge with the notifiers of the current topology,
// for the current subscription manager. Every call into the host — the
// stale unsubscribers and the registrations — runs with no bridge lock
// held: a notifier may call the callback back while it registers, and an
// unsubscribe may wait for a callback in flight, and the callback
// re-enters the bridge ([Bridge.EmitEvent] reads the manager under b.mu).
// Before, both ran under b.mu held for writing and such a notifier
// stopped the bridge (review II, W2-conc-07). b.mu guards the list and
// the generation only: a wiring a later one overtook while it registered
// is detached again instead of installed, so the last one wins.
func (b *Bridge) rewireMeasurementListeners() {
	b.mu.Lock()
	stale := b.measurementUnsubscribers
	b.measurementUnsubscribers = nil
	b.listenerGeneration++
	generation := b.listenerGeneration
	mgr, topology := b.subManager, b.topology
	b.mu.Unlock()
	runUnsubscribers(stale)
	if mgr == nil || topology == nil {
		return
	}
	unsubs := b.wireMeasurementListeners(mgr, topology)
	b.mu.Lock()
	overtaken := b.listenerGeneration != generation
	if !overtaken {
		b.measurementUnsubscribers = unsubs
	}
	b.mu.Unlock()
	if overtaken {
		runUnsubscribers(unsubs)
	}
}

// detachHostListeners unregisters the bridge from every host notifier —
// the value listeners and the switch hooks — for [Bridge.Stop]: a
// stopped bridge is not called into (review II, W2-conc-10). The next
// Start reassembles and wires them again. The unsubscribers run with no
// bridge lock held.
func (b *Bridge) detachHostListeners() {
	b.mu.Lock()
	measurement := b.measurementUnsubscribers
	switches := b.switchUnsubscribers
	b.measurementUnsubscribers = nil
	b.switchUnsubscribers = nil
	b.listenerGeneration++ // a wiring still registering detaches itself
	b.mu.Unlock()
	runUnsubscribers(measurement)
	runUnsubscribers(switches)
}

// runUnsubscribers calls every closure of unsubs.
func runUnsubscribers(unsubs []func()) {
	for _, unsub := range unsubs {
		if unsub != nil {
			unsub()
		}
	}
}

// wireRootNotifiers subscribes every root cluster server that
// implements [contract.ChangeNotifier] — today the Groupcast server, whose
// Membership and counts move with the group state — to its own reportable
// attributes on endpoint 0: a fire marks them dirty so subscribers get a
// report. A root server keeps its own DataVersion (it bumps it before it
// fires), so unlike a bridged endpoint nothing is bumped here. Mirrors the
// reactive state matter.js keeps for root behaviors. The caller holds no
// bridge lock.
func (b *Bridge) wireRootNotifiers(set *listenerSet, mgr *subscription.Manager, topology *endpointpkg.Topology) {
	root := topology.FindByID(0)
	if root == nil {
		return
	}
	for _, srv := range endpointpkg.ClusterServers(root) {
		notifier, ok := srv.(contract.ChangeNotifier)
		if !ok {
			continue
		}
		paths := make([]im.ConcreteAttributePath, 0, len(srv.MatterReportable()))
		for _, attr := range srv.MatterReportable() {
			paths = append(paths, im.ConcreteAttributePath{
				Endpoint: root.ID, Cluster: srv.MatterClusterID(), Attribute: attr,
				HasEndpoint: true, HasCluster: true, HasAttribute: true,
			})
		}
		set.add(notifier.OnMatterValueChanged(func() {
			for _, p := range paths {
				mgr.OnAttributeChanged(p)
			}
		}))
	}
}

// wireAttributeNotifiers subscribes every cluster server that
// implements [contract.AttributeChangeNotifier] — the servers that keep
// their own attribute state, such as cluster/opstate and cluster/modebase
// on a bridged endpoint or the OTA Software Update Requestor on the root —
// independently of whether the endpoint's source has a notifier of its
// own. On a bridged endpoint a fire advances the cluster's endpoint-hosted
// DataVersion once, before the named attributes are marked dirty, so the
// report carries the post-change version (matter.js Datasource.ts
// advanceVersion, then broadcastChanges for the changed properties only)
// — but for a change from a controller's write, whose version the
// dispatcher advanced already: one write, one advance.
// A root or aggregator server is a persistent instance whose own tracker
// is the DataVersion the dispatcher reports (endpoint/dispatcher.go
// clusterDataVersionFor), so it bumps that itself before it fires and
// nothing is bumped here. matter.js reports a state change of every
// behavior on every endpoint the same way, the root's included — the OTA
// requestor's UpdateState is plain behavior state
// (OtaSoftwareUpdateRequestorServer.ts #updateState :1164-1204). A root
// server that is also a [contract.ChangeNotifier] is wired by
// wireRootNotifiers already and is skipped here. A server that is a
// [contract.AttributeReportNotifier] is subscribed through its report view
// — a quieter attribute by its throttle, as matter.js's
// ServerBehaviorBacking subscribes a QuietEvent's `online` observable
// (ServerBehaviorBacking.ts:251-259) — and through OnAttributeChange,
// which tells the host of every change at once, otherwise. Caller holds
// b.mu.
func (b *Bridge) wireAttributeNotifiers(set *listenerSet, mgr *subscription.Manager, topology *endpointpkg.Topology) {
	for _, ep := range topology.Endpoints {
		if ep == nil {
			continue
		}
		persistent := ep.IsRoot() || ep.IsAggregator()
		for _, srv := range endpointpkg.ClusterServers(ep) {
			notifier, ok := srv.(contract.AttributeChangeNotifier)
			if !ok {
				continue
			}
			if _, wired := srv.(contract.ChangeNotifier); wired && ep.IsRoot() {
				continue
			}
			clusterID := srv.MatterClusterID()
			subscribe := notifier.OnAttributeChange
			if r, reports := srv.(contract.AttributeReportNotifier); reports {
				subscribe = r.OnAttributeReport
			}
			set.add(subscribe(func(change contract.Change) {
				attrIDs := change.Attributes
				if len(attrIDs) == 0 {
					return
				}
				// A controller's write is one commit with one advance:
				// the dispatcher's (endpoint/dispatcher.go WriteAuthorized),
				// so the change the write itself reports does not advance
				// it again (matter.js Datasource.ts:1440-1448,
				// advanceVersion once per commit).
				if !persistent && change.Origin != contract.OriginControllerWrite {
					ep.BumpClusterDataVersion(clusterID)
				}
				for _, attr := range attrIDs {
					mgr.OnAttributeChanged(im.ConcreteAttributePath{
						Endpoint: ep.ID, Cluster: clusterID, Attribute: attr,
						HasEndpoint: true, HasCluster: true, HasAttribute: true,
					})
				}
				if hook := b.onAttributeChange.Load(); hook != nil {
					(*hook)(ep.ID, clusterID, change)
				}
			}))
		}
	}
}

// wireMeasurementNotifier subscribes notifier to pathSet: on each fire it
// advances the touched clusters' endpoint-hosted DataVersion BEFORE dirty-
// marking (so the shipped report carries the post-change version and a
// controller's DataVersionFilter misses on the next read — mirrors matter.js
// Datasource.ts:1035), then marks every path dirty on the Subscribe manager. The
// unsubscribe is tracked so a reassemble tears every listener down.
func (b *Bridge) wireMeasurementNotifier(set *listenerSet, mgr *subscription.Manager, ep *endpointpkg.Endpoint, notifier contract.ChangeNotifier, pathSet []im.ConcreteAttributePath) {
	epID := ep.ID
	boolState := newBooleanStateEvents(b, ep, pathSet)
	occupancy := newOccupancyEvents(b, ep, pathSet)
	values := newPathValues(ep, pathSet)
	set.add(notifier.OnMatterValueChanged(func() {
		defer occupancy.check()
		defer boolState.check()
		// Only the attributes whose value moved are reported, and only
		// their clusters' DataVersions advance: matter.js Datasource
		// compares the new state with the old one and broadcasts the
		// changed properties alone (Datasource.ts, broadcastChanges). A
		// source that notifies once per change of any of its values
		// otherwise reported every attribute each time — FanMode on every
		// SpeedSetting step (TC-FAN-3.2).
		changed := values.changed()
		if b.logger != nil {
			b.logger.Debug("matter.bridge.measurement.notify",
				slog.Int("endpoint", int(epID)),
				slog.Int("paths", len(pathSet)),
				slog.Int("changed", len(changed)))
		}
		bumped := make(map[uint32]struct{}, 1)
		for _, p := range changed {
			if _, done := bumped[p.Cluster]; !done {
				ep.BumpClusterDataVersion(p.Cluster)
				bumped[p.Cluster] = struct{}{}
			}
		}
		for _, p := range changed {
			mgr.OnAttributeChanged(p)
		}
	}))
}

// sameNotifier reports whether a and b are the same notifier — the
// source that is also one of its endpoint's cluster servers. Notifiers of
// a type that cannot be compared are never the same.
func sameNotifier(a, b contract.ChangeNotifier) bool {
	if a == nil || b == nil {
		return false
	}
	ta := reflect.TypeOf(a)
	if ta != reflect.TypeOf(b) || !ta.Comparable() {
		return false
	}
	return a == b
}

// filterPathsByNotifierCluster narrows reportable paths to only those
// whose cluster is owned by the notifier. A source typically also
// implements [contract.ClusterServer] (e.g. *switchdev.Switch
// is both the endpoint source and the OnOff cluster server), in which
// case its `MatterClusterID()` identifies the cluster whose attributes
// it can actually change. Paths on other clusters of the same
// endpoint (Identify, Descriptor, BridgedDeviceBasicInformation, …)
// are skipped because the notifier-fire signals a value change on the
// source's cluster only.
//
// When the notifier does not also implement
// [contract.ClusterServer] — the measurement-source path,
// where the source is a sensor-DP and the cluster server is materialised
// separately — we fall back to the full path set. Every reportable
// cluster on such an endpoint is derived from that one source (an
// air-quality endpoint serves both the concentration cluster and the
// AirQuality level computed from it), so the wider set still carries
// only attributes that genuinely moved, plus the static Descriptor/BDBI
// ones. A future per-cluster-attribute refinement can tighten this
// further if Apple turns out to dislike the static-attr noise on sensor
// endpoints too.
func filterPathsByNotifierCluster(notifier contract.ChangeNotifier, paths []im.ConcreteAttributePath) []im.ConcreteAttributePath {
	srv, ok := notifier.(contract.ClusterServer)
	if !ok {
		return append([]im.ConcreteAttributePath(nil), paths...)
	}
	clusterID := srv.MatterClusterID()
	out := make([]im.ConcreteAttributePath, 0, 1)
	for _, p := range paths {
		if p.Cluster == clusterID {
			out = append(out, p)
		}
	}
	return out
}

// resolveSessionFabric maps an active SessionID to its operational
// FabricIndex via the [SessionFabricResolver] optional capability.
// Answers 0 for sessionID 0 (the unsecured channel) and for a session
// the resolver reports as unknown or without a fabric (a PASE session
// before AddNOC), and [endpointpkg.FabricIndexUnresolvable] when the
// wired SessionLookup has no working resolver at all.
//
// Used by Subscribe to scope per-fabric subscription quotas correctly,
// and by any other fabric-aware path that doesn't have a richer
// session handle around.
func (b *Bridge) resolveSessionFabric(sessionID uint16) uint8 {
	if sessionID == 0 {
		return 0
	}
	b.mu.RLock()
	sessions := b.sessions
	b.mu.RUnlock()

	resolver, ok := sessions.(SessionFabricResolver)
	if ok {
		// An adapter can carry FabricFor and still have no resolver behind
		// it, in which case it answers (0, false) for everything.
		if r, reports := sessions.(interface{ FabricResolutionWired() bool }); reports && !r.FabricResolutionWired() {
			ok = false
		}
	}
	if !ok {
		// The bridge cannot name this session's fabric. FabricIndexUnresolvable
		// says "not known", and the ACL gates deny it — whether or not an ACL
		// lister is attached (review 2026-11 W2-sec-03): only a PASE session
		// carries the commissioning channel's implicit Administer, never a
		// session whose fabric is missing. Mirrors matter.js
		// packages/protocol/src/interaction/FabricAccessControl.ts:189-191
		// (`authMode === Pase && isCommissioning`).
		return endpointpkg.FabricIndexUnresolvable
	}
	idx, _ := resolver.FabricFor(sessionID)
	return idx
}

// resolveSessionSubject returns the requesting peer's operational
// NodeID + CASE Authenticated Tags for sessionID, or (0, nil) when
// the lookup does not implement [SessionSubjectResolver] or the
// session is unknown. Used by the receive pipeline to stamp the
// subject into the IM dispatcher's context so [TopologyDispatcher.
// CheckACL] can enforce per-subject ACEs (Matter §9.10.5.6). Session 0
// returns (0, nil); a PASE session is granted by its auth mode
// ([Bridge.resolveSessionPASE]), not by its subject.
func (b *Bridge) resolveSessionSubject(sessionID uint16) (nodeID uint64, cats []uint32) {
	if sessionID == 0 {
		return 0, nil
	}
	b.mu.RLock()
	sessions := b.sessions
	b.mu.RUnlock()
	resolver, ok := sessions.(SessionSubjectResolver)
	if !ok {
		return 0, nil
	}
	nodeID, cats, _ = resolver.SubjectFor(sessionID)
	return nodeID, cats
}

// localNodeIDFor returns this node's operational node id on the session's
// accessing fabric — the session's local node id — or 0 for a session
// without a fabric, as matter.js EventReadResponse.ts:68 takes
// UNSPECIFIED_NODE_ID there. It selects the EventFilterIB that applies to
// this node ([im.EventFiltersFor]).
func (b *Bridge) localNodeIDFor(sessionID uint16, fabricIndex uint8) uint64 {
	if sessionID == 0 || fabricIndex == 0 || fabricIndex == endpointpkg.FabricIndexUnresolvable {
		return 0
	}
	b.mu.RLock()
	sessions := b.sessions
	b.mu.RUnlock()
	sess, ok := sessions.Lookup(sessionID)
	if !ok || sess == nil {
		return 0
	}
	return sess.LocalNodeID()
}

// resolveSessionPASE reports whether sessionID was established over
// PASE, or false when the lookup does not implement
// [SessionPASEResolver] or the session is unknown. The receive pipeline
// stamps the answer via [im.WithAuthModePASE] so the IM ACL gates keep
// the commissioning channel's implicit Administer grant after AddNOC
// adopted the session onto the new fabric — matter.js
// packages/protocol/src/interaction/FabricAccessControl.ts:189-191 keys
// that grant on the auth mode, not on the fabric.
//
// sessionID==0 is the unencrypted path, which carries no IM at all
// (the receive path drops it before decode).
func (b *Bridge) resolveSessionPASE(sessionID uint16) bool {
	if sessionID == 0 {
		return false
	}
	b.mu.RLock()
	sessions := b.sessions
	b.mu.RUnlock()
	resolver, ok := sessions.(SessionPASEResolver)
	if !ok {
		return false
	}
	pase, _ := resolver.IsPASE(sessionID)
	return pase
}

// booleanStateEvents emits BooleanState.StateChange when an endpoint's
// StateValue changes: matter.js BooleanStateServer enables the ChangeEvent
// feature by default and emits the event on stateValue$Changed
// (packages/node/src/behaviors/boolean-state/BooleanStateServer.ts). The
// value is a host measurement, so the change is observed here, where the
// host's notification arrives.
type booleanStateEvents struct {
	b  *Bridge
	ep *endpointpkg.Endpoint
	// mu guards last.
	mu   sync.Mutex
	last *bool
}

// newBooleanStateEvents returns nil when the notifier does not cover
// BooleanState.StateValue, or when the endpoint's BooleanState server
// holds the value and emits StateChange itself
// (measurement.HeldBooleanState, the device kit's default).
func newBooleanStateEvents(b *Bridge, ep *endpointpkg.Endpoint, pathSet []im.ConcreteAttributePath) *booleanStateEvents {
	for _, srv := range endpointpkg.ClusterServers(ep) {
		if _, held := srv.(*measurement.HeldBooleanState); held {
			return nil
		}
	}
	for _, p := range pathSet {
		if p.Cluster == measurement.ClusterBooleanState && p.Attribute == 0x0000 {
			e := &booleanStateEvents{b: b, ep: ep}
			if v, ok := e.read(); ok {
				e.last = &v
			}
			return e
		}
	}
	return nil
}

func (e *booleanStateEvents) read() (value, ok bool) {
	for _, srv := range endpointpkg.ClusterServers(e.ep) {
		if srv.MatterClusterID() == measurement.ClusterBooleanState {
			v, ok := srv.MatterRead(0x0000)
			bv, isBool := v.(bool)
			return bv, ok && isBool
		}
	}
	return false, false
}

func (e *booleanStateEvents) check() {
	if e == nil {
		return
	}
	v, ok := e.read()
	if !ok {
		return
	}
	e.mu.Lock()
	changed := e.last == nil || *e.last != v
	e.last = &v
	e.mu.Unlock()
	if changed {
		e.b.MatterEmitEvent(e.ep.ID, measurement.ClusterBooleanState, 0x00,
			measurement.BooleanStateChangeEvent{StateValue: v}, contract.EventPriorityInfo)
	}
}

// occupancyEvents emits OccupancySensing.OccupancyChanged when the
// Occupancy of an endpoint's measurement-projected server changes and the
// server lists the event — the OccupancyEvent feature of its selection.
// matter.js emits it on occupancy$Changed when that feature is set
// (packages/node/src/behaviors/occupancy-sensing/OccupancySensingServer.ts:67-74,
// :88-90 at 862ad75f); the projected server is rebuilt on every dispatch,
// so the change is observed here, where the host's notification arrives,
// as for BooleanState's StateChange. The device kit's server
// (sensing.Occupancy) emits the event itself and is no
// measurement.OccupancySensingServer.
type occupancyEvents struct {
	b  *Bridge
	ep *endpointpkg.Endpoint
	// mu guards last.
	mu   sync.Mutex
	last *uint8
}

// newOccupancyEvents returns nil when the notifier does not cover
// OccupancySensing.Occupancy or the endpoint's projected server does not
// list OccupancyChanged.
func newOccupancyEvents(b *Bridge, ep *endpointpkg.Endpoint, pathSet []im.ConcreteAttributePath) *occupancyEvents {
	srv := projectedOccupancy(ep)
	if srv == nil || !slices.Contains(srv.MatterEvents(), measurement.OccupancyEventChanged) {
		return nil
	}
	for _, p := range pathSet {
		if p.Cluster == measurement.ClusterOccupancySensing && p.Attribute == 0x0000 {
			e := &occupancyEvents{b: b, ep: ep}
			if v, ok := e.read(); ok {
				e.last = &v
			}
			return e
		}
	}
	return nil
}

// projectedOccupancy is the endpoint's measurement-projected
// OccupancySensing server, nil without one.
func projectedOccupancy(ep *endpointpkg.Endpoint) *measurement.OccupancySensingServer {
	for _, srv := range endpointpkg.ClusterServers(ep) {
		if s, ok := srv.(*measurement.OccupancySensingServer); ok {
			return s
		}
	}
	return nil
}

func (e *occupancyEvents) read() (uint8, bool) {
	srv := projectedOccupancy(e.ep)
	if srv == nil {
		return 0, false
	}
	v, ok := srv.MatterRead(0x0000)
	bitmap, isU8 := v.(uint8)
	return bitmap, ok && isU8
}

func (e *occupancyEvents) check() {
	if e == nil {
		return
	}
	v, ok := e.read()
	if !ok {
		return
	}
	e.mu.Lock()
	changed := e.last == nil || *e.last != v
	e.last = &v
	e.mu.Unlock()
	if changed {
		e.b.MatterEmitEvent(e.ep.ID, measurement.ClusterOccupancySensing, measurement.OccupancyEventChanged,
			measurement.OccupancyChangedEvent{Occupancy: occdef.OccupancyBitmap(v)}, contract.EventPriorityInfo)
	}
}

// pathValues remembers the last value of each path a change notifier
// covers, so a notification reports only the paths that moved.
type pathValues struct {
	// mu guards last.
	mu    sync.Mutex
	ep    *endpointpkg.Endpoint
	paths []im.ConcreteAttributePath
	last  map[im.ConcreteAttributePath]attributeReading
}

func newPathValues(ep *endpointpkg.Endpoint, paths []im.ConcreteAttributePath) *pathValues {
	v := &pathValues{ep: ep, paths: paths, last: make(map[im.ConcreteAttributePath]attributeReading, len(paths))}
	for p, val := range v.read() {
		v.last[p] = val
	}
	return v
}

// read captures every path's current value; a path no server answers is
// left out, so it counts as changed on every notification.
func (v *pathValues) read() map[im.ConcreteAttributePath]attributeReading {
	servers := map[uint32]contract.ClusterServer{}
	for _, srv := range endpointpkg.ClusterServers(v.ep) {
		if srv != nil {
			servers[srv.MatterClusterID()] = srv
		}
	}
	out := make(map[im.ConcreteAttributePath]attributeReading, len(v.paths))
	for _, p := range v.paths {
		srv := servers[p.Cluster]
		if srv == nil {
			continue
		}
		if val, ok := srv.MatterRead(p.Attribute); ok {
			out[p] = readingOf(im.AttributeValue{Value: val})
		}
	}
	return out
}

// changed returns the paths whose value differs from the last
// notification's, compared by value ([attributeReading]), and remembers
// the new values.
func (v *pathValues) changed() []im.ConcreteAttributePath {
	now := v.read()
	v.mu.Lock()
	defer v.mu.Unlock()
	out := make([]im.ConcreteAttributePath, 0, len(v.paths))
	for _, p := range v.paths {
		cur, ok := now[p]
		if prev, had := v.last[p]; ok && had && prev.equal(cur) {
			continue
		}
		out = append(out, p)
		if ok {
			v.last[p] = cur
		}
	}
	return out
}

// unskippedChangedPaths drops from a report's changed paths each concrete
// path the subscription covers only through wildcard paths whose
// WildcardPathFlags skip it, so a change report omits what the priming
// report omitted (matter.js builds every report through
// AttributeReadResponse, packages/protocol/src/action/server/
// AttributeReadResponse.ts:149-197). A wildcard changed path is left to
// [Bridge.readAuthorizedResults].
func (b *Bridge) unskippedChangedPaths(ctx context.Context, dispatcher im.Dispatcher, sub *subscription.Subscription, paths []im.ConcreteAttributePath) []im.ConcreteAttributePath {
	flagged := false
	for _, sp := range sub.AttributePaths {
		if sp.WildcardPathFlags != 0 {
			flagged = true
			break
		}
	}
	if !flagged {
		return paths
	}
	qctx := im.WithElementQualities(ctx, specElementQualities{})
	out := make([]im.ConcreteAttributePath, 0, len(paths))
	for _, p := range paths {
		if !p.HasEndpoint || !p.HasCluster || !p.HasAttribute {
			out = append(out, p)
			continue
		}
		for _, sp := range sub.AttributePaths {
			if subscribedPathCovers(sp, p) && !im.WildcardPathFlagsSkip(qctx, dispatcher, sp, p) {
				out = append(out, p)
				break
			}
		}
	}
	return out
}

// subscribedPathCovers reports whether the subscribed (possibly wildcard)
// path covers the concrete path p.
func subscribedPathCovers(sp, p im.ConcreteAttributePath) bool {
	return (!sp.HasEndpoint || sp.Endpoint == p.Endpoint) &&
		(!sp.HasCluster || sp.Cluster == p.Cluster) &&
		(!sp.HasAttribute || sp.Attribute == p.Attribute)
}

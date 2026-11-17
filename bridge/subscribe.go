// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	clusterpkg "github.com/SukramJ/go-fabric/cluster"
	"github.com/SukramJ/go-fabric/cluster/measurement"
	"github.com/SukramJ/go-fabric/cluster/spec"
	eemdef "github.com/SukramJ/go-fabric/cluster/spec/electricalenergymeasurement"
	occdef "github.com/SukramJ/go-fabric/cluster/spec/occupancysensing"
	psdef "github.com/SukramJ/go-fabric/cluster/spec/powersource"
	"github.com/SukramJ/go-fabric/contract"
	endpointpkg "github.com/SukramJ/go-fabric/endpoint"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/im/subscription"
	"github.com/SukramJ/go-fabric/store"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// Errors of the bridge's unsolicited sends: sendUnsolicitedIM returns
// them to the report path that called it, and the OTA provider link, the
// BDX responder and the CASE initiator return them for their own sends.
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
// chunkStatusResponseReliable is the term that replaces the backoff over a
// reliable connection (TCP, BLE): matter.js takes max(30 s, expected
// processing time) there (MRP.ts:90-92, :111-113 at db021da0).
const (
	chunkStatusResponseExpectedProcessing = 2 * time.Second
	chunkStatusResponseBuffer             = 5 * time.Second
	chunkStatusResponseReliable           = 30 * time.Second
)

// chunkStatusResponseTimeout bounds how long a chunk loop (Subscribe-
// Initial and Read-response) waits for the peer's IM StatusResponse on
// one ReportData chunk before abandoning the interaction — the ongoing
// report and the read and initial-report chunks. Takes matter.js's bound
// for this wait — MRP.maxPeerResponseTimeOf for a UDP/MRP session
// (MRP.ts:100-108 at db021da0): this node's own retransmission schedule on
// its own session intervals, active at first and idle once the wait passes
// its active threshold, with its sender-side pad (maxResponseTimeOf,
// :198-224), plus the default expected processing time and the response
// buffer — and adds the formula's peer term, the peer's retransmission
// schedule, which matter.js leaves at 0 for this wait
// (packages/protocol/src/protocol/MessageExchange.ts:878-885 passes
// undefined for includeMaximumSendingTime, :882; MessageChannel.ts:239-240
// then hands no peerSessionParameters; MRP.ts:102 makes the term 0 — review
// II, R2-P6-par-0707): the module's one timer runs from the send, not
// from the report's acknowledgement (notes/parity/by_design.md,
// BD-Matter-Subscription-ReportWaitBound; review II, R2-P1-req2-12).
//
// The peer's schedule is summed on the base interval the session lookup
// resolves for it now (the spec idle default when it resolves none) and
// does not switch from the peer's active to its idle interval past the
// peer's active threshold, as matter.js's formula does: the session port
// names one base interval, not the peer's three parameters.
//
// Over a reliable connection (TCP, BLE) no retransmission runs: the bound
// is max(30 s, the expected processing time) plus the response buffer,
// 35 s, as matter.js's maxPeerResponseTimeOf for such a channel (MRP.ts:90-92,
// :111-113 at db021da0). route is where the wait's message went (nil: a
// UDP peer).
//
// A test may pin the bound through chunkStatusResponseTimeoutOverride;
// zero means derive it.
func (b *Bridge) chunkStatusResponseTimeout(route net.Addr, sessionID uint16) time.Duration {
	if b.chunkStatusResponseTimeoutOverride > 0 {
		return b.chunkStatusResponseTimeoutOverride
	}
	if r, ok := route.(*connRoute); ok && r != nil && r.conn != nil && r.conn.Reliable() {
		return max(chunkStatusResponseReliable, chunkStatusResponseExpectedProcessing) + chunkStatusResponseBuffer
	}
	base := b.outboundBaseInterval(sessionID, b.clk().Now())
	if base <= 0 {
		base = mrp.SessionIdleIntervalDefault
	}
	peer := maxScheduleTime(base, base, 0, false, 0)
	local := b.cfg.SessionIntervals
	if local.Idle <= 0 {
		local.Idle = mrp.SessionIdleIntervalDefault
	}
	if local.Active <= 0 {
		local.Active = mrp.SessionActiveIntervalDefault
	}
	if local.ActiveThreshold <= 0 {
		local.ActiveThreshold = mrp.SessionActiveThresholdDefault
	}
	own := maxScheduleTime(local.Idle, local.Active, local.ActiveThreshold, true, b.LocalFixedMRPBackoff())
	return peer + own + chunkStatusResponseExpectedProcessing + chunkStatusResponseBuffer
}

// maxScheduleTime is matter.js's maxResponseTimeOf (MRP.ts:198-224 at
// db021da0): every transmission's maximum interval (full jitter) summed,
// on the active interval while active and the wait has not passed
// threshold, else on the idle one, each with pad added unamplified.
func maxScheduleTime(idle, active, threshold time.Duration, isActive bool, pad time.Duration) time.Duration {
	var wait time.Duration
	for n := 0; n <= mrp.MaxRetransmissions; n++ {
		if isActive && wait > threshold {
			isActive = false
		}
		base := idle
		if isActive {
			base = active
		}
		wait += mrp.BackoffDuration(base, n, func() float64 { return 1 }) + pad
	}
	return wait
}

// AttachSubscriptionManager wires the subscription manager that
// powers IM Subscribe. When set, inbound SubscribeRequest datagrams
// produce:
//
//  1. Initial ReportData (synchronous, with HasSubscription=true and
//     the freshly-allocated SubscriptionID).
//  2. SubscribeResponse carrying SubscriptionID + MaxInterval.
//
// The attach installs the bridge's [subscription.ReportSender] on m
// ([subscription.Manager.SetReportSender]): every report of the manager —
// the ongoing reports with their events and attribute changes, the
// keep-alives, the flush before a fabric or a session leaves — goes out
// through it, re-reading the values and shipping a fresh ReportData over
// the original commissioner session, looked up via
// [exchangeRouting.subTargets] (populated here on successful Subscribe).
// Nothing else is to wire: a manager built with
// subscription.NewManager(cfg, nil, logger) and attached here reports
// everything (review II, R2-P6-api-0508, owner decision 138). A manager
// the host wired with the deprecated hooks ([Bridge.SubscriptionReporter],
// [Bridge.SubscriptionEventReporter], [Bridge.SubscriptionFlushReporter])
// keeps reporting through them for the deprecation window.
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
	started, run := b.started, b.runGen
	b.mu.Unlock()
	// The host's value listeners are rewired onto m only while a run is
	// live: Stop detached them and keeps the topology, so a rewire on a
	// stopped bridge would register it with the host's notifiers again;
	// the next Start's assembly wires them (review II, R2-P2-sess-09,
	// N-bridge2-4). A Stop that sets in after the wiring detaches what it
	// finds; one that set in before the wiring was installed missed it,
	// so it goes again here — as in Reassemble. An attach while Start
	// runs is wired by Start once the run is started (review II,
	// R2-P2-dr-req-05).
	if started {
		generation := b.rewireMeasurementListeners()
		b.mu.RLock()
		ended := b.runEndedLocked(&run)
		b.mu.RUnlock()
		if ended {
			b.detachListenersOfGeneration(generation)
		}
	}
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
		// The one hook every report goes through.
		m.SetReportSender(b.sendSubscriptionReport)
	}
}

// sendSubscriptionReport is the bridge's [subscription.ReportSender]: a
// flush goes out as the flush's one ReportData ([Bridge.reportSubscriptionFlush]),
// a report with events as one ReportData of the events and the attribute
// changes due with them ([Bridge.reportSubscriptionEvents]), anything else
// as an attribute report, empty for a keep-alive
// ([Bridge.reportSubscription]). It sends a pass whose data is all filtered
// for the subscriber empty and counts it (owner decision 169), so its
// outcome is always [subscription.Sent].
func (b *Bridge) sendSubscriptionReport(ctx context.Context, sub *subscription.Subscription, r subscription.Report) subscription.ReportOutcome {
	switch {
	case r.Flush:
		b.reportSubscriptionFlush(ctx, sub, r.Events, r.Paths)
	case len(r.Events) > 0:
		b.reportEventsWith(ctx, sub, r.Events, func() []im.ConcreteAttributePath { return r.Paths })
	default:
		b.reportSubscription(ctx, sub, r.Paths)
	}
	return subscription.Sent
}

// releaseSubscriptionRouting drops every bridge-side table entry that
// belongs to subID. Called by the subscription manager's close
// observer, so it must not take b.mu (the manager can close a
// subscription from within a bridge call).
func (b *Bridge) releaseSubscriptionRouting(subID uint32) {
	b.closePrimingAbort(subID)
	b.routing.subTargets.Delete(subID)
	b.resetFailedUpdates(subID)
	b.subAccess.Delete(subID)
	b.forgetReportExchange(subID)
	// reportCounterOwner is keyed by (session, counter), so the owning
	// subscription is only visible in the value — a scan is the only
	// way to reclaim the in-flight report counters of a subscription
	// whose peer never ACKs them.
	b.reportCounterOwner.Range(func(key, value any) bool {
		if owed, ok := value.(*owedReport); ok && owed.subID == subID {
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
// sessionID is the secure session under which the reports travel,
// AES-CCM-sealed via [channel.Session] — a CASE or a PASE one ([subTarget.pase]
// tells them apart). Session 0 is the unsecured session, which carries no
// IM ([Bridge.resolveSessionPASE]); review II, R2-P6-doc-0515.
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
	// with — the subscription engine's stop — the flush reporter to its
	// flush's ctx (Stop's budget, a fabric removal), and it is never
	// stored in the routing table. Nil: the wait ends with its answer or
	// its timer.
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
//
// Deprecated: [Bridge.AttachSubscriptionManager] installs the bridge's
// [subscription.ReportSender] itself; pass nil as
// [subscription.NewManager]'s reporter. Removable in the next minor release.
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
// [subTarget] used for attribute reports, and takes the attribute changes
// that are due with them ([subscription.Subscription.TakeDuePaths]): one
// ReportData carries both ([Bridge.reportSubscriptionEvents]).
//
// Deprecated: [Bridge.AttachSubscriptionManager] installs the bridge's
// [subscription.ReportSender] itself, which reports events too. Removable
// in the next minor release.
func (b *Bridge) SubscriptionEventReporter() subscription.EventReporter {
	return b.reportSubscriptionEvents
}

// SubscriptionFlushReporter is the hook for the flush before a fabric
// leaves. The daemon wires it via [subscription.Manager.SetFlushReporter]
// beside the other two; [subscription.Manager.FlushFabric] hands it what
// one subscription owes — events and attributes together — and it ships
// them as one ReportData and waits for the peer's answer
// ([Bridge.reportSubscriptionFlush]).
//
// Deprecated: [Bridge.AttachSubscriptionManager] installs the bridge's
// [subscription.ReportSender] itself, which reports the flush too.
// Removable in the next minor release.
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
// pase==true after it adopted the session onto the new fabric) passes the
// ACL gate inside [im.AuthorizeEventReports]; any other session of a
// dispatcher without an ACLChecker reads no event (owner decision 136);
// its fabric-sensitive drop applies to every
// session — a fabric-sensitive record reaches only the fabric that owns it,
// none for a PASE session before AddNOC. Shared by
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
// owned by another fabric, as [im.AuthorizeEventReports] does. A PASE session
// passes the ACL gate, and a dispatcher without an [im.ACLChecker] grants
// any other session nothing; the fabric-sensitive drop applies to every
// session. Each event is gated at its
// read privilege — View, the Matter default, for most clusters — and a
// fabric-sensitive record (AccessControl's among them, Matter §8.4.3.2 /
// §9.10.7.1) is dropped when its FabricIndex differs from the subscribing
// fabric: path-ACL alone does not stop cross-fabric disclosure of such a
// record. Closes the event half of the subscribe-path ACL bypass.
func (b *Bridge) authorizedEventReports(ctx context.Context, target subTarget, events []im.EventReport) []im.EventReport {
	auth := b.eventReadAuthorizer(b.Dispatcher(), target.fabricIndex, target.pase, target.subjectNodeID, target.subjectCATs)
	return im.AuthorizeEventReports(ctx, auth, events)
}

// reportSubscriptionEvents is the deprecated [subscription.EventReporter]
// over [Bridge.reportEventsWith]: it takes the attribute changes due with
// the events from the subscription ([subscription.Subscription.TakeDuePaths]).
func (b *Bridge) reportSubscriptionEvents(ctx context.Context, sub *subscription.Subscription, events []im.EventReport) {
	b.reportEventsWith(ctx, sub, events, sub.TakeDuePaths) //nolint:staticcheck // the deprecated hook, held for its window (R2-P6-api-0508)
}

// reportEventsWith assembles + ships the ReportData of a report of sub
// that carries events: the events, and in the same message the attribute
// changes that are due with them, which takePaths hands over once the
// report is going out. Called through the manager's engine tick. Mirrors matter.js
// packages/node/src/node/server/ServerSubscription.ts:539-555 at db021da0
// (#sendUpdate takes the attribute filter and the events' minimum number
// together) and :860-887, :926-936 (#iterateDataUpdate yields the
// attributes, then the events, into one DataReport).
func (b *Bridge) reportEventsWith(ctx context.Context, sub *subscription.Subscription, events []im.EventReport, takePaths func() []im.ConcreteAttributePath) {
	if sub == nil || len(events) == 0 {
		return
	}
	target, ok := b.subscriptionTargetOf(sub)
	if !ok {
		return
	}
	if dispatcher := b.Dispatcher(); dispatcher != nil && b.endIfSubjectReadsNothing(ctx, dispatcher, target, sub) {
		return
	}
	// The attribute changes due in this report are this reporter's from
	// here on: sent below, or owed again when the send fails.
	paths := takePaths()

	// Authorize each event against the subscribing subject: a CASE
	// subject only receives events on (endpoint, cluster) it holds at
	// least View privilege for (Matter §9.10 — events carry a read
	// privilege, default View; AccessControlEntryChanged is Administer).
	// Without this an ACE-less subject would stream every fired event.
	// A PASE subscriber passes the ACL gate (the auth mode, not fabric
	// index 0, which alone is refused); a dispatcher without an
	// ACLChecker grants any other subscriber nothing (decision 136). The
	// fabric-sensitive drop applies to every subscriber.
	report := im.ReportData{
		HasSubscription: true,
		SubscriptionID:  sub.ID,
		EventReports:    b.authorizedEventReports(ctx, target, events),
	}
	if dispatcher := b.Dispatcher(); dispatcher != nil && len(paths) > 0 {
		report.Reports = b.subscriptionAttributeReports(ctx, dispatcher, target, sub, paths)
	}
	// A pass whose data is all filtered for this subscriber — the access
	// check, a fabric-sensitive event of another fabric — sends the
	// ReportData empty and counts as the subscription's report, as the
	// attribute path does: one rule, both references' (owner decision 169,
	// which replaced decision 145; review II, F-db021da0-8). matter.js
	// sends the DataReport empty here
	// (packages/node/src/node/server/ServerSubscription.ts:921-936 at
	// db021da0, `suppressEmptyReport: onlyWithData`, false for the pass a
	// change or the send interval starts, :198-200; the messenger omits it
	// only with that flag set, InteractionMessenger.ts:794-796), and so
	// does connectedhomeip for a dirtied subscription whose pass encodes
	// nothing, restarting both the min and the max interval
	// (src/app/reporting/Engine.cpp:818-853, src/app/ReadHandler.cpp:352-362,
	// src/app/reporting/ReportScheduler.h:141-147 at 6170af84).

	// The engine's stop ends the wait for a chunk's answer (W2-conc-06),
	// and so does the subscription's close (R2-P6-fsm-0513).
	abort, endAbort := b.reportAbort(ctx, sub.ID)
	defer endAbort()
	target.abort = abort

	// Same bridge-initiated exchange as the attribute path: the
	// commissioner closed its Subscribe exchange when the
	// SubscribeResponse landed, and a report sent there with
	// Initiator=false is an unsolicited message on an unknown exchange —
	// matter.js answers it with a standalone ack and drops it
	// (ExchangeManager.ts:496-507 at db021da0), which the MRP layer here
	// would then mistake for delivery. Ongoing server reports are always
	// fresh server-initiated exchanges (ServerSubscription.ts:900).
	freshExch, sent := b.deliverOngoingReport(sub, target, report, &owedReport{paths: paths, eventsFrom: events[0].Number, hasEvents: true}, "event_report")
	if !sent {
		return
	}
	b.logger.Debug("matter.tx.subscribe.event_report",
		slog.Int("subscription_id", int(sub.ID)),
		slog.Int("events", len(events)),
		slog.Int("reports", len(report.Reports)),
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
// — returns every result; a dispatcher without an [im.ACLChecker] returns
// none to any other session, as it denies a Read (owner decision 136).
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
	allow := readResultAuthorizer(ctx, dispatcher)
	if allow == nil {
		return results
	}
	authorized := make([]im.ReadResult, 0, len(results))
	for _, rr := range results {
		if allow(rr) {
			authorized = append(authorized, rr)
		}
	}
	return authorized
}

// readResultAuthorizer is the gate [Bridge.readAuthorizedResults] applies
// to each result, for the subject ctx carries: nil when every result
// passes (PASE), else a func reporting whether the subject may read the
// result's attribute — one that refuses every result for a dispatcher
// without an [im.ACLChecker] (owner decision 136).
func readResultAuthorizer(ctx context.Context, dispatcher im.Dispatcher) func(im.ReadResult) bool {
	_, fabricIndex := im.FabricFilterFromContext(ctx)
	if im.IsPASEFromContext(ctx) {
		return nil // PASE — commissioning channel, implicit Administer
	}
	aclChecker, hasACL := dispatcher.(im.ACLChecker)
	if !hasACL {
		return func(im.ReadResult) bool { return false }
	}
	if !im.HasAccessingFabric(fabricIndex) {
		// No accessing fabric and not PASE: no subject the ACL could grant
		// anything to (matter.js FabricAccessControl.ts:189-191 keys the
		// implicit grant on the PASE auth mode only; review 2026-11
		// W2-sec-03). An index the bridge could not resolve
		// (endpoint.FabricIndexUnresolvable) is no accessing fabric
		// either, as at every gate of im: the filter does not rest on the
		// access-control source knowing that index (review II,
		// R2-P0-rv-A08).
		return func(im.ReadResult) bool { return false }
	}
	subjectNodeID, subjectCATs := im.SubjectFromContext(ctx)
	privProvider, hasPriv := dispatcher.(im.AttributeReadPrivilegeProvider)
	return func(rr im.ReadResult) bool {
		var priv uint8 = 1 // View default
		if hasPriv {
			priv = privProvider.MinReadPrivilege(rr.Path.Endpoint, rr.Path.Cluster, rr.Path.Attribute)
		}
		// A subject not permitted to read this attribute — drop.
		return aclChecker.CheckACL(ctx, fabricIndex, subjectNodeID, subjectCATs, rr.Path.Endpoint, rr.Path.Cluster, priv).IsSuccess()
	}
}

// withSessionAuthMode stamps the PASE auth mode on ctx when sessionID was
// established over PASE ([Bridge.resolveSessionPASE]).
func (b *Bridge) withSessionAuthMode(ctx context.Context, sessionID uint16) context.Context {
	if b.resolveSessionPASE(sessionID) {
		return im.WithAuthModePASE(ctx)
	}
	return ctx
}

// withReadAuthority stamps on ctx the subject's read check
// ([im.WithAuthority]) that [readResultAuthorizer] applies to the
// results, so the dispatcher authorises a location before it reads the
// server there ([im.LookupAuthority]): a subscriber without the grant
// drives no server read in its priming report or its reports, as a
// Read's subject drives none since R2-P6-bound-0405 (decision 162;
// review II, wave 3, the gate's hand-over). The per-result filter stays
// the gate of what is reported. A PASE session gets no stamp — nothing is
// checked for it; a dispatcher without an [im.ACLChecker] and a session
// without an accessing fabric get one that refuses every location, as the
// filter refuses every result.
func withReadAuthority(ctx context.Context, dispatcher im.Dispatcher) context.Context {
	if im.IsPASEFromContext(ctx) {
		return ctx
	}
	refuse := func(uint16, uint32, uint8) im.StatusCode { return im.StatusUnsupportedAccess }
	aclChecker, hasACL := dispatcher.(im.ACLChecker)
	if !hasACL {
		return im.WithAuthority(ctx, refuse)
	}
	_, fabricIndex := im.FabricFilterFromContext(ctx)
	if !im.HasAccessingFabric(fabricIndex) {
		return im.WithAuthority(ctx, refuse)
	}
	subjectNodeID, subjectCATs := im.SubjectFromContext(ctx)
	base := ctx
	return im.WithAuthority(ctx, func(endpoint uint16, clusterID uint32, privilege uint8) im.StatusCode {
		return aclChecker.CheckACL(base, fabricIndex, subjectNodeID, subjectCATs, endpoint, clusterID, privilege)
	})
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
	if b.endIfSubjectReadsNothing(ctx, dispatcher, target, sub) {
		return
	}

	// The engine's stop ends the wait for a chunk's answer (W2-conc-06),
	// and so does the subscription's close (R2-P6-fsm-0513).
	abort, endAbort := b.reportAbort(ctx, sub.ID)
	defer endAbort()
	target.abort = abort

	report := im.ReportData{
		HasSubscription: true,
		SubscriptionID:  sub.ID,
		Reports:         b.subscriptionAttributeReports(ctx, dispatcher, target, sub, paths),
	}

	// An empty report is a max-interval keep-alive (nothing changed, or
	// every dirty path was dropped by the ACL gate). It asks for the peer's
	// StatusResponse like a report with data — SuppressResponse stays
	// false — and waits for it as a report with data does
	// ([Bridge.deliverOngoingReport]), so the peer's INVALID_SUBSCRIPTION
	// answer ends the subscription and a keep-alive left unanswered is a
	// failed update. Mirrors matter.js
	// packages/node/src/node/server/ServerSubscription.ts:913-914 at
	// db021da0 (the keep-alive's `suppressResponse: false`, "Requested so
	// the peer's INVALID_SUBSCRIPTION answer (Core § 8.5) is read on every
	// transport") and :938-950 (an error status ends it).
	freshExch, sent := b.deliverOngoingReport(sub, target, report, &owedReport{paths: paths}, "report")
	if !sent {
		return
	}
	b.logger.Debug("matter.tx.subscribe.report",
		slog.Int("subscription_id", int(sub.ID)),
		slog.Int("paths", len(paths)),
		slog.Int("reports", len(report.Reports)),
		slog.Int("exchange_id", int(freshExch)))
}

// endIfSubjectReadsNothing ends sub, terminated, when the subject it was
// established for may read none of its paths any more
// ([Bridge.subjectReadsNothing]) and reports whether it did: a report finds
// the subject without a readable path after an ACL change, and the
// subscription gives its place in the fabric's quota back instead of
// living on on keep-alives (review II, R2-P3-admin-03, owner decision 53).
// The rule is the module's own, beside its own quota; it mirrors no
// reference line.
func (b *Bridge) endIfSubjectReadsNothing(ctx context.Context, dispatcher im.Dispatcher, target subTarget, sub *subscription.Subscription) bool {
	if !b.subjectReadsNothing(ctx, dispatcher, target, sub) {
		return false
	}
	b.routing.subTargets.Delete(sub.ID)
	b.resetFailedUpdates(sub.ID)
	b.subAccess.Delete(sub.ID)
	b.forgetReportExchange(sub.ID)
	if m := b.subscriptionManagerLocked(); m != nil {
		_ = m.Close(sub.ID) // ErrNotFound is fine — racing with peer Close
	}
	if b.logger != nil {
		b.logger.Info("matter.subscribe.subject_reads_nothing",
			slog.Int("subscription_id", int(sub.ID)),
			slog.Int("fabric_index", int(target.fabricIndex)))
	}
	return true
}

// subjectAccess is what [Bridge.subjectReadsNothing] remembers of one
// subscription between its reports.
type subjectAccess struct {
	mu sync.Mutex
	// passed: the subject last read a path of the subscription under
	// passedACL, the fabric's entries, and the served topology passedGen.
	passed    bool
	passedACL []store.ACLEntry
	passedGen uint64
	// refused: a first observation found every path refused under
	// refusedACL, the paths the node served then fingerprinted by
	// refusedServed; a second one under the same ends the subscription.
	refused       bool
	refusedACL    []store.ACLEntry
	refusedServed [sha256.Size]byte
}

// subjectReadsNothing reports whether the subscription's subject is
// refused every attribute and every event cluster its paths resolve to,
// at least one of them, at two consecutive observations of one unchanged
// ACL of its fabric and one set of the subscription's paths the node
// serves — not a count of reassemblies, which moves without a change
// (review II, R2-P3-rr-dev-03) — one observation is not
// enough, since a list write of the ACL stores its REPLACE before its
// appends and an observation between them finds the entries missing
// (review II, R2-P3-dr-dev-01). It decides nothing while a chunked write
// of the fabric's ACL is in progress (the write's hold, owner decision
// 52), nor on a fabric ACL it cannot list: the walk over the paths is
// judged against the entries listed for it, never against a second read
// that may fail (R2-P3-dr-dev-05). A path that resolves to no attribute
// the node serves is no refusal (R2-P3-dr-dev-04). The walk runs only when
// the fabric's entries or the served topology differ from those the
// subject last passed under, not before every report (R2-P3-dr-dev-06);
// the entries are read at each report, the one store read the decision
// costs. A PASE subscription, a dispatcher without an ACL, a session
// without an accessing fabric, a bridge without an ACL source, and any
// answer of the access check other than UNSUPPORTED_ACCESS leave the
// subscription alone.
func (b *Bridge) subjectReadsNothing(ctx context.Context, dispatcher im.Dispatcher, target subTarget, sub *subscription.Subscription) bool {
	checker, hasACL := dispatcher.(im.ACLChecker)
	if target.pase || !hasACL || !im.HasAccessingFabric(target.fabricIndex) {
		return false
	}
	b.mu.RLock()
	lister := b.aclLister
	gen := b.servedGen
	b.mu.RUnlock()
	if lister == nil || b.aclWriteInProgress(target.fabricIndex) {
		return false
	}
	entries, err := lister.ListACL(ctx, target.fabricIndex)
	if err != nil {
		return false
	}
	raw, _ := b.subAccess.LoadOrStore(sub.ID, &subjectAccess{})
	if sub.IsClosed() {
		// Closed meanwhile: its routing release may have run before the
		// entry was stored, and nothing else would drop it (review II,
		// R2-P6-conc-0512). A close after this check releases it.
		b.subAccess.Delete(sub.ID)
		return false
	}
	st, ok := raw.(*subjectAccess)
	if !ok {
		return false
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.passed && st.passedGen == gen && reflect.DeepEqual(st.passedACL, entries) {
		return false
	}
	refused, served := b.subjectRefusedEveryPath(withPinnedACL(ctx, target.fabricIndex, entries), checker, dispatcher, target, sub)
	if ctx.Err() != nil {
		return false
	}
	if !refused {
		st.passed, st.passedACL, st.passedGen = true, entries, gen
		st.refused, st.refusedACL = false, nil
		return false
	}
	if st.refused && st.refusedServed == served && reflect.DeepEqual(st.refusedACL, entries) && !b.aclWriteInProgress(target.fabricIndex) {
		return true
	}
	st.passed, st.passedACL = false, nil
	st.refused, st.refusedACL, st.refusedServed = true, entries, served
	return false
}

// aclWriteInProgress reports whether a chunked write of fabricIndex's ACL
// (AccessControl, endpoint 0, attribute ACL) holds the attribute.
func (b *Bridge) aclWriteInProgress(fabricIndex uint8) bool {
	return b.routing.writeHolds.Held(fabricIndex, 0, accessControlClusterID, accessControlACLAttributeID)
}

// accessControlClusterID and accessControlACLAttributeID name the
// AccessControl cluster's ACL attribute, the ids im/write_chunking.go
// names (accessControlCluster, accessControlACL).
const (
	accessControlClusterID      uint32 = 0x001F
	accessControlACLAttributeID uint32 = 0x0000
)

// subjectRefusedEveryPath walks the subscription's paths, as ctx's subject
// under its pinned ACL, and reports whether at least one is refused and
// none may be read; it stops at the first path the subject may read. For
// a refusal, served fingerprints the locations the walk judged — the
// (endpoint, cluster, privilege) of every attribute location and the
// (endpoint, cluster) of every event cluster, in walk order.
//
// The walk judges a location without reading the server there: it stamps
// a recording access check ([im.WithAuthority]) that the dispatcher asks
// before each read ([im.LookupAuthority]), so a denied location is noted
// and not read, and the first granted one ends the walk (Fable's read of
// sub round 1, A-1; decision 162). The dispatcher asks only at a location
// that exists, so a path that resolves to nothing the node serves stays
// no refusal (R2-P3-dr-dev-04). A dispatcher that never asks the check
// has read each location already; its results are judged as they come.
func (b *Bridge) subjectRefusedEveryPath(ctx context.Context, checker im.ACLChecker, dispatcher im.Dispatcher, target subTarget, sub *subscription.Subscription) (refused bool, served [sha256.Size]byte) {
	digest := sha256.New()
	note := func(kind byte, endpoint uint16, cluster, detail uint32) {
		var rec [11]byte
		rec[0] = kind
		binary.BigEndian.PutUint16(rec[1:], endpoint)
		binary.BigEndian.PutUint32(rec[3:], cluster)
		binary.BigEndian.PutUint32(rec[7:], detail)
		digest.Write(rec[:])
	}
	readCtx := im.WithFabricFilter(ctx, target.fabricFiltered, target.fabricIndex)
	readCtx = im.WithSubject(readCtx, target.subjectNodeID, target.subjectCATs)
	granted, asked := false, false
	attrCtx := im.WithAuthority(readCtx, func(endpoint uint16, clusterID uint32, privilege uint8) im.StatusCode {
		asked = true
		if granted {
			// The walk's answer is known; read nothing more.
			return im.StatusUnsupportedAccess
		}
		st := checker.CheckACL(readCtx, target.fabricIndex, target.subjectNodeID, target.subjectCATs, endpoint, clusterID, privilege)
		if st != im.StatusUnsupportedAccess {
			granted = true
			return st
		}
		note('a', endpoint, clusterID, uint32(privilege))
		refused = true
		return st
	})
	exp := im.NewAttributeExpansion(im.WithElementQualities(attrCtx, specElementQualities{}), dispatcher, sub.AttributePaths)
	privProvider, hasPriv := dispatcher.(im.AttributeReadPrivilegeProvider)
	for !granted {
		_, rr, ok := exp.Next()
		if !ok {
			break
		}
		if asked || notServed(rr.Status) {
			continue
		}
		// A dispatcher that never asks the stamped check (not an
		// [endpoint.TopologyDispatcher]) has read the location already;
		// the result is judged as it comes back.
		var priv uint8 = 1 // View default
		if hasPriv {
			priv = privProvider.MinReadPrivilege(rr.Path.Endpoint, rr.Path.Cluster, rr.Path.Attribute)
		}
		if checker.CheckACL(readCtx, target.fabricIndex, target.subjectNodeID, target.subjectCATs, rr.Path.Endpoint, rr.Path.Cluster, priv) != im.StatusUnsupportedAccess {
			return false, served
		}
		note('a', rr.Path.Endpoint, rr.Path.Cluster, rr.Path.Attribute)
		refused = true
	}
	if granted {
		return false, served
	}
	auth := b.eventReadAuthorizer(dispatcher, target.fabricIndex, false, target.subjectNodeID, target.subjectCATs)
	eventCtx := im.WithAuthority(readCtx, func(endpoint uint16, clusterID uint32, _ uint8) im.StatusCode {
		asked = true
		if granted {
			return im.StatusUnsupportedAccess
		}
		if auth.Allows(readCtx, endpoint, clusterID) {
			granted = true
			return im.StatusSuccess
		}
		note('e', endpoint, clusterID, 0)
		refused = true
		return im.StatusUnsupportedAccess
	})
	for _, p := range sub.EventPaths {
		probe := im.ConcreteAttributePath{
			Endpoint: p.Endpoint, HasEndpoint: p.HasEndpoint,
			Cluster: p.Cluster, HasCluster: p.HasCluster,
			Attribute: clusterRevisionAttributeID, HasAttribute: true,
		}
		for _, rr := range dispatcher.Read(eventCtx, probe) {
			if granted {
				return false, served
			}
			if asked || rr.Status != im.StatusSuccess {
				continue
			}
			if auth.Allows(readCtx, rr.Path.Endpoint, rr.Path.Cluster) {
				return false, served
			}
			note('e', rr.Path.Endpoint, rr.Path.Cluster, 0)
			refused = true
		}
		if granted {
			return false, served
		}
	}
	digest.Sum(served[:0])
	return refused, served
}

// notServed reports whether a read result's status says that its path
// resolves to nothing the node serves.
func notServed(st im.StatusCode) bool {
	return st == im.StatusUnsupportedNode || st == im.StatusUnsupportedEndpoint ||
		st == im.StatusUnsupportedCluster || st == im.StatusUnsupportedAttribute
}

// pinnedACL is a fabric's ACL as one listing returned it, attached to a
// context by [withPinnedACL].
type pinnedACL struct {
	fabricIndex uint8
	entries     []store.ACLEntry
}

type pinnedACLKey struct{}

// withPinnedACL has every access check made under ctx judge fabricIndex
// against entries instead of reading the ACL source again.
func withPinnedACL(ctx context.Context, fabricIndex uint8, entries []store.ACLEntry) context.Context {
	return context.WithValue(ctx, pinnedACLKey{}, pinnedACL{fabricIndex: fabricIndex, entries: entries})
}

// pinnedACLSource is the ACL source the bridge hands its dispatcher: the
// attached one, except for a context that carries a pinned ACL of the
// fabric asked for ([withPinnedACL]).
type pinnedACLSource struct{ inner endpointpkg.ACLLister }

func (s pinnedACLSource) ListACL(ctx context.Context, fabricIndex uint8) ([]store.ACLEntry, error) {
	if pin, ok := ctx.Value(pinnedACLKey{}).(pinnedACL); ok && pin.fabricIndex == fabricIndex {
		return pin.entries, nil
	}
	return s.inner.ListACL(ctx, fabricIndex)
}

// pinnableACL wraps l for the dispatcher; nil stays nil, so a bridge
// without an ACL source keeps denying every operational request.
func pinnableACL(l endpointpkg.ACLLister) endpointpkg.ACLLister {
	if l == nil {
		return nil
	}
	return pinnedACLSource{inner: l}
}

// subscriptionAttributeReports reads paths for sub — the changed ones its
// wildcard flags do not skip — and returns the attribute reports the
// subscribing subject may read, under the identity captured on target.
// Shared by the ongoing report and the flush.
//
// A change report carries values only: a changed path whose read no longer
// succeeds — its endpoint removed, an attribute no longer served — is left
// out (review II, R2-P4-imread-09). Mirrors matter.js
// packages/protocol/src/action/server/AttributeSubscriptionResponse.ts:91-93
// at db021da0 (addStatus suppressed "For Filtered responses"), through which
// packages/node/src/node/server/ServerSubscription.ts:860-868 builds every
// update.
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
	readCtx = withReadAuthority(readCtx, dispatcher)
	var reports []im.AttributeReport
	for _, path := range b.unskippedChangedPaths(readCtx, dispatcher, sub, paths) {
		for _, rr := range b.readAuthorizedResults(readCtx, dispatcher, path) {
			if rr.Status != im.StatusSuccess {
				continue
			}
			reports = append(reports, im.AttributeReport{Path: rr.Path, DataVersion: rr.DataVersion, Value: rr.Value, FabricScope: rr.FabricScope})
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
	// ctx ends every chunk's wait, not only the last one's
	// (awaitFlushedReport): Stop's stopSessionCloseBudget and a fabric
	// removal's ctx bound the whole flush however many chunks it takes
	// (review II, R2-P2-dr-req-01).
	target.abort = ctx.Done()
	_, freshExch, wait, err := b.sendInitiatedReportArmed(target, report, true)
	if err != nil {
		if len(events) > 0 {
			sub.RequeueEvents(events[0].Number)
		}
		if len(paths) > 0 {
			sub.RequeueAttributes(paths)
		}
		if errors.Is(err, errChunkWaitAborted) {
			// ctx ended the flush (Stop's budget, a removal's ctx): the
			// peer did nothing wrong, as for the ongoing reporters (review
			// II, R2-P2-rr-dev-05).
			return
		}
		b.noteSubscriptionSendError(sub.ID, "flush", err)
		return
	}
	// The flushed chunks are untracked, so no counter comes back and no
	// counter→subscription entry is owed: nothing retransmits them, and
	// their acks find no entry (outboundReliableTracker.Ack is a no-op).
	b.noteUpdateDelivered(sub.ID)
	b.rememberReportExchange(sub.ID, target.sessionID, freshExch)
	b.releaseReportOfClosed(sub)
	b.logger.Debug("matter.tx.subscribe.flush",
		slog.Int("subscription_id", int(sub.ID)),
		slog.Int("events", len(report.EventReports)),
		slog.Int("reports", len(report.Reports)),
		slog.Int("exchange_id", int(freshExch)))
	b.awaitFlushedReport(ctx, "subscribe.flush", target.sessionID, freshExch, wait)
}

// sendErrorRetryLimit is how many consecutive failed updates an ongoing
// report path tolerates before the subscription is reaped: the third
// ends it. matter.js requeues while `#sendUpdateErrorCounter <= 2`
// (packages/node/src/node/server/ServerSubscription.ts:569 at db021da0);
// at the third it closes for a communication failure only (:590-603) —
// here every kind closes ([Bridge.noteSubscriptionSendError]).
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
// is still reaped; its sessions are left alone. Every kind of failed
// update counts here: a send that fails at once, a report whose
// StatusResponse does not come within the wait, and one whose last
// message ran out of retransmissions ([Bridge.noteReportUndelivered]).
// A subscription has one report in flight ([Bridge.deliverOngoingReport]),
// so its failures arrive in order, one per update. matter.js counts every
// failed update and owes its data again while the count is at most 2
// (packages/node/src/node/server/ServerSubscription.ts:564, :569-584 at
// db021da0); at the third it closes the subscription, leaving the session
// alone, only when the failure is one of its four communication classes
// (:590-603) — no answer, retransmissions run out, the session closed, a
// network error — and otherwise keeps the subscription with the update's
// data dropped (:605). Here the third failure closes the subscription
// whatever its kind: the communication kinds as matter.js, the local ones
// (an encode or chunking failure, a message above the TCP limit, a missing
// listener, an encryption failure) by the module's own rule, so that a
// subscriber is not left with data it will never receive.
//
// op names the reporter for the log line ("report" / "event_report").
func (b *Bridge) noteSubscriptionSendError(subID uint32, op string, err error) {
	b.subUpdateMu.Lock()
	if _, live := b.routing.subTargets.Load(subID); !live {
		// Closed while the report was being sent: nothing is left to
		// count against. Taken under subUpdateMu, which every release
		// takes after it drops the routing target
		// ([Bridge.resetFailedUpdates]), so no count is stored after the
		// release has reset it.
		b.subUpdateMu.Unlock()
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
	} else {
		// The routing target goes under the lock, so a failure taken at
		// the same moment finds the subscription closed and counts
		// nothing.
		b.routing.subTargets.Delete(subID)
		b.subSendErrorCount.Delete(subID)
	}
	b.subUpdateMu.Unlock()
	if failures <= sendErrorRetryLimit {
		if b.logger != nil {
			b.logger.Debug("matter.tx.subscribe."+op,
				slog.Int("subscription_id", int(subID)),
				slog.Int("send_failures", failures),
				slog.Int("retry_limit", sendErrorRetryLimit),
				slog.Any("err", err))
		}
		return
	}
	// Cap reached — the routing target is dropped; evict the subscription
	// from the manager so the engine stops ticking a dead subscription.
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

// resetFailedUpdates drops subID's count of failed updates: the
// subscription ended. Every change of the count is taken under
// subUpdateMu, so a reset is never overwritten by a failed update counted
// from another goroutine at the same moment — the reporter and the ACK
// pump each change it.
func (b *Bridge) resetFailedUpdates(subID uint32) {
	b.subUpdateMu.Lock()
	b.subSendErrorCount.Delete(subID)
	b.subUpdateMu.Unlock()
}

// noteUpdateDelivered takes an update of subID as delivered: the count of
// failed updates starts again — matter.js ServerSubscription.ts:555-557 at
// db021da0 resets #sendUpdateErrorCounter once #sendUpdateMessage
// returned, which is after the peer's Success StatusResponse.
func (b *Bridge) noteUpdateDelivered(subID uint32) {
	b.subUpdateMu.Lock()
	defer b.subUpdateMu.Unlock()
	b.subSendErrorCount.Delete(subID)
}

// owedReport is what an ongoing report owes its subscription until the
// update is settled: the attribute paths it carried and, when it carried
// events, the number of its first one. Recorded in
// [Bridge.reportCounterOwner] under the report's last message's counter
// while its reporter waits ([Bridge.deliverOngoingReport]).
type owedReport struct {
	subID      uint32
	exchangeID uint16 // the report's exchange, whose wait the ACK pump ends
	paths      []im.ConcreteAttributePath
	eventsFrom uint64
	hasEvents  bool
	// settled is taken by whoever settles the update — the reporter at
	// the end of its wait, or the ACK pump when the report's last message
	// ran out of retransmissions — so it is counted and owed again once.
	settled atomic.Bool
}

// requeue owes what the report carried again: the next report of sub
// carries it. Mirrors matter.js
// packages/node/src/node/server/ServerSubscription.ts:569-584 at db021da0
// (the attribute filter and #outstandingEventsMinNumber put back while
// #sendUpdateErrorCounter <= 2).
func (o *owedReport) requeue(sub *subscription.Subscription) {
	if o.hasEvents {
		sub.RequeueEvents(o.eventsFrom)
	}
	sub.RequeueAttributes(o.paths)
}

// deliverOngoingReport sends report, an ongoing report of sub that owes
// owed, on a fresh exchange and returns once the update is settled, so a
// subscription has one report in flight: the engine starts no report of
// sub while its reporter runs, and what changes meanwhile stays owed on
// the subscription — one set of attribute paths and the lowest event
// number — for the next report to carry together
// ([subscription.Manager] tick). The update is settled by the peer's
// StatusResponse to the report's last message: Success delivers it; any
// other status ends the subscription at once; none within the wait — or
// the last message's retransmissions running out, or the session's close
// — is a failed update, its data owed again and the third in a row
// ending the subscription ([Bridge.noteSubscriptionSendError]). The wait
// is bounded as a chunk's ([Bridge.chunkStatusResponseTimeout]) and by
// the engine's stop, which owes the data again without counting. A
// keep-alive waits as a report with data does. It returns the report's
// exchange and whether the report was delivered.
//
// Mirrors matter.js at db021da0: #triggerSendUpdate sends no update while
// one is in flight and only notes the next as due
// (packages/node/src/node/server/ServerSubscription.ts:520-529); changes
// meanwhile join one outstanding attribute set and one minimum event
// number (:346-357, :392-394), which the next report takes whole
// (:537-543, :610-622); #sendUpdateMessage sends with
// `suppressResponse: false`, the keep-alive included (:913-914, :928),
// and sendDataReport then awaits waitForSuccess
// (packages/protocol/src/interaction/InteractionMessenger.ts:838-845,
// :221-227); an error status ends the subscription
// (ServerSubscription.ts:938-950), a missing answer is a failed update
// (:558-608).
func (b *Bridge) deliverOngoingReport(sub *subscription.Subscription, target subTarget, report im.ReportData, owed *owedReport, op string) (exchangeID uint16, delivered bool) {
	owed.subID = sub.ID
	counters, exch, wait, err := b.sendInitiatedReportArmed(target, report, true)
	if err != nil {
		switch {
		case errors.Is(err, errChunkWaitAborted):
			owed.requeue(sub) // the engine stopped; the peer did nothing wrong
		case errors.Is(err, errChunkRejected):
			// An error status to a chunk ends the subscription as one to
			// the last message does.
			b.closeRefusedSubscription(sub.ID, 0, err)
		default:
			// Counted before the data is owed again: the report the
			// requeue starts is the next update.
			b.noteSubscriptionSendError(sub.ID, op, err)
			owed.requeue(sub)
		}
		return 0, false
	}
	owed.exchangeID = exch
	b.rememberReportExchange(sub.ID, target.sessionID, exch)
	key := b.recordOngoingReport(target.sessionID, counters, owed)
	answer, answered, aborted := b.awaitReportAnswer(target, exch, wait, op)
	status := answer.status
	if key != 0 {
		b.reportCounterOwner.CompareAndDelete(key, owed)
	}
	if !owed.settled.CompareAndSwap(false, true) {
		// The ACK pump settled it: the last message ran out of
		// retransmissions.
		b.releaseReportOfClosed(sub)
		return exch, false
	}
	switch {
	case aborted:
		owed.requeue(sub)
	case answered && answer.undecodable:
		// No status of the peer's, so no refusal: a failed update, its
		// data owed again (owner decision 176; [errChunkAnswerUndecodable]).
		b.noteSubscriptionSendError(sub.ID, op, fmt.Errorf("%w: exchange %d", errChunkAnswerUndecodable, exch))
		owed.requeue(sub)
	case answered && status.IsSuccess():
		b.noteUpdateDelivered(sub.ID)
		b.releaseReportOfClosed(sub)
		return exch, true
	case answered:
		b.closeRefusedSubscription(sub.ID, exch, fmt.Errorf("%w: %s", errChunkRejected, status))
	default:
		b.noteSubscriptionSendError(sub.ID, op, fmt.Errorf("%w: exchange %d", errChunkUnanswered, exch))
		owed.requeue(sub)
	}
	b.releaseReportOfClosed(sub)
	return exch, false
}

// recordOngoingReport records owed under the last of the report's
// tracked messages counters on sessionID, for the ACK pump to settle the
// update should that message run out of retransmissions
// ([Bridge.noteReportUndelivered]), and returns the record's key (0: no
// tracked message — no tracker, a reliable connection). A message the
// pump gave up before the record existed is settled here as it would have
// been then (review II, R2-P4-nv-07).
func (b *Bridge) recordOngoingReport(sessionID uint16, counters []uint32, owed *owedReport) uint64 {
	if len(counters) == 0 {
		return 0
	}
	last := counters[len(counters)-1]
	key := reportCounterKey(sessionID, last)
	b.reportCounterOwner.Store(key, owed)
	b.mu.RLock()
	tracker := b.outboundReliable
	b.mu.RUnlock()
	if tracker.Abandoned(sessionID, last) {
		b.noteReportUndelivered(sessionID, last)
	}
	return key
}

// reportAnswerBoundForTests, when set, replaces the bound of
// [Bridge.awaitReportAnswer] — set by this package's tests only, so the
// many that leave a report unanswered do not wait out the production bound.
// Zero in production.
var reportAnswerBoundForTests time.Duration

// awaitReportAnswer waits for the peer's StatusResponse to an ongoing
// report's last message, armed by [Bridge.sendInitiatedReportArmed]:
// answered with its status, or not — the wait ran out, or it was disarmed
// without an answer (the session's close, the ACK pump giving the message
// up) — or aborted by the engine's stop. An answer's standalone ack goes
// out at once: nothing of ours follows on the exchange to piggyback it on.
func (b *Bridge) awaitReportAnswer(target subTarget, exchangeID uint16, wait <-chan statusAnswer, op string) (answer statusAnswer, answered, aborted bool) {
	// One timer from the send, on [Bridge.chunkStatusResponseTimeout]'s
	// bound: over UDP the sum of two schedule terms — this node's own
	// retransmissions (with its sender pad) and the peer's, summed on one
	// base interval and without a sender pad — plus the expected
	// processing time and the response buffer; over a reliable connection
	// 35 s. It differs from matter.js's bound in where the clock starts and
	// in the peer term (notes/parity/by_design.md,
	// BD-Matter-Subscription-ReportWaitBound); whether it can come out
	// shorter than matter.js's is not established.
	timeout := b.chunkStatusResponseTimeout(target.src, target.sessionID)
	if reportAnswerBoundForTests > 0 && b.chunkStatusResponseTimeoutOverride == 0 {
		timeout = reportAnswerBoundForTests
	}
	timer := b.clk().NewTimer(timeout)
	defer timer.Stop()
	defer b.disarmStatusResponseWait(target.sessionID, exchangeID, true)
	select {
	case st, ok := <-wait:
		if !ok {
			b.logger.Debug("matter.tx.subscribe."+op+".unanswered",
				slog.Int("exchange_id", int(exchangeID)),
				slog.String("hint", "wait ended without an answer"))
			return statusAnswer{}, false, false
		}
		b.expediteDuplicateAck(target.sessionID, exchangeID, true)
		b.RunAckPumpOnce(b.clk().Now())
		return st, true, false
	case <-target.abort:
		return statusAnswer{}, false, true
	case <-timer.C():
		b.logger.Debug("matter.tx.subscribe."+op+".unanswered",
			slog.Int("exchange_id", int(exchangeID)),
			slog.String("timeout", timeout.String()))
		return statusAnswer{}, false, false
	}
}

// noteReportUndelivered is called by the ACK pump when a message of
// sessionID ran out of retransmissions. When it is the last message of an
// ongoing report whose update is not settled yet, the report is one failed
// update of its subscription: the failure is counted, the third in a row
// ending the subscription ([Bridge.noteSubscriptionSendError]), what it
// carried is owed again, and its reporter's wait ends. It is no peer
// loss — the peer's sessions stay. Mirrors matter.js
// packages/node/src/node/server/InteractionServer.ts:96-97 at db021da0
// ("a report we cannot push does not prove they are" gone:
// SUBSCRIPTION_EXCHANGE_OPTIONS = { suppressPeerLoss: true }, the options
// of every report exchange, :785-797) and ServerSubscription.ts:569-584,
// :590-603 (the failed update requeued while #sendUpdateErrorCounter <= 2;
// at the third, a failure of this communication kind gives the
// subscription up, "The session is left alone").
func (b *Bridge) noteReportUndelivered(sessionID uint16, counter uint32) {
	raw, ok := b.reportCounterOwner.LoadAndDelete(reportCounterKey(sessionID, counter))
	if !ok {
		return
	}
	owed, ok := raw.(*owedReport)
	if !ok || owed.subID == 0 || !owed.settled.CompareAndSwap(false, true) {
		return
	}
	// Counted before the data is owed again: the report the requeue
	// starts is the next update.
	b.noteSubscriptionSendError(owed.subID, "report_undelivered", mrp.ErrMaxRetransmissionsReached)
	if m := b.subscriptionManagerLocked(); m != nil {
		if sub, err := m.Get(owed.subID); err == nil {
			owed.requeue(sub)
		}
	}
	if owed.exchangeID != 0 {
		b.disarmStatusResponseWait(sessionID, owed.exchangeID, true)
	}
}

// rememberReportExchange records that subID's latest ongoing report
// rides the bridge-initiated exchange (sessionID, exchangeID), replacing
// the record of the previous report so the table holds one entry per
// live subscription. Consulted by [Bridge.closeSubscriptionByExchange]
// when the peer answers on that exchange after its reporter stopped
// waiting.
func (b *Bridge) rememberReportExchange(subID uint32, sessionID, exchangeID uint16) {
	key := mrp.ExchangeKey{SessionID: sessionID, ExchangeID: exchangeID, Initiator: true}
	if prev, loaded := b.subReportExchange.Swap(subID, key); loaded {
		if prevKey, ok := prev.(mrp.ExchangeKey); ok && prevKey != key {
			b.reportExchangeOwner.Delete(prevKey)
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
		}
	}
}

// closeSubscriptionByExchange ends the subscription whose latest ongoing
// report rode the bridge-initiated exchange (sessionID, exchangeID),
// because the peer answered that report with the error status given —
// after the report's reporter stopped waiting, which ends it itself
// otherwise ([Bridge.deliverOngoingReport]). A report exchange nobody
// remembers (an initial-report chunk, a timed-request reply, a
// subscription already reaped) is ignored.
func (b *Bridge) closeSubscriptionByExchange(sessionID, exchangeID uint16, status im.StatusCode) {
	raw, ok := b.reportExchangeOwner.Load(mrp.ExchangeKey{SessionID: sessionID, ExchangeID: exchangeID, Initiator: true})
	if !ok {
		return
	}
	subID, ok := raw.(uint32)
	if !ok || subID == 0 {
		return
	}
	b.closeRefusedSubscription(subID, exchangeID, fmt.Errorf("%w: %s", errChunkRejected, status))
}

// closeRefusedSubscription ends subID because the peer answered one of
// its reports with an error status (cause): at once, without a retry.
// Mirrors matter.js ServerSubscription.ts:938-950 at db021da0: every error
// status the peer answers a report with marks the subscription terminated
// (:947; InvalidSubscription and Failure differ from the others only in
// the log line, :939-944), and #closeFromUpdate cancels it (:950).
func (b *Bridge) closeRefusedSubscription(subID uint32, exchangeID uint16, cause error) {
	b.routing.subTargets.Delete(subID)
	b.resetFailedUpdates(subID)
	b.forgetReportExchange(subID)
	if m := b.subscriptionManagerLocked(); m != nil {
		_ = m.Close(subID) // ErrNotFound is fine — racing with peer Close
	}
	if b.logger != nil {
		b.logger.Info("matter.subscribe.peer_rejected",
			slog.Int("subscription_id", int(subID)),
			slog.Int("exchange_id", int(exchangeID)),
			slog.Any("err", cause),
			slog.String("hint", "peer answered an ongoing report with an error status; subscription closed"))
	}
}

// releaseReportCounter takes the peer's acknowledgement of (sessionID,
// counter): the last message of an ongoing report reached the peer, so
// its retransmissions cannot run out and the ACK pump has nothing to
// settle; the update itself is settled by the peer's StatusResponse
// ([Bridge.deliverOngoingReport]). Caller is the inbound HasAck pipeline,
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

// nextOutboundExchangeID allocates the exchange identifier of a
// server-initiated exchange: the next value of a 16-bit counter that New
// seeds at random and that wraps past 0xFFFF to 0. Mirrors matter.js
// ExchangeCounter.getIncrementedCounter
// (packages/protocol/src/protocol/ExchangeManager.ts:794-808) and
// connectedhomeip src/messaging/ExchangeMgr.cpp:67, :128
// (`mNextExchangeId++` on a uint16). The exchange id is a full 16-bit
// field; the Initiator flag is a separate bit of the exchange flags.
//
// An id is re-used only after the whole 16-bit space has been consumed.
// matter.js also skips an id an open exchange it started on the same
// session still holds (ExchangeManager.ts:189-197 at db021da0,
// #freeExchangeIdFor); the bridge does so for the exchanges it keeps a
// table of — the ones it opens on a secure session for BDX and Invoke
// ([Bridge.openInitiatedExchange]; review II, R2-P2-ctr-03).
func (b *Bridge) nextOutboundExchangeID() uint16 {
	return uint16(b.outboundExchangeID.Add(1)) //nolint:gosec // G115: the truncation to 16 bits is the wrap past 0xFFFF to 0
}

// sendReportChunksNotify splits report into per-datagram chunks and ships
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
//
// With armFinal it also arms the wait for the peer's StatusResponse to the last chunk just
// before that chunk goes out and returns it as finalWait — the waits for
// the chunks before it are this loop's own, on the same key, so arming the
// final wait up front would have the first of them replace and close it.
// A failed send disarms it.
func (b *Bridge) sendReportChunksNotify(target subTarget, report im.ReportData, armFinal bool) (counters []uint32, finalWait <-chan statusAnswer, err error) {
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
	leg := chunkLeg{
		op: op, src: target.src,
		sessionID: target.sessionID, exchangeID: target.exchangeID, role: ourRole,
		send: func(i int, body []byte) (uint32, error) {
			if i > 0 {
				// Chunk N+1 piggybacks the ack of the peer's StatusResponse
				// to chunk N. chip's ReliableMessageMgr drops it otherwise
				// ("Dropping message without piggyback ack when we are
				// waiting for an ack") — the same rule a reply's chunks
				// honour ([Bridge.replyChunkLeg]). matter.js gets it for
				// free: its MessageExchange piggybacks the pending ack on
				// the next send (packages/protocol/src/protocol/MessageExchange.ts:711-716
				// at db021da0, the pending ack taken for the message).
				target = b.piggybackOwedAck(target, ourRole)
			}
			return b.sendUnsolicitedIM(target, im.OpcodeReportData, body)
		},
		abort: target.abort,
	}
	if target.flush {
		leg.timeout = flushStatusResponseTimeout
	}
	for i, chunk := range chunks {
		body, err := EncodeReportData(chunk)
		if err != nil {
			debugReplyError(b.logger, "encode_ongoing_report", target.src, err)
			return nil, nil, err
		}
		wait := chunkNoWait
		switch {
		case chunk.MoreChunkedMessages:
			wait = chunkWaitHere
		case armFinal:
			// The last chunk's answer is awaited too, by the caller:
			// matter.js's sendDataReportMessage waits for the Success to
			// every chunk it sends, the last included — Millis(500) for a
			// closed subscription's report (packages/protocol/src/
			// interaction/InteractionMessenger.ts:799-844, :844 at
			// db021da0). Armed before the send, so the answer cannot land
			// before the waiter exists.
			wait = chunkWaitCaller
		}
		res, err := b.sendChunk(leg, i, body, chunk, wait)
		if !res.sent {
			return nil, nil, err
		}
		if res.counter != 0 {
			counters = append(counters, res.counter)
		}
		if err != nil {
			return counters, nil, err
		}
		finalWait = res.armed
	}
	return counters, finalWait, nil
}

// sendInitiatedReportArmed ships report on a **fresh bridge-initiated
// exchange** instead of reusing the commissioner's Subscribe exchange.
// Use this for ongoing subscribe reports (matter.js's
// ServerSubscription.#sendUpdateMessage pattern,
// packages/node/src/node/server/ServerSubscription.ts:893-900 at
// db021da0). Every
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
//
// With arm it also arms the wait for the peer's StatusResponse to the report's
// last chunk on the fresh exchange, just before that chunk goes out
// ([Bridge.sendReportChunksNotify] armFinal), so the answer cannot land
// before the waiter exists; the caller receives on wait through
// [Bridge.awaitFlushedReport], which disarms it. A failed send disarms
// it here.
func (b *Bridge) sendInitiatedReportArmed(target subTarget, report im.ReportData, arm bool) (counters []uint32, exchangeID uint16, wait <-chan statusAnswer, err error) {
	freshExchangeID := b.nextOutboundExchangeID()
	freshTarget := target
	freshTarget.exchangeID = freshExchangeID
	// Set peerInitiator=false so sendUnsolicitedIM's
	// `Initiator: !target.peerInitiator` evaluates to true — we are the
	// exchange initiator on this fresh exchange.
	freshTarget.peerInitiator = false
	// A chunk the peer leaves unanswered fails the send here, through the
	// wait for its StatusResponse, and the reporter counts the failed
	// update; it is no peer loss (noteReportUndelivered).
	counters, wait, err = b.sendReportChunksNotify(freshTarget, report, arm)
	if err != nil {
		if arm {
			b.disarmStatusResponseWait(target.sessionID, freshExchangeID, true)
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
func (b *Bridge) awaitFlushedReport(ctx context.Context, op string, sessionID, exchangeID uint16, wait <-chan statusAnswer) {
	timeout := flushStatusResponseTimeout
	timer := b.clk().NewTimer(timeout)
	defer timer.Stop()
	defer b.disarmStatusResponseWait(sessionID, exchangeID, true)
	select {
	case answer := <-wait:
		b.expediteDuplicateAck(sessionID, exchangeID, true)
		b.RunAckPumpOnce(b.clk().Now())
		b.logger.Debug("matter.tx."+op+".flush_answered",
			slog.Int("exchange_id", int(exchangeID)),
			slog.String("status", answer.status.String()),
			slog.Bool("undecodable", answer.undecodable))
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
	// Route to the address the peer most recently used on this session
	// with a new message counter, not the one frozen into the target at
	// Subscribe time: a controller whose source address rotates
	// mid-session keeps authenticating from the new one. matter.js adopts
	// the address of a non-duplicate initiator message that opens a new
	// exchange ("the new message wins", ExchangeManager.ts:480-486 at
	// db021da0) and opens each report's exchange for the subscription's
	// peer address (ServerSubscription.ts:900); this module moves the
	// route on every new counter, connectedhomeip's rule (the route
	// comment in receive.go; by_design BD-chip-SessionRouteByNewCounter).
	dest := b.currentPeerAddr(target.sessionID, target.src)
	if listener == nil && !isConnRoute(dest) {
		return 0, ErrUnsolicitedListenerMissing
	}
	// Over a reliable connection MRP is off: no R flag, no piggybacked
	// acknowledgement (matter.js MessageExchange.ts:665, :685-691).
	usesMRP := routeUsesMRP(dest)

	// sendUnsolicitedIM is the low-level primitive that ships an IM
	// datagram on whichever exchange the caller has prepared in `target`.
	// sendReportChunksNotify is the caller; both ongoing reporters
	// (attributes and events) and the flush reach it through
	// sendInitiatedReportArmed, which forges a fresh bridge-opened exchange
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
		// Not sent while an earlier message of the exchange waits for its
		// acknowledgement ([outboundReliableTracker.TrackOnExchange]; hs 4b).
		if err := tracker.TrackOnExchange(counter, target.sessionID, target.exchangeID, !target.peerInitiator, datagram, dest, b.clk().Now()); err != nil {
			return 0, err
		}
	}
	if err := b.sendRouted(listener, dest, target.sessionID, datagram); err != nil {
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
//  1. teardownForSubscribe — the KeepSubscriptions=false teardown, before
//     the request's refusals; registerSubscription — manager Subscribe.
//  2. primingMatches — reject a Subscribe that matches nothing.
//  3. streamPrimingChunks — the priming report, generated chunk by chunk
//     (newPrimingSource, primingEvents), with a per-chunk ack wait.
//  4. sendSubscribeResponse — SubscribeResponse with piggyback ack + TouchLastReport.
//
// abandonPrimingSubscription closes a subscription whose priming report
// or SubscribeResponse did not go out: the controller never saw it
// established, and a priming subscription would otherwise sit silent in
// the manager until its session closed. It never became active, so it is
// released, not terminated: no row was persisted for it and the fabric is
// not re-announced, as the re-establishment unwinds the same state
// (review II, R2-P6-sec-0509). A CASE peer that subscribes to nothing
// does not start one re-announcement per request.
func (b *Bridge) abandonPrimingSubscription(subID uint32) {
	if m := b.subscriptionManagerLocked(); m != nil && subID != 0 {
		_ = m.Release(subID)
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
	return b.subscribeWith(ctx, dispatcher, src, requestHdr, proto, req)
}

// subscribeWith is [Bridge.handleSubscribeRequest] against dispatcher.
func (b *Bridge) subscribeWith(
	ctx context.Context,
	dispatcher im.Dispatcher,
	src net.Addr,
	requestHdr *message.Header,
	proto message.ProtocolHeader,
	req im.SubscribeRequest,
) error {
	// The order is matter.js InteractionServer.handleSubscribeRequest's
	// at db021da0 (F-db021da0-14, review II R2-P2-sess-04): the path
	// count (:635-641), the KeepSubscriptions=false teardown (:647-665),
	// the request naming no path (:667-672), the illegal paths
	// (validateReadPaths, :699). A request refused for naming no path or
	// an illegal one has cost the peer its subscriptions, as there.
	if len(req.AttributeRequests)+len(req.EventRequests) > im.MaxReadPaths {
		return b.rejectSubscribeStatus(src, requestHdr, proto, "path", im.StatusPathsExhausted)
	}

	// The priming report waits for the peer's answer to each chunk; a
	// session already holding [maxChunkWaitsPerSession] such waits is
	// answered Busy before anything is registered or torn down (review
	// II, R2-P1-req2-01). matter.js has no such refusal; this module's
	// own bound refuses side-effect free, and the controller retries.
	release, claimed := b.claimChunkWait(requestHdr.SessionID, true)
	if !claimed {
		return b.rejectSubscribeStatus(src, requestHdr, proto, "chunk_waits", im.StatusBusy)
	}
	defer release()

	b.teardownForSubscribe(src, requestHdr, req)

	// A Subscribe naming no attribute and no event paths at all is
	// InvalidAction — matter.js InteractionServer.ts:667-672 at db021da0 ("No
	// attributes or events requested").
	if len(req.AttributeRequests) == 0 && len(req.EventRequests) == 0 {
		return b.rejectSubscribeInvalidAction(src, requestHdr, proto, "empty")
	}

	// Reject illegal paths with a top-level InvalidAction StatusResponse
	// (wildcard cluster + concrete non-global attribute, or wildcard
	// cluster + concrete event) before building any report. Mirrors
	// matter.js InteractionServer.ts validateReadPaths (#3926, Matter
	// §8.4.3.2; :138, called for a subscribe at :699 at db021da0), which
	// gates Read and Subscribe through the same check.
	if status := im.ValidateReadPaths(req.AttributeRequests, req.EventRequests); status != im.StatusSuccess {
		return b.rejectSubscribeStatus(src, requestHdr, proto, "path", status)
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
	// A path naming a node is answered for this node's id on the
	// session's fabric only (review II, R2-P2-alloc-07).
	subLocalNodeID := b.localNodeIDFor(requestHdr.SessionID, subFabricIndex)
	subCtx = im.WithLocalNodeID(subCtx, subLocalNodeID)
	subCtx = withReadAuthority(b.withSessionAuthMode(subCtx, requestHdr.SessionID), dispatcher)

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
	if errors.Is(regErr, subscription.ErrFabricQuotaExceeded) {
		// The fabric holds its quota. connectedhomeip's rule: the access
		// check first — a request whose paths the subject may read none of
		// is InvalidAction (src/app/InteractionModelEngine.cpp:861-868 at
		// 6170af84), as it is here with room (no_match below) — then the
		// eviction that makes room, so the request is admitted in the
		// place of the fabric's oldest subscription (:871-875,
		// :1341-1350). A subject that may read nothing takes nobody's
		// place (review II, R2-P3-admin-03, owner decision 53).
		if !b.primingMatches(subCtx, dispatcher, primingRequest(req, subLocalNodeID)) {
			return b.rejectSubscribeInvalidAction(src, requestHdr, proto, "no_match")
		}
		stamp = im.ReportData{}
		subID, regErr = b.registerSubscriptionAt(src, requestHdr, proto, req, &stamp, true)
	}
	if regErr != nil {
		// The manager refused the subscription (per-fabric quota, cadence
		// out of range, cadence inverted after clamp). The controller must
		// hear that as a StatusResponse: a SubscribeResponse would tell it
		// a subscription exists, and it would then wait forever for
		// reports the bridge never sends.
		return b.rejectSubscribeStatus(src, requestHdr, proto, "manager", subscribeRejectStatus(regErr))
	}
	// No path leaves the subscription priming: unless its
	// SubscribeResponse went out, it is closed on the way out — a panic
	// the transport recovers above this handler included; the returns
	// below close it before they answer, a second close is a no-op. A priming
	// subscription is skipped by the KeepSubscriptions=false teardown and
	// the session replacement, so one left behind would hold its place in
	// the fabric's quota until its session closed (review II,
	// R2-P1-dr-req-07).
	established := false
	defer func() {
		if !established {
			b.abandonPrimingSubscription(subID)
		}
	}()
	// One EventFilterIB applies to the priming report: the first for this
	// node (matter.js EventReadResponse.ts:68-77; review 2026-11
	// W2-wire-05).
	primingReq := primingRequest(req, subLocalNodeID)
	// A Subscribe whose (possibly wildcard) paths match zero attributes
	// AND zero events cannot be established — matter.js
	// ServerSubscription.ts:707-711 at db021da0 rejects it with InvalidAction, after
	// the admission above as there. The match is taken BEFORE
	// DataVersionFilter suppression, so a legitimately quiet re-subscribe
	// (all clusters cached) still establishes. It is decided before the
	// first chunk, by an expansion that stops at the first match.
	if !b.primingMatches(subCtx, dispatcher, primingReq) {
		b.abandonPrimingSubscription(subID)
		return b.rejectSubscribeInvalidAction(src, requestHdr, proto, "no_match")
	}
	// The priming report is generated chunk by chunk (review II, decision
	// 44): attribute reports in request-path order, then the events.
	attributes := b.newPrimingSource(subCtx, dispatcher, primingReq)
	var primed im.ReportData
	var events func() []im.EventReport
	if len(primingReq.EventRequests) > 0 {
		events = func() []im.EventReport {
			primed.EventReports = b.primingEvents(subCtx, dispatcher, primingReq, false)
			return primed.EventReports
		}
	}
	limits := b.chunkLimitsFor(src, requestHdr.SessionID)
	chunker := newReportChunker(stamp, limits.budget, limits.hardCap, attributes.next, events)
	// The priming report waits for the peer's StatusResponse per chunk,
	// which arrives on this session: hand its turn on. The wait ends with
	// the subscription too — a KeepSubscriptions=false request that
	// replaces it, its session's close.
	abort := b.primingAbort(subID)
	defer b.forgetPrimingAbort(subID)
	leaveSessionOrder(ctx)
	if err := b.streamPrimingChunks(ctx, abort, src, requestHdr, proto, subID, chunker); err != nil {
		b.abandonPrimingSubscription(subID)
		if errors.Is(err, errChunkAnswerUndecodable) {
			// The peer's answer carried no status: the interaction ends
			// with FAILURE on its own exchange (owner decision 176;
			// [errChunkAnswerUndecodable]).
			_ = b.rejectSubscribeStatus(src, requestHdr, proto, "priming_answer_undecodable", im.StatusFailure)
		}
		return err
	}
	b.logger.Debug("matter.tx.subscribe.initial_reports",
		slog.Int("subscription_id", int(subID)),
		slog.Int("initial_reports", attributes.reports))
	if err := b.sendSubscribeResponse(src, requestHdr, proto, req, subID, primed); err != nil {
		b.abandonPrimingSubscription(subID)
		return err
	}
	established = true
	// The subscription is active: record it for re-establishment after a
	// restart (matter.js SubscriptionsServer.#addSubscription, fired when
	// InteractionServer activates the subscription after its
	// SubscribeResponse).
	b.persistSubscription(ctx, subID)
	return nil
}

// primingRequest is req as its priming report reads it: one EventFilterIB
// applies, the first for this node (matter.js EventReadResponse.ts:68-77;
// review 2026-11 W2-wire-05).
func primingRequest(req im.SubscribeRequest, localNodeID uint64) im.SubscribeRequest {
	req.EventFilters = im.EventFiltersFor(req.EventFilters, localNodeID)
	return req
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
// notifiers, so the caller holds no bridge lock; it returns the wiring,
// with the unsubscribe closures of what it registered.
func (b *Bridge) wireMeasurementListeners(mgr *subscription.Manager, topology *endpointpkg.Topology) *listenerSet {
	set := &listenerSet{}
	b.wireRootNotifiers(set, mgr, topology)
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
	// The servers' own change notifiers last: each settles its changes
	// against the memory of the change notifications wired above
	// ([listenerSet.settlerFor]), which is complete by now.
	b.wireAttributeNotifiers(set, mgr, topology)
	if b.logger != nil {
		b.logger.Info("matter.bridge.measurement_listeners_wired",
			slog.Int("examined", examined),
			slog.Int("measurement_seen", measurementSeen),
			slog.Int("notifier_ok", notifierOK),
			slog.Int("with_paths", withPaths),
			slog.Int("registered", len(set.unsubs)))
	}
	// The throttles of quiet paths this wiring did not take up belong to
	// paths it no longer serves; the ones it took up are parked when it is
	// torn down, after every notifier that feeds them was detached.
	b.dropParkedQuietPaths()
	for _, qp := range set.quiet {
		set.unsubs = append(set.unsubs, func() { b.parkQuietPath(qp) })
	}
	return set
}

// listenerSet collects the unsubscribe closures of one wiring of the
// host's notifiers.
type listenerSet struct {
	unsubs []func()
	// quiet holds the throttle of each quieter path of the wiring
	// ([listenerSet.quietPathFor]).
	quiet map[im.ConcreteAttributePath]*quietPath
	// values holds the last values of each endpoint's paths of the
	// wiring ([listenerSet.pathValuesFor]).
	values map[uint16]*pathMemory
	// reclaimed holds, per endpoint, the clusters whose DataVersion the
	// wiring advanced for a quieter path it took up changed
	// ([listenerSet.quietPathFor]): the changes of the gap are one
	// commit per cluster, as a notification's are.
	reclaimed map[uint16]map[uint32]struct{}
}

// pathValuesFor returns a pathValues of paths over the set's memory of
// ep's paths, made on first use, so the notifiers of one wiring that cover
// a path share its last value.
func (l *listenerSet) pathValuesFor(ep *endpointpkg.Endpoint, paths []im.ConcreteAttributePath) *pathValues {
	mem, ok := l.values[ep.ID]
	if !ok {
		mem = &pathMemory{ep: ep, last: make(map[im.ConcreteAttributePath]attributeReading, len(paths))}
		if l.values == nil {
			l.values = make(map[uint16]*pathMemory)
		}
		l.values[ep.ID] = mem
	}
	return newPathValuesIn(mem, ep, paths)
}

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
// is detached again instead of installed, so the last one wins. It
// returns the generation it wired under.
func (b *Bridge) rewireMeasurementListeners() (generation uint64) {
	b.mu.Lock()
	stale := b.measurementUnsubscribers
	b.measurementUnsubscribers = nil
	b.listenerWiring = nil
	b.listenerGeneration++
	generation = b.listenerGeneration
	mgr, topology := b.subManager, b.topology
	b.listenerManager = mgr
	b.mu.Unlock()
	runUnsubscribers(stale)
	if mgr == nil || topology == nil {
		return generation
	}
	set := b.wireMeasurementListeners(mgr, topology)
	unsubs := set.unsubs
	b.mu.Lock()
	overtaken := b.listenerGeneration != generation
	if !overtaken {
		b.measurementUnsubscribers = unsubs
		b.listenerWiring = set
	}
	b.mu.Unlock()
	if overtaken {
		runUnsubscribers(unsubs)
	}
	return generation
}

// detachListenersOfGeneration detaches the value listeners while they are
// still the ones [Bridge.rewireMeasurementListeners] installed under
// generation: a later wiring or detach moved the generation on and owns
// what is installed now.
func (b *Bridge) detachListenersOfGeneration(generation uint64) {
	b.mu.Lock()
	if b.listenerGeneration != generation {
		b.mu.Unlock()
		return
	}
	unsubs := b.measurementUnsubscribers
	b.measurementUnsubscribers = nil
	b.listenerWiring = nil
	b.listenerGeneration++
	b.mu.Unlock()
	runUnsubscribers(unsubs)
}

// detachHostListeners unregisters the bridge from every host notifier —
// the value listeners and the switch hooks — for [Bridge.Stop]: a
// stopped bridge is not called into (review II, W2-conc-10). The next
// Start reassembles and wires them again. The unsubscribers run with no
// bridge lock held.
func (b *Bridge) detachHostListeners() {
	b.mu.Lock()
	measurements := b.measurementUnsubscribers
	switches := b.switchUnsubscribers
	b.measurementUnsubscribers = nil
	b.listenerWiring = nil
	b.switchUnsubscribers = nil
	b.listenerGeneration++ // a wiring still registering detaches itself
	b.mu.Unlock()
	runUnsubscribers(measurements)
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
// attributes on endpoint 0: a fire advances the cluster's DataVersion
// through [endpointpkg.Endpoint.BumpDataVersionOf] — the endpoint-held one
// of a server that keeps none of its own; a server with a tracker of its
// own advances that itself, so nothing is advanced twice — and then marks
// the attributes dirty so subscribers get a report. A fire while a command
// runs against the cluster does neither: the command advances the version
// once when it ends (invokeCommitTable); a fire while a controller's write
// is applied to the root is held and settled when the write ends
// (writeHoldTable). Mirrors the reactive state
// matter.js keeps for root behaviors. The caller holds no bridge lock.
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
		clusterID := srv.MatterClusterID()
		attrs := srv.MatterReportable()
		set.add(notifier.OnMatterValueChanged(func() {
			// A command running against the cluster advances the
			// version once when it ends (invokeCommitTable).
			if b.invokeCommits.hold(root.ID, clusterID, attrs) {
				return
			}
			// A controller's write being applied to the root settles
			// them when it ends (writeHoldTable): one commit, one
			// advance (review II, R2-P5-write-03).
			if len(paths) > 0 && len(b.writeHolds.holdPaths(root, mgr, paths)) == 0 {
				return
			}
			// The version first, so the report carries the post-change
			// one: the endpoint-held tracker of a server that keeps none
			// of its own (review II, decision 46).
			root.BumpDataVersionOf(clusterID)
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
// A root or aggregator server with a tracker of its own
// (contract.ClusterDataVersion) is a persistent instance whose tracker is
// the DataVersion the dispatcher reports (endpoint/dispatcher.go
// clusterDataVersionFor), so it bumps that itself before it fires and
// nothing is bumped here; one without has the endpoint-held version
// advanced as a bridged endpoint's is ([endpointpkg.Endpoint.BumpDataVersionOf]).
// matter.js reports a state change of every
// behavior on every endpoint the same way, the root's included — the OTA
// requestor's UpdateState is plain behavior state
// (OtaSoftwareUpdateRequestorServer.ts #updateState :1164-1204). A root
// server that is also a [contract.ChangeNotifier] is wired by
// wireRootNotifiers already and is skipped here. A server that is a
// [contract.AttributeReportNotifier] has its attributes marked dirty
// through its report view — a quieter attribute by its throttle, as
// matter.js's ServerBehaviorBacking subscribes a QuietEvent's `quiet`
// observable (ServerBehaviorBacking.ts:253-261 at db021da0) — and the version
// advanced through OnAttributeChange, which tells of every change at once
// ([quieterVersion]); any other server is subscribed through
// OnAttributeChange alone. It takes no lock of b's; the rewire calls it
// without b.mu (review II, R2-P6-doc-0515).
func (b *Bridge) wireAttributeNotifiers(set *listenerSet, mgr *subscription.Manager, topology *endpointpkg.Topology) {
	for _, ep := range topology.Endpoints {
		if ep == nil {
			continue
		}
		for _, srv := range endpointpkg.ClusterServers(ep) {
			notifier, ok := srv.(contract.AttributeChangeNotifier)
			if !ok {
				continue
			}
			if _, wired := srv.(contract.ChangeNotifier); wired && ep.IsRoot() {
				continue
			}
			clusterID := srv.MatterClusterID()
			r, reports := srv.(contract.AttributeReportNotifier)
			if !reports {
				set.add(notifier.OnAttributeChange(func(change contract.Change) {
					b.reportServerChange(mgr, ep, clusterID, change, true)
				}))
				continue
			}
			// The change listener first: a change told to both views at
			// once advances the version before its report marks it dirty.
			// The report advances nothing ([Bridge.quieterChanged]).
			settle := set.settlerFor(ep, clusterID)
			set.add(notifier.OnAttributeChange(func(change contract.Change) {
				b.quieterChanged(ep, clusterID, change, settle)
			}))
			set.add(r.OnAttributeReport(func(change contract.Change) {
				b.reportServerChange(mgr, ep, clusterID, change, false)
			}))
		}
	}
}

// reportServerChange marks the attributes of a server's change dirty,
// advancing the cluster's DataVersion before when advance holds, and tells
// the host's hook. A command running against the cluster advances the
// version once when it ends (invokeCommitTable): its servers' changes are
// held there. A controller's write is one commit with one advance, the
// dispatcher's (endpoint/dispatcher.go WriteAuthorized), so the change the
// write itself reports does not advance it again (matter.js
// Datasource.ts:1446-1457 at db021da0, advanceVersion once per commit),
// and a change with another origin made while a write to the endpoint is
// applied is held and settled against the write's record when it ends
// ([Bridge.holdWritten]).
func (b *Bridge) reportServerChange(mgr *subscription.Manager, ep *endpointpkg.Endpoint, clusterID uint32, change contract.Change, advance bool) {
	attrIDs := change.Attributes
	if len(attrIDs) == 0 {
		return
	}
	// A report view's mark waits for a write applied meanwhile, the
	// write's own change included: the write's report marks it after the
	// dispatcher's advance (Fable read 5b-write R3).
	if change.Origin != contract.OriginControllerWrite && (b.invokeCommits.hold(ep.ID, clusterID, attrIDs) || b.holdWritten(mgr, ep, clusterID, attrIDs, !advance)) ||
		change.Origin == contract.OriginControllerWrite && !advance && b.holdWritten(mgr, ep, clusterID, attrIDs, true) {
		if hook := b.onAttributeChange.Load(); hook != nil {
			(*hook)(ep.ID, clusterID, change)
		}
		return
	}
	if advance && change.Origin != contract.OriginControllerWrite {
		ep.BumpDataVersionOf(clusterID)
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
}

// holdWritten holds a change of attrs of cluster on ep while a
// controller's write to ep is applied ([writeHoldTable]): the write's end
// settles it against the write's record, so a change the host made from
// inside the write — reported with its own origin — is not advanced and
// reported beside the write's commit (review II, R2-P5-write-03).
//
// markOnly is a server's report view, which advances nothing: its mark
// waits for the write's end too, so a report built meanwhile cannot carry
// the change with the version from before the write's commit — a
// generated server on the root advances its version only at the write's
// own store, after its Sink's Set reported the sibling (review II, Fable
// read 5b-write R3). The settle marks such a path and advances nothing
// for it.
func (b *Bridge) holdWritten(mgr *subscription.Manager, ep *endpointpkg.Endpoint, cluster uint32, attrs []uint32, markOnly bool) bool {
	if markOnly {
		return b.writeHolds.holdMarks(ep, mgr, cluster, attrs)
	}
	paths := make([]im.ConcreteAttributePath, len(attrs))
	for i, a := range attrs {
		paths[i] = attributeOf(ep.ID, cluster, a)
	}
	return len(b.writeHolds.holdPaths(ep, mgr, paths)) == 0
}

// quieterChanged advances the DataVersion of a cluster whose server
// reports a quieter attribute later than it changes it
// ([contract.AttributeReportNotifier]) when the change happens, not when
// its report goes out: one advance per change, for a quieter and a plain
// attribute changed together too, and none at the throttle's report.
// matter.js advances the version at the commit that changed a value, a
// quieter property's included
// (packages/node/src/behavior/state/managed/Datasource.ts:1456-1457 at
// db021da0), and at a quiet emit only for a dynamic property
// (packages/node/src/behavior/internal/ServerBehaviorBacking.ts:258-261
// calls advanceVersionFor, Datasource.ts:679-688) — of the reporting
// servers here, LevelControl's and ColorControl's remainingTime
// (LevelControlServer.ts:806-822, ColorControlServer.ts:1967-1984), which
// those servers report with their change. A change made while a command
// runs against the cluster is the command's commit's: the commit advances
// the version when it ends, whether or not the change's report went out
// inside it, and the report waits for the throttle (review II,
// R2-P4-fr-im-02). A controller's write advanced it already.
//
// A change a notification of the endpoint's source, or of a server that
// forwards its port's, saw already advanced the version there; settle
// ([listenerSet.settlerFor]) tells it, and remembers a change it did not
// see, so that notification does not advance or report it again — one
// change of a value, one advance (Datasource.ts:1456-1457 at db021da0).
func (b *Bridge) quieterChanged(ep *endpointpkg.Endpoint, clusterID uint32, change contract.Change, settle func([]uint32) bool) {
	if len(change.Attributes) == 0 || change.Origin == contract.OriginControllerWrite {
		return
	}
	if b.invokeCommits.advanceAtEnd(ep.ID, clusterID) {
		return
	}
	if settle != nil && !settle(change.Attributes) {
		return
	}
	// A controller's write being applied to the endpoint settles the
	// advance when it ends (writeHoldTable); the report waits for the
	// throttle as before.
	if b.writeHolds.holdAdvance(ep, clusterID, change.Attributes) {
		return
	}
	ep.BumpDataVersionOf(clusterID)
}

// settlerFor returns how a server's own change of attributes of cluster
// on ep is settled against the memory of the change notifications the
// wiring covers its paths with ([listenerSet.quietPathFor],
// [listenerSet.pathValuesFor]): the memory takes the current value of
// each named path it holds, and the answer is whether the change is new
// to it — a path it holds moved since its last reading, or a path it does
// not hold. A false answer means a notification read every named value
// already and advanced the version for it. nil when the wiring covers no
// path of the cluster. Called after the notifications are wired: the
// memory it reads is complete and no longer grows.
func (l *listenerSet) settlerFor(ep *endpointpkg.Endpoint, cluster uint32) func([]uint32) bool {
	mem := l.values[ep.ID]
	quiet := map[uint32]*quietPath{}
	for p, qp := range l.quiet {
		if p.Endpoint == ep.ID && p.Cluster == cluster {
			quiet[p.Attribute] = qp
		}
	}
	covered := len(quiet) > 0
	if mem != nil && !covered {
		mem.mu.Lock()
		for p := range mem.last {
			if p.Cluster == cluster {
				covered = true
				break
			}
		}
		mem.mu.Unlock()
	}
	if !covered {
		return nil
	}
	return func(attrs []uint32) bool {
		moved := false
		for _, attr := range attrs {
			if qp, ok := quiet[attr]; ok {
				moved = qp.settle() || moved
				continue
			}
			if mem == nil || mem.settle(im.ConcreteAttributePath{
				Endpoint: ep.ID, Cluster: cluster, Attribute: attr,
				HasEndpoint: true, HasCluster: true, HasAttribute: true,
			}) {
				moved = true
			}
		}
		return moved
	}
}

// isOpen reports whether a command's commit of (endpoint, cluster) is open.
func (t *invokeCommitTable) isOpen(endpoint uint16, cluster uint32) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, ok := t.open[invokedCluster{endpoint, cluster}]
	return ok
}

// advanceAtEnd records in the open commit of (endpoint, cluster) that it
// advances the version when it ends, marking nothing dirty for it, and
// reports true; false when no commit is open.
func (t *invokeCommitTable) advanceAtEnd(endpoint uint16, cluster uint32) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	c, ok := t.open[invokedCluster{endpoint, cluster}]
	if ok {
		c.advance = true
	}
	return ok
}

// wireMeasurementNotifier subscribes notifier to pathSet: on each fire it
// advances the touched clusters' endpoint-hosted DataVersion BEFORE dirty-
// marking (so the shipped report carries the post-change version and a
// controller's DataVersionFilter misses on the next read — mirrors matter.js
// Datasource.ts:1456-1457 at db021da0, where a commit advances the version in
// its first phase, ahead of the change notifications of postCommit,
// :1286-1326), then marks every path dirty on the Subscribe manager. The
// unsubscribe is tracked so a reassemble tears every listener down.
//
// A path whose attribute has the quieter quality ("Q" in its definition)
// is reported through its throttle instead ([quietPath]): its change
// advances the cluster's version at once, with the direct paths' changes
// of the same notification as one commit, and only the dirty-marking waits
// for the throttle. matter.js advances the version at the commit that
// changed a value, a quieter property's included
// (packages/node/src/behavior/state/managed/Datasource.ts:1446-1457 at
// db021da0, #noteIfChanged :1476-1493), and broadcasts a quieter property
// only when its QuietEvent emits
// (packages/node/src/behavior/internal/ServerBehaviorBacking.ts:251-262),
// whose advanceVersionFor adds an advance for a dynamic property alone
// (Datasource.ts:679-688). Review II, R2-P4-nv-05b, R2-P4-dr-sub-03.
func (b *Bridge) wireMeasurementNotifier(set *listenerSet, mgr *subscription.Manager, ep *endpointpkg.Endpoint, notifier contract.ChangeNotifier, pathSet []im.ConcreteAttributePath) {
	epID := ep.ID
	boolState := newBooleanStateEvents(b, ep, pathSet)
	occupancy := newOccupancyEvents(b, ep, pathSet)
	energy := newElectricalEnergyEvents(b, ep, notifier)
	var quiet []*quietPath
	direct := make([]im.ConcreteAttributePath, 0, len(pathSet))
	for _, p := range pathSet {
		if quieterAttribute(p.Cluster, p.Attribute) {
			quiet = append(quiet, set.quietPathFor(b, mgr, ep, p))
			continue
		}
		direct = append(direct, p)
	}
	values := set.pathValuesFor(ep, direct)
	set.add(notifier.OnMatterValueChanged(func() {
		defer energy.check()
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
		// A command running against a cluster advances its version
		// once when it ends (invokeCommitTable): its paths are held.
		changed = slices.DeleteFunc(changed, func(p im.ConcreteAttributePath) bool {
			return b.invokeCommits.hold(epID, p.Cluster, []uint32{p.Attribute})
		})
		// A controller's write being applied to the endpoint settles
		// them when it ends (writeHoldTable): the memory holds the new
		// values already.
		changed = b.writeHolds.holdPaths(ep, mgr, changed)
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
		for _, qp := range quiet {
			// A command running against the cluster advances its version
			// once when it ends; the report still waits for the throttle,
			// so the commit owes the advance alone (advanceAtEnd), not a
			// mark that would pass the throttle (review II, N4-x3-1).
			if b.invokeCommits.isOpen(epID, qp.path.Cluster) {
				inCommit := map[uint32]struct{}{qp.path.Cluster: {}}
				if qp.check(inCommit) && !b.invokeCommits.advanceAtEnd(epID, qp.path.Cluster) {
					// The commit ended meanwhile: the change is its own.
					if _, done := bumped[qp.path.Cluster]; !done {
						ep.BumpClusterDataVersion(qp.path.Cluster)
						bumped[qp.path.Cluster] = struct{}{}
					}
				}
				continue
			}
			if !b.writeHolds.holdQuiet(epID, qp) {
				qp.check(bumped)
			}
		}
	}))
}

// quieterAttribute reports whether the attribute's definition gives it the
// quieter quality ("Q", matter.js effectiveQuality.quieter); a cluster no
// definition is registered for has none.
func quieterAttribute(clusterID, attrID uint32) bool {
	def := spec.Lookup(clusterID)
	if def == nil {
		return false
	}
	a := def.Attribute(attrID)
	return a != nil && a.Quality.Quieter
}

// powerSourceBatteryQuietInterval is the throttle interval matter.js gives
// PowerSource's BatPercentRemaining, BatTimeRemaining and
// BatTimeToFullCharge: "According to specs changes to these attributes
// should not occur more often than every 10 seconds"
// (packages/node/src/behaviors/power-source/PowerSourceServer.ts:25-34 at
// db021da0, minimumEmitInterval = Seconds(10)).
const powerSourceBatteryQuietInterval = 10 * time.Second

// quietIntervalOf is the throttle interval of a quieter attribute:
// [powerSourceBatteryQuietInterval] for PowerSource's three battery
// estimates, [clusterpkg.QuietMinimumInterval] for every other.
func quietIntervalOf(clusterID, attrID uint32) time.Duration {
	if clusterID == psdef.ClusterID {
		switch attrID {
		case psdef.AttrBatPercentRemaining, psdef.AttrBatTimeRemaining, psdef.AttrBatTimeToFullCharge:
			return powerSourceBatteryQuietInterval
		}
	}
	return clusterpkg.QuietMinimumInterval
}

// quietPath reports one measurement-projected path of quality Q through a
// [clusterpkg.Quieter]: a change of its value advances the cluster's
// DataVersion and goes to the throttle, and the throttle's report marks the
// path dirty. One quietPath serves the path for every notifier that covers it
// in one wiring ([listenerSet.quietPathFor]) — an endpoint's source and the
// cluster server that forwards its notifier both do — so a change is
// told to the throttle once.
//
// It outlives a wiring that serves the path again: the wiring's teardown
// parks it ([Bridge.parkQuietPath]) and the next wiring takes it up with
// its last value, its throttle's last report and a report the throttle
// holds back, so a re-wiring neither drops a held change nor restarts the
// interval. A re-wiring keeps the endpoint's cluster version, and the
// throttle is kept with it: matter.js's QuietEvent observer lives as long
// as its behavior's backing — registered in the backing's constructor and
// closed only with it
// (packages/node/src/behavior/internal/ServerBehaviorBacking.ts:35-38,
// :214-216, #configureEventSuppression :224-262 at db021da0). Parked, it
// reads nothing and reports nothing: a notification that still reaches it
// is left to the next wiring, which compares the value with the last one
// when it takes the path up ([listenerSet.quietPathFor]), and a report
// its throttle sends is owed to that wiring (review II, R2-P4-dr-sub-01,
// R2-P4-dr-sub-05, R2-P4-rr-im-04).
type quietPath struct {
	path im.ConcreteAttributePath
	q    *clusterpkg.Quieter
	// mu guards ep, last and had and orders the readings and the
	// throttle's calls.
	mu   sync.Mutex
	ep   *endpointpkg.Endpoint // nil while parked
	last attributeReading
	had  bool // last holds a reading
	// sinkMu guards mgr and owed; taken under mu, or alone by the
	// throttle's report.
	sinkMu sync.Mutex
	mgr    *subscription.Manager // nil while parked
	owed   bool                  // the throttle reported while parked
}

// quietPathFor returns the set's quietPath of p: the one a torn-down
// wiring parked for p, taken up and compared once with its last value — a
// change no live wiring saw advances the version and goes to the throttle,
// as a notification's would ([quietPath.check]) —, or a new one, reading
// its first value.
func (l *listenerSet) quietPathFor(b *Bridge, mgr *subscription.Manager, ep *endpointpkg.Endpoint, p im.ConcreteAttributePath) *quietPath {
	if qp, ok := l.quiet[p]; ok {
		return qp
	}
	qp := b.reclaimQuietPath(p)
	fresh := qp == nil
	if fresh {
		qp = &quietPath{path: p}
		qp.q = &clusterpkg.Quieter{
			Clock:    b.clk(),
			Interval: quietIntervalOf(p.Cluster, p.Attribute),
			Report:   qp.reported,
		}
	}
	qp.attach(ep, mgr, fresh)
	if !fresh {
		// One advance per cluster for the changes of the gap, however
		// many of its quieter paths moved (review II, R2-P4-fr-im-04).
		bumped, ok := l.reclaimed[ep.ID]
		if !ok {
			bumped = make(map[uint32]struct{}, 1)
			if l.reclaimed == nil {
				l.reclaimed = make(map[uint16]map[uint32]struct{})
			}
			l.reclaimed[ep.ID] = bumped
		}
		qp.check(bumped)
	}
	if l.quiet == nil {
		l.quiet = make(map[im.ConcreteAttributePath]*quietPath)
	}
	l.quiet[p] = qp
	return qp
}

// attach binds qp to a wiring's endpoint and manager, reading its first
// value when fresh, and marks the path dirty when its throttle reported
// while qp was parked.
func (qp *quietPath) attach(ep *endpointpkg.Endpoint, mgr *subscription.Manager, fresh bool) {
	qp.mu.Lock()
	qp.ep = ep
	if fresh {
		qp.last, qp.had = qp.read()
	}
	qp.sinkMu.Lock()
	qp.mgr = mgr
	owed := qp.owed
	qp.owed = false
	qp.sinkMu.Unlock()
	qp.mu.Unlock()
	if owed {
		mgr.OnAttributeChanged(qp.path)
	}
}

// park unbinds qp from its wiring; its throttle runs on.
func (qp *quietPath) park() {
	qp.mu.Lock()
	qp.ep = nil
	qp.sinkMu.Lock()
	qp.mgr = nil
	qp.sinkMu.Unlock()
	qp.mu.Unlock()
}

// reported is the throttle's report: the path marked dirty on the wiring's
// manager, or owed to the next wiring while qp is parked.
func (qp *quietPath) reported() {
	qp.sinkMu.Lock()
	mgr := qp.mgr
	if mgr == nil {
		qp.owed = true
	}
	qp.sinkMu.Unlock()
	if mgr != nil {
		mgr.OnAttributeChanged(qp.path)
	}
}

// parkQuietPath parks qp for the next wiring to take up
// ([listenerSet.quietPathFor]). A path parked already by a wiring that was
// overtaken while it registered gives its place to qp, and its throttle is
// stopped.
func (b *Bridge) parkQuietPath(qp *quietPath) {
	qp.park()
	b.quietMu.Lock()
	prev := b.quietParked[qp.path]
	if b.quietParked == nil {
		b.quietParked = make(map[im.ConcreteAttributePath]*quietPath)
	}
	b.quietParked[qp.path] = qp
	b.quietMu.Unlock()
	if prev != nil && prev != qp {
		prev.q.Stop()
	}
}

// reclaimQuietPath takes the quietPath parked for p, nil when none is.
func (b *Bridge) reclaimQuietPath(p im.ConcreteAttributePath) *quietPath {
	b.quietMu.Lock()
	defer b.quietMu.Unlock()
	qp := b.quietParked[p]
	delete(b.quietParked, p)
	return qp
}

// dropParkedQuietPaths stops the throttle of every parked quietPath and
// forgets it: a wiring that did not take one up serves its path no more,
// and a change the throttle held back is dropped with the path.
func (b *Bridge) dropParkedQuietPaths() {
	b.quietMu.Lock()
	parked := b.quietParked
	b.quietParked = nil
	b.quietMu.Unlock()
	for _, qp := range parked {
		qp.q.Stop()
	}
}

// read is the path's current value; false when no server answers it.
func (qp *quietPath) read() (attributeReading, bool) {
	for _, srv := range endpointpkg.ClusterServers(qp.ep) {
		if srv == nil || srv.MatterClusterID() != qp.path.Cluster {
			continue
		}
		v, ok := srv.MatterRead(qp.path.Attribute)
		if !ok {
			return attributeReading{}, false
		}
		return readingOf(im.AttributeValue{Value: v}), true
	}
	return attributeReading{}, false
}

// check reads the value of a bound quietPath and, on a change, advances the cluster's
// DataVersion — unless bumped holds the cluster, advanced by this
// notification already (or owed by a command's commit), which check adds
// it to — and tells the throttle, with the nullness of the old and the new
// value; a value no server answers counts as null. It reports whether the
// value changed.
func (qp *quietPath) check(bumped map[uint32]struct{}) bool {
	qp.mu.Lock()
	defer qp.mu.Unlock()
	if qp.ep == nil {
		return false // parked: the next wiring sees the change
	}
	cur, ok := qp.read()
	wasNull := !qp.had || qp.last.raw == nil
	isNull := !ok || cur.raw == nil
	if (wasNull && isNull) || (ok && qp.had && qp.last.equal(cur)) {
		return false
	}
	qp.last, qp.had = cur, ok
	if _, done := bumped[qp.path.Cluster]; !done {
		qp.ep.BumpClusterDataVersion(qp.path.Cluster)
		bumped[qp.path.Cluster] = struct{}{}
	}
	qp.q.Changed(wasNull, isNull)
	return true
}

// settle reads the value of a bound quietPath into its memory without
// telling the throttle, and reports whether it moved since the last
// reading: the server reports the change itself ([Bridge.quieterChanged]).
// A parked path counts as moved.
func (qp *quietPath) settle() bool {
	qp.mu.Lock()
	defer qp.mu.Unlock()
	if qp.ep == nil {
		return true
	}
	cur, ok := qp.read()
	wasNull := !qp.had || qp.last.raw == nil
	isNull := !ok || cur.raw == nil
	if (wasNull && isNull) || (ok && qp.had && qp.last.equal(cur)) {
		return false
	}
	qp.last, qp.had = cur, ok
	return true
}

// refresh reads the value of a bound quietPath again without telling the
// throttle: a controller's write changed it and was reported
// ([Bridge.settleWrite]).
func (qp *quietPath) refresh() {
	qp.mu.Lock()
	defer qp.mu.Unlock()
	if qp.ep == nil {
		return // parked: the next wiring compares with the value
	}
	qp.last, qp.had = qp.read()
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
	// mu guards last and orders the readings (check, changed).
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

// read is the reading the source took: an unobserved source, which reads
// false on the wire, has taken none and is neither emitted nor remembered
// (measurement.BooleanStateServer.ObservedStateValue). A BooleanState
// server of another type is read as it answers on the wire.
func (e *booleanStateEvents) read() (value, ok bool) {
	for _, srv := range endpointpkg.ClusterServers(e.ep) {
		if s, projected := srv.(*measurement.BooleanStateServer); projected {
			return s.ObservedStateValue()
		}
		if srv.MatterClusterID() == measurement.ClusterBooleanState {
			v, ok := srv.MatterRead(0x0000)
			bv, isBool := v.(bool)
			return bv, ok && isBool
		}
	}
	return false, false
}

// check reads, compares, remembers and emits under e.mu: two
// notifications that overlap store the latest reading last and emit in
// the order they read (review II, R2-P1-req2-10).
func (e *booleanStateEvents) check() {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	v, ok := e.read()
	if !ok {
		return
	}
	changed := e.last == nil || *e.last != v
	e.last = &v
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
	// mu guards last and orders the readings (check, changed).
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

// read is the reading the source took: an unobserved source, which reads 0
// on the wire, has taken none and is neither emitted nor remembered.
func (e *occupancyEvents) read() (uint8, bool) {
	srv := projectedOccupancy(e.ep)
	if srv == nil {
		return 0, false
	}
	return srv.ObservedOccupancy()
}

// check is [booleanStateEvents.check] for Occupancy: read, compare,
// remember and emit under e.mu (review II, R2-P1-req2-10).
func (e *occupancyEvents) check() {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	v, ok := e.read()
	if !ok {
		return
	}
	changed := e.last == nil || *e.last != v
	e.last = &v
	if changed {
		e.b.MatterEmitEvent(e.ep.ID, measurement.ClusterOccupancySensing, measurement.OccupancyEventChanged,
			measurement.OccupancyChangedEvent{Occupancy: occdef.OccupancyBitmap(v)}, contract.EventPriorityInfo)
	}
}

// electricalEnergyEvents emits ElectricalEnergyMeasurement's
// CumulativeEnergyMeasured when the CumulativeEnergyImported of an
// endpoint's measurement-projected server changes to a value: matter.js
// emits the event with each measurement its server's setMeasurement sets
// (packages/node/src/behaviors/electrical-energy-measurement/
// ElectricalEnergyMeasurementServer.ts:44-62 at db021da0), connectedhomeip
// when a snapshot changes the attribute to a value that is not null
// (src/app/clusters/electrical-energy-measurement-server/
// ElectricalEnergyMeasurementCluster.cpp:442-469 at 6170af84). The
// projected server is rebuilt on every dispatch and keeps no reading, so
// the change is observed here, where the host's notification arrives, as
// for OccupancyChanged (review II, R2-P4-nv-05a).
type electricalEnergyEvents struct {
	b  *Bridge
	ep *endpointpkg.Endpoint
	// mu guards last and orders the readings (check, changed).
	mu   sync.Mutex
	last *int64
}

// newElectricalEnergyEvents returns nil unless notifier is the endpoint's
// measurement-projected ElectricalEnergyMeasurement server and it lists
// CumulativeEnergyMeasured. The server is wired as a notifier of its own
// once per endpoint ([Bridge.wireMeasurementNotifier]'s callers), while its
// paths may be in the path set of the endpoint's source as well: keyed to
// the server, one change emits one event.
func newElectricalEnergyEvents(b *Bridge, ep *endpointpkg.Endpoint, notifier contract.ChangeNotifier) *electricalEnergyEvents {
	srv, ok := notifier.(*measurement.ElectricalEnergyServer)
	if !ok || !slices.Contains(srv.MatterEvents(), measurement.ElectricalEnergyEventCumulativeMeasured) {
		return nil
	}
	e := &electricalEnergyEvents{b: b, ep: ep}
	if v, ok := e.read(); ok {
		e.last = &v
	}
	return e
}

// read is the endpoint's current CumulativeEnergyImported in mWh; false
// while the source has no reading (the attribute is null).
func (e *electricalEnergyEvents) read() (int64, bool) {
	for _, srv := range endpointpkg.ClusterServers(e.ep) {
		s, ok := srv.(*measurement.ElectricalEnergyServer)
		if !ok {
			continue
		}
		v, _ := s.MatterRead(eemdef.AttrCumulativeEnergyImported)
		m, isStruct := v.(measurement.EnergyMeasurementStruct)
		return m.Energy, isStruct
	}
	return 0, false
}

// check is [booleanStateEvents.check] for CumulativeEnergyImported: read,
// compare, remember and emit under e.mu. A reading that becomes null emits
// nothing and is not remembered, as connectedhomeip skips a null snapshot.
func (e *electricalEnergyEvents) check() {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	v, ok := e.read()
	if !ok {
		return
	}
	changed := e.last == nil || *e.last != v
	e.last = &v
	if changed {
		e.b.MatterEmitEvent(e.ep.ID, measurement.ClusterElectricalEnergy, measurement.ElectricalEnergyEventCumulativeMeasured,
			measurement.CumulativeEnergyMeasuredEvent{EnergyImported: &measurement.EnergyMeasurementStruct{Energy: v}}, contract.EventPriorityInfo)
	}
}

// pathValues remembers the last value of each path a change notifier
// covers, so a notification reports only the paths that moved.
type pathValues struct {
	ep    *endpointpkg.Endpoint
	paths []im.ConcreteAttributePath
	mem   *pathMemory
}

// pathMemory is the last value of each path of one endpoint, shared by
// the [pathValues] of every notifier that covers the path in one wiring
// ([listenerSet.pathValuesFor]): an endpoint's measurement source and a
// cluster server that forwards its notifier are told of one change twice,
// and the first to read it reports it — one change, one DataVersion
// advance, as one matter.js commit advances a cluster's version once
// (packages/node/src/behavior/state/managed/Datasource.ts:1446-1457 at
// db021da0).
type pathMemory struct {
	// ep is the endpoint whose paths the memory holds.
	ep *endpointpkg.Endpoint
	// mu guards last and orders the readings (check, changed, refresh).
	mu   sync.Mutex
	last map[im.ConcreteAttributePath]attributeReading
}

func newPathValues(ep *endpointpkg.Endpoint, paths []im.ConcreteAttributePath) *pathValues {
	return newPathValuesIn(&pathMemory{ep: ep, last: make(map[im.ConcreteAttributePath]attributeReading, len(paths))}, ep, paths)
}

// refresh reads p again into the memory when the memory holds a value of
// it: a controller's write changed it and was reported
// ([Bridge.settleWrite]), so the next notification compares with the
// written value.
func (m *pathMemory) refresh(p im.ConcreteAttributePath) {
	key := im.ConcreteAttributePath{
		Endpoint: p.Endpoint, Cluster: p.Cluster, Attribute: p.Attribute,
		HasEndpoint: true, HasCluster: true, HasAttribute: true,
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, held := m.last[key]; !held || m.ep == nil {
		return
	}
	now := (&pathValues{ep: m.ep, paths: []im.ConcreteAttributePath{key}}).read()
	if v, ok := now[key]; ok {
		m.last[key] = v
	}
}

// settle reads p into the memory and reports whether it moved since the
// last reading; a path the memory holds no value of counts as moved and
// stays unheld, so the notifications that cover it compare as before.
func (m *pathMemory) settle(p im.ConcreteAttributePath) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	prev, held := m.last[p]
	if !held || m.ep == nil {
		return true
	}
	now := (&pathValues{ep: m.ep, paths: []im.ConcreteAttributePath{p}}).read()
	cur, ok := now[p]
	if ok && prev.equal(cur) {
		return false
	}
	if ok {
		m.last[p] = cur
	}
	return true
}

// newPathValuesIn is a pathValues over mem: it remembers the current
// value of each of its paths mem holds none for.
func newPathValuesIn(mem *pathMemory, ep *endpointpkg.Endpoint, paths []im.ConcreteAttributePath) *pathValues {
	v := &pathValues{ep: ep, paths: paths, mem: mem}
	mem.mu.Lock()
	defer mem.mu.Unlock()
	for p, val := range v.read() {
		if _, had := mem.last[p]; !had {
			mem.last[p] = val
		}
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
// the new values. The values are read under the memory's lock, so of two
// notifications that overlap the later reading is remembered last
// (review II, R2-P1-req2-10).
func (v *pathValues) changed() []im.ConcreteAttributePath {
	v.mem.mu.Lock()
	defer v.mem.mu.Unlock()
	now := v.read()
	out := make([]im.ConcreteAttributePath, 0, len(v.paths))
	for _, p := range v.paths {
		cur, ok := now[p]
		if prev, had := v.mem.last[p]; ok && had && prev.equal(cur) {
			continue
		}
		out = append(out, p)
		if ok {
			v.mem.last[p] = cur
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
	// A flagged path with a WildcardFilterConfigurationVersion reads the
	// node's ConfigurationVersion each time its flags are asked; one
	// report asks once per (changed path × covering subscribed path), so
	// the reads are taken once per report (review II, R2-P6-bound-0565).
	reads := &reportReads{Dispatcher: dispatcher}
	out := make([]im.ConcreteAttributePath, 0, len(paths))
	for _, p := range paths {
		if !p.HasEndpoint || !p.HasCluster || !p.HasAttribute {
			out = append(out, p)
			continue
		}
		for _, sp := range sub.AttributePaths {
			if subscribedPathCovers(sp, p) && !im.WildcardPathFlagsSkip(qctx, reads, sp, p) {
				out = append(out, p)
				break
			}
		}
	}
	return out
}

// reportReads is a dispatcher whose reads are taken once for the one
// report it serves: a second read of a path answers the first's results.
// One goroutine uses it, for one report's path filter.
type reportReads struct {
	im.Dispatcher
	reads map[im.ConcreteAttributePath][]im.ReadResult
}

func (r *reportReads) Read(ctx context.Context, path im.ConcreteAttributePath) []im.ReadResult {
	if res, ok := r.reads[path]; ok {
		return res
	}
	res := r.Dispatcher.Read(ctx, path)
	if r.reads == nil {
		r.reads = make(map[im.ConcreteAttributePath][]im.ReadResult)
	}
	r.reads[path] = res
	return res
}

// subscribedPathCovers reports whether the subscribed (possibly wildcard)
// path covers the concrete path p.
func subscribedPathCovers(sp, p im.ConcreteAttributePath) bool {
	return (!sp.HasEndpoint || sp.Endpoint == p.Endpoint) &&
		(!sp.HasCluster || sp.Cluster == p.Cluster) &&
		(!sp.HasAttribute || sp.Attribute == p.Attribute)
}

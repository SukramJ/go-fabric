// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/icd"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// Subscription defaults. Mirrors matter.js
// packages/protocol/src/action/client/ClientInteraction.ts:96
// (SUBSCRIPTION_PROCESSING_TIME = 10 s), :149 (DEFAULT_MIN_INTERVAL_FLOOR =
// 1 s) and :157 (SUBSCRIPTION_MAX_INTERVAL_PUBLISHER_LIMIT = 60 min) at
// 0ceb202d.
const (
	SubscriptionProcessingTime            = 10 * time.Second
	DefaultMinIntervalFloor               = time.Second
	SubscriptionMaxIntervalPublisherLimit = 60 * time.Minute
)

// maxIntervalSeconds is UINT16_MAX, the range of both interval bounds in
// seconds (action/request/Subscribe.ts:46-60).
const maxIntervalSeconds = 0xFFFF

// Errors of Subscribe.
var (
	// ErrNoSubscriptions is a Subscribe on an interaction without a
	// registry (Config.Subscriptions).
	ErrNoSubscriptions = errors.New("interaction: no subscription registry")
	// ErrUncommissioned is a Subscribe to a peer without a peer address —
	// matter.js "Subscription unavailable because peer is uncommissioned"
	// (ClientInteraction.ts:991-994).
	ErrUncommissioned = errors.New("interaction: subscription unavailable because peer is uncommissioned")
)

// SubscribeRequest is a subscription — matter.js Subscribe
// (action/request/Subscribe.ts:18-63) with ClientSubscribe
// (subscription/ClientSubscribe.ts), numeric paths.
type SubscribeRequest struct {
	// Attributes, Events, EventFilters, DataVersionFilters and
	// NoFabricFilter are a read's ([ReadRequest]).
	Attributes         []im.ConcreteAttributePath
	Events             []im.ConcreteEventPath
	EventFilters       []im.EventMinimumNumber
	DataVersionFilters []im.DataVersionFilter
	NoFabricFilter     bool
	// ClientRequest names the subscribe exchange's network profile and
	// margin; a sustained subscription's attempts take it, the priority
	// network aside (SustainedSubscription.ts:139-149).
	ClientRequest
	// ReplaceSubscriptions is keepSubscriptions false: our other
	// subscriptions with the peer are closed first and the peer is told to
	// drop its own (CI:996-1008). keepSubscriptions defaults to true
	// (Subscribe.ts:44).
	ReplaceSubscriptions bool
	// MinIntervalFloor is the floor of the reporting interval; nil is
	// [DefaultMinIntervalFloor] (CI:1011).
	MinIntervalFloor *time.Duration
	// MaxIntervalCeiling is the ceiling; nil is
	// [SubscriptionMaxIntervalPublisherLimit] (CI:1012).
	MaxIntervalCeiling *time.Duration
	// Updated receives each report with data, its chunks merged — the
	// priming report too.
	Updated func(r *Report)
	// KeepaliveReceived is called for an empty report (Subscribe.ts:33-37).
	KeepaliveReceived func()
	// Closed is called when the subscription closed (Subscribe.ts:28-31).
	Closed func()
	// Sustain keeps the subscription alive: when the underlying
	// subscription is lost it is re-established, the peer's reachability
	// verified first (ClientSubscribe.ts:14-19, SustainedSubscription.ts).
	Sustain bool
	// BootstrapRead, for a sustained subscription, runs before an attempt
	// until it succeeds — the hook of bootstrapWithRead
	// (SustainedSubscription.ts:187-204), which the node layer fills; an
	// error fails the attempt.
	BootstrapRead func(ctx context.Context) error
	// ActiveChanged, for a sustained subscription, is called when it gains
	// or loses its underlying subscription (SustainedSubscription.ts:83-95).
	ActiveChanged func(active bool)
	// RefreshRequest, for a sustained subscription, rewrites the request
	// before every attempt but the very first and again right after a
	// successful bootstrap read — matter.js refreshRequest
	// (subscription/ClientSubscribe.ts:30-34; SustainedSubscription.ts:182-186,
	// :200-203), which the node layer fills to send its current
	// DataVersionFilters and event minimum. It rewrites the paths and
	// filters; the interval bounds stay Subscribe's and the subscription
	// keeps its callbacks.
	RefreshRequest func(SubscribeRequest) SubscribeRequest
	// MaxEventNumberChanged, for a sustained subscription, is called with
	// the new highest event number once a report raised it, after Updated
	// returned and before the report counts as delivered; an error is
	// logged and the subscription continues. matter.js
	// maxEventNumberChanged (SustainedSubscription.ts:354-364).
	MaxEventNumberChanged func(number uint64) error
	// Wakefulness, for a sustained subscription, returns the peer's ICD
	// wakefulness, nil while the peer is no registered ICD — matter.js
	// icdWakefulness (ClientSubscribe.ts:51-56). It is asked again on
	// every iteration, so a peer registered later or flipped SIT⇄LIT is
	// honoured live: while it requires awaiting the subscription parks
	// instead of retrying on a timer, holds the wakefulness while
	// subscribed, and recreates itself on an operating-mode flip
	// (SustainedSubscription.ts:116-337).
	Wakefulness func() *icd.PeerWakefulness
	// PeerFed, for a sustained subscription, registers fn for the start of
	// a peer's ICD registration on the fabric, with the peer's node id —
	// matter.js peerFed (SustainedSubscription.ts:600-605, FabricIcd
	// peerFed); it returns the unsubscribe. A subscription running without
	// a wakefulness races it, to recreate itself for the peer's mode.
	PeerFed func(fn func(peerNodeID uint64)) (unsubscribe func())
}

// Interval returns d as a subscription interval bound.
func Interval(d time.Duration) *time.Duration { return &d }

// Subscribe subscribes on a fresh exchange. Mirrors matter.js
// ClientInteraction.subscribe (ClientInteraction.ts:978-1123) at 0ceb202d:
//
//   - a request without paths is refused, more paths than the provider's
//     subscribePathsSupported only logged (:981-989);
//   - the peer must have a peer address (:991-994);
//   - ReplaceSubscriptions closes our subscriptions with the peer first
//     (:996-1008);
//   - the intervals default to 1 s and 60 min, the ceiling may not be below
//     the floor (:1010-1021), and each is at most 65535 s (Subscribe.ts:46-60);
//   - the attempt (:1023-1078): the request with the intervals in seconds,
//     encoded to fit one message (InteractionMessenger.ts:1052-1055); the
//     priming report's chunks read and acknowledged (:1048,
//     InteractionMessenger.ts:938-972) and handed to Updated; the
//     SubscribeResponse; the subscription registered by (peer, id), its
//     liveness timeout its maximum interval plus twice the maximum peer
//     response time for a 10 s processing time (:1054-1063).
//
// ctx bounds the establishment of a plain subscription. A sustained one
// (Sustain) is returned at once and establishes itself on its own until
// closed (:1092-1115).
func (c *Interaction) Subscribe(ctx context.Context, req SubscribeRequest) (*Subscription, error) {
	paths := len(req.Attributes) + len(req.Events)
	if paths == 0 {
		return nil, fmt.Errorf("%w: when subscribing to attributes and events, at least one must be specified", ErrMalformedRequest)
	}
	if paths > c.provider.SubscribePathsSupported() {
		c.log.Info("matter.controller.interaction.subscribe_paths", slog.Int("paths", paths), slog.Int("supported", c.provider.SubscribePathsSupported()))
	}
	peer, ok := c.provider.PeerAddress()
	if !ok {
		return nil, ErrUncommissioned
	}
	if c.subs == nil {
		return nil, ErrNoSubscriptions
	}
	floor, ceiling := DefaultMinIntervalFloor, SubscriptionMaxIntervalPublisherLimit
	if req.MinIntervalFloor != nil {
		floor = *req.MinIntervalFloor
	}
	if req.MaxIntervalCeiling != nil {
		ceiling = *req.MaxIntervalCeiling
	}
	for _, d := range []time.Duration{floor, ceiling} {
		if d < 0 || d/time.Second > maxIntervalSeconds {
			return nil, fmt.Errorf("%w: interval %v is out of range", ErrMalformedRequest, d)
		}
	}
	if ceiling < floor {
		return nil, fmt.Errorf("%w: maxIntervalCeiling (%v) is less than minIntervalFloor (%v)", ErrMalformedRequest, ceiling, floor)
	}
	if req.ReplaceSubscriptions {
		for _, s := range c.subs.Active() {
			if s.peer == peer {
				c.log.Debug("matter.controller.interaction.subscription_replaced", slog.Uint64("subscription_id", uint64(s.ID())))
				s.Close()
			}
		}
	}
	if req.Sustain {
		return c.sustain(peer, req, floor, ceiling), nil
	}
	sub, err := c.subscribeOnce(ctx, peer, req, floor, ceiling, req.exchangeOptions())
	if err != nil {
		return nil, err
	}
	c.subs.addActive(sub)
	return sub, nil
}

// subscribeOnce is the attempt of CI:1023-1078.
func (c *Interaction) subscribeOnce(ctx context.Context, peer exchange.PeerAddress, req SubscribeRequest, floor, ceiling time.Duration, opts ExchangeOptions) (*Subscription, error) {
	m, ctx, err := c.begin(ctx, opts)
	if err != nil {
		return nil, err
	}
	abandoned := false
	defer func() {
		if !abandoned {
			m.close()
		}
	}()
	// Fitted to the exchange's session, after the exchange chose it
	// (InteractionMessenger.ts:1145-1165, W4-ctlim-11).
	payload, err := encodeFitting(subscribeRequest(im.SubscribeRequest{
		KeepSubscriptions:  !req.ReplaceSubscriptions,
		MinIntervalFloor:   uint16(floor / time.Second),   //nolint:gosec // bounded to 65535 s by Subscribe
		MaxIntervalCeiling: uint16(ceiling / time.Second), //nolint:gosec // bounded to 65535 s by Subscribe
		AttributeRequests:  req.Attributes,
		EventRequests:      req.Events,
		EventFilters:       req.EventFilters,
		FabricFiltered:     !req.NoFabricFilter,
		DataVersionFilters: req.DataVersionFilters,
	}), m.x.Session().MaxPayloadSize())
	if err != nil {
		return nil, err
	}
	// A failed establishment is abandoned, not dropped: the request may
	// have reached the peer, which may hold the subscription and send its
	// report — unless the peer or the session ended it (ClientInteraction.ts:1084-1097
	// at 862ad75f).
	fail := func(err error) (*Subscription, error) {
		awaitReport := !errors.Is(err, exchange.ErrTransient) && !errors.Is(err, exchange.ErrNetwork) &&
			!errors.Is(err, exchange.ErrRetransmissionLimit) && !errors.Is(err, exchange.ErrSessionClosed) && !isStatusError(err)
		if awaitReport || m.reportAwaitsStatus {
			abandoned = true
			//nolint:contextcheck // the abandonment outlives the caller's ctx and ends with the interaction's closing (ClientInteraction.ts:1285-1300)
			go func() {
				defer m.close()
				c.abandonSubscription(m, awaitReport)
			}()
		}
		return nil, c.closedCause(ctx, err)
	}
	if err := m.send(ctx, im.OpcodeSubscribeRequest, payload, 0); err != nil {
		return fail(err)
	}
	a := newAssembler(c.log)
	a.report.Session = m.x.Session()
	if err := readDataReports(ctx, c.log, m, nil, func(rd *im.ReportData) error {
		a.add(rd)
		if !rd.MoreChunkedMessages && req.Updated != nil {
			req.Updated(a.report)
		}
		return nil
	}); err != nil {
		return fail(err)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	msg, err := m.next(ctx, im.OpcodeSubscribeResponse, 0)
	if err != nil {
		return fail(err)
	}
	resp, err := im.UnmarshalSubscribeResponseTLV(tlv.NewDecoder(msg.Payload))
	if err != nil {
		return nil, fmt.Errorf("%w: SubscribeResponse: %w", ErrUnexpectedMessage, err)
	}
	sub := c.subs.newSubscription(peer, req, c.log)
	sub.id, sub.maxInterval = resp.SubscriptionID, resp.MaxInterval
	sub.maxPeerResponseTime = c.provider.MaximumPeerResponseTime(SubscriptionProcessingTime, false)
	c.subs.AddPeer(sub)
	c.log.Info("matter.controller.interaction.subscribed",
		slog.Uint64("subscription_id", uint64(resp.SubscriptionID)), slog.Int("max_interval", int(resp.MaxInterval)), slog.Duration("timeout", sub.timeout()))
	return sub, nil
}

// abandonSubscription ends an abandoned establishment on the peer —
// InteractionMessenger.abandonSubscription (InteractionMessenger.ts:1116-1137
// at 862ad75f) under the interaction's closing: a report whose status is
// owed is answered INVALID_SUBSCRIPTION; otherwise, with awaitReport, the
// next report is awaited and answered so. A failure is logged only.
func (c *Interaction) abandonSubscription(m *messenger, awaitReport bool) {
	ctx := c.abort
	id := m.reportSubscriptionID
	if !m.reportAwaitsStatus {
		if !awaitReport {
			return
		}
		msg, err := m.x.Next(ctx, exchange.ReceiveOptions{})
		if err != nil {
			c.log.Debug("matter.controller.interaction.abandoned_not_terminated", slog.Any("err", err))
			return
		}
		if msg.ProtocolID != im.InteractionModelProtocolID || msg.Opcode != im.OpcodeReportData {
			return
		}
		if rd, err := decodeReport(msg.Payload); err == nil {
			id = rd.SubscriptionID
		}
	}
	m.reportAwaitsStatus = false
	// Sent once even while closing; the closing ends only the wait for
	// its acknowledgement and the retransmissions (ackWaitAbort,
	// InteractionMessenger.ts:1129, MessageExchange.ts:125-130).
	if err := m.sendInvalidSubscription(ctx.Done()); err != nil {
		c.log.Debug("matter.controller.interaction.abandoned_not_terminated", slog.Uint64("subscription_id", uint64(id)), slog.Any("err", err))
		return
	}
	c.log.Debug("matter.controller.interaction.abandoned_terminated", slog.Uint64("subscription_id", uint64(id)))
}

// isStatusError reports a StatusResponse the peer sent in place of the
// expected message — matter.js ReceivedStatusResponseError.
func isStatusError(err error) bool {
	var se *StatusError
	return errors.As(err, &se)
}

// subscribeRequest is a SubscribeRequest whose DataVersionFilters
// #encodeReadingRequest may shorten (InteractionMessenger.ts:1052-1055).
type subscribeRequest im.SubscribeRequest

func (r subscribeRequest) encode(filters []im.DataVersionFilter) ([]byte, error) {
	req := im.SubscribeRequest(r)
	req.DataVersionFilters = filters
	enc := tlv.NewEncoder()
	req.MarshalTLV(enc)
	return enc.Bytes()
}

func (r subscribeRequest) filters() []im.DataVersionFilter { return r.DataVersionFilters }

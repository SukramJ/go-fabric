// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package interaction

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/SukramJ/go-fabric/controller/exchange"
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
				c.log.Debug("matter.controller.interaction.subscription_replaced", slog.Uint64("subscription", uint64(s.ID())))
				s.Close()
			}
		}
	}
	if req.Sustain {
		return c.sustain(peer, req, floor, ceiling), nil
	}
	sub, err := c.subscribeOnce(ctx, peer, req, floor, ceiling, ExchangeOptions{})
	if err != nil {
		return nil, err
	}
	c.subs.addActive(sub)
	return sub, nil
}

// subscribeOnce is the attempt of CI:1023-1078.
func (c *Interaction) subscribeOnce(ctx context.Context, peer exchange.PeerAddress, req SubscribeRequest, floor, ceiling time.Duration, opts ExchangeOptions) (*Subscription, error) {
	payload, err := encodeFitting(subscribeRequest(im.SubscribeRequest{
		KeepSubscriptions:  !req.ReplaceSubscriptions,
		MinIntervalFloor:   uint16(floor / time.Second),   //nolint:gosec // bounded to 65535 s by Subscribe
		MaxIntervalCeiling: uint16(ceiling / time.Second), //nolint:gosec // bounded to 65535 s by Subscribe
		AttributeRequests:  req.Attributes,
		EventRequests:      req.Events,
		EventFilters:       req.EventFilters,
		FabricFiltered:     !req.NoFabricFilter,
		DataVersionFilters: req.DataVersionFilters,
	}))
	if err != nil {
		return nil, err
	}
	m, err := c.begin(ctx, opts)
	if err != nil {
		return nil, err
	}
	defer m.x.Close()
	if err := m.send(ctx, im.OpcodeSubscribeRequest, payload, 0); err != nil {
		return nil, err
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
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	msg, err := m.next(ctx, im.OpcodeSubscribeResponse, 0)
	if err != nil {
		return nil, err
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
		slog.Uint64("subscription", uint64(resp.SubscriptionID)), slog.Int("max_interval", int(resp.MaxInterval)), slog.Duration("timeout", sub.timeout()))
	return sub, nil
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

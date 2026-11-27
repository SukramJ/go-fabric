// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// Package interaction is the controller's Interaction Model client (ADR
// 0020, stages 2 and 3), ported from matter.js
// packages/protocol/src/action/client/ (ClientInteraction.ts, InputChunk.ts,
// subscription/) and the client half of interaction/InteractionMessenger.ts
// at 0ceb202d: Invoke (one command), Read with events and filters, Write,
// Subscribe with the registry of subscriptions and their liveness, the
// handler of the reports a peer pushes, and sustained subscriptions. An
// [Interaction] is bound to an exchange [Provider]: one session, or a peer
// whose sessions come and go. An invoke of several commands is split by
// the peer's MaxPathsPerInvoke (InvokeBatch), and single commands invoked
// within one batch duration share an InvokeRequest when the peer takes
// more than one path (Invoke). The batcher is matter.js's
// #invokeWithBatching (ClientInteraction.ts:752-1014 at b25ed960).
//
// The messages are encoded and decoded with im/'s client encoders and
// decoders, the shapes of matter.js's TLV message definitions.
package interaction

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	mrand "math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"thingfabric.dev/go-fabric/client/exchange"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/tlv"
)

// Timeouts of the client. Mirrors matter.js
// packages/protocol/src/action/client/ClientInteraction.ts at b25ed960:
// DEFAULT_TIMED_REQUEST_TIMEOUT = 10 s (ClientInteraction.ts:194 at b25ed960)
// and DEFAULT_MINIMUM_RESPONSE_TIMEOUT_WITH_FAILSAFE = 30 s
// (ClientInteraction.ts:195 at b25ed960).
const (
	DefaultTimedRequestTimeout                = 10 * time.Second
	DefaultMinimumResponseTimeoutWithFailsafe = 30 * time.Second
)

// ErrUnexpectedMessage is a reply that is not the message the interaction
// expects — matter.js UnexpectedDataError / MatterFlowError
// (InteractionMessenger.ts:186-190, :924-928).
var ErrUnexpectedMessage = errors.New("interaction: unexpected message")

// ErrNoResponse is an InvokeResponse without an entry for the command:
// matter.js #invokeCommand then returns undefined
// (ControllerCommissioningFlow.ts:510-529).
var ErrNoResponse = errors.New("interaction: no response for the command")

// StatusError is an interaction the peer ended with a status other than
// Success — a CommandStatusIB of the command (matter.js
// StatusResponseError.create, ControllerCommissioningFlow.ts:519-523) or a
// StatusResponse in place of the expected message
// (ReceivedStatusResponseError, InteractionMessenger.ts:201-214, :920-923).
type StatusError struct {
	Status im.StatusCode
	// ClusterStatus is the cluster-specific status, nil when absent.
	ClusterStatus *uint8
	// Path is the attribute the status is for — a write's failing path
	// (matter.js PathError, action/response/WriteResult.ts:41-59) — nil for
	// an interaction-level status.
	Path *im.ConcreteAttributePath
}

func (e *StatusError) Error() string {
	var path string
	if e.Path != nil {
		path = fmt.Sprintf(" at %d/0x%04X/0x%04X", e.Path.Endpoint, e.Path.Cluster, e.Path.Attribute)
	}
	if e.ClusterStatus != nil {
		return fmt.Sprintf("interaction: status %s, cluster status %d%s", e.Status, *e.ClusterStatus, path)
	}
	return "interaction: status " + e.Status.String() + path
}

// Interaction is the client side of the Interaction Model towards one
// peer — matter.js ClientInteraction (ClientInteraction.ts:239-1367 at
// b25ed960) bound to an exchange [Provider]: a session ([New]) or a peer
// whose sessions come and go ([ForPeer]). It is safe for concurrent use;
// each call runs on its own exchange.
type Interaction struct {
	provider Provider
	subs     *Subscriptions
	log      *slog.Logger
	clock    exchange.Clock
	rand     func() float64
	// network is [Config].Network, #network (ClientInteraction.ts:252 at
	// b25ed960).
	network string
	// maxReport is the report bound of its exchanges
	// ([Config].MaxReportBytesPerSession).
	maxReport int
	// attemptHook, a test seam, replaces a sustained subscription's
	// subscribe of one attempt — matter.js's injected subscribe
	// (SustainedSubscription.Configuration.subscribe).
	attemptHook func(ctx context.Context, req SubscribeRequest, opts ExchangeOptions) (*Subscription, error)

	// closeMu guards closed and the in-flight count's Add against Close;
	// inflight is #interactions (ClientInteraction.ts:245-246 at b25ed960),
	// one per running exchange; abort is #abort (ClientInteraction.ts:249 at
	// b25ed960), aborted by close (ClientInteraction.ts:311 at b25ed960),
	// which every running exchange's context follows
	// (ClientInteraction.ts:1269 at b25ed960).
	closeMu  sync.Mutex
	closed   error
	inflight sync.WaitGroup
	abort    context.Context
	abortFn  context.CancelCauseFunc

	// batchMu guards the batcher: lanes is #batchLanes and nextCommandRef
	// #nextCommandRef (ClientInteraction.ts:255-256 at b25ed960).
	batchMu        sync.Mutex
	lanes          map[string]*batchLane
	nextCommandRef uint16
	// flushing counts the lane flushes running (a test seam's reading).
	flushing atomic.Int32

	// estMu guards established: #established, the subscriptions this
	// interaction established — each inner peer subscription and each
	// returned one — removed again once it closed
	// (packages/protocol/src/action/client/ClientInteraction.ts:248, :1121,
	// :1129, :1169, :1190 at b25ed960).
	estMu       sync.Mutex
	established map[*Subscription]struct{}
}

// ErrClosed is the reason of [Interaction.Close] without one — matter.js
// ClosedError "Interaction component closed" (ClientInteraction.ts:286-288
// at b25ed960).
var ErrClosed = errors.New("interaction: interaction component closed")

// Config configures an [Interaction] — what matter.js ClientInteraction
// takes from its context (ClientInteraction.ts:140-147).
type Config struct {
	// Subscriptions is the controller's subscription registry
	// ([NewSubscriptions]); Subscribe needs it — matter.js takes it from
	// the environment (ClientInteraction.ts:188-190, :252-257).
	Subscriptions *Subscriptions
	// Logger receives the diagnostics; nil is slog.Default().
	Logger *slog.Logger
	// Clock paces the sustained subscriptions' retries; nil is the
	// system clock.
	Clock exchange.Clock
	// Rand returns the retry jitter draw in [0, 1); nil is
	// math/rand/v2.Float64.
	Rand func() float64
	// Network is the network profile of every request that names none —
	// matter.js ClientInteractionContext.network
	// (ClientInteraction.ts:181 at b25ed960), the field #network
	// (ClientInteraction.ts:252 at b25ed960) set in the constructor
	// (ClientInteraction.ts:271 at b25ed960), applied as
	// `request.network ?? this.#network` to the exchange
	// (ClientInteraction.ts:1273 at b25ed960) and to the batch lane of a
	// command (ClientInteraction.ts:773 at b25ed960): a command without a network and one
	// naming this network share a batch. "" is the peer's medium profile.
	Network string
	// MaxReportBytesPerSession bounds the report chunks the session's
	// interactions hold while the rest of their chunks arrive, each charged
	// its payload and decoded elements ([DefaultMaxReportBytesPerSession],
	// [ErrReportTooLarge]); zero or less is the bound of Subscriptions
	// when it is set, else [DefaultMaxReportBytesPerSession].
	MaxReportBytesPerSession int
}

// New returns the interaction over session s of mgr — matter.js
// DedicatedChannelExchangeProvider
// (packages/protocol/src/protocol/ExchangeProvider.ts:123-165 at
// db91886c). A nil
// logger is slog.Default().
func New(mgr *exchange.Manager, s *exchange.Session, log *slog.Logger) *Interaction {
	return Bind(SessionProvider(mgr, s), Config{Logger: log})
}

// Bind returns the interaction over provider p.
func Bind(p Provider, cfg Config) *Interaction {
	c := &Interaction{provider: p, subs: cfg.Subscriptions, log: cfg.Logger, clock: cfg.Clock, rand: cfg.Rand, network: cfg.Network, maxReport: cfg.MaxReportBytesPerSession}
	c.abort, c.abortFn = context.WithCancelCause(context.Background())
	if c.log == nil {
		c.log = slog.Default()
	}
	if c.clock == nil {
		c.clock = exchange.SystemClock{}
	}
	if c.rand == nil {
		c.rand = mrand.Float64
	}
	if c.maxReport <= 0 {
		c.maxReport = DefaultMaxReportBytesPerSession
		if c.subs != nil {
			c.maxReport = c.subs.maxReport
		}
	}
	return c
}

// Provider is the interaction's exchange provider.
func (c *Interaction) Provider() Provider { return c.provider }

// Session is the session the interaction runs on — the provider's
// dedicated session, or the peer's newest one (nil when none).
func (c *Interaction) Session() *exchange.Session { return c.provider.Session() }

// MaximumPeerResponseTime is the longest wait for the peer's answer to a
// message with expected processing time ept: our retransmissions, ept and
// the 5 s buffer, plus — with includeSending — the peer's retransmissions
// back to us. Mirrors matter.js ClientInteraction.maximumPeerResponseTime
// (packages/protocol/src/action/client/ClientInteraction.ts:1357-1358 at
// b25ed960), the provider's computation (review II, R2-P6-doc-2906).
func (c *Interaction) MaximumPeerResponseTime(ept time.Duration, includeSending bool) time.Duration {
	return c.provider.MaximumPeerResponseTime(ept, includeSending)
}

// Close closes the interaction: the subscriptions it established close,
// so none outlives it (a sustained one would retry forever) — those other
// interactions established to the same peer stay open —, every running
// exchange is aborted with reason, and Close returns once they have
// ended. Later interactions fail with reason; nil is [ErrClosed].
// Idempotent; the first reason stays. Mirrors matter.js
// packages/protocol/src/action/client/ClientInteraction.ts:285-315 at
// b25ed960 (ClientInteraction.close).
func (c *Interaction) Close(reason error) {
	if reason == nil {
		reason = ErrClosed
	}
	c.closeMu.Lock()
	if c.closed == nil {
		c.closed = reason
	}
	reason = c.closed
	c.closeMu.Unlock()
	c.closeEstablished()
	// The queued commands are rejected, then the interaction aborts before
	// it waits for what is in flight, so an in-flight batch cancels instead
	// of running to completion (ClientInteraction.ts:300-311 at b25ed960).
	c.closeBatches(reason)
	c.abortFn(reason)
	c.inflight.Wait()
}

// register adds s to the subscriptions the interaction established and
// then registers it with add; s leaves the set when it closes. The check
// of the close and the addition run under closeMu, so a subscription
// either is in the set Close snapshots or sees the interaction closed:
// then it is closed once registered and register returns the close
// reason, so no subscription outlives the interaction
// (ClientInteraction.ts:1121, :1129, :1169, :1190 at b25ed960).
//
// add runs outside closeMu, and s is in the set from before it runs, so
// a Close on another goroutine — the interaction's, or one that reaches s
// otherwise — can close s before add registers it; s.Close then deleted
// nothing. A closed s is not added (an AddPeer would displace the live
// subscription holding its id), and after add a closed s is deleted
// again: a Close that set isClosed before the check is caught here, one
// after it deletes s itself (review II, R2-C2-conc-2101).
func (c *Interaction) register(s *Subscription, add func(*Subscription)) error {
	registered := s.closed
	s.closed = func() {
		if registered != nil {
			registered()
		}
		c.estMu.Lock()
		delete(c.established, s)
		c.estMu.Unlock()
	}
	c.closeMu.Lock()
	closed := c.closed
	if closed == nil {
		c.estMu.Lock()
		if c.established == nil {
			c.established = map[*Subscription]struct{}{}
		}
		c.established[s] = struct{}{}
		c.estMu.Unlock()
	}
	c.closeMu.Unlock()
	if !s.isClosedNow() {
		add(s)
	}
	if s.isClosedNow() {
		s.reg.Delete(s)
	}
	if closed != nil {
		s.Close()
		return closed
	}
	return nil
}

// closeEstablished closes the subscriptions the interaction established —
// the first step of Close (ClientInteraction.ts:293-298 at b25ed960).
func (c *Interaction) closeEstablished() {
	c.estMu.Lock()
	subs := make([]*Subscription, 0, len(c.established))
	for s := range c.established {
		subs = append(subs, s)
	}
	c.estMu.Unlock()
	for _, s := range subs {
		s.Close()
	}
}

// CloseSubscriptions closes every active subscription of the
// interaction's peer in the registry, whichever interaction established
// it — what a disabled node runs for its subscriptions (ClientNode.disable
// closes its node interaction, packages/node/src/node/ClientNode.ts:274-288,
// :326-340 at b25ed960). Close closes only those the interaction
// established.
func (c *Interaction) CloseSubscriptions() {
	if c.subs == nil || c.provider == nil {
		return
	}
	peer, ok := c.provider.PeerAddress()
	if !ok {
		return
	}
	for _, s := range c.subs.Active() {
		if s.peer == peer {
			s.Close()
		}
	}
}

// messenger is one IM exchange — matter.js InteractionClientMessenger.
type messenger struct {
	x       *exchange.Exchange
	release func()
	// maxReport is the bound on the report chunks the exchange's session
	// holds ([Config].MaxReportBytesPerSession); zero is the default.
	maxReport int
	// reportAwaitsStatus and reportSubscriptionID are matter.js's
	// #reportAwaitsStatus (InteractionMessenger.ts:994 at db91886c) and
	// #subscriptionId (InteractionMessenger.ts:996 at db91886c), set in
	// readDataReports (InteractionMessenger.ts:1025-1062 at db91886c): a
	// report was read whose status is still owed, and the subscription it
	// named.
	reportAwaitsStatus   bool
	reportSubscriptionID uint32
	// reportInterruption is #reportInterruption
	// (InteractionMessenger.ts:995 at db91886c): why the data report the
	// peer sent did not complete, "" when it completed or failed for
	// another reason.
	reportInterruption reportInterruption
}

// reportInterruption is matter.js ReportInterruption
// (InteractionMessenger.ts:982-991 at db91886c): the peer did not
// acknowledge our status to a chunk that more follow, or to the final
// chunk, or it sent no next chunk after our status to a chunk.
type reportInterruption string

const (
	reportChunkUnacknowledged reportInterruption = "chunk-unacknowledged"
	reportFinalUnacknowledged reportInterruption = "final-unacknowledged"
	reportChunkMissing        reportInterruption = "chunk-missing"
)

// isAckMissing reports a send the peer never acknowledged within its MRP
// budget — matter.js PeerAckMissingError (PeerCommunicationError.ts:63-67
// at db91886c), which the Go exchange raises as the exact
// [*exchange.PeerUnresponsiveError] (a [*exchange.PeerMessageMissingError]
// is no match: errors.As takes the type, not its Is).
func isAckMissing(err error) bool {
	var unresponsive *exchange.PeerUnresponsiveError
	return errors.As(err, &unresponsive)
}

// isMessageMissing reports a wait for the peer's next message that ended
// without one — matter.js PeerMessageMissingError
// (packages/protocol/src/protocol/MessageExchange.ts:915 at db91886c),
// which the Go exchange raises as the exact
// [*exchange.PeerMessageMissingError]. errors.As takes the type: a
// [*exchange.PeerUnresponsiveError] is no match, although
// PeerMessageMissingError.Is matches that target.
func isMessageMissing(err error) bool {
	var missing *exchange.PeerMessageMissingError
	return errors.As(err, &missing)
}

// sendReportStatus answers rd with a StatusResponse Success and records a
// status the peer never acknowledged. Mirrors matter.js
// IncomingInteractionClientMessenger.#sendReportStatus
// (packages/protocol/src/interaction/InteractionMessenger.ts:1075-1087 at
// db91886c).
func (m *messenger) sendReportStatus(ctx context.Context, rd *im.ReportData) error {
	err := m.sendStatus(ctx, im.StatusSuccess)
	if err != nil && isAckMissing(err) {
		m.reportInterruption = reportFinalUnacknowledged
		if rd.MoreChunkedMessages {
			m.reportInterruption = reportChunkUnacknowledged
		}
	}
	return err
}

// requestEncoding is the encoding of an Invoke's or Write's flags on m's
// exchange: [im.RequestFlagsForSending] for a unicast request, with the
// peer's InteractionModelRevision from the session parameters and this
// module's own revision — the call matter.js makes before an Invoke is sent
// and before a Write is chunked
// (packages/protocol/src/interaction/InteractionMessenger.ts:1266, :1330 at
// db91886c).
func (m *messenger) requestEncoding(omitFalseFlags bool) im.RequestEncoding {
	return im.RequestFlagsForSending(omitFalseFlags, false, m.x.Session().Parameters().InteractionModelRevision, uint16(im.MatterInteractionModelRevision))
}

// close closes the exchange and ends its in-flight entry.
func (m *messenger) close() {
	m.x.Close()
	m.release()
}

// begin opens the interaction's exchange — ClientInteraction.#begin
// (ClientInteraction.ts:1168-1237) through the provider's initiateExchange
// — as one in-flight entry (:1191-1197): the context it returns, which the
// exchange's work runs under, ends with ctx and with [Interaction.Close];
// a closed interaction refuses with its close reason.
func (c *Interaction) begin(ctx context.Context, opts ExchangeOptions) (*messenger, context.Context, error) {
	opts.Network = c.networkFor(opts.Network)
	c.closeMu.Lock()
	if err := c.closed; err != nil {
		c.closeMu.Unlock()
		return nil, ctx, err
	}
	c.inflight.Add(1)
	c.closeMu.Unlock()
	ictx, cancel := context.WithCancelCause(ctx)
	stop := context.AfterFunc(c.abort, func() { cancel(context.Cause(c.abort)) })
	release := func() {
		stop()
		cancel(nil)
		c.inflight.Done()
	}
	x, err := c.provider.InitiateExchange(ictx, opts)
	if err != nil {
		release()
		return nil, ctx, c.closedCause(ictx, err)
	}
	return &messenger{x: x, release: release, maxReport: c.maxReport}, ictx, nil
}

// networkFor is `request.network ?? this.#network`
// (ClientInteraction.ts:765, :1250): network, or the interaction's default
// when it names none.
func (c *Interaction) networkFor(network string) string {
	if network == "" {
		return c.network
	}
	return network
}

// closedCause is err, or the close reason when Close aborted ictx.
func (c *Interaction) closedCause(ictx context.Context, err error) error {
	//nolint:contextcheck // c.abort is the interaction's closing, not a request context
	if cause := context.Cause(ictx); cause != nil && c.abort.Err() != nil && errors.Is(cause, context.Cause(c.abort)) {
		return cause
	}
	return err
}

// send sends one IM message reliably.
func (m *messenger) send(ctx context.Context, opcode uint8, payload []byte, ept time.Duration) error {
	return m.x.Send(ctx, opcode, payload, exchange.SendOptions{ExpectedProcessingTime: ept})
}

// sendStatus sends a StatusResponse — InteractionMessenger.ts:148-161.
func (m *messenger) sendStatus(ctx context.Context, status im.StatusCode) error {
	enc := tlv.NewEncoder()
	im.StatusResponse{Status: status}.MarshalTLV(enc)
	b, err := enc.Bytes()
	if err != nil {
		return err
	}
	return m.x.Send(ctx, im.OpcodeStatusResponse, b, exchange.SendOptions{})
}

// sendInvalidSubscription sends a StatusResponse INVALID_SUBSCRIPTION
// whose wait for the acknowledgement ackWaitAbort ends — sendInvalidSubscription
// (InteractionMessenger.ts:1094-1104 at b25ed960).
func (m *messenger) sendInvalidSubscription(ackWaitAbort <-chan struct{}) error {
	enc := tlv.NewEncoder()
	im.StatusResponse{Status: im.StatusInvalidSubscription}.MarshalTLV(enc)
	b, err := enc.Bytes()
	if err != nil {
		return err
	}
	//nolint:contextcheck // sent once whatever ends; ackWaitAbort ends the wait (MessageExchange.ts:125-130)
	return m.x.Send(context.Background(), im.OpcodeStatusResponse, b, exchange.SendOptions{AckWaitAbort: ackWaitAbort})
}

// next waits for the next message, which must carry want: a
// StatusResponse other than Success is a *StatusError (:179-192,
// :201-214, :913-931), a StatusResponse Success or any other message in
// its place an ErrUnexpectedMessage.
func (m *messenger) next(ctx context.Context, want uint8, ept time.Duration) (*exchange.Message, error) {
	msg, err := m.x.Next(ctx, exchange.ReceiveOptions{ExpectedProcessingTime: ept})
	if err != nil {
		return nil, err
	}
	if msg.ProtocolID != im.InteractionModelProtocolID {
		return nil, fmt.Errorf("%w: protocol 0x%04X", ErrUnexpectedMessage, msg.ProtocolID)
	}
	if msg.Opcode == im.OpcodeStatusResponse {
		sr, err := im.UnmarshalStatusResponseTLV(tlv.NewDecoder(msg.Payload))
		if err != nil {
			return nil, fmt.Errorf("%w: StatusResponse: %w", ErrUnexpectedMessage, err)
		}
		if sr.Status != im.StatusSuccess {
			return nil, &StatusError{Status: sr.Status}
		}
	}
	if msg.Opcode != want {
		return nil, fmt.Errorf("%w: message type 0x%02X, expected 0x%02X", ErrUnexpectedMessage, msg.Opcode, want)
	}
	return msg, nil
}

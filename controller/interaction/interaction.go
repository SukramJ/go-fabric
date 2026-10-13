// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package interaction is the controller's Interaction Model client, as
// far as commissioning needs it (ADR 0020, stage 2): one Invoke and one
// Read on an existing secure session, ported from matter.js
// packages/protocol/src/action/client/ClientInteraction.ts and the client
// half of interaction/InteractionMessenger.ts at 0ceb202d. Stage 3 grows
// it into the full client (Write, Subscribe, batching, chunked requests).
//
// The messages are encoded and decoded with im/'s client encoders and
// decoders (ADR 0020, stage 3), the shapes of matter.js's TLV message
// definitions.
package interaction

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	mrand "math/rand/v2"
	"time"

	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// Timeouts of the client. Mirrors matter.js
// packages/protocol/src/action/client/ClientInteraction.ts:159
// (DEFAULT_TIMED_REQUEST_TIMEOUT = 10 s) and :160
// (DEFAULT_MINIMUM_RESPONSE_TIMEOUT_WITH_FAILSAFE = 30 s) at 0ceb202d.
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
}

func (e *StatusError) Error() string {
	if e.ClusterStatus != nil {
		return fmt.Sprintf("interaction: status %s, cluster status %d", e.Status, *e.ClusterStatus)
	}
	return "interaction: status " + e.Status.String()
}

// Interaction is the client side of the Interaction Model towards one
// peer — matter.js ClientInteraction (ClientInteraction.ts:165-1259 at
// 0ceb202d) bound to an exchange [Provider]: a session ([New]) or a peer
// whose sessions come and go ([ForPeer]). It is safe for concurrent use;
// each call runs on its own exchange.
type Interaction struct {
	provider Provider
	log      *slog.Logger
	clock    exchange.Clock
	rand     func() float64
}

// Config configures an [Interaction] — what matter.js ClientInteraction
// takes from its context (ClientInteraction.ts:140-147).
type Config struct {
	// Logger receives the diagnostics; nil discards them.
	Logger *slog.Logger
	// Clock paces the sustained subscriptions' retries; nil is the
	// system clock.
	Clock exchange.Clock
	// Rand returns the retry jitter draw in [0, 1); nil is
	// math/rand/v2.Float64.
	Rand func() float64
}

// New returns the interaction over session s of mgr — matter.js
// DedicatedChannelExchangeProvider (ExchangeProvider.ts:122-164). A nil
// logger discards.
func New(mgr *exchange.Manager, s *exchange.Session, log *slog.Logger) *Interaction {
	return Bind(SessionProvider(mgr, s), Config{Logger: log})
}

// Bind returns the interaction over provider p.
func Bind(p Provider, cfg Config) *Interaction {
	c := &Interaction{provider: p, log: cfg.Logger, clock: cfg.Clock, rand: cfg.Rand}
	if c.log == nil {
		c.log = slog.New(slog.DiscardHandler)
	}
	if c.clock == nil {
		c.clock = exchange.SystemClock{}
	}
	if c.rand == nil {
		c.rand = mrand.Float64
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
// (ClientInteraction.ts:1249-1251), the provider's computation.
func (c *Interaction) MaximumPeerResponseTime(ept time.Duration, includeSending bool) time.Duration {
	return c.provider.MaximumPeerResponseTime(ept, includeSending)
}

// messenger is one IM exchange — matter.js InteractionClientMessenger.
type messenger struct {
	x *exchange.Exchange
}

// begin opens the interaction's exchange — ClientInteraction.#begin
// (ClientInteraction.ts:1168-1237) through the provider's initiateExchange.
func (c *Interaction) begin(ctx context.Context, opts ExchangeOptions) (*messenger, error) {
	x, err := c.provider.InitiateExchange(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &messenger{x: x}, nil
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

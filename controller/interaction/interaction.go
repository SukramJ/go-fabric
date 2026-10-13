// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

// Package interaction is the controller's Interaction Model client, as
// far as commissioning needs it (ADR 0020, stage 2): one Invoke and one
// Read on an existing secure session, ported from matter.js
// packages/protocol/src/action/client/ClientInteraction.ts and the client
// half of interaction/InteractionMessenger.ts at 0ceb202d. Stage 3 grows
// it into the full client (Write, Subscribe, batching, chunked requests).
//
// The requests are encoded here, not through im/'s server-side types,
// until stage 3 moves the client encoders into im/ (docs/open-items.md):
// a ReadRequest carries the InteractionModelRevision and no empty
// AttributeRequests, as matter.js's TlvReadRequest does.
package interaction

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
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

// Interaction is the client side of the Interaction Model on one secure
// session — the part of matter.js ClientInteraction bound to an exchange
// provider that commissioning uses. It is safe for concurrent use; each
// call runs on its own exchange.
type Interaction struct {
	mgr     *exchange.Manager
	session *exchange.Session
	log     *slog.Logger
}

// New returns the interaction over session s of mgr. A nil logger
// discards.
func New(mgr *exchange.Manager, s *exchange.Session, log *slog.Logger) *Interaction {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Interaction{mgr: mgr, session: s, log: log}
}

// Session is the session the interaction runs on.
func (c *Interaction) Session() *exchange.Session { return c.session }

// MaximumPeerResponseTime is the longest wait for the peer's answer to a
// message with expected processing time ept: our retransmissions, ept and
// the 5 s buffer, plus — with includeSending — the peer's retransmissions
// back to us. Mirrors matter.js ClientInteraction.maximumPeerResponseTime
// (ClientInteraction.ts:1249-1251) over exchange.MaxPeerResponseTime.
func (c *Interaction) MaximumPeerResponseTime(ept time.Duration, includeSending bool) time.Duration {
	if ept == 0 {
		ept = exchange.DefaultExpectedProcessingTime
	}
	local := exchange.IntervalsOf(c.mgr.LocalParameters())
	if !includeSending {
		return exchange.MaxPeerResponseTime(nil, false, local, ept)
	}
	peer := exchange.IntervalsOf(c.session.Parameters())
	return exchange.MaxPeerResponseTime(&peer, c.session.IsPeerActive(), local, ept)
}

// messenger is one IM exchange — matter.js InteractionClientMessenger.
type messenger struct {
	x *exchange.Exchange
}

func (c *Interaction) begin() (*messenger, error) {
	x, err := c.mgr.Initiate(c.session, im.InteractionModelProtocolID, exchange.Options{})
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

// decodeNode reads a payload into a node tree.
func decodeNode(b []byte) (spec.Node, error) {
	n, err := spec.ReadElement(tlv.NewDecoder(b))
	if err != nil {
		return spec.Node{}, fmt.Errorf("%w: %w", ErrUnexpectedMessage, err)
	}
	return n, nil
}

// child returns the context-tagged child tag of n.
func child(n spec.Node, tag uint8) (spec.Node, bool) {
	for _, c := range n.Children {
		if c.El.Tag.Kind == tlv.TagKindContext && c.El.Tag.Number == uint32(tag) {
			return c, true
		}
	}
	return spec.Node{}, false
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridgetest

// A secure session (PASE or CASE) of the commissioner's exchange manager
// and the Interaction Model client over it — controller/interaction bound
// to the session, the subscriptions in the commissioner's registry.

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/controller/exchange"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// ErrSessionClosed is returned by a call on a session that was closed —
// by [Session.Close], or by the device with a CloseSession StatusReport.
var ErrSessionClosed = errors.New("bridgetest: session closed")

// ErrHandshake is returned when the device ends a PASE or CASE handshake
// with an error StatusReport or answers a step with the wrong message.
var ErrHandshake = errors.New("bridgetest: session handshake failed")

// StatusError is an interaction the device ended with an IM
// StatusResponse instead of the expected message.
type StatusError struct {
	Status im.StatusCode
}

func (e *StatusError) Error() string {
	return "bridgetest: interaction ended with status " + e.Status.String()
}

// closeTimeout bounds the CloseSession a [Session.Close] sends.
const closeTimeout = 5 * time.Second

// Session is one secure session with the device. It is safe for
// concurrent use.
type Session struct {
	c       *Commissioner
	s       *exchange.Session
	ix      *interaction.Interaction
	reports chan *Report
}

// PeerNodeID is the device's operational node id (0 on a PASE session).
func (s *Session) PeerNodeID() uint64 { return s.s.PeerNodeID() }

// LocalNodeID is the controller node id the session authenticates (0 on
// a PASE session).
func (s *Session) LocalNodeID() uint64 { return s.s.NodeID() }

// Close ends the session: a CloseSession StatusReport to the device, then
// the keys are dropped. It is idempotent.
func (s *Session) Close() {
	if !s.s.IsClosed() {
		ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
		s.c.mgr.CloseSession(ctx, s.s)
		cancel()
	}
	s.c.forget(s)
}

// closed reports whether the session is gone — closed by [Session.Close],
// by the device's CloseSession (the exchange manager's Secure Channel
// handler closes it) or by the commissioner.
func (s *Session) closed() bool { return s.s.IsClosed() }

// failure maps an error of controller/interaction onto this package's:
// a status the device ended the interaction with is a [*StatusError], an
// error on a session that closed meanwhile wraps [ErrSessionClosed].
func (s *Session) failure(err error) error {
	var se *interaction.StatusError
	if errors.As(err, &se) && se.Path == nil {
		return &StatusError{Status: se.Status}
	}
	if s.closed() && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("%w: %w", ErrSessionClosed, err)
	}
	return err
}

// Read reads paths (fabric-filtered) and returns the report.
func (s *Session) Read(ctx context.Context, paths ...im.ConcreteAttributePath) (*Report, error) {
	if s.closed() {
		return nil, ErrSessionClosed
	}
	r, err := s.ix.Read(ctx, paths...)
	if err != nil {
		return nil, s.failure(err)
	}
	return fromReport(r), nil
}

// Subscription is an established subscription.
type Subscription struct {
	// ID is the SubscriptionID the device assigned.
	ID uint32
	// MaxInterval is the negotiated maximum interval, in seconds.
	MaxInterval uint16
	// Priming is the priming report.
	Priming *Report
}

// Subscribe subscribes (fabric-filtered, KeepSubscriptions false) to
// paths. Later reports arrive through [Session.NextReport]; keepalives
// (empty reports) are not queued.
//
// Only a CASE session subscribes: on a PASE session Subscribe fails with
// an error wrapping [interaction.ErrUncommissioned], as matter.js's client
// subscribes only to a peer with a peer address
// (packages/protocol/src/action/client/ClientInteraction.ts:991-994 at
// 0ceb202d).
func (s *Session) Subscribe(ctx context.Context, minInterval, maxInterval uint16, paths ...im.ConcreteAttributePath) (*Subscription, error) {
	if s.closed() {
		return nil, ErrSessionClosed
	}
	var (
		mu      sync.Mutex
		priming *Report
		primed  bool
	)
	sub, err := s.ix.Subscribe(ctx, interaction.SubscribeRequest{
		Attributes:           paths,
		ReplaceSubscriptions: true,
		MinIntervalFloor:     interaction.Interval(time.Duration(minInterval) * time.Second),
		MaxIntervalCeiling:   interaction.Interval(time.Duration(maxInterval) * time.Second),
		Updated: func(r *interaction.Report) {
			report := fromReport(r)
			mu.Lock()
			if !primed {
				// The priming report reaches Updated before Subscribe
				// returns.
				primed, priming = true, report
				mu.Unlock()
				return
			}
			mu.Unlock()
			s.c.route(r.Session, report)
		},
	})
	if err != nil {
		return nil, s.failure(err)
	}
	mu.Lock()
	defer mu.Unlock()
	return &Subscription{ID: sub.ID(), MaxInterval: uint16(sub.MaxInterval() / time.Second), Priming: priming}, nil //nolint:gosec // the device's maximum interval is a uint16 in seconds
}

// NextReport returns the next report the device sent on its own — of a
// subscription made on this session, or one it re-established onto it.
func (s *Session) NextReport(ctx context.Context) (*Report, error) {
	ctx, cancel := withDefaultTimeout(ctx)
	defer cancel()
	select {
	case r := <-s.reports:
		return r, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// encodable adapts a field writer to spec.Encodable.
type encodable func(e *tlv.Encoder, tag tlv.Tag)

func (f encodable) EncodeTLV(e *tlv.Encoder, tag tlv.Tag) { f(e, tag) }

// Write writes one attribute and returns the status the device reported
// for it. value encodes the attribute value under the tag it is given; it
// goes as one AttributeDataIB, a list value included.
func (s *Session) Write(ctx context.Context, path im.ConcreteAttributePath, value func(e *tlv.Encoder, tag tlv.Tag)) (im.StatusCode, error) {
	if s.closed() {
		return 0, ErrSessionClosed
	}
	res, err := s.ix.Write(ctx, []interaction.WriteRequest{{Path: path, Value: encodable(value)}}, interaction.WriteOptions{DisableListChunking: true})
	if err != nil {
		return 0, s.failure(err)
	}
	if len(res) != 1 {
		return 0, fmt.Errorf("bridgetest: WriteResponse with %d statuses, want 1", len(res))
	}
	return res[0].Status, nil
}

// InvokeResult is the device's answer to one command: response fields, or
// a status.
type InvokeResult struct {
	// Command is the response command id; Fields its fields. Both are
	// zero when the device answered with a status.
	Command uint32
	Fields  TLV
	// Status is the CommandStatusIB status; Success alongside Fields.
	Status im.StatusCode
	// ClusterStatus is the cluster-specific status, when HasClusterStatus.
	ClusterStatus    uint8
	HasClusterStatus bool
}

// Invoke invokes one command. fields encodes the request's fields under
// the tag it is given — a generated request's EncodeTLV fits — and nil
// sends an empty structure. A CommandStatusIB other than Success is the
// result's Status, not an error; a StatusResponse in place of the
// InvokeResponse is a [*StatusError].
func (s *Session) Invoke(ctx context.Context, path im.ConcreteCommandPath, fields func(e *tlv.Encoder, tag tlv.Tag)) (*InvokeResult, error) {
	if s.closed() {
		return nil, ErrSessionClosed
	}
	var payload spec.Encodable
	if fields != nil {
		payload = encodable(fields)
	}
	res, err := s.ix.Invoke(ctx, path, payload, interaction.InvokeOptions{})
	var se *interaction.StatusError
	if err != nil && (!errors.As(err, &se) || res.Status == im.StatusSuccess) {
		// Not a CommandStatusIB: interaction.Invoke returns the result of
		// one alongside its *StatusError, a StatusResponse with none.
		return nil, s.failure(err)
	}
	out := &InvokeResult{Command: res.Command, Status: res.Status}
	if res.HasFields {
		out.Fields = fromNode(res.Fields)
	}
	if res.ClusterStatus != nil {
		out.ClusterStatus, out.HasClusterStatus = *res.ClusterStatus, true
	}
	return out, nil
}

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
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// InvokeOptions configures [Interaction.Invoke] — the options of matter.js
// Invoke.Definition the commissioning flow sets.
type InvokeOptions struct {
	// ExpectedProcessingTime is the peer's processing time of the command;
	// 0 is the default (ClientInteraction.ts:466-474).
	ExpectedProcessingTime time.Duration
	// ExtendedFailSafeTimeout is useExtendedFailSafeMessageResponseTimeout:
	// without an ExpectedProcessingTime the command's processing time is
	// [DefaultMinimumResponseTimeoutWithFailsafe] (:466-474).
	ExtendedFailSafeTimeout bool
	// Timed sends a TimedRequest first (:438-440) and marks the invoke as
	// timed.
	Timed bool
	// TimedTimeout is the TimedRequest's timeout; 0 is
	// [DefaultTimedRequestTimeout] (:439).
	TimedTimeout time.Duration
	// SuppressResponse asks the peer for no InvokeResponse: the send
	// expects an acknowledgement only and Invoke returns the zero Result.
	SuppressResponse bool
	// ClientRequest names the invoke exchange's network profile and margin.
	ClientRequest
}

// Result is the peer's answer to the command — the "cmd-response" or
// "cmd-status" entry of matter.js's decoded invoke result.
type Result struct {
	// Command is the response command id; HasFields reports whether the
	// answer carried a response command, Fields its fields.
	Command   uint32
	HasFields bool
	Fields    spec.Node
	// Status is the command's status: Success with fields, or the
	// CommandStatusIB's.
	Status im.StatusCode
	// ClusterStatus is the cluster-specific status, nil when absent.
	ClusterStatus *uint8
}

// Invoke invokes one command on a fresh exchange and returns the peer's
// answer; a status other than Success is a *StatusError. fields is the
// request payload (a generated request's value), nil for an empty one.
// Mirrors matter.js ClientInteraction.#invokeSingle
// (ClientInteraction.ts:434-474) with InteractionClientMessenger
// .sendInvokeCommand (interaction/InteractionMessenger.ts:1061-1108) at
// 0ceb202d:
//
//   - a timed invoke first sends a TimedRequest and waits for its Success
//     StatusResponse (:438-440, InteractionMessenger.ts:1124-1136);
//   - the single command goes without a commandRef (:457-464);
//   - the processing time is ExpectedProcessingTime, else 30 s with the
//     extended failsafe timeout, else the exchange's default (:466-474),
//     for the send's final wait and the wait for the response;
//   - a chunked InvokeResponse is acknowledged with a StatusResponse
//     Success after every chunk with more to come and its entries are
//     accumulated (InteractionMessenger.ts:1082-1105);
//   - the first entry is the answer, as the flow's #invokeCommand reads it
//     (ControllerCommissioningFlow.ts:506-530): a CommandStatusIB other
//     than Success is a *StatusError.
//
// Errors of the exchange layer pass through unchanged.
func (c *Interaction) Invoke(ctx context.Context, path im.ConcreteCommandPath, fields spec.Encodable, opts InvokeOptions) (Result, error) {
	path.HasEndpoint, path.HasCluster, path.HasCommand = true, true, true
	entries, err := c.exchangeInvoke(ctx, []im.CommandInvocation{{Path: path, Fields: fields}}, opts)
	if err != nil || opts.SuppressResponse {
		return Result{}, err
	}
	if len(entries) == 0 {
		return Result{}, ErrNoResponse
	}
	res := resultOf(entries[0])
	if res.Status != im.StatusSuccess {
		return res, &StatusError{Status: res.Status, ClusterStatus: res.ClusterStatus}
	}
	return res, nil
}

// exchangeInvoke sends one InvokeRequest of invokes on a fresh exchange —
// the TimedRequest first for a timed invoke, the commands with their
// fields (each invocation's Fields a spec.Encodable, nil for an empty
// payload) — and returns the entries of every InvokeResponse chunk, none
// for a suppressed response. It is the exchange of [Interaction.Invoke]
// and of each batch of [Interaction.InvokeBatch]: matter.js
// ClientInteraction.#invokeSingle (ClientInteraction.ts:487-649 at
// 862ad75f) up to the decoding of its entries.
func (c *Interaction) exchangeInvoke(ctx context.Context, invokes []im.CommandInvocation, opts InvokeOptions) ([]im.InvokeResponseEntry, error) {
	m, err := c.begin(ctx, opts.exchangeOptions())
	if err != nil {
		return nil, err
	}
	defer m.x.Close()
	// A timeout alone marks the invoke timed, as a write's does. Mirrors
	// matter.js packages/protocol/src/action/request/Invoke.ts:114
	// (timedRequest = !!options.timed || !!timeout).
	timed := opts.Timed || opts.TimedTimeout > 0
	if timed {
		timeout := opts.TimedTimeout
		if timeout == 0 {
			timeout = DefaultTimedRequestTimeout
		}
		if err := m.timed(ctx, timeout); err != nil {
			return nil, err
		}
	}
	ept := opts.ExpectedProcessingTime
	if ept == 0 && opts.ExtendedFailSafeTimeout {
		ept = DefaultMinimumResponseTimeoutWithFailsafe
	}
	enc := tlv.NewEncoder()
	im.MarshalInvokeRequestTLV(enc, im.InvokeRequest{SuppressResponse: opts.SuppressResponse, TimedRequest: timed, Invokes: invokes},
		func(e *tlv.Encoder, tag tlv.Tag, v any) {
			fields, _ := v.(spec.Encodable)
			if fields == nil {
				e.StartStruct(tag)
				_ = e.EndContainer()
				return
			}
			fields.EncodeTLV(e, tag)
		})
	req, err := enc.Bytes()
	if err != nil {
		return nil, err
	}
	for _, inv := range invokes {
		path := inv.Path
		c.log.Debug("matter.controller.interaction.invoke", slog.Uint64("endpoint", uint64(path.Endpoint)), slog.Uint64("cluster", uint64(path.Cluster)), slog.Uint64("command", uint64(path.Command)))
	}
	if opts.SuppressResponse {
		// A suppressed invoke expects an acknowledgement only; a message in
		// its place is tolerated unless it is a StatusResponse with an
		// error, and there is no result. Mirrors matter.js
		// packages/protocol/src/interaction/InteractionMessenger.ts:1210-1218
		// with requestWithSuppressedResponse (:1454-1478) and
		// packages/protocol/src/action/client/ClientInteraction.ts:528.
		err := m.x.Send(ctx, im.OpcodeInvokeRequest, req, exchange.SendOptions{ExpectAckOnly: true, ExpectedProcessingTime: ept})
		var unexpected *exchange.UnexpectedMessageError
		if errors.As(err, &unexpected) {
			return nil, statusOf(unexpected.Message)
		}
		return nil, err
	}
	if err := m.send(ctx, im.OpcodeInvokeRequest, req, ept); err != nil {
		return nil, err
	}
	var entries []im.InvokeResponseEntry
	for {
		msg, err := m.next(ctx, im.OpcodeInvokeResponse, ept)
		if err != nil {
			return nil, err
		}
		resp, err := im.UnmarshalInvokeResponseTLV(tlv.NewDecoder(msg.Payload), readFields)
		if err != nil {
			return nil, fmt.Errorf("%w: InvokeResponse: %w", ErrUnexpectedMessage, err)
		}
		entries = append(entries, resp.Responses...)
		if !resp.MoreChunkedMessages {
			return entries, nil
		}
		if err := m.sendStatus(ctx, im.StatusSuccess); err != nil {
			return nil, err
		}
	}
}

// resultOf is one entry of an InvokeResponse as a [Result]: the response
// command and its fields, or the status and cluster status.
func resultOf(e im.InvokeResponseEntry) Result {
	if e.IsStatus {
		res := Result{Status: e.Status.Status}
		if e.Status.HasClusterStatus {
			cs := e.Status.ClusterStatus
			res.ClusterStatus = &cs
		}
		return res
	}
	res := Result{Command: e.Path.Command, Status: im.StatusSuccess}
	if n, ok := e.Response.(spec.Node); ok {
		res.HasFields, res.Fields = true, n
	}
	return res
}

// readFields keeps a response command's fields as a node tree.
func readFields(_ im.ConcreteCommandPath, dec *tlv.Decoder, el tlv.Element) (any, error) {
	if el.IsContainer {
		return spec.ReadContainer(dec, el)
	}
	return spec.Node{El: el}, nil
}

// timed sends a TimedRequest and waits for its Success — matter.js
// InteractionClientMessenger.sendTimedRequest
// (InteractionMessenger.ts:1440-1452, TlvTimedRequest: [0] timeout in
// milliseconds, [255] InteractionModelRevision).
func (m *messenger) timed(ctx context.Context, timeout time.Duration) error {
	enc := tlv.NewEncoder()
	im.TimedRequest{TimeoutMs: uint16(min(timeout.Milliseconds(), 0xFFFF))}.MarshalTLV(enc) //nolint:gosec // bounded to uint16
	req, err := enc.Bytes()
	if err != nil {
		return err
	}
	if err := m.send(ctx, im.OpcodeTimedRequest, req, 0); err != nil {
		return err
	}
	_, err = m.next(ctx, im.OpcodeStatusResponse, 0)
	return err
}

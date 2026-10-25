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

// ListChunked reports whether a list value of the attribute is chunked by
// default: every attribute but AccessControl ACL and Extension
// (Write.ts:18-21, isAclOrExtensionPath).
func ListChunked(cluster, attribute uint32) bool { return im.ListChunkedByDefault(cluster, attribute) }

// WriteRequest is one attribute to write — a Write.Attribute of matter.js
// (Write.ts:204-218) with numeric ids.
type WriteRequest struct {
	// Path names the attribute: the endpoint may be a wildcard, the cluster
	// and attribute must be set.
	Path im.ConcreteAttributePath
	// Value is the value to write: a generated type's value, or a node tree
	// through [Raw].
	Value spec.Encodable
	// DataVersion, when set, makes the write conditional; it needs a
	// concrete endpoint (Write.ts:64-69, :128-131).
	DataVersion *uint32
}

// Raw writes a node tree as a value.
func Raw(n spec.Node) spec.Encodable { return rawNode(n) }

type rawNode spec.Node

func (n rawNode) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) { spec.Node(n).EncodeTLV(enc, tag) }

// WriteOptions configures [Interaction.Write] — matter.js Write.Options
// (Write.ts:188-199).
type WriteOptions struct {
	// Timed sends a TimedRequest first (ClientInteraction.ts:369-371).
	Timed bool
	// TimedTimeout is the TimedRequest's timeout; 0 is
	// [DefaultTimedRequestTimeout]. A timeout marks the write timed
	// (Write.ts:74).
	TimedTimeout time.Duration
	// SuppressResponse asks the peer for no WriteResponse.
	SuppressResponse bool
	// DisableListChunking writes a list value as one AttributeDataIB; by
	// default a list is written as REPLACE-ALL with the empty list and one
	// append per element (chunkLists = true, Write.ts:59, :149-171).
	DisableListChunking bool
	// ClientRequest names the write exchange's network profile and margin.
	ClientRequest
}

// WriteStatus is the peer's status for one written path — matter.js
// WriteResult.AttributeStatus (action/response/WriteResult.ts:34-39), the
// path with its ListIndex.
type WriteStatus struct {
	Path          im.ConcreteAttributePath
	Status        im.StatusCode
	ClusterStatus *uint8
}

// WriteStatuses is a write's result.
type WriteStatuses []WriteStatus

// Err is nil when every path succeeded, the failing path's *StatusError
// when one failed, and the failing paths' errors joined when several did.
// Mirrors matter.js WriteResult.assertSuccess (WriteResult.ts:41-59).
func (w WriteStatuses) Err() error {
	var errs []error
	for i := range w {
		if w[i].Status == im.StatusSuccess {
			continue
		}
		p := w[i].Path
		errs = append(errs, &StatusError{Status: w[i].Status, ClusterStatus: w[i].ClusterStatus, Path: &p})
	}
	switch len(errs) {
	case 0:
		return nil
	case 1:
		return errs[0]
	}
	return errors.Join(errs...)
}

// ErrMalformedRequest is a request matter.js refuses to formulate —
// MalformedRequestError (action/request/MalformedRequestError.ts).
var ErrMalformedRequest = errors.New("interaction: malformed request")

// Write writes attributes on a fresh exchange. Mirrors matter.js
// ClientInteraction.write (ClientInteraction.ts:365-429) with Write()
// (action/request/Write.ts:43-185) and
// InteractionClientMessenger.sendWriteCommand
// (interaction/InteractionMessenger.ts:1110-1162) at 0ceb202d:
//
//   - a DataVersion needs a concrete endpoint, and a write needs at least
//     one attribute (Write.ts:64-69, :105-107);
//   - a list value becomes the empty list (REPLACE-ALL) and one append per
//     element with a null ListIndex, unless chunking is off or the path is
//     AccessControl ACL or Extension (:149-171);
//   - a timed write first sends a TimedRequest on the same exchange and
//     waits for its Success (:369-371; the timeout defaults to 10 s, :159);
//   - one WriteRequest, MoreChunkedMessages false (Write.ts:77); one
//     that does not fit a message, or a timed one whose WriteResponse
//     might not, is an ErrRequestTooLarge before anything is sent;
//   - with SuppressResponse the send expects an acknowledgement only; a
//     message in its place is tolerated unless it is a StatusResponse
//     with an error (IM:1138-1162, the "Matter-SDK bug" of :1156), and the
//     result is nil (:382-384);
//   - otherwise the WriteResponse's statuses are the result, an empty one
//     an empty result (:385-387).
func (c *Interaction) Write(ctx context.Context, writes []WriteRequest, opts WriteOptions) (WriteStatuses, error) {
	req, err := formulateWrite(writes, opts)
	if err != nil {
		return nil, err
	}
	enc := tlv.NewEncoder()
	req.MarshalTLV(enc, func(e *tlv.Encoder, tag tlv.Tag, v im.AttributeValue) {
		if value, ok := v.Value.(spec.Encodable); ok {
			value.EncodeTLV(e, tag)
		}
	})
	payload, err := enc.Bytes()
	if err != nil {
		return nil, err
	}
	// A write that does not fit one message is refused before anything is
	// sent. Mirrors matter.js
	// packages/protocol/src/interaction/InteractionMessenger.ts:1343-1354
	// (a timed or suppressed write) and :1388-1392 (a value too large for a
	// message); matter.js chunks any other write, which this one-message
	// write does not.
	if len(payload) > exchange.MaxPayloadSize {
		return nil, fmt.Errorf("%w: a write of %d bytes, at most %d", ErrRequestTooLarge, len(payload), exchange.MaxPayloadSize)
	}
	// A timed write cannot be chunked, so its WriteResponse — every status
	// at its largest, Failure with a cluster status — must fit one message
	// too. Mirrors matter.js
	// packages/protocol/src/interaction/InteractionMessenger.ts:103-106,
	// :1335-1354.
	if req.TimedRequest {
		if size := worstWriteResponseSize(req); size > exchange.MaxPayloadSize {
			return nil, fmt.Errorf("%w: a timed write whose statuses take up to %d bytes, at most %d", ErrRequestTooLarge, size, exchange.MaxPayloadSize)
		}
	}
	m, err := c.begin(ctx, opts.exchangeOptions())
	if err != nil {
		return nil, err
	}
	defer m.x.Close()
	if req.TimedRequest {
		timeout := opts.TimedTimeout
		if timeout == 0 {
			timeout = DefaultTimedRequestTimeout
		}
		if err := m.timed(ctx, timeout); err != nil {
			return nil, err
		}
	}
	c.log.Debug("matter.controller.interaction.write", slog.Int("attributes", len(req.Writes)), slog.Bool("timed", req.TimedRequest))
	if req.SuppressResponse {
		err := m.x.Send(ctx, im.OpcodeWriteRequest, payload, exchange.SendOptions{ExpectAckOnly: true})
		var unexpected *exchange.UnexpectedMessageError
		if errors.As(err, &unexpected) {
			return nil, statusOf(unexpected.Message)
		}
		return nil, err
	}
	if err := m.send(ctx, im.OpcodeWriteRequest, payload, 0); err != nil {
		return nil, err
	}
	msg, err := m.next(ctx, im.OpcodeWriteResponse, 0)
	if err != nil {
		return nil, err
	}
	resp, err := im.UnmarshalWriteResponseTLV(tlv.NewDecoder(msg.Payload))
	if err != nil {
		return nil, fmt.Errorf("%w: WriteResponse: %w", ErrUnexpectedMessage, err)
	}
	out := make(WriteStatuses, 0, len(resp.Responses))
	for _, r := range resp.Responses {
		s := WriteStatus{Path: r.Path, Status: r.Status.Status}
		if r.Status.HasClusterStatus {
			cs := r.Status.ClusterStatus
			s.ClusterStatus = &cs
		}
		out = append(out, s)
	}
	return out, nil
}

// worstWriteResponseSize is the size of req's WriteResponse with every
// status at its largest (maxStatusSizeOf, InteractionMessenger.ts:103-106).
func worstWriteResponseSize(req im.WriteRequest) int {
	worst := im.WriteResponse{Responses: make([]im.AttributeStatus, 0, len(req.Writes))}
	for _, w := range req.Writes {
		worst.Responses = append(worst.Responses, im.AttributeStatus{
			Path:   w.Path,
			Status: im.StatusIB{Status: im.StatusFailure, ClusterStatus: uint8(im.StatusFailure), HasClusterStatus: true},
		})
	}
	enc := tlv.NewEncoder()
	worst.MarshalTLV(enc)
	b, _ := enc.Bytes()
	return len(b)
}

// statusOf is throwIfErrorStatusMessage (InteractionMessenger.ts:201-214)
// for the message that answered a suppressed write: a StatusResponse other
// than Success is a *StatusError, anything else nil.
func statusOf(msg *exchange.Message) error {
	if msg.ProtocolID != im.InteractionModelProtocolID || msg.Opcode != im.OpcodeStatusResponse {
		return nil
	}
	sr, err := im.UnmarshalStatusResponseTLV(tlv.NewDecoder(msg.Payload))
	if err != nil {
		return fmt.Errorf("%w: StatusResponse: %w", ErrUnexpectedMessage, err)
	}
	if sr.Status != im.StatusSuccess {
		return &StatusError{Status: sr.Status}
	}
	return nil
}

// formulateWrite is Write() (Write.ts:43-185) over numeric paths.
func formulateWrite(writes []WriteRequest, opts WriteOptions) (im.WriteRequest, error) {
	req := im.WriteRequest{
		TimedRequest:     opts.Timed || opts.TimedTimeout > 0,
		SuppressResponse: opts.SuppressResponse,
	}
	for _, w := range writes {
		if !w.Path.HasCluster || !w.Path.HasAttribute {
			return im.WriteRequest{}, fmt.Errorf("%w: a write must name the cluster and the attribute", ErrMalformedRequest)
		}
		if w.DataVersion != nil && !w.Path.HasEndpoint {
			return im.WriteRequest{}, fmt.Errorf("%w: a write with a DataVersion must target a concrete endpoint", ErrMalformedRequest)
		}
		if w.Value == nil {
			return im.WriteRequest{}, fmt.Errorf("%w: a write without a value", ErrMalformedRequest)
		}
		base := im.AttributeWrite{Path: w.Path}
		base.Path.HasListIndex, base.Path.ListAppend = false, false
		if w.DataVersion != nil {
			base.DataVersion, base.HasDataVersion = *w.DataVersion, true
		}
		elements, isList, err := listElements(w.Value)
		if err != nil {
			return im.WriteRequest{}, err
		}
		if !isList || opts.DisableListChunking || !ListChunked(w.Path.Cluster, w.Path.Attribute) {
			a := base
			a.Value = im.AttributeValue{Value: w.Value}
			req.Writes = append(req.Writes, a)
			continue
		}
		// REPLACE-ALL with the empty list, then one append per element
		// (ArraySchema.encodeAsChunkedArray, types/src/tlv/TlvArray.ts:156-164).
		first := base
		first.Value = im.AttributeValue{Value: rawNode(spec.Node{El: tlv.Element{Type: tlv.TypeArray, IsContainer: true}})}
		req.Writes = append(req.Writes, first)
		for _, e := range elements {
			a := base
			a.Path.HasListIndex, a.Path.ListAppend = true, true
			a.Value = im.AttributeValue{Value: rawNode(e)}
			req.Writes = append(req.Writes, a)
		}
	}
	if len(req.Writes) == 0 {
		return im.WriteRequest{}, fmt.Errorf("%w: the write contains no attributes", ErrMalformedRequest)
	}
	return req, nil
}

// listElements is [spec.ListElements] with the request's error.
func listElements(v spec.Encodable) ([]spec.Node, bool, error) {
	elements, isList, err := spec.ListElements(v)
	if err != nil {
		return nil, false, fmt.Errorf("%w: value: %w", ErrMalformedRequest, err)
	}
	return elements, isList, nil
}

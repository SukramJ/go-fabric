// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

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

// The AccessControl attributes whose list values are never chunked.
// Mirrors matter.js packages/protocol/src/action/request/Write.ts:14-21 at
// 0ceb202d (AccessControl 0x001F, Acl 0x0000, Extension 0x0001 —
// model/src/standard/elements/access-control.element.ts:20, :31, :35), the
// connectedhomeip #38263 exemption of :151-156.
const (
	accessControlCluster   uint32 = 0x001F
	accessControlACL       uint32 = 0x0000
	accessControlExtension uint32 = 0x0001
)

// ListChunked reports whether a list value of the attribute is chunked by
// default: every attribute but AccessControl ACL and Extension
// (Write.ts:18-21, isAclOrExtensionPath).
func ListChunked(cluster, attribute uint32) bool {
	return cluster != accessControlCluster || (attribute != accessControlACL && attribute != accessControlExtension)
}

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

func (n rawNode) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) { writeNode(enc, tag, spec.Node(n)) }

// writeNode writes n at tag, integers at their minimal width.
func writeNode(enc *tlv.Encoder, tag tlv.Tag, n spec.Node) {
	el := n.El
	switch {
	case el.IsContainer:
		switch el.Type {
		case tlv.TypeArray:
			enc.StartArray(tag)
		case tlv.TypeList:
			enc.StartList(tag)
		default:
			enc.StartStruct(tag)
		}
		for _, c := range n.Children {
			writeNode(enc, c.El.Tag, c)
		}
		_ = enc.EndContainer()
	case el.IsNull || el.Type == tlv.TypeNull:
		enc.PutNull(tag)
	case el.Type <= tlv.TypeSignedInt8:
		enc.PutInt(tag, el.Int)
	case el.Type <= tlv.TypeUnsignedInt8:
		enc.PutUint(tag, el.Uint)
	case el.Type == tlv.TypeBoolFalse || el.Type == tlv.TypeBoolTrue:
		enc.PutBool(tag, el.Bool)
	case el.Type == tlv.TypeFloat4:
		enc.PutFloat32(tag, float32(el.Float))
	case el.Type == tlv.TypeFloat8:
		enc.PutFloat64(tag, el.Float)
	case el.Type >= tlv.TypeUTF8Str1 && el.Type <= tlv.TypeUTF8Str8:
		enc.PutUTF8(tag, el.String)
	default:
		enc.PutOctets(tag, el.Octets)
	}
}

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
//   - one WriteRequest, MoreChunkedMessages false (Write.ts:77);
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
	m, err := c.begin(ctx, ExchangeOptions{})
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

// listElements encodes v and reports whether it is a list (a TLV array),
// with its elements — the value side of matter.js's
// `Array.isArray(value) && tlv instanceof ArraySchema` (Write.ts:154-155).
func listElements(v spec.Encodable) ([]spec.Node, bool, error) {
	enc := tlv.NewEncoder()
	v.EncodeTLV(enc, tlv.AnonymousTag())
	b, err := enc.Bytes()
	if err != nil {
		return nil, false, fmt.Errorf("%w: value: %w", ErrMalformedRequest, err)
	}
	n, err := spec.ReadElement(tlv.NewDecoder(b))
	if err != nil {
		return nil, false, fmt.Errorf("%w: value: %w", ErrMalformedRequest, err)
	}
	if n.El.Type != tlv.TypeArray {
		return nil, false, nil
	}
	return n.Children, true, nil
}

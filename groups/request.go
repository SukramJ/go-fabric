// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package groups

import (
	"context"
	"fmt"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// The Interaction Model face of a [Sender]: the requests a group can carry.
// Mirrors matter.js ClientGroupInteraction
// (packages/node/src/node/client/ClientGroupInteraction.ts at 0ceb202d):
// a group message names no endpoint, nobody answers it, and reads,
// subscriptions and timed requests are refused — so the Sender offers
// Invoke and Write only, each without a response.

// RequestOptions configures [Sender.Invoke] and [Sender.Write].
type RequestOptions struct {
	// Timed asks for a timed request, which a group refuses
	// ([ErrInvalidGroupOperation]; ClientGroupInteraction.ts:48-50,
	// :74-76).
	Timed bool
	// DisableListChunking writes a list value as one AttributeDataIB; by
	// default a group write lists a list value REPLACE-ALL with the empty
	// list and one append per element, as a unicast write does (matter.js
	// Write.ts chunkLists = true, :59, :149-171 at 0ceb202d).
	DisableListChunking bool
}

// Invoke sends one command to the group. The path names the cluster and
// the command; a path naming an endpoint is refused, as is a timed invoke
// ([ErrInvalidGroupOperation]). The InvokeRequest carries SuppressResponse:
// invoking on a group yields no response by definition. fields is the
// request payload (a generated request's value), nil for an empty one.
// Mirrors ClientGroupInteraction.invoke (ClientGroupInteraction.ts:70-81)
// and the InvokeRequest encoding of im.MarshalInvokeRequestTLV.
func (s *Sender) Invoke(ctx context.Context, path im.ConcreteCommandPath, fields spec.Encodable, opts RequestOptions) error {
	if path.HasEndpoint {
		return fmt.Errorf("%w: invoking a concrete command on a group address is not supported", ErrInvalidGroupOperation)
	}
	if opts.Timed {
		return fmt.Errorf("%w: timed requests are not supported for group address invokes", ErrInvalidGroupOperation)
	}
	path.HasCluster, path.HasCommand = true, true
	enc := tlv.NewEncoder()
	im.MarshalInvokeRequestTLV(enc, im.InvokeRequest{SuppressResponse: true, Invokes: []im.CommandInvocation{{Path: path}}},
		func(e *tlv.Encoder, tag tlv.Tag, _ any) {
			if fields == nil {
				e.StartStruct(tag)
				_ = e.EndContainer()
				return
			}
			fields.EncodeTLV(e, tag)
		})
	payload, err := enc.Bytes()
	if err != nil {
		return fmt.Errorf("groups: encode InvokeRequest: %w", err)
	}
	return s.Send(ctx, payload, im.InteractionModelProtocolID, im.OpcodeInvokeRequest)
}

// Write writes one attribute on the group. The path names the cluster and
// the attribute and no endpoint, or the write is refused, as is a timed
// write ([ErrInvalidGroupOperation]). The WriteRequest carries
// SuppressResponse: writing to a group yields no response. A list value is
// list-chunked in the one message — REPLACE-ALL with the empty list, then
// one append per element — unless the attribute is AccessControl's ACL or
// Extension or opts.DisableListChunking is set: ClientGroupInteraction.write
// hands the request to ClientInteraction.write
// (ClientGroupInteraction.ts:44-68 at 0ceb202d), whose Write() builder
// chunks a list as a unicast write does (Write.ts:149-171). The encoding is
// im.WriteRequest.MarshalTLV's.
func (s *Sender) Write(ctx context.Context, path im.ConcreteAttributePath, value spec.Encodable, opts RequestOptions) error {
	if opts.Timed {
		return fmt.Errorf("%w: timed requests are not supported for group address writes", ErrInvalidGroupOperation)
	}
	if path.HasEndpoint || !path.HasCluster || !path.HasAttribute {
		return fmt.Errorf("%w: not all attribute write paths are valid for group address writes", ErrInvalidGroupOperation)
	}
	if value == nil {
		return fmt.Errorf("%w: a write needs a value", ErrInvalidGroupOperation)
	}
	writes := []im.AttributeWrite{{Path: path, Value: im.AttributeValue{Value: value}}}
	if !opts.DisableListChunking && im.ListChunkedByDefault(path.Cluster, path.Attribute) {
		elements, isList, err := spec.ListElements(value)
		if err != nil {
			return fmt.Errorf("groups: encode WriteRequest: %w", err)
		}
		if isList {
			writes = []im.AttributeWrite{{Path: path, Value: im.AttributeValue{Value: spec.Node{El: tlv.Element{Type: tlv.TypeArray, IsContainer: true}}}}}
			for _, e := range elements {
				p := path
				p.HasListIndex, p.ListAppend = true, true
				writes = append(writes, im.AttributeWrite{Path: p, Value: im.AttributeValue{Value: e}})
			}
		}
	}
	enc := tlv.NewEncoder()
	im.WriteRequest{SuppressResponse: true, Writes: writes}.MarshalTLV(enc,
		func(e *tlv.Encoder, tag tlv.Tag, v im.AttributeValue) {
			if value, ok := v.Value.(spec.Encodable); ok {
				value.EncodeTLV(e, tag)
			}
		})
	payload, err := enc.Bytes()
	if err != nil {
		return fmt.Errorf("groups: encode WriteRequest: %w", err)
	}
	return s.Send(ctx, payload, im.InteractionModelProtocolID, im.OpcodeWriteRequest)
}

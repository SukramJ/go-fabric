// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package node

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/SukramJ/go-fabric/bridge"
	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/contract"
	"github.com/SukramJ/go-fabric/controller/client"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// AttributeValue is one attribute value the node serves, as
// [Node.ReadAttribute] and [Node.ReadCluster] return it.
type AttributeValue struct {
	Endpoint  uint16
	Cluster   uint32
	Attribute uint32
	// Value is the value as the cluster server's MatterRead returns it —
	// the Go value the node encodes on the wire; nil for a null.
	Value any
	// DataVersion is the cluster's DataVersion at the read.
	DataVersion uint32
}

// JSON renders Value by name, as a controller of this module renders the
// same attribute read over the wire (controller/client.DecodeJSON with
// NameBased and the attribute's type from the generated cluster
// definitions): a struct keyed by its camelCase field names, a list an
// array, bytes base64, null for a null. A value of a cluster or attribute
// without a definition renders untyped, a struct keyed by its context
// tags.
func (v AttributeValue) JSON() (json.RawMessage, error) {
	return renderJSON(client.AttributeField(v.Cluster, v.Attribute), v.Value)
}

// ReadAttribute returns the value one attribute of the node serves, read
// through the node's own read path — the cluster server's MatterRead, as a
// controller's read reaches it — with the cluster's DataVersion. It applies
// no access control (the host reads its own node) and no fabric: a
// fabric-scoped list holds every fabric's entries, their fabric-sensitive
// fields withheld. An attribute the node does not serve is a
// [contract.StatusError] with the status a controller's read receives
// (UNSUPPORTED_ENDPOINT, UNSUPPORTED_CLUSTER, UNSUPPORTED_ATTRIBUTE);
// before [Node.Start] it is [ErrNotStarted].
func (n *Node) ReadAttribute(ctx context.Context, endpoint uint16, cluster, attribute uint32) (AttributeValue, error) {
	results, err := n.readServed(ctx, im.ConcreteAttributePath{
		Endpoint: endpoint, Cluster: cluster, Attribute: attribute,
		HasEndpoint: true, HasCluster: true, HasAttribute: true,
	})
	if err != nil {
		return AttributeValue{}, err
	}
	for _, r := range results {
		if r.Status != im.StatusSuccess {
			return AttributeValue{}, statusError(r)
		}
		return attributeValueOf(r), nil
	}
	return AttributeValue{}, contract.StatusError{Status: uint8(im.StatusUnsupportedAttribute)}
}

// ReadCluster returns every attribute one cluster of an endpoint serves,
// the global attributes among them, in the order a controller's wildcard
// read reports them, each as [Node.ReadAttribute] returns it. An attribute
// whose read fails is left out. An endpoint or cluster the node does not
// serve is a [contract.StatusError]; before [Node.Start] it is
// [ErrNotStarted].
func (n *Node) ReadCluster(ctx context.Context, endpoint uint16, cluster uint32) ([]AttributeValue, error) {
	results, err := n.readServed(ctx, im.ConcreteAttributePath{
		Endpoint: endpoint, Cluster: cluster, HasEndpoint: true, HasCluster: true,
	})
	if err != nil {
		return nil, err
	}
	out := make([]AttributeValue, 0, len(results))
	for _, r := range results {
		if r.Status != im.StatusSuccess {
			if !r.Path.HasAttribute || r.Status == im.StatusUnsupportedEndpoint || r.Status == im.StatusUnsupportedCluster {
				return nil, statusError(r)
			}
			continue
		}
		out = append(out, attributeValueOf(r))
	}
	return out, nil
}

// readServed runs path through the node's dispatcher.
func (n *Node) readServed(ctx context.Context, path im.ConcreteAttributePath) ([]im.ReadResult, error) {
	d := n.br.Dispatcher()
	if d == nil {
		return nil, ErrNotStarted
	}
	return d.Read(ctx, path), nil
}

func attributeValueOf(r im.ReadResult) AttributeValue {
	v := r.Value.Value
	if r.Value.IsNull {
		v = nil
	}
	return AttributeValue{
		Endpoint: r.Path.Endpoint, Cluster: r.Path.Cluster, Attribute: r.Path.Attribute,
		Value: v, DataVersion: r.DataVersion,
	}
}

func statusError(r im.ReadResult) error {
	return contract.StatusError{
		Status:  uint8(r.Status),
		Message: fmt.Sprintf("read %d/0x%04X/0x%04X", r.Path.Endpoint, r.Path.Cluster, r.Path.Attribute),
	}
}

// Event is one event the node served, as [Node.Events] returns it and
// [Observers].OnEvent receives it: the record of the node's event log,
// which answers a controller's event read.
type Event struct {
	// Number is the node-wide event number (Matter Core §7.14.2.1).
	Number   uint64
	Priority contract.EventPriority
	Endpoint uint16
	Cluster  uint32
	Event    uint32
	// Time is the event's epoch timestamp, millisecond precision.
	Time time.Time
	// Payload is the event's data as the cluster server emitted it; nil
	// without data.
	Payload any
}

// JSON renders Payload by name, as [AttributeValue.JSON] renders a value,
// with the event's type from the generated cluster definitions.
func (e Event) JSON() (json.RawMessage, error) {
	return renderJSON(client.EventField(e.Cluster, e.Event), e.Payload)
}

// EventQuery selects the events [Node.Events] returns. A nil Endpoint,
// Cluster or Event matches every one; the zero query matches every event.
type EventQuery struct {
	Endpoint *uint16
	Cluster  *uint32
	Event    *uint32
	// MinNumber is the lowest event number returned (inclusive), as a
	// controller's EventMin filter.
	MinNumber uint64
	// Limit, when positive, keeps only the newest Limit matching events.
	Limit int
}

// Events returns the events the node's event log holds that match q,
// oldest first — the log a controller's event read is answered from, the
// same records, without access control. The log is bounded (the oldest of
// the lowest priority go first) and volatile: it is emptied when the node
// stops. A node not started returns none.
func (n *Node) Events(q EventQuery) []Event {
	endpoint, cluster, event := uint16(0xFFFF), uint32(0xFFFFFFFF), uint32(0xFFFFFFFF)
	if q.Endpoint != nil {
		endpoint = *q.Endpoint
	}
	if q.Cluster != nil {
		cluster = *q.Cluster
	}
	if q.Event != nil {
		event = *q.Event
	}
	records := n.br.EventLog().Query(endpoint, cluster, event, q.MinNumber)
	if q.Limit > 0 && len(records) > q.Limit {
		records = records[len(records)-q.Limit:]
	}
	out := make([]Event, 0, len(records))
	for _, r := range records {
		out = append(out, eventOf(r))
	}
	return out
}

func eventOf(r im.EventRecord) Event {
	return Event{
		Number: r.Number, Priority: contract.EventPriority(r.Priority),
		Endpoint: r.Endpoint, Cluster: r.Cluster, Event: r.EventID,
		Time: time.UnixMilli(int64(r.EpochMS)), Payload: r.Payload, //nolint:gosec // an epoch in milliseconds fits int64
	}
}

// renderJSON encodes v as the node puts it on the wire and renders the
// element with the controller side's DecodeJSON, so the node and a
// controller render one value alike.
func renderJSON(f client.Field, v any) (json.RawMessage, error) {
	b, err := bridge.AttributeValueTLV(v)
	if err != nil {
		return nil, fmt.Errorf("node: encode %T: %w", v, err)
	}
	el, err := spec.ReadElement(tlv.NewDecoder(b))
	if err != nil {
		return nil, fmt.Errorf("node: decode %T: %w", v, err)
	}
	return client.DecodeJSON(f, el, client.NameBased)
}

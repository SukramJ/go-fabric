// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

// A port of the chunked-write hold cases of matter.js
// packages/node/test/endpoint/server/InteractionProtocolTest.ts at 862ad75f
// (:1605-1916, #4768): an attribute a chunked write holds is answered BUSY
// to another exchange's concrete write and skipped by its wildcard write;
// the hold moves with the chunk and lasts until the chunk's writes are done;
// a write that fits one message holds nothing.
//
// matter.js drives InteractionServer.handleWriteRequest with mocked
// messengers whose sendWriteResponse / readNextWriteRequest callbacks run
// the competing exchange; the port drives HandleWriteRequest once per
// message, one WriteTransaction per exchange over one WriteHolds (what the
// bridge keeps per exchange, bridge/exchange_routing.go), and runs the
// competing write from the dispatcher's write hook where matter.js runs it
// from a $Changing event or between the chunks.
package im

import (
	"context"
	"slices"
	"testing"
)

const (
	holdBasicInformation uint32 = 0x0028
	holdNodeLabel        uint32 = 5
	holdLocation         uint32 = 6
)

// labelDispatcher stores string attributes of endpoint 0 and runs onWrite
// after each write it applies.
type labelDispatcher struct {
	values  map[uint32]string
	onWrite func(attribute uint32, value string)
}

func (d *labelDispatcher) Read(context.Context, ConcreteAttributePath) []ReadResult { return nil }

func (d *labelDispatcher) Write(_ context.Context, p ConcreteAttributePath, v AttributeValue) []WriteResult {
	s, _ := v.Value.(string)
	d.values[p.Attribute] = s
	if d.onWrite != nil {
		d.onWrite(p.Attribute, s)
	}
	return []WriteResult{{Path: p, Status: StatusSuccess}}
}

// WriteAuthorized expands a wildcard-endpoint path to endpoint 0 and skips
// a location authorize denies, as endpoint.TopologyDispatcher does.
func (d *labelDispatcher) WriteAuthorized(ctx context.Context, p ConcreteAttributePath, v AttributeValue, authorize WriteAuthorizer) []WriteResult {
	if p.HasEndpoint {
		return d.Write(ctx, p, v)
	}
	if authorize != nil && authorize(0, p.Cluster, p.Attribute) != StatusSuccess {
		return nil
	}
	p.Endpoint, p.HasEndpoint = 0, true
	return d.Write(ctx, p, v)
}

func (d *labelDispatcher) Invoke(context.Context, ConcreteCommandPath, any) InvokeResult {
	return InvokeResult{}
}

func holdAttr(attribute uint32) ConcreteAttributePath {
	return ConcreteAttributePath{Endpoint: 0, Cluster: holdBasicInformation, Attribute: attribute, HasEndpoint: true, HasCluster: true, HasAttribute: true}
}

type holdWrite struct {
	path  ConcreteAttributePath
	value string
}

// holdExchange is one exchange's write interaction.
type holdExchange struct {
	d  *labelDispatcher
	tx *WriteTransaction
}

func (x holdExchange) write(more bool, writes ...holdWrite) []StatusCode {
	req := WriteRequest{MoreChunkedMessages: more}
	for _, w := range writes {
		req.Writes = append(req.Writes, AttributeWrite{Path: w.path, Value: AttributeValue{Value: w.value}})
	}
	resp := HandleWriteRequest(WithWriteTransaction(context.Background(), x.tx), x.d, req)
	out := make([]StatusCode, 0, len(resp.Responses))
	for _, r := range resp.Responses {
		out = append(out, r.Status.Status)
	}
	return out
}

func newHoldNode() (d *labelDispatcher, exchange func() holdExchange) {
	d = &labelDispatcher{values: map[uint32]string{}}
	holds := &WriteHolds{}
	return d, func() holdExchange { return holdExchange{d: d, tx: NewWriteTransaction(holds)} }
}

func wantStatuses(t *testing.T, what string, got []StatusCode, want ...StatusCode) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("%s: statuses %v, want %v", what, got, want)
	}
}

func TestChunkedWriteHoldParityMatterJS(t *testing.T) {
	t.Parallel()
	nodeLabel, location := holdAttr(holdNodeLabel), holdAttr(holdLocation)

	// InteractionProtocolTest.ts:1605-1665.
	t.Run("answers BUSY to another exchange's write of an attribute while a chunked write holds it", func(t *testing.T) {
		t.Parallel()
		d, exchange := newHoldNode()
		holder := exchange()
		wantStatuses(t, "first chunk", holder.write(true, holdWrite{nodeLabel, "first"}), StatusSuccess)
		wantStatuses(t, "competing", exchange().write(false, holdWrite{nodeLabel, "competing"}), StatusBusy)
		wantStatuses(t, "second chunk", holder.write(false, holdWrite{nodeLabel, "second"}), StatusSuccess)
		if d.values[holdNodeLabel] != "second" {
			t.Fatalf("nodeLabel %q, want second", d.values[holdNodeLabel])
		}
		wantStatuses(t, "after", exchange().write(false, holdWrite{nodeLabel, "after"}), StatusSuccess)
		if d.values[holdNodeLabel] != "after" {
			t.Fatalf("nodeLabel %q, want after", d.values[holdNodeLabel])
		}
	})

	// InteractionProtocolTest.ts:1667-1731.
	t.Run("releases the attribute a chunked write held once the write moves to another attribute", func(t *testing.T) {
		t.Parallel()
		d, exchange := newHoldNode()
		holder := exchange()
		holder.write(true, holdWrite{nodeLabel, "first"})
		wantStatuses(t, "nodeLabel while held", exchange().write(false, holdWrite{nodeLabel, "other"}), StatusBusy)
		holder.write(true, holdWrite{location, "XX"})
		wantStatuses(t, "nodeLabel after the move", exchange().write(false, holdWrite{nodeLabel, "other"}), StatusSuccess)
		wantStatuses(t, "location while held", exchange().write(false, holdWrite{location, "ZZ"}), StatusBusy)
		holder.write(false, holdWrite{location, "YY"})
		if d.values[holdLocation] != "YY" {
			t.Fatalf("location %q, want YY", d.values[holdLocation])
		}
	})

	// InteractionProtocolTest.ts:1733-1781: between the first chunk's write
	// and its answer another exchange writes the attribute through a
	// wildcard-endpoint path; the hold makes it skip the attribute, with no
	// status.
	t.Run("skips an attribute a chunked write holds in another exchange's wildcard write", func(t *testing.T) {
		t.Parallel()
		d, exchange := newHoldNode()
		holder := exchange()
		holder.write(true, holdWrite{nodeLabel, "first"})
		wild := nodeLabel
		wild.HasEndpoint, wild.Endpoint = false, 0
		if got := exchange().write(false, holdWrite{wild, "x"}); len(got) != 0 {
			t.Fatalf("wildcard write answered %v, want no status", got)
		}
		if d.values[holdNodeLabel] != "first" {
			t.Fatalf("nodeLabel after the competing write %q, want first", d.values[holdNodeLabel])
		}
		holder.write(false, holdWrite{nodeLabel, "second"})
		if d.values[holdNodeLabel] != "second" {
			t.Fatalf("nodeLabel %q, want second", d.values[holdNodeLabel])
		}
	})

	// InteractionProtocolTest.ts:1783-1823: the competing write overlaps the
	// first chunk's write — the hold is taken before the value is applied.
	t.Run("answers BUSY to a write that overlaps another exchange's first chunk of the same attribute", func(t *testing.T) {
		t.Parallel()
		d, exchange := newHoldNode()
		holder, competing := exchange(), exchange()
		var competingStatus []StatusCode
		d.onWrite = func(attribute uint32, value string) {
			if attribute == holdNodeLabel && value == "first" {
				competingStatus = competing.write(false, holdWrite{nodeLabel, "competing"})
			}
		}
		holder.write(true, holdWrite{nodeLabel, "first"})
		holder.write(false, holdWrite{nodeLabel, "second"})
		wantStatuses(t, "competing", competingStatus, StatusBusy)
		if d.values[holdNodeLabel] != "second" {
			t.Fatalf("nodeLabel %q, want second", d.values[holdNodeLabel])
		}
	})

	// InteractionProtocolTest.ts:1825-1877: the second chunk writes nodeLabel
	// and moves on to location; nodeLabel stays held until the chunk's
	// writes are done, so a write fired from nodeLabel's change is BUSY.
	t.Run("holds an attribute until the writes of its chunk are done when the chunk moves to another one", func(t *testing.T) {
		t.Parallel()
		d, exchange := newHoldNode()
		holder, competing := exchange(), exchange()
		var competingStatus []StatusCode
		d.onWrite = func(attribute uint32, value string) {
			if attribute == holdNodeLabel && value == "second" && competingStatus == nil {
				competingStatus = competing.write(false, holdWrite{nodeLabel, "competing"})
			}
		}
		holder.write(true, holdWrite{nodeLabel, "first"})
		holder.write(true, holdWrite{nodeLabel, "second"}, holdWrite{location, "XX"})
		holder.write(false, holdWrite{location, "YY"})
		wantStatuses(t, "competing", competingStatus, StatusBusy)
		if d.values[holdNodeLabel] != "second" || d.values[holdLocation] != "YY" {
			t.Fatalf("nodeLabel %q location %q, want second and YY", d.values[holdNodeLabel], d.values[holdLocation])
		}
	})

	// InteractionProtocolTest.ts:1879-1916.
	t.Run("does not hold the attribute of a write that fits one message", func(t *testing.T) {
		t.Parallel()
		d, exchange := newHoldNode()
		first, competing := exchange(), exchange()
		var competingStatus []StatusCode
		d.onWrite = func(attribute uint32, value string) {
			if value == "first" {
				competingStatus = competing.write(false, holdWrite{nodeLabel, "competing"})
			}
		}
		first.write(false, holdWrite{nodeLabel, "first"})
		wantStatuses(t, "competing", competingStatus, StatusSuccess)
		if d.values[holdNodeLabel] != "competing" {
			t.Fatalf("nodeLabel %q, want competing", d.values[holdNodeLabel])
		}
	})
}

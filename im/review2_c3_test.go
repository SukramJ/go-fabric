// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"context"
	"errors"
	"testing"

	"thingfabric.dev/go-fabric/tlv"
)

// invokeResponseWithFields is an InvokeResponseMessage of one CommandDataIB
// whose CommandFields put writes.
func invokeResponseWithFields(t *testing.T, put func(enc *tlv.Encoder)) []byte {
	t.Helper()
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutBool(tlv.ContextTag(0), false)
	enc.StartArray(tlv.ContextTag(1))
	enc.StartStruct(tlv.AnonymousTag())
	enc.StartStruct(tlv.ContextTag(0)) // CommandDataIB
	ConcreteCommandPath{Endpoint: 1, Cluster: 6, Command: 0, HasEndpoint: true, HasCluster: true, HasCommand: true}.MarshalTLV(enc, tlv.ContextTag(0))
	put(enc)
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	_ = enc.EndContainer()
	enc.PutUint(tlv.ContextTag(0xFF), 12)
	_ = enc.EndContainer()
	b, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestInvokeResponseFieldsThatDoNotDecodeFailTheResponse (R2-C2-wire-0551):
// a command response whose CommandFields is not a structure does not
// decode; the response is an error, never an entry that reads as a Success
// without fields. A typed reject of the client's own fields reader fails it
// too: TestInvokeResponseFieldsATypedReaderRejectFailTheResponse.
func TestInvokeResponseFieldsThatDoNotDecodeFailTheResponse(t *testing.T) {
	t.Parallel()
	accept := func(ConcreteCommandPath, *tlv.Decoder, tlv.Element) (any, error) { return "fields", nil }
	reject := func(_ ConcreteCommandPath, dec *tlv.Decoder, el tlv.Element) (any, error) {
		return nil, constraintFieldsError{}
	}
	cases := []struct {
		name   string
		put    func(enc *tlv.Encoder)
		reader CommandFieldsReader
	}{
		{"a scalar", func(enc *tlv.Encoder) { enc.PutUint(tlv.ContextTag(1), 7) }, accept},
		{"an array", func(enc *tlv.Encoder) {
			enc.StartArray(tlv.ContextTag(1))
			enc.PutUint(tlv.AnonymousTag(), 7)
			_ = enc.EndContainer()
		}, accept},
		{"a list", func(enc *tlv.Encoder) {
			enc.StartList(tlv.ContextTag(1))
			enc.PutUint(tlv.ContextTag(0), 7)
			_ = enc.EndContainer()
		}, reject},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			resp, err := UnmarshalInvokeResponseTLV(tlv.NewDecoder(invokeResponseWithFields(t, tc.put)), tc.reader)
			if err == nil {
				t.Fatalf("decoded without an error: %+v", resp.Responses)
			}
			if !errors.Is(err, ErrInvalidInvokeRequest) {
				t.Errorf("err = %v, want it to wrap ErrInvalidInvokeRequest", err)
			}
		})
	}
	// The control: a structure the reader takes is a response with fields,
	// and without a reader the fields are skipped, as before.
	b := invokeResponseWithFields(t, func(enc *tlv.Encoder) {
		enc.StartStruct(tlv.ContextTag(1))
		_ = enc.EndContainer()
	})
	resp, err := UnmarshalInvokeResponseTLV(tlv.NewDecoder(b), func(_ ConcreteCommandPath, dec *tlv.Decoder, el tlv.Element) (any, error) {
		return "fields", skipContainer(dec)
	})
	if err != nil || len(resp.Responses) != 1 || !resp.Responses[0].HasResponse {
		t.Fatalf("a structure: err=%v responses=%+v", err, resp.Responses)
	}
	scalar := invokeResponseWithFields(t, func(enc *tlv.Encoder) { enc.PutUint(tlv.ContextTag(1), 7) })
	if _, err := UnmarshalInvokeResponseTLV(tlv.NewDecoder(scalar), nil); err != nil {
		t.Fatalf("no reader: %v", err)
	}
}

// TestInvokeResponseFieldsATypedReaderRejectFailTheResponse
// (R2-C2-wire-0551, decision 531): a typed reject (a [StatusCodeError]) of
// the client's fields reader for a structure CommandFields fails the
// decode with the reader's error, as matter.js's invoke throws from the
// response's decodeTlv (packages/protocol/src/action/client/
// ClientInteraction.ts:578 at b25ed960, no catch in #invokeSingle) — never
// an entry that reads as a Success without fields, never a per-command
// status. It fails every command of the response, the ones that decoded
// included (#executeBatch rejects them all, :1005-1010).
func TestInvokeResponseFieldsATypedReaderRejectFailTheResponse(t *testing.T) {
	t.Parallel()
	reject := func(_ ConcreteCommandPath, dec *tlv.Decoder, el tlv.Element) (any, error) {
		return nil, constraintFieldsError{}
	}
	structure := func(enc *tlv.Encoder) {
		enc.StartStruct(tlv.ContextTag(1))
		enc.PutUint(tlv.ContextTag(0), 7)
		_ = enc.EndContainer()
	}
	resp, err := UnmarshalInvokeResponseTLV(tlv.NewDecoder(invokeResponseWithFields(t, structure)), reject)
	if err == nil {
		t.Fatalf("decoded without an error: %+v", resp.Responses)
	}
	if !errors.Is(err, ErrInvalidInvokeRequest) {
		t.Errorf("err = %v, want it to wrap ErrInvalidInvokeRequest", err)
	}
	var sce StatusCodeError
	if !errors.As(err, &sce) || sce.MatterStatusCode() != StatusConstraintError {
		t.Errorf("err = %v, want it to carry the reader's ConstraintError", err)
	}

	// Two responses, the first decodes: the reject of the second still
	// fails the whole response.
	calls := 0
	second := func(_ ConcreteCommandPath, dec *tlv.Decoder, el tlv.Element) (any, error) {
		calls++
		if calls == 2 {
			return nil, constraintFieldsError{}
		}
		return "fields", skipContainer(dec)
	}
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutBool(tlv.ContextTag(0), false)
	enc.StartArray(tlv.ContextTag(1))
	for ref := uint64(1); ref <= 2; ref++ {
		enc.StartStruct(tlv.AnonymousTag())
		enc.StartStruct(tlv.ContextTag(0)) // CommandDataIB
		ConcreteCommandPath{Endpoint: 1, Cluster: 6, Command: 0, HasEndpoint: true, HasCluster: true, HasCommand: true}.MarshalTLV(enc, tlv.ContextTag(0))
		structure(enc)
		enc.PutUint(tlv.ContextTag(2), ref)
		_ = enc.EndContainer()
		_ = enc.EndContainer()
	}
	_ = enc.EndContainer()
	enc.PutUint(tlv.ContextTag(0xFF), 12)
	_ = enc.EndContainer()
	b, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if resp, err := UnmarshalInvokeResponseTLV(tlv.NewDecoder(b), second); err == nil {
		t.Fatalf("two responses, the second rejected: decoded without an error: %+v", resp.Responses)
	}
}

// TestConcreteEventPathWithSentinelValuesSelectsNoOtherEvent
// (R2-C2-wire-0553): a concrete EventPathIB that names endpoint 0xFFFF,
// cluster 0xFFFFFFFF or event 0xFFFFFFFF names those ids, not "any": it
// selects no event of another endpoint, cluster or event. The wildcard is
// the absent field.
func TestConcreteEventPathWithSentinelValuesSelectsNoOtherEvent(t *testing.T) {
	t.Parallel()
	log := NewEventLog()
	log.AppendRecord(EventRecord{Priority: EventPriorityInfo, Endpoint: 1, Cluster: 0x3B, EventID: 1})
	log.AppendRecord(EventRecord{Priority: EventPriorityInfo, Endpoint: 2, Cluster: 0x3B, EventID: 2})
	for _, p := range []ConcreteEventPath{
		{Endpoint: 0xFFFF, Cluster: 0x3B, Event: 1, HasEndpoint: true, HasCluster: true, HasEvent: true},
		{Endpoint: 1, Cluster: 0xFFFFFFFF, Event: 1, HasEndpoint: true, HasCluster: true, HasEvent: true},
		{Endpoint: 1, Cluster: 0x3B, Event: 0xFFFFFFFF, HasEndpoint: true, HasCluster: true, HasEvent: true},
	} {
		if reps := BuildEventReports([]ConcreteEventPath{p}, log, nil); len(reps) != 0 {
			t.Errorf("concrete path %+v selected %d events, want none", p, len(reps))
		}
	}
	// The wildcards still select: an absent endpoint, cluster, event.
	for _, tc := range []struct {
		p    ConcreteEventPath
		want int
	}{
		{ConcreteEventPath{Cluster: 0x3B, Event: 1, HasCluster: true, HasEvent: true}, 1},
		{ConcreteEventPath{Endpoint: 1, Event: 1, HasEndpoint: true, HasEvent: true}, 1},
		{ConcreteEventPath{Endpoint: 1, Cluster: 0x3B, HasEndpoint: true, HasCluster: true}, 1},
		{ConcreteEventPath{}, 2},
	} {
		if reps := BuildEventReports([]ConcreteEventPath{tc.p}, log, nil); len(reps) != tc.want {
			t.Errorf("wildcard path %+v selected %d events, want %d", tc.p, len(reps), tc.want)
		}
	}
}

// holdRaceDispatcher lets another interaction take the hold of the
// written attribute during the write's access check — the window between
// busyBefore and hold.
type holdRaceDispatcher struct {
	onACL  func()
	writes int
}

func (d *holdRaceDispatcher) Read(_ context.Context, p ConcreteAttributePath) []ReadResult {
	return []ReadResult{{Path: p, Status: StatusSuccess}}
}

func (d *holdRaceDispatcher) Write(_ context.Context, p ConcreteAttributePath, _ AttributeValue) []WriteResult {
	d.writes++
	return []WriteResult{{Path: p, Status: StatusSuccess}}
}

func (d *holdRaceDispatcher) Invoke(context.Context, ConcreteCommandPath, any) InvokeResult {
	return InvokeResult{}
}

func (d *holdRaceDispatcher) CheckACL(context.Context, uint8, uint64, []uint32, uint16, uint32, uint8) StatusCode {
	if d.onACL != nil {
		d.onACL()
		d.onACL = nil
	}
	return StatusSuccess
}

// TestSingleWriteIsBusyOnAHoldTakenSinceBusyBefore (R2-C2-conc-0554): a
// write that is not chunked is BUSY, and not applied, on an attribute
// another interaction took the hold of since busyBefore — as a chunked
// write in the same position is. It takes no hold itself.
func TestSingleWriteIsBusyOnAHoldTakenSinceBusyBefore(t *testing.T) {
	t.Parallel()
	path := ConcreteAttributePath{Endpoint: 1, Cluster: 6, Attribute: 0x4001, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	for _, more := range []bool{false, true} {
		holds := &WriteHolds{}
		other := NewWriteTransaction(holds)
		d := &holdRaceDispatcher{}
		d.onACL = func() {
			if !other.hold(path, 1, true) {
				t.Fatal("the other interaction could not take the hold")
			}
		}
		tx := NewWriteTransaction(holds)
		ctx := WithWriteTransaction(WithFabricFilter(context.Background(), false, 1), tx)
		resp := HandleWriteRequest(ctx, d, WriteRequest{MoreChunkedMessages: more, Writes: []AttributeWrite{{Path: path, Value: AttributeValue{Value: uint16(5)}}}})
		if d.writes != 0 {
			t.Errorf("more=%v: the write was applied %d times under another interaction's hold", more, d.writes)
		}
		if len(resp.Responses) != 1 || resp.Responses[0].Status.Status != StatusBusy {
			t.Errorf("more=%v: responses %+v, want one BUSY", more, resp.Responses)
		}
		if len(tx.held) != 0 {
			t.Errorf("more=%v: the refused write holds %d attributes", more, len(tx.held))
		}
	}
	// Without another holder a single write is applied and holds nothing.
	holds := &WriteHolds{}
	d := &holdRaceDispatcher{}
	tx := NewWriteTransaction(holds)
	ctx := WithWriteTransaction(WithFabricFilter(context.Background(), false, 1), tx)
	HandleWriteRequest(ctx, d, WriteRequest{Writes: []AttributeWrite{{Path: path, Value: AttributeValue{Value: uint16(5)}}}})
	if d.writes != 1 || len(tx.held) != 0 || holds.Held(1, 1, 6, 0x4001) {
		t.Errorf("an uncontended single write: writes=%d held=%d", d.writes, len(tx.held))
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/tlv"
)

// statusDispatcher answers every read with one status.
type statusDispatcher struct {
	labelDispatcher
	status StatusCode
}

func (d *statusDispatcher) Read(_ context.Context, p ConcreteAttributePath) []ReadResult {
	return []ReadResult{{Path: p, Status: d.status}}
}

// TestReachesWriteGates: a write's DataVersion is judged on an attribute
// the node serves and writes, and left to the dispatcher's own answer
// otherwise (matter.js AttributeWriteResponse.ts:221-257 at 862ad75f).
func TestReachesWriteGates(t *testing.T) {
	t.Parallel()
	const basicInformation = 0x0028
	path := func(attr uint32) ConcreteAttributePath {
		return ConcreteAttributePath{Endpoint: 0, Cluster: basicInformation, Attribute: attr, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	}
	cases := []struct {
		name   string
		attr   uint32
		status StatusCode
		want   bool
	}{
		{"a writable attribute the node serves", 5, StatusSuccess, true}, // NodeLabel
		{"a read-only attribute", 3, StatusSuccess, false},               // ProductName
		{"an attribute the node does not serve", 5, StatusUnsupportedAttribute, false},
		{"a cluster the node does not serve", 5, StatusUnsupportedCluster, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d := &statusDispatcher{labelDispatcher: labelDispatcher{values: map[uint32]string{}}, status: tc.status}
			if got := reachesWriteGates(context.Background(), d, path(tc.attr)); got != tc.want {
				t.Fatalf("reachesWriteGates = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestWriteTransactionRelease: Release ends every hold of an abandoned
// chunked write (InteractionServer.ts:469 at 862ad75f).
func TestWriteTransactionRelease(t *testing.T) {
	t.Parallel()
	holds := &WriteHolds{}
	p := ConcreteAttributePath{Cluster: 0x0028, Attribute: 5, HasEndpoint: true, HasCluster: true, HasAttribute: true}
	holder, other := NewWriteTransaction(holds), NewWriteTransaction(holds)
	if holder.heldByOther(1, p.Endpoint, p.Cluster, p.Attribute) {
		t.Fatal("the first hold was refused")
	}
	if !holder.hold(p, 1, true) {
		t.Fatal("the first hold was refused")
	}
	if !other.heldByOther(1, p.Endpoint, p.Cluster, p.Attribute) {
		t.Fatal("the attribute is not held for the other exchange")
	}
	holder.Release()
	if other.heldByOther(1, p.Endpoint, p.Cluster, p.Attribute) {
		t.Fatal("the attribute stays held after Release")
	}
}

// TestWriteRequestSuppressResponseType: a SuppressResponse that is not a
// boolean fails the decode, and the request is answered
// (SuppressResponseTest.ts:399-403 at 862ad75f, UNREADABLE_SUPPRESS_RESPONSE).
func TestWriteRequestSuppressResponseType(t *testing.T) {
	t.Parallel()
	enc := tlv.NewEncoder()
	enc.StartStruct(tlv.AnonymousTag())
	enc.PutUint(tlv.ContextTag(0), 1)
	_ = enc.EndContainer()
	data, err := enc.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UnmarshalWriteRequestTLV(tlv.NewDecoder(data), nil); !errors.Is(err, ErrInvalidWriteRequest) {
		t.Fatalf("err = %v, want ErrInvalidWriteRequest", err)
	}
}

// TestWriteRequestDecodeRefusals: the WriteRequest decoder refuses what
// TlvWriteRequest.ts:16-24 at 862ad75f does not describe — a top level that
// is no struct, a WriteRequests that is no array, an AttributeDataIB that
// is no struct, a message cut short — and skips a field it does not know.
func TestWriteRequestDecodeRefusals(t *testing.T) {
	t.Parallel()
	encode := func(t *testing.T, body func(enc *tlv.Encoder)) []byte {
		t.Helper()
		enc := tlv.NewEncoder()
		body(enc)
		data, err := enc.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	cases := []struct {
		name    string
		body    func(enc *tlv.Encoder)
		cut     int
		refused bool
	}{
		{"a top level that is no struct", func(enc *tlv.Encoder) { enc.PutBool(tlv.AnonymousTag(), true) }, 0, true},
		{"a WriteRequests that is no array", func(enc *tlv.Encoder) {
			enc.StartStruct(tlv.AnonymousTag())
			enc.PutBool(tlv.ContextTag(2), true)
			_ = enc.EndContainer()
		}, 0, true},
		{"an AttributeDataIB that is no struct", func(enc *tlv.Encoder) {
			enc.StartStruct(tlv.AnonymousTag())
			enc.StartArray(tlv.ContextTag(2))
			enc.PutBool(tlv.AnonymousTag(), true)
			_ = enc.EndContainer()
			_ = enc.EndContainer()
		}, 0, true},
		{"a message cut short", func(enc *tlv.Encoder) {
			enc.StartStruct(tlv.AnonymousTag())
			enc.PutBool(tlv.ContextTag(1), true)
			_ = enc.EndContainer()
		}, 2, true},
		{"a field it does not know", func(enc *tlv.Encoder) {
			enc.StartStruct(tlv.AnonymousTag())
			enc.StartStruct(tlv.ContextTag(9))
			enc.PutBool(tlv.ContextTag(0), true)
			_ = enc.EndContainer()
			enc.PutBool(tlv.AnonymousTag(), true)
			_ = enc.EndContainer()
		}, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := encode(t, tc.body)
			data = data[:len(data)-tc.cut]
			_, err := UnmarshalWriteRequestTLV(tlv.NewDecoder(data), nil)
			if (err != nil) != tc.refused {
				t.Fatalf("err = %v, refused %v", err, tc.refused)
			}
		})
	}
}

// TestWriteRequestWithoutClusterOrAttribute: a write whose path names no
// cluster or no attribute is answered INVALID_ACTION on that path and the
// request goes on (chip WriteHandler.cpp, "Wildcard path write must specify
// an attributeId"; see HandleWriteRequest).
func TestWriteRequestWithoutClusterOrAttribute(t *testing.T) {
	t.Parallel()
	d := &labelDispatcher{values: map[uint32]string{}}
	req := WriteRequest{Writes: []AttributeWrite{{Path: ConcreteAttributePath{Endpoint: 0, HasEndpoint: true}}}}
	resp := HandleWriteRequest(context.Background(), d, req)
	if len(resp.Responses) != 1 || resp.Responses[0].Status.Status != StatusInvalidAction {
		t.Fatalf("responses %+v, want one INVALID_ACTION", resp.Responses)
	}
}

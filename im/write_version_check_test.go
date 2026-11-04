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

// TestVersionCheckApplies: a write's DataVersion is judged on an attribute
// the node serves and writes, and left to the dispatcher's own answer
// otherwise (matter.js AttributeWriteResponse.ts:221-257 at 862ad75f).
func TestVersionCheckApplies(t *testing.T) {
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
			if got := versionCheckApplies(context.Background(), d, path(tc.attr)); got != tc.want {
				t.Fatalf("versionCheckApplies = %v, want %v", got, tc.want)
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
	if !holder.holdUnlessBusy(p, true) {
		t.Fatal("the first hold was refused")
	}
	if !other.heldByOther(p.Endpoint, p.Cluster, p.Attribute) {
		t.Fatal("the attribute is not held for the other exchange")
	}
	holder.Release()
	if other.heldByOther(p.Endpoint, p.Cluster, p.Attribute) {
		t.Fatal("the attribute stays held after Release")
	}
}

// TestWriteRequestSuppressResponseType: a SuppressResponse that is not a
// boolean fails the decode (TlvWriteRequest.ts:18 TlvOptionalField(0,
// TlvBoolean); SuppressResponseTest.ts:399-403 at 862ad75f).
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

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"bytes"
	"reflect"
	"testing"

	"thingfabric.dev/go-fabric/cluster/spec"
	acldef "thingfabric.dev/go-fabric/cluster/spec/accesscontrol"
	userlabeldef "thingfabric.dev/go-fabric/cluster/spec/userlabel"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/tlv"
)

// listAppendCluster is a vendor cluster with two writable lists of
// scalars, a uint8 list (0) and an octet-string list (1), each with its
// definition's decoder, as a generated or hand-written definition states
// it.
var listAppendCluster = func() *spec.Cluster {
	rw := spec.Access{RW: "RW"}
	m := spec.Conformance{Text: "M", Op: spec.ConfMandatory}
	u8 := spec.Type{Name: "uint8", Kind: spec.KindUint, Bits: 8}
	octets := spec.Type{Name: "octstr", Kind: spec.KindBytes}
	return &spec.Cluster{ID: 0xFFF1FC11, Name: "ListAppend", Revision: 1, Attributes: []spec.Attribute{
		{
			ID: 0, Name: "Numbers", Type: spec.Type{Name: "list", Kind: spec.KindList, Entry: &spec.Field{Type: u8}},
			Conformance: m, Access: rw, Decode: func(n spec.Node) (any, error) { return spec.DecodeList(spec.DecodeUint[uint8](8))(n) },
		},
		{
			ID: 1, Name: "Blobs", Type: spec.Type{Name: "list", Kind: spec.KindList, Entry: &spec.Field{Type: octets}},
			Conformance: m, Access: rw, Decode: func(n spec.Node) (any, error) { return spec.DecodeList(spec.DecodeBytes)(n) },
		},
	}}
}()

// TestAttributeValueReader_ListAppendOfAScalarEntry: an entry appended
// (null ListIndex) to a list of scalars a registered definition decodes is
// decoded with that definition — the list's entry type — into a one-entry
// list of the type the definition decodes the whole list into, the type
// a REPLACE stored and the dispatcher's appendListValue requires; an entry
// the entry type refuses is refused. Before, it reached the dispatcher as
// []any and every append answered CONSTRAINT_ERROR (P2b-F, TC-IDM-3.1
// step 13). Mirrors matter.js
// packages/protocol/src/action/server/AttributeWriteResponse.ts:446-460 at
// b25ed960 (an ADD decoded with the list schema's elementSchema, :455).
func TestAttributeValueReader_ListAppendOfAScalarEntry(t *testing.T) {
	t.Parallel()
	spec.Register(listAppendCluster)
	read := func(t *testing.T, attr uint32, put func(*tlv.Encoder)) im.AttributeValue {
		t.Helper()
		enc := tlv.NewEncoder()
		put(enc)
		b, err := enc.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		dec := tlv.NewDecoder(b)
		path := im.ConcreteAttributePath{HasCluster: true, HasAttribute: true, Cluster: listAppendCluster.ID, Attribute: attr, ListAppend: true}
		v, err := attributeValueReader(path, advanceToContent(t, dec), dec)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	t.Run("uint8", func(t *testing.T) {
		t.Parallel()
		v := read(t, 0, func(e *tlv.Encoder) { e.PutUint(tlv.AnonymousTag(), 7) })
		if got, ok := v.Value.([]uint8); !ok || !reflect.DeepEqual(got, []uint8{7}) {
			t.Fatalf("appended uint8 decoded as %T %v, want []uint8{7}", v.Value, v.Value)
		}
	})
	t.Run("octet string", func(t *testing.T) {
		t.Parallel()
		v := read(t, 1, func(e *tlv.Encoder) { e.PutOctets(tlv.AnonymousTag(), []byte{0xde, 0xad}) })
		if got, ok := v.Value.([][]byte); !ok || len(got) != 1 || !bytes.Equal(got[0], []byte{0xde, 0xad}) {
			t.Fatalf("appended octet string decoded as %T %v, want [][]byte{dead}", v.Value, v.Value)
		}
	})
	t.Run("an entry its type refuses", func(t *testing.T) {
		t.Parallel()
		v := read(t, 0, func(e *tlv.Encoder) { e.PutUint(tlv.AnonymousTag(), 300) })
		if _, ok := v.Value.([]uint8); ok {
			t.Fatalf("an appended 300 to a uint8 list decoded as %v, want a refusal", v.Value)
		}
	})
}

// TestScalarAppendedToAStructListEndsTheWrite: a scalar appended (null
// ListIndex) to a list of structures — the ACL and a LabelList, decoded
// from a whole WriteRequest as the bridge receives it, after the REPLACE of
// the list — is refused by the list's decoder as the wrong type: an
// undecodable value, which ends the write interaction with FAILURE (owner
// decision 161; im valueGateFor). matter.js's element schema throws a
// non-status error for another TLV type (packages/types/src/tlv/TlvObject.ts:205-207
// at b25ed960), which writeValue rethrows
// (packages/protocol/src/action/server/AttributeWriteResponse.ts:463-470).
// Before P2b-F the entry reached the dispatcher as []any and answered
// CONSTRAINT_ERROR on its path (measured over a node, P2b-F report).
func TestScalarAppendedToAStructListEndsTheWrite(t *testing.T) {
	t.Parallel()
	for name, list := range map[string]im.ConcreteAttributePath{
		"ACL":       {Endpoint: 0, Cluster: acldef.ClusterID, Attribute: acldef.AttrAcl, HasEndpoint: true, HasCluster: true, HasAttribute: true},
		"LabelList": {Endpoint: 1, Cluster: userlabeldef.ClusterID, Attribute: userlabeldef.AttrLabelList, HasEndpoint: true, HasCluster: true, HasAttribute: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			add := list
			add.ListAppend = true
			b := writeRequestBytes(t, im.WriteRequest{Writes: []im.AttributeWrite{
				{Path: list, Value: im.AttributeValue{Value: spec.Node{El: tlv.Element{Type: tlv.TypeArray, IsContainer: true}}}},
				{Path: add, Value: im.AttributeValue{Value: spec.Node{El: tlv.Element{Type: tlv.TypeUnsignedInt1, Uint: 5}}}},
			}})
			req, err := im.UnmarshalWriteRequestTLV(tlv.NewDecoder(b), attributeValueReader)
			if err != nil {
				t.Fatal(err)
			}
			if len(req.Writes) != 2 || !req.Writes[1].Path.ListAppend {
				t.Fatalf("decoded %d writes (%+v), want the REPLACE and the ADD", len(req.Writes), req.Writes)
			}
			if !im.IsUndecodable(req.Writes[1].Value) {
				t.Fatalf("the appended scalar decoded as %T %v, want an undecodable value", req.Writes[1].Value.Value, req.Writes[1].Value.Value)
			}
		})
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package main

import (
	"bytes"

	"thingfabric.dev/go-fabric/cluster/spec"
	"thingfabric.dev/go-fabric/tlv"
)

// The UnitTesting cluster (--unit-testing, TESTING ONLY): the attributes
// of CHIP's test cluster that the data-type steps of TC-IDM-3.1 and
// TC-IDM-4.1 write, served on the Aggregator (endpoint 1) so the Go-TH
// leg addresses it where matter.js's all-clusters test app serves it, and
// so TC-IDM-2.1 step 21 finds a manufacturer-specific cluster. It is CHIP's
// own test cluster and in no Matter specification, so it is stated here by
// hand through cluster/spec ([spec.Register], a vendor-prefixed id) and not
// in the schema snapshot; the server is [spec.NewServer]'s, which stores
// what a write carries, as matter.js's does.
//
// Mirrors matter.js support/chip-testing/src/cert/unit-testing-cluster.ts:63-107
// at db91886c (UNIT_TESTING_ENDPOINT 1, LONG_OCTET_STRING_ENTRY,
// UnitTestingServer and its State), served unconditionally on the
// all-clusters endpoint 1 there (src/AllClustersTestInstance.ts:1198);
// here behind the flag, the daemon serving no test cluster by default
// (owner decisions 337, 354).
const (
	unitTestingClusterID uint32 = 0xFFF1FC05

	unitTestingAttrFloatSingle         uint32 = 0x17
	unitTestingAttrOctetString         uint32 = 0x19
	unitTestingAttrStructAttr          uint32 = 0x25
	unitTestingAttrListLongOctetString uint32 = 0x2A
)

// unitTestingLongOctetStringEntry is the one value each entry of
// listLongOctetString holds on a chip TH: "0123456789abcdef" 32 times, 512
// bytes (unit-testing-cluster.ts:66-70 at db91886c).
var unitTestingLongOctetStringEntry = bytes.Repeat([]byte("0123456789abcdef"), 32)

// unitTestingSimpleStruct is CHIP's SimpleStruct without the optional
// field 8 (unit-testing-cluster.ts:35-61 at db91886c): every field is
// optional — matter.js's @field states no conformance — so an absent one
// stays absent through a write and its read-back.
type unitTestingSimpleStruct struct {
	A *uint8
	B *bool
	C *uint8 // SimpleEnum
	D *[]byte
	E *string
	F *uint8 // CHIP's SimpleBitmap, as the integer it is on the wire (:52)
	G *float32
	H *float64
}

// EncodeTLV implements spec.Encodable.
func (v unitTestingSimpleStruct) EncodeTLV(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartStruct(tag)
	spec.PutOptional(enc, tlv.ContextTag(0), v.A, spec.PutUint[uint8])
	spec.PutOptional(enc, tlv.ContextTag(1), v.B, spec.PutBool)
	spec.PutOptional(enc, tlv.ContextTag(2), v.C, spec.PutUint[uint8])
	spec.PutOptional(enc, tlv.ContextTag(3), v.D, spec.PutBytes)
	spec.PutOptional(enc, tlv.ContextTag(4), v.E, spec.PutString)
	spec.PutOptional(enc, tlv.ContextTag(5), v.F, spec.PutUint[uint8])
	spec.PutOptional(enc, tlv.ContextTag(6), v.G, spec.PutFloat32)
	spec.PutOptional(enc, tlv.ContextTag(7), v.H, spec.PutFloat64)
	_ = enc.EndContainer()
}

// DecodeTLV decodes the structure n into v.
func (v *unitTestingSimpleStruct) DecodeTLV(n spec.Node) error {
	return spec.DecodeFields(n, "SimpleStruct",
		spec.Bind(0, "a", false, &v.A, spec.DecodeOptional(spec.DecodeUint[uint8](8))),
		spec.Bind(1, "b", false, &v.B, spec.DecodeOptional(spec.DecodeBool)),
		spec.Bind(2, "c", false, &v.C, spec.DecodeOptional(spec.DecodeUint[uint8](8))),
		spec.Bind(3, "d", false, &v.D, spec.DecodeOptional(spec.DecodeBytes)),
		spec.Bind(4, "e", false, &v.E, spec.DecodeOptional(spec.DecodeString)),
		spec.Bind(5, "f", false, &v.F, spec.DecodeOptional(spec.DecodeUint[uint8](8))),
		spec.Bind(6, "g", false, &v.G, spec.DecodeOptional(spec.DecodeFloat32)),
		spec.Bind(7, "h", false, &v.H, spec.DecodeOptional(spec.DecodeFloat64)),
	)
}

// unitTestingDefinition is the cluster's definition, one value for the
// process: [spec.Register] takes the same pointer again as a no-op.
var unitTestingDefinition = unitTestingCluster()

// unitTestingCluster states the cluster (unit-testing-cluster.ts:83-106 at
// db91886c): revision 1 (no ClusterRevision stated), no features, commands
// or events, four mandatory attributes, each "writable" — access RW with
// no privilege stated, so View to read and Operate to write — and not
// nonvolatile.
func unitTestingCluster() *spec.Cluster {
	mandatory := spec.Conformance{Text: "M", Op: spec.ConfMandatory}
	rw := spec.Access{RW: "RW"}
	u8 := spec.Type{Name: "uint8", Kind: spec.KindUint, Bits: 8}
	octets := spec.Type{Name: "octstr", Kind: spec.KindBytes}
	simpleEnum := &spec.Enum{Name: "SimpleEnum", Bits: 8, Values: []spec.EnumValue{
		{Value: 0, Name: "Unspecified"}, {Value: 1, Name: "ValueA"}, {Value: 2, Name: "ValueB"}, {Value: 3, Name: "ValueC"},
	}}
	simpleStruct := &spec.Struct{Name: "SimpleStruct", Fields: []spec.Field{
		{ID: 0, Name: "a", Type: u8},
		{ID: 1, Name: "b", Type: spec.Type{Name: "bool", Kind: spec.KindBool}},
		{ID: 2, Name: "c", Type: spec.Type{Name: "SimpleEnum", Kind: spec.KindEnum, Bits: 8, Enum: simpleEnum}},
		{ID: 3, Name: "d", Type: octets},
		{ID: 4, Name: "e", Type: spec.Type{Name: "string", Kind: spec.KindString}},
		{ID: 5, Name: "f", Type: u8},
		{ID: 6, Name: "g", Type: spec.Type{Name: "single", Kind: spec.KindFloat32}},
		{ID: 7, Name: "h", Type: spec.Type{Name: "double", Kind: spec.KindFloat64}},
	}}
	return &spec.Cluster{
		ID: unitTestingClusterID, Name: "UnitTesting", Revision: 1,
		Attributes: []spec.Attribute{
			{
				ID: unitTestingAttrFloatSingle, Name: "floatSingle", Type: spec.Type{Name: "single", Kind: spec.KindFloat32},
				Conformance: mandatory, Access: rw,
			},
			{
				ID: unitTestingAttrOctetString, Name: "octetString", Type: octets,
				Conformance: mandatory, Access: rw,
			},
			{
				ID: unitTestingAttrStructAttr, Name: "structAttr", Type: spec.Type{Name: "SimpleStruct", Kind: spec.KindStruct, Struct: simpleStruct},
				Conformance: mandatory, Access: rw,
				Decode: func(n spec.Node) (any, error) { return spec.DecodeStruct[unitTestingSimpleStruct](n) },
			},
			{
				ID: unitTestingAttrListLongOctetString, Name: "listLongOctetString",
				Type:        spec.Type{Name: "list", Kind: spec.KindList, Entry: &spec.Field{Type: octets}},
				Conformance: mandatory, Access: rw,
				Decode: func(n spec.Node) (any, error) { return spec.DecodeList(spec.DecodeBytes)(n) },
			},
		},
	}
}

// newUnitTesting registers the definition and returns its server holding
// matter.js's initial state (unit-testing-cluster.ts:91-106 at db91886c):
// floatSingle 0, octetString empty, structAttr with every field at its
// zero value, listLongOctetString four long entries. Every value is
// Initial: a definition's Default renders a number only as a float64 and
// no octet string or list at all (cluster/spec defaultOf), and an
// attribute without a value is absent from reads and subscriptions.
func newUnitTesting() (*spec.Server, error) {
	spec.Register(unitTestingDefinition)
	zero8, no, empty, none := uint8(0), false, []byte{}, ""
	g, h := float32(0), float64(0)
	entries := make([][]byte, 4)
	for i := range entries {
		entries[i] = unitTestingLongOctetStringEntry
	}
	return spec.NewServer(unitTestingDefinition, spec.Options{}, spec.ServerConfig{Initial: map[uint32]any{
		unitTestingAttrFloatSingle: float32(0),
		unitTestingAttrOctetString: []byte{},
		unitTestingAttrStructAttr: unitTestingSimpleStruct{
			A: &zero8, B: &no, C: &zero8, D: &empty, E: &none, F: &zero8, G: &g, H: &h,
		},
		unitTestingAttrListLongOctetString: entries,
	}})
}

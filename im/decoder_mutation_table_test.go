// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package im

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/SukramJ/go-fabric/tlv"
)

// The Interaction Model message decoders, request and response side, held
// by generated negative tables (R2-P5-tmut-13): one well-formed message per
// decoder is parsed into a TLV tree and varied element by element, and
// each variant is held to a property every decoder of this package keeps.
//
//   - truncated: every strict prefix of the message is refused, and the
//     error keeps the TLV decoder's cause;
//   - unknown member: a member under a context tag the decoder does not
//     know, of any shape, inserted at any place of any structure or list
//     of the message, is skipped whole — the decode equals the message's;
//   - foreign member: a member under a profile tag inserted likewise is
//     refused or skipped, never read;
//   - wrong type: an element replaced by one of another TLV type is
//     refused or ignored — the decode then equals the message's with that
//     element removed — never read as a value.
//
// An attribute value, an event's data and a command's fields are the
// readers' (the cluster's), not the message decoder's: the test's readers
// drain them and return a constant, and their elements are not varied.

// tnode is one TLV element and, for a container, its members.
type tnode struct {
	tag  tlv.Tag
	el   tlv.Element
	kids []*tnode
}

// dataMarker opens every reader-owned value of the test messages.
const dataMarker = "@@reader-owned"

func parseTLVTree(t *testing.T, b []byte) *tnode {
	t.Helper()
	dec := tlv.NewDecoder(b)
	n, err := parseTLVNode(dec)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if dec.Remaining() != 0 {
		t.Fatalf("parse: %d bytes after the message", dec.Remaining())
	}
	return n
}

func parseTLVNode(dec *tlv.Decoder) (*tnode, error) {
	el, err := dec.Next()
	if err != nil {
		return nil, err
	}
	n := &tnode{tag: el.Tag, el: el}
	if !el.IsContainer {
		return n, nil
	}
	for {
		mark := *dec
		end, err := dec.Next()
		if err != nil {
			return nil, err
		}
		if end.IsEndContainer {
			return n, nil
		}
		*dec = mark
		kid, err := parseTLVNode(dec)
		if err != nil {
			return nil, err
		}
		n.kids = append(n.kids, kid)
	}
}

func (n *tnode) encode(e *tlv.Encoder) {
	switch el := n.el; {
	case el.Type == tlv.TypeStructure, el.Type == tlv.TypeArray, el.Type == tlv.TypeList:
		switch el.Type {
		case tlv.TypeStructure:
			e.StartStruct(n.tag)
		case tlv.TypeArray:
			e.StartArray(n.tag)
		default:
			e.StartList(n.tag)
		}
		for _, k := range n.kids {
			k.encode(e)
		}
		_ = e.EndContainer()
	case el.Type <= tlv.TypeSignedInt8:
		e.PutInt(n.tag, el.Int)
	case el.Type <= tlv.TypeUnsignedInt8:
		e.PutUintWidth(n.tag, el.Uint, 1<<(el.Type-tlv.TypeUnsignedInt1))
	case el.Type == tlv.TypeBoolFalse, el.Type == tlv.TypeBoolTrue:
		e.PutBool(n.tag, el.Type == tlv.TypeBoolTrue)
	case el.Type == tlv.TypeFloat4:
		e.PutFloat32(n.tag, float32(el.Float))
	case el.Type == tlv.TypeFloat8:
		e.PutFloat64(n.tag, el.Float)
	case el.Type >= tlv.TypeUTF8Str1 && el.Type <= tlv.TypeUTF8Str8:
		e.PutUTF8(n.tag, el.String)
	case el.Type >= tlv.TypeOctetStr1 && el.Type <= tlv.TypeOctetStr8:
		e.PutOctets(n.tag, el.Octets)
	default:
		e.PutNull(n.tag)
	}
}

func (n *tnode) bytes(t *testing.T) []byte {
	t.Helper()
	e := tlv.NewEncoder()
	n.encode(e)
	b, err := e.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func (n *tnode) clone() *tnode {
	c := &tnode{tag: n.tag, el: n.el}
	for _, k := range n.kids {
		c.kids = append(c.kids, k.clone())
	}
	return c
}

// at returns the node path names, an index per level below the root.
func (n *tnode) at(path []int) *tnode {
	for _, i := range path {
		n = n.kids[i]
	}
	return n
}

// readerOwned reports whether n is a value the test's readers own.
func (n *tnode) readerOwned() bool {
	return n.el.IsContainer && len(n.kids) > 0 && n.kids[0].el.String == dataMarker
}

// positions lists the path of every element below the root that a
// variant may touch: every element but the readers' values and what they
// hold.
func (n *tnode) positions() [][]int {
	var out [][]int
	var walk func(n *tnode, path []int)
	walk = func(n *tnode, path []int) {
		for i, k := range n.kids {
			p := append(append([]int(nil), path...), i)
			if k.readerOwned() {
				continue
			}
			out = append(out, p)
			walk(k, p)
		}
	}
	walk(n, nil)
	return out
}

// kind classes of TLV elements, each with a well-formed sample under a tag.
var tlvKinds = []struct {
	name   string
	is     func(tlv.ElementType) bool
	sample func(tag tlv.Tag) *tnode
}{
	{
		"a boolean", func(t tlv.ElementType) bool { return t == tlv.TypeBoolFalse || t == tlv.TypeBoolTrue },
		func(tag tlv.Tag) *tnode { return &tnode{tag: tag, el: tlv.Element{Type: tlv.TypeBoolTrue, Bool: true}} },
	},
	{
		"an unsigned integer", func(t tlv.ElementType) bool { return t >= tlv.TypeUnsignedInt1 && t <= tlv.TypeUnsignedInt8 },
		func(tag tlv.Tag) *tnode {
			return &tnode{tag: tag, el: tlv.Element{Type: tlv.TypeUnsignedInt1, Uint: 1}}
		},
	},
	{
		"a signed integer", func(t tlv.ElementType) bool { return t <= tlv.TypeSignedInt8 },
		func(tag tlv.Tag) *tnode { return &tnode{tag: tag, el: tlv.Element{Type: tlv.TypeSignedInt1, Int: 1}} },
	},
	{
		"a float", func(t tlv.ElementType) bool { return t == tlv.TypeFloat4 || t == tlv.TypeFloat8 },
		func(tag tlv.Tag) *tnode { return &tnode{tag: tag, el: tlv.Element{Type: tlv.TypeFloat8, Float: 1}} },
	},
	{
		"a character string", func(t tlv.ElementType) bool { return t >= tlv.TypeUTF8Str1 && t <= tlv.TypeUTF8Str8 },
		func(tag tlv.Tag) *tnode {
			return &tnode{tag: tag, el: tlv.Element{Type: tlv.TypeUTF8Str1, String: "x"}}
		},
	},
	{
		"an octet string", func(t tlv.ElementType) bool { return t >= tlv.TypeOctetStr1 && t <= tlv.TypeOctetStr8 },
		func(tag tlv.Tag) *tnode {
			return &tnode{tag: tag, el: tlv.Element{Type: tlv.TypeOctetStr1, Octets: []byte{1}}}
		},
	},
	{
		"a structure", func(t tlv.ElementType) bool { return t == tlv.TypeStructure },
		func(tag tlv.Tag) *tnode {
			return &tnode{tag: tag, el: tlv.Element{Type: tlv.TypeStructure, IsContainer: true}, kids: []*tnode{
				{tag: tlv.ContextTag(0), el: tlv.Element{Type: tlv.TypeUnsignedInt1, Uint: 1}},
			}}
		},
	},
	{
		"an array", func(t tlv.ElementType) bool { return t == tlv.TypeArray },
		func(tag tlv.Tag) *tnode {
			return &tnode{tag: tag, el: tlv.Element{Type: tlv.TypeArray, IsContainer: true}, kids: []*tnode{
				{tag: tlv.AnonymousTag(), el: tlv.Element{Type: tlv.TypeUnsignedInt1, Uint: 1}},
			}}
		},
	},
	{
		"a list", func(t tlv.ElementType) bool { return t == tlv.TypeList },
		func(tag tlv.Tag) *tnode {
			return &tnode{tag: tag, el: tlv.Element{Type: tlv.TypeList, IsContainer: true}, kids: []*tnode{
				{tag: tlv.ContextTag(1), el: tlv.Element{Type: tlv.TypeUnsignedInt1, Uint: 1}},
			}}
		},
	},
}

// unknownMembers are members under a context tag no decoder knows (200),
// of every shape, nested containers included.
func unknownMembers() map[string]*tnode {
	tag := tlv.ContextTag(200)
	nested := &tnode{tag: tag, el: tlv.Element{Type: tlv.TypeStructure, IsContainer: true}, kids: []*tnode{
		{tag: tlv.ContextTag(0), el: tlv.Element{Type: tlv.TypeStructure, IsContainer: true}, kids: []*tnode{
			{tag: tlv.ContextTag(1), el: tlv.Element{Type: tlv.TypeArray, IsContainer: true}, kids: []*tnode{
				{tag: tlv.AnonymousTag(), el: tlv.Element{Type: tlv.TypeUnsignedInt1, Uint: 7}},
			}},
		}},
		{tag: tlv.ContextTag(2), el: tlv.Element{Type: tlv.TypeList, IsContainer: true}},
	}}
	out := map[string]*tnode{"nested containers": nested}
	for _, k := range tlvKinds {
		out[k.name] = k.sample(tag)
	}
	return out
}

// decoderCase is one decoder and its well-formed message.
type decoderCase struct {
	name   string
	msg    func() []byte
	decode func(b []byte) (any, error)
}

// refusesForeign names the decoders that refuse a member under a profile
// tag — the request side (R2-P6-wire-0504, H-5); the response side skips
// one.
var refusesForeign = map[string]bool{
	"ReadRequest": true, "SubscribeRequest": true, "WriteRequest": true, "InvokeRequest": true, "TimedRequest": true,
	"AttributePath": true, "CommandPath": true, "StatusIB": true, "StatusResponse": true,
}

// refusingHolders are the containers of a response that refuse a member
// under a profile tag all the same: the event and command paths, the
// CommandDataIB, whose decoders the request side shares.
// Measured at HEAD; the other containers of a response skip one.
var refusingHolders = map[string]map[string]bool{
	"ReportData":     {"/2/[0]/1/0": true, "/2/[1]/0/0": true},
	"InvokeResponse": {"/1/[0]/0": true, "/1/[0]/0/0": true, "/1/[1]/1/0": true},
}

func drainValue(_ ConcreteAttributePath, el tlv.Element, dec *tlv.Decoder) (AttributeValue, error) {
	if el.IsContainer {
		if err := skipContainer(dec); err != nil {
			return AttributeValue{}, err
		}
	}
	return AttributeValue{Value: "v"}, nil
}

func drainEvent(_ ConcreteEventPath, el tlv.Element, dec *tlv.Decoder) (AttributeValue, error) {
	return drainValue(ConcreteAttributePath{}, el, dec)
}

func drainFields(_ ConcreteCommandPath, dec *tlv.Decoder, el tlv.Element) (any, error) {
	if el.IsContainer {
		if err := skipContainer(dec); err != nil {
			return nil, err
		}
	}
	return "f", nil
}

func writeMarker(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartStruct(tag)
	enc.PutUTF8(tlv.ContextTag(0), dataMarker)
	_ = enc.EndContainer()
}

func markerValue(enc *tlv.Encoder, tag tlv.Tag, _ AttributeValue) { writeMarker(enc, tag) }

func markerFields(enc *tlv.Encoder, tag tlv.Tag, _ any) { writeMarker(enc, tag) }

// encodeWith encodes a test message; a test message always encodes.
func encodeWith(f func(*tlv.Encoder)) []byte {
	e := tlv.NewEncoder()
	f(e)
	b, err := e.Bytes()
	if err != nil {
		panic(err)
	}
	return b
}

// withMembers appends members to the container at path of the encoded
// message b — fields the encoders of this package do not write.
func withMembers(b []byte, path []int, add ...*tnode) []byte {
	root, err := parseTLVNode(tlv.NewDecoder(b))
	if err != nil {
		panic(err)
	}
	h := root.at(path)
	h.kids = append(h.kids, add...)
	return encodeWith(root.encode)
}

func uintMember(tag uint8, v uint64) *tnode {
	return &tnode{tag: tlv.ContextTag(tag), el: tlv.Element{Type: tlv.TypeUnsignedInt8, Uint: v}}
}

func attrPathSamples() []ConcreteAttributePath {
	return []ConcreteAttributePath{
		{Node: 0x1122, Endpoint: 1, Cluster: 0x0006, Attribute: 0, ListIndex: 3, HasNode: true, HasEndpoint: true, HasCluster: true, HasAttribute: true, HasListIndex: true},
		{Cluster: 0x0028, HasCluster: true, WildcardPathFlags: 0x7, WildcardFilterConfigurationVersion: 9, HasWildcardFilterConfigurationVersion: true},
		{Endpoint: 2, Cluster: 0x001F, Attribute: 0, HasEndpoint: true, HasCluster: true, HasAttribute: true, HasListIndex: true, ListAppend: true},
	}
}

func eventPathSamples() []ConcreteEventPath {
	return []ConcreteEventPath{
		{Node: 0x1122, Endpoint: 1, Cluster: 0x0028, Event: 0, HasNode: true, HasEndpoint: true, HasCluster: true, HasEvent: true, IsUrgent: true},
		{Cluster: 0x0028, HasCluster: true},
	}
}

func decoderCases() []decoderCase {
	cmd := ConcreteCommandPath{Endpoint: 1, Cluster: 0x0006, Command: 2, HasEndpoint: true, HasCluster: true, HasCommand: true}
	filters := []EventMinimumNumber{{NodeID: 0x1122, HasNodeID: true, EventMin: 5}, {EventMin: 9}}
	dvf := []DataVersionFilter{{Endpoint: 1, Cluster: 6, DataVersion: 77, NodeID: 0x1122, HasNodeID: true}, {Endpoint: 2, Cluster: 8, DataVersion: 1}}
	cases := []decoderCase{
		{"ReadRequest", func() []byte {
			return encodeWith(ReadRequest{AttributeRequests: attrPathSamples(), EventRequests: eventPathSamples(), EventFilters: filters, FabricFiltered: true, DataVersionFilters: dvf}.MarshalTLV)
		}, func(b []byte) (any, error) { return UnmarshalReadRequestTLV(tlv.NewDecoder(b)) }},
		{"SubscribeRequest", func() []byte {
			return encodeWith(SubscribeRequest{KeepSubscriptions: true, MinIntervalFloor: 1, MaxIntervalCeiling: 60, AttributeRequests: attrPathSamples(), EventRequests: eventPathSamples(), EventFilters: filters, FabricFiltered: true, DataVersionFilters: dvf}.MarshalTLV)
		}, func(b []byte) (any, error) { return UnmarshalSubscribeRequestTLV(tlv.NewDecoder(b)) }},
		{"WriteRequest", func() []byte {
			return encodeWith(func(e *tlv.Encoder) {
				WriteRequest{SuppressResponse: true, TimedRequest: true, MoreChunkedMessages: true, Writes: []AttributeWrite{
					{Path: attrPathSamples()[0], DataVersion: 4, HasDataVersion: true},
					{Path: attrPathSamples()[2]},
				}}.MarshalTLV(e, markerValue)
			})
		}, func(b []byte) (any, error) { return UnmarshalWriteRequestTLV(tlv.NewDecoder(b), drainValue) }},
		{"InvokeRequest", func() []byte {
			b := encodeWith(func(e *tlv.Encoder) {
				MarshalInvokeRequestTLV(e, InvokeRequest{SuppressResponse: true, TimedRequest: true, Invokes: []CommandInvocation{
					{Path: cmd, Fields: 1, CommandRef: 1, HasCommandRef: true},
					{Path: ConcreteCommandPath{Endpoint: 2, Cluster: 0x0008, Command: 0, HasEndpoint: true, HasCluster: true, HasCommand: true}, Fields: 1, CommandRef: 2, HasCommandRef: true},
				}}, markerFields)
			})
			return withMembers(b, nil, &tnode{tag: tlv.ContextTag(tagInvokeReqDelayReportData), el: tlv.Element{Type: tlv.TypeStructure, IsContainer: true}, kids: []*tnode{
				uintMember(0, 300), uintMember(1, 7),
			}})
		}, func(b []byte) (any, error) { return UnmarshalInvokeRequestTLV(tlv.NewDecoder(b), drainFields) }},
		{
			"TimedRequest", func() []byte { return encodeWith(TimedRequest{TimeoutMs: 500}.MarshalTLV) },
			func(b []byte) (any, error) { return UnmarshalTimedRequestTLV(tlv.NewDecoder(b)) },
		},
		{
			"StatusResponse", func() []byte { return encodeWith(StatusResponse{Status: StatusBusy}.MarshalTLV) },
			func(b []byte) (any, error) { return UnmarshalStatusResponseTLV(tlv.NewDecoder(b)) },
		},
		{"StatusIB", func() []byte {
			return encodeWith(func(e *tlv.Encoder) {
				StatusIB{Status: StatusFailure, ClusterStatus: 3, HasClusterStatus: true}.MarshalTLV(e, tlv.AnonymousTag())
			})
		}, func(b []byte) (any, error) { return UnmarshalStatusIBTLV(tlv.NewDecoder(b)) }},
		{"AttributePath", func() []byte {
			return encodeWith(func(e *tlv.Encoder) { attrPathSamples()[0].MarshalTLV(e, tlv.AnonymousTag()) })
		}, func(b []byte) (any, error) { return UnmarshalAttributePathTLV(tlv.NewDecoder(b)) }},
		{"CommandPath", func() []byte {
			return encodeWith(func(e *tlv.Encoder) { cmd.MarshalTLV(e, tlv.AnonymousTag()) })
		}, func(b []byte) (any, error) { return UnmarshalCommandPathTLV(tlv.NewDecoder(b)) }},
		{"WriteResponse", func() []byte {
			return encodeWith(WriteResponse{Responses: []AttributeStatus{
				{Path: attrPathSamples()[0], Status: StatusIB{Status: StatusSuccess}},
				{Path: attrPathSamples()[2], Status: StatusIB{Status: StatusFailure, ClusterStatus: 2, HasClusterStatus: true}},
			}}.MarshalTLV)
		}, func(b []byte) (any, error) { return UnmarshalWriteResponseTLV(tlv.NewDecoder(b)) }},
		{"SubscribeResponse", func() []byte {
			return encodeWith(SubscribeResponse{SubscriptionID: 0x1234, MaxInterval: 60}.MarshalTLV)
		}, func(b []byte) (any, error) { return UnmarshalSubscribeResponseTLV(tlv.NewDecoder(b)) }},
		{"ReportData", func() []byte {
			b := encodeWith(func(e *tlv.Encoder) {
				ReportData{
					SubscriptionID: 7, HasSubscription: true, MoreChunkedMessages: true, SuppressResponse: true,
					Reports: []AttributeReport{
						{Path: attrPathSamples()[0], Value: AttributeValue{Value: true}, DataVersion: 5, HasDataVersion: true},
						{Path: attrPathSamples()[2], IsStatus: true, Status: StatusIB{Status: StatusUnsupportedAttribute, ClusterStatus: 1, HasClusterStatus: true}},
					},
					EventReports: []EventReport{
						{Path: eventPathSamples()[0], Number: 3, Priority: EventPriorityCritical, Timestamp: 1000, Data: AttributeValue{Value: 1}},
						{Path: eventPathSamples()[0], IsStatus: true, Status: StatusIB{Status: StatusUnsupportedEvent}},
					},
				}.MarshalTLV(e, markerValue)
			})
			return withMembers(b, []int{2, 0, 0}, uintMember(tagEventDataSystemTimestamp, 4000), uintMember(tagEventDataDeltaEpochTimestamp, 50), uintMember(tagEventDataDeltaSystemTimestamp, 60))
		}, func(b []byte) (any, error) { return UnmarshalReportDataTLV(tlv.NewDecoder(b), drainValue, drainEvent) }},
		{"InvokeResponse", func() []byte {
			return encodeWith(func(e *tlv.Encoder) {
				InvokeResponse{MoreChunkedMessages: true, Responses: []InvokeResponseEntry{
					{Path: cmd, Response: 1, HasResponse: true, CommandRef: 1, HasCommandRef: true},
					{Path: cmd, IsStatus: true, Status: StatusIB{Status: StatusFailure, ClusterStatus: 4, HasClusterStatus: true}, CommandRef: 2, HasCommandRef: true},
				}}.MarshalTLV(e, markerFields)
			})
		}, func(b []byte) (any, error) { return UnmarshalInvokeResponseTLV(tlv.NewDecoder(b), drainFields) }},
	}
	return cases
}

// decodeVariant decodes a variant, a panic counting as a failure of the
// test (a decoder never panics on input).
func decodeVariant(t *testing.T, c decoderCase, b []byte, what string) (v any, err error) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("%s: %s: decoder panicked: %v", c.name, what, r)
			err = errors.New("panic")
		}
	}()
	return c.decode(b)
}

func describe(root *tnode, path []int) string {
	var parts []string
	n := root
	for _, i := range path {
		n = n.kids[i]
		switch n.tag.Kind {
		case tlv.TagKindContext:
			parts = append(parts, strconv.FormatUint(uint64(n.tag.Number), 10))
		default:
			parts = append(parts, fmt.Sprintf("[%d]", i))
		}
	}
	return "/" + strings.Join(parts, "/")
}

func TestDecodersRefuseEveryTruncation(t *testing.T) {
	t.Parallel()
	for _, c := range decoderCases() {
		b := c.msg()
		if _, err := c.decode(b); err != nil {
			t.Fatalf("%s: the well-formed message: %v", c.name, err)
		}
		for k := range b {
			_, err := decodeVariant(t, c, b[:k], "truncated")
			if err == nil {
				t.Errorf("%s: the first %d of %d bytes decoded", c.name, k, len(b))
			} else if !errors.Is(err, tlv.ErrTruncated) && !errors.Is(err, tlv.ErrLengthOverflow) && !errors.Is(err, io.EOF) {
				t.Errorf("%s: the first %d of %d bytes: %v, want the TLV decoder's cause kept", c.name, k, len(b), err)
			}
		}
	}
}

func TestDecodersSkipUnknownContextMembers(t *testing.T) {
	t.Parallel()
	for _, c := range decoderCases() {
		root := parseTLVTree(t, c.msg())
		want, err := c.decode(root.bytes(t))
		if err != nil {
			t.Fatalf("%s: the well-formed message: %v", c.name, err)
		}
		holders := append([][]int{nil}, root.positions()...)
		for _, p := range holders {
			at := root.at(p)
			if at.el.Type != tlv.TypeStructure && at.el.Type != tlv.TypeList {
				continue
			}
			for name, m := range unknownMembers() {
				for i := 0; i <= len(at.kids); i += max(1, len(at.kids)) {
					v := root.clone()
					h := v.at(p)
					h.kids = append(h.kids[:i:i], append([]*tnode{m.clone()}, h.kids[i:]...)...)
					got, err := decodeVariant(t, c, v.bytes(t), "unknown member")
					if err != nil {
						t.Errorf("%s: %s under context tag 200 at %s#%d refused: %v", c.name, name, describe(root, p), i, err)
						continue
					}
					if !reflect.DeepEqual(got, want) {
						t.Errorf("%s: %s under context tag 200 at %s#%d was read:\n got %+v\nwant %+v", c.name, name, describe(root, p), i, got, want)
					}
				}
			}
		}
	}
}

func TestDecodersNeverReadForeignMembers(t *testing.T) {
	t.Parallel()
	for _, c := range decoderCases() {
		root := parseTLVTree(t, c.msg())
		want, _ := c.decode(root.bytes(t))
		holders := append([][]int{nil}, root.positions()...)
		for _, p := range holders {
			at := root.at(p)
			if at.el.Type != tlv.TypeStructure && at.el.Type != tlv.TypeList {
				continue
			}
			for _, k := range tlvKinds {
				// A profile tag numbered like each member the holder has,
				// before the members and after them.
				refuses := foreignRefused(c.name, describe(root, p))
				for _, mem := range append([]*tnode{{tag: tlv.ContextTag(0)}}, at.kids...) {
					for _, first := range []bool{true, false} {
						v := root.clone()
						h := v.at(p)
						m := k.sample(tlv.CommonTag(mem.tag.Number))
						if first {
							h.kids = append([]*tnode{m}, h.kids...)
						} else {
							h.kids = append(h.kids, m)
						}
						got, err := decodeVariant(t, c, v.bytes(t), "foreign member")
						switch {
						case refuses && err == nil:
							t.Errorf("%s: %s under common-profile tag %d at %s (first %v) was not refused: %+v", c.name, k.name, mem.tag.Number, describe(root, p), first, got)
						case !refuses && err != nil:
							t.Errorf("%s: %s under common-profile tag %d at %s (first %v) was refused, not skipped: %v", c.name, k.name, mem.tag.Number, describe(root, p), first, err)
						case !refuses && !reflect.DeepEqual(got, want):
							t.Errorf("%s: %s under common-profile tag %d at %s (first %v) was read:\n got %+v\nwant %+v", c.name, k.name, mem.tag.Number, describe(root, p), first, got, want)
						}
					}
				}
			}
		}
	}
}

func TestDecodersRefuseOrIgnoreWrongTypes(t *testing.T) {
	t.Parallel()
	for _, c := range decoderCases() {
		root := parseTLVTree(t, c.msg())
		for _, p := range root.positions() {
			orig := root.at(p)
			removed := root.clone()
			parent := removed.at(p[:len(p)-1])
			i := p[len(p)-1]
			parent.kids = append(parent.kids[:i:i], parent.kids[i+1:]...)
			without, withoutErr := decodeVariant(t, c, removed.bytes(t), "removed")
			for _, k := range tlvKinds {
				if k.is(orig.el.Type) || orig.el.Type == tlv.TypeNull {
					continue
				}
				v := root.clone()
				v.at(p[:len(p)-1]).kids[i] = k.sample(orig.tag)
				got, err := decodeVariant(t, c, v.bytes(t), "wrong type")
				if err != nil {
					continue
				}
				if withoutErr != nil {
					t.Errorf("%s: %s at %s was accepted (%+v); the message without the element is refused: %v", c.name, k.name, describe(root, p), got, withoutErr)
					continue
				}
				if !reflect.DeepEqual(got, without) {
					t.Errorf("%s: %s at %s was read:\n got %+v\nwant %+v (the element ignored)", c.name, k.name, describe(root, p), got, without)
				}
			}
		}
	}
}

// containsUint reports whether some integer reachable from v equals want.
func containsUint(v reflect.Value, want uint64) bool {
	switch v.Kind() { //nolint:exhaustive // the kinds that can hold an integer
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint() == want
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() >= 0 && uint64(v.Int()) == want
	case reflect.Pointer, reflect.Interface:
		return !v.IsNil() && containsUint(v.Elem(), want)
	case reflect.Struct:
		for i := range v.NumField() {
			if containsUint(v.Field(i), want) {
				return true
			}
		}
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			if containsUint(v.Index(i), want) {
				return true
			}
		}
	}
	return false
}

// TestDecodersNeverCutAnInteger: an unsigned element given a value at or
// just past a width's limit is refused, ignored, or read as that value —
// never cut to a narrower width or read as another number.
func TestDecodersNeverCutAnInteger(t *testing.T) {
	t.Parallel()
	limits := []uint64{0xFF, 0x100, 0xFFFF, 0x1_0000, 0xFFFF_FFFF, 0x1_0000_0000, 1<<64 - 1}
	for _, c := range decoderCases() {
		root := parseTLVTree(t, c.msg())
		for _, p := range root.positions() {
			orig := root.at(p)
			if orig.el.Type < tlv.TypeUnsignedInt1 || orig.el.Type > tlv.TypeUnsignedInt8 {
				continue
			}
			removed := root.clone()
			parent := removed.at(p[:len(p)-1])
			i := p[len(p)-1]
			parent.kids = append(parent.kids[:i:i], parent.kids[i+1:]...)
			without, withoutErr := decodeVariant(t, c, removed.bytes(t), "removed")
			for _, lim := range limits {
				v := root.clone()
				v.at(p).el = tlv.Element{Type: tlv.TypeUnsignedInt8, Uint: lim}
				got, err := decodeVariant(t, c, v.bytes(t), "limit")
				if err != nil || containsUint(reflect.ValueOf(got), lim) {
					continue
				}
				if withoutErr == nil && reflect.DeepEqual(got, without) {
					continue
				}
				t.Errorf("%s: %#x at %s was read as another number: %+v", c.name, lim, describe(root, p), got)
			}
		}
	}
}

// TestDecodersReadEveryValue: a scalar given another value of its type
// changes the decode, unless the decoder ignores the element (the decode
// then equals the message's without it) — a decoder reads each value from
// its own element.
func TestDecodersReadEveryValue(t *testing.T) {
	t.Parallel()
	for _, c := range decoderCases() {
		root := parseTLVTree(t, c.msg())
		want, _ := c.decode(root.bytes(t))
		for _, p := range root.positions() {
			orig := root.at(p)
			v := root.clone()
			el := &v.at(p).el
			switch {
			case orig.el.Type == tlv.TypeBoolFalse || orig.el.Type == tlv.TypeBoolTrue:
				el.Type = tlv.TypeBoolFalse
				if orig.el.Type == tlv.TypeBoolFalse {
					el.Type = tlv.TypeBoolTrue
				}
			case orig.el.Type >= tlv.TypeUnsignedInt1 && orig.el.Type <= tlv.TypeUnsignedInt8:
				el.Uint ^= 1
			default:
				continue
			}
			got, err := decodeVariant(t, c, v.bytes(t), "another value")
			if err != nil || !reflect.DeepEqual(got, want) {
				continue
			}
			removed := root.clone()
			parent := removed.at(p[:len(p)-1])
			i := p[len(p)-1]
			parent.kids = append(parent.kids[:i:i], parent.kids[i+1:]...)
			if without, err := decodeVariant(t, c, removed.bytes(t), "removed"); err != nil || !reflect.DeepEqual(without, want) {
				t.Errorf("%s: another value at %s decodes as the original, yet the element is not ignored", c.name, describe(root, p))
			}
		}
	}
}

var containerTypes = []tlv.ElementType{tlv.TypeStructure, tlv.TypeArray, tlv.TypeList}

// retype makes n a container of type ct with its members kept: anonymous
// in an array, tagged as they were (or by position, when they were
// anonymous) in a structure or list.
func retype(n *tnode, ct tlv.ElementType) {
	wasArray := n.el.Type == tlv.TypeArray
	n.el.Type = ct
	for i, k := range n.kids {
		switch {
		case ct == tlv.TypeArray:
			k.tag = tlv.AnonymousTag()
		case wasArray:
			k.tag = tlv.ContextTag(uint8(i)) //nolint:gosec // a test message has few members
		}
	}
}

// laxContainers are the containers a decoder reads whatever their container
// type, measured at HEAD and reported (round 2 notes of the tests unit):
// the response side reads a CommandDataIB or CommandStatusIB sent as a list
// (readInvokeResponseIB checks only that the member is a container), where
// the request side refuses one that is not a structure (readCommandData).
var laxContainers = map[string]map[string]bool{
	"InvokeResponse": {"/1/[0]/0": true, "/1/[1]/1": true},
}

// foreignRefused reports whether the container at path of case c's message
// refuses a member under a profile tag.
func foreignRefused(c, path string) bool {
	return (refusesForeign[c] && !skippingHolders[c][path]) || refusingHolders[c][path]
}

// skippingHolders are the containers of a request that skip a member
// under a profile tag, measured at HEAD and reported (round 2 notes of the
// tests unit): DelayReportData (readDelayReportData skips every member but
// context tags 0 and 1), where the rest of the request side refuses one.
var skippingHolders = map[string]map[string]bool{
	"InvokeRequest": {"/3": true},
}

// TestDecodersRefuseOrIgnoreRetypedContainers: a container whose members
// are kept but whose container type is another — a structure for an
// array, a list for a structure — is refused or ignored like any wrong
// type, the message's own opener included (refused).
func TestDecodersRefuseOrIgnoreRetypedContainers(t *testing.T) {
	t.Parallel()
	for _, c := range decoderCases() {
		root := parseTLVTree(t, c.msg())
		for _, ct := range containerTypes {
			if ct == root.el.Type {
				continue
			}
			v := root.clone()
			retype(v, ct)
			if got, err := decodeVariant(t, c, v.bytes(t), "retyped message"); err == nil {
				t.Errorf("%s: the message as container type %#x decoded: %+v", c.name, ct, got)
			}
		}
		for _, p := range root.positions() {
			orig := root.at(p)
			if !orig.el.IsContainer {
				continue
			}
			removed := root.clone()
			parent := removed.at(p[:len(p)-1])
			i := p[len(p)-1]
			parent.kids = append(parent.kids[:i:i], parent.kids[i+1:]...)
			without, withoutErr := decodeVariant(t, c, removed.bytes(t), "removed")
			for _, ct := range containerTypes {
				if ct == orig.el.Type {
					continue
				}
				v := root.clone()
				retype(v.at(p), ct)
				got, err := decodeVariant(t, c, v.bytes(t), "retyped")
				if err != nil {
					continue
				}
				if laxContainers[c.name][describe(root, p)] {
					continue
				}
				if withoutErr != nil || !reflect.DeepEqual(got, without) {
					t.Errorf("%s: the container at %s as type %#x was read: %+v", c.name, describe(root, p), ct, got)
				}
			}
		}
	}
}

// TestDecodersReadAnyIntegerWidth: an integer is the same whatever width
// its encoding takes — every unsigned element re-encoded at each width
// its value fits decodes as the message does.
func TestDecodersReadAnyIntegerWidth(t *testing.T) {
	t.Parallel()
	for _, c := range decoderCases() {
		root := parseTLVTree(t, c.msg())
		want, _ := c.decode(root.bytes(t))
		for _, p := range root.positions() {
			orig := root.at(p)
			if orig.el.Type < tlv.TypeUnsignedInt1 || orig.el.Type > tlv.TypeUnsignedInt8 {
				continue
			}
			for w := tlv.TypeUnsignedInt1; w <= tlv.TypeUnsignedInt8; w++ {
				if bits := 8 << (w - tlv.TypeUnsignedInt1); bits < 64 && orig.el.Uint>>bits != 0 {
					continue
				}
				v := root.clone()
				v.at(p).el.Type = w
				got, err := decodeVariant(t, c, v.bytes(t), "width")
				if err != nil || !reflect.DeepEqual(got, want) {
					t.Errorf("%s: %d at %s encoded in %d bytes: %+v, %v", c.name, orig.el.Uint, describe(root, p), 1<<(w-tlv.TypeUnsignedInt1), got, err)
				}
			}
		}
	}
}

// TestDecodersWithoutReaders: a decoder handed no value, event or fields
// reader skips what the reader would read and decodes the rest.
func TestDecodersWithoutReaders(t *testing.T) {
	t.Parallel()
	for _, c := range []decoderCase{
		{"ReportData", nil, func(b []byte) (any, error) { return UnmarshalReportDataTLV(tlv.NewDecoder(b), nil, nil) }},
		{"InvokeResponse", nil, func(b []byte) (any, error) { return UnmarshalInvokeResponseTLV(tlv.NewDecoder(b), nil) }},
	} {
		var msg func() []byte
		for _, d := range decoderCases() {
			if d.name == c.name {
				msg = d.msg
			}
		}
		got, err := decodeVariant(t, c, msg(), "no readers")
		if err != nil {
			t.Fatalf("%s without readers: %v", c.name, err)
		}
		switch r := got.(type) {
		case ReportData:
			if len(r.Reports) != 2 || len(r.EventReports) != 2 || r.Reports[0].Value.Value != nil || r.EventReports[0].Data.Value != nil || r.EventReports[0].Number != 3 {
				t.Errorf("ReportData without readers: %+v", r)
			}
		case InvokeResponse:
			if len(r.Responses) != 2 || r.Responses[1].Status.ClusterStatus != 4 {
				t.Errorf("InvokeResponse without readers: %+v", r)
			}
		}
	}
}

func TestIsGlobalAttributeIDBounds(t *testing.T) {
	t.Parallel()
	for id, want := range map[uint32]bool{0xFFF7: false, 0xFFF8: true, 0xFFFB: true, 0xFFFD: true, 0xFFFE: false} {
		if got := isGlobalAttributeID(id); got != want {
			t.Errorf("isGlobalAttributeID(%#x) = %v, want %v", id, got, want)
		}
	}
}

// badElementMarker is a placeholder whose encoding [withBadElement]
// replaces by one control byte of no element type (0x1F).
var badElementMarker = []byte{0xDE, 0xAD, 0xBE, 0xEF, 0x5A}

func badElement(tag tlv.Tag) *tnode {
	return &tnode{tag: tag, el: tlv.Element{Type: tlv.TypeOctetStr1, Octets: badElementMarker}}
}

// withBadElement encodes n with the marker element replaced by 0x1F.
func withBadElement(t *testing.T, n *tnode) []byte {
	t.Helper()
	b := n.bytes(t)
	i := bytes.Index(b, badElementMarker)
	if i < 2 {
		t.Fatal("no marker in the message")
	}
	start := i - 2 // control and length byte of an anonymous octet string
	if b[start] != byte(tlv.TypeOctetStr1) {
		start = i - 3 // control, context tag and length byte
	}
	return append(append(append([]byte(nil), b[:start]...), 0x1F), b[i+len(badElementMarker):]...)
}

// TestDecodersKeepTheCauseOfAMalformedSkippedElement: a malformed element
// (a control byte of no element type) inside a member the decoder skips —
// an unknown member, a foreign member a response skips, a wrong-typed
// container the decoder ignores — refuses the message with the TLV
// decoder's cause, tlv.ErrInvalidElementType: a skip that fails is not
// taken for the member's end.
func TestDecodersKeepTheCauseOfAMalformedSkippedElement(t *testing.T) {
	t.Parallel()
	check := func(c decoderCase, b []byte, what string) {
		t.Helper()
		_, err := decodeVariant(t, c, b, what)
		if !errors.Is(err, tlv.ErrInvalidElementType) {
			t.Errorf("%s: %s: %v, want tlv.ErrInvalidElementType", c.name, what, err)
		}
	}
	holderOf := func(tag tlv.Tag, ct tlv.ElementType) *tnode {
		inner := tlv.ContextTag(0)
		if ct == tlv.TypeArray {
			inner = tlv.AnonymousTag()
		}
		return &tnode{tag: tag, el: tlv.Element{Type: ct, IsContainer: true}, kids: []*tnode{badElement(inner)}}
	}
	for _, c := range decoderCases() {
		root := parseTLVTree(t, c.msg())
		for _, p := range append([][]int{nil}, root.positions()...) {
			at := root.at(p)
			if at.el.Type != tlv.TypeStructure && at.el.Type != tlv.TypeList {
				continue
			}
			for _, ct := range containerTypes {
				v := root.clone()
				h := v.at(p)
				h.kids = append([]*tnode{holderOf(tlv.ContextTag(200), ct)}, h.kids...)
				check(c, withBadElement(t, v), fmt.Sprintf("a bad element in an unknown container (type %#x) at %s", ct, describe(root, p)))
				if foreignRefused(c.name, describe(root, p)) {
					continue
				}
				v = root.clone()
				h = v.at(p)
				h.kids = append([]*tnode{holderOf(tlv.CommonTag(0), ct)}, h.kids...)
				check(c, withBadElement(t, v), fmt.Sprintf("a bad element in a foreign container (type %#x) at %s", ct, describe(root, p)))
			}
		}
		for _, p := range root.positions() {
			orig := root.at(p)
			for _, ct := range containerTypes {
				if ct == orig.el.Type {
					continue
				}
				// Only where the decoder ignores the wrong-typed container.
				plain := root.clone()
				plain.at(p[:len(p)-1]).kids[p[len(p)-1]] = &tnode{tag: orig.tag, el: tlv.Element{Type: ct, IsContainer: true}}
				if _, err := decodeVariant(t, c, plain.bytes(t), "ignored container"); err != nil {
					continue
				}
				v := root.clone()
				v.at(p[:len(p)-1]).kids[p[len(p)-1]] = holderOf(orig.tag, ct)
				check(c, withBadElement(t, v), fmt.Sprintf("a bad element in an ignored container (type %#x) at %s", ct, describe(root, p)))
			}
		}
	}
}

func TestSubscribeResponseIntegerLimits(t *testing.T) {
	t.Parallel()
	msg := func(id, maxInterval uint64) []byte {
		return encodeWith(func(e *tlv.Encoder) {
			e.StartStruct(tlv.AnonymousTag())
			e.PutUint(tlv.ContextTag(tagSubRespSubscriptionID), id)
			e.PutUint(tlv.ContextTag(tagSubRespMaxInterval), maxInterval)
			_ = e.EndContainer()
		})
	}
	if sr, err := UnmarshalSubscribeResponseTLV(tlv.NewDecoder(msg(0xFFFF_FFFF, 0xFFFF))); err != nil || sr.SubscriptionID != 0xFFFF_FFFF || sr.MaxInterval != 0xFFFF {
		t.Errorf("the largest SubscriptionId and MaxInterval: %+v, %v", sr, err)
	}
	for _, b := range [][]byte{msg(0x1_0000_0000, 1), msg(1, 0x1_0000)} {
		if sr, err := UnmarshalSubscribeResponseTLV(tlv.NewDecoder(b)); !errors.Is(err, ErrInvalidSubscribeResponse) {
			t.Errorf("a value past its width: %+v, %v", sr, err)
		}
	}
}

// invokeRequestWith is an InvokeRequest of one command (endpoint 1, OnOff
// Toggle) whose CommandFields fields writes, followed by a second, well-formed
// one, with extra written after the InvokeRequests.
func invokeRequestWith(fields func(*tlv.Encoder, tlv.Tag), extra func(*tlv.Encoder)) []byte {
	cmd := func(e *tlv.Encoder, ref uint64, f func(*tlv.Encoder, tlv.Tag)) {
		e.StartStruct(tlv.AnonymousTag())
		ConcreteCommandPath{Endpoint: 1, Cluster: 6, Command: 2, HasEndpoint: true, HasCluster: true, HasCommand: true}.MarshalTLV(e, tlv.ContextTag(tagCmdDataPath))
		f(e, tlv.ContextTag(tagCmdDataFields))
		e.PutUint(tlv.ContextTag(tagCmdDataRef), ref)
		_ = e.EndContainer()
	}
	return encodeWith(func(e *tlv.Encoder) {
		e.StartStruct(tlv.AnonymousTag())
		e.PutBool(tlv.ContextTag(tagInvokeReqSuppressResponse), false)
		e.PutBool(tlv.ContextTag(tagInvokeReqTimedRequest), false)
		e.StartArray(tlv.ContextTag(tagInvokeReqInvokeRequests))
		cmd(e, 1, fields)
		cmd(e, 2, writeMarker)
		_ = e.EndContainer()
		if extra != nil {
			extra(e)
		}
		_ = e.EndContainer()
	})
}

// TestInvokeRequestFieldsThatAreNotAStructure: CommandFields of another TLV
// type is that command's INVALID_COMMAND — no fields, not read by the
// reader — and the rest of the batch decodes; a malformed element inside
// such a container still refuses the request with the TLV cause.
func TestInvokeRequestFieldsThatAreNotAStructure(t *testing.T) {
	t.Parallel()
	for name, fields := range map[string]func(*tlv.Encoder, tlv.Tag){
		"a list": func(e *tlv.Encoder, tag tlv.Tag) {
			e.StartList(tag)
			e.PutUint(tlv.ContextTag(0), 1)
			_ = e.EndContainer()
		},
		"an array": func(e *tlv.Encoder, tag tlv.Tag) {
			e.StartArray(tag)
			e.PutUint(tlv.AnonymousTag(), 1)
			_ = e.EndContainer()
		},
		"an unsigned integer": func(e *tlv.Encoder, tag tlv.Tag) { e.PutUint(tag, 1) },
	} {
		req, err := UnmarshalInvokeRequestTLV(tlv.NewDecoder(invokeRequestWith(fields, nil)), drainFields)
		if err != nil {
			t.Errorf("fields as %s: %v", name, err)
			continue
		}
		if len(req.Invokes) != 2 || req.Invokes[0].DecodeStatus != StatusInvalidCommand || req.Invokes[0].Fields != nil ||
			req.Invokes[1].DecodeStatus != 0 || req.Invokes[1].Fields != "f" || req.Invokes[1].CommandRef != 2 {
			t.Errorf("fields as %s: %+v", name, req.Invokes)
		}
	}
	bad := invokeRequestWith(func(e *tlv.Encoder, tag tlv.Tag) {
		e.StartList(tag)
		e.PutOctets(tlv.ContextTag(0), badElementMarker)
		_ = e.EndContainer()
	}, nil)
	i := bytes.Index(bad, badElementMarker)
	bad = append(append(append([]byte(nil), bad[:i-3]...), 0x1F), bad[i+len(badElementMarker):]...)
	if _, err := UnmarshalInvokeRequestTLV(tlv.NewDecoder(bad), drainFields); !errors.Is(err, tlv.ErrInvalidElementType) {
		t.Errorf("a malformed element in list fields: %v, want tlv.ErrInvalidElementType", err)
	}
}

// TestInvokeRequestWithoutAFieldsReader: without a fields reader the
// fields are skipped, a malformed element inside them refusing the
// request with the TLV cause.
func TestInvokeRequestWithoutAFieldsReader(t *testing.T) {
	t.Parallel()
	req, err := UnmarshalInvokeRequestTLV(tlv.NewDecoder(invokeRequestWith(writeMarker, nil)), nil)
	if err != nil || len(req.Invokes) != 2 || req.Invokes[0].Fields != nil || req.Invokes[1].CommandRef != 2 {
		t.Fatalf("%+v, %v", req, err)
	}
	b := invokeRequestWith(func(e *tlv.Encoder, tag tlv.Tag) {
		e.StartStruct(tag)
		e.PutOctets(tlv.ContextTag(0), badElementMarker)
		_ = e.EndContainer()
	}, nil)
	i := bytes.Index(b, badElementMarker)
	b = append(append(append([]byte(nil), b[:i-3]...), 0x1F), b[i+len(badElementMarker):]...)
	if _, err := UnmarshalInvokeRequestTLV(tlv.NewDecoder(b), nil); !errors.Is(err, tlv.ErrInvalidElementType) {
		t.Errorf("a malformed element in skipped fields: %v, want tlv.ErrInvalidElementType", err)
	}
}

// TestInvokeRequestDelayReportDataAndRevision: DelayReportData's two
// uint16 fields read to their maximum, a value past it is a deferred
// CONSTRAINT_ERROR, a field of another type or a DelayReportData that is
// not a structure INVALID_ACTION; InteractionModelRevision reads up to
// 255 and is ignored past it.
func TestInvokeRequestDelayReportDataAndRevision(t *testing.T) {
	t.Parallel()
	delay := func(put func(*tlv.Encoder)) []byte {
		return invokeRequestWith(writeMarker, func(e *tlv.Encoder) {
			e.StartStruct(tlv.ContextTag(tagInvokeReqDelayReportData))
			put(e)
			_ = e.EndContainer()
		})
	}
	status := func(err error) StatusCode {
		if sce, ok := errors.AsType[StatusCodeError](err); ok {
			return sce.MatterStatusCode()
		}
		return 0
	}
	req, err := UnmarshalInvokeRequestTLV(tlv.NewDecoder(delay(func(e *tlv.Encoder) {
		e.PutUint(tlv.ContextTag(0), 0xFFFF)
		e.PutUint(tlv.ContextTag(1), 0xFFFF)
	})), drainFields)
	if err != nil || req.DelayReportData == nil || *req.DelayReportData != (DelayReportData{DelayMinMs: 0xFFFF, DelayJitterWindowMs: 0xFFFF}) {
		t.Errorf("DelayReportData at its maximum: %+v, %v", req.DelayReportData, err)
	}
	if _, err := UnmarshalInvokeRequestTLV(tlv.NewDecoder(delay(func(e *tlv.Encoder) { e.PutUint(tlv.ContextTag(1), 0x1_0000) })), drainFields); status(err) != StatusConstraintError {
		t.Errorf("DelayJitterWindowMs past uint16: %v, want CONSTRAINT_ERROR", err)
	}
	if _, err := UnmarshalInvokeRequestTLV(tlv.NewDecoder(delay(func(e *tlv.Encoder) { e.PutBool(tlv.ContextTag(0), true) })), drainFields); status(err) != StatusInvalidAction {
		t.Errorf("DelayMinMs a boolean: %v, want INVALID_ACTION", err)
	}
	notStruct := invokeRequestWith(writeMarker, func(e *tlv.Encoder) {
		e.StartList(tlv.ContextTag(tagInvokeReqDelayReportData))
		e.PutUint(tlv.ContextTag(0), 1)
		_ = e.EndContainer()
	})
	if _, err := UnmarshalInvokeRequestTLV(tlv.NewDecoder(notStruct), drainFields); status(err) != StatusInvalidAction {
		t.Errorf("DelayReportData a list: %v, want INVALID_ACTION", err)
	}
	for rev, want := range map[uint64]uint8{0xFF: 0xFF, 0x100: 0} {
		b := invokeRequestWith(writeMarker, func(e *tlv.Encoder) { e.PutUint(tlv.ContextTag(tagInteractionModelRevision), rev) })
		if req, err := UnmarshalInvokeRequestTLV(tlv.NewDecoder(b), drainFields); err != nil || req.InteractionModelRevision != want {
			t.Errorf("InteractionModelRevision %#x: read %#x, %v; want %#x", rev, req.InteractionModelRevision, err, want)
		}
	}
}

// replaceBadElement replaces the marker element (under a context tag) in b
// by a control byte of no element type.
func replaceBadElement(t *testing.T, b []byte) []byte {
	t.Helper()
	i := bytes.Index(b, badElementMarker)
	if i < 3 {
		t.Fatal("no marker")
	}
	return append(append(append([]byte(nil), b[:i-3]...), 0x1F), b[i+len(badElementMarker):]...)
}

// badValue writes a structure holding the marker element.
func badValue(enc *tlv.Encoder, tag tlv.Tag) {
	enc.StartStruct(tag)
	enc.PutOctets(tlv.ContextTag(0), badElementMarker)
	_ = enc.EndContainer()
}

// TestReaderValuesAreRefusedWhole: a malformed element inside an attribute
// value or an event's data refuses a ReportData with the TLV cause whether
// or not a reader is handed, and a reader's error refuses the message — a
// WriteRequest's and a ReportData's value, an event's data.
func TestReaderValuesAreRefusedWhole(t *testing.T) {
	t.Parallel()
	report := func(value func(*tlv.Encoder, tlv.Tag, AttributeValue)) []byte {
		return encodeWith(func(e *tlv.Encoder) {
			ReportData{
				Reports:      []AttributeReport{{Path: attrPathSamples()[0], Value: AttributeValue{Value: 1}}},
				EventReports: []EventReport{{Path: eventPathSamples()[0], Number: 3, Priority: EventPriorityCritical, Timestamp: 1, Data: AttributeValue{Value: 1}}},
			}.MarshalTLV(e, value)
		})
	}
	// The bad element in the attribute value, then in the event data.
	both := report(func(enc *tlv.Encoder, tag tlv.Tag, _ AttributeValue) { badValue(enc, tag) })
	first := bytes.Index(both, badElementMarker)
	eventOnly := append(append(append([]byte(nil), both[:first]...), 0xAB, 0xAB, 0xAB, 0xAB, 0xAB), both[first+len(badElementMarker):]...)
	attrOnly := append([]byte(nil), both...)
	second := first + 1 + bytes.Index(both[first+1:], badElementMarker)
	copy(attrOnly[second:], []byte{0xAB, 0xAB, 0xAB, 0xAB, 0xAB})
	for name, b := range map[string][]byte{"attribute value": replaceBadElement(t, attrOnly), "event data": replaceBadElement(t, eventOnly)} {
		for readers, decode := range map[string]func([]byte) (ReportData, error){
			"readers": func(b []byte) (ReportData, error) {
				return UnmarshalReportDataTLV(tlv.NewDecoder(b), drainValue, drainEvent)
			},
			"no readers": func(b []byte) (ReportData, error) { return UnmarshalReportDataTLV(tlv.NewDecoder(b), nil, nil) },
		} {
			if _, err := decode(b); !errors.Is(err, tlv.ErrInvalidElementType) {
				t.Errorf("ReportData with a malformed %s, %s: %v, want tlv.ErrInvalidElementType", name, readers, err)
			}
		}
	}
	errReader := errors.New("the reader refused the value")
	ok := report(markerValue)
	if _, err := UnmarshalReportDataTLV(tlv.NewDecoder(ok), func(ConcreteAttributePath, tlv.Element, *tlv.Decoder) (AttributeValue, error) {
		return AttributeValue{}, errReader
	}, drainEvent); !errors.Is(err, errReader) {
		t.Errorf("ReportData whose value reader fails: %v", err)
	}
	if _, err := UnmarshalReportDataTLV(tlv.NewDecoder(ok), drainValue, func(ConcreteEventPath, tlv.Element, *tlv.Decoder) (AttributeValue, error) {
		return AttributeValue{}, errReader
	}); !errors.Is(err, errReader) {
		t.Errorf("ReportData whose event reader fails: %v", err)
	}
	write := encodeWith(func(e *tlv.Encoder) {
		WriteRequest{Writes: []AttributeWrite{{Path: attrPathSamples()[0]}}}.MarshalTLV(e, markerValue)
	})
	if _, err := UnmarshalWriteRequestTLV(tlv.NewDecoder(write), func(ConcreteAttributePath, tlv.Element, *tlv.Decoder) (AttributeValue, error) {
		return AttributeValue{}, errReader
	}); !errors.Is(err, errReader) {
		t.Errorf("WriteRequest whose value reader fails: %v", err)
	}
}

// TestInvokeRequestFieldsRefusedByTheReader: a fields reader's typed
// refusal that leaves the fields container to the decoder has it drained —
// a malformed element inside refusing the request with the TLV cause.
func TestInvokeRequestFieldsRefusedByTheReader(t *testing.T) {
	t.Parallel()
	refuse := func(ConcreteCommandPath, *tlv.Decoder, tlv.Element) (any, error) {
		return nil, invokeDecodeStatusError{"refused", StatusConstraintError}
	}
	req, err := UnmarshalInvokeRequestTLV(tlv.NewDecoder(invokeRequestWith(writeMarker, nil)), refuse)
	if err != nil || len(req.Invokes) != 2 || req.Invokes[0].DecodeStatus != StatusConstraintError {
		t.Fatalf("%+v, %v", req, err)
	}
	b := replaceBadElement(t, invokeRequestWith(badValue, nil))
	if _, err := UnmarshalInvokeRequestTLV(tlv.NewDecoder(b), refuse); !errors.Is(err, tlv.ErrInvalidElementType) {
		t.Errorf("a malformed element in refused fields: %v, want tlv.ErrInvalidElementType", err)
	}
}

// TestInvokeResponseMoreChunkedMessagesIsABoolean: a MoreChunkedMessages of
// another type is refused, not read as false.
func TestInvokeResponseMoreChunkedMessagesIsABoolean(t *testing.T) {
	t.Parallel()
	b := encodeWith(func(e *tlv.Encoder) {
		e.StartStruct(tlv.AnonymousTag())
		e.PutBool(tlv.ContextTag(tagInvokeRespSuppressResponse), false)
		e.StartArray(tlv.ContextTag(tagInvokeRespResponses))
		_ = e.EndContainer()
		e.PutUint(tlv.ContextTag(tagInvokeRespMoreChunked), 1)
		_ = e.EndContainer()
	})
	if resp, err := UnmarshalInvokeResponseTLV(tlv.NewDecoder(b), drainFields); err == nil {
		t.Errorf("MoreChunkedMessages as an unsigned integer decoded: %+v", resp)
	}
}

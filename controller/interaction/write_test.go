// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/controller/interaction"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

type writeResult struct {
	res interaction.WriteStatuses
	err error
}

func writeAsync(c *interaction.Interaction, writes []interaction.WriteRequest, opts interaction.WriteOptions) <-chan writeResult {
	ch := make(chan writeResult, 1)
	go func() {
		res, err := c.Write(context.Background(), writes, opts)
		ch <- writeResult{res, err}
	}()
	return ch
}

// nodeReader keeps a written value as a node tree.
func nodeReader(_ im.ConcreteAttributePath, el tlv.Element, dec *tlv.Decoder) (im.AttributeValue, error) {
	if el.IsContainer {
		n, err := spec.ReadContainer(dec, el)
		return im.AttributeValue{Value: n}, err
	}
	return im.AttributeValue{Value: spec.Node{El: el}}, nil
}

func attrPath(endpoint uint16, cluster, attribute uint32) im.ConcreteAttributePath {
	return im.ConcreteAttributePath{Endpoint: endpoint, Cluster: cluster, Attribute: attribute, HasEndpoint: true, HasCluster: true, HasAttribute: true}
}

// list is a node tree of an array of unsigned integers.
func list(values ...uint64) spec.Node {
	n := spec.Node{El: tlv.Element{Type: tlv.TypeArray, IsContainer: true}}
	for _, v := range values {
		n.Children = append(n.Children, spec.Node{El: tlv.Element{Type: tlv.TypeUnsignedInt1, Uint: v}})
	}
	return n
}

func writeResponse(t *testing.T, statuses ...im.AttributeStatus) []byte {
	t.Helper()
	return encode(t, func(e *tlv.Encoder) { im.WriteResponse{Responses: statuses}.MarshalTLV(e) })
}

// TestWrite: the WriteRequest's shape (TlvWriteRequest: not timed, the
// DataVersion, MoreChunkedMessages false, the revision), the statuses of
// the WriteResponse with their ListIndex and cluster status, and
// WriteStatuses.Err (WriteResult.ts:41-59).
func TestWrite(t *testing.T) {
	t.Parallel()
	s := newScripted(t)
	c := interaction.New(s.mgr, s.session, nil)
	version := uint32(9)
	label := attrPath(0, 0x0028, 0x0005)
	done := writeAsync(c, []interaction.WriteRequest{
		{Path: label, Value: interaction.Raw(spec.Node{El: tlv.Element{Type: tlv.TypeUTF8Str1, String: "x"}}), DataVersion: &version},
		{Path: attrPath(1, 0x0006, 0x4003), Value: interaction.Raw(spec.Node{El: tlv.Element{Type: tlv.TypeUnsignedInt1, Uint: 1}})},
	}, interaction.WriteOptions{})
	req := s.next(t)
	if req.proto.Opcode != im.OpcodeWriteRequest {
		t.Fatalf("opcode 0x%02X", req.proto.Opcode)
	}
	wr, err := im.UnmarshalWriteRequestTLV(tlv.NewDecoder(req.payload), nodeReader)
	if err != nil || wr.TimedRequest || wr.SuppressResponse || wr.MoreChunkedMessages || len(wr.Writes) != 2 {
		t.Fatalf("WriteRequest %+v (%v)", wr, err)
	}
	if w := wr.Writes[0]; !w.HasDataVersion || w.DataVersion != 9 || w.Value.Value.(spec.Node).El.String != "x" || w.Path.Attribute != 5 {
		t.Fatalf("first write %+v", w)
	}
	failed := attrPath(1, 0x0006, 0x4003)
	s.reply(t, req, im.InteractionModelProtocolID, im.OpcodeWriteResponse, writeResponse(t,
		im.AttributeStatus{Path: label, Status: im.StatusIB{Status: im.StatusSuccess}},
		im.AttributeStatus{Path: failed, Status: im.StatusIB{Status: im.StatusConstraintError, ClusterStatus: 4, HasClusterStatus: true}},
	))
	r := await(t, done)
	if r.err != nil || len(r.res) != 2 || r.res[1].Status != im.StatusConstraintError || *r.res[1].ClusterStatus != 4 {
		t.Fatalf("result %+v, %v", r.res, r.err)
	}
	var se *interaction.StatusError
	if err := r.res.Err(); !errors.As(err, &se) || se.Status != im.StatusConstraintError || se.Path == nil || se.Path.Attribute != 0x4003 {
		t.Fatalf("Err() = %v", err)
	}
	if err := r.res[:1].Err(); err != nil {
		t.Fatalf("Err() of a success = %v", err)
	}
	both := interaction.WriteStatuses{r.res[1], r.res[1]}
	if err := both.Err(); err == nil || errors.As(err, &se) && se.Status != im.StatusConstraintError {
		t.Fatalf("joined Err() = %v", err)
	}
	// An empty WriteResponse is an empty result (ClientInteraction.ts:385-387).
	done = writeAsync(c, []interaction.WriteRequest{{Path: label, Value: interaction.Raw(spec.Node{El: tlv.Element{Type: tlv.TypeUTF8Str1, String: "y"}})}}, interaction.WriteOptions{})
	s.reply(t, s.next(t), im.InteractionModelProtocolID, im.OpcodeWriteResponse, writeResponse(t))
	if r := await(t, done); r.err != nil || r.res == nil || len(r.res) != 0 {
		t.Fatalf("empty response %+v, %v", r.res, r.err)
	}
}

// TestWriteChunksLists: a list is written as REPLACE-ALL with the empty
// list and one append (ListIndex null) per element (Write.ts:149-171,
// TlvArray.ts:156-164), timed when asked with a timeout (Write.ts:74);
// AccessControl ACL is not chunked (:151-157), nor anything with chunking
// off.
func TestWriteChunksLists(t *testing.T) {
	t.Parallel()
	s := newScripted(t)
	c := interaction.New(s.mgr, s.session, nil)
	binding := attrPath(1, 0x001E, 0x0000)
	done := writeAsync(c, []interaction.WriteRequest{{Path: binding, Value: interaction.Raw(list(1, 2))}}, interaction.WriteOptions{TimedTimeout: 3 * time.Second})
	timed := s.next(t)
	tr, err := im.UnmarshalTimedRequestTLV(tlv.NewDecoder(timed.payload))
	if timed.proto.Opcode != im.OpcodeTimedRequest || err != nil || tr.TimeoutMs != 3000 {
		t.Fatalf("TimedRequest %+v (%v)", tr, err)
	}
	s.reply(t, timed, im.InteractionModelProtocolID, im.OpcodeStatusResponse, statusResponse(t, im.StatusSuccess))
	req := s.next(t)
	wr, err := im.UnmarshalWriteRequestTLV(tlv.NewDecoder(req.payload), nodeReader)
	if err != nil || !wr.TimedRequest || len(wr.Writes) != 3 {
		t.Fatalf("chunked WriteRequest %+v (%v)", wr, err)
	}
	first := wr.Writes[0]
	if first.Path.HasListIndex || first.Value.Value.(spec.Node).El.Type != tlv.TypeArray || len(first.Value.Value.(spec.Node).Children) != 0 {
		t.Fatalf("REPLACE-ALL %+v", first)
	}
	for i, w := range wr.Writes[1:] {
		if !w.Path.ListAppend || w.Value.Value.(spec.Node).El.Uint != uint64(i+1) {
			t.Fatalf("append %d: %+v", i, w)
		}
	}
	s.reply(t, req, im.InteractionModelProtocolID, im.OpcodeWriteResponse, writeResponse(t))
	await(t, done)

	for name, tc := range map[string]struct {
		path im.ConcreteAttributePath
		opts interaction.WriteOptions
	}{
		"acl":          {attrPath(0, 0x001F, 0x0000), interaction.WriteOptions{}},
		"extension":    {attrPath(0, 0x001F, 0x0001), interaction.WriteOptions{}},
		"chunking off": {binding, interaction.WriteOptions{DisableListChunking: true}},
	} {
		done := writeAsync(c, []interaction.WriteRequest{{Path: tc.path, Value: interaction.Raw(list(1, 2))}}, tc.opts)
		req := s.next(t)
		wr, err := im.UnmarshalWriteRequestTLV(tlv.NewDecoder(req.payload), nodeReader)
		if err != nil || len(wr.Writes) != 1 || len(wr.Writes[0].Value.Value.(spec.Node).Children) != 2 {
			t.Fatalf("%s: %+v (%v)", name, wr, err)
		}
		s.reply(t, req, im.InteractionModelProtocolID, im.OpcodeWriteResponse, writeResponse(t))
		await(t, done)
	}
}

// TestWriteSuppressed: a suppressed write expects an ack only; a stray
// response other than an error StatusResponse is tolerated, an error
// StatusResponse is a *StatusError (InteractionMessenger.ts:1138-1162).
func TestWriteSuppressed(t *testing.T) {
	t.Parallel()
	s := newScripted(t)
	c := interaction.New(s.mgr, s.session, nil)
	value := interaction.Raw(spec.Node{El: tlv.Element{Type: tlv.TypeBoolTrue, Bool: true}})
	writes := []interaction.WriteRequest{{Path: attrPath(1, 0x0006, 0x0000), Value: value}}
	opts := interaction.WriteOptions{SuppressResponse: true}

	done := writeAsync(c, writes, opts)
	req := s.next(t)
	if wr, err := im.UnmarshalWriteRequestTLV(tlv.NewDecoder(req.payload), nodeReader); err != nil || !wr.SuppressResponse {
		t.Fatalf("WriteRequest %+v (%v)", wr, err)
	}
	s.ack(t, req)
	if r := await(t, done); r.err != nil || r.res != nil {
		t.Fatalf("acknowledged: %+v, %v", r.res, r.err)
	}
	done = writeAsync(c, writes, opts)
	s.reply(t, s.next(t), im.InteractionModelProtocolID, im.OpcodeWriteResponse, writeResponse(t))
	if r := await(t, done); r.err != nil {
		t.Fatalf("stray WriteResponse: %v", r.err)
	}
	done = writeAsync(c, writes, opts)
	s.reply(t, s.next(t), im.InteractionModelProtocolID, im.OpcodeStatusResponse, statusResponse(t, im.StatusUnsupportedAccess))
	var se *interaction.StatusError
	if r := await(t, done); !errors.As(r.err, &se) || se.Status != im.StatusUnsupportedAccess {
		t.Fatalf("error StatusResponse: %v", r.err)
	}
}

// TestWriteMalformed: the requests matter.js's Write() refuses
// (Write.ts:64-69, :105-107, :114-121).
func TestWriteMalformed(t *testing.T) {
	t.Parallel()
	s := newScripted(t)
	c := interaction.New(s.mgr, s.session, nil)
	v := uint32(1)
	value := interaction.Raw(spec.Node{El: tlv.Element{Type: tlv.TypeBoolTrue, Bool: true}})
	for name, writes := range map[string][]interaction.WriteRequest{
		"none":                  nil,
		"wildcard with version": {{Path: im.ConcreteAttributePath{Cluster: 6, Attribute: 0, HasCluster: true, HasAttribute: true}, Value: value, DataVersion: &v}},
		"no attribute":          {{Path: im.ConcreteAttributePath{Endpoint: 1, Cluster: 6, HasEndpoint: true, HasCluster: true}, Value: value}},
		"no value":              {{Path: attrPath(1, 6, 0)}},
	} {
		if _, err := c.Write(context.Background(), writes, interaction.WriteOptions{}); !errors.Is(err, interaction.ErrMalformedRequest) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// TestRawRoundTrip: a node tree written through Raw reads back the same —
// every element type a report can carry.
func TestRawRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := func(n uint32) tlv.Tag { return tlv.ContextTag(uint8(n)) }
	in := spec.Node{El: tlv.Element{Type: tlv.TypeStructure, IsContainer: true}, Children: []spec.Node{
		{El: tlv.Element{Tag: ctx(0), Type: tlv.TypeSignedInt1, Int: -3}},
		{El: tlv.Element{Tag: ctx(1), Type: tlv.TypeUnsignedInt2, Uint: 300}},
		{El: tlv.Element{Tag: ctx(2), Type: tlv.TypeBoolFalse}},
		{El: tlv.Element{Tag: ctx(3), Type: tlv.TypeFloat4, Float: 1.5}},
		{El: tlv.Element{Tag: ctx(4), Type: tlv.TypeFloat8, Float: 2.25}},
		{El: tlv.Element{Tag: ctx(5), Type: tlv.TypeUTF8Str1, String: "s"}},
		{El: tlv.Element{Tag: ctx(6), Type: tlv.TypeOctetStr1, Octets: []byte{1, 2}}},
		{El: tlv.Element{Tag: ctx(7), Type: tlv.TypeNull, IsNull: true}},
		{El: tlv.Element{Tag: ctx(8), Type: tlv.TypeList, IsContainer: true}, Children: []spec.Node{{El: tlv.Element{Tag: ctx(1), Type: tlv.TypeUnsignedInt1, Uint: 1}}}},
	}}
	b := encode(t, func(e *tlv.Encoder) { interaction.Raw(in).EncodeTLV(e, tlv.AnonymousTag()) })
	out, err := spec.ReadElement(tlv.NewDecoder(b))
	if err != nil || len(out.Children) != len(in.Children) {
		t.Fatalf("%+v (%v)", out, err)
	}
	c := out.Children
	if c[0].El.Int != -3 || c[1].El.Uint != 300 || c[2].El.Bool || c[3].El.Float != 1.5 || c[4].El.Float != 2.25 ||
		c[5].El.String != "s" || len(c[6].El.Octets) != 2 || !c[7].El.IsNull || c[8].El.Type != tlv.TypeList || c[8].Children[0].El.Uint != 1 {
		t.Fatalf("round trip %+v", c)
	}
}

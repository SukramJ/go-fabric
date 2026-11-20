// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SukramJ/go-fabric/cluster/spec"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// writtenResp is the generated form of the test response: Code mandatory,
// Detail mandatory where the request's Enable is set.
type writtenResp struct {
	Code   uint8
	Detail *uint8
}

// handResp is a hand-written form of the same response.
type handResp struct {
	code   uint8
	detail *uint8
	skip   bool // write no Code
	wide   bool // write a Code outside its constraint
}

// writeHand is the writer that puts a handResp on the wire.
func writeHand(enc *tlv.Encoder, tag tlv.Tag, v any) {
	h, ok := v.(handResp)
	if !ok {
		return
	}
	enc.StartStruct(tag)
	switch {
	case h.wide:
		enc.PutUint(tlv.ContextTag(0), 300)
	case !h.skip:
		enc.PutUint(tlv.ContextTag(0), uint64(h.code))
	}
	if h.detail != nil {
		enc.PutUint(tlv.ContextTag(1), uint64(*h.detail))
	}
	_ = enc.EndContainer()
}

// TestCheckResponseAsWritten (decision 343): a response in a hand-written
// form is judged as its writer puts it on the wire — a mandatory field the
// written bytes lack is FAILURE, the decoded generated form is judged as
// CheckResponse judges it — and is not judged without a writer, when the
// writer writes no structure, or when the decoder refuses a value.
func TestCheckResponseAsWritten(t *testing.T) {
	t.Parallel()
	respCmd := spec.Command{
		ID: 1, Name: "Resp", Direction: spec.Response, Conformance: op(spec.ConfMandatory),
		Fields: []spec.Field{
			{ID: 0, Name: "Code", Type: spec.Type{Kind: spec.KindUint, Bits: 8}, Conformance: op(spec.ConfMandatory)},
			{ID: 1, Name: "Detail", Type: spec.Type{Kind: spec.KindUint, Bits: 8}, Conformance: spec.Conformance{
				Text: "Req.Enable", Op: spec.ConfDot, Args: []spec.Conformance{name("Req"), name("Enable")},
			}},
		},
		Decode: func(n spec.Node) (any, error) {
			var r writtenResp
			err := spec.DecodeFields(n, "Resp",
				spec.Bind(0, "Code", true, &r.Code, spec.DecodeUintIn[uint8](8, 0, 100)),
				spec.Bind(1, "Detail", false, &r.Detail, spec.DecodeOptional(spec.DecodeUint[uint8](8))),
			)
			return r, err
		},
	}
	def := &spec.Cluster{ID: 0xFFF1FC0F, Name: "Written", Revision: 1, Commands: []spec.Command{
		{
			ID: 1, Name: "Req", Direction: spec.Request, Response: "Resp", Conformance: op(spec.ConfMandatory), Decode: decodesAs[ctReq](),
			Fields: []spec.Field{{ID: 0, Name: "Enable", Type: spec.Type{Kind: spec.KindBool}, Conformance: op(spec.ConfOptional)}},
		},
		respCmd,
	}}
	noDecoder := &spec.Cluster{ID: def.ID, Name: def.Name, Revision: 1, Commands: []spec.Command{def.Commands[0], respCmd}}
	noDecoder.Commands[1].Decode = nil
	yes, d := true, uint8(1)
	enabled := ctReq{Enable: &yes}
	ctx := context.Background()
	for _, c := range []struct {
		name    string
		def     *spec.Cluster
		req     any
		resp    any
		write   im.CommandFieldsWriter
		failure bool
	}{
		{"complete", def, ctReq{}, handResp{code: 1}, writeHand, false},
		{"complete with the field the request requires", def, enabled, handResp{code: 1, detail: &d}, writeHand, false},
		{"a mandatory field missing from the bytes", def, ctReq{}, handResp{skip: true}, writeHand, true},
		{"a field the request requires missing", def, enabled, handResp{code: 1}, writeHand, true},
		{"a value the decoder refuses is not judged further", def, ctReq{}, handResp{wide: true}, writeHand, false},
		{"no writer", def, ctReq{}, handResp{skip: true}, nil, false},
		{"a writer that writes nothing", def, ctReq{}, handResp{skip: true}, func(*tlv.Encoder, tlv.Tag, any) {}, false},
		{"a writer that writes no structure", def, ctReq{}, handResp{skip: true}, func(e *tlv.Encoder, tag tlv.Tag, _ any) { e.PutUint(tag, 1) }, false},
		{"a response command without a decoder", noDecoder, ctReq{}, handResp{skip: true}, writeHand, false},
		{"absent", def, ctReq{}, nil, writeHand, true},
	} {
		err := spec.CheckResponseAsWritten(ctx, c.def, 0, 1, c.req, c.resp, nil, c.write)
		var se *spec.StatusError
		if got := errors.As(err, &se) && se.Status == im.StatusFailure; got != c.failure {
			t.Errorf("%s: %v, want failure %v", c.name, err, c.failure)
		}
	}
	if err := spec.CheckResponseAsWritten(ctx, nil, 0, 1, nil, nil, nil, writeHand); err != nil {
		t.Errorf("a nil definition: %v", err)
	}
}

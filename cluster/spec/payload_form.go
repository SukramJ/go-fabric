// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package spec

import (
	"context"
	"reflect"
	"sync"

	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// HandForm carries a request a hand-written reader decoded into its
// server's own form together with the definition's generated form of the
// same payload: Generated is what the definition's decoder made of the
// bytes ([Command.DecodeRequest]), Hand what the server is written
// against. The endpoint dispatcher judges Generated ([AdmitRequest],
// [CheckResponse]) and hands Hand to the server, so a hand-read request
// is held to its definition by construction — from the bytes the decoder
// read, with no mapping between two Go types to drift — and the server
// never sees the carrier.
type HandForm struct {
	Generated any
	Hand      any
}

// SplitHandForm returns the form the definition judges and the form the
// server takes: Generated and Hand of a [HandForm], fields twice for any
// other payload.
func SplitHandForm(fields any) (judged, served any) {
	if hf, ok := fields.(HandForm); ok {
		return hf.Generated, hf.Hand
	}
	return fields, fields
}

// CheckResponse holds the response a server answered the request command
// cmdID with to the definition def under the feature selection features,
// after the server ran — the response's twin of [AdmitRequest], run by the
// endpoint dispatcher on every command a wire invoke reaches a server
// with: the presence of the response's fields against their conformance
// (one a request field names, request being the request as the definition
// judged it), peers resolving the attributes a conformance names. An
// absent response (nil, or a nil pointer) of a command whose response has
// fields is judged as one without fields, so a mandatory field is missing
// and the command answers FAILURE. A refused response is a [StatusError]
// with FAILURE; nil for a nil def, a command def does not state, a
// status-only one, and a response not in the definition's generated form
// or a declared one ([MapFields], [DefinitionFormer]).
//
// Mirrors matter.js packages/node/src/node/integration/ProtocolService.ts:717-733
// at b25ed960 (completeResponse in invokeCommand, the same function that
// holds the request, :671-705; the response validated as `data ?? {}`,
// :624-625; a failure a FailureError).
func CheckResponse(ctx context.Context, def *Cluster, features, cmdID uint32, request, response any, peers Peers) error {
	return CheckResponseAsWritten(ctx, def, features, cmdID, request, response, peers, nil)
}

// CheckResponseAsWritten is [CheckResponse] for a server whose responses
// may be in a hand-written form the definition does not know: such a
// response is judged as write — the encoder that puts it on the wire —
// writes it. The written bytes are decoded through the definition's
// response decoder: a field the definition makes mandatory ("M") and the
// bytes do not carry is FAILURE, and a response the decoder reads is
// judged as [CheckResponse] judges its generated form; one the decoder
// refuses for a value outside its type or constraint is not judged
// further, as a response's types and constraints are not judged here.
// A nil write is [CheckResponse]: a hand-written form is not judged.
// The endpoint dispatcher passes the bridge's response writer
// (TopologyDispatcher.SetResponseWriter).
func CheckResponseAsWritten(_ context.Context, def *Cluster, features, cmdID uint32, request, response any, peers Peers, write im.CommandFieldsWriter) error {
	if def == nil {
		return nil
	}
	i := &Instance{def: def, features: features}
	return i.checkResponse(cmdID, request, response, peers, write)
}

// writtenResponse is response — a form [payloadValue] does not judge — as
// write puts it on the wire, decoded through resp's decoder: rv the
// generated form, ok whether it is judged further, err the FAILURE of a
// mandatory field the bytes lack. Nothing is judged without write, for an
// absent response (payloadValue judged it) or a response command without
// a decoder.
func (i *Instance) writtenResponse(req, resp *Command, response any, write im.CommandFieldsWriter) (reflect.Value, bool, error) {
	if write == nil || response == nil || resp.Decode == nil {
		return reflect.Value{}, false, nil
	}
	enc := tlv.NewEncoder()
	write(enc, tlv.AnonymousTag(), response)
	raw, err := enc.Bytes()
	if err != nil || len(raw) == 0 {
		return reflect.Value{}, false, nil //nolint:nilerr // a response the written bytes do not decode is not judged further; its types and constraints are not the response check's
	}
	dec := tlv.NewDecoder(raw)
	open, err := dec.Next()
	if err != nil || !open.IsContainer || open.Type != tlv.TypeStructure {
		return reflect.Value{}, false, nil //nolint:nilerr // a response the written bytes do not decode is not judged further; its types and constraints are not the response check's
	}
	n, err := ReadContainer(dec, open)
	if err != nil {
		return reflect.Value{}, false, nil //nolint:nilerr // a response the written bytes do not decode is not judged further; its types and constraints are not the response check's
	}
	present := map[uint32]bool{}
	for _, c := range n.Children {
		if c.El.Tag.Kind == tlv.TagKindContext {
			present[c.El.Tag.Number] = true
		}
	}
	for fi := range resp.Fields {
		if f := &resp.Fields[fi]; f.Conformance.Op == ConfMandatory && !present[f.ID] {
			return reflect.Value{}, false, Errorf(im.StatusFailure, "%s: response validation failed for %s: %s.%s: mandatory field missing from the written response %T",
				i.def.Name, req.Name, resp.Name, f.Name, response)
		}
	}
	generated, err := resp.Decode(n)
	if err != nil {
		return reflect.Value{}, false, nil //nolint:nilerr // a response the written bytes do not decode is not judged further; its types and constraints are not the response check's
	}
	rv, ok := payloadValue(resp, generated, true)
	return rv, ok, nil
}

// payloadValue is a request or response payload v of cmd as the struct
// value the definition's checks read: an empty struct for an absent one
// (nil; a nil pointer too where nilPointerAbsent); ok is false for a value
// not judged. Judged are the definition's own generated form of cmd (its
// decoder's type, [Command.payloadType]) and a type that declares how it
// carries the definition ([MapFields], [DefinitionFormer]); a foreign
// type whose Go field names happen to match the definition's is not
// judged — a server written against its own types states the mapping,
// never by matching names.
func payloadValue(cmd *Command, v any, nilPointerAbsent bool) (reflect.Value, bool) {
	if v == nil {
		return reflect.ValueOf(struct{}{}), true
	}
	if d, ok := v.(DefinitionFormer); ok {
		v = d.DefinitionForm()
	}
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return reflect.ValueOf(struct{}{}), nilPointerAbsent
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return reflect.Value{}, false
	}
	if _, declared := mappedType(rv.Type()); declared {
		for fi := range cmd.Fields {
			if fv, mapped := mappedField(rv, cmd.Fields[fi].Name); !mapped || !fv.IsValid() {
				return reflect.Value{}, false
			}
		}
		return rv, true
	}
	if t := cmd.payloadType(); t != nil && rv.Type() == t {
		return rv, true
	}
	return reflect.Value{}, false
}

// payloadTypes caches [Command.payloadType]: *Command → reflect.Type.
var payloadTypes sync.Map

// payloadType is the Go type of cmd's generated payload — the type its
// decoder returns, read from the decoder itself on an empty structure
// (a generated decoder returns its type's value with or without an
// error), a pointer type dereferenced as [payloadValue] dereferences the
// payload it compares with it; nil for a command without a decoder or
// one that returns an untyped nil there (see [Command.Decode]).
func (c *Command) payloadType() reflect.Type {
	if t, ok := payloadTypes.Load(c); ok {
		rt, _ := t.(reflect.Type)
		return rt
	}
	if c.Decode == nil {
		return nil
	}
	v, _ := c.Decode(Node{El: tlv.Element{Type: tlv.TypeStructure, IsContainer: true}})
	t := reflect.TypeOf(v)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	payloadTypes.Store(c, t)
	return t
}

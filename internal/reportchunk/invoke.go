// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package reportchunk

import (
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/internal/valueenc"
	"thingfabric.dev/go-fabric/tlv"
)

// InvokeFailure is a command response the resolver could not encode: the
// request's command path it answered FAILURE for instead, and why.
type InvokeFailure struct {
	Path im.ConcreteCommandPath
	Err  error
}

// CommandWriter is write that also writes an [Encoded] response verbatim.
func CommandWriter(write im.CommandFieldsWriter) im.CommandFieldsWriter {
	return func(enc *tlv.Encoder, tag tlv.Tag, fields any) {
		if x, ok := fields.(Encoded); ok {
			_ = enc.PutRawElement(tag, x)
			return
		}
		write(enc, tag, fields)
	}
}

// ResolveInvoke is resp with every command response resolved once
// ([Encoded]): written by write alone, on an encoder of its own under a
// recover ([valueenc.Encode]). A response whose encoder panics, leaves a
// container open or writes other than one element is answered by a
// CommandStatusIB FAILURE on the request's command path with its
// CommandRef — the command has run, its effect stays — and failed, when
// not nil, hears of it. As connectedhomeip, which on a failing response
// encode logs and adds a FAILURE status for the request path
// (src/app/CommandHandlerImpl.cpp:232-240 at 6170af84). resp is not
// modified; the responses that are already statuses are kept as they are.
//
// The request's command is the invocation the entry answers: the one
// with the entry's CommandRef — every command of a batch carries a
// distinct one ([im.ValidateInvokeBatch]) and an entry carries its
// invocation's — or the request's only invocation. An entry neither names
// keeps the path it carries.
func ResolveInvoke(req im.InvokeRequest, resp im.InvokeResponse, write im.CommandFieldsWriter, failed func(InvokeFailure)) im.InvokeResponse {
	var out []im.InvokeResponseEntry
	for i, ent := range resp.Responses {
		if ent.IsStatus || !ent.HasResponse {
			continue
		}
		if _, ok := ent.Response.(Encoded); ok {
			continue
		}
		if out == nil {
			out = append([]im.InvokeResponseEntry(nil), resp.Responses...)
		}
		response := ent.Response
		el, err := valueenc.Encode(func(enc *tlv.Encoder, tag tlv.Tag) { write(enc, tag, response) })
		if err == nil {
			out[i].Response = Encoded(el)
			continue
		}
		path := ent.Path
		if cmd, ok := requestCommand(req, ent); ok {
			path.Command = cmd
		}
		if failed != nil {
			failed(InvokeFailure{Path: path, Err: err})
		}
		out[i] = im.InvokeResponseEntry{
			Path: path, CommandRef: ent.CommandRef, HasCommandRef: ent.HasCommandRef,
			IsStatus: true, Status: im.StatusIB{Status: im.StatusFailure},
		}
	}
	if out != nil {
		resp.Responses = out
	}
	return resp
}

// requestCommand is the command id of the invocation ent answers
// ([ResolveInvoke]).
func requestCommand(req im.InvokeRequest, ent im.InvokeResponseEntry) (uint32, bool) {
	if ent.HasCommandRef {
		for _, inv := range req.Invokes {
			if inv.HasCommandRef && inv.CommandRef == ent.CommandRef {
				return inv.Path.Command, true
			}
		}
	}
	if len(req.Invokes) == 1 {
		return req.Invokes[0].Path.Command, true
	}
	return 0, false
}

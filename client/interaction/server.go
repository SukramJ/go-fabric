// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"

	"thingfabric.dev/go-fabric/client/exchange"
	"thingfabric.dev/go-fabric/cluster/wire"
	"thingfabric.dev/go-fabric/im"
	"thingfabric.dev/go-fabric/internal/reportchunk"
	"thingfabric.dev/go-fabric/internal/valueenc"
	"thingfabric.dev/go-fabric/tlv"
)

// ServerConfig is the Interaction Model server the client stack serves on
// endpoints of its own — matter.js's one InteractionServer on a node that
// is also a controller: the ClientSubscriptionHandler is installed into
// it (Peers.ts:589-590, InteractionServer.ts:302-304 at db021da0) and its
// onNewExchange hands a first ReportData to the client
// handler and every other first message to the server
// (InteractionServer.ts:240-249). The
// controller role composes the endpoints (a camera controller's
// WebRtcTransportRequestor, an OTA provider) and installs them here with
// [Subscriptions.Serve]; a camera then reaches the requestor over the
// session the controller opened, as chip's camera-app attaches to the
// newest live session with its peer (OperationalSessionSetup.cpp:75-96 at
// 6170af84).
type ServerConfig struct {
	// Dispatcher serves the endpoints: endpoint.NewTopologyDispatcher
	// over the role's tree, with its ACL lister set. It is also the
	// [im.ACLChecker] the handlers consult.
	Dispatcher im.Dispatcher
	// Qualities answers the wildcard-read path flags for the served
	// clusters; nil skips no Diagnostics cluster and knows no Fixed
	// attribute beyond the globals.
	Qualities im.ElementQualities
	// MaxPathsPerInvoke bounds the invokes of one InvokeRequest
	// (InteractionServer.ts:970-1021); 0 is 1, the Matter default.
	MaxPathsPerInvoke int
	// FieldsReader, FieldsWriter and ValueWriter are the codecs of the
	// served clusters; nil is the application codec of the generated
	// definitions ([wire.CommandFieldsReader], [wire.CommandFieldsWriter],
	// [wire.AttributeValueWriter]) — every generated server encodes through
	// it.
	FieldsReader im.CommandFieldsReader
	FieldsWriter im.CommandFieldsWriter
	ValueWriter  im.AttributeValueWriter
	// ValueReader decodes a written value; nil is [wire.AttributeValueReader].
	ValueReader im.AttributeValueReader
	// Dispatch, when set, returns the dispatcher of each request in place of
	// Dispatcher: a role whose endpoint tree is re-assembled serves the tree
	// of the moment (a node's bridge, whose dispatcher each assembly
	// replaces). A nil answer — no tree yet — answers the request Failure.
	Dispatch func() im.Dispatcher
	// RequestContext, when set, adds the role's own facts of the session to
	// each request's context — what the role's own server stamps for a
	// session of its own — on top of the server's fabric, subject, node id
	// and qualities; the timed and Large Message stamps follow it.
	RequestContext func(ctx context.Context, s *exchange.Session) context.Context
}

// server is the installed [ServerConfig].
type server struct {
	cfg ServerConfig
	log *slog.Logger
	clk exchange.Clock
}

// Serve installs cfg: from now on a ReadRequest, WriteRequest,
// InvokeRequest or TimedRequest that opens an exchange is served by its
// dispatcher; a SubscribeRequest stays InvalidAction (the served endpoints
// publish no subscription). A config with neither Dispatcher nor
// Dispatch uninstalls, back to the report-only handler.
func (r *Subscriptions) Serve(cfg ServerConfig) {
	if cfg.Dispatcher == nil && cfg.Dispatch == nil {
		r.server.Store(nil)
		return
	}
	if cfg.MaxPathsPerInvoke <= 0 {
		cfg.MaxPathsPerInvoke = 1
	}
	if cfg.FieldsReader == nil {
		cfg.FieldsReader = wire.CommandFieldsReader
	}
	if cfg.FieldsWriter == nil {
		cfg.FieldsWriter = wire.CommandFieldsWriter
	}
	if cfg.ValueWriter == nil {
		cfg.ValueWriter = wire.AttributeValueWriter
	}
	if cfg.ValueReader == nil {
		cfg.ValueReader = wire.AttributeValueReader
	}
	r.server.Store(&server{cfg: cfg, log: r.log, clk: r.clock})
}

// serverFor reports the installed server when it serves opcode.
func (r *Subscriptions) serverFor(opcode uint8) *server {
	s := r.server.Load()
	if s == nil {
		return nil
	}
	switch opcode {
	case im.OpcodeReadRequest, im.OpcodeWriteRequest, im.OpcodeInvokeRequest, im.OpcodeTimedRequest:
		return s
	}
	return nil
}

// serverRef is the atomic slot [Subscriptions] keeps the server in.
type serverRef = atomic.Pointer[server]

// handle serves one exchange a peer opened with first — matter.js
// InteractionServerMessenger.handleRequest: the message is decoded and
// dispatched by type, and a request that fails is answered with the status
// its error carries, FAILURE otherwise
// (packages/protocol/src/interaction/InteractionMessenger.ts:344, :431-435 at
// db021da0), sent unless the request asked to suppress its response
// (:458-460, :317-322; the flag read from a request whose other fields do
// not decode, :333-342, :153-162); the exchange is closed afterwards. The
// bridge answers an undecodable request the same way
// (bridge.replyDecodeError); review II, R2-P6-par-0701.
func (s *server) handle(ctx context.Context, x *exchange.Exchange, first *exchange.Message) {
	defer x.Close()
	status := s.serve(ctx, x, first, false)
	if status != im.StatusSuccess {
		s.sendStatus(ctx, x, status)
	}
}

// serve dispatches one request message. timed says the message arrived
// inside a timed interaction this exchange opened. A non-Success result is
// the StatusResponse the caller sends.
func (s *server) serve(ctx context.Context, x *exchange.Exchange, msg *exchange.Message, timed bool) im.StatusCode {
	dec := tlv.NewDecoder(msg.Payload)
	switch msg.Opcode {
	case im.OpcodeReadRequest:
		req, err := im.UnmarshalReadRequestTLV(dec)
		if err != nil {
			s.log.Debug("matter.controller.interaction.server.read_decode", slog.Any("err", err))
			return decodeFailure(err)
		}
		return s.read(ctx, x, req)
	case im.OpcodeWriteRequest:
		req, err := im.UnmarshalWriteRequestTLV(dec, s.cfg.ValueReader)
		if err != nil {
			s.log.Debug("matter.controller.interaction.server.write_decode", slog.Any("err", err))
			return decodeFailureOf(msg.Payload, err)
		}
		return s.write(ctx, x, req, timed)
	case im.OpcodeInvokeRequest:
		req, err := im.UnmarshalInvokeRequestTLV(dec, s.cfg.FieldsReader)
		if err != nil {
			s.log.Debug("matter.controller.interaction.server.invoke_decode", slog.Any("err", err))
			return decodeFailureOf(msg.Payload, err)
		}
		return s.invoke(ctx, x, req, timed)
	case im.OpcodeTimedRequest:
		req, err := im.UnmarshalTimedRequestTLV(dec)
		if err != nil {
			s.log.Debug("matter.controller.interaction.server.timed_decode", slog.Any("err", err))
			return decodeFailure(err)
		}
		return s.timedInteraction(ctx, x, req)
	}
	return im.StatusInvalidAction
}

// decodeFailure is the status a request that does not decode is answered
// with: the one its error carries ([im.StatusCodeError]), FAILURE
// otherwise ([server.handle]).
func decodeFailure(err error) im.StatusCode {
	var sce im.StatusCodeError
	if errors.As(err, &sce) {
		return sce.MatterStatusCode()
	}
	return im.StatusFailure
}

// decodeFailureOf is [decodeFailure] for a Write or Invoke request, whose
// SuppressResponse is read from payload on its own
// ([im.SuppressResponseOf]): a suppressed request is answered with nothing
// (Success: [server.handle] sends no status).
func decodeFailureOf(payload []byte, err error) im.StatusCode {
	if im.SuppressResponseOf(payload) {
		return im.StatusSuccess
	}
	return decodeFailure(err)
}

// dispatcher is the dispatcher a request is served by
// ([ServerConfig].Dispatch, else Dispatcher); nil when the role has no tree.
func (s *server) dispatcher() im.Dispatcher {
	if s.cfg.Dispatch != nil {
		return s.cfg.Dispatch()
	}
	return s.cfg.Dispatcher
}

// requestContext is the context a served request runs under: the
// session's fabric and subject, as the device-side bridge builds it
// (bridge/receive_dispatch.go) — only a CASE session reaches it
// (HandleExchange refuses any other), so no PASE mark; our node id on the session's fabric, so a path that names
// another node is answered as the device side answers it (UnsupportedNode
// for a concrete path, nothing for a wildcard; review II,
// R2-P3-ctl-dev-03; matter.js AttributeReadResponse.ts:160-162 and
// :205-208 at db021da0); the wildcard-read qualities when configured.
func (s *server) requestContext(ctx context.Context, x *exchange.Exchange, fabricFiltered bool) context.Context {
	sess := x.Session()
	ctx = im.WithFabricFilter(ctx, fabricFiltered, sess.FabricIndex())
	ctx = im.WithSubject(ctx, sess.PeerNodeID(), sess.CaseAuthenticatedTags())
	ctx = im.WithLocalNodeID(ctx, sess.NodeID())
	if s.cfg.Qualities != nil {
		ctx = im.WithElementQualities(ctx, s.cfg.Qualities)
	}
	if s.cfg.RequestContext != nil {
		ctx = s.cfg.RequestContext(ctx, sess)
	}
	return ctx
}

// read is InteractionServer.handleReadRequest (InteractionServer.ts:335-390):
// the paths validated (a wildcard cluster is InvalidAction, :133), the
// report generated and sent as sendDataReport sends it
// (InteractionMessenger.ts:470-510): chunk by chunk in the session's
// payload size, every chunk but the last answered by the peer's
// StatusResponse before the next is generated — the reports pulled from
// [im.NewReadReports] through the chunker the device side uses
// ([reportchunk.Chunker]; review II, decision 44, R2-P5-write-08). The
// served endpoints log no events, so an event request reports none.
func (s *server) read(ctx context.Context, x *exchange.Exchange, req im.ReadRequest) im.StatusCode {
	if status := im.ValidateReadPaths(req.AttributeRequests, req.EventRequests); status != im.StatusSuccess {
		return status
	}
	d := s.dispatcher()
	if d == nil {
		return im.StatusFailure
	}
	rctx := s.requestContext(ctx, x, req.FabricFiltered)
	budget := x.Session().MaxPayloadSize()
	write := reportchunk.Writer(s.cfg.ValueWriter)
	chunker := reportchunk.New(im.ReportData{SuppressResponse: true}, budget, budget, write, reportchunk.Reads(im.NewReadReports(rctx, d, req)), nil, s.encodeFailed)
	for {
		chunk, ok, err := chunker.Next()
		if err == nil && !ok {
			return im.StatusSuccess
		}
		var body []byte
		if err == nil {
			body, err = reportchunk.Encode(chunk, write)
		}
		if err != nil {
			s.log.Debug("matter.controller.interaction.server.report_encode", slog.Any("err", err))
			return im.StatusFailure
		}
		last := !chunk.MoreChunkedMessages
		if err := x.Send(ctx, im.OpcodeReportData, body, exchange.SendOptions{ExpectAckOnly: last}); err != nil {
			s.log.Debug("matter.controller.interaction.server.report_send", slog.Any("err", err))
			return im.StatusSuccess
		}
		if last || !s.awaitChunkStatus(ctx, x) {
			return im.StatusSuccess
		}
	}
}

// awaitChunkStatus reads the StatusResponse a peer owes for a chunk that
// announced more (InteractionMessenger.ts:509-510, sendAndResetReport:
// each chunk goes out through sendDataReportMessage, which reads the
// status); a status other than Success, or anything else, ends the report.
func (s *server) awaitChunkStatus(ctx context.Context, x *exchange.Exchange) bool {
	msg, err := x.Next(ctx, exchange.ReceiveOptions{})
	if err != nil || msg.Opcode != im.OpcodeStatusResponse {
		return false
	}
	st, err := im.UnmarshalStatusResponseTLV(tlv.NewDecoder(msg.Payload))
	return err == nil && st.Status == im.StatusSuccess
}

// write is InteractionServer.handleWriteRequest (InteractionServer.ts:392-452):
// the timed flag must match the exchange's timed interaction
// (TimedRequestMismatch, :434), then the writes run and the response goes
// back unless suppressed. An interaction that ended at a data element
// ([im.WriteResponse.Status]) is answered with that status in a
// StatusResponse instead, unless suppressed: matter.js throws it out of
// AttributeWriteResponse.process
// (packages/protocol/src/action/server/AttributeWriteResponse.ts:59-66 at
// db021da0) and the messenger answers it
// (packages/protocol/src/interaction/InteractionMessenger.ts:457-459),
// sending no status for a request that suppressed its response (:317-322).
//
// A chunked write (MoreChunkedMessages) is refused whole with FAILURE
// before any of it is applied: this server answers one WriteRequest per
// exchange and would apply the first chunk alone — an ACL replace would
// leave the list at its first chunk. It fails closed until ADR 0032's
// single session layer serves these sessions with the bridge's server
// (review II, F-db021da0-51).
func (s *server) write(ctx context.Context, x *exchange.Exchange, req im.WriteRequest, timed bool) im.StatusCode {
	if req.TimedRequest != timed {
		return im.StatusTimedRequestMismatch
	}
	if req.MoreChunkedMessages {
		return im.StatusFailure
	}
	d := s.dispatcher()
	if d == nil {
		return im.StatusFailure
	}
	wctx := s.requestContext(ctx, x, false)
	wctx = im.WithTimedInteraction(wctx, timed)
	resp := im.HandleWriteRequest(wctx, d, req)
	if !resp.Status.IsSuccess() && !req.SuppressResponse {
		return resp.Status
	}
	if req.SuppressResponse {
		return im.StatusSuccess
	}
	enc := tlv.NewEncoder()
	resp.MarshalTLV(enc)
	body, err := enc.Bytes()
	if err != nil {
		return im.StatusFailure
	}
	if err := x.Send(ctx, im.OpcodeWriteResponse, body, exchange.SendOptions{ExpectAckOnly: true}); err != nil {
		s.log.Debug("matter.controller.interaction.server.write_send", slog.Any("err", err))
	}
	return im.StatusSuccess
}

// invoke is InteractionServer.handleInvokeRequest (InteractionServer.ts:970-1025):
// the timed flag must match the exchange's timed interaction
// (TimedRequestMismatch, :999-1003), the batch is bounded by
// maxPathsPerInvoke (InvalidAction, :1017-1021), the invokes run, each
// response names its own command, and the response goes back in chunks of
// the session's payload size unless suppressed.
func (s *server) invoke(ctx context.Context, x *exchange.Exchange, req im.InvokeRequest, timed bool) im.StatusCode {
	if req.TimedRequest != timed {
		return im.StatusTimedRequestMismatch
	}
	if status := im.ValidateInvokeBatchWithin(req, s.cfg.MaxPathsPerInvoke); status != im.StatusSuccess {
		return status
	}
	ictx := s.requestContext(ctx, x, false)
	ictx = im.WithTimedInteraction(ictx, timed)
	// The session carries large messages exactly when it is bound to a TCP
	// channel; the invoke path refuses a Large Message command otherwise
	// ([im.WithLargeMessageSession]).
	ictx = im.WithLargeMessageSession(ictx, x.Session().Transport() == exchange.TransportTCP)
	after := &im.AfterResponse{}
	defer after.Run()
	ictx = im.WithAfterResponse(ictx, after)
	d := s.dispatcher()
	if d == nil {
		return im.StatusFailure
	}
	resp := im.HandleInvokeRequest(ictx, d, req)
	for i := range resp.Responses {
		wire.ResponseCommandOf(&resp.Responses[i])
	}
	if req.SuppressResponse {
		return im.StatusSuccess
	}
	// Every response is resolved once, in isolation: one that cannot be
	// encoded answers FAILURE for its command (#625).
	resp = reportchunk.ResolveInvoke(req, resp, s.cfg.FieldsWriter, s.invokeEncodeFailed)
	write := reportchunk.CommandWriter(s.cfg.FieldsWriter)
	chunks := s.chunkInvokeResponse(resp, x.Session().MaxPayloadSize(), write)
	for i, chunk := range chunks {
		last := i == len(chunks)-1
		chunk.MoreChunkedMessages = !last
		enc := tlv.NewEncoder()
		chunk.MarshalTLV(enc, write)
		body, err := enc.Bytes()
		if err != nil {
			s.log.Debug("matter.controller.interaction.server.invoke_encode", slog.Any("err", err))
			return im.StatusFailure
		}
		if err := x.Send(ctx, im.OpcodeInvokeResponse, body, exchange.SendOptions{ExpectAckOnly: last}); err != nil {
			s.log.Debug("matter.controller.interaction.server.invoke_send", slog.Any("err", err))
			return im.StatusSuccess
		}
		if !last && !s.awaitChunkStatus(ctx, x) {
			return im.StatusSuccess
		}
	}
	return im.StatusSuccess
}

// chunkInvokeResponse splits a response whose encoding exceeds budget one
// entry at a time; an entry above the budget on its own becomes a
// ResourceExhausted status for its path (bridge/receive_dispatch.go
// chunkInvokeResponse).
func (s *server) chunkInvokeResponse(resp im.InvokeResponse, budget int, write im.CommandFieldsWriter) []im.InvokeResponse {
	size := func(r im.InvokeResponse) int {
		enc := tlv.NewEncoder()
		r.MarshalTLV(enc, write)
		b, _ := enc.Bytes()
		return len(b)
	}
	if size(resp) <= budget {
		return []im.InvokeResponse{resp}
	}
	var chunks []im.InvokeResponse
	cur := im.InvokeResponse{SuppressResponse: resp.SuppressResponse}
	for _, ent := range resp.Responses {
		one := im.InvokeResponse{SuppressResponse: resp.SuppressResponse, Responses: []im.InvokeResponseEntry{ent}, MoreChunkedMessages: true}
		if size(one) > budget {
			ent = im.InvokeResponseEntry{
				Path: ent.Path, CommandRef: ent.CommandRef, HasCommandRef: ent.HasCommandRef,
				IsStatus: true, Status: im.StatusIB{Status: im.StatusResourceExhausted},
			}
		}
		trial := cur
		trial.Responses = append(append([]im.InvokeResponseEntry(nil), cur.Responses...), ent)
		trial.MoreChunkedMessages = true
		if size(trial) > budget && len(cur.Responses) > 0 {
			chunks = append(chunks, cur)
			cur = im.InvokeResponse{SuppressResponse: resp.SuppressResponse}
		}
		cur.Responses = append(cur.Responses, ent)
	}
	return append(chunks, cur)
}

// timedInteraction is handleTimedRequest as the messenger runs it
// (InteractionMessenger.ts:400-410): the request is answered Success and
// the exchange waits for the write or invoke it announces, which must
// arrive before the timeout — a message after it is answered Timeout, as
// an expired timed interaction is (InteractionServer.ts:994-997) — and
// must carry the timed flag (:999-1003, checked by invoke and write).
func (s *server) timedInteraction(ctx context.Context, x *exchange.Exchange, req im.TimedRequest) im.StatusCode {
	s.sendStatus(ctx, x, im.StatusSuccess)
	window := time.Duration(req.TimeoutMs) * time.Millisecond
	deadline := s.clk.Now().Add(window)
	msg, err := x.Next(ctx, exchange.ReceiveOptions{Timeout: window})
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			s.log.Debug("matter.controller.interaction.server.timed_wait", slog.Any("err", err))
		}
		return im.StatusSuccess
	}
	if s.clk.Now().After(deadline) {
		return im.StatusTimeout
	}
	switch msg.Opcode {
	case im.OpcodeWriteRequest, im.OpcodeInvokeRequest:
		return s.serve(ctx, x, msg, true)
	}
	return im.StatusInvalidAction
}

func (s *server) sendStatus(ctx context.Context, x *exchange.Exchange, status im.StatusCode) {
	enc := tlv.NewEncoder()
	im.StatusResponse{Status: status}.MarshalTLV(enc)
	body, err := enc.Bytes()
	if err != nil {
		return
	}
	if err := x.Send(ctx, im.OpcodeStatusResponse, body, exchange.SendOptions{ExpectAckOnly: true}); err != nil {
		s.log.Debug("matter.controller.interaction.server.status_send", slog.Any("err", err))
	}
}

// encodeFailed logs a value the chunker could not encode: its path answers
// FAILURE and the rest of the read goes out ([reportchunk.EncodeFailure];
// #625). The error at ERROR, a panic's stack at DEBUG ([s.logEncodeFailure]).
func (s *server) encodeFailed(f reportchunk.EncodeFailure) {
	if f.IsEvent {
		s.logEncodeFailure("matter.controller.interaction.server.encode_failed", f.Err,
			slog.Int("endpoint", int(f.EventPath.Endpoint)),
			slog.Uint64("cluster", uint64(f.EventPath.Cluster)),
			slog.Uint64("event", uint64(f.EventPath.Event)))
		return
	}
	s.logEncodeFailure("matter.controller.interaction.server.encode_failed", f.Err,
		slog.Int("endpoint", int(f.AttributePath.Endpoint)),
		slog.Uint64("cluster", uint64(f.AttributePath.Cluster)),
		slog.Uint64("attribute", uint64(f.AttributePath.Attribute)))
}

// logEncodeFailure logs the path and the error at ERROR and, for a panic,
// its stack at DEBUG: a value that fails on every read puts no stack into
// the log at the default level, and the error names a panic value's type,
// never the value ([valueenc.PanicError]).
func (s *server) logEncodeFailure(msg string, err error, path ...slog.Attr) {
	args := make([]any, 0, len(path)+1)
	for _, a := range path {
		args = append(args, a)
	}
	s.log.Error(msg, append(args, slog.Any("err", err))...)
	if stack := valueenc.PanicStack(err); stack != nil {
		s.log.Debug(msg, append(args[:len(path):len(path)], slog.String("stack", string(stack)))...)
	}
}

// invokeEncodeFailed logs a command response that could not be encoded:
// the command answers FAILURE ([reportchunk.ResolveInvoke]; #625).
func (s *server) invokeEncodeFailed(f reportchunk.InvokeFailure) {
	s.logEncodeFailure("matter.controller.interaction.server.invoke_encode_failed", f.Err,
		slog.Int("endpoint", int(f.Path.Endpoint)),
		slog.Uint64("cluster", uint64(f.Path.Cluster)),
		slog.Uint64("command", uint64(f.Path.Command)))
}

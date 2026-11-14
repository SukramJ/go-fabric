// SPDX-License-Identifier: Apache-2.0
// Copyright (C) 2026 SukramJ.

package interaction

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/SukramJ/go-fabric/client/exchange"
	"github.com/SukramJ/go-fabric/cluster/wire"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
)

// ServerConfig is the Interaction Model server the client stack serves on
// endpoints of its own — matter.js's one InteractionServer on a node that
// is also a controller: the ClientSubscriptionHandler is installed into
// it (Peers.ts:589-590, InteractionServer.ts:218-228 at 862ad75f) and its
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
// publish no subscription). A nil Dispatcher uninstalls, back to the
// report-only handler.
func (r *Subscriptions) Serve(cfg ServerConfig) {
	if cfg.Dispatcher == nil {
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
// InteractionServerMessenger.handleRequest (InteractionMessenger.ts:344-459
// at 862ad75f): the message is decoded and dispatched by type, a
// StatusResponseError on the way is answered as a StatusResponse with its
// code (:425-459), a decode failure as InvalidAction, and the exchange is
// closed afterwards (:462).
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
			return im.StatusInvalidAction
		}
		return s.read(ctx, x, req)
	case im.OpcodeWriteRequest:
		req, err := im.UnmarshalWriteRequestTLV(dec, s.cfg.ValueReader)
		if err != nil {
			s.log.Debug("matter.controller.interaction.server.write_decode", slog.Any("err", err))
			return im.StatusInvalidAction
		}
		return s.write(ctx, x, req, timed)
	case im.OpcodeInvokeRequest:
		req, err := im.UnmarshalInvokeRequestTLV(dec, s.cfg.FieldsReader)
		if err != nil {
			s.log.Debug("matter.controller.interaction.server.invoke_decode", slog.Any("err", err))
			return im.StatusInvalidAction
		}
		return s.invoke(ctx, x, req, timed)
	case im.OpcodeTimedRequest:
		req, err := im.UnmarshalTimedRequestTLV(dec)
		if err != nil {
			s.log.Debug("matter.controller.interaction.server.timed_decode", slog.Any("err", err))
			return im.StatusInvalidAction
		}
		return s.timedInteraction(ctx, x, req)
	}
	return im.StatusInvalidAction
}

// requestContext is the context a served request runs under: the
// session's fabric and subject, as the device-side bridge builds it
// (bridge/receive_dispatch.go) — a client stack's sessions are CASE, so no
// PASE mark; our node id on the session's fabric, so a path that names
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
	return ctx
}

// read is InteractionServer.handleReadRequest (InteractionServer.ts:335-390):
// the paths validated (a wildcard cluster is InvalidAction, :133), the
// report assembled and sent as sendDataReport sends it
// (InteractionMessenger.ts:470-510): in chunks of the session's payload
// size, every chunk but the last answered by the peer's StatusResponse.
// The served endpoints log no events, so an event request reports none.
func (s *server) read(ctx context.Context, x *exchange.Exchange, req im.ReadRequest) im.StatusCode {
	if status := im.ValidateReadPaths(req.AttributeRequests, req.EventRequests); status != im.StatusSuccess {
		return status
	}
	rctx := s.requestContext(ctx, x, req.FabricFiltered)
	report := im.HandleReadRequest(rctx, s.cfg.Dispatcher, req)
	report.SuppressResponse = true
	chunks := s.chunkReport(report, x.Session().MaxPayloadSize())
	for i, chunk := range chunks {
		last := i == len(chunks)-1
		chunk.MoreChunkedMessages = !last
		chunk.SuppressResponse = last
		enc := tlv.NewEncoder()
		chunk.MarshalTLV(enc, s.cfg.ValueWriter)
		body, err := enc.Bytes()
		if err != nil {
			s.log.Debug("matter.controller.interaction.server.report_encode", slog.Any("err", err))
			return im.StatusFailure
		}
		if err := x.Send(ctx, im.OpcodeReportData, body, exchange.SendOptions{ExpectAckOnly: last}); err != nil {
			s.log.Debug("matter.controller.interaction.server.report_send", slog.Any("err", err))
			return im.StatusSuccess
		}
		if !last && !s.awaitChunkStatus(ctx, x) {
			return im.StatusSuccess
		}
	}
	return im.StatusSuccess
}

// chunkReport splits a report whose encoding exceeds budget into reports
// that fit, one attribute or event report at a time; a single report
// above the budget becomes a ResourceExhausted status for its path, as
// the bridge caps an oversized report (bridge/reply.go chunkReportData).
func (s *server) chunkReport(rd im.ReportData, budget int) []im.ReportData {
	size := func(r im.ReportData) int {
		enc := tlv.NewEncoder()
		r.MarshalTLV(enc, s.cfg.ValueWriter)
		b, _ := enc.Bytes()
		return len(b)
	}
	if size(rd) <= budget {
		return []im.ReportData{rd}
	}
	var chunks []im.ReportData
	cur := im.ReportData{HasSubscription: rd.HasSubscription, SubscriptionID: rd.SubscriptionID}
	for _, rep := range rd.Reports {
		one := im.ReportData{HasSubscription: rd.HasSubscription, SubscriptionID: rd.SubscriptionID, Reports: []im.AttributeReport{rep}, MoreChunkedMessages: true}
		if size(one) > budget {
			rep = im.AttributeReport{Path: rep.Path, IsStatus: true, Status: im.StatusIB{Status: im.StatusResourceExhausted}}
		}
		trial := cur
		trial.Reports = append(append([]im.AttributeReport(nil), cur.Reports...), rep)
		trial.MoreChunkedMessages = true
		if size(trial) > budget && (len(cur.Reports) > 0 || len(cur.EventReports) > 0) {
			chunks = append(chunks, cur)
			cur = im.ReportData{HasSubscription: rd.HasSubscription, SubscriptionID: rd.SubscriptionID}
		}
		cur.Reports = append(cur.Reports, rep)
	}
	for _, ev := range rd.EventReports {
		trial := cur
		trial.EventReports = append(append([]im.EventReport(nil), cur.EventReports...), ev)
		trial.MoreChunkedMessages = true
		if size(trial) > budget && (len(cur.Reports) > 0 || len(cur.EventReports) > 0) {
			chunks = append(chunks, cur)
			cur = im.ReportData{HasSubscription: rd.HasSubscription, SubscriptionID: rd.SubscriptionID}
		}
		cur.EventReports = append(cur.EventReports, ev)
	}
	return append(chunks, cur)
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
// back unless suppressed.
func (s *server) write(ctx context.Context, x *exchange.Exchange, req im.WriteRequest, timed bool) im.StatusCode {
	if req.TimedRequest != timed {
		return im.StatusTimedRequestMismatch
	}
	wctx := s.requestContext(ctx, x, false)
	wctx = im.WithTimedInteraction(wctx, timed)
	resp := im.HandleWriteRequest(wctx, s.cfg.Dispatcher, req)
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
	after := &im.AfterResponse{}
	defer after.Run()
	ictx = im.WithAfterResponse(ictx, after)
	resp := im.HandleInvokeRequest(ictx, s.cfg.Dispatcher, req)
	for i := range resp.Responses {
		wire.ResponseCommandOf(&resp.Responses[i])
	}
	if req.SuppressResponse {
		return im.StatusSuccess
	}
	chunks := s.chunkInvokeResponse(resp, x.Session().MaxPayloadSize())
	for i, chunk := range chunks {
		last := i == len(chunks)-1
		chunk.MoreChunkedMessages = !last
		enc := tlv.NewEncoder()
		chunk.MarshalTLV(enc, s.cfg.FieldsWriter)
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
func (s *server) chunkInvokeResponse(resp im.InvokeResponse, budget int) []im.InvokeResponse {
	size := func(r im.InvokeResponse) int {
		enc := tlv.NewEncoder()
		r.MarshalTLV(enc, s.cfg.FieldsWriter)
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

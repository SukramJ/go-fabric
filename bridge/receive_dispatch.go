// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package bridge

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/SukramJ/go-fabric/cluster/core"
	"github.com/SukramJ/go-fabric/im"
	"github.com/SukramJ/go-fabric/tlv"
	"github.com/SukramJ/go-fabric/transport/message"
	"github.com/SukramJ/go-fabric/transport/mrp"
)

// errChunkRejected is returned by a chunk loop when the peer answered a
// ReportData chunk with an error StatusResponse; the interaction is
// abandoned, exactly as matter.js's waitForSuccess throws
// (packages/protocol/src/interaction/InteractionMessenger.ts:183-196).
var errChunkRejected = errors.New("receive: peer rejected ReportData chunk")

// errChunkUnanswered is returned by a chunk loop when the peer never
// answered a ReportData chunk within the exchange timeout. matter.js
// aborts the interaction on that timeout (MessageExchange.ts:829-858
// PeerMessageMissingError); continuing would stream every remaining
// chunk to a peer that is not listening, holding the dispatch slot for
// the whole report.
var errChunkUnanswered = errors.New("receive: peer did not answer ReportData chunk")

// absorbStatusResponse handles an inbound StatusResponse opcode.
// StatusResponse is the spec-mandated ACK for a ReportData /
// SubscribeResponse / Invoke / Write reply we sent earlier (Matter
// §8.6.2). Apple Home, Google Home, and chip-tool all emit it
// after consuming our reply; silently absorbing it satisfies the
// MRP / Reliable Messaging contract without making the dispatcher
// see a bogus "request". Without this branch the commissioner
// retransmits its previous request indefinitely after pairing,
// which Apple eventually surfaces as "device added" → immediate
// disconnect.
func (b *Bridge) absorbStatusResponse(src *net.UDPAddr, requestHdr *message.Header, proto message.ProtocolHeader, payload []byte) error {
	// Decode the carried status: the peer's answer is only a go-ahead
	// when it is Success. matter.js reads it on every inbound
	// StatusResponse (InteractionMessenger.ts:183-196
	// throwIfErrorStatusMessage) and throws on anything else; an
	// undecodable one is treated as Failure so the waiting interaction
	// stops rather than carries on blind.
	status := im.StatusFailure
	if sr, err := im.UnmarshalStatusResponseTLV(tlv.NewDecoder(payload)); err != nil {
		b.logger.Warn("matter.rx.im.status_decode",
			slog.String("src", srcString(src)),
			slog.Int("exchange", int(proto.ExchangeID)),
			slog.String("err", err.Error()))
	} else {
		status = sr.Status
	}
	// Do NOT call dischargeOwedAck here. The previous code did
	// — the rationale "we just piggyback-acked on an outbound reply"
	// is correct for *request* opcodes that immediately produce a
	// reply, but a StatusResponse is itself the peer's IM-level
	// reply; there is no outbound on which to piggyback. Cancelling
	// the obligation that `owedInboundAck` (line ~147) registered a
	// moment earlier leaves the inbound StatusResponse silently
	// un-ACKed at the MRP layer, and chip-tool's ReliableMessaging
	// retransmits it 4 times before giving up (symptom:
	// `Dropping message without piggyback ack when we are waiting
	// for an ack`, then `CHIP Error 0x32 Timeout`). Let the ack pump
	// fire the synthesised StandaloneAck per its normal cadence so
	// the peer's MRP layer can advance to receive subsequent
	// retransmits / new exchanges.
	//
	// Subscribe-Initial chunk-streaming loop in subscribe.go blocks
	// on this signal between chunks to mirror matter.js's
	// `sendDataReportMessage(_, waitForAck=true)` round-trip
	// pattern. Apple's ReadClient emits one IM:StatusResponse per
	// inbound ReportData chunk; without per-chunk sync our burst
	// fires past Apple's state-machine and
	// `ProcessSubscribeResponse` never triggers (Run 19 of the
	// Apple-pair-diagnose cycle).
	b.signalStatusResponseRX(requestHdr.SessionID, proto.ExchangeID, !proto.Initiator, status)
	// An error status answering an ongoing subscription report — a
	// bridge-initiated exchange, so the peer speaks as the responder —
	// means the peer no longer honours the subscription
	// (InvalidSubscription) or could not process the report (Failure).
	// matter.js ServerSubscription.ts:866-876 closes the subscription on
	// either; nothing else ever would, because the report itself was
	// MRP-acked and the reply refreshes the session's activity.
	if !status.IsSuccess() && !proto.Initiator {
		b.closeSubscriptionByExchange(requestHdr.SessionID, proto.ExchangeID, status)
	}
	b.logger.Debug("matter.rx.im.status_ack",
		slog.String("src", srcString(src)),
		slog.Int("exchange", int(proto.ExchangeID)),
		slog.String("status", status.String()))
	return nil
}

// rejectUnsupportedOpcode handles any opcode that is not a request opcode
// and not StatusResponse. Returns ErrUnsupportedOpcode.
func (b *Bridge) rejectUnsupportedOpcode(src *net.UDPAddr, proto message.ProtocolHeader) error {
	err := fmt.Errorf("%w: opcode=0x%02X", ErrUnsupportedOpcode, proto.Opcode)
	b.logger.Debug("matter.rx.im.unsupported",
		slog.String("src", srcString(src)),
		slog.Int("opcode", int(proto.Opcode)),
		slog.String("err", err.Error()))
	return err
}

// rejectGroupSession handles Read/Subscribe/Timed request opcodes that
// arrived over a Secure Group session, which is forbidden by Matter §8.5.7.
// Sends StatusResponse(InvalidAction) and discharges the owed ACK.
// The matter.js citation for the group-session rule is on classifyIMOpcode.
func (b *Bridge) rejectGroupSession(src *net.UDPAddr, requestHdr *message.Header, proto message.ProtocolHeader) error {
	b.logger.Warn("matter.rx.im.group_reject",
		slog.String("src", srcString(src)),
		slog.Int("opcode", int(proto.Opcode)))
	body, err := EncodeStatusResponse(im.StatusResponse{Status: im.StatusInvalidAction})
	if err != nil {
		debugReplyError(b.logger, "encode_group_reject", src, err)
		return err
	}
	if err := b.sendReply(src, requestHdr, proto, im.OpcodeStatusResponse, body); err != nil {
		debugReplyError(b.logger, "send_group_reject", src, err)
		return err
	}
	b.dischargeOwedAck(requestHdr.SessionID, proto.ExchangeID, !proto.Initiator)
	return nil
}

// dispatchReadRequest handles a decoded ReadRequest. The TLV decode and
// dispatcher nil-check are done by the caller (handleIMOpcode); this method
// receives the already-decoded req and runs fabric-context stamping, event
// merging, chunked report encoding, and the per-chunk IM:StatusResponse wait.
func (b *Bridge) dispatchReadRequest(ctx context.Context, src *net.UDPAddr, requestHdr *message.Header, proto message.ProtocolHeader, dispatcher im.Dispatcher, req im.ReadRequest) error {
	// Diagnostic: dump every requested attribute path so we can
	// identify which spec-conformance probe Apple Home runs
	// post-CommissioningComplete. The 27/28-byte read requests
	// that immediately precede RemoveFabric carry one
	// AttributePath each — logging endpoint/cluster/attribute
	// triples reveals exactly what Apple's iCloud-Heim sync is
	// reading and rejecting.
	for _, p := range req.AttributeRequests {
		b.logger.Debug("matter.rx.im.read_path",
			slog.String("src", srcString(src)),
			slog.Any("endpoint", p.Endpoint),
			slog.String("endpoint_set", strconv.FormatBool(p.HasEndpoint)),
			slog.Any("cluster", p.Cluster),
			slog.String("cluster_set", strconv.FormatBool(p.HasCluster)),
			slog.Any("attribute", p.Attribute),
			slog.String("attribute_set", strconv.FormatBool(p.HasAttribute)))
	}
	// Reject illegal paths up front (wildcard cluster + concrete non-global
	// attribute, or wildcard cluster + concrete event) with a top-level
	// InvalidAction StatusResponse, and a request beyond the path ceiling
	// with PathsExhausted. Mirrors matter.js InteractionServer.ts
	// validateReadPaths (#3926, Matter §8.4.3.2) and the MAX_READ_PATHS
	// check that follows it (:366-369).
	if status := im.ValidateReadPaths(req.AttributeRequests, req.EventRequests); status != im.StatusSuccess {
		body, err := EncodeStatusResponse(im.StatusResponse{Status: status})
		if err != nil {
			debugReplyError(b.logger, "encode_read_path_reject", src, err)
			return err
		}
		if err := b.sendReply(src, requestHdr, proto, im.OpcodeStatusResponse, body); err != nil {
			debugReplyError(b.logger, "send_read_path_reject", src, err)
			return err
		}
		b.dischargeOwedAck(requestHdr.SessionID, proto.ExchangeID, !proto.Initiator)
		return nil
	}
	// Stamp the FabricFiltered flag + the requesting FabricIndex
	// into the context so fabric-scoped cluster servers
	// (OperationalCredentials, AccessControl) can project their
	// fabric-sensitive list attributes to the requesting fabric.
	// Mirrors matter.js InteractionServer.ts:startReadInteraction →
	// OnlineContext.forFabricFilteredRead + fabricIndex.
	readFabricIndex := b.resolveSessionFabric(requestHdr.SessionID)
	readSubjectNodeID, readSubjectCATs := b.resolveSessionSubject(requestHdr.SessionID)
	readCtx := im.WithFabricFilter(ctx, req.FabricFiltered, readFabricIndex)
	readCtx = im.WithSubject(readCtx, readSubjectNodeID, readSubjectCATs)
	readPASE := b.resolveSessionPASE(requestHdr.SessionID)
	if readPASE {
		readCtx = im.WithAuthModePASE(readCtx)
	}
	report := im.HandleReadRequest(readCtx, dispatcher, req)
	// Evaluate EventRequests against the persistent event log so
	// chip-tool `read-event-by-id` and Apple MTRDevice liveness
	// checks return the buffered StartUp / BootReason / etc. events.
	// Matter §10.6.6: a ReadRequest may carry both AttributeRequests
	// and EventRequests; the bridge merges both into one ReportData.
	//
	// Gate the event reads by ACL + fabric-sensitive filtering the same
	// way HandleReadRequest gates attribute reads: a wildcard event read
	// must not leak another fabric's AccessControl events, and a
	// non-Administer subject must not read AccessControl events (Matter
	// §8.4.3.2 / §9.10.7.1). Mirrors matter.js EventReadResponse.ts
	// #readAllowedEvents.
	if len(req.EventRequests) > 0 {
		auth := b.eventReadAuthorizer(dispatcher, readFabricIndex, readPASE, readSubjectNodeID, readSubjectCATs)
		report.EventReports = append(im.DeniedEventPathStatuses(readCtx, auth, req.EventRequests),
			im.AuthorizeEventReports(readCtx, auth, im.HandleReadEventRequest(req, b.eventLog))...)
	}
	// Diagnostic: show what we returned per path.
	for i, r := range report.Reports {
		b.logger.Debug("matter.tx.im.read_report",
			slog.Int("idx", i),
			slog.Any("endpoint", r.Path.Endpoint),
			slog.Any("cluster", r.Path.Cluster),
			slog.Any("attribute", r.Path.Attribute),
			slog.Bool("status", r.IsStatus),
			slog.Any("status_code", r.Status.Status),
			slog.String("value_type", fmt.Sprintf("%T", r.Value.Value)),
			slog.Any("value", r.Value.Value))
	}
	if len(report.EventReports) > 0 {
		b.logger.Debug("matter.tx.im.read_event_reports",
			slog.Int("event_reports", len(report.EventReports)))
	}
	chunks, err := chunkReportData(report, reportChunkPayloadBudget)
	if err != nil {
		debugReplyError(b.logger, "chunk_report", src, err)
		return err
	}
	for i, chunk := range chunks {
		body, err := EncodeReportData(chunk)
		if err != nil {
			debugReplyError(b.logger, "encode_report", src, err)
			return err
		}
		// Mirror the Subscribe path's per-chunk IM:StatusResponse wait
		// so Apple Home's MTRDevice processes each ReportData frame on
		// the IM layer before the next arrives. matter.js's
		// InteractionMessenger acks every non-final chunk on the IM layer
		// (not just MRP); without the wait go-fabric
		// burst-fires all chunks back-to-back and Apple's
		// `ProcessReadResponse` state machine drops late chunks.
		//
		// A chunk carrying SuppressResponse=true (only the terminal chunk
		// of a plain Read — see [im.HandleReadRequest]) expects nothing but
		// a Standalone MRP-Ack, so the controller never emits an IM
		// StatusResponse for it; waiting would only run out the exchange timeout.
		// Mirrors matter.js
		// packages/protocol/src/interaction/InteractionMessenger.ts:679,701
		// (`suppressResponse` chunk → `expectAckOnly: true`, no StatusResponse
		// wait). Reliable delivery of the final chunk is still guaranteed by
		// sendReplyReliable's MRP retransmit + the post-loop dischargeOwedAck.
		var waitCh <-chan im.StatusCode
		if !chunk.SuppressResponse {
			waitCh = b.armStatusResponseWait(requestHdr.SessionID, proto.ExchangeID, !proto.Initiator)
		}
		// Piggyback the latest peer-sent counter on this chunk's
		// AckCounter. Without this rewrite every chunk carries
		// the stale ReadRequest counter, and python-matter-server's
		// ReliableMessaging drops chunk N+1 after the peer has
		// StatusResponse-acked chunk N — "Dropping message without
		// piggyback ack when we are waiting for an ack". Mirrors
		// the symmetric fix in the Subscribe-Initial loop.
		chunkHdr := *requestHdr
		b.refreshAckCounter(&chunkHdr, proto.ExchangeID, !proto.Initiator)
		if err := b.sendReplyReliable(src, &chunkHdr, proto, im.OpcodeReportData, body); err != nil {
			if waitCh != nil {
				b.disarmStatusResponseWait(requestHdr.SessionID, proto.ExchangeID, !proto.Initiator)
			}
			debugReplyError(b.logger, "send_report", src, err)
			return err
		}
		if waitCh != nil {
			if err := b.awaitChunkStatusResponse(waitCh, "read", src, requestHdr.SessionID, proto.ExchangeID, !proto.Initiator, i, chunk); err != nil {
				return err
			}
		}
		b.logger.Debug("matter.rx.im.read.chunk",
			slog.String("src", srcString(src)),
			slog.Int("chunk", i),
			slog.Int("of", len(chunks)),
			slog.Int("bytes", len(body)),
			slog.Bool("more", chunk.MoreChunkedMessages))
	}
	b.dischargeOwedAck(requestHdr.SessionID, proto.ExchangeID, !proto.Initiator)
	b.logger.Debug("matter.rx.im.read",
		slog.String("src", srcString(src)),
		slog.Int("attribute_reports", len(report.Reports)),
		slog.Int("chunks", len(chunks)))
	return nil
}

// awaitChunkStatusResponse blocks until the peer answers the chunk just
// sent on (session, exchange) with an IM StatusResponse, then decides
// whether the chunk loop may go on. Mirrors matter.js
// InteractionMessenger.ts:719-721 sendDataReportMessage →
// waitForSuccess: a non-Success status or a missing answer aborts the
// interaction — the caller returns the error and sends nothing more on
// the exchange. op names the loop for the log line ("read" /
// "subscribe").
func (b *Bridge) awaitChunkStatusResponse(waitCh <-chan im.StatusCode, op string, src *net.UDPAddr, sessionID, exchangeID uint16, initiator bool, chunkIdx int, chunk im.ReportData) error {
	timeout := b.chunkStatusResponseTimeout(sessionID)
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case status := <-waitCh:
		b.disarmStatusResponseWait(sessionID, exchangeID, initiator)
		if status.IsSuccess() {
			return nil
		}
		b.logger.Debug("matter.tx."+op+".chunk_rejected",
			slog.String("src", srcString(src)),
			slog.Int("chunk", chunkIdx),
			slog.Int("exchange", int(exchangeID)),
			slog.String("status", status.String()))
		return fmt.Errorf("%w: %s", errChunkRejected, status.String())
	case <-timer.C:
		b.disarmStatusResponseWait(sessionID, exchangeID, initiator)
		b.logger.Debug("matter.tx."+op+".chunk_ack_timeout",
			slog.String("src", srcString(src)),
			slog.Int("chunk", chunkIdx),
			slog.Int("exchange", int(exchangeID)),
			slog.Bool("final", !chunk.MoreChunkedMessages),
			slog.String("timeout", timeout.String()))
		return fmt.Errorf("%w: exchange %d chunk %d after %s", errChunkUnanswered, exchangeID, chunkIdx, timeout)
	}
}

// dispatchWriteRequest handles a decoded WriteRequest. The TLV decode and
// dispatcher nil-check are done by the caller (handleIMOpcode).
func (b *Bridge) dispatchWriteRequest(ctx context.Context, src *net.UDPAddr, requestHdr *message.Header, proto message.ProtocolHeader, dispatcher im.Dispatcher, req im.WriteRequest) error {
	for _, w := range req.Writes {
		b.logger.Debug("matter.rx.im.write_path",
			slog.String("src", srcString(src)),
			slog.Any("endpoint", w.Path.Endpoint),
			slog.Any("cluster", w.Path.Cluster),
			slog.Any("attribute", w.Path.Attribute),
			slog.String("value_type", fmt.Sprintf("%T", w.Value.Value)))
	}
	// A chunked write may not suppress the response — matter.js
	// InteractionServer.ts:397-402 rejects the combination with
	// InvalidAction before any timed-interaction handling.
	if req.MoreChunkedMessages && req.SuppressResponse {
		return b.replyTimedStatus(src, requestHdr, proto, "write_chunked_suppress", im.StatusInvalidAction)
	}
	if status, gated := b.checkTimedGate(req.TimedRequest, requestHdr.SessionID, proto.ExchangeID); gated {
		return b.replyTimedStatus(src, requestHdr, proto, "write", status)
	}
	// A write inside a timed interaction may not be chunked — matter.js
	// InteractionServer.ts:408-413 ("Write Request action that is part
	// of a Timed Write Interaction SHALL NOT be chunked"). The gate
	// above passed, so a set TimedRequest flag means the timed window
	// existed and was valid.
	if req.TimedRequest && req.MoreChunkedMessages {
		return b.replyTimedStatus(src, requestHdr, proto, "write_timed_chunked", im.StatusInvalidAction)
	}
	// Stamp the session FabricIndex onto ctx so fabric-scoped writes
	// (AccessControl.ACL above all) resolve the caller's fabric the
	// same way reads do via [im.FabricFilterFromContext]. Without
	// this stamp AccessControl.MatterWrite falls back to fabric=1
	// (last resort) and Apple's post-CommissioningComplete ACL
	// update — which carries the new Resident NodeID + CAT subjects
	// — never reaches the requesting fabric. Apple then reads the
	// ACL from its own fabric on the Subscribe-Initial, sees only
	// the pre-existing case_admin_subject entry, classifies the
	// bridge as missing Administer privilege, and tears the pair
	// down with the iOS "accessory could not be added" dialog.
	// Mirrors the Invoke pathway below.
	writeFabricIndex := b.resolveSessionFabric(requestHdr.SessionID)
	writeSubjectNodeID, writeSubjectCATs := b.resolveSessionSubject(requestHdr.SessionID)
	writeCtx := im.WithFabricFilter(ctx, false, writeFabricIndex)
	writeCtx = im.WithSubject(writeCtx, writeSubjectNodeID, writeSubjectCATs)
	// The ACL write Apple sends right after AddNOC still travels the PASE
	// channel; without this stamp it is evaluated as fabric N / node-id 0
	// and denied.
	if b.resolveSessionPASE(requestHdr.SessionID) {
		writeCtx = im.WithAuthModePASE(writeCtx)
	}
	// The chunks of one chunked write share a transaction, as chip's
	// WriteHandler serves the whole interaction (TC-ACL-2.6, 2.8).
	writeCtx = im.WithWriteTransaction(writeCtx, b.routing.writeTransaction(
		mrp.ExchangeKey{SessionID: requestHdr.SessionID, ExchangeID: proto.ExchangeID, Initiator: !proto.Initiator},
		req.MoreChunkedMessages, time.Now(),
	))
	resp := im.HandleWriteRequest(writeCtx, dispatcher, req)
	b.reportWrittenAttributes(resp)
	// Honor SuppressResponse=true per Matter §10.6.3.1: when the
	// initiator opts out of the WriteResponse the server MUST
	// elide it. matter.js InteractionServer.ts and chip
	// WriteHandler.cpp both gate the reply on this flag. We still
	// drive the side-effects (cluster writes already happened
	// inside HandleWriteRequest) and discharge any owed MRP ack.
	if req.SuppressResponse {
		b.dischargeOwedAck(requestHdr.SessionID, proto.ExchangeID, !proto.Initiator)
		b.logger.Debug("matter.rx.im.write.suppressed",
			slog.String("src", srcString(src)),
			slog.Int("attribute_statuses", len(resp.Responses)))
		return nil
	}
	body, err := EncodeWriteResponse(resp)
	if err != nil {
		debugReplyError(b.logger, "encode_write", src, err)
		return err
	}
	// Reliable: a lost WriteResponse leaves the controller waiting and
	// retrying the whole write. matter.js ships every non-standalone-ack
	// reply on an MRP session reliably (MessageExchange.ts:602
	// `requiresAck ?? (session.usesMrp && !isStandaloneAck)`); the ACK
	// pump retransmits until the peer acks. The response is a pure
	// outcome report, so it meets sendReplyReliable's idempotency
	// contract.
	if err := b.sendReplyReliable(src, requestHdr, proto, im.OpcodeWriteResponse, body); err != nil {
		debugReplyError(b.logger, "send_write", src, err)
		return err
	}
	b.dischargeOwedAck(requestHdr.SessionID, proto.ExchangeID, !proto.Initiator)
	b.logger.Debug("matter.rx.im.write",
		slog.String("src", srcString(src)),
		slog.Int("attribute_statuses", len(resp.Responses)))
	// Surface non-Success statuses one-per-line so failed writes
	// (notably AccessControl CONSTRAINT_ERROR during Apple pair)
	// leave a forensic trail. Apple's homed prints only its own
	// MTRInteractionErrorDomain mapping, never the raw IM status
	// code — without daemon-side logging the chain of cause is
	// impossible to reconstruct from the wire alone.
	for _, r := range resp.Responses {
		if r.Status.Status.IsSuccess() {
			continue
		}
		b.logger.Warn("matter.tx.im.write_status",
			slog.String("src", srcString(src)),
			slog.Any("endpoint", r.Path.Endpoint),
			slog.Any("cluster", r.Path.Cluster),
			slog.Any("attribute", r.Path.Attribute),
			slog.Any("status_code", uint8(r.Status.Status)),
			slog.String("status_name", r.Status.Status.String()),
			slog.Uint64("fabric", uint64(writeFabricIndex)))
	}
	return nil
}

// Timed interactions, as matter.js splits them: the interaction-level gate
// compares the request's own Timed flag with the exchange's timed window
// (a mismatch either way is TIMED_REQUEST_MISMATCH, an expired window
// TIMEOUT — InteractionServer.ts:940-950), and a command marked "T" in the
// matter.js model (schema.IsTimedInvoke) invoked outside a timed interaction
// answers NEEDS_TIMED_INTERACTION for its own path in the InvokeResponse
// (CommandInvokeResponse.ts:291), via [im.WithTimedInteraction].
func (b *Bridge) dispatchInvokeRequest(ctx context.Context, src *net.UDPAddr, requestHdr *message.Header, proto message.ProtocolHeader, dispatcher im.Dispatcher, req im.InvokeRequest) error {
	if status, gated := b.checkTimedGate(req.TimedRequest, requestHdr.SessionID, proto.ExchangeID); gated {
		return b.replyTimedStatus(src, requestHdr, proto, "invoke", status)
	}
	// Batch-invoke path validation: a malformed batch (wildcard-endpoint path
	// mixed with others, a concrete path missing its CommandRef, a duplicate
	// CommandRef, or a duplicate concrete path) is rejected up front with a
	// top-level StatusResponse(InvalidAction) instead of dispatching any
	// command. Mirrors matter.js
	// packages/protocol/src/action/server/CommandInvokeResponse.ts:64-92
	// (process/#processConcrete), whose StatusResponseError(InvalidAction)
	// aborts the whole invoke before a producer runs. Runs after the timed gate
	// because matter.js validates the timed window first (InteractionServer.ts
	// handleInvokeRequest).
	if status := im.ValidateInvokeBatch(req); status != im.StatusSuccess {
		body, err := EncodeStatusResponse(im.StatusResponse{Status: status})
		if err != nil {
			debugReplyError(b.logger, "encode_invoke_batch_reject", src, err)
			return err
		}
		if err := b.sendReply(src, requestHdr, proto, im.OpcodeStatusResponse, body); err != nil {
			debugReplyError(b.logger, "send_invoke_batch_reject", src, err)
			return err
		}
		b.dischargeOwedAck(requestHdr.SessionID, proto.ExchangeID, !proto.Initiator)
		b.logger.Debug("matter.rx.im.invoke.batch_reject",
			slog.String("src", srcString(src)),
			slog.Int("invokes", len(req.Invokes)),
			slog.Any("status_code", uint8(status)))
		return nil
	}
	// Stamp the FabricIndex into the context so cluster handlers
	// can distinguish PASE (0) from CASE (>0) — required by
	// GeneralCommissioning.CommissioningComplete (matter.js
	// GeneralCommissioningServer.ts:218-243), AccessControl Write
	// validation (AuthMode-vs-fabric coupling), and
	// OperationalCredentials AddNOC PASE-only guard.
	invokeFabricIndex := b.resolveSessionFabric(requestHdr.SessionID)
	invokeSubjectNodeID, invokeSubjectCATs := b.resolveSessionSubject(requestHdr.SessionID)
	invokeCtx := im.WithFabricFilter(ctx, false, invokeFabricIndex)
	invokeCtx = im.WithSubject(invokeCtx, invokeSubjectNodeID, invokeSubjectCATs)
	if b.resolveSessionPASE(requestHdr.SessionID) {
		invokeCtx = im.WithAuthModePASE(invokeCtx)
	}
	// Stamp the operational session ID so
	// OperationalCredentials.handleAddNOC can verify it matches
	// the session that issued the CSRRequest (matter.js
	// OperationalCredentialsServer.ts session-ID binding guard).
	invokeCtx = core.WithInvokeSessionID(invokeCtx, requestHdr.SessionID)
	// The gate above passed, so a set Timed flag means a valid window.
	invokeCtx = im.WithTimedInteraction(invokeCtx, req.TimedRequest)
	before := b.snapshotInvokedClusters(invokeCtx, dispatcher, req)
	resp := im.HandleInvokeRequest(invokeCtx, dispatcher, req)
	b.reportInvokeChanges(invokeCtx, dispatcher, before)
	for i := range resp.Responses {
		rewriteInvokeResponseCommand(&resp.Responses[i])
	}
	// SuppressResponse handling per Matter §8.8.3.2.1: a suppress-response invoke
	// still ships its InvokeResponse when a CommandDataIB was generated, but sends
	// nothing when the response carries only CommandStatusIB entries. Mirrors
	// matter.js packages/node/src/node/server/InteractionServer.ts:1043-1074
	// (the held `suppressedBuffer` is discarded — no message sent — when no
	// cmd-response is produced). The command side-effects already ran inside
	// HandleInvokeRequest; we only elide the wire reply and discharge the owed
	// MRP ack (the controller opted out of the StatusResponse handshake, so the
	// Standalone Ack is all it expects).
	if req.SuppressResponse && !resp.HasCommandData() {
		b.dischargeOwedAck(requestHdr.SessionID, proto.ExchangeID, !proto.Initiator)
		b.logger.Debug("matter.rx.im.invoke.suppressed",
			slog.String("src", srcString(src)),
			slog.Int("statuses", len(resp.Responses)))
		return nil
	}
	chunks, err := chunkInvokeResponse(resp, reportChunkPayloadBudget)
	if err != nil {
		debugReplyError(b.logger, "encode_invoke", src, err)
		return err
	}
	// All but the last chunk wait for the controller's StatusResponse
	// before the next goes out, as for a chunked read (matter.js
	// InteractionMessenger.ts sendInvokeResponse → waitForSuccess).
	for i, chunk := range chunks[:len(chunks)-1] {
		waitCh := b.armStatusResponseWait(requestHdr.SessionID, proto.ExchangeID, !proto.Initiator)
		chunkHdr := *requestHdr
		b.refreshAckCounter(&chunkHdr, proto.ExchangeID, !proto.Initiator)
		if err := b.sendReplyReliable(src, &chunkHdr, proto, im.OpcodeInvokeResponse, chunk); err != nil {
			b.disarmStatusResponseWait(requestHdr.SessionID, proto.ExchangeID, !proto.Initiator)
			debugReplyError(b.logger, "send_invoke_chunk", src, err)
			return err
		}
		if err := b.awaitChunkStatusResponse(waitCh, "invoke", src, requestHdr.SessionID, proto.ExchangeID, !proto.Initiator, i, im.ReportData{MoreChunkedMessages: true}); err != nil {
			return err
		}
	}
	body := chunks[len(chunks)-1]
	if len(chunks) > 1 {
		lastHdr := *requestHdr
		b.refreshAckCounter(&lastHdr, proto.ExchangeID, !proto.Initiator)
		requestHdr = &lastHdr
	}
	// Reliable: a lost InvokeResponse surfaces to the controller as
	// "Not Responding" (Apple Home) even though the command executed.
	// matter.js ships every non-standalone-ack reply on an MRP session
	// reliably (MessageExchange.ts:602); the ACK pump retransmits the
	// identical datagram until the peer acks. The response is a pure
	// outcome report, meeting sendReplyReliable's idempotency contract.
	if err := b.sendReplyReliable(src, requestHdr, proto, im.OpcodeInvokeResponse, body); err != nil {
		debugReplyError(b.logger, "send_invoke", src, err)
		return err
	}
	b.dischargeOwedAck(requestHdr.SessionID, proto.ExchangeID, !proto.Initiator)
	// Diagnose: capture Endpoint/Cluster/Command per InvokeRequest +
	// Status per InvokeResponse. Apple retransmit-loops on Invokes
	// whose response is malformed or contains an unexpected status,
	// so we need to see which command Apple insists on and what we
	// reply.
	reqPaths := make([]string, 0, len(req.Invokes))
	for _, ir := range req.Invokes {
		reqPaths = append(reqPaths, fmt.Sprintf("ep=%d cl=0x%04X cmd=0x%X", ir.Path.Endpoint, ir.Path.Cluster, ir.Path.Command))
	}
	respStatuses := make([]string, 0, len(resp.Responses))
	for _, r := range resp.Responses {
		if r.IsStatus {
			respStatuses = append(respStatuses, fmt.Sprintf("status=%d (path=ep=%d cl=0x%04X cmd=0x%X)", r.Status.Status, r.Path.Endpoint, r.Path.Cluster, r.Path.Command))
		} else {
			respStatuses = append(respStatuses, fmt.Sprintf("cmd_data cmd=0x%X has_response=%v", r.Path.Command, r.HasResponse))
		}
	}
	b.logger.Debug("matter.rx.im.invoke",
		slog.String("src", srcString(src)),
		slog.Int("responses", len(resp.Responses)),
		slog.Int("session_id", int(requestHdr.SessionID)),
		slog.Int("invoke_fabric_index", int(invokeFabricIndex)),
		slog.String("req_paths", strings.Join(reqPaths, ",")),
		slog.String("resp", strings.Join(respStatuses, ",")))
	return nil
}

// dispatchTimedRequest handles a decoded TimedRequest.
// TimedRequest gates a follow-up Write / Invoke against a
// per-exchange deadline (Matter §8.7). The bridge captures
// the deadline in `exchangeRouting.timedDeadlines` so the next Write / Invoke
// on the same exchange can be checked against it; expired
// or missing-prior-TimedRequest cases are rejected via
// StatusResponse with the spec-mandated codes.
func (b *Bridge) dispatchTimedRequest(src *net.UDPAddr, requestHdr *message.Header, proto message.ProtocolHeader, req im.TimedRequest) error {
	now := time.Now()
	deadline := now.Add(time.Duration(req.TimeoutMs) * time.Millisecond)
	// Key on (sessionID, exchangeID) so a different session cannot
	// consume a deadline registered by another session.
	b.routing.timedDeadlines.Store(timedKey{sessionID: requestHdr.SessionID, exchangeID: proto.ExchangeID}, deadline)
	// Reclaim abandoned deadlines at the site that creates them: a
	// controller whose follow-up Write / Invoke never arrives leaves an
	// entry nothing consumes, so the table would otherwise grow for the
	// daemon's whole uptime. Amortised — see [timedSweepInterval].
	b.routing.maybeSweepExpiredTimedDeadlines(now)
	body, err := EncodeStatusResponse(im.StatusResponse{Status: im.StatusSuccess})
	if err != nil {
		debugReplyError(b.logger, "encode_status", src, err)
		return err
	}
	// Reliable: this StatusResponse is the go-ahead the controller waits
	// for before sending its timed Write/Invoke (Matter §8.7). If it is
	// dropped the timed action never arrives and the exchange dies.
	// matter.js ships it reliably (MessageExchange.ts:602).
	if err := b.sendReplyReliable(src, requestHdr, proto, im.OpcodeStatusResponse, body); err != nil {
		debugReplyError(b.logger, "send_status", src, err)
		return err
	}
	b.dischargeOwedAck(requestHdr.SessionID, proto.ExchangeID, !proto.Initiator)
	b.logger.Debug("matter.rx.im.timed",
		slog.String("src", srcString(src)),
		slog.Int("timeout_ms", int(req.TimeoutMs)))
	return nil
}

// reportWrittenAttributes marks every successfully written attribute dirty
// for the subscriptions that cover it. (The dispatcher has already advanced
// the cluster's DataVersion: endpoint/dispatcher.go WriteAuthorized for a
// bridged endpoint, the server's own tracker for the root.) A write is a state change
// like any other: matter.js commits it to the behavior's state, whose
// Datasource advances the version and broadcasts the changed property to
// every subscriber (Datasource.ts). Without this a subscriber learned of a
// write — its own or another controller's — only when the cluster happened
// to fire a change notification of its own; a root attribute such as
// BasicInformation.NodeLabel was never reported at all. Found by the CHIP
// Python harness (TC-IDM-2.3 step 4).
func (b *Bridge) reportWrittenAttributes(resp im.WriteResponse) {
	mgr := b.subscriptionManagerLocked()
	for _, r := range resp.Responses {
		if !r.Status.Status.IsSuccess() || !r.Path.HasEndpoint || !r.Path.HasCluster || !r.Path.HasAttribute {
			continue
		}
		if mgr != nil {
			mgr.OnAttributeChanged(im.ConcreteAttributePath{
				Endpoint: r.Path.Endpoint, Cluster: r.Path.Cluster, Attribute: r.Path.Attribute,
				HasEndpoint: true, HasCluster: true, HasAttribute: true,
			})
		}
	}
}

// invokedCluster names one cluster instance an invoke ran against.
type invokedCluster struct {
	endpoint uint16
	cluster  uint32
}

// snapshotInvokedClusters renders every attribute of each cluster a
// concrete invoke path names, before the commands run. A subscription
// manager is required for the comparison to matter; without one this is a
// no-op.
func (b *Bridge) snapshotInvokedClusters(ctx context.Context, d im.Dispatcher, req im.InvokeRequest) map[invokedCluster]map[uint32]string {
	if b.subscriptionManagerLocked() == nil {
		return nil
	}
	out := map[invokedCluster]map[uint32]string{}
	for _, inv := range req.Invokes {
		if !inv.Path.HasEndpoint {
			continue
		}
		key := invokedCluster{inv.Path.Endpoint, inv.Path.Cluster}
		if _, done := out[key]; !done {
			out[key] = renderCluster(ctx, d, key)
		}
	}
	return out
}

// reportInvokeChanges marks dirty every attribute whose value a command
// changed, after advancing a bridged cluster's DataVersion. A command is a
// state change like a write: matter.js commits whatever the command handler
// assigned to the behavior's state, and its Datasource advances the version
// and reports the changed properties (Datasource.ts). Root servers here
// change their state inside MatterInvoke without a change notification —
// GeneralCommissioning's Breadcrumb on ArmFailSafe, OperationalCredentials'
// Fabrics on AddNOC — so without this comparison no subscriber ever saw
// those changes. Found by the CHIP Python harness (TC-IDM-1.5 subscribes to
// Breadcrumb and arms the fail-safe).
func (b *Bridge) reportInvokeChanges(ctx context.Context, d im.Dispatcher, before map[invokedCluster]map[uint32]string) {
	mgr := b.subscriptionManagerLocked()
	if mgr == nil || len(before) == 0 {
		return
	}
	topo := b.Topology()
	for key, old := range before {
		now := renderCluster(ctx, d, key)
		bumped := false
		for attr, v := range now {
			if attr >= 0xFFF8 {
				continue
			}
			if prev, ok := old[attr]; ok && prev == v {
				continue
			}
			if !bumped && topo != nil {
				if ep := topo.FindByID(key.endpoint); ep != nil && !ep.IsRoot() && !ep.IsAggregator() {
					ep.BumpClusterDataVersion(key.cluster)
				}
				bumped = true
			}
			mgr.OnAttributeChanged(im.ConcreteAttributePath{
				Endpoint: key.endpoint, Cluster: key.cluster, Attribute: attr,
				HasEndpoint: true, HasCluster: true, HasAttribute: true,
			})
		}
	}
}

// renderCluster reads every attribute of one cluster instance and renders
// each value for comparison.
func renderCluster(ctx context.Context, d im.Dispatcher, key invokedCluster) map[uint32]string {
	out := map[uint32]string{}
	for _, r := range d.Read(ctx, im.ConcreteAttributePath{Endpoint: key.endpoint, Cluster: key.cluster, HasEndpoint: true, HasCluster: true}) {
		if r.Status == im.StatusSuccess {
			out[r.Path.Attribute] = fmt.Sprintf("%#v", r.Value)
		}
	}
	return out
}

// chunkInvokeResponse encodes an InvokeResponse into one message, or — when
// its responses do not fit one — into as many as needed, every one but the
// last carrying MoreChunkedMessages. Responses are never split; a single
// response too large for a message on its own is replaced by a
// ResourceExhausted status for its path, as matter.js answers a response it
// cannot send. Mirrors matter.js InteractionServer invoke chunking (the
// InvokeResponse split by maxPayloadSize). Found by the CHIP Python harness
// (TC-IDM-1.4 step 11 batches two commands whose responses exceed one
// message).
func chunkInvokeResponse(resp im.InvokeResponse, budget int) ([][]byte, error) {
	whole, err := EncodeInvokeResponse(resp)
	if err != nil {
		return nil, err
	}
	if len(whole) <= budget || len(resp.Responses) <= 1 {
		if len(whole) > reportChunkHardCap && len(resp.Responses) == 1 {
			resp.Responses[0] = oversizedInvokeEntry(resp.Responses[0])
			whole, err = EncodeInvokeResponse(resp)
			if err != nil {
				return nil, err
			}
		}
		return [][]byte{whole}, nil
	}
	var chunks [][]byte
	cur := im.InvokeResponse{SuppressResponse: resp.SuppressResponse}
	flush := func(more bool) error {
		cur.MoreChunkedMessages = more
		body, err := EncodeInvokeResponse(cur)
		if err != nil {
			return err
		}
		chunks = append(chunks, body)
		cur = im.InvokeResponse{SuppressResponse: resp.SuppressResponse}
		return nil
	}
	for _, ent := range resp.Responses {
		trial := cur
		trial.Responses = append(append([]im.InvokeResponseEntry(nil), cur.Responses...), ent)
		trial.MoreChunkedMessages = true
		body, err := EncodeInvokeResponse(trial)
		if err != nil {
			return nil, err
		}
		if len(body) > budget && len(cur.Responses) > 0 {
			if err := flush(true); err != nil {
				return nil, err
			}
			trial = im.InvokeResponse{SuppressResponse: resp.SuppressResponse, Responses: []im.InvokeResponseEntry{ent}, MoreChunkedMessages: true}
			if body, err = EncodeInvokeResponse(trial); err != nil {
				return nil, err
			}
		}
		if len(body) > reportChunkHardCap {
			ent = oversizedInvokeEntry(ent)
		}
		cur.Responses = append(cur.Responses, ent)
	}
	if err := flush(false); err != nil {
		return nil, err
	}
	return chunks, nil
}

// oversizedInvokeEntry answers a response that cannot fit a message with
// ResourceExhausted for its path.
func oversizedInvokeEntry(ent im.InvokeResponseEntry) im.InvokeResponseEntry {
	return im.InvokeResponseEntry{
		Path: ent.Path, CommandRef: ent.CommandRef, HasCommandRef: ent.HasCommandRef,
		IsStatus: true, Status: im.StatusIB{Status: im.StatusResourceExhausted},
	}
}
